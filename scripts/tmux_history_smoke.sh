#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/tmux_history_smoke.sh [options]

Run the isolated TermX + tmux history smoke flow:
  daemon -> create stress terminal -> attach -> wait_surface
  -> copy-mode repeated g -> capture
  -> resize -> capture
  -> reattach -> capture

Options:
  --scenario NAME       scenario to run: baseline | standard | deep-live-tail | history-semantics | bg-forensics | floating-owner-resize | floating-owner-reattach-history | floating-owner-wheel-history | floating-owner-marker-history | floating-owner-marker-wheel-history | floating-owner-remote-pair-wheel-history; default baseline
  --root PATH           artifact root; default is a new /tmp directory
  --bin PATH            existing termx binary to use; default builds into ROOT/termx
  --lines N             stress line count; default 1000
  --seed N              stress seed; default 100
  --width-hint N        stress width hint; default 120
  --attach-size CxR     initial tmux attach size; default 120x36
  --resize-size CxR     resize target; default 90x28
  --g-repeats N         repeated g count in copy-mode; default 24
  --keep-root           keep artifact root after success/failure (default)
  --cleanup-root        remove artifact root on exit
  -h, --help            show this help

Artifacts:
  baseline:
    copy-top.txt
    copy-top.raw.txt
    copy-top-resized.txt
    copy-top-resized.raw.txt
    reattach-top.txt
    reattach-top.raw.txt
  deep-live-tail:
    latest-tail.txt
    latest-tail.raw.txt
    copy-committed-boundary.txt
    copy-committed-boundary.raw.txt
    copy-committed-boundary.gridtrace.summary.txt
    copy-committed-boundary.gridtrace.log
    reattach-latest-tail.txt
    reattach-latest-tail.raw.txt
    reattach-committed-boundary.txt
    reattach-committed-boundary.raw.txt
    reattach-committed-boundary.gridtrace.summary.txt
    reattach-committed-boundary.gridtrace.log
  history-semantics:
    history-semantics-live.txt
    history-semantics-live.raw-preserve.txt
    history-semantics-copy-latest.txt
    history-semantics-copy-latest.raw-preserve.txt
    history-semantics-copy-top.txt
    history-semantics-copy-top.raw-preserve.txt
  bg-forensics:
    bg-forensics-input.raw
    bg-forensics-live.raw.txt
    bg-forensics-copy-tail.raw.txt
    bg-forensics.report.txt
  floating-owner-resize:
    floating-base.txt
    floating-base.raw.txt
    floating-base.termx-ls.txt
    floating-manager.txt
    floating-manager.raw.txt
    floating-attached.txt
    floating-attached.raw.txt
    floating-owned.txt
    floating-owned.raw.txt
    floating-resized.txt
    floating-resized.raw.txt
    floating-resized.termx-ls.txt
    floating-owner-resize.resize.summary.txt
  floating-owner-reattach-history:
    floating-reattach-live.txt
    floating-reattach-copy-top.txt
    floating-reattach-copy-top.gridtrace.summary.txt
    floating-owner-reattach-history.resize.summary.txt
  floating-owner-wheel-history:
    floating-wheel-resized.txt
    floating-wheel-after-wheel.txt
    floating-owner-wheel-history.resize.summary.txt
  floating-owner-marker-history:
    floating-marker-resized.txt
    floating-marker-after-marker.txt
    floating-owner-marker-history.resize.summary.txt
  floating-owner-marker-wheel-history:
    floating-marker-wheel-resized.txt
    floating-marker-wheel-after-marker.txt
    floating-owner-marker-wheel-history.resize.summary.txt
  floating-owner-remote-pair-wheel-history:
    floating-remote-pair-wheel-resized.txt
    floating-remote-pair-wheel-after-qr.txt
    floating-remote-pair-wheel-after-expires.txt
    floating-remote-pair-wheel-grid-dump.txt
    floating-remote-pair-wheel-continuity.txt
    floating-owner-remote-pair-wheel-history.resize.summary.txt
EOF
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

log() {
  printf '[tmux-history-smoke] %s\n' "$*"
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

preferred_tmux_session_size() {
  local size
  size="$(
    tmux list-clients -F '#{client_width} #{client_height}' 2>/dev/null \
      | awk -v minw="$ATTACH_COLS" -v minh="$ATTACH_ROWS" '
          BEGIN { bestw=minw+0; besth=minh+0 }
          NF >= 2 {
            w=$1+0
            h=$2+0
            if (w > bestw || (w == bestw && h > besth)) {
              bestw=w
              besth=h
            }
          }
          END { printf "%d %d\n", bestw, besth }
        '
  )"
  if [[ -z "$size" ]]; then
    printf '%s %s\n' "$ATTACH_COLS" "$ATTACH_ROWS"
    return
  fi
  printf '%s\n' "$size"
}

run_in_pty() {
  local cols="$1"
  local rows="$2"
  shift 2
  python3 - "$cols" "$rows" "$@" <<'PY'
import fcntl
import os
import pty
import select
import struct
import subprocess
import sys
import termios

cols = int(sys.argv[1])
rows = int(sys.argv[2])
cmd = sys.argv[3:]

master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
proc = subprocess.Popen(cmd, stdin=slave, stdout=slave, stderr=slave, close_fds=True)
os.close(slave)

chunks = []
while True:
    ready, _, _ = select.select([master], [], [], 0.1)
    if master in ready:
      try:
        data = os.read(master, 4096)
      except OSError:
        data = b""
      if data:
        chunks.append(data)
    if proc.poll() is not None:
      while True:
        ready, _, _ = select.select([master], [], [], 0.05)
        if master not in ready:
          break
        try:
          data = os.read(master, 4096)
        except OSError:
          data = b""
        if not data:
          break
        chunks.append(data)
      break

os.close(master)
sys.stdout.buffer.write(b"".join(chunks))
raise SystemExit(proc.wait())
PY
}

wait_for_daemon() {
  local attempt
  for attempt in $(seq 1 "$WAIT_ATTEMPTS"); do
    if [[ -S "$SOCK" ]] && python3 - "$SOCK" >/dev/null 2>&1 <<'PY'
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
    sleep "$WAIT_DELAY"
  done
  echo "timed out waiting for daemon socket: $SOCK" >&2
  return 1
}

wait_for_terminal() {
  local terminal_id="$1"
  local listing
  local attempt
  for attempt in $(seq 1 "$WAIT_ATTEMPTS"); do
    listing="$(
      XDG_CONFIG_HOME="$CFG" \
      XDG_STATE_HOME="$STATE" \
      TERMX_REMOTE_ENABLE=false \
      "$BIN" --socket "$SOCK" --log-file "$LOG" ls 2>/dev/null || true
    )"
    if printf '%s\n' "$listing" | awk -F '\t' -v want="$terminal_id" '$1 == want { found=1 } END { exit(found ? 0 : 1) }'; then
      return 0
    fi
    sleep "$WAIT_DELAY"
  done
  echo "timed out waiting for terminal inventory entry: $terminal_id" >&2
  return 1
}

cleanup_tmux_session() {
  local session="$1"
  if [[ -z "$session" ]] || ! tmux_session_recorded "$session"; then
    return 0
  fi
  tmux kill-session -t "$session" 2>/dev/null || true
  unregister_tmux_session "$session"
}

sanitize_tmux_name() {
  local value="$1"
  value="$(printf '%s' "$value" | tr -cs 'A-Za-z0-9_-' '-')"
  value="${value#-}"
  value="${value%-}"
  if [[ -z "$value" ]]; then
    value="termx-grid-smoke"
  fi
  printf '%s\n' "${value:0:48}"
}

tmux_session_exists() {
  local session="$1"
  [[ -n "$session" ]] && tmux has-session -t "$session" 2>/dev/null
}

tmux_session_recorded() {
  local session="$1"
  [[ -n "$session" && -f "$TMUX_SESSIONS_FILE" ]] || return 1
  grep -Fxq -- "$session" "$TMUX_SESSIONS_FILE"
}

register_tmux_session() {
  local session="$1"
  if [[ -z "$session" ]] || tmux_session_recorded "$session"; then
    return 0
  fi
  printf '%s\n' "$session" >>"$TMUX_SESSIONS_FILE"
}

unregister_tmux_session() {
  local session="$1"
  local tmp
  [[ -f "$TMUX_SESSIONS_FILE" ]] || return 0
  tmp="$(mktemp "$ROOT/.tmux-sessions.XXXXXX")"
  if ! grep -Fxv -- "$session" "$TMUX_SESSIONS_FILE" >"$tmp"; then
    : >"$tmp"
  fi
  mv "$tmp" "$TMUX_SESSIONS_FILE"
}

first_owned_tmux_session() {
  [[ -f "$TMUX_SESSIONS_FILE" ]] || return 1
  awk 'NF { print; found=1; exit } END { exit(found ? 0 : 1) }' "$TMUX_SESSIONS_FILE"
}

tmux_target_exists() {
  local target="$1"
  [[ -n "$target" ]] && tmux display-message -p -t "$target" '#{pane_id}' >/dev/null 2>&1
}

write_tmux_diagnostics() {
  local base="$1"
  local session="$2"
  local target="$3"
  local session_exists="0"
  local target_exists="0"

  if tmux_session_exists "$session"; then
    session_exists="1"
  fi
  if tmux_target_exists "$target"; then
    target_exists="1"
  fi

  {
    printf 'session=%s\n' "$session"
    printf 'target=%s\n' "$target"
    printf 'session_exists=%s\n' "$session_exists"
    printf 'target_exists=%s\n' "$target_exists"
  } >"$ROOT/$base.tmux-state.txt"

  tmux ls >"$ROOT/$base.tmux-ls.txt" 2>&1 || true
  tmux list-panes -a -F '#{session_name}|||#{window_index}|||#{pane_index}|||#{pane_id}|||#{pane_dead}|||#{pane_dead_status}|||#{pane_current_command}|||#{pane_width}|||#{pane_height}|||#{pane_title}' \
    >"$ROOT/$base.tmux-list-panes.txt" 2>&1 || true
  tmux show-messages >"$ROOT/$base.tmux-show-messages.txt" 2>&1 || true
  if [[ -n "$session" ]]; then
    tmux list-panes -t "$session" -F '#{session_name}|||#{window_index}|||#{pane_index}|||#{pane_id}|||#{pane_dead}|||#{pane_dead_status}|||#{pane_current_command}|||#{pane_width}|||#{pane_height}|||#{pane_title}' \
      >"$ROOT/$base.tmux-session-panes.txt" 2>&1 || true
  fi
  if [[ -n "$target" ]]; then
    tmux display-message -p -t "$target" '#{session_name}|||#{window_index}|||#{pane_index}|||#{pane_id}|||#{pane_dead}|||#{pane_dead_status}|||#{pane_current_command}|||#{pane_width}|||#{pane_height}|||#{pane_title}' \
      >"$ROOT/$base.tmux-target.txt" 2>&1 || true
  fi
}

write_termx_log_excerpt() {
  local base="$1"
  if [[ -f "$LOG" ]]; then
    tail -n 200 "$LOG" >"$ROOT/$base.termx-log.txt" 2>&1 || true
    return 0
  fi
  printf 'missing termx log: %s\n' "$LOG" >"$ROOT/$base.termx-log.txt"
}

write_termx_inventory_artifact() {
  local base="$1"
  XDG_CONFIG_HOME="$CFG" \
  XDG_STATE_HOME="$STATE" \
  TERMX_REMOTE_ENABLE=false \
  "$BIN" --socket "$SOCK" --log-file "$LOG" ls >"$ROOT/$base.termx-ls.txt" 2>&1 || true
}

ensure_tmux_target() {
  local session="$1"
  local target="$2"
  local base="$3"
  if tmux_target_exists "$target"; then
    return 0
  fi
  write_tmux_diagnostics "$base" "$session" "$target"
  echo "tmux pane target unavailable: session=$session target=$target" >&2
  return 1
}

