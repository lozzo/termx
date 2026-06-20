#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/termx_tui_stress_memory.sh [options]

Run the real interactive stress path:
  build/use termx -> start core-v2 daemon -> create shell terminal
  -> attach tui-v3 in tmux -> type and run:
     time python3 scripts/generate_terminal_stress.py --lines N
  -> record daemon/TUI RSS + heap profiles -> enter copy/history oldest
  -> verify the oldest page can trace back to line 000000.

Options:
  --root PATH           artifact root; default is a new /tmp directory
  --bin PATH            existing termx binary; default builds into ROOT/termx
  --lines N             stress line count; default 100000
  --seed N              stress seed; default 100
  --width-hint N        stress width hint; default 120
  --attach-size CxR     tmux attach size; default 120x36
  --baseline-time       run the same stress script outside termx first (default)
  --no-baseline-time    skip outside-termx baseline timing
  --profile-mode MODE   heap profile capture mode: final, all, or none; default final
  --daemon-memory-limit-mb N
                        set TERMX_DAEMON_MEMORY_LIMIT_MB for daemon runtime GC pacing
  --daemon-request-reclaim-min-heap-mb N
                        set TERMX_DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB for request-boundary page reclaim
  --daemon-history-file-backend
                        force daemon compact history payloads into artifact file backend dir
  --tui-memory-limit-mb N
                        set TERMX_TUI_MEMORY_LIMIT_MB for TUI runtime GC pacing
  --wait-seconds N      max wait for attach/copy markers; default 90
  --keep-root           keep artifact root after success/failure (default)
  --cleanup-root        remove artifact root on exit
  -h, --help            show this help

Artifacts:
  memory.tsv            RSS, CPU%, and cumulative CPU samples
  memory-samples.tsv    daemon/TUI RSS samples collected during stress
  memory-peaks.tsv      daemon/TUI peak RSS derived from stress samples
  baseline-time.txt     outside-termx baseline time(1) output when enabled
  stress-time.txt       captured time(1) output line from the TUI pane
  profile-summary.txt   daemon/TUI pprof top summaries
  profile-graphs/       daemon/TUI pprof DOT graphs and SVG graphs when Graphviz exists
  daemon-memstats/       daemon runtime MemStats samples collected at RSS stages
  tui-memstats/          TUI runtime MemStats samples collected at RSS stages
  state/termx/core-v2-history/ default daemon file-backed compact history payloads
  daemon-history-backend/ forced compact history payload dir when --daemon-history-file-backend is set
  live.txt              tmux capture after stress completes
  copy-oldest.txt       capture after copy/history oldest jump
  daemon-heap/          daemon heap profiles captured by --profile-mode
  tui-heap/             TUI heap profiles captured by --profile-mode
EOF
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

log() {
  printf '[termx-tui-stress-memory] %s\n' "$*"
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
    if tmux capture-pane -t "$target" -p -S -4000 2>/dev/null | LC_ALL=C grep -Fq "$pattern"; then
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

process_stats() {
  local pid="$1"
  ps -o rss= -o %cpu= -o time= -p "$pid" 2>/dev/null | awk 'NF >= 3 {
    printf "%d\t%.1f\t%s\n", $1 + 0, $2 + 0, $3
    found=1
  } END {
    if (!found) print "0\t0.0\t0:00.00"
  }'
}

sample_processes_until_stopped() {
  local stop_file="$1"
  local interval="$2"
  local daemon_pid="$3"
  local tui_pid="$4"
  local samples="$ROOT/memory-samples.tsv"
  printf 'unix_ms\tprocess\tpid\trss_kib\trss_mib\tcpu_percent\tcpu_time\n' >"$samples"
  while [[ ! -f "$stop_file" ]]; do
    sample_process_once "daemon" "$daemon_pid" "$samples"
    sample_process_once "tui" "$tui_pid" "$samples"
    sleep "$interval"
  done
  sample_process_once "daemon" "$daemon_pid" "$samples"
  sample_process_once "tui" "$tui_pid" "$samples"
}

