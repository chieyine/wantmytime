//go:build integration

package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAnnouncementEmailsReachOnlyConfirmedSubscribers(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	t.Setenv("SMTP_HOST", "127.0.0.1") // email counts as configured; the harness captures every message
	ops, _ := h.operator()

	// A seller who said yes with a verified address is on the list at once.
	s := h.newSeller("fixed")
	s.expect(200, "PUT", "/api/v1/me/marketing", map[string]any{"subscribed": true, "source": "seller_signup"})
	if got := s.expect(200, "GET", "/api/v1/me/marketing", nil); got["subscribed"] != true || got["confirmed"] != true {
		t.Fatalf("seller consent %v", got)
	}

	// A buyer who ticked the box but hasn't paid waits for confirmation.
	// (bookings/start opens a booking session without a code, as the booking page does)
	buyer := h.client(unique("b") + "@buyer.test")
	buyer.expect(200, "POST", "/api/v1/bookings/start", map[string]string{"email": buyer.email})
	buyer.expect(200, "PUT", "/api/v1/me/marketing", map[string]any{"subscribed": true, "source": "booking"})
	if got := buyer.expect(200, "GET", "/api/v1/me/marketing", nil); got["confirmed"] != false {
		t.Fatalf("unpaid buyer should wait for confirmation: %v", got)
	}
	summary := ops.expect(200, "GET", "/api/v1/ops/marketing", nil)
	if summary["audience"] != float64(1) || summary["pending_confirmation"] != float64(1) {
		t.Fatalf("before payment %v", summary)
	}
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "B", "duration_minutes": 30, "starts_at": h.slot(s.handle, 0)}, "Idempotency-Key", idempotencyKey())
	buyer.expect(201, "POST", "/api/v1/dev/quotes/"+quote["id"].(string)+"/simulate-payment", "{}")
	if got := ops.expect(200, "GET", "/api/v1/ops/marketing", nil)["audience"]; got != float64(2) {
		t.Fatalf("paid buyer should be confirmed, audience %v", got)
	}

	// Someone who said no is never emailed.
	no := h.newSeller("fixed")
	no.expect(200, "PUT", "/api/v1/me/marketing", map[string]any{"subscribed": false, "source": "seller_signup"})

	// Export: confirmed subscribers only, each with an unsubscribe link.
	res := ops.do("GET", "/api/v1/ops/marketing/contacts.csv", nil)
	csvText := string(res.Body)
	if res.Status != 200 || !strings.Contains(csvText, s.client.email) || !strings.Contains(csvText, buyer.email) || strings.Contains(csvText, no.client.email) || !strings.Contains(csvText, "/unsubscribe?token=") {
		t.Fatalf("export %d:\n%s", res.Status, csvText)
	}

	// Sending needs the operator to confirm the number of people.
	id := ops.expect(201, "POST", "/api/v1/ops/broadcasts", map[string]any{"subject": "Kredit is here", "heading": "Meet kredit.ng", "body": "First paragraph.\n\nSecond paragraph.", "action_label": "Visit kredit.ng", "action_url": "https://kredit.ng"})["id"].(string)
	ops.expect(409, "POST", "/api/v1/ops/broadcasts/"+id+"/send", map[string]any{"confirm_audience": 5})
	ops.expect(200, "POST", "/api/v1/ops/broadcasts/"+id+"/send", map[string]any{"confirm_audience": 2})
	ops.expect(409, "POST", "/api/v1/ops/broadcasts/"+id+"/send", map[string]any{"confirm_audience": 2})

	itMail.mu.Lock()
	start := len(itMail.messages)
	itMail.mu.Unlock()
	if err := h.api.processBroadcasts(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if err := h.api.processBroadcasts(context.Background(), 100); err != nil { // closes the finished announcement
		t.Fatal(err)
	}
	itMail.mu.Lock()
	var sent []emailMessage
	for _, m := range itMail.messages[start:] {
		if m.Subject == "Kredit is here" {
			sent = append(sent, m)
		}
	}
	itMail.mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent %d announcements", len(sent))
	}
	msg := sent[0]
	if msg.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" || !strings.Contains(msg.Headers["List-Unsubscribe"], "/api/v1/marketing/one-click?token=") || !strings.Contains(msg.Text, "Unsubscribe: ") || !strings.Contains(msg.HTML, "https://kredit.ng") {
		t.Fatalf("announcement headers %v\n%s", msg.Headers, msg.Text)
	}
	if state := scalar[string](t, `SELECT state FROM broadcasts WHERE id=$1`, id); state != "sent" {
		t.Fatalf("broadcast state %s", state)
	}

	// Gmail-style one-click unsubscribe: a POST with no Origin, only the token.
	oneClick := strings.Trim(msg.Headers["List-Unsubscribe"], "<>")
	parsed, _ := url.Parse(oneClick)
	token := parsed.Query().Get("token")
	resp, err := http.Post(h.server.URL+"/api/v1/marketing/one-click?token="+url.QueryEscape(token), "application/x-www-form-urlencoded", strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("one-click unsubscribe %d %s", resp.StatusCode, body)
	}
	anon := h.client("")
	if got := anon.expect(200, "GET", "/api/v1/marketing/unsubscribe?token="+url.QueryEscape(token), nil); got["subscribed"] != false || !strings.Contains(got["email"].(string), "•") {
		t.Fatalf("after one-click %v", got)
	}
	anon.expect(200, "POST", "/api/v1/marketing/resubscribe", map[string]string{"token": token})
	anon.expect(200, "POST", "/api/v1/marketing/unsubscribe", map[string]string{"token": token})
	anon.expect(404, "POST", "/api/v1/marketing/unsubscribe", map[string]string{"token": "not-a-token"})
	if n := scalar[int64](t, `SELECT count(*) FROM marketing_consent_events WHERE email=$1`, msg.To); n < 4 {
		t.Fatalf("consent history has %d entries", n)
	}

	// Unsubscribes from a mail tool come back in.
	got := ops.expect(200, "POST", "/api/v1/ops/marketing/unsubscribes", map[string]any{"emails": []string{s.client.email, "nobody@nowhere.test"}})
	if got["unsubscribed"] != float64(1) || got["not_found"] != float64(1) {
		t.Fatalf("import %v", got)
	}
	if got := ops.expect(200, "GET", "/api/v1/ops/marketing", nil)["audience"]; got != float64(0) {
		t.Fatalf("audience after unsubscribes %v", got)
	}

	// Deleting an account removes the address and its history.
	no.expect(200, "POST", "/api/v1/me/deletion", map[string]string{"confirm_email": no.client.email})
	if n := scalar[int64](t, `SELECT count(*) FROM marketing_consent_events WHERE email=$1`, no.client.email); n != 0 {
		t.Fatalf("deleted person's consent history remains: %d", n)
	}
}
