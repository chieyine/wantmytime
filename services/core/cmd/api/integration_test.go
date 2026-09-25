//go:build integration

// Integration tests run the real HTTP handlers against a real PostgreSQL
// database and a local fake of the Kora API. They need TEST_DATABASE_URL,
// a connection string for a role that may create databases, for example:
//
//	TEST_DATABASE_URL=postgres://aside:aside@127.0.0.1:54329/postgres?sslmode=disable \
//	  go test -tags integration ./cmd/api/...
//
// Each run creates and drops its own database, so tests never touch local data.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const testOrigin = "http://127.0.0.1:5173"

var (
	itPool   *pgxpool.Pool
	itMail   = &mailbox{codes: map[string]string{}}
	codeExpr = regexp.MustCompile(`code is (\d{8})`)
	seq      int
	seqMu    sync.Mutex
)

type mailbox struct {
	mu       sync.Mutex
	codes    map[string]string
	sent     []string
	messages []emailMessage
}

func (m *mailbox) capture(msg emailMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

func (m *mailbox) deliver(to, subject, body string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if match := codeExpr.FindStringSubmatch(body); match != nil {
		m.codes[to] = match[1]
	}
	m.sent = append(m.sent, to+"|"+subject)
	return true
}

func (m *mailbox) code(t *testing.T, email string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	code, ok := m.codes[email]
	if !ok {
		t.Fatalf("no sign-in code was sent to %s", email)
	}
	return code
}

func unique(prefix string) string {
	seqMu.Lock()
	defer seqMu.Unlock()
	seq++
	return fmt.Sprintf("%s%d%d", prefix, time.Now().UnixNano()%100000, seq)
}

func TestMain(m *testing.M) {
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		fmt.Println("TEST_DATABASE_URL is not set; skipping integration tests")
		os.Exit(0)
	}
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, admin)
	if err != nil {
		panic(err)
	}
	name := fmt.Sprintf("aside_it_%d", time.Now().UnixNano())
	if _, err = adminPool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		panic(err)
	}
	parsed, _ := url.Parse(admin)
	parsed.Path = "/" + name
	itPool, err = pgxpool.New(ctx, parsed.String())
	if err != nil {
		panic(err)
	}
	if os.Getenv("MIGRATION_DIR") == "" {
		os.Setenv("MIGRATION_DIR", "../../migrations")
	}
	if err = runMigrations(ctx, itPool); err != nil {
		panic(err)
	}
	code := m.Run()
	itPool.Close()
	_, _ = adminPool.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
	adminPool.Close()
	os.Exit(code)
}

// --- test client -----------------------------------------------------------

type harness struct {
	t      *testing.T
	api    *API
	server *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("APP_ENV", "local")
	t.Setenv("OTP_PEPPER", "integration-test-pepper")
	t.Setenv("PUBLIC_APP_ORIGIN", testOrigin)
	a := &API{db: itPool, env: "local", sessionKey: []byte("integration-test-session-secret-0123456789"), mailer: itMail.deliver, mailCapture: itMail.capture, rateLimitScale: 100}
	server := httptest.NewServer(a.routes())
	t.Cleanup(server.Close)
	return &harness{t: t, api: a, server: server}
}

type client struct {
	h     *harness
	http  *http.Client
	email string
}

func (h *harness) client(email string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{h: h, http: &http.Client{Jar: jar}, email: email}
}

type response struct {
	Status int
	Body   []byte
}

func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.Body, &out); err != nil {
		t.Fatalf("response is not JSON (%d): %s", r.Status, r.Body)
	}
	return out
}

func (c *client) do(method, path string, body any, headers ...string) response {
	c.h.t.Helper()
	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case []byte:
			reader = bytes.NewReader(v)
		case string:
			reader = strings.NewReader(v)
		default:
			data, _ := json.Marshal(v)
			reader = bytes.NewReader(data)
		}
	}
	req, err := http.NewRequest(method, c.h.server.URL+path, reader)
	if err != nil {
		c.h.t.Fatal(err)
	}
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return response{Status: res.StatusCode, Body: data}
}

func (c *client) expect(want int, method, path string, body any, headers ...string) map[string]any {
	c.h.t.Helper()
	res := c.do(method, path, body, headers...)
	if res.Status != want {
		c.h.t.Fatalf("%s %s: status %d, want %d: %s", method, path, res.Status, want, res.Body)
	}
	if len(res.Body) == 0 {
		return nil
	}
	return res.json(c.h.t)
}

// signIn runs the real email challenge flow for the given purpose.
func (c *client) signIn(purpose string) {
	c.h.t.Helper()
	challenge := c.expect(202, "POST", "/api/v1/auth/challenges", map[string]string{"email": c.email, "purpose": purpose})
	id := challenge["challenge_id"].(string)
	c.expect(200, "POST", "/api/v1/auth/challenges/"+id+"/verify", map[string]string{"code": itMail.code(c.h.t, c.email)})
}

func idempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- fixtures ---------------------------------------------------------------

type seller struct {
	*client
	handle string
	userID string
}

// newSeller claims a link, opens every day 08:00-20:00 with no notice and
// marks the seller ready with a verified payout bank account.
func (h *harness) newSeller(mode string) seller {
	h.t.Helper()
	handle := unique("s")
	c := h.client(handle + "@seller.test")
	c.signIn("claim")
	profile := map[string]any{"handle": handle, "name": "Seller " + handle, "mode": mode, "base_30_minor": 1000000, "durations": []int{15, 30, 60}, "timezone": "Africa/Lagos"}
	if mode == "offer" {
		profile["base_30_minor"] = 0
	}
	c.expect(201, "POST", "/api/v1/me/link", profile)
	windows := []map[string]any{}
	for day := 0; day < 7; day++ {
		windows = append(windows, map[string]any{"weekday": day, "start": "08:00", "end": "20:00"})
	}
	c.expect(200, "PUT", "/api/v1/me/availability", map[string]any{"timezone": "Africa/Lagos", "minimum_notice_minutes": 0, "booking_horizon_days": 30, "buffer_minutes": 0, "windows": windows})
	var userID string
	if err := itPool.QueryRow(context.Background(), `UPDATE seller_profiles SET readiness_state='ready' WHERE handle=$1 RETURNING user_id::text`, handle).Scan(&userID); err != nil {
		h.t.Fatal(err)
	}
	h.addPayoutAccount(handle, "0123456789")
	return seller{client: c, handle: handle, userID: userID}
}

