package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *API) mySettlements(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT si.id::text,b.id::text,sp.handle,b.starts_at,b.currency,pa.gross_minor,pa.deduction_minor,pa.seller_entitlement_minor,pa.settlement_route,si.state,si.provider_reference,sc.confirmed_at FROM settlement_items si JOIN payment_allocations pa ON pa.id=si.allocation_id JOIN bookings b ON b.id=pa.booking_id JOIN seller_profiles sp ON sp.id=b.seller_id LEFT JOIN LATERAL (SELECT max(confirmed_at) confirmed_at FROM settlement_confirmations WHERE settlement_item_id=si.id AND reconciliation_state='matched') sc ON true WHERE sp.user_id=$1 AND ($2::timestamptz IS NULL OR (b.starts_at,si.id)<($2,$3::uuid)) ORDER BY b.starts_at DESC,si.id DESC LIMIT 100`, u.ID, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Settlement records could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, bid, handle, currency, route, state string
		var reference *string
		var starts time.Time
		var gross, deduction, entitlement int64
		var settled *time.Time
		if err := rows.Scan(&id, &bid, &handle, &starts, &currency, &gross, &deduction, &entitlement, &route, &state, &reference, &settled); err != nil {
			problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Settlement records could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "booking_id": bid, "seller": handle, "starts_at": starts, "currency": currency, "gross_minor": gross, "deduction_minor": deduction, "seller_entitlement_minor": entitlement, "route": route, "state": state, "provider_reference": reference, "settled_at": settled})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Settlement records could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"settlements": items, "next_cursor": nextListCursor(items, "starts_at", "id"), "payment_collection_enabled": a.providerCheckoutConfigured()})
}

func (a *API) mySettlementDetail(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var out map[string]any
	var id, bid, handle, currency, route, state string
	var reference *string
	var starts time.Time
	var gross, deduction, entitlement int64
	var settled *time.Time
	err := a.db.QueryRow(r.Context(), `SELECT si.id::text,b.id::text,sp.handle,b.starts_at,b.currency,pa.gross_minor,pa.deduction_minor,pa.seller_entitlement_minor,pa.settlement_route,si.state,si.provider_reference,(SELECT max(confirmed_at) FROM settlement_confirmations WHERE settlement_item_id=si.id AND reconciliation_state='matched') FROM settlement_items si JOIN payment_allocations pa ON pa.id=si.allocation_id JOIN bookings b ON b.id=pa.booking_id JOIN seller_profiles sp ON sp.id=b.seller_id WHERE si.id=$1 AND sp.user_id=$2`, r.PathValue("id"), u.ID).Scan(&id, &bid, &handle, &starts, &currency, &gross, &deduction, &entitlement, &route, &state, &reference, &settled)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This settlement record is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Settlement detail could not be loaded.")
		return
	}
	out = map[string]any{"id": id, "booking_id": bid, "seller": handle, "starts_at": starts, "currency": currency, "gross_minor": gross, "deduction_minor": deduction, "seller_entitlement_minor": entitlement, "route": route, "state": state, "provider_reference": reference, "settled_at": settled}
	jsonOut(w, 200, out)
}

func (a *API) opsPayments(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT p.id::text,p.booking_id::text,p.provider,p.environment,p.merchant_reference,p.expected_minor,p.currency,p.canonical_state,p.created_at,p.last_verified_at FROM payment_attempts p WHERE $1::timestamptz IS NULL OR (p.created_at,p.id)<($1,$2::uuid) ORDER BY p.created_at DESC,p.id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "PAYMENTS_UNAVAILABLE", "Payment attempts could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, provider, environment, reference, currency, state string
		var bid *string
		var amount int64
		var created time.Time
		var verified *time.Time
		if err := rows.Scan(&id, &bid, &provider, &environment, &reference, &amount, &currency, &state, &created, &verified); err != nil {
			problem(w, 503, "PAYMENTS_UNAVAILABLE", "Payment attempts could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "booking_id": bid, "provider": provider, "environment": environment, "reference": reference, "amount_minor": amount, "currency": currency, "state": state, "created_at": created, "last_verified_at": verified})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "PAYMENTS_UNAVAILABLE", "Payment attempts could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"payments": items, "next_cursor": nextListCursor(items, "created_at", "id"), "collection_enabled": a.providerCheckoutConfigured()})
}

func (a *API) opsProviderEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id::text,COALESCE(event_type,''),COALESCE(provider_reference,''),state,processing_attempts,last_error_code,received_at FROM provider_events WHERE $1::timestamptz IS NULL OR (received_at,id)<($1,$2::uuid) ORDER BY received_at DESC,id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "PROVIDER_EVENTS_UNAVAILABLE", "Provider events could not be loaded.")
		return
	}
	defer rows.Close()
	events := []map[string]any{}
	for rows.Next() {
		var id, eventType, reference, state string
		var attempts int
		var errorCode *string
		var received time.Time
		if err := rows.Scan(&id, &eventType, &reference, &state, &attempts, &errorCode, &received); err != nil {
			problem(w, 503, "PROVIDER_EVENTS_UNAVAILABLE", "Provider events could not be read.")
			return
		}
		events = append(events, map[string]any{"id": id, "event_type": eventType, "reference": reference, "state": state, "attempts": attempts, "last_error_code": errorCode, "received_at": received})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "PROVIDER_EVENTS_UNAVAILABLE", "Provider events could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"events": events, "next_cursor": nextListCursor(events, "received_at", "id")})
}

func (a *API) opsProviderCases(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT c.id::text,c.case_type,c.state,c.provider_case_id,c.provider_reference,c.payment_attempt_id::text,c.amount_minor,c.currency,c.provider_deadline,c.last_event_type,c.updated_at FROM provider_cases c WHERE c.state<>'resolved' AND ($1::timestamptz IS NULL OR (c.updated_at,c.id)<($1,$2::uuid)) ORDER BY c.updated_at DESC,c.id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "PROVIDER_CASES_UNAVAILABLE", "Provider cases could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, kind, state, caseID, reference, lastEvent string
		var attemptID, currency *string
		var amount *int64
		var deadline *time.Time
		var updated time.Time
		if err = rows.Scan(&id, &kind, &state, &caseID, &reference, &attemptID, &amount, &currency, &deadline, &lastEvent, &updated); err != nil {
			problem(w, 503, "PROVIDER_CASES_UNAVAILABLE", "Provider cases could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "type": kind, "state": state, "provider_case_id": caseID, "provider_reference": reference, "payment_attempt_id": attemptID, "amount_minor": amount, "currency": currency, "provider_deadline": deadline, "last_event_type": lastEvent, "updated_at": updated})
	}
	if err = rows.Err(); err != nil {
		problem(w, 503, "PROVIDER_CASES_UNAVAILABLE", "Provider cases could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"cases": items, "next_cursor": nextListCursor(items, "updated_at", "id"), "financial_actions_enabled": false, "notice": "Provider case updates are recorded for review. Refunds and dispute responses are not submitted from this system."})
}

func (a *API) opsRetryProviderEvent(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:booking:resolve")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 500 {
		problem(w, 422, "REASON_REQUIRED", "Record why this provider event is being retried.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Provider event retry could not be recorded.")
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	tag, err := tx.Exec(r.Context(), `UPDATE provider_events SET state='queued',processing_attempts=0,last_error_code=NULL,next_attempt_at=now(),claimed_at=NULL WHERE id=$1 AND state='failed'`, id)
	if err != nil || tag.RowsAffected() != 1 {
		problem(w, 409, "PROVIDER_EVENT_NOT_RETRYABLE", "Only a failed provider event can be retried.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'provider_event.retry',$2,$3,'{}')`, actor.ID, id, strings.TrimSpace(in.Reason)); err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "The retry could not be audited.")
		return
	}
	if tx.Commit(r.Context()) != nil {
		problem(w, 503, "DATABASE_ERROR", "Provider event retry could not be committed.")
		return
	}
	jsonOut(w, 200, map[string]bool{"queued": true})
}

