# TermX Remote Rebuild Runbook

## Slice 11-A External Managed Smoke

Purpose: temporary public smoke for the current web-control + hub + daemon remote path.

Hosts:

- Web/control + hub temporary host: `root@114.66.58.243`
- Agent temporary host: `al`
- Local browser/test client: this workstation

Safety rules:

- Use only `/tmp/termx-devstack` for temporary files on external hosts.
- Do not modify SSH config, firewall, iptables, DNS, TLS stores, or systemd.
- Do not clear system directories.
- Stop services with recorded PID files.
- Remove only `/tmp/termx-devstack` files created by this run.

Planned remote host layout:

```sh
mkdir -p /tmp/termx-devstack/bin /tmp/termx-devstack/data /tmp/termx-devstack/logs
umask 077
cat > /tmp/termx-devstack/secrets.env <<'EOF'
TERMX_WEB_CONTROL_TOKEN_SECRET=<generated-with-openssl-rand-hex-32>
TERMX_WEB_CONTROL_HUB_SECRET=<generated-with-openssl-rand-hex-32>
TERMX_HUB_DEBUG_TOKEN=<generated-with-openssl-rand-hex-32>
TERMX_SMOKE_PASSWORD=<generated-with-openssl-rand-hex-16>
EOF
```

Planned web/control + hub start commands on `root@114.66.58.243`:

```sh
cd /tmp/termx-devstack
set -a
. /tmp/termx-devstack/secrets.env
set +a
TERMX_WEB_CONTROL_ADDR=0.0.0.0:12306 \
TERMX_WEB_CONTROL_SQLITE_DSN='file:/tmp/termx-devstack/data/web-control.sqlite?_pragma=busy_timeout(5000)' \
TERMX_WEB_CONTROL_TOKEN_SECRET="$TERMX_WEB_CONTROL_TOKEN_SECRET" \
TERMX_WEB_CONTROL_HUB_SECRET="$TERMX_WEB_CONTROL_HUB_SECRET" \
nohup /tmp/termx-devstack/bin/web-control > /tmp/termx-devstack/logs/web-control.log 2>&1 &
echo $! > /tmp/termx-devstack/web-control.pid

TERMX_HUB_ADDR=0.0.0.0:8447 \
TERMX_HUB_CONTROL_URL=http://127.0.0.1:12306 \
TERMX_HUB_CONTROL_SECRET="$TERMX_WEB_CONTROL_HUB_SECRET" \
TERMX_HUB_STUN_SERVERS='stun:stun.l.google.com:19302' \
TERMX_HUB_DEBUG_TOKEN="$TERMX_HUB_DEBUG_TOKEN" \
nohup /tmp/termx-devstack/bin/termx-hub > /tmp/termx-devstack/logs/termx-hub.log 2>&1 &
echo $! > /tmp/termx-devstack/termx-hub.pid
```

Planned agent start commands on `al`:

```sh
mkdir -p /tmp/termx-devstack/bin /tmp/termx-devstack/data /tmp/termx-devstack/logs
cat > /tmp/termx-devstack/termx.yaml <<'EOF'
remote:
  enabled: true
  controlURL: http://114.66.58.243:12306
  hubURL: http://114.66.58.243:8447
  accessTokenEnv: TERMX_REMOTE_ACCESS_TOKEN
  dataDir: /tmp/termx-devstack/data/agent-remote
  deviceName: external-smoke-agent
EOF
TERMX_REMOTE_ACCESS_TOKEN='<temporary-token-from-control>' \
TERMX_REMOTE_LOCAL_WEB_ENABLE=true \
TERMX_REMOTE_LOCAL_WEB_ADDR=127.0.0.1:18888 \
nohup /tmp/termx-devstack/bin/termx --config /tmp/termx-devstack/termx.yaml daemon \
  --socket /tmp/termx-devstack/termx.sock \
  --log-file /tmp/termx-devstack/logs/termx-daemon.log \
  > /tmp/termx-devstack/logs/termx-daemon.stdout.log 2>&1 &
echo $! > /tmp/termx-devstack/termx-daemon.pid
```

Planned local SSH tunnel for pairing only:

```sh
ssh -N -L 18888:127.0.0.1:18888 al
```

If `127.0.0.1:18888` is already occupied locally, use an explicit alternate local port:

```sh
ssh -N -L 127.0.0.1:18890:127.0.0.1:18888 al
```

Planned local smoke client command:

```sh
/tmp/termx-devstack-build/termx-remote-e2e \
  --control-url http://114.66.58.243:12306 \
  --hub-url http://114.66.58.243:8447 \
  --email devstack-smoke@example.com \
  --password '<generated-smoke-password-from-secrets-env>' \
  --pair-url http://127.0.0.1:18890/api/local/pair \
  --pair-session-id '<from termx remote pair --json on al>' \
  --pair-secret '<from termx remote pair --json on al>' \
  --stun-url stun:stun.l.google.com:19302
```

Planned stop commands:

```sh
for pidfile in /tmp/termx-devstack/*.pid; do
  [ -f "$pidfile" ] || continue
  kill "$(cat "$pidfile")" 2>/dev/null || true
done
```

Planned cleanup commands:

```sh
rm -rf /tmp/termx-devstack
```

Execution log:

