-- name: ApprovedChannelFee :one
SELECT percent_bps, fixed_minor, cap_minor FROM provider_fee_schedules
WHERE provider = 'kora' AND currency = 'NGN' AND channel = sqlc.arg(channel) AND approved_at IS NOT NULL AND effective_from <= now()
ORDER BY effective_from DESC LIMIT 1;

-- name: CheckoutQuote :one
SELECT q.id, q.seller_id::text AS seller_id, q.buyer_name, q.gross_minor, q.state, q.expires_at, sp.paused, (sp.readiness_state = 'ready')::boolean AS ready,
       EXISTS (SELECT 1 FROM seller_payout_accounts spa WHERE spa.seller_id = sp.id)::boolean AS has_payout_account, i.normalized_identifier AS buyer_email
FROM quotes q
JOIN seller_profiles sp ON sp.id = q.seller_id
JOIN user_identities i ON i.user_id = q.buyer_user_id AND i.type = 'email'
WHERE q.id = sqlc.arg(quote_id) AND q.buyer_user_id = sqlc.arg(buyer_user_id)
LIMIT 1;

-- name: UpsertPaymentAttempt :one
INSERT INTO payment_attempts(id, quote_id, provider, environment, merchant_reference, expected_minor, currency, canonical_state, approved_fee_minor, fee_basis_points, channel, buyer_fee_minor)
VALUES (gen_random_uuid(), sqlc.arg(quote_id)::uuid, 'kora', sqlc.arg(environment), sqlc.arg(reference), sqlc.arg(expected_minor), 'NGN', 'initializing', sqlc.arg(approved_fee_minor)::bigint, sqlc.arg(fee_basis_points)::int, sqlc.arg(channel)::text, sqlc.arg(buyer_fee_minor)::bigint)
ON CONFLICT (provider, environment, merchant_reference) DO UPDATE SET updated_at = now()
RETURNING id, canonical_state, COALESCE(authorization_url, '')::text AS authorization_url, transfer_details, instructions_expire_at,
  COALESCE(approved_fee_minor, 0)::bigint AS approved_fee_minor;

-- name: ClaimPaymentInitialization :execrows
UPDATE payment_attempts SET canonical_state = 'initialization_started', updated_at = now()
WHERE id = sqlc.arg(id) AND canonical_state = 'initializing' AND authorization_url IS NULL;

-- name: MarkInitializationUnknown :exec
UPDATE payment_attempts SET canonical_state = 'initialization_unknown', updated_at = now()
WHERE id = sqlc.arg(id) AND canonical_state = 'initialization_started';

-- name: SavePaymentAuthorization :execrows
UPDATE payment_attempts SET authorization_url = sqlc.arg(authorization_url)::text, access_code = sqlc.arg(access_code)::text, canonical_state = 'awaiting_payment', updated_at = now()
WHERE id = sqlc.arg(id) AND canonical_state = 'initialization_started';

-- name: SaveTransferInstructions :execrows
UPDATE payment_attempts SET transfer_details = sqlc.arg(details)::jsonb, instructions_expire_at = sqlc.arg(expires_at)::timestamptz, canonical_state = 'awaiting_payment', updated_at = now()
WHERE id = sqlc.arg(id) AND canonical_state = 'initialization_started';

-- name: InsertProviderEvent :exec
INSERT INTO provider_events(id, provider, environment, event_digest, raw_body_hash, state, event_type, provider_reference, minimal_payload)
VALUES (gen_random_uuid(), 'kora', sqlc.arg(environment), sqlc.arg(digest), sqlc.arg(digest), sqlc.arg(state), sqlc.arg(event_type)::text, NULLIF(sqlc.arg(reference)::text, ''), sqlc.arg(payload)::jsonb)
ON CONFLICT (provider, environment, event_digest) DO NOTHING;

-- name: UpsertProviderCase :exec
INSERT INTO provider_cases(provider, environment, provider_case_id, payment_attempt_id, provider_reference, case_type, state, amount_minor, currency, provider_deadline, last_event_type)
VALUES ('kora', sqlc.arg(environment), sqlc.arg(case_id),
        (SELECT pa.id FROM payment_attempts pa WHERE pa.provider = 'kora' AND pa.environment = sqlc.arg(environment) AND pa.merchant_reference = sqlc.arg(reference) LIMIT 1),
        sqlc.arg(reference), sqlc.arg(case_type), sqlc.arg(state), NULLIF(sqlc.arg(amount_minor)::bigint, 0), NULLIF(sqlc.arg(currency)::text, ''), sqlc.narg(deadline)::timestamptz, sqlc.arg(event_type))
ON CONFLICT (provider, environment, provider_case_id) DO UPDATE SET
  state = EXCLUDED.state,
  amount_minor = COALESCE(EXCLUDED.amount_minor, provider_cases.amount_minor),
  currency = COALESCE(EXCLUDED.currency, provider_cases.currency),
  provider_deadline = COALESCE(EXCLUDED.provider_deadline, provider_cases.provider_deadline),
  last_event_type = EXCLUDED.last_event_type,
  updated_at = now();

-- name: ClaimNextProviderEvent :one
SELECT id, provider_reference FROM provider_events
WHERE provider = 'kora' AND environment = sqlc.arg(environment)
  AND ((state IN ('queued', 'retry') AND next_attempt_at <= now()) OR (state = 'processing' AND claimed_at < now() - interval '2 minutes'))
ORDER BY received_at
FOR UPDATE SKIP LOCKED
LIMIT 1;

-- name: MarkProviderEventProcessing :exec
UPDATE provider_events SET state = 'processing', processing_attempts = processing_attempts + 1, claimed_at = now() WHERE id = sqlc.arg(id);

-- name: MarkProviderEventProcessed :exec
UPDATE provider_events SET state = 'processed', last_error_code = NULL, claimed_at = NULL WHERE id = sqlc.arg(id);

-- name: FailProviderEvent :exec
UPDATE provider_events
SET state = CASE WHEN sqlc.arg(retry)::boolean AND processing_attempts < 8 THEN 'retry' ELSE 'failed' END,
    last_error_code = sqlc.arg(error_code)::text,
    claimed_at = NULL,
    next_attempt_at = now() + LEAST(interval '15 minutes', interval '5 seconds' * power(2, LEAST(processing_attempts, 8)))
WHERE id = sqlc.arg(id);
