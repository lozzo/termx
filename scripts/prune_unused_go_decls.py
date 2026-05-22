#!/usr/bin/env python3
import argparse
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


U1000_RE = re.compile(
    r"^(?P<path>[^:\n]+\.go):(?P<line>\d+):\d+: (?P<kind>func|const|var|type) (?P<name>.+?) is unused \(U1000\)$"
)


@dataclass
class UnusedDecl:
    path: Path
    line: int
    kind: str
    name: str


def run_staticcheck(packages: list[str]) -> list[UnusedDecl]:
    cmd = ["/Users/lozzow/.go/bin/staticcheck", *packages]
    proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, check=False)
    decls: list[UnusedDecl] = []
    for raw in proc.stdout.splitlines():
        match = U1000_RE.match(raw.strip())
        if not match:
            continue
        decls.append(
            UnusedDecl(
                path=Path(match.group("path")),
                line=int(match.group("line")),
                kind=match.group("kind"),
                name=match.group("name"),
            )
        )
    return decls


def block_end(lines: list[str], start: int) -> int:
    depth = 0
    saw_open = False
    for i in range(start, len(lines)):
        line = lines[i]
        depth += line.count("{")
        if line.count("{") > 0:
            saw_open = True
        depth -= line.count("}")
        if saw_open and depth <= 0:
            return i + 1
    return len(lines)


def delete_decl(path: Path, line_no: int) -> bool:
    text = path.read_text()
    lines = text.splitlines(keepends=True)
    idx = line_no - 1
    if idx < 0 or idx >= len(lines):
        return False

    start = idx
    while start > 0 and lines[start - 1].strip().startswith("//"):
        start -= 1

    line = lines[idx]
    stripped = line.lstrip()
    if stripped.startswith(("func ", "type ", "const ", "var ")):
        if stripped.startswith("func ") or stripped.startswith("type "):
            end = block_end(lines, idx)
        else:
            end = idx + 1
            if end < len(lines) and lines[idx].rstrip().endswith("("):
                while end < len(lines):
                    if lines[end].strip() == ")":
                        end += 1
                        break
                    end += 1
        del lines[start:end]
    else:
        return False

    while start < len(lines) and lines[start].strip() == "":
        if start == 0 or lines[start - 1].strip() == "":
            del lines[start]
        else:
            break

    path.write_text("".join(lines))
    return True


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("packages", nargs="+")
    parser.add_argument("--max-rounds", type=int, default=8)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()

    total_deleted = 0
    for _ in range(args.max_rounds):
        decls = run_staticcheck(args.packages)
        if not decls:
            print("no U1000 declarations")
            return 0
        if not args.apply:
            for decl in decls:
                print(f"candidate {decl.kind} {decl.name} at {decl.path}:{decl.line}")
            print("dry-run only; rerun with --apply to delete")
            return 0
        deleted_this_round = 0
        seen = set()
        for decl in decls:
            key = (decl.path, decl.line)
            if key in seen:
                continue
            seen.add(key)
            if delete_decl(decl.path, decl.line):
                print(f"deleted {decl.kind} {decl.name} at {decl.path}:{decl.line}")
                deleted_this_round += 1
        total_deleted += deleted_this_round
        if deleted_this_round == 0:
            print("stopped: no declarations deleted in round")
            return 1
    print(f"stopped after max rounds, deleted {total_deleted} declarations")
    return 0


if __name__ == "__main__":
    sys.exit(main())
