package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// The expected signature was computed independently with botocore's
// S3SigV4Auth for the same request, credentials and time.
func TestObjectStoreSignatureMatchesAWSReference(t *testing.T) {
	// Over http botocore signs the payload hash, as this client always does.
	endpoint, _ := url.Parse("http://acct.r2.cloudflarestorage.com")
	s := &objectStore{endpoint: endpoint, bucket: "wmt-media", accessKey: "AKIDEXAMPLE", secretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", region: "auto", now: func() time.Time { return time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC) }}
	body := []byte("hello png")
	req, _ := http.NewRequest(http.MethodPut, s.objectURL("avatars/abc/0f1e.png"), bytes.NewReader(body))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	s.sign(req, body)
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20260924/auto/s3/aws4_request, SignedHeaders=cache-control;content-type;host;x-amz-content-sha256;x-amz-date, Signature=d415075f87c612db8b254e40dc1c2de980801386080262123f155565c54ab40a"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("signature mismatch\n got %s\nwant %s", got, want)
	}
}

// fakeBucket is an in-memory S3 endpoint that checks requests are signed.
type fakeBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
}

func newFakeBucket(t *testing.T) (*fakeBucket, *httptest.Server) {
	b := &fakeBucket{objects: map[string][]byte{}, types: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-key/") {
			w.WriteHeader(403)
			return
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			b.objects[r.URL.Path] = data
			b.types[r.URL.Path] = r.Header.Get("Content-Type")
		case http.MethodGet:
			data, ok := b.objects[r.URL.Path]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Type", b.types[r.URL.Path])
			_, _ = w.Write(data)
		case http.MethodDelete:
			delete(b.objects, r.URL.Path)
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(srv.Close)
	return b, srv
}

func (b *fakeBucket) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.objects)
}

func TestObjectStoreRoundTrip(t *testing.T) {
	bucket, srv := newFakeBucket(t)
	t.Setenv("MEDIA_S3_ENDPOINT", srv.URL)
	t.Setenv("MEDIA_S3_BUCKET", "media")
	t.Setenv("MEDIA_S3_ACCESS_KEY_ID", "test-key")
	t.Setenv("MEDIA_S3_SECRET_ACCESS_KEY", "test-secret")
	s, err := newObjectStoreFromEnv("local")
	if err != nil || s == nil {
		t.Fatalf("store: %v", err)
	}
	ctx := context.Background()
	if err = s.put(ctx, "avatars/x/1.png", "image/png", "", []byte("png")); err != nil {
		t.Fatal(err)
	}
	data, typ, err := s.get(ctx, "avatars/x/1.png", 1024)
	if err != nil || string(data) != "png" || typ != "image/png" {
		t.Fatalf("get: %q %q %v", data, typ, err)
	}
	if _, _, err = s.get(ctx, "avatars/x/1.png", 2); err == nil {
		t.Fatal("size limit ignored")
	}
	if err = s.delete(ctx, "avatars/x/1.png"); err != nil || bucket.count() != 0 {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err = s.get(ctx, "avatars/x/1.png", 1024); err != errObjectNotFound {
		t.Fatalf("missing object: %v", err)
	}
	if _, err = newObjectStoreFromEnv("production"); err == nil {
		t.Fatal("plain-http endpoint accepted in production")
	}
}
