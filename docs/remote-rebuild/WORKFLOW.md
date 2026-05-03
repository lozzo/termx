# TermX Remote Rebuild Workflow

Compressed status file for unattended remote rebuild work. Keep this file short enough to reload after context compaction. Historical details are intentionally summarized here; recover full old detail from git history before `2026-05-03T16:40:26+08:00` if needed.

## Current State

- Current phase: Remote Web / Hub / Agent Buildout.
- Active todo: select next pending remote rebuild todo.
- Last updated: 2026-05-03T20:24:34+08:00.
- Workflow size policy: keep this file under 900 lines. Completed slice details older than the current/previous slice belong in compressed summaries, not full per-step logs.
- Worktree note: repository was already dirty at task start. Existing dirty files include root and package AGENTS files, remote rebuild docs, `go.work.sum`, and untracked remote rebuild planning docs. Do not revert or overwrite those user-provided changes.
- Current product conclusion: APP/remote-ui is the user operation entry and opens to a simple machine list. Web Control is only account/control-plane/status/admin, not a terminal operation surface. Connection attempts progress `local` / LAN first, then `public_p2p`, then `managed`. During development, rendezvous and managed relay are open to registered/dev users so the full flow can be proven before billing, plan, quota, and entitlement gates are reintroduced.
- Client-visible paths are only `local / public_p2p / managed`. Relay is not a fourth client transport and may appear only as connection info, capability, policy, quota, or telemetry.
- Required workflow check: `bash docs/remote-rebuild/check_workflow_rules.sh`.
- Required remote-ui validation for current frontend slices: `cd remote-ui && npm test && npm run typecheck && npm run build`.

## Operating Rules

- Continue unattended: after a slice is implemented, validated, reviewed, committed, and hash-recorded, immediately start the next pending slice unless the user pauses, changes target, or a high-risk irreversible operation / unmockable external dependency appears.
- Every slice still requires TDD, focused red test, implementation, broader validation, subagent review or recorded self-review, workflow update, commit, and hash backfill.
- If `WORKFLOW.md` and code disagree, fix `WORKFLOW.md` first.
- Prefer subagents for independent exploration/review. If unavailable, record the reason and do forced self-review.
- Mock external systems behind interfaces; record true production integrations as `deferred_external`.
- Do not use HTTP/WebSocket as terminal/file/api/events runtime. Runtime remains WebRTC DataChannel through `RtcSession`.
- Hub remains stateless: no DB, no durable source of truth, only bounded TTL memory.

## Ordered Todos

