#!/usr/bin/env python3
"""Emit controlled terminal output for Muxvia live -> copy/history smoke tests."""

from __future__ import annotations

import sys


RESET = "\x1b[0m"


def bg(index: int) -> str:
    return f"\x1b[48;5;{index}m"


def emit_supported_semantics(prefix: str, bg_index: int) -> None:
    sys.stdout.write(f"{prefix}_BEGIN\n")
    sys.stdout.write(f"{bg(bg_index)}{prefix}_EL_TO_EOL\x1b[K{RESET}\n")
    sys.stdout.write(f"{bg(bg_index + 10)}{prefix}_EXPLICIT_SPACES    {RESET}\n")
    sys.stdout.write(f"{prefix}_CR_OLD_TRAIL\r{prefix}_CR_FINAL\x1b[K\n")
    sys.stdout.write(f"{prefix}_GAP\x1b[3CX\n")
    sys.stdout.write(f"{prefix}_T\tX\n")
    sys.stdout.write(f"{prefix}_SUGGEST\x1b[90m_TMP\x1b[0m\x1b[4D\x1b[K\n")


def main() -> int:
    emit_supported_semantics("HSEM_COMMITTED", 24)
    for index in range(48):
        sys.stdout.write(f"HSEM_FILLER_{index:02d} force committed boundary\n")
    emit_supported_semantics("HSEM_LIVE", 25)
    sys.stdout.write("HSEM_READY\n")
    sys.stdout.flush()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
