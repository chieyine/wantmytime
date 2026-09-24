-- name: SaveCalendarConnection :exec
INSERT INTO calendar_connections(seller_id, account_email, encrypted_refresh_token, scopes)
VALUES (sqlc.arg(seller_id)::uuid, sqlc.arg(account_email), sqlc.arg(encrypted_refresh_token), sqlc.arg(scopes))
ON CONFLICT (seller_id) DO UPDATE SET account_email = EXCLUDED.account_email, encrypted_refresh_token = EXCLUDED.encrypted_refresh_token,
  scopes = EXCLUDED.scopes, status = 'active', last_error = NULL, last_synced_at = NULL, connected_at = now(), updated_at = now();

-- name: CalendarConnectionForUser :one
SELECT c.seller_id::text AS seller_id, c.account_email, c.check_busy, c.add_events, c.create_meet_links, c.status, c.last_error, c.last_synced_at, c.connected_at,
       c.encrypted_refresh_token
FROM calendar_connections c JOIN seller_profiles sp ON sp.id = c.seller_id
WHERE sp.user_id = sqlc.arg(user_id);

-- name: CalendarConnection :one
SELECT seller_id::text AS seller_id, account_email, encrypted_refresh_token, check_busy, add_events, create_meet_links, status
FROM calendar_connections WHERE seller_id = sqlc.arg(seller_id)::uuid;

-- name: UpdateCalendarPreferences :exec
UPDATE calendar_connections SET check_busy = sqlc.arg(check_busy), add_events = sqlc.arg(add_events), create_meet_links = sqlc.arg(create_meet_links), updated_at = now()
WHERE seller_id = sqlc.arg(seller_id)::uuid;

-- name: DeleteCalendarConnection :exec
DELETE FROM calendar_connections WHERE seller_id = sqlc.arg(seller_id)::uuid;

-- name: DeleteBusyBlocks :exec
DELETE FROM calendar_busy_blocks WHERE seller_id = sqlc.arg(seller_id)::uuid;

-- name: MarkCalendarConnectionProblem :exec
UPDATE calendar_connections SET status = sqlc.arg(status), last_error = sqlc.arg(last_error)::text, updated_at = now()
WHERE seller_id = sqlc.arg(seller_id)::uuid;

-- name: MarkCalendarSynced :exec
UPDATE calendar_connections SET last_synced_at = now(), status = 'active', last_error = NULL, updated_at = now()
WHERE seller_id = sqlc.arg(seller_id)::uuid AND status <> 'revoked';

-- name: ConnectionsDueForBusySync :many
SELECT c.seller_id::text AS seller_id, sp.booking_horizon_days
FROM calendar_connections c JOIN seller_profiles sp ON sp.id = c.seller_id
WHERE c.status IN ('active', 'error') AND c.check_busy
  AND (c.last_synced_at IS NULL OR c.last_synced_at < now() - make_interval(secs => sqlc.arg(max_age_seconds)::int))
ORDER BY c.last_synced_at NULLS FIRST
LIMIT sqlc.arg(batch_size)::int;

-- name: SellerForBusyRefresh :one
SELECT c.seller_id::text AS seller_id, sp.booking_horizon_days,
       (c.last_synced_at IS NULL OR c.last_synced_at < now() - make_interval(secs => sqlc.arg(max_age_seconds)::int))::boolean AS stale
FROM calendar_connections c JOIN seller_profiles sp ON sp.id = c.seller_id
WHERE sp.id = sqlc.arg(seller_id)::uuid AND c.status IN ('active', 'error') AND c.check_busy;

-- name: InsertBusyBlock :exec
INSERT INTO calendar_busy_blocks(seller_id, busy)
VALUES (sqlc.arg(seller_id)::uuid, tstzrange(sqlc.arg(starts_at)::timestamptz, sqlc.arg(ends_at)::timestamptz, '[)'));

-- name: BusyBlocksBetween :many
SELECT lower(busy)::timestamptz AS starts_at, upper(busy)::timestamptz AS ends_at
FROM calendar_busy_blocks
WHERE seller_id = sqlc.arg(seller_id)::uuid AND busy && tstzrange(sqlc.arg(from_at)::timestamptz, sqlc.arg(to_at)::timestamptz, '[)');