func (a *API) opsPaymentDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	var id, provider, environment, reference, currency, state string
	var bid *string
	var amount int64
	var created time.Time
	var verified *time.Time
	var allocationID, route, settlementState *string
	var gross, deduction, entitlement, processorCost *int64
	var cardCountry, cardBrand *string
	err := a.db.QueryRow(r.Context(), `SELECT p.id::text,p.booking_id::text,p.provider,p.environment,p.merchant_reference,p.expected_minor,p.currency,p.canonical_state,p.created_at,p.last_verified_at,pa.id::text,pa.settlement_route,pa.gross_minor,pa.deduction_minor,pa.seller_entitlement_minor,si.state,pa.processor_cost_minor,p.card_country,p.card_brand FROM payment_attempts p LEFT JOIN payment_allocations pa ON pa.booking_id=p.booking_id LEFT JOIN settlement_items si ON si.allocation_id=pa.id WHERE p.id=$1`, r.PathValue("id")).Scan(&id, &bid, &provider, &environment, &reference, &amount, &currency, &state, &created, &verified, &allocationID, &route, &gross, &deduction, &entitlement, &settlementState, &processorCost, &cardCountry, &cardBrand)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This payment attempt was not found.")
		return
	}
	if err != nil {
		problem(w, 503, "PAYMENTS_UNAVAILABLE", "Payment detail could not be loaded.")
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "booking_id": bid, "provider": provider, "environment": environment, "reference": reference, "amount_minor": amount, "currency": currency, "state": state, "created_at": created, "last_verified_at": verified, "allocation_id": allocationID, "route": route, "gross_minor": gross, "deduction_minor": deduction, "seller_entitlement_minor": entitlement, "settlement_state": settlementState, "processor_cost_minor": processorCost, "card_country": cardCountry, "card_brand": cardBrand, "collection_enabled": a.providerCheckoutConfigured()})
}

