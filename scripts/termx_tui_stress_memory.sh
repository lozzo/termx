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
  --repeat N            run the stress script N times in the same terminal; default 1
  --seed N              stress seed; default omitted to match the user command
  --width-hint N        stress width hint; default omitted to use the generator default
  --attach-size CxR     tmux attach size; default 120x36
  --baseline-time       run the same stress script outside termx first (default)
  --no-baseline-time    skip outside-termx baseline timing
  --profile-mode MODE   heap profile capture mode: final, all, or none; default final
  --daemon-memory-limit-mb N
                        set TERMX_DAEMON_MEMORY_LIMIT_MB for daemon runtime GC pacing
  --daemon-request-reclaim-min-heap-mb N
                        set TERMX_DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB for request-boundary page reclaim
  --use-real-state      do not isolate XDG_STATE_HOME; useful for reproducing user default-state RSS
  --tui-memory-limit-mb N
                        set TERMX_TUI_MEMORY_LIMIT_MB for TUI runtime GC pacing
  --wait-seconds N      max wait for attach/copy markers; default 90
  --keep-root           keep artifact root after success/failure (default)
  --cleanup-root        remove artifact root on exit
  -h, --help            show this help

Artifacts:
  memory.tsv            RSS, CPU%, cumulative CPU, idle, and post-profile-GC diagnostic stages
  memory-samples.tsv    daemon/TUI RSS samples collected during stress, grouped by phase
  memory-peaks.tsv      daemon/TUI peak RSS derived from stress samples, grouped by phase
  baseline-time.txt     outside-termx baseline time(1) output when enabled
  stress-time.txt       captured time(1) output from the last TUI stress run
  stress-times.tsv      parsed per-run time(1) output captured from the TUI pane
  stress-latency.tsv    per-run wall-clock latency from tmux send-keys to visible DONE
  stress-commands.tsv   exact stress command typed into the TUI pane
  history-trace.tsv     live/copy captures checked for oldest/newest stress markers
  history-files.tsv     file-backed history payload count/bytes after each stress run
  profile-summary.txt   daemon/TUI pprof top summaries
  profile-graphs/       daemon/TUI pprof DOT graphs and SVG graphs when Graphviz exists
  daemon-memstats/       daemon runtime MemStats samples collected at RSS stages
  tui-memstats/          TUI runtime MemStats samples collected at RSS stages
  state/termx/history-v2/ default daemon file-backed history payloads
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

now_ms() {
  python3 - <<'PY'
import time
print(int(time.time() * 1000))
PY
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
  local found_var="${4:-}"
  local attempt
  for attempt in $(seq 1 "$deadline"); do
    if tmux capture-pane -t "$target" -p -S -4000 2>/dev/null | LC_ALL=C grep -Fq "$pattern"; then
      if [[ -n "$found_var" ]]; then
        printf -v "$found_var" '%s' "$(now_ms)"
      fi
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_for_copy_oldest() {
  local target="$1"
  local deadline="$2"
  local attempt
  # 中文说明：真实 tmux harness 下，进入 copy mode 后第一次普通按键可能和
  # 重绘/输入读取边界竞争；这里循环发送 oldest intent，避免把 harness 时序误判为历史失败。
  for attempt in $(seq 1 "$deadline"); do
    tmux send-keys -t "$target" g
    sleep 1
    capture_pane "$target" "copy-oldest"
    if LC_ALL=C grep -Fq "000000" "$ROOT/copy-oldest.txt"; then
      return 0
    fi
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
  local phase="$5"
  local samples="$ROOT/memory-samples.tsv"
  if [[ ! -s "$samples" ]]; then
    printf 'unix_ms\tphase\tprocess\tpid\trss_kib\trss_mib\tcpu_percent\tcpu_time\n' >"$samples"
  fi
  while [[ ! -f "$stop_file" ]]; do
    sample_process_once "$phase" "daemon" "$daemon_pid" "$samples"
    sample_process_once "$phase" "tui" "$tui_pid" "$samples"
    sleep "$interval"
  done
  sample_process_once "$phase" "daemon" "$daemon_pid" "$samples"
  sample_process_once "$phase" "tui" "$tui_pid" "$samples"
}

sample_process_once() {
  local phase="$1"
  local process="$2"
  local pid="$3"
  local samples="$4"
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
  awk -v unix_ms="$unix_ms" -v phase="$phase" -v process="$process" -v pid="$pid" -v rss="$rss" -v cpu_percent="$cpu_percent" -v cpu_time="$cpu_time" 'BEGIN {
    printf "%s\t%s\t%s\t%s\t%d\t%.1f\t%.1f\t%s\n", unix_ms, phase, process, pid, rss, rss / 1024, cpu_percent, cpu_time
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
      phase=$2
      process=$3
      key=phase "\t" process
      rss=$5 + 0
      if (!(key in peak) || rss > peak[key]) {
        peak[key]=rss
        peak_mib[key]=$6
        peak_cpu_percent[key]=$7
        peak_cpu_time[key]=$8
        peak_unix_ms[key]=$1
      }
    }
    END {
      print "phase\tprocess\tpeak_unix_ms\tpeak_rss_kib\tpeak_rss_mib\tcpu_percent_at_peak\tcpu_time_at_peak"
      for (key in peak) {
        printf "%s\t%s\t%d\t%.1f\t%.1f\t%s\n", key, peak_unix_ms[key], peak[key], peak_mib[key], peak_cpu_percent[key], peak_cpu_time[key]
      }
    }
  ' "$samples" >"$peaks"
}

