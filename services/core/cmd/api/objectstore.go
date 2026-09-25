package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// objectStore keeps files (profile photos) outside PostgreSQL. It speaks the
// S3 API with AWS Signature Version 4, which Cloudflare R2 accepts, so no SDK
// is needed. Configure it with:
//
//	MEDIA_S3_ENDPOINT          https://<account>.r2.cloudflarestorage.com
//	MEDIA_S3_BUCKET            bucket name
//	MEDIA_S3_ACCESS_KEY_ID     R2 API token key (Object Read & Write, this bucket only)
//	MEDIA_S3_SECRET_ACCESS_KEY R2 API token secret
//	MEDIA_S3_REGION            "auto" for R2 (default)
//	MEDIA_PUBLIC_BASE_URL      optional public bucket domain, e.g. https://media.wantmytime.com
//
// Without a public domain the API streams photos from the bucket itself.
type objectStore struct {
	endpoint   *url.URL
	bucket     string
	accessKey  string
	secretKey  string
	region     string
	publicBase string
	client     *http.Client
	now        func() time.Time
}

var errObjectNotFound = errors.New("object not found")

func newObjectStoreFromEnv(appEnv string) (*objectStore, error) {
	endpoint := strings.TrimSpace(os.Getenv("MEDIA_S3_ENDPOINT"))
	if endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(appEnv != "production" && u.Scheme == "http")) {
		return nil, fmt.Errorf("MEDIA_S3_ENDPOINT must be an https URL")
	}
	s := &objectStore{
		endpoint:  u,
		bucket:    strings.TrimSpace(os.Getenv("MEDIA_S3_BUCKET")),
		accessKey: strings.TrimSpace(os.Getenv("MEDIA_S3_ACCESS_KEY_ID")),
		secretKey: strings.TrimSpace(os.Getenv("MEDIA_S3_SECRET_ACCESS_KEY")),
		region:    envOr("MEDIA_S3_REGION", "auto"),
		client:    &http.Client{Timeout: 10 * time.Second},
		now:       time.Now,
	}
	if s.bucket == "" || s.accessKey == "" || s.secretKey == "" {
		return nil, fmt.Errorf("MEDIA_S3_BUCKET, MEDIA_S3_ACCESS_KEY_ID and MEDIA_S3_SECRET_ACCESS_KEY are required with MEDIA_S3_ENDPOINT")
	}
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("MEDIA_PUBLIC_BASE_URL")), "/"); base != "" {
		pu, err := url.Parse(base)
		if err != nil || pu.Host == "" || (pu.Scheme != "https" && !(appEnv != "production" && pu.Scheme == "http")) {
			return nil, fmt.Errorf("MEDIA_PUBLIC_BASE_URL must be an https URL")
		}
		s.publicBase = base
	}
	return s, nil
}

func (s *objectStore) objectURL(key string) string {
	u := *s.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/" + s.bucket + "/" + key
	return u.String()
}

// publicURL is where browsers fetch a public object, or "" without a public domain.
func (s *objectStore) publicURL(key string) string {
	if s.publicBase == "" {
		return ""
	}
	return s.publicBase + "/" + key
}

func (s *objectStore) put(ctx context.Context, key, contentType, cacheControl string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if cacheControl != "" {
		req.Header.Set("Cache-Control", cacheControl)
	}
	s.sign(req, body)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("object store PUT returned %d", resp.StatusCode)
	}
	return nil
}

func (s *objectStore) get(ctx context.Context, key string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return nil, "", err
	}
	s.sign(req, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", errObjectNotFound
	}
	if resp.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("object store GET returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", fmt.Errorf("object larger than %d bytes", limit)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (s *objectStore) delete(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	s.sign(req, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("object store DELETE returned %d", resp.StatusCode)
	}
	return nil
}

// sign adds an AWS Signature Version 4 Authorization header.
func (s *objectStore) sign(req *http.Request, body []byte) {
	now := s.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	payload := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(payload[:])
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("Host", req.URL.Host)

	names := []string{"host"}
	values := map[string]string{"host": req.URL.Host}
	for name, v := range req.Header {
		lower := strings.ToLower(name)
		if lower == "host" || lower == "authorization" {
			continue
		}
		if strings.HasPrefix(lower, "x-amz-") || lower == "content-type" || lower == "cache-control" {
			names = append(names, lower)
			values[lower] = strings.TrimSpace(strings.Join(v, ","))
		}
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n + ":" + values[n] + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := strings.Join([]string{req.Method, awsEscapePath(req.URL.EscapedPath()), canonicalQuery(req.URL.Query()), canonHeaders.String(), signed, payloadHash}, "\n")
	scope := day + "/" + s.region + "/s3/aws4_request"
	sum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
	key := hmacSHA256([]byte("AWS4"+s.secretKey), day)
	key = hmacSHA256(key, s.region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.accessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+signature)
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// awsEscapePath keeps Go's path escaping but also escapes characters S3
// expects encoded in the canonical request.
func awsEscapePath(p string) string {
	if p == "" {
		return "/"
	}
	return strings.NewReplacer("!", "%21", "'", "%27", "(", "%28", ")", "%29", "*", "%2A", "$", "%24", "+", "%2B", ",", "%2C", ";", "%3B", "=", "%3D", "@", "%40", ":", "%3A").Replace(p)
}

func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, strings.ReplaceAll(url.QueryEscape(k), "+", "%20")+"="+strings.ReplaceAll(url.QueryEscape(v), "+", "%20"))
		}
	}
	return strings.Join(parts, "&")
}