write_tmux_command_error() {
  local kind="$1"
  local base="$2"
  local session="$3"
  local target="$4"
  local status="$5"
  local stderr_file="$6"
  shift 6
  local artifact_base="tmux-${kind}-error.${base}"

  write_tmux_diagnostics "$artifact_base" "$session" "$target"
  {
    printf 'session=%s\n' "$session"
    printf 'target=%s\n' "$target"
    printf 'status=%s\n' "$status"
    printf 'command='
    printf '%q ' "$@"
    printf '\n'
    printf 'stderr:\n'
    cat "$stderr_file"
  } >"$ROOT/$artifact_base.txt"
}

run_tmux_capture() {
  local session="$1"
  local target="$2"
  local base="$3"
  local label="$4"
  shift 4
  local stdout_file
  local stderr_file
  local status
  local artifact_base="tmux-capture-error.${base}.${label}"

  stdout_file="$(mktemp "$ROOT/.tmux-capture.${label}.stdout.XXXXXX")"
  stderr_file="$(mktemp "$ROOT/.tmux-capture.${label}.stderr.XXXXXX")"
  if "$@" >"$stdout_file" 2>"$stderr_file"; then
    cat "$stdout_file"
    rm -f "$stdout_file" "$stderr_file"
    return 0
  fi

  status=$?
  write_tmux_diagnostics "$artifact_base" "$session" "$target"
  {
    printf 'session=%s\n' "$session"
    printf 'target=%s\n' "$target"
    printf 'status=%s\n' "$status"
    printf 'command='
    printf '%q ' "$@"
    printf '\n'
    printf 'stderr:\n'
    cat "$stderr_file"
  } >"$ROOT/$artifact_base.txt"
  if [[ -s "$stdout_file" ]]; then
    cp "$stdout_file" "$ROOT/$artifact_base.stdout.txt"
  fi
  rm -f "$stdout_file" "$stderr_file"
  return "$status"
}

send_tmux_keys() {
  local session="$1"
  local target="$2"
  local base="$3"
  shift 3
  local stderr_file
  local status

  ensure_tmux_target "$session" "$target" "$base.send-target-missing" || return 1
  stderr_file="$(mktemp "$ROOT/.tmux-send.stderr.XXXXXX")"
  if tmux send-keys -t "$target" "$@" 2>"$stderr_file"; then
    rm -f "$stderr_file"
    return 0
  fi

  status=$?
  write_tmux_command_error "send" "$base" "$session" "$target" "$status" "$stderr_file" tmux send-keys -t "$target" "$@"
  rm -f "$stderr_file"
  return "$status"
}

send_tmux_literal() {
  local session="$1"
  local target="$2"
  local base="$3"
  local text="$4"
  local stderr_file
  local status

  ensure_tmux_target "$session" "$target" "$base.send-literal-target-missing" || return 1
  stderr_file="$(mktemp "$ROOT/.tmux-send-literal.stderr.XXXXXX")"
  if tmux send-keys -t "$target" -l "$text" 2>"$stderr_file"; then
    rm -f "$stderr_file"
    return 0
  fi

  status=$?
  write_tmux_command_error "send-literal" "$base" "$session" "$target" "$status" "$stderr_file" tmux send-keys -t "$target" -l "$text"
  rm -f "$stderr_file"
  return "$status"
}

send_tmux_mouse_wheel_up() {
  local session="$1"
  local target="$2"
  local base="$3"
  local x="$4"
  local y="$5"
  local seq

  ensure_tmux_target "$session" "$target" "$base.mouse-target-missing" || return 1
  seq="$(printf '\033[<64;%d;%dM' "$x" "$y")"
  tmux send-keys -t "$target" -l "$seq"
}

send_tmux_mouse_wheel_down() {
  local session="$1"
  local target="$2"
  local base="$3"
  local x="$4"
  local y="$5"
  local seq

  ensure_tmux_target "$session" "$target" "$base.mouse-target-missing" || return 1
  seq="$(printf '\033[<65;%d;%dM' "$x" "$y")"
  tmux send-keys -t "$target" -l "$seq"
}

start_tmux_session() {
  local session="$1"
  local cols="$2"
  local rows="$3"
  local command="$4"
  local pane

  pane="$(tmux new-session -d -P -F '#{pane_id}' -s "$session" -x "$cols" -y "$rows" "$command")"
  pane="$(printf '%s\n' "$pane" | tr -d '\r' | awk 'NF {line=$0} END {print line}')"
  if [[ -z "$pane" ]]; then
    echo "failed to create tmux session: $session" >&2
    return 1
  fi
  tmux set-option -t "$session" remain-on-exit on >/dev/null 2>&1 || true
  register_tmux_session "$session"
  printf '%s\n' "$pane"
}

start_tmux_client() {
  local session="$1"
  local target="$2"
  local size
  local cols
  local rows

  size="$(
    tmux list-clients -F '#{client_width} #{client_height}' 2>/dev/null \
      | awk '
          BEGIN { bestw=0; besth=0 }
          NF >= 2 {
            w=$1+0
            h=$2+0
            if (w > bestw || (w == bestw && h > besth)) {
              bestw=w
              besth=h
            }
          }
          END {
            if (bestw > 0 && besth > 0) {
              printf "%d %d\n", bestw, besth
            }
          }
        '
  )"
  if [[ -z "$size" ]]; then
    size="$(tmux display-message -p -t "$target" '#{pane_width} #{pane_height}' 2>/dev/null || true)"
  fi
  cols="${size%% *}"
  rows="${size##* }"
  if [[ -z "$cols" || -z "$rows" || "$cols" == "$size" || "$rows" == "$size" ]]; then
    cols="$ATTACH_COLS"
    rows="$ATTACH_ROWS"
  fi

  python3 - "$cols" "$rows" "$session" <<'PY' >"$ROOT/$session.tmux-client.out" 2>&1 &
import fcntl
import os
import pty
import select
import struct
import subprocess
import signal
import sys
import termios

cols = int(sys.argv[1])
rows = int(sys.argv[2])
session = sys.argv[3]

child = None
stopping = False

def handle_signal(signum, frame):
    global stopping
    stopping = True
    if child is not None and child.poll() is None:
        child.terminate()

signal.signal(signal.SIGTERM, handle_signal)
signal.signal(signal.SIGINT, handle_signal)

master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
child = subprocess.Popen(
    ["tmux", "attach-session", "-r", "-t", session],
    stdin=slave,
    stdout=slave,
    stderr=slave,
    close_fds=True,
)
os.close(slave)

while True:
    ready, _, _ = select.select([master], [], [], 0.05)
    if master in ready:
        try:
            data = os.read(master, 4096)
        except OSError:
            data = b""
        if not data:
            if child.poll() is not None:
                break
        else:
            os.write(1, data)
    if child.poll() is not None:
        break
    if stopping and child.poll() is None:
        child.terminate()

if child.poll() is None:
    child.terminate()
    try:
        child.wait(timeout=0.5)
    except subprocess.TimeoutExpired:
        child.kill()
        child.wait(timeout=0.5)

os.close(master)
PY
  printf '%s\n' "$!"
}

stop_tmux_client() {
  local pid="${1:-}"
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
}

prime_tmux_client() {
  local session="$1"
  local target="$2"
  local pid
  pid="$(start_tmux_client "$session" "$target")"
  sleep 0.25
  stop_tmux_client "$pid"
}

surface_has_attach_placeholder() {
  grep -Eq 'No terminal attached|Attach existing terminal'
}

should_retry_attach_surface() {
  local reason="${1:-}"
  case "$reason" in
    attach-placeholder-stuck|attach-placeholder-timeout)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

record_attach_surface_artifact() {
  local base="$1"
  local session="$2"
  local target="$3"
  local reason="$4"
  local retry_count="$5"
  local retry_limit="$6"
  local pane_alt=""
  local pane_normal=""
  local alternate_on=""

  ensure_tmux_target "$session" "$target" "$base.target-missing" || true
  pane_alt="$(capture_pane_alt "$session" "$target" "$base" || true)"
  pane_normal="$(capture_pane_normal "$session" "$target" "$base" || true)"
  alternate_on="$(tmux display-message -p -t "$target" '#{alternate_on}' 2>/dev/null || true)"
  printf '%s\n' "$pane_normal" >"$ROOT/$base.normal.txt"
  printf '%s\n' "$pane_alt" >"$ROOT/$base.alt.txt"
  printf '%s\n' "$alternate_on" >"$ROOT/$base.alternate_on.txt"
  {
    printf 'reason=%s\n' "$reason"
    printf 'attach_retry_count=%s\n' "$retry_count"
    printf 'attach_retry_limit=%s\n' "$retry_limit"
    printf 'wait_attempt=%s\n' "${WAIT_SURFACE_LAST_ATTEMPT:-0}"
    printf 'placeholder_seen=%s\n' "${WAIT_SURFACE_LAST_PLACEHOLDER_SEEN:-0}"
    printf 'session=%s\n' "$session"
    printf 'target=%s\n' "$target"
  } >"$ROOT/$base.summary.txt"
  write_tmux_diagnostics "$base" "$session" "$target"
  write_termx_log_excerpt "$base"
  write_termx_inventory_artifact "$base"
}

cleanup() {
  set +e
  local owned_session
  stop_tmux_client "$CLIENT_MAIN_PID"
  stop_tmux_client "$CLIENT_REATTACH_PID"
  while owned_session="$(first_owned_tmux_session)"; do
    cleanup_tmux_session "$owned_session"
  done
  if [[ -n "${DAEMON_PID:-}" ]] && kill -0 "$DAEMON_PID" 2>/dev/null; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  if [[ -n "${GRID_DUMP_TOOL_DIR:-}" ]]; then
    rm -rf "$GRID_DUMP_TOOL_DIR"
  fi
  if [[ "$KEEP_ROOT" == "0" ]]; then
    rm -rf "$ROOT"
  else
    log "artifacts kept at $ROOT"
  fi
}

copy_gridtrace_artifact() {
  local session="$1"
  local dest_base="$2"
  local trace_path="$ROOT/$session.gridtrace.log"
  if [[ ! -f "$trace_path" ]]; then
    return 0
  fi
  cp "$trace_path" "$ROOT/$dest_base.gridtrace.log"
  if ! rg -n 'runtime\.load_grid_viewport|app\.copy_mode\.sync_viewport' "$trace_path" >"$ROOT/$dest_base.gridtrace.summary.txt"; then
    : > "$ROOT/$dest_base.gridtrace.summary.txt"
  fi
}

copy_resize_log_artifact() {
  local dest_base="$1"
  if [[ ! -f "$LOG" ]]; then
    : >"$ROOT/$dest_base.resize.summary.txt"
    return 0
  fi
  if ! rg -n 'server attached terminal|method=ensure_resize|method=resize|termx transport resize frame|resize_owner' "$LOG" >"$ROOT/$dest_base.resize.summary.txt"; then
    : >"$ROOT/$dest_base.resize.summary.txt"
  fi
}

deep_live_tail_expected_first_row() {
  local retained_committed_rows="$LINES"
  local screen_committed_rows=$((ATTACH_ROWS - DEEP_LIVE_TAIL_SCREEN_LIVE_ROWS))
  if (( screen_committed_rows < 0 )); then
    screen_committed_rows=0
  fi
  retained_committed_rows=$((retained_committed_rows - screen_committed_rows))
  if (( retained_committed_rows < 0 )); then
    retained_committed_rows=0
  fi
  printf '%d\n' $((retained_committed_rows - (2 * DEEP_LIVE_TAIL_PAGE_LIMIT)))
}

