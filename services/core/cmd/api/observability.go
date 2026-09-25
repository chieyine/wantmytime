package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/observe"
	"aside/core/internal/store"
)

var instanceName = func() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "api"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}()

// beat records that a background worker completed a cycle (and its error, if any).
func (a *API) beat(ctx context.Context, name string, cycleErr error) {
	if ctx.Err() != nil {
		return
	}
	q := store.New(a.db)
	var err error
	if cycleErr != nil {
		text := cycleErr.Error()
		if len(text) > 500 {
			text = text[:500]
		}
		err = q.RecordWorkerError(ctx, store.RecordWorkerErrorParams{Name: name, Instance: instanceName, ErrorText: text})
	} else {
		err = q.RecordWorkerBeat(ctx, store.RecordWorkerBeatParams{Name: name, Instance: instanceName})
	}
	if err != nil && ctx.Err() == nil {
		a.log().WarnContext(ctx, "worker heartbeat not recorded", "worker", name, "error", err.Error())
	}
}

func (a *API) metricsHandler() http.HandlerFunc {
	metrics := a.metrics
	if metrics == nil {
		metrics = observe.NewMetrics()
		a.metrics = metrics
	}
	return func(w http.ResponseWriter, r *http.Request) {
		a.metrics.Handler(os.Getenv("METRICS_TOKEN"))(w, r)
	}
}

// operationalGauges exposes queue depths and worker liveness to /metrics.
func (a *API) operationalGauges(ctx context.Context) ([]observe.Gauge, error) {
	q := store.New(a.db)
	var out []observe.Gauge
	events, err := q.ProviderEventCountsByState(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		out = append(out, observe.Gauge{Name: "aside_provider_events", Help: "Provider webhook events by processing state.", Labels: map[string]string{"state": e.State}, Value: float64(e.Total)})
	}
	notes, err := q.NotificationCountsByState(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		out = append(out, observe.Gauge{Name: "aside_notifications", Help: "Email outbox messages by state.", Labels: map[string]string{"state": n.State}, Value: float64(n.Total)})
	}
	signals, err := q.AlertSignals(ctx)
	if err != nil {
		return nil, err
	}
	gauges, err := q.OperationalGauges(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out,
		observe.Gauge{Name: "aside_provider_event_oldest_pending_seconds", Help: "Age of the oldest queued or retrying provider event.", Value: signals.ProviderOldestPendingSeconds},
		observe.Gauge{Name: "aside_notification_oldest_due_seconds", Help: "How long the oldest due email has waited.", Value: signals.NotificationsOldestDueSeconds},
		observe.Gauge{Name: "aside_payment_exceptions_open", Help: "Payment exceptions not yet resolved.", Value: float64(signals.PaymentExceptionsOpen)},
		observe.Gauge{Name: "aside_provider_cases_open", Help: "Disputes and refunds not yet resolved.", Value: float64(gauges.ProviderCasesOpen)},
		observe.Gauge{Name: "aside_provider_cases_due_soon", Help: "Open provider cases with a deadline in the next 72 hours.", Value: float64(signals.ProviderCasesDueSoon)},
		observe.Gauge{Name: "aside_active_holds", Help: "Unexpired slot holds.", Value: float64(gauges.ActiveHolds)},
		observe.Gauge{Name: "aside_bookings_next_24h", Help: "Confirmed bookings starting in the next 24 hours.", Value: float64(gauges.BookingsNext24h)},
		observe.Gauge{Name: "aside_alerts_firing", Help: "Operational alerts currently firing.", Value: float64(gauges.AlertsFiring)},
	)
	beats, err := q.WorkerHeartbeats(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range beats {
		out = append(out, observe.Gauge{Name: "aside_worker_heartbeat_age_seconds", Help: "Seconds since each background worker last completed a cycle.", Labels: map[string]string{"worker": b.Name}, Value: b.AgeSeconds})
	}
	return out, nil
}

// --- watchdog -------------------------------------------------------------

const alertReminderInterval = 6 * time.Hour

// backupStaleAfter allows a daily backup job a couple of hours of slack.
const backupStaleAfter = 26 * time.Hour

type alertRule struct {
	key      string
	severity string
	firing   bool
	value    float64
	summary  string
}

type alertMessage struct {
	subject string
	body    string
}

func (a *API) runWatchdog(ctx context.Context) {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		err := a.evaluateAlerts(ctx)
		if err != nil && ctx.Err() == nil {
			a.log().ErrorContext(ctx, "watchdog evaluation failed", "error", err.Error())
		}
		a.beat(ctx, "watchdog", err)
		timer.Reset(time.Minute)
	}
}

