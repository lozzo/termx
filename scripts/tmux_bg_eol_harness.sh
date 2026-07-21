#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/muxvia-bg-eol.XXXXXX")}"
SESSION="muxvia-bg-eol-$$"

cleanup() {
  tmux kill-session -t "$SESSION" >/dev/null 2>&1 || true
}
trap cleanup EXIT

tmux new-session -d -s "$SESSION" -x 32 -y 8 \
  "python3 '$ROOT/scripts/emit_terminal_bg_eol.py'; sleep 2"
sleep 0.3

tmux capture-pane -t "$SESSION:0.0" -epN >"$ARTIFACT_DIR/raw.ansi"
tmux capture-pane -t "$SESSION:0.0" -pN >"$ARTIFACT_DIR/plain.txt"

python3 - "$ARTIFACT_DIR/raw.ansi" <<'PY'
from __future__ import annotations

import sys


raw_path = sys.argv[1]
lines = open(raw_path, "rb").read().splitlines()


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(message)


require(len(lines) >= 4, f"expected at least 4 captured lines, got {len(lines)}")
reset_before, no_reset, erase_to_eol, explicit = lines[:4]

require(
    b"\x1b[48;5;52mRESET_BEFORE_NL\x1b[49m " in reset_before,
    f"reset-before-newline should reset bg before trailing blanks: {reset_before!r}",
)
require(
    b"\x1b[48;5;53mNO_RESET_BEFORE_NL\x1b[49m" in no_reset,
    f"newline without erase should not keep bg across trailing blanks: {no_reset!r}",
)
require(
    erase_to_eol.startswith(b"\x1b[48;5;24mEL_TO_EOL"),
    f"erase-to-EOL line should start with its bg: {erase_to_eol!r}",
)
require(
    b"\x1b[49m" not in erase_to_eol,
    f"erase-to-EOL styled blanks should keep bg until row end: {erase_to_eol!r}",
)
require(
    b"\x1b[48;5;55mEXPLICIT    \x1b[49m " in explicit,
    f"explicit blanks should keep bg only for emitted blanks: {explicit!r}",
)
PY

printf 'tmux bg-eol harness passed: %s\n' "$ARTIFACT_DIR"
