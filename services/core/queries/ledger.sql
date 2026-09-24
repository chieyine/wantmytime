-- name: InsertLedgerJournal :one
INSERT INTO ledger_journals(source_type, source_id, currency, description, request_digest)
VALUES (sqlc.arg(source_type), sqlc.arg(source_id), sqlc.arg(currency), sqlc.arg(description), sqlc.arg(request_digest))
ON CONFLICT (source_type, source_id) DO NOTHING
RETURNING id;

-- name: LedgerJournalBySource :one
SELECT id, request_digest FROM ledger_journals WHERE source_type = sqlc.arg(source_type) AND source_id = sqlc.arg(source_id);

-- name: InsertLedgerAccount :one
INSERT INTO ledger_accounts(account_code, currency, scope_id)
VALUES (sqlc.arg(account_code), sqlc.arg(currency), sqlc.narg(scope_id))
ON CONFLICT DO NOTHING
RETURNING id;

-- name: FindLedgerAccount :one
SELECT id FROM ledger_accounts
WHERE account_code = sqlc.arg(account_code) AND currency = sqlc.arg(currency)
  AND COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid) = COALESCE(sqlc.narg(scope_id)::uuid, '00000000-0000-0000-0000-000000000000'::uuid);

-- name: InsertLedgerEntry :exec
INSERT INTO ledger_entries(journal_id, account_id, side, amount_minor)
VALUES (sqlc.arg(journal_id), sqlc.arg(account_id), sqlc.arg(side), sqlc.arg(amount_minor));