sample_process_once() {
  local process="$1"
  local pid="$2"
  local samples="$3"
  local stats rss cpu_percent cpu_time unix_ms
  stats="$(process_stats "$pid")"
  IFS=$'\t' read -r rss cpu_percent cpu_time <<EOF
$stats
EOF
  unix_ms="$(python3 - <<'PY'
import time
print(int(time.time() * 1000))
PY
)"
  awk -v unix_ms="$unix_ms" -v process="$process" -v pid="$pid" -v rss="$rss" -v cpu_percent="$cpu_percent" -v cpu_time="$cpu_time" 'BEGIN {
    printf "%s\t%s\t%s\t%d\t%.1f\t%.1f\t%s\n", unix_ms, process, pid, rss, rss / 1024, cpu_percent, cpu_time
  }' >>"$samples"
}

write_memory_peaks() {
  local samples="$ROOT/memory-samples.tsv"
  local peaks="$ROOT/memory-peaks.tsv"
  if [[ ! -s "$samples" ]]; then
    return 0
  fi
  awk -F '\t' '
    NR == 1 { next }
    {
      process=$2
      rss=$4 + 0
      if (!(process in peak) || rss > peak[process]) {
        peak[process]=rss
        peak_mib[process]=$5
        peak_cpu_percent[process]=$6
        peak_cpu_time[process]=$7
        peak_unix_ms[process]=$1
      }
    }
    END {
      print "process\tpeak_unix_ms\tpeak_rss_kib\tpeak_rss_mib\tcpu_percent_at_peak\tcpu_time_at_peak"
      for (process in peak) {
        printf "%s\t%s\t%d\t%.1f\t%.1f\t%s\n", process, peak_unix_ms[process], peak[process], peak_mib[process], peak_cpu_percent[process], peak_cpu_time[process]
      }
    }
  ' "$samples" >"$peaks"
}

record_rss() {
  local stage="$1"
  local process="$2"
  local pid="$3"
  local stats rss cpu_percent cpu_time
  stats="$(process_stats "$pid")"
  IFS=$'\t' read -r rss cpu_percent cpu_time <<EOF
$stats
EOF
  awk -v stage="$stage" -v process="$process" -v pid="$pid" -v rss="$rss" -v cpu_percent="$cpu_percent" -v cpu_time="$cpu_time" 'BEGIN {
    printf "%s\t%s\t%s\t%d\t%.1f\t%.1f\t%s\n", stage, process, pid, rss, rss / 1024, cpu_percent, cpu_time
  }' >>"$REPORT"
}

capture_memstats() {
  local stage="$1"
  local process="$2"
  local pid="$3"
  if ! kill -0 "$pid" 2>/dev/null; then
    return 0
  fi
  printf '%s\n' "$stage" >"$DIAG_STAGE_FILE"
  kill -USR2 "$pid" 2>/dev/null || true
  sleep 0.05
}

record_stage() {
  local stage="$1"
  local process="$2"
  local pid="$3"
  record_rss "$stage" "$process" "$pid"
  capture_memstats "$stage" "$process" "$pid"
}

capture_heap_profile() {
  local stage="$1"
  local process="$2"
  local pid="$3"
  if [[ "$PROFILE_MODE" == "none" ]]; then
    return 0
  fi
  if kill -0 "$pid" 2>/dev/null; then
    log "capturing ${process} heap profile: $stage"
    kill -USR1 "$pid" 2>/dev/null || true
    sleep 0.3
  fi
}

maybe_capture_stage_profile() {
  local stage="$1"
  local process="$2"
  local pid="$3"
  if [[ "$PROFILE_MODE" == "all" ]]; then
    capture_heap_profile "$stage" "$process" "$pid"
  fi
}

capture_pane() {
  local target="$1"
  local name="$2"
  tmux capture-pane -t "$target" -p -S -4000 >"$ROOT/$name.txt" || true
  tmux capture-pane -t "$target" -ep -S -4000 >"$ROOT/$name.raw.txt" || true
}

latest_profile() {
  local dir="$1"
  find "$dir" -type f -name '*.pprof' -print0 2>/dev/null | xargs -0 ls -t 2>/dev/null | head -n 1 || true
}

write_profile_svg() {
  local title="$1"
  local output="$2"
  shift 2
  local err
  err="$("$@" 2>&1 >/dev/null)" || {
    {
      echo "$title"
      echo "$err"
      echo
    } >>"$ROOT/profile-graphs/README.txt"
    return 0
  }
  printf '%s\n' "$output"
}

