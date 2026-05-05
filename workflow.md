# Remote Product Build Workflow

## Objective

当前阶段目标不再是迁移 remote 出 core，而是完成可用的 remote 产品主链路：

- 构建无数据库、可多区域部署的 Hub
- 完成本地 Agent runtime 与远程建链流程
- 明确 Web Controller 与 Hub 的职责边界
- 保持 local / public_p2p / managed 三路径共用统一 session/signaling 架构
- 完成 browser 侧 `remote-ui` 对统一 remote/hub/agent 流程的接入

## Non-Negotiable Constraints

- Hub 没有数据库。
- Hub 不保存 durable business state。
- Web Controller 只做业务与鉴权控制，不做 runtime 代理或流量转发。
- runtime 数据面始终是 WebRTC DataChannel。
- relay 不是第四种 path。
- `remote-ui` 当前只实现 browser adapter，不实现 native adapter。
- `termx-core` 不回流 remote 产品代码。

## Execution Protocol

每个切片必须按以下步骤执行并更新本文件：

1. 认领最高优先级 open 任务并移动到 in_progress
2. 写明目标行为与预期失败测试
3. 先写失败测试，再实现最小代码
4. 跑 focused tests 与相关 broader tests
5. 记录验证结果
6. 发起独立 code review subagent
7. 修复 review 发现
8. 更新 done / new issues / reprioritized backlog

## Priority Backlog

### P0 Current Mission Status

- [x] WF-201 ICE Gathering 早停 + DataChannel 背压控制
  - Exit criteria:
    - `session/rtc/offer_handler.go` 实现三路竞争等待：gatherComplete | earlyStop(500ms after first host/srflx) | timeout(5s)
    - `bridge/datachannel_transport.go` 实现背压：发送缓冲 > 512KB 时暂停，OnBufferedAmountLow(128KB) 后恢复
    - pion/webrtc 升级至 v4.2.9，pion/ice 升级至 v4.2.1
    - 有单元测试覆盖早停行为和背压行为
    - 所有现有测试继续通过

- [x] WF-202 内置 TURN 服务器 + Relay 运行时检测
  - Exit criteria:
    - `termx-remote/hub/turn/` 包封装 pion/turn，Hub 启动时若 TERMX_HUB_TURN_SECRET 存在则自动启动（UDP + TCP 双栈）
    - `termx-hub/cmd/termx-hub/main.go` 读取 TURN 相关 env（TERMX_HUB_TURN_SECRET、TERMX_HUB_TURN_PORT）并启动 TURN
    - `session/rtc/` 提供 `IsRelayConnection(pc) bool`，用 `pc.GetStats()` 检查候选对类型
    - relay 权限控制：AllowRelayTransfer = false 且 IsRelayConnection = true 时，file transfer API 返回 403
    - 有单元测试覆盖 TURN 凭证生成、relay 检测、relay 权限控制

- [x] WF-203 remote-ui 对齐新 hub 协议，统一 local/cloud 连接路径
  - Exit criteria:
    - 删除 `localAgentApi.ts` 中调用已删除 localweb 端点的代码（`/api/local/rtc/offer`、`/api/local/pair`、`/api/local/terminals`）；保留 status-only `getStatus()` 兼容面
    - 删除 `localRtcConnector.ts`（调用 localweb offer 端点）
    - local mode 改为使用 `managedHubRtcConnector`，hub URL 指向本地嵌入 hub（`http://LAN_IP:port`）
    - local mode pairing 改为使用 `managedHubApi.createPairingClaim()`，同样指向本地 hub URL
    - `connectionOrchestrator` 的 local 路径重构为：获取本地 hub URL → 走 managedHubRtcConnector
    - 新增 LocalHubUrlProvider 接口，支持 QR 扫描或手动输入本地 hub URL
    - 所有现有测试继续通过，新增 orchestrator local-path 测试

- [ ] WF-101 明确 Hub / Web Controller / Agent 的产品边界与协议契约
  - Exit criteria:
    - `termx-remote/hub`、`termx-remote/agent`、Web Controller contract 的职责边界在代码与文档中清晰一致
    - 明确哪些状态在 Hub，哪些状态只允许在 Web Controller
    - 明确 local / managed 的统一 session/signaling 流程

