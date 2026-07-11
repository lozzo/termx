#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/termx_memory_smoke.sh [options]

Run a real-process memory smoke path:
  build/use termx -> start core-v2 daemon -> create stress terminal
  -> attach tui-v3 in tmux -> enter copy latest -> jump oldest
  -> record daemon/TUI RSS.

Options:
  --root PATH           artifact root; default is a new /tmp directory
  --bin PATH            existing termx binary; default builds into ROOT/termx
  --lines N             stress line count; default 30000
  --seed N              stress seed; default 100
  --width-hint N        stress width hint; default 120
  --attach-size CxR     tmux attach size; default 120x36
  --daemon-memory-limit-mb N
                        set TERMX_DAEMON_MEMORY_LIMIT_MB for daemon runtime GC pacing
  --wait-seconds N      max wait for attach/copy markers; default 30
  --keep-root           keep artifact root after success/failure (default)
  --cleanup-root        remove artifact root on exit
  -h, --help            show this help

Artifacts:
  memory.tsv            RSS samples in KiB and MiB
  live.txt              tmux capture after attach
  copy-latest.txt       capture after entering copy/history latest
  copy-oldest.txt       capture after jumping to oldest history
  daemon-heap/          daemon heap profiles captured at RSS sample points
  tui-heap/             TUI heap profiles when the runtime crosses its heap threshold
EOF
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

log() {
  printf '[termx-memory-smoke] %s\n' "$*"
}

parse_size() {
  local value="$1"
  local cols="${value%x*}"
  local rows="${value#*x}"
  if [[ -z "$cols" || -z "$rows" || "$cols" == "$value" || "$rows" == "$value" ]]; then
    echo "invalid size: $value (want COLSxROWS)" >&2
    exit 1
  fi
  printf '%s %s\n' "$cols" "$rows"
}

shell_quote() {
  printf '%q' "$1"
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

wait_for_file_value() {
  local path="$1"
  local deadline="$2"
  local attempt
  for attempt in $(seq 1 "$deadline"); do
    if [[ -s "$path" ]]; then
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_for_capture() {
  local target="$1"
  local pattern="$2"
  local deadline="$3"
  local attempt
  for attempt in $(seq 1 "$deadline"); do
    if tmux capture-pane -t "$target" -p -S -2000 2>/dev/null | LC_ALL=C grep -Fq "$pattern"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

rss_kib() {
  local pid="$1"
  ps -o rss= -p "$pid" 2>/dev/null | awk 'NF { print $1 + 0; found=1 } END { if (!found) print 0 }'
}

record_rss() {
  local stage="$1"
  local process="$2"
  local pid="$3"
  if [[ "$process" == "daemon" && -n "${DAEMON_HEAP_DIR:-}" ]] && kill -0 "$pid" 2>/dev/null; then
    kill -USR1 "$pid" 2>/dev/null || true
    sleep 0.2
  fi
  local rss
  rss="$(rss_kib "$pid")"
  awk -v stage="$stage" -v process="$process" -v pid="$pid" -v rss="$rss" 'BEGIN {
    printf "%s\t%s\t%s\t%d\t%.1f\n", stage, process, pid, rss, rss / 1024
  }' >>"$REPORT"
}

capture_pane() {
  local target="$1"
  local name="$2"
  tmux capture-pane -t "$target" -p -S -2000 >"$ROOT/$name.txt" || true
  tmux capture-pane -t "$target" -ep -S -2000 >"$ROOT/$name.raw.txt" || true
}

ROOT=""
BIN=""
LINES=30000
SEED=100
WIDTH_HINT=120
ATTACH_SIZE="120x36"
WAIT_SECONDS=30
CLEANUP_ROOT=0
DAEMON_MEMORY_LIMIT_MB=""

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
    --width-hint)
      WIDTH_HINT="$2"
      shift 2
      ;;
    --attach-size)
      ATTACH_SIZE="$2"
      shift 2
      ;;
    --daemon-memory-limit-mb)
      DAEMON_MEMORY_LIMIT_MB="$2"
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

case "$LINES" in
  ''|*[!0-9]*)
    echo "--lines must be a positive integer" >&2
    exit 1
    ;;