| ID | Phase | Todo | Status | Commit |
| --- | --- | --- | --- | --- |
| 0 | docs/workflow | Harden AGENTS and reset `WORKFLOW.md` for remote web/control-plane + hub + daemon-agent buildout | completed | `41671b21` |
| 1 | web-control | Create Web Control skeleton with Go backend, SQLite migration/test helper, health API, Vite React shell | completed | `e3f8541b` |
| 2 | web-control/auth | Implement web auth/account/plan/subscription foundations with provider interfaces and mock payment | completed | `f04a92c1` |
| 2-A | web-control/auth-review | Fix Slice 2 review findings around migration, auth, refresh, subscription, mock/provider ownership | completed | `f04a92c1` |
| 2-A-A | web-control/auth-self-review | Harden provider order identity and missing token issuer paths | completed | `f04a92c1` |
| 2-A-B | web-control/auth-follow-up | Fix Me nil issuer, pending payment sync, payment/subscription transaction atomicity | completed | `f04a92c1` |
| 2-B | external | Defer real payment/email/OAuth/billing/tax/risk integrations behind provider interfaces | deferred_external |  |
| 3 | web-control/machines | Implement machine, app device, app certificate, revocation, bootstrap, and claim control model | completed | `732689ba` |
| 3-A | web-control/machines-self-review | Reject private-key fields and prevent bootstrap mutation of claimed machines | completed | `732689ba` |
| 3-B | web-control/machines-review | Fix certificate metadata, signature verification, claim proof, conditional bootstrap writes | completed | `732689ba` |
| 3-B-A | web-control/machines-deferred | Preserve signed certificate bytes for future cross-service verification | deferred | `732689ba` |
| 3-B-B | web-control/machines-deferred | Add claim token TTL/rotation policy with daemon pairing UX | deferred | `732689ba` |
| 4 | public_p2p | Implement registered public P2P rendezvous, signaling forwarding, TTL/rate limits, STUN-only policy | completed | `61ed506a` |
| 4-A | external | Defer production DNS/TLS/public STUN/rendezvous deployment and abuse provider setup | deferred_external | `61ed506a` |
| 4-B | public_p2p-self-review | Harden rendezvous STUN whitelist, ICE payloads, TTL cap, expired cleanup | completed | `61ed506a` |
| 4-C | public_p2p-review | Fix payload validation, secret placement, STUN fail-closed, cleanup scheduling, workflow freshness | completed | `61ed506a` |
| 4-C-A | public_p2p-follow-up | Fix field types, ICE relay token parsing, envelope compatibility, body limits | completed | `61ed506a` |
| 4-C-B | public_p2p-final | Fix private-key-shaped cert fields, relay candidates in SDP, workflow freshness | completed | `61ed506a` |
| 5 | hub | Create Hub skeleton and agent registry with register, heartbeat, poll, answer, expiry | completed | `aa81dda7` |
| 5-A | hub-self-review | Harden answer assignment and runtime-payload rejection | completed | `aa81dda7` |
| 5-B | hub-review-auth | Enforce agent authority, reject rebinds, validate connect tickets before offers | completed | `aa81dda7` |
| 5-C | hub-review-ttl | Cleanup expired signaling without dropping valid queued offers | completed | `aa81dda7` |
| 5-D | hub-review-delivery | Make delivery retry and duplicate-answer semantics explicit | completed | `aa81dda7` |
| 5-E | hub-review-clone | Clone returned terminal slices | completed | `aa81dda7` |
| 5-F | hub-review-workflow | Refresh workflow and create production hub identity deferred todo | completed | `aa81dda7` |
| 5-G | external | Defer production hub identity, control-plane registration secret, signing/rotation | deferred_external | `aa81dda7` |
| 5-H | hub-self-review | Move authority verifier calls outside registry mutex | completed | `aa81dda7` |
| 5-I | hub-follow-up | Reject expired-agent heartbeats instead of resurrecting registrations | completed | `aa81dda7` |
| 5-J | hub-follow-up | Enforce signaling TTL on Poll, SubmitAnswer, GetAnswer | completed | `aa81dda7` |
| 5-K | hub-follow-up | Require basic SDP-shaped signaling payloads | completed | `aa81dda7` |
| 5-L | hub-follow-up | Refresh workflow state after Slice 5 follow-up | completed | `aa81dda7` |
| 6 | managed-signaling | Implement managed connect tickets and Hub signaling without TURN relay | completed | `2b35d6a1` |
| 6-A | managed-signaling-self | Mark managed connect tickets used atomically | completed | `2b35d6a1` |
| 6-B | managed-signaling-review | Authorize answer retrieval by ticket/session and machine | completed | `2b35d6a1` |
| 6-C | managed-signaling-review | Cap ticket TTL and guard overflow | completed | `2b35d6a1` |
| 6-D | managed-signaling-review | Make ownership check and ticket insert transactional | completed | `2b35d6a1` |
| 6-E | managed-signaling-review | Strengthen terminal correlation tests through verifier and registry | completed | `2b35d6a1` |
| 6-F | managed-signaling-deferred | Record ticket cleanup/session binding residual risk | deferred | `2b35d6a1` |
| 6-G | managed-signaling-self | Prevent relay capability before TURN policy exists | completed | `2b35d6a1` |
| 6-H | managed-signaling-follow-up | Preflight offers so malformed/offline submissions do not consume tickets | completed | `2b35d6a1` |
| 6-I | managed-signaling-follow-up | Remove exported registry verifier bypass | completed | `2b35d6a1` |
| 7 | paid-relay | Implement TURN/STUN relay MVP with temporary credentials and relay lease | completed | `e9b013e7` |
| 7-A | external | Defer production TURN public IP, DNS, TLS, firewall/ports, cloud account, deployment approval | deferred_external | `e9b013e7` |
| 7-B | paid-relay-self | Harden lease expiry/subscription expiry/path validation | completed | `e9b013e7` |
| 7-C | paid-relay-review | Validate ICE URL schemes and enforce relay session/quota policy | completed | `e9b013e7` |
| 7-D | paid-relay-follow-up | Reject exhausted monthly quota and bind TURN credentials to relay lease IDs | completed | `e9b013e7` |
| 7-E | paid-relay-final | Reject managed relay TURN credentials when relay lease ID is blank | completed | `e9b013e7` |
| 8 | quota | Implement relay quota, active session limit, heartbeat, TTL cleanup, throttling | completed | `f5333362` |
| 8-A | quota-policy | Replace exhausted-quota denial with terminal-friendly throttled relay policy | completed | `f5333362` |
| 8-B | quota-review | Make relay usage heartbeat idempotent | completed | `f5333362` |
| 8-C | quota-review | Enforce relay heartbeat hub authority and online status | completed | `f5333362` |
| 8-D | workflow-review | Refresh Slice 8 workflow review state | completed | `f5333362` |
| 8-C-A | quota-follow-up | Require authenticated hub principal for heartbeat accounting | completed | `f5333362` |
| 9 | daemon-agent | Integrate `termx daemon` cloud bootstrap, hub heartbeat/poll/answer, WebRTC offers | in_progress |  |
| 9-A | daemon-agent-auth | Verify managed offers against machine/app cert, app signature, terminal inventory | completed | `9cc258c3` |
| 9-A-A | daemon-agent-auth-review | Scope managed runtime channel policy to app certificate capabilities | completed | `9cc258c3` |
| 9-A-B | daemon-agent-auth-deferred | Add production ticket/cert revocation verifier seam | deferred | `9cc258c3` |
| 9-A-C | daemon-agent-auth-follow-up | Bind standalone ICE candidates into offer signature verification | completed | `9cc258c3` |
| 9-A-C-A | daemon-agent-auth-validation | Restore missing module checksum for full validation | completed | `9cc258c3` |
| 9-A-D | daemon-agent-auth-final | Record candidate canonicalization/workflow residual risks | completed | `9cc258c3` |
| 9-A-D-A | daemon-agent-auth-final | Harden candidate canonicalization against newline/list-boundary collisions | completed | `9cc258c3` |
| 9-A-D-B | daemon-agent-auth-final | Align Go and TypeScript JSON candidate escaping | completed | `9cc258c3` |
| 9-B | daemon-agent-signaling | Handle hub answer submission failures without silently dropping offers | completed | `02a38d36` |
| 9-B-A | daemon-agent-signaling-review | Retry original generated answer after transient submit failures | completed | `02a38d36` |
| 9-B-A-A | daemon-agent-signaling-self | Treat 204 No Content as successful answer submission | completed | `02a38d36` |
| 9-B-A-B | daemon-agent-signaling-follow-up | Preserve raw offer bytes in pending answer cache key | completed | `02a38d36` |
| 9-C | daemon-agent-config | Load daemon cloud remote bootstrap config without storing secrets | completed | `28e4e8db` |
| 9-C-A | daemon-agent-config-self | Fail closed on malformed config and allow env disable override | completed | `28e4e8db` |
| 9-C-B | daemon-agent-config-self | Prove daemon passes RemoteConfig into server option boundary | completed | `28e4e8db` |
| 9-C-C | daemon-agent-config-review | Honor file `remote.enabled: false` | completed | `28e4e8db` |
| 9-C-D | daemon-agent-config-final | Fail closed on invalid explicit remote enabled values | completed | `28e4e8db` |
| 10 | remote-ui | Connect remote-ui to real Web Control/public_p2p/managed API adapters | in_progress |  |
| 10-A | remote-ui-web-control-api | Add Web Control adapter for public_p2p rendezvous and managed connect tickets | completed | `f199366a` |
| 10-A-A | remote-ui-web-control-api-self | Export adapter without browser type leakage | completed | `f199366a` |
| 10-A-B | remote-ui-web-control-api-self | Include terminal_id creating public_p2p channels | completed | `f199366a` |
| 10-A-C | remote-ui-web-control-api-self | Require terminal_id for public_p2p channel creation | completed | `f199366a` |
| 10-A-D | remote-ui-web-control-api-self | Fail closed when Web Control token is empty | completed | `f199366a` |
| 10-A-E | remote-ui-web-control-api-review | Align public_p2p offer payload with validator | completed | `f199366a` |
| 10-A-F | remote-ui-web-control-api-follow-up | Normalize local offer signatures for rendezvous envelopes | completed | `f199366a` |
| 10-B | remote-ui-managed-hub-api | Add managed Hub HTTP adapter and app-facing session contract seed | completed | `a1a7421f` |
| 10-B-A | remote-ui-managed-hub-api-risk | Record offerer/answerer mismatch, keep slice to signaling contract | completed | `a1a7421f` |
| 10-B-B | remote-ui-managed-hub-api-self | Align Hub one-shot answer response | completed | `a1a7421f` |
| 10-B-C | remote-ui-managed-hub-api-self | Preserve app certificate, signature, candidates through Hub signaling | completed | `a1a7421f` |
| 10-B-D | remote-ui-managed-hub-api-review | Preserve app-provided managed session_id | completed | `a1a7421f` |
| 10-B-E | remote-ui-managed-hub-api-review | Make one-shot timeout recoverable without blindly burning ticket | completed | `a1a7421f` |
| 10-B-F | remote-ui-managed-hub-api-review | Add body-size limit to managed session HTTP endpoints | completed | `a1a7421f` |
| 10-B-G | remote-ui-managed-hub-api-follow-up | Make pending recovery usable by remote-ui and Hub answer lookup | completed | `a1a7421f` |
| 10-B-H | remote-ui-managed-hub-api-follow-up | Add pending answer polling method | completed | `a1a7421f` |
| 10-B-I | remote-ui-managed-hub-api-follow-up | Scope public session-id lookup to ticket and machine | completed | `a1a7421f` |
| 11 | devstack | Build local devstack and optional external smoke runbook | in_progress |  |
| 11-A | devstack-external-smoke | Make web-control + hub + daemon runnable for external managed signaling smoke | completed | `87edb896` |
| 11-A-A | devstack-external-smoke | Defer production DNS/TLS/systemd/firewall/TURN deployment | deferred_external | `87edb896` |
| 11-A-H | devstack-network | Record STUN-only public DataChannel failure as external ICE/TURN/public-port limitation | deferred_external | `87edb896` |
| 11-A-I | devstack-follow-up | Record follow-up review items for canonical golden tests and hub endpoint body caps | deferred | `87edb896` |
| 11-B | web-control-ui-devstack | Serve usable Web Control UI from public devstack for inspection | completed | `eef8a74a` |
| 11-C | devstack-public-daemon | Start temporary public daemon and smoke managed WebRTC through Hub | completed | `79bfe4ce` |
| 11-C-A | devstack-public-daemon-api-follow-up | Record `/api/terminals?machine_id=...` filter mismatch | deferred |  |
| 11-C-C | devstack-public-daemon-token-refresh | Refresh public-host daemon temporary access token | completed | `e9c4f229` |
| 12 | product-alignment | Align docs/AGENTS with APP-first product shape and dev-free relay strategy | completed |  |
| 12-B | tgent-comparison | Compare `../tgent` web/hub/app and write migration strategy plus prompt | completed |  |
| 13 | remote-ui-app-shell | Build APP-first machine list shell | completed | `0014e9a7` |
| 13-A | workflow-unattended-continuation | Harden AGENTS so future slices continue unattended across boundaries | completed | `4f7bf2d1` |
| 14 | remote-ui-qr-store | Define `termx://` QR payload and local MachineStore, rejecting machine private key material | completed | `9c078273` |
| 15 | remote-ui-connection-orchestrator | Implement local/LAN -> public_p2p -> managed orchestration returning only `RtcSession` | completed | `c038442b` |
| 16 | web-control-hub-closed-loop | Implement Hub discover, heartbeat, policy/kick response, force-offline in Web Control | completed | `163c3ee2` |
| 16-A | hub-force-offline-agent-scope | Keep force-offline scoped to one agent without blocking other online agents for the same machine | resolved | `163c3ee2` |
| 16-B | hub-agent-session-bounds | Add TTL cleanup/max bounds to Hub HTTP agent session maps used by policy checks | resolved | `163c3ee2` |
| 17 | daemon-login-hub-select | Implement token/password/device-code daemon login and Hub discovery/selection | completed | `755641be` |
| 17-A | daemon-hub-selection-policy | Add production Hub selection policy using region/health/capacity/expiry/weights | completed | `07eb0bbb` |
| 17-B | external | Defer production OAuth/email/SMS and secure OS keychain/secret storage for CLI/device login | deferred_external |  |
| 18 | stateless-hub-policy-relay | Add Hub policy sync, bounded memory, dev-free managed relay integration | completed | `54aac488` |
| 19 | native-app-rtc-seam | Add native APP `RtcSession` seam without WebRTC type leakage | completed | `119a42e9` |
| 20 | app-devstack-e2e | Validate APP shell / Web Control / stateless Hub / daemon closed loop | completed | `41d7869c` |