- [ ] WF-102 完善 `termx-remote/hub` 的无状态多区域模型
  - Exit criteria:
    - Hub 只保留 TTL 内存状态
    - Hub 启动/cleanup/backpressure/expiry 行为有测试覆盖
    - region / endpoint / heartbeat / ICE policy 能表达多区域部署需求

- [ ] WF-103 完善 `termx-remote/agent` 本地运行时与 Hub 协同
  - Exit criteria:
    - agent 注册、heartbeat、inventory、pairing、session answer 行为闭合
    - local agent 与 managed hub 路径继续通过统一 session/signaling abstractions 工作

- [ ] WF-104 完成本地模式的 CLI 集成与 embedded local hub 体验
  - Exit criteria:
    - `termx-cli` 可在本地模式嵌入启动 local hub
    - local 模式生成的 pairing / bootstrap payload 与 managed 模式结构一致
    - local/LAN 连接链路有端到端验证

- [ ] WF-105 完善 Web Controller 控制面契约
  - Exit criteria:
    - 只负责 user/account/device ownership、hub routing、ticket/policy/quota、forced disconnect
    - 不承担 terminal/file/api/events runtime 代理
    - 对 Hub/agent/app 的契约有测试或 mock contract coverage

- [ ] WF-106 完善 `remote-ui` browser 接入统一 remote/hub/agent 流程
  - Exit criteria:
    - `remote-ui` 通过 interface-first runtime 接入统一 hub/signaling/session 过程
    - 组件层不直接依赖浏览器网络对象
    - 仍只保留 browser implementation

### Completed WF-201 / WF-202 / WF-203 Slice Log

- WF-201 / Slice-A ICE Gathering 早停
  - Target behavior:
    - RTC answer ICE gathering waits on three competing outcomes: gathering complete, 500ms early stop after the first host/srflx candidate, or a 5s hard timeout.
    - P2P candidates should stop answer generation in under 1s instead of waiting for the old fixed 8s timeout.
  - Failing test record:
    - `cd termx-remote && go test ./session/rtc -run 'TestGatheringEarlyStopOnHostCandidate|TestGatheringHardTimeoutWhenNoCandidate|TestGatheringCompleteBeforeEarlyStop'` failed to compile because `waitForICEGatheringEvents` did not exist; the current production path only waited for gather-complete or the old 8s timeout.
  - Validation:
    - `cd termx-remote && go test ./session/rtc -run 'TestGatheringEarlyStopOnHostCandidate|TestGatheringHardTimeoutWhenNoCandidate|TestGatheringCompleteBeforeEarlyStop'`: passed.
    - After review fix, `cd termx-remote && go test ./session/rtc -run 'TestGathering'`: passed.
    - `cd termx-remote && go test ./session/rtc/...`: passed.
  - Review: independent subagent found candidate callback registration was race-prone after `SetLocalDescription`, and tests only covered the helper. Fixed by creating/registering the waiter before `SetLocalDescription`, adding `TestGatheringWaiterRegistersCandidateCallbackBeforeWaiting`, and tightening the candidate-relative 500ms assertion.
- WF-201 / Slice-B DataChannel 背压控制
  - Target behavior:
    - `bridge.DataChannelTransport.Send` pauses while `BufferedAmount() > 512KB`.
    - `OnBufferedAmountLow` resumes blocked sends after the channel drains to the 128KB low watermark.
    - Closing the transport while blocked returns `io.EOF`.
  - Failing test record:
    - `cd termx-remote && go test ./bridge -run 'TestSendBlocksWhenBufferFull|TestSendResumesAfterLowWatermark'` failed to compile because `sendBufferHigh`, `sendBufferLow`, and mockable `newDataChannelTransport` did not exist; current implementation sent immediately and never registered buffered amount low callbacks.
  - Validation:
    - `cd termx-remote && go test ./bridge -run 'TestSendBlocksWhenBufferFull|TestSendResumesAfterLowWatermark'`: passed.
    - `cd termx-remote && go test ./bridge/...`: passed.
    - After review fixes, `cd termx-remote && go test -race ./bridge -run 'TestSendBlocksWhenBufferFull|TestSendResumesAfterLowWatermark|TestSendIgnoresStaleLowWatermarkToken|TestSendReturnsEOFWhenClosedWhileBlocked'`: passed.
    - After review fixes, `cd termx-remote && go test ./bridge/...`: passed.
    - Second review fix, `cd termx-remote && go test -race ./bridge -run 'TestSend|TestRecvReturnsEOFWithoutClosingReceiveChannel'`: passed.
    - Second review fix, `cd termx-remote && go test ./bridge/...`: passed.
  - Review: independent subagent found stale low-watermark tokens could bypass backpressure, close/drain races could send after close, tests had data races, and no close-while-blocked test. Fixed by looping/rechecking buffered amount, checking done again before send, adding a blocked-send timeout, making tests race-safe, and adding stale-token and close-while-blocked coverage. Re-review then found unsafe `recvCh` close and a final close/send race; fixed by never closing `recvCh`, making `Recv` prefer `done`, and synchronizing close with send via `sendMu`.
