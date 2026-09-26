package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"
)

// Sendly (sendlyai.com), with EMAIL_PROVIDER=sendly. Field names follow
// https://developer.sendlyai.com/docs ("Send a message").
//
// Transactional email goes through POST /v1/messages. Announcements carry
// our own List-Unsubscribe headers, which the HTTP API does not accept, so
// they go through Sendly's SMTP relay when SENDLY_SMTP_USERNAME and
// SENDLY_SMTP_PASSWORD are set. We never use Sendly's own "unsubscribe": true:
// it adds the person to Sendly's suppression list, which then skips every
// later email to them, sign-in codes and booking emails included.

func (a *API) sendSendly(ctx context.Context, msg emailMessage) bool {
	announcement := len(msg.Headers) > 0
	if announcement {
		if user, pass := os.Getenv("SENDLY_SMTP_USERNAME"), os.Getenv("SENDLY_SMTP_PASSWORD"); user != "" && pass != "" {
			if err := a.sendSendlySMTP(ctx, msg, user, pass); err != nil {
				a.log().WarnContext(ctx, "email delivery failed", "code", "SMTP_ERROR", "provider", "sendly", "error", err.Error())
				return false
			}
			return true
		}
		warnNoRelay.Do(func() {
			a.log().WarnContext(ctx, "announcements are going out without one-click unsubscribe headers; set SENDLY_SMTP_USERNAME and SENDLY_SMTP_PASSWORD")
		})
	}
	return a.sendSendlyHTTP(ctx, msg, announcement)
}

var warnNoRelay sync.Once

func sendlyFrom(msg emailMessage) string {
	if msg.From != "" {
		return msg.From
	}
	return os.Getenv("EMAIL_FROM")
}

func (a *API) sendSendlyHTTP(ctx context.Context, msg emailMessage, announcement bool) bool {
	key, from := os.Getenv("EMAIL_API_KEY"), sendlyFrom(msg)
	if key == "" || from == "" || strings.ContainsAny(from, "\r\n") {
		return false
	}
	body := msg.HTML
	if body == "" {
		body = textAsHTML(msg.Text)
	}
	payload := map[string]any{
		"channel": "email",
		"to":      []string{msg.To},
		"from":    from,
		"subject": msg.Subject,
		"html":    body,
		// Click tracking rewrites links through Sendly. Booking emails carry
		// private links, so only announcements are tracked.
		"tracking": announcement,
	}
	if msg.Text != "" {
		payload["text"] = msg.Text
	}
	if len(msg.Attachments) > 0 {
		items := []map[string]string{}
		for _, att := range msg.Attachments {
			items = append(items, map[string]string{"filename": att.Filename, "content": base64.StdEncoding.EncodeToString(att.Content), "contentType": att.ContentType})
		}
		payload["attachments"] = items
	}
	encoded, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(envOr("SENDLY_API_BASE", "https://api.sendlyai.com"), "/")+"/v1/messages", bytes.NewReader(encoded))
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	// The same key and body returns the first answer instead of sending twice.
	if msg.IdempotencyKey != "" && len(msg.IdempotencyKey) <= 256 {
		req.Header.Set("Idempotency-Key", msg.IdempotencyKey)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		a.log().WarnContext(ctx, "email delivery failed", "code", "PROVIDER_UNAVAILABLE", "provider", "sendly")
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		a.log().WarnContext(ctx, "email delivery failed", "code", "PROVIDER_REJECTED", "provider", "sendly", "status", resp.StatusCode)
		return false
	}
	return true
}

// sendSendlySMTP sends one message through Sendly's relay (STARTTLS on 587).
// It refuses to authenticate over an unencrypted connection.
func (a *API) sendSendlySMTP(ctx context.Context, msg emailMessage, user, pass string) error {
	host := envOr("SENDLY_SMTP_HOST", "smtp.sendlyai.com")
	return sendRelaySMTP(ctx, host, envOr("SENDLY_SMTP_PORT", "587"), &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}, user, pass, sendlyFrom(msg), msg)
}

func sendRelaySMTP(ctx context.Context, host, port string, tlsConfig *tls.Config, user, pass, from string, msg emailMessage) error {
	sender, err := mail.ParseAddress(from)
	if err != nil {
		return fmt.Errorf("invalid sender")
	}
	if !validEmail(msg.To) {
		return fmt.Errorf("invalid recipient")
	}
	raw, err := buildMIME(from, os.Getenv("EMAIL_REPLY_TO"), msg)
	if err != nil {
		return err
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.Hello("wantmytime.com"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("relay does not offer STARTTLS")
	}
	if err = c.StartTLS(tlsConfig); err != nil {
		return err
	}
	if err = c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
		return err
	}
	if err = c.Mail(sender.Address); err != nil {
		return err
	}
	if err = c.Rcpt(msg.To); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(raw); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// textAsHTML turns a plain-text email (sign-in codes, alerts) into minimal,
// escaped HTML, since Sendly needs html (or a template) on every message.
func textAsHTML(text string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:1.55;color:#171817;">`)
	for _, p := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(p), "\n", "<br>") + "</p>")
		}
	}
	b.WriteString("</div>")
	return b.String()
}
