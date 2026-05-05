# Remote Migration Workflow

## Objective

一次性完成 remote 域迁移：

- `termx-core` 变回纯 daemon/core
- 新建 `termx-remote` 承载 hub/agent/remote protocol
- `termx-cli` 成为唯一 remote 集成层
- `remote-ui` 变为 interface-first 网络架构，目前只保留 browser implementation

## Non-Negotiable Constraints

- `termx-core` 不能继续包含 remote runtime / pairing / signaling / localweb / hub / QR / remote RPC。
- `termx-remote` 与 `termx-core` 的边界以 Go `interface` 为主，RPC 只是 adapter。
- local/LAN/managed 必须共享同一套 hub/signaling/session 代码路径。
- `remote-ui` 所有网络能力必须先定义 TypeScript `interface`，当前阶段只实现 browser adapter，不实现 native adapter。
- 每个切片都必须 TDD 推进，并在完成后做独立 code review。

## Execution Protocol

对每个切片，必须按以下步骤执行并更新本文件：

1. 认领一个最高优先级 open 任务，并移动到 in_progress
2. 写下该切片目标与预期失败测试
3. 先写失败测试，再实现最小代码
4. 跑 focused tests 与相关 broader tests
5. 记录验证结果
6. 发起独立 code review subagent
7. 修复 review 发现
8. 更新 done / new issues / reprioritized backlog

## Priority Backlog

### P0 Open

- None. WF-001 through WF-008 are done.

## In Progress

- None

## Blocked

- None

## Done

- [x] WF-001 收紧 `termx-core/clientapi` 与 `protocol` 边界，移除 remote 能力与 remote RPC
  - Completed:
    - `termx-core/clientapi/client.go` no longer exposes remote product methods on `Client` or `ProtocolClient`.
    - `termx-core/protocol/client.go` no longer exposes remote RPC helpers (`RemoteStatus`, `RemotePairStart`, `RemoteLocal*`).
    - `termx-core/termx.go` no longer dispatches removed `remote.*` JSON-RPC methods; remote methods now return `404 unknown method` while generic unsupported methods keep the existing `400 unsupported method` contract.
    - Boundary tests now use exact method allowlists for `clientapi.Client`, `clientapi.ProtocolClient`, and `protocol.Client`.
  - Deferred:
    - Remote DTOs and direct server runtime APIs remain temporarily for the later package migration slices listed in New Issues / Discoveries.

- [x] WF-002 建立 `termx-remote` 目录骨架与包边界
  - Completed:
    - Added standalone `termx-remote` Go module and included it in `go.work`.
    - Added `termx-remote/agent`, `termx-remote/hub`, `termx-remote/protocol`, and `termx-remote/client` packages.
    - Added `termx-remote/AGENTS.md` with the root migration rules and boundary constraints.
    - Added remote-owned module boundary tests and a minimal shell-neutral daemon capability interface in `termx-remote/client`.
  - Review fixes:
    - Moved repository/module boundary tests out of `termx-core` and into `termx-remote`.
    - Changed the initial `termx-remote/client.Daemon` boundary from embedding full `clientapi.Client` to the deliberately small `List(context.Context)` capability currently needed by the skeleton.

- [x] WF-003 迁移 remote protocol / session / pairing / discovery 到 `termx-remote`
  - Completed:
    - Moved hub v1 protocol/signature types to `termx-remote/protocol/hubv1`.
    - Moved pairing, discovery/control clients, cert/identity/bridge/fileapi, and RTC offer/session code to `termx-remote`.
    - Removed old `termx-core/remote` and `termx-core/internal/remote` implementations from the active core tree.
    - Moved hub product packages, including heartbeat, under `termx-remote/hub`.

- [x] WF-004 迁移 local remote runtime / local web / QR / local hub 能力到 `termx-remote`
  - Completed:
    - Added `termx-remote.Service` for local status, pairing, local web handler, RTC answer handling, terminal inventory, and local ICE TCP mux orchestration.
    - Local web generated assets now sync to `termx-remote/localweb/static`.
    - Local RTC answering now uses `sessionflow.AnswerLocal`.