- WF-201 / Slice-C pion 版本升级
  - Target behavior:
    - `github.com/pion/webrtc/v4` is at least v4.2.9.
    - `github.com/pion/ice/v4` is at least v4.2.1.
  - Failing test record:
    - `cd termx-remote && go test ./session/rtc -run TestPionVersionsAtRequiredMinimums` failed with `github.com/pion/webrtc/v4 version v4.1.6 is below required minimum v4.2.9`; `go.mod` also had `github.com/pion/ice/v4 v4.0.10`.
  - Validation:
    - `cd termx-remote && go test ./session/rtc -run 'TestPionVersionsAtRequiredMinimums|TestGathering'`: passed.
    - `cd termx-remote && go test ./bridge/...`: passed.
    - `cd termx-remote && go list -m all | rg 'github.com/pion/(webrtc|ice|turn)/v4'`: confirmed `webrtc/v4 v4.2.9`, `ice/v4 v4.2.1`, `turn/v4 v4.1.4`.
    - `cd termx-remote && go test ./...`: passed.
    - After review fix, `cd termx-cli && go mod tidy`: updated standalone CLI module metadata to the same Pion versions.
    - After review fix, `cd termx-cli && GOWORK=off go test -count=1 ./...`: passed.
    - After review fix, `cd termx-cli && go test ./...`: passed.
    - After review fix, `cd termx-cli && go list -m github.com/pion/webrtc/v4 github.com/pion/ice/v4 && GOWORK=off go list -m github.com/pion/webrtc/v4 github.com/pion/ice/v4`: both modes confirmed `webrtc/v4 v4.2.9`, `ice/v4 v4.2.1`.
  - Review: independent subagent found standalone `termx-cli` still had stale indirect Pion pins and failed `GOWORK=off go test` due to an untidied module. Fixed by upgrading/tidying `termx-cli/go.mod` and validating workspace plus standalone CLI tests.
- WF-202 / Slice-D 内置 TURN 服务器
  - Target behavior:
    - `termx-remote/hub/turn` owns the embedded TURN server wrapper with UDP + TCP listeners.
    - Credentials are 24h TURN REST style: username is an expiry timestamp and credential is `base64(HMAC-SHA1(username, secret))`.
    - Hub ICE config includes embedded TURN URLs and fresh credentials when relay is allowed.
    - `termx-hub` starts embedded TURN when `TERMX_HUB_TURN_SECRET` is set, without involving Web Controller or durable state.
  - Failing test record:
    - `cd termx-remote && go test ./hub/turn/...` failed to compile because `hub/turn.NewServer` and `hub/turn.Config` did not exist.
  - Validation:
    - `cd termx-remote && go test ./hub/turn/...`: passed.
    - `cd termx-remote && go test ./hub/ice ./hub/turn`: passed.
    - `cd termx-hub && go test ./cmd/termx-hub`: passed.
    - `cd termx-hub && go build ./...`: passed.
    - `cd termx-remote && go test ./hub/turn ./hub/ice ./hub/httpapi`: passed.
    - `cd termx-remote && go test ./...`: passed.
    - After review fixes, `cd termx-remote && go test ./hub/cloud ./session/rtc ./agent/runtime ./hub/turn`: passed.
    - After review fixes, `cd termx-hub && go test ./cmd/termx-hub`: passed.
  - Review: independent subagent found embedded TURN was not reachable through the normal cloud session path because cloud offers still defaulted `AllowRelay=false`, default env could advertise `0.0.0.0`, and TCP/listener wiring tests were weak. Fixed by adding `AllowRelayByDefault` to hub/cloud, enabling it from `termx-hub` when embedded/static TURN exists, requiring `TERMX_HUB_TURN_PUBLIC_IP` for unspecified TURN listen addresses, and adding TCP STUN plus handler session/poll TURN coverage.
