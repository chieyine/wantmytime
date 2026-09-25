# Deployment status

No production deployment is configured or authorized. SvelteKit uses pinned `@sveltejs/adapter-node` and the root Dockerfile builds a standalone Node server. `infrastructure/nginx.conf` provides a same-origin gateway for `/api/v1/*` and the web app; Compose exposes it locally on `127.0.0.1:5173`. Public-profile SSR and authenticated app server loads use `API_INTERNAL_BASE`. `services/core/Dockerfile` builds a non-root API image.

Before production deployment, select a hosting platform, provide TLS at the trusted ingress, configure production origins, PostgreSQL, secrets and email/provider settings per environment, build and scan container images, add health checks and rollback/migration instructions, then complete staging restore and payment-provider verification. Apply schema changes as a deliberate release step by running `aside-api migrate` once before rolling out API instances; the API no longer migrates on startup. Configure `ORIGIN`, `PUBLIC_APP_ORIGIN`, `API_INTERNAL_BASE`, `SESSION_SECRET`, `OTP_PEPPER`, `OPS_MFA_ENCRYPTION_KEY` and `MEETING_LINK_ENCRYPTION_KEY` through a secret manager. `PAYMENTS_ENABLED` defaults false; sandbox requires an `sk_test_` key, approved fee schedule and channel list. Production additionally requires `PAYMENT_ENV=live`, `LIVE_PAYMENTS_ENABLED=true`, an `sk_live_` key, approved Kora fee schedules, live Kora keys, `PAYOUT_ACCOUNT_ENCRYPTION_KEY`, `FEE_POLICY_APPROVED=true`, approved return origin and a non-empty `PAYMENT_APPROVAL_ID`. Keep `CHECKOUTS_PAUSED=true` until the launch is approved; this blocks new provider checkout initialization but leaves signed webhook intake and processing for existing payments active. Never set live switches or remove the pause before documented owner, legal and provider approval. No domain or provider credentials were supplied. Docker was not run during this audit/fix pass.

After migration `012_audit_fixes.sql`, re-run `aside-api bootstrap-admin <email>` for existing operators (with `OPS_BOOTSTRAP_TOTP_SECRET` set to their current secret) to add the new `ops:seller:approve` permission, which gates the audited payout-readiness action. Set `TRUST_PROXY_HEADERS=true` only when the API is reachable exclusively through the gateway. `CHECKOUT_HOLD_MINUTES` (10–60, default 30) sets how long a time stays held once provider checkout opens.

Observability settings (all optional; see OPERATIONS.md):

- `LOG_LEVEL`
- `APP_RELEASE`
- `SENTRY_DSN`
- `PUBLIC_SENTRY_DSN`, `PUBLIC_APP_ENV`, `PUBLIC_APP_RELEASE` (web)
- `METRICS_TOKEN`
- `ALERT_EMAILS`

Keep `/metrics` on the private network; the gateway already blocks it. Migration `013_observability.sql` adds `worker_heartbeats` and `ops_alerts`. Enable the `backup` Compose profile or an equivalent scheduled job, plus provider point-in-time recovery, before taking real payments.

Domain. The product runs at `https://wantmytime.com`. Set `PUBLIC_APP_ORIGIN=https://wantmytime.com` for the API and `ORIGIN=https://wantmytime.com` for the web server, point the apex (and `www`, redirected to the apex) at the gateway with TLS, and use this origin wherever a provider needs an address: Kora webhooks go to `https://wantmytime.com/api/v1/webhooks/kora`, Google's redirect URI is `https://wantmytime.com/api/v1/integrations/google/callback`. Verify the domain with the email provider (SPF, DKIM, DMARC) and send from an address on it, for example `EMAIL_FROM="WantMyTime <bookings@wantmytime.com>"`.

Payments (Kora). Create a Kora merchant account and set `KORA_SECRET_KEY` and `KORA_PUBLIC_KEY` (`sk_test_`/`pk_test_` outside production, `sk_live_`/`pk_live_` in production), `PAYMENT_ROUTE=escrow_payout` and `APPROVED_PAYMENT_CHANNELS=bank_transfer` (transfer only for now; add `,card` later to offer cards as a fallback). Kora sends webhooks to `<PUBLIC_APP_ORIGIN>/api/v1/webhooks/kora`, which WantMyTime passes with each charge; make sure the gateway forwards that path to the API. In the Kora dashboard, keep the defaults for bank-transfer underpayments (return all) and overpayments (return excess). Set `PAYOUT_ACCOUNT_ENCRYPTION_KEY` (base64 of 32 random bytes) in production; it protects sellers' account numbers and must never change without re-encrypting them. Approve one fee schedule per channel, recording who approved it (Kora's fee including VAT, in basis points):