write_profile_summary() {
  local summary="$ROOT/profile-summary.txt"
  local daemon_profile tui_profile
  local graphs_dir="$ROOT/profile-graphs"
  daemon_profile="$(latest_profile "$DAEMON_HEAP_DIR")"
  tui_profile="$(latest_profile "$TUI_HEAP_DIR")"
  mkdir -p "$graphs_dir"
  : >"$graphs_dir/README.txt"
  {
    echo "# termx TUI stress profile summary"
    echo
    echo "artifact_root=$ROOT"
    echo "profile_mode=$PROFILE_MODE"
    echo "daemon_profile=$daemon_profile"
    echo "tui_profile=$tui_profile"
    if [[ -s "$DAEMON_MEMSTATS_DIR/memstats.tsv" ]]; then
      echo
      echo "## daemon memstats"
      cat "$DAEMON_MEMSTATS_DIR/memstats.tsv"
    fi
    if [[ -s "$TUI_MEMSTATS_DIR/memstats.tsv" ]]; then
      echo
      echo "## tui memstats"
      cat "$TUI_MEMSTATS_DIR/memstats.tsv"
    fi
    if [[ -s "$ROOT/memory-peaks.tsv" ]]; then
      echo
      echo "## stress RSS peaks"
      cat "$ROOT/memory-peaks.tsv"
    fi
    if [[ -d "$DAEMON_DEFAULT_HISTORY_BACKEND_DIR" ]]; then
      echo
      echo "## daemon default history file backend"
      find "$DAEMON_DEFAULT_HISTORY_BACKEND_DIR" -type f -name '*.compact' -print0 2>/dev/null | xargs -0 ls -lh 2>/dev/null || true
    fi
    if [[ "$DAEMON_HISTORY_FILE_BACKEND" == "1" ]]; then
      echo
      echo "## daemon forced history file backend"
      find "$DAEMON_HISTORY_BACKEND_DIR" -type f -name '*.compact' -print0 2>/dev/null | xargs -0 ls -lh 2>/dev/null || true
    fi
    echo
    if [[ -n "$daemon_profile" ]]; then
      echo "## daemon inuse_space"
      go tool pprof -top "$daemon_profile" || true
      echo
      echo "## daemon alloc_space"
      go tool pprof -top -alloc_space "$daemon_profile" || true
      echo
      go tool pprof -dot -output "$graphs_dir/daemon-inuse.dot" "$daemon_profile" >/dev/null 2>&1 || true
      go tool pprof -dot -alloc_space -output "$graphs_dir/daemon-alloc.dot" "$daemon_profile" >/dev/null 2>&1 || true
      write_profile_svg "daemon inuse SVG failed" "$graphs_dir/daemon-inuse.svg" go tool pprof -svg -output "$graphs_dir/daemon-inuse.svg" "$daemon_profile"
      write_profile_svg "daemon alloc SVG failed" "$graphs_dir/daemon-alloc.svg" go tool pprof -svg -alloc_space -output "$graphs_dir/daemon-alloc.svg" "$daemon_profile"
    fi
    if [[ -n "$tui_profile" ]]; then
      echo "## tui inuse_space"
      go tool pprof -top "$tui_profile" || true
      echo
      echo "## tui alloc_space"
      go tool pprof -top -alloc_space "$tui_profile" || true
      echo
      go tool pprof -dot -output "$graphs_dir/tui-inuse.dot" "$tui_profile" >/dev/null 2>&1 || true
      go tool pprof -dot -alloc_space -output "$graphs_dir/tui-alloc.dot" "$tui_profile" >/dev/null 2>&1 || true
      write_profile_svg "tui inuse SVG failed" "$graphs_dir/tui-inuse.svg" go tool pprof -svg -output "$graphs_dir/tui-inuse.svg" "$tui_profile"
      write_profile_svg "tui alloc SVG failed" "$graphs_dir/tui-alloc.svg" go tool pprof -svg -alloc_space -output "$graphs_dir/tui-alloc.svg" "$tui_profile"
    fi
  } >"$summary"
}

positive_integer() {
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

ROOT=""
BIN=""
LINES=100000
SEED=100
WIDTH_HINT=120
ATTACH_SIZE="120x36"
WAIT_SECONDS=90
CLEANUP_ROOT=0
DAEMON_MEMORY_LIMIT_MB=""
DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB=""
DAEMON_HISTORY_FILE_BACKEND=0
TUI_MEMORY_LIMIT_MB=""
BASELINE_TIME=1
PROFILE_MODE="final"

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
    --baseline-time)
      BASELINE_TIME=1
      shift
      ;;
    --no-baseline-time)
      BASELINE_TIME=0
      shift
      ;;
    --profile-mode)
      PROFILE_MODE="$2"
      shift 2
      ;;
    --daemon-memory-limit-mb)
      DAEMON_MEMORY_LIMIT_MB="$2"
      shift 2
      ;;
    --daemon-request-reclaim-min-heap-mb)
      DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB="$2"
      shift 2
      ;;
    --daemon-history-file-backend)
      DAEMON_HISTORY_FILE_BACKEND=1
      shift
      ;;
    --tui-memory-limit-mb)
      TUI_MEMORY_LIMIT_MB="$2"
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