- WF-202 / Slice-E Relay 运行时检测
  - Target behavior:
    - `session/rtc.IsRelayConnection(pc)` detects a succeeded ICE candidate pair that includes a relay local or remote candidate.
    - RTC session context records relay usage after connection establishment.
    - File manager API routes and file data channels are forbidden with HTTP 403 / close behavior when relay is in use and `AllowRelayTransfer=false`; they remain allowed when `AllowRelayTransfer=true`.
  - Failing test record:
    - `cd termx-remote && go test ./session/rtc -run 'TestIsRelayConnectionReturnsTrueWhenRelayCandidate|TestIsRelayConnectionReturnsFalseWhenP2PCandidate|TestFileTransferForbiddenOnRelayWithoutPermission|TestFileTransferAllowedOnRelayWithPermission'` failed to compile because relay stats helpers and relay-aware context policy did not exist.
  - Validation:
    - `cd termx-remote && go test ./session/rtc -run 'TestIsRelayConnectionReturnsTrueWhenRelayCandidate|TestIsRelayConnectionReturnsFalseWhenP2PCandidate|TestFileTransferForbiddenOnRelayWithoutPermission|TestFileTransferAllowedOnRelayWithPermission'`: passed.
    - `cd termx-remote && go test ./session/rtc/...`: passed.
    - `cd termx-remote && go test ./fileapi ./agent/runtime`: passed.
    - `cd termx-remote && go test ./...`: passed.
    - After review fixes, `cd termx-remote && go test ./hub/cloud ./session/rtc ./agent/runtime ./hub/turn`: passed.
  - Review: independent subagent found `AllowRelayTransfer=true` was dropped before the real RTC answerer, relay detection could false-positive on non-selected succeeded pairs, and the allowed API test was too loose. Fixed by copying `offer.AllowRelayTransfer` into `ChannelPolicy`, selecting relay status from `TransportStats.SelectedCandidatePairID` or nominated pairs only, and tightening tests.
- WF-203 / Slice-F 删除 localweb 死代码，统一连接路径
  - Target behavior:
    - Local mode runtime connection uses the standard Hub session API through `managedHubApi.createSession` / `managedHubRtcConnector`, with the local embedded Hub URL as base URL.
    - Local mode pairing uses `managedHubApi.pair` / `/api/v1/pairing/claims`, not `/api/local/pair`.
    - `localRtcConnector.ts` and localweb offer/pair endpoints are removed from production source.
  - Expected failing tests:
    - `cd remote-ui && npm run test -- --run src/localConnectionUsesHubApi.test.ts`
    - Current local path still uses `localRtcConnector` and `localAgentApi` localweb endpoints.
  - Failing test record:
    - `cd remote-ui && npm run test -- localConnectionUsesHubApi.test.ts` initially failed because local mode still depended on the deleted local RTC connector/localweb offer path; the new hub-path regression test expected `/api/v1/sessions` and `/api/v1/pairing/claims`.
  - Validation:
    - `cd remote-ui && npm run typecheck`: passed.
    - `cd remote-ui && npm run test -- localConnectionUsesHubApi.test.ts localAgentApi.test.ts localWebEntry.test.tsx LocalRemoteApp.test.tsx LocalRemoteApp.files.test.tsx browserRtcSession.behavior.test.ts`: passed.
    - After review fixes, `cd remote-ui && npm run test -- localConnectionUsesHubApi.test.ts connectionOrchestrator.test.ts managedHubRtcConnector.test.ts localWebEntry.test.tsx LocalPairPanel.test.tsx`: passed.
    - After review fixes, `cd remote-ui && npm run test`: passed, 46 files / 217 tests. jsdom still prints the existing canvas warning.
    - `cd remote-ui && npm run build`: passed. Vite still prints the existing >500KB chunk warning.
    - `rg -n "/api/local/rtc/offer|/api/local/pair|localRtcConnector" remote-ui/src --glob '*.ts' --glob '*.tsx'`: empty.
  - Review: independent subagent found first-run local UI could not reach pairing before a certificate existed, local Hub pairing omitted `machine_id`, and the shared managed Hub RTC connector still forced `path: managed`. Fixed by setting machine status before inventory load, returning an empty first-run terminal list until a local app certificate exists, passing `machineId` into `LocalPairPanel`, and adding a `path: 'local'` option through the orchestrator/local connector into `managedHubRtcConnector`.