-- name: CalendarBusyConflict :one
-- Busy ranges that sit entirely inside the booking being moved are that
-- booking's own calendar event, so they do not block its new time.
SELECT EXISTS (
  SELECT 1 FROM calendar_busy_blocks bb
  WHERE bb.seller_id = sqlc.arg(seller_id)::uuid
    AND bb.busy && tstzrange(sqlc.arg(from_at)::timestamptz, sqlc.arg(to_at)::timestamptz, '[)')
    AND NOT EXISTS (
      SELECT 1 FROM bookings own
      WHERE own.id = sqlc.narg(ignore_booking_id)::uuid
        AND tstzrange(own.starts_at, own.starts_at + own.duration_minutes * interval '1 minute', '[]') @> bb.busy
    )
)::boolean;

-- name: EnqueueCalendarSync :exec
INSERT INTO calendar_jobs(booking_id)
SELECT b.id FROM bookings b
JOIN calendar_connections c ON c.seller_id = b.seller_id AND c.status IN ('active', 'error') AND (c.add_events OR c.create_meet_links)
WHERE b.id = sqlc.arg(booking_id)::uuid
ON CONFLICT (booking_id) WHERE state = 'queued' DO UPDATE SET due_at = LEAST(calendar_jobs.due_at, now());

-- name: EnqueueCalendarSyncForSeller :exec
INSERT INTO calendar_jobs(booking_id)
SELECT b.id FROM bookings b
WHERE b.seller_id = sqlc.arg(seller_id)::uuid AND b.state = 'confirmed' AND b.starts_at > now()
ON CONFLICT (booking_id) WHERE state = 'queued' DO NOTHING;

-- name: ClaimCalendarJobs :many
WITH claimable AS (
  SELECT id FROM calendar_jobs
  WHERE attempts < 10 AND ((state = 'queued' AND due_at <= now()) OR (state = 'processing' AND claimed_at < now() - interval '5 minutes'))
  ORDER BY due_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT sqlc.arg(batch_size)::int
)
UPDATE calendar_jobs j SET state = 'processing', claimed_at = now(), attempts = j.attempts + 1, updated_at = now()
FROM claimable c WHERE j.id = c.id
RETURNING j.id::text AS id, j.booking_id::text AS booking_id, j.attempts;

-- name: CalendarBookingContext :one
SELECT b.id::text AS booking_id, b.seller_id::text AS seller_id, b.state, b.starts_at, b.duration_minutes, b.buyer_name, sp.handle,
       COALESCE(b.calendar_event_id, '')::text AS calendar_event_id, (b.meeting_url IS NOT NULL)::boolean AS has_meeting_link,
       COALESCE(b.meeting_source, '')::text AS meeting_source
FROM bookings b JOIN seller_profiles sp ON sp.id = b.seller_id
WHERE b.id = sqlc.arg(booking_id)::uuid;

-- name: SetBookingCalendarEvent :exec
UPDATE bookings SET calendar_event_id = sqlc.narg(event_id)::text WHERE id = sqlc.arg(booking_id)::uuid;

-- name: SetBookingMeetLink :execrows
UPDATE bookings SET meeting_url = sqlc.arg(meeting_url), meeting_ready_at = now(), meeting_source = 'google_meet'
WHERE id = sqlc.arg(booking_id)::uuid AND meeting_url IS NULL AND state = 'confirmed' AND starts_at > now();

-- name: FinishCalendarJob :exec
UPDATE calendar_jobs SET state = sqlc.arg(state), last_error = sqlc.narg(last_error)::text, claimed_at = NULL, updated_at = now(),
  due_at = CASE WHEN sqlc.arg(state) = 'queued' THEN now() + make_interval(secs => sqlc.arg(retry_seconds)::int) ELSE due_at END
WHERE id = sqlc.arg(id)::uuid;

-- name: CreateOAuthState :exec
INSERT INTO oauth_states(state_hash, user_id, code_verifier, expires_at)
VALUES (sqlc.arg(state_hash), sqlc.arg(user_id)::uuid, sqlc.arg(code_verifier), now() + interval '10 minutes');

-- name: ConsumeOAuthState :one
DELETE FROM oauth_states WHERE state_hash = sqlc.arg(state_hash) AND expires_at > now()
RETURNING user_id::text AS user_id, code_verifier;

-- name: PruneOAuthStates :exec
DELETE FROM oauth_states WHERE expires_at < now() - interval '1 hour';

-- name: SellerIDForUser :one
SELECT id::text FROM seller_profiles WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: SellerIDForHandle :one
SELECT id::text FROM seller_profiles WHERE handle = sqlc.arg(handle);