positive_integer "--lines" "$LINES"
positive_integer "--wait-seconds" "$WAIT_SECONDS"
if [[ -n "$DAEMON_MEMORY_LIMIT_MB" ]]; then
  positive_integer "--daemon-memory-limit-mb" "$DAEMON_MEMORY_LIMIT_MB"
fi
if [[ -n "$DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB" ]]; then
  positive_integer "--daemon-request-reclaim-min-heap-mb" "$DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB"
fi
if [[ -n "$TUI_MEMORY_LIMIT_MB" ]]; then
  positive_integer "--tui-memory-limit-mb" "$TUI_MEMORY_LIMIT_MB"
fi
case "$PROFILE_MODE" in
  final|all|none)
    ;;
  *)
    echo "--profile-mode must be final, all, or none" >&2
    exit 1
    ;;
esac

need awk
need go
need grep
need ps
need python3
need tmux

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STRESS_SCRIPT="$REPO_ROOT/scripts/generate_terminal_stress.py"
read -r ATTACH_COLS ATTACH_ROWS < <(parse_size "$ATTACH_SIZE")

if [[ -z "$ROOT" ]]; then
  ROOT="$(mktemp -d "${TMPDIR:-/tmp}/termx-tui-stress-memory.XXXXXX")"
else
  mkdir -p "$ROOT"
fi
ROOT="$(cd "$ROOT" && pwd)"

SOCK="$ROOT/termx-core-v2.sock"
LOG="$ROOT/termx.log"
REPORT="$ROOT/memory.tsv"
DAEMON_HEAP_DIR="$ROOT/daemon-heap"
TUI_HEAP_DIR="$ROOT/tui-heap"
DAEMON_MEMSTATS_DIR="$ROOT/daemon-memstats"
TUI_MEMSTATS_DIR="$ROOT/tui-memstats"
DAEMON_HISTORY_BACKEND_DIR="$ROOT/daemon-history-backend"
DAEMON_DEFAULT_STATE_DIR="$ROOT/state"
DAEMON_DEFAULT_HISTORY_BACKEND_DIR="$DAEMON_DEFAULT_STATE_DIR/termx/core-v2-history"
DIAG_STAGE_FILE="$ROOT/diag-stage.txt"
SESSION="termx-tui-stress-$$"
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
  (cd "$REPO_ROOT" && go build -o "$BIN" ./termx-cli/cmd/termx)
else
  BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"
fi

if [[ ! -x "$BIN" ]]; then
  echo "termx binary is not executable: $BIN" >&2
  exit 1
fi

printf 'stage\tprocess\tpid\trss_kib\trss_mib\tcpu_percent\tcpu_time\n' >"$REPORT"
mkdir -p "$TUI_HEAP_DIR"
mkdir -p "$DAEMON_HEAP_DIR"
mkdir -p "$TUI_MEMSTATS_DIR"
mkdir -p "$DAEMON_MEMSTATS_DIR"
if [[ "$DAEMON_HISTORY_FILE_BACKEND" == "1" ]]; then
  mkdir -p "$DAEMON_HISTORY_BACKEND_DIR"
fi

if [[ "$BASELINE_TIME" == "1" ]]; then
  log "running outside-termx stress baseline: lines=$LINES seed=$SEED"
  /usr/bin/time -p python3 "$STRESS_SCRIPT" --lines "$LINES" --seed "$SEED" --width-hint "$WIDTH_HINT" >"$ROOT/baseline-output.txt" 2>"$ROOT/baseline-time.txt"
  rm -f "$ROOT/baseline-output.txt"
fi

