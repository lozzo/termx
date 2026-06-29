#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/termx_core_history_smoke.sh [options]

Run a non-rendering core-v2 infinite history smoke:
  build/use termx -> start isolated core-v2 daemon -> run a finite stress terminal
  -> wait for exit -> dump authoritative history.window pages -> verify oldest/newest
  markers and file-backed history payloads.

Options:
  --root PATH        artifact root; default is a new /tmp directory
  --bin PATH         existing termx binary; default builds into ROOT/termx
  --lines N          stress line count; default 2000
  --seed N           stress seed; default 100
  --cols N           history projection columns; default 120
  --limit N          rows per history.window page; default 256
  --wait-seconds N   max wait for daemon and terminal exit; default 30
  --keep-root        keep artifact root after success/failure (default)
  --cleanup-root     remove artifact root on exit
  -h, --help         show this help

Artifacts:
  history.dump.txt   output from termx v3 history-dump
  ls.final.tsv       final terminal inventory
  termx.log          daemon/client log
  state/termx/history-v2/
                    file-backed core-v2 history payloads
EOF
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

log() {
  printf '[termx-core-history-smoke] %s\n' "$*"
}

positive_int() {
  local name="$1"
  local value="$2"
  case "$value" in
    ''|*[!0-9]*)
      echo "$name must be a positive integer" >&2
      exit 1
      ;;
  esac
  if [[ "$value" -le 0 ]]; then
    echo "$name must be a positive integer" >&2
    exit 1
  fi
}

wait_for_socket() {
  local socket_path="$1"
  local deadline="$2"
  local attempt
  for attempt in $(seq 1 "$deadline"); do
    if [[ -S "$socket_path" ]] && python3 - "$socket_path" >/dev/null 2>&1 <<'PY'
import socket
import sys

sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.settimeout(0.2)
try:
    sock.connect(sys.argv[1])
finally:
    sock.close()
PY
    then
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_for_terminal_state() {
  local terminal_id="$1"
  local want="$2"
  local deadline="$3"
  local attempt
  for attempt in $(seq 1 "$deadline"); do
    "$BIN" --socket "$SOCK" --log-file "$LOG_FILE" ls >"$ROOT/ls.final.tsv" 2>/dev/null || true
    if awk -F '\t' -v id="$terminal_id" -v state="$want" '$1 == id && $4 == state { found=1 } END { exit(found ? 0 : 1) }' "$ROOT/ls.final.tsv"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

history_file_count() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    printf '0\n'
    return 0
  fi
  find "$dir" -type f -name '*.history-lines.bin' -print | wc -l | awk '{ print $1 + 0 }'
}

ROOT=""
BIN=""
LINES=2000
SEED=100
COLS=120
LIMIT=256
WAIT_SECONDS=30
CLEANUP_ROOT=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --root)
      ROOT="$2"
      shift 2
      ;;
    --bin)
      BIN="$2"
      shift 2
      ;;
    --lines)
      LINES="$2"
      shift 2
      ;;
    --seed)
      SEED="$2"
      shift 2
      ;;
    --cols)
      COLS="$2"
      shift 2
      ;;
    --limit)
      LIMIT="$2"
      shift 2
      ;;
    --wait-seconds)
      WAIT_SECONDS="$2"
      shift 2
      ;;
    --keep-root)
      CLEANUP_ROOT=0
      shift
      ;;
    --cleanup-root)
      CLEANUP_ROOT=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

positive_int "--lines" "$LINES"
positive_int "--seed" "$SEED"
positive_int "--cols" "$COLS"
positive_int "--limit" "$LIMIT"
positive_int "--wait-seconds" "$WAIT_SECONDS"

need awk
need find
need go
need grep
need python3
need wc

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -z "$ROOT" ]]; then
  ROOT="$(mktemp -d "${TMPDIR:-/tmp}/termx-core-history-smoke.XXXXXX")"
else
  mkdir -p "$ROOT"
fi
ROOT="$(cd "$ROOT" && pwd)"

