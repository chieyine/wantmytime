package main

import (
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Announcement email: news from WantMyTime and Kredit Technologies,
// sent only to people who said yes. Transactional email never depends on it.

// marketingWording is the exact sentence people agree to. It is stored with
// every consent, so a later change of wording never rewrites what someone saw.
const marketingWording = "Send me news and offers. Unsubscribe any time."

var marketingSources = map[string]bool{"seller_signup": true, "booking": true, "offer": true, "settings": true, "dashboard": true}

type dbExecQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// recordMarketingChoice stores a yes or no for an address and logs it as
// proof of consent. confirmed says the address is known to belong to the
// person (a sign-in code); unconfirmed yeses wait for a paid booking.
func recordMarketingChoice(ctx context.Context, db dbExecQuerier, email, userID string, subscribed bool, source string, confirmed bool) error {
	email = strings.ToLower(strings.TrimSpace(email))
	token, err := randomToken(24)
	if err != nil {
		return err
	}
	var uid *string
	if userID != "" {
		uid = &userID
	}
	if _, err = db.Exec(ctx, `INSERT INTO marketing_contacts(email,user_id,subscribed,source,wording,confirmed_at,unsubscribe_token,consented_at,unsubscribed_at)
		VALUES($1,$2,$3,$4,$5,CASE WHEN $6 THEN now() END,$7,CASE WHEN $3 THEN now() END,CASE WHEN NOT $3 THEN now() END)
		ON CONFLICT(email) DO UPDATE SET
			user_id=COALESCE(EXCLUDED.user_id,marketing_contacts.user_id),
			subscribed=EXCLUDED.subscribed,
			source=EXCLUDED.source,
			wording=EXCLUDED.wording,
			confirmed_at=COALESCE(marketing_contacts.confirmed_at,EXCLUDED.confirmed_at),
			consented_at=CASE WHEN EXCLUDED.subscribed THEN now() ELSE marketing_contacts.consented_at END,
			unsubscribed_at=CASE WHEN EXCLUDED.subscribed THEN NULL ELSE now() END,
			updated_at=now()`, email, uid, subscribed, source, marketingWording, confirmed, token); err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO marketing_consent_events(email,subscribed,source,wording) VALUES($1,$2,$3,$4)`, email, subscribed, source, marketingWording)
	return err
}

// confirmMarketingEmail marks an address as belonging to a real person: they
// entered a sign-in code sent to it, or paid for a booking made with it.
func confirmMarketingEmail(ctx context.Context, db dbExecQuerier, email string) error {
	_, err := db.Exec(ctx, `UPDATE marketing_contacts SET confirmed_at=now(),updated_at=now() WHERE email=$1 AND confirmed_at IS NULL`, strings.ToLower(strings.TrimSpace(email)))
	return err
}

func (a *API) identityVerified(ctx context.Context, userID string) bool {
	var verified bool
	_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_identities WHERE user_id=$1 AND type='email' AND verified_at IS NOT NULL)`, userID).Scan(&verified)
	return verified
}

// myMarketing reports the signed-in person's choice. Guest booking sessions
// may use it too, since buyers are asked when they book.
func (a *API) myMarketing(w http.ResponseWriter, r *http.Request) {
	u, ok, _, _ := a.buyerActor(w, r)
	if !ok {
		return
	}
	var subscribed bool
	var confirmed *time.Time
	err := a.db.QueryRow(r.Context(), `SELECT subscribed,confirmed_at FROM marketing_contacts WHERE email=$1`, strings.ToLower(u.Email)).Scan(&subscribed, &confirmed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Your email preferences could not be loaded.")
		return
	}
	// Ask on the dashboard only once, and never someone who already said no
	// in Settings, unsubscribed from an email or was imported as unsubscribed.
	ask := false
	if !subscribed {
		if err = a.db.QueryRow(r.Context(), `SELECT NOT EXISTS(SELECT 1 FROM marketing_consent_events WHERE email=$1 AND source IN ('dashboard','settings','unsubscribe_link','ops_import'))`, strings.ToLower(u.Email)).Scan(&ask); err != nil {
			ask = false
		}
	}
	jsonOut(w, 200, map[string]any{"subscribed": subscribed, "confirmed": confirmed != nil, "wording": marketingWording, "ask": ask})
}

