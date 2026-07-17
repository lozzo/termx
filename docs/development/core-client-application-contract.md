# Core 与 Client Application Contract

## 问题

当前 terminal application 数据存在三种结构：

- `core/history` 与 `core` 中的 daemon domain projection。
- `internal/protocol` 中的 wire-facing Go message。
- `tui/state` 中的 reducer/view projection。

三种结构不等于三份真值，但如果 `client/runtime` 再复制 terminal/history/live/file/workbench DTO，就会形成第四套长期模型，并让字段语义、generation 和兼容字段继续漂移。

因此，client runtime application interface 不能直接使用 `internal/protocol`，也不能重新定义 daemon-local domain DTO。必须先建立 core-owned、wire-independent、UI-independent 的 application projection contract。

## 目标 ownership

```text
core implementation / core history truth
                |
                v
             core/api
       daemon-owned projection DTO
          /                 \
         v                   v
internal/protocol       client/runtime
wire encode/decode      EndpointID + session stamp
         |                   |
         v                   v
      daemon wire       TUI / CLI / binding adapter
```

- `core/api` 拥有 daemon-local terminal lifecycle、history window、native live screen、path、file 和 workbench projection 的字段语义。
- `core/api` 不 import storage backend、core runtime implementation、TUI、Cobra、client runtime、protocol 或 wire protobuf。
- `internal/protocol` 只负责 `core/api <-> wire` 编解码，不重新拥有业务字段语义。
- `client/runtime` 只为 core API request/result 增加 `EndpointID`、`EndpointSessionStamp`、`AttachmentStamp` 和 runtime error/lifecycle 语义。
- `tui/state` 只保留 reducer/view 所需交互状态。需要本地 reflow、selection 或 pane binding 的字段属于 UI projection；daemon lifecycle/history truth 仍来自 `core/api`。

## API 分组

```text
core/api/
  terminal.go       TerminalInfo、TerminalSize、create/list/mutation metadata
  attachment.go     daemon-local channel、resize ownership/control
  history.go        token、cursor、boundary、logical line、window、copy range
  live.go           semantic cell、cursor、terminal modes、native screen、invalidation
  path.go           defaults 与 directory candidates
  file.go           metadata、list、mutation、high-level transfer result
  workbench.go      workspace/tab/pane/split snapshot 与 mutation
```

## 接口先行顺序

1. 从当前 core domain 和 consumer 使用点列出稳定字段、旧兼容字段和 UI-only 字段。
2. 在 `core/api` 定义最小 DTO 与 validation，不接 protocol，不移动 implementation。
3. 使用 fixture/harness 对比 core 当前输出语义，证明 API 不丢 authoritative boundary。
4. core implementation 改为产出 `core/api`；删除 core 内重复 projection type。
5. protocol adapter 映射 `core/api` 与 wire；删除 protocol 业务 type owner。
6. client runtime application interface 使用 `core/api` 并增加 endpoint/session stamp。
7. TUI protocol adapter 改为 protocol/clientruntime adapter，最终删除 TUI 对 wire message 的直接依赖。

## History 规则

- physical row/cell 与 seal lifecycle truth 仍在 core implementation，不进入 client/TUI state。
- `HistoryWindow` 是 core API 的 authoritative query projection，token/cursor/boundary/generation 只能原样传回。
- TUI local reflow、selection anchor 和 pane rows 是 UI projection，不写回 core API。
- `LoadedRows`、visual row offset、snapshot scrollback 和 render row 不能进入 API truth。
- 旧 compatibility 字段必须逐个判断是否仍有真实 consumer；不得原样复制到新 API。

## Live 规则

- `NativeScreenSnapshot` 只表达 latest semantic cell matrix、cursor、modes、size、revision 和 timestamp。
- API cell DTO 不 import renderer、DOM/canvas 或 TUI style；default color 保持语义属性，明确 RGB 保持内容属性。
- live revision 与 history generation 是两个独立空间，不能共享字段或 stale guard。
- invalidation 是 wake signal，不是 frame delivery。

## File 与 Workbench 规则

- file application contract 使用高层 list/stat/mutation/read/write/transfer result，不向 CLI/TUI 暴露 protocol channel、frame type、ACK window 或 transfer goroutine。
- workbench 只保存 workspace/tab/pane layout 与 terminal binding intent，不包含 terminal lifecycle、history、session generation 或 transport。

## 停止条件

- `core/api` 需要 import `internal/protocol`、wire protobuf、TUI 或 client runtime。
- client runtime 继续复制 daemon-local `TerminalInfo`、`HistoryWindow` 或 `NativeScreenSnapshot`。
- 为兼容当前 wire 原样保留没有 consumer 的字段。
- TUI reducer type 被当作跨端 API 或 wire contract。
- core、protocol、client、TUI 同时修改同一状态字段并形成多 owner。
