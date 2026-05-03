# TermX Remote Rebuild Workflow

Compressed status file for unattended remote rebuild work. Keep this file short enough to reload after context compaction. Historical details are intentionally summarized here; recover full old detail from git history before `2026-05-03T16:40:26+08:00` if needed.

## Current State

- Current phase: Remote Web / Hub / Agent Buildout.
- Active todo: `14` QR payload/store.
- Last updated: 2026-05-03T17:04:56+08:00.
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
| 14 | remote-ui-qr-store | Define `termx://` QR payload and local MachineStore, rejecting machine private key material | in_progress |  |
| 15 | remote-ui-connection-orchestrator | Implement local/LAN -> public_p2p -> managed orchestration returning only `RtcSession` | pending |  |
| 16 | web-control-hub-closed-loop | Implement Hub discover, heartbeat, policy/kick response, force-offline in Web Control | pending |  |
| 17 | daemon-login-hub-select | Implement token/password/device-code daemon login and Hub discovery/selection | pending |  |
| 18 | stateless-hub-policy-relay | Add Hub policy sync, bounded memory, dev-free managed relay integration | pending |  |
| 19 | native-app-rtc-seam | Add native APP `RtcSession` seam without WebRTC type leakage | pending |  |
| 20 | app-devstack-e2e | Validate APP shell / Web Control / stateless Hub / daemon closed loop | pending |  |

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

- 状态：in_progress
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
- 下一步：finish post-review validation, commit Slice `14`, hash backfill, then start Slice `15`.
- commit：

## Deferred External / Human Items

- Real payment/subscription/invoice/tax/fraud integrations remain `deferred_external` behind provider interfaces.
- Real email/SMS/OAuth provider setup remains `deferred_external`.
- Production DNS/TLS/cloud accounts/TURN public deployment/firewall/systemd/app-store signing remain `deferred_external`.
- Public devstack host can be used at `ssh root@114.66.58.243` only after recording reason, temp path, start/stop/cleanup commands, and residual state.

## Next Exact Action

1. Finish post-review validation for Slice `14`.
2. Commit Slice `14` and backfill its hash.
3. Start Slice `15`: write failing tests for `local -> public_p2p -> managed` connection orchestration returning only `RtcSession`.
