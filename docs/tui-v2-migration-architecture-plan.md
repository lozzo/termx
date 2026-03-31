# TUI V2 迁移架构规划

状态：Draft
日期：2026-03-30

---

## 1. 背景与目标

当前 `termx` 的 TUI 已经具备可运行、可用的功能基础，尤其在以下方面已经积累了真实资产：

- `tui/render.go` 中基于 cell 的 compositor、row cache、pane cache、viewport 裁剪与光标映射
- `tui/layout.go` 中相对纯净的 pane 几何与 split/adjust 逻辑
- `tui/client.go` 中清晰的后端访问边界
- `tui/render_coordinator.go`、`tui/workbench.go`、`tui/app.go`、`tui/terminal_store.go` 中已经出现的“职责收拢”尝试

问题不在于“没有功能”，而在于**当前系统处在一个危险的过渡态**：

- 状态分散，ownership 不明确
- 同一行为存在多条解释路径
- 结构状态、运行时状态、渲染状态混在一起
- `Model` 过大，但又已经和 `App` / `Workbench` / `RenderLoop` / `TerminalStore` 形成半拆分状态
- `Model.workspace` 仍是主真相，而 `Workbench` 在很多路径上只是镜像/快照
- 原地重构容易遗漏行为、引入回归

因此，V2 的目标不是从头发明一套新 TUI，也不是对现有代码继续做大规模原地手术，而是：

> 在 `tuiv2/` 下建立一套可渐进迁移的新架构，
> 让旧代码继续作为行为参考和能力来源，
> 重点重建状态边界、控制流和模块职责，
> 同时尽可能复用 V1 已验证成熟的渲染与终端相关能力。

同时增加一条硬约束：

> `tuiv2/` 可以从现有 `tui/` 复制代码作为迁移起点，
> 但禁止保留任何代码层级的运行时/编译时依赖到旧 `tui`；
> 目标是删除旧 TUI 代码后，`tuiv2/` 仍可独立编译和运行。

---

## 2. 总体策略

### 2.1 不做大规模原地重写

本次重构明确采用：

- 新建与 `tui/` 平级的 `tuiv2/`
- 分阶段迁移
- 保留旧 `tui` 作为参考实现与回退基线
- 通过复制、改造、重组逐步复用旧逻辑

### 2.2 V2 重建上层，复用底层

V2 的重点是重建：

- 状态组织方式
- 输入解释链路
- 模块边界
- terminal pool / pane binding / render / structure 的职责分离
- 启动与恢复编排
- 跨 owner 复合动作的执行 contract
- 持久化 schema
- 从“双真相”过渡到“单真相”的迁移策略

V2 优先复用：

- `tui/client.go`
- `tui/layout.go`
- `tui/render.go`
- 现有 terminal attach / stream / snapshot / recover 语义
- 现有 terminal store / owner-follower / connection snapshot 思路

但复用方式必须是：

- 允许复制、改造、重组
- 不允许最终形成 `tuiv2/` 对旧 `tui/` 的编译时或运行时依赖

---

## 3. 必须先写死的架构规则

### 3.1 `bridge` 禁止成为 `tuiv2 -> tui` 的依赖口子

`bridge` 只允许做两类事情：

- 桥接协议层/后端层
- 桥接 `tuiv2/` 内部已经复制进来的兼容实现

明确禁止：

- import 旧 `tui/` 包
- 调用旧 `tui` 的 render/runtime/workbench 实现作为长期依赖
- 以文件级桥接保留 `tuiv2 -> tui` 的真实代码依赖

### 3.2 跨 owner 复合行为必须通过显式 orchestrator contract

对于下列行为：

- `close pane`
- `split + attach`
- picker 选择后 mutate workbench + 更新 terminal binding + 关闭 modal
- workspace restore 后 hydrate terminal registry / pane binding，并决定是否继续 bootstrap

不允许：

- 模块之间直接互相穿透调用
- 最终把全部业务吸回 `app.Update()`

必须定义：

- `SemanticAction`
- `Effect`
- `Msg`
- `orchestrator`

### 3.3 `persist` 采用 V2 schema

不再要求 `tuiv2` 继续以旧 workspace state schema 作为内部模型。

明确选择：