- [x] WF-005 迁移 managed agent runtime 到 `termx-remote/agent`
  - Completed:
    - Moved managed agent registration/discovery/session answer/pairing claim runtime and tests to `termx-remote/agent/runtime`.
    - Managed cloud offer answering now uses `sessionflow.AnswerManaged`.

- [x] WF-006 让 `termx-cli` 成为唯一 remote 集成层
  - Completed:
    - Added `termx-cli/cmd/termx/remote_runtime.go` to adapt shell-neutral core daemon operations to `termx-remote.Service`.
    - CLI owns the `remote.*` JSON-RPC adapter and calls generic protocol `Client.Call` with DTOs from `termx-remote/protocol`.
    - `termx-core` has no remote RPC adapter when CLI integration is absent.

- [x] WF-007 重构 `remote-ui` 为 interface-first 网络边界，仅保留 browser implementation
  - Completed:
    - Added `RemoteNetworkRuntime`, `RemoteRuntimeFetch`, and `RemoteRuntimeStorage` interfaces to `remote-ui/src/transport.ts`.
    - Isolated browser access to `fetch`, `localStorage`, and `location` in `browserNetworkRuntime.ts`.
    - Updated local web, Web Control, managed Hub API clients, mount code, and components to receive runtime/network dependencies by interface.
    - Kept native as a future factory boundary only; no native implementation was added.

- [x] WF-008 删除 `termx-core` 中剩余全部 remote 代码并做全量回归
  - Completed:
    - Deleted active `termx-core` remote directories/files and removed remote DTO/RPC exposure from core.
    - Confirmed `termx-core` does not import `termx-remote`.
    - Completed independent reviews and review fixes.
    - Full Go and remote-ui regression passed; details are in Validation Log and Review Log.

## New Issues / Discoveries

- 2026-05-04 WF-001: `termx-core/protocol/messages.go` still contains remote DTOs (`RemoteStatus`, `PairStart*`, `RemoteLocal*`) because current in-core remote runtime code still depends on them. Deferred to WF-003/WF-004 when the remote protocol/runtime packages move to `termx-remote`.
- 2026-05-04 WF-001: old protocol-level remote E2E tests in `termx-core/remote_test.go` and `termx-core/protocol/client_test.go` were removed from the core test surface because they asserted the deleted core remote RPC exposure. Equivalent behavior coverage must be restored under `termx-remote` during WF-003/WF-006.
- 2026-05-04 WF-003: mechanically redirecting `termx-core` callers to moved `termx-remote` packages creates a forbidden `termx-core -> termx-remote` dependency. WF-003 therefore cannot stop at import redirection; the remaining remote runtime/localweb/agent orchestration must move forward so core does not depend on the remote module.
- 2026-05-04 WF-003/WF-006: completing the Go boundary required advancing WF-003 through WF-006 together: remote protocol/session/local runtime/agent code moved to `termx-remote`, while `termx-cli` owns the `remote.*` RPC adapter and injects only shell-neutral daemon interfaces into the remote service.
- 2026-05-04 WF-003: `termx-hub` was another consumer of hub v1 protocol types; it now needs to depend on `termx-remote/protocol/hubv1` instead of the deleted core package.
- 2026-05-04 WF-007: after the remote-ui network boundary change, local web embedded assets must be regenerated from `remote-ui` so `termx-remote/localweb/static` matches the source.
- 2026-05-04 WF-007/WF-008: `remote-ui/scripts/sync-localweb-assets.mjs` still pointed at `termx-core/internal/remote/localweb/static`; fixed it to sync to `termx-remote/localweb/static` and removed the mistakenly regenerated core remote static tree.
- 2026-05-04 WF-008: WF-001 deferred remote DTOs and direct remote server APIs are now resolved by moving DTOs to `termx-remote/protocol` and making CLI the only `remote.*` RPC adapter.

## Validation Log

