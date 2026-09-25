//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// signInWithZone verifies an email the way the web app does, sending the
// browser's time zone along with the code.
func (c *client) signInWithZone(purpose, zone string) {
	c.h.t.Helper()
	challenge := c.expect(202, "POST", "/api/v1/auth/challenges", map[string]string{"email": c.email, "purpose": purpose})
	id := challenge["challenge_id"].(string)
	c.expect(200, "POST", "/api/v1/auth/challenges/"+id+"/verify", map[string]string{"code": itMail.code(c.h.t, c.email), "timezone": zone})
}

// deliverDue runs the notification worker once and returns the messages it
// sent to one address.
func (h *harness) deliverDue(to string) []emailMessage {
	h.t.Helper()
	itMail.mu.Lock()
	start := len(itMail.messages)
	itMail.mu.Unlock()
	for i := 0; i < 5; i++ { // a few batches in case other tests left queued mail
		if err := h.api.processNotificationBatch(context.Background()); err != nil {
			h.t.Fatal(err)
		}
	}
	itMail.mu.Lock()
	defer itMail.mu.Unlock()
	var out []emailMessage
	for _, m := range itMail.messages[start:] {
		if to == "" || m.To == to {
			out = append(out, m)
		}
	}
	return out
}

func findMessage(t *testing.T, msgs []emailMessage, subjectPart string) emailMessage {
	t.Helper()
	for _, m := range msgs {
		if strings.Contains(m.Subject, subjectPart) {
			return m
		}
	}
	subjects := []string{}
	for _, m := range msgs {
		subjects = append(subjects, m.Subject)
	}
	t.Fatalf("no email with subject containing %q; got %v", subjectPart, subjects)
	return emailMessage{}
}

