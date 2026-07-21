#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/muxvia_shortcut_smoke.sh [--bin PATH] [--root PATH] [--keep-root]

Run an isolated core-v2 + tui-v3 shortcut smoke in tmux. The flow verifies:
  live -> panel sticky mode -> Esc -> system mode -> Help overlay -> Esc
  -> copy/history -> Esc -> system quit
and a second attach injects a keyboard capability response plus Kitty CSI-u Ctrl-1.

Failure artifacts are retained under ROOT. Successful temporary roots are removed
unless --keep-root is set.
EOF
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

ROOT=""
BIN=""
KEEP_ROOT=0
ROOT_PROVIDED=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin) BIN="$2"; shift 2 ;;
    --root) ROOT="$2"; ROOT_PROVIDED=1; shift 2 ;;
    --keep-root) KEEP_ROOT=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

need go
need tmux
need python3

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -z "$ROOT" ]]; then
  ROOT="$(mktemp -d "${TMPDIR:-/tmp}/muxvia-shortcut-smoke.XXXXXX")"
fi
if [[ "$ROOT_PROVIDED" == "1" ]]; then
  KEEP_ROOT=1
fi
mkdir -p "$ROOT"
CFG="$ROOT/config"
STATE="$ROOT/state"
SOCK="$ROOT/muxvia.sock"
LOG="$ROOT/muxvia.log"
DAEMON_OUT="$ROOT/daemon.out"
TMUX_SOCKET="$ROOT/tmux.sock"
SESSION="muxvia-shortcut-$$"
CSI_SESSION="muxvia-shortcut-csi-$$"
FAILED=1
DAEMON_PID=""

tmux_cmd() {
  tmux -S "$TMUX_SOCKET" -f /dev/null "$@"
}

cleanup() {
  tmux_cmd kill-session -t "$SESSION" 2>/dev/null || true
  tmux_cmd kill-session -t "$CSI_SESSION" 2>/dev/null || true
  if [[ -n "$DAEMON_PID" ]]; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  if [[ "$FAILED" == "0" && "$KEEP_ROOT" == "0" ]]; then
    rm -rf "$ROOT"
  else
    printf '[muxvia-shortcut-smoke] artifacts: %s\n' "$ROOT" >&2
  fi
}
trap cleanup EXIT

if [[ -z "$BIN" ]]; then
  BIN="$ROOT/muxvia"
  (cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/muxvia)
fi

mkdir -p "$CFG/muxvia" "$STATE"
XDG_CONFIG_HOME="$CFG" XDG_STATE_HOME="$STATE" MUXVIA_REMOTE_ENABLE=false \
  "$BIN" --socket "$SOCK" --log-file "$LOG" daemon >"$DAEMON_OUT" 2>&1 &
DAEMON_PID=$!

wait_for_socket() {
  local attempt
  for attempt in $(seq 1 100); do
    if [[ -S "$SOCK" ]] && python3 - "$SOCK" >/dev/null 2>&1 <<'PY'
import socket
import sys
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(0.2)
try:
    s.connect(sys.argv[1])
finally:
    s.close()
PY
    then
      return 0
    fi
    sleep 0.05
  done
  return 1
}

capture() {
  local session="$1"
  local name="$2"
  tmux_cmd capture-pane -t "$session" -p -S -200 >"$ROOT/$name.txt"
  tmux_cmd capture-pane -t "$session" -ep -S -200 >"$ROOT/$name.ansi.txt"
}

wait_for_text() {
  local session="$1"
  local text="$2"
  local name="$3"
  local attempt
  for attempt in $(seq 1 100); do
    capture "$session" "$name"
    if grep -Fq "$text" "$ROOT/$name.txt"; then
      return 0
    fi
    sleep 0.05
  done
  echo "timed out waiting for '$text' in tmux session $session" >&2
  return 1
}

wait_for_exit() {
  local session="$1"
  local attempt
  for attempt in $(seq 1 100); do
    if ! tmux_cmd has-session -t "$session" 2>/dev/null; then
      return 0
    fi
    sleep 0.05
  done
  return 1
}