esac
case "$WAIT_SECONDS" in
  ''|*[!0-9]*)
    echo "--wait-seconds must be a positive integer" >&2
    exit 1
    ;;
esac
case "$DAEMON_MEMORY_LIMIT_MB" in
  ''|*[!0-9]*)
    if [[ -n "$DAEMON_MEMORY_LIMIT_MB" ]]; then
      echo "--daemon-memory-limit-mb must be a positive integer" >&2
      exit 1
    fi
    ;;
esac
if [[ -n "$DAEMON_MEMORY_LIMIT_MB" && "$DAEMON_MEMORY_LIMIT_MB" -le 0 ]]; then
  echo "--daemon-memory-limit-mb must be a positive integer" >&2
  exit 1
fi

need awk
need go
need grep
need ps
need python3
need tmux

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
read -r ATTACH_COLS ATTACH_ROWS < <(parse_size "$ATTACH_SIZE")

if [[ -z "$ROOT" ]]; then
  ROOT="$(mktemp -d "${TMPDIR:-/tmp}/termx-memory-smoke.XXXXXX")"
else
  mkdir -p "$ROOT"
fi
ROOT="$(cd "$ROOT" && pwd)"

SOCK="$ROOT/core.sock"
LOG="$ROOT/termx.log"
REPORT="$ROOT/memory.tsv"
DAEMON_HEAP_DIR="$ROOT/daemon-heap"
TUI_HEAP_DIR="$ROOT/tui-heap"
SESSION="termx-memory-$$"
TARGET="$SESSION:0.0"
ATTACH_PID_FILE="$ROOT/tui.pid"
TERMINAL_ID_FILE="$ROOT/terminal.id"
DAEMON_PID=""
TERMINAL_ID=""

cleanup() {
  set +e
  if tmux has-session -t "$SESSION" 2>/dev/null; then
    tmux kill-session -t "$SESSION" 2>/dev/null
  fi
  if [[ -n "$TERMINAL_ID" && -x "$BIN" ]]; then
    "$BIN" --socket "$SOCK" --log-file "$LOG" v3 kill "$TERMINAL_ID" >/dev/null 2>&1
    "$BIN" --socket "$SOCK" --log-file "$LOG" v3 rm "$TERMINAL_ID" >/dev/null 2>&1
  fi
  if [[ -n "$DAEMON_PID" ]] && kill -0 "$DAEMON_PID" 2>/dev/null; then
    kill "$DAEMON_PID" 2>/dev/null
    wait "$DAEMON_PID" 2>/dev/null
  fi
  if [[ "$CLEANUP_ROOT" == "1" ]]; then
    rm -rf "$ROOT"
  fi
}
trap cleanup EXIT

log "artifact root: $ROOT"

if [[ -z "$BIN" ]]; then
  BIN="$ROOT/termx"
  log "building termx binary"
  (cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/termx)
else
  BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"
fi

if [[ ! -x "$BIN" ]]; then
  echo "termx binary is not executable: $BIN" >&2
  exit 1
fi

printf 'stage\tprocess\tpid\trss_kib\trss_mib\n' >"$REPORT"
mkdir -p "$TUI_HEAP_DIR"
mkdir -p "$DAEMON_HEAP_DIR"

log "starting daemon"
if [[ -n "$DAEMON_MEMORY_LIMIT_MB" ]]; then
  log "daemon memory limit: ${DAEMON_MEMORY_LIMIT_MB}MB"
  TERMX_DAEMON_MEMORY_LIMIT_MB="$DAEMON_MEMORY_LIMIT_MB" TERMX_DAEMON_HEAP_PROFILE_DIR="$DAEMON_HEAP_DIR" "$BIN" --socket "$SOCK" --log-file "$LOG" daemon >"$ROOT/daemon.stdout" 2>"$ROOT/daemon.stderr" &
else
  TERMX_DAEMON_HEAP_PROFILE_DIR="$DAEMON_HEAP_DIR" "$BIN" --socket "$SOCK" --log-file "$LOG" daemon >"$ROOT/daemon.stdout" 2>"$ROOT/daemon.stderr" &
