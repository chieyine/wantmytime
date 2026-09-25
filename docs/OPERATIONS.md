# Operations status

Updated: 2026-09-24

## Implemented

- PostgreSQL connection pool, ordered transactional migrations, and persistent identity, booking, offer, notification, payment and audit records.
- MFA-protected operations routes with per-action grants, bootstrap, TOTP lockout, encrypted secrets and ten-hour MFA freshness.
- Live overview, people and detail, bookings and detail, offers, cancellations, overdue meeting links, payments and detail, settlements, payment exceptions and detail, provider events, system health, audit history, growth event aggregates and protected-settings status.
- Audited account restriction, session revocation, booking issue review, cancellation review, payment-exception review and failed provider-event retry. No balance edit or mark-paid action exists.
- Provider checkout initialization, raw-body webhook verification, durable event inbox, provider verification worker, payment exception capture, immutable allocation/ledger writes and payment notification enqueue. New provider checkouts can be emergency-paused with `CHECKOUTS_PAUSED=true`; event processing remains active while paused.
- Notification outbox worker, bounded retries, booking/meeting/reschedule messages and due reminders, when an email transport is configured. Compose routes local SMTP through Mailpit.
- Meeting-link deadline queue, encrypted booking-specific HTTPS meeting links, private ICS, participant issue reports, independent completion signals, rescheduling and cancellation requests.

## Operational limits

- Keyset cursors are available on operational list pages. Minimized payment-exception evidence exports require operations permission and are audited before return.
- Settings are intentionally read-only in the web UI. Provider, fee, approval and emergency-pause controls come from protected deployment configuration; no browser action can enable live collection.
- Normalized settlement CSV matching, minimized refund/dispute case intake, cancellation-review email jobs, attribution cohorts and raster share cards are implemented. CSV rows are provisional review evidence; only a separately verified provider source may establish a settled payout. Import requires `ops:settlement:import` in addition to recent MFA. Provider export compatibility, settlement confirmation, refund execution, dispute responses and evidence submission remain gated or unverified.
- Workers run in the API process rather than a separately deployed worker. Email and payment provider integrations require configured secrets and have not been exercised in sandbox/staging here.
- `/health` checks PostgreSQL only. Email, payment-worker and backup health are covered by the watchdog alerts below, not by `/health`.

## Owner bootstrap

After deploying a base64-encoded 32-byte `OPS_MFA_ENCRYPTION_KEY`, verify the owner account by email, create an authenticator secret, securely inject it as `OPS_BOOTSTRAP_TOTP_SECRET` for one invocation of `aside-api bootstrap-admin verified-owner@example.com`, then remove it. Configure `MEETING_LINK_ENCRYPTION_KEY` separately. The bootstrap command grants read, account restriction, session revocation, issue resolution and provisional settlement import. An owner bootstrapped before this change must rerun the command to receive the new import grant. Keep secrets out of shell arguments, logs and saved environment files.

Keep `CHECKOUTS_PAUSED=true` for incidents affecting new collections. This stops provider checkout initialization only; webhook intake and processing of existing payment events must remain available.

## Emails

Email is the record of every event. Phone and browser notifications (web push, self-hosted) add four nudges for people who switch them on: a seller's new booking and a buyer's problem report, and "your call starts in 10 minutes" for both people. They are best-effort: a push that can't be delivered is dropped, a browser that has unsubscribed is forgotten, and nothing waits on them. Every event where the other person must act, or should know, sends an email:

- **Bookings:** confirmations (with a calendar file), meeting link ready, reminders, missing meeting links.
- **Reschedules:** a request, an acceptance (with an updated calendar file), a decline.
- **Cancellations:** a request, a review.
- **Offers:** received, countered, accepted, declined, withdrawn.

Each email shows times in the recipient's own time zone, which is captured from the browser when they verify their email, and the other person's time when it differs. An email whose event has gone stale by send time is cancelled instead of sent, for example a reminder for a cancelled booking or a "new offer" email for an offer already withdrawn. Sends through Resend carry an idempotency key per queued email, so a retry after a lost response does not send twice.

## Cancellations, refunds, no-shows and reviews

