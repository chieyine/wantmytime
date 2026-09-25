//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRateLimitsApplyPerAddress(t *testing.T) {
	h := newHarness(t)
	// A server with real limits (the shared harness multiplies them).
	h.api = &API{db: itPool, env: "local", sessionKey: h.api.sessionKey, mailer: itMail.deliver}
	h.server = httptest.NewServer(h.api.routes())
	t.Cleanup(h.server.Close)
	anon := h.client("")
	for i := 0; i < 20; i++ {
		if res := anon.do("POST", "/api/v1/quotes", map[string]any{}); res.Status == 429 {
			t.Fatalf("request %d was limited early", i+1)
		}
	}
	res := anon.do("POST", "/api/v1/quotes", map[string]any{})
	if res.Status != 429 || !strings.Contains(string(res.Body), "RATE_LIMITED") {
		t.Fatalf("21st checkout request: %d %s", res.Status, res.Body)
	}
	// Webhooks are never limited by the general write limit.
	for i := 0; i < 130; i++ {
		if res = anon.do("POST", "/api/v1/webhooks/kora", "{}"); res.Status == 429 {
			t.Fatal("provider webhook was rate limited")
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t)
	res, err := http.Get(h.server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for header, want := range map[string]string{"Content-Security-Policy": "default-src 'none'", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Cross-Origin-Resource-Policy": "same-site"} {
		if got := res.Header.Get(header); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if res.Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent outside production")
	}
	t.Setenv("APP_ENV", "production")
	rec := httptest.NewRecorder()
	secureHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Header().Get("Strict-Transport-Security"), "max-age=63072000") {
		t.Error("HSTS missing in production")
	}
}

func uploadPhoto(t *testing.T, s seller) int {
	t.Helper()
	var form bytes.Buffer
	boundary := "wmt-test-boundary"
	form.WriteString("--" + boundary + "\r\nContent-Disposition: form-data; name=\"avatar\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\n")
	form.Write(tinyPNG())
	form.WriteString("\r\n--" + boundary + "--\r\n")
	res := s.do("PUT", "/api/v1/me/avatar", form.Bytes(), "Content-Type", "multipart/form-data; boundary="+boundary)
	if res.Status != 200 {
		t.Fatalf("photo upload: %d %s", res.Status, res.Body)
	}
	return int(res.json(t)["avatar_version"].(float64))
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestProfilePhotoInObjectStorage(t *testing.T) {
	h := newHarness(t)
	bucket, srv := newFakeBucket(t)
	t.Setenv("MEDIA_S3_ENDPOINT", srv.URL)
	t.Setenv("MEDIA_S3_BUCKET", "media")
	t.Setenv("MEDIA_S3_ACCESS_KEY_ID", "test-key")
	t.Setenv("MEDIA_S3_SECRET_ACCESS_KEY", "test-secret")
	store, err := newObjectStoreFromEnv("local")
	if err != nil {
		t.Fatal(err)
	}
	h.api.media = store
	s := h.newSeller("fixed")
	version := uploadPhoto(t, s)
	var inDB bool
	var key string
	if err = itPool.QueryRow(context.Background(), `SELECT avatar_data IS NOT NULL,avatar_key FROM seller_profiles WHERE handle=$1`, s.handle).Scan(&inDB, &key); err != nil {
		t.Fatal(err)
	}
	if inDB || !strings.HasPrefix(key, "avatars/") || bucket.count() != 1 {
		t.Fatalf("photo not moved to the bucket: in database %v, key %q, objects %d", inDB, key, bucket.count())
	}
	anon := h.client("")
	photoPath := "/api/v1/people/" + s.handle + "/avatar?v="
	res := anon.do("GET", photoPath+itoa(version), nil)
	if res.Status != 200 || !bytes.HasPrefix(res.Body, []byte("\x89PNG")) {
		t.Fatalf("photo streamed from bucket: %d", res.Status)
	}
	// With a public bucket domain the API redirects to it.
	store.publicBase = "https://media.example.test"
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	raw, err := noFollow.Get(h.server.URL + photoPath + itoa(version))
	if err != nil {
		t.Fatal(err)
	}
	raw.Body.Close()
	if raw.StatusCode != 302 || raw.Header.Get("Location") != "https://media.example.test/"+key {
		t.Fatalf("public redirect: %d %q", raw.StatusCode, raw.Header.Get("Location"))
	}
	// Replacing the photo removes the old file; deleting removes the new one.
	uploadPhoto(t, s)
	waitFor(t, "old photo removal", func() bool { return bucket.count() == 1 })
	s.expect(204, "DELETE", "/api/v1/me/avatar", nil)
	waitFor(t, "photo removal", func() bool { return bucket.count() == 0 })
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestDataExport(t *testing.T) {
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	h := newHarness(t)
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))
	res := s.do("GET", "/api/v1/me/data-export", nil)
	if res.Status != 200 {
		t.Fatalf("export: %d %s", res.Status, res.Body)
	}
	body := string(res.Body)
	var out map[string]any
	if err := json.Unmarshal(res.Body, &out); err != nil {
		t.Fatal(err)
	}
	account := out["account"].(map[string]any)
	if account["email"] != s.email {
		t.Fatalf("account section: %v", account)
	}
	if profile := out["seller_profile"].(map[string]any); profile["handle"] != s.handle {
		t.Fatalf("profile section: %v", profile)
	}
	if hosted := out["bookings_you_hosted"].([]any); len(hosted) != 1 || hosted[0].(map[string]any)["id"] != bookingID {
		t.Fatalf("hosted bookings: %v", hosted)
	}
	if payout := out["payout_account"].(map[string]any); payout["account_last4"] != "6789" {
		t.Fatalf("payout account: %v", payout)
	}
	for _, secret := range []string{"0123456789", "account_sealed", "token_hash", "encrypted_refresh_token", "code_hash"} {
		if strings.Contains(body, secret) {
			t.Fatalf("export contains %q", secret)
		}
	}
	// A guest session cannot export; a full sign-in as the buyer can.
	buyer.expect(401, "GET", "/api/v1/me/data-export", nil)
	buyerAccount := h.client(buyer.email)
	buyerAccount.signIn("login")
	made := buyerAccount.expect(200, "GET", "/api/v1/me/data-export", nil)["bookings_you_made"].([]any)
	if len(made) != 1 || made[0].(map[string]any)["with_handle"] != s.handle {
		t.Fatalf("buyer bookings: %v", made)
	}
	s.expect(200, "GET", "/api/v1/me/data-export", nil)
	s.expect(200, "GET", "/api/v1/me/data-export", nil)
	s.expect(429, "GET", "/api/v1/me/data-export", nil)
}

