package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/textproto"
	"os"
	"strings"
	"time"
	"unicode"
)

// emailMessage is one outgoing email. Text is always present; HTML is optional.
type emailMessage struct {
	To          string
	Subject     string
	Text        string
	HTML        string
	Attachments []emailAttachment
	// IdempotencyKey lets the provider drop a repeat of the same logical email
	// (for example when a worker retries after a timeout that actually sent).
	IdempotencyKey string
	// Headers are extra message headers, such as List-Unsubscribe on
	// announcements. Names and values must be single-line.
	Headers map[string]string
	// From overrides EMAIL_FROM (announcements can use their own sender).
	From string
}

type emailAttachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

// emailFact is one labelled line in the summary box ("When", "Length").
type emailFact struct{ Label, Value string }

type emailLink struct{ Label, URL string }

// emailContent is the structured body of a transactional email. It renders to
// both plain text and a simple, accessible HTML layout from the same data.
type emailContent struct {
	Subject    string
	Preheader  string
	Heading    string
	Paragraphs []string
	Facts      []emailFact
	Action     *emailLink
	Notes      []string
	Calendar   *calendarEvent
	// Code is a one-time code, shown large and easy to copy.
	Code string
	// Links are secondary links under the main button ("Download your receipt").
	Links []emailLink
	// Image is an optional picture across the top of the card (announcements).
	Image *emailImage
	// Footer replaces the line saying why the email was sent; Unsubscribe adds a link.
	Footer      string
	Unsubscribe *emailLink
}

type emailImage struct{ URL, Alt string }

// companyLine identifies the sender in every email, as the privacy notice does.
const companyLine = "WantMyTime · Kredit Technologies Limited · House No. 348, Jamaina Road, Pompomari Bypass, Maiduguri, Borno State, Nigeria"

func emailReplyTo() string {
	if replyTo := strings.TrimSpace(os.Getenv("EMAIL_REPLY_TO")); replyTo != "" && validEmail(replyTo) {
		return replyTo
	}
	return ""
}

func (c emailContent) message(to, idempotencyKey string) emailMessage {
	text, htmlBody := renderEmail(c)
	msg := emailMessage{To: to, Subject: c.Subject, Text: text, HTML: htmlBody, IdempotencyKey: idempotencyKey}
	if c.Calendar != nil {
		method := c.Calendar.Method
		if method == "" {
			method = "PUBLISH"
		}
		msg.Attachments = append(msg.Attachments, emailAttachment{Filename: "wantmytime-booking.ics", ContentType: "text/calendar; charset=utf-8; method=" + method, Content: c.Calendar.ICS()})
	}
	return msg
}

// emailFooter says why the email was sent and where to get help.
func emailFooter() string {
	return "You’re getting this because of your account or a booking on WantMyTime."
}

func emailHelpText() string {
	if emailReplyTo() != "" {
		return "Questions? Just reply to this email."
	}
	return "Questions? See " + appOrigin() + "/help"
}