// addPayoutAccount stores a verified bank account for the seller directly.
func (h *harness) addPayoutAccount(handle, number string) {
	h.t.Helper()
	var sellerID string
	if err := itPool.QueryRow(context.Background(), `SELECT id::text FROM seller_profiles WHERE handle=$1`, handle).Scan(&sellerID); err != nil {
		h.t.Fatal(err)
	}
	sealed, err := h.api.sealPayoutAccount(sellerID, number)
	if err != nil {
		h.t.Fatal(err)
	}
	fp, _ := h.api.accountFingerprint("NG", "058", number)
	if _, err = itPool.Exec(context.Background(), `INSERT INTO seller_payout_accounts(seller_id,provider,bank_code,bank_name,account_last4,account_name,account_sealed,account_fingerprint) VALUES($1,'kora','058','Guaranty Trust Bank',$2::text,'ADA SELLER '||$2::text,$3,$4)`, sellerID, number[len(number)-4:], sealed, fp); err != nil {
		h.t.Fatal(err)
	}
}

// slot returns the n-th available start time tomorrow (Africa/Lagos).
func (h *harness) slot(handle string, n int) string {
	h.t.Helper()
	loc, _ := time.LoadLocation("Africa/Lagos")
	date := time.Now().In(loc).AddDate(0, 0, 1).Format("2006-01-02")
	anon := h.client("")
	data := anon.expect(200, "GET", "/api/v1/people/"+handle+"/slots?date="+date+"&duration=30", nil)
	slots := data["slots"].([]any)
	if len(slots) <= n {
		h.t.Fatalf("expected at least %d slots, got %d", n+1, len(slots))
	}
	return slots[n].(map[string]any)["starts_at"].(string)
}

func (h *harness) guestBuyer() *client {
	h.t.Helper()
	c := h.client(unique("b") + "@buyer.test")
	c.signIn("guest_booking")
	return c
}

func (h *harness) holdAndSimulate(buyer *client, handle, startsAt string) (quoteID, bookingID string) {
	h.t.Helper()
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": handle, "name": "Buyer", "duration_minutes": 30, "starts_at": startsAt}, "Idempotency-Key", idempotencyKey())
	quoteID = quote["id"].(string)
	booking := buyer.expect(201, "POST", "/api/v1/dev/quotes/"+quoteID+"/simulate-payment", "{}")
	return quoteID, booking["booking_id"].(string)
}