func TestAccountDeletion(t *testing.T) {
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	h := newHarness(t)
	ctx := context.Background()
	s := h.newSeller("fixed")
	uploadPhoto(t, s)

	// A buyer with a session still to come must wait.
	buyer := h.guestBuyer()
	h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))
	buyerAccount := h.client(buyer.email)
	buyerAccount.signIn("login")
	check := buyerAccount.expect(200, "GET", "/api/v1/me/deletion", nil)
	if check["can_delete"] != false || len(check["blockers"].([]any)) != 1 {
		t.Fatalf("deletion check: %v", check)
	}
	res := buyerAccount.do("POST", "/api/v1/me/deletion", map[string]string{"confirm_email": buyer.email})
	if res.Status != 409 || !strings.Contains(string(res.Body), "ACCOUNT_HAS_OPEN_ITEMS") {
		t.Fatalf("deletion with an upcoming booking: %d %s", res.Status, res.Body)
	}
	// The seller is blocked by the same booking.
	s.expect(409, "POST", "/api/v1/me/deletion", map[string]string{"confirm_email": s.email})

	// Once the session is long over, both can go.
	if _, err := itPool.Exec(ctx, `UPDATE bookings b SET starts_at=now()-interval '3 days',state='completed' FROM seller_profiles sp WHERE sp.id=b.seller_id AND sp.handle=$1`, s.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := itPool.Exec(ctx, `UPDATE seller_payouts p SET state='paid',paid_at=now() FROM seller_profiles sp WHERE sp.id=p.seller_id AND sp.handle=$1`, s.handle); err != nil {
		t.Fatal(err)
	}
	s.expect(422, "POST", "/api/v1/me/deletion", map[string]string{"confirm_email": "someone@else.test"})
	s.expect(200, "POST", "/api/v1/me/deletion", map[string]string{"confirm_email": strings.ToUpper(s.email)})
	s.expect(401, "GET", "/api/v1/me", nil)

	var status, name string
	var identities, payoutAccounts, sessions int
	if err := itPool.QueryRow(ctx, `SELECT status,display_name,(SELECT count(*) FROM user_identities WHERE user_id=$1),(SELECT count(*) FROM seller_payout_accounts pa JOIN seller_profiles sp ON sp.id=pa.seller_id WHERE sp.user_id=$1),(SELECT count(*) FROM sessions WHERE user_id=$1) FROM users WHERE id=$1`, s.userID).Scan(&status, &name, &identities, &payoutAccounts, &sessions); err != nil {
		t.Fatal(err)
	}
	if status != "deleted" || name != deletedPersonName || identities != 0 || payoutAccounts != 0 || sessions != 0 {
		t.Fatalf("after deletion: status %s, name %s, identities %d, payout accounts %d, sessions %d", status, name, identities, payoutAccounts, sessions)
	}
	var bookings int
	if err := itPool.QueryRow(ctx, `SELECT count(*) FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE sp.user_id=$1`, s.userID).Scan(&bookings); err != nil || bookings != 1 {
		t.Fatalf("booking records must be kept: %d %v", bookings, err)
	}
	anon := h.client("")
	anon.expect(404, "GET", "/api/v1/people/"+s.handle, nil)
	if avail := anon.expect(200, "GET", "/api/v1/handles/"+s.handle+"/availability", nil); avail["available"] != false {
		t.Fatal("deleted person's link is claimable straight away")
	}
	taker := h.client(unique("t") + "@seller.test")
	taker.signIn("claim")
	taker.expect(409, "POST", "/api/v1/me/link", map[string]any{"handle": s.handle, "name": "Impostor", "mode": "fixed", "base_30_minor": 1000000, "durations": []int{30}, "timezone": "Africa/Lagos"})

	// The same email can start over as a new account.
	again := h.client(s.email)
	again.signIn("login")
	if me := again.expect(200, "GET", "/api/v1/me", nil); me["id"] == s.userID {
		t.Fatal("signing in again revived the deleted account")
	}

	// The buyer's name leaves the booking the seller kept.
	buyerAccount.expect(200, "POST", "/api/v1/me/deletion", map[string]string{"confirm_email": buyer.email})
	var buyerName, guestEmail string
	if err := itPool.QueryRow(ctx, `SELECT b.buyer_name,b.guest_email FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE sp.user_id=$1`, s.userID).Scan(&buyerName, &guestEmail); err != nil {
		t.Fatal(err)
	}
	if buyerName != deletedPersonName || guestEmail != "" {
		t.Fatalf("buyer details kept: %q %q", buyerName, guestEmail)
	}
}

