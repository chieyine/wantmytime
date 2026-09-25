package main

import (
	"net/http"
	"time"
)

func (a *API) opsGrowth(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	events := []map[string]any{}
	rows, err := a.db.Query(r.Context(), `SELECT event_name,count(*) FROM product_events WHERE event_day>=((now() AT TIME ZONE 'UTC')::date-6) AND environment=$1 GROUP BY event_name ORDER BY event_name`, envOr("APP_ENV", "local"))
	if err != nil {
		problem(w, 503, "GROWTH_UNAVAILABLE", "Growth events could not be loaded.")
		return
	}
	for rows.Next() {
		var name string
		var count int64
		if err = rows.Scan(&name, &count); err != nil {
			rows.Close()
			problem(w, 503, "GROWTH_UNAVAILABLE", "Growth events could not be read.")
			return
		}
		events = append(events, map[string]any{"event": name, "count": count})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		problem(w, 503, "GROWTH_UNAVAILABLE", "Growth events could not be read.")
		return
	}
	rows.Close()

	var attributed, observed30Days int64
	err = a.db.QueryRow(r.Context(), `SELECT count(DISTINCT gr.id),count(DISTINCT gr.id) FILTER(WHERE gr.attributed_at<=now()-interval '30 days')
		FROM growth_relationships gr
		JOIN bookings b ON b.id=gr.qualifying_booking_id
		JOIN payment_attempts pa ON pa.booking_id=b.id AND pa.canonical_state='success' AND pa.environment='live'
		WHERE gr.attribution_method='verified_paid_booking_and_receipt_cta'
		AND b.payment_state='paid'
		AND NOT EXISTS (SELECT 1 FROM payment_exceptions pe WHERE (pe.booking_id=b.id OR pe.payment_attempt_id=pa.id) AND pe.kind IN ('provider_dispute','provider_reversal','duplicate_charge'))
		AND NOT EXISTS (SELECT 1 FROM provider_cases pc WHERE (pc.payment_attempt_id=pa.id OR (pc.provider_reference=pa.merchant_reference AND pc.environment=pa.environment)) AND pc.case_type IN ('dispute','refund'))`).Scan(&attributed, &observed30Days)
	if err != nil {
		problem(w, 503, "GROWTH_UNAVAILABLE", "Verified attribution records could not be loaded.")
		return
	}

	cohorts := []map[string]any{}
	rows, err = a.db.Query(r.Context(), `SELECT date_trunc('month',gr.attributed_at)::date,count(DISTINCT gr.id),count(DISTINCT gr.id) FILTER(WHERE gr.attributed_at<=now()-interval '30 days')
		FROM growth_relationships gr
		JOIN bookings b ON b.id=gr.qualifying_booking_id
		JOIN payment_attempts pa ON pa.booking_id=b.id AND pa.canonical_state='success' AND pa.environment='live'
		WHERE gr.attribution_method='verified_paid_booking_and_receipt_cta' AND b.payment_state='paid'
		AND NOT EXISTS (SELECT 1 FROM payment_exceptions pe WHERE (pe.booking_id=b.id OR pe.payment_attempt_id=pa.id) AND pe.kind IN ('provider_dispute','provider_reversal','duplicate_charge'))
		AND NOT EXISTS (SELECT 1 FROM provider_cases pc WHERE (pc.payment_attempt_id=pa.id OR (pc.provider_reference=pa.merchant_reference AND pc.environment=pa.environment)) AND pc.case_type IN ('dispute','refund'))
		GROUP BY 1 ORDER BY 1 DESC LIMIT 24`)
	if err != nil {
		problem(w, 503, "GROWTH_UNAVAILABLE", "Attribution cohorts could not be loaded.")
		return
	}
	for rows.Next() {
		var month time.Time
		var total, observed int64
		if err = rows.Scan(&month, &total, &observed); err != nil {
			rows.Close()
			problem(w, 503, "GROWTH_UNAVAILABLE", "Attribution cohorts could not be read.")
			return
		}
		cohorts = append(cohorts, map[string]any{"month": month.Format("2006-01"), "attributed_relationships": total, "observed_30_days": observed})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		problem(w, 503, "GROWTH_UNAVAILABLE", "Attribution cohorts could not be read.")
		return
	}
	rows.Close()

	jsonOut(w, 200, map[string]any{
		"events_7d":                              events,
		"verified_buyer_to_seller_relationships": attributed,
		"observed_30_days":                       observed30Days,
		"cohorts":                                cohorts,
		"attribution_rule":                       "A verified live-payment buyer clicks the seller CTA on their receipt, then creates their first link within 30 days. Self-payments and non-live payments are excluded.",
		"limitations":                            []string{"Counts exclude records with a recorded dispute, reversal, or duplicate-charge exception.", "The 30-day observed count is not a claim that a payment cannot later be refunded or disputed."},
	})
}

// funnelStep is one stage of a funnel. From counts events or records; Basis
// says which, so nobody reads a click count as a person count.
type funnelStep struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
	Basis string `json:"basis"`
}