- `persist` 使用新的 V2 schema
- 旧 schema 只作为导入兼容层存在
- 旧 schema 不再反向决定 `tuiv2` 的内部结构

这意味着必须重新定义：

- `V2WorkspaceState`
- `V2TabState`
- `V2PaneState`
- `PersistedTerminalMetadata`

明确规则：

- 不再单独定义 `PersistedTerminalBinding`
- 绑定关系只由 `PaneEntryV2.TerminalID` 表达，并与 `workbench.PaneState.TerminalID` 保持一致
- `PersistedTerminalMetadata` 的 canonical owner 是 `WorkspaceStateFileV2` 顶层，而不是 tab 级
- `PersistedTerminalMetadata` 在 Phase 0 就存在，但第一版只持久化 `Name` / `Command` / `Tags`
- `TerminalState` / `ExitCode` **不进入 V2 持久化 schema**，它们属于运行时派生状态

### 3.4 输入必须分成两条通道

必须区分：

- **Semantic Action**：split、focus、open picker、submit prompt、close pane 等
- **Terminal Input / Passthrough**：直接送入 active terminal 的 bytes / paste / encoded key

### 3.5 `input` 是 active mode 的唯一真源

明确选择：

- `input.ModeState.Kind` 是唯一可写 mode 真相
- `ModalHost` 是 modal 局部状态的唯一 owner
- `ModalSession.Kind` 只是与当前 active modal 对齐的派生态描述，不是第二份 mode 真相

### 3.6 modal 必须有完整异步生命周期 contract

必须显式定义：

- `ModalHost`
- `ModalSession`
- `OpenModalEffect`
- `LoadModalDataEffect`
- `ModalLoadedMsg`
- `ModalResultMsg`
- `CloseModalEffect`

### 3.7 runtime 必须 terminal-centric

runtime 必须至少拆成两层：

- `TerminalRegistry`
- `PaneBinding`

### 3.8 binding 必须有单一可写 owner

明确规则：

- **结构上的绑定意图** 由 `workbench.PaneState.TerminalID` 持有
- `runtime.PaneBinding` 只持有运行时连接态、角色、channel/recovery 相关信息，不再重复存储独立可写的 `TerminalID`
- persist 只序列化结构上的绑定意图，与 `workbench` 对齐
- `TerminalRegistry` 中若存在 `OwnerPaneID` / `BoundPaneIDs`，它们只能是**只读派生缓存**，不得成为第二份可写绑定真相

### 3.9 V1 复杂显示模型不进入一期核心 state contract

`fit/fixed/pin/readonly/auto-acquire` 这套模型不写入 V2 一期核心状态。

### 3.10 必须单独规划“双真相 -> 单真相”迁移

当前至少存在：

- `Model.workspace`
- `Workbench.current/store`

迁移计划中必须明确每一阶段的唯一主真相是谁。

---

## 4. V2 目录规划

推荐目录结构：

```text
tuiv2/
├── app/
│   ├── model.go
│   ├── messages.go
│   ├── update.go
│   ├── view.go
│   ├── commands.go
│   └── services.go
├── orchestrator/
│   ├── orchestrator.go
│   ├── effects.go
│   └── msgs.go
├── workbench/
│   ├── types.go
│   ├── workbench.go
│   ├── workspace.go
│   ├── tab.go
│   ├── pane.go
│   ├── floating.go
│   ├── layout.go
│   ├── mutate.go
│   └── visible.go
├── runtime/
│   ├── types.go
│   ├── terminal_registry.go
│   ├── pane_binding.go
│   ├── runtime.go
│   ├── create_attach.go
│   ├── stream.go
│   ├── recovery.go
│   ├── resize.go
│   ├── input.go
│   └── snapshot.go
├── input/
│   ├── actions.go
│   ├── terminal_input.go
│   ├── router.go
│   ├── mode.go
│   ├── keymap.go
│   ├── raw.go
│   └── translate.go
├── render/
│   ├── renderer.go
│   ├── coordinator.go
│   ├── adapter.go
│   ├── frame.go
│   ├── overlays.go
│   └── cache.go
├── modal/
│   ├── host.go
│   ├── session.go
│   ├── picker.go
│   ├── terminal_manager.go
│   ├── prompt.go
│   ├── help.go
│   └── workspace_picker.go
├── bootstrap/
│   ├── bootstrap.go
│   ├── restore.go
│   ├── layout.go
│   └── startup.go
├── persist/
│   ├── workspace_state.go
│   ├── schema_v2.go
│   └── legacy_import.go
├── bridge/
│   ├── client.go
│   └── protocol_types.go
└── shared/
    ├── config.go
    ├── errors.go
    ├── ids.go
    └── clock.go
```