func TestRetentionSweep(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.newSeller("fixed")
	var sellerID string
	if err := itPool.QueryRow(ctx, `SELECT id::text FROM seller_profiles WHERE handle=$1`, s.handle).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO email_challenges(id,normalized_email,purpose,code_hash,expires_at) VALUES(gen_random_uuid(),'old@retention.test','login','\x00',now()-interval '3 days')`,
		`INSERT INTO product_events(id,event_name,subject_hash,environment,props,occurred_at,received_at,seller_id) VALUES(gen_random_uuid(),'retention_test','\x00','local','{}',now()-interval '500 days',now()-interval '500 days',$1)`,
		`UPDATE sessions SET expires_at=now()-interval '40 days' WHERE user_id=(SELECT user_id FROM seller_profiles WHERE id=$1)`,
		`INSERT INTO handle_holds(handle,held_until) VALUES('expired-hold-test',now()-interval '1 day')`,
	} {
		args := []any{}
		if strings.Contains(q, "$1") {
			args = append(args, sellerID)
		}
		if _, err := itPool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := h.api.applyRetention(ctx); err != nil {
		t.Fatal(err)
	}
	var challenges, events, sessions, holds int
	if err := itPool.QueryRow(ctx, `SELECT (SELECT count(*) FROM email_challenges WHERE normalized_email='old@retention.test'),(SELECT count(*) FROM product_events WHERE event_name='retention_test'),(SELECT count(*) FROM sessions WHERE user_id=$1),(SELECT count(*) FROM handle_holds WHERE handle='expired-hold-test')`, s.userID).Scan(&challenges, &events, &sessions, &holds); err != nil {
		t.Fatal(err)
	}
	if challenges+events+sessions+holds != 0 {
		t.Fatalf("left behind: challenges %d, events %d, sessions %d, holds %d", challenges, events, sessions, holds)
	}
	// Runs at most hourly.
	h.api.retentionRan = time.Now()
	if err := h.api.maybeApplyRetention(ctx, time.Now().Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h.api.retentionRan.After(time.Now()) {
		t.Fatal("sweep ran again within the hour")
	}
}
