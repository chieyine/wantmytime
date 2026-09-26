package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The exact shape of PushSubscription.toJSON() in Chrome, Firefox and Safari.
func TestPushSubscriptionFromABrowserIsAccepted(t *testing.T) {
	key := make([]byte, 65)
	key[0] = 4
	body := `{"endpoint":"https://fcm.googleapis.com/fcm/send/abc123","expirationTime":null,"keys":{"p256dh":"` + b64.EncodeToString(key) + `","auth":"` + b64.EncodeToString(make([]byte, 16)) + `"}}`
	var in pushSubscriptionInput
	if err := decode(httptest.NewRequest("POST", "/api/v1/push/subscriptions", strings.NewReader(body)), &in); err != nil {
		t.Fatalf("browser subscription rejected: %v", err)
	}
	if !in.valid() {
		t.Fatal("browser subscription judged invalid")
	}
	withExpiry := strings.Replace(body, `"expirationTime":null`, `"expirationTime":1790409600000`, 1)
	if err := decode(httptest.NewRequest("POST", "/", strings.NewReader(withExpiry)), &in); err != nil || !in.valid() {
		t.Fatalf("subscription with an expiry rejected: %v", err)
	}
	evil := strings.Replace(body, "fcm.googleapis.com", "evil.example", 1)
	if err := decode(httptest.NewRequest("POST", "/", strings.NewReader(evil)), &in); err != nil || in.valid() {
		t.Fatal("an unknown push service must be refused")
	}
}
