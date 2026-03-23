# termx TUI 当前状态

状态：Reset Stage
日期：2026-03-23

---

## 1. 当前判断

termx TUI 当前处于“主线重定义已开始、代码重建未开始”的阶段。

现状可以概括为：

- 旧版 TUI 已归档到 `deprecated/tui-legacy/`
- 原 `tui/` 主线路径当前为空
- 新主线文档已经重新建立
- 新主线代码还没有开始正式落地

---

## 2. 已完成

当前已经完成的事情：

1. 旧版资产已归档
2. legacy 设计和代码已做第一轮整理
3. 新主线产品概念已重新收口
4. 新主线交互规则、线框、架构、测试策略、交付计划已成文
5. 第一批 TDD 代码骨架已经落地
6. 第二轮 TDD 已补上 `close pane` 语义和 workspace picker 树状态机

对应文档：

- `product-spec.md`
- `interaction-spec.md`
- `wireframes.md`
- `architecture.md`
- `testing-strategy.md`
- `implementation-roadmap.md`

当前已经落到代码里的支点：

- `tui/domain/types`
- `tui/domain/layout`
- `tui/domain/connection`
- `tui/domain/workspace`
- `tui/app/intent`
- `tui/app/reducer`
- `tui/runtime.go`
- `tui/client.go`

本轮新增并通过测试的能力：

- `layout` 纯逻辑树和矩形投影
- `connection` 的 connect / owner / migrate 基线
- `workspace picker` 树构建与 query 命中祖先展开
- `workspace tree jump` 焦点决议
- `ConnectTerminalIntent`
- `StopTerminalIntent`
- `TerminalProgramExitedIntent`
- `ClosePaneIntent`
- `WorkspaceTreeJumpIntent`

---

## 3. 尚未开始

当前还没有正式开始的部分：

1. 新版 `layout` 领域层
2. 完整的 `intent -> reducer -> effect` 扩展面
3. 新版 bubbletea shell
4. 新版 renderer
5. 新版 terminal picker / terminal manager / workspace restore
6. 新版单测、场景回归、E2E 回迁

---

## 4. 当前最高优先级

下一阶段最高优先级不是补 UI，而是先把下面几个边界立住：

1. `workspace picker` 完整 reducer
2. `terminal manager connect here` reducer / effect
3. `overlay` 和 `mode` 状态迁移
4. 更完整的 `AppState / UIState`
5. 新版 bubbletea shell 接口

原因：

- 这些边界决定后续是否还会回到补丁式开发
- shared terminal 的复杂度必须先被模型化
- 输入路径必须先统一

---

## 5. 当前主要风险

### 5.1 文档和实现再次分叉

如果没有按新文档起代码骨架，很容易再次回到：

- 先做功能
- 后补设计
- 最后结构失控

### 5.2 shared terminal 复杂度再次失控

如果不先把 `ConnectionState` 做成一等模型，owner/follower 会再次散回 UI 和 runtime 逻辑。

### 5.3 渲染问题过早主导实现

如果过早恢复旧版那种复杂 render/cache 路线，会把新主线重新拖回旧结构。

---

## 6. 当前推荐动作

当前最合适的下一步是：

1. 你先确认文档主线
2. 然后从 `layout / connection / workspace / intent / reducer` 开始写第一批代码

---

## 7. 当前一句话状态

termx TUI 现在已经进入“文档已重置，第一批骨架代码已起，继续按 TDD 扩 reducer 和 UI 状态机”的阶段。
