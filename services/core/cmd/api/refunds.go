package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

const refundMaxAttempts = 8

// createRefund records a refund for a booking inside the caller's
// transaction. Paid bookings are sent to the provider by the worker (or wait for
// an operator when automatic refunds are off); simulated bookings move no
// money and are recorded as not required.
func (a *API) createRefund(ctx context.Context, tx pgx.Tx, b store.LockBookingForCancellationRow, amount int64, reason string, requestedBy *string) (string, error) {
	if amount <= 0 {
		return "", nil
	}
	if amount > b.GrossMinor {
		amount = b.GrossMinor
	}
	platform, seller := refundShares(amount, b.GrossMinor, b.DeductionMinor)
	// When the seller is at fault and the buyer gets the whole price back,
	// the transfer fee they paid comes back too, at the platform's cost.
	if (reason == "seller_cancelled" || reason == "seller_no_show") && amount == b.GrossMinor && b.BuyerFeeMinor > 0 && b.PaymentState == "paid" {
		amount += b.BuyerFeeMinor
		platform += b.BuyerFeeMinor
	}
	state := "pending_approval"
	var note *string
	switch {
	case b.PaymentState == "simulated":
		state = "not_required"
		n := "Local simulator booking: no money was collected, so nothing is refunded."
		note = &n
		platform, seller = 0, amount
	case b.PaymentState != "paid" || b.PaymentAttemptID == "":
		return "", errors.New("booking has no verified payment to refund")
	case a.refundsEnabled():
		state = "queued"
	}
	// While the seller's payout is still held, their share of the refund
	// simply comes out of it. Once the payout has gone, it is owed back.
	liability := "receivable"
	var payoutState string
	err := tx.QueryRow(ctx, `SELECT state FROM seller_payouts WHERE booking_id=$1 FOR UPDATE`, b.BookingID).Scan(&payoutState)
	switch {
	case err == nil && (payoutState == "scheduled" || payoutState == "cancelled"):
		liability = "payable"
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return "", err
	}
	if state == "not_required" {
		liability = "payable"
	}
	return store.New(tx).CreateRefund(ctx, store.CreateRefundParams{
		BookingID: b.BookingID, PaymentAttemptID: nullableString(b.PaymentAttemptID), AmountMinor: amount, Currency: strings.TrimSpace(b.Currency),
		PlatformShareMinor: platform, SellerShareMinor: seller, Reason: reason, State: state, RequestedBy: requestedBy, Note: note, SellerLiability: liability,
	})
}

// --- worker --------------------------------------------------------------------

func (a *API) processRefunds(ctx context.Context) error {
	if !a.providerEnvironmentConfigured() {
		return nil
	}
	client, err := a.collection()
	if err != nil {
		return err
	}
	due, err := store.New(a.db).ClaimDueRefunds(ctx, 10)
	if err != nil {
		return err
	}
	for _, r := range due {
		a.advanceRefund(ctx, client, r)
	}
	return nil
}

// refundReference is the provider reference for an WantMyTime refund; the same
// reference is reused on every retry, so a refund is never sent twice.
func refundReference(id string) string { return "wmt-refund-" + strings.ReplaceAll(id, "-", "") }

