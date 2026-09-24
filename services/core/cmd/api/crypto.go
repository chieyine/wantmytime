package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	_ "image/jpeg"
	"os"
	"time"
)

func decodeMFAKey() ([]byte, error) {
	raw := os.Getenv("OPS_MFA_ENCRYPTION_KEY")
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("OPS_MFA_ENCRYPTION_KEY must be base64 encoding of 32 bytes")
	}
	return key, nil
}

func decryptMFASecret(ciphertext []byte) ([]byte, error) {
	key, err := decodeMFAKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted MFA secret")
	}
	return gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
}

func meetingLinkKey(a *API) ([]byte, error) {
	raw := os.Getenv("MEETING_LINK_ENCRYPTION_KEY")
	if raw == "" && a.env != "production" {
		sum := sha256.Sum256(a.sessionKey)
		return sum[:], nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("MEETING_LINK_ENCRYPTION_KEY must be base64 encoding of 32 bytes")
	}
	return key, nil
}

func (a *API) encryptMeetingLink(plaintext []byte) ([]byte, error) {
	key, err := meetingLinkKey(a)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...), nil
}

func (a *API) decryptMeetingLink(ciphertext []byte) ([]byte, error) {
	key, err := meetingLinkKey(a)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted meeting link")
	}
	return gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
}

func encryptMFASecret(secret []byte) ([]byte, error) {
	key, err := decodeMFAKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, secret, nil)...), nil
}

func validTOTP(secret []byte, code string, now time.Time) bool {
	_, ok := matchTOTPStep(secret, code, now)
	return ok
}

// matchTOTPStep returns the 30-second time step the code belongs to, so the
// caller can refuse to accept the same step twice (replay protection).
func matchTOTPStep(secret []byte, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for offset := int64(-1); offset <= 1; offset++ {
		step := now.Unix()/30 + offset
		counter := uint64(step)
		var msg [8]byte
		for i := 7; i >= 0; i-- {
			msg[i] = byte(counter)
			counter >>= 8
		}
		mac := hmac.New(sha1.New, secret)
		mac.Write(msg[:])
		sum := mac.Sum(nil)
		index := sum[len(sum)-1] & 0x0f
		value := (uint32(sum[index])&0x7f)<<24 | uint32(sum[index+1])<<16 | uint32(sum[index+2])<<8 | uint32(sum[index+3])
		if subtle.ConstantTimeCompare([]byte(fmt.Sprintf("%06d", value%1000000)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// calendarTokenKey protects Google refresh tokens. Outside production a key is
// derived from the session secret so local development needs no setup.
func calendarTokenKey(a *API) ([]byte, error) {
	raw := os.Getenv("CALENDAR_TOKEN_ENCRYPTION_KEY")
	if raw == "" && a.env != "production" {
		sum := sha256.Sum256(append([]byte("calendar-token:"), a.sessionKey...))
		return sum[:], nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("CALENDAR_TOKEN_ENCRYPTION_KEY must be base64 encoding of 32 bytes")
	}
	return key, nil
}

// sealCalendarToken encrypts a refresh token bound to its seller (AAD), so a
// ciphertext copied onto another seller's row cannot be decrypted there.
func (a *API) sealCalendarToken(sellerID string, token []byte) ([]byte, error) {
	key, err := calendarTokenKey(a)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, token, []byte("calendar:"+sellerID))...), nil
}

func (a *API) openCalendarToken(sellerID string, ciphertext []byte) ([]byte, error) {
	key, err := calendarTokenKey(a)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted calendar token")
	}
	return gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], []byte("calendar:"+sellerID))
}

// payoutAccountKey protects sellers' bank account numbers. Outside production
// a key is derived from the session secret so local development needs no setup.
func payoutAccountKey(a *API) ([]byte, error) {
	raw := os.Getenv("PAYOUT_ACCOUNT_ENCRYPTION_KEY")
	if raw == "" && a.env != "production" {
		sum := sha256.Sum256(append([]byte("payout-account:"), a.sessionKey...))
		return sum[:], nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("PAYOUT_ACCOUNT_ENCRYPTION_KEY must be base64 encoding of 32 bytes")
	}
	return key, nil
}

// sealPayoutAccount encrypts a bank account number bound to its seller.
func (a *API) sealPayoutAccount(sellerID, accountNumber string) ([]byte, error) {
	key, err := payoutAccountKey(a)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, []byte(accountNumber), []byte("payout-account:"+sellerID))...), nil
}

func (a *API) openPayoutAccount(sellerID string, ciphertext []byte) (string, error) {
	key, err := payoutAccountKey(a)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted payout account")
	}
	plain, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], []byte("payout-account:"+sellerID))
	return string(plain), err
}