---

## 5. 模块职责定义

### 5.1 `app`：Bubble Tea 壳层

职责：

- 承载 Bubble Tea 根 `Model`
- 接收顶层 `tea.Msg`
- 将输入消息交给 `input.Router`
- 将 runtime / modal / orchestrator 返回消息分发给对应 owner
- 选择当前显示主工作台还是 modal
- 调用 `render.Coordinator` 产出最终 frame

### 5.2 `orchestrator`：跨 owner 行为编排层

职责：

- 接收 `SemanticAction`
- 调用 `workbench` / `runtime` / `modal` 的明确接口
- 组装 `Effect`
- 处理跨模块动作的顺序、补偿和用户提示

### 5.3 `workbench`：结构状态中心

职责：

- 管理 workspace store / order / active workspace
- 管理 tab、pane、floating pane、zoom、layout preset
- 管理 pane 的结构标识与**结构绑定意图**
- 提供 visible state 给 render 层使用

建议核心结构：

```go
type PaneState struct {
    ID         string
    Title      string
    TerminalID string // 结构上的绑定意图，唯一可写 owner
}
```

### 5.4 `runtime`：terminal-centric 运行时中心

runtime 必须拆成两层：

#### `TerminalRegistry`
- 管理 terminal 级一级实体
- 保存 terminal metadata、state、channel、snapshot、stream ownership、owner/follower 关系
- `OwnerPaneID` / `BoundPaneIDs` 若存在，只是只读派生缓存
- `TerminalRuntime` 中的 `Name` / `Command` / `Tags` 等 metadata 字段是从 server event / List 结果同步过来的**派生数据**，不是第二份 canonical source

#### `PaneBinding`
- 管理 pane 的运行时连接态
- 管理 pane 在当前 terminal 上的角色与局部 attach/runtime 视图
- 不再作为结构绑定真相 owner

### 5.5 `input`：统一输入解释层

职责：

- 接收 `tea.KeyMsg`、uv event、raw bytes
- 统一维护输入 mode
- 输出 `SemanticAction` 或 `TerminalInput`

### 5.6 `modal`：带异步生命周期的浮层子系统

职责：

- `ModalHost` 是 modal 局部状态的唯一 owner
- `ModalSession` 是 active modal 的异步生命周期状态
- `input` 是 active mode 真相

### 5.7 `render`：高性能渲染层

职责：

- 管理 render invalidation / schedule / flush / ticker / stats
- 接收 workbench + runtime 的 visible projection
- 驱动迁移后的 compositor 实现
- 产出最终 frame

### 5.8 `bootstrap`：启动与恢复编排

职责：

- attach 初始 terminal
- 加载 workspace state
- 自动发现 layout
- 启动时 fallback 到 picker 或 create first pane

### 5.9 `persist`：持久化层

职责：

- workspace state 导入导出
- V2 schema 管理
- 旧 schema 兼容导入

要求：

- `PersistedTerminalMetadata` 的 canonical owner 在 `WorkspaceStateFileV2` 顶层
- 不单独定义 `PersistedTerminalBinding`
- `TerminalState` / `ExitCode` 不持久化
- `legacy_import.go` 负责 V1 `workspaceStateFile` → V2 `WorkspaceStateFileV2` 的单向转换
- V1 schema struct 定义必须**复制进** `legacy_import.go`，不可 import 旧 `tui/`

### 5.10 `bridge`：协议与迁移辅助层

职责：

- 桥接现有 `Client`
- 承接协议层类型适配
- runtime 通过它接触协议层类型

---

## 6. 模块依赖方向

建议固定如下依赖方向：

