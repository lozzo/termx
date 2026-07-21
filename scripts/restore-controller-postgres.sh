#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]] || [[ -z "${MUXVIA_RESTORE_POSTGRES_DSN:-}" ]] || [[ -z "${MUXVIA_BACKUP_AGE_IDENTITY:-}" ]]; then
  echo "usage: MUXVIA_RESTORE_POSTGRES_DSN=... MUXVIA_BACKUP_AGE_IDENTITY=... $0 BACKUP.tar.age" >&2
  exit 2
fi

for command in pg_restore age tar shasum; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required" >&2
    exit 1
  fi
done

backup_path="$1"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/muxvia-restore.XXXXXX")"
cleanup() {
  rm -rf "$temporary_dir"
}
trap cleanup EXIT INT TERM

umask 077
age -d -i "$MUXVIA_BACKUP_AGE_IDENTITY" -o "$temporary_dir/controller-backup.tar" "$backup_path"
tar -C "$temporary_dir" -xf "$temporary_dir/controller-backup.tar"
(cd "$temporary_dir" && shasum -a 256 -c controller.dump.sha256)
pg_restore \
  --dbname="$MUXVIA_RESTORE_POSTGRES_DSN" \
  --clean \
  --if-exists \
  --no-owner \
  --no-privileges \
  --exit-on-error \
  "$temporary_dir/controller.dump"
