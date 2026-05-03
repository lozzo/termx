# TermX Remote Rebuild Runbook

## Slice 20 APP / Remote UI E2E Plan

Purpose: validate the APP-first remote-ui flow before touching the public server. The first pass is local and repeatable: a stored machine record appears in the APP shell, selecting it drives the orchestrator through `local -> public_p2p -> managed`, and terminal/file/api/events consumers receive the same connected `RtcSession`.

Local-only scope:

- Add focused tests under `remote-ui/src`.
- Use real `MachineStore`, `RemoteAppShell`, and `ConnectionOrchestrator`.
- Use fake `RtcConnector` and fake `RtcSession` only at the provider boundary.
- Do not use HTTP/WebSocket as terminal/file/api/events runtime.
- Do not introduce `relay` as a fourth path; managed relay remains `relayInUse` / capability info.

Optional public devstack scope:

- Use `root@114.66.58.243` only after the local e2e harness passes and only if the slice needs a live daemon/Web Control/Hub smoke.
- Use existing temporary layout under `/tmp/termx-devstack`; do not modify SSH config, firewall, iptables, DNS, TLS, systemd, or system directories.
- Refresh expiring devstack tokens or restart only scoped temp services when needed; record every command and residual state in this runbook.
- Keep public daemon local pair API bound to `127.0.0.1` and use an SSH tunnel only for pairing.

Planned local validation commands:

```sh
cd remote-ui
npm test -- --run src/appConnectionE2E.test.tsx
npm test
npm run typecheck
npm run build
```

Execution log:

- 2026-05-03T20:18:00+08:00: Slice `20` started. Public devstack is not being used yet; first step is a local APP e2e harness so the product flow is testable without external dependencies.
- 2026-05-03T20:23:00+08:00: added local `remote-ui/src/appConnectionE2E.test.tsx`. First run failed because the test incorrectly passed an object to `parsePairingPayload`; corrected it to a real JSON QR payload string. Focused local e2e then passed. No public server access used.
- 2026-05-03T20:52:00+08:00: review found the first local harness called fake runtime methods directly and did not prove stored QR metadata drove connector inputs. Updated the test to render real `useTerminalSession` and `useFileManager` consumers over the connected `RtcSession`, subscribe to events through `RtcSession`, and build connector inputs from stored local/public/control/hub/pairing/bootstrap metadata. Full remote-ui validation passed locally. Public devstack smoke is still needed for Web Control/Hub/daemon closed-loop coverage.

Planned scoped public devstack checks for this slice:

```sh
# health and current process state only
ssh root@114.66.58.243 'set -eu
  test -d /tmp/termx-devstack
  test -f /tmp/termx-devstack/web-control.pid
  test -f /tmp/termx-devstack/termx-hub.pid
  curl -fsS http://127.0.0.1:12306/api/health
  curl -fsS http://127.0.0.1:8447/api/health
  if [ -f /tmp/termx-devstack/public-daemon/termx-daemon.pid ]; then
    ps -p "$(cat /tmp/termx-devstack/public-daemon/termx-daemon.pid)" -o pid=,comm=
  fi
'
```

If the public daemon token is expired, refresh only the temporary smoke account token and restart only `/tmp/termx-devstack/public-daemon` using the existing Slice `11-C` layout. Do not restart Web Control/Hub unless a health check fails. Do not clean `/tmp/termx-devstack`.

