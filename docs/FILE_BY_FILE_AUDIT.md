# File-by-file audit

Date: 2026-09-25
Scope: every tracked file in the repository (Go API, SQL migrations and queries, generated store code, SvelteKit routes and components, styles, static files, infrastructure, OpenAPI and docs). Generated build output (`build/`, `.svelte-kit/`, `node_modules/`) was not audited. The earlier version of this file claimed test runs and ratings that had not happened; it is replaced by this one.

Method: each file was read in full (or, for the largest generated and CSS files, read for anything that touches money, identity, currency or flow), then every buyer and seller journey was traced through the code step by step ([FLOW_WALKTHROUGH.md](FLOW_WALKTHROUGH.md)). Two goals were checked throughout: **the platform should be seamless** (no step where a buyer or seller gets stuck, or waits on an operator for something the system can decide), and **Nigeria first, not Nigeria only**.

Verification after the fixes: `gofmt`, `go build ./...` and `go vet ./...` (which also compiles every test file) pass; `svelte-check` passes with 0 errors and 0 warnings; `vite build` (adapter-node) passes; `api/openapi.yaml` parses (110 paths). As instructed, the test suites were not run and no tests were added; the few existing test expectations that encoded the old behaviour were updated so the suite matches the new behaviour. Migration 022 has not been applied to a database yet.

---

## What was wrong, and what changed

### Breaks in the flows

| # | Severity | Where | Problem | Fix |
|---|---|---|---|---|
| 1 | Critical | `quotes.go` `createOfferQuote` | A buyer reopening an accepted offer from the email on another device (access session) was refused at checkout with "offer not available": only the session that sent the offer could pay. | Any guest session whose scope includes the offer can check out. |
| 2 | Critical | `payout_provider.go` `resolveAccount` | Kora's account-name check was sent `currency: "NG"` (the country code). Kora expects the currency (`NGN`), so no seller could have saved a bank account against the real API. The test fake encoded the same mistake. | Sends the market's currency; fake corrected. |
| 3 | High | `payouts.go` `payoutHoldSQL`, `ops.go` | A buyer's "exceptional circumstances" cancellation request held the seller's payout until an operator resolved it, even after the call took place. | Requests go to the seller (email, notice on the booking with the reason); saying yes is cancelling with a full refund. They no longer hold payouts and close themselves at the booking time (`lifecycle_extras.go`). |
| 4 | High | `payment_processing.go` | A slow bank transfer that landed after its hold lapsed always became a manual exception, even when the time was still free. | `reclaimLateSlot` rebooks the time if it's in the future, inside the seller's hours and free (savepoint around the exclusion constraint). |
| 5 | High | `payment_processing.go`, `refunds.go` | Double payments, payments after the time was taken, payments to a seller who paused, and payments for a closed offer all waited for an operator to refund by hand; the buyer heard nothing. | Automatic full refund tied to the exception (`unbooked.go`, migration 022), emails at start and completion, exception resolves itself. |
| 6 | High | `payment_processing.go` | A processor fee above the platform's share (for example an unexpected processor charge) refused the booking even though the buyer had paid in full. | The booking goes ahead, the platform absorbs the difference, and anything outside the approved schedule is flagged for review. |
| 7 | High | `payouts.go` | Every failed payout needed an operator, even after the seller fixed their bank details. | Automatic retry once after two hours and whenever the seller saves a different account (after its 24-hour hold), audited as `payout.auto_retry`. |
| 8 | High | booking lifecycle | A seller who never added a meeting link left the buyer with none at the start time. | At the link deadline, a Jitsi link is created, saved and emailed to both (`AUTO_MEETING_LINKS`, `MEETING_LINK_BASE`); the alert now fires only if that fails. |
| 9 | Medium | `offers.go`, `PublicPersonPage.svelte` | Offers could be sent to a seller with no payout account; the buyer could never have paid. | Offers require a ready seller; the page shows "Bookings open soon". |
| 10 | Medium | `payment/return/+page.svelte` | A pay-with-bank or mobile money payment still being confirmed left the buyer on a "check again" button. | Checks by itself every 5 seconds for about two minutes; clear messages for declined and returned payments. |
| 11 | Medium | `offer/[id]`, `app/offers/[id]` | Times were labelled with the seller's timezone while shown in the buyer's; the page opened on a date without times. | Labelled correctly, opens on the first free day. |
| 12 | Medium | `offer/new/+page.svelte` | Told buyers "Email notifications are not configured, so the seller will see it in their account" (false). | Removed; explains what happens next. |
| 13 | Medium | `book/new/+page.svelte` | Opening the booking page for an offer-mode seller failed at the hold step. | Redirects to the offer page (and the reverse). |
| 14 | Medium | receipts, emails, booking page | The buyer's receipt and confirmation showed the price, not what they paid (the payment fee was missing); receipts disappeared after a partial refund. | Receipt shows price, payment fee, total paid, refunds and method (seller sees their share instead); confirmation email and booking page show the total paid; receipts stay available when refunded. |
| 15 | Low | `/login` | A signed-in person clicking "Log in" was asked for a code again. | Goes straight on. |
| 16 | Low | `mail.go` | Every email said replies are not monitored, even with `EMAIL_REPLY_TO` set. | Footer invites replies when a reply address is set. |
| 17 | Low | `noshow.go`, `reviews.go` | Three row loops ignored `rows.Err()`. | Checked. |
| 18 | Low | `observability.go` | The payment-exceptions alert would fire for refunds already in progress. | Counts only exceptions still needing a person. |

