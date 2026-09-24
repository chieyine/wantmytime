#!/bin/sh
# Restores an Aside backup into a throwaway database and checks it is usable.
# Exits non-zero if the dump is corrupt, fails to restore, or fails a check.
#
#   ./verify-restore.sh backups/aside-20260924T020000Z.dump
#
# Where the restore happens:
#   RESTORE_DATABASE_URL set  -> a temporary database is created on that server
#                               (a scratch server is best; the role needs CREATEDB)
#   otherwise                 -> a temporary PostgreSQL cluster is started locally
#                               with initdb/pg_ctl (must not run as root)
# Optional LIVE_DATABASE_URL enables comparisons against the source database.
set -eu

DUMP=${1:?usage: verify-restore.sh <dump-file>}
[ -f "$DUMP" ] || { echo "no such file: $DUMP" >&2; exit 2; }

log() { printf '{"time":"%s","level":"%s","msg":"%s","component":"restore-check"}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2"; }

if [ -f "$DUMP.sha256" ]; then
  (cd "$(dirname "$DUMP")" && sha256sum -c "$(basename "$DUMP").sha256" >/dev/null 2>&1) || { log error "checksum mismatch for $DUMP"; exit 1; }
fi
pg_restore --list "$DUMP" >/dev/null || { log error "dump table of contents is unreadable"; exit 1; }

CHECK_DB="aside_restore_check_$(date -u +%Y%m%d%H%M%S)_$$"
CLUSTER=""
cleanup() {
  if [ -n "$CLUSTER" ]; then
    pg_ctl -D "$CLUSTER/data" -m immediate stop >/dev/null 2>&1 || true
    rm -rf "$CLUSTER"
  elif [ -n "${ADMIN_URL:-}" ]; then
    psql "$ADMIN_URL" -X -q -c "DROP DATABASE IF EXISTS \"$CHECK_DB\" WITH (FORCE)" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

if [ -n "${RESTORE_DATABASE_URL:-}" ]; then
  ADMIN_URL=$RESTORE_DATABASE_URL
  psql "$ADMIN_URL" -X -q -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$CHECK_DB\"" >/dev/null
  # Swap the database name in the URL, keeping any query string.
  TARGET_URL=$(printf '%s' "$ADMIN_URL" | sed -E "s#^(postgres(ql)?://[^/]*)(/[^?]*)?#\1/$CHECK_DB#")
else
  [ "$(id -u)" != 0 ] || { log error "run as a non-root user or set RESTORE_DATABASE_URL"; exit 2; }
  command -v initdb >/dev/null 2>&1 || PATH="$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | tail -n 1):$PATH"
  CLUSTER=$(mktemp -d "${TMPDIR:-/tmp}/aside-restore.XXXXXX")
  initdb -D "$CLUSTER/data" -U postgres -A trust --no-sync >/dev/null
  pg_ctl -D "$CLUSTER/data" -w -l "$CLUSTER/log" -o "-c listen_addresses='' -k $CLUSTER -c fsync=off" start >/dev/null
  psql -h "$CLUSTER" -U postgres -X -q -c "CREATE DATABASE \"$CHECK_DB\"" postgres >/dev/null
  TARGET_URL="postgresql://postgres@/$CHECK_DB?host=$CLUSTER"
fi

started=$(date +%s)
pg_restore --exit-on-error --no-owner --no-privileges --dbname="$TARGET_URL" "$DUMP" || { log error "pg_restore failed"; exit 1; }
restore_seconds=$(( $(date +%s) - started ))

q() { psql "$TARGET_URL" -X -A -t -v ON_ERROR_STOP=1 -c "$1"; }

# 1. Core tables exist.
for table in schema_migrations users seller_profiles bookings payment_attempts ledger_journals ledger_entries notification_outbox audit_events; do
  [ "$(q "SELECT to_regclass('public.$table') IS NOT NULL")" = t ] || { log error "restored database is missing table $table"; exit 1; }
done

# 2. Migrations recorded.
migrations=$(q "SELECT count(*) FROM schema_migrations")
[ "$migrations" -gt 0 ] || { log error "schema_migrations is empty"; exit 1; }
latest=$(q "SELECT max(version) FROM schema_migrations")

# 3. Double-entry ledger balances in every journal.
unbalanced=$(q "SELECT count(*) FROM (SELECT journal_id FROM ledger_entries GROUP BY journal_id HAVING sum(CASE side WHEN 'debit' THEN amount_minor ELSE -amount_minor END) <> 0) j")
[ "$unbalanced" = 0 ] || { log error "$unbalanced ledger journal(s) do not balance in the restored copy"; exit 1; }

# 4. Referential integrity spot check.
orphans=$(q "SELECT count(*) FROM bookings b LEFT JOIN seller_profiles s ON s.id = b.seller_id WHERE s.id IS NULL")
[ "$orphans" = 0 ] || { log error "$orphans booking(s) reference a missing seller"; exit 1; }

users=$(q "SELECT count(*) FROM users")
bookings=$(q "SELECT count(*) FROM bookings")
entries=$(q "SELECT count(*) FROM ledger_entries")

# 5. Against the live database: the ledger is append-only, so the copy can never hold more.
if [ -n "${LIVE_DATABASE_URL:-}" ]; then
  live_entries=$(psql "$LIVE_DATABASE_URL" -X -A -t -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM ledger_entries")
  [ "$entries" -le "$live_entries" ] || { log error "restored ledger has more entries ($entries) than live ($live_entries)"; exit 1; }
  live_latest=$(psql "$LIVE_DATABASE_URL" -X -A -t -v ON_ERROR_STOP=1 -c "SELECT max(version) FROM schema_migrations")
  [ "$latest" = "$live_latest" ] || log warn "restored schema is at $latest but live is at $live_latest (a migration ran after the dump)"
fi

log info "restore check passed for $(basename "$DUMP"): ${restore_seconds}s restore, schema $latest, $users users, $bookings bookings, $entries ledger entries"
