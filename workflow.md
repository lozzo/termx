# 工作流：Proto API 架构收口

## 当前结论

- 当前最早未完成切片是 `PA005A3`。`PA005A2R` 已按 client-owned origin session、connection-bound atomic admission、transactional resource publication 和 descriptor baseline 完成修正，架构 reviewer 与代码 reviewer 均明确 PASS。
- 用户已确立仓库级强约定：所有插件、第三方客户端、官方客户端、跨进程和跨语言 API 的唯一 schema truth 必须位于 `proto/`。
- 完整运行链路固定为 `插件/客户端 -> transport/platform binding -> protocol framing -> generated proto -> api_layer -> api_mapping -> core`，返回方向相反。Proto 是 schema/message truth，不是 transport 或主动运行层；任何入口都不得绕过 API Layer 消费 core domain struct。
- `core/api` Go DTO 路线已判定错误，必须删除；此前 `AR003B1A/AR003B1B` 结论作废，不得继续迁移或补兼容层。
- 当前仓库仍是私有开发真值。插件系统实现仍在独立分支；本分支只建设插件和第三方客户端未来必须依赖的公共 Proto API 基础，不恢复插件 runtime。

## 已完成基线

- Endpoint/Route registry、assembler 与 portable bootstrap/share contract 位于 `client/endpoint`。
- `client/runtime` 已定义 connection/session generation、attempt、ReadySession、错误和 Clock 基础 contract。
- TUI 已拆为 `tui/port`、`tui/adapter/*`、`tui/testkit`，并有依赖守卫。
- CLI concrete dependency 债务已有冻结清单；当前明确编译缺口不得用 stub、fallback 或旧 helper 恢复。
- `proto/wirepb`、`proto/remoteauthpb`、`proto/cloudpb` 等已有 schema 是迁移输入，不代表当前 API ownership 已经清晰。

## Proto API 硬边界

```text
plugins / CLI / TUI / mobile / desktop / web / third-party clients
       |
       v
transport or platform binding
  Unix Socket / TCP+TLS / SSH / WebRTC DataChannel / JNI / Swift / WASM
       |
       v
protocol framing
  Hello / channel / correlation / payload transport
       |
       v
generated proto command / result / event
       |
       v
api_layer
  application dispatch / auth / session fence / cancel / lifecycle / stable errors
       |
       v
api_mapping
  generated proto <-> core domain; stateless and deterministic
       |
       v
core domain truth
```

- `proto/` 拥有 API message、enum、oneof、command/event envelope、capability、version 和兼容语义。
- `api_layer/` 只使用 proto 生成 request/result/event；不定义平行业务 DTO，不处理 transport framing。
- `api_mapping/` 是 generated proto 与 core domain 的唯一字段映射点；它不是 transport，不拥有连接、framing、状态、权限、route、fallback、重试或 session。
- `internal/protocol/` 只保留 framing、Hello、channel、correlation、payload transport 和连接级错误；不得重新拥有 proto 业务字段。
- `core/` 可以保留内部领域类型，但不得把内部类型作为客户端/插件 API。
- `client/runtime` 可以拥有 endpoint/session stamp、opaque handle 和 runtime lifecycle，但业务 command/result/event 必须来自 proto。
- `tui/port` 与 UI state 可以拥有 UI-only view model；凡表达 daemon/client application API 的字段必须由 proto 经 API Mapping 投影，不得复制成另一份契约真值。
- 所有 schema 修改顺序固定为：proto -> generated code -> round-trip/compatibility harness -> API Layer -> API Mapping -> core adapter -> transport/consumer。

## 停止条件

- 新增跨边界 Go DTO、interface request/result 或 event，但没有对应 proto schema。
- API Layer 或 API Mapping 依赖 TUI、Cobra、具体 transport、Cloud implementation 或插件实现。
- protocol package 同时拥有业务 DTO 和 wire codec，或 core/client/TUI 各维护一份同字段结构。
- API Mapping 建立连接、处理 framing，或持有缓存、goroutine、session、route winner、history truth、权限或 reducer state。
- 为维持编译增加 alias、兼容 wrapper、双编码、旧 method fallback、nil runtime 或 panic stub。
- proto schema 无法表达 endpoint/session stamp、错误、取消、资源释放或 capability/version 时继续写实现。

## 当前允许范围

- 主动范围：`AGENTS.md`、`workflow.md`、`docs/development/`、`proto/`、`api_layer/`、`api_mapping/`、`internal/protocol/`、`client/runtime/` 与对应 tests/guards。
- 受限联动：`core/`、`client/adapter/`、`tui/{port,adapter,testkit}/`、`cmd/termx/`、`remote/`、`private/cloud/`，只能为当前 Proto API 迁移切片最小触及。
- 禁止范围：插件 runtime、`private/archive/`、多区域 Cloud、正式开源工程、真实 Android/iOS/Desktop/WASM binding，除非先修改本文件。

