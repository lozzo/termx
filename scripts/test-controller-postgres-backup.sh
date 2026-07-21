#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
base_dsn="${MUXVIA_TEST_POSTGRES_DSN:-postgres://127.0.0.1:55432/postgres?sslmode=disable}"
suffix="$$"
source_db="muxvia_backup_source_$suffix"
restore_db="muxvia_backup_restore_$suffix"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/muxvia-backup-test.XXXXXX")"
cleanup() {
  dropdb --if-exists --force --maintenance-db="$base_dsn" "$source_db" >/dev/null 2>&1 || true
  dropdb --if-exists --force --maintenance-db="$base_dsn" "$restore_db" >/dev/null 2>&1 || true
  rm -rf "$temporary_dir"
}
trap cleanup EXIT INT TERM

for command in createdb dropdb psql age-keygen; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required" >&2
    exit 1
  fi
done

createdb --maintenance-db="$base_dsn" "$source_db"
createdb --maintenance-db="$base_dsn" "$restore_db"
source_dsn="postgres://127.0.0.1:55432/$source_db?sslmode=disable"
restore_dsn="postgres://127.0.0.1:55432/$restore_db?sslmode=disable"
psql "$source_dsn" -v ON_ERROR_STOP=1 -f "$repo_root/private/cloud/control-plane/postgres/migrations/0001_controller.sql" >/dev/null
psql "$source_dsn" -v ON_ERROR_STOP=1 -c "INSERT INTO commerce_accounts(account_id,email,projection,password_hash,auth_revision) VALUES('backup-account','backup@muxvia.invalid',decode('00','hex'),decode('01','hex'),1)" >/dev/null

age-keygen -o "$temporary_dir/identity.txt" >/dev/null 2>&1
recipient="$(age-keygen -y "$temporary_dir/identity.txt")"
backup_path="$temporary_dir/controller.tar.age"
MUXVIA_CONTROLLER_POSTGRES_DSN="$source_dsn" \
MUXVIA_BACKUP_AGE_RECIPIENT="$recipient" \
  "$repo_root/scripts/backup-controller-postgres.sh" "$backup_path" >/dev/null
MUXVIA_RESTORE_POSTGRES_DSN="$restore_dsn" \
MUXVIA_BACKUP_AGE_IDENTITY="$temporary_dir/identity.txt" \
  "$repo_root/scripts/restore-controller-postgres.sh" "$backup_path"

restored="$(psql "$restore_dsn" -Atqc "SELECT COUNT(*) FROM commerce_accounts WHERE account_id='backup-account'")"
if [[ "$restored" != "1" ]]; then
  echo "restored Controller PostgreSQL data is incomplete" >&2
  exit 1
fi
printf '%s\n' "Controller PostgreSQL encrypted backup restore passed"
