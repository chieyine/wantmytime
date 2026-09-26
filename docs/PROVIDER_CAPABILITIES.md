# Provider capabilities and gates

Updated: 2026-09-25

## Kora (payments and payouts)

- Chosen because bank transfers settle into the Kora balance instantly, and payouts to Nigerian banks go out from that balance by API. Kora also collects by mobile money in Ghana, Kenya and other African markets. WantMyTime takes no card payments, which suits later expansion.
- Present code: environment-matched Kora client (`kora.go`); transfer-first checkout with a one-off account per payment shown in WantMyTime and confirmed automatically; hosted pay-with-bank and mobile money checkout; charge lookup that counts only the accepted amount; HMAC-SHA256 webhook signature over the `data` object; minimized durable webhook inbox and retrying verification worker; refunds by WantMyTime reference; payouts with bank list, account-name check, reference-first transfer and lookup; sealed seller account numbers.
- Checked against Kora's public documentation on 2026-09-24. Endpoint paths used: `/api/v1/charges/initialize`, `/api/v1/charges/bank-transfer`, `/api/v1/charges/{reference}`, `/api/v1/refunds/initiate`, `/api/v1/refunds/{reference}`, `/api/v1/transactions/disburse`, `/api/v1/transactions/{reference}`, `/api/v1/misc/banks`, `/api/v1/misc/banks/resolve`. The charge-lookup and payout-lookup paths and some response field names (`amount_accepted`) were not shown verbatim in the public docs; confirm them in the sandbox before launch.
- Added 2026-09-25 for other countries (confirm in the sandbox before switching a country on with `SELLER_COUNTRIES`):
  - Hosted checkout (`/charges/initialize`) with `currency` GHS or KES and channel `mobile_money`.
  - Mobile money payouts: `/transactions/disburse` with `destination.type = "mobile_money"` and `destination.mobile_money = { operator, mobile_number }`. Operator codes used: `mtn-gh`, `vodafone-gh`, `airteltigo-gh`, `safaricom-ke`, `airtel-ke`; wallet numbers are sent in international form (2547…, 233…).
  - Bank payouts in GHS and KES with bank codes from `/misc/banks?countryCode=GH|KE`. Account-name resolution is used only for Nigerian accounts (`/misc/banks/resolve` takes the currency, `NGN`; the earlier code sent the country code, which is now fixed). Outside Nigeria the seller types the name on the account.
  - Settlement timing for mobile money and pay with bank (`SETTLEMENT_WAIT_HOURS`, default 24, weekends skipped).
- Sandbox evidence: none yet; tests run against a local fake of the API above.
- Commercial approval: none. Confirm with Kora that holding buyers' payments and paying sellers from the balance is permitted on the account (and whether a marketplace or payment-facilitator arrangement is required), plus fees, settlement timing and any licensing questions, before enabling live payments.
- Live collection remains disabled until `PAYMENTS_ENABLED`, fee approval, live keys and `PAYMENT_APPROVAL_ID` are all set. Each currency also needs its channels and limits before its sellers take payments. Kora adds its own fee for the buyer (`merchant_bears_cost=false`); confirm in the sandbox that the transfer response's `amount_expected` includes it and how the charge lookup reports `amount`.
- Refunds by API (`REFUNDS_ENABLED=true`) must be allowed on the account: automatic refunds cover buyer and seller cancellations, seller no-shows and payments that could not become a booking.

## Email

- Sendly (sendlyai.com) is the chosen provider: `EMAIL_PROVIDER=sendly`, `EMAIL_API_KEY` and a verified `EMAIL_FROM`. Field names follow https://developer.sendlyai.com/docs.
  - Transactional email (sign-in codes, booking emails, alerts) goes to `POST https://api.sendlyai.com/v1/messages` with `channel`, `to`, `from`, `subject`, `html`, `text`, `attachments` (`filename`, base64 `content`, `contentType`; used for the booking calendar file), `tracking: false` (private booking links are never rewritten) and an `Idempotency-Key` header, so a retry never sends twice. Plain-text emails are also sent as escaped HTML.
  - Reply-to is not an API field: set it once in Sendly's dashboard (Settings → sender defaults) to the same address as `EMAIL_REPLY_TO`.
  - Announcements need our own `List-Unsubscribe` headers, which the HTTP API does not take, so they go through Sendly's SMTP relay (`smtp.sendlyai.com:587`, STARTTLS required, PLAIN auth) when `SENDLY_SMTP_USERNAME` and `SENDLY_SMTP_PASSWORD` are set. Without them, announcements still send over HTTP with the unsubscribe link in the footer, but without the one-click headers Gmail and Yahoo expect, and the API logs a warning.
  - Sendly's own `unsubscribe: true` is deliberately never used: it adds the person to Sendly's suppression list, which skips every later email to them, including sign-in codes and booking emails.
  - Hard bounces and spam complaints land on Sendly's suppression list and are skipped by Sendly. A `sk_test_` key runs the whole request without delivering, which suits staging.
- Resend remains available with `EMAIL_PROVIDER=resend` and supports all of the above.
- Local SMTP is restricted to loopback. OTP logging requires explicit `ALLOW_LOG_OTP=true` in non-production.
- Delivery credentials and verified sender domain remain external launch gates.

## Database

- PostgreSQL schema, pgxpool and versioned transactional migration runner are implemented. Apply migrations explicitly with `aside-api migrate` before starting application instances.
- Schema application and API startup were verified on an isolated temporary PostgreSQL database on 2026-09-23. Restore rehearsal and production database provisioning remain open.

## Meetings

Private seller-supplied links remain the selected delivery method. If none exists at the deadline (30 minutes before the start), WantMyTime creates a Jitsi Meet link (`MEETING_LINK_BASE`, `AUTO_MEETING_LINKS`) and emails it to both people; meet.jit.si can ask the first person to join to sign in before the call starts, so set `MEETING_LINK_BASE` to a self-hosted Jitsi or similar for no sign-in at all. Booking-specific encrypted link entry, participant delivery, deadline reminders, private ICS, rescheduling and completion signals are implemented. Sellers can optionally connect Google Calendar (free/busy and owned events only) to block busy times, mirror bookings as private events, and get a Google Meet link per booking. A link the seller adds by hand always takes priority. See docs/OPERATIONS.md.
