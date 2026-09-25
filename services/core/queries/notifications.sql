-- name: BookingParticipants :one
SELECT b.buyer_user_id, sp.user_id AS seller_user_id, b.starts_at
FROM bookings b JOIN seller_profiles sp ON sp.id = b.seller_id
WHERE b.id = sqlc.arg(booking_id);

-- name: EnqueueNotification :exec
INSERT INTO notification_outbox(id, event_key, booking_id, recipient_user_id, kind, due_at, reference_id)
VALUES (gen_random_uuid(), sqlc.arg(event_key), sqlc.arg(booking_id)::uuid, sqlc.arg(recipient_user_id), sqlc.arg(kind), sqlc.arg(due_at), sqlc.narg(reference_id)::uuid)
ON CONFLICT (event_key) DO NOTHING;

-- name: EnqueueOfferNotification :exec
INSERT INTO notification_outbox(id, event_key, offer_id, recipient_user_id, kind, due_at)
VALUES (gen_random_uuid(), sqlc.arg(event_key), sqlc.arg(offer_id)::uuid, sqlc.arg(recipient_user_id)::uuid, sqlc.arg(kind), now())
ON CONFLICT (event_key) DO NOTHING;

-- name: NotificationTarget :one
SELECT kind, (offer_id IS NOT NULL)::boolean AS is_offer FROM notification_outbox WHERE id = sqlc.arg(id) AND state = 'processing';

-- name: EnqueueMeetingReady :exec
INSERT INTO notification_outbox(id, event_key, booking_id, recipient_user_id, kind, due_at)
SELECT gen_random_uuid(), b.id::text || ':meeting-link-ready:' || b.meeting_ready_at::text, b.id, b.buyer_user_id, 'meeting_link_ready_buyer', now()
FROM bookings b WHERE b.id = sqlc.arg(booking_id)::uuid
ON CONFLICT (event_key) DO NOTHING;

-- name: EnqueueRescheduleEmails :exec
INSERT INTO notification_outbox(id, event_key, booking_id, recipient_user_id, kind, due_at)
SELECT gen_random_uuid(), sqlc.arg(request_id)::text || ':reschedule:' || recipient.role, b.id, recipient.user_id, recipient.kind, now()
FROM bookings b
JOIN seller_profiles sp ON sp.id = b.seller_id
CROSS JOIN LATERAL (VALUES ('buyer', b.buyer_user_id, 'reschedule_accepted_buyer'), ('seller', sp.user_id, 'reschedule_accepted_seller')) AS recipient(role, user_id, kind)
WHERE b.id = sqlc.arg(booking_id)::uuid
ON CONFLICT (event_key) DO NOTHING;

-- name: EnqueueCancellationReview :exec
INSERT INTO notification_outbox(id, event_key, booking_id, recipient_user_id, kind, due_at)
SELECT gen_random_uuid(), sqlc.arg(cancellation_id)::text || ':cancellation-review:' || recipient.role, b.id, recipient.user_id, recipient.kind, now()
FROM booking_cancellation_requests cr
JOIN bookings b ON b.id = cr.booking_id
JOIN seller_profiles sp ON sp.id = b.seller_id
CROSS JOIN LATERAL (VALUES ('buyer', b.buyer_user_id, 'cancellation_reviewed_buyer'), ('seller', sp.user_id, 'cancellation_reviewed_seller')) AS recipient(role, user_id, kind)
WHERE cr.id = sqlc.arg(cancellation_id)::uuid AND recipient.user_id IS NOT NULL
ON CONFLICT (event_key) DO NOTHING;

-- name: CancelPendingBookingReminders :exec
UPDATE notification_outbox SET state = 'cancelled', claimed_at = NULL, last_error_code = sqlc.arg(reason_code)::text
WHERE booking_id = sqlc.arg(booking_id)::uuid AND state IN ('queued', 'processing')
  AND (kind LIKE 'booking_reminder_%' OR kind LIKE 'meeting_link_due_%' OR kind = 'review_request_buyer');

-- name: FailExhaustedNotifications :exec
UPDATE notification_outbox SET state = 'failed', claimed_at = NULL, last_error_code = 'RETRY_LIMIT'
WHERE attempts >= 8 AND state IN ('queued', 'processing');

-- name: ClaimDueNotifications :many
WITH claimable AS (
  SELECT id FROM notification_outbox
  WHERE attempts < 8 AND ((state = 'queued' AND due_at <= now()) OR (state = 'processing' AND claimed_at < now() - interval '5 minutes'))
  ORDER BY due_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT sqlc.arg(batch_size)::int
)
UPDATE notification_outbox n SET state = 'processing', claimed_at = now(), attempts = n.attempts + 1
FROM claimable c WHERE n.id = c.id
RETURNING n.id;