```sql
INSERT INTO provider_fee_schedules(id,provider,currency,channel,percent_bps,fixed_minor,cap_minor,effective_from,approved_at,approved_by)
VALUES (gen_random_uuid(),'kora','NGN','bank_transfer',<bps>,<fixed kobo>,<cap kobo or NULL>,now(),now(),'<owner user id>');
```

International cards (only if cards are switched on later). Cards issued abroad cost more than local ones, and the checkout cannot know which card a buyer will use, so a foreign card on a small booking can cost more than the platform fee. To accept them, enable international payments on the Kora account, set `INTERNATIONAL_CARDS_ENABLED=true` and approve a `card_international` schedule the same way. Verification then accepts a foreign-card payment whose fee is within that schedule; the platform absorbs the difference and the seller's share never changes. Anything else stops as a payment exception for review. Buyers are told on the checkout page that they are charged in Naira.

Migrations `014_notification_events.sql` (offer, reschedule-request and cancellation-request emails) and `015_international_cards.sql` (card country and brand on payment attempts) apply with `aside-api migrate`. `EMAIL_REPLY_TO` optionally sets a support reply address on booking emails.

Google Calendar and Meet (optional; the feature is hidden until configured):

1. In Google Cloud, create a project, enable the **Google Calendar API**, and set up the OAuth consent screen (external). Add the scopes `openid`, `email`, `.../auth/calendar.freebusy` and `.../auth/calendar.events.owned`. Link the privacy page, which includes the required Limited Use statement.
2. Create an OAuth client of type **Web application** with the authorised redirect URI `<PUBLIC_APP_ORIGIN>/api/v1/integrations/google/callback`.
3. Set `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` and `CALENDAR_TOKEN_ENCRYPTION_KEY` (base64 of 32 random bytes, for example `openssl rand -base64 32`). Keep all three in the secret manager.
4. Apply migration `016_google_calendar.sql`. The calendar worker starts automatically when these are set.

Calendar scopes are sensitive. Until Google verifies the app, sellers see an "unverified app" warning and the app is limited to test users (a small cap). Submit the app for verification before opening sign-ups. Rotating `CALENDAR_TOKEN_ENCRYPTION_KEY` makes existing connections unreadable: sellers would need to reconnect.

Redis (shared rate limits). Set `REDIS_URL` to a Redis 6+ instance reachable only from the API, for example a managed Upstash or Redis Cloud database (`rediss://default:<password>@<host>:<port>`, TLS) or the Compose `redis` service (`redis://redis:6379/0`). It holds only short-lived counters, so it needs no persistence or backups; 64 MB is plenty. Without it, limits are counted per API instance.

Cloudflare R2 (profile photos). Create a bucket (for example `wantmytime-media`) and an R2 API token with Object Read & Write on that bucket only. Set `MEDIA_S3_ENDPOINT=https://<account id>.r2.cloudflarestorage.com`, `MEDIA_S3_BUCKET`, `MEDIA_S3_ACCESS_KEY_ID` and `MEDIA_S3_SECRET_ACCESS_KEY` (region defaults to `auto`). To serve photos from Cloudflare's edge, connect a custom domain to the bucket (for example `media.wantmytime.com`) and set it as `MEDIA_PUBLIC_BASE_URL` on the API and `PUBLIC_MEDIA_BASE_URL` on the web server (the web server adds it to the Content Security Policy). Photo addresses stay `/api/v1/people/<handle>/avatar?v=<n>`; the API redirects to the bucket. Without a public domain the API streams photos from the private bucket. Without R2 at all, photos stay in PostgreSQL. Existing photos in PostgreSQL keep working; they move to R2 when the person next uploads one.

Off-site backups use R2 too, through the backup job's `BACKUP_S3_URI` (`s3://<bucket>/<prefix>`), `BACKUP_S3_ENDPOINT` (the same account endpoint) and a separate token scoped to a separate backups bucket. Turn on object lock or versioning for that bucket.

Start-up checks. With `APP_ENV=production` the API validates its configuration before serving and exits with a list of problems (weak, placeholder or reused secrets; bad key lengths; non-https origin; test switches left on). Generate each key with `openssl rand -base64 32` and each secret with `openssl rand -base64 48`, and never reuse one value for two settings.

Security headers are set by the web server and the API themselves; the gateway passes them through. If TLS ends somewhere other than Cloudflare or a load balancer that already sends HSTS, the app's HSTS header (production only) covers it. Once the domain is stable on HTTPS, submit it at hstspreload.org.

Migration `019_hardening.sql` adds R2 photo keys, deleted-account fields, link holds and the data-request log; apply it with `aside-api migrate`.
