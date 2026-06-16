#!/usr/bin/env python3
"""Emit deterministic background footprint rows for TermX tmux harness."""

from __future__ import annotations

import sys


RESET = "\x1b[0m"


def bg(index: int) -> str:
    return f"\x1b[48;5;{index}m"


def main() -> int:
    sys.stdout.write("000001 [INFO  ] TAIL_NO_BG_SPACES")
    sys.stdout.write("\n")
    sys.stdout.write(f"{bg(24)}000002 [INFO  ] TAIL_WITH_BG_SPACES        {RESET}")
    sys.stdout.write("\n")
    sys.stdout.write("BG_FOOTPRINT_READY\n")
    sys.stdout.flush()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