wait_surface() {
  local session="$1"
  local target="$2"
  local pane
  local pane_alt
  local alternate_on
  local attempt
  local placeholder_seen="0"
  local placeholder_streak="0"
  WAIT_SURFACE_LAST_REASON=""
  WAIT_SURFACE_LAST_ATTEMPT="0"
  WAIT_SURFACE_LAST_PLACEHOLDER_SEEN="0"
  prime_tmux_client "$session" "$target"
  for attempt in $(seq 1 "$WAIT_ATTEMPTS"); do
    WAIT_SURFACE_LAST_ATTEMPT="$attempt"
    if ! ensure_tmux_target "$session" "$target" "$session.wait-target-missing"; then
      WAIT_SURFACE_LAST_REASON="target-missing"
      return 1
    fi
    if pane_alt="$(capture_pane_alt "$session" "$target" "$session.wait-surface")"; then
      :
    else
      pane_alt=""
    fi
    pane="$pane_alt"
    if [[ -z "$pane" ]]; then
      if pane="$(capture_pane_normal "$session" "$target" "$session.wait-surface")"; then
        :
      else
        pane=""
      fi
    fi
    if printf '%s\n' "$pane" | grep -Fq 'protocol error 404'; then
      WAIT_SURFACE_LAST_REASON="terminal-not-found"
      echo "attach failed in tmux session $session: terminal not found" >&2
      printf '%s\n' "$pane" >&2
      return 1
    fi
    if printf '%s\n' "$pane" | grep -Fq '[tmux-history-smoke] attach exited status='; then
      WAIT_SURFACE_LAST_REASON="attach-exited"
      printf '%s\n' "$pane" >"$ROOT/$session.attach-exited.txt"
      write_tmux_diagnostics "$session.attach-exited" "$session" "$target"
      echo "attach command exited before surface became ready in tmux session: $session" >&2
      return 1
    fi
    if printf '%s\n' "$pane" | surface_has_attach_placeholder; then
      placeholder_seen="1"
      WAIT_SURFACE_LAST_PLACEHOLDER_SEEN="1"
      placeholder_streak=$((placeholder_streak + 1))
      alternate_on="$(tmux display-message -p -t "$target" '#{alternate_on}' 2>/dev/null || true)"
      if [[ "$alternate_on" == "1" && -z "$pane_alt" ]]; then
        prime_tmux_client "$session" "$target"
      fi
      if (( placeholder_streak >= ATTACH_PLACEHOLDER_RETRY_ATTEMPTS )); then
        WAIT_SURFACE_LAST_REASON="attach-placeholder-stuck"
        return 1
      fi
      sleep "$WAIT_DELAY"
      continue
    fi
    placeholder_streak="0"
    if printf '%s\n' "$pane" | grep -Eq '([0-9]{6}|stress|PERSISTED-[0-9]{5}|LIVE-SCREEN-open-tail|HSEM_READY)'; then
      return 0
    fi
    sleep "$WAIT_DELAY"
  done
  WAIT_SURFACE_LAST_PLACEHOLDER_SEEN="$placeholder_seen"
  if [[ "$placeholder_seen" == "1" ]]; then
    WAIT_SURFACE_LAST_REASON="attach-placeholder-timeout"
  else
    WAIT_SURFACE_LAST_REASON="wait-timeout"
  fi
  echo "timed out waiting for attached surface in tmux session: $session" >&2
  return 1
}

start_attach_tmux_session() {
  local session="$1"
  local terminal_id="$2"
  local phase="$3"
  local pane=""
  local client_pid=""
  local attempt
  ATTACH_SESSION_PANE=""
  ATTACH_SESSION_CLIENT_PID=""
  ATTACH_SESSION_RETRY_COUNT="0"

  for attempt in $(seq 0 "$ATTACH_SURFACE_RETRY_LIMIT"); do
    pane="$(start_tmux_session "$session" "$SESSION_COLS" "$SESSION_ROWS" "$(attach_tmux_command "$terminal_id" "$session")")"
    client_pid="$(start_tmux_client "$session" "$pane")"
    if wait_surface "$session" "$pane"; then
      ATTACH_SESSION_PANE="$pane"
      ATTACH_SESSION_CLIENT_PID="$client_pid"
      ATTACH_SESSION_RETRY_COUNT="$attempt"
      return 0
    fi

    if should_retry_attach_surface "$WAIT_SURFACE_LAST_REASON" && (( attempt < ATTACH_SURFACE_RETRY_LIMIT )); then
      record_attach_surface_artifact "attach-${phase}.retry-$((attempt + 1))" "$session" "$pane" "$WAIT_SURFACE_LAST_REASON" "$((attempt + 1))" "$ATTACH_SURFACE_RETRY_LIMIT"
      stop_tmux_client "$client_pid"
      cleanup_tmux_session "$session"
      sleep "$WAIT_DELAY"
      continue
    fi

    record_attach_surface_artifact "attach-${phase}.failure" "$session" "$pane" "$WAIT_SURFACE_LAST_REASON" "$attempt" "$ATTACH_SURFACE_RETRY_LIMIT"
    stop_tmux_client "$client_pid"
    return 1
  done

  return 1
}

page_to_top() {
  local session="$1"
  local target="$2"
  local repeats="${3:-$G_REPEATS}"
  local repeat
  send_tmux_keys "$session" "$target" "$session.page-to-top.copy-mode-enter" C-v
  sleep "$G_DELAY"
  for repeat in $(seq 1 "$repeats"); do
    send_tmux_keys "$session" "$target" "$session.page-to-top.g-$repeat" g
    sleep "$G_DELAY"
  done
}

capture_session() {
  local session="$1"
  local target="$2"
  local base="$3"
  local raw_alt
  local clean_alt
  local normal
  local normal_raw
  local alternate_on
  ensure_tmux_target "$session" "$target" "$base.capture-target-missing" || return 1
  raw_alt="$(capture_pane_alt_raw "$session" "$target" "$base" || true)"
  clean_alt="$(capture_pane_alt "$session" "$target" "$base" || true)"
  normal="$(capture_pane_normal "$session" "$target" "$base" || true)"
  alternate_on="$(tmux display-message -p -t "$target" '#{alternate_on}' 2>/dev/null || true)"
  if [[ -z "$raw_alt" && -z "$clean_alt" && "$alternate_on" == "1" ]] && printf '%s\n' "$normal" | grep -Fq 'No terminal attached'; then
    prime_tmux_client "$session" "$target"
    raw_alt="$(capture_pane_alt_raw "$session" "$target" "$base" || true)"
    clean_alt="$(capture_pane_alt "$session" "$target" "$base" || true)"
    normal="$(capture_pane_normal "$session" "$target" "$base" || true)"
  fi
  if [[ -n "$raw_alt" || -n "$clean_alt" ]]; then
    normal_raw="$(capture_pane_normal_raw "$session" "$target" "$base" || true)"
    printf '%s\n' "$raw_alt" > "$ROOT/$base.raw.txt"
    printf '%s\n' "$clean_alt" > "$ROOT/$base.txt"
    printf '%s\n' "$raw_alt" > "$ROOT/$base.alt.raw.txt"
    printf '%s\n' "$clean_alt" > "$ROOT/$base.alt.txt"
    printf '%s\n' "$normal_raw" > "$ROOT/$base.normal.raw.txt"
    printf '%s\n' "$normal" > "$ROOT/$base.normal.txt"
    return 0
  fi

  normal_raw="$(capture_pane_normal_raw "$session" "$target" "$base" || true)"
  printf '%s\n' "$normal_raw" > "$ROOT/$base.raw.txt"
  printf '%s\n' "$normal" > "$ROOT/$base.txt"
  if [[ -n "$normal" || -n "$normal_raw" ]]; then
    return 0
  fi
  return 1
}

capture_pane_alt() {
  local session="$1"
  local target="$2"
  local base="${3:-capture}"
  run_tmux_capture "$session" "$target" "$base" "alt" tmux capture-pane -a -t "$target" -p
}

capture_pane_alt_raw() {
  local session="$1"
  local target="$2"
  local base="${3:-capture}"
  run_tmux_capture "$session" "$target" "$base" "alt-raw" tmux capture-pane -a -t "$target" -ep
}

capture_pane_normal() {
  local session="$1"
  local target="$2"
  local base="${3:-capture}"
  run_tmux_capture "$session" "$target" "$base" "normal" tmux capture-pane -t "$target" -p
}

capture_pane_normal_raw() {
  local session="$1"
  local target="$2"
  local base="${3:-capture}"
  run_tmux_capture "$session" "$target" "$base" "normal-raw" tmux capture-pane -t "$target" -ep
}

capture_pane_alt_raw_preserve() {
  local session="$1"
  local target="$2"
  local base="${3:-capture}"
  run_tmux_capture "$session" "$target" "$base" "alt-raw-preserve" tmux capture-pane -a -t "$target" -epN
}

capture_preserved_raw_artifact() {
  local session="$1"
  local target="$2"
  local base="$3"
  local raw
  if [[ -s "$ROOT/$base.raw.txt" ]]; then
    cp "$ROOT/$base.raw.txt" "$ROOT/$base.raw-preserve.txt"
    return
  fi
  # tmux 3.6 的 `capture-pane -epN` 在 alternate screen 下可能只返回空行；
  # `-ep` 已经保留 SGR 和行内空格，足够验证背景是否延伸到 marker 后的空白区。
  raw="$(capture_pane_alt_raw "$session" "$target" "$base.raw-preserve" || true)"
  if [[ -n "$raw" ]]; then
    printf '%s\n' "$raw" >"$ROOT/$base.raw-preserve.txt"
    return
  fi
  capture_pane_alt_raw_preserve "$session" "$target" "$base" >"$ROOT/$base.raw-preserve.txt"
}

extract_first_match() {
  local file="$1"
  local regex="$2"
  grep -m1 -oE "$regex" "$file" || true
}

assert_contains() {
  local file="$1"
  local needle="$2"
  if ! grep -Fq -- "$needle" "$file"; then
    echo "expected '$needle' in $file" >&2
    return 1
  fi
}

assert_regex_count_at_least() {
  local file="$1"
  local regex="$2"
  local expected="$3"
  local got
  got="$(grep -E -c -- "$regex" "$file" 2>/dev/null || true)"
  if (( got < expected )); then
    echo "expected at least $expected matches for /$regex/ in $file, got $got" >&2
    return 1
  fi
}

terminal_size_from_inventory() {
  local file="$1"
  local terminal_id="$2"
  awk -F '\t' -v want="$terminal_id" '
    $1 == want {
      split($5, size, "x")
      if (size[1] != "" && size[2] != "") {
        print size[1], size[2]
        found=1
      }
      exit
    }
    END { exit(found ? 0 : 1) }
  ' "$file"
}

assert_terminal_size_shrunk() {
  local before_file="$1"
  local after_file="$2"
  local terminal_id="$3"
  local before_size
  local after_size
  local before_cols
  local before_rows
  local after_cols
  local after_rows

  before_size="$(terminal_size_from_inventory "$before_file" "$terminal_id")" || {
    echo "expected terminal $terminal_id size in $before_file" >&2
    return 1
  }
  after_size="$(terminal_size_from_inventory "$after_file" "$terminal_id")" || {
    echo "expected terminal $terminal_id size in $after_file" >&2
    return 1
  }
  before_cols="${before_size%% *}"
  before_rows="${before_size##* }"
  after_cols="${after_size%% *}"
  after_rows="${after_size##* }"
  if (( after_cols >= before_cols || after_rows >= before_rows )); then
    echo "expected terminal $terminal_id to shrink after floating owner resize: before=${before_cols}x${before_rows} after=${after_cols}x${after_rows}" >&2
    return 1
  fi
}