if ! wait_for_socket; then
  echo "daemon socket did not become ready: $SOCK" >&2
  exit 1
fi

TERMINAL_ID="$(
  XDG_CONFIG_HOME="$CFG" XDG_STATE_HOME="$STATE" MUXVIA_REMOTE_ENABLE=false \
    "$BIN" --socket "$SOCK" --log-file "$LOG" new --name shortcut-smoke -- \
    /bin/sh -lc 'printf "shortcut-smoke-ready\n"; exec cat'
)"
TERMINAL_ID="$(printf '%s\n' "$TERMINAL_ID" | tr -d '\r' | awk 'NF { value=$0 } END { print value }')"
if [[ -z "$TERMINAL_ID" ]]; then
  echo "muxvia new did not return a terminal id" >&2
  exit 1
fi

printf -v ATTACH 'env -u MUXVIA -u MUXVIA_TERMINAL_ID -u MUXVIA_TUI_DIAG -u MUXVIA_TUI_INPUT_TRACE XDG_CONFIG_HOME=%q XDG_STATE_HOME=%q MUXVIA_REMOTE_ENABLE=false TERM=xterm-256color %q --socket %q --log-file %q attach %q' \
  "$CFG" "$STATE" "$BIN" "$SOCK" "$LOG" "$TERMINAL_ID"
tmux_cmd new-session -d -x 100 -y 28 -s "$SESSION" "$ATTACH"
wait_for_text "$SESSION" "shortcut-smoke-ready" "live"

tmux_cmd send-keys -t "$SESSION" C-p
wait_for_text "$SESSION" "PANE" "panel"
tmux_cmd send-keys -t "$SESSION" Escape
sleep 0.1

tmux_cmd send-keys -t "$SESSION" C-g
wait_for_text "$SESSION" "GLOBAL" "overlay-system"
tmux_cmd send-keys -t "$SESSION" '?'
wait_for_text "$SESSION" "Help" "help"
tmux_cmd send-keys -t "$SESSION" Escape
sleep 0.1

tmux_cmd send-keys -t "$SESSION" C-v
wait_for_text "$SESSION" "COPY" "copy"
tmux_cmd send-keys -t "$SESSION" Escape
sleep 0.1

tmux_cmd send-keys -t "$SESSION" C-g
wait_for_text "$SESSION" "GLOBAL" "system"
tmux_cmd send-keys -t "$SESSION" q
if ! wait_for_exit "$SESSION"; then
  capture "$SESSION" "quit-failure"
  echo "system quit did not exit the primary attach session" >&2
  exit 1
fi

cat >"$CFG/muxvia/tui-v3.yaml" <<'EOF'
version: 1
tui:
  shortcuts:
    global:
      ctrl-1: menu.panel
      ctrl-g: menu.system
    system:
      q: system.quit
EOF

tmux_cmd new-session -d -x 100 -y 28 -s "$CSI_SESSION" "$ATTACH"
wait_for_text "$CSI_SESSION" "shortcut-smoke-ready" "csi-live"
tmux_cmd send-keys -t "$CSI_SESSION" -l $'\033[?1u'
tmux_cmd send-keys -t "$CSI_SESSION" -l $'\033[49;5u'
wait_for_text "$CSI_SESSION" "PANE" "csi-panel"
tmux_cmd send-keys -t "$CSI_SESSION" Escape
sleep 0.1
tmux_cmd send-keys -t "$CSI_SESSION" C-g
wait_for_text "$CSI_SESSION" "GLOBAL" "csi-system"
tmux_cmd send-keys -t "$CSI_SESSION" q
if ! wait_for_exit "$CSI_SESSION"; then
  capture "$CSI_SESSION" "csi-quit-failure"
  echo "system quit did not exit the CSI-u attach session" >&2
  exit 1
fi

FAILED=0
printf '[muxvia-shortcut-smoke] PASS root/sticky/overlay/copy/quit/CSI-u\n'
