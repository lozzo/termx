# termx TUI 当前状态

状态：TDD State Machine Stage
日期：2026-03-23

---

## 1. 当前判断

termx TUI 当前处于“文档主线已稳定，领域骨架和第一批 UI 状态机已按 TDD 落地”的阶段。

现状可以概括为：

- 旧版 TUI 已归档到 `deprecated/tui-legacy/`
- 新主线文档已经建立并持续作为实现约束
- 新主线代码已进入 reducer / state machine 落地期
- 当前仍未进入 bubbletea shell 和 renderer 恢复阶段

---

## 2. 已完成

当前已经完成的事情：

1. 旧版资产已归档
2. legacy 设计和代码已做第一轮整理
3. 新主线产品概念已重新收口
4. 新主线交互规则、线框、架构、测试策略、交付计划已成文
5. 第一批 TDD 代码骨架已经落地
6. 第二轮 TDD 已补上 `close pane` 语义和 workspace picker 树状态机
7. 第三轮 TDD 已补上 `overlay / mode / workspace picker navigation` 状态迁移
8. 第四轮 TDD 已补上 `terminal manager here / new tab / floating` effect 契约和管理状态骨架
9. 第五轮 TDD 已补上 `workspace picker query / backspace / create row prompt handoff`
10. 第六轮 TDD 已补上 `terminal manager search / edit / stop`
11. 第七轮 TDD 已补上 `prompt overlay` 和 `create workspace submit/cancel`

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
- `tui/domain/prompt`
- `tui/domain/terminalmanager`
- `tui/app/intent`
- `tui/app/reducer`
- `tui/runtime.go`
- `tui/client.go`

当前已经落到代码里的能力：

- `layout` 纯逻辑树和矩形投影
- `connection` 的 connect / owner / migrate 基线
- `workspace picker` 树构建与 query 命中祖先展开
- `workspace tree jump` 焦点决议
- `ConnectTerminalIntent`
- `StopTerminalIntent`
- `TerminalProgramExitedIntent`
- `ClosePaneIntent`
- `WorkspaceTreeJumpIntent`
- `OpenWorkspacePickerIntent`
- `CloseOverlayIntent`
- `WorkspacePickerMoveIntent`
- `WorkspacePickerAppendQueryIntent`
- `WorkspacePickerBackspaceIntent`
- `WorkspacePickerExpandIntent`
- `WorkspacePickerCollapseIntent`
- `WorkspacePickerSubmitIntent`
- `OpenTerminalManagerIntent`
- `OpenPromptIntent`
- `TerminalManagerMoveIntent`
- `TerminalManagerAppendQueryIntent`
- `TerminalManagerBackspaceIntent`
- `TerminalManagerConnectHereIntent`
- `TerminalManagerConnectInNewTabIntent`
- `TerminalManagerConnectInFloatingPaneIntent`
- `TerminalManagerEditMetadataIntent`
- `TerminalManagerStopIntent`
- `SubmitPromptIntent`
- `CancelPromptIntent`
- `ActivateModeIntent`
- `ModeTimedOutIntent`

本轮新增并通过测试的能力：

- `UIState` 已包含 `OverlayState` 和 `ModeState`
- `FocusState` 已补上 `OverlayTarget`
- `workspace picker` 已支持默认选中当前 active pane
- `workspace picker` 已支持选择移动、展开、折叠
- 搜索清空后会恢复“默认展开 + 手动展开/折叠”状态
- 打开 overlay 时会保存返回焦点
- 关闭 overlay 时会恢复原 pane 焦点
- picker 回车已能跳转 workspace / tab / pane，并在成功后关闭 overlay
- 非 sticky mode 超时后会自动清空
- `terminal manager` 已支持独立 overlay 状态对象和默认选中当前 pane 所连接 terminal
- `terminal manager` 已支持选择移动和稳定排序
- `terminal manager` 已支持 `connect here`
- `terminal manager` 已支持产出 `new tab / floating` 的 effect plan
- pane 改连新 terminal 时，旧 terminal 的 connection snapshot 会同步清理，避免 owner/follower 脏引用
- `workspace picker` 已支持 query 追加输入
- `workspace picker` 已支持 backspace 回退 query
- query 命中后会把选择移动到首个匹配节点，便于直接回车 jump
- 选中 `+ create workspace` 回车时，reducer 已能关闭 picker 并产出 `OpenPromptEffect{create_workspace}`
- 已补上一条 reducer 场景型 E2E：搜索后直接跳到目标 pane
- `terminal manager` 已支持 query 追加输入和 backspace
- `terminal manager` search 已支持匹配 terminal name / id / command / tags
- `terminal manager` 已支持对选中 terminal 发起 metadata prompt handoff
- `terminal manager` 已支持 stop 选中 terminal，并同步更新 reducer 内 terminal 状态
- 已补上一条 reducer 场景型 E2E：搜索后直接 stop 目标 terminal
- 已补上独立的 `prompt overlay` 状态对象
- `OpenPromptIntent` 已能把焦点切到 `prompt` layer
- `CancelPromptIntent` 已能关闭 prompt 并恢复原 pane 焦点
- `SubmitPromptIntent` 已支持 `create workspace`
- create workspace 会建立最小可工作骨架：默认 tab + 未连接 pane
- 已补上一条 reducer 场景型 E2E：workspace picker create row -> prompt -> create workspace

本轮验证：

- `go test ./tui/domain/prompt ./tui/app/reducer -count=1`
- `go test ./tui/domain/terminalmanager ./tui/app/reducer -count=1`
- `go test ./tui/domain/workspace ./tui/app/reducer -count=1`
- `go test ./tui/... -count=1`
- `go test ./... -count=1`

---

## 3. 尚未开始

当前还没有正式开始的部分：

1. `terminal manager` 的分组 / details / create-new-terminal 行为
2. `metadata prompt` 的 submit / cancel 流程
3. 新版 bubbletea shell
4. 新版 renderer
5. 新版 terminal picker / restore 流程
6. 新主线真实 TUI E2E 回迁

---

## 4. 当前最高优先级

下一阶段最高优先级不是补 UI，而是先把下面几个边界立住：

1. `metadata prompt` 的 submit / cancel
2. `terminal manager` 的 details / create-new-terminal / 分组
3. 更完整的 `intent -> reducer -> effect` 契约
4. 新版 bubbletea shell 接口
5. 真实 TUI E2E 场景壳

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

1. 补 `metadata prompt` 的 submit / cancel
2. 补 `terminal manager` 的 details / create-new-terminal / 分组
3. 为 reducer 补更多场景级测试
4. 再进入 bubbletea shell 最小接线

---

## 7. 当前一句话状态

termx TUI 现在已经进入“picker / manager / prompt 三条主状态机都已开始成形，继续按 TDD 扩 metadata prompt、details 视图和 runtime 契约”的阶段。
