-- name: ReleaseExpiredHolds :exec
WITH released AS (
  UPDATE slot_reservations sr SET active = false
  WHERE sr.seller_id = sqlc.arg(seller_id)::uuid AND sr.active AND sr.reservation_kind = 'hold' AND sr.expires_at <= now()
    AND sr.occupied_range && tstzrange(sqlc.arg(range_from)::timestamptz, sqlc.arg(range_to)::timestamptz, '[)')
  RETURNING sr.quote_id
)
UPDATE quotes q SET state = 'expired' FROM released WHERE q.id = released.quote_id AND q.state = 'held';

-- name: ReleaseBuyerUnpaidHoldsWithSeller :exec
WITH released AS (
  UPDATE slot_reservations sr SET active = false FROM quotes q
  WHERE sr.quote_id = q.id AND q.buyer_user_id = sqlc.arg(buyer_user_id)::uuid AND q.seller_id = sqlc.arg(seller_id)::uuid AND q.state = 'held'
    AND sr.active AND sr.reservation_kind = 'hold'
    AND NOT EXISTS (SELECT 1 FROM payment_attempts p WHERE p.quote_id = q.id)
  RETURNING q.id
)
UPDATE quotes SET state = 'expired' FROM released WHERE quotes.id = released.id;

-- name: CountBuyerActiveHolds :one
SELECT count(*) FROM quotes q JOIN slot_reservations sr ON sr.quote_id = q.id
WHERE q.buyer_user_id = sqlc.arg(buyer_user_id) AND q.state = 'held' AND sr.active AND sr.reservation_kind = 'hold' AND sr.expires_at > now();

-- name: ExtendQuoteHold :one
UPDATE quotes SET expires_at = GREATEST(expires_at, sqlc.arg(until)::timestamptz)
WHERE id = sqlc.arg(id) AND state = 'held' AND expires_at > now()
RETURNING offer_id;

-- name: ExtendSlotHold :execrows
UPDATE slot_reservations SET expires_at = GREATEST(expires_at, sqlc.arg(until)::timestamptz)
WHERE quote_id = sqlc.arg(quote_id)::uuid AND active AND reservation_kind = 'hold' AND expires_at > now();

-- name: ExtendOfferCheckoutWindow :exec
UPDATE offers SET checkout_expires_at = GREATEST(checkout_expires_at, sqlc.arg(until)::timestamptz)
WHERE id = sqlc.arg(id) AND state = 'agreed';
