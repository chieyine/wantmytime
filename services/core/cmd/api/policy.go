package main

import (
	"os"
	"strconv"
	"time"
)

// The cancellation rule, the same for every booking. Seller cancellations
// and seller no-shows always refund the buyer in full; this rule only decides
// what a buyer gets back when they cancel.
type cancellationPolicy struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

var cancellationRule = cancellationPolicy{Name: "Flexible", Summary: "Full refund if cancelled at least 24 hours before the start."}

// refundNotice: cancelling at least this long before the start refunds in full.
const refundNotice = 24 * time.Hour

// buyerRefundPercent is the share of the price a buyer gets back when they
// cancel at now.
func buyerRefundPercent(starts, now time.Time) int64 {
	if starts.Sub(now) >= refundNotice {
		return 100
	}
	return 0
}

// nextRefundDrop returns when the buyer's full refund ends, so the interface
// can say "full refund until …". Zero means there is no refund left to lose.
func nextRefundDrop(starts, now time.Time) time.Time {
	if buyerRefundPercent(starts, now) == 0 {
		return time.Time{}
	}
	return starts.Add(-refundNotice)
}

// refundShares splits a refund between the platform fee and the seller's
// share in the same proportion as the original charge.
func refundShares(amount, gross, deduction int64) (platform, seller int64) {
	if gross <= 0 || amount <= 0 {
		return 0, 0
	}
	platform = (amount/gross)*deduction + ((amount%gross)*deduction)/gross
	return platform, amount - platform
}

// refundsEnabled: refunds are sent to Kora automatically. Otherwise each
// refund waits for an operator, who can approve it or record that it was
// made in the Kora dashboard.
func (a *API) refundsEnabled() bool {
	// On by default: refunds go to Kora by themselves. REFUNDS_ENABLED=false
	// holds each one for approval in Operations instead.
	return os.Getenv("REFUNDS_ENABLED") != "false" && a.providerEnvironmentConfigured()
}

// recoveryMaxBps caps how much of a seller's share on one booking goes to
// repaying earlier refunds (default half).
func recoveryMaxBps() int64 {
	v, err := strconv.ParseInt(os.Getenv("REFUND_RECOVERY_MAX_BPS"), 10, 64)
	if err != nil || v < 0 || v > 10000 {
		return 5000
	}
	return v
}
