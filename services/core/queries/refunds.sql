-- name: LockBookingForCancellation :one
SELECT b.id::text AS booking_id, b.state, b.payment_state, b.starts_at, b.created_at, b.duration_minutes, b.gross_minor, b.currency,
       b.buyer_user_id::text AS buyer_user_id, sp.user_id::text AS seller_user_id, sp.id::text AS seller_id,
       COALESCE(pa.deduction_minor, 0)::bigint AS deduction_minor,
       COALESCE(att.id::text, '')::text AS payment_attempt_id,
       COALESCE(att.buyer_fee_minor, 0)::bigint AS buyer_fee_minor
FROM bookings b
JOIN seller_profiles sp ON sp.id = b.seller_id
LEFT JOIN payment_allocations pa ON pa.booking_id = b.id
LEFT JOIN payment_attempts att ON att.booking_id = b.id AND att.canonical_state = 'success'
WHERE b.id = sqlc.arg(booking_id)::uuid
FOR UPDATE OF b;

-- name: MarkBookingCancelled :exec
UPDATE bookings SET state = 'cancelled', cancelled_at = now(), cancelled_by_role = sqlc.arg(role)::text, cancellation_reason = sqlc.narg(reason)::text
WHERE id = sqlc.arg(booking_id)::uuid AND state = 'confirmed';

-- name: ReleaseBookingSlot :exec
UPDATE slot_reservations SET active = false WHERE booking_id = sqlc.arg(booking_id)::uuid AND active;

-- name: CancelUpcomingBookingMail :exec
UPDATE notification_outbox SET state = 'cancelled', claimed_at = NULL, last_error_code = sqlc.arg(reason_code)::text
WHERE booking_id = sqlc.arg(booking_id)::uuid AND state = 'queued'
  AND (kind LIKE 'booking_reminder_%' OR kind LIKE 'meeting_link_due_%' OR kind = 'review_request_buyer' OR kind = 'meeting_link_ready_buyer');

-- name: CloseBookingRequests :exec
WITH closed_cancellations AS (
  UPDATE booking_cancellation_requests SET state = 'resolved', resolution = sqlc.arg(resolution)::text, resolved_at = now()
  WHERE booking_id = sqlc.arg(booking_id)::uuid AND state = 'open'
)
UPDATE reschedule_requests SET state = 'expired', version = version + 1
WHERE booking_id = sqlc.arg(booking_id)::uuid AND state = 'pending';

-- name: CreateRefund :one
INSERT INTO refunds(booking_id, payment_attempt_id, amount_minor, currency, platform_share_minor, seller_share_minor, reason, state, requested_by, note, seller_liability)
VALUES (sqlc.arg(booking_id)::uuid, sqlc.narg(payment_attempt_id)::uuid, sqlc.arg(amount_minor), sqlc.arg(currency), sqlc.arg(platform_share_minor), sqlc.arg(seller_share_minor),
        sqlc.arg(reason), sqlc.arg(state), sqlc.narg(requested_by)::uuid, sqlc.narg(note)::text, sqlc.arg(seller_liability)::text)
RETURNING id::text;

-- name: ClaimDueRefunds :many
WITH due AS (
  SELECT id FROM refunds
  WHERE state IN ('queued', 'submitted') AND next_attempt_at <= now()
  ORDER BY next_attempt_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT sqlc.arg(batch_size)::int
)
UPDATE refunds r SET next_attempt_at = now() + interval '5 minutes', attempts = r.attempts + 1, updated_at = now()
FROM due WHERE r.id = due.id
RETURNING r.id::text AS id, r.state, r.attempts, r.amount_minor, r.currency, COALESCE(r.provider_refund_id, '')::text AS provider_refund_id,
  COALESCE((SELECT merchant_reference FROM payment_attempts pa WHERE pa.id = r.payment_attempt_id), '')::text AS reference,
  COALESCE((SELECT COALESCE(provider_transaction_id, merchant_reference) FROM payment_attempts pa WHERE pa.id = r.payment_attempt_id), '')::text AS transaction_key;

-- name: MarkRefundSubmitted :exec
UPDATE refunds SET state = 'submitted', provider_refund_id = sqlc.arg(provider_refund_id)::text, submitted_at = COALESCE(submitted_at, now()),
  next_attempt_at = now() + make_interval(secs => sqlc.arg(poll_seconds)::int), last_error = sqlc.narg(last_error)::text, updated_at = now()
WHERE id = sqlc.arg(id)::uuid AND state IN ('queued', 'submitted');

-- name: RetryRefundLater :exec
UPDATE refunds SET next_attempt_at = now() + make_interval(secs => sqlc.arg(retry_seconds)::int), last_error = sqlc.arg(last_error)::text, updated_at = now()
WHERE id = sqlc.arg(id)::uuid AND state IN ('queued', 'submitted');

-- name: MarkRefundFailed :exec
UPDATE refunds SET state = 'failed', last_error = sqlc.arg(last_error)::text, updated_at = now()
WHERE id = sqlc.arg(id)::uuid AND state IN ('queued', 'submitted', 'pending_approval');

-- name: LockRefundForFinalize :one
SELECT r.id::text AS id, r.state, r.seller_liability, r.amount_minor, r.currency, r.platform_share_minor, r.seller_share_minor, r.booking_id::text AS booking_id,
       b.seller_id::text AS seller_id, b.gross_minor, b.payment_state, b.buyer_user_id::text AS buyer_user_id
FROM refunds r JOIN bookings b ON b.id = r.booking_id
WHERE r.id = sqlc.arg(id)::uuid
FOR UPDATE OF r;

-- name: MarkRefundProcessed :exec
UPDATE refunds SET state = 'processed', processed_at = now(), note = COALESCE(sqlc.narg(note)::text, note), approved_by = COALESCE(sqlc.narg(approved_by)::uuid, approved_by), updated_at = now()
WHERE id = sqlc.arg(id)::uuid;

-- name: SetBookingPaymentState :exec
UPDATE bookings SET payment_state = sqlc.arg(payment_state)::text WHERE id = sqlc.arg(booking_id)::uuid;

-- name: InsertSellerRecovery :exec
INSERT INTO seller_recoveries(seller_id, refund_id, amount_minor, currency)
VALUES (sqlc.arg(seller_id)::uuid, sqlc.arg(refund_id)::uuid, sqlc.arg(amount_minor), sqlc.arg(currency))
ON CONFLICT (refund_id) DO NOTHING;

-- name: LockOpenRecoveries :many
SELECT id::text, amount_minor - recovered_minor AS outstanding
FROM seller_recoveries WHERE seller_id = sqlc.arg(seller_id)::uuid AND recovered_minor < amount_minor
ORDER BY created_at, id
FOR UPDATE;

-- name: ApplyRecovery :exec
UPDATE seller_recoveries SET recovered_minor = recovered_minor + sqlc.arg(amount_minor), updated_at = now() WHERE id = sqlc.arg(id)::uuid;

-- name: NudgeRefundByProviderID :exec
UPDATE refunds SET next_attempt_at = now(), updated_at = now() WHERE provider_refund_id = sqlc.arg(provider_refund_id)::text AND state = 'submitted';

-- name: SellerRecoverySummary :one
SELECT COALESCE(sum(amount_minor), 0)::bigint AS total_minor, COALESCE(sum(recovered_minor), 0)::bigint AS recovered_minor
FROM seller_recoveries sr JOIN seller_profiles sp ON sp.id = sr.seller_id
WHERE sp.user_id = sqlc.arg(user_id)::uuid;