func (a *API) opsSettlements(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT si.id::text,si.allocation_id::text,b.id::text,sp.handle,si.route,si.state,si.provider_reference,pa.seller_entitlement_minor,b.currency,b.starts_at,(SELECT max(confirmed_at) FROM settlement_confirmations WHERE settlement_item_id=si.id AND reconciliation_state='matched') FROM settlement_items si JOIN payment_allocations pa ON pa.id=si.allocation_id JOIN bookings b ON b.id=pa.booking_id JOIN seller_profiles sp ON sp.id=b.seller_id WHERE $1::timestamptz IS NULL OR (b.starts_at,si.id)<($1,$2::uuid) ORDER BY b.starts_at DESC,si.id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Settlement queue could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, allocation, bid, handle, route, state, currency string
		var reference *string
		var amount int64
		var starts time.Time
		var settled *time.Time
		if err := rows.Scan(&id, &allocation, &bid, &handle, &route, &state, &reference, &amount, &currency, &starts, &settled); err != nil {
			problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Settlement queue could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "allocation_id": allocation, "booking_id": bid, "seller": handle, "route": route, "state": state, "provider_reference": reference, "amount_minor": amount, "currency": currency, "starts_at": starts, "settled_at": settled})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "SETTLEMENTS_UNAVAILABLE", "Settlement queue could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"settlements": items, "next_cursor": nextListCursor(items, "starts_at", "id")})
}

func (a *API) opsPaymentExceptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id::text,kind,state,booking_id::text,payment_attempt_id::text,provider_case_reference,provider_deadline,amount_minor,currency,reason,created_at FROM payment_exceptions WHERE state<>'resolved' AND ($1::timestamptz IS NULL OR (created_at,id)<($1,$2::uuid)) ORDER BY created_at DESC,id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "EXCEPTIONS_UNAVAILABLE", "Payment exceptions could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, kind, state, reason string
		var bookingID, attemptID, caseRef, currency *string
		var deadline, created *time.Time
		var amount *int64
		if err := rows.Scan(&id, &kind, &state, &bookingID, &attemptID, &caseRef, &deadline, &amount, &currency, &reason, &created); err != nil {
			problem(w, 503, "EXCEPTIONS_UNAVAILABLE", "Payment exceptions could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "kind": kind, "state": state, "booking_id": bookingID, "payment_attempt_id": attemptID, "provider_case_reference": caseRef, "provider_deadline": deadline, "amount_minor": amount, "currency": currency, "reason": reason, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "EXCEPTIONS_UNAVAILABLE", "Payment exceptions could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"exceptions": items, "next_cursor": nextListCursor(items, "created_at", "id")})
}

