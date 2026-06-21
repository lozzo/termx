# v2/v3 默认入口与 remote 迁移审计

本文档只保留当前分支仍有效的边界。旧切片细节按 git 历史追溯；如与根目录 `workflow.md` 冲突，以 `workflow.md` 为准。

## 当前结论

- 默认本地入口已经切到 `termx-core-v2` + `termx-tui-v3`。
- 旧 `termx-core/` 与 `tuiv2/` 已删除，不再作为只读参考、legacy 命令、remote fallback 或 go.mod replace 存在。
- `termx legacy ...`、旧 daemon auto-start、`remote_runtime.go` 和 `remote_protocol_codec.go` 已删除。
- `termx remote ...` 只能通过 `resolveV3Socket` + `dialOrStartV3Client` 连接 core-v2 daemon；remote status/local/pair 已由 core-v2 typed `RemoteService` hook 承接，不再有旧 daemon 或旧 adapter fallback。
- remote runtime API 的 terminal management、storage、events 和 WebRTC/datachannel transport 都经 `termx-remote.Service` 路由到 core-v2 daemon truth；remote 不持有第二份 terminal/storage truth。
- remote config/auth 路径由 CLI helper 处理，不再依赖 `tuiv2/shared`；显式 `--config` 会传给 core-v2 daemon auto-start 与 remote bootstrap。
- 真实 `termx-app/` 与 `remote-ui/` 已完成 remote runtime/history/copy contract 收口；App 可以保留 live display/cache，但 copy/history truth 必须走 core-v2 logical-line history/window/copy。

## 命令矩阵

| 命令 | 当前路径 | 关键约束 |
| --- | --- | --- |
| `termx` | core-v2 daemon + tui-v3 root runtime | 不读取旧 TUI config，不恢复 `runTUIv2` |
| `termx attach <id>` | tui-v3 attach runtime | 只绑定 core-v2 terminal |
| `termx daemon` | core-v2 `NewServer` | 不构造旧 core server |
| `termx new/ls/kill/rm` | core-v2 protocol client | 默认 socket 使用 v3 路径 |
| `termx remote login` | CLI HTTP login + CLI config/auth helper | 不依赖旧 TUI shared helper |
| `termx remote status/local/pair` | core-v2 protocol client + `termx-remote.Service` hook | 不调用旧 core daemon；`remote.local.*` 生命周期跟随 core-v2 daemon |
| remote runtime API terminal/storage/events | `termx-remote.Service` -> core-v2 daemon adapter | terminal lifecycle、metadata、storage 和 events 只来自 core-v2 truth |
| remote WebRTC/datachannel transport | `termx-remote.Service` -> core-v2 `ServeScopedTransport` | terminal datachannel 受 terminal scope 约束；`machine-events` 是受限 protocol transport |

## 守卫规则

- `termx-cli/cmd/termx/default_dependency_guard_test.go` 扫描所有非测试 CLI 源文件，包括 `remote_*.go`。
- 默认和 remote CLI 源文件不得 import `github.com/lozzow/termx/termx-core` 或 `github.com/lozzow/termx/tuiv2`。
- `legacy_*.go` 不得重新出现。
- `go.work`、`termx-cli/go.mod`、`termx-testkit/go.mod` 和相关 module 文件不得恢复旧 `termx-core` / `tuiv2` replace。

## remote 后端迁移结果

R176-R187 已按 core-v2 contract 收口 remote 后端，不允许补旧 fallback：

1. `termx-proto` / `internal/protocol` 已对齐 remote wire 与 typed domain contract。
2. core-v2 已提供 remote service hook、transport scope API、terminal create/process 字段和 storage/events routing。
3. CLI daemon 已用 core-v2 daemon/domain adapter 装配 `termx-remote.Service`。
4. `remote.status`、`remote.local.*`、`remote.pair.start`、remote terminal/storage/events 和 remote transport session 都有 core-v2 truth smoke。
5. `remote_backend_contract_test.go` 串联验证 daemon socket remote hook、runtime API、events、storage、scoped transport，并守卫旧 fallback 目录/文件不得恢复。

## App 集成结果

R189-R196 已按真实 App 入口和 shared UI/runtime 收口，不得把 App 侧缓存升级成历史 truth：

1. `termx-app/` 仍通过 `RemoteControlApp` 注入 native/browser runtime，native 本地探测走 CLI remote local/hub 的 `/api/v1/sessions/ice`，不再访问 App 私有 local status API。
2. App terminal list/create/attach/input/resize/restart/remove 都经同一个 core-v2 runtime session 的 API channel 或 terminal datachannel。
3. live surface 可以保留 xterm.js、短 scrollback、native bridge backlog 或 render cache 作为显示 projection。
4. App infinite history 由 `remote-ui` 的 `CoreV2HistorySource`、`CoreV2HistorySurface` 和 `CoreV2HistoryInteraction` 分层承接：source 调 `history.window`/`history.copy`，surface 只持有 render/cache window，selection/search/copy 只用 logical-line range。
5. copy/search/selection 和无限历史回滚必须请求 core-v2 logical-line history/copy contract，不能从 xterm buffer、snapshot rows、DOM/canvas rows 或 App 本地 append log 拼最终文本。
6. `termx-app-history-ref/` 只作为窗口化渲染、overscan、cache 和 renderer 参考；其中 mock source、visual-row truth 或 demo 数据不得进入协议或 App 历史真值。

## 验收命令

当前 remote 后端 checkpoint 的基线验收：

| 范围 | 命令 |
| --- | --- |
| CLI | `cd termx-cli && go test ./cmd/termx -count=1` |
| 默认依赖守卫 | `cd termx-cli && go test ./cmd/termx -run TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI -count=1` |
| core-v2 | `cd termx-core-v2 && go test ./... -count=1` |
| remote | `cd termx-remote && go test ./... -count=1` |
| remote backend contract | `cd termx-cli && go test ./cmd/termx -run TestRemoteBackendContractRoutesEverythingThroughCoreV2Truth -count=1` |
| 旧 fallback 文件守卫 | `cd termx-cli && go test ./cmd/termx -run TestRemoteBackendLegacyFallbackBoundaryIsGone -count=1` |
| App remote-ui | `cd remote-ui && npm run typecheck && npm run test && npm run build` |
| App build | `cd termx-app && npm run build` |
| App end-to-end smoke | `cd remote-ui && npm run test -- src/integration/appCoreV2EndToEndSmoke.test.ts` |
| App history/copy harness | `cd remote-ui && npm run test -- src/terminal/coreV2HistorySource.test.ts src/terminal/coreV2HistorySurface.test.ts src/terminal/coreV2HistoryInteraction.test.ts` |
| remote 旧 import 检查 | `rg '"github.com/lozzow/termx/termx-core"|\"github.com/lozzow/termx/tuiv2' termx-cli/cmd/termx/remote_*.go -n` 应无结果 |
| diff | `git diff --check` |

Android/Kotlin 编译需要本机 Android SDK。当前机器已有 Homebrew 安装的 OpenJDK 21，但
`termx-app/android/local.properties` 指向的 `/Users/lozzow/Library/Android/sdk`
不存在，因此 App native 编译未作为当前 checkpoint 的通过条件。