assert_first_match_equals() {
  local file="$1"
  local regex="$2"
  local expected="$3"
  local got
  got="$(extract_first_match "$file" "$regex")"
  if [[ "$got" != "$expected" ]]; then
    echo "expected first match '$expected' in $file, got '${got:-<none>}'" >&2
    return 1
  fi
}

assert_stress_history_label_order() {
  local file="$1"
  python3 - "$file" <<'PY'
import re
import sys

path = sys.argv[1]
pattern = re.compile(r"(?<![0-9])([0-9]{6})\s+\[[A-Z ]{6}\]")
labels = []
with open(path, "r", encoding="utf-8", errors="replace") as fh:
    for line in fh:
        match = pattern.search(line)
        if match:
            labels.append(int(match.group(1)))

if len(labels) < 5:
    raise SystemExit(f"expected at least 5 stress labels in {path}, got {labels!r}")
try:
    start = labels.index(0)
except ValueError:
    raise SystemExit(f"expected visible top history label 000000 in {path}, got labels={labels!r}")
focused = labels[start:]
if len(focused) < 5:
    raise SystemExit(f"expected at least 5 stress labels after 000000 in {path}, got {focused!r}; all={labels!r}")
for prev, cur in zip(focused, focused[1:]):
    if cur < prev:
        raise SystemExit(f"stress labels went backwards in {path}: {prev:06d} -> {cur:06d}; labels={focused!r}; all={labels!r}")
PY
}

assert_stress_history_label_order_visible() {
  local file="$1"
  python3 - "$file" <<'PY'
import re
import sys

path = sys.argv[1]
pattern = re.compile(r"(?<![0-9])([0-9]{6})\s+\[[A-Z ]{6}\]")
labels = []
lines = open(path, "r", encoding="utf-8", errors="replace").read().splitlines()
floating_left = None
for line in lines:
    markers = [match.start() for match in re.finditer(re.escape("┌─ ["), line)]
    if markers:
        marker = max(markers)
        if marker == 0:
            continue
        floating_left = marker
        break

for line in lines:
    # Floating panes are captured over the tiled pane behind them. Once the
    # floating frame is present, compare only labels inside that frame.
    if floating_left is not None:
        if len(line) <= floating_left or line[floating_left] not in "│┌└":
            continue
        scan = line[floating_left + 1:]
    else:
        scan = line
    match = pattern.search(scan)
    if match:
        labels.append(int(match.group(1)))

if len(labels) < 5:
    raise SystemExit(f"expected at least 5 stress labels in {path}, got {labels!r}")
for prev, cur in zip(labels, labels[1:]):
    if cur < prev:
        raise SystemExit(f"stress labels went backwards in {path}: {prev:06d} -> {cur:06d}; labels={labels!r}")
PY
}

write_grid_viewport_dump_tool() {
  GRID_DUMP_TOOL_DIR="$REPO_ROOT/termx-cli/termx-smoke-grid-dump-$$"
  mkdir -p "$GRID_DUMP_TOOL_DIR"
  cat >"$GRID_DUMP_TOOL_DIR/main.go" <<'GO'
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	unixtransport "github.com/lozzow/termx/termx-shared/transport/unix"
)

