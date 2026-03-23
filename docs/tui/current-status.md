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
12. 第八轮 TDD 已补上 `metadata prompt submit/cancel`
13. 第九轮 TDD 已补上 `terminal manager` 的分组 / details / create row
14. 第十轮 TDD 已补上 `terminal manager details` 的位置列表投影
15. 第十一轮 TDD 已补上 `prompt draft` 输入模型
16. 第十二轮 TDD 已补上 `terminal manager details` 的 `visibility / owner / tags` 投影
17. 第十三轮 TDD 已补上 `create terminal` 的默认参数策略
18. 第十四轮 TDD 已补上 `prompt` 的结构化字段模型
19. 第十五轮 TDD 已补上 `prompt` 的反向字段切换和深拷贝语义
20. 第十六轮 TDD 已补上最小 bubbletea 输入映射层
21. 第十七轮 TDD 已补上最小 bubbletea shell 容器
22. 第十八轮 TDD 已补上最小 runtime effect executor 回流链路
23. 第十九轮 TDD 已补上 terminal manager 动作键映射
24. 第二十轮 TDD 已补上 runtime feedback 错误与 notice 通道
25. 第二十一轮 TDD 已补上 terminal picker 主线接线

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
- `tui/domain/terminalpicker`
- `tui/app/intent`
- `tui/app/reducer`
- `tui/bt`
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
- `TerminalManagerCreateTerminalIntent`
- `OpenTerminalPickerIntent`
- `TerminalPickerMoveIntent`
- `TerminalPickerAppendQueryIntent`
- `TerminalPickerBackspaceIntent`
- `TerminalPickerSubmitIntent`
- `SubmitPromptIntent`
- `CancelPromptIntent`
- `PromptAppendInputIntent`
- `PromptBackspaceIntent`
- `PromptNextFieldIntent`
- `PromptPreviousFieldIntent`
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
- `SubmitPromptIntent` 已支持 `edit terminal metadata`
- metadata prompt 已能更新 terminal `name / tags`
- metadata prompt 提交后会关闭 prompt、恢复原 pane 焦点，并产出 `UpdateTerminalMetadataEffect`
- 已补上一条 reducer 场景型 E2E：terminal manager edit metadata -> prompt -> submit
- `terminal manager` 已支持稳定的分组投影：`NEW / VISIBLE / PARKED / EXITED`
- `terminal manager` 已支持顶部 `+ new terminal` 入口
- `terminal manager` 已支持当前选中 terminal 的 details 投影
- `terminal manager` 的选择模型现在覆盖 create row 与 terminal 行，header 行保持非可选
- 已补上一条 reducer 场景型 E2E：terminal manager create row -> create terminal effect
- `terminal manager details` 已支持列出 terminal 当前出现的位置
- 位置列表已能区分 `pane:<id>` 和 `float:<id>`
- details 的连接计数现在和位置投影保持一致
- 已补上一条 reducer 测试覆盖 details 中的 pane / float 位置投影
- `prompt` 已持有独立 `draft`，不再只依赖 `SubmitPromptIntent{Value}`
- `PromptAppendInputIntent` 和 `PromptBackspaceIntent` 已能直接驱动 prompt draft
- 打开 metadata prompt 时会自动用当前 terminal 的 `name/tags` 预填 draft
- `SubmitPromptIntent` 在未显式传值时会直接提交当前 draft
- 已补上一条 reducer 场景型 E2E：workspace create flow 直接走 prompt draft 提交
- `terminal manager details` 已支持 `visibility label`
- `terminal manager details` 已支持 `owner slot label`
- `terminal manager details` 已支持稳定排序后的 `tags` 投影
- 已补上一条 reducer 测试覆盖 details 中的 `visibility / owner / tags`
- `terminal manager create row` 产出的 `CreateTerminalEffect` 已带默认 command
- `CreateTerminalEffect` 已带稳定默认 name：`workspace-tab-pane`
- 已补上一条 reducer 场景型 E2E：create row -> create effect 时默认参数完整
- `prompt` 已支持结构化字段模型
- metadata prompt 已拆成 `name / tags` 两个字段
- `PromptNextFieldIntent` 已能在结构化字段间切换焦点
- `PromptPreviousFieldIntent` 已能在结构化字段间反向切换焦点
- prompt 输入现在优先写入当前字段，`SubmitPromptIntent` 可直接从字段模型生成提交值
- 已补上一条 reducer 测试覆盖 metadata prompt 的字段切换与结构化提交
- `prompt overlay` clone 已改为深拷贝字段切片，避免 reducer 纯状态克隆时共享底层字段数据
- 已补上一条 reducer 测试覆盖 metadata prompt 的反向字段切换
- 已补上一条 prompt 单元测试覆盖结构化字段深拷贝
- 已新增 `tui/bt` 输入映射层，负责把 `bubbletea.KeyMsg` 翻译成显式 intent
- 根层已支持最小主入口映射：`Ctrl-w -> workspace picker`，`Ctrl-g` 进入 global 前缀，随后 `t -> terminal manager`
- `workspace picker` 已接上键盘映射：移动、展开、折叠、提交、关闭、query 输入
- `terminal manager` 已接上最小键盘映射：移动、query 输入、connect here、关闭
- `prompt` 已接上键盘映射：输入、回退、提交、取消、`Tab/Shift-Tab` 字段切换
- 已补上一条跨层场景型 E2E：`KeyMsg -> intent mapper -> reducer -> workspace picker jump`
- `tui/bt` 已补上最小 `tea.Model` 容器，串起 `KeyMsg -> mapper -> reducer -> effect handler`
- shell 容器当前已抽出 `EffectHandler` / `Renderer` 接口，后续可继续接 runtime executor 和真实 render
- `Model` 已支持对非键盘消息保持稳定忽略，避免输入层误改状态
- 已补上一条跨层场景型 E2E：`KeyMsg -> Model.Update -> workspace picker jump`
- `tui/bt` 已补上 `RuntimeExecutor` 和 `RuntimeEffectHandler`
- `OpenPromptEffect` 现在已能回流成 `OpenPromptIntent` 并重新进入 reducer
- `ConnectTerminal / CreateTerminal / StopTerminal / UpdateTerminalMetadata / new tab / floating` effect 已有稳定的 runtime service 接口落点
- `Model.Update` 已支持消费 effect feedback message，形成 `key -> effect -> feedback intent -> reducer` 闭环
- 已补上一条跨层场景型 E2E：`workspace picker create row -> OpenPromptEffect -> prompt overlay`
- `terminal manager` 已补上动作键映射：`t new tab`、`o floating`、`e edit`、`k stop`
- `terminal manager` 的 `edit metadata` 现在已能从 shell 容器中走完整 prompt handoff
- 已补上一条跨层场景型 E2E：`Ctrl-g -> t -> e -> metadata prompt`
- `tui/bt` 已补上最小 `Notice` 模型，当前由 shell 容器持有和追加
- runtime effect 执行失败不再静默吞掉，`RuntimeEffectHandler` 现在会把错误转换成 `error notice`
- `Model.Update` 已支持消费 notice feedback message，并保留当前 notice 列表供后续 renderer 接线
- 已补上一条跨层场景型 E2E：`terminal manager stop` 失败后记录 error notice
- 已新增 `tui/domain/terminalpicker`，提供最小 terminal picker 列表态、搜索、选择和 create row
- 根层已支持 `Ctrl-f -> terminal picker`
- `terminal picker` 已接上键盘映射：移动、query 输入、回退、提交、关闭
- `terminal picker` 已支持 `connect existing terminal`
- `terminal picker` 已支持 `+ new terminal` 入口并复用统一默认参数策略
- 已补上一条跨层场景型 E2E：`Ctrl-f -> query -> connect selected terminal`