// advanceRefund moves one refund forward: look it up by its reference, send
// it if the provider has never seen it, and record the outcome.
func (a *API) advanceRefund(ctx context.Context, client *koraClient, r store.ClaimDueRefundsRow) {
	q := store.New(a.db)
	retry := func(err error) {
		if providerRejected(err) || int(r.Attempts) >= refundMaxAttempts {
			_ = q.MarkRefundFailed(ctx, store.MarkRefundFailedParams{LastError: err.Error(), ID: r.ID})
			a.log().ErrorContext(ctx, "refund failed", "refund_id", r.ID, "error", err.Error())
			return
		}
		backoff := time.Duration(60<<min(int(r.Attempts), 6)) * time.Second
		_ = q.RetryRefundLater(ctx, store.RetryRefundLaterParams{RetrySeconds: int32(backoff.Seconds()), LastError: err.Error(), ID: r.ID})
	}
	if r.Reference == "" {
		_ = q.MarkRefundFailed(ctx, store.MarkRefundFailedParams{LastError: "no provider payment for this booking", ID: r.ID})
		return
	}
	reference := refundReference(r.ID)
	refund, found, err := client.queryRefund(ctx, reference)
	if err != nil {
		retry(err)
		return
	}
	if !found {
		if r.State != "queued" {
			// Submitted but unknown to the provider: something is wrong.
			_ = q.MarkRefundFailed(ctx, store.MarkRefundFailedParams{LastError: "The provider has no record of this refund.", ID: r.ID})
			return
		}
		refund, err = client.refund(ctx, r.Reference, reference, r.AmountMinor, "Refund for your WantMyTime booking")
		if err != nil {
			retry(err)
			return
		}
	}
	switch refund.Status {
	case "success":
		if err = q.MarkRefundSubmitted(ctx, store.MarkRefundSubmittedParams{ProviderRefundID: reference, PollSeconds: 0, ID: r.ID}); err == nil {
			err = a.finalizeRefund(ctx, r.ID, nil, nil)
		}
		if err != nil {
			a.log().ErrorContext(ctx, "refund could not be finalized", "refund_id", r.ID, "error", err.Error())
		}
	case "failed":
		_ = q.MarkRefundFailed(ctx, store.MarkRefundFailedParams{LastError: "The provider reported the refund as failed", ID: r.ID})
	default:
		// Still processing: check again later. Webhooks bring the check forward.
		_ = q.MarkRefundSubmitted(ctx, store.MarkRefundSubmittedParams{ProviderRefundID: reference, PollSeconds: 1800, ID: r.ID})
	}
}