func main() {
	socket := flag.String("socket", "", "termx daemon socket")
	terminalID := flag.String("terminal", "", "terminal id")
	offset := flag.Int("offset", 0, "scrollback offset")
	limit := flag.Int("limit", 1000, "scrollback limit")
	cols := flag.Int("cols", 80, "reflow columns")
	flag.Parse()
	if *socket == "" || *terminalID == "" {
		fmt.Fprintln(os.Stderr, "socket and terminal are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := unixtransport.Dial(*socket)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client := protocol.NewClient(conn)
	defer client.Close()
	if err := client.Hello(ctx, protocol.Hello{Version: wire.Version, Client: "tmux-history-smoke"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	viewport, err := client.GridViewport(ctx, *terminalID, *offset, *limit, *cols)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("# terminal=%s cols=%d rows=%d offset=%d limit=%d total=%d logical_total=%d has_more=%v loaded_rows=%d generation=%d first_row_id=%d last_row_id=%d\n",
		viewport.TerminalID,
		viewport.Size.Cols,
		viewport.Size.Rows,
		viewport.ScrollbackOffset,
		viewport.ScrollbackLimit,
		viewport.ScrollbackTotal,
		viewport.ScrollbackLogicalTotal,
		viewport.ScrollbackHasMore,
		viewport.LoadedRows,
		viewport.HistoryGeneration,
		viewport.FirstRowID,
		viewport.LastRowID,
	)
	for i, row := range viewport.Rows {
		var b strings.Builder
		for _, cell := range row.DecodeCells() {
			if cell.Content == "" {
				b.WriteString(" ")
			} else {
				b.WriteString(cell.Content)
			}
		}
		ownership := ""
		if i < len(viewport.RowOwnership) {
			ownership = viewport.RowOwnership[i]
		}
		fmt.Printf("%06d\t%s\t%s\n", i, ownership, strings.TrimRight(b.String(), " "))
	}
	snapshot, err := client.Snapshot(ctx, *terminalID, 0, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("# screen terminal=%s cols=%d rows=%d\n", snapshot.TerminalID, snapshot.Size.Cols, snapshot.Size.Rows)
	for i, row := range snapshot.Screen.Cells {
		var b strings.Builder
		for _, cell := range row {
			if cell.Content == "" {
				b.WriteString(" ")
			} else {
				b.WriteString(cell.Content)
			}
		}
		fmt.Printf("screen:%06d\t%s\n", i, strings.TrimRight(b.String(), " "))
	}
}
GO
}

dump_grid_viewport_artifact() {
  local dest_base="$1"
  local limit="$2"
  local cols="$3"
  write_grid_viewport_dump_tool
  (cd "$REPO_ROOT/termx-cli" && go run "./$(basename "$GRID_DUMP_TOOL_DIR")" \
    --socket "$SOCK" \
    --terminal "$TERM_ID" \
    --offset 0 \
    --limit "$limit" \
    --cols "$cols") >"$ROOT/$dest_base.txt"
}

assert_remote_pair_history_continuity() {
  local dump_file="$1"
  local report_file="$2"
  python3 - "$dump_file" "$report_file" <<'PY'
import re
import sys

dump_path, report_path = sys.argv[1], sys.argv[2]
label_re = re.compile(r"(?<![0-9])([0-9]{6})\s+\[[A-Z ]{6}\]")

rows = []
for raw in open(dump_path, "r", encoding="utf-8", errors="replace"):
    if raw.startswith("#"):
        continue
    try:
        parts = raw.rstrip("\n").split("\t", 2)
    except ValueError:
        continue
    if len(parts) == 2:
        row_no_text, text = parts
    elif len(parts) == 3:
        row_no_text, _ownership, text = parts
    else:
        continue
    if row_no_text.startswith("screen:"):
        row_no = 1_000_000 + int(row_no_text.removeprefix("screen:"))
    else:
        row_no = int(row_no_text)
    rows.append((row_no, text))

events = []
for row_no, text in rows:
    match = label_re.search(text)
    if match:
        events.append((row_no, "label", int(match.group(1)), text))
    if "uri:" in text or "termx://pair" in text:
        events.append((row_no, "uri", None, text))
    if "expires_at:" in text:
        events.append((row_no, "expires", None, text))

labels = [(row_no, value) for row_no, kind, value, _ in events if kind == "label"]
uri_rows = [row_no for row_no, kind, _, _ in events if kind == "uri"]
expires_rows = [row_no for row_no, kind, _, _ in events if kind == "expires"]

errors = []
if len(labels) < 150:
    errors.append(f"expected at least 150 visible stress labels in dump, got {len(labels)}")
if not uri_rows:
    errors.append("expected remote pair uri in dump")
if not expires_rows:
    errors.append("expected remote pair expires_at in dump")

segments = []
current = []
for item in labels:
    if not current:
        current = [item]
        continue
    _, prev = current[-1]
    _, cur = item
    if cur == prev + 1:
        current.append(item)
    else:
        segments.append(current)
        current = [item]
if current:
    segments.append(current)

valid_segments = []
for segment in segments:
    values = [value for _, value in segment]
    if len(values) >= 20:
        valid_segments.append((segment[0][0], segment[-1][0], values[0], values[-1], len(values)))

first_tail = [
    (row_no, value)
    for row_no, value in labels
    if 96 <= value <= 100 and uri_rows and row_no < min(uri_rows)
]
second_run = [
    (row_no, value)
    for row_no, value in labels
    if uri_rows and row_no > min(uri_rows)
]
second_values = [value for _, value in second_run]

if [value for _, value in first_tail[-5:]] != [96, 97, 98, 99, 100]:
    errors.append(f"expected first stress tail 000096..000100 before uri, got {[value for _, value in first_tail[-8:]]}")
if 0 not in second_values:
    errors.append("expected second stress run 000000 after remote pair")
else:
    start = second_values.index(0)
    got_prefix = second_values[start:]
    if got_prefix != sorted(got_prefix):
        errors.append(f"second stress run labels must stay monotonic after remote pair, got prefix={got_prefix[:130]}")
    duplicates = sorted({value for value in got_prefix if got_prefix.count(value) > 1})
    if duplicates:
        errors.append(f"second stress run labels must not duplicate after remote pair, duplicates={duplicates[:20]} prefix={got_prefix[:130]}")
    if got_prefix[:10] != list(range(0, 10)):
        errors.append(f"second stress run must start at 000000..000009 after remote pair, got prefix={got_prefix[:20]}")
    gaps = []
    for prev, cur in zip(got_prefix, got_prefix[1:]):
        if cur > prev + 1:
            gaps.append((prev, cur))
            if len(gaps) >= 8:
                break
    if gaps:
        errors.append(f"second stress run must not skip visible labels after remote pair, gaps={gaps}")
    if not all(value in got_prefix for value in range(96, 101)):
        errors.append(f"second stress run tail 000096..000100 must remain visible after resize, got tail={[value for value in got_prefix if value >= 90]}")
if uri_rows and expires_rows and min(expires_rows) < min(uri_rows):
    errors.append(f"expected expires_at after uri, got uri_rows={uri_rows[:3]} expires_rows={expires_rows[:3]}")

with open(report_path, "w", encoding="utf-8") as report:
    report.write(f"rows={len(rows)}\n")
    report.write(f"labels={len(labels)}\n")
    report.write(f"uri_rows={uri_rows[:5]}\n")
    report.write(f"expires_rows={expires_rows[:5]}\n")
    report.write(f"segments={valid_segments[:8]}\n")
    report.write(f"first_tail={[value for _, value in first_tail[-8:]]}\n")
    report.write(f"second_prefix={second_values[:120]}\n")
    if errors:
        report.write("errors=\n")
        for error in errors:
            report.write(f"- {error}\n")

if errors:
    raise SystemExit("; ".join(errors))
PY
}

assert_not_contains() {
  local file="$1"
  local needle="$2"
  if grep -Fq -- "$needle" "$file"; then
    echo "expected $file not to contain '$needle'" >&2
    return 1
  fi
}

assert_bg_spaces_after_marker() {
  local file="$1"
  local marker="$2"
  local expected_bg="$3"
  local min_spaces="${4:-4}"
  python3 - "$file" "$marker" "$expected_bg" "$min_spaces" <<'PY'
import sys

path, marker_text, expected_bg, min_spaces_text = sys.argv[1:5]
marker = marker_text.encode()
min_spaces = int(min_spaces_text)


def parse_sgr_params(raw: bytes) -> list[int]:
    if not raw:
        return [0]
    values = []
    for part in raw.replace(b":", b";").split(b";"):
        if not part:
            values.append(0)
            continue
        try:
            values.append(int(part))
        except ValueError:
            continue
    return values or [0]


def apply_sgr(params, bg):
    i = 0
    while i < len(params):
        value = params[i]
        if value == 0 or value == 49:
            bg = None
        elif 40 <= value <= 47:
            bg = f"ansi:{value - 40}"
        elif 100 <= value <= 107:
            bg = f"ansi:{value - 100 + 8}"
        elif value == 48 and i + 2 < len(params) and params[i + 1] == 5:
            bg = f"idx:{params[i + 2]}"
            i += 2
        elif value == 48 and i + 4 < len(params) and params[i + 1] == 2:
            bg = f"#{params[i + 2]:02x}{params[i + 3]:02x}{params[i + 4]:02x}"
            i += 4
        i += 1
    return bg


def cells_from_raw_line(line):
    cells = []
    bg = None
    i = 0
    while i < len(line):
        if line[i] == 0x1B and i + 1 < len(line) and line[i + 1] == ord("["):
            end = i + 2
            while end < len(line) and not (0x40 <= line[end] <= 0x7E):
                end += 1
            if end >= len(line):
                break
            if line[end] == ord("m"):
                bg = apply_sgr(parse_sgr_params(line[i + 2:end]), bg)
            i = end + 1
            continue
        cells.append((line[i], bg))
        i += 1
    return cells


for raw_line in open(path, "rb").read().splitlines():
    cells = cells_from_raw_line(raw_line)
    plain = bytes(byte for byte, _ in cells)
    start = plain.find(marker)
    if start < 0:
        continue
    after = cells[start + len(marker):]
    styled_spaces = 0
    for byte, bg in after:
        if byte != 0x20:
            break
        if bg != expected_bg:
            break
        styled_spaces += 1
    if styled_spaces >= min_spaces:
        raise SystemExit(0)
    raise SystemExit(
        f"{path}: marker {marker_text!r} has only {styled_spaces} styled spaces with {expected_bg}; "
        f"plain={plain!r} raw={raw_line!r}"
    )

raise SystemExit(f"{path}: marker {marker_text!r} not found")
PY
}

assert_floating_contains() {
  local file="$1"
  local needle="$2"
  python3 - "$file" "$needle" <<'PY'
import re
import sys

path = sys.argv[1]
needle = sys.argv[2]
lines = open(path, "r", encoding="utf-8", errors="replace").read().splitlines()
floating_left = None
for line in lines:
    markers = [match.start() for match in re.finditer(re.escape("┌─ ["), line)]
    if markers:
        marker = max(markers)
        if marker == 0:
            continue
        floating_left = marker
        break

scanned = []
for line in lines:
    if floating_left is not None:
        if len(line) <= floating_left or line[floating_left] not in "│┌└":
            continue
        scanned.append(line[floating_left + 1:])
    else:
        scanned.append(line)

if needle not in "\n".join(scanned):
    raise SystemExit(f"expected floating pane in {path} to contain {needle!r}")
PY
}

assert_gridtrace_single_requested_cols() {
  local file="$1"
  python3 - "$file" <<'PY'
import re
import sys

path = sys.argv[1]
cols = []
with open(path, "r", encoding="utf-8", errors="replace") as fh:
    for line in fh:
        if "runtime.load_grid_viewport" not in line:
            continue
        match = re.search(r'requested_cols="([0-9]+)"', line)
        if match:
            cols.append(int(match.group(1)))

if not cols:
    raise SystemExit(f"expected runtime.load_grid_viewport requested_cols in {path}")
unique = sorted(set(cols))
if len(unique) != 1:
    raise SystemExit(f"expected one requested_cols value in {path}, got {unique!r}; all={cols!r}")
PY
}

wait_log_regex_count_at_least() {
  local regex="$1"
  local expected="$2"
  local attempt
  local got
  for attempt in $(seq 1 "$WAIT_ATTEMPTS"); do
    got="$(grep -E -c -- "$regex" "$LOG" 2>/dev/null || true)"
    if (( got >= expected )); then
      return 0
    fi
    sleep "$WAIT_DELAY"
  done
  echo "timed out waiting for at least $expected matches for /$regex/ in $LOG, got ${got:-0}" >&2
  return 1
}

capture_until_contains() {
  local session="$1"
  local target="$2"
  local base="$3"
  local needle="$4"
  local attempt
  for attempt in $(seq 1 "$WAIT_ATTEMPTS"); do
    capture_session "$session" "$target" "$base" || return 1
    if grep -Fq -- "$needle" "$ROOT/$base.txt"; then
      return 0
    fi
    sleep "$WAIT_DELAY"
  done
  echo "timed out waiting for '$needle' in $ROOT/$base.txt" >&2
  return 1
}

enter_copy_mode_and_repeat_top_until_contains() {
  local session="$1"
  local target="$2"
  local base="$3"
  local needle="$4"
  local repeats="$5"
  local attempt

  send_tmux_keys "$session" "$target" "$base.copy-mode-enter" C-v
  sleep "$G_DELAY"
  for attempt in $(seq 1 "$repeats"); do
    send_tmux_keys "$session" "$target" "$base.copy-mode-g-$attempt" g
    sleep "$G_DELAY"
    capture_session "$session" "$target" "$base" || return 1
    if grep -Fq -- "$needle" "$ROOT/$base.txt"; then
      return 0
    fi
  done
  echo "timed out waiting for '$needle' in $ROOT/$base.txt after $repeats top attempts" >&2
  return 1
}

enter_copy_mode_and_scan_down_until_contains() {
  local session="$1"
  local target="$2"
  local base="$3"
  local needle="$4"
  local repeats="$5"
  local attempt

  send_tmux_keys "$session" "$target" "$base.copy-mode-enter" C-v
  sleep "$G_DELAY"
  send_tmux_keys "$session" "$target" "$base.copy-mode-top" g
  sleep "$G_DELAY"
  capture_session "$session" "$target" "$base" || return 1
  if grep -Fq -- "$needle" "$ROOT/$base.txt"; then
    return 0
  fi
  for attempt in $(seq 1 "$repeats"); do
    send_tmux_keys "$session" "$target" "$base.copy-mode-down-$attempt" d
    sleep "$G_DELAY"
    capture_session "$session" "$target" "$base" || return 1
    if grep -Fq -- "$needle" "$ROOT/$base.txt"; then
      return 0
    fi
  done
  echo "timed out waiting for '$needle' in $ROOT/$base.txt after $repeats down attempts" >&2
  return 1
}

scan_down_until_contains() {
  local session="$1"
  local target="$2"
  local base="$3"
  local needle="$4"
  local repeats="$5"
  local attempt

  for attempt in $(seq 1 "$repeats"); do
    send_tmux_keys "$session" "$target" "$base.copy-mode-down-$attempt" j
    sleep "$G_DELAY"
    capture_session "$session" "$target" "$base" || return 1
    if grep -Fq -- "$needle" "$ROOT/$base.txt"; then
      return 0
    fi
  done
  echo "timed out waiting for '$needle' in $ROOT/$base.txt after $repeats line-down attempts" >&2
  return 1
}

mouse_wheel_until_contains() {
  local session="$1"
  local target="$2"
  local base="$3"
  local needle="$4"
  local repeats="$5"
  local direction="$6"
  local x=$(( ATTACH_COLS / 2 ))
  local y=$(( ATTACH_ROWS / 2 ))
  local attempt

  for attempt in $(seq 1 "$repeats"); do
    if [[ "$direction" == "down" ]]; then
      send_tmux_mouse_wheel_down "$session" "$target" "$base.wheel-down-$attempt" "$x" "$y"
    else
      send_tmux_mouse_wheel_up "$session" "$target" "$base.wheel-up-$attempt" "$x" "$y"
    fi
    sleep "$G_DELAY"
    capture_session "$session" "$target" "$base" || return 1
    if grep -Fq -- "$needle" "$ROOT/$base.txt"; then
      return 0
    fi
  done
  echo "timed out waiting for '$needle' in $ROOT/$base.txt after $repeats mouse wheel $direction attempts" >&2
  return 1
}

start_daemon() {
  mkdir -p "$CFG" "$STATE"
  if [[ "$SCENARIO" == "floating-owner-remote-pair-wheel-history" ]]; then
    XDG_CONFIG_HOME="$CFG" \
    XDG_STATE_HOME="$STATE" \
    TERMX_REMOTE_ENABLE=true \
    TERMX_REMOTE_MODE=local \
    TERMX_REMOTE_LOCAL_WEB_ADDR=127.0.0.1:0 \
    TERMX_REMOTE_LOCAL_ICE_TCP_ADDR=127.0.0.1:0 \
    "$BIN" --socket "$SOCK" --log-file "$LOG" daemon >"$DAEMON_STDOUT" 2>&1 &
  else
    XDG_CONFIG_HOME="$CFG" \
    XDG_STATE_HOME="$STATE" \
    TERMX_REMOTE_ENABLE=false \
    "$BIN" --socket "$SOCK" --log-file "$LOG" daemon >"$DAEMON_STDOUT" 2>&1 &
  fi
  DAEMON_PID=$!
  wait_for_daemon
}

build_bin_if_needed() {
  if [[ "$BUILD_BIN" == "1" ]]; then
    log "building termx binary at $BIN"
    go build -o "$BIN" ./termx-cli/cmd/termx
  fi
}

build_generator_command() {
  case "$SCENARIO" in
    baseline|standard|floating-owner-resize|floating-owner-reattach-history|floating-owner-wheel-history)
      printf 'python3 %q --lines %q --seed %q --width-hint %q; exec cat' \
        "$REPO_ROOT/scripts/generate_terminal_stress.py" "$LINES" "$SEED" "$WIDTH_HINT"
      ;;
    floating-owner-marker-history|floating-owner-marker-wheel-history)
      printf 'python3 %q --lines %q --seed %q --width-hint %q --marker-block-at %q; exec cat' \
        "$REPO_ROOT/scripts/generate_terminal_stress.py" "$LINES" "$SEED" "$WIDTH_HINT" "$(( LINES / 2 ))"
      ;;
    floating-owner-remote-pair-wheel-history)
      printf 'python3 %q --lines 100 --seed %q --width-hint %q; TERMX_REMOTE_ENABLE=true %q --socket %q --log-file %q remote pair --ttl 2m; python3 %q --lines 100 --seed %q --width-hint %q; exec cat' \
        "$REPO_ROOT/scripts/generate_terminal_stress.py" "$SEED" "$WIDTH_HINT" \
        "$BIN" "$SOCK" "$LOG" \
        "$REPO_ROOT/scripts/generate_terminal_stress.py" "$(( SEED + 1 ))" "$WIDTH_HINT"
      ;;
    deep-live-tail)
      printf 'python3 -c %q %q %q; exec cat' \
        'import sys; persisted=int(sys.argv[1]); cols=int(sys.argv[2]); out=sys.stdout; pad=lambda label, width: label + ("." * max(0, width - len(label))); out.write("".join(f"PERSISTED-{i:05d} committed row\n" for i in range(persisted))); out.write(pad("LIVE-ROW-0", cols) + pad("LIVE-ROW-1", cols) + "LIVE-SCREEN-open-tail"); out.flush()' \
        "$LINES" "$ATTACH_COLS"
      ;;
    history-semantics)
      printf 'python3 %q; exec cat' "$REPO_ROOT/scripts/emit_terminal_history_semantics.py"
      ;;
    bg-forensics)
      printf 'exec ${SHELL:-/bin/sh}'
      ;;
    *)
      echo "unknown scenario: $SCENARIO" >&2
      exit 1
      ;;
  esac
}

