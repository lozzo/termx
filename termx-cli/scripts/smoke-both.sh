#!/usr/bin/env bash
set -euo pipefail

TERMX_BIN="${TERMX_BIN:-termx}"
CONTROL_URL="${CONTROL_URL:-http://localhost:12306}"

if [[ -z "${TERMX_TOKEN:-}" ]]; then
  echo "TERMX_TOKEN is required for Web Control /api/v1/machines online verification" >&2
  exit 1
fi

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

need curl
need jq

# Equivalent command: termx remote status --json
status_json="$("$TERMX_BIN" remote status --json)"

local_hub_url="$(printf '%s' "$status_json" | jq -r '.local.http_url // empty')"
if [[ -z "$local_hub_url" ]]; then
  echo "local.http_url missing; run: termx remote enable --mode both" >&2
  exit 1
fi

curl -fsS "$local_hub_url/api/health" >/dev/null
echo "local hub health ok: $local_hub_url/api/health"

remote_state="$(printf '%s' "$status_json" | jq -r '.remote.state // empty')"
if [[ "$remote_state" != "online" ]]; then
  echo "unexpected remote.state: ${remote_state:-<empty>}" >&2
  exit 1
fi
echo "remote state: $remote_state"

control_url="$(printf '%s' "$status_json" | jq -r '.remote.control_url // empty')"
hub_url="$(printf '%s' "$status_json" | jq -r '.remote.hub_url // empty')"
if [[ -z "$control_url" ]]; then
  echo "remote.control_url missing from status" >&2
  exit 1
fi
if [[ -z "$hub_url" ]]; then
  echo "remote.hub_url missing from status; wait for hub discovery or pass --hub-url during enable" >&2
  exit 1
fi
echo "remote control URL: $control_url"
echo "remote hub URL: $hub_url"

machines="$(curl -fsS \
  -H "Authorization: Bearer $TERMX_TOKEN" \
  "$CONTROL_URL/api/v1/machines")"
online_count="$(printf '%s' "$machines" | jq '[.machines[]? | select(.online == true)] | length')"
if [[ "$online_count" == "0" ]]; then
  echo "no online machines from Web Control; wait for agent heartbeat and retry" >&2
  exit 1
fi
echo "agent online machines: $online_count"