- 2026-05-04 WF-001 failing tests recorded before implementation:
  - `cd termx-core && go test ./clientapi -run TestClientAPIBoundaryDoesNotExposeRemoteCapabilities -count=1` fails because `clientapi.Client` exposes `RemoteStatus`.
  - `cd termx-core && go test ./protocol -run TestClientBoundaryDoesNotExposeRemoteRPCMethods -count=1` fails because `protocol.Client` exposes `RemoteLocalDisable`.
  - `cd termx-core && go test . -run TestServerDoesNotDispatchRemoteRPCMethods -count=1` fails because `remote.status` is still dispatched successfully by `termx-core/termx.go`.
- 2026-05-04 WF-001 focused tests after implementation:
  - `cd termx-core && go test ./clientapi -run TestClientAPIBoundaryDoesNotExposeRemoteCapabilities -count=1` passed.
  - `cd termx-core && go test ./protocol -run TestClientBoundaryDoesNotExposeRemoteRPCMethods -count=1` passed.
  - `cd termx-core && go test . -run TestServerDoesNotDispatchRemoteRPCMethods -count=1` passed.
  - `cd termx-core && go test ./clientapi ./protocol -count=1` passed.
- 2026-05-04 WF-001 broader tests:
  - `cd termx-core && go test ./... -count=1` initially failed in `TestHandleTransportSendsProtocolErrors` because unknown methods now return `404 unknown method` instead of `400 unsupported`; contract test was updated.
  - `cd termx-core && go test . -run TestHandleTransportSendsProtocolErrors -count=1` passed.
  - `cd termx-core && go test ./... -count=1` passed.
- 2026-05-04 WF-001 review fixes validation:
  - `cd termx-core && go test ./clientapi -run TestClientAPIBoundaryDoesNotExposeRemoteCapabilities -count=1` passed.
  - `cd termx-core && go test ./protocol -run TestClientBoundaryDoesNotExposeRemoteRPCMethods -count=1` passed.
  - `cd termx-core && go test . -run 'TestServerDoesNotDispatchRemoteRPCMethods|TestHandleTransportSendsProtocolErrors' -count=1` passed.
  - `cd termx-core && go test ./... -count=1` passed.
- 2026-05-04 WF-002 failing tests recorded before implementation:
  - `cd termx-core && go test . -run TestTermxRemoteModuleSkeleton -count=1` fails because `../termx-remote/AGENTS.md` does not exist.
  - `cd termx-core && go test . -run TestTermxRemoteClientPackageUsesCoreClientAPI -count=1` fails because `../termx-remote/client` does not exist.
- 2026-05-04 WF-002 focused tests after implementation:
  - `cd termx-core && go test . -run 'TestTermxRemoteModuleSkeleton|TestTermxRemoteClientPackageUsesCoreClientAPI' -count=1` passed.
  - `cd termx-remote && go test ./... -count=1` passed.
  - `go test ./termx-remote/client -run TestClientPackageAcceptsCoreClientAPI -count=1` passed.
- 2026-05-04 WF-002 broader tests:
  - `go test ./termx-core/... ./termx-remote/... -count=1` passed.
- 2026-05-04 WF-002 review fixes validation:
  - `cd termx-remote && go test . -run TestTermxRemoteModuleSkeleton -count=1` passed.
  - `cd termx-remote && go test ./client -run 'TestClientPackageAcceptsMinimalCoreClientAPI|TestDaemonBoundaryIsMinimalShellNeutralCapability' -count=1` passed.
  - `go test ./termx-core/... ./termx-remote/... -count=1` passed.
- 2026-05-04 WF-003 failing tests recorded before implementation:
  - `cd termx-remote && go test . -run 'TestWF003RemoteProtocolSessionPackagesMigratedFromCore|TestWF003CoreRedirectsToTermxRemotePackages' -count=1` fails because `termx-remote/protocol/hubv1` is missing and core callers still import old core remote packages.
- 2026-05-04 WF-003/WF-006 focused tests after Go migration implementation:
  - `cd termx-core && go test ./... -count=1` passed.
  - `cd termx-remote && go test ./... -count=1` passed.
  - `cd termx-cli && go test ./... -count=1` passed.
  - `cd termx-hub && go test ./... -count=1` passed.
