package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Web push, self-hosted: WantMyTime signs each message with its own VAPID key
// (RFC 8292) and encrypts it for the browser (RFC 8291, aes128gcm), then posts
// it to the push service the browser chose (Google, Mozilla, Apple). No
// third-party account, no per-message cost. The push service only sees
// ciphertext.

// errPushGone means the browser dropped the subscription (HTTP 404/410); it
// should be deleted.
var errPushGone = errors.New("push subscription is gone")

type vapidKeys struct {
	public  []byte // uncompressed P-256 point, 65 bytes
	private *ecdsa.PrivateKey
	subject string
}

var b64 = base64.RawURLEncoding

// loadVAPID reads VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY (base64url, as
// printed by `aside-api vapid-keys`). Push is off when they are not set.
func loadVAPID() (*vapidKeys, error) {
	pub, priv := strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")), strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY"))
	if pub == "" || priv == "" {
		return nil, nil
	}
	scalar, err := b64.DecodeString(strings.TrimRight(priv, "="))
	if err != nil || len(scalar) != 32 {
		return nil, errors.New("VAPID_PRIVATE_KEY must be a base64url 32-byte P-256 key")
	}
	ecdhKey, err := ecdh.P256().NewPrivateKey(scalar)
	if err != nil {
		return nil, errors.New("VAPID_PRIVATE_KEY is not a valid P-256 key")
	}
	public := ecdhKey.PublicKey().Bytes()
	if given, decErr := b64.DecodeString(strings.TrimRight(pub, "=")); decErr != nil || !bytes.Equal(given, public) {
		return nil, errors.New("VAPID_PUBLIC_KEY does not match VAPID_PRIVATE_KEY")
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), public) //nolint:staticcheck // plain point decoding for ecdsa
	if x == nil {
		return nil, errors.New("VAPID public key could not be decoded")
	}
	key := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, D: new(big.Int).SetBytes(scalar)}
	subject := strings.TrimSpace(os.Getenv("VAPID_SUBJECT"))
	if subject == "" {
		subject = "mailto:support@wantmytime.com"
	}
	return &vapidKeys{public: public, private: key, subject: subject}, nil
}

// generateVAPIDKeys prints a new key pair for the environment.
func generateVAPIDKeys() (public, private string, err error) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return b64.EncodeToString(key.PublicKey().Bytes()), b64.EncodeToString(key.Bytes()), nil
}

// vapidAuthorization is the Authorization header for one push service.
func (v *vapidKeys) vapidAuthorization(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", errors.New("push endpoint must be https")
	}
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": time.Now().Add(12 * time.Hour).Unix(), "sub": v.subject})
	signing := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, v.private, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + b64.EncodeToString(v.public), nil
}

// encryptPush encrypts a payload for one subscription (RFC 8291).
func encryptPush(payload []byte, p256dh, auth string) ([]byte, error) {
	uaPublic, err := b64.DecodeString(strings.TrimRight(p256dh, "="))
	if err != nil {
		return nil, errors.New("invalid subscription key")
	}
	authSecret, err := b64.DecodeString(strings.TrimRight(auth, "="))
	if err != nil || len(authSecret) != 16 {
		return nil, errors.New("invalid subscription auth secret")
	}
	asKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return nil, err
	}
	return encryptPushWith(payload, uaPublic, authSecret, asKey, salt)
}

// encryptPushWith is encryptPush with the sender key and salt supplied.
func encryptPushWith(payload, uaPublic, authSecret []byte, asKey *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	uaKey, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, errors.New("invalid subscription key")
	}
	shared, err := asKey.ECDH(uaKey)
	if err != nil {
		return nil, err
	}
	asPublic := asKey.PublicKey().Bytes()
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// One record: the payload, then the 0x02 last-record delimiter.
	sealed := gcm.Seal(nil, nonce, append(append([]byte{}, payload...), 0x02), nil)
	out := make([]byte, 0, 16+4+1+len(asPublic)+len(sealed))
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, 4096)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	return append(out, sealed...), nil
}

// sendPush delivers one message. ttl is how long the push service may hold
// it for an offline device.
func (v *vapidKeys) sendPush(ctx context.Context, client *http.Client, endpoint, p256dh, auth string, payload []byte, ttl time.Duration) error {
	body, err := encryptPush(payload, p256dh, auth)
	if err != nil {
		return err
	}
	authz, err := v.vapidAuthorization(endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	req.Header.Set("Urgency", "high")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return errPushGone
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return nil
	default:
		return fmt.Errorf("push service answered %d", res.StatusCode)
	}
}

// allowedPushHost keeps subscriptions to the browsers' own push services,
// so the server never posts to an arbitrary address.
func allowedPushHost(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range []string{"fcm.googleapis.com", "android.googleapis.com", "updates.push.services.mozilla.com", "push.apple.com", "notify.windows.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