func renderEmail(c emailContent) (string, string) {
	footer := emailFooter()
	if c.Footer != "" {
		footer = c.Footer
	}

	var t strings.Builder
	t.WriteString(c.Heading + "\n\n")
	for _, p := range c.Paragraphs {
		t.WriteString(p + "\n\n")
	}
	if c.Code != "" {
		t.WriteString("Your code is " + c.Code + "\n\n")
	}
	if len(c.Facts) > 0 {
		for _, f := range c.Facts {
			t.WriteString(f.Label + ": " + f.Value + "\n")
		}
		t.WriteString("\n")
	}
	if c.Action != nil {
		t.WriteString(c.Action.Label + ": " + c.Action.URL + "\n\n")
	}
	for _, l := range c.Links {
		t.WriteString(l.Label + ": " + l.URL + "\n")
	}
	if len(c.Links) > 0 {
		t.WriteString("\n")
	}
	for _, n := range c.Notes {
		t.WriteString(n + "\n\n")
	}
	t.WriteString("--\n" + emailHelpText() + "\n" + footer + "\n")
	if c.Unsubscribe != nil {
		t.WriteString(c.Unsubscribe.Label + ": " + c.Unsubscribe.URL + "\n")
	}
	t.WriteString(companyLine + "\n")

	e := html.EscapeString
	const ink, muted, rule, paper = "#171817", "#5b5f58", "#d8d4c8", "#f1efe8"
	font := "-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"
	var h strings.Builder
	h.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><meta name="supported-color-schemes" content="light"><title>`)
	h.WriteString(e(c.Subject))
	h.WriteString(`</title></head><body style="margin:0;padding:0;background:` + paper + `;color:` + ink + `;font-family:` + font + `;-webkit-text-size-adjust:100%;">`)
	if c.Preheader != "" {
		// Hidden preview text, padded so the inbox preview doesn't pull in body text.
		h.WriteString(`<div style="display:none;max-height:0;overflow:hidden;opacity:0;">` + e(c.Preheader) + strings.Repeat("&#8199;&#65279;&#847; ", 30) + `</div>`)
	}
	h.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:` + paper + `;"><tr><td align="center" style="padding:28px 12px 36px;">`)
	h.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;">`)
	// The wordmark, as on the site: WantMyTime with a red full stop.
	h.WriteString(`<tr><td style="padding:0 4px 18px;"><a href="` + e(appOrigin()) + `" style="color:` + ink + `;text-decoration:none;font-size:22px;font-weight:800;letter-spacing:-.03em;">WantMyTime<span style="color:#b43a30;">.</span></a></td></tr>`)
	h.WriteString(`<tr><td style="background:#ffffff;border:1px solid ` + ink + `;">`)
	if c.Image != nil {
		h.WriteString(`<img src="` + e(c.Image.URL) + `" alt="` + e(c.Image.Alt) + `" width="558" style="display:block;width:100%;max-width:558px;height:auto;border:0;border-bottom:1px solid ` + ink + `;">`)
	}
	h.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr><td style="padding:32px 28px 28px;">`)
	h.WriteString(`<h1 style="margin:0 0 16px;font-size:26px;line-height:1.2;font-weight:800;letter-spacing:-.025em;color:` + ink + `;">` + e(c.Heading) + `</h1>`)
	for _, p := range c.Paragraphs {
		h.WriteString(`<p style="margin:0 0 14px;font-size:16px;line-height:1.6;color:` + ink + `;">` + e(p) + `</p>`)
	}
	if c.Code != "" {
		h.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:10px 0 22px;"><tr><td align="center" style="background:` + paper + `;border:1px solid ` + rule + `;padding:20px 12px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:34px;line-height:1;font-weight:700;letter-spacing:8px;color:` + ink + `;">` + e(c.Code) + `</td></tr></table>`)
	}
	if len(c.Facts) > 0 {
		h.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:8px 0 24px;border-top:1px solid ` + rule + `;">`)
		for _, f := range c.Facts {
			h.WriteString(`<tr><td style="padding:11px 14px 11px 0;border-bottom:1px solid ` + rule + `;font-size:13px;color:` + muted + `;white-space:nowrap;vertical-align:top;width:1%;">` + e(f.Label) + `</td><td style="padding:11px 0;border-bottom:1px solid ` + rule + `;font-size:15px;font-weight:600;line-height:1.45;vertical-align:top;color:` + ink + `;">` + e(f.Value) + `</td></tr>`)
		}
		h.WriteString(`</table>`)
	}
	if c.Action != nil {
		h.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" style="margin:4px 0 22px;"><tr><td style="background:` + ink + `;"><a href="` + e(c.Action.URL) + `" style="display:inline-block;padding:14px 24px;color:#ffffff;text-decoration:none;font-size:15px;font-weight:700;">` + e(c.Action.Label) + ` &rarr;</a></td></tr></table>`)
	}
	for _, l := range c.Links {
		h.WriteString(`<p style="margin:0 0 12px;font-size:15px;line-height:1.5;"><a href="` + e(l.URL) + `" style="color:` + ink + `;font-weight:600;text-decoration:underline;">` + e(l.Label) + `</a></p>`)
	}
	for i, n := range c.Notes {
		top := "0"
		if i == 0 && (len(c.Links) > 0 || c.Action != nil) {
			top = "18px"
		}
		h.WriteString(`<p style="margin:` + top + ` 0 12px;font-size:14px;line-height:1.6;color:#3d403b;">` + e(n) + `</p>`)
	}
	h.WriteString(`</td></tr></table></td></tr>`)
	h.WriteString(`<tr><td style="padding:18px 4px 0;font-size:12px;line-height:1.6;color:` + muted + `;">`)
	if emailReplyTo() != "" {
		h.WriteString(`<p style="margin:0 0 6px;">Questions? Just reply to this email.</p>`)
	} else {
		h.WriteString(`<p style="margin:0 0 6px;">Questions? <a href="` + e(appOrigin()+"/help") + `" style="color:` + muted + `;">See our help page</a>.</p>`)
	}
	h.WriteString(`<p style="margin:0 0 6px;">` + e(footer))
	if c.Unsubscribe != nil {
		h.WriteString(` <a href="` + e(c.Unsubscribe.URL) + `" style="color:` + muted + `;">` + e(c.Unsubscribe.Label) + `</a>`)
	}
	h.WriteString(`</p><p style="margin:0;">` + e(companyLine) + `</p></td></tr>`)
	h.WriteString(`</table></td></tr></table></body></html>`)
	return t.String(), h.String()
}

