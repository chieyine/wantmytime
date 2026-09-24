//go:build integration

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// sentTo returns the subjects mailed to one address since the given index.
func (m *mailbox) sentTo(address string, since int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, s := range m.sent[since:] {
		if to, subject, ok := strings.Cut(s, "|"); ok && to == address {
			out = append(out, subject)
		}
	}
	return out
}

func (m *mailbox) mark() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

func countWith(subjects []string, fragment string) int {
	n := 0
	for _, s := range subjects {
		if strings.Contains(s, fragment) {
			n++
		}
	}
	return n
}

func TestRequestIDAndMetricsEndpoint(t *testing.T) {
	t.Setenv("METRICS_TOKEN", "integration-metrics-token")
	h := newHarness(t)
	h.api.metrics.AddGauges(h.api.operationalGauges)
	anon := h.client("")

	req, _ := http.NewRequest("GET", h.server.URL+"/health", nil)
	req.Header.Set("X-Request-ID", "gateway-request-0001")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Header.Get("X-Request-ID") != "gateway-request-0001" {
		t.Fatalf("gateway request ID not propagated: %q", res.Header.Get("X-Request-ID"))
	}

	// Error bodies from the API keep their normal shape; the ID travels in the header.
	notFound, _ := http.Get(h.server.URL + "/api/v1/no-such-route")
	notFound.Body.Close()
	if len(notFound.Header.Get("X-Request-ID")) != 32 {
		t.Fatalf("missing generated request ID on unmatched route")
	}

	if got := anon.do("GET", "/metrics", nil).Status; got != 401 {
		t.Fatalf("metrics without token: %d", got)
	}
	body := string(anon.do("GET", "/metrics", nil, "Authorization", "Bearer integration-metrics-token").Body)
	for _, want := range []string{
		`route="GET /health"`, // ServeMux patterns survive the CORS and security-header wrappers
		`route="unmatched"`,
		"aside_alerts_firing ",
		"aside_active_holds ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "gauge source failed") {
		t.Errorf("operational gauges failed:\n%s", body)
	}

	t.Setenv("METRICS_TOKEN", "")
	if got := anon.do("GET", "/metrics", nil).Status; got != 404 {
		t.Fatalf("metrics with no token configured: %d, want 404", got)
	}
}

func TestWorkerHeartbeatsRecordErrors(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	name := unique("worker_it_")
	h.api.beat(ctx, name, nil)
	h.api.beat(ctx, name, errors.New("provider timeout"))
	if got := scalar[string](t, `SELECT coalesce(last_error,'') FROM worker_heartbeats WHERE name=$1`, name); got != "provider timeout" {
		t.Fatalf("last_error = %q", got)
	}
	h.api.beat(ctx, name, nil)
	if got := scalar[string](t, `SELECT coalesce(last_error,'') FROM worker_heartbeats WHERE name=$1`, name); got != "provider timeout" {
		t.Fatalf("a clean cycle must keep the last error for review, got %q", got)
	}
	_, _ = itPool.Exec(ctx, `DELETE FROM worker_heartbeats WHERE name=$1`, name)
}

