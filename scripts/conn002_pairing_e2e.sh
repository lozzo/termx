#!/usr/bin/env bash

set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/anytty-conn002.XXXXXX")"
BIN_DIR="$TMP_ROOT/bin"
SECRET_DIR="$TMP_ROOT/secrets"
REPORT_DIR="$TMP_ROOT/reports"
DAEMON_RUNTIME="$TMP_ROOT/daemon/runtime"
DAEMON_STATE="$TMP_ROOT/daemon/state"
DAEMON_CONFIG="$TMP_ROOT/daemon/config"
DAEMON_SOCKET="$DAEMON_RUNTIME/anytty.sock"
PAIRING_SOCKET="$DAEMON_SOCKET.pair"
DAEMON_LOG="$REPORT_DIR/daemon.log"
ANYTTY_BIN="$BIN_DIR/anytty"
HARNESS_BIN="$BIN_DIR/conn002client"
daemon_pid=""

fail() {
  printf 'conn002 e2e failed: %s\n' "$*" >&2
  exit 1
}

stop_daemon() {
  if [[ -z "$daemon_pid" ]]; then
    return
  fi
  if kill -0 "$daemon_pid" 2>/dev/null; then
    kill -TERM "$daemon_pid" 2>/dev/null || true
  fi
  set +e
  wait "$daemon_pid"
  set -e
  daemon_pid=""
}

