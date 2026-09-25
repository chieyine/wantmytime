# Decisions

Updated: 2026-09-24

1. The product is operated by Kredit Technologies Limited. The only starting product brief is `README(1).md`; it was read in full before implementation. The product is named WantMyTime and lives at `wantmytime.com` (domain bought 2026-09-24; renamed from the working name “Aside”). Internal names such as the `aside-api` binary, the Go module and the session cookie are unchanged.
2. Architecture: SvelteKit + strict TypeScript, Go API, PostgreSQL. Business authority stays in Go and PostgreSQL. Values are integer NGN minor units; public prices and offers are versioned.
3. Financial policy: maximum platform deduction is 500 bps and fee/allocation constraints are encoded in the schema. The owner has not approved fee interpretation, subsidies, merchant arrangement or settlement route. Live collection, webhook acknowledgment and payout are disabled.
4. A local-only payment simulator is enabled only with both `APP_ENV=local` and `LOCAL_PAYMENT_SIMULATOR=true`. Simulated bookings carry `payment_state=simulated` and do not create allocations or settlement entries.
5. Sellers set their local timezone and recurring availability. Holds use database overlap exclusion; slots honor notice, horizon, buffers, overrides and DST ambiguity/gap rules.
6. Email sessions require a verified identity. Public booking and offer submissions use restricted guest sessions scoped to verified resource IDs; account sessions remain separate. Offers preserve versioned agreed prices and expire after a bounded checkout interval.
7. Operations requires an explicit bootstrap of an already verified account, an encrypted TOTP secret and per-route permissions. Account restriction and session revocation require an audit record in the same transaction. No balance-edit or mark-paid operation exists.
8. Meeting links are booking-specific HTTPS URLs, encrypted at rest with `MEETING_LINK_ENCRYPTION_KEY`, visible only to booking participants and due 30 minutes before start. The API permits late delivery until the meeting starts; operations can see overdue links. Calendar export is private and participant-authorized.
9. Buyer/seller completion signals and private issue reporting are persisted on the booking. The transactional notification outbox and worker, meeting reminders, participant rescheduling, cancellation requests, provider payment processing and receipt lifecycle are implemented in code; email/provider runtime behavior remains externally gated or unverified.
10. Exclusions: no expertise fields, marketplace, courses, wallet, AI features, affiliate rewards or mobile app.
11. Rate limits are shared through Redis with a local fallback, using a small built-in client rather than an SDK (one command, no new dependencies). Profile photos move to Cloudflare R2 through the S3 API with a built-in Signature V4 signer, checked against the AWS reference implementation.
12. Account deletion anonymizes rather than deletes rows that money records point at: names and emails go, amounts and dates stay for six years. A deleted person's link is held for 180 days.
13. The Content Security Policy uses SvelteKit nonces for scripts; inline styles stay allowed because Svelte uses style attributes and styles cannot run code.
14. Buyers pay before confirming their email (founder's choice, 2026-09-25). Picking a time opens a booking-only session for the email typed (`POST /api/v1/bookings/start`): it can hold that time, pay and see that booking, nothing else, and it never counts as a confirmed email. Booking emails go to the address; opening the booking elsewhere, or signing in, still needs a code, and that code claims the same account. Limits: route rate limits, 10 such sessions per email per hour, three unpaid holds per person. Offers still need a code first.
15. Sellers open for bookings automatically once the bank confirms their payout account name (founder's choice, 2026-09-25). Money is held until after each call, so buyers stay protected. An operator can put a seller on hold (`readiness_state='held'`); only an operator lifts it, and a bank account change never does.
16. The buyer pays the bank transfer fee on top of the price (founder's choice, 2026-09-25). The seller keeps 95% of the price. If the buyer cancels, the fee is not refunded; if the seller cancels or doesn't show, the buyer gets everything back, fee included, and the platform bears it. Migration 021 records the fee per payment attempt.

## Environment evidence and gates

- A temporary PostgreSQL 17 instance under `/private/tmp` accepted the schema and the API's migration runner; email/profile/booking/offer flows were previously exercised against temporary databases.
- Go tests/vet and Svelte check/build pass. No provider sandbox credentials, verified sender, legal/commercial approval, approved deployment target or final domain is configured.
- Docker is unavailable. Production deployment, restore/recovery and visual browser review have not been demonstrated.
