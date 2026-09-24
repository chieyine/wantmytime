package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

const maxActiveHoldsPerBuyer = 3

// releaseExpiredHolds deactivates lapsed holds that overlap [from, to) so a
// stale, unpaid hold cannot block a time the public slot list shows as free.
func releaseExpiredHolds(ctx context.Context, tx pgx.Tx, sellerID string, from, to time.Time) error {
	return store.New(tx).ReleaseExpiredHolds(ctx, store.ReleaseExpiredHoldsParams{SellerID: sellerID, RangeFrom: from, RangeTo: to})
}

// claimBuyerHoldCapacity releases the buyer's earlier unpaid holds with the
// same seller (choosing a new time replaces the old one) and reports whether
// the buyer is still under the active-hold limit. Call it with the buyer row locked.
func claimBuyerHoldCapacity(ctx context.Context, tx pgx.Tx, buyerID, sellerID string) (bool, error) {
	q := store.New(tx)
	if err := q.ReleaseBuyerUnpaidHoldsWithSeller(ctx, store.ReleaseBuyerUnpaidHoldsWithSellerParams{BuyerUserID: buyerID, SellerID: sellerID}); err != nil {
		return false, err
	}
	active, err := q.CountBuyerActiveHolds(ctx, buyerID)
	if err != nil {
		return false, err
	}
	return active < maxActiveHoldsPerBuyer, nil
}

// checkoutHoldDuration is how long a hold lasts once provider checkout opens.
// Bank transfer and USSD payments routinely take longer than the 10 minute
// selection hold, so the hold is extended before the buyer is sent to pay.
func checkoutHoldDuration() time.Duration {
	minutes, err := strconv.Atoi(os.Getenv("CHECKOUT_HOLD_MINUTES"))
	if err != nil || minutes < 10 || minutes > 60 {
		minutes = 30
	}
	return time.Duration(minutes) * time.Minute
}

// extendCheckoutHold lengthens a still-valid hold (and any agreed offer window
// it belongs to). It returns false when the hold has already lapsed.
func extendCheckoutHold(ctx context.Context, tx pgx.Tx, quoteID string, until time.Time) (bool, error) {
	q := store.New(tx)
	offerID, err := q.ExtendQuoteHold(ctx, store.ExtendQuoteHoldParams{Until: until, ID: quoteID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	extended, err := q.ExtendSlotHold(ctx, store.ExtendSlotHoldParams{Until: until, QuoteID: quoteID})
	if err != nil {
		return false, err
	}
	if extended != 1 {
		return false, nil
	}
	if offerID != nil {
		if err = q.ExtendOfferCheckoutWindow(ctx, store.ExtendOfferCheckoutWindowParams{Until: until, ID: *offerID}); err != nil {
			return false, err
		}
	}
	return true, nil
}