## Compressed Completed Slice Summary

- Slices `0`-`8`: Web Control skeleton/auth/machine/certificate/public_p2p/managed signaling/TURN relay/quota foundations are implemented and reviewed. Durable state stays in Web Control; public_p2p remains STUN/rendezvous-only; relay is capability/policy under managed, not a path.
- Slice `9`: Daemon agent cloud bootstrap/offer verification/config slices are partly complete; top-level `9` remains `in_progress` because production login/refresh/hub-select hardening is still pending in later Slice `17`.
- Slice `10`: remote-ui Web Control/public_p2p/managed API adapters are partly complete; top-level `10` remains `in_progress` because APP shell/store/orchestrator integration is still underway.
- Slice `11`: devstack and public-host managed smoke are partly complete; top-level `11` remains `in_progress` because broader APP/devstack e2e is pending in Slice `20`.
- Slice `12`: APP-first product alignment and `tgent-comparison` strategy are completed in docs. Existing uncommitted planning docs may still be in the worktree and should not be reverted.
- Slice `13`: APP-first remote-ui shell completed in `0014e9a7`; workflow hash recorded in `60c34fc1`. It adds `MachineList`, `RemoteAppShell`, and `appMachine` types; first screen is machine list, click enters connection flow, relay is info only.
- Slice `13-A`: root `AGENTS.md` now requires unattended continuation across slice boundaries; committed in `4f7bf2d1` and hash-recorded in `8f8784d0`.
- Historical P2/P3 details before the remote buildout have been intentionally removed from this hot workflow file. Use git history or old commits for archaeology.

## Active Slice Details

### 13 Remote UI APP Machine List Shell

- 状态：completed
- 父条目：none
- 来源：APP-first product direction requires `remote-ui` to open to a compact machine list, not terminal/Web Control.
- 目标：add APP-first shell and machine list; clicking machine enters connection flow before terminal.
- 范围：`remote-ui/src`, `remote-ui/docs/webrtc-rewrite-log.md`, workflow.
- 非目标：QR payload/store, connection orchestrator, native bridge, Web Control terminal UI.
- 外部依赖：none.
- mock 策略：static test fixtures and spy callbacks only.
- 先写的失败测试：`MachineList` and `RemoteAppShell` focused tests for default list, empty state, row metadata, no forbidden wording, click-to-flow behavior.
- 预期失败结果：focused tests failed because modules did not exist.
- 实现摘要：added `appMachine`, `MachineList`, `RemoteAppShell`, package exports.
- 重构摘要：kept `LocalRemoteApp` as local embedded-web harness; new APP shell is independent.
- 运行命令：`cd remote-ui && npm test -- --run src/MachineList.test.tsx src/RemoteAppShell.test.tsx`; `cd remote-ui && npm test`; `cd remote-ui && npm run typecheck`; `cd remote-ui && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`.
- 测试结果：focused tests passed; full remote-ui tests passed 38 files / 183 tests; typecheck/build passed with existing Vite chunk warning.
- subagent review：`Kant`/`Anscombe` explored; `Hypatia` reviewed implementation.
- review 发现：timer cleanup risk; tautological MachineList props/path test; stale next-action/log text.
- review 后修复：timer cleanup added; test changed to source-boundary coverage; workflow/log refreshed.
- 新增派生条目：none.
- deferred human items：none.
- 剩余风险：QR/store, orchestrator, native adapter, e2e remain later slices.
- 下一步：Slice `14`.
- commit：`0014e9a7`

### 13-A Unattended Continuation Rule Hardening