### Nigeria-only assumptions (70+ places)

The database refused any currency but NGN (`pricing_versions`, `quotes`), pricing, quotes, payment attempts, Kora calls and offer emails hard-coded NGN, payouts accepted only 10-digit Nigerian accounts, the web app printed ₦ everywhere, the timezone list had 13 entries with Lagos as the fallback, checkout told buyers to pay "from any Nigerian bank app", and copy described bank transfer as the only way to pay.

Now: each seller has a country and currency (migration 022, `markets.go`). Nigeria is on by default; Ghana and Kenya are built in and switched on with `SELLER_COUNTRIES` once Kora enables them. Channels, charge limits and fee schedules are per currency. Payouts go to bank accounts in each country and to mobile money wallets in Ghana and Kenya. Buyers anywhere see their own timezone (every IANA zone is selectable) and pay in the seller's currency by the local method (no cards). All money on every page and email is formatted in the record's own currency.

---

## Files

Status: **OK** (no change needed), **Fixed** (changed in this pass), **New**.

### Root and infrastructure

| File | Status | Notes |
|---|---|---|
| `.dockerignore`, `services/core/.dockerignore` | OK | Excludes secrets and build output. |
| `.env.example` | Fixed | Seller countries, per-currency channels and limits, no card channel, automatic refunds on, automatic meeting links. |
| `.gitignore` | OK | |
| `Dockerfile` (web) | OK | Node 24 Alpine, runs `svelte-check` in the build, non-root. |
| `services/core/Dockerfile` | OK | Distroless, non-root, ships migrations. |
| `Makefile`, `apps/web/Makefile` | OK | `verify` expects a local PostgreSQL for integration tests. |
| `compose.yaml` | Fixed | Passes the new settings to the API. |
| `infrastructure/nginx.conf` | OK | Same-origin gateway, `/metrics` blocked, 3 MB body limit for photos. |
| `infrastructure/backup/*.sh` (4) | OK | Dump, checksum, restore-verify (ledger balance check), off-site copy, heartbeat. |
| `README.md` | Fixed | Removed the "Friends get your time free" tagline (not the intended positioning); countries and payments described correctly. |
| `README(1).md` | OK | Original product brief, kept as the historical source. |
| `api/openapi.yaml` | Fixed | Kora webhook (was still Paystack), 31 missing routes added, stale Paystack subaccount field removed. |

### Database: migrations