func TestSlotsInViewerTimeZoneSpanSellerDays(t *testing.T) {
	h := newHarness(t)
	s := h.newSeller("fixed") // Africa/Lagos, 08:00-20:00 every day
	viewer, _ := time.LoadLocation("Pacific/Auckland")
	lagos, _ := time.LoadLocation("Africa/Lagos")
	day := time.Now().In(viewer).AddDate(0, 0, 3)
	date := day.Format("2006-01-02")
	data := h.client("").expect(200, "GET", "/api/v1/people/"+s.handle+"/slots?date="+date+"&duration=30&tz=Pacific/Auckland", nil)
	if data["viewer_timezone"] != "Pacific/Auckland" || data["timezone"] != "Africa/Lagos" {
		t.Fatalf("zones: %v", data)
	}
	slots := data["slots"].([]any)
	if len(slots) == 0 {
		t.Fatal("expected slots")
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, viewer)
	to := from.AddDate(0, 0, 1)
	sellerDates := map[string]bool{}
	for _, raw := range slots {
		slot := raw.(map[string]any)
		at, err := time.Parse(time.RFC3339, slot["starts_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if at.Before(from) || !at.Before(to) {
			t.Fatalf("slot %s is outside the viewer's day %s", at, date)
		}
		if want := at.In(viewer).Format("Mon, Jan 2 · 3:04 PM MST"); slot["local_label"] != want {
			t.Fatalf("local_label %v, want %s", slot["local_label"], want)
		}
		if slot["seller_label"] != at.In(lagos).Format("Mon, Jan 2 · 3:04 PM MST") {
			t.Fatalf("seller_label %v", slot["seller_label"])
		}
		sellerDates[at.In(lagos).Format("2006-01-02")] = true
	}
	if len(sellerDates) != 2 {
		t.Fatalf("an Auckland day should draw on two Lagos days, got %v", sellerDates)
	}
	// Without tz the date is the seller's, as before.
	plain := h.client("").expect(200, "GET", "/api/v1/people/"+s.handle+"/slots?date="+date+"&duration=30", nil)
	for _, raw := range plain["slots"].([]any) {
		at, _ := time.Parse(time.RFC3339, raw.(map[string]any)["starts_at"].(string))
		if at.In(lagos).Format("2006-01-02") != date {
			t.Fatalf("seller-zone query returned %s", at)
		}
	}
	h.client("").expect(422, "GET", "/api/v1/people/"+s.handle+"/slots?date="+date+"&duration=30&tz=Mars/Base", nil)
}

func TestBookingEmailsUseEachPersonsTimeZone(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("fixed")
	buyer := h.client(unique("b") + "@buyer.test")
	buyer.signInWithZone("guest_booking", "America/New_York")
	if got := scalar[string](t, `SELECT u.timezone FROM users u JOIN user_identities i ON i.user_id=u.id WHERE i.normalized_identifier=$1`, buyer.email); got != "America/New_York" {
		t.Fatalf("buyer timezone = %s", got)
	}
	start := h.slot(s.handle, 0)
	_, bookingID := h.holdAndSimulate(buyer, s.handle, start)
	at, _ := time.Parse(time.RFC3339, start)
	ny, _ := time.LoadLocation("America/New_York")
	lagos, _ := time.LoadLocation("Africa/Lagos")

	all := h.deliverDue("")
	to := func(address string) []emailMessage {
		var out []emailMessage
		for _, m := range all {
			if m.To == address {
				out = append(out, m)
			}
		}
		return out
	}
	confirm := findMessage(t, to(buyer.email), "Confirmed: 30 minutes with Seller "+s.handle)
	if !strings.Contains(confirm.Text, formatWhen(at, ny)) {
		t.Fatalf("buyer email should use New York time %q:\n%s", formatWhen(at, ny), confirm.Text)
	}
	if !strings.Contains(confirm.Text, "Seller "+s.handle+"’s time: "+formatClock(at, lagos)) {
		t.Fatalf("buyer email should show the seller's Lagos time:\n%s", confirm.Text)
	}
	if !strings.Contains(confirm.HTML, "<h1") || strings.Contains(confirm.HTML, "<script") {
		t.Fatal("buyer email should have an HTML body")
	}
	if len(confirm.Attachments) != 1 || !strings.HasPrefix(confirm.Attachments[0].ContentType, "text/calendar") {
		t.Fatalf("expected a calendar attachment, got %+v", confirm.Attachments)
	}
	ics := string(confirm.Attachments[0].Content)
	for _, want := range []string{"BEGIN:VCALENDAR", "UID:" + bookingID + "@wantmytime.com", "DTSTART:" + at.UTC().Format("20060102T150405Z"), "METHOD:PUBLISH"} {
		if !strings.Contains(ics, want) {
			t.Fatalf("calendar missing %q:\n%s", want, ics)
		}
	}
	for _, line := range strings.Split(ics, "\r\n") {
		if len(line) > 75 {
			t.Fatalf("calendar line longer than 75 octets: %q", line)
		}
	}
	if confirm.IdempotencyKey == "" {
		t.Fatal("notification emails need an idempotency key")
	}

	seller := findMessage(t, to(s.client.email), "New booking: Buyer")
	if !strings.Contains(seller.Text, formatWhen(at, lagos)) || !strings.Contains(seller.Text, "Buyer’s time: "+formatClock(at, ny)) {
		t.Fatalf("seller email should lead with Lagos time and show the buyer's New York time:\n%s", seller.Text)
	}
}

func TestRescheduleAndCancellationRequestsEmailTheOtherPerson(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))
	h.deliverDue(s.client.email) // confirmations

	proposed := h.slot(s.handle, 4)
	request := buyer.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/reschedules", map[string]string{"proposed_starts_at": proposed})
	asked := findMessage(t, h.deliverDue(s.client.email), "asked to move your booking")
	at, _ := time.Parse(time.RFC3339, proposed)
	lagos, _ := time.LoadLocation("Africa/Lagos")
	if !strings.Contains(asked.Text, "Proposed time: "+formatWhen(at, lagos)) || !strings.Contains(asked.Text, "Respond by:") {
		t.Fatalf("reschedule email lacks the proposal:\n%s", asked.Text)
	}
	s.client.expect(200, "POST", "/api/v1/reschedules/"+request["id"].(string)+"/decline", "{}")
	findMessage(t, h.deliverDue(buyer.email), "kept the original time")

	buyer.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/cancellation", map[string]string{"reason": "I can no longer make this time"})
	cancel := findMessage(t, h.deliverDue(s.client.email), "asked to cancel your booking")
	if strings.Contains(cancel.Text, "no longer make") {
		t.Fatal("the private cancellation reason must not be emailed to the other person")
	}
}

