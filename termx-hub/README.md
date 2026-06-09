# TermX Hub

`termx-hub` is the standalone cloud Hub process. Product logic lives in
`termx-remote/hub`; this executable reads environment variables, starts the Hub
HTTP API, optionally starts embedded TURN, and optionally runs management-plane
heartbeat to `web-control`.

## Minimal Startup

```bash
cd termx-hub
cp deploy/termx-hub.env.example /tmp/termx-hub.env
set -a
. /tmp/termx-hub.env
set +a
go run ./cmd/termx-hub
```

In another shell:

```bash
curl http://127.0.0.1:8447/api/health
```

Expected response includes `ok`.

For cloud registration, set:

- `TERMX_HUB_PUBLIC_HTTP_URL` to the URL browsers can reach.
- `TERMX_HUB_CONTROL_URL` to the web-control base URL.
- `TERMX_HUB_CONTROL_SECRET` to the same value as web-control `HUB_SECRET`.

When all three are present, the Hub starts periodic management heartbeat and
web-control should show the hub online. Missing any of those values disables
heartbeat without affecting local Hub signaling.

## systemd

The sample unit is `deploy/termx-hub.service`. Install the binary at
`/usr/local/bin/termx-hub`, place the environment file at
`/etc/termx-hub/termx-hub.env`, then enable the unit:

```bash
sudo cp deploy/termx-hub.service /etc/systemd/system/termx-hub.service
sudo systemctl daemon-reload
sudo systemctl enable --now termx-hub
sudo systemctl status termx-hub
```