history_backend_dir() {
  if [[ "$USE_REAL_STATE" == "1" ]]; then
    printf '%s/termx/history-v2\n' "$(real_state_home)"
  else
    printf '%s\n' "$DAEMON_DEFAULT_HISTORY_BACKEND_DIR"
  fi
}

real_state_home() {
  if [[ -n "${XDG_STATE_HOME:-}" ]]; then
    printf '%s' "$XDG_STATE_HOME"
    return 0
  fi
  printf '%s/.local/state' "$HOME"
}

history_backend_stats() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    printf '0\t0\t0.0\n'
    return 0
  fi
  find "$dir" -type f -name '*.history-lines.bin' -print0 2>/dev/null | python3 -c '
import os
import sys

data = sys.stdin.buffer.read().split(b"\0")
files = [path.decode("utf-8", "surrogateescape") for path in data if path]
total = 0
for path in files:
    try:
        total += os.path.getsize(path)
    except OSError:
        pass
print(f"{len(files)}\t{total}\t{total / 1048576:.1f}")
'
}

record_history_backend_size() {
  local stage="$1"
  local dir stats files bytes mib
  dir="$(history_backend_dir)"
  stats="$(history_backend_stats "$dir")"
  IFS=$'\t' read -r files bytes mib <<EOF
$stats
EOF
  printf '%s\t%s\t%s\t%s\t%s\n' "$stage" "$files" "$bytes" "$mib" "$dir" >>"$HISTORY_REPORT"
}

append_stress_time() {
  local run="$1"
  local seed="$2"
  local path="$3"
  local real="" user="" sys="" seed_label
  seed_label="${seed:--}"
  if [[ -s "$path" ]]; then
    real="$(parse_time_field "$path" real)"
    user="$(parse_time_field "$path" user)"
    sys="$(parse_time_field "$path" sys)"
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$run" "$seed_label" "$real" "$user" "$sys" >>"$STRESS_TIMES_REPORT"
}

append_stress_latency() {
  local run="$1"
  local seed="$2"
  local send_ms="$3"
  local done_ms="$4"
  local path="$5"
  local status="${6:-visible}"
  local observed_end_ms="${7:-$done_ms}"
  local real="" observed_ms="" visible_ms="" post_process_ms="" seed_label
  seed_label="${seed:--}"
  if [[ -s "$path" ]]; then
    real="$(parse_time_field "$path" real)"
  fi
  if [[ -n "$send_ms" && -n "$observed_end_ms" ]]; then
    observed_ms=$((observed_end_ms - send_ms))
  fi
  if [[ -n "$send_ms" && -n "$done_ms" ]]; then
    visible_ms=$((done_ms - send_ms))
  fi
  post_process_ms="$(latency_delta_ms "$visible_ms" "$real")"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$run" "$seed_label" "$status" "$send_ms" "$done_ms" "$observed_ms" "$visible_ms" "$real" "$post_process_ms" >>"$STRESS_LATENCY_REPORT"
}