func (a *API) setMyMarketing(w http.ResponseWriter, r *http.Request) {
	u, ok, _, _ := a.buyerActor(w, r)
	if !ok {
		return
	}
	var in struct {
		Subscribed *bool  `json:"subscribed"`
		Source     string `json:"source"`
	}
	if decode(r, &in) != nil || in.Subscribed == nil {
		problem(w, 400, "INVALID_BODY", "Say whether you want announcement emails.")
		return
	}
	if in.Source == "" {
		in.Source = "settings"
	}
	if !marketingSources[in.Source] {
		problem(w, 422, "INVALID_SOURCE", "Unknown place this choice was made.")
		return
	}
	if err := recordMarketingChoice(r.Context(), a.db, u.Email, u.ID, *in.Subscribed, in.Source, a.identityVerified(r.Context(), u.ID)); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your email preferences could not be saved.")
		return
	}
	jsonOut(w, 200, map[string]any{"subscribed": *in.Subscribed})
}

func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 1 {
		return "•••" + email[max(at, 0):]
	}
	return email[:1] + strings.Repeat("•", min(at-1, 6)) + email[at:]
}

// marketingByToken shows the unsubscribe page what the link is for, without
// changing anything: mail scanners open links, so only a POST unsubscribes.
func (a *API) marketingByToken(w http.ResponseWriter, r *http.Request) {
	var email string
	var subscribed bool
	err := a.db.QueryRow(r.Context(), `SELECT email,subscribed FROM marketing_contacts WHERE unsubscribe_token=$1`, r.URL.Query().Get("token")).Scan(&email, &subscribed)
	if err != nil {
		problem(w, 404, "NOT_FOUND", "This unsubscribe link is not valid.")
		return
	}
	jsonOut(w, 200, map[string]any{"email": maskEmail(email), "subscribed": subscribed})
}

func (a *API) setMarketingByToken(subscribed bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		_ = decode(r, &in)
		// RFC 8058 one-click: mail providers POST to the header URL with the token in the query.
		if in.Token == "" {
			in.Token = r.URL.Query().Get("token")
		}
		var email string
		var userID *string
		if in.Token == "" || a.db.QueryRow(r.Context(), `SELECT email,user_id::text FROM marketing_contacts WHERE unsubscribe_token=$1`, in.Token).Scan(&email, &userID) != nil {
			problem(w, 404, "NOT_FOUND", "This unsubscribe link is not valid.")
			return
		}
		uid := ""
		if userID != nil {
			uid = *userID
		}
		// The link reached this inbox, so the address is real either way.
		if err := recordMarketingChoice(r.Context(), a.db, email, uid, subscribed, "unsubscribe_link", true); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Your choice could not be saved. Try again.")
			return
		}
		jsonOut(w, 200, map[string]any{"email": maskEmail(email), "subscribed": subscribed})
	}
}

// Ops tools.

