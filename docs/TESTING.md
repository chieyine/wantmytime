# Testing

Every push and pull request runs `.github/workflows/ci.yml`:

| Check | What it proves |
|---|---|
| `gofmt`, `go vet` | Go code is formatted and type-checks, including the integration tests. |
| `sqlc diff` | The generated query code in `services/core/internal/store` matches `services/core/queries/*.sql` and the migrations. |
| `go test ./...` | Unit tests (validation, TOTP, time zones, origin policy, fail-closed payment gates). |
| `go test -tags integration` | The real HTTP handlers against a real PostgreSQL database and a local fake Kora API (charges, transfer accounts, refunds, payouts, banks). |
| Migration run | Every migration applies to an empty database. |
| `npm run check`, `npm run build` | The SvelteKit app type-checks and builds. |
| Docker builds | Both container images build. |

## Integration tests

`services/core/cmd/api/integration_test.go` creates a throwaway database, applies every migration, and drives the API through `httptest` with real cookies, origins and email challenges (codes are captured in memory instead of being emailed). A fake Kora server stands in for `api.korapay.com`; it is reachable only because `KORA_API_BASE` is honoured outside production.

Covered journeys:

- Seller claim (including the second-link and unknown-field rules) and repeated availability saves.
- A booking paid by bank transfer end to end: a one-off account shown in WantMyTime (reused on refresh), the hold lasting past the account's expiry, a short transfer refused as underpaid, then the signed `charge.success` webhook, worker verification, booking, allocation, immediately-available payout funds, balanced ledger journal and confirmation emails. A forged webhook is refused.
- Pay with bank as a second method: the hosted page, its funds held until settlement, and a second payment for the same quote flagged as a duplicate charge instead of a second booking.
- Kora amounts in naira strings and numbers convert to kobo exactly; bank transfer is always offered first; the webhook signature covers the `data` object.
- A payment against a withdrawn offer becoming an `offer_conflict` exception instead of an error.
- Reschedule acceptance, cancellation review, session revocation, payout readiness, settlement import (all audited), TOTP replay and self-restriction refusal.
- Stale holds not blocking a slot, the three-hold limit, offer-mode sellers refusing fixed quotes and malformed IDs returning 404.
- A smoke test that calls every route once with valid input and fails on any server error.
- Observability (`observability_integration_test.go`): gateway request IDs kept and echoed, `/metrics` token rules and route-pattern labels, worker heartbeats, and the watchdog firing, not repeating, re-notifying on a new failure, resolving by email, and standing down while another instance holds the lock.
- Time zones and email (`notifications_integration_test.go`):
  - A buyer abroad sees slots for their own calendar day, which can span two of the seller's days.
  - The verified browser time zone is saved.
  - Confirmation emails lead with each person's own time and show the other person's time too.
  - They include an HTML body and a calendar file with lines folded to 75 octets.
  - Reschedule, cancellation and offer events email the other person.
  - A stale email (for example an offer already withdrawn) is cancelled rather than sent.
  - Rendering escapes HTML and neutralises header injection.

- Google Calendar (`calendar_integration_test.go`, against a fake Google):
  - Connecting uses PKCE and offline access, and stores the refresh token encrypted.
  - Busy times remove slots and refuse holds; turning busy checks off clears them.
  - A missing scope, a bad code, or a callback in another account's session is refused.
  - A booking creates a guest-free event, waits while Google finishes the Meet link, then saves and emails it. Rescheduling moves the event.
  - A seller's own link takes priority over Meet.
  - Revoked access is shown to the seller.
  - Disconnecting revokes the grant and is audited.
  - A booking's own event never blocks moving it.

- Cancellations, refunds, no-shows and reviews (`lifecycle_integration_test.go`):
  - Policy arithmetic, including the grace period and proportional refund shares.
  - A buyer cancellation under a frozen moderate policy is refused when the shown refund is stale, then succeeds. The time is released.
  - The refund goes processing then successful at Kora via webhook, with a balanced journal, the seller's share taken from the held payout (the rest is paid out), a partly refunded booking, and emails with a calendar cancellation.
  - A seller cancellation before the payout refunds in full and cancels the payout; nothing is transferred and the seller owes nothing.
  - With automatic refunds off, nothing is sent to Kora. Recording a refund is audited and cannot happen twice.
  - A no-show reported too early is refused. An undisputed one stands with a refund. A disputed one is rejected by operations, with an audit record.
  - Reviews are refused before the session, from the seller, when invalid or when repeated. Replies are limited to one, ratings appear publicly and on the profile, hiding works, and the review request is dropped once a review exists.

`services/core/internal/observe` has unit tests for request-ID validation, Sentry DSN parsing, event throttling, panic recovery (one report per panic, no panic text in the response), metrics output and log levels.

## Phase 7 checks

- `funnels_integration_test.go`: a page view, time picker, time pick, share, hold and simulated payment each move the right funnel step by one; the endpoint needs operations access.
- Accessibility and layout are checked in a browser rather than in CI: seed a seller, a buyer and an operator, then run axe-core on every page at 1280px and 390px, recording violations, horizontal overflow and console errors, and tab through the main pages to confirm focus order and visible focus. Last run (2026-09-25): no violations, no overflow, no errors.

## Phase 6 checks

- `ratelimit_test.go`: RESP parsing, Redis URL parsing, fallback when Redis is down, and one count shared by two limiter instances (needs `REDIS_TEST_URL`, for example `redis://127.0.0.1:6379/3`; skipped without it).
- `objectstore_test.go`: the S3 Signature V4 header matches a value computed independently with botocore, and a put/get/delete round trip against a fake bucket.
- `main_test.go` `TestProductionConfigChecks`: start-up configuration problems are caught.
- `hardening_integration_test.go`: route rate limits (webhooks exempt), API security headers, photos stored in and served from the bucket (stream and redirect) and cleaned up on replace and delete, the data export (contents, no secrets, guest sessions refused, daily limit), account deletion (blockers, confirmation, erased identifiers, kept bookings, link hold, fresh sign-up, buyer name removal) and the retention sweep.

The integration harness multiplies every rate limit by 100 so journeys are not throttled; `TestRateLimitsApplyPerAddress` uses the real limits.

## Backups

`infrastructure/backup` scripts are exercised by hand against a local PostgreSQL. A clean run keeps a verified dump. A corrupted dump, a tampered checksum and an unbalanced ledger each fail and record `backup failed` in `worker_heartbeats`. `restore.sh` refuses without `--yes` and refuses a non-empty target. Repeat this drill before launch (docs/OPERATIONS.md).

Run locally with the Compose database:

```sh
make db-up
make test-integration
```

## Typed queries

The money path (payments, webhooks, ledger, holds, email outbox, settlement import) uses sqlc. After changing `services/core/queries/*.sql` or a migration, run `make sqlc` (or `sqlc generate` in `services/core`) and commit the regenerated `internal/store` files; CI fails if they drift. The remaining handlers still use inline SQL and are covered by the integration smoke test; move them to `queries/` as they are touched.
