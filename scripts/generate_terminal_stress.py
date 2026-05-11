#!/usr/bin/env python3
"""Emit a large amount of styled terminal output for scrollback/render testing."""

from __future__ import annotations

import argparse
import random
import sys
import time


RESET = "\x1b[0m"

FG_256 = [
    33, 39, 45, 51, 69, 75, 81, 87, 111, 117, 123, 129, 135, 141, 147,
    153, 159, 165, 171, 177, 183, 189, 195, 201, 202, 203, 204, 205, 206,
    207, 208, 209, 210, 214, 215, 216, 220, 221, 222, 223, 224, 225, 229,
]
BG_256 = [17, 18, 19, 22, 23, 24, 52, 53, 54, 55, 56, 57, 88, 89, 90, 91]
LEVELS = ["TRACE", "DEBUG", "INFO", "WARN", "ERROR", "NOTICE"]
PREFIXES = [
    "auth", "cache", "conn", "disk", "exec", "http", "input", "local",
    "mux", "panel", "pty", "queue", "relay", "render", "resize", "rtc",
    "scroll", "screen", "shell", "state", "stream", "sync", "term", "ui",
]
SUFFIXES = [
    "accepted", "blocked", "buffered", "cached", "closed", "completed",
    "decoded", "deferred", "drained", "flushed", "loaded", "merged",
    "pending", "primed", "replayed", "retried", "settled", "skipped",
    "stalled", "streamed", "trimmed", "updated", "verified", "wrapped",
]
WORDS = [
    "alpha", "amber", "atlas", "batch", "beacon", "binary", "bridge", "burst",
    "cinder", "clip", "cursor", "delta", "ember", "falcon", "frame", "fuse",
    "gamma", "glint", "grid", "halo", "hazel", "helix", "ivy", "jitter",
    "knurl", "laser", "ledger", "lumen", "matrix", "mosaic", "nova", "onyx",
    "packet", "plume", "quartz", "quill", "radar", "ripple", "signal",
    "socket", "static", "throttle", "toggle", "vector", "velvet", "violet",
    "watch", "whisper", "window", "zebra",
]


def ansi_style(rng: random.Random) -> str:
    parts: list[str] = []
    if rng.random() < 0.65:
        parts.append(f"\x1b[38;5;{rng.choice(FG_256)}m")
    if rng.random() < 0.25:
        parts.append(f"\x1b[48;5;{rng.choice(BG_256)}m")
    if rng.random() < 0.35:
        parts.append("\x1b[1m")
    if rng.random() < 0.18:
        parts.append("\x1b[4m")
    if rng.random() < 0.08:
        parts.append("\x1b[7m")
    return "".join(parts)


def segment(rng: random.Random, size: int) -> str:
    words = [rng.choice(WORDS) for _ in range(size)]
    return "-".join(words)


def payload(rng: random.Random, line_no: int, width_hint: int) -> str:
    cells = [
        f"id={line_no:06d}",
        f"lat={rng.randint(1, 999):03d}ms",
        f"q={rng.randint(0, 8192)}",
        f"bytes={rng.randint(64, 65535)}",
        f"mode={rng.choice(['raw', 'screen', 'alt', 'follow', 'owner'])}",
        f"cursor={rng.randint(0, 220)}:{rng.randint(0, 120)}",
        f"rev={rng.randint(1, 4096)}",
    ]
    extra_chunks = rng.randint(3, 8)
    for _ in range(extra_chunks):
        kind = rng.random()
        if kind < 0.25:
            cells.append(segment(rng, rng.randint(2, 5)))
        elif kind < 0.5:
            cells.append(f"flag={rng.choice(['keep', 'drop', 'sync', 'cold', 'fast', 'wide'])}")
        elif kind < 0.75:
            cells.append(f"hash={rng.getrandbits(32):08x}")
        else:
            cells.append(f"path=/var/tmp/{segment(rng, 2)}/{segment(rng, 3)}")

    if rng.random() < 0.18:
        cells.append("bar=" + ("#" * rng.randint(12, 40)))
    if rng.random() < 0.12:
        cells.append("wrap=" + ("=" * max(8, width_hint // 3)))

    return " ".join(cells)


def build_line(rng: random.Random, line_no: int, width_hint: int) -> str:
    left = f"{line_no:06d}"
    level = rng.choice(LEVELS)
    prefix = rng.choice(PREFIXES)
    suffix = rng.choice(SUFFIXES)
    left_style = ansi_style(rng)
    level_style = ansi_style(rng)
    prefix_style = ansi_style(rng)
    suffix_style = ansi_style(rng)
    body_style = ansi_style(rng)

    leader = (
        f"{left_style}{left}{RESET} "
        f"{level_style}[{level:<6}]{RESET} "
        f"{prefix_style}{prefix:<8}{RESET} "
        f"{suffix_style}{suffix:<9}{RESET}"
    )
    body = payload(rng, line_no, width_hint)
    trailer = ""
    if rng.random() < 0.35:
        trailer = " " + ansi_style(rng) + f"<{segment(rng, rng.randint(1, 3))}>" + RESET
    return f"{leader} {body_style}{body}{RESET}{trailer}"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Print many styled lines for terminal stress testing.")
    parser.add_argument("--lines", type=int, default=100_000, help="number of lines to print")
    parser.add_argument("--seed", type=int, default=None, help="random seed; defaults to current time")
    parser.add_argument("--width-hint", type=int, default=120, help="influences long wrapped payload frequency")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    seed = args.seed if args.seed is not None else int(time.time() * 1000) & 0xFFFFFFFF
    rng = random.Random(seed)

    sys.stdout.write(f"{ansi_style(rng)}000000 [INFO  ] stress   boot      seed={seed} lines={args.lines}{RESET}\n")
    for line_no in range(1, args.lines + 1):
        sys.stdout.write(build_line(rng, line_no, args.width_hint))
        sys.stdout.write("\n")
    sys.stdout.flush()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