SOCK="$ROOT/termx-core-v2.sock"
LOG_FILE="$ROOT/termx.log"
DUMP="$ROOT/history.dump.txt"
CFG_HOME="$ROOT/config"
STATE_HOME="$ROOT/state"
RUNTIME_DIR="$ROOT/runtime"
HISTORY_DIR="$STATE_HOME/termx/history-v2"
EMITTER="$ROOT/emit_core_history_lines.py"
TERMINAL_ID=""
DAEMON_PID=""

cleanup() {
  set +e
  if [[ -n "$DAEMON_PID" ]]; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  if [[ "$CLEANUP_ROOT" == "1" ]]; then
    rm -rf "$ROOT"
  else
    log "artifacts kept at $ROOT"
  fi
}
trap cleanup EXIT

mkdir -p "$CFG_HOME" "$STATE_HOME" "$RUNTIME_DIR"
cat >"$EMITTER" <<'PY'
#!/usr/bin/env python3
import argparse
import sys

parser = argparse.ArgumentParser()
parser.add_argument("--lines", type=int, required=True)
parser.add_argument("--seed", type=int, required=True)
args = parser.parse_args()
if args.lines <= 0:
    raise SystemExit("--lines must be positive")

for index in range(0, args.lines + 1):
    sys.stdout.write(f"TERM_X_HISTORY {index:06d} seed={args.seed}\n")
sys.stdout.flush()
PY
export XDG_CONFIG_HOME="$CFG_HOME"
export XDG_STATE_HOME="$STATE_HOME"
export XDG_RUNTIME_DIR="$RUNTIME_DIR"
export TERMX_REMOTE_ENABLE=false

if [[ -z "$BIN" ]]; then
  BIN="$ROOT/termx"
  log "building termx binary"
  (cd "$REPO_ROOT" && go build -o "$BIN" ./termx-cli/cmd/termx)
else
  BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"
fi

log "starting core-v2 daemon"
"$BIN" --socket "$SOCK" --log-file "$LOG_FILE" daemon &
DAEMON_PID="$!"
if ! wait_for_socket "$SOCK" "$WAIT_SECONDS"; then
  echo "timed out waiting for daemon socket: $SOCK" >&2
  exit 1
fi

log "creating finite stress terminal"
TERMINAL_ID="$(
  "$BIN" --socket "$SOCK" --log-file "$LOG_FILE" new -- \
    python3 "$EMITTER" \
      --lines "$LINES" \
      --seed "$SEED"
)"
TERMINAL_ID="$(printf '%s\n' "$TERMINAL_ID" | awk 'NF { last=$0 } END { print last }')"
if [[ -z "$TERMINAL_ID" ]]; then
  echo "failed to create terminal" >&2
  exit 1
fi

if ! wait_for_terminal_state "$TERMINAL_ID" "exited" "$WAIT_SECONDS"; then
  echo "timed out waiting for terminal exit: $TERMINAL_ID" >&2
  exit 1
fi

log "dumping authoritative history"
"$BIN" --socket "$SOCK" --log-file "$LOG_FILE" v3 history-dump "$TERMINAL_ID" --out "$DUMP" --cols "$COLS" --limit "$LIMIT" >/dev/null

if ! grep -Fq "text=\"TERM_X_HISTORY 000000 seed=$SEED\"" "$DUMP"; then
  echo "history dump is missing oldest stress marker 000000" >&2
  exit 1
fi
LAST_LINE="$(printf '%06d' "$LINES")"
if ! grep -Fq "text=\"TERM_X_HISTORY $LAST_LINE seed=$SEED\"" "$DUMP"; then
  echo "history dump is missing newest stress marker $LAST_LINE" >&2
  exit 1
fi
if ! grep -Fq 'output_order=oldest-to-newest' "$DUMP"; then
  echo "history dump did not declare oldest-to-newest output order" >&2
  exit 1
fi

FILE_COUNT="$(history_file_count "$HISTORY_DIR")"
if [[ "$FILE_COUNT" -le 0 ]]; then
  echo "file-backed history payloads were not created under $HISTORY_DIR" >&2
  exit 1
fi

log "ok: terminal=$TERMINAL_ID lines=$LINES dump=$DUMP history_files=$FILE_COUNT"
