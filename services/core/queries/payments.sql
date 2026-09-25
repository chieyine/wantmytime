-- name: PaymentAttemptOwnedByBuyer :one
SELECT EXISTS (
  SELECT 1 FROM payment_attempts p JOIN quotes q ON q.id = p.quote_id
  WHERE q.id = sqlc.arg(quote_id) AND q.buyer_user_id = sqlc.arg(buyer_user_id)
    AND p.provider = 'kora' AND p.environment = sqlc.arg(environment) AND p.merchant_reference = sqlc.arg(reference)
)::boolean AS owned;

-- name: LockPaymentAttemptByReference :one
SELECT id, quote_id, expected_minor, currency, canonical_state, approved_fee_minor, fee_basis_points, provider_transaction_id, booking_id, COALESCE(channel, '')::text AS channel, buyer_fee_minor
FROM payment_attempts
WHERE provider = 'kora' AND environment = sqlc.arg(environment) AND merchant_reference = sqlc.arg(reference)
FOR UPDATE;

-- name: RecordPaymentAttemptStatus :exec
UPDATE payment_attempts
SET canonical_state = sqlc.arg(state), provider_transaction_id = COALESCE(sqlc.narg(transaction_id), provider_transaction_id), last_verified_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND canonical_state NOT IN ('success', 'exception');

-- name: MarkPaymentAttemptException :exec
UPDATE payment_attempts
SET canonical_state = 'exception', provider_transaction_id = COALESCE(sqlc.narg(transaction_id), provider_transaction_id), last_verified_at = now(), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordPaymentChannel :exec
UPDATE payment_attempts SET channel = COALESCE(NULLIF(sqlc.arg(channel)::text, ''), channel), paid_minor = sqlc.arg(paid_minor)::bigint, updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordPaymentCard :exec
UPDATE payment_attempts SET card_country = sqlc.narg(card_country)::text, card_brand = sqlc.narg(card_brand)::text, updated_at = now()
WHERE id = sqlc.arg(id);

-- name: MarkPaymentAttemptSuccess :exec
UPDATE payment_attempts
SET booking_id = COALESCE(sqlc.narg(booking_id)::uuid, booking_id), canonical_state = 'success',
    provider_transaction_id = COALESCE(sqlc.narg(transaction_id), provider_transaction_id), last_verified_at = now(), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: BookingIDForQuote :one
SELECT id FROM bookings WHERE quote_id = sqlc.arg(quote_id)::uuid;

-- name: LockQuoteForPayment :one
SELECT seller_id, buyer_user_id, buyer_name, duration_minutes, starts_at, expires_at, state, offer_id
FROM quotes WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: LockHoldForQuote :one
SELECT active, expires_at FROM slot_reservations WHERE quote_id = sqlc.arg(quote_id)::uuid FOR UPDATE;

-- name: LockSellerAvailability :one
SELECT (u.status = 'active' AND NOT sp.paused)::boolean AS available
FROM seller_profiles sp JOIN users u ON u.id = sp.user_id
WHERE sp.id = sqlc.arg(seller_id)
FOR UPDATE OF sp;

-- name: LockOfferState :one
SELECT state FROM offers WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: VerifiedEmailForUser :one
-- The buyer's email, confirmed or not (buyers may pay before confirming it).
SELECT normalized_identifier FROM user_identities
WHERE user_id = sqlc.arg(user_id) AND type = 'email'
ORDER BY verified_at DESC NULLS LAST LIMIT 1;

-- name: CreatePaidBookingFromQuote :execrows
INSERT INTO bookings(id, quote_id, seller_id, buyer_user_id, buyer_name, guest_email, duration_minutes, starts_at, gross_minor, currency, state, payment_state, quote_snapshot, meeting_deadline)
SELECT sqlc.arg(booking_id)::uuid, q.id, q.seller_id, q.buyer_user_id, q.buyer_name, sqlc.arg(buyer_email)::text, q.duration_minutes, q.starts_at, q.gross_minor, q.currency, 'confirmed', 'paid',
       jsonb_build_object('quote_id', q.id, 'offer_id', q.offer_id, 'price_minor', q.gross_minor, 'currency', q.currency, 'duration_minutes', q.duration_minutes, 'starts_at', q.starts_at, 'payment_mode', 'kora_escrow'),
       q.starts_at - interval '30 minutes'
FROM quotes q WHERE q.id = sqlc.arg(quote_id)::uuid;

-- name: RecordServerProductEvent :exec
INSERT INTO product_events(id, event_name, subject_hash, environment, props, occurred_at, seller_id)
VALUES (gen_random_uuid(), sqlc.arg(event_name), sqlc.arg(subject_hash), sqlc.arg(environment), '{}', now(), sqlc.narg(seller_id));

-- name: CreatePaymentAllocation :exec
INSERT INTO payment_allocations(id, booking_id, gross_minor, deduction_minor, seller_entitlement_minor, processor_cost_minor, settlement_route, settlement_route_snapshot, fee_basis_points)
VALUES (gen_random_uuid(), sqlc.arg(booking_id), sqlc.arg(gross_minor), sqlc.arg(deduction_minor), sqlc.arg(seller_entitlement_minor), sqlc.arg(processor_cost_minor), 'approved_transfer',
        jsonb_build_object('provider', 'kora', 'environment', sqlc.arg(environment)::text, 'reference', sqlc.arg(reference)::text, 'route', 'approved_transfer'),
        sqlc.arg(fee_basis_points));

-- name: CreateSettlementItemForBooking :exec
INSERT INTO settlement_items(id, allocation_id, route, state, provider_reference)
SELECT gen_random_uuid(), id, settlement_route, 'payout_scheduled', sqlc.arg(reference)::text
FROM payment_allocations WHERE booking_id = sqlc.arg(booking_id)::uuid;

-- name: ConvertQuote :exec
UPDATE quotes SET state = 'converted' WHERE id = sqlc.arg(id);

-- name: ConvertOffer :execrows
UPDATE offers SET state = 'converted', converted_booking_id = sqlc.arg(booking_id)::uuid
WHERE id = sqlc.arg(id) AND state IN ('agreed', 'expired');

-- name: ConfirmHoldAsBooking :exec
UPDATE slot_reservations SET reservation_kind = 'booking', expires_at = NULL, booking_id = sqlc.arg(booking_id)::uuid
WHERE quote_id = sqlc.arg(quote_id)::uuid AND active;

-- name: AddPaymentException :exec
INSERT INTO payment_exceptions(id, payment_attempt_id, kind, state, provider_case_reference, amount_minor, currency, reason, evidence)
VALUES (gen_random_uuid(), sqlc.arg(payment_attempt_id)::uuid, sqlc.arg(kind), 'open', sqlc.arg(reference)::text, sqlc.arg(amount_minor)::bigint, sqlc.arg(currency)::text, sqlc.arg(reason),
        jsonb_build_object('quote_id', sqlc.arg(quote_id)::text))
ON CONFLICT (payment_attempt_id, kind) WHERE payment_attempt_id IS NOT NULL DO NOTHING;