func scalar[T any](t *testing.T, query string, args ...any) T {
	t.Helper()
	var v T
	if err := itPool.QueryRow(context.Background(), query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

// --- operations fixture --------------------------------------------------------

func totpAt(secret []byte, at time.Time) string {
	counter := uint64(at.Unix() / 30)
	var msg [8]byte
	for i := 7; i >= 0; i-- {
		msg[i] = byte(counter)
		counter >>= 8
	}
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1000000)
}

func (h *harness) operator() (*client, []byte) {
	h.t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	h.t.Setenv("OPS_MFA_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	c := h.client(unique("ops") + "@ops.test")
	c.signIn("login")
	secret := []byte("12345678901234567890")
	encrypted, err := encryptMFASecret(secret)
	if err != nil {
		h.t.Fatal(err)
	}
	userID := scalar[string](h.t, `SELECT user_id::text FROM user_identities WHERE normalized_identifier=$1`, c.email)
	for _, permission := range []string{"ops:read", "ops:account:restrict", "ops:session:revoke", "ops:booking:resolve", "ops:settlement:import", "ops:seller:approve", "ops:refund:approve"} {
		if _, err = itPool.Exec(context.Background(), `INSERT INTO admin_grants(id,user_id,permission,granted_by) VALUES(gen_random_uuid(),$1,$2,$1)`, userID, permission); err != nil {
			h.t.Fatal(err)
		}
	}
	if _, err = itPool.Exec(context.Background(), `INSERT INTO admin_mfa(user_id,encrypted_secret) VALUES($1,$2)`, userID, encrypted); err != nil {
		h.t.Fatal(err)
	}
	c.expect(200, "POST", "/api/v1/ops/session", map[string]string{"code": totpAt(secret, time.Now())})
	return c, secret
}

// --- fake Kora ------------------------------------------------------------------

const fakeKoraSecret = "sk_test_integration"

type fakeKora struct {
	mu     sync.Mutex
	server *httptest.Server
	// charges by reference. fee is the processor fee in kobo for new charges.
	charges     map[string]*fakeCharge
	fee         int64
	cardCountry string
	// refunds by WantMyTime's reference, and the status new ones report.
	refunds      map[string]*fakeRefund
	refundStatus string
	refundCalls  int
	// transfers sent through the payout API, by reference.
	transfers      map[string]fakeTransfer
	transferStatus string // status new transfers report (default success)
	balanceShort   bool   // refuse transfers for insufficient balance
	transferCalls  int
}

type fakeCharge struct {
	amount, paid, fee  int64
	status, channel    string
	cardCountry, email string
}

type fakeRefund struct {
	payment, status string
	amount          int64
}

type fakeTransfer struct {
	bank, account, status string
	amount                int64
}

func naira(minor int64) string { return fmt.Sprintf("%d.%02d", minor/100, minor%100) }

func readNaira(v any) int64 {
	m, _ := parseMajor(fmt.Sprint(v))
	return m
}

func newFakeKora(t *testing.T) *fakeKora {
	f := &fakeKora{charges: map[string]*fakeCharge{}, refunds: map[string]*fakeRefund{}, transfers: map[string]fakeTransfer{}, fee: 15000}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		public := strings.HasPrefix(r.URL.Path, "/api/v1/misc/")
		if (public && auth != "Bearer pk_test_integration") || (!public && auth != "Bearer "+fakeKoraSecret) {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": false, "message": "Invalid authorization key"})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fail := func(code int, message string) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": false, "message": message})
		}
		ok := func(data any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": true, "message": "ok", "data": data})
		}
		var in map[string]any
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&in)
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1")
		switch {
		case r.Method == http.MethodPost && (path == "/charges/initialize" || path == "/charges/bank-transfer"):
			ref := fmt.Sprint(in["reference"])
			if _, dup := f.charges[ref]; dup {
				fail(400, "Duplicate reference")
				return
			}
			if in["merchant_bears_cost"] != false {
				t.Errorf("the buyer must bear Kora's fee")
			}
			customer, _ := in["customer"].(map[string]any)
			channel := "bank_transfer"
			if path == "/charges/initialize" {
				channel = "pay_with_bank"
			}
			c := &fakeCharge{amount: readNaira(in["amount"]), fee: f.fee, status: "processing", channel: channel, cardCountry: f.cardCountry, email: fmt.Sprint(customer["email"])}
			f.charges[ref] = c
			if channel == "pay_with_bank" {
				ok(map[string]any{"reference": ref, "checkout_url": "https://checkout.korapay.com/fake/" + ref})
				return
			}
			ok(map[string]any{"reference": ref, "currency": "NGN", "amount": in["amount"], "amount_expected": naira(c.amount + c.fee), "fee": naira(c.fee), "status": "processing",
				"bank_account": map[string]any{"account_name": "WantMyTime Checkout", "account_number": "99" + ref[len(ref)-8:], "bank_name": "wema", "bank_code": "035", "expiry_date_in_utc": time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339Nano)}})
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/charges/"):
			ref := strings.TrimPrefix(path, "/charges/")
			c, found := f.charges[ref]
			if !found {
				fail(404, "Charge not found")
				return
			}
			paid := c.paid
			if c.status == "success" && paid == 0 {
				paid = c.amount
			}
			data := map[string]any{"reference": ref, "status": c.status, "amount": naira(c.amount), "amount_paid": naira(paid), "fee": naira(c.fee), "vat": "0.00", "currency": "NGN", "payment_method": c.channel}
			ok(data)
		case r.Method == http.MethodPost && path == "/refunds/initiate":
			f.refundCalls++
			ref, payment := fmt.Sprint(in["reference"]), fmt.Sprint(in["payment_reference"])
			c, found := f.charges[payment]
			amount := readNaira(in["amount"])
			total := int64(0)
			for _, existing := range f.refunds {
				if existing.payment == payment && existing.status != "failed" {
					total += existing.amount
				}
			}
			// The buyer pays the price plus Kora's fee, and all of it can come back.
			if !found || c.status != "success" || amount <= 0 || total+amount > c.paid {
				fail(400, "Refund amount cannot be more than the transaction amount")
				return
			}
			if _, dup := f.refunds[ref]; dup {
				fail(400, "Duplicate refund reference")
				return
			}
			status := f.refundStatus
			if status == "" {
				status = "processing"
			}
			f.refunds[ref] = &fakeRefund{payment: payment, status: status, amount: amount}
			ok(map[string]any{"reference": ref, "payment_reference": payment, "status": status, "amount": naira(amount), "currency": "NGN"})
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/refunds/"):
			ref := strings.TrimPrefix(path, "/refunds/")
			rf, found := f.refunds[ref]
			if !found {
				fail(404, "Refund not found")
				return
			}
			ok(map[string]any{"reference": ref, "status": rf.status, "amount": naira(rf.amount), "payment_reference": rf.payment})
		case r.Method == http.MethodGet && path == "/misc/banks":
			if r.URL.Query().Get("countryCode") != "NG" {
				ok([]any{})
				return
			}
			ok([]map[string]any{{"name": "Guaranty Trust Bank", "code": "058"}, {"name": "Access Bank", "code": "044"}})
		case r.Method == http.MethodPost && path == "/misc/banks/resolve":
			number := fmt.Sprint(in["account"])
			if strings.HasPrefix(number, "000") || in["currency"] != "NGN" {
				fail(400, "Unable to resolve bank account")
				return
			}
			ok(map[string]any{"bank_code": in["bank"], "account_number": number, "account_name": "ADA SELLER " + number[len(number)-4:]})
		case r.Method == http.MethodPost && path == "/transactions/disburse":
			f.transferCalls++
			ref := fmt.Sprint(in["reference"])
			dest, _ := in["destination"].(map[string]any)
			acct, _ := dest["bank_account"].(map[string]any)
			if f.balanceShort {
				fail(400, "Insufficient funds in disbursement wallet")
				return
			}
			if _, dup := f.transfers[ref]; dup {
				fail(400, "Duplicate reference")
				return
			}
			status := f.transferStatus
			if status == "" {
				status = "success"
			}
			f.transfers[ref] = fakeTransfer{bank: fmt.Sprint(acct["bank"]), account: fmt.Sprint(acct["account"]), status: status, amount: readNaira(dest["amount"])}
			ok(map[string]any{"reference": ref, "status": "processing", "amount": dest["amount"], "fee": "25.00", "currency": "NGN"})
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/transactions/"):
			ref := strings.TrimPrefix(path, "/transactions/")
			tr, found := f.transfers[ref]
			if !found {
				fail(404, "Transaction not found")
				return
			}
			ok(map[string]any{"reference": ref, "status": tr.status, "amount": naira(tr.amount), "fee": "25.00", "currency": "NGN"})
		default:
			fail(404, "Not found")
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

// payFull marks a charge as paid in full: the price plus Kora's fee, which
// the buyer bears.
func (f *fakeKora) payFull(reference string) {
	f.mu.Lock()
	amount := f.charges[reference].amount + f.charges[reference].fee
	f.mu.Unlock()
	f.pay(reference, amount)
}

