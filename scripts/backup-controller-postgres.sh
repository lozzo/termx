#!/usr/bin/env bash

set -euo pipefail

if [[ -z "${MUXVIA_CONTROLLER_POSTGRES_DSN:-}" ]]; then
  echo "MUXVIA_CONTROLLER_POSTGRES_DSN is required" >&2
  exit 2
fi
if [[ -z "${MUXVIA_BACKUP_AGE_RECIPIENT:-}" ]]; then
  echo "MUXVIA_BACKUP_AGE_RECIPIENT is required" >&2
  exit 2
fi

for command in pg_dump age tar shasum; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required" >&2
    exit 1
  fi
done

schema="${MUXVIA_POSTGRES_SCHEMA:-public}"
output_dir="${MUXVIA_BACKUP_OUTPUT_DIR:-.artifacts/postgres-backups}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
output_path="${1:-$output_dir/muxvia-controller-$timestamp.tar.age}"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/muxvia-backup.XXXXXX")"
cleanup() {
  rm -rf "$temporary_dir"
}
trap cleanup EXIT INT TERM

mkdir -p "$(dirname "$output_path")"
umask 077
pg_dump "$MUXVIA_CONTROLLER_POSTGRES_DSN" \
  --format=custom \
  --schema="$schema" \
  --no-owner \
  --no-privileges \
  --file="$temporary_dir/controller.dump"
(cd "$temporary_dir" && shasum -a 256 controller.dump > controller.dump.sha256)
tar -C "$temporary_dir" -cf "$temporary_dir/controller-backup.tar" controller.dump controller.dump.sha256
age -r "$MUXVIA_BACKUP_AGE_RECIPIENT" -o "$output_path" "$temporary_dir/controller-backup.tar"
chmod 600 "$output_path"

if [[ -n "${MUXVIA_R2_BUCKET:-}" ]]; then
  if [[ -z "${MUXVIA_R2_ENDPOINT_URL:-}" ]] || ! command -v aws >/dev/null 2>&1; then
    echo "R2 upload requires MUXVIA_R2_ENDPOINT_URL and aws CLI" >&2
    exit 1
  fi
  prefix="${MUXVIA_R2_PREFIX:-controller-postgres}"
  aws s3 cp "$output_path" "s3://$MUXVIA_R2_BUCKET/$prefix/$(basename "$output_path")" \
    --endpoint-url "$MUXVIA_R2_ENDPOINT_URL" \
    --only-show-errors
fi

printf '%s\n' "$output_path"
