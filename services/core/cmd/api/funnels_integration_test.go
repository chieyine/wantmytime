//go:build integration

package main

import "testing"

func funnelCounts(t *testing.T, c *client) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	body := c.expect(200, "GET", "/api/v1/ops/funnels?days=7", nil)
	if body["days"].(float64) != 7 {
		t.Fatalf("period: %v", body["days"])
	}
	for _, side := range []string{"buyer", "seller"} {
		for _, s := range body[side].([]any) {
			step := s.(map[string]any)
			out[side+"."+step["key"].(string)] = step["count"].(float64)
		}
	}
	return out
}

func TestFunnels(t *testing.T) {
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	h := newHarness(t)
	ops, _ := h.operator()
	before := funnelCounts(t, ops)

	s := h.newSeller("fixed")
	anon := h.client("")
	for _, event := range []string{"public_link_viewed", "booking_started", "slot_selected"} {
		anon.expect(204, "POST", "/api/v1/analytics/events", map[string]string{"event": event, "handle": s.handle})
	}
	s.expect(204, "POST", "/api/v1/analytics/events", map[string]string{"event": "link_copy_clicked", "handle": s.handle})
	buyer := h.guestBuyer()
	h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))

	after := funnelCounts(t, ops)
	for key, want := range map[string]float64{"buyer.viewed": 1, "buyer.started": 1, "buyer.picked": 1, "buyer.held": 1, "buyer.paid": 1, "seller.claimed": 1, "seller.hours": 1, "seller.bank": 1, "seller.shared": 1, "seller.booked": 1, "seller.paid": 1} {
		if got := after[key] - before[key]; got != want {
			t.Errorf("%s grew by %v, want %v", key, got, want)
		}
	}
	// Operations permission is required.
	s.expect(403, "GET", "/api/v1/ops/funnels", nil)
}