func (a *API) opsMarketingSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	var audience, pending, unsubscribed int
	if err := a.db.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE subscribed AND confirmed_at IS NOT NULL),count(*) FILTER (WHERE subscribed AND confirmed_at IS NULL),count(*) FILTER (WHERE NOT subscribed) FROM marketing_contacts`).Scan(&audience, &pending, &unsubscribed); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The list could not be counted.")
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT b.id::text,b.subject,b.state,b.created_at,b.queued_at,b.finished_at,
		count(d.email) FILTER (WHERE d.state='sent'),count(d.email) FILTER (WHERE d.state='queued'),count(d.email) FILTER (WHERE d.state IN ('failed','skipped'))
		FROM broadcasts b LEFT JOIN broadcast_deliveries d ON d.broadcast_id=b.id GROUP BY b.id ORDER BY b.created_at DESC LIMIT 50`)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Announcements could not be loaded.")
		return
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id, subject, state string
		var created time.Time
		var queued, finished *time.Time
		var sent, waiting, dropped int
		if rows.Scan(&id, &subject, &state, &created, &queued, &finished, &sent, &waiting, &dropped) == nil {
			list = append(list, map[string]any{"id": id, "subject": subject, "state": state, "created_at": created, "queued_at": queued, "finished_at": finished, "sent": sent, "waiting": waiting, "not_sent": dropped})
		}
	}
	jsonOut(w, 200, map[string]any{"audience": audience, "pending_confirmation": pending, "unsubscribed": unsubscribed, "broadcasts": list, "wording": marketingWording})
}

// opsMarketingExport downloads everyone who can be emailed, for a mail tool
// such as Brevo or Mailchimp. Each row carries its own unsubscribe link.
func (a *API) opsMarketingExport(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:marketing:send")
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT mc.email,COALESCE(u.display_name,''),mc.source,mc.consented_at,mc.unsubscribe_token FROM marketing_contacts mc LEFT JOIN users u ON u.id=mc.user_id WHERE mc.subscribed AND mc.confirmed_at IS NOT NULL ORDER BY mc.consented_at`)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The list could not be exported.")
		return
	}
	defer rows.Close()
	type contact struct {
		email, name, source, token string
		consented                  *time.Time
	}
	var contacts []contact
	for rows.Next() {
		var c contact
		if err = rows.Scan(&c.email, &c.name, &c.source, &c.consented, &c.token); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The list could not be exported.")
			return
		}
		contacts = append(contacts, c)
	}
	if _, err = a.db.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,reason,safe_summary) VALUES(gen_random_uuid(),$1,'marketing.exported','Downloaded announcement list',jsonb_build_object('contacts',$2::int))`, actor.ID, len(contacts)); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The list could not be exported.")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="wantmytime-announcement-list.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	out := csv.NewWriter(w)
	_ = out.Write([]string{"email", "name", "source", "consented_at", "unsubscribe_url"})
	for _, c := range contacts {
		consented := ""
		if c.consented != nil {
			consented = c.consented.UTC().Format(time.RFC3339)
		}
		_ = out.Write([]string{c.email, csvSafe(c.name), c.source, consented, unsubscribeURL(c.token)})
	}
	out.Flush()
}

// csvSafe stops a name being read as a spreadsheet formula when opened.
func csvSafe(v string) string {
	if v != "" && strings.ContainsAny(v[:1], "=+-@\t\r") {
		return "'" + v
	}
	return v
}

func unsubscribeURL(token string) string {
	return appOrigin() + "/unsubscribe?token=" + url.QueryEscape(token)
}

func oneClickUnsubscribeURL(token string) string {
	return appOrigin() + "/api/v1/marketing/one-click?token=" + url.QueryEscape(token)
}