- **Refunds** (Operations > Refunds, permission `ops:refund:approve`):
  - Refunds go to Kora by themselves (`REFUNDS_ENABLED` is on unless set to `false`; off, approve each one there).
  - A refund Kora never accepted is retried by itself 1, 6 and 24 hours after failing; only one still failing after that raises the `refunds_failed` alert.
  - Operators can also refund any paid booking directly, with a reason.
  - Operators bootstrapped earlier must re-run `aside-api bootstrap-admin` to receive the new permission.
- **Refund balance:** refunds come out of the platform's Kora balance. Before the payout, the seller's share comes out of the held payout; after it, it is recovered from their next payouts (see MONEY_FLOW.md).
- **Payouts** (Operations > Payouts): sellers are paid about 3 hours after each session, to a bank account or (Ghana, Kenya) a mobile money wallet, in their own currency. Payouts waiting for funds to settle or for a payout account retry by themselves. A failed or bank-returned payout emails the seller, is sent again by itself 2, 12, 24 and 48 hours after successive failures, and at once when the seller saves a different payout account (after its 24-hour safety hold); only one still failing after all of that raises `payouts_failed` (manual retry needs `ops:refund:approve`). `PAYOUTS_PAUSED=true` stops all transfers. Transfer-paid payouts go at the 3-hour mark; pay-with-bank and mobile money ones also wait for settlement (the next working day).
- **Reported problems:** a buyer's problem goes to the seller first, who has `PROBLEM_RESPONSE_HOURS` (24) to refund in full, refund part, or disagree. A refund settles it; no answer refunds the buyer in full automatically. Only a disagreement comes to you (alert `problems_open`): open the booking in Operations, read both sides, and resolve it with an outcome and, if warranted, a refund amount. The seller's payout waits until it's settled.
- **Other automatic recovery:** payment events that failed verification and emails that failed to send are put back in their queues 1, 6 and 24 hours later; their alerts fire only if they still fail. Payments that arrive with the wrong amount or currency are refunded automatically, like late and double payments.
- **Requests to cancel outside the policy** (Operations > Cancellation requests): these go to the seller, who can say yes by cancelling (the buyer gets a full refund) or let the booking stand. They no longer hold payouts and close by themselves when the booking time arrives. Operations can still see them and step in.
- **Payments that could not become a booking** (Operations > Payment exceptions and Refunds): a late transfer still gets its time if it is free. If the time was taken, the buyer paid twice, the seller stopped taking bookings or the offer had closed, the payment is refunded automatically (with `REFUNDS_ENABLED=true`; otherwise it waits in Refunds for approval) and the buyer is emailed twice: when it starts and when it is sent. Such exceptions show as `awaiting_provider` until the refund completes and then resolve themselves; they do not fire the payment-exceptions alert.
- **No-shows** (Operations > No-shows): disputed reports wait for a decision. Undisputed ones settle themselves after 24 hours through the `lifecycle` worker.
- **Reviews** (Operations > Reviews): hide only for abuse, personal data or clear falsehood.
- **Alerts:**
  - Critical: any refund or payout in `failed`.
  - Warning: refunds waiting over an hour for approval, disputed no-shows, payouts over a day late, and buyer problems holding payouts.

## Google Calendar and Meet

Sellers connect Google Calendar from Settings > Calendar and meetings.

**Busy times:**
- WantMyTime reads busy times only (not event details) from the seller's primary calendar across their booking window.
- A background worker refreshes them every 5 minutes, and again just before a time is held if the copy is over 2 minutes old.
- If Google can't be reached, the last known busy times still block slots.

**Events and Meet links:**
- Each confirmed booking becomes a private event on the seller's calendar, with no guests, so no email addresses are shared.
- The event moves when the booking is rescheduled and is removed if the booking stops being confirmed.
- Unless the seller already added their own link, Google creates a Meet link, which is saved as the booking's meeting link and emailed to the buyer.
- Because the buyer isn't a guest, the seller admits them from the call.

**Failures:** a revoked grant shows on the seller's page with a reconnect button, and their calendar jobs stop. The `calendar` worker appears under System health and raises the usual stalled-worker alert. Connect and disconnect actions are recorded in the audit log.

## Seller holds

