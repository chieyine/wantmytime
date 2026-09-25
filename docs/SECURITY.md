# Security status

Updated: 2026-09-24. This is not ready for public launch or real payment data.

- Live payment checkout and webhook processing are fail-closed; no provider credentials were supplied.
- Email challenges are hashed, one-time, attempt-limited and rate-limited by email. Verified identities receive revocable HttpOnly sessions; public buyer sessions carry resource-limited guest scopes and cannot enter account or operations routes. Mutating browser requests require an allowed Origin; signed Kora webhook POSTs are the server-to-server exception and still require HMAC validation.
- Profile edits and private bookings/offers require authenticated ownership/participant scope. Booking and offer submissions use idempotency and PostgreSQL transactions; schedule overlaps are constrained in the database.
- Meeting URLs are HTTPS-only, encrypted at rest with a separate key, returned only to booking participants and never placed in public pages or calendar files.
- Ops requires verified identity, an active permission grant and recent MFA. TOTP secrets are encrypted at rest, repeated bad codes lock the account temporarily, sensitive account/session/issue actions require reasons and audit in the same transaction, and audit rows reject update/delete at the database layer.
- Payment verification stores only the issuing country and brand of the paying card, never card numbers, BINs or last-four digits. Booking emails never include the other person's email address, and cancellation reasons stay with the review team. Names in email subjects are stripped of control characters.
- Google Calendar:
  - The connection uses OAuth with PKCE. The `state` value is single-use, expires after 10 minutes and is bound to the signed-in account that started the flow.
  - Only the free/busy and owned-events scopes are accepted. A partial grant is revoked.
  - Refresh tokens are AES-GCM encrypted with a dedicated key and bound to the seller, so a copied ciphertext is useless. Access tokens are held only in memory.
  - Disconnecting revokes the grant at Google.
  - WantMyTime never reads event titles or details, and never adds guests.
- Refunds:
  - They can only go back to the original payment, via Kora, and never exceed the price.
  - One live refund is allowed per booking, enforced by the database.
  - Cancellation confirms against the refund amount shown.
  - Operator refund actions need their own permission, a reason, and an audit record in the same transaction.
- Payments: Kora webhooks must carry a valid HMAC-SHA256 signature of their `data` object; nothing in a webhook is trusted beyond the reference, and every charge is confirmed by looking it up with Kora.
- Payouts:
  - Money only goes to the seller's own bank account, saved after the bank confirms the account name. The account number is encrypted (AES-GCM, bound to the seller); only the last four digits, the bank and the name are readable.
  - A replaced bank account waits 24 hours before it is paid into; account lookups are limited to 20 an hour per person; every change is audited.
  - Every transfer attempt has a unique reference and is looked up before sending, so a retry never pays twice. Operators cannot choose where a payout goes or mark one paid; they can only retry a failed one to the seller's current account, with a reason.
  - A buyer's problem report holds the payout; the buyer's reporting window closes 2 hours after the session (configurable).
- Reviews are limited to the buyer of a booking that took place. They show only the reviewer's first name, and hiding them is audited.
- Simulator records are explicitly marked synthetic and create no allocations or settlement records.

- Rate limits, per client address, shared by every API instance through Redis (`REDIS_URL`): sign-in and account deletion 30 a minute, holds, offers and checkout 20 a minute, profile photo uploads 10 per 10 minutes, public profile reads 300 a minute, analytics 120 a minute, and every other change 120 a minute. Kora webhooks are exempt (they are signature-checked). Redis keys hold a hash of the address, never the address. If Redis stops answering, each instance falls back to its own count within 250 ms, so limits never switch off; the fallback is counted in `aside_rate_limit_redis_errors_total`. Behind the bundled gateway set `TRUST_PROXY_HEADERS=true`. Authenticator codes cannot be replayed within their time window.
- Pay-first bookings. A buyer who hasn't confirmed their email gets a session limited to the one booking they are making (purpose `guest_booking`, flag `unverified`): it can hold that time, pay and view that booking, and cannot reach accounts, offers, data export or other bookings. Someone could type another person's email; the worst they can do is pay for a booking that emails that address, or briefly use up that person's three unpaid holds. At most 10 such sessions per email per hour, plus route limits. A code later claims the same account.
- Security headers. Web pages carry a Content Security Policy with a per-request nonce on every script (no inline script runs without one), `frame-ancestors 'none'`, `form-action 'self'`, `object-src 'none'`, plus `X-Frame-Options`, `nosniff`, a strict referrer policy, `Cross-Origin-Opener-Policy` and a Permissions Policy that turns off camera, microphone, location and payment APIs. The error-reporting and public photo origins are added from the environment. API responses carry `default-src 'none'`. HSTS (two years, subdomains, preload) is sent in production.
- Production start-up checks. In `APP_ENV=production` the API refuses to start if a secret is missing, short, still a placeholder or reused for two purposes, if an encryption key is not 32 bytes, if `PUBLIC_APP_ORIGIN` is not https, or if a test switch (`LOCAL_PAYMENT_SIMULATOR`, `ALLOW_LOG_OTP`, `KORA_API_BASE`, Google base overrides) is set. Missing Redis, object storage, database TLS or error reporting is logged as a warning.
- Profile photos are decoded and re-encoded (dropping metadata) before storage. With Cloudflare R2 configured they live in the bucket under unguessable names; the R2 token should be limited to that one bucket.
- Data rights (Nigeria Data Protection Act). People can download everything held about them (limited to three downloads a day, with full account numbers, codes and keys left out) and delete their account from Settings → Your data. Deletion is refused while money is still moving or a session or report is open, and needs the email typed out. It erases name, email, photo, bank account, calendar connection, availability, sessions and analytics, withdraws open offers, strips the person's name from bookings others keep, and holds the link for 180 days so nobody can impersonate them. Payment, refund, payout and ledger rows stay for the legal retention period without identifiers. An hourly sweep deletes expired codes, ended sessions, old idempotency replies and analytics, and strips names and meeting links from old records (see OPERATIONS.md).
- A buyer can hold at most three unpaid times at once; picking a new time with the same seller replaces the earlier hold.
- Error reports carry route, status, request ID and stack only (no bodies, headers, cookies or query strings); `/metrics` needs a bearer token and is blocked at the gateway. A 500 response tells the user only a reference ID. Backup dumps contain all personal and payment data: keep the backup volume and off-site bucket private, encrypted at rest and in a separate account.

Remaining work before beta includes device-level abuse limits, security-event controls and broader redacted logging. Configure email and all encryption/session secrets in a secret manager. Complete staging browser, authorization, restore and provider lifecycle verification. Schema constraints are useful evidence, but not a substitute for those checks.
