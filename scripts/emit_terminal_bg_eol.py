#!/usr/bin/env python3
"""Emit minimal ANSI cases for background-to-EOL terminal semantics."""

from __future__ import annotations

import sys


RESET = "\x1b[0m"


def bg(index: int) -> str:
    return f"\x1b[48;5;{index}m"


def main() -> int:
    # Different background colors keep tmux raw dumps from merging adjacent cases.
    sys.stdout.write(f"{bg(52)}RESET_BEFORE_NL{RESET}\n")
    sys.stdout.write(f"{bg(53)}NO_RESET_BEFORE_NL\n{RESET}")
    sys.stdout.write(f"{bg(24)}EL_TO_EOL\x1b[K{RESET}\n")
    sys.stdout.write(f"{bg(55)}EXPLICIT    {RESET}\n")
    sys.stdout.flush()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