## 任务队列

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| PA001 | 已完成 | 固化 Proto API 强约定并清理错误基线 | 更新 AGENTS/workflow/架构文档；删除 `core/api` 和未接线的重复 application DTO；恢复 clean compile baseline；文档和守卫不再宣称 Go DTO 是 API truth |
| PA002 | 已完成 | Proto API inventory 与缺口表 | 列出 terminal/attachment/path/history/live/file/storage/workbench/endpoint/access/cloud 的现有 proto、Go-only DTO、consumer、兼容字段和缺失 command/event/error；不写实现 |
| PA003 | 已完成 | 公共 API schema 与 envelope | 在 `proto/` 定义 versioned command/result/event/error/capability/resource handle；区分公共 application API 与内部 wire framing；生成代码和 compatibility harness 通过 |
| PA004 | 已完成 | API Layer 与 API Mapping 骨架 | 建立 `api_layer/` 与 `api_mapping/`；只依赖 core domain interface 与生成 proto；静态守卫禁止 UI/transport/private/state owner；fake harness 覆盖取消、释放和错误 |
| PA005A1 | 已完成 | Terminal/attachment/path Proto schema | 定义 TerminalRef、lifecycle、attachment/input/resize、path typed command/result/event；加入 application envelope、生成代码和 compatibility harness |
| PA005A2 | 已完成 | Terminal API Layer 与 API Mapping | proto validation、core domain mapping、typed error/cancel/release；不接 protocol transport 或 UI |
| PA005A2R | 已完成 | Proto API 基础契约审查修正 | envelope 顶层 context 保留未知 command correlation；result/event 回显 origin session；API Layer 使用 connection-bound atomic admission lease；resource handle 绑定 origin session 且事务式发布；稳定错误、enum/数值边界、response clone、descriptor baseline 和双 reviewer 通过 |
| PA005A3 | 待开始 | Core/protocol terminal adapter 迁移 | protocol framing 把 proto payload 交给 API Layer，API Layer 经 API Mapping 调用 core；删除 terminal/attachment/path protocol DTO，不使用 alias |
| PA005A4 | 待开始 | Terminal consumer 与守卫收口 | client runtime、CLI/TUI/remote consumer 使用 proto；删除重复 application DTO；依赖守卫与测试通过 |
| PA005B | 待开始 | History/live 迁移 | authoritative history window/native screen API 进入 proto；保持 history/live revision 边界；删除重复 projection owner |
| PA005C | 待开始 | File/storage/workbench 迁移 | file 隐藏 frame/channel，storage 保持 opaque，workbench 只表达 client intent；删除旧专用或重复 DTO |
| PA006 | 待开始 | Protocol 与 consumer 收口 | protocol 只传 proto payload；CLI/TUI/remote/Cloud 通过 transport/protocol/API Layer/API Mapping 链路；Go-only API 债务和 concrete dependency 清单归零或有明确延期 |
| PA007 | 待开始 | 架构就绪双审 | import graph、schema coverage、重复 DTO、fallback、生成代码、文档和 tests 通过；架构 reviewer 与代码 reviewer 明确 PASS 后恢复 C3B |
| C3B | 暂停 | RouteSelectionPlanner | PA007 PASS 后恢复 |
| C3C | 暂停 | fresh daemon proof / ReadySession | PA007 PASS 后恢复 |
| C3D | 暂停 | shared runtime session owner | PA007 PASS 后恢复 |
| C3E | 暂停 | CLI 接入共享 runtime | PA007 PASS 后恢复 |
| C3F | 暂停 | operation generation stamp | PA007 PASS 后恢复 |
| C3G | 暂停 | local + SSH race E2E | PA007 PASS 后恢复 |
| C3H | 暂停 | 最终准入与双审 | PA007 PASS 后恢复 |

## 测试准入

- `PA001`：`git diff --check`；`GOWORK=off go test ./client/... ./tui/... -run '^$'`；可编译的 core/protocol contract packages；确认不存在 `core/api` import。
- `PA002`：`git diff --check`；inventory 中每项必须有 schema owner、consumer、迁移目标和删除条件。
- `PA003`：生成代码检查；proto round-trip、unknown-field/compatibility、enum/oneof/version harness；`git diff --check`。
- `PA004`：API Layer/API Mapping unit tests 与 dependency guards；取消、资源释放和错误映射 harness。
- `PA005A2R`：generated-code check；descriptor baseline；Proto/API Layer/API Mapping race tests；client-owned origin session、atomic admission lease、command authorization、resource ownership、unknown command correlation、typed error 和边界 validation harness；`git diff --check`。
- `PA005-PA006`：对应 core/protocol/client/TUI/CLI tests；迁移后的重复类型与旧 helper 扫描；必要 race/E2E。
- `PA007`：全量可运行测试、generated-code check、import graph、重复 schema/DTO 扫描和双 Agent 审查。

## 执行规则

1. 每轮先读取本文件和 `AGENTS.md`，再检查 worktree。
2. 只执行最早的进行中/待开始切片，不跨切片扩展。
3. 先改 proto，再生成并补 compatibility harness，再写 API Layer/API Mapping，最后迁移 core、protocol transport 与 consumer；不得逆序。
4. 迁移完成必须删除旧 Go-only API，不保留 alias、wrapper、双路径或 fallback。
5. 每个切片必须说明 schema owner、domain owner、truth source、转换点、消息链路、取消、释放和失败条件。
6. 每个有效切片运行准入并使用中文提交信息提交。