| File | Status | Notes |
|---|---|---|
| `001`–`021` | OK | Read in order; constraints, triggers (append-only audit, ledger, reschedule history) and indexes are sound. `001` and `003` carry NGN-only checks, lifted by `022`. |
| `022_international_and_seamless.sql` | New | Seller country and currency; NGN checks dropped (found by definition); payout destination type; refunds and emails for payment exceptions; new email kinds; `meeting_source='auto'`. |

### Database: queries and generated store (`services/core/queries`, `internal/store`)

| File | Status | Notes |
|---|---|---|
| `provider.sql` / `provider.sql.go` | Fixed | Fee schedules looked up by currency; checkout quote returns currency and seller country; payment attempts store the quote's currency. Edited by hand to match what `sqlc generate` produces. |
| `notifications.sql` / `notifications.sql.go` | Fixed | Notification target knows payment-exception emails. |
| `observability.sql` / `observability.sql.go` | Fixed | Payment-exception alert ignores refunds in progress. |
| `calendar`, `holds`, `ledger`, `payments`, `refunds`, `settlements` (`.sql` and `.sql.go`), `db.go`, `models.go`, `sqlc.yaml` | OK | Currency flows through from the quote; no change needed. |

### Go API (`services/core/cmd/api`)

| File | Status | Notes |
|---|---|---|
| `main.go` | OK | Migrations are an explicit command; workers start conditionally; graceful shutdown. |
| `config.go` | Fixed | Refuses unknown countries in `SELLER_COUNTRIES`. |
| `server.go` | Fixed | `GET /api/v1/markets`. |
| `markets.go` | New | Countries, currencies, channels, payout options, limits, readiness per currency. |
| `auth.go` | OK | Codes, sessions, guest scopes. |
| `quickbook.go` | OK | Pay-first booking sessions. |
| `profiles.go` | Fixed | Country chosen at claim; currency on profile, prices and public page; payment methods on the public page; default timezone UTC, not Lagos. |
| `availability.go` | OK | DST-safe slots, viewer timezone labels. |
| `holds.go` | OK | |
| `quotes.go` | Fixed | Quotes in the seller's currency; per-method fees; readiness per currency; offer checkout from access sessions (#1). |
| `checkout.go` | Fixed | Currency-aware limits, fees and channels; hosted page for pay-with-bank and mobile money. |
| `kora.go` | OK | Currency was already a parameter; bank transfer stays NGN-only by design. |
| `payment_processing.go` | Fixed | #4, #5, #6. |
| `unbooked.go` | New | Late-slot reclaim, automatic refunds of unbooked payments, their emails. |
| `refunds.go` | Fixed | Exception refunds finalized separately; operations list includes them; recoveries show currency. |
| `payouts.go` | Fixed | Payout accounts per country (bank or mobile money), #7, hold rules (#3), currency on totals. |
| `payout_provider.go` | Fixed | #2; mobile money destinations. |
| `bookings.go` | Fixed | Currency, payment fee and the buyer's cancellation request on the booking; receipts by role; request goes to the seller. |
| `cancellation.go`, `policy.go` | OK | Frozen policy, refund-can't-drop check. |
| `noshow.go` | Fixed | `rows.Err()`. |
| `reviews.go` | Fixed | `rows.Err()`; lifecycle worker runs the two new steps. |
| `lifecycle_extras.go` | New | Closes started cancellation requests; creates missing meeting links. |
| `offers.go` | Fixed | Ready sellers only; currency and seller name in responses. |
| `notifications.go` | Fixed | Currency symbols; total paid; seller-facing cancellation request; automatic meeting link email; payment-exception emails; "payout account" wording. |
| `mail.go` | Fixed | #16. |
| `ops.go` | Fixed | Per-country readiness on System health; currency on bookings. |
| `ops_sellers.go` | OK | Hold and lift. |
| `settlement_import.go` | Fixed | Accepts any currency code. |
| `observability.go` | Fixed | #18; meeting-link alert matches automatic links; payout alert text. |
| `finance_read.go`, `growth.go`, `analytics.go`, `privacy.go`, `calendar.go`, `google.go`, `crypto.go`, `objectstore.go`, `redis.go`, `ratelimit.go`, `pagination.go`, `httputil.go`, `ledger.go`, `email.go`, `smtp.go` | OK | Read in full; nothing tied to one country or blocking a flow. |
| `*_test.go` (10) | Fixed | Only expectations that encoded the old behaviour (Kora name-check currency, payout email subject, fee-over-schedule now books). |
| `internal/observe/*` (6) | OK | Logging, metrics, Sentry. |
| `go.mod`, `go.sum` | OK | Only pgx. |

