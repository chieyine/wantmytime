# Decisions

Updated: 2026-09-23

1. The only starting product brief is `README(1).md`; it was read in full before implementation. The product is named WantMyTime and lives at `wantmytime.com` (domain bought 2026-09-24; renamed from the working name “Aside”). Internal names such as the `aside-api` binary, the Go module and the session cookie are unchanged.
2. Architecture: SvelteKit + strict TypeScript, Go API, PostgreSQL. Business authority stays in Go and PostgreSQL. Values are integer NGN minor units; public prices and offers are versioned.
3. Financial policy: maximum platform deduction is 500 bps and fee/allocation constraints are encoded in the schema. The owner has not approved fee interpretation, subsidies, merchant arrangement or settlement route. Live collection, webhook acknowledgment and payout are disabled.
4. A local-only payment simulator is enabled only with both `APP_ENV=local` and `LOCAL_PAYMENT_SIMULATOR=true`. Simulated bookings carry `payment_state=simulated` and do not create allocations or settlement entries.
5. Sellers set their local timezone and recurring availability. Holds use database overlap exclusion; slots honor notice, horizon, buffers, overrides and DST ambiguity/gap rules.
6. Email sessions require a verified identity. Public booking and offer submissions use restricted guest sessions scoped to verified resource IDs; account sessions remain separate. Offers preserve versioned agreed prices and expire after a bounded checkout interval.
7. Operations requires an explicit bootstrap of an already verified account, an encrypted TOTP secret and per-route permissions. Account restriction and session revocation require an audit record in the same transaction. No balance-edit or mark-paid operation exists.
8. Meeting links are booking-specific HTTPS URLs, encrypted at rest with `MEETING_LINK_ENCRYPTION_KEY`, visible only to booking participants and due 30 minutes before start. The API permits late delivery until the meeting starts; operations can see overdue links. Calendar export is private and participant-authorized.
9. Buyer/seller completion signals and private issue reporting are persisted on the booking. The transactional notification outbox and worker, meeting reminders, participant rescheduling, cancellation requests, provider payment processing and receipt lifecycle are implemented in code; email/provider runtime behavior remains externally gated or unverified.
10. Exclusions: no expertise fields, marketplace, courses, wallet, AI features, affiliate rewards or mobile app.

## Environment evidence and gates

- A temporary PostgreSQL 17 instance under `/private/tmp` accepted the schema and the API's migration runner; email/profile/booking/offer flows were previously exercised against temporary databases.
- Go tests/vet and Svelte check/build pass. No provider sandbox credentials, verified sender, legal/commercial approval, approved deployment target or final domain is configured.
- Docker is unavailable. Production deployment, restore/recovery and visual browser review have not been demonstrated.