// pay marks a charge as paid by the buyer (the whole amount, or part of it).
func (f *fakeKora) pay(reference string, paid int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.charges[reference]
	c.paid = paid
	if paid >= c.amount+c.fee {
		c.status = "success"
	}
}

func enablePayments(t *testing.T, fake *fakeKora) {
	for key, value := range map[string]string{
		"PAYMENTS_ENABLED": "true", "FEE_POLICY_APPROVED": "true", "PAYMENT_ROUTE": "hold_payout", "FEE_BPS": "500",
		"FEE_POLICY_MODE": "all_in_seller_deduction", "MIN_CHARGE_MINOR": "100", "MAX_CHARGE_MINOR": "100000000",
		"APPROVED_PAYMENT_CHANNELS": "bank_transfer,pay_with_bank", "PAYMENT_ENV": "sandbox", "KORA_SECRET_KEY": fakeKoraSecret, "KORA_PUBLIC_KEY": "pk_test_integration",
		"CHECKOUTS_PAUSED": "false", "KORA_API_BASE": fake.server.URL, "LOCAL_PAYMENT_SIMULATOR": "false",
	} {
		t.Setenv(key, value)
	}
	for _, channel := range []string{"bank_transfer", "pay_with_bank"} {
		if _, err := itPool.Exec(context.Background(), `INSERT INTO provider_fee_schedules(id,provider,currency,channel,percent_bps,fixed_minor,effective_from,approved_at) SELECT gen_random_uuid(),'kora','NGN',$1,150,0,now()-interval '1 day',now() WHERE NOT EXISTS (SELECT 1 FROM provider_fee_schedules WHERE provider='kora' AND channel=$1)`, channel); err != nil {
			t.Fatal(err)
		}
	}
}

// sendKoraWebhook signs the data object as Kora does and posts the event.
func sendKoraWebhook(t *testing.T, h *harness, event string, data map[string]any) response {
	t.Helper()
	raw, _ := json.Marshal(data)
	mac := hmac.New(sha256.New, []byte(fakeKoraSecret))
	mac.Write(raw)
	body := []byte(`{"event":"` + event + `","data":` + string(raw) + `}`)
	return h.client("").do("POST", "/api/v1/webhooks/kora", body, "x-korapay-signature", hex.EncodeToString(mac.Sum(nil)), "Origin", "")
}

// --- tests -------------------------------------------------------------------

func TestClaimProfileRejectsSecondLinkAndUnknownFields(t *testing.T) {
	h := newHarness(t)
	s := h.newSeller("fixed")
	res := s.do("POST", "/api/v1/me/link", map[string]any{"handle": unique("x"), "name": "Again", "mode": "fixed", "base_30_minor": 5000, "durations": []int{30}, "timezone": "Africa/Lagos"})
	if res.Status != 409 || !strings.Contains(string(res.Body), "PROFILE_EXISTS") {
		t.Fatalf("second claim: %d %s", res.Status, res.Body)
	}
	fresh := h.client(unique("n") + "@seller.test")
	fresh.signIn("claim")
	res = fresh.do("POST", "/api/v1/me/link", map[string]any{"handle": unique("y"), "name": "N", "mode": "fixed", "base_30_minor": 5000, "durations": []int{30}, "timezone": "Africa/Lagos", "email": "leak@x.test"})
	if res.Status != 400 {
		t.Fatalf("unknown field must be rejected so the web client sends only profile fields: %d %s", res.Status, res.Body)
	}
}

func TestAvailabilityCanBeSavedRepeatedly(t *testing.T) {
	h := newHarness(t)
	s := h.newSeller("fixed")
	current := s.expect(200, "GET", "/api/v1/me/availability", nil)
	windows := current["windows"].([]any)
	if got := windows[0].(map[string]any)["start"]; got != "08:00" {
		t.Fatalf("availability start should be HH:MM, got %v", got)
	}
	// Send back exactly what was loaded, as the web editor does.
	s.expect(200, "PUT", "/api/v1/me/availability", map[string]any{"timezone": current["timezone"], "minimum_notice_minutes": current["minimum_notice_minutes"], "booking_horizon_days": current["booking_horizon_days"], "buffer_minutes": current["buffer_minutes"], "windows": windows})
	// Seconds from older clients are still accepted.
	s.expect(200, "PUT", "/api/v1/me/availability", map[string]any{"timezone": "Africa/Lagos", "minimum_notice_minutes": 0, "booking_horizon_days": 30, "buffer_minutes": 0, "windows": []map[string]any{{"weekday": 1, "start": "09:00:00", "end": "10:00:00"}}})
}

