# Launch checklist

Updated: 2026-09-25

The product code is complete for buyers and sellers in every flow (see [FLOW_WALKTHROUGH.md](FLOW_WALKTHROUGH.md)). What is left needs you: accounts, approvals, secrets, legal sign-off and switching things on. Nothing below needs more code.

## 1. Kora (payments and payouts)

- [ ] Merchant account approved for this model in writing: WantMyTime collects the full payment, holds it, and pays sellers out from the balance after each session (ask whether a marketplace or payment-facilitator arrangement is needed).
- [ ] Sandbox keys (`sk_test_`, `pk_test_`) in the staging environment.
- [ ] Refunds by API allowed on the account (WantMyTime sends refunds automatically by default).
- [ ] Ask Kora to waive or reduce the standard 10% rolling reserve (held 180 days): WantMyTime takes no cards, so there are no card chargebacks. If it stays, keep enough of your own money in the Kora balance to cover it.
- [ ] Webhook URL set to `https://wantmytime.com/api/v1/webhooks/kora`; dashboard defaults kept for short and excess transfers (return them).
- [ ] Transaction limits set (`MIN_CHARGE_MINOR`, `MAX_CHARGE_MINOR`). Optionally record Kora's fee per channel so buyers see an estimate first (DEPLOYMENT.md); Kora adds its actual fee itself.
- [ ] In the sandbox, confirm that a seller-cancellation refund of the price plus Kora's fee goes through (the buyer is promised the fee back when the seller is at fault).
- [ ] Sandbox run-through of FLOW_WALKTHROUGH.md: transfer, pay with bank, late transfer, double payment, cancellation refunds, seller no-show refund, payout, failed payout and retry, name check on a bank account.
- [ ] Live keys and your approval reference (`PAYMENT_APPROVAL_ID`) when approved.
- [ ] Later, per extra country (Ghana, Kenya): Kora enables the currency; confirm the mobile money and payout endpoints in PROVIDER_CAPABILITIES.md in the sandbox; set channels and limits; add it to `SELLER_COUNTRIES`.

## 2. Legal and company

- [ ] `support@wantmytime.com` and `privacy@wantmytime.com` mailboxes set up and watched.
- [ ] Data processing agreements with Kora, Cloudflare, the email provider, the error-reporting service and the host; check whether WantMyTime must register with the Nigeria Data Protection Commission and file if so.
- [ ] Confirm the fee (5%), the 2-hour problem window, the 3-hour payout time and the three cancellation policies as final.

## 3. Email

- [ ] Sendly account; verify `wantmytime.com` (SPF, DKIM, DMARC). Rotate any API key that was ever pasted into a chat or document.
- [ ] `EMAIL_PROVIDER=sendly`, `EMAIL_API_KEY` (in the secret manager only), `EMAIL_FROM="WantMyTime <bookings@wantmytime.com>"`, and `EMAIL_REPLY_TO=support@wantmytime.com` so people can reply.
- [ ] In Sendly's dashboard: set the reply-to address (Settings → sender defaults) to `support@wantmytime.com`, and create SMTP relay credentials for announcements (`SENDLY_SMTP_USERNAME`, `SENDLY_SMTP_PASSWORD`). Use a `sk_test_` key in staging.

- [ ] Phone notifications: run `aside-api vapid-keys` once and store the two keys it prints as `VAPID_PUBLIC_KEY` and `VAPID_PRIVATE_KEY` (never change them afterwards).

## 4. Hosting and data

- [ ] Choose the host; production PostgreSQL (with point-in-time recovery), Redis, and two Cloudflare R2 buckets (photos, backups).
- [ ] Generate every secret and key (`openssl rand -base64 32` / `48`) into the secret manager; the API refuses to start in production with weak, reused or missing ones.
- [ ] In Vercel → Domains, make `wantmytime.com` the primary domain and redirect `www.wantmytime.com` to it (today it is the other way round). Canonical links, the sitemap, Kora's return address and Google's callback all use `wantmytime.com`; sign-in works either way since the API also accepts the `www` twin.
- [ ] `aside-api migrate` (migrations 001 to 028), then deploy the API, web app and gateway with TLS on `wantmytime.com`.
- [ ] Backups running and one restore drill done (OPERATIONS.md).
- [ ] CI green on the pull request (`.github/workflows/ci.yml`: lint, type check, build, Go unit and integration tests, end-to-end tests). To run it locally, see the README.

## 5. Operations

- [ ] Sign in with your email, then run `aside-api bootstrap-admin <your email>` with your authenticator secret; verify at `/ops/access`.
- [ ] `ALERT_EMAILS` set to where alerts should go; `SENTRY_DSN` and `PUBLIC_SENTRY_DSN` if you want error reports.
- [ ] Announcements: run `bootstrap-admin` again (same authenticator secret) so your account gets `ops:marketing:send`; set `EMAIL_MARKETING_FROM` to a sender on its own subdomain (for example `news@news.wantmytime.com`) verified with SPF, DKIM and DMARC.
- [ ] Legal check of the news consent: the box is pre-ticked, which GDPR does not accept and the NDPA may not either. Unticking it by default is a one-line change (`MarketingConsent.svelte`).

## 6. Optional: Google Calendar and Meet

- [ ] Google Cloud OAuth client, consent screen and app verification (DEPLOYMENT.md). Without it, sellers paste links and WantMyTime creates call links automatically.

## 7. Switch on

- [ ] Staging: `PAYMENTS_ENABLED=true`, `FEE_POLICY_APPROVED=true`, `PAYMENT_ENV=sandbox`, `CHECKOUTS_PAUSED=false`; do the sandbox run-through.
- [ ] Production: `PAYMENT_ENV=live`, `LIVE_PAYMENTS_ENABLED=true`, live keys, `PAYMENT_APPROVAL_ID`, then `CHECKOUTS_PAUSED=false`.
- [ ] Look through the public, booking, workspace and operations pages on a phone and a laptop.
