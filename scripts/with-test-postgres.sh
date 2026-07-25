#!/usr/bin/env bash

set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo "usage: $0 COMMAND [ARG ...]" >&2
  exit 2
fi

if [[ -n "${MUXVIA_TEST_POSTGRES_DSN:-}" ]]; then
  exec "$@"
fi

postgres_bin=""
for candidate in \
  "$(command -v pg_isready 2>/dev/null || true)" \
  /opt/homebrew/opt/postgresql@17/bin/pg_isready \
  /usr/local/opt/postgresql@17/bin/pg_isready; do
  if [[ -x "$candidate" ]]; then
    postgres_bin="$(dirname "$candidate")"
    break
  fi
done
if [[ -z "$postgres_bin" ]]; then
  echo "PostgreSQL 17 test binaries are required; set MUXVIA_TEST_POSTGRES_DSN or install PostgreSQL" >&2
  exit 1
fi

test_dsn='postgres://127.0.0.1:55432/postgres?sslmode=disable'
if "$postgres_bin/pg_isready" -h 127.0.0.1 -p 55432 >/dev/null 2>&1; then
  export MUXVIA_TEST_POSTGRES_DSN="$test_dsn"
  exec "$@"
fi

data_dir="$(mktemp -d "${TMPDIR:-/tmp}/muxvia-postgres.XXXXXX")"
cleanup() {
  "$postgres_bin/pg_ctl" -D "$data_dir" stop -m fast >/dev/null 2>&1 || true
  rm -rf "$data_dir"
}
trap cleanup EXIT INT TERM

"$postgres_bin/initdb" -D "$data_dir" --auth=trust --no-locale --encoding=UTF8 >/dev/null
"$postgres_bin/pg_ctl" -D "$data_dir" -o '-h 127.0.0.1 -p 55432' -l "$data_dir/server.log" start >/dev/null
export MUXVIA_TEST_POSTGRES_DSN="$test_dsn"
export MUXVIA_TEST_POSTGRES_DATA_DIR="$data_dir"
export MUXVIA_TEST_POSTGRES_BIN="$postgres_bin"
"$@"