// calendarEvent renders an RFC 5545 event. Meeting links are never included:
// they stay behind the participant-only booking page.
type calendarEvent struct {
	UID         string
	Sequence    int
	Start, End  time.Time
	Summary     string
	Description string
	URL         string
	Method      string // PUBLISH (default) or CANCEL
}

func (ev calendarEvent) ICS() []byte {
	escape := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\r\n", "\\n", "\r", "\\n", "\n", "\\n").Replace
	method := ev.Method
	if method == "" {
		method = "PUBLISH"
	}
	status := "CONFIRMED"
	if method == "CANCEL" {
		status = "CANCELLED"
	}
	stamp := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//WantMyTime//Booking//EN", "CALSCALE:GREGORIAN", "METHOD:" + method,
		"BEGIN:VEVENT",
		"UID:" + ev.UID + "@wantmytime.com",
		fmt.Sprintf("SEQUENCE:%d", ev.Sequence),
		"DTSTAMP:" + stamp(time.Now()),
		"DTSTART:" + stamp(ev.Start),
		"DTEND:" + stamp(ev.End),
		"SUMMARY:" + escape(ev.Summary),
		"STATUS:" + status,
	}
	if ev.Description != "" {
		lines = append(lines, "DESCRIPTION:"+escape(ev.Description))
	}
	if ev.URL != "" {
		lines = append(lines, "URL:"+ev.URL)
	}
	if method != "CANCEL" {
		lines = append(lines, "BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:"+escape(ev.Summary), "TRIGGER:-PT15M", "END:VALARM")
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(foldICSLine(line))
	}
	return []byte(b.String())
}

// foldICSLine splits content lines longer than 75 octets (RFC 5545 §3.1)
// without breaking a UTF-8 sequence.
func foldICSLine(line string) string {
	var b strings.Builder
	limit := 75
	for len(line) > limit {
		cut := limit
		for cut > 0 && !isRuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut] + "\r\n ")
		line = line[cut:]
		limit = 74 // the leading space counts toward the next line
	}
	b.WriteString(line + "\r\n")
	return b.String()
}

func isRuneStart(c byte) bool { return c&0xC0 != 0x80 }

// headerText removes control characters so user-supplied names cannot break
// out of an email header.
func headerText(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsControl), " ")
}

// deliverEmail sends one message through the configured provider.
func (a *API) deliverEmail(ctx context.Context, msg emailMessage) bool {
	msg.Subject = headerText(msg.Subject)
	if !validEmail(msg.To) || msg.Subject == "" || msg.Text == "" {
		return false
	}
	if a.mailCapture != nil {
		a.mailCapture(msg)
	}
	if a.mailer != nil {
		return a.mailer(msg.To, msg.Subject, msg.Text)
	}
	switch os.Getenv("EMAIL_PROVIDER") {
	case "resend":
		return a.sendResend(ctx, msg)
	case "sendly":
		return a.sendSendly(ctx, msg)
	}
	if host := os.Getenv("SMTP_HOST"); host != "" && a.env != "production" {
		from := envOr("EMAIL_FROM", "WantMyTime <local@wantmytime.com>")
		if msg.From != "" {
			from = msg.From
		}
		raw, err := buildMIME(from, os.Getenv("EMAIL_REPLY_TO"), msg)
		if err != nil {
			return false
		}
		if err = sendSMTP(host+":"+envOr("SMTP_PORT", "1025"), raw); err != nil {
			a.log().WarnContext(ctx, "email delivery failed", "code", "SMTP_ERROR")
			return false
		}
		return true
	}
	return false
}