Sellers open for bookings on their own once the bank confirms their payout account. To stop a seller taking new bookings, open them under Operations → People and choose **Put on hold** with a reason; existing bookings stay. Only an operator can lift a hold. Restricting the account (also under People) goes further and signs them out.

## Funnels

Operations → Growth shows two funnels for the last 7, 30 or 90 days (`GET /api/v1/ops/funnels?days=`):

- Buyers: viewed a booking page → opened the time picker → picked a time → held the time → opened payment → paid → the session took place.
- Sellers who claimed a link in the period: claimed → set hours → added a bank account → copied or shared the link → first booking → first paid booking, plus the median time from claiming to the first paid booking.

The first three buyer steps are browser events (`public_link_viewed`, `booking_started`, `slot_selected`), sent once per page visit with no names, emails or amounts; ad blockers make them undercount, so a later step can exceed an earlier one. Every later step is read from the holds, payment attempts and bookings themselves. Outside production, simulated payments count as paid (the page says so).

## Data requests and retention

People download their data and delete their accounts themselves from Settings → Your data; the API records each request in `data_requests` and each deletion in the audit log (`account.deleted`). For a request by email to privacy@wantmytime.com, verify the sender controls the account's address before acting, and reply within 30 days. If a deletion is blocked, the page tells the person what is still open (an upcoming or recent session, a payout on its way, a refund, an unrecovered refund, an open report); resolve that first. An operator with an active grant cannot delete their own account until the grant is revoked.

Deleted accounts keep their ID with `status='deleted'`; their link is renamed `deleted-<id>` and the old name is held in `handle_holds` for 180 days.

The lifecycle worker runs a retention sweep at most once an hour (in batches, so it never holds long locks):

| Data | Rule |
|---|---|
| Sign-in codes | deleted one day after expiry |
| Sessions | deleted 30 days after they expire or are revoked |
| Calendar sign-in states, past busy times | deleted after a day |
| Idempotency replies | deleted after 30 days |
| Product analytics | deleted after 400 days |
| Names on expired holds | cleared after 90 days |
| Emails on closed offers | cleared after 180 days |
| Meeting links | cleared 30 days after the session |
| Data-request log | deleted after two years |
| Expired link holds | deleted |

Payment, refund, payout, ledger, booking and audit records are kept for six years (tax and anti-money-laundering records). Nothing deletes them automatically yet; schedule a review before the first records reach that age. Backups keep deleted data for `BACKUP_RETENTION_DAYS` (default 14) plus any off-site bucket retention, which the privacy notice states.

A personal-data breach must be reported to the Nigeria Data Protection Commission within 72 hours of becoming aware of it, and to affected people without delay when it puts them at high risk. Record what happened, what data, how many people and what was done, even when no report is needed.

## Logs and request IDs

The API writes one JSON line per request and per event to stdout (`time`, `level`, `msg`, `request_id`, `route`, `status`, `duration_ms`). The web server and the gateway do the same. Nginx creates the request ID, passes it to the web server and the API as `X-Request-ID`, and returns it on every response. When a user reports "Something went wrong (Reference: 3f2a…)", search all logs for that value to see the gateway, web and API lines for the same request. `LOG_LEVEL=debug` also logs `/health` and `/metrics` hits.

## Error reporting

Set `SENTRY_DSN` (API and web server) and `PUBLIC_SENTRY_DSN` (browser) to a Sentry project; the free tier is enough. Without them errors are still logged. Reports include the route pattern, status, request ID and a stack trace, but never request bodies, headers, cookies or query strings. The same error is reported at most once a minute. Error-level log lines are also reported, with their attributes as tags, so do not put personal data in error log attributes. Set `APP_RELEASE` (for example the git commit) to see which release introduced an error.

## Metrics

`GET /metrics` on the API returns Prometheus text when `METRICS_TOKEN` is set, and requires `Authorization: Bearer <token>`. It returns 404 when the token is not set, and the gateway always returns 404 for `/metrics`, so scrape the API port over the private network. It includes request counts and latency by route, plus gauges for payment events and emails by state, open provider cases, active holds, bookings in the next 24 hours, firing alerts and worker heartbeat age. Grafana Cloud's free tier or any Prometheus can scrape it. Suggested dashboard panels: 5xx rate by route, p95 latency, `aside_alerts_firing`, `aside_worker_heartbeat_age_seconds`.