- 2026-05-04 WF-003/WF-006 broader tests after Go migration implementation:
  - `go test ./termx-core/... ./termx-remote/... ./termx-cli/... ./termx-hub/... -count=1` passed.
  - `find termx-core -maxdepth 3 \( -name remote -o -name '*remote*' \) -print` returned no paths.
  - Boundary scan for old core remote imports/public DTOs found no active core remote implementation imports or public remote RPC surface.
- 2026-05-04 WF-007 failing tests recorded before implementation:
  - `cd remote-ui && npm test -- --run src/transport.test.ts src/browserNetworkRuntime.test.ts` fails because `src/browserNetworkRuntime.ts` cannot be resolved and `transport.ts` does not define `RemoteNetworkRuntime`.
- 2026-05-04 WF-007 focused tests after implementation:
  - `cd remote-ui && npm test -- --run src/transport.test.ts src/browserNetworkRuntime.test.ts src/localAgentApi.test.ts src/webControlApi.test.ts src/managedHubApi.test.ts src/WebControlRemoteApp.test.tsx` passed.
  - `cd remote-ui && npm run typecheck` passed.
- 2026-05-04 WF-007 broader tests:
  - `cd remote-ui && npm test` passed: 46 test files, 225 tests.
  - `cd remote-ui && npm run build:localweb` passed after fixing the sync target to `termx-remote/localweb/static`.
  - `cd remote-ui && npm test -- --run scripts/sync-localweb-assets.test.mjs` passed.
- 2026-05-04 WF-008 final regression before review:
  - `go test ./termx-core/... ./termx-remote/... ./termx-cli/... ./termx-hub/... -count=1` passed.
  - `cd remote-ui && npm test` passed.
  - `cd remote-ui && npm run typecheck` passed.
  - `find termx-core -path 'termx-core/_tmux-src/.git' -prune -o \( -name remote -o -name '*remote*' \) -print` returned no active core remote paths.
  - Boundary scan for old core remote imports/public DTOs found only legitimate `termx-remote` ownership, CLI remote integration code, and `termx-remote/migration_boundary_test.go` historical forbidden-path assertions.
- 2026-05-04 WF-003/WF-008 adversarial self-review fix before independent review completed:
  - Removed `termx-remote/client` test coupling to `termx-core/clientapi.Client`; the boundary test now covers only the minimal shell-neutral daemon capability.
  - `cd termx-remote && go test ./client -run TestDaemonBoundaryIsMinimalShellNeutralCapability -count=1` passed.
  - `go test ./termx-core/... ./termx-remote/... ./termx-cli/... ./termx-hub/... -count=1` passed.
  - `rg -n "github.com/lozzow/termx/termx-core/clientapi|clientapi\\.Client" termx-remote -g '*.go'` returned no matches.

## Review Log

- 2026-05-04 WF-001 independent review completed by subagent:
  - Medium: generic unknown methods were accidentally changed from `400 unsupported method` to `404 unknown method`; fixed by adding explicit `remote.*` rejection while preserving the generic contract.
  - Low: dispatcher boundary test only covered `remote.status`; fixed by covering `remote.status`, `remote.pair.start`, `remote.local.enable`, `remote.local.status`, and `remote.local.disable`.
  - Low: public client boundary tests were prefix-shaped; fixed by switching to exact public method allowlists.
  - Residual risks accepted for later slices: remote DTOs and direct server remote APIs remain until WF-003/WF-004.
- 2026-05-04 WF-002 independent review completed by subagent:
  - Medium: repository/module-boundary tests were incorrectly located in `termx-core`, causing core tests to know about `termx-remote`; fixed by moving those tests into `termx-remote`.
  - Low: WF-002 ledger was still in progress after validation; fixed by moving WF-002 to done after review fixes.
  - Low: `termx-remote/client` embedded the full `clientapi.Client` method set; fixed by defining a deliberately small shell-neutral daemon capability interface.