create_terminal() {
  local generator_cmd
  local output
  generator_cmd="$(build_generator_command)"
  output="$(
    XDG_CONFIG_HOME="$CFG" \
    XDG_STATE_HOME="$STATE" \
    TERMX_REMOTE_ENABLE=false \
    run_in_pty "$ATTACH_COLS" "$ATTACH_ROWS" \
      env \
      XDG_CONFIG_HOME="$CFG" \
      XDG_STATE_HOME="$STATE" \
      TERMX_REMOTE_ENABLE=false \
      "$BIN" --socket "$SOCK" --log-file "$LOG" \
      new --name grid-stress -- /bin/sh -lc "$generator_cmd"
  )"
  TERM_ID="$(printf '%s\n' "$output" | tr -d '\r' | awk 'NF {line=$0} END {print line}')"
  if [[ -z "$TERM_ID" ]]; then
    echo "failed to parse terminal id from termx new output" >&2
    printf '%s\n' "$output" >&2
    exit 1
  fi
  wait_for_terminal "$TERM_ID"
}

run_baseline_scenario() {
  local pane_main
  local pane_reattach

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "main"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"
  if ! enter_copy_mode_and_repeat_top_until_contains "$SESSION_MAIN" "$pane_main" "copy-top" "000000 [INFO  ] stress   boot" "$G_REPEATS"; then
    return 1
  fi

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "copy-top.escape" Escape
  tmux resize-window -t "$SESSION_MAIN" -x "$RESIZE_COLS" -y "$RESIZE_ROWS"
  sleep 0.5
  if ! enter_copy_mode_and_repeat_top_until_contains "$SESSION_MAIN" "$pane_main" "copy-top-resized" "000000" "$G_REPEATS"; then
    return 1
  fi

  cleanup_tmux_session "$SESSION_MAIN"
  start_attach_tmux_session "$SESSION_REATTACH" "$TERM_ID" "reattach"
  pane_reattach="$ATTACH_SESSION_PANE"
  CLIENT_REATTACH_PID="$ATTACH_SESSION_CLIENT_PID"
  if ! enter_copy_mode_and_repeat_top_until_contains "$SESSION_REATTACH" "$pane_reattach" "reattach-top" "000000" "$G_REPEATS"; then
    return 1
  fi

  assert_contains "$ROOT/copy-top.txt" "000000 [INFO  ] stress   boot"
  assert_contains "$ROOT/copy-top-resized.txt" "000000"
  assert_contains "$ROOT/reattach-top.txt" "000000"

  log "PASS copy-top -> $ROOT/copy-top.txt"
  log "PASS copy-top-resized -> $ROOT/copy-top-resized.txt"
  log "PASS reattach-top -> $ROOT/reattach-top.txt"
}

run_deep_live_tail_scenario() {
  local expected_first_row
  local expected_label
  local pane_main
  local pane_reattach

  if (( LINES < DEEP_LIVE_TAIL_MATERIALIZED_LIMIT + DEEP_LIVE_TAIL_PAGE_LIMIT )); then
    echo "deep-live-tail requires --lines >= $((DEEP_LIVE_TAIL_MATERIALIZED_LIMIT + DEEP_LIVE_TAIL_PAGE_LIMIT))" >&2
    exit 1
  fi

  expected_first_row="$(deep_live_tail_expected_first_row)"
  printf -v expected_label 'PERSISTED-%05d committed row' "$expected_first_row"

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "main"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_session "$SESSION_MAIN" "$pane_main" "latest-tail"
  assert_contains "$ROOT/latest-tail.txt" "LIVE-SCREEN-open-tail"

  if ! enter_copy_mode_and_repeat_top_until_contains "$SESSION_MAIN" "$pane_main" "copy-committed-boundary" "$expected_label" "$DEEP_LIVE_TAIL_TOP_REPEATS"; then
    copy_gridtrace_artifact "$SESSION_MAIN" "copy-committed-boundary"
    return 1
  fi
  copy_gridtrace_artifact "$SESSION_MAIN" "copy-committed-boundary"
  if [[ -f "$ROOT/copy-committed-boundary.gridtrace.summary.txt" ]]; then
    assert_contains "$ROOT/copy-committed-boundary.gridtrace.summary.txt" 'requested_offset="0" requested_limit="500"'
    assert_contains "$ROOT/copy-committed-boundary.gridtrace.summary.txt" 'requested_offset="500" requested_limit="500"'
    assert_contains "$ROOT/copy-committed-boundary.gridtrace.summary.txt" "first_row_id=\"$expected_first_row\""
  fi
  assert_first_match_equals "$ROOT/copy-committed-boundary.txt" 'PERSISTED-[0-9]{5} committed row' "$expected_label"

  cleanup_tmux_session "$SESSION_MAIN"
  start_attach_tmux_session "$SESSION_REATTACH" "$TERM_ID" "reattach"
  pane_reattach="$ATTACH_SESSION_PANE"
  CLIENT_REATTACH_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_session "$SESSION_REATTACH" "$pane_reattach" "reattach-latest-tail"
  assert_contains "$ROOT/reattach-latest-tail.txt" "LIVE-SCREEN-open-tail"

  if ! enter_copy_mode_and_repeat_top_until_contains "$SESSION_REATTACH" "$pane_reattach" "reattach-committed-boundary" "$expected_label" "$DEEP_LIVE_TAIL_TOP_REPEATS"; then
    copy_gridtrace_artifact "$SESSION_REATTACH" "reattach-committed-boundary"
    return 1
  fi
  copy_gridtrace_artifact "$SESSION_REATTACH" "reattach-committed-boundary"
  if [[ -f "$ROOT/reattach-committed-boundary.gridtrace.summary.txt" ]]; then
    assert_contains "$ROOT/reattach-committed-boundary.gridtrace.summary.txt" 'requested_offset="0" requested_limit="500"'
    assert_contains "$ROOT/reattach-committed-boundary.gridtrace.summary.txt" 'requested_offset="500" requested_limit="500"'
    assert_contains "$ROOT/reattach-committed-boundary.gridtrace.summary.txt" "first_row_id=\"$expected_first_row\""
  fi
  assert_first_match_equals "$ROOT/reattach-committed-boundary.txt" 'PERSISTED-[0-9]{5} committed row' "$expected_label"

  log "PASS latest-tail -> $ROOT/latest-tail.txt"
  log "PASS copy-committed-boundary -> $ROOT/copy-committed-boundary.txt"
  log "PASS reattach-committed-boundary -> $ROOT/reattach-committed-boundary.txt"
}

enter_copy_mode_until_contains() {
  local session="$1"
  local target="$2"
  local base="$3"
  local needle="$4"
  local attempt

  send_tmux_keys "$session" "$target" "$base.copy-mode-enter" C-v
  sleep "$G_DELAY"
  for attempt in $(seq 1 "$WAIT_ATTEMPTS"); do
    capture_session "$session" "$target" "$base" || return 1
    capture_preserved_raw_artifact "$session" "$target" "$base"
    if grep -Fq -- "$needle" "$ROOT/$base.txt"; then
      return 0
    fi
    sleep "$WAIT_DELAY"
  done
  echo "timed out waiting for '$needle' in $ROOT/$base.txt after entering copy mode" >&2
  return 1
}

copy_top_until_contains() {
  local session="$1"
  local target="$2"
  local base="$3"
  local needle="$4"
  local repeats="$5"
  local attempt

  for attempt in $(seq 1 "$repeats"); do
    send_tmux_keys "$session" "$target" "$base.copy-mode-g-$attempt" g
    sleep "$G_DELAY"
    capture_session "$session" "$target" "$base" || return 1
    capture_preserved_raw_artifact "$session" "$target" "$base"
    if grep -Fq -- "$needle" "$ROOT/$base.txt"; then
      return 0
    fi
  done
  echo "timed out waiting for '$needle' in $ROOT/$base.txt after $repeats top attempts" >&2
  return 1
}

assert_history_semantics_text() {
  local file="$1"
  local prefix="$2"
  assert_contains "$file" "${prefix}_EL_TO_EOL"
  assert_contains "$file" "${prefix}_CR_FINAL"
  assert_contains "$file" "${prefix}_GAP   X"
  assert_contains "$file" "${prefix}_T"
  assert_contains "$file" "${prefix}_SUGGEST"
  assert_not_contains "$file" "${prefix}_CR_OLD_TRAIL"
  assert_not_contains "$file" "${prefix}_SUGGEST_TMP"
}

run_history_semantics_scenario() {
  local pane_main

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "history-semantics"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"

  capture_until_contains "$SESSION_MAIN" "$pane_main" "history-semantics-live" "HSEM_READY"
  capture_preserved_raw_artifact "$SESSION_MAIN" "$pane_main" "history-semantics-live"
  assert_history_semantics_text "$ROOT/history-semantics-live.txt" "HSEM_LIVE"
  assert_bg_spaces_after_marker "$ROOT/history-semantics-live.raw-preserve.txt" "HSEM_LIVE_EL_TO_EOL" "idx:25" 4

  enter_copy_mode_until_contains "$SESSION_MAIN" "$pane_main" "history-semantics-copy-latest" "HSEM_READY"
  assert_history_semantics_text "$ROOT/history-semantics-copy-latest.txt" "HSEM_LIVE"
  assert_bg_spaces_after_marker "$ROOT/history-semantics-copy-latest.raw-preserve.txt" "HSEM_LIVE_EL_TO_EOL" "idx:25" 4

  copy_top_until_contains "$SESSION_MAIN" "$pane_main" "history-semantics-copy-top" "HSEM_COMMITTED_BEGIN" "$G_REPEATS"
  assert_history_semantics_text "$ROOT/history-semantics-copy-top.txt" "HSEM_COMMITTED"
  assert_bg_spaces_after_marker "$ROOT/history-semantics-copy-top.raw-preserve.txt" "HSEM_COMMITTED_EL_TO_EOL" "idx:24" 4

  log "PASS history-semantics-live -> $ROOT/history-semantics-live.txt"
  log "PASS history-semantics-copy-latest -> $ROOT/history-semantics-copy-latest.txt"
  log "PASS history-semantics-copy-top -> $ROOT/history-semantics-copy-top.txt"
}

run_bg_forensics_scenario() {
  local pane_main
  local command

  pane_main="$(start_tmux_session "$SESSION_MAIN" "$SESSION_COLS" "$SESSION_ROWS" "$(attach_tmux_command "$TERM_ID" "$SESSION_MAIN")")"
  CLIENT_MAIN_PID="$(start_tmux_client "$SESSION_MAIN" "$pane_main")"
  prime_tmux_client "$SESSION_MAIN" "$pane_main"
  sleep "$G_DELAY"
  ensure_tmux_target "$SESSION_MAIN" "$pane_main" "bg-forensics.attach-target" || return 1
  ATTACH_SESSION_PANE="$pane_main"
  pane_main="$ATTACH_SESSION_PANE"

  command="$(printf 'python3 %q --lines %q --seed %q --width-hint %q | tee %q' \
    "$REPO_ROOT/scripts/generate_terminal_stress.py" "$LINES" "$SEED" "$WIDTH_HINT" "$ROOT/bg-forensics-input.raw")"
  send_tmux_literal "$SESSION_MAIN" "$pane_main" "bg-forensics-run-command" "$command"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "bg-forensics-run-enter" Enter

  capture_until_contains "$SESSION_MAIN" "$pane_main" "bg-forensics-live" "$(printf '%06d' "$LINES")"
  enter_copy_mode_until_contains "$SESSION_MAIN" "$pane_main" "bg-forensics-copy" "000"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "bg-forensics-copy-bottom" G
  sleep "$G_DELAY"
  capture_session "$SESSION_MAIN" "$pane_main" "bg-forensics-copy-tail"

  python3 "$REPO_ROOT/scripts/analyze_history_bg_forensics.py" \
    --input "$ROOT/bg-forensics-input.raw" \
    --live "$ROOT/bg-forensics-live.raw.txt" \
    --copy "$ROOT/bg-forensics-copy-tail.raw.txt" \
    --report "$ROOT/bg-forensics.report.txt"

  log "PASS bg-forensics-input -> $ROOT/bg-forensics-input.raw"
  log "PASS bg-forensics-live -> $ROOT/bg-forensics-live.raw.txt"
  log "PASS bg-forensics-copy-tail -> $ROOT/bg-forensics-copy-tail.raw.txt"
  log "PASS bg-forensics-report -> $ROOT/bg-forensics.report.txt"
}