// finalizeRefund records a completed refund: ledger journal, the seller's
// share to recover, the booking's payment state and the buyer's email. It is
// idempotent.
func (a *API) finalizeRefund(ctx context.Context, refundID string, operator *string, note *string) error {
	var unbooked bool
	if err := a.db.QueryRow(ctx, `SELECT payment_exception_id IS NOT NULL FROM refunds WHERE id=$1`, refundID).Scan(&unbooked); err != nil {
		return err
	}
	if unbooked {
		return a.finalizeExceptionRefund(ctx, refundID, operator, note)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	r, err := q.LockRefundForFinalize(ctx, refundID)
	if err != nil {
		return err
	}
	if r.State == "processed" || r.State == "not_required" {
		return tx.Commit(ctx)
	}
	if err = q.MarkRefundProcessed(ctx, store.MarkRefundProcessedParams{Note: note, ApprovedBy: operator, ID: refundID}); err != nil {
		return err
	}
	currency := strings.TrimSpace(r.Currency)
	// Money leaves the platform's balance. The platform gives back its fee
	// share. The seller's share either reduces the payout still held for them
	// or, if it was already paid, becomes a debt recovered from later payouts.
	lines := []ledgerLine{{AccountCode: "provider_receivable", Side: "credit", Amount: r.AmountMinor}}
	if r.PlatformShareMinor > 0 {
		lines = append(lines, ledgerLine{AccountCode: "platform_fee_revenue", Side: "debit", Amount: r.PlatformShareMinor})
	}
	if r.SellerShareMinor > 0 && r.SellerLiability == "payable" {
		seller := r.SellerID
		lines = append(lines, ledgerLine{AccountCode: "seller_payable", ScopeID: &seller, Side: "debit", Amount: r.SellerShareMinor})
	} else if r.SellerShareMinor > 0 {
		seller := r.SellerID
		lines = append(lines, ledgerLine{AccountCode: "seller_receivable", ScopeID: &seller, Side: "debit", Amount: r.SellerShareMinor})
		if err = q.InsertSellerRecovery(ctx, store.InsertSellerRecoveryParams{SellerID: r.SellerID, RefundID: refundID, AmountMinor: r.SellerShareMinor, Currency: currency}); err != nil {
			return err
		}
	}
	if _, err = postLedgerJournal(ctx, tx, "refund", refundID, currency, "buyer refund", lines); err != nil {
		return err
	}
	paymentState := "partially_refunded"
	if r.AmountMinor >= r.GrossMinor {
		paymentState = "refunded"
	}
	if err = q.SetBookingPaymentState(ctx, store.SetBookingPaymentStateParams{PaymentState: paymentState, BookingID: r.BookingID}); err != nil {
		return err
	}
	if err = enqueueBookingEvent(ctx, tx, r.BookingID, r.BuyerUserID, "refund_processed_buyer", refundID+":refund-processed", &refundID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// applyRecovery marks earlier debts as repaid, oldest first. Runs in the
// caller's transaction.
func applyRecovery(ctx context.Context, q *store.Queries, sellerID string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	open, err := q.LockOpenRecoveries(ctx, sellerID)
	if err != nil {
		return err
	}
	for _, rec := range open {
		if amount <= 0 {
			break
		}
		take := min(amount, int64(rec.Outstanding))
		if err = q.ApplyRecovery(ctx, store.ApplyRecoveryParams{AmountMinor: take, ID: rec.ID}); err != nil {
			return err
		}
		amount -= take
	}
	return nil
}

// --- seller view -------------------------------------------------------------

func (a *API) myRecoveries(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	s, err := store.New(a.db).SellerRecoverySummary(r.Context(), u.ID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Refund recoveries could not be loaded.")
		return
	}
	currency := "NGN"
	_ = a.db.QueryRow(r.Context(), `SELECT currency::text FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&currency)
	jsonOut(w, 200, map[string]any{"total_minor": s.TotalMinor, "recovered_minor": s.RecoveredMinor, "outstanding_minor": s.TotalMinor - s.RecoveredMinor, "max_share_bps": recoveryMaxBps(), "currency": currency})
}

// --- operations ----------------------------------------------------------------

func (a *API) opsRefunds(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT rf.id::text,COALESCE(rf.booking_id::text,''),COALESCE(sp.handle,qsp.handle,''),rf.amount_minor,rf.currency,rf.platform_share_minor,rf.seller_share_minor,rf.reason,rf.state,COALESCE(rf.provider_refund_id,''),COALESCE(rf.last_error,''),COALESCE(rf.note,''),rf.attempts,rf.created_at,rf.processed_at FROM refunds rf LEFT JOIN bookings b ON b.id=rf.booking_id LEFT JOIN seller_profiles sp ON sp.id=b.seller_id LEFT JOIN payment_attempts pa ON pa.id=rf.payment_attempt_id AND rf.booking_id IS NULL LEFT JOIN quotes q ON q.id=pa.quote_id LEFT JOIN seller_profiles qsp ON qsp.id=q.seller_id ORDER BY (rf.state IN ('pending_approval','failed')) DESC, rf.created_at DESC LIMIT 200`)
	if err != nil {
		problem(w, 503, "REFUNDS_UNAVAILABLE", "Refunds could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, booking, handle, currency, reason, state, providerID, lastError, note string
		var amount, platform, seller int64
		var attempts int16
		var created time.Time
		var processed *time.Time
		if err = rows.Scan(&id, &booking, &handle, &amount, &currency, &platform, &seller, &reason, &state, &providerID, &lastError, &note, &attempts, &created, &processed); err != nil {
			problem(w, 503, "REFUNDS_UNAVAILABLE", "Refunds could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "booking_id": booking, "seller": handle, "amount_minor": amount, "currency": strings.TrimSpace(currency), "platform_share_minor": platform, "seller_share_minor": seller, "reason": reason, "state": state, "provider_refund_id": providerID, "last_error": lastError, "note": note, "attempts": attempts, "created_at": created, "processed_at": processed})
	}
	if rows.Err() != nil {
		problem(w, 503, "REFUNDS_UNAVAILABLE", "Refunds could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"refunds": items, "automatic_refunds": a.refundsEnabled()})
}

// opsRefundAction approves, retries or records an external refund. Every
// action needs a reason and is audited.
func (a *API) opsRefundAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := a.requireOps(w, r, "ops:refund:approve")
		if !ok {
			return
		}
		var in struct {
			Reason string `json:"reason"`
		}
		if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 500 {
			problem(w, 422, "REASON_REQUIRED", "Describe the evidence for this refund action.")
			return
		}
		id := r.PathValue("id")
		if action == "approve" && !a.refundsEnabled() {
			problem(w, 409, "AUTOMATIC_REFUNDS_OFF", "Automatic refunds are off. Refund in the Kora dashboard, then record it as processed.")
			return
		}
		tx, err := a.db.Begin(r.Context())
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The refund could not be updated.")
			return
		}
		defer tx.Rollback(r.Context())
		var sql string
		switch action {
		case "approve":
			sql = `UPDATE refunds SET state='queued',approved_by=$2,next_attempt_at=now(),updated_at=now() WHERE id=$1 AND state='pending_approval'`
		case "retry":
			sql = `UPDATE refunds SET state='queued',attempts=0,last_error=NULL,next_attempt_at=now(),updated_at=now(),approved_by=$2 WHERE id=$1 AND state='failed' AND provider_refund_id IS NULL`
		case "record":
			// Only refunds WantMyTime has not sent (or that the provider rejected) can be
			// recorded as done elsewhere; otherwise the buyer could be paid twice.
			sql = `UPDATE refunds SET state='submitted',approved_by=$2,updated_at=now() WHERE id=$1 AND state IN ('pending_approval','failed')`
		}
		tag, err := tx.Exec(r.Context(), sql, id, actor.ID)
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The refund could not be updated.")
			return
		}
		if tag.RowsAffected() != 1 {
			problem(w, 409, "REFUND_STATE_CHANGED", "This refund is not in a state that allows that action. Refresh and try again.")
			return
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,$2,$3,$4,'{}')`, actor.ID, "refund."+action, id, strings.TrimSpace(in.Reason)); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The refund could not be updated.")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The refund could not be updated.")
			return
		}
		if action == "record" {
			note := "Recorded by an operator as refunded outside WantMyTime: " + strings.TrimSpace(in.Reason)
			if err = a.finalizeRefund(r.Context(), id, &actor.ID, &note); err != nil {
				problem(w, 503, "DATABASE_ERROR", "The refund was marked but could not be finalized. Try again.")
				return
			}
		}
		jsonOut(w, 200, map[string]string{"id": id, "action": action})
	}
}

// opsCreateRefund lets an operator refund a paid booking outside the policy
// (for example after reviewing a complaint).
func (a *API) opsCreateRefund(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:refund:approve")
	if !ok {
		return
	}
	var in struct {
		AmountMinor int64  `json:"amount_minor"`
		Reason      string `json:"reason"`
	}
	if decode(r, &in) != nil || in.AmountMinor <= 0 || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 500 {
		problem(w, 422, "INVALID_REFUND", "Enter an amount and the reason for this refund.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The refund could not be created.")
		return
	}
	defer tx.Rollback(r.Context())
	b, err := store.New(tx).LockBookingForCancellation(r.Context(), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking was not found.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The refund could not be created.")
		return
	}
	if in.AmountMinor > b.GrossMinor {
		problem(w, 422, "REFUND_TOO_LARGE", "A refund cannot be more than the amount paid.")
		return
	}
	refundID, err := a.createRefund(r.Context(), tx, b, in.AmountMinor, "operator", &actor.ID)
	if err != nil {
		if strings.Contains(err.Error(), "no verified payment") {
			problem(w, 409, "NOT_REFUNDABLE", "This booking has no verified payment to refund.")
			return
		}
		if pgErrCode(err) == "23505" {
			problem(w, 409, "REFUND_EXISTS", "This booking already has a refund.")
			return
		}
		problem(w, 503, "DATABASE_ERROR", "The refund could not be created.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'refund.created',$2,$3,jsonb_build_object('amount_minor',$4::bigint))`, actor.ID, refundID, strings.TrimSpace(in.Reason), in.AmountMinor); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The refund could not be created.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The refund could not be created.")
		return
	}
	jsonOut(w, 201, map[string]string{"id": refundID})
}
