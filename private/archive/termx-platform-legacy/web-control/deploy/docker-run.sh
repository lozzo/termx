#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:?usage: docker-run.sh <image>}"
NAME="${TERMX_WEB_CONTROL_CONTAINER:-termx-web-control}"
PORT="${PORT:-12306}"
DATA_DIR="${TERMX_WEB_CONTROL_DATA_DIR:-/opt/termx-web-control/data}"
ENV_FILE="${TERMX_WEB_CONTROL_ENV_FILE:-/etc/termx-web-control/web-control.env}"
PUBLISH_ADDR="${TERMX_WEB_CONTROL_PUBLISH_ADDR:-0.0.0.0}"

install -d -m 0755 "${DATA_DIR}"
chown -R 1001:1001 "${DATA_DIR}"

if docker ps -a --format '{{.Names}}' | grep -Fxq "${NAME}"; then
  docker rm -f "${NAME}"
fi

docker run -d \
  --name "${NAME}" \
  --restart unless-stopped \
  --env-file "${ENV_FILE}" \
  -e PORT="${PORT}" \
  -e HOSTNAME=0.0.0.0 \
  -e SQLITE_PATH=/app/data/termx-web.sqlite \
  -p "${PUBLISH_ADDR}:${PORT}:${PORT}" \
  -v "${DATA_DIR}:/app/data" \
  "${IMAGE}"

docker image prune -f >/dev/null