func TestOfferEventsAreEmailed(t *testing.T) {
	h := newHarness(t)
	s := h.newSeller("offer")
	buyer := h.client(unique("b") + "@buyer.test")
	buyer.signIn("guest_offer")
	offer := buyer.expect(201, "POST", "/api/v1/offers", map[string]any{"seller": s.handle, "name": "Tunde", "duration_minutes": 30, "amount_minor": 500000}, "Idempotency-Key", idempotencyKey())
	id := offer["id"].(string)
	received := findMessage(t, h.deliverDue(s.client.email), "New offer from Tunde: ₦5,000 for 30 minutes")
	if !strings.Contains(received.Text, "/app/offers/"+id) {
		t.Fatalf("seller offer email should link to the offer:\n%s", received.Text)
	}
	s.client.expect(200, "POST", "/api/v1/offers/"+id+"/counter", map[string]any{"version": 1, "amount_minor": 750000})
	findMessage(t, h.deliverDue(buyer.email), "countered your offer: ₦7,500")
	buyer.expect(200, "POST", "/api/v1/offers/"+id+"/accept", map[string]any{"version": 2})
	findMessage(t, h.deliverDue(s.client.email), "Tunde accepted your counteroffer")
	if n := scalar[int64](t, `SELECT count(*) FROM notification_outbox WHERE offer_id=$1 AND state='sent'`, id); n != 3 {
		t.Fatalf("expected 3 sent offer emails, got %d", n)
	}
}

func TestStaleNotificationsAreCancelledNotSent(t *testing.T) {
	h := newHarness(t)
	s := h.newSeller("offer")
	buyer := h.client(unique("b") + "@buyer.test")
	buyer.signIn("guest_offer")
	offer := buyer.expect(201, "POST", "/api/v1/offers", map[string]any{"seller": s.handle, "name": "Kemi", "duration_minutes": 30, "amount_minor": 400000}, "Idempotency-Key", idempotencyKey())
	id := offer["id"].(string)
	// The buyer withdraws before the worker runs: the seller gets the
	// withdrawal, not a "new offer" email for a closed offer.
	buyer.expect(200, "POST", "/api/v1/offers/"+id+"/withdraw", map[string]any{"version": 1})
	msgs := h.deliverDue(s.client.email)
	findMessage(t, msgs, "Kemi withdrew their offer")
	for _, m := range msgs {
		if strings.Contains(m.Subject, "New offer") {
			t.Fatalf("stale offer email was sent: %s", m.Subject)
		}
	}
	if got := scalar[string](t, `SELECT state FROM notification_outbox WHERE offer_id=$1 AND kind='offer_received_seller'`, id); got != "cancelled" {
		t.Fatalf("stale notification state = %s", got)
	}
}

func TestEmailRenderingEscapesAndFormats(t *testing.T) {
	c := emailContent{Subject: "Hi <b>", Heading: `Tom & "Jerry" <script>`, Paragraphs: []string{"a < b"}, Facts: []emailFact{{"When", "now"}}, Action: &emailLink{"Open", `https://x.test/?a=1&b="2"`}}
	text, html := renderEmail(c)
	if strings.Contains(html, "<script>") || !strings.Contains(html, "Tom &amp; &#34;Jerry&#34; &lt;script&gt;") || !strings.Contains(html, `href="https://x.test/?a=1&amp;b=&#34;2&#34;"`) {
		t.Fatalf("HTML not escaped:\n%s", html)
	}
	if !strings.Contains(text, "When: now") || !strings.Contains(text, "Open: https://x.test/?a=1&b=\"2\"") {
		t.Fatalf("text body:\n%s", text)
	}
	for minor, want := range map[int64]string{0: "₦0", 100: "₦1", 1050: "₦10.50", 1000000: "₦10,000", 123456789: "₦1,234,567.89"} {
		if got := formatMoney("NGN", minor); got != want {
			t.Errorf("formatMoney(%d) = %s, want %s", minor, got, want)
		}
	}
	if got := formatMoney("USD", 250000); got != "$2,500" {
		t.Errorf("USD formatting: %s", got)
	}
	// A currency without a known symbol is written with its ISO code.
	if got := formatMoney("XOF", 250000); got != "XOF 2,500" {
		t.Errorf("fallback formatting: %s", got)
	}
	raw, err := buildMIME("WantMyTime <no-reply@aside.test>", "", c.message("to@x.test", ""))
	if err != nil || !strings.Contains(string(raw), "multipart/alternative") || !strings.Contains(string(raw), "Subject: Hi <b>") {
		t.Fatalf("MIME: %v\n%s", err, raw)
	}
	if headerText("Ada\r\nBcc: evil@x.test") != "Ada Bcc: evil@x.test" {
		t.Fatal("header injection not neutralised")
	}
	ev := calendarEvent{UID: "u", Start: time.Unix(0, 0), End: time.Unix(1800, 0), Summary: strings.Repeat("é", 60), Description: "x"}
	for _, line := range strings.Split(string(ev.ICS()), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("unfolded line: %q", line)
		}
	}
}