// opsFunnels returns the buyer booking funnel and the seller setup funnel for
// the last 7, 30 or 90 days. Every stage after the first page view comes from
// the records that make the booking happen (holds, checkouts, bookings), not
// from browser events, so ad blockers cannot distort them.
func (a *API) opsFunnels(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	days := 30
	switch r.URL.Query().Get("days") {
	case "7":
		days = 7
	case "90":
		days = 90
	}
	// Outside production the payment simulator stands in for real payments.
	paid := `payment_state='paid'`
	if a.env != "production" {
		paid = `payment_state IN ('paid','simulated')`
	}
	ctx := r.Context()
	since := `now()-make_interval(days=>$1)`
	env := envOr("APP_ENV", "local")
	var b [7]int64
	err := a.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM product_events WHERE event_name='public_link_viewed' AND environment=$2 AND received_at>=`+since+`),
		(SELECT count(*) FROM product_events WHERE event_name='booking_started' AND environment=$2 AND received_at>=`+since+`),
		(SELECT count(*) FROM product_events WHERE event_name='slot_selected' AND environment=$2 AND received_at>=`+since+`),
		(SELECT count(*) FROM quotes WHERE created_at>=`+since+`),
		(SELECT count(DISTINCT quote_id) FROM payment_attempts WHERE created_at>=`+since+`),
		(SELECT count(*) FROM bookings WHERE `+paid+` AND created_at>=`+since+`),
		(SELECT count(*) FROM bookings WHERE `+paid+` AND state='completed' AND created_at>=`+since+`)`, days, env).Scan(&b[0], &b[1], &b[2], &b[3], &b[4], &b[5], &b[6])
	if err != nil {
		a.log().ErrorContext(ctx, "buyer funnel failed", "error", err.Error())
		problem(w, 503, "FUNNEL_UNAVAILABLE", "The funnels could not be loaded.")
		return
	}
	buyer := []funnelStep{
		{"viewed", "Viewed a booking page", b[0], "page views"},
		{"started", "Opened the time picker", b[1], "page views"},
		{"picked", "Picked a time", b[2], "page views"},
		{"held", "Held the time", b[3], "holds"},
		{"checkout", "Opened payment", b[4], "holds with a payment started"},
		{"paid", "Paid", b[5], "bookings"},
		{"completed", "Session took place", b[6], "bookings"},
	}
	// Seller setup: sellers whose link was claimed in the period, and how far
	// each has got since.
	var s [6]int64
	err = a.db.QueryRow(ctx, `WITH cohort AS (SELECT id FROM seller_profiles WHERE created_at>=`+since+` AND publication_state<>'deleted')
		SELECT
		(SELECT count(*) FROM cohort),
		(SELECT count(*) FROM cohort c WHERE EXISTS(SELECT 1 FROM availability_windows aw WHERE aw.seller_id=c.id)),
		(SELECT count(*) FROM cohort c WHERE EXISTS(SELECT 1 FROM seller_payout_accounts pa WHERE pa.seller_id=c.id)),
		(SELECT count(*) FROM cohort c WHERE EXISTS(SELECT 1 FROM product_events pe WHERE pe.seller_id=c.id AND pe.event_name IN ('link_copy_clicked','share_action_opened'))),
		(SELECT count(*) FROM cohort c WHERE EXISTS(SELECT 1 FROM bookings bk WHERE bk.seller_id=c.id)),
		(SELECT count(*) FROM cohort c WHERE EXISTS(SELECT 1 FROM bookings bk WHERE bk.seller_id=c.id AND bk.`+paid+`))`, days).Scan(&s[0], &s[1], &s[2], &s[3], &s[4], &s[5])
	if err != nil {
		a.log().ErrorContext(ctx, "seller funnel failed", "error", err.Error())
		problem(w, 503, "FUNNEL_UNAVAILABLE", "The funnels could not be loaded.")
		return
	}
	seller := []funnelStep{
		{"claimed", "Claimed a link", s[0], "sellers"},
		{"hours", "Set their hours", s[1], "sellers"},
		{"bank", "Added a bank account", s[2], "sellers"},
		{"shared", "Copied or shared their link", s[3], "sellers"},
		{"booked", "Got a first booking", s[4], "sellers"},
		{"paid", "Got a first paid booking", s[5], "sellers"},
	}
	var medianHours *float64
	if err = a.db.QueryRow(ctx, `SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM first_paid-claimed)/3600)
		FROM (SELECT sp.created_at AS claimed,(SELECT min(bk.created_at) FROM bookings bk WHERE bk.seller_id=sp.id AND bk.`+paid+`) AS first_paid
		FROM seller_profiles sp WHERE sp.created_at>=`+since+` AND sp.publication_state<>'deleted') t WHERE first_paid IS NOT NULL`, days).Scan(&medianHours); err != nil {
		problem(w, 503, "FUNNEL_UNAVAILABLE", "The funnels could not be loaded.")
		return
	}
	jsonOut(w, 200, map[string]any{
		"days":                        days,
		"buyer":                       buyer,
		"seller":                      seller,
		"median_hours_to_first_paid":  medianHours,
		"includes_simulated_payments": a.env != "production",
		"notes": []string{
			"The first three buyer steps are counted in the browser, once per page visit. Ad blockers and closed tabs make them undercount, so a later step can be larger than an earlier one.",
			"Holds, payments and bookings come from the records themselves.",
			"Seller steps follow the sellers who claimed a link in the period, wherever they have got to since.",
		},
	})
}
