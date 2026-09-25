-- name: RecordWorkerBeat :exec
INSERT INTO worker_heartbeats(name, instance, last_beat_at)
VALUES (sqlc.arg(name), sqlc.arg(instance), now())
ON CONFLICT (name) DO UPDATE SET instance = EXCLUDED.instance, last_beat_at = now();

-- name: RecordWorkerError :exec
INSERT INTO worker_heartbeats(name, instance, last_beat_at, last_error_at, last_error)
VALUES (sqlc.arg(name), sqlc.arg(instance), now(), now(), sqlc.arg(error_text)::text)
ON CONFLICT (name) DO UPDATE SET instance = EXCLUDED.instance, last_beat_at = now(), last_error_at = now(), last_error = EXCLUDED.last_error;

-- name: WorkerHeartbeats :many
SELECT name, instance, last_beat_at, last_error_at, last_error,
       extract(epoch FROM now() - last_beat_at)::double precision AS age_seconds
FROM worker_heartbeats ORDER BY name;

-- name: AlertSignals :one
SELECT
  (SELECT count(*) FROM provider_events WHERE state = 'failed' AND auto_retries >= 3)::bigint AS provider_events_failed,
  COALESCE((SELECT extract(epoch FROM now() - min(received_at)) FROM provider_events WHERE state IN ('queued', 'retry')), 0)::double precision AS provider_oldest_pending_seconds,
  (SELECT count(*) FROM notification_outbox WHERE state = 'failed' AND auto_retries >= 3 AND created_at > now() - interval '7 days')::bigint AS notifications_failed_24h,
  COALESCE((SELECT extract(epoch FROM now() - min(due_at)) FROM notification_outbox WHERE state = 'queued' AND due_at <= now()), 0)::double precision AS notifications_oldest_due_seconds,
  (SELECT count(*) FROM payment_exceptions WHERE state IN ('open', 'investigating'))::bigint AS payment_exceptions_open,
  (SELECT count(*) FROM provider_cases WHERE state <> 'resolved' AND provider_deadline IS NOT NULL AND provider_deadline < now() + interval '72 hours')::bigint AS provider_cases_due_soon,
  (SELECT count(*) FROM bookings WHERE state = 'confirmed' AND meeting_url IS NULL AND starts_at > now() AND starts_at < now() + interval '2 hours')::bigint AS meetings_missing_link_soon;

-- name: GetAlert :one
SELECT key, state, severity, summary, value, first_fired_at, last_notified_at, resolved_at, updated_at
FROM ops_alerts WHERE key = sqlc.arg(key);

-- name: FireAlert :exec
INSERT INTO ops_alerts(key, state, severity, summary, value, first_fired_at, last_notified_at, updated_at)
VALUES (sqlc.arg(key), 'firing', sqlc.arg(severity), sqlc.arg(summary), sqlc.arg(value), now(), sqlc.narg(notified_at), now())
ON CONFLICT (key) DO UPDATE SET
  state = 'firing',
  severity = EXCLUDED.severity,
  summary = EXCLUDED.summary,
  value = EXCLUDED.value,
  first_fired_at = CASE WHEN ops_alerts.state = 'resolved' THEN now() ELSE ops_alerts.first_fired_at END,
  last_notified_at = COALESCE(EXCLUDED.last_notified_at, ops_alerts.last_notified_at),
  resolved_at = NULL,
  updated_at = now();

-- name: ResolveAlert :exec
UPDATE ops_alerts SET state = 'resolved', resolved_at = now(), updated_at = now() WHERE key = sqlc.arg(key) AND state = 'firing';

-- name: ListAlerts :many
SELECT key, state, severity, summary, value, first_fired_at, last_notified_at, resolved_at, updated_at
FROM ops_alerts
WHERE state = 'firing' OR resolved_at > now() - interval '7 days'
ORDER BY (state = 'firing') DESC, updated_at DESC
LIMIT 100;

-- name: ProviderEventCountsByState :many
SELECT state, count(*)::bigint AS total FROM provider_events GROUP BY state ORDER BY state;

-- name: NotificationCountsByState :many
SELECT state, count(*)::bigint AS total FROM notification_outbox GROUP BY state ORDER BY state;

-- name: OperationalGauges :one
SELECT
  (SELECT count(*) FROM provider_cases WHERE state <> 'resolved')::bigint AS provider_cases_open,
  (SELECT count(*) FROM slot_reservations WHERE active AND reservation_kind = 'hold' AND expires_at > now())::bigint AS active_holds,
  (SELECT count(*) FROM bookings WHERE state = 'confirmed' AND starts_at > now() AND starts_at < now() + interval '24 hours')::bigint AS bookings_next_24h,
  (SELECT count(*) FROM ops_alerts WHERE state = 'firing')::bigint AS alerts_firing;