### Final Validation Log

- `cd termx-remote && go test ./...`: passed.
- `cd termx-hub && go build ./...`: passed.
- `cd termx-hub && go test ./...`: passed.
- `cd termx-cli && go test ./...`: passed.
- `cd termx-cli && GOWORK=off go test -count=1 ./...`: passed.
- `cd remote-ui && npm run test`: passed, 46 files / 217 tests.
- `cd remote-ui && npm run build`: passed.
- `grep -r "localweb\|controlclient" --include="*.go" termx-remote/`: empty.
- `grep -r "/api/local/rtc/offer\|/api/local/pair\|localRtcConnector" remote-ui/src --include="*.ts" --include="*.tsx"`: empty.
- WF-201, WF-202, and WF-203 are complete.

### Blocked

- None

### Done

- Slice-1 构建修复：删除 `termx-cli` webshell Cobra 残留入口
  - Validation:
    - Initial failing `cd termx-cli && go build ./...`: failed on missing `internal/webshell`.
    - Initial failing `grep -r "webshell" --include="*.go" termx-cli/`: found `cmd/termx/web.go`.
    - After implementation `grep -r "webshell" --include="*.go" termx-cli/`: empty.
    - After implementation `cd termx-cli && go build ./...`: passed.
    - After implementation `cd termx-cli && go test ./...`: passed.
  - Review: independent subagent reported no findings; verified `web.go` deletion, only the `webCommand` registration line removed from `main.go`, build passed, and webshell grep was empty.

- Slice-2 删除 hub/controlclient 包：hub/httpapi 不再依赖 Web Controller verifier/policy
  - Failing test record:
    - `cd termx-remote && go test ./hub/httpapi -run 'TestHandlerConfigHasNoControlVerifierFields|TestHubSessionDoesNotCallExternalHTTP'` failed because `httpapi.Config` exposed `AgentPolicy` and `registry.Register` returned `authority verifier is required`.
  - Validation:
    - Focused `cd termx-remote && go test ./hub/httpapi ./hub/registry ./hub/cloud ./hub/heartbeat`: passed.
    - Focused `cd termx-hub && go test ./cmd/termx-hub`: passed.
    - Dead-code grep `rg -n "controlclient|TERMX_HUB_CONTROL|AgentPolicy|ConnectionTicketVerifier|TicketVerifier|Verifier:" termx-remote/hub termx-hub --glob '*.go' termx-hub/deploy termx-hub/scripts`: empty.
    - `cd termx-remote && go build ./...`: passed.
    - `cd termx-hub && go build ./...`: passed.
    - `cd termx-remote && go test ./...`: passed.
    - `cd termx-hub && go test ./...`: passed.
  - Review: independent subagent reported no blocking issues; verified no Web Controller calls, no `controlclient` package files, no control env wiring, no cert/ticket verification in hub, and passing `go build ./...` / `go test ./...` in `termx-remote` and `termx-hub`.

- Slice-3 删除 localweb 包与 service.go 清理
  - Failing test record:
    - `cd termx-remote && go test . -run TestLocalEnableReturnsError` failed because old `LocalEnable` tried to bind `127.0.0.1:18889` instead of returning not implemented.
  - Validation:
    - `cd termx-remote && go test . -run TestLocalEnableReturnsError`: passed.
    - `cd termx-remote && go build ./...`: passed.
    - `cd termx-remote && go test ./...`: passed.
    - `test ! -e termx-remote/localweb`: passed.
    - `grep -r "localweb" --include="*.go" termx-remote/`: empty.
    - `rg -n "LocalWebHandler|localRTCAnswer|localWebAdapter|LocalPlan|AnswerLocal|NewLocalWebStaticAssets|EmbeddedLocalWebAssets" termx-remote --glob '*.go'`: only negative boundary assertion remains in `session_flow_boundary_test.go`.
  - Review: independent subagent reported no findings; verified localweb directory removal, empty localweb grep, service cleanup, LocalEnable not-implemented behavior, sessionflow cleanup, cloud RTC terminal management path, and passing `go build ./...` / `go test ./...` in `termx-remote`.