- 状态：completed
- 父条目：13
- 来源：用户要求后续任务无人值守。
- 目标：root `AGENTS.md` states that after slice validation/review/commit/hash, agent continues to next pending slice without asking.
- 范围：`AGENTS.md`, `WORKFLOW.md`.
- 非目标：do not lower TDD/review/validation/commit standards.
- 外部依赖：none.
- mock 策略：not applicable.
- 先写的失败测试：`rg`/workflow guard; old AGENTS lacked explicit slice-to-slice continuation rule.
- 预期失败结果：old AGENTS did not contain the new continuation language.
- 实现摘要：added unattended continuation instructions, including context-compaction recovery from `Next Exact Action`.
- 重构摘要：documentation-only.
- 运行命令：`wc -l docs/remote-rebuild/WORKFLOW.md`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`.
- 测试结果：`WORKFLOW.md` compressed to 240 lines; workflow guard passed; diff whitespace check passed.
- subagent review：`Bohr` (`019ded06-8080-7c13-a026-6917c751b12a`) reviewed compression/unattended-rule slice.
- review 发现：`13-A` was marked completed before hash backfill; review status text conflicted with `none`; guard missed explicit APP-first and dev-free managed relay strings.
- review 后修复：kept `13-A` in review until commit/hash backfill; recorded concrete review findings; added APP-first and dev-free managed relay assertions to workflow guard.
- 新增派生条目：none.
- deferred human items：none.
- 剩余风险：future agents must still stop for high-risk irreversible operations or unmockable external dependencies.
- 下一步：continue Slice `14`.
- commit：`4f7bf2d1`

### 14 Remote UI QR Pairing Payload And Machine Store

- 状态：completed
- 父条目：none
- 来源：APP-first flow needs scan/add to store machine/service info from `termx` CLI QR.
- 目标：define `termx://` QR payload schema and local `MachineStore`; parse/save local/LAN/public/control/hub/pairing metadata; reject machine private key; keep app private key only as key-store reference; support schema version compatibility.
- 范围：`remote-ui/src` parser/store/types/tests, `remote-ui/docs/webrtc-rewrite-log.md`, `WORKFLOW.md`.
- 非目标：camera UI, native secure storage implementation, connection orchestrator, machine private key upload/export, relay as path.
- 外部依赖：none; real native secure storage remains future native APP slice.
- mock 策略：use in-memory key-value storage and fake app key-store reference; do not mock parser validation or private-key rejection.
- 先写的失败测试：`remote-ui/src/pairingPayload.test.ts` and `remote-ui/src/machineStore.test.ts` cover parsing `termx://pair?...` and JSON payloads, saving local/LAN/public/control/hub/pairing metadata, rejecting machine/app/generic/JWK private-key-shaped fields, rejecting contaminated local storage reads, storing only app key refs, rejecting `relay` as a path, and accepting v1 payloads while normalizing to current store shape.
- 预期失败结果：`cd remote-ui && npm test -- --run src/pairingPayload.test.ts src/machineStore.test.ts` failed because `./pairingPayload` / `./machineStore` modules did not exist.
- 实现摘要：added `remote-ui/src/pairingPayload.ts` and `remote-ui/src/machineStore.ts`, exported parser/store from `src/index.ts`, and logged Slice 14 in `remote-ui/docs/webrtc-rewrite-log.md`.
- 重构摘要：removed Node `Buffer` fallback from browser-facing parser after typecheck; added UTF-8 QR decoding; hardened parser/store to fail closed on generic private-key and JWK private material; kept APP/store code platform-neutral and storage-based.
- 运行命令：`cd remote-ui && npm test -- --run src/pairingPayload.test.ts src/machineStore.test.ts`; `cd remote-ui && npm run typecheck`; `cd remote-ui && npm test`; `cd remote-ui && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`.
- 测试结果：focused tests passed 2 files / 9 tests after review fixes; full remote-ui tests passed 40 files / 192 tests; typecheck passed; build passed with existing Vite chunk-size warning; workflow guard and diff whitespace check passed.
- subagent review：`Popper` (`019ded0f-bc8d-7871-b18a-3225bb003e9a`) reviewed Slice 14 implementation.
- review 发现：parser accepted generic private-key/JWK fields; MachineStore read path did not reject contaminated persisted private keys; app-private-key store test threw in parser rather than MachineStore; workflow/log contained stale Slice 13-A and next-step text.
- review 后修复：parser/store now reject generic private-key and JWK private material; MachineStore rejects contaminated storage on read; app-private-key test exercises `saveMachine`; workflow/log stale text refreshed.
- 新增派生条目：none yet.
- deferred human items：real native secure storage integration remains future native APP slice.
- 剩余风险：native secure storage, camera scan UI, connection orchestration, and e2e remain future slices.
- 下一步：Slice `15`.
- commit：`9c078273`

### 15 Remote UI Connection Orchestrator

- 状态：completed
- 父条目：none
- 来源：APP machine click must enter a connection state machine that tries local/LAN, then `public_p2p`, then `managed`, while terminal/file/api/events receive only `RtcSession`.
- 目标：add a platform-neutral orchestrator that invokes existing connectors in order, emits APP-facing stage snapshots, stops after the first success, carries relay only as managed connection info/capability, and returns `RtcSession`.
- 范围：`remote-ui/src` orchestrator/types/tests, exports, `remote-ui/docs/webrtc-rewrite-log.md`, `WORKFLOW.md`.
- 非目标：camera scan UI, native WebRTC adapter, actual network connectors beyond existing interfaces, Web Control terminal UI, e2e devstack.
- 外部依赖：none.
- mock 策略：use fake `RtcConnector` implementations and fake `RtcSession` objects behind the public connector/session interfaces; no HTTP/WebSocket/runtime mocks in business layer.
- 先写的失败测试：`remote-ui/src/connectionOrchestrator.test.ts` covers local success skipping later paths, local failure then public success, public failure then managed success, public_p2p relay rejection, abort stop behavior, post-connect validation disconnect, managed relay as info not path, all-path failure snapshots, and returned session being the only runtime object.
- 预期失败结果：`cd remote-ui && npm test -- --run src/connectionOrchestrator.test.ts` failed because `./connectionOrchestrator` module did not exist.
- 实现摘要：added `remote-ui/src/connectionOrchestrator.ts` and exported it from `src/index.ts`; orchestrator tries local, then `public_p2p`, then `managed`, and returns only `RtcSession`.
- 重构摘要：normalized missing relay info to `false` on success snapshots/results; rejected relay usage outside `managed`; abort now stops orchestration instead of becoming a path failure; sessions created before failed post-connect validation are disconnected; fixed test mock type completeness after typecheck.
- 运行命令：`cd remote-ui && npm test -- --run src/connectionOrchestrator.test.ts`; `cd remote-ui && npm run typecheck`; `cd remote-ui && npm test`; `cd remote-ui && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`.
- 测试结果：focused test failed first because module did not exist, then passed 1 file / 8 tests after review fixes; full remote-ui tests passed 41 files / 200 tests; typecheck passed; build passed with existing Vite chunk-size warning; workflow guard and diff whitespace check passed.
- subagent review：`Schrodinger` (`019ded1b-bc05-77a2-895e-f94f1257758a`) reviewed Slice 15 implementation.
- review 发现：orchestrator accepted relay usage from non-managed paths; abort after connect could be swallowed as normal path failure; post-connect validation failure could leave created sessions open; workflow next-step text was stale.
- review 后修复：non-managed relay usage is rejected and disconnected; abort errors are rethrown and stop fallback; failed post-connect validation disconnects created sessions; workflow/log next-step text refreshed.
- 新增派生条目：none yet.
- deferred human items：none.
- 剩余风险：native adapter, retry/backoff UX policy, and e2e remain future slices.
- 下一步：Slice `16`.
- commit：`c038442b`

### 16 Web Control / Hub Closed Loop