// opsMarketingImportUnsubscribes brings unsubscribes from a mail tool back,
// so nobody who left there is emailed from here.
func (a *API) opsMarketingImportUnsubscribes(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:marketing:send")
	if !ok {
		return
	}
	var in struct {
		Emails []string `json:"emails"`
	}
	if decode(r, &in) != nil || len(in.Emails) == 0 || len(in.Emails) > 50000 {
		problem(w, 400, "INVALID_BODY", "Send between 1 and 50,000 email addresses.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The unsubscribes could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	changed, unknown := 0, 0
	for _, raw := range in.Emails {
		email := strings.ToLower(strings.TrimSpace(raw))
		if !validEmail(email) {
			unknown++
			continue
		}
		var subscribed bool
		if tx.QueryRow(r.Context(), `SELECT subscribed FROM marketing_contacts WHERE email=$1`, email).Scan(&subscribed) != nil {
			unknown++
			continue
		}
		if !subscribed {
			continue
		}
		if err = recordMarketingChoice(r.Context(), tx, email, "", false, "ops_import", false); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The unsubscribes could not be saved.")
			return
		}
		changed++
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,reason,safe_summary) VALUES(gen_random_uuid(),$1,'marketing.unsubscribes_imported','Imported unsubscribes from a mail tool',jsonb_build_object('unsubscribed',$2::int,'not_found',$3::int))`, actor.ID, changed, unknown); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The unsubscribes could not be saved.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The unsubscribes could not be saved.")
		return
	}
	jsonOut(w, 200, map[string]any{"unsubscribed": changed, "not_found": unknown})
}

type broadcastInput struct {
	Subject     string `json:"subject"`
	Heading     string `json:"heading"`
	Body        string `json:"body"`
	ActionLabel string `json:"action_label"`
	ActionURL   string `json:"action_url"`
	ImageURL    string `json:"image_url"`
}

func (in *broadcastInput) validate() string {
	in.Subject, in.Heading, in.Body = strings.TrimSpace(in.Subject), strings.TrimSpace(in.Heading), strings.TrimSpace(in.Body)
	in.ActionLabel, in.ActionURL, in.ImageURL = strings.TrimSpace(in.ActionLabel), strings.TrimSpace(in.ActionURL), strings.TrimSpace(in.ImageURL)
	if in.Subject == "" || len(in.Subject) > 150 || strings.ContainsAny(in.Subject, "\r\n") {
		return "Write a one-line subject of up to 150 characters."
	}
	if in.Heading == "" || len(in.Heading) > 150 {
		return "Write a heading of up to 150 characters."
	}
	if in.Body == "" || len(in.Body) > 20000 {
		return "Write the message (up to 20,000 characters)."
	}
	if (in.ActionLabel == "") != (in.ActionURL == "") {
		return "A button needs both a label and a link."
	}
	if in.ActionURL != "" {
		if u, err := url.Parse(in.ActionURL); err != nil || u.Scheme != "https" || u.Host == "" {
			return "The button link must be a full https:// address."
		}
	}
	if in.ImageURL != "" {
		if u, err := url.Parse(in.ImageURL); err != nil || u.Scheme != "https" || u.Host == "" || len(in.ImageURL) > 2000 {
			return "The image must be a full https:// address."
		}
	}
	return ""
}

func (a *API) opsCreateBroadcast(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:marketing:send")
	if !ok {
		return
	}
	var in broadcastInput
	if decode(r, &in) != nil {
		problem(w, 400, "INVALID_BODY", "Check the announcement.")
		return
	}
	if msg := in.validate(); msg != "" {
		problem(w, 422, "INVALID_BROADCAST", msg)
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(), `INSERT INTO broadcasts(subject,heading,body,action_label,action_url,image_url,created_by) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7) RETURNING id::text`, in.Subject, in.Heading, in.Body, in.ActionLabel, in.ActionURL, in.ImageURL, actor.ID).Scan(&id); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be saved.")
		return
	}
	jsonOut(w, 201, map[string]any{"id": id})
}

type broadcast struct {
	id, subject, heading, body, state string
	actionLabel, actionURL, imageURL  *string
}

func (a *API) loadBroadcast(ctx context.Context, db dbExecQuerier, id string, lock bool) (broadcast, error) {
	var b broadcast
	q := `SELECT id::text,subject,heading,body,action_label,action_url,image_url,state FROM broadcasts WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	err := db.QueryRow(ctx, q, id).Scan(&b.id, &b.subject, &b.heading, &b.body, &b.actionLabel, &b.actionURL, &b.imageURL, &b.state)
	return b, err
}