func TestWatchdogAlertsFireRemindAndResolve(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	recipient := unique("alerts") + "@ops.test"
	t.Setenv("ALERT_EMAILS", " "+recipient+" , ")
	exec := func(sql string) {
		t.Helper()
		if _, err := itPool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DELETE FROM ops_alerts WHERE key IN ('backup_stale','backup_failed')`)
	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(), `DELETE FROM worker_heartbeats WHERE name='backup'`)
		_, _ = itPool.Exec(context.Background(), `DELETE FROM ops_alerts WHERE key IN ('backup_stale','backup_failed')`)
	})

	// The last good backup is 30 hours old and the latest attempt failed.
	exec(`INSERT INTO worker_heartbeats(name,instance,last_beat_at,last_error_at,last_error)
	      VALUES('backup','it',now()-interval '30 hours',now()-interval '1 hour','backup failed during: restore check')
	      ON CONFLICT(name) DO UPDATE SET last_beat_at=EXCLUDED.last_beat_at,last_error_at=EXCLUDED.last_error_at,last_error=EXCLUDED.last_error`)
	mark := itMail.mark()
	if err := h.api.evaluateAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	sent := itMail.sentTo(recipient, mark)
	if countWith(sent, "[WantMyTime CRITICAL] Database backups have stopped") != 1 || countWith(sent, "[WantMyTime CRITICAL] Database backup failed") != 1 {
		t.Fatalf("expected both backup alerts, got %v", sent)
	}
	if got := scalar[string](t, `SELECT state FROM ops_alerts WHERE key='backup_failed'`); got != "firing" {
		t.Fatalf("backup_failed state %q", got)
	}

	// Still failing a minute later: no repeat email inside the reminder interval.
	mark = itMail.mark()
	if err := h.api.evaluateAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	if sent = itMail.sentTo(recipient, mark); countWith(sent, "backup") != 0 {
		t.Fatalf("alerts repeated too soon: %v", sent)
	}

	// A new failure is worse than the one already reported, so it re-notifies at once.
	exec(`UPDATE worker_heartbeats SET last_error_at=now() WHERE name='backup'`)
	mark = itMail.mark()
	if err := h.api.evaluateAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	if sent = itMail.sentTo(recipient, mark); countWith(sent, "Database backup failed") != 1 || countWith(sent, "have stopped") != 0 {
		t.Fatalf("new failure should re-notify only backup_failed, got %v", sent)
	}

	// The ops endpoint shows both firing alerts and the backup heartbeat.
	ops, _ := h.operator()
	out := ops.expect(200, "GET", "/api/v1/ops/alerts", nil)
	if out["alert_recipients_configured"] != true {
		t.Fatalf("recipients not reported as configured: %v", out)
	}
	firing := map[string]bool{}
	for _, raw := range out["alerts"].([]any) {
		al := raw.(map[string]any)
		if al["state"] == "firing" {
			firing[al["key"].(string)] = true
		}
	}
	if !firing["backup_stale"] || !firing["backup_failed"] {
		t.Fatalf("ops alerts missing backup alerts: %v", out["alerts"])
	}
	var sawBackup bool
	for _, raw := range out["workers"].([]any) {
		sawBackup = sawBackup || raw.(map[string]any)["name"] == "backup"
	}
	if !sawBackup {
		t.Fatalf("backup heartbeat missing from workers: %v", out["workers"])
	}
	if got := h.client("").do("GET", "/api/v1/ops/alerts", nil).Status; got != 401 && got != 403 {
		t.Fatalf("anonymous ops alerts: %d", got)
	}

	// A verified backup clears both alerts with one resolution email each.
	exec(`UPDATE worker_heartbeats SET last_beat_at=now() WHERE name='backup'`)
	mark = itMail.mark()
	if err := h.api.evaluateAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	sent = itMail.sentTo(recipient, mark)
	if countWith(sent, "[WantMyTime RESOLVED] Database backups have stopped") != 1 || countWith(sent, "[WantMyTime RESOLVED] Database backup failed") != 1 {
		t.Fatalf("expected two resolution emails, got %v", sent)
	}
	if got := scalar[int64](t, `SELECT count(*) FROM ops_alerts WHERE key IN ('backup_stale','backup_failed') AND state='resolved' AND resolved_at IS NOT NULL`); got != 2 {
		t.Fatalf("resolved alerts: %d", got)
	}
}

func TestWatchdogSkipsWhenAnotherInstanceHoldsTheLock(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	recipient := unique("alerts") + "@ops.test"
	t.Setenv("ALERT_EMAILS", recipient)
	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(), `DELETE FROM worker_heartbeats WHERE name='backup'`)
		_, _ = itPool.Exec(context.Background(), `DELETE FROM ops_alerts WHERE key IN ('backup_stale','backup_failed')`)
	})
	if _, err := itPool.Exec(ctx, `INSERT INTO worker_heartbeats(name,instance,last_beat_at) VALUES('backup','it',now()-interval '30 hours') ON CONFLICT(name) DO UPDATE SET last_beat_at=EXCLUDED.last_beat_at, last_error_at=NULL`); err != nil {
		t.Fatal(err)
	}
	tx, err := itPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('aside-watchdog'))`); err != nil {
		t.Fatal(err)
	}
	mark := itMail.mark()
	if err = h.api.evaluateAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	if sent := itMail.sentTo(recipient, mark); len(sent) != 0 {
		t.Fatalf("second instance should not evaluate alerts while locked, sent %v", sent)
	}
}