- 2026-05-03T12:50:00+08:00: local runnable-service tests passed. External smoke is needed because the requested verification spans a public web/control + hub host, a separate `al` daemon host, and a local WebRTC offerer; this cannot be fully represented by in-process tests. Proceeding with temporary `/tmp/termx-devstack` deployment only.
- 2026-05-03T12:51:00+08:00: built temporary binaries under `/tmp/termx-devstack-build`: linux/amd64 `web-control`, `termx-hub`, `termx`; local `termx-remote-e2e` smoke client.
- 2026-05-03T12:52:00+08:00: confirmed SSH access and `/tmp/termx-devstack` writeability on `root@114.66.58.243` and `al`; copied `web-control` and `termx-hub` to the public host and `termx` to `al`.
- 2026-05-03T12:54:00+08:00: started public host temp services with pid files `web-control.pid=3315802`, `termx-hub.pid=3315807`; health checks passed at `http://114.66.58.243:12306/api/health` and `http://114.66.58.243:8447/api/health`.
- 2026-05-03T13:05:00+08:00: started daemon on `al` with remote enabled, Control URL `http://114.66.58.243:12306`, Hub URL `http://114.66.58.243:8447`, local web at `127.0.0.1:18888`, socket `/tmp/termx-devstack/termx.sock`, and logs under `/tmp/termx-devstack/logs`.
- 2026-05-03T13:12:00+08:00: first pairing smoke hit the wrong local endpoint because `127.0.0.1:18888` was already occupied by a local TermX instance. Switched tunnel to `127.0.0.1:18890 -> al:127.0.0.1:18888`; `curl http://127.0.0.1:18890/api/local/status` returned the `al` daemon machine.
- 2026-05-03T13:25:00+08:00: Hub diagnostics were added and protected by `TERMX_HUB_DEBUG_TOKEN`; diagnostics showed the daemon polled the managed offer but returned `offer signature verification failed`. Root cause was Hub trimming signed SDP bytes.
- 2026-05-03T13:48:00+08:00: after preserving exact offer/answer SDP bytes through Hub, external smoke advanced to a successful managed answer submission. Hub diagnostics showed the active agent had `answer_count=1` and no last error.
- 2026-05-03T13:49:00+08:00: final STUN-only external smoke result before review fixes: control login, device registration, local pairing certificate claim, managed ticket creation, Hub offer poll, daemon verification, and Hub answer succeeded; WebRTC DataChannel timed out waiting for `terminal:1` to open. This is recorded as the STUN-only NAT/public ICE limitation in `WORKFLOW.md` item `11-A-H`.
- 2026-05-03T13:18:00+08:00: Slice `11-A` review found fixed public devstack secrets and missing signed agent registration proof. Code now requires machine-signed Hub registration and Web Control nonce verification; runbook now uses per-run generated secrets under `/tmp/termx-devstack/secrets.env`. The current externally running services must be restarted with rebuilt binaries and generated secrets before any further public smoke.
- 2026-05-03T13:24:00+08:00: rebuilt and copied linux/amd64 `web-control`, `termx-hub`, and `termx` plus local `termx-remote-e2e`. Stopped old pidfile services. Cleared only this run's temporary SQLite and agent identity under `/tmp/termx-devstack` to avoid old owner/secret contamination.
- 2026-05-03T13:26:00+08:00: restarted public web-control and hub with per-run generated secrets from `/tmp/termx-devstack/secrets.env`. Health checks passed. Public host pid files: `web-control.pid=3330462`, `termx-hub.pid=3330463`.
- 2026-05-03T13:27:00+08:00: registered a fresh smoke account and started daemon on `al` with pid `2207230`; daemon status is online with signed Hub registration and local web at `127.0.0.1:18888`.
- 2026-05-03T13:28:00+08:00: created terminal `1` on `al` with command `bash -lc 'while true; do sleep 60; done'`. Web Control inventory shows machine `device-0fbc2e86970eb988` and terminal `1`; Hub diagnostics show the signed agent online.
- 2026-05-03T13:29:00+08:00: local `127.0.0.1:18890` was already occupied by a previous SSH tunnel process (`ssh` pid `9764`), so this smoke used temporary tunnel `127.0.0.1:18891 -> al:127.0.0.1:18888`. The `18891` tunnel was stopped after the smoke.
- 2026-05-03T13:31:00+08:00: final post-review smoke command reached the same true boundary: local pair succeeded, Control inventory succeeded, managed ticket/session succeeded, Hub diagnostics showed `answer_count=1` and no `last_error`, but the local WebRTC offerer timed out waiting for `terminal:1` DataChannel open. This confirms control/signaling/daemon verification are working and public STUN-only ICE is the remaining runtime network limitation.

Current temporary state:

- Public host services are intentionally left running for inspection under `/tmp/termx-devstack`: web-control pid `3330462`, hub pid `3330463`.
- Agent host daemon is intentionally left running for inspection under `/tmp/termx-devstack`: daemon pid `2207230`, socket `/tmp/termx-devstack/termx.sock`, terminal `1` running.
- Local tunnel used by this run (`18891`) was stopped. Local port `18890` remains occupied by a pre-existing SSH tunnel process (`ssh` pid `9764`); it was not killed because it predates this final smoke context.