run_floating_owner_resize_scenario() {
  local pane_main
  local key

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "floating-owner-resize"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-base" "grid-stress"
  write_termx_inventory_artifact "floating-base"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-manager.global-mode" C-g
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-manager.open" t
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-manager" "TERMINALS"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-manager-ready" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-attach" C-o
  wait_log_regex_count_at_least 'server attached terminal' 2
  sleep 0.5
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-attached" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-mode.enter" C-o
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-owner.take" a
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 2
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-owned"

  for key in L L L L J J; do
    send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-resize.$key" "$key"
    sleep "$G_DELAY"
  done
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 3
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-resized"
  write_termx_inventory_artifact "floating-resized"
  copy_resize_log_artifact "floating-owner-resize"

  assert_contains "$ROOT/floating-base.txt" "grid-stress"
  assert_contains "$ROOT/floating-manager.txt" "TERMINALS"
  assert_contains "$ROOT/floating-manager-ready.txt" "grid-stress"
  assert_contains "$ROOT/floating-attached.txt" "grid-stress"
  assert_contains "$ROOT/floating-owned.txt" "grid-stress"
  assert_contains "$ROOT/floating-resized.txt" "grid-stress"
  assert_regex_count_at_least "$ROOT/floating-owner-resize.resize.summary.txt" 'server attached terminal' 2
  assert_regex_count_at_least "$ROOT/floating-owner-resize.resize.summary.txt" 'server attached terminal.*resize_owner=false' 1
  assert_regex_count_at_least "$ROOT/floating-owner-resize.resize.summary.txt" 'method=ensure_resize' 3
  assert_terminal_size_shrunk "$ROOT/floating-base.termx-ls.txt" "$ROOT/floating-resized.termx-ls.txt" "$TERM_ID"

  log "PASS floating-base -> $ROOT/floating-base.txt"
  log "PASS floating-owned -> $ROOT/floating-owned.txt"
  log "PASS floating-resized -> $ROOT/floating-resized.txt"
  log "PASS floating-owner-resize log -> $ROOT/floating-owner-resize.resize.summary.txt"
}

run_floating_owner_reattach_history_scenario() {
  local pane_main
  local pane_reattach
  local key

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "floating-owner-reattach"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-reattach-base" "grid-stress"
  write_termx_inventory_artifact "floating-reattach-base"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-reattach-manager.global-mode" C-g
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-reattach-manager.open" t
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-reattach-manager" "TERMINALS"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-reattach-manager-ready" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-reattach-attach" C-o
  wait_log_regex_count_at_least 'server attached terminal' 2
  sleep 0.5
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-reattach-attached" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-reattach-mode.enter" C-o
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-reattach-owner.take" a
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 2
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-reattach-owned"

  for key in L L L L J J; do
    send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-reattach-resize.$key" "$key"
    sleep "$G_DELAY"
  done
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 3
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-reattach-resized"
  write_termx_inventory_artifact "floating-reattach-resized"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-reattach-quit.global-mode" C-g
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-reattach-quit.quit" q
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-reattach-exited" "[tmux-history-smoke] attach exited status=0"
  stop_tmux_client "$CLIENT_MAIN_PID"
  CLIENT_MAIN_PID=""
  cleanup_tmux_session "$SESSION_MAIN"

  start_attach_tmux_session "$SESSION_REATTACH" "$TERM_ID" "floating-owner-reenter"
  pane_reattach="$ATTACH_SESSION_PANE"
  CLIENT_REATTACH_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_until_contains "$SESSION_REATTACH" "$pane_reattach" "floating-reattach-live" "grid-stress"
  write_termx_inventory_artifact "floating-reattach-live"

  if ! enter_copy_mode_and_repeat_top_until_contains "$SESSION_REATTACH" "$pane_reattach" "floating-reattach-copy-top" "000000" "$G_REPEATS"; then
    copy_gridtrace_artifact "$SESSION_REATTACH" "floating-reattach-copy-top"
    return 1
  fi
  copy_gridtrace_artifact "$SESSION_REATTACH" "floating-reattach-copy-top"
  copy_resize_log_artifact "floating-owner-reattach-history"

  assert_contains "$ROOT/floating-reattach-base.txt" "grid-stress"
  assert_contains "$ROOT/floating-reattach-attached.txt" "grid-stress"
  assert_contains "$ROOT/floating-reattach-owned.txt" "grid-stress"
  assert_contains "$ROOT/floating-reattach-resized.txt" "grid-stress"
  assert_contains "$ROOT/floating-reattach-exited.txt" "[tmux-history-smoke] attach exited status=0"
  assert_contains "$ROOT/floating-reattach-live.txt" "grid-stress"
  assert_contains "$ROOT/floating-reattach-copy-top.txt" "000000"
  assert_stress_history_label_order "$ROOT/floating-reattach-copy-top.txt"
  if [[ -f "$ROOT/floating-reattach-copy-top.gridtrace.summary.txt" ]]; then
    assert_gridtrace_single_requested_cols "$ROOT/floating-reattach-copy-top.gridtrace.summary.txt"
  fi
  assert_regex_count_at_least "$ROOT/floating-owner-reattach-history.resize.summary.txt" 'server attached terminal' 3
  assert_regex_count_at_least "$ROOT/floating-owner-reattach-history.resize.summary.txt" 'method=ensure_resize' 3
  assert_terminal_size_shrunk "$ROOT/floating-reattach-base.termx-ls.txt" "$ROOT/floating-reattach-resized.termx-ls.txt" "$TERM_ID"

  log "PASS floating-reattach-live -> $ROOT/floating-reattach-live.txt"
  log "PASS floating-reattach-copy-top -> $ROOT/floating-reattach-copy-top.txt"
  log "PASS floating-owner-reattach-history log -> $ROOT/floating-owner-reattach-history.resize.summary.txt"
}

run_floating_owner_wheel_history_scenario() {
  local pane_main
  local key
  local x
  local y

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "floating-owner-wheel"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-wheel-base" "grid-stress"
  write_termx_inventory_artifact "floating-wheel-base"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-wheel-manager.global-mode" C-g
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-wheel-manager.open" t
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-wheel-manager" "TERMINALS"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-wheel-manager-ready" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-wheel-attach" C-o
  wait_log_regex_count_at_least 'server attached terminal' 2
  sleep 0.5
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-wheel-attached" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-wheel-mode.enter" C-o
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-wheel-owner.take" a
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 2
  sleep 0.5

  for key in L L L L J J; do
    send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-wheel-resize.$key" "$key"
    sleep "$G_DELAY"
  done
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 3
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-wheel-resized"
  write_termx_inventory_artifact "floating-wheel-resized"

  x=$(( ATTACH_COLS / 2 ))
  y=$(( ATTACH_ROWS / 2 ))
  send_tmux_mouse_wheel_up "$SESSION_MAIN" "$pane_main" "floating-wheel-up" "$x" "$y"
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-wheel-after-wheel"
  copy_resize_log_artifact "floating-owner-wheel-history"

  assert_contains "$ROOT/floating-wheel-base.txt" "grid-stress"
  assert_contains "$ROOT/floating-wheel-attached.txt" "grid-stress"
  assert_contains "$ROOT/floating-wheel-resized.txt" "grid-stress"
  assert_not_contains "$ROOT/floating-wheel-after-wheel.txt" "000000"
  assert_stress_history_label_order_visible "$ROOT/floating-wheel-after-wheel.txt"
  assert_regex_count_at_least "$ROOT/floating-owner-wheel-history.resize.summary.txt" 'server attached terminal' 2
  assert_regex_count_at_least "$ROOT/floating-owner-wheel-history.resize.summary.txt" 'method=ensure_resize' 3
  assert_terminal_size_shrunk "$ROOT/floating-wheel-base.termx-ls.txt" "$ROOT/floating-wheel-resized.termx-ls.txt" "$TERM_ID"

  log "PASS floating-wheel-after-wheel -> $ROOT/floating-wheel-after-wheel.txt"
  log "PASS floating-owner-wheel-history log -> $ROOT/floating-owner-wheel-history.resize.summary.txt"
}

run_floating_owner_marker_history_scenario() {
  local pane_main
  local key

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "floating-owner-marker"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-base" "grid-stress"
  write_termx_inventory_artifact "floating-marker-base"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-manager.global-mode" C-g
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-manager.open" t
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-manager-ready" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-attach" C-o
  wait_log_regex_count_at_least 'server attached terminal' 2
  sleep 0.5
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-attached" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-mode.enter" C-o
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-owner.take" a
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 2
  sleep 0.5

  for key in L L L L J J; do
    send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-resize.$key" "$key"
    sleep "$G_DELAY"
  done
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 3
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-marker-resized"
  write_termx_inventory_artifact "floating-marker-resized"

  enter_copy_mode_and_scan_down_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-after-marker" "TERM_X_TEXT_QR_BEGIN" "$G_REPEATS"
  scan_down_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-after-marker" "████ ▄▄▄▄▄ ███▄" 8
  copy_resize_log_artifact "floating-owner-marker-history"

  assert_contains "$ROOT/floating-marker-base.txt" "grid-stress"
  assert_contains "$ROOT/floating-marker-resized.txt" "grid-stress"
  assert_contains "$ROOT/floating-marker-after-marker.txt" "TERM_X_TEXT_QR_BEGIN"
  assert_floating_contains "$ROOT/floating-marker-after-marker.txt" "████ ▄▄▄▄▄ ███▄"
  assert_stress_history_label_order_visible "$ROOT/floating-marker-after-marker.txt"
  assert_regex_count_at_least "$ROOT/floating-owner-marker-history.resize.summary.txt" 'server attached terminal' 2
  assert_regex_count_at_least "$ROOT/floating-owner-marker-history.resize.summary.txt" 'method=ensure_resize' 3
  assert_terminal_size_shrunk "$ROOT/floating-marker-base.termx-ls.txt" "$ROOT/floating-marker-resized.termx-ls.txt" "$TERM_ID"

  log "PASS floating-marker-after-marker -> $ROOT/floating-marker-after-marker.txt"
  log "PASS floating-owner-marker-history log -> $ROOT/floating-owner-marker-history.resize.summary.txt"
}

run_floating_owner_marker_wheel_history_scenario() {
  local pane_main
  local key
  local x
  local y

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "floating-owner-marker-wheel"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-base" "grid-stress"
  write_termx_inventory_artifact "floating-marker-wheel-base"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-manager.global-mode" C-g
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-manager.open" t
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-manager-ready" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-attach" C-o
  wait_log_regex_count_at_least 'server attached terminal' 2
  sleep 0.5
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-attached" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-mode.enter" C-o
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-owner.take" a
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 2
  sleep 0.5

  for key in L L L L J J; do
    send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-resize.$key" "$key"
    sleep "$G_DELAY"
  done
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 3
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-resized"
  write_termx_inventory_artifact "floating-marker-wheel-resized"

  x=$(( ATTACH_COLS / 2 ))
  y=$(( ATTACH_ROWS / 2 ))
  send_tmux_mouse_wheel_up "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-enter-copy" "$x" "$y"
  sleep 0.5
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-copy-top" g
  sleep "$G_DELAY"
  capture_session "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-at-top"

  mouse_wheel_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-after-marker" "TERM_X_TEXT_QR_BEGIN" "$G_REPEATS" down
  mouse_wheel_until_contains "$SESSION_MAIN" "$pane_main" "floating-marker-wheel-after-marker" "████ ▄▄▄▄▄ ███▄" 20 down
  copy_gridtrace_artifact "$SESSION_MAIN" "floating-marker-wheel-after-marker"
  copy_resize_log_artifact "floating-owner-marker-wheel-history"

  assert_contains "$ROOT/floating-marker-wheel-base.txt" "grid-stress"
  assert_contains "$ROOT/floating-marker-wheel-resized.txt" "grid-stress"
  assert_contains "$ROOT/floating-marker-wheel-after-marker.txt" "TERM_X_TEXT_QR_BEGIN"
  assert_floating_contains "$ROOT/floating-marker-wheel-after-marker.txt" "████ ▄▄▄▄▄ ███▄"
  assert_stress_history_label_order_visible "$ROOT/floating-marker-wheel-after-marker.txt"
  assert_regex_count_at_least "$ROOT/floating-owner-marker-wheel-history.resize.summary.txt" 'server attached terminal' 2
  assert_regex_count_at_least "$ROOT/floating-owner-marker-wheel-history.resize.summary.txt" 'method=ensure_resize' 3
  assert_terminal_size_shrunk "$ROOT/floating-marker-wheel-base.termx-ls.txt" "$ROOT/floating-marker-wheel-resized.termx-ls.txt" "$TERM_ID"

  log "PASS floating-marker-wheel-after-marker -> $ROOT/floating-marker-wheel-after-marker.txt"
  log "PASS floating-owner-marker-wheel-history log -> $ROOT/floating-owner-marker-wheel-history.resize.summary.txt"
}