-- name: NotificationContext :one
SELECT n.kind, b.id AS booking_id, identity.normalized_identifier AS recipient_email, owner.display_name AS seller_name, b.buyer_name,
       sp.handle, sp.timezone, recipient.timezone AS recipient_timezone, (recipient.id = sp.user_id)::boolean AS recipient_is_seller,
       COALESCE(buyer.timezone, sp.timezone)::text AS buyer_timezone,
       b.state AS booking_state, b.payment_state, b.starts_at, b.duration_minutes, b.meeting_url, b.gross_minor, b.currency, b.calendar_sequence,
       COALESCE(pa.seller_entitlement_minor, 0)::bigint AS seller_entitlement_minor, (pa.id IS NOT NULL)::boolean AS has_allocation,
       rr.proposed_starts_at AS reschedule_starts_at, rr.expires_at AS reschedule_expires_at, COALESCE(rr.state, '')::text AS reschedule_state,
       b.cancellation_policy, COALESCE(b.cancelled_by_role, '')::text AS cancelled_by_role,
       COALESCE(rf.amount_minor, 0)::bigint AS refund_minor, COALESCE(rf.seller_share_minor, 0)::bigint AS refund_seller_share_minor, COALESCE(rf.state, '')::text AS refund_state, COALESCE(rf.seller_liability, '')::text AS refund_seller_liability,
       COALESCE(ns.absent_role, '')::text AS no_show_absent_role, COALESCE(ns.state, '')::text AS no_show_state, ns.resolves_at AS no_show_resolves_at,
       COALESCE(rv.rating, 0)::int AS review_rating, COALESCE(rv.body, '')::text AS review_body,
       EXISTS (SELECT 1 FROM reviews own WHERE own.booking_id = b.id)::boolean AS has_review,
       COALESCE(po.state, '')::text AS payout_state, COALESCE(po.amount_minor - po.recovery_minor, 0)::bigint AS payout_net_minor,
       COALESCE(po.recovery_minor, 0)::bigint AS payout_recovery_minor, COALESCE(po.bank_name, '')::text AS payout_bank_name,
       COALESCE(po.account_last4, '')::text AS payout_account_last4,
       COALESCE(b.issue_resolution, '')::text AS issue_resolution
FROM notification_outbox n
JOIN bookings b ON b.id = n.booking_id
LEFT JOIN users buyer ON buyer.id = b.buyer_user_id
LEFT JOIN reschedule_requests rr ON rr.id = n.reference_id AND rr.booking_id = b.id
LEFT JOIN refunds rf ON rf.id = n.reference_id AND rf.booking_id = b.id
LEFT JOIN no_show_reports ns ON ns.id = n.reference_id AND ns.booking_id = b.id
LEFT JOIN reviews rv ON rv.id = n.reference_id AND rv.booking_id = b.id
LEFT JOIN seller_payouts po ON po.id = n.reference_id AND po.booking_id = b.id
JOIN seller_profiles sp ON sp.id = b.seller_id
JOIN users owner ON owner.id = sp.user_id
JOIN users recipient ON recipient.id = n.recipient_user_id AND recipient.status = 'active'
-- Buyers who paid without confirming their email still get their booking emails.
JOIN user_identities identity ON identity.user_id = recipient.id AND identity.type = 'email'
LEFT JOIN payment_allocations pa ON pa.booking_id = b.id
WHERE n.id = sqlc.arg(id) AND n.state = 'processing' AND (recipient.id = sp.user_id OR recipient.id = b.buyer_user_id)
LIMIT 1;

-- name: OfferNotificationContext :one
SELECT n.kind, o.id AS offer_id, identity.normalized_identifier AS recipient_email, recipient.timezone AS recipient_timezone,
       (recipient.id = sp.user_id)::boolean AS recipient_is_seller, owner.display_name AS seller_name, sp.handle, sp.timezone AS seller_timezone,
       o.buyer_name, o.duration_minutes, o.state AS offer_state, o.version, o.expires_at, o.checkout_expires_at,
       COALESCE(v.amount_minor, 0)::bigint AS amount_minor, COALESCE(v.actor, '')::text AS amount_actor
FROM notification_outbox n
JOIN offers o ON o.id = n.offer_id
JOIN seller_profiles sp ON sp.id = o.seller_id
JOIN users owner ON owner.id = sp.user_id
JOIN users recipient ON recipient.id = n.recipient_user_id AND recipient.status = 'active'
-- Buyers who paid without confirming their email still get their booking emails.
JOIN user_identities identity ON identity.user_id = recipient.id AND identity.type = 'email'
LEFT JOIN LATERAL (SELECT amount_minor, actor FROM offer_versions WHERE offer_id = o.id ORDER BY version DESC LIMIT 1) v ON true
WHERE n.id = sqlc.arg(id) AND n.state = 'processing' AND (recipient.id = sp.user_id OR recipient.id = o.buyer_user_id)
LIMIT 1;

-- name: MarkNotificationSent :exec
UPDATE notification_outbox SET state = 'sent', sent_at = now(), claimed_at = NULL, last_error_code = NULL
WHERE id = sqlc.arg(id) AND state = 'processing';

-- name: MarkNotificationFailed :exec
UPDATE notification_outbox
SET state = CASE WHEN attempts >= 8 THEN 'failed' ELSE 'queued' END,
    due_at = now() + make_interval(secs => LEAST(3600, 30 * (2 ^ LEAST(attempts, 7))::int)),
    claimed_at = NULL, last_error_code = sqlc.arg(error_code)::text
WHERE id = sqlc.arg(id) AND state = 'processing';

-- name: MarkNotificationCancelled :exec
UPDATE notification_outbox SET state = 'cancelled', claimed_at = NULL, last_error_code = 'STALE_BOOKING'
WHERE id = sqlc.arg(id) AND state = 'processing';
