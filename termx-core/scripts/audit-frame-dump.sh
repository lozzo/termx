#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 3 ]]; then
  echo "usage: $0 <dump-path> [width] [height]" >&2
  exit 1
fi

dump_path=$1
width=${2:-120}
height=${3:-40}

go run ./cmd/termx-frame-audit -dump "$dump_path" -width "$width" -height "$height" -auto-size