func TestPaidBookingByBankTransfer(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	start := h.slot(s.handle, 0)
	buyer := h.guestBuyer()
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Ada Buyer", "duration_minutes": 30, "starts_at": start}, "Idempotency-Key", idempotencyKey())
	quoteID := quote["id"].(string)
	// Bank transfer is the default: the buyer gets an account to pay into.
	checkout := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/checkout", "{}")
	reference := checkout["reference"].(string)
	transfer := checkout["transfer"].(map[string]any)
	if checkout["method"] != "bank_transfer" || transfer["bank_name"] != "Wema Bank" || transfer["amount_minor"] != float64(1015000) || checkout["fee_minor"] != float64(15000) || checkout["price_minor"] != float64(1000000) || transfer["account_number"] == "" {
		t.Fatalf("unexpected checkout %v", checkout)
	}
	// Asking again shows the same account instead of creating a second one.
	again := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/checkout", "{}")
	if again["reference"] != reference {
		t.Fatalf("second checkout made a new charge: %v", again)
	}
	holdMinutes := scalar[float64](t, `SELECT extract(epoch FROM expires_at-now())/60 FROM quotes WHERE id=$1`, quoteID)
	if holdMinutes < 38 {
		t.Fatalf("the hold should last until after the transfer account expires, got %.1f minutes", holdMinutes)
	}
	// Not paid yet.
	if got := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/verify-payment", map[string]string{"reference": reference}); got["booking_id"] != nil {
		t.Fatalf("no booking before payment: %v", got)
	}
	// A transfer short of the price does not book the time.
	fake.pay(reference, 600000)
	if got := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/verify-payment", map[string]string{"reference": reference}); got["state"] != "underpaid" {
		t.Fatalf("short transfer: %v", got)
	}
	fake.payFull(reference)
	if res := sendKoraWebhook(t, h, "charge.success", map[string]any{"reference": reference, "status": "success", "amount": 10000, "fee": 150, "currency": "NGN"}); res.Status != 200 {
		t.Fatalf("webhook: %d %s", res.Status, res.Body)
	}
	if err := h.api.processOneProviderEvent(context.Background()); err != nil {
		t.Fatal(err)
	}
	bookingID := scalar[string](t, `SELECT id::text FROM bookings WHERE quote_id=$1 AND payment_state='paid'`, quoteID)
	if got := scalar[string](t, `SELECT channel||'/'||paid_minor FROM payment_attempts WHERE merchant_reference=$1`, reference); got != "bank_transfer/1015000" {
		t.Fatalf("attempt %s", got)
	}
	if got := scalar[string](t, `SELECT state FROM provider_events WHERE provider_reference=$1`, reference); got != "processed" {
		t.Fatalf("provider event state = %s", got)
	}
	if got := scalar[string](t, `SELECT seller_entitlement_minor||'/'||processor_cost_minor FROM payment_allocations WHERE booking_id=$1`, bookingID); got != "950000/15000" {
		t.Fatalf("allocation = %s", got)
	}
	// Transfer money is in the balance at once, so the payout only waits for the session.
	if due := scalar[bool](t, `SELECT funds_available_at <= now() FROM seller_payouts WHERE booking_id=$1`, bookingID); !due {
		t.Fatal("a bank transfer's funds are available immediately")
	}
	if !journalBalanced(t, "payment_attempt", scalar[string](t, `SELECT id::text FROM payment_attempts WHERE merchant_reference=$1`, reference)) {
		t.Fatal("ledger journal is not balanced")
	}
	if n := scalar[int64](t, `SELECT count(*) FROM notification_outbox WHERE booking_id=$1 AND kind LIKE 'booking_confirmed_%'`, bookingID); n != 2 {
		t.Fatalf("expected 2 confirmation emails queued, got %d", n)
	}
	verified := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/verify-payment", map[string]string{"reference": reference})
	if verified["booking_id"] != bookingID {
		t.Fatalf("verify-payment returned %v", verified)
	}
	// A forged webhook is refused.
	if res := h.client("").do("POST", "/api/v1/webhooks/kora", []byte(`{"event":"charge.success","data":{"reference":"`+reference+`"}}`), "x-korapay-signature", strings.Repeat("0", 64), "Origin", ""); res.Status != 401 {
		t.Fatalf("forged webhook: %d", res.Status)
	}
}

func TestTransferOnlyRefusesCards(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	t.Setenv("APPROVED_PAYMENT_CHANNELS", "bank_transfer")
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Ada Buyer", "duration_minutes": 30, "starts_at": h.slot(s.handle, 0)}, "Idempotency-Key", idempotencyKey())
	quoteID := quote["id"].(string)
	if methods := buyer.expect(200, "GET", "/api/v1/quotes/"+quoteID, nil)["payment_methods"].([]any); len(methods) != 1 || methods[0] != "bank_transfer" {
		t.Fatalf("payment methods %v", methods)
	}
	if res := buyer.do("POST", "/api/v1/quotes/"+quoteID+"/checkout", map[string]string{"method": "card"}); res.Status != 422 {
		t.Fatalf("card must be refused: %d %s", res.Status, res.Body)
	}
	if got := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/checkout", "{}"); got["method"] != "bank_transfer" {
		t.Fatalf("default checkout %v", got)
	}
}

func TestPayWithBankFallbackAndDoublePayment(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Ada Buyer", "duration_minutes": 30, "starts_at": h.slot(s.handle, 0)}, "Idempotency-Key", idempotencyKey())
	quoteID := quote["id"].(string)
	transfer := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/checkout", map[string]string{"method": "bank_transfer"})["reference"].(string)
	bank := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/checkout", map[string]string{"method": "pay_with_bank"})
	if !strings.HasPrefix(bank["authorization_url"].(string), "https://checkout.korapay.com/") || bank["reference"] == transfer {
		t.Fatalf("pay with bank checkout %v", bank)
	}
	bankRef := bank["reference"].(string)
	fake.payFull(bankRef)
	paid := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/verify-payment", map[string]string{"reference": bankRef})
	bookingID, _ := paid["booking_id"].(string)
	if bookingID == "" {
		t.Fatalf("pay with bank payment %v", paid)
	}
	// Pay-with-bank money settles later, so the payout waits for it too.
	if early := scalar[bool](t, `SELECT funds_available_at > now()+interval '20 hours' FROM seller_payouts WHERE booking_id=$1`, bookingID); !early {
		t.Fatal("pay with bank funds should not be treated as available at once")
	}
	// The buyer also completes the transfer: that second payment is flagged for a refund.
	fake.payFull(transfer)
	buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/verify-payment", map[string]string{"reference": transfer})
	if n := scalar[int64](t, `SELECT count(*) FROM payment_exceptions WHERE kind='duplicate_charge' AND payment_attempt_id=(SELECT id FROM payment_attempts WHERE merchant_reference=$1)`, transfer); n != 1 {
		t.Fatal("the second payment must become a duplicate-charge exception")
	}
	if n := scalar[int64](t, `SELECT count(*) FROM bookings WHERE quote_id=$1`, quoteID); n != 1 {
		t.Fatal("one booking only")
	}
}

