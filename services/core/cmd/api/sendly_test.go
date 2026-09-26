package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sendlyHTTPServer(t *testing.T, status int) (*httptest.Server, *map[string]any, *http.Header) {
	t.Helper()
	got, headers := map[string]any{}, http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(404)
			return
		}
		for k, v := range r.Header {
			headers[k] = v
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	t.Setenv("EMAIL_PROVIDER", "sendly")
	t.Setenv("EMAIL_API_KEY", "sk_test_example")
	t.Setenv("EMAIL_FROM", "WantMyTime <bookings@wantmytime.com>")
	t.Setenv("SENDLY_API_BASE", server.URL)
	return server, &got, &headers
}

func TestSendlyBookingEmail(t *testing.T) {
	_, got, headers := sendlyHTTPServer(t, 202)
	a := &API{env: "production"}
	if !a.emailConfigured() {
		t.Fatal("sendly with a key and sender should count as configured")
	}
	msg := emailContent{Subject: "Booked", Heading: "You’re booked", Paragraphs: []string{"See you then."}, Calendar: &calendarEvent{UID: "b1", Start: time.Now(), End: time.Now().Add(30 * time.Minute), Summary: "Call"}}.message("ada@example.com", "booking:b1:confirmed")
	if !a.deliverEmail(context.Background(), msg) {
		t.Fatal("delivery reported failure")
	}
	p := *got
	if headers.Get("Authorization") != "Bearer sk_test_example" || headers.Get("Idempotency-Key") != "booking:b1:confirmed" {
		t.Fatalf("headers %v", *headers)
	}
	to, _ := p["to"].([]any)
	if p["channel"] != "email" || len(to) != 1 || to[0] != "ada@example.com" || p["from"] != "WantMyTime <bookings@wantmytime.com>" || p["subject"] != "Booked" {
		t.Fatalf("payload %v", p)
	}
	if p["tracking"] != false || !strings.Contains(p["text"].(string), "See you then.") || !strings.Contains(p["html"].(string), "See you then.") {
		t.Fatalf("booking emails keep links untracked and include text: %v", p)
	}
	atts, _ := p["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("attachments %v", p["attachments"])
	}
	att := atts[0].(map[string]any)
	ics, err := base64.StdEncoding.DecodeString(att["content"].(string))
	if att["filename"] != "wantmytime-booking.ics" || !strings.HasPrefix(att["contentType"].(string), "text/calendar") || err != nil || !strings.Contains(string(ics), "BEGIN:VCALENDAR") {
		t.Fatalf("calendar attachment %v", att)
	}
	if _, ok := p["unsubscribe"]; ok {
		t.Fatal("Sendly's own unsubscribe would suppress booking emails too; it must never be set")
	}
}

func TestSendlyPlainTextBecomesHTML(t *testing.T) {
	_, got, _ := sendlyHTTPServer(t, 202)
	a := &API{env: "production"}
	if !a.deliverEmail(context.Background(), emailMessage{To: "ada@example.com", Subject: "Your code", Text: "Your code is 12345678.\n\nIt expires <soon>."}) {
		t.Fatal("delivery reported failure")
	}
	html, _ := (*got)["html"].(string)
	if !strings.Contains(html, "<p>Your code is 12345678.</p>") || !strings.Contains(html, "&lt;soon&gt;") {
		t.Fatalf("html %s", html)
	}
}

func TestSendlyRejectionIsAFailure(t *testing.T) {
	sendlyHTTPServer(t, 401)
	a := &API{env: "production"}
	if a.deliverEmail(context.Background(), emailMessage{To: "ada@example.com", Subject: "Hi", Text: "Hello", HTML: "<p>Hello</p>"}) {
		t.Fatal("a rejected send must report failure so the outbox retries")
	}
}

func TestSendlyAnnouncementWithoutRelayUsesHTTP(t *testing.T) {
	_, got, _ := sendlyHTTPServer(t, 202)
	t.Setenv("SENDLY_SMTP_USERNAME", "")
	a := &API{env: "production"}
	msg := broadcastMessage(broadcast{id: "b1", subject: "News", heading: "News", body: "Hello"}, "ada@example.com", "tok")
	if !a.deliverEmail(context.Background(), msg) {
		t.Fatal("delivery reported failure")
	}
	if (*got)["tracking"] != true || !strings.Contains((*got)["html"].(string), "/unsubscribe?token=tok") {
		t.Fatalf("announcement over HTTP %v", *got)
	}
}

