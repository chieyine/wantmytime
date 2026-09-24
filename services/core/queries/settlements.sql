-- name: SettlementMatchForReference :one
SELECT si.id AS settlement_item_id, pa.seller_entitlement_minor, b.currency
FROM payment_attempts p
JOIN payment_allocations pa ON pa.booking_id = p.booking_id
JOIN settlement_items si ON si.allocation_id = pa.id
JOIN bookings b ON b.id = pa.booking_id
WHERE p.provider = 'kora' AND p.environment = sqlc.arg(environment) AND p.merchant_reference = sqlc.arg(reference)
LIMIT 1;

-- name: LockSettlementItem :exec
SELECT id FROM settlement_items WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: SettlementItemAlreadyMatched :one
SELECT (EXISTS (SELECT 1 FROM settlement_confirmations sc WHERE sc.settlement_item_id = sqlc.arg(id) AND sc.reconciliation_state = 'matched')
     OR EXISTS (SELECT 1 FROM settlement_import_rows r WHERE r.settlement_item_id = sqlc.arg(id) AND r.reconciliation_state = 'matched'))::boolean AS matched;

-- name: InsertSettlementImportRow :exec
INSERT INTO settlement_import_rows(import_id, provider, environment, provider_reference, settlement_reference, amount_minor, currency, settled_at, reconciliation_state, settlement_item_id)
VALUES (sqlc.arg(import_id), 'kora', sqlc.arg(environment), sqlc.arg(reference), NULLIF(sqlc.arg(settlement_reference)::text, ''), sqlc.arg(amount_minor), sqlc.arg(currency), sqlc.arg(settled_at), sqlc.arg(state), sqlc.narg(settlement_item_id));

-- name: AuditSettlementImport :exec
INSERT INTO audit_events(id, actor_id, action, reason, safe_summary)
VALUES (gen_random_uuid(), sqlc.arg(actor_id)::uuid, 'settlement.imported', 'Imported operator-supplied normalized CSV for provisional review',
        jsonb_build_object('import_id', sqlc.arg(import_id)::text, 'matched', sqlc.arg(matched)::int, 'unmatched', sqlc.arg(unmatched)::int,
                           'amount_mismatch', sqlc.arg(amount_mismatch)::int, 'currency_mismatch', sqlc.arg(currency_mismatch)::int, 'duplicates', sqlc.arg(duplicates)::int));