latency_delta_ms() {
  local visible_ms="$1"
  local real_seconds="$2"
  python3 - "$visible_ms" "$real_seconds" <<'PY'
import sys

visible = sys.argv[1]
real = sys.argv[2]
if not visible or not real:
    print("")
    raise SystemExit(0)
try:
    value = int(visible) - int(round(float(real) * 1000))
except ValueError:
    print("")
else:
    print(value)
PY
}

parse_time_field() {
  local path="$1"
  local field="$2"
  awk -v field="$field" '
    $1 == field { print $2; found=1; exit }
    field == "real" && $NF == "total" { print $(NF-1); found=1; exit }
    field == "user" && $NF == "user" { print $(NF-1); found=1; exit }
    field == "sys" && $NF == "system" { print $(NF-1); found=1; exit }
    field == "sys" && $NF == "sys" { print $(NF-1); found=1; exit }
    END { if (!found) print "" }
  ' "$path"
}

stress_script_args() {
  local run_seed="$1"
  printf ' --lines %s' "$LINES"
  if [[ -n "$run_seed" ]]; then
    printf ' --seed %s' "$run_seed"
  fi
  if [[ -n "$WIDTH_HINT" ]]; then
    printf ' --width-hint %s' "$WIDTH_HINT"
  fi
}

stress_command_display() {
  local run_seed="$1"
  printf 'python3 scripts/generate_terminal_stress.py'
  stress_script_args "$run_seed"
}

stress_command_shell() {
  local run_seed="$1"
  printf 'python3 %s' "$(shell_quote "$STRESS_SCRIPT")"
  stress_script_args "$run_seed"
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
    printf '%s\n' "$stage" >"$DIAG_STAGE_FILE"
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

append_history_trace() {
  local stage="$1"
  local path="$2"
  local oldest newest done
  oldest=0
  newest=0
  done=0
  if [[ -s "$path" ]]; then
    if LC_ALL=C grep -Fq "000000" "$path"; then
      oldest=1
    fi
    if LC_ALL=C grep -Fq "$(printf '%06d' "$LINES")" "$path"; then
      newest=1
    fi
    if LC_ALL=C grep -Fq "TERM_X_TUI_STRESS_DONE" "$path"; then
      done=1
    fi
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$stage" "$oldest" "$newest" "$done" "$path" >>"$HISTORY_TRACE_REPORT"
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
    if [[ -s "$ROOT/stress-times.tsv" ]]; then
      echo
      echo "## stress time per run"
      cat "$ROOT/stress-times.tsv"
    fi
    if [[ -s "$ROOT/stress-latency.tsv" ]]; then
      echo
      echo "## stress visible latency per run"
      cat "$ROOT/stress-latency.tsv"
    fi
    if [[ -s "$ROOT/stress-commands.tsv" ]]; then
      echo
      echo "## stress commands"
      cat "$ROOT/stress-commands.tsv"
    fi
    if [[ -s "$ROOT/history-files.tsv" ]]; then
      echo
      echo "## history file sizes"
      cat "$ROOT/history-files.tsv"
    fi
    if [[ -s "$ROOT/history-trace.tsv" ]]; then
      echo
      echo "## history trace"
      cat "$ROOT/history-trace.tsv"
    fi
    if [[ -d "$DAEMON_DEFAULT_HISTORY_BACKEND_DIR" ]]; then
      echo
      echo "## daemon default history file backend"
      find "$DAEMON_DEFAULT_HISTORY_BACKEND_DIR" -type f -name '*.history-lines.bin' -print0 2>/dev/null | xargs -0 ls -lh 2>/dev/null || true
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
REPEAT=1
SEED=""
WIDTH_HINT=""
ATTACH_SIZE="120x36"
WAIT_SECONDS=90
CLEANUP_ROOT=0
DAEMON_MEMORY_LIMIT_MB=""
DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB=""
USE_REAL_STATE=0
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
    --repeat)
      REPEAT="$2"
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
    --use-real-state)
      USE_REAL_STATE=1
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
positive_integer "--repeat" "$REPEAT"
positive_integer "--wait-seconds" "$WAIT_SECONDS"
if [[ -n "$DAEMON_MEMORY_LIMIT_MB" ]]; then
  positive_integer "--daemon-memory-limit-mb" "$DAEMON_MEMORY_LIMIT_MB"