### Web app (`apps/web`)

| File | Status | Notes |
|---|---|---|
| `package.json`, `package-lock.json`, `.npmrc`, `svelte.config.js`, `vite.config.ts`, `tsconfig.json` | OK | Nonce CSP; no external scripts. |
| `src/app.html`, `app.d.ts`, `hooks.client.ts`, `hooks.server.ts`, `node-runtime.d.ts` | OK | Security headers, request IDs, error reporting. |
| `src/lib/money.ts` | Fixed | `formatMoney(minor, currency)`, symbols, method labels. |
| `src/lib/timezones.ts` | Fixed | Every IANA zone. |
| `src/lib/payouts.ts` | Fixed | Wording and currency. |
| `src/lib/api.ts`, `analytics.ts`, `brand.ts`, `time.ts`, `observe/sentry.ts` | OK | |
| `components/BookingDetail.svelte` | Fixed | Currency, total paid, seller sees the buyer's request, automatic-link note, payout wording. |
| `components/PublicPersonPage.svelte` | Fixed | Currency, how to pay, readiness for offer mode. |
| `components/Header`, `Footer`, `CursorPager`, `TimeDial` | OK | |
| `src/styles.css` | OK | Design tokens and layouts; no country-specific content. |
| `routes/+page.svelte` (home) | Fixed | Copy no longer says transfer is the only way to pay. |
| `routes/[handle]/*` | OK | Server-rendered profile with OpenGraph. |
| `routes/book/new` | Fixed | Currency, methods, offer-mode redirect. |
| `routes/checkout/[checkout_id]` | Fixed | Every method with its total. |
| `routes/payment/return` | Fixed | #10. |
| `routes/claim` | Fixed | Country and currency. |
| `routes/verify` | Fixed | Passes the country to the claim. |
| `routes/login` | Fixed | #15. |
| `routes/access`, `access/bookings` | OK / Fixed | Currency on the list. |
| `routes/booking/[id]`, `reschedule`, `auth/verify`, `r/[share_id]` | OK | |
| `routes/booking/[id]/receipt` | Fixed | #14. |
| `routes/offer/new`, `offer/[id]` | Fixed | #11, #12, currency. |
| `routes/app/*` (layout, overview, bookings, money, offers, link, availability, onboarding, share, settings pages) | Fixed where money or copy was involved | Currency everywhere; payout account wording; availability explains buyers see their own timezone. |
| `routes/app/settings/payouts` | Fixed | Bank or mobile money per country; typed name where no bank check. |
| `routes/app/settings/connections`, `data`, `security` | OK | |
| `routes/ops/*` (24 pages) | Fixed where money shown | Amounts in each record's currency; refunds page shows automatic unbooked refunds; System health lists countries; hold wording on people. |
| `routes/pricing`, `help` | Fixed | Currencies, countries, pay with bank, mobile money, no cards, late and double payments. |
| `routes/terms`, `acceptable-use` | Fixed | Were one-paragraph placeholders; now full terms matching the product, effective 25 September 2026. |
| `routes/privacy` | Fixed | Rights for people outside Nigeria; Kora's countries. |
| `routes/og/*` | Fixed | Price in the seller's currency. |
| `routes/+layout`, `+error`, `health`, `dev/ui` | OK | |
| `static/*` | OK | |

### Docs