- Slice-4 实现 LocalEnable 嵌入 hub + cmux（HTTP + ICE-TCP）
  - Failing test record:
    - `cd termx-remote && go test . -run 'TestLocalEnableStartsHub|TestLocalHubAcceptsAgentRegistration|TestLocalDisableStopsHub|TestLocalEnableIdempotent'` initially failed because `LocalEnable` returned `local hub: not implemented`.
  - Implementation notes:
    - `LocalEnable` now starts embedded `hub/httpapi.NewHandler` behind cmux HTTP/ICE-TCP on one listener, advertises a LAN URL, and attaches the real agent manager to the local hub using standard hub register/heartbeat/poll/pairing flows.
    - Local ICE answers use the local TCP mux via hub-scoped answer options; `LocalDisable` closes the listener and detaches only the embedded hub.
  - Validation:
    - `cd termx-remote && go test . -run 'TestLocalEnableStartsHub|TestLocalHubAcceptsAgentRegistration|TestLocalEnableRegistersRealAgentWithHub|TestLocalDisableStopsHub|TestLocalDisableDetachesManagerFromHub|TestLocalEnableIdempotent'`: passed.
    - `cd termx-remote && go test -race -count=1 . -run 'TestLocalEnableRegistersRealAgentWithHub|TestLocalDisableDetachesManagerFromHub|TestLocalEnableIdempotent'`: passed.
    - `cd termx-remote && go test ./...`: passed.
    - `cd termx-cli && go test ./...`: passed.
    - `cd termx-remote && go build ./...`: passed.
    - `cd termx-cli && go build ./...`: passed.
  - Review: independent reviews found and fixes covered real manager attachment, ICE-TCP answer option wiring, non-tautological agent/pairing coverage, and local disable cleanup; final review path clean.

- Slice-5 Manager 支持多 hub URL（both 模式）
  - Failing test record:
    - `cd termx-remote && go test ./agent/runtime -run 'TestManagerRegistersTwoHubs|TestManagerReconnectsOnHubFailure'` initially failed to compile because `config.Config` did not yet have `HubURLs`.
    - Post-review tests also exposed stalled-first-hub blocking and live-daemon both-mode cloud discovery gaps before fixes.
  - Implementation notes:
    - `runtime.Manager` now accepts `HubURLs`, keeps per-hub registration/signaling state, registers one shared `agent_id` and persisted machine key with each hub, and isolates per-hub answer options.
    - Presence sync runs per hub with per-hub timeout so a stalled hub does not block a healthy hub.
    - `LocalEnableParams` can carry `HubURLs`, `ControlURL`, `AccessToken`, and `Region`; a running daemon can add explicit cloud hubs or discover a cloud hub without restart while preserving the embedded local hub.
  - Validation:
    - `cd termx-remote && go test ./agent/runtime -run 'TestManagerRegistersTwoHubs|TestManagerReconnectsOnHubFailure|TestManagerRegistersHealthyHubWhenAnotherHubStalls|TestManagerAddsDiscoveredHubWithoutDroppingExistingLocalHub|TestManagerKeepsLocalHubWhenExplicitCloudHubConfigured|TestHubSignalingLoopUsesHubScopedAnswerOptions'`: passed.
    - `cd termx-remote && go test -race -count=1 ./agent/runtime -run 'TestManagerAddsDiscoveredHubWithoutDroppingExistingLocalHub|TestManagerKeepsLocalHubWhenExplicitCloudHubConfigured|TestManagerRegistersHealthyHubWhenAnotherHubStalls|TestManagerRegistersTwoHubs|TestManagerReconnectsOnHubFailure|TestHubSignalingLoopUsesHubScopedAnswerOptions|TestManagerReregistersHubWhenHeartbeatUnauthorized'`: passed.
    - `cd termx-cli && go test ./cmd/termx -run 'TestRemoteEnableBothPassesCloudHubToRunningLocalDaemon|TestRemoteEnableBothPassesCloudDiscoveryToRunningLocalDaemon|TestRemoteEnableCloudPersistsBootstrapOutsideConfigFile|TestRemoteEnableCloudUsesBrowserLoginWhenKeyMissing|TestRemoteEnableLocalOnlyEmitsLocalStatus'`: passed.
    - `cd termx-remote && go test ./...`: passed.
    - `cd termx-cli && go test ./...`: passed.
    - `cd termx-remote && go build ./...`: passed.
    - `cd termx-cli && go build ./...`: passed.
  - Review: independent reviews found stalled hub blocking, live-daemon cloud discovery not reaching the running manager, hub state lock misuse, and explicit-cloud/local ordering. Fixes were implemented and final Slice-5 re-review reported no findings.