- `app` -> `input` / `orchestrator` / `render` / `modal` / `bootstrap` / `workbench`(只读) / `runtime`(只读) / `shared`
- `orchestrator` -> `workbench` / `runtime` / `modal` / `persist` / `bootstrap` / `shared`
- `render` -> 只读依赖 `workbench` 与 `runtime`
- `runtime` -> `bridge` / `shared`
- `workbench` -> `shared`
- `persist` -> `workbench` / `shared`
- `modal` -> `input` / `shared`
- `bootstrap` -> `persist` / `workbench` / `runtime` / `shared`
- `bridge` -> 只做协议与 client 适配，不进入 render/runtime/workbench 主链路

说明：`app` 持有 `workbench` 和 `runtime` 的只读引用，用于将 visible state 注入 render 层；`app` 不通过它们做业务编排（编排走 `orchestrator`）。

---

## 7. Phase 0 Canonical Manifest

Phase 0 唯一有效文件清单如下：

```text
tuiv2/shared/config.go
tuiv2/shared/errors.go
tuiv2/shared/ids.go
tuiv2/shared/clock.go
tuiv2/bridge/client.go
tuiv2/bridge/protocol_types.go
tuiv2/input/actions.go
tuiv2/input/terminal_input.go
tuiv2/input/mode.go
tuiv2/input/router.go
tuiv2/input/keymap.go
tuiv2/input/raw.go
tuiv2/input/translate.go
tuiv2/modal/host.go
tuiv2/modal/session.go
tuiv2/modal/picker.go
tuiv2/modal/terminal_manager.go
tuiv2/modal/prompt.go
tuiv2/modal/help.go
tuiv2/modal/workspace_picker.go
tuiv2/workbench/types.go
tuiv2/workbench/workbench.go
tuiv2/workbench/workspace.go
tuiv2/workbench/tab.go
tuiv2/workbench/pane.go
tuiv2/workbench/floating.go
tuiv2/workbench/layout.go
tuiv2/workbench/mutate.go
tuiv2/workbench/visible.go
tuiv2/runtime/types.go
tuiv2/runtime/terminal_registry.go
tuiv2/runtime/pane_binding.go
tuiv2/runtime/runtime.go
tuiv2/runtime/create_attach.go
tuiv2/runtime/stream.go
tuiv2/runtime/recovery.go
tuiv2/runtime/resize.go
tuiv2/runtime/input.go
tuiv2/runtime/snapshot.go
tuiv2/render/renderer.go
tuiv2/render/coordinator.go
tuiv2/render/adapter.go
tuiv2/render/frame.go
tuiv2/render/overlays.go
tuiv2/render/cache.go
tuiv2/bootstrap/bootstrap.go
tuiv2/bootstrap/restore.go
tuiv2/bootstrap/layout.go
tuiv2/bootstrap/startup.go
tuiv2/persist/schema_v2.go
tuiv2/persist/workspace_state.go
tuiv2/persist/legacy_import.go
tuiv2/orchestrator/orchestrator.go
tuiv2/orchestrator/effects.go
tuiv2/orchestrator/msgs.go
tuiv2/app/model.go
tuiv2/app/messages.go
tuiv2/app/update.go
tuiv2/app/view.go
tuiv2/app/commands.go
tuiv2/app/services.go
```

这份清单是唯一 canonical manifest；其他文档不再各自维护另一套 Phase 0 列表。

---

## 8. 推荐评审重点

- `PersistedTerminalBinding` 是否已彻底移除
- `PersistedTerminalMetadata` 是否已上提到 `WorkspaceStateFileV2` 顶层
- `app -> shared`、`orchestrator -> shared`、`bootstrap -> shared` 是否已写入允许依赖
- `app -> workbench(只读) / runtime(只读)` 的注入路径是否已明确
- runtime 是否仍守住 `runtime -> bridge` 而非 `runtime -> protocol`
- reverse binding 字段是否已明确为只读派生缓存
- `ModeState.Kind` 与 `ModalSession.Kind` 的真相/派生关系是否已写死
- `TabEntryV2` 是否包含 layout tree 字段
- `legacy_import.go` 是否明确为 V1→V2 单向转换，V1 struct 内嵌而非 import
- `Orchestrator` struct 是否持有 workbench / runtime / modalHost 引用
- Phase 0 manifest 是否成为唯一文件清单来源

---

## 9. 结论

现在的目标不是再加新概念，而是把最后一轮一致性冲突全部消除，让文档从“方向正确”变成“开工时不会误判”。