// broadcastMessage renders an announcement for one person, with their own
// unsubscribe link in the footer and in the List-Unsubscribe headers.
func broadcastMessage(b broadcast, to, token string) emailMessage {
	paragraphs := []string{}
	for _, p := range strings.Split(strings.ReplaceAll(b.body, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			paragraphs = append(paragraphs, p)
		}
	}
	content := emailContent{
		Subject:     b.subject,
		Preheader:   b.heading,
		Heading:     b.heading,
		Paragraphs:  paragraphs,
		Footer:      "You’re getting this because you asked for our news.",
		Unsubscribe: &emailLink{Label: "Unsubscribe", URL: unsubscribeURL(token)},
	}
	if b.actionLabel != nil && b.actionURL != nil {
		content.Action = &emailLink{Label: *b.actionLabel, URL: *b.actionURL}
	}
	if b.imageURL != nil {
		content.Image = &emailImage{URL: *b.imageURL, Alt: b.heading}
	}
	msg := content.message(to, "broadcast:"+b.id+":"+to)
	msg.From = os.Getenv("EMAIL_MARKETING_FROM")
	msg.Headers = map[string]string{
		"List-Unsubscribe":      "<" + oneClickUnsubscribeURL(token) + ">",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
	return msg
}

// opsTestBroadcast sends the announcement only to the operator, as people will see it.
func (a *API) opsTestBroadcast(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:marketing:send")
	if !ok {
		return
	}
	b, err := a.loadBroadcast(r.Context(), a.db, r.PathValue("id"), false)
	if err != nil {
		problem(w, 404, "NOT_FOUND", "This announcement was not found.")
		return
	}
	if !a.deliverEmail(r.Context(), broadcastMessage(b, actor.Email, "test")) {
		problem(w, 503, "EMAIL_UNAVAILABLE", "The test email could not be sent.")
		return
	}
	jsonOut(w, 200, map[string]any{"sent_to": actor.Email})
}

// opsSendBroadcast queues the announcement for everyone on the list. The
// operator confirms the number of people, so a stale page can't send it to
// more people than they saw.
func (a *API) opsSendBroadcast(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:marketing:send")
	if !ok {
		return
	}
	var in struct {
		ConfirmAudience int `json:"confirm_audience"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "INVALID_BODY", "Confirm how many people this goes to.")
		return
	}
	if !a.emailConfigured() {
		problem(w, 503, "EMAIL_UNAVAILABLE", "Email delivery is not configured.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be sent.")
		return
	}
	defer tx.Rollback(r.Context())
	b, err := a.loadBroadcast(r.Context(), tx, r.PathValue("id"), true)
	if err != nil {
		problem(w, 404, "NOT_FOUND", "This announcement was not found.")
		return
	}
	if b.state != "draft" {
		problem(w, 409, "ALREADY_SENT", "This announcement was already sent or cancelled.")
		return
	}
	var audience int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM marketing_contacts WHERE subscribed AND confirmed_at IS NOT NULL`).Scan(&audience); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be sent.")
		return
	}
	if audience == 0 || in.ConfirmAudience != audience {
		problem(w, 409, "AUDIENCE_CHANGED", "The list is now "+strconv.Itoa(audience)+" people. Check the number and confirm again.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO broadcast_deliveries(broadcast_id,email) SELECT $1,email FROM marketing_contacts WHERE subscribed AND confirmed_at IS NOT NULL`, b.id); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be sent.")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE broadcasts SET state='sending',queued_at=now() WHERE id=$1`, b.id); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be sent.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'marketing.broadcast_sent',$2,$3,jsonb_build_object('audience',$4::int))`, actor.ID, b.id, b.subject, audience); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be sent.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be sent.")
		return
	}
	jsonOut(w, 200, map[string]any{"queued": audience})
}

