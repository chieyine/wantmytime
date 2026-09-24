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

type cancellationPreview struct {
	CanCancel     bool       `json:"can_cancel"`
	Reason        string     `json:"reason,omitempty"`
	Role          string     `json:"role"`
	RefundMinor   int64      `json:"refund_minor"`
	RefundPercent int64      `json:"refund_percent"`
	Policy        string     `json:"policy"`
	PolicyName    string     `json:"policy_name"`
	PolicySummary string     `json:"policy_summary"`
	RefundDropsAt *time.Time `json:"refund_drops_at,omitempty"`
	Paid          bool       `json:"paid"`
}

// previewCancellation works out what cancelling now would mean for the
// person asking. Sellers always refund the buyer in full.
func previewCancellation(b store.LockBookingForCancellationRow, userID string, now time.Time) cancellationPreview {
	p := policyOrDefault(b.CancellationPolicy)
	out := cancellationPreview{Policy: p.Key, PolicyName: p.Name, PolicySummary: p.Summary, Role: "buyer", Paid: b.PaymentState == "paid"}
	if userID == b.SellerUserID {
		out.Role = "seller"
	}
	switch {
	case b.State != "confirmed":
		out.Reason = "This booking can no longer be cancelled."
		return out
	case !b.StartsAt.After(now):
		out.Reason = "This booking has already started. Report a problem or a no-show instead."
		return out
	}
	out.CanCancel = true
	if out.Role == "seller" {
		out.RefundPercent = 100
	} else {
		out.RefundPercent = buyerRefundPercent(b.CancellationPolicy, b.CreatedAt, b.StartsAt, now)
		if drop := nextRefundDrop(b.CancellationPolicy, b.CreatedAt, b.StartsAt, now); !drop.IsZero() {
			out.RefundDropsAt = &drop
		}
	}
	out.RefundMinor = b.GrossMinor * out.RefundPercent / 100
	return out
}

// lockParticipantBooking loads a booking for update and checks the caller
// takes part in it.
func lockParticipantBooking(ctx context.Context, tx pgx.Tx, bookingID, userID string) (store.LockBookingForCancellationRow, error) {
	b, err := store.New(tx).LockBookingForCancellation(ctx, bookingID)
	if err != nil {
		return b, err
	}
	if b.BuyerUserID != userID && b.SellerUserID != userID {
		return b, pgx.ErrNoRows
	}
	return b, nil
}

func (a *API) cancellationPreviewHandler(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Cancellation details could not be loaded.")
		return
	}
	defer tx.Rollback(r.Context())
	b, err := lockParticipantBooking(r.Context(), tx, r.PathValue("id"), u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Cancellation details could not be loaded.")
		return
	}
	jsonOut(w, 200, previewCancellation(b, u.ID, time.Now()))
}

// cancelBooking cancels an upcoming booking straight away: the time is
// released, the calendar event removed, both people are emailed and any
// refund due under the booking's policy is started.
func (a *API) cancelBooking(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var in struct {
		Reason              string `json:"reason"`
		ExpectedRefundMinor *int64 `json:"expected_refund_minor"`
	}
	if decode(r, &in) != nil || in.ExpectedRefundMinor == nil || len(in.Reason) > 500 {
		problem(w, 422, "INVALID_CANCELLATION", "Confirm the refund shown before cancelling.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking could not be cancelled.")
		return
	}
	defer tx.Rollback(r.Context())
	b, err := lockParticipantBooking(r.Context(), tx, r.PathValue("id"), u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking could not be cancelled.")
		return
	}
	preview := previewCancellation(b, u.ID, time.Now())
	if !preview.CanCancel {
		problem(w, 409, "CANCELLATION_UNAVAILABLE", preview.Reason)
		return
	}
	// The refund can drop between showing it and confirming; never surprise
	// the buyer with a smaller amount than they agreed to.
	if *in.ExpectedRefundMinor != preview.RefundMinor {
		problem(w, 409, "REFUND_CHANGED", "The refund for cancelling has changed. Review the new amount and confirm again.")
		return
	}
	if err = a.applyCancellation(r.Context(), tx, b, preview.Role, strings.TrimSpace(in.Reason), preview.RefundMinor, &u.ID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking could not be cancelled.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking could not be cancelled.")
		return
	}
	jsonOut(w, 200, map[string]any{"state": "cancelled", "refund_minor": preview.RefundMinor})
}

// applyCancellation performs the cancellation inside the caller's transaction.
func (a *API) applyCancellation(ctx context.Context, tx pgx.Tx, b store.LockBookingForCancellationRow, role, reason string, refund int64, actor *string) error {
	q := store.New(tx)
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	if err := q.MarkBookingCancelled(ctx, store.MarkBookingCancelledParams{Role: role, Reason: reasonPtr, BookingID: b.BookingID}); err != nil {
		return err
	}
	if err := q.ReleaseBookingSlot(ctx, b.BookingID); err != nil {
		return err
	}
	if err := q.CancelUpcomingBookingMail(ctx, store.CancelUpcomingBookingMailParams{ReasonCode: "BOOKING_CANCELLED", BookingID: b.BookingID}); err != nil {
		return err
	}
	if err := q.CloseBookingRequests(ctx, store.CloseBookingRequestsParams{Resolution: "Closed because the booking was cancelled.", BookingID: b.BookingID}); err != nil {
		return err
	}
	refundReason := "buyer_cancelled"
	if role == "seller" {
		refundReason = "seller_cancelled"
	}
	var refundID *string
	if refund > 0 {
		id, err := a.createRefund(ctx, tx, b, refund, refundReason, actor)
		if err != nil {
			return err
		}
		refundID = &id
	}
	for _, recipient := range []struct{ user, kind string }{{b.BuyerUserID, "booking_cancelled_buyer"}, {b.SellerUserID, "booking_cancelled_seller"}} {
		if err := enqueueBookingEvent(ctx, tx, b.BookingID, recipient.user, recipient.kind, b.BookingID+":cancelled:"+recipient.kind, refundID); err != nil {
			return err
		}
	}
	return enqueueCalendarSync(ctx, tx, b.BookingID)
}

// setCancellationPolicy lets a seller choose the policy for future bookings.
func (a *API) setCancellationPolicy(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		Policy string `json:"policy"`
	}
	if decode(r, &in) != nil {
		problem(w, 422, "INVALID_POLICY", "Choose a cancellation policy.")
		return
	}
	p, known := cancellationPolicies[in.Policy]
	if !known {
		problem(w, 422, "INVALID_POLICY", "Choose flexible, moderate or strict.")
		return
	}
	tag, err := a.db.Exec(r.Context(), `UPDATE seller_profiles SET cancellation_policy=$2 WHERE user_id=$1`, u.ID, p.Key)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The policy could not be saved.")
		return
	}
	if tag.RowsAffected() != 1 {
		problem(w, 409, "SELLER_REQUIRED", "Claim your link before choosing a policy.")
		return
	}
	jsonOut(w, 200, p)
}

func (a *API) getCancellationPolicy(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var key string
	if err := a.db.QueryRow(r.Context(), `SELECT cancellation_policy FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&key); err != nil {
		problem(w, 404, "SELLER_REQUIRED", "Claim your link before choosing a policy.")
		return
	}
	options := []cancellationPolicy{cancellationPolicies["flexible"], cancellationPolicies["moderate"], cancellationPolicies["strict"]}
	jsonOut(w, 200, map[string]any{"policy": key, "options": options})
}