- 状态：completed
- 父条目：none
- 来源：daemon must register through Web Control, receive Hub list/policy, Hub reports registry/capacity/health, and Web Control can force agents offline without turning into terminal UI.
- 目标：add Web Control durable hub/agent registry state and Hub policy/kick/force-offline integration while keeping Hub stateless and bounded TTL-only.
- 范围：`web-control/internal` service/store/httpapi tests and implementation, `termx-hub/internal` registry/httpapi/controlclient tests and implementation, workflow docs.
- 非目标：terminal/file runtime, Web Control terminal UI, daemon login/device-code implementation, production DNS/TLS/systemd/TURN deployment.
- 外部依赖：none; production hub identity/signing/rotation remains existing deferred external.
- mock 策略：use shared-secret hub HTTP auth and fake in-process control clients; no production cloud/provider dependency.
- 先写的失败测试：`web-control/internal/hubregistry/service_test.go` covers durable hub report/discover, agent report policy, force-offline ownership, and TTL cleanup; `termx-hub/internal/registry/registry_test.go` covers force-offline rejecting heartbeat/poll/offer and expiring in bounded memory; HTTP red tests now cover Web Control hub report/discover/force-offline/policy endpoints and Hub heartbeat/poll policy rejection.
- 预期失败结果：`cd web-control && GOWORK=off go test ./internal/hubregistry` failed because package had no implementation; `cd termx-hub && GOWORK=off go test ./internal/registry` failed because `ForceOffline` API/error types did not exist. Service/registry implementation was then added and both focused packages passed. `cd web-control && GOWORK=off go test ./internal/httpapi -run TestHubReportDiscoverAndForceOfflineHTTP` failed because `httpapi.Config` lacked `HubRegistry` and the test helper lacked `getJSON`. `cd termx-hub && GOWORK=off go test ./internal/httpapi -run TestAgentHTTPAppliesForceOfflinePolicy` first failed because `termx-hub/go.mod` did not declare the existing local `termx-core` dependency required by `hubv1`; fix is needed before the HTTP policy red test can compile.
- 实现摘要：added Web Control durable `hubregistry` service/schema plus `/api/v1/hubs`, `/api/v1/hub/report`, `/api/v1/hub/agents/policy`, and owner force-offline HTTP endpoints; added Hub stateless force-offline TTL map in `registry`; added Hub controlclient policy lookup and handler-side heartbeat/poll/answer force-offline rejection.
- 重构摘要：factored shared Hub auth helper in Web Control HTTP handlers; kept force-offline in Hub registry as agent-scoped TTL state after `16-A`; removed unused handler session-delete helper.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/hubregistry`; `cd termx-hub && GOWORK=off go test ./internal/registry`; `cd web-control && GOWORK=off go test ./internal/httpapi -run TestHubReportDiscoverAndForceOfflineHTTP`; `cd termx-hub && GOWORK=off go test ./internal/httpapi -run TestAgentHTTPAppliesForceOfflinePolicy`; `cd termx-hub && GOWORK=off go test ./internal/controlclient -run TestManagedTicketVerifierFetchesAgentPolicyThroughWebControl`; `cd web-control && GOWORK=off go test ./...`; `cd termx-hub && GOWORK=off go test ./...`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`.
- 测试结果：focused tests, full `web-control` tests, full `termx-hub` tests, workflow guard, and diff whitespace check pass after `16-A`/`16-B` and review fixes.
- subagent review：attempted parallel explorers but `spawn_agent` returned thread limit before implementation; completed review still required after slice, retry after closing completed agents or record forced self-review if unavailable.
- review 发现：`Newton` reported machine-wide force-offline offer blocking, missing Hub production cleanup/bounds for force-offline and agent session maps, forced agents being durably re-marked online by reports, uncapped hub report TTL, unbound controlclient policy response identity, and missing hub report body/agent-count backpressure.
- review 后修复：resolved `16-A` and `16-B`; added Web Control hub TTL cap, agent batch cap, body limit, and forced durable offline status preservation; added Hub HTTP agent session TTL/max bounds and cleanup loop wiring; added controlclient policy identity check.
- 新增派生条目：`16-A` resolved agent-scope force-offline bug found during local diff review.
- deferred human items：production hub identity/signing/rotation remains existing deferred external.
- 剩余风险：daemon login/hub selection and e2e remain later slices.
- 下一步：run broader Web Control/Hub validations, perform review, fix findings, commit, and hash backfill.
- commit：`163c3ee2`

### 17 Daemon Login And Hub Discovery

- 状态：completed
- 父条目：none
- 来源：`termx daemon` must login/register to Web Control, receive Hub list, and avoid uploading machine private keys.
- 目标：add minimal token/password/device-code login and Hub discovery/selection seams for daemon/CLI without putting Web Control business logic into CLI/core.
- 范围：inspect then likely `web-control/internal` auth/device-code endpoints, `termx-core/internal/remote` cloud config/runtime, `termx-cli/cmd/termx` login command wiring, workflow docs.
- 非目标：terminal/file runtime, production OAuth/email/SMS, real device store encryption, subscription/entitlement.
- 外部依赖：real OAuth/email/SMS remains deferred external; use local provider/stub where needed.
- mock 策略：HTTP test servers and provider interfaces; tests must assert machine private key is rejected/not uploaded.
- 只读探索：CLI 目前只有 `remote` local/status/pair/open 命令和只读 remote config parser；Web Control 已有 login/refresh/me、daemon register、`/api/v1/hubs`，但缺 device-code flow，core manager 还不会在 control 注册后自动 discover/select Hub。
- 先写的失败测试：
  - `termx-core/internal/remote/discovery`：`DiscoverHubs` must call `GET /api/v1/hubs` with bearer token and return online Hub HTTP URLs.
  - `termx-core/internal/remote/runtime`：manager with `ControlURL` + token but no `HubURL` must register in Web Control, discover hubs, select one, then register to Hub.
  - `web-control/internal/deviceauth` and `web-control/internal/httpapi`：device code create/pending/approve/poll/expire/reject, one-time token exchange, authenticated confirm/reject, and hashed code storage.
  - `termx-cli/cmd/termx`：`remote login` token/password/device-code must validate or exchange credentials through Web Control, persist daemon bootstrap without writing raw tokens into `termx.yaml`, and let daemon config load the stored token.
- 预期失败结果：focused tests failed as expected: `DiscoverHubs` undefined; manager stayed `configured` after control registration without Hub discovery; `web-control/internal/deviceauth` has no implementation; CLI login tests need new command/auth-store seams.
- 实现摘要：added Web Control device-code service/routes with hashed code storage, one-time exchange, TTL/retention cleanup, active-code cap, bad-attempt lockout, authenticated confirm/reject; added core `DiscoverHubs` and manager selection after control registration; added CLI `remote login token/password/device-code`, auth-store bootstrap, and daemon config loading from auth store.
- 重构摘要：added `account.IssueForUserIDInTx` so device-code consumption and token/session issuance share one SQLite transaction; added CLI secret-source flags (`--token-env`, `--token-file`, `--password-env`, `--password-file`) and kept raw tokens out of `termx.yaml`.
- 运行命令：`cd termx-core && GOWORK=off go test ./internal/remote/discovery ./internal/remote/runtime -run 'TestDiscoverHubsUsesBearerTokenAndReturnsHubList|TestManagerDiscoversAndSelectsHubAfterControlRegistration'`; `cd web-control && GOWORK=off go test ./internal/deviceauth ./internal/httpapi -run 'TestDeviceCodeApprovePollRejectExpireAndHashStorage|TestDeviceAuthHTTPFlow'`; `cd termx-cli && GOWORK=off go test ./cmd/termx -run 'TestRemoteLogin'`; `cd termx-core && GOWORK=off go test ./...`; `cd web-control && GOWORK=off go test ./...`; `cd termx-cli && GOWORK=off go test ./...`; `bash docs/remote-rebuild/check_workflow_rules.sh`.
- 测试结果：focused red tests failed first as expected; implementation focused tests passed; final broader validations passed after review fixes.
- subagent review：`Tesla` (`019ded5a-ed71-7731-b780-1406a1196ef9`) reviewed Slice 17.
- review 发现：unbounded unauthenticated device-code creates; 32-bit user-code and no attempt throttle; device-code consumed before token/session issuance; CLI token/password flags leak via argv; Hub selection is first-online only; CLI go.mod tidy/replace churn needs documentation.
- review 后修复：added active code cap, higher-entropy user codes, bad-attempt lockout, retention cleanup wired to Web Control cleanup loop, atomic consume+token issuance, env/file secret inputs for CLI; documented remaining Hub selection and secure keychain limits as deferred/follow-up.
- 新增派生条目：`17-A` pending for production Hub selection policy; `17-B` deferred_external for OS keychain/secret storage and production OAuth/email/SMS.
- deferred human items：real OAuth provider, email/SMS, secure OS keychain integration remain deferred external.
- 剩余风险：Hub selection is still simple first-online; production OAuth/email/SMS/OS keychain remain deferred; CLI `--token`/`--password` remain dev-compatible but env/file inputs are preferred.
- 下一步：commit, hash backfill, then continue Slice `18`.
- commit：`755641be`

