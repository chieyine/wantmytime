# Provider capabilities and gates

Updated: 2026-09-24

## Kora (payments and payouts)

- Chosen because bank transfers settle into the Kora balance instantly (cards the next working day), and payouts to Nigerian banks go out from that balance by API. Kora also operates in Ghana, Kenya, South Africa and other African markets, which suits later expansion.
- Present code: environment-matched Kora client (`kora.go`); transfer-first checkout with a one-off account per payment shown in WantMyTime and confirmed automatically; hosted card checkout as a fallback; charge lookup that counts only the accepted amount; HMAC-SHA256 webhook signature over the `data` object; minimized durable webhook inbox and retrying verification worker; refunds by WantMyTime reference; payouts with bank list, account-name check, reference-first transfer and lookup; sealed seller account numbers.
- Checked against Kora's public documentation on 2026-09-24. Endpoint paths used: `/api/v1/charges/initialize`, `/api/v1/charges/bank-transfer`, `/api/v1/charges/{reference}`, `/api/v1/refunds/initiate`, `/api/v1/refunds/{reference}`, `/api/v1/transactions/disburse`, `/api/v1/transactions/{reference}`, `/api/v1/misc/banks`, `/api/v1/misc/banks/resolve`. The charge-lookup and payout-lookup paths and some response field names (`amount_accepted`, card issuer country) were not shown verbatim in the public docs; confirm them in the sandbox before launch.
- Sandbox evidence: none yet; tests run against a local fake of the API above.
- Commercial approval: none. Confirm with Kora that holding buyers' payments and paying sellers from the balance is permitted on the account (and whether a marketplace or payment-facilitator arrangement is required), plus fees, settlement timing and any licensing questions, before enabling live payments.
- Live collection remains disabled until `PAYMENTS_ENABLED`, fee approval, live keys and `PAYMENT_APPROVAL_ID` are all set.

## Email

- Resend adapter is available with `EMAIL_PROVIDER=resend`, `EMAIL_API_KEY` and a verified `EMAIL_FROM`; it has not been exercised without credentials.
- Local SMTP is restricted to loopback. OTP logging requires explicit `ALLOW_LOG_OTP=true` in non-production.
- Delivery credentials and verified sender domain remain external launch gates.

## Database

- PostgreSQL schema, pgxpool and versioned transactional migration runner are implemented. Apply migrations explicitly with `aside-api migrate` before starting application instances.
- Schema application and API startup were verified on an isolated temporary PostgreSQL database on 2026-09-23. Restore rehearsal and production database provisioning remain open.

## Meetings

Private seller-supplied links remain the selected delivery method. Booking-specific encrypted link entry, participant delivery, deadline reminders, private ICS, rescheduling and completion signals are implemented. Sellers can optionally connect Google Calendar (free/busy and owned events only) to block busy times, mirror bookings as private events, and get a Google Meet link per booking. A link the seller adds by hand always takes priority. See docs/OPERATIONS.md.