| File | Status | Notes |
|---|---|---|
| `FLOW_WALKTHROUGH.md` | New | Every buyer and seller step, traced and marked where fixed. |
| `FILE_BY_FILE_AUDIT.md` | Replaced | This file. |
| `LAUNCH_CHECKLIST.md` | Rewritten | Only items that need the owner. |
| `DECISIONS.md`, `MONEY_FLOW.md`, `STATE_MACHINES.md`, `PROVIDER_CAPABILITIES.md`, `DEPLOYMENT.md`, `OPERATIONS.md`, `SECURITY.md`, `TESTING.md`, `IMPLEMENTATION_STATUS.md` | Updated | Countries, automatic refunds, payout retries, cancellation requests, meeting links, endpoints to confirm with Kora. |
| `DESIGN_SYSTEM.md` | OK | |

---

## Left as they are, on purpose

- **Offers still need an email code first** (founder decision 14); bookings don't.
- **Disputed no-shows and buyer problem reports** are decided by a person; the payout waits. Alerts cover both.
- **Seller's country can't be changed by the seller** once chosen (prices, payouts and the ledger are in that currency); a move is a support action.
- **Kora endpoint details for Ghana and Kenya** (mobile money operator codes, payout destination shape) follow Kora's public documentation and must be confirmed in the sandbox before those countries are switched on; they are off by default.
- **meet.jit.si** may ask the first person joining to sign in before the call starts; point `MEETING_LINK_BASE` at a self-hosted Jitsi for none.

## Cards removed (2026-09-25)

At the founder's request WantMyTime takes no card payments. `card` is no longer a supported channel (`markets.go`), South Africa is dropped, the international-card fee path, `INTERNATIONAL_CARDS_ENABLED` and its operations panels are removed,  and every page, email, the terms and the privacy notice now describe bank transfer, pay with bank and mobile money only. `SETTLEMENT_WAIT_HOURS` (old name `CARD_SETTLEMENT_HOURS` still read) sets how long pay-with-bank and mobile money money waits to settle.

## Kora adds its fee for the buyer (2026-09-25)

`kora.go` sends `merchant_bears_cost=false`; `checkout.go` asks Kora for the price and records the exact total Kora quotes for a transfer; `payment_processing.go` records the fee Kora actually charged as the buyer's fee, so the seller's share and the 5% never absorb a processor fee. Fee schedules are now optional estimates; the fee-above-share exception is gone. The checkout page says the provider's charge is added and shows the exact figure before payment.

## Automatic operations (2026-09-25)

`automation.go` (new) and migration 023: sellers answer buyer problem reports themselves (`POST /api/v1/bookings/{id}/issue/response`: refund in full, refund part, disagree); unanswered reports refund the buyer in full after `PROBLEM_RESPONSE_HOURS`; only disagreements reach Operations. Failed refunds (never accepted by Kora), payment events and emails are re-queued 1, 6 and 24 hours after failing; failed payouts 2, 12, 24 and 48 hours, with a `payout_failed_seller` email each time. Wrong-amount and wrong-currency payments refund automatically. `REFUNDS_ENABLED` is on unless set to `false`. Alerts count only what is still stuck after automatic handling. Booking page, operations booking page, help and terms updated.

## Phone and browser notifications (2026-09-25)

`webpush.go` (new): VAPID signing and RFC 8291 aes128gcm encryption using only Go's standard library, checked against the RFC 8291 test vector; posts only to the browsers' own push services. `push.go` (new): config and subscribe/unsubscribe endpoints, the push queue and worker (new booking and problem report queued at the event; call-in-10-minutes found by the worker, so reschedules and cancellations are handled). Migration 024: `push_subscriptions`, `push_outbox`. `aside-api vapid-keys` prints a key pair. Web: `static/push-sw.js`, manifest icons and iPhone home-screen tags, `lib/push.ts`, `PushToggle.svelte` on the workspace home, Settings and the buyer's booking page. Privacy notice, help, data export and account deletion cover it.