- 2026-05-04 WF-003/WF-008 independent review completed by subagent Copernicus:
  - High: `termx-hub` still owns hub HTTP routing, registry/signaling, pairing queues, and ICE/relay policy while `termx-remote/hub` is only a placeholder. Required fix: move hub product implementation into `termx-remote/hub`; keep `termx-hub` as executable/config adapter.
  - High: local and managed flows still look split: local uses `/api/local/*` and `Service.LocalEnable`, managed uses agent hub polling/signaling. Required fix: introduce/centralize shared hub/signaling/session abstractions in `termx-remote` and make local web use that path.
  - Medium: `termx-core` still has remote namespace knowledge through an explicit `remote.*` branch and tests. Required fix: remove the core branch and let CLI adapter own remote compatibility.
  - Medium: `WebControlRemoteApp` still imports/constructs browser runtime/RTC/crypto adapters. Required fix: inject browser adapters through interfaces from entry/mount code.
  - Low: remote-ui boundary tests rely partly on raw-source placement and missed public component browser-adapter imports. Required fix: add boundary tests forbidding those imports.
- 2026-05-04 WF-003/WF-008 review fixes:
  - Moved hub product implementation packages from `termx-hub/internal/{cloud,controlclient,httpapi,ice,registry}` into `termx-remote/hub/{cloud,controlclient,httpapi,ice,registry}`; `termx-hub` now only owns the cmd/config adapter.
  - Added `termx-remote/hub/sessionflow` and wired both local runtime and managed agent answer path through the shared session-flow boundary; relay remains policy inside the managed/local plan, not a client path.
  - Removed `termx-core`'s `remote.*` special-case branch; without CLI adapter, remote methods now follow the generic unsupported-method contract.
  - Removed browser runtime/RTC/crypto adapter construction from `WebControlRemoteApp`; browser construction is injected from `remoteAppMount`.
  - Added remote-ui boundary test coverage that forbids browser adapter imports in `WebControlRemoteApp`.
  - Fixed local web asset sync target to `termx-remote/localweb/static`.
  - Review fix validation:
    - `cd termx-core && go test . -run TestServerDoesNotSpecialCaseRemoteRPCMethods -count=1` failed before the core fix with `404 unknown method`, then passed after removing the special case.
    - `cd remote-ui && npm test -- --run src/browserNetworkRuntime.test.ts` failed before the component-boundary fix, then `cd remote-ui && npm test -- --run src/browserNetworkRuntime.test.ts src/WebControlRemoteApp.test.tsx` passed after injection changes.
    - `cd termx-remote && go test . -run TestWF003HubProductImplementationOwnedByTermxRemote -count=1` failed before hub package migration, then passed after moving hub packages.
    - `cd termx-remote && go test . -run TestWF004LocalAndManagedUseSharedHubSessionFlow -count=1` failed before adding shared session flow, then passed after wiring local and managed paths.
    - `go test ./termx-core/... ./termx-remote/... ./termx-cli/... ./termx-hub/... -count=1` passed.
    - `cd remote-ui && npm test` passed: 46 files, 226 tests.
    - `cd remote-ui && npm run typecheck` passed.
    - `cd remote-ui && npm run build:localweb` passed and synced assets to `termx-remote/localweb/static`.
- 2026-05-04 WF-003/WF-008 independent re-review completed by subagent Mencius:
  - High: `termx-hub` still owns hub heartbeat/control-plane registration logic (`hubHeartbeatConfig`, heartbeat loop, payload construction, registry inspection, POST to control). Required fix: move heartbeat implementation into `termx-remote/hub`, leaving only env parsing/cmd adapter in `termx-hub`.
  - High: `termx-remote/hub/sessionflow` is only a path/ICE helper and does not centralize local/managed pairing/signaling/session orchestration. Required fix: move local and managed RTC answer orchestration through a shared session-flow orchestrator.
  - Closed by re-review: core remote branching/DTO/RPC surface, WebControl browser adapter injection, relay not becoming a client path, and workflow prior review records.
