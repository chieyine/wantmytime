package main

import (
	"os"
	"strconv"
	"time"
)

// Cancellation policies a seller can choose. A booking keeps the policy it
// was made under. Seller cancellations and seller no-shows always refund the
// buyer in full; these rules only decide what a buyer gets back when they
// cancel.
type refundStep struct {
	before  time.Duration // cancelled at least this long before the start...
	percent int64         // ...refunds this share of the price
}

type cancellationPolicy struct {
	Key     string       `json:"key"`
	Name    string       `json:"name"`
	Summary string       `json:"summary"`
	steps   []refundStep // longest notice first
}

var cancellationPolicies = map[string]cancellationPolicy{
	"flexible": {Key: "flexible", Name: "Flexible", Summary: "Full refund if cancelled at least 24 hours before the start.", steps: []refundStep{{24 * time.Hour, 100}}},
	"moderate": {Key: "moderate", Name: "Moderate", Summary: "Full refund if cancelled at least 3 days before the start, half refund if at least 24 hours before.", steps: []refundStep{{72 * time.Hour, 100}, {24 * time.Hour, 50}}},
	"strict":   {Key: "strict", Name: "Strict", Summary: "Half refund if cancelled at least 7 days before the start. No refund after that.", steps: []refundStep{{7 * 24 * time.Hour, 50}}},
}

// bookingGracePeriod: a buyer who cancels soon after booking, for a time at
// least a day away, gets everything back whatever the policy.
const bookingGracePeriod = time.Hour

func policyOrDefault(key string) cancellationPolicy {
	if p, ok := cancellationPolicies[key]; ok {
		return p
	}
	return cancellationPolicies["flexible"]
}

// buyerRefundPercent is the share of the price a buyer gets back when they
// cancel at now.
func buyerRefundPercent(policy string, bookedAt, starts, now time.Time) int64 {
	notice := starts.Sub(now)
	if notice <= 0 {
		return 0
	}
	if now.Sub(bookedAt) <= bookingGracePeriod && notice >= 24*time.Hour {
		return 100
	}
	for _, step := range policyOrDefault(policy).steps {
		if notice >= step.before {
			return step.percent
		}
	}
	return 0
}

// nextRefundDrop returns when the buyer's refund next gets smaller, so the
// interface can say "full refund until …". Zero means it cannot drop further.
func nextRefundDrop(policy string, bookedAt, starts, now time.Time) time.Time {
	current := buyerRefundPercent(policy, bookedAt, starts, now)
	if current == 0 {
		return time.Time{}
	}
	var candidates []time.Time
	if grace := bookedAt.Add(bookingGracePeriod); grace.After(now) {
		candidates = append(candidates, grace)
	}
	for _, step := range policyOrDefault(policy).steps {
		if t := starts.Add(-step.before); t.After(now) {
			candidates = append(candidates, t)
		}
	}
	best := time.Time{}
	for _, c := range candidates {
		if buyerRefundPercent(policy, bookedAt, starts, c.Add(time.Second)) < current && (best.IsZero() || c.Before(best)) {
			best = c
		}
	}
	return best
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
	return os.Getenv("REFUNDS_ENABLED") == "true" && a.providerEnvironmentConfigured()
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
