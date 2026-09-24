#!/bin/sh
# Takes a compressed logical backup of the Aside database, proves it restores,
# optionally copies it off-site, prunes old backups and records the result in
# worker_heartbeats so the API watchdog can alert on failed or missing backups.
#
#   DATABASE_URL=postgres://... BACKUP_DIR=/backups ./backup.sh
#
# Optional:
#   BACKUP_RETENTION_DAYS   days of local dumps to keep (default 14; newest is always kept)
#   RESTORE_DATABASE_URL    server used for the restore check (see verify-restore.sh)
#   BACKUP_S3_URI           off-site copy, e.g. s3://aside-backups/db (Cloudflare R2 works)
#   BACKUP_S3_ENDPOINT      S3 endpoint, e.g. https://<account>.r2.cloudflarestorage.com
#   (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY for the off-site copy, used by the aws CLI)
set -eu

: "${DATABASE_URL:?DATABASE_URL is required}"
BACKUP_DIR=${BACKUP_DIR:-./backups}
RETENTION_DAYS=${BACKUP_RETENTION_DAYS:-14}
HERE=$(cd "$(dirname "$0")" && pwd)
INSTANCE="$(hostname 2>/dev/null || echo backup):$$"

case "$RETENTION_DAYS" in
  ''|*[!0-9]*) echo "BACKUP_RETENTION_DAYS must be a whole number" >&2; exit 2 ;;
esac

log() { printf '{"time":"%s","level":"%s","msg":"%s","component":"backup"}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2"; }

# record_result ok|error [message]: best effort, a reporting failure never hides the backup result.
record_result() {
  if [ "$1" = ok ]; then
    psql "$DATABASE_URL" -X -q -v ON_ERROR_STOP=1 -v inst="$INSTANCE" >/dev/null <<'SQL' || log warn "could not record backup heartbeat"
INSERT INTO worker_heartbeats(name, instance, last_beat_at) VALUES ('backup', :'inst', now())
ON CONFLICT (name) DO UPDATE SET instance = EXCLUDED.instance, last_beat_at = now();
SQL
  else
    # A failure must not refresh last_beat_at, or the stale-backup alert would never fire.
    psql "$DATABASE_URL" -X -q -v ON_ERROR_STOP=1 -v inst="$INSTANCE" -v err="$2" >/dev/null <<'SQL' || log warn "could not record backup failure"
INSERT INTO worker_heartbeats(name, instance, last_beat_at, last_error_at, last_error)
VALUES ('backup', :'inst', 'epoch', now(), left(:'err', 500))
ON CONFLICT (name) DO UPDATE SET instance = EXCLUDED.instance, last_error_at = now(), last_error = EXCLUDED.last_error;
SQL
  fi
}

STEP="starting"
fail() {
  log error "backup failed during: $STEP"
  record_result error "backup failed during: $STEP"
  rm -f "${PARTIAL:-}"
  # Keep a dump that failed its check for investigation, but never under a name
  # that looks like a verified backup.
  if [ -n "${OUT:-}" ] && [ -f "$OUT" ] && [ "$STEP" != "off-site copy" ] && [ "$STEP" != "prune" ]; then
    mv "$OUT" "$OUT.unverified" && rm -f "$OUT.sha256"
  fi
  exit 1
}
trap 'fail' EXIT

mkdir -p "$BACKUP_DIR"
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
NAME="aside-$STAMP.dump"
PARTIAL="$BACKUP_DIR/.$NAME.partial"
OUT="$BACKUP_DIR/$NAME"

STEP="pg_dump"
log info "dumping database to $OUT"
pg_dump --format=custom --compress=6 --no-owner --no-privileges --dbname="$DATABASE_URL" --file="$PARTIAL"
mv "$PARTIAL" "$OUT"
PARTIAL=""

STEP="checksum"
(cd "$BACKUP_DIR" && sha256sum "$NAME" > "$NAME.sha256")

STEP="restore check"
LIVE_DATABASE_URL="$DATABASE_URL" "$HERE/verify-restore.sh" "$OUT"

if [ -n "${BACKUP_S3_URI:-}" ]; then
  STEP="off-site copy"
  command -v aws >/dev/null 2>&1 || { log error "BACKUP_S3_URI is set but the aws CLI is not installed"; exit 1; }
  ENDPOINT_ARG=""
  [ -n "${BACKUP_S3_ENDPOINT:-}" ] && ENDPOINT_ARG="--endpoint-url=$BACKUP_S3_ENDPOINT"
  # shellcheck disable=SC2086
  aws s3 cp $ENDPOINT_ARG --only-show-errors "$OUT" "${BACKUP_S3_URI%/}/$NAME"
  # shellcheck disable=SC2086
  aws s3 cp $ENDPOINT_ARG --only-show-errors "$OUT.sha256" "${BACKUP_S3_URI%/}/$NAME.sha256"
  log info "copied off-site to ${BACKUP_S3_URI%/}/$NAME"
fi

STEP="prune"
# Never prune the dump that was just verified, even with a retention of 0.
find "$BACKUP_DIR" -maxdepth 1 -type f \( -name 'aside-*.dump' -o -name 'aside-*.dump.sha256' -o -name 'aside-*.dump.unverified' \) \
  -mtime +"$RETENTION_DAYS" ! -name "$NAME" ! -name "$NAME.sha256" -print | while read -r old; do
  rm -f "$old" && log info "pruned $(basename "$old")"
done
find "$BACKUP_DIR" -maxdepth 1 -type f -name '.aside-*.partial' -mmin +360 -exec rm -f {} + 2>/dev/null || true

trap - EXIT
record_result ok
log info "backup $NAME verified ($(du -h "$OUT" | cut -f1))"