fi
if [[ -n "$SEED" ]]; then
  positive_integer "--seed" "$SEED"
fi
if [[ -n "$WIDTH_HINT" ]]; then
  positive_integer "--width-hint" "$WIDTH_HINT"
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
HISTORY_REPORT="$ROOT/history-files.tsv"
STRESS_TIMES_REPORT="$ROOT/stress-times.tsv"
STRESS_LATENCY_REPORT="$ROOT/stress-latency.tsv"
STRESS_COMMANDS_REPORT="$ROOT/stress-commands.tsv"
HISTORY_TRACE_REPORT="$ROOT/history-trace.tsv"
DAEMON_HEAP_DIR="$ROOT/daemon-heap"
TUI_HEAP_DIR="$ROOT/tui-heap"
DAEMON_MEMSTATS_DIR="$ROOT/daemon-memstats"
TUI_MEMSTATS_DIR="$ROOT/tui-memstats"
DAEMON_DEFAULT_STATE_DIR="$ROOT/state"
DAEMON_DEFAULT_HISTORY_BACKEND_DIR="$DAEMON_DEFAULT_STATE_DIR/termx/history-v2"
DIAG_STAGE_FILE="$ROOT/diag-stage.txt"
SESSION="termx-tui-stress-$$"
TARGET="$SESSION:0.0"
ATTACH_PID_FILE="$ROOT/tui.pid"
TERMINAL_ID_FILE="$ROOT/terminal.id"
DAEMON_PID=""
TERMINAL_ID=""

run_cleanup_command() {
  local timeout="$1"
  shift
  "$@" >/dev/null 2>&1 &
  local pid=$!
  (
    sleep "$timeout"
    kill "$pid" 2>/dev/null || true
    sleep 1
    kill -KILL "$pid" 2>/dev/null || true
  ) &
  local timer_pid=$!
  wait "$pid" 2>/dev/null
  local status=$?
  kill "$timer_pid" 2>/dev/null || true
  wait "$timer_pid" 2>/dev/null || true
  return "$status"
}

