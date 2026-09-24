#!/bin/sh
# Container entrypoint for the compose `backup` service (postgres:*-alpine image).
#   docker compose --profile ops up -d backup      # daily loop
#   docker compose run --rm backup once            # single run now
#   docker compose run --rm backup verify <file>   # re-check an existing dump
set -eu
BACKUP_DIR=${BACKUP_DIR:-/backups}
mkdir -p "$BACKUP_DIR"
chown postgres "$BACKUP_DIR"
# The official postgres images ship gosu or su-exec depending on version.
if command -v gosu >/dev/null 2>&1; then DROP=gosu; elif command -v su-exec >/dev/null 2>&1; then DROP=su-exec; else echo "need gosu or su-exec" >&2; exit 1; fi
as_postgres() { "$DROP" postgres "$@"; }

# The postgres image has no aws CLI; install it once when an off-site copy is configured.
if [ -n "${BACKUP_S3_URI:-}" ] && ! command -v aws >/dev/null 2>&1 && command -v apk >/dev/null 2>&1; then
  apk add --no-cache aws-cli >/dev/null
fi

case "${1:-loop}" in
  once) exec "$DROP" postgres /backup/backup.sh ;;
  verify) shift; exec "$DROP" postgres /backup/verify-restore.sh "$@" ;;
  loop)
    INTERVAL=${BACKUP_INTERVAL_SECONDS:-86400}
    while :; do
      as_postgres /backup/backup.sh || true   # failures are recorded and alerted by the API watchdog
      sleep "$INTERVAL"
    done ;;
  *) echo "usage: run.sh [loop|once|verify <dump>]" >&2; exit 2 ;;
esac
