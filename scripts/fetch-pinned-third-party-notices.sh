#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mode="update"
if [[ "${1:-}" == "--check" ]]; then
  mode="check"
elif [[ $# -ne 0 ]]; then
  echo "usage: $0 [--check]" >&2
  exit 2
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/termx-pinned-notices.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

fetch_checked() {
  local url="$1"
  local expected="$2"
  local output="$3"
  curl --retry 5 --retry-all-errors --connect-timeout 15 --max-time 180 -fsSL "$url" -o "$output"
  local actual
  actual="$(sha256_file "$output")"
  if [[ "$actual" != "$expected" ]]; then
    echo "pinned notice hash mismatch: $url" >&2
    echo "expected $expected, got $actual" >&2
    exit 1
  fi
}

verify_derived() {
  local path="$1"
  local expected="$2"
  local actual
  actual="$(sha256_file "$path")"
  if [[ "$actual" != "$expected" ]]; then
    echo "derived notice hash mismatch: $path" >&2
    echo "expected $expected, got $actual" >&2
    exit 1
  fi
}

normalize_text() {
  LC_ALL=C perl -0777 -pe 's/\r\n/\n/g; s/[ \t]+(?=\n)//g; s/[ \t\r\n]*\z/\n/s' "$1"
}

publish() {
  local source="$1"
  local relative_target="$2"
  local target="$repo_root/$relative_target"
  local normalized
  normalized="$(mktemp "$tmp_dir/publish.XXXXXX")"
  normalize_text "$source" >"$normalized"
  if [[ "$mode" == "check" ]]; then
    if [[ ! -f "$target" ]] || ! cmp -s "$normalized" "$target"; then
      echo "stale or missing pinned notice: $relative_target" >&2
      exit 1
    fi
    return
  fi
  mkdir -p "$(dirname "$target")"
  install -m 0644 "$normalized" "$target"
}

fetch_checked \
  "https://raw.githubusercontent.com/nearinfinity/node-bplist-parser/84abe1c3b552a807461b28a5a28084ce4f827ce1/README.md" \
  "4a09c55c64d57abe284e09318763dcd69795ed7a201a84edf1bf8774c3b91a8e" \
  "$tmp_dir/bplist-parser-README.md"
sed -n '/^(The MIT License)$/,$p' "$tmp_dir/bplist-parser-README.md" >"$tmp_dir/bplist-parser-LICENSE.txt"
verify_derived "$tmp_dir/bplist-parser-LICENSE.txt" "ee3cfbe17c87702b4e1b1ce52be0d0cb923d39de3745a60a2ca09798977566b2"
publish "$tmp_dir/bplist-parser-LICENSE.txt" "docs/legal/third-party/npm/bplist-parser-0.3.2-LICENSE.txt"

fetch_checked \
  "https://raw.githubusercontent.com/bufbuild/protobuf-es/63a85470d21154c4ed069b2bc196b294327830f3/LICENSE" \
  "c04b4216f1cd4c5a4f7fb2f2a1b0ae70d847e9e0cac7c9dee9bf8cc03177c449" \
  "$tmp_dir/bufbuild-protobuf-Apache-2.0.txt"
publish "$tmp_dir/bufbuild-protobuf-Apache-2.0.txt" "docs/legal/third-party/npm/bufbuild-protobuf-2.12.0-Apache-2.0.txt"

fetch_checked \
  "https://raw.githubusercontent.com/bufbuild/protobuf-es/63a85470d21154c4ed069b2bc196b294327830f3/packages/protobuf/src/wire/varint.ts" \
  "afbc1944b4641a57cd26cb36e127a8ce20481046d239f26dd04df210c0d3d47c" \
  "$tmp_dir/bufbuild-protobuf-varint.ts"
sed -n '1,/support library is itself covered by the above license\./p' "$tmp_dir/bufbuild-protobuf-varint.ts" \
  | sed 's#^// ##; s#^//$##' >"$tmp_dir/bufbuild-protobuf-BSD-3-Clause.txt"
verify_derived "$tmp_dir/bufbuild-protobuf-BSD-3-Clause.txt" "182a1bc8985a586e8e0ca3b5a3af1ff3c28bd3475833a07f50b42b53dd7ac889"
publish "$tmp_dir/bufbuild-protobuf-BSD-3-Clause.txt" "docs/legal/third-party/npm/bufbuild-protobuf-2.12.0-BSD-3-Clause.txt"

fetch_checked \
  "https://raw.githubusercontent.com/TooTallNate/Java-WebSocket/v1.5.6/LICENSE" \
  "15101a7cbdaa7f1c161424b760e907e7832e4a1e7f05d03373ca91fbffdb95ee" \
  "$tmp_dir/Java-WebSocket-LICENSE.txt"
publish "$tmp_dir/Java-WebSocket-LICENSE.txt" "docs/legal/third-party/android/Java-WebSocket-1.5.6-LICENSE.txt"

fetch_checked \
  "https://raw.githubusercontent.com/qos-ch/slf4j/v_2.0.6/LICENSE.txt" \
  "6fbe2eaf44b193b8a40eed9208f52848572224ad8d7672dd09418aa174847e73" \
  "$tmp_dir/slf4j-LICENSE.txt"
publish "$tmp_dir/slf4j-LICENSE.txt" "docs/legal/third-party/android/slf4j-api-2.0.6-LICENSE.txt"

fetch_checked \
  "https://raw.githubusercontent.com/webrtc-sdk/android/878c5b093f8bbbd4955d1037316484aabe962d18/LICENSE" \
  "e6b282fe6c0fb353928923470457f31b44cbab203effd60c0cde4a5bb96c8aec" \
  "$tmp_dir/WEBRTC_SDK_WRAPPER_LICENSE.txt"
publish "$tmp_dir/WEBRTC_SDK_WRAPPER_LICENSE.txt" "termx-app/public/third-party/WEBRTC_SDK_WRAPPER_LICENSE.txt"

fetch_checked \
  "https://raw.githubusercontent.com/webrtc-sdk/android/878c5b093f8bbbd4955d1037316484aabe962d18/Licenses/WEBRTC.md" \
  "d1f9382c6878ac024155fd6d44a5977329108bb8b0a01cea40e4a2f1d7de252e" \
  "$tmp_dir/WEBRTC.md"
publish "$tmp_dir/WEBRTC.md" "termx-app/public/third-party/WEBRTC.md"

verify_derived "$repo_root/remote-ui/src/assets/fonts/LICENSE" "4ff4cfa7c2b208356fe1d7a658c2d751d25f9409d4895d11c203022106939908"
publish "$repo_root/remote-ui/src/assets/fonts/LICENSE" "termx-app/public/third-party/FONTS.txt"
publish "$repo_root/docs/legal/public-snapshot/LICENSE" "termx-app/public/APACHE-2.0.txt"

echo "pinned third-party notices are $mode"