cleanup() {
  set +e
  if tmux has-session -t "$SESSION" 2>/dev/null; then
    tmux kill-session -t "$SESSION" 2>/dev/null
  fi
  if [[ -n "$TERMINAL_ID" && -x "$BIN" ]]; then
    # 中文说明：压力报告已经完成后，诊断清理不得因 daemon 忙于历史回收而无限阻塞。
    run_cleanup_command 10 "$BIN" --socket "$SOCK" --log-file "$LOG" v3 kill "$TERMINAL_ID"
    run_cleanup_command 10 "$BIN" --socket "$SOCK" --log-file "$LOG" v3 rm "$TERMINAL_ID"
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
printf 'stage\tfiles\tbytes\tmib\tdir\n' >"$HISTORY_REPORT"
printf 'run\tseed\treal\tuser\tsys\n' >"$STRESS_TIMES_REPORT"
printf 'run\tseed\tstatus\tsend_unix_ms\tdone_visible_unix_ms\tobserved_ms\tvisible_ms\tpython_real_s\tpost_process_visible_ms\n' >"$STRESS_LATENCY_REPORT"
printf 'run\tseed\tcommand\n' >"$STRESS_COMMANDS_REPORT"
printf 'stage\thas_oldest_000000\thas_newest_line\thas_done_marker\tpath\n' >"$HISTORY_TRACE_REPORT"
mkdir -p "$TUI_HEAP_DIR"
mkdir -p "$DAEMON_HEAP_DIR"
mkdir -p "$TUI_MEMSTATS_DIR"
mkdir -p "$DAEMON_MEMSTATS_DIR"
if [[ "$BASELINE_TIME" == "1" ]]; then
  log "running outside-termx stress baseline: $(stress_command_display "$SEED")"
  /usr/bin/time -p "$(command -v python3)" "$STRESS_SCRIPT" $(stress_script_args "$SEED") >"$ROOT/baseline-output.txt" 2>"$ROOT/baseline-time.txt"
  rm -f "$ROOT/baseline-output.txt"
fi

log "starting daemon"
DAEMON_ENV_PREFIX=(TERMX_STRESS_HARNESS=1)
if [[ "$USE_REAL_STATE" == "0" ]]; then
  DAEMON_ENV_PREFIX+=(XDG_STATE_HOME="$DAEMON_DEFAULT_STATE_DIR")
fi
if [[ "$USE_REAL_STATE" == "1" ]]; then
  log "daemon default history file backend: real user state"
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
if [[ "$USE_REAL_STATE" == "0" ]]; then
  printf 'export XDG_STATE_HOME=%s\n' "$(shell_quote "$DAEMON_DEFAULT_STATE_DIR")" >>"$ATTACH_SCRIPT"
fi
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

TIME_FILE=""
for RUN in $(seq 1 "$REPEAT"); do
  RUN_LABEL="$(printf '%02d' "$RUN")"
  RUN_SEED=""
  if [[ -n "$SEED" ]]; then
    RUN_SEED=$((SEED + RUN - 1))
  fi
  DONE_MARKER="TERM_X_TUI_STRESS_DONE_${RUN_LABEL}"
  TIME_FILE="$ROOT/stress-time-run-${RUN_LABEL}.txt"
  STRESS_DISPLAY="$(stress_command_display "$RUN_SEED")"
  STRESS_CMD="/usr/bin/time -p $(stress_command_shell "$RUN_SEED") 2>$(shell_quote "$TIME_FILE"); printf '\\n$DONE_MARKER\\n'"
  printf '%s\t%s\t%s\n' "$RUN_LABEL" "${RUN_SEED:--}" "$STRESS_DISPLAY" >>"$STRESS_COMMANDS_REPORT"

  log "typing stress command in TUI: run=$RUN_LABEL/$REPEAT $STRESS_DISPLAY"
  SAMPLE_STOP_FILE="$ROOT/.stress-sampling-done-${RUN_LABEL}"
  rm -f "$SAMPLE_STOP_FILE"
  sample_processes_until_stopped "$SAMPLE_STOP_FILE" 0.2 "$DAEMON_PID" "$TUI_PID" "stress_run_${RUN_LABEL}" &
  SAMPLE_PID=$!
  # 中文说明：send -> DONE visible 是用户感知 live 延迟；pane 内 time 只量程序本身。
  STRESS_SEND_MS="$(now_ms)"
  STRESS_DONE_MS=""
  tmux send-keys -t "$TARGET" "$STRESS_CMD" Enter
  if ! wait_for_capture "$TARGET" "$DONE_MARKER" "$WAIT_SECONDS" STRESS_DONE_MS; then
    STRESS_TIMEOUT_MS="$(now_ms)"
    touch "$SAMPLE_STOP_FILE"
    wait "$SAMPLE_PID" 2>/dev/null || true
    append_stress_time "$RUN_LABEL" "$RUN_SEED" "$TIME_FILE"
    append_stress_latency "$RUN_LABEL" "$RUN_SEED" "$STRESS_SEND_MS" "" "$TIME_FILE" "timeout" "$STRESS_TIMEOUT_MS"
    capture_pane "$TARGET" "live-timeout-run-${RUN_LABEL}"
    echo "stress command run $RUN_LABEL did not reach done marker within ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  touch "$SAMPLE_STOP_FILE"
  wait "$SAMPLE_PID" 2>/dev/null || true
  append_stress_time "$RUN_LABEL" "$RUN_SEED" "$TIME_FILE"
  append_stress_latency "$RUN_LABEL" "$RUN_SEED" "$STRESS_SEND_MS" "$STRESS_DONE_MS" "$TIME_FILE"
  capture_pane "$TARGET" "live-run-${RUN_LABEL}"
  append_history_trace "live_run_${RUN_LABEL}" "$ROOT/live-run-${RUN_LABEL}.txt"
  record_stage "daemon_after_stress_run_${RUN_LABEL}" "daemon" "$DAEMON_PID"
  maybe_capture_stage_profile "daemon_after_stress_run_${RUN_LABEL}" "daemon" "$DAEMON_PID"
  record_stage "tui_after_stress_run_${RUN_LABEL}" "tui" "$TUI_PID"
  maybe_capture_stage_profile "tui_after_stress_run_${RUN_LABEL}" "tui" "$TUI_PID"
  record_history_backend_size "after_stress_run_${RUN_LABEL}"
done
write_memory_peaks
capture_pane "$TARGET" "live"
append_history_trace "live" "$ROOT/live.txt"
record_stage "daemon_after_stress" "daemon" "$DAEMON_PID"
maybe_capture_stage_profile "daemon_after_stress" "daemon" "$DAEMON_PID"
record_stage "tui_after_stress" "tui" "$TUI_PID"
maybe_capture_stage_profile "tui_after_stress" "tui" "$TUI_PID"

log "entering copy/history latest"
tmux send-keys -t "$TARGET" C-v
sleep 3
capture_pane "$TARGET" "copy-latest"
append_history_trace "copy_latest" "$ROOT/copy-latest.txt"
record_stage "daemon_copy_latest" "daemon" "$DAEMON_PID"
maybe_capture_stage_profile "daemon_copy_latest" "daemon" "$DAEMON_PID"
record_stage "tui_copy_latest" "tui" "$TUI_PID"
maybe_capture_stage_profile "tui_copy_latest" "tui" "$TUI_PID"

log "jumping to oldest history page"
if ! wait_for_copy_oldest "$TARGET" "$WAIT_SECONDS"; then
  echo "copy-oldest did not show oldest stress line 000000" >&2
  exit 1
fi
append_history_trace "copy_oldest" "$ROOT/copy-oldest.txt"
record_stage "daemon_copy_oldest" "daemon" "$DAEMON_PID"
record_stage "tui_copy_oldest" "tui" "$TUI_PID"
sleep 2
record_stage "daemon_copy_oldest_idle" "daemon" "$DAEMON_PID"
record_stage "tui_copy_oldest_idle" "tui" "$TUI_PID"
if [[ "$PROFILE_MODE" == "final" ]]; then
  capture_heap_profile "copy_oldest_final" "daemon" "$DAEMON_PID"
  capture_heap_profile "copy_oldest_final" "tui" "$TUI_PID"
  sleep 1
  record_stage "daemon_after_profile_gc" "daemon" "$DAEMON_PID"
  record_stage "tui_after_profile_gc" "tui" "$TUI_PID"
fi

if [[ -s "$TIME_FILE" ]]; then
  cp "$TIME_FILE" "$ROOT/stress-time.txt"
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
  log "last stress time"
  cat "$TIME_FILE"
fi
if [[ -s "$ROOT/stress-times.tsv" ]]; then
  log "stress times by run"
  if command -v column >/dev/null 2>&1; then
    column -t -s $'\t' "$ROOT/stress-times.tsv"
  else
    cat "$ROOT/stress-times.tsv"
  fi
fi
if [[ -s "$ROOT/stress-latency.tsv" ]]; then
  log "stress visible latency by run"
  if command -v column >/dev/null 2>&1; then
    column -t -s $'\t' "$ROOT/stress-latency.tsv"
  else
    cat "$ROOT/stress-latency.tsv"
  fi
fi
if [[ -s "$ROOT/stress-commands.tsv" ]]; then
  log "stress commands"
  if command -v column >/dev/null 2>&1; then
    column -t -s $'\t' "$ROOT/stress-commands.tsv"
  else
    cat "$ROOT/stress-commands.tsv"
  fi
fi
if [[ -s "$ROOT/memory-peaks.tsv" ]]; then
  log "stress RSS peaks"
  if command -v column >/dev/null 2>&1; then
    column -t -s $'\t' "$ROOT/memory-peaks.tsv"
  else
    cat "$ROOT/memory-peaks.tsv"
  fi
fi
if [[ -s "$ROOT/history-files.tsv" ]]; then
  log "history file sizes"
  if command -v column >/dev/null 2>&1; then
    column -t -s $'\t' "$ROOT/history-files.tsv"
  else
    cat "$ROOT/history-files.tsv"
  fi
fi
if [[ -s "$ROOT/history-trace.tsv" ]]; then
  log "history trace"
  if command -v column >/dev/null 2>&1; then
    column -t -s $'\t' "$ROOT/history-trace.tsv"
  else
    cat "$ROOT/history-trace.tsv"
  fi
fi
if [[ -s "$ROOT/profile-summary.txt" ]]; then
  log "profile summary"
  sed -n '1,80p' "$ROOT/profile-summary.txt"
fi
log "artifacts kept at: $ROOT"
