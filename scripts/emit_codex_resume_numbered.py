#!/usr/bin/env python3
"""Emit numbered primary-screen redraws that resemble a Codex /resume flow."""

from __future__ import annotations

import argparse
import sys
import time
from pathlib import Path


ESC = "\x1b"
CSI = f"{ESC}["
SYNC_ON = f"{ESC}[?2026h"
SYNC_OFF = f"{ESC}[?2026l"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Print deterministic numbered lines with ANSI clear/redraw controls. "
            "Run it inside the target terminal/PTY, then inspect history for missing "
            "Sxx line numbers."
        )
    )
    parser.add_argument("--lines", type=int, default=100, help="numbered lines per session")
    parser.add_argument("--sessions", type=int, default=3, help="session frames to emit")
    parser.add_argument(
        "--initial",
        choices=("stream", "frame"),
        default="stream",
        help="emit the first session as ordinary output or as a clear/redraw frame",
    )
    parser.add_argument(
        "--clear",
        choices=("ed2", "ed3", "ris", "none"),
        default="ed2",
        help="clear primitive used before resumed frames",
    )
    parser.add_argument(
        "--redraw-mode",
        choices=("all", "viewport"),
        default="all",
        help="all emits the whole resumed transcript; viewport emits only the tail",
    )
    parser.add_argument(
        "--viewport-lines",
        type=int,
        default=50,
        help="tail line count used when --redraw-mode=viewport",
    )
    parser.add_argument(
        "--sync",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="wrap redraw phases in synchronized output mode 2026",
    )
    parser.add_argument(
        "--markers",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="print phase boundary marker lines",
    )
    parser.add_argument("--phase-delay", type=float, default=0.6, help="seconds between phases")
    parser.add_argument("--line-delay", type=float, default=0.0, help="seconds between lines")
    parser.add_argument(
        "--cjk-every",
        type=int,
        default=0,
        help="append a no-space Chinese marker every N lines; 0 disables it",
    )
    parser.add_argument(
        "--manifest-out",
        type=Path,
        default=None,
        help="optional path for the expected Sxx line list",
    )
    return parser.parse_args()


def emit(text: str = "", *, flush: bool = False) -> None:
    sys.stdout.write(text)
    if flush:
        sys.stdout.flush()


def clear_sequence(kind: str) -> str:
    if kind == "ed2":
        return f"{CSI}2J{CSI}H"
    if kind == "ed3":
        return f"{CSI}3J{CSI}2J{CSI}H"
    if kind == "ris":
        return f"{ESC}c{CSI}H"
    if kind == "none":
        return f"{CSI}H"
    raise ValueError(f"unsupported clear kind: {kind}")


def marker(session_no: int, label: str, args: argparse.Namespace) -> str:
    return (
        f"=== {label} S{session_no:02d} lines={args.lines} "
        f"clear={args.clear} sync={int(args.sync)} ==="
    )


def numbered_line(session_no: int, line_no: int, total: int, cjk_every: int) -> str:
    text = f"S{session_no:02d} {line_no:03d}/{total:03d} | seq={line_no:03d}"
    if cjk_every > 0 and line_no % cjk_every == 0:
        text += f" | 中文编号{line_no:03d}中文"
    return text


def maybe_sleep(seconds: float) -> None:
    if seconds > 0:
        time.sleep(seconds)


def emit_lines(session_no: int, args: argparse.Namespace, first_line: int = 1) -> None:
    for line_no in range(first_line, args.lines + 1):
        emit(numbered_line(session_no, line_no, args.lines, args.cjk_every))
        emit("\n", flush=args.line_delay > 0)
        maybe_sleep(args.line_delay)
    sys.stdout.flush()


def emit_stream_session(session_no: int, args: argparse.Namespace) -> None:
    if args.markers:
        emit(marker(session_no, "STREAM_BEGIN", args) + "\n")
    emit_lines(session_no, args)
    if args.markers:
        emit(marker(session_no, "STREAM_END", args) + "\n", flush=True)


def emit_redraw_session(session_no: int, args: argparse.Namespace) -> None:
    # 这里的 truth source 是真实写入 PTY 的 ANSI 序列：clear 负责触发 vterm 清屏/scrollback，
    # 后续编号行负责模拟 Codex resume 后把新 session 内容重新画到 primary screen。
    if args.sync:
        emit(SYNC_ON)
    emit(clear_sequence(args.clear), flush=True)
    first_line = 1
    if args.redraw_mode == "viewport":
        first_line = max(1, args.lines - args.viewport_lines + 1)
    if args.markers:
        emit(marker(session_no, "REDRAW_BEGIN", args) + "\n")
    emit_lines(session_no, args, first_line)
    if args.markers:
        emit(marker(session_no, "REDRAW_END", args) + "\n", flush=True)
    if args.sync:
        emit(SYNC_OFF, flush=True)


def write_manifest(args: argparse.Namespace) -> None:
    if args.manifest_out is None:
        return
    lines: list[str] = []
    lines.append(
        f"expected sessions={args.sessions} lines={args.lines} initial={args.initial} "
        f"clear={args.clear} sync={int(args.sync)} redraw_mode={args.redraw_mode}"
    )
    lines.append(
        "ED2 keeps terminal scrollback semantics; ED3 asks the terminal to clear scrollback."
    )
    if args.redraw_mode == "viewport":
        lines.append(
            f"viewport mode intentionally emits only the last {args.viewport_lines} "
            "numbered lines for redraw sessions."
        )
    for session_no in range(1, args.sessions + 1):
        lines.append(f"[S{session_no:02d}]")
        first_line = 1
        if session_no != 1 or args.initial == "frame":
            if args.redraw_mode == "viewport":
                first_line = max(1, args.lines - args.viewport_lines + 1)
        for line_no in range(first_line, args.lines + 1):
            lines.append(numbered_line(session_no, line_no, args.lines, args.cjk_every))
    args.manifest_out.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main() -> int:
    args = parse_args()
    if args.lines <= 0:
        raise SystemExit("--lines must be positive")
    if args.sessions <= 0:
        raise SystemExit("--sessions must be positive")
    if args.viewport_lines <= 0:
        raise SystemExit("--viewport-lines must be positive")

    write_manifest(args)

    for session_no in range(1, args.sessions + 1):
        if session_no == 1 and args.initial == "stream":
            emit_stream_session(session_no, args)
        else:
            emit_redraw_session(session_no, args)
        if session_no != args.sessions:
            maybe_sleep(args.phase_delay)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