- 2026-05-03T20:53:00+08:00: scoped public health checks passed: Web Control and Hub returned `status=ok`; public daemon pid `3355324` was still running but `remote.state=degraded` because the 15-minute access token expired.
- 2026-05-03T20:54:00+08:00: refreshed only the temporary smoke account token via `POST /api/v1/auth/refresh`, wrote the new token to `/tmp/termx-devstack/public-daemon/access-token`, and restarted only the public daemon. New public daemon pid `3425079`; Web Control and Hub were not restarted and `/tmp/termx-devstack` was not cleaned.
- 2026-05-03T20:55:00+08:00: public daemon reported `remote.state=online` but `terminal_count=0` after restart, so created scoped terminal `1` named `public-host-slice20`. Hub debug with `X-TermX-Debug-Token` showed current public-host agent session `agent_session_QQ0Ds_p_IKlkARwmo8Y_Hw`.
- 2026-05-03T20:55:00+08:00: first managed smoke attempt failed before runtime because the local `18992` SSH tunnel exited with the shell; pairing endpoint returned connection refused. Reopened a stable tunnel with `ssh -fN -o ExitOnForwardFailure=yes -L 127.0.0.1:18993:127.0.0.1:18988 root@114.66.58.243`.
- 2026-05-03T20:55:00+08:00: generated a fresh pair session `pair_3oFA2m9K3bSDSbN6PPkXYg` from the public daemon and ran local `/tmp/termx-devstack-build/termx-remote-e2e` with explicit machine `device-8bce73b2996907df` and terminal `1`. Output: `remote managed smoke passed`.
- 2026-05-03T20:56:00+08:00: stopped the local `18993` SSH tunnel. Final state: public daemon `remote.state=online`, terminal `1` `public-host-slice20` running, Hub debug current public-host agent `agent_session_QQ0Ds_p_IKlkARwmo8Y_Hw` has `answer_count=1`, `last_answer_session_id=ct_W3F0lXItE-uYetwJMYO4hw-terminal-1`, `last_error=""`.

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
TERMX_WEB_CONTROL_STATIC_DIR=/tmp/termx-devstack/web-control-frontend \
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
- 2026-05-03T13:58:00+08:00: Slice `11-B` added a Web Control inspection UI and static serving. Future temporary web-control starts must include `TERMX_WEB_CONTROL_STATIC_DIR=/tmp/termx-devstack/web-control-frontend`. The UI is for login, health, machine/terminal inventory, and managed-ticket inspection only; it does not carry terminal/file/api/events runtime over HTTP.
- 2026-05-03T13:59:00+08:00: copied the rebuilt `web-control` binary as `/tmp/termx-devstack/bin/web-control.new`, stopped only the old web-control pid, replaced `/tmp/termx-devstack/bin/web-control`, and restarted web-control pid `3338937` with `TERMX_WEB_CONTROL_STATIC_DIR`. Hub and `al` daemon were not stopped for this web UI redeploy.
- 2026-05-03T14:02:00+08:00: the previous smoke access token had expired after the web-control restart. Refreshed the temporary smoke account token, wrote it to `/tmp/termx-devstack-build/access-token`, restarted only the temporary `al` daemon with the refreshed `TERMX_REMOTE_ACCESS_TOKEN`, and recreated terminal `1` with `termx new --name remote-ui-smoke -- bash -lc 'while true; do sleep 60; done'`.
- 2026-05-03T14:05:00+08:00: Web UI/API verification passed: `GET http://114.66.58.243:12306/` returned the Vite shell, `/api/health` returned OK, `/api/devices` returned machine `device-0fbc2e86970eb988`, `/api/terminals` returned terminal `1`, and `/api/v1/managed/connect-tickets` created ticket `ct_15lu3l6hdEsWTN1GdPAgBw` with `path=managed` and relay denied for the registered-free plan.
- 2026-05-03T14:11:00+08:00: after Slice `11-B` review, rebuilt and redeployed web-control again so missing `/assets/...` files return 404 instead of the SPA shell. Final verification: `GET /` returned 200, `/assets/old-hash.js` returned 404, authenticated device/terminal inventory returned the live `al` daemon, and `ssh al termx remote status --json` returned `remote.state=online` with `terminal_count=1`.

## Slice 11-C Public Host Daemon Smoke

Purpose: temporary managed WebRTC smoke with the `termx daemon` running directly on the public host `root@114.66.58.243`, because the previous `al` daemon smoke proved control/signaling but timed out at STUN-only DataChannel open across NAT.

Safety rules:

- Use only `/tmp/termx-devstack/public-daemon` for the new daemon state, logs, pidfile, and config.
- Do not stop the already-running public web-control or hub unless explicitly fixing this slice.
- Do not modify SSH config, firewall, iptables, DNS, TLS stores, or systemd.
- Do not bind the daemon local web UI to a public interface; keep it on `127.0.0.1` and use an SSH tunnel only for pairing.
- Do not introduce terminal/file/api/events HTTP or WebSocket runtime fallback.

Planned public-host daemon layout:

```sh
mkdir -p /tmp/termx-devstack/bin \
  /tmp/termx-devstack/public-daemon/data \
  /tmp/termx-devstack/public-daemon/logs
cat > /tmp/termx-devstack/public-daemon/termx.yaml <<'EOF'
remote:
  enabled: true
  controlURL: http://127.0.0.1:12306
  hubURL: http://127.0.0.1:8447
  accessTokenEnv: TERMX_REMOTE_ACCESS_TOKEN
  dataDir: /tmp/termx-devstack/public-daemon/data/agent-remote
  deviceName: public-host-agent
EOF
```

Planned local build/copy command for the Web Control binary when this slice needs redeploying:

```sh
cd web-control
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOWORK=off go build \
  -o /tmp/termx-devstack-build/web-control ./cmd/web-control
scp /tmp/termx-devstack-build/web-control root@114.66.58.243:/tmp/termx-devstack/bin/web-control
```

Planned public-host daemon start command:

```sh
TERMX_REMOTE_ACCESS_TOKEN='<temporary-token-from-control>' \
TERMX_REMOTE_LOCAL_WEB_ENABLE=true \
TERMX_REMOTE_LOCAL_WEB_ADDR=127.0.0.1:18988 \
nohup /tmp/termx-devstack/bin/termx --config /tmp/termx-devstack/public-daemon/termx.yaml daemon \
  --socket /tmp/termx-devstack/public-daemon/termx.sock \
  --log-file /tmp/termx-devstack/public-daemon/logs/termx-daemon.log \
  > /tmp/termx-devstack/public-daemon/logs/termx-daemon.stdout.log 2>&1 &
echo $! > /tmp/termx-devstack/public-daemon/termx-daemon.pid
```

Planned public-host terminal command:

```sh
/tmp/termx-devstack/bin/termx \
  --socket /tmp/termx-devstack/public-daemon/termx.sock \
  --log-file /tmp/termx-devstack/public-daemon/logs/termx-cli.log \
  new --name public-host-smoke -- bash -lc 'while true; do sleep 60; done'
```

Planned local pairing tunnel:

```sh
ssh -N -L 127.0.0.1:18991:127.0.0.1:18988 root@114.66.58.243
```

Planned local smoke command:

```sh
/tmp/termx-devstack-build/termx-remote-e2e \
  --control-url http://114.66.58.243:12306 \
  --hub-url http://114.66.58.243:8447 \
  --email '<current-smoke-email>' \
  --password '<generated-smoke-password-from-secrets-env>' \
  --pair-url http://127.0.0.1:18991/api/local/pair \
  --pair-session-id '<from public daemon termx remote pair --json>' \
  --pair-secret '<from public daemon termx remote pair --json>' \
  --machine-id '<public-host-daemon-machine-id>' \
  --terminal-id 1 \
  --stun-url stun:stun.l.google.com:19302
```

Planned stop commands for this daemon only:

```sh
/tmp/termx-devstack/bin/termx \
  --socket /tmp/termx-devstack/public-daemon/termx.sock \
  --log-file /tmp/termx-devstack/public-daemon/logs/termx-cli.log \
  ls | awk '{print $1}' | while read -r terminal_id; do
    [ -n "$terminal_id" ] || continue
    /tmp/termx-devstack/bin/termx \
      --socket /tmp/termx-devstack/public-daemon/termx.sock \
      --log-file /tmp/termx-devstack/public-daemon/logs/termx-cli.log \
      kill "$terminal_id" 2>/dev/null || true
  done
kill "$(cat /tmp/termx-devstack/public-daemon/termx-daemon.pid)" 2>/dev/null || true
```

Planned cleanup commands for this daemon only:

```sh
rm -rf /tmp/termx-devstack/public-daemon
```

Execution log:

- 2026-05-03T14:35:00+08:00: Slice `11-C` started. Public host daemon is needed to test whether managed WebRTC DataChannel opens when the daemon itself has a public network position. This does not change the product policy: client path remains `managed`, relay is only capability/policy info, and terminal runtime must remain WebRTC DataChannel.
- 2026-05-03T14:36:00+08:00: red check confirmed the current smoke account had no `public-host-agent` machine yet (`device_count=0`, `public_host_agent_count=0`). The first helper script assumed `smoke-auth.json` had a top-level `email`; corrected it to use `user.email`.
- 2026-05-03T14:37:00+08:00: copied `/tmp/termx-devstack-build/termx` to `/tmp/termx-devstack/bin/termx` on `root@114.66.58.243`; created `/tmp/termx-devstack/public-daemon/termx.yaml`; started the public daemon pid `3349076` with Control URL `http://127.0.0.1:12306`, Hub URL `http://127.0.0.1:8447`, socket `/tmp/termx-devstack/public-daemon/termx.sock`, local pair API `127.0.0.1:18988`, and logs under `/tmp/termx-devstack/public-daemon/logs`.
- 2026-05-03T14:37:00+08:00: public daemon status returned `remote.state=online`, `device_id=device-8bce73b2996907df`, `device_name=public-host-agent`, and `terminal_count=0`; Web Control inventory returned `public-host-agent`; Hub debug returned the corresponding signed agent online.
- 2026-05-03T14:37:00+08:00: created terminal `1` with `/tmp/termx-devstack/bin/termx --socket /tmp/termx-devstack/public-daemon/termx.sock new --name public-host-smoke -- bash -lc 'while true; do sleep 60; done'`. Status then returned `terminal_count=1`.
- 2026-05-03T14:38:00+08:00: opened temporary local pairing tunnel `127.0.0.1:18991 -> root@114.66.58.243:127.0.0.1:18988` with local ssh pid `48326`; generated a 5-minute pair session from the public daemon; ran local `/tmp/termx-devstack-build/termx-remote-e2e` with explicit `--machine-id device-8bce73b2996907df --terminal-id 1`.
- 2026-05-03T14:38:27+08:00: managed WebRTC smoke passed. The command printed `remote managed smoke passed`. Hub debug for `device-8bce73b2996907df` showed `answer_count=1`, `last_answer_session_id=ct_H90AGukqyBxzDk7pDNKsuQ-terminal-1`, and `last_error=""`.
- 2026-05-03T14:39:00+08:00: stopped only the temporary local pairing tunnel pid `48326`; public-host daemon pid `3349076` was intentionally left running for inspection. Observed non-blocking API follow-up: `GET /api/terminals?machine_id=device-8bce73b2996907df` currently returns all account terminals rather than filtering; this is recorded in `WORKFLOW.md` item `11-C-A`.
- 2026-05-03T14:47:00+08:00: Slice `11-C` review found the stop command did not explicitly terminate the long-running smoke terminal. Updated the daemon stop command to first kill terminal IDs through the public-daemon socket, then kill the daemon pid. Verified the scoped terminal cleanup path by creating terminal `2` named `cleanup-command-check` and killing it with `termx kill 2`; terminal `1` remains running intentionally for user inspection.
- 2026-05-03T14:50:00+08:00: cleanup verification exposed a Web Control registration bug: the public daemon briefly reported `remote.state=degraded` because Control rejected a terminal snapshot with `UNIQUE constraint failed: machine_terminals.machine_id, machine_terminals.id`. This is tracked as `WORKFLOW.md` item `11-C-B`.
- 2026-05-03T14:55:00+08:00: first redeploy attempt accidentally copied a darwin/arm64 `web-control` binary to the Linux public host because `GOOS/GOARCH` were not set. The process failed immediately and health returned connection refused; recovered by rebuilding with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOWORK=off` and replacing only `/tmp/termx-devstack/bin/web-control`.
- 2026-05-03T14:58:00+08:00: after the duplicate-terminal registration fix and Linux binary redeploy, Web Control health passed with pid `3352733`; public daemon returned `remote.state=online`, `device_id=device-8bce73b2996907df`, and `terminal_count=1`; Web Control terminals returned terminal `1` `public-host-smoke` as `running`.
- 2026-05-03T15:06:00+08:00: post-fix managed e2e passed again after the Web Control duplicate-terminal fix and redeploy. Command used explicit `--machine-id device-8bce73b2996907df --terminal-id 1`; output was `2026/05/03 14:50:10 remote managed smoke passed`. The temporary `18991` pairing tunnel was stopped after the smoke.
- 2026-05-03T15:18:00+08:00: final post-commit status check showed public daemon degraded with `invalid_token`; this is the expected limitation of the current 15-minute skeleton access tokens and is tracked as `WORKFLOW.md` item `11-C-C`. Refreshing the token and restarting only the public daemon before handing the environment back to the user.
- 2026-05-03T15:22:00+08:00: refreshed the devstack smoke account token, restarted only the public daemon as pid `3355324`, recreated terminal `1` named `public-host-smoke`, and verified `remote.state=online`, `terminal_count=1`, Web Control inventory, Hub current agent session, and no leftover local `18991` tunnel.

Current temporary state:

- Public host services are intentionally left running for inspection under `/tmp/termx-devstack`: web-control pid `3352733`, hub pid `3330463`, public daemon pid `3355324`.
- Public daemon state: `/tmp/termx-devstack/public-daemon`, socket `/tmp/termx-devstack/public-daemon/termx.sock`, local pair API `127.0.0.1:18988` on the public host only, machine `device-8bce73b2996907df`, terminal `1` named `public-host-smoke` running.
- Agent host `al` daemon is still intentionally left running from Slice `11-A/11-B` under `/tmp/termx-devstack`: current daemon pid is in `/tmp/termx-devstack/termx-daemon.pid`, socket `/tmp/termx-devstack/termx.sock`, terminal `1` running.
- Local tunnel used by Slice `11-C` (`18991`, ssh pid `48326`) was stopped. Local port `18890` remains occupied by a pre-existing SSH tunnel process (`ssh` pid `9764`); it was not killed because it predates this smoke context.