run_floating_owner_remote_pair_wheel_history_scenario() {
  local pane_main
  local key
  local x
  local y

  start_attach_tmux_session "$SESSION_MAIN" "$TERM_ID" "floating-owner-remote-pair-wheel"
  pane_main="$ATTACH_SESSION_PANE"
  CLIENT_MAIN_PID="$ATTACH_SESSION_CLIENT_PID"
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-base" "grid-stress"
  write_termx_inventory_artifact "floating-remote-pair-wheel-base"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-manager.global-mode" C-g
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-manager.open" t
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-manager-ready" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-attach" C-o
  wait_log_regex_count_at_least 'server attached terminal' 2
  sleep 0.5
  capture_until_contains "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-attached" "grid-stress"

  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-mode.enter" C-o
  sleep "$G_DELAY"
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-owner.take" a
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 2
  sleep 0.5

  for key in L L L L J J; do
    send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-resize.$key" "$key"
    sleep "$G_DELAY"
  done
  wait_log_regex_count_at_least 'msg="termx protocol request started".*method=ensure_resize' 3
  sleep 0.5
  capture_session "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-resized"
  write_termx_inventory_artifact "floating-remote-pair-wheel-resized"

  x=$(( ATTACH_COLS / 2 ))
  y=$(( ATTACH_ROWS / 2 ))
  send_tmux_mouse_wheel_up "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-enter-copy" "$x" "$y"
  sleep 0.5
  send_tmux_keys "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-copy-top" g
  sleep "$G_DELAY"
  capture_session "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-at-top"

  dump_grid_viewport_artifact "floating-remote-pair-wheel-grid-dump" 1200 "$RESIZE_COLS"
  assert_remote_pair_history_continuity "$ROOT/floating-remote-pair-wheel-grid-dump.txt" "$ROOT/floating-remote-pair-wheel-continuity.txt"
  send_tmux_mouse_wheel_down "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-wheel-sample-1" "$x" "$y"
  sleep "$G_DELAY"
  capture_session "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-after-qr"
  send_tmux_mouse_wheel_down "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-wheel-sample-2" "$x" "$y"
  sleep "$G_DELAY"
  capture_session "$SESSION_MAIN" "$pane_main" "floating-remote-pair-wheel-after-expires"
  copy_gridtrace_artifact "$SESSION_MAIN" "floating-remote-pair-wheel-after-expires"
  copy_resize_log_artifact "floating-owner-remote-pair-wheel-history"

  assert_contains "$ROOT/floating-remote-pair-wheel-base.txt" "grid-stress"
  assert_contains "$ROOT/floating-remote-pair-wheel-resized.txt" "grid-stress"
  assert_contains "$ROOT/floating-remote-pair-wheel-grid-dump.txt" "uri:"
  assert_contains "$ROOT/floating-remote-pair-wheel-grid-dump.txt" "termx://pair"
  assert_contains "$ROOT/floating-remote-pair-wheel-grid-dump.txt" "expires_at:"
  assert_stress_history_label_order_visible "$ROOT/floating-remote-pair-wheel-at-top.txt"
  assert_regex_count_at_least "$ROOT/floating-owner-remote-pair-wheel-history.resize.summary.txt" 'server attached terminal' 2
  assert_regex_count_at_least "$ROOT/floating-owner-remote-pair-wheel-history.resize.summary.txt" 'method=ensure_resize' 3
  assert_terminal_size_shrunk "$ROOT/floating-remote-pair-wheel-base.termx-ls.txt" "$ROOT/floating-remote-pair-wheel-resized.termx-ls.txt" "$TERM_ID"

  log "PASS floating-remote-pair-wheel-after-qr -> $ROOT/floating-remote-pair-wheel-after-qr.txt"
  log "PASS floating-remote-pair-wheel-after-expires -> $ROOT/floating-remote-pair-wheel-after-expires.txt"
  log "PASS floating-remote-pair-wheel-continuity -> $ROOT/floating-remote-pair-wheel-continuity.txt"
  log "PASS floating-owner-remote-pair-wheel-history log -> $ROOT/floating-owner-remote-pair-wheel-history.resize.summary.txt"
}

attach_command() {
  local terminal_id="$1"
  local session_name="${2:-}"
  local trace_path=""
  if [[ ( "$SCENARIO" == "deep-live-tail" || "$SCENARIO" == "floating-owner-reattach-history" || "$SCENARIO" == "floating-owner-wheel-history" || "$SCENARIO" == "floating-owner-marker-history" || "$SCENARIO" == "floating-owner-marker-wheel-history" || "$SCENARIO" == "floating-owner-remote-pair-wheel-history" ) && -n "$session_name" && "${TERMX_TMUX_HISTORY_TRACE:-0}" != "0" ]]; then
    trace_path="$ROOT/${session_name}.gridtrace.log"
  fi
  if [[ -n "$trace_path" ]]; then
    printf 'env XDG_CONFIG_HOME=%q XDG_STATE_HOME=%q TERMX_REMOTE_ENABLE=false TERMX_GRID_HISTORY_TRACE=%q %q --socket %q --log-file %q attach %q' \
      "$CFG" "$STATE" "$trace_path" "$BIN" "$SOCK" "$LOG" "$terminal_id"
    return
  fi
  printf 'env XDG_CONFIG_HOME=%q XDG_STATE_HOME=%q TERMX_REMOTE_ENABLE=false %q --socket %q --log-file %q attach %q' \
    "$CFG" "$STATE" "$BIN" "$SOCK" "$LOG" "$terminal_id"
}

attach_tmux_command() {
  local terminal_id="$1"
  local session_name="$2"
  local command
  command="$(attach_command "$terminal_id" "$session_name")"
  printf 'sh -lc %q' "set +e; $command; status=\$?; printf '\n[tmux-history-smoke] attach exited status=%s\n' \"\$status\"; while :; do sleep 3600; done"
}

main() {
  local attach_size
  local resize_size

  REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  ROOT=""
  BIN=""
  BUILD_BIN="1"
  KEEP_ROOT="1"
  LINES="1000"
  SEED="100"
  WIDTH_HINT="120"
  SCENARIO="baseline"
  ATTACH_COLS="120"
  ATTACH_ROWS="36"
  RESIZE_COLS="90"
  RESIZE_ROWS="28"
  G_REPEATS="24"
  DEEP_LIVE_TAIL_MATERIALIZED_LIMIT="12000"
  DEEP_LIVE_TAIL_PAGE_LIMIT="500"
  DEEP_LIVE_TAIL_SCREEN_LIVE_ROWS="3"
  DEEP_LIVE_TAIL_TOP_REPEATS="4"
  WAIT_ATTEMPTS="80"
  WAIT_DELAY="0.2"
  ATTACH_PLACEHOLDER_RETRY_ATTEMPTS="24"
  ATTACH_SURFACE_RETRY_LIMIT="1"
  G_DELAY="0.12"
  SESSION_PREFIX=""
  SESSION_MAIN=""
  SESSION_REATTACH=""
  CLIENT_MAIN_PID=""
  CLIENT_REATTACH_PID=""
  DAEMON_PID=""
  GRID_DUMP_TOOL_DIR=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --scenario)
        SCENARIO="$2"
        shift 2
        ;;
      --root)
        ROOT="$2"
        shift 2
        ;;
      --bin)
        BIN="$2"
        BUILD_BIN="0"
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
        attach_size="$(parse_size "$2")"
        ATTACH_COLS="${attach_size%% *}"
        ATTACH_ROWS="${attach_size##* }"
        shift 2
        ;;
      --resize-size)
        resize_size="$(parse_size "$2")"
        RESIZE_COLS="${resize_size%% *}"
        RESIZE_ROWS="${resize_size##* }"
        shift 2
        ;;
      --g-repeats)
        G_REPEATS="$2"
        shift 2
        ;;
      --keep-root)
        KEEP_ROOT="1"
        shift
        ;;
      --cleanup-root)
        KEEP_ROOT="0"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "unknown argument: $1" >&2
        usage >&2
        exit 1
        ;;
    esac
  done

  if [[ -z "$ROOT" ]]; then
    ROOT="$(mktemp -d /tmp/termx-grid-smoke.XXXXXX)"
  else
    mkdir -p "$ROOT"
  fi

  if [[ -z "$BIN" ]]; then
    BIN="$ROOT/termx"
  fi

  SOCK="$ROOT/termx.sock"
  LOG="$ROOT/termx.log"
  CFG="$ROOT/config"
  STATE="$ROOT/state"
  DAEMON_STDOUT="$ROOT/daemon.out"
  TMUX_SESSIONS_FILE="$ROOT/tmux-sessions.txt"
  : >"$TMUX_SESSIONS_FILE"
  SESSION_PREFIX="$(sanitize_tmux_name "$(basename "$ROOT")")-$$"
  SESSION_MAIN="${SESSION_PREFIX}-main"
  SESSION_REATTACH="${SESSION_PREFIX}-re"
  read -r SESSION_COLS SESSION_ROWS <<<"$(preferred_tmux_session_size)"

  trap cleanup EXIT

  need go
  need python3
  need tmux

  build_bin_if_needed
  log "artifact root: $ROOT"
  log "tmux session size: ${SESSION_COLS}x${SESSION_ROWS} (terminal pty ${ATTACH_COLS}x${ATTACH_ROWS})"
  log "starting isolated daemon"
  start_daemon
  log "creating stress terminal"
  create_terminal
  log "created terminal: $TERM_ID"

  case "$SCENARIO" in
    baseline|standard)
      run_baseline_scenario
      ;;
    deep-live-tail)
      run_deep_live_tail_scenario
      ;;
    history-semantics)
      run_history_semantics_scenario
      ;;
    bg-forensics)
      run_bg_forensics_scenario
      ;;
    floating-owner-resize)
      run_floating_owner_resize_scenario
      ;;
    floating-owner-reattach-history)
      run_floating_owner_reattach_history_scenario
      ;;
    floating-owner-wheel-history)
      run_floating_owner_wheel_history_scenario
      ;;
    floating-owner-marker-history)
      run_floating_owner_marker_history_scenario
      ;;
    floating-owner-marker-wheel-history)
      run_floating_owner_marker_wheel_history_scenario
      ;;
    floating-owner-remote-pair-wheel-history)
      run_floating_owner_remote_pair_wheel_history_scenario
      ;;
    *)
      echo "unknown scenario: $SCENARIO" >&2
      exit 1
      ;;
  esac
}

main "$@"