- Slice-6 Agent 侧 App Certificate 验证路径确认与测试补全
  - Test record:
    - `cd termx-remote && go test ./agent/runtime -run 'TestOfferWithValidCertIsAccepted|TestOfferWithExpiredCertIsRejected|TestOfferWithWrongMachineIDIsRejected|TestOfferWithInvalidSignatureIsRejected'`: passed, confirming the existing hub offer path already verifies app cert signature, expiry, local `machine_id`, and offer signature before answering.
  - Implementation notes:
    - Added explicit cert verification tests using real Ed25519 machine/app keys.
    - Hub remains a dumb relay; app cert verification stays in `agent/runtime`.
  - Validation:
    - `cd termx-remote && go test ./agent/runtime -run 'TestOfferWithValidCertIsAccepted|TestOfferWithExpiredCertIsRejected|TestOfferWithWrongMachineIDIsRejected|TestOfferWithInvalidSignatureIsRejected|TestManagerVerifiesCloudOfferSignatureAndRejectsReplay'`: passed.
    - `cd termx-remote && go test ./...`: passed.
    - `cd termx-remote && go build ./...`: passed.
  - Review: independent Slice-6 review reported no findings; verified cert verification happens before WebRTC answer generation, rejected offers do not call the answerer, tests use real Ed25519 keys, and hub code does not verify app certs.

- 迁移阶段 WF-001 ~ WF-008 已完成；它们不再是当前主线，但可作为历史背景参考。

## New Issues / Discoveries

- Slice-2 review noted that `hub/cloud` still uses names and correlation checks such as `OfferPolicy`, `Ticket`, `ConsumeOfferTicket`, and wrong-machine errors. Current behavior is bounded in-memory offer/answer correlation only, with no Web Controller call, durable state, cryptographic cert verification, or ticket verification. Defer naming cleanup unless it blocks a later slice.

## Validation Log

- 2026-05-05 final acceptance:
  - Workspace-root `go build ./...`: not applicable in this checkout; failed with `pattern ./...: directory prefix . does not contain modules listed in go.work or their selected dependencies` because root `.` is not a module in `go.work`.
  - Module builds used as equivalent acceptance and passed:
    - `cd termx-core && go build ./...`
    - `cd termx-remote && go build ./...`
    - `cd termx-cli && go build ./...`
    - `cd termx-hub && go build ./...`
  - Required module tests passed:
    - `cd termx-cli && go test ./...`
    - `cd termx-remote && go test ./...`
    - `cd termx-hub && go test ./...`
  - Dead-code checks passed empty:
    - `grep -r "localweb" --include="*.go" termx-remote/`
    - `grep -r "controlclient" --include="*.go" termx-remote/hub/`
    - `grep -r "webshell" --include="*.go" termx-cli/`
  - Boundary tests passed:
    - `cd termx-remote && go test -run TestBoundary ./...`

## Review Log

- Slice-4: independent reviews found missing real manager attachment, missing embedded ICE-TCP mux wiring, and local disable cleanup gaps; all fixed and validated with real embedded hub/agent/pairing tests.
- Slice-5: independent reviews found stalled hub blocking, live both-mode cloud discovery gaps, unsafe hub state locking, and explicit-cloud/local ordering risk; all fixed and final re-review reported no findings.
- Slice-6: independent review reported no findings; agent-side cert verification covers signature, expiry, machine_id, offer signature/replay, and hub remains free of cert verification.