fi
DAEMON_PID=$!
if ! wait_for_socket "$SOCK" "$WAIT_SECONDS"; then
  echo "daemon socket did not become ready: $SOCK" >&2
  exit 1
fi
record_rss "daemon_idle" "daemon" "$DAEMON_PID"

STRESS_SCRIPT="$REPO_ROOT/scripts/generate_terminal_stress.py"
DONE_MARKER="TERM_X_MEMORY_STRESS_DONE"
STRESS_CMD="python3 $(shell_quote "$STRESS_SCRIPT") --lines $LINES --seed $SEED --width-hint $WIDTH_HINT; printf '\\n$DONE_MARKER\\n'; sleep 300"

log "creating stress terminal: lines=$LINES seed=$SEED"
TERMINAL_ID="$("$BIN" --socket "$SOCK" --log-file "$LOG" v3 new --name memory-smoke -- /bin/sh -lc "$STRESS_CMD")"
printf '%s\n' "$TERMINAL_ID" >"$TERMINAL_ID_FILE"

ATTACH_SCRIPT="$ROOT/attach.sh"
ATTACH_PID_QUOTED="$(shell_quote "$ATTACH_PID_FILE")"
TUI_HEAP_DIR_QUOTED="$(shell_quote "$TUI_HEAP_DIR")"
BIN_QUOTED="$(shell_quote "$BIN")"
SOCK_QUOTED="$(shell_quote "$SOCK")"
LOG_QUOTED="$(shell_quote "$LOG")"
TERMINAL_ID_QUOTED="$(shell_quote "$TERMINAL_ID")"
cat >"$ATTACH_SCRIPT" <<EOF
#!/bin/sh
echo "\$\$" > $ATTACH_PID_QUOTED
export TERM=xterm-256color
export TERMX_ALLOW_NESTED=1
export TERMX_TUI_HEAP_PROFILE_DIR=$TUI_HEAP_DIR_QUOTED
export TERMX_TUI_HEAP_PROFILE_EVERY_MB=8
export TERMX_TUI_DIAG=1
export TERMX_TUI_DIAG_INTERVAL_MS=200
exec $BIN_QUOTED --socket $SOCK_QUOTED --log-file $LOG_QUOTED v3 attach $TERMINAL_ID_QUOTED
EOF
chmod +x "$ATTACH_SCRIPT"

log "starting tmux attach session: $SESSION"
tmux new-session -d -x "$ATTACH_COLS" -y "$ATTACH_ROWS" -s "$SESSION" /bin/sh "$ATTACH_SCRIPT"
if ! wait_for_file_value "$ATTACH_PID_FILE" "$WAIT_SECONDS"; then
  echo "TUI PID file did not appear: $ATTACH_PID_FILE" >&2
  exit 1
fi
TUI_PID="$(tr -dc '0-9' <"$ATTACH_PID_FILE")"
if [[ -z "$TUI_PID" ]]; then
  echo "empty TUI PID file: $ATTACH_PID_FILE" >&2
  exit 1
fi

if ! wait_for_capture "$TARGET" "$DONE_MARKER" "$WAIT_SECONDS"; then
  capture_pane "$TARGET" "live-timeout"
  echo "stress terminal did not reach done marker within ${WAIT_SECONDS}s" >&2
  exit 1
fi
capture_pane "$TARGET" "live"
record_rss "daemon_after_stress" "daemon" "$DAEMON_PID"
record_rss "tui_attached" "tui" "$TUI_PID"

log "entering copy/history latest"
tmux send-keys -t "$TARGET" C-v
sleep 2
capture_pane "$TARGET" "copy-latest"
record_rss "daemon_copy_latest" "daemon" "$DAEMON_PID"
record_rss "tui_copy_latest" "tui" "$TUI_PID"

log "jumping to oldest history page"
tmux send-keys -t "$TARGET" g
sleep 3
capture_pane "$TARGET" "copy-oldest"
record_rss "daemon_copy_oldest" "daemon" "$DAEMON_PID"
record_rss "tui_copy_oldest" "tui" "$TUI_PID"

log "RSS report"
if command -v column >/dev/null 2>&1; then
  column -t -s $'\t' "$REPORT"
else
  cat "$REPORT"
fi

log "artifacts kept at: $ROOT"