log "starting daemon"
DAEMON_ENV_PREFIX=(XDG_STATE_HOME="$DAEMON_DEFAULT_STATE_DIR")
DAEMON_HISTORY_FILE_BACKEND_ENV=""
if [[ "$DAEMON_HISTORY_FILE_BACKEND" == "1" ]]; then
  log "daemon history file backend: $DAEMON_HISTORY_BACKEND_DIR"
  DAEMON_HISTORY_FILE_BACKEND_ENV="$DAEMON_HISTORY_BACKEND_DIR"
  DAEMON_ENV_PREFIX+=(TERMX_DAEMON_HISTORY_FILE_BACKEND_DIR="$DAEMON_HISTORY_FILE_BACKEND_ENV")
else
  log "daemon default history file backend: $DAEMON_DEFAULT_HISTORY_BACKEND_DIR"
fi
if [[ -n "$DAEMON_MEMORY_LIMIT_MB" ]]; then
  log "daemon memory limit: ${DAEMON_MEMORY_LIMIT_MB}MB"
  env "${DAEMON_ENV_PREFIX[@]}" TERMX_DAEMON_MEMORY_LIMIT_MB="$DAEMON_MEMORY_LIMIT_MB" TERMX_DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB="$DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB" TERMX_DAEMON_HEAP_PROFILE_DIR="$DAEMON_HEAP_DIR" TERMX_DAEMON_MEMSTATS_DIR="$DAEMON_MEMSTATS_DIR" TERMX_DIAG_STAGE_FILE="$DIAG_STAGE_FILE" "$BIN" --socket "$SOCK" --log-file "$LOG" daemon >"$ROOT/daemon.stdout" 2>"$ROOT/daemon.stderr" &
else
  if [[ -n "$DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB" ]]; then
    log "daemon request reclaim min heap: ${DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB}MB"
  fi
  env "${DAEMON_ENV_PREFIX[@]}" TERMX_DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB="$DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB" TERMX_DAEMON_HEAP_PROFILE_DIR="$DAEMON_HEAP_DIR" TERMX_DAEMON_MEMSTATS_DIR="$DAEMON_MEMSTATS_DIR" TERMX_DIAG_STAGE_FILE="$DIAG_STAGE_FILE" "$BIN" --socket "$SOCK" --log-file "$LOG" daemon >"$ROOT/daemon.stdout" 2>"$ROOT/daemon.stderr" &
fi
DAEMON_PID=$!
if ! wait_for_socket "$SOCK" "$WAIT_SECONDS"; then
  echo "daemon socket did not become ready: $SOCK" >&2
  exit 1
fi
record_stage "daemon_idle" "daemon" "$DAEMON_PID"
maybe_capture_stage_profile "daemon_idle" "daemon" "$DAEMON_PID"

log "creating interactive shell terminal"
TERMINAL_ID="$("$BIN" --socket "$SOCK" --log-file "$LOG" v3 new --name stress-shell -- /bin/sh -l)"
printf '%s\n' "$TERMINAL_ID" >"$TERMINAL_ID_FILE"

ATTACH_SCRIPT="$ROOT/attach.sh"
cat >"$ATTACH_SCRIPT" <<EOF
#!/bin/sh
echo "\$\$" > $(shell_quote "$ATTACH_PID_FILE")
export TERM=xterm-256color
export TERMX_ALLOW_NESTED=1
export TERMX_TUI_HEAP_PROFILE_MODE=$(shell_quote "$PROFILE_MODE")
export TERMX_TUI_HEAP_PROFILE_DIR=$(shell_quote "$TUI_HEAP_DIR")
export TERMX_TUI_MEMSTATS_DIR=$(shell_quote "$TUI_MEMSTATS_DIR")
export TERMX_DIAG_STAGE_FILE=$(shell_quote "$DIAG_STAGE_FILE")
EOF
if [[ "$PROFILE_MODE" == "all" ]]; then
  cat >>"$ATTACH_SCRIPT" <<EOF
export TERMX_TUI_HEAP_PROFILE_EVERY_MB=8
export TERMX_TUI_DIAG=1
export TERMX_TUI_DIAG_INTERVAL_MS=200
EOF
fi
if [[ -n "$TUI_MEMORY_LIMIT_MB" ]]; then
  log "TUI memory limit: ${TUI_MEMORY_LIMIT_MB}MB"
  printf 'export TERMX_TUI_MEMORY_LIMIT_MB=%s\n' "$(shell_quote "$TUI_MEMORY_LIMIT_MB")" >>"$ATTACH_SCRIPT"