func alertRecipients() []string {
	var out []string
	for _, part := range strings.Split(os.Getenv("ALERT_EMAILS"), ",") {
		if email := strings.ToLower(strings.TrimSpace(part)); validEmail(email) {
			out = append(out, email)
		}
	}
	return out
}

// alertRules turns current signals into the list of conditions to evaluate.
func (a *API) alertRules(ctx context.Context, q *store.Queries) ([]alertRule, error) {
	s, err := q.AlertSignals(ctx)
	if err != nil {
		return nil, err
	}
	rules := []alertRule{
		{"provider_events_failed", "critical", s.ProviderEventsFailed > 0, float64(s.ProviderEventsFailed),
			fmt.Sprintf("%d payment provider event(s) failed verification after every retry. A buyer may have paid without a booking. Review Operations > Provider events.", s.ProviderEventsFailed)},
		{"provider_events_stuck", "critical", s.ProviderOldestPendingSeconds > 15*60, s.ProviderOldestPendingSeconds,
			fmt.Sprintf("The oldest payment provider event has waited %s for verification. The payment worker may be down.", humanSeconds(s.ProviderOldestPendingSeconds))},
		{"payment_exceptions_open", "critical", s.PaymentExceptionsOpen > 0, float64(s.PaymentExceptionsOpen),
			fmt.Sprintf("%d payment exception(s) need review (refunds of unbooked payments start by themselves and are not counted). Review Operations > Payment exceptions.", s.PaymentExceptionsOpen)},
		{"provider_cases_due_soon", "critical", s.ProviderCasesDueSoon > 0, float64(s.ProviderCasesDueSoon),
			fmt.Sprintf("%d dispute or refund case(s) have a provider deadline within 72 hours. Review Operations > Disputes and refunds.", s.ProviderCasesDueSoon)},
	}
	if autoMeetingLinksEnabled() {
		// Links are created automatically at the deadline; alert only when
		// that did not happen (for example the link encryption key is missing).
		var missed int64
		if err = a.db.QueryRow(ctx, `SELECT count(*) FROM bookings WHERE state='confirmed' AND meeting_url IS NULL AND meeting_deadline<now()-interval '5 minutes' AND starts_at>now()`).Scan(&missed); err != nil {
			return nil, err
		}
		rules = append(rules, alertRule{"meetings_missing_link_soon", "critical", missed > 0, float64(missed),
			fmt.Sprintf("%d booking(s) passed their link deadline without a meeting link, and none could be created automatically. Check MEETING_LINK_ENCRYPTION_KEY, then Operations > Meeting delivery.", missed)})
	} else {
		rules = append(rules, alertRule{"meetings_missing_link_soon", "warning", s.MeetingsMissingLinkSoon > 0, float64(s.MeetingsMissingLinkSoon),
			fmt.Sprintf("%d booking(s) start within 2 hours without a meeting link. Review Operations > Meeting delivery.", s.MeetingsMissingLinkSoon)})
	}
	if a.emailConfigured() {
		rules = append(rules,
			alertRule{"notifications_failed", "warning", s.NotificationsFailed24h > 0, float64(s.NotificationsFailed24h),
				fmt.Sprintf("%d email(s) still failed after every automatic retry in the last week. Check the email provider. Review Operations > System health.", s.NotificationsFailed24h)},
			alertRule{"notifications_backlog", "warning", s.NotificationsOldestDueSeconds > 30*60, s.NotificationsOldestDueSeconds,
				fmt.Sprintf("The oldest due email has waited %s. The email worker or provider may be down.", humanSeconds(s.NotificationsOldestDueSeconds))},
		)
	}
	var refundsFailed, refundsWaiting, noShowDisputes int64
	if err = a.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM refunds WHERE state='failed' AND (provider_refund_id IS NOT NULL OR payment_attempt_id IS NULL OR auto_retries>=3)),(SELECT count(*) FROM refunds WHERE state='pending_approval' AND created_at<now()-interval '1 hour'),(SELECT count(*) FROM no_show_reports WHERE state='disputed')`).Scan(&refundsFailed, &refundsWaiting, &noShowDisputes); err != nil {
		return nil, err
	}
	rules = append(rules,
		alertRule{"refunds_failed", "critical", refundsFailed > 0, float64(refundsFailed),
			fmt.Sprintf("%d refund(s) still failed after automatic retries. The buyer has not been paid back. Review Operations > Refunds.", refundsFailed)},
		alertRule{"refunds_awaiting_approval", "warning", refundsWaiting > 0, float64(refundsWaiting),
			fmt.Sprintf("%d refund(s) have waited over an hour for approval. Review Operations > Refunds.", refundsWaiting)},
		alertRule{"no_show_disputes", "warning", noShowDisputes > 0, float64(noShowDisputes),
			fmt.Sprintf("%d disputed no-show report(s) need a decision. Review Operations > No-shows.", noShowDisputes)},
	)
	// Payouts: failures need a person; anything still unpaid a day after it
	// was due (and not held by a dispute) means sellers are waiting.
	var payoutsFailed, payoutsLate, problemsOpen int64
	if err = a.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM seller_payouts WHERE state='failed' AND generation>=5),
		(SELECT count(*) FROM seller_payouts po JOIN bookings b ON b.id=po.booking_id WHERE po.state IN ('scheduled','processing') AND `+payoutReleaseAtSQL+` < now()-interval '24 hours' AND (po.state='processing' OR (`+payoutHoldSQL+`)='')),
		(SELECT count(*) FROM bookings WHERE issue_reason IS NOT NULL AND issue_resolved_at IS NULL AND issue_reported_by='buyer' AND issue_disputed_at IS NOT NULL)`, int(payoutDelay()/time.Minute)).Scan(&payoutsFailed, &payoutsLate, &problemsOpen); err != nil {
		return nil, err
	}
	rules = append(rules,
		alertRule{"payouts_failed", "critical", payoutsFailed > 0, float64(payoutsFailed),
			fmt.Sprintf("%d seller payout(s) still failed after every automatic retry (2, 12, 24 and 48 hours). The seller has been emailed; a new payout account sends it at once. Review Operations > Payouts.", payoutsFailed)},
		alertRule{"payouts_late", "warning", payoutsLate > 0, float64(payoutsLate),
			fmt.Sprintf("%d seller payout(s) are more than a day late (no bank account, funds not settled, or provider trouble). Review Operations > Payouts.", payoutsLate)},
		alertRule{"problems_open", "warning", problemsOpen > 0, float64(problemsOpen),
			fmt.Sprintf("%d problem report(s) are disputed by the seller and need your decision. Review Operations > Bookings.", problemsOpen)},
	)
	beats, err := q.WorkerHeartbeats(ctx)
	if err != nil {
		return nil, err
	}
	expected := map[string]bool{"provider_events": true, "lifecycle": true}
	if a.emailConfigured() {
		expected["notifications"] = true
	}
	if a.googleCalendarConfigured() {
		expected["calendar"] = true
	}
	for _, b := range beats {
		if b.Name == "backup" {
			// Written by infrastructure/backup/backup.sh after a dump passes its
			// restore check; a failed run records only last_error_at.
			rules = append(rules, alertRule{"backup_stale", "critical", b.AgeSeconds > backupStaleAfter.Seconds(), b.AgeSeconds,
				fmt.Sprintf("The last verified database backup finished %s ago. Check the backup job.", humanSeconds(b.AgeSeconds))})
			failed := b.LastErrorAt != nil && b.LastErrorAt.After(b.LastBeatAt)
			value, reason := 0.0, ""
			if failed {
				value = float64(b.LastErrorAt.Unix())
				if b.LastError != nil {
					reason = *b.LastError
				}
			}
			rules = append(rules, alertRule{"backup_failed", "critical", failed, value,
				"The most recent database backup or its restore check failed: " + reason})
			continue
		}
		if !expected[b.Name] {
			continue
		}
		rules = append(rules, alertRule{"worker_stale_" + b.Name, "critical", b.AgeSeconds > 180, b.AgeSeconds,
			fmt.Sprintf("The %s worker has not completed a cycle for %s. Restart the API and check its logs.", strings.ReplaceAll(b.Name, "_", " "), humanSeconds(b.AgeSeconds))})
	}
	return rules, nil
}