### 16-A Hub Force Offline Agent Scope

- 状态：resolved
- 父条目：16
- 来源：local diff review found registry offer preflight blocked the entire machine when any one agent had an active force-offline policy.
- 是否阻塞父条目：yes.
- 目标：force-offline remains agent-scoped; a different online agent for the same machine can still receive managed offers while the forced agent is rejected.
- 先写的失败测试：`termx-hub/internal/registry/registry_test.go` added `TestForceOfflineDoesNotBlockOtherAgentsForSameMachine`.
- 预期失败结果：`cd termx-hub && GOWORK=off go test ./internal/registry -run TestForceOfflineDoesNotBlockOtherAgentsForSameMachine` failed with `agent forced offline` on app offer submission.
- 实现摘要：removed machine-wide force-offline offer rejection; offer preflight now accepts another non-forced online agent and returns `ErrAgentForcedOffline` only when no usable agent remains and the machine has active force-offline state.
- 测试结果：focused force-offline registry tests pass.
- 解决方式：agent-scoped policy preserved; no relay/path taxonomy changes.

### 16-B Hub Agent Session Bounds

- 状态：resolved
- 父条目：16
- 来源：forced self-review while waiting for subagent review found `httpapi.agentSessions` maps used by Slice 16 policy checks had no TTL cleanup or max bound.
- 是否阻塞父条目：yes.
- 目标：bound Hub HTTP agent session maps without adding durable state or terminal runtime.
- 先写的失败测试：`TestAgentHTTPSessionsAreTTLBounded` covers max-session eviction and TTL expiry through diagnostics/heartbeat; `TestHubHandlerCleanupLoopRemovesExpiredRegistryState` checks handler cleanup loop exposure/stop; controlclient mismatch and Web Control TTL/status/cap tests cover review findings.
- 预期失败结果：review showed existing maps had no TTL/max and production handler had no cleanup path.
- 实现摘要：`httpapi.Handler` now exposes `StartCleanup`, agent sessions carry last-seen/expires-at and enforce TTL/max eviction, and `main` starts cleanup ticker for registry/session TTL state.
- 测试结果：focused Hub HTTP/controlclient/cmd and Web Control hubregistry/httpapi tests pass.
- 解决方式：Hub remains stateless with bounded TTL memory; no DB added to Hub.

### 18 Stateless Hub Policy Relay