fi
cat >>"$ATTACH_SCRIPT" <<EOF
exec $(shell_quote "$BIN") --socket $(shell_quote "$SOCK") --log-file $(shell_quote "$LOG") v3 attach $(shell_quote "$TERMINAL_ID")
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
sleep 1
record_stage "tui_idle" "tui" "$TUI_PID"
maybe_capture_stage_profile "tui_idle" "tui" "$TUI_PID"

DONE_MARKER="TERM_X_TUI_STRESS_DONE"
TIME_FILE="$ROOT/stress-time.txt"
STRESS_CMD="/usr/bin/time -p python3 $(shell_quote "$STRESS_SCRIPT") --lines $LINES --seed $SEED --width-hint $WIDTH_HINT 2>$(shell_quote "$TIME_FILE"); printf '\\n$DONE_MARKER\\n'"

log "typing stress command in TUI: lines=$LINES seed=$SEED"
SAMPLE_STOP_FILE="$ROOT/.stress-sampling-done"
rm -f "$SAMPLE_STOP_FILE"
sample_processes_until_stopped "$SAMPLE_STOP_FILE" 0.2 "$DAEMON_PID" "$TUI_PID" &
SAMPLE_PID=$!
tmux send-keys -t "$TARGET" "$STRESS_CMD" Enter
if ! wait_for_capture "$TARGET" "$DONE_MARKER" "$WAIT_SECONDS"; then
  touch "$SAMPLE_STOP_FILE"
  wait "$SAMPLE_PID" 2>/dev/null || true
  capture_pane "$TARGET" "live-timeout"
  echo "stress command did not reach done marker within ${WAIT_SECONDS}s" >&2
  exit 1
fi
touch "$SAMPLE_STOP_FILE"
wait "$SAMPLE_PID" 2>/dev/null || true
write_memory_peaks
capture_pane "$TARGET" "live"
record_stage "daemon_after_stress" "daemon" "$DAEMON_PID"
maybe_capture_stage_profile "daemon_after_stress" "daemon" "$DAEMON_PID"
record_stage "tui_after_stress" "tui" "$TUI_PID"
maybe_capture_stage_profile "tui_after_stress" "tui" "$TUI_PID"

log "entering copy/history latest"
tmux send-keys -t "$TARGET" C-v
sleep 3
capture_pane "$TARGET" "copy-latest"
record_stage "daemon_copy_latest" "daemon" "$DAEMON_PID"
maybe_capture_stage_profile "daemon_copy_latest" "daemon" "$DAEMON_PID"
record_stage "tui_copy_latest" "tui" "$TUI_PID"
maybe_capture_stage_profile "tui_copy_latest" "tui" "$TUI_PID"

log "jumping to oldest history page"
tmux send-keys -t "$TARGET" g
sleep 4
capture_pane "$TARGET" "copy-oldest"
record_stage "daemon_copy_oldest" "daemon" "$DAEMON_PID"
record_stage "tui_copy_oldest" "tui" "$TUI_PID"
if [[ "$PROFILE_MODE" == "final" ]]; then
  capture_heap_profile "copy_oldest_final" "daemon" "$DAEMON_PID"
  capture_heap_profile "copy_oldest_final" "tui" "$TUI_PID"
fi

if ! LC_ALL=C grep -Fq "000000" "$ROOT/copy-oldest.txt"; then
  echo "copy-oldest did not show oldest stress line 000000" >&2
  exit 1
fi

if [[ -s "$TIME_FILE" ]]; then
  cp "$TIME_FILE" "$ROOT/stress-time.raw.txt"
fi
write_profile_summary

log "RSS report"
if command -v column >/dev/null 2>&1; then
  column -t -s $'\t' "$REPORT"
else
  cat "$REPORT"
fi
if [[ -s "$ROOT/baseline-time.txt" ]]; then
  log "outside-termx baseline time"
  cat "$ROOT/baseline-time.txt"
fi
if [[ -s "$TIME_FILE" ]]; then
  log "stress time"
  cat "$TIME_FILE"
fi
if [[ -s "$ROOT/memory-peaks.tsv" ]]; then
  log "stress RSS peaks"
  if command -v column >/dev/null 2>&1; then
    column -t -s $'\t' "$ROOT/memory-peaks.tsv"
  else
    cat "$ROOT/memory-peaks.tsv"
  fi
fi
if [[ -s "$ROOT/profile-summary.txt" ]]; then
  log "profile summary"
  sed -n '1,80p' "$ROOT/profile-summary.txt"
fi
log "artifacts kept at: $ROOT"
