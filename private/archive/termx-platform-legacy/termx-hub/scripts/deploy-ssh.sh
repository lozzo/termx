#!/usr/bin/env bash
set -euo pipefail

HOST="${1:-root@114.66.58.243}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_DIR="$(cd "${ROOT_DIR}/.." && pwd)"
BUILD_DIR="${ROOT_DIR}/.build"
BIN="${BUILD_DIR}/termx-hub-linux-amd64"
REMOTE_TMP="/tmp/termx-hub-linux-amd64"

PUBLIC_HTTP_URL="${TERMX_HUB_PUBLIC_HTTP_URL:-http://114.66.58.243:8447}"
HUB_ID="${TERMX_HUB_ID:-termx-hub-114-66-58-243}"
HUB_NAME="${TERMX_HUB_NAME:-TermX Hub 114.66.58.243}"
HUB_REGION="${TERMX_HUB_REGION:-cn}"
STUN_SERVERS="${TERMX_HUB_STUN_SERVERS:-stun:stun.l.google.com:19302}"
ADDR="${TERMX_HUB_ADDR:-0.0.0.0:8447}"
MAX_AGENTS="${TERMX_HUB_MAX_AGENTS:-1000}"

mkdir -p "${BUILD_DIR}"
(
  cd "${REPO_DIR}/termx-hub"
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "${BIN}" ./cmd/termx-hub
)

scp "${BIN}" "${HOST}:${REMOTE_TMP}"
ssh "${HOST}" "install -d -m 0755 /etc/termx-hub && install -m 0755 ${REMOTE_TMP} /usr/local/bin/termx-hub && rm -f ${REMOTE_TMP}"
ssh "${HOST}" "cat > /etc/termx-hub/termx-hub.env" <<EOF
TERMX_HUB_ADDR=${ADDR}
TERMX_HUB_ID=${HUB_ID}
TERMX_HUB_NAME=${HUB_NAME}
TERMX_HUB_REGION=${HUB_REGION}
TERMX_HUB_PUBLIC_HTTP_URL=${PUBLIC_HTTP_URL}
TERMX_HUB_STUN_SERVERS=${STUN_SERVERS}
TERMX_HUB_HEARTBEAT_INTERVAL=30s
TERMX_HUB_MAX_AGENTS=${MAX_AGENTS}
EOF
scp "${ROOT_DIR}/deploy/termx-hub.service" "${HOST}:/etc/systemd/system/termx-hub.service"
ssh "${HOST}" "systemctl daemon-reload && systemctl enable --now termx-hub && systemctl --no-pager --full status termx-hub"