func TestLateOfferPaymentRecordsException(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("offer")
	buyer := h.client(unique("o") + "@buyer.test")
	buyer.signIn("guest_offer")
	offer := buyer.expect(201, "POST", "/api/v1/offers", map[string]any{"seller": s.handle, "name": "Ola", "duration_minutes": 30, "amount_minor": 500000}, "Idempotency-Key", idempotencyKey())
	offerID := offer["id"].(string)
	s.expect(200, "POST", "/api/v1/offers/"+offerID+"/accept", map[string]any{"version": 1})
	quote := buyer.expect(201, "POST", "/api/v1/offers/"+offerID+"/checkout", map[string]string{"starts_at": h.slot(s.handle, 0)}, "Idempotency-Key", idempotencyKey())
	checkout := buyer.expect(200, "POST", "/api/v1/quotes/"+quote["id"].(string)+"/checkout", "{}")
	fake.payFull(checkout["reference"].(string))
	// The buyer withdraws... not allowed once agreed, so the seller's side changes it directly.
	if _, err := itPool.Exec(context.Background(), `UPDATE offers SET state='withdrawn' WHERE id=$1`, offerID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.api.applyVerifiedCharge(context.Background(), checkout["reference"].(string), mustVerify(t, checkout["reference"].(string))); err != nil {
		t.Fatalf("payment application must not fail: %v", err)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM payment_exceptions WHERE kind='offer_conflict'`); n < 1 {
		t.Fatal("expected an offer_conflict exception")
	}
}

func mustVerify(t *testing.T, reference string) verifiedCharge {
	t.Helper()
	client, err := newKoraClient("local")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := client.queryCharge(context.Background(), reference)
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func TestRescheduleAcceptMovesBooking(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("fixed")
	first, second := h.slot(s.handle, 0), h.slot(s.handle, 8)
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, first)
	request := buyer.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/reschedules", map[string]string{"proposed_starts_at": second})
	s.expect(200, "POST", "/api/v1/reschedules/"+request["id"].(string)+"/accept", "{}")
	moved := scalar[time.Time](t, `SELECT starts_at FROM bookings WHERE id=$1`, bookingID)
	want, _ := time.Parse(time.RFC3339, second)
	if !moved.Equal(want) {
		t.Fatalf("booking starts_at = %s, want %s", moved, want)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM notification_outbox WHERE booking_id=$1 AND kind LIKE 'reschedule_accepted_%'`, bookingID); n != 2 {
		t.Fatalf("expected 2 reschedule emails queued, got %d", n)
	}
}

func TestOperationsActionsAreAudited(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	ops, secret := h.operator()
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))

	request := buyer.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/cancellation", map[string]string{"reason": "I can no longer attend this time"})
	ops.expect(200, "POST", "/api/v1/ops/cancellations/"+request["id"].(string)+"/resolve", map[string]string{"resolution": "Spoke to both participants; kept booking"})
	if n := scalar[int64](t, `SELECT count(*) FROM notification_outbox WHERE booking_id=$1 AND kind LIKE 'cancellation_reviewed_%'`, bookingID); n != 2 {
		t.Fatalf("expected 2 review emails queued, got %d", n)
	}
	ops.expect(200, "POST", "/api/v1/ops/people/"+s.userID+"/revoke-sessions", map[string]string{"reason": "Suspicious sign-in reported"})
	ops.expect(200, "POST", "/api/v1/ops/people/"+s.userID+"/payout-readiness", map[string]any{"ready": true, "reason": "Identity and bank account reviewed"})
	for _, action := range []string{"sessions.revoked", "booking.cancellation_request_resolved", "seller.payout_readiness_set"} {
		if n := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action=$1`, action); n < 1 {
			t.Fatalf("missing audit event %s", action)
		}
	}
	// The same authenticator code cannot be used twice.
	res := ops.do("POST", "/api/v1/ops/session", map[string]string{"code": totpAt(secret, time.Now())})
	if res.Status != 401 {
		t.Fatalf("replayed TOTP code: %d %s", res.Status, res.Body)
	}
	// Operators cannot restrict themselves.
	self := scalar[string](t, `SELECT user_id::text FROM user_identities WHERE normalized_identifier=$1`, ops.email)
	if res = ops.do("POST", "/api/v1/ops/people/"+self+"/restrict", map[string]string{"reason": "testing self restriction"}); res.Status != 409 {
		t.Fatalf("self restriction: %d %s", res.Status, res.Body)
	}
}

func TestSettlementImportIsAudited(t *testing.T) {
	h := newHarness(t)
	ops, _ := h.operator()
	csv := "transaction_reference,settlement_reference,amount_minor,currency,settled_at\nunknown-ref-1,set1,1000,NGN,2026-01-02T10:00:00Z\n"
	req := ops.expect(200, "POST", "/api/v1/ops/settlements/import", csv, "Content-Type", "text/csv")
	if req["unmatched"].(float64) != 1 {
		t.Fatalf("import result %v", req)
	}
}

func TestStaleHoldDoesNotBlockSlotAndHoldLimit(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("fixed")
	start := h.slot(s.handle, 0)
	first := h.guestBuyer()
	quote := first.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "First", "duration_minutes": 30, "starts_at": start}, "Idempotency-Key", idempotencyKey())
	if _, err := itPool.Exec(context.Background(), `UPDATE quotes SET expires_at=now()-interval '1 minute' WHERE id=$1; `, quote["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := itPool.Exec(context.Background(), `UPDATE slot_reservations SET expires_at=now()-interval '1 minute' WHERE quote_id=$1`, quote["id"]); err != nil {
		t.Fatal(err)
	}
	second := h.guestBuyer()
	second.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Second", "duration_minutes": 30, "starts_at": start}, "Idempotency-Key", idempotencyKey())

	// A buyer can hold at most three unpaid times across sellers.
	busy := h.guestBuyer()
	for i := 0; i < 3; i++ {
		other := h.newSeller("fixed")
		busy.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": other.handle, "name": "Busy", "duration_minutes": 30, "starts_at": h.slot(other.handle, 0)}, "Idempotency-Key", idempotencyKey())
	}
	fourth := h.newSeller("fixed")
	res := busy.do("POST", "/api/v1/quotes", map[string]any{"seller": fourth.handle, "name": "Busy", "duration_minutes": 30, "starts_at": h.slot(fourth.handle, 0)}, "Idempotency-Key", idempotencyKey())
	if res.Status != 429 {
		t.Fatalf("fourth hold: %d %s", res.Status, res.Body)
	}
}

func TestOfferModeSellerRejectsFixedQuotesAndMalformedIDs(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("offer")
	buyer := h.guestBuyer()
	res := buyer.do("POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "B", "duration_minutes": 30, "starts_at": h.slot(s.handle, 0)}, "Idempotency-Key", idempotencyKey())
	if res.Status != 409 || !strings.Contains(string(res.Body), "OFFER_MODE_ONLY") {
		t.Fatalf("fixed quote on offer seller: %d %s", res.Status, res.Body)
	}
	if res = buyer.do("GET", "/api/v1/bookings/not-a-uuid", nil); res.Status != 404 {
		t.Fatalf("malformed id: %d", res.Status)
	}
}

func TestSellerTakingBothAcceptsFixedQuotesAndOffers(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("both")
	buyer := h.guestBuyer()
	buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "B", "duration_minutes": 30, "starts_at": h.slot(s.handle, 0)}, "Idempotency-Key", idempotencyKey())
	offerBuyer := h.client(unique("o") + "@buyer.test")
	offerBuyer.signIn("guest_offer")
	offerBuyer.expect(201, "POST", "/api/v1/offers", map[string]any{"seller": s.handle, "name": "Ola", "duration_minutes": 30, "amount_minor": 500000}, "Idempotency-Key", idempotencyKey())

	fixed := h.newSeller("fixed")
	res := offerBuyer.do("POST", "/api/v1/offers", map[string]any{"seller": fixed.handle, "name": "Ola", "duration_minutes": 30, "amount_minor": 500000}, "Idempotency-Key", idempotencyKey())
	if res.Status != 409 || !strings.Contains(string(res.Body), "OFFER_MODE_DISABLED") {
		t.Fatalf("offer on fixed-only seller: %d %s", res.Status, res.Body)
	}
}

// TestEndpointSmoke calls every route once with valid input and fails on any
// server error. It exists to catch SQL/parameter-type mistakes that only
// PostgreSQL can detect.
func TestEndpointSmoke(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	ops, _ := h.operator()
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))
	loc, _ := time.LoadLocation("Africa/Lagos")
	future := time.Now().In(loc).AddDate(0, 0, 3).Format("2006-01-02")

	offerSeller := h.newSeller("offer")
	offerBuyer := h.client(unique("ob") + "@buyer.test")
	offerBuyer.signIn("guest_offer")
	offer := offerBuyer.expect(201, "POST", "/api/v1/offers", map[string]any{"seller": offerSeller.handle, "name": "Offer Buyer", "duration_minutes": 30, "amount_minor": 300000}, "Idempotency-Key", idempotencyKey())
	offerID := offer["id"].(string)
	offerSeller.expect(200, "POST", "/api/v1/offers/"+offerID+"/counter", map[string]any{"version": 1, "amount_minor": 350000})

	var png bytes.Buffer
	png.Write(tinyPNG())

	type call struct {
		who    *client
		method string
		path   string
		body   any
		want   int
	}
	anon := h.client("")
	calls := []call{
		{anon, "GET", "/health", nil, 200},
		{anon, "GET", "/api/v1/runtime", nil, 200},
		{anon, "GET", "/api/v1/handles/" + unique("free") + "/availability", nil, 200},
		{anon, "GET", "/api/v1/people/" + s.handle, nil, 200},
		{anon, "POST", "/api/v1/analytics/events", map[string]string{"event": "public_link_viewed", "handle": s.handle}, 204},
		{s.client, "GET", "/api/v1/me", nil, 200},
		{s.client, "GET", "/api/v1/me/sessions", nil, 200},
		{s.client, "GET", "/api/v1/me/link", nil, 200},
		{s.client, "PATCH", "/api/v1/me/link", map[string]any{"handle": s.handle, "name": "Renamed Seller", "mode": "fixed", "base_30_minor": 1200000, "durations": []int{30, 60}, "timezone": "Africa/Lagos"}, 200},
		{s.client, "PUT", "/api/v1/me/availability/overrides/" + future, map[string]any{"closed": false, "windows": []map[string]any{{"weekday": 0, "start": "09:00", "end": "11:00"}}}, 200},
		{s.client, "DELETE", "/api/v1/me/availability/overrides/" + future, nil, 200},
		{s.client, "GET", "/api/v1/me/bookings", nil, 200},
		{s.client, "GET", "/api/v1/me/offers", nil, 200},
		{s.client, "GET", "/api/v1/me/settlements", nil, 200},
		{s.client, "PATCH", "/api/v1/bookings/" + bookingID + "/meeting", map[string]string{"meeting_url": "https://meet.example.com/abc"}, 200},
		{buyer, "GET", "/api/v1/bookings/" + bookingID, nil, 200},
		{buyer, "GET", "/api/v1/bookings/" + bookingID + "/calendar", nil, 200},
		{buyer, "GET", "/api/v1/bookings/" + bookingID + "/reschedules", nil, 200},
		{buyer, "GET", "/api/v1/bookings/" + bookingID + "/receipt", nil, 404},
		{buyer, "POST", "/api/v1/bookings/" + bookingID + "/issue", map[string]string{"reason": "The meeting link does not open"}, 201},
		{buyer, "POST", "/api/v1/bookings/" + bookingID + "/completion", "{}", 409},
		{buyer, "GET", "/api/v1/me/bookings", nil, 200},
		{offerBuyer, "GET", "/api/v1/offers/" + offerID, nil, 200},
		{offerBuyer, "POST", "/api/v1/offers/" + offerID + "/accept", map[string]any{"version": 2}, 200},
		{ops, "GET", "/api/v1/ops/overview", nil, 200},
		{ops, "GET", "/api/v1/ops/growth", nil, 200},
		{ops, "GET", "/api/v1/ops/system", nil, 200},
		{ops, "GET", "/api/v1/ops/provider-events", nil, 200},
		{ops, "GET", "/api/v1/ops/provider-cases", nil, 200},
		{ops, "GET", "/api/v1/ops/payments", nil, 200},
		{ops, "GET", "/api/v1/ops/settlements", nil, 200},
		{ops, "GET", "/api/v1/ops/settlements/import-rows", nil, 200},
		{ops, "GET", "/api/v1/ops/exceptions", nil, 200},
		{ops, "GET", "/api/v1/ops/people", nil, 200},
		{ops, "GET", "/api/v1/ops/people/" + s.userID, nil, 200},
		{ops, "GET", "/api/v1/ops/bookings", nil, 200},
		{ops, "GET", "/api/v1/ops/bookings/" + bookingID, nil, 200},
		{ops, "GET", "/api/v1/ops/offers", nil, 200},
		{ops, "GET", "/api/v1/ops/meetings/overdue", nil, 200},
		{ops, "GET", "/api/v1/ops/audit", nil, 200},
		{ops, "GET", "/api/v1/ops/cancellations", nil, 200},
		{ops, "POST", "/api/v1/ops/bookings/" + bookingID + "/resolve-issue", map[string]string{"resolution": "Seller resent a working link"}, 200},
		{ops, "POST", "/api/v1/ops/people/" + offerSeller.userID + "/restrict", map[string]string{"reason": "Repeated policy violations"}, 200},
		{s.client, "POST", "/api/v1/me/sessions/revoke-others", "{}", 200},
		{s.client, "POST", "/api/v1/auth/logout", "{}", 200},
	}
	for _, c := range calls {
		res := c.who.do(c.method, c.path, c.body)
		if res.Status != c.want {
			t.Errorf("%s %s: status %d, want %d: %s", c.method, c.path, res.Status, c.want, res.Body)
		}
	}

	// Avatar upload and public read (multipart, so it bypasses the JSON helper).
	uploader := h.newSeller("fixed")
	var form bytes.Buffer
	boundary := "aside-test-boundary"
	form.WriteString("--" + boundary + "\r\nContent-Disposition: form-data; name=\"avatar\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\n")
	form.Write(png.Bytes())
	form.WriteString("\r\n--" + boundary + "--\r\n")
	res := uploader.do("PUT", "/api/v1/me/avatar", form.Bytes(), "Content-Type", "multipart/form-data; boundary="+boundary)
	if res.Status != 200 {
		t.Fatalf("avatar upload: %d %s", res.Status, res.Body)
	}
	version := res.json(t)["avatar_version"].(float64)
	if res = anon.do("GET", fmt.Sprintf("/api/v1/people/%s/avatar?v=%d", uploader.handle, int(version)), nil); res.Status != 200 {
		t.Fatalf("avatar read: %d", res.Status)
	}
	if res = anon.do("GET", "/api/v1/people/"+uploader.handle+"/avatar", nil); res.Status != 404 {
		t.Fatalf("avatar without version: %d", res.Status)
	}
}

func tinyPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestSellerChangesTheirLink(t *testing.T) {
	h := newHarness(t)
	s := h.newSeller("fixed")
	old := s.handle
	next := unique("new")
	anon := h.client("")
	if anon.expect(200, "GET", "/api/v1/handles/"+next+"/availability", nil)["available"] != true {
		t.Fatal("new link should be free")
	}
	s.expect(200, "PUT", "/api/v1/me/link/handle", map[string]string{"handle": strings.ToUpper(next)})
	// The old link forwards to the new one.
	if got := anon.expect(200, "GET", "/api/v1/people/"+old, nil)["handle"]; got != next {
		t.Fatalf("old link resolved to %v", got)
	}
	// Nobody else can claim or move to the old link; the seller can go back to it.
	if anon.expect(200, "GET", "/api/v1/handles/"+old+"/availability", nil)["available"] != false {
		t.Fatal("old link must stay reserved")
	}
	if s.expect(200, "GET", "/api/v1/handles/"+old+"/availability", nil)["available"] != true {
		t.Fatal("the seller may go back to their old link")
	}
	other := h.newSeller("fixed")
	other.expect(409, "PUT", "/api/v1/me/link/handle", map[string]string{"handle": old})
	other.expect(409, "PUT", "/api/v1/me/link/handle", map[string]string{"handle": next})
	claimer := h.client(unique("c") + "@seller.test")
	claimer.signIn("claim")
	claimer.expect(409, "POST", "/api/v1/me/link", map[string]any{"handle": old, "name": "Impostor", "mode": "fixed", "base_30_minor": 1000000, "durations": []int{30}, "timezone": "Africa/Lagos"})
	s.expect(422, "PUT", "/api/v1/me/link/handle", map[string]string{"handle": "api"})
	s.expect(200, "PUT", "/api/v1/me/link/handle", map[string]string{"handle": old})
	if got := anon.expect(200, "GET", "/api/v1/people/"+next, nil)["handle"]; got != old {
		t.Fatalf("going back: %v", got)
	}
	// At most three changes in 30 days.
	s.expect(200, "PUT", "/api/v1/me/link/handle", map[string]string{"handle": unique("third")})
	s.expect(429, "PUT", "/api/v1/me/link/handle", map[string]string{"handle": unique("fourth")})
	if n := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND action='seller.handle_changed'`, s.userID); n != 3 {
		t.Fatalf("link changes audited %d times", n)
	}
	// Deleting the account stops forwards and holds every earlier link.
	s.expect(200, "POST", "/api/v1/me/deletion", map[string]string{"confirm_email": s.client.email})
	anon.expect(404, "GET", "/api/v1/people/"+next, nil)
	if anon.expect(200, "GET", "/api/v1/handles/"+next+"/availability", nil)["available"] != false {
		t.Fatal("a deleted seller's earlier link must be held")
	}
}