- 2026-05-04 WF-003/WF-004 re-review fix TDD failures:
  - `cd termx-remote && go test ./hub/sessionflow -count=1` failed before implementation because `AnswerLocal`, `AnswerManaged`, and `AnswerInput` did not exist.
  - `cd termx-remote && go test . -run TestWF004LocalAndManagedUseSharedHubSessionFlow -count=1` failed before implementation because the strengthened boundary test required `sessionflow.AnswerLocal` and `sessionflow.AnswerManaged` call sites.
  - `cd termx-hub && go test ./... -count=1` failed before the heartbeat ownership fix because `main.go` still called `heartbeatpkgRunLoop` and `main_test.go` still referenced removed `postHubHeartbeat` / `hubHeartbeatConfig`.
- 2026-05-04 WF-003/WF-004 re-review fixes:
  - Added `termx-remote/hub/heartbeat` as the owner of heartbeat payload construction, registry machine inspection, control POST, and loop execution; `termx-hub` now only parses env into `heartbeat.Config` and starts `heartbeat.RunLoop`.
  - Moved the heartbeat behavior test into `termx-remote/hub/heartbeat` and removed product heartbeat tests from `termx-hub/cmd`.
  - Added `sessionflow.AnswerLocal` and `sessionflow.AnswerManaged`, both using a shared `AnswerInput` / `Answerer` orchestration boundary before delegating to RTC answer implementation.
  - Updated local web RTC answer handling to call `sessionflow.AnswerLocal`; updated managed agent cloud offer handling to call `sessionflow.AnswerManaged`.
  - Added boundary checks preventing hub heartbeat implementation symbols in `termx-hub/cmd` and requiring the shared session-flow answer call sites.
  - Focused validation after fixes:
    - `cd termx-remote && go test ./hub/heartbeat -count=1` passed.
    - `cd termx-remote && go test ./hub/sessionflow -count=1` passed.
    - `cd termx-remote && go test . -run 'TestWF003HubProductImplementationOwnedByTermxRemote|TestWF004LocalAndManagedUseSharedHubSessionFlow' -count=1` passed.
    - `cd termx-hub && go test ./... -count=1` passed.
  - Relevant broader validation after fixes:
    - `cd termx-remote && go test ./agent/runtime . -count=1` passed.
    - `cd termx-remote && go test ./... -count=1` passed.
    - `rg -n "hubHeartbeatConfig|postHubHeartbeat|runHubHeartbeatLoop|heartbeatpkgRunLoop|type hubHeartbeat" termx-hub termx-remote -g '*.go'` found only boundary-test forbidden strings.
- 2026-05-04 WF-003/WF-008 final review by subagent Hegel:
  - No blockers found; Mencius high findings are closed.
  - Confirmed heartbeat payload construction, registry inspection, control POST, and loop are owned by `termx-remote/hub/heartbeat`; `termx-hub/cmd` only builds config and starts the remote-owned loop.
  - Confirmed local RTC answering uses `sessionflow.AnswerLocal`, managed cloud offers use `sessionflow.AnswerManaged`, and `relay` remains rejected as a client path.
  - Residual risk: `termx-hub/AGENTS.md` still described heartbeat as a hub responsibility in a way that could mislead future work. Fixed by clarifying `termx-hub` is executable/config adapter and hub product logic belongs in `termx-remote/hub`.
- 2026-05-04 final regression after review fixes:
  - `go test ./termx-core/... ./termx-remote/... ./termx-cli/... ./termx-hub/... -count=1` passed.
  - `cd remote-ui && npm test` passed: 46 files, 226 tests.
  - `cd remote-ui && npm run typecheck` passed.
  - `cd remote-ui && npm run build:localweb` passed and synced assets to `termx-remote/localweb/static`.
  - `find termx-core -path 'termx-core/_tmux-src/.git' -prune -o \( -name remote -o -name '*remote*' \) -print` returned no active core remote paths.
  - `rg -n "github.com/lozzow/termx/termx-remote" termx-core -g '*.go'` returned no matches.
  - Core remote-name scan found only `termx-core/termx_test.go` coverage that asserts core does not special-case `remote.*` methods.
