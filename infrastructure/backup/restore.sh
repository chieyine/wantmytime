#!/bin/sh
# Restores a verified Aside backup into an EMPTY target database.
# Read docs/OPERATIONS.md first. This does not touch the source database.
#
#   ./restore.sh backups/aside-20260924T020000Z.dump postgres://user:pass@host:5432/aside_restored --yes
set -eu

DUMP=${1:?usage: restore.sh <dump-file> <target-database-url> --yes}
TARGET=${2:?usage: restore.sh <dump-file> <target-database-url> --yes}
[ "${3:-}" = "--yes" ] || { echo "Refusing to restore without --yes. Target: the database in the URL you passed." >&2; exit 2; }
[ -f "$DUMP" ] || { echo "no such file: $DUMP" >&2; exit 2; }

if [ -f "$DUMP.sha256" ]; then
  (cd "$(dirname "$DUMP")" && sha256sum -c "$(basename "$DUMP").sha256" >/dev/null 2>&1) || { echo "checksum mismatch; not restoring" >&2; exit 1; }
fi

tables=$(psql "$TARGET" -X -A -t -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")
if [ "$tables" != 0 ]; then
  echo "Target database already has $tables table(s) in public. Restore only into a new, empty database." >&2
  exit 1
fi

pg_restore --exit-on-error --no-owner --no-privileges --single-transaction --dbname="$TARGET" "$DUMP"
echo "Restored $(basename "$DUMP"). Latest migration: $(psql "$TARGET" -X -A -t -c 'SELECT max(version) FROM schema_migrations')"
echo "Next: point DATABASE_URL at the restored database, run 'aside-api migrate', then start the API."
