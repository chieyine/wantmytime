package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// opsSetPayoutReadiness is the audited operator action that makes a seller
// bookable once their identity has been checked (or takes them out of
// service). A seller must have added a verified bank account first, because
// every payment is paid out to it after the session.
func (a *API) opsSetPayoutReadiness(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:seller:approve")
	if !ok {
		return
	}
	var in struct {
		Ready  bool   `json:"ready"`
		Reason string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 300 {
		problem(w, 422, "REASON_REQUIRED", "Give a reason (at least 8 characters) for the audit log.")
		return
	}
	id := r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Payout readiness could not be updated.")
		return
	}
	defer tx.Rollback(r.Context())
	var hasAccount bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM seller_payout_accounts a WHERE a.seller_id=sp.id) FROM seller_profiles sp WHERE sp.user_id=$1 FOR UPDATE OF sp`, id).Scan(&hasAccount)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This account has not claimed a seller link.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Payout readiness could not be updated.")
		return
	}
	if in.Ready && !hasAccount {
		problem(w, 422, "PAYOUT_ACCOUNT_REQUIRED", "The seller must add a verified bank account before taking paid bookings.")
		return
	}
	// Sellers open themselves once their bank account is confirmed; an
	// operator's "not ready" is a hold that stays until an operator lifts it.
	state := "held"
	if in.Ready {
		state = "ready"
	}
	if _, err = tx.Exec(r.Context(), `UPDATE seller_profiles SET readiness_state=$2,public_version=public_version+1 WHERE user_id=$1`, id, state); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Payout readiness could not be updated.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'seller.payout_readiness_set',$2,$3,jsonb_build_object('ready',$4::boolean))`, actor.ID, id, strings.TrimSpace(in.Reason), in.Ready); err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "The change could not be audited; nothing was saved.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Payout readiness could not be updated.")
		return
	}
	jsonOut(w, 200, map[string]any{"readiness_state": state, "payout_account_added": hasAccount})
}