func (a *API) sendResend(ctx context.Context, msg emailMessage) bool {
	key, from := os.Getenv("EMAIL_API_KEY"), os.Getenv("EMAIL_FROM")
	if msg.From != "" {
		from = msg.From
	}
	if key == "" || from == "" || strings.ContainsAny(from, "\r\n") {
		return false
	}
	payload := map[string]any{"from": from, "to": []string{msg.To}, "subject": msg.Subject, "text": msg.Text}
	if msg.HTML != "" {
		payload["html"] = msg.HTML
	}
	if replyTo := strings.TrimSpace(os.Getenv("EMAIL_REPLY_TO")); replyTo != "" && validEmail(replyTo) {
		payload["reply_to"] = replyTo
	}
	if len(msg.Headers) > 0 {
		payload["headers"] = msg.Headers
	}
	if len(msg.Attachments) > 0 {
		items := []map[string]string{}
		for _, att := range msg.Attachments {
			items = append(items, map[string]string{"filename": att.Filename, "content": base64.StdEncoding.EncodeToString(att.Content), "content_type": att.ContentType})
		}
		payload["attachments"] = items
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, envOr("RESEND_API_BASE", "https://api.resend.com")+"/emails", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	if msg.IdempotencyKey != "" && len(msg.IdempotencyKey) <= 256 {
		req.Header.Set("Idempotency-Key", msg.IdempotencyKey)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		a.log().WarnContext(ctx, "email delivery failed", "code", "PROVIDER_UNAVAILABLE")
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		a.log().WarnContext(ctx, "email delivery failed", "code", "PROVIDER_REJECTED", "status", resp.StatusCode)
		return false
	}
	return true
}

// buildMIME assembles a multipart message: text and HTML alternatives, plus
// any attachments.
func buildMIME(from, replyTo string, msg emailMessage) ([]byte, error) {
	if strings.ContainsAny(from, "\r\n") || strings.ContainsAny(replyTo, "\r\n") {
		return nil, fmt.Errorf("invalid header")
	}
	var out bytes.Buffer
	mixed := multipart.NewWriter(&out)
	idBytes := make([]byte, 12)
	_, _ = rand.Read(idBytes)
	domain := "aside.local"
	if at := strings.LastIndex(from, "@"); at >= 0 {
		domain = strings.Trim(from[at+1:], "> ")
	}
	headers := []string{
		"From: " + from,
		"To: " + msg.To,
		"Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Message-ID: <" + hex.EncodeToString(idBytes) + "@" + domain + ">",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=" + mixed.Boundary(),
	}
	if replyTo != "" {
		headers = append(headers, "Reply-To: "+replyTo)
	}
	for name, value := range msg.Headers {
		if name == "" || strings.ContainsAny(name, "\r\n: ") || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("invalid header")
		}
		headers = append(headers, name+": "+value)
	}
	var head bytes.Buffer
	head.WriteString(strings.Join(headers, "\r\n") + "\r\n\r\n")

	altHeader := textproto.MIMEHeader{}
	var altBody bytes.Buffer
	alt := multipart.NewWriter(&altBody)
	altHeader.Set("Content-Type", "multipart/alternative; boundary="+alt.Boundary())
	writeQP := func(w *multipart.Writer, contentType, content string) error {
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", contentType)
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		part, err := w.CreatePart(h)
		if err != nil {
			return err
		}
		qp := quotedprintable.NewWriter(part)
		if _, err = qp.Write([]byte(strings.ReplaceAll(content, "\n", "\r\n"))); err != nil {
			return err
		}
		return qp.Close()
	}
	if err := writeQP(alt, "text/plain; charset=utf-8", msg.Text); err != nil {
		return nil, err
	}
	if msg.HTML != "" {
		if err := writeQP(alt, "text/html; charset=utf-8", msg.HTML); err != nil {
			return nil, err
		}
	}
	if err := alt.Close(); err != nil {
		return nil, err
	}
	part, err := mixed.CreatePart(altHeader)
	if err != nil {
		return nil, err
	}
	if _, err = part.Write(altBody.Bytes()); err != nil {
		return nil, err
	}
	for _, att := range msg.Attachments {
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", att.ContentType)
		h.Set("Content-Transfer-Encoding", "base64")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename}))
		p, err := mixed.CreatePart(h)
		if err != nil {
			return nil, err
		}
		encoded := base64.StdEncoding.EncodeToString(att.Content)
		for len(encoded) > 76 {
			_, _ = p.Write([]byte(encoded[:76] + "\r\n"))
			encoded = encoded[76:]
		}
		_, _ = p.Write([]byte(encoded + "\r\n"))
	}
	if err = mixed.Close(); err != nil {
		return nil, err
	}
	return append(head.Bytes(), out.Bytes()...), nil
}