// evaluateAlerts fires, reminds and resolves alerts. Only one API instance
// evaluates at a time; emails are sent after the state is committed.
func (a *API) evaluateAlerts(ctx context.Context) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('aside-watchdog'))`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	q := store.New(tx)
	rules, err := a.alertRules(ctx, q)
	if err != nil {
		return err
	}
	now := time.Now()
	var messages []alertMessage
	for _, rule := range rules {
		existing, getErr := q.GetAlert(ctx, rule.key)
		exists := getErr == nil
		if getErr != nil && !errors.Is(getErr, pgx.ErrNoRows) {
			return getErr
		}
		if rule.firing {
			// Count-type alerts re-notify when the count rises; age-type alerts
			// (which rise every minute by definition) only on the reminder interval.
			ageBased := strings.HasSuffix(rule.key, "_stuck") || strings.HasPrefix(rule.key, "worker_stale") || rule.key == "notifications_backlog" || rule.key == "backup_stale"
			notify := !exists || existing.State == "resolved" || existing.LastNotifiedAt == nil ||
				(!ageBased && rule.value > existing.Value) ||
				now.Sub(*existing.LastNotifiedAt) >= alertReminderInterval
			var notifiedAt *time.Time
			if notify {
				notifiedAt = &now
				label := strings.ToUpper(rule.severity)
				messages = append(messages, alertMessage{subject: "[WantMyTime " + label + "] " + alertTitle(rule.key), body: rule.summary + "\n\nThis alert repeats every 6 hours while the condition lasts, and sooner if it gets worse. You will get a separate email when it resolves."})
				level := "warning"
				if rule.severity == "critical" {
					level = "error"
				}
				a.reporter.CaptureMessage(ctx, level, "Alert: "+alertTitle(rule.key), map[string]string{"alert": rule.key})
			}
			if err = q.FireAlert(ctx, store.FireAlertParams{Key: rule.key, Severity: rule.severity, Summary: rule.summary, Value: rule.value, NotifiedAt: notifiedAt}); err != nil {
				return err
			}
		} else if exists && existing.State == "firing" {
			if err = q.ResolveAlert(ctx, rule.key); err != nil {
				return err
			}
			messages = append(messages, alertMessage{subject: "[WantMyTime RESOLVED] " + alertTitle(rule.key), body: "This condition has cleared: " + existing.Summary})
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	recipients := alertRecipients()
	for _, m := range messages {
		a.log().WarnContext(ctx, "operational alert", "subject", m.subject)
		for _, to := range recipients {
			if !a.sendEmail(to, m.subject, m.body) {
				a.log().ErrorContext(ctx, "alert email not delivered", "subject", m.subject)
			}
		}
	}
	return nil
}

func alertTitle(key string) string {
	titles := map[string]string{
		"provider_events_failed":     "Payment events failed verification",
		"provider_events_stuck":      "Payment events are not being processed",
		"payment_exceptions_open":    "Payment exceptions need review",
		"provider_cases_due_soon":    "Dispute or refund deadline approaching",
		"meetings_missing_link_soon": "Bookings starting soon without a meeting link",
		"notifications_failed":       "Emails failed to send",
		"notifications_backlog":      "Emails are not being sent",
		"backup_stale":               "Database backups have stopped",
		"backup_failed":              "Database backup failed",
		"refunds_failed":             "Refunds failed",
		"refunds_awaiting_approval":  "Refunds waiting for approval",
		"no_show_disputes":           "Disputed no-shows need a decision",
		"payouts_failed":             "Seller payouts failed",
		"payouts_late":               "Seller payouts are late",
		"problems_open":              "Reported problems are holding payouts",
	}
	if t, ok := titles[key]; ok {
		return t
	}
	if strings.HasPrefix(key, "worker_stale_") {
		return "Background worker stopped: " + strings.ReplaceAll(strings.TrimPrefix(key, "worker_stale_"), "_", " ")
	}
	return key
}

func humanSeconds(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	if d >= time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	if d >= time.Minute {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	return fmt.Sprintf("%d seconds", int(d.Seconds()))
}

// opsAlerts lists firing and recently resolved alerts plus worker liveness.
func (a *API) opsAlerts(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	q := store.New(a.db)
	alerts, err := q.ListAlerts(r.Context())
	if err != nil {
		problem(w, 503, "ALERTS_UNAVAILABLE", "Alerts could not be loaded.")
		return
	}
	beats, err := q.WorkerHeartbeats(r.Context())
	if err != nil {
		problem(w, 503, "ALERTS_UNAVAILABLE", "Worker status could not be loaded.")
		return
	}
	alertItems := []map[string]any{}
	for _, al := range alerts {
		alertItems = append(alertItems, map[string]any{"key": al.Key, "title": alertTitle(al.Key), "state": al.State, "severity": al.Severity, "summary": al.Summary, "first_fired_at": al.FirstFiredAt, "last_notified_at": al.LastNotifiedAt, "resolved_at": al.ResolvedAt, "updated_at": al.UpdatedAt})
	}
	workerItems := []map[string]any{}
	for _, b := range beats {
		workerItems = append(workerItems, map[string]any{"name": b.Name, "instance": b.Instance, "last_beat_at": b.LastBeatAt, "age_seconds": int64(b.AgeSeconds), "last_error_at": b.LastErrorAt, "last_error": b.LastError})
	}
	jsonOut(w, 200, map[string]any{"alerts": alertItems, "workers": workerItems, "alert_recipients_configured": len(alertRecipients()) > 0, "error_reporting_configured": a.reporter != nil})
}
