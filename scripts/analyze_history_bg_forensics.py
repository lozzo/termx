#!/usr/bin/env python3
"""Compare live/copy raw tmux captures for trailing background differences."""

from __future__ import annotations

import argparse
import re
from collections import Counter
from dataclasses import dataclass
from pathlib import Path


LABEL_RE = re.compile(r"(?<!\d)(\d{6})\s+\[[A-Z ]{6}\]")


@dataclass(frozen=True)
class Style:
    fg: str | None = None
    bg: str | None = None
    reverse: bool = False

    def effective_bg(self) -> str | None:
        if self.reverse:
            return self.fg
        return self.bg


@dataclass(frozen=True)
class Cell:
    ch: str
    style: Style


@dataclass
class RowInfo:
    label: str
    plain: str
    bg_cells: int
    tail_bg_spaces: int
    tail_same_as_last_bg_spaces: int
    last_bg: str | None
    tail_bg_counts: Counter[str]


def parse_sgr_params(raw: bytes) -> list[int]:
    if not raw:
        return [0]
    values: list[int] = []
    for part in raw.replace(b":", b";").split(b";"):
        if not part:
            values.append(0)
            continue
        try:
            values.append(int(part))
        except ValueError:
            continue
    return values or [0]


def ansi_color(prefix: str, value: int) -> str:
    return f"{prefix}:{value}"


def apply_sgr(style: Style, params: list[int]) -> Style:
    fg = style.fg
    bg = style.bg
    reverse = style.reverse
    i = 0
    while i < len(params):
        value = params[i]
        if value == 0:
            fg = None
            bg = None
            reverse = False
        elif value == 7:
            reverse = True
        elif value == 27:
            reverse = False
        elif value == 39:
            fg = None
        elif value == 49:
            bg = None
        elif 30 <= value <= 37:
            fg = ansi_color("ansi", value - 30)
        elif 90 <= value <= 97:
            fg = ansi_color("ansi", value - 90 + 8)
        elif 40 <= value <= 47:
            bg = ansi_color("ansi", value - 40)
        elif 100 <= value <= 107:
            bg = ansi_color("ansi", value - 100 + 8)
        elif value in (38, 48) and i + 2 < len(params) and params[i + 1] == 5:
            color = f"idx:{params[i + 2]}"
            if value == 38:
                fg = color
            else:
                bg = color
            i += 2
        elif value in (38, 48) and i + 4 < len(params) and params[i + 1] == 2:
            color = f"#{params[i + 2]:02x}{params[i + 3]:02x}{params[i + 4]:02x}"
            if value == 38:
                fg = color
            else:
                bg = color
            i += 4
        i += 1
    return Style(fg=fg, bg=bg, reverse=reverse)


def decode_one(data: bytes, start: int) -> tuple[str, int]:
    for size in range(1, 5):
        part = data[start : start + size]
        if not part:
            break
        try:
            return part.decode("utf-8"), start + size
        except UnicodeDecodeError:
            continue
    return chr(data[start]), start + 1


def parse_cells(raw: bytes) -> list[Cell]:
    cells: list[Cell] = []
    style = Style()
    i = 0
    while i < len(raw):
        if raw[i] == 0x1B and i + 1 < len(raw) and raw[i + 1] == ord("["):
            end = i + 2
            while end < len(raw) and not (0x40 <= raw[end] <= 0x7E):
                end += 1
            if end >= len(raw):
                break
            if raw[end] == ord("m"):
                style = apply_sgr(style, parse_sgr_params(raw[i + 2 : end]))
            i = end + 1
            continue
        ch, i = decode_one(raw, i)
        if ch not in ("\r", "\n"):
            cells.append(Cell(ch=ch, style=style))
    return cells


def content_cells(row: list[Cell]) -> list[Cell]:
    border_positions = [index for index, cell in enumerate(row) if cell.ch == "│"]
    if len(border_positions) >= 2:
        return row[border_positions[0] + 1 : border_positions[-1]]
    return row


def summarize_capture(path: Path) -> dict[str, RowInfo]:
    rows: dict[str, RowInfo] = {}
    for raw_line in path.read_bytes().splitlines():
        content = content_cells(parse_cells(raw_line))
        plain = "".join(cell.ch for cell in content)
        match = LABEL_RE.search(plain)
        if not match:
            continue
        label = match.group(1)
        last_non_space = -1
        for index, cell in enumerate(content):
            if cell.ch != " ":
                last_non_space = index
        if last_non_space < 0:
            continue
        tail = content[last_non_space + 1 :]
        last_bg = content[last_non_space].style.effective_bg()
        tail_bg_counts: Counter[str] = Counter(
            cell.style.effective_bg() for cell in tail if cell.ch == " " and cell.style.effective_bg()
        )
        rows[label] = RowInfo(
            label=label,
            plain=plain.rstrip(),
            bg_cells=sum(1 for cell in content if cell.style.effective_bg()),
            tail_bg_spaces=sum(tail_bg_counts.values()),
            tail_same_as_last_bg_spaces=tail_bg_counts.get(last_bg or "", 0),
            last_bg=last_bg,
            tail_bg_counts=tail_bg_counts,
        )
    return rows