cleanup() {
  stop_daemon
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT INT TERM

mkdir -p "$BIN_DIR" "$SECRET_DIR" "$REPORT_DIR" "$DAEMON_RUNTIME" "$DAEMON_STATE" "$DAEMON_CONFIG"

env GOWORK=off go build -o "$ANYTTY_BIN" ./cmd/anytty
env GOWORK=off go build -o "$HARNESS_BIN" ./testkit/conn002client

clean_anytty_env() {
  "$ROOT/scripts/with-clean-anytty-env.sh" "$@"
}

daemon_cli() {
  clean_anytty_env env \
    XDG_RUNTIME_DIR="$DAEMON_RUNTIME" \
    XDG_STATE_HOME="$DAEMON_STATE" \
    XDG_CONFIG_HOME="$DAEMON_CONFIG" \
    "$ANYTTY_BIN" --socket "$DAEMON_SOCKET" --log-file "$DAEMON_LOG" "$@"
}

client_cli() {
  local client_root="$1"
  shift
  mkdir -p "$client_root/runtime" "$client_root/state" "$client_root/config"
  clean_anytty_env env \
    XDG_RUNTIME_DIR="$client_root/runtime" \
    XDG_STATE_HOME="$client_root/state" \
    XDG_CONFIG_HOME="$client_root/config" \
    "$ANYTTY_BIN" --socket "$client_root/runtime/unused.sock" --log-file "$client_root/client.log" "$@"
}

prepare_client_endpoint() {
  local client_root="$1"
  local endpoint_id="$2"
  local device_id="$3"
  local device_fingerprint="$4"
  client_cli "$client_root" endpoint --registry "$client_root/connections.yaml" add cloud "$endpoint_id" \
    --device-id "$device_id" --device-fingerprint "$device_fingerprint" --target-device-id "$device_id" \
    >"$REPORT_DIR/${endpoint_id}.endpoint-add.out"
}

start_daemon() {
  clean_anytty_env env \
    XDG_RUNTIME_DIR="$DAEMON_RUNTIME" \
    XDG_STATE_HOME="$DAEMON_STATE" \
    XDG_CONFIG_HOME="$DAEMON_CONFIG" \
    ANYTTY_HISTORY_DISABLE=1 \
    "$ANYTTY_BIN" --socket "$DAEMON_SOCKET" --log-file "$DAEMON_LOG" daemon run \
    >>"$REPORT_DIR/daemon.stdout" 2>>"$REPORT_DIR/daemon.stderr" &
  daemon_pid=$!

  local attempt
  for attempt in $(seq 1 200); do
    if [[ -S "$DAEMON_SOCKET" && -S "$PAIRING_SOCKET" ]]; then
      if daemon_cli --timeout 2s access identity --json >"$REPORT_DIR/identity.ready.json" 2>"$REPORT_DIR/identity.ready.err"; then
        return
      fi
    fi
    if ! kill -0 "$daemon_pid" 2>/dev/null; then
      fail "daemon exited before both local sockets became ready"
    fi
    sleep 0.05
  done
  fail "daemon did not become ready"
}

wait_for_access_label() {
  local label="$1"
  local output="$2"
  local attempt
  for attempt in $(seq 1 200); do
    if daemon_cli --timeout 2s access list --json >"$output" 2>"$REPORT_DIR/access-list.poll.err"; then
      if node -e '
        const fs = require("fs");
        const records = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
        process.exit(records.some((record) => record.client_label === process.argv[2]) ? 0 : 1);
      ' "$output" "$label"; then
        return
      fi
    fi
    sleep 0.05
  done
  fail "access record for $label was not committed"
}

start_daemon
daemon_cli access identity --json >"$REPORT_DIR/identity.before.json"

SECOND_SOCKET="$DAEMON_RUNTIME/anytty-second.sock"
set +e
clean_anytty_env env \
  XDG_RUNTIME_DIR="$DAEMON_RUNTIME" XDG_STATE_HOME="$DAEMON_STATE" XDG_CONFIG_HOME="$DAEMON_CONFIG" ANYTTY_HISTORY_DISABLE=1 \
  "$ANYTTY_BIN" --socket "$SECOND_SOCKET" --log-file "$REPORT_DIR/daemon-second.log" daemon run \
  >"$REPORT_DIR/daemon-second.stdout" 2>"$REPORT_DIR/daemon-second.stderr" &
second_pid=$!
for _ in $(seq 1 100); do
  if ! kill -0 "$second_pid" 2>/dev/null; then
    break
  fi
  sleep 0.02
done
if kill -0 "$second_pid" 2>/dev/null; then
  kill -TERM "$second_pid" 2>/dev/null || true
  wait "$second_pid" 2>/dev/null || true
  set -e
  fail "second daemon sharing client-access state did not fail closed"
fi
wait "$second_pid"
second_status=$?
set -e
[[ "$second_status" -ne 0 ]] || fail "second daemon sharing client-access state exited successfully"

RACE_BUNDLE="$SECRET_DIR/race-pairing.pb"
daemon_cli pair create --out "$RACE_BUNDLE" --ttl 5m --grant-ttl 1h --label conn002-race >"$REPORT_DIR/race-create.out"
daemon_cli pair inspect "$RACE_BUNDLE" --json >"$REPORT_DIR/race-bundle.json"
RACE_DEVICE_ID="$(node -e 'const v=require(process.argv[1]); process.stdout.write(v.device_id)' "$REPORT_DIR/race-bundle.json")"
RACE_FINGERPRINT="$(node -e 'const v=require(process.argv[1]); process.stdout.write(v.device_fingerprint)' "$REPORT_DIR/race-bundle.json")"

CLIENT_A="$TMP_ROOT/client-a"
CLIENT_B="$TMP_ROOT/client-b"
prepare_client_endpoint "$CLIENT_A" endpoint-race "$RACE_DEVICE_ID" "$RACE_FINGERPRINT"
prepare_client_endpoint "$CLIENT_B" endpoint-race "$RACE_DEVICE_ID" "$RACE_FINGERPRINT"
set +e
client_cli "$CLIENT_A" pair import "$RACE_BUNDLE" --id endpoint-race --pair-socket "$PAIRING_SOCKET" \
  --registry "$CLIENT_A/connections.yaml" --client-label race-a >"$REPORT_DIR/race-a.out" 2>"$REPORT_DIR/race-a.err" &
pid_a=$!
client_cli "$CLIENT_B" pair import "$RACE_BUNDLE" --id endpoint-race --pair-socket "$PAIRING_SOCKET" \
  --registry "$CLIENT_B/connections.yaml" --client-label race-b >"$REPORT_DIR/race-b.out" 2>"$REPORT_DIR/race-b.err" &
pid_b=$!
wait "$pid_a"
status_a=$?
wait "$pid_b"
status_b=$?
set -e

if [[ "$status_a" -eq 0 && "$status_b" -ne 0 ]]; then
  WINNER_ROOT="$CLIENT_A"
  LOSER_ROOT="$CLIENT_B"
elif [[ "$status_b" -eq 0 && "$status_a" -ne 0 ]]; then
  WINNER_ROOT="$CLIENT_B"
  LOSER_ROOT="$CLIENT_A"
else
  fail "concurrent ticket redemption must have exactly one winner (a=$status_a b=$status_b)"
fi

if client_cli "$LOSER_ROOT" pair import "$RACE_BUNDLE" --id endpoint-race --pair-socket "$PAIRING_SOCKET" \
  --registry "$LOSER_ROOT/connections.yaml" --client-label race-loser-retry >"$REPORT_DIR/race-loser-retry.out" 2>"$REPORT_DIR/race-loser-retry.err"; then
  fail "ticket bound to the winning key was accepted for the losing key"
fi

BAD_FINGERPRINT_BUNDLE="$SECRET_DIR/bad-fingerprint.pb"
"$HARNESS_BIN" --mode tamper-fingerprint --bundle "$RACE_BUNDLE" --output "$BAD_FINGERPRINT_BUNDLE"
if client_cli "$TMP_ROOT/client-bad-fingerprint" pair import "$BAD_FINGERPRINT_BUNDLE" --id endpoint-bad \
  --pair-socket "$PAIRING_SOCKET" --registry "$TMP_ROOT/client-bad-fingerprint/connections.yaml" \
  >"$REPORT_DIR/bad-fingerprint.out" 2>"$REPORT_DIR/bad-fingerprint.err"; then
  fail "bundle with the wrong daemon fingerprint was accepted"
fi

daemon_cli access list --json >"$REPORT_DIR/access-after-race.json"
node - "$REPORT_DIR/access-after-race.json" <<'NODE'
const fs = require("fs");
const records = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (records.length !== 1 || !["race-a", "race-b"].includes(records[0].client_label)) {
  throw new Error(`unexpected access records after race: ${JSON.stringify(records)}`);
}
NODE

"$HARNESS_BIN" --mode verify \
  --daemon-identity-dir "$DAEMON_STATE/anytty/remote-v2/identity" \
  --daemon-access-dir "$DAEMON_STATE/anytty/remote-v2/access" \
  --credential-dir "$WINNER_ROOT/state/anytty/remote-v2/credentials" \
  --endpoint-id endpoint-race --expect active >"$REPORT_DIR/race-winner-active.json"

SHARED_BUNDLE="$SECRET_DIR/shared-key-race.pb"
SHARED_CLIENT="$TMP_ROOT/client-shared"
daemon_cli pair create --out "$SHARED_BUNDLE" --ttl 5m --grant-ttl 1h --label conn002-shared-key >"$REPORT_DIR/shared-create.out"
prepare_client_endpoint "$SHARED_CLIENT" endpoint-shared "$RACE_DEVICE_ID" "$RACE_FINGERPRINT"
set +e
client_cli "$SHARED_CLIENT" pair import "$SHARED_BUNDLE" --id endpoint-shared --pair-socket "$PAIRING_SOCKET" \
  --registry "$SHARED_CLIENT/connections.yaml" --client-label shared-key >"$REPORT_DIR/shared-a.out" 2>"$REPORT_DIR/shared-a.err" &
shared_a=$!
client_cli "$SHARED_CLIENT" pair import "$SHARED_BUNDLE" --id endpoint-shared --pair-socket "$PAIRING_SOCKET" \
  --registry "$SHARED_CLIENT/connections.yaml" --client-label shared-key >"$REPORT_DIR/shared-b.out" 2>"$REPORT_DIR/shared-b.err" &
shared_b=$!
client_cli "$SHARED_CLIENT" endpoint --registry "$SHARED_CLIENT/connections.yaml" add ssh sidecar \
  --host sidecar.example --remote-socket auto >"$REPORT_DIR/shared-endpoint-add.out" 2>"$REPORT_DIR/shared-endpoint-add.err" &
shared_endpoint_add=$!
wait "$shared_a"
shared_status_a=$?
wait "$shared_b"
shared_status_b=$?
wait "$shared_endpoint_add"
shared_endpoint_status=$?
set -e
if [[ "$shared_status_a" -ne 0 || "$shared_status_b" -ne 0 || "$shared_endpoint_status" -ne 0 ]]; then
  fail "pair import and endpoint mutation did not converge (a=$shared_status_a b=$shared_status_b endpoint=$shared_endpoint_status)"
fi
client_cli "$SHARED_CLIENT" endpoint --registry "$SHARED_CLIENT/connections.yaml" show sidecar --json >"$REPORT_DIR/shared-sidecar.json"
node -e '
  const value = require(process.argv[1]);
  if (value.item?.id !== "sidecar" || value.item.routes.length !== 1 || value.item.routes[0].kind !== "ssh-webrtc-tcp") process.exit(1);
' "$REPORT_DIR/shared-sidecar.json" || fail "concurrent endpoint mutation was lost"
"$HARNESS_BIN" --mode verify \
  --daemon-identity-dir "$DAEMON_STATE/anytty/remote-v2/identity" \
  --daemon-access-dir "$DAEMON_STATE/anytty/remote-v2/access" \
  --credential-dir "$SHARED_CLIENT/state/anytty/remote-v2/credentials" \
  --endpoint-id endpoint-shared --expect active >"$REPORT_DIR/shared-key-active.json"

LOST_BUNDLE="$SECRET_DIR/lost-response.pb"
LOST_CLIENT="$TMP_ROOT/client-lost"
mkdir -p "$LOST_CLIENT/state/anytty/remote-v2/credentials"
daemon_cli pair create --out "$LOST_BUNDLE" --ttl 2s --grant-ttl 1h --label conn002-lost >"$REPORT_DIR/lost-create.out"
prepare_client_endpoint "$LOST_CLIENT" endpoint-lost "$RACE_DEVICE_ID" "$RACE_FINGERPRINT"
"$HARNESS_BIN" --mode drop-response --bundle "$LOST_BUNDLE" --pair-socket "$PAIRING_SOCKET" \
  --credential-dir "$LOST_CLIENT/state/anytty/remote-v2/credentials" --endpoint-id endpoint-lost \
  --client-label lost-response >"$REPORT_DIR/lost-drop.json"
wait_for_access_label lost-response "$REPORT_DIR/access-after-drop.json"
sleep 3
client_cli "$LOST_CLIENT" pair import "$LOST_BUNDLE" --id endpoint-lost --pair-socket "$PAIRING_SOCKET" \
  --registry "$LOST_CLIENT/connections.yaml" --client-label lost-response >"$REPORT_DIR/lost-retry.out" 2>"$REPORT_DIR/lost-retry.err"

"$HARNESS_BIN" --mode verify \
  --daemon-identity-dir "$DAEMON_STATE/anytty/remote-v2/identity" \
  --daemon-access-dir "$DAEMON_STATE/anytty/remote-v2/access" \
  --credential-dir "$LOST_CLIENT/state/anytty/remote-v2/credentials" \
  --endpoint-id endpoint-lost --expect active >"$REPORT_DIR/lost-active.json"

daemon_cli access list --json >"$REPORT_DIR/access-before-revoke.json"
LOST_GRANT_ID="$(node -e '
  const fs = require("fs");
  const records = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  const record = records.find((item) => item.client_label === "lost-response");
  if (!record || !record.grant_id) process.exit(1);
  process.stdout.write(record.grant_id);
' "$REPORT_DIR/access-before-revoke.json")"
[[ -n "$LOST_GRANT_ID" ]] || fail "lost-response grant id was not listed"
daemon_cli access revoke "$LOST_GRANT_ID" --json >"$REPORT_DIR/revoke.json"

"$HARNESS_BIN" --mode verify \
  --daemon-identity-dir "$DAEMON_STATE/anytty/remote-v2/identity" \
  --daemon-access-dir "$DAEMON_STATE/anytty/remote-v2/access" \
  --credential-dir "$LOST_CLIENT/state/anytty/remote-v2/credentials" \
  --endpoint-id endpoint-lost --expect revoked >"$REPORT_DIR/lost-revoked.json"

stop_daemon
start_daemon
daemon_cli access identity --json >"$REPORT_DIR/identity.after.json"
cmp -s "$REPORT_DIR/identity.before.json" "$REPORT_DIR/identity.after.json" || fail "daemon DeviceIdentity changed across restart"
daemon_cli access list --json >"$REPORT_DIR/access-after-restart.json"
node - "$REPORT_DIR/access-after-restart.json" "$LOST_GRANT_ID" <<'NODE'
const fs = require("fs");
const records = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const record = records.find((item) => item.grant_id === process.argv[3]);
if (!record || !record.revoked_at || record.revoked_at.startsWith("0001-")) {
  throw new Error(`revocation did not survive restart: ${JSON.stringify(record)}`);
}
NODE

"$HARNESS_BIN" --mode verify \
  --daemon-identity-dir "$DAEMON_STATE/anytty/remote-v2/identity" \
  --daemon-access-dir "$DAEMON_STATE/anytty/remote-v2/access" \
  --credential-dir "$LOST_CLIENT/state/anytty/remote-v2/credentials" \
  --endpoint-id endpoint-lost --expect revoked >"$REPORT_DIR/lost-revoked-after-restart.json"

if rg -n 'anytty-grant-v2|anytty-pairing-ticket-v1|"pairing_ticket"|"capability_grant"' "$REPORT_DIR"; then
  fail "non-secret logs or command output leaked ticket/grant material"
fi

printf 'conn002 pairing e2e ok\n'
