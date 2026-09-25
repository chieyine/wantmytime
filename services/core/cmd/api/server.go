package main

import (
	"aside/core/internal/observe"
	_ "image/jpeg"
	"net/http"
	"os"
	"strings"
	"time"
)

// routes builds the complete HTTP handler, including origin and security
// middleware. Tests use it to exercise exactly what production serves.
func (a *API) routes() http.Handler {
	mux := http.NewServeMux()
	// Every {id} route addresses a UUID; malformed identifiers are a 404, not a database error.
	handle := func(pattern string, h http.HandlerFunc) {
		if strings.Contains(pattern, "{id}") {
			h = requireUUIDPath("id", h)
		}
		mux.HandleFunc(pattern, h)
	}
	// Generous per-IP ceilings: mobile carriers put many people behind one
	// address. Per-email limits in createChallenge still apply.
	challengeLimit := a.limiter("challenge", 30, time.Minute)
	analyticsLimit := a.limiter("analytics", 120, time.Minute)
	publicLimit := a.limiter("public_read", 300, time.Minute)
	checkoutLimit := a.limiter("checkout", 20, time.Minute)
	uploadLimit := a.limiter("upload", 10, 10*time.Minute)
	writeLimit := a.limiter("write", 120, time.Minute)
	handle("GET /health", a.health)
	handle("GET /api/v1/handles/{handle}/availability", a.rateLimited(publicLimit, a.availability))
	handle("GET /api/v1/people/{handle}", a.rateLimited(publicLimit, a.publicPerson))
	handle("GET /api/v1/people/{handle}/avatar", a.rateLimited(publicLimit, a.publicAvatar))
	handle("GET /api/v1/people/{handle}/slots", a.rateLimited(publicLimit, a.publicSlots))
	handle("GET /api/v1/markets", a.rateLimited(publicLimit, a.publicMarkets))
	handle("GET /api/v1/runtime", func(w http.ResponseWriter, _ *http.Request) {
		jsonOut(w, 200, map[string]bool{"local_payment_simulator": a.localPaymentSimulatorEnabled(), "provider_checkout_enabled": a.providerCheckoutConfigured()})
	})
	handle("POST /api/v1/auth/challenges", a.rateLimited(challengeLimit, a.createChallenge))
	handle("POST /api/v1/auth/challenges/{id}/verify", a.rateLimited(challengeLimit, a.verifyChallenge))
	handle("POST /api/v1/auth/logout", a.logout)
	handle("GET /api/v1/me", a.me)
	handle("GET /api/v1/me/sessions", a.mySessions)
	handle("GET /api/v1/me/data-export", a.dataExport)
	handle("GET /api/v1/me/deletion", a.deletionCheck)
	handle("POST /api/v1/me/deletion", a.rateLimited(challengeLimit, a.deleteAccount))
	handle("POST /api/v1/me/sessions/revoke-others", a.revokeOtherSessions)
	handle("GET /api/v1/me/settlements", a.mySettlements)
	handle("GET /api/v1/me/settlements/{id}", a.mySettlementDetail)
	handle("GET /api/v1/me/link", a.getOwnProfile)
	handle("POST /api/v1/me/link", a.claimProfile)
	handle("PATCH /api/v1/me/link", a.updateProfile)
	handle("PUT /api/v1/me/avatar", a.rateLimited(uploadLimit, a.updateAvatar))
	handle("DELETE /api/v1/me/avatar", a.deleteAvatar)
	handle("GET /api/v1/me/availability", a.getAvailability)
	handle("PUT /api/v1/me/availability", a.putAvailability)
	handle("PUT /api/v1/me/availability/overrides/{date}", a.putAvailabilityOverride)
	handle("DELETE /api/v1/me/availability/overrides/{date}", a.deleteAvailabilityOverride)
	handle("POST /api/v1/bookings", a.rateLimited(checkoutLimit, a.createBooking))
	handle("POST /api/v1/bookings/start", a.rateLimited(checkoutLimit, a.startGuestBooking))
	handle("POST /api/v1/quotes", a.rateLimited(checkoutLimit, a.createQuote))
	handle("GET /api/v1/quotes/{id}", a.getQuote)
	handle("POST /api/v1/dev/quotes/{id}/simulate-payment", a.simulatePayment)
	handle("GET /api/v1/me/bookings", a.listBookings)
	handle("GET /api/v1/bookings/{id}", a.getBooking)
	handle("GET /api/v1/bookings/{id}/receipt", a.getBookingReceipt)
	handle("GET /api/v1/bookings/{id}/reschedules", a.listReschedules)
	handle("POST /api/v1/bookings/{id}/reschedules", a.createReschedule)
	handle("POST /api/v1/reschedules/{id}/accept", a.respondReschedule(true))
	handle("POST /api/v1/reschedules/{id}/decline", a.respondReschedule(false))
	handle("PATCH /api/v1/bookings/{id}/meeting", a.updateMeetingLink)
	handle("POST /api/v1/bookings/{id}/issue", a.reportBookingIssue)
	handle("GET /api/v1/push/config", a.pushConfig)
	handle("POST /api/v1/push/subscriptions", a.savePushSubscription)
	handle("DELETE /api/v1/push/subscriptions", a.deletePushSubscription)
	handle("POST /api/v1/bookings/{id}/issue/response", a.respondToProblem)
	handle("POST /api/v1/bookings/{id}/cancellation", a.requestCancellation)
	handle("POST /api/v1/bookings/{id}/completion", a.completeBooking)
	handle("GET /api/v1/bookings/{id}/calendar", a.bookingCalendar)
	handle("POST /api/v1/offers", a.rateLimited(checkoutLimit, a.createOffer))
	handle("GET /api/v1/me/offers", a.listOffers)
	handle("GET /api/v1/offers/{id}", a.getOffer)
	handle("POST /api/v1/offers/{id}/checkout", a.rateLimited(checkoutLimit, a.createOfferQuote))
	handle("POST /api/v1/offers/{id}/accept", a.offerRespond("accept"))
	handle("POST /api/v1/offers/{id}/counter", a.offerRespond("counter"))
	handle("POST /api/v1/offers/{id}/decline", a.offerRespond("decline"))
	handle("POST /api/v1/offers/{id}/withdraw", a.offerRespond("withdraw"))
	handle("POST /api/v1/access/challenges", a.rateLimited(challengeLimit, a.createAccessChallenge))
	handle("POST /api/v1/quotes/{id}/checkout", a.rateLimited(checkoutLimit, a.initializeQuoteCheckout))
	handle("POST /api/v1/quotes/{id}/verify-payment", a.rateLimited(checkoutLimit, a.verifyQuotePayment))
	handle("POST /api/v1/webhooks/kora", a.koraWebhook)
	handle("POST /api/v1/ops/session", a.opsSession)
	handle("GET /api/v1/ops/overview", a.opsOverview)
	handle("GET /api/v1/ops/growth", a.opsGrowth)
	handle("GET /api/v1/ops/funnels", a.opsFunnels)
	handle("GET /api/v1/ops/system", a.opsSystem)
	handle("GET /api/v1/ops/provider-events", a.opsProviderEvents)
	handle("GET /api/v1/ops/provider-cases", a.opsProviderCases)
	handle("POST /api/v1/ops/provider-events/{id}/retry", a.opsRetryProviderEvent)
	handle("GET /api/v1/ops/payments", a.opsPayments)
	handle("GET /api/v1/ops/payments/{id}", a.opsPaymentDetail)
	handle("GET /api/v1/ops/settlements", a.opsSettlements)
	handle("POST /api/v1/ops/settlements/import", a.importSettlementCSV)
	handle("GET /api/v1/ops/settlements/import-rows", a.opsSettlementImportRows)
	handle("GET /api/v1/ops/exceptions", a.opsPaymentExceptions)
	handle("GET /api/v1/ops/exceptions/{id}", a.opsPaymentExceptionDetail)
	handle("POST /api/v1/ops/exceptions/{id}/export", a.opsExportPaymentException)
	handle("POST /api/v1/ops/exceptions/{id}/resolve", a.opsResolvePaymentException)
	handle("GET /api/v1/ops/people", a.opsPeople)
	handle("GET /api/v1/ops/people/{id}", a.opsPersonDetail)
	handle("GET /api/v1/ops/bookings", a.opsBookings)
	handle("GET /api/v1/ops/bookings/{id}", a.opsBookingDetail)
	handle("GET /api/v1/ops/offers", a.opsOffers)
	handle("GET /api/v1/ops/meetings/overdue", a.opsOverdueMeetings)
	handle("POST /api/v1/ops/bookings/{id}/resolve-issue", a.opsResolveBookingIssue)
	handle("GET /api/v1/ops/cancellations", a.opsCancellations)
	handle("POST /api/v1/ops/cancellations/{id}/resolve", a.opsResolveCancellation)
	handle("POST /api/v1/analytics/events", a.rateLimited(analyticsLimit, a.recordProductEvent))
	handle("GET /api/v1/ops/audit", a.opsAudit)
	handle("POST /api/v1/ops/people/{id}/restrict", a.opsRestrict)
	handle("POST /api/v1/ops/people/{id}/revoke-sessions", a.opsRevokeSessions)
	handle("POST /api/v1/ops/people/{id}/payout-readiness", a.opsSetPayoutReadiness)
	mux.HandleFunc("/api/v1/ops/", func(w http.ResponseWriter, _ *http.Request) {
		problem(w, 404, "NOT_FOUND", "This operations route is unavailable.")
	})
	handle("GET /metrics", a.metricsHandler())
	handle("GET /api/v1/ops/alerts", a.opsAlerts)
	handle("GET /api/v1/bookings/{id}/cancellation-preview", a.cancellationPreviewHandler)
	handle("POST /api/v1/bookings/{id}/cancel", a.cancelBooking)
	handle("POST /api/v1/bookings/{id}/no-show", a.reportNoShow)
	handle("POST /api/v1/bookings/{id}/no-show/dispute", a.disputeNoShow)
	handle("POST /api/v1/bookings/{id}/review", a.createReview)
	handle("POST /api/v1/reviews/{id}/reply", a.replyToReview)
	handle("GET /api/v1/people/{handle}/reviews", a.rateLimited(publicLimit, a.publicReviews))
	handle("GET /api/v1/me/cancellation-policy", a.getCancellationPolicy)
	handle("PUT /api/v1/me/cancellation-policy", a.setCancellationPolicy)
	handle("GET /api/v1/me/refund-recoveries", a.myRecoveries)
	handle("GET /api/v1/ops/refunds", a.opsRefunds)
	handle("POST /api/v1/ops/refunds/{id}/approve", a.opsRefundAction("approve"))
	handle("POST /api/v1/ops/refunds/{id}/retry", a.opsRefundAction("retry"))
	handle("POST /api/v1/ops/refunds/{id}/record", a.opsRefundAction("record"))
	handle("POST /api/v1/ops/bookings/{id}/refund", a.opsCreateRefund)
	handle("GET /api/v1/ops/no-shows", a.opsNoShows)
	handle("POST /api/v1/ops/no-shows/{id}/resolve", a.opsResolveNoShow)
	handle("GET /api/v1/payout-banks", a.listPayoutBanks)
	handle("GET /api/v1/me/payout-account", a.getPayoutAccount)
	handle("PUT /api/v1/me/payout-account", a.setPayoutAccount)
	handle("POST /api/v1/me/payout-account/resolve", a.resolvePayoutAccount)
	handle("GET /api/v1/me/payouts", a.myPayouts)
	handle("GET /api/v1/ops/payouts", a.opsPayouts)
	handle("POST /api/v1/ops/payouts/{id}/retry", a.opsRetryPayout)
	handle("GET /api/v1/ops/reviews", a.opsReviews)
	handle("POST /api/v1/ops/reviews/{id}/hide", a.opsSetReviewVisibility(true))
	handle("POST /api/v1/ops/reviews/{id}/restore", a.opsSetReviewVisibility(false))
	handle("GET /api/v1/me/calendar", a.calendarStatus)
	handle("PATCH /api/v1/me/calendar", a.calendarUpdate)
	handle("DELETE /api/v1/me/calendar", a.calendarDisconnect)
	handle("POST /api/v1/me/calendar/google", a.calendarConnectStart)
	handle("GET /api/v1/integrations/google/callback", a.calendarCallback)
	metrics := a.metrics
	if metrics == nil {
		metrics = observe.NewMetrics()
		a.metrics = metrics
	}
	return observe.Middleware(secureHeaders(a.cors(a.writeLimited(writeLimit, mux))), a.log(), a.reporter, metrics)
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if e := a.db.Ping(r.Context()); e != nil {
		problem(w, 503, "DATABASE_UNAVAILABLE", "Service is temporarily unavailable.")
		return
	}
	jsonOut(w, 200, map[string]string{"status": "ok"})
}

func (a *API) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := map[string]bool{}
		if a.env != "production" {
			allowed["http://127.0.0.1:5173"] = true
			allowed["http://localhost:5173"] = true
		}
		if configured := os.Getenv("PUBLIC_APP_ORIGIN"); configured != "" {
			allowed[configured] = true
		}
		if origin != "" && !allowed[origin] {
			problem(w, 403, "ORIGIN_DENIED", "Request origin is not allowed.")
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		providerWebhook := r.Method == http.MethodPost && r.URL.Path == "/api/v1/webhooks/kora"
		if r.Method != http.MethodGet && r.Method != http.MethodHead && origin == "" && !providerWebhook {
			problem(w, 403, "ORIGIN_REQUIRED", "A same-origin request is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// secureHeaders applies to every API response. The API only returns JSON,
// calendar files and profile photos, so its policy forbids everything else.
func secureHeaders(next http.Handler) http.Handler {
	hsts := os.Getenv("APP_ENV") == "production"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}
		next.ServeHTTP(w, r)
	})
}