def summarize_input(path: Path) -> dict[str, RowInfo]:
    rows: dict[str, RowInfo] = {}
    for raw_line in path.read_bytes().splitlines():
        cells = parse_cells(raw_line)
        plain = "".join(cell.ch for cell in cells)
        match = LABEL_RE.search(plain)
        if not match:
            continue
        label = match.group(1)
        last_non_space = -1
        for index, cell in enumerate(cells):
            if cell.ch != " ":
                last_non_space = index
        tail = cells[last_non_space + 1 :] if last_non_space >= 0 else []
        last_bg = cells[last_non_space].style.effective_bg() if last_non_space >= 0 else None
        tail_bg_counts: Counter[str] = Counter(
            cell.style.effective_bg() for cell in tail if cell.ch == " " and cell.style.effective_bg()
        )
        rows[label] = RowInfo(
            label=label,
            plain=plain.rstrip(),
            bg_cells=sum(1 for cell in cells if cell.style.effective_bg()),
            tail_bg_spaces=sum(tail_bg_counts.values()),
            tail_same_as_last_bg_spaces=tail_bg_counts.get(last_bg or "", 0),
            last_bg=last_bg,
            tail_bg_counts=tail_bg_counts,
        )
    return rows


def counts_text(counter: Counter[str]) -> str:
    if not counter:
        return "{}"
    return "{" + ", ".join(f"{key}:{value}" for key, value in counter.most_common()) + "}"


def short_plain(value: str, limit: int = 160) -> str:
    if len(value) <= limit:
        return value
    return value[: limit - 1] + "…"


def write_report(
    report: Path,
    source_rows: dict[str, RowInfo],
    live_rows: dict[str, RowInfo],
    copy_rows: dict[str, RowInfo],
    min_live_bg_spaces: int,
) -> bool:
    common = sorted(set(live_rows) & set(copy_rows))
    candidates: list[tuple[str, RowInfo, RowInfo]] = []
    for label in common:
        live = live_rows[label]
        copy = copy_rows[label]
        tail_lost = live.tail_bg_spaces >= min_live_bg_spaces and copy.tail_bg_spaces < live.tail_bg_spaces
        row_lost = live.bg_cells >= min_live_bg_spaces and copy.bg_cells + min_live_bg_spaces <= live.bg_cells
        if tail_lost or row_lost:
            candidates.append((label, live, copy))

    reproduced = bool(candidates)
    common_with_live_bg = sum(1 for label in common if live_rows[label].bg_cells > 0)
    common_with_copy_bg = sum(1 for label in common if copy_rows[label].bg_cells > 0)
    common_bg_mismatch = sum(
        1
        for label in common
        if live_rows[label].bg_cells != copy_rows[label].bg_cells
        or live_rows[label].tail_bg_spaces != copy_rows[label].tail_bg_spaces
    )
    lines: list[str] = [
        f"reproduced={'yes' if reproduced else 'no'}",
        f"input_rows={len(source_rows)}",
        f"live_rows={len(live_rows)}",
        f"copy_rows={len(copy_rows)}",
        f"common_rows={len(common)}",
        f"common_rows_with_live_bg={common_with_live_bg}",
        f"common_rows_with_copy_bg={common_with_copy_bg}",
        f"common_bg_mismatch={common_bg_mismatch}",
        f"diff_candidates={len(candidates)}",
        "",
    ]

    if not common:
        lines.append("error=no common stress labels between live and copy captures")
    elif candidates:
        lines.append("examples=live has more styled background cells than copy")
        for label, live, copy in candidates[:8]:
            source = source_rows.get(label)
            lines.append(f"- label={label}")
            if source is not None:
                lines.append(
                    "  input: "
                    f"bg_cells={source.bg_cells} tail_bg_spaces={source.tail_bg_spaces} "
                    f"last_bg={source.last_bg} tail_bg={counts_text(source.tail_bg_counts)} "
                    f"text={short_plain(source.plain)!r}"
                )
            lines.append(
                "  live:  "
                f"bg_cells={live.bg_cells} tail_bg_spaces={live.tail_bg_spaces} "
                f"same_as_last={live.tail_same_as_last_bg_spaces} "
                f"last_bg={live.last_bg} tail_bg={counts_text(live.tail_bg_counts)} "
                f"text={short_plain(live.plain)!r}"
            )
            lines.append(
                "  copy:  "
                f"bg_cells={copy.bg_cells} tail_bg_spaces={copy.tail_bg_spaces} "
                f"same_as_last={copy.tail_same_as_last_bg_spaces} "
                f"last_bg={copy.last_bg} tail_bg={counts_text(copy.tail_bg_counts)} "
                f"text={short_plain(copy.plain)!r}"
            )
    else:
        lines.append("examples=none")
        for label in common[-8:]:
            source = source_rows.get(label)
            live = live_rows[label]
            copy = copy_rows[label]
            lines.append(
                f"- label={label} "
                f"input_bg={source.bg_cells if source else 'n/a'} "
                f"input_tail_bg={source.tail_bg_spaces if source else 'n/a'} "
                f"live_bg={live.bg_cells} copy_bg={copy.bg_cells} "
                f"live_tail_bg={live.tail_bg_spaces} copy_tail_bg={copy.tail_bg_spaces} "
                f"live_tail={counts_text(live.tail_bg_counts)} copy_tail={counts_text(copy.tail_bg_counts)}"
            )

    report.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return reproduced


def main() -> int:
    parser = argparse.ArgumentParser(description="Compare live and copy raw captures for background loss.")
    parser.add_argument("--input", required=True, type=Path)
    parser.add_argument("--live", required=True, type=Path)
    parser.add_argument("--copy", required=True, type=Path)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--min-live-bg-spaces", type=int, default=4)
    args = parser.parse_args()

    source_rows = summarize_input(args.input)
    live_rows = summarize_capture(args.live)
    copy_rows = summarize_capture(args.copy)
    write_report(args.report, source_rows, live_rows, copy_rows, args.min_live_bg_spaces)
    if not (set(live_rows) & set(copy_rows)):
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