func (a *API) opsPaymentExceptionDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	var id, kind, state, reason string
	var bookingID, attemptID, caseRef, currency, resolution *string
	var deadline, resolved, created *time.Time
	var amount *int64
	var evidence []byte
	err := a.db.QueryRow(r.Context(), `SELECT id::text,kind,state,booking_id::text,payment_attempt_id::text,provider_case_reference,provider_deadline,amount_minor,currency,reason,evidence,resolution,created_at,resolved_at FROM payment_exceptions WHERE id=$1`, r.PathValue("id")).Scan(&id, &kind, &state, &bookingID, &attemptID, &caseRef, &deadline, &amount, &currency, &reason, &evidence, &resolution, &created, &resolved)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This payment exception was not found.")
		return
	}
	if err != nil {
		problem(w, 503, "EXCEPTIONS_UNAVAILABLE", "Exception detail could not be loaded.")
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "kind": kind, "state": state, "booking_id": bookingID, "payment_attempt_id": attemptID, "provider_case_reference": caseRef, "provider_deadline": deadline, "amount_minor": amount, "currency": currency, "reason": reason, "evidence": jsonRaw(evidence), "resolution": resolution, "created_at": created, "resolved_at": resolved})
}

// opsExportPaymentException returns a compact JSON evidence bundle. Provider
// webhook payloads are already minimized before persistence; this export never
// includes raw callback bodies, credentials, buyer contact data, or meeting URLs.
func (a *API) opsExportPaymentException(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:read")
	if !ok {
		return
	}
	id := r.PathValue("id")
	var kind, state, reason string
	var bookingID, attemptID, caseRef, currency *string
	var deadline, created, resolved *time.Time
	var amount *int64
	var resolution *string
	var evidence []byte
	if err := a.db.QueryRow(r.Context(), `SELECT kind,state,reason,booking_id::text,payment_attempt_id::text,provider_case_reference,provider_deadline,amount_minor,currency,evidence,resolution,created_at,resolved_at FROM payment_exceptions WHERE id=$1`, id).Scan(&kind, &state, &reason, &bookingID, &attemptID, &caseRef, &deadline, &amount, &currency, &evidence, &resolution, &created, &resolved); errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This payment exception was not found.")
		return
	} else if err != nil {
		problem(w, 503, "EXCEPTIONS_UNAVAILABLE", "Evidence could not be assembled.")
		return
	}

	attempts := []map[string]any{}
	if attemptID != nil {
		rows, err := a.db.Query(r.Context(), `SELECT id::text,provider,environment,merchant_reference,provider_transaction_id,expected_minor,currency,canonical_state,approved_fee_minor,fee_basis_points,last_verified_at,created_at FROM payment_attempts WHERE id=$1`, *attemptID)
		if err != nil {
			problem(w, 503, "EVIDENCE_UNAVAILABLE", "Payment evidence could not be loaded.")
			return
		}
		for rows.Next() {
			var attempt, provider, environment, reference, currencyValue, paymentState string
			var transactionID *string
			var expected int64
			var fee *int64
			var bps *int
			var verified *time.Time
			var attemptCreated time.Time
			if err := rows.Scan(&attempt, &provider, &environment, &reference, &transactionID, &expected, &currencyValue, &paymentState, &fee, &bps, &verified, &attemptCreated); err != nil {
				rows.Close()
				problem(w, 503, "EVIDENCE_UNAVAILABLE", "Payment evidence could not be read.")
				return
			}
			attempts = append(attempts, map[string]any{"id": attempt, "provider": provider, "environment": environment, "merchant_reference": reference, "provider_transaction_id": transactionID, "expected_minor": expected, "currency": currencyValue, "state": paymentState, "approved_fee_minor": fee, "fee_basis_points": bps, "last_verified_at": verified, "created_at": attemptCreated})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			problem(w, 503, "EVIDENCE_UNAVAILABLE", "Payment evidence could not be read.")
			return
		}
		rows.Close()
	}

	providerReference := ""
	if caseRef != nil {
		providerReference = *caseRef
	}
	if len(attempts) > 0 {
		if reference, ok := attempts[0]["merchant_reference"].(string); ok && reference != "" {
			providerReference = reference
		}
	}
	events := []map[string]any{}
	if providerReference != "" {
		rows, err := a.db.Query(r.Context(), `SELECT id::text,event_type,provider_reference,state,processing_attempts,last_error_code,minimal_payload,received_at FROM provider_events WHERE provider_reference=$1 ORDER BY received_at,id`, providerReference)
		if err != nil {
			problem(w, 503, "EVIDENCE_UNAVAILABLE", "Provider evidence could not be loaded.")
			return
		}
		for rows.Next() {
			var eventID, eventType, reference, eventState string
			var tries int
			var errorCode *string
			var payload []byte
			var received time.Time
			if err := rows.Scan(&eventID, &eventType, &reference, &eventState, &tries, &errorCode, &payload, &received); err != nil {
				rows.Close()
				problem(w, 503, "EVIDENCE_UNAVAILABLE", "Provider evidence could not be read.")
				return
			}
			events = append(events, map[string]any{"id": eventID, "event_type": eventType, "reference": reference, "state": eventState, "attempts": tries, "last_error_code": errorCode, "minimized_payload": jsonRaw(payload), "received_at": received})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			problem(w, 503, "EVIDENCE_UNAVAILABLE", "Provider evidence could not be read.")
			return
		}
		rows.Close()
	}

	bundle := map[string]any{
		"format": "aside.payment-exception-evidence.v1", "exported_at": time.Now().UTC(),
		"exception":        map[string]any{"id": id, "kind": kind, "state": state, "reason": reason, "booking_id": bookingID, "payment_attempt_id": attemptID, "provider_case_reference": caseRef, "provider_deadline": deadline, "amount_minor": amount, "currency": currency, "evidence": jsonRaw(evidence), "resolution": resolution, "created_at": created, "resolved_at": resolved},
		"payment_attempts": attempts, "provider_events": events,
		"limitations": []string{"Provider statements and external dispute documents are not attached unless separately recorded.", "Webhook data contains only the minimized fields retained by WantMyTime."},
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "Evidence export could not be audited.")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'payment.exception_evidence_exported',$2,'Exported minimized exception evidence',jsonb_build_object('provider_events',$3::int,'payment_attempts',$4::int))`, actor.ID, id, len(events), len(attempts)); err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "Evidence export could not be audited.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "Evidence export could not be audited.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="wantmytime-payment-exception-%s.json"`, id))
	if err := json.NewEncoder(w).Encode(bundle); err != nil {
		return
	}
}

func (a *API) opsResolvePaymentException(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:booking:resolve")
	if !ok {
		return
	}
	var in struct {
		Resolution string `json:"resolution"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Resolution)) < 8 || len(in.Resolution) > 500 {
		problem(w, 422, "RESOLUTION_REQUIRED", "Record what was reviewed or communicated.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The exception could not be resolved.")
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	tag, err := tx.Exec(r.Context(), `UPDATE payment_exceptions SET state='resolved',resolution=$2,resolved_at=now() WHERE id=$1 AND state<>'resolved'`, id, strings.TrimSpace(in.Resolution))
	if err != nil || tag.RowsAffected() != 1 {
		problem(w, 404, "EXCEPTION_NOT_OPEN", "This exception is not open.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'payment.exception_resolved',$2,$3,'{}')`, actor.ID, id, strings.TrimSpace(in.Resolution)); err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "Exception resolution could not be audited.")
		return
	}
	if tx.Commit(r.Context()) != nil {
		problem(w, 503, "DATABASE_ERROR", "The exception could not be resolved.")
		return
	}
	jsonOut(w, 200, map[string]bool{"resolved": true})
}

func jsonRaw(data []byte) any {
	if len(data) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(data)
}
