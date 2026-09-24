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