Application counters: `aside_rate_limited_total{limiter}` (requests refused by each limit) and `aside_rate_limit_redis_errors_total{limiter}` (checks that fell back to the local count because Redis failed). A steady rise in the second means Redis is down or unreachable.

## Alerts

A watchdog in the API checks conditions every minute. Only one instance evaluates at a time (PostgreSQL advisory lock). Each alert is emailed to `ALERT_EMAILS` (comma-separated) when it starts, again every 6 hours while it lasts (sooner if a count gets worse), and once more when it resolves. Alert emails need a working email provider. Operations > System health shows firing alerts, recently resolved ones and worker check-ins.

| Alert | Fires when | First response |
| --- | --- | --- |
| Payment events failed verification | A Kora event exhausted its retries | Operations > Provider events; a buyer may have paid without a booking |
| Payment events are not being processed | The oldest pending event is over 15 minutes old | Check the API is running and can reach Kora |
| Payment exceptions need review | An exception still `open` or `investigating` (automatic refunds in progress are not counted) | Operations > Payment exceptions |
| Dispute or refund deadline approaching | A provider case is due within 72 hours | Operations > Disputes and refunds |
| Bookings without a meeting link | With automatic links on (default): a booking passed its link deadline and no link could be created (check `MEETING_LINK_ENCRYPTION_KEY`). With them off: a booking starts within 2 hours with no link | Operations > Meeting delivery; contact the seller |
| Emails failed to send | An email failed every retry in the last 24 hours | Check the email provider account and API logs |
| Emails are not being sent | The oldest due email waited over 30 minutes | Check the provider and the notification worker |
| Background worker stopped | A worker has not finished a cycle for 3 minutes | Restart the API and read its logs |
| Database backup failed | The latest backup or its restore check failed | Run the backup by hand and read its output (below) |
| Database backups have stopped | No verified backup for 26 hours | Check the backup service is running |

## Backups and restore

`infrastructure/backup/backup.sh` takes a compressed `pg_dump`, writes a SHA-256 checksum, then restores it into a throwaway database with `verify-restore.sh`. That check confirms the core tables exist, migrations are recorded, every ledger journal balances, bookings reference real sellers, and the copy never holds more ledger entries than the live database. Only then is the backup kept under its normal name. A dump that fails is renamed `*.dump.unverified`. It optionally copies the dump off-site (`BACKUP_S3_URI`, which works with Cloudflare R2), prunes dumps older than `BACKUP_RETENTION_DAYS` (the newest is always kept), and records the result in `worker_heartbeats`, which drives the two backup alerts.

- Daily in Compose: `docker compose --profile ops up -d backup`. Run once now: `docker compose run --rm backup once`. Re-check an old dump: `docker compose run --rm backup verify /backups/<file>`.
- Anywhere else, run `DATABASE_URL=... BACKUP_DIR=... infrastructure/backup/backup.sh` from cron as a non-root user with PostgreSQL client and server tools installed, so the restore check can start a temporary local cluster. Alternatively, set `RESTORE_DATABASE_URL` to a scratch server where the role can create databases. Do not point it at the production server.
- Managed PostgreSQL: also turn on the provider's point-in-time recovery (at least 7 days). PITR covers the minutes before an incident; these dumps are the independent, verified copy that survives losing the provider account. Keep the off-site bucket in a different account, with object versioning or a retention lock.

Restore drill (do it once before launch and then quarterly):

1. Pick the newest `aside-*.dump` that has a matching `.sha256` file. Never pick an `.unverified` one.
2. Create a new, empty database. Never restore over the live one.
3. `infrastructure/backup/restore.sh <dump> <new-database-url> --yes`. It checks the checksum and refuses a database that already has tables.
4. Point a staging API at it, run `aside-api migrate`, sign in to Operations, and compare recent bookings and payments with Kora's dashboard.
5. For a real recovery, keep `CHECKOUTS_PAUSED=true` and switch `DATABASE_URL` to the restored database. Then use Operations > Provider events to reconcile any Kora events received after the backup time; Kora retries webhooks for up to 72 hours, and each charge can be looked up by its reference for that window. Only then unpause checkouts.
6. Record the dump time, restore duration and any gaps.