// fakeRelay is a minimal SMTP server that insists on STARTTLS before AUTH.
type fakeRelay struct {
	addr, user, pass, from, to, data string
	authBeforeTLS                    bool
	done                             chan struct{}
}

func startFakeRelay(t *testing.T) (*fakeRelay, *tls.Config) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "relay"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	r := &fakeRelay{addr: ln.Addr().String(), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		say := func(s string) { rw.WriteString(s + "\r\n"); rw.Flush() }
		say("220 relay ESMTP")
		secure := false
		for {
			line, err := rw.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.TrimRight(line, "\r\n")
			upper := strings.ToUpper(cmd)
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				if secure {
					say("250-relay\r\n250 AUTH PLAIN")
				} else {
					say("250-relay\r\n250 STARTTLS")
				}
			case upper == "STARTTLS":
				say("220 go ahead")
				tlsConn := tls.Server(conn, serverTLS)
				if tlsConn.Handshake() != nil {
					return
				}
				conn, secure = tlsConn, true
				rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
			case strings.HasPrefix(upper, "AUTH PLAIN"):
				if !secure {
					r.authBeforeTLS = true
				}
				raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(cmd[len("AUTH PLAIN"):]))
				parts := strings.Split(string(raw), "\x00")
				if len(parts) == 3 {
					r.user, r.pass = parts[1], parts[2]
				}
				say("235 ok")
			case strings.HasPrefix(upper, "MAIL FROM:"):
				r.from = strings.Trim(cmd[len("MAIL FROM:"):], "<> ")
				say("250 ok")
			case strings.HasPrefix(upper, "RCPT TO:"):
				r.to = strings.Trim(cmd[len("RCPT TO:"):], "<> ")
				say("250 ok")
			case upper == "DATA":
				say("354 go")
				var b strings.Builder
				for {
					l, err := rw.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				r.data = b.String()
				say("250 queued")
			case upper == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	return r, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
}

func TestSendlyRelayCarriesOurUnsubscribeHeaders(t *testing.T) {
	relay, clientTLS := startFakeRelay(t)
	host, port, _ := net.SplitHostPort(relay.addr)
	msg := broadcastMessage(broadcast{id: "b1", subject: "News", heading: "News", body: "Hello"}, "ada@example.com", "tok123")
	if err := sendRelaySMTP(context.Background(), host, port, clientTLS, "smtp-user", "smtp-pass", "WantMyTime <news@wantmytime.com>", msg); err != nil {
		t.Fatal(err)
	}
	<-relay.done
	if relay.authBeforeTLS || relay.user != "smtp-user" || relay.pass != "smtp-pass" {
		t.Fatalf("auth: beforeTLS=%v user=%q", relay.authBeforeTLS, relay.user)
	}
	if relay.from != "news@wantmytime.com" || relay.to != "ada@example.com" {
		t.Fatalf("envelope %q -> %q", relay.from, relay.to)
	}
	if !strings.Contains(relay.data, "List-Unsubscribe: <") || !strings.Contains(relay.data, "/api/v1/marketing/one-click?token=tok123>") || !strings.Contains(relay.data, "List-Unsubscribe-Post: List-Unsubscribe=One-Click") {
		t.Fatalf("headers missing:\n%s", relay.data)
	}
}

func TestSendlyRelayRefusesWithoutTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		rw.WriteString("220 plain\r\n")
		rw.Flush()
		for {
			if _, err := rw.ReadString('\n'); err != nil {
				return
			}
			rw.WriteString("250 ok\r\n") // never offers STARTTLS
			rw.Flush()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	err = sendRelaySMTP(context.Background(), host, port, &tls.Config{ServerName: host}, "u", "p", "WantMyTime <news@wantmytime.com>", emailMessage{To: "ada@example.com", Subject: "Hi", Text: "Hi"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected a refusal without STARTTLS, got %v", err)
	}
}