本轮验证：

- `go test ./tui/domain/terminalpicker ./tui/app/reducer ./tui/bt -count=1`
- `go test ./tui/bt -run TestE2E -count=1`
- `go test ./tui/bt -count=1`
- `go test ./tui/bt -run TestE2EModelScenario -count=1`
- `go test ./tui/bt -run TestE2EIntentMapperScenario -count=1`
- `go test ./tui/domain/prompt ./tui/app/reducer -count=1`
- `go test ./tui/app/reducer -run TestE2EReducerScenario -count=1`
- `go test ./tui/domain/terminalmanager ./tui/app/reducer -count=1`
- `go test ./tui/domain/workspace ./tui/app/reducer -count=1`
- `go test ./tui/... -count=1`
- `go test ./... -count=1`

---

## 3. 尚未开始

当前还没有正式开始的部分：

1. 新版 renderer
2. restore 流程
3. 新主线真实 TUI E2E 回迁
4. notice timeout / 清理策略

---

## 4. 当前最高优先级

下一阶段最高优先级不是补 UI，而是先把下面几个边界立住：

1. 更完整的 `intent -> reducer -> effect -> runtime feedback` 契约
2. restore 流程
3. 真实 TUI E2E 场景壳
4. 新版 renderer 最小骨架

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

1. 给 notice 补 timeout / 清理策略
2. 把 restore 流程接进当前 shell 主线
3. 继续扩真实 TUI E2E 场景壳

---

## 7. 当前一句话状态

termx TUI 现在已经进入“picker / manager / prompt 三条 overlay 主线都已接入当前 shell 容器，runtime feedback 的错误与 notice 通道也已接回，下一步继续按 TDD 补 notice 生命周期、接 restore 并扩真实 TUI E2E 壳”的阶段。