func (a *API) opsCancelBroadcast(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:marketing:send")
	if !ok {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be stopped.")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE broadcasts SET state='cancelled',finished_at=now() WHERE id=$1 AND state IN ('draft','sending')`, r.PathValue("id"))
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be stopped.")
		return
	}
	if tag.RowsAffected() == 0 {
		problem(w, 409, "NOT_CANCELLABLE", "This announcement already finished.")
		return
	}
	var stopped int64
	if tag, err = tx.Exec(r.Context(), `UPDATE broadcast_deliveries SET state='skipped' WHERE broadcast_id=$1 AND state='queued'`, r.PathValue("id")); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be stopped.")
		return
	}
	stopped = tag.RowsAffected()
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'marketing.broadcast_cancelled',$2,'Stopped an announcement',jsonb_build_object('not_sent',$3::int))`, actor.ID, r.PathValue("id"), stopped); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be stopped.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The announcement could not be stopped.")
		return
	}
	jsonOut(w, 200, map[string]any{"not_sent": stopped})
}

// Sending, a batch at a time, so a large list neither floods the provider
// nor delays booking emails.

func broadcastBatchSize() int {
	perMinute, err := strconv.Atoi(os.Getenv("MARKETING_SEND_PER_MINUTE"))
	if err != nil || perMinute <= 0 {
		perMinute = 60
	}
	return max(1, perMinute/3) // the worker runs every 20 seconds
}

func (a *API) runBroadcastWorker(ctx context.Context) {
	for {
		err := a.processBroadcasts(ctx, broadcastBatchSize())
		if err != nil && !errors.Is(err, context.Canceled) {
			a.log().ErrorContext(ctx, "announcement worker cycle failed", "error", err.Error())
		}
		a.beat(ctx, "broadcasts", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Second):
		}
	}
}

func (a *API) processBroadcasts(ctx context.Context, limit int) error {
	for sent := 0; sent < limit; sent++ {
		done, err := a.sendOneBroadcastEmail(ctx)
		if err != nil || done {
			return err
		}
	}
	return nil
}

// sendOneBroadcastEmail sends the next queued announcement email. It reports
// done when nothing is waiting.
func (a *API) sendOneBroadcastEmail(ctx context.Context) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var broadcastID, email string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT d.broadcast_id::text,d.email,d.attempts FROM broadcast_deliveries d JOIN broadcasts b ON b.id=d.broadcast_id WHERE d.state='queued' AND b.state='sending' ORDER BY b.queued_at,d.email LIMIT 1 FOR UPDATE OF d SKIP LOCKED`).Scan(&broadcastID, &email, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		// Nothing queued: close announcements that have finished.
		_, err = tx.Exec(ctx, `UPDATE broadcasts b SET state='sent',finished_at=now() WHERE b.state='sending' AND NOT EXISTS (SELECT 1 FROM broadcast_deliveries d WHERE d.broadcast_id=b.id AND d.state='queued')`)
		if err == nil {
			err = tx.Commit(ctx)
		}
		return true, err
	}
	if err != nil {
		return false, err
	}
	// People who unsubscribed after the announcement was queued are skipped.
	var token string
	if tx.QueryRow(ctx, `SELECT unsubscribe_token FROM marketing_contacts WHERE email=$1 AND subscribed AND confirmed_at IS NOT NULL`, email).Scan(&token) != nil {
		if _, err = tx.Exec(ctx, `UPDATE broadcast_deliveries SET state='skipped' WHERE broadcast_id=$1 AND email=$2`, broadcastID, email); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	b, err := a.loadBroadcast(ctx, tx, broadcastID, false)
	if err != nil {
		return false, err
	}
	if a.deliverEmail(ctx, broadcastMessage(b, email, token)) {
		_, err = tx.Exec(ctx, `UPDATE broadcast_deliveries SET state='sent',sent_at=now(),attempts=attempts+1 WHERE broadcast_id=$1 AND email=$2`, broadcastID, email)
	} else if attempts+1 >= 3 {
		_, err = tx.Exec(ctx, `UPDATE broadcast_deliveries SET state='failed',attempts=attempts+1 WHERE broadcast_id=$1 AND email=$2`, broadcastID, email)
	} else {
		// Try again on a later cycle; stop this one so a provider outage doesn't burn attempts.
		if _, err = tx.Exec(ctx, `UPDATE broadcast_deliveries SET attempts=attempts+1 WHERE broadcast_id=$1 AND email=$2`, broadcastID, email); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}