- 状态：completed
- 父条目：none
- 来源：development policy requires registered/dev users to receive managed relay capability so the managed path can be proven before billing/entitlement gates return.
- 目标：sync Web Control policy to Hub and expose dev-free managed relay capability/ICE info without adding a `relay` client path or leaking TURN credentials into `public_p2p`.
- 范围：inspect then likely `web-control/internal/connect` managed ticket/relay policy, `termx-hub/internal/httpapi` policy/capability cache, bounded memory/cleanup tests, workflow docs.
- 非目标：production billing/entitlement, real TURN cloud deployment, terminal/file runtime changes, Web Control terminal UI.
- 外部依赖：production TURN public IP/DNS/TLS/firewall/cloud account remains deferred external; use existing local/mock TURN credential seams.
- mock 策略：real SQLite for Web Control policy; Hub keeps bounded TTL memory only; tests must assert path remains `managed`.
- 只读探索：Web Control `connect.CreateManagedTicket` still inserts `allow_relay = 0`; HTTP managed-ticket and hub-ticket tests assert old no-relay free-plan behavior. Hub `managed.Service.SubmitOffer` and agent poll hardcode relay capability false; `ice.Service` already safely emits TURN only for `PathManaged && AllowRelay` and keeps `public_p2p` STUN-only. Daemon currently answers with registration-level ICE servers only, so session-specific managed TURN must be carried as neutral signaling `rtc_config`, not browser/native WebRTC types.
- 先写的失败测试：revise Web Control managed ticket tests to expect dev-free managed relay capability but no TURN/runtime data; revise Hub managed/httpapi tests to expect `AllowRelay` from verified managed tickets, managed-only TURN ICE info in app session and agent poll responses, no `path:"relay"`; add managed service TTL/max cleanup coverage; add core manager coverage for session-specific managed ICE config.
- 预期失败结果：focused tests failed as expected: Web Control managed tickets/checks returned `allow_relay:false`; Web Control relay lease denied registered-free users; Hub compile failed because `managed.Config` lacks `OfferTTL`/`MaxOffers`, `managed.Service` lacks `CleanupExpired`, and `httpapi.Config` lacks `ICE`; core manager answered with registration-level ICE instead of offer-level managed RTC config.
- 实现摘要：Web Control managed ticket policy now uses a provider-backed dev-free managed relay capability and still writes `path = managed`; relay lease policy falls back to configurable dev-free managed quota/session limits when no active paid policy exists. Hub managed signaling now carries verified ticket relay capability through offer/poll/session responses, generates managed-only ICE config via existing `ice.Service`, wires local TURN env into Hub session ICE generation, and adds TTL/max cleanup for managed offer policy maps. Core daemon now prefers offer-scoped managed RTC config over registration-level ICE servers, and the e2e smoke tool accepts managed relay capability.
- 重构摘要：added neutral `hubv1.RTCConfig` to signaling offer/registration types; kept TURN credentials under managed `rtc_config` / `ice_servers`; kept `public_p2p` STUN-only guards unchanged; made dev-free relay fallback configurable for future entitlement gating; no Hub DB added.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/connect ./internal/httpapi ./internal/relay -run 'TestManagedTicketOwnerPolicyAndDevFreeRelayCapability|TestManagedConnectTicketHTTPFlow|TestHubManagedTicketCheckAndConsumeHTTP|TestRelayLeasePolicyAllowsDevFreeAndPaidManagedLease|TestPublicP2PRendezvousHTTPFlow'`; `cd termx-hub && GOWORK=off go test ./internal/managed ./internal/httpapi ./internal/ice -run 'TestManagedSignalingSurfacesManagedRelayCapability|TestManagedOfferPolicyCacheIsTTLBounded|TestManagedSessionHTTPContract|TestAgentHTTPRegisterHeartbeatPollAndAnswer|TestManagedWithoutRelayAndPublicP2PDoNotReceiveTurnCredentials'`; `cd termx-core && GOWORK=off go test ./internal/remote/runtime -run TestManagerUsesOfferScopedManagedRTCConfig`.
- 测试结果：focused red tests failed as expected, then passed after implementation; broader `web-control`, `termx-hub`, and `termx-core` package tests passed after updating expired-subscription relay lease expectations to dev-free fallback. Final validation passed: `web-control`, `termx-hub`, `termx-core` full package tests; workflow guard; diff whitespace check.
- subagent review：`Russell` reviewed Slice 18 diff.
- review 发现：managed TURN path bypassed relay quota/session enforcement; real Hub binary initially lacked ICE/TURN service wiring; session answer paths could consume a ticket before ICE response assembly; `termx-remote-e2e` still rejected `allow_relay:true`.
- review 后修复：made relay dev-free fallback configurable and covered gate/custom quota tests; wired Hub TURN env into `ice.Service` while keeping registration STUN-only; preflighted ICE response before consuming managed tickets; updated smoke tool to accept managed relay and apply Hub-returned ICE servers.
- 新增派生条目：none yet.
- deferred human items：production TURN/DNS/TLS/firewall/cloud account and billing/entitlement providers remain deferred external.
- 剩余风险：production TURN public DNS/TLS/firewall/cloud deployment still deferred; production entitlement/billing provider remains deferred; managed relay sessions are now capability/ICE enabled for dev but full end-to-end accounting heartbeat through Hub/Web Control should be exercised in Slice 20.
- 下一步：commit and hash backfill, then continue Slice `19`.
- commit：`54aac488`

### 19 Native APP RtcSession Seam

- 状态：completed
- 父条目：none
- 来源：APP-first remote-ui needs a native bridge seam so mobile/native WebRTC can implement `RtcSession` without leaking platform/browser/native types into public machine/orchestrator business layers.
- 目标：add a minimal APP bridge contract around `RtcSession`/connection targets, with tests proving browser `RTCPeerConnection`/`RTCDataChannel` and native WebRTC types stay out of common remote-ui business modules.
- 范围：`remote-ui/src/bridgeRtcSession.ts`, bridge/public P2P tests, `remote-ui/docs/webrtc-rewrite-log.md`, `WORKFLOW.md`.
- 非目标：actual iOS/Android WebRTC implementation, camera QR UI, app store signing, Web Control terminal UI, devstack e2e.
- 外部依赖：real native secure storage, app store signing, native WebRTC runtime packaging remain deferred external.
- mock 策略：use fake bridge/provider interfaces returning existing `RtcSession`; no browser/native WebRTC mocks in common business layer.
- 先写的失败测试：added focused `remote-ui/src/bridgeRtcSession.test.ts` coverage for bridge offer/answerer behavior, relay/path fail-closed behavior, connector interoperability, runtime channel delegation, capability updater behavior, source-boundary leakage, and barrel export shape. Added `publicP2pRtcConnector.test.ts` red test rejecting `turn:` from public rendezvous before offer creation.
- 预期失败结果：first `cd remote-ui && npm test -- --run src/nativeRtcSession.test.ts` failed because module did not exist; after subagent findings, revised to `bridgeRtcSession` and `public_p2p` STUN-only focused test failed because `publicP2pRtcConnector` forwarded `turn:` URLs into `createOffer`.
- 实现摘要：added `remote-ui/src/bridgeRtcSession.ts` with neutral `BridgeRtcSessionAdapter` implementing `RtcSession & RtcSessionNegotiator & RtcSessionAnswerer & RtcSessionCapabilityUpdater`; kept it out of `src/index.ts` barrel exports; hardened `publicP2pRtcConnector` so public rendezvous ICE servers must be `stun:`/`stuns:`.
- 重构摘要：renamed initial native-specific seam to neutral bridge seam after review; common business modules and public barrel do not import/export bridge/native/browser runtime details; relay validation remains managed-only capability/info.
- 运行命令：`cd remote-ui && npm test -- --run src/nativeRtcSession.test.ts` failed first; `cd remote-ui && npm test -- --run src/bridgeRtcSession.test.ts src/publicP2pRtcConnector.test.ts` failed on public_p2p TURN leakage, then passed 2 files / 12 tests; `cd remote-ui && npm run typecheck`; `cd remote-ui && npm test`; `cd remote-ui && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`.
- 测试结果：focused bridge/public P2P tests passed; typecheck passed; full remote-ui tests passed 42 files / 206 tests; build passed with existing Vite chunk-size warning; workflow guard and diff whitespace check passed.
- subagent review：initial explorer spawn failed with agent thread limit; closed completed historical agents and started explorer `Meitner` (`019ded95-26d5-7373-8d72-eca7824cffcf`) for read-only native seam boundary review.
- review 发现：native-specific public API naming/export can leak into common business layer; `publicP2pRtcConnector` accepted `turn:` URLs in `public_p2p` rendezvous; capability updater must be explicit for server-negotiated policy.
- review 后修复：reworked seam to neutral `BridgeRtcSessionAdapter`, removed public barrel export, added business-source guards, made bridge session implement `RtcSessionCapabilityUpdater`, rejected non-STUN public P2P ICE servers before offer creation, and tightened the test so invalid public P2P ICE is rejected before creating a runtime session.
- 新增派生条目：none yet.
- deferred human items：native platform packaging/signing and production secure storage remain deferred external.
- 剩余风险：actual native WebRTC packaging/secure storage remains deferred; app/devstack e2e remains Slice 20.
- 下一步：hash backfill, then start Slice `20`.
- commit：`119a42e9`

### 20 APP / Remote UI Devstack E2E

- 状态：completed
- 父条目：none
- 来源：APP-first product conclusion requires an end-to-end proof that scanned/stored machine records drive `local -> public_p2p -> managed` connection flow and that terminal/file/api/events receive only `RtcSession`.
- 目标：add a repeatable APP shell e2e harness first, then optionally run the public devstack after runbook/workflow records are updated.
- 范围：first local/focused `remote-ui/src` tests and runbook/workflow docs; optional later public devstack commands may touch no repo files unless fixes are found.
- 非目标：new Web Control terminal UI, HTTP/WebSocket runtime fallback, production DNS/TLS/systemd/TURN deployment, native app packaging/signing.
- 外部依赖：public server `root@114.66.58.243` may be used only after this section/runbook explains why, temp paths, start/stop/cleanup, and residual state; production cloud/TURN/DNS/app-store items stay deferred external.
- mock 策略：local APP e2e uses real `RemoteAppShell`, `MachineStore`, and `ConnectionOrchestrator` with fake `RtcConnector`/`RtcSession` implementations at provider boundaries; not a tautological hook-only test.
- 先写的失败测试：added `remote-ui/src/appConnectionE2E.test.tsx` to load a scanned/stored machine record, render the APP machine list, click the machine, exercise local fail -> public_p2p fail -> managed success through `ConnectionOrchestrator`, and prove terminal/file/api/events consumers receive the returned `RtcSession`.
- 预期失败结果：first focused run failed while constructing the test payload because `parsePairingPayload` correctly requires a QR/JSON string, not an object; fixed the test to use a real JSON QR payload string before accepting the harness result.
- 实现摘要：added local APP e2e harness test only; no product code change was required for the local APP/store/orchestrator seam.
- 重构摘要：kept fake objects behind `RtcConnector`/`RtcSession` provider interfaces; no HTTP/WebSocket runtime fallback; no relay path taxonomy.
- 运行命令：`cd remote-ui && npm test -- --run src/appConnectionE2E.test.tsx` failed first on invalid test payload shape, then passed 1 file / 1 test; `cd remote-ui && npm test`; `cd remote-ui && npm run typecheck`; `cd remote-ui && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`; scoped public checks over `ssh root@114.66.58.243`; local `/tmp/termx-devstack-build/termx-remote-e2e` with public daemon pair tunnel.
- 测试结果：focused local APP e2e passed; full remote-ui tests passed 43 files / 207 tests; typecheck passed after using store lookup for metadata; build passed with existing Vite chunk-size warning; workflow guard and diff whitespace check passed. Public devstack smoke passed after refreshing only the temporary public-daemon token and recreating terminal `1`.
- subagent review：`Hume` (`019deda5-08d3-7623-9cf1-5e9dd854c5f0`) reviewed Slice 20 local harness.
- review 发现：runtime consumer coverage was too tautological because it directly called fake `RtcSession`; stored QR metadata was not clearly driving connector inputs; public devstack smoke is still needed unless Slice 20 is downgraded to local-harness-only.
- review 后修复：local harness now renders a runtime consumer using real `useTerminalSession` and `useFileManager`, subscribes to events via `RtcSession`, uses stored local/public/control/hub/pairing/bootstrap metadata to build connector inputs, keeps fake implementations only at `RtcConnector`/`RtcSession` boundaries, and ran scoped public devstack smoke for Web Control/Hub/daemon closed-loop coverage.
- 新增派生条目：none yet.
- deferred human items：production DNS/TLS/TURN/app-store signing remain deferred external.
- 剩余风险：public devstack tokens can expire again; production DNS/TLS/TURN/systemd/app-store signing remain deferred external.
- 下一步：hash backfill, then continue next pending remote rebuild todo.
- commit：`41d7869c`

### 17-A Daemon Hub Selection Policy

- 状态：completed
- 父条目：17
- 来源：Slice 17 review found daemon Hub selection still chooses the first online Hub and does not apply expiry/health/capacity/region/weight policy.
- 目标：make daemon Hub selection production-shaped: filter expired/offline/unhealthy/zero-capacity hubs, prefer requested/daemon region when available, and rank by health/capacity/weight without introducing terminal runtime HTTP.
- 范围：`termx-core/internal/remote/discovery` / runtime selection code and tests; `web-control/internal/hubregistry` / `httpapi` / SQLite migration for durable `weight`; `termx-cli` remote config/bootstrap region handling; `WORKFLOW.md`.
- 非目标：real geo DNS, cloud load balancer, persistent Hub state in `termx-hub`, terminal/file/api/events runtime changes, full Hub telemetry reporter.
- 外部依赖：real region mapping / cloud capacity telemetry remains deferred external; use provider fields already returned by Web Control where present.
- mock 策略：service/client tests use static hub records through discovery/registry interfaces; no fake terminal runtime. Hub weight ingestion is covered at Web Control registry/API boundary and remains provider-shaped for future Hub telemetry.
- 只读探索：core daemon currently selects the first hub whose `http_url` is non-empty and `status == online`; `DiscoverHubs` parses region/capacity/health/expires_at but not weight. Web Control hub registry stores/returns region/capacity/health/expires_at, but has no `weight` column/API field. No existing daemon preferred-region config field was found in env/file config.
- 先写的失败测试：added Web Control hub registry/httpapi/discovery-client assertions for `weight`; added core runtime selection tests covering expired/offline/unhealthy/zero-capacity filtering, preferred-region ranking, and weight/capacity fallback. Next add CLI region/bootstrap tests so login does not freeze the first discovered HubURL.
- 预期失败结果：focused tests failed as expected: missing `weight` fields/column/API response; missing `selectDiscoveredHub` and selector options.
- 实现摘要：Web Control hub registry now persists and returns `weight` through SQLite migration, service model, report HTTP request, and `/api/v1/hubs` discovery response. Core discovery parses `weight`; daemon selection now filters missing URL/offline/expired/unhealthy/zero-capacity hubs and ranks preferred region first, then weight, capacity, later expiry, stable ID. CLI remote config now reads optional `region` from env/file, login no longer freezes the first discovered HubURL into config, and legacy auth-store `hub_url` no longer bypasses runtime discovery.
- 重构摘要：selection logic is kept in core runtime helper functions with deterministic tests; health parsing remains JSON-only and fail-closed for malformed health; explicit `remote.hubURL` remains a manual override, while discovery-selected Hub URLs are re-evaluated on later reconcile and stop the old signaling loop when switching. Hub remains stateless with no DB or durable source of truth.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/store ./internal/hubregistry ./internal/httpapi -run 'TestOpenAndMigrateSQLiteCreatesCoreTables|TestMigrateUpgradesMachineClaimTokenSchema|TestHubReportDiscoverForceOfflineAndCleanup|TestHubReportDiscoverAndForceOfflineHTTP'`; `cd termx-core && GOWORK=off go test ./internal/remote/discovery ./internal/remote/runtime -run 'TestDiscoverHubsUsesBearerTokenAndReturnsHubList|TestManagerReevaluatesDiscoveredHubOnReconcile|TestManagerDiscoversHubUsingRegionAndWeightPolicy|TestSelectDiscoveredHub|TestManagerDiscoversAndSelectsHubAfterControlRegistration'`; `cd termx-cli && GOWORK=off go test ./cmd/termx -run 'TestRemoteConfigFromEnv|TestRemoteConfigFromFileLoadsCloudBootstrapWithoutRawToken|TestRemoteConfigEnvOverridesFile|TestRemoteConfigIgnoresLegacyAuthStoreHubURL|TestRemoteLoginTokenPersistsBootstrapOutsideConfigFile'`; `cd termx-core && GOWORK=off go test ./...`; `cd web-control && GOWORK=off go test ./...`; `cd termx-cli && GOWORK=off go test ./...`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`.
- 测试结果：focused tests failed first as expected on missing `weight` field/column/API and missing selector helper. Review regression tests then failed as expected for pinned discovered Hub and legacy auth-store `hub_url`; after fixes all focused tests passed. Broader `termx-core`, `web-control`, and `termx-cli` package tests passed; workflow guard and diff check passed.
- subagent review：`Euler` (`019dedc1-e2e3-7071-bc80-5171eeea7a7d`) reviewed Slice 17-A.
- review 发现：discovery-selected Hub was pinned forever and not re-evaluated after expiry/health/capacity changes; legacy auth-store `hub_url` bypassed the new selection policy; workflow next action was stale.
- review 后修复：added failing tests for post-selection rediscovery and legacy auth-store bypass; reworked manager to distinguish explicit HubURL override from discovery-selected HubURL, reselect discovered Hubs on reconcile, stop old signaling loop on switch, and ignore legacy auth-store `hub_url`; refreshed workflow next action.
- 新增派生条目：none yet.
- deferred human items：production geo/region source and cloud capacity telemetry remain deferred external.
- 剩余风险：real production region mapping and dynamic Hub capacity/weight telemetry source remain deferred external.
- 下一步：continue next pending remote rebuild todo.
- commit：`07eb0bbb`

## Deferred External / Human Items

- Real payment/subscription/invoice/tax/fraud integrations remain `deferred_external` behind provider interfaces.
- Real email/SMS/OAuth provider setup remains `deferred_external`.
- Production DNS/TLS/cloud accounts/TURN public deployment/firewall/systemd/app-store signing remain `deferred_external`.
- Public devstack host can be used at `ssh root@114.66.58.243` only after recording reason, temp path, start/stop/cleanup commands, and residual state.

## Next Exact Action

1. Select the next pending remote rebuild todo from the ordered table, update this workflow, then run that slice TDD-first.
