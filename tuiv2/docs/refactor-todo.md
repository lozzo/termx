# TUIv2 Refactor TODO

## 目标

这轮重构按“一把过”的思路推进，但不追求一次提交做完所有代码。
目标是先把 `tuiv2` 的边界重新立住，再做模块拆分和样式/交互收口，避免继续在已有耦合上补丁叠补丁。

最终验收口径：

- `app` 不再直接散落双写 `workbench` 和 `runtime` 的同一业务事务。
- `Visible*` / render projection 路径保持纯读，不做隐式 mutation。
- `render` 不再直接依赖输入绑定文档拼 modal/footer 文案。
- modal 内不展示快捷键字符串；快捷键只出现在 status/help/外部文档。
- `coordinator` 从业务编排和超长函数签名里拆出来，至少完成第一轮职责分离。
- 当前已知 flaky 用例 `TestE2ETabSwitchSharedTerminalPromotesOwnerResizesAndShowsCursor` 被稳定修复。

## 执行规则

- 严格按任务列表顺序推进，除非上游任务明确证明不再是前置依赖。
- 每次只做一个主任务，任务完成后先补测试，再做 review，再进入下一个任务。
- 每完成一项代码任务，必须拉一个 subagent 做 code review。
- review 默认用 `explorer` 子代理，问题要具体到改动文件和风险点，不做泛泛“帮我看看”。
- review 没过之前，不开始下一个主任务。

建议每个任务固定走这个流程：

1. 实现任务最小闭环。
2. 跑本任务相关测试。
3. 拉一个 `explorer` subagent 做 code review，聚焦本次 touched files。
4. 修复 review findings。
5. 重新跑测试并提交。

建议 review prompt 模板：

```text
Review the refactor in these files only:
- <file-1>
- <file-2>

Focus on:
- behavior regressions
- ownership / layering violations
- missing tests
- hidden state sync risks

Do not summarize first. List findings by severity with file references.
```

## Phase 0: Baseline And Guardrails

- [ ] 0.1 建立重构基线文档
  - 记录当前 tag：`v0.0.1.beta`
  - 记录已知 flaky：`tuiv2/app` 里的 shared terminal owner/resize 用例
  - 记录当前禁止继续扩散的边界：`app` 双写、`Visible*` mutation、`render` 反查 binding catalog
  - 完成标准：基线、风险、边界写入文档并在后续 PR/commit 中遵守

- [ ] 0.2 建立任务推进纪律
  - 每个任务都要列出 touched files、测试命令、subagent review 结果
  - 不把截图、实验目录、临时 harness 混进功能提交
  - 完成标准：后续每个任务提交都能对照这三项说明

## Phase 1: 收口 Pane / Terminal 事务边界

- [ ] 1.1 盘点当前 `app` 里直接双写 `workbench` / `runtime` 的路径
  - 重点看 terminal attach、bind、detach、owner handoff、restart、resize、session sync
  - 输出一个清单，标出每条路径的当前入口和目标归属
  - 完成标准：列出必须收口到 orchestrator/service 的调用面

- [ ] 1.2 引入统一的 pane-terminal transaction service
  - 首批覆盖：attach / bind existing / detach / close / restart / owner promote
  - service 内统一更新 `workbench`、`runtime`、pending state 和后续 effects
  - `app` 只发请求，不再自己手工拼双写事务
  - 完成标准：`app` 相关调用点改为单入口，重复状态同步逻辑消失

- [ ] 1.3 把 resize 协调并入事务边界
  - owner 切换后谁负责 resize、何时 resize、失败如何回退，要在 service 内说清
  - 修复 shared terminal owner handoff 后尺寸不更新的问题
  - 完成标准：`TestE2ETabSwitchSharedTerminalPromotesOwnerResizesAndShowsCursor` 稳定通过

- [ ] 1.4 清理 `app` 内部遗留的事务性 side effects
  - 清掉 attach/bind/restart 这类流程里重复的 `render.Invalidate()`、`saveStateCmd()`、pending map 操作
  - 保留必要的 UI reaction，但不再承担领域事务协调
  - 完成标准：`update_runtime.go` / `session_sync.go` / attach 相关文件职责明显收窄

## Phase 2: 把 Visible Projection 变成纯读

- [ ] 2.1 清理 `Visible*` 路径中的隐式 mutation
  - 禁止 `VisibleWithSize()` / `Visible()` / render adapter 在读取时调用 normalize 并修改状态
  - 首先迁出 `normalizeFloatingState()` 这类读时修状态的逻辑
  - 完成标准：projection 层只读，不改业务状态

- [ ] 2.2 把 normalize 下沉到 mutate path
  - 在创建、更新、恢复、导入 floating state 时做一次性规范化
  - 对非法状态要么修正后 `touch()`，要么明确 reject，不允许静默拖到 render 才修
  - 完成标准：state 规范化发生在写路径，读路径不补救

- [ ] 2.3 补 projection invariants 测试
  - 针对 `VisibleWithSize`、floating visibility、zoom projection、workspace projection 增加纯读断言
  - 测试方法先定死：
  - 对 `Workbench` / `Runtime` 记录 `version`，调用 projection 前后版本不能变化
  - 对关键结构做快照比对：workspace/tab/pane/floating 的导出字段前后一致
  - 必要时补只读测试 helper，避免靠人工肉眼检查
  - 完成标准：调用 projection 前后版本不变，关键 state 快照一致

## Phase 3: 先拆掉 render 对 input 文案的反向依赖

- [ ] 3.0 引入统一的 status / overlay view-model builder
  - 先定义 render 真正需要的输入：
  - status hint tokens
  - modal / overlay footer action labels
  - 当前 mode 的语义说明
  - 这层可以放在 `app` 邻近层，也可以是独立 view-model builder，但不能继续埋在 `render`
  - 完成标准：`render` 拿到的是结构化 token/model，而不是自己回头查 binding catalog

- [ ] 3.1 移除 overlay footer 对 `input.DefaultBindingCatalog()` 的依赖
  - overlay footer 只接受语义动作 label，例如 `open` / `rename` / `close`
  - 快捷键展示回到 status bar / help
  - 完成标准：`render/overlays.go` 不再反查 binding doc 拼 `[Ctrl-X]`

- [ ] 3.2 移除 status bar 对 binding catalog 的直接扫描
  - 由 `app` 或专门的 view-model builder 生成当前模式下的 status hint model
  - `render/frame.go` 只消费已整理好的 token
  - 完成标准：`render` 不再直接依赖 `input.DefaultBindingCatalog()`

- [ ] 3.3 补 modal/status/help 三处职责边界
  - modal 显示语义动作
  - status 显示当前模式快捷键
  - help 显示完整绑定文档
  - 完成标准：交互信息分布符合 `AGENTS.md`

## Phase 4: 拆 `render/coordinator.go`

- [ ] 4.1 先按职责切文件，不先追求大改算法
  - 推荐第一刀：
  - `coordinator_state.go`：cache key / invalidate / cursor blink
  - `body_projection.go`：pane entry / content key / frame key
  - `body_cache.go`：body canvas cache / sprite cache
  - `cursor_projection.go`：cursor target / synthetic cursor
  - `surface_terminal_pool.go`：terminal pool surface
  - `overlay_render.go`：overlay compose
  - 完成标准：
  - 上面这 6 组职责至少各自落到独立文件
  - `coordinator.go` 只保留顶层入口、公共 struct 和薄 orchestration
  - `coordinator.go` 目标压到 800 行以内；超出说明拆分还没完成

- [ ] 4.2 收缩超长参数签名
  - 像 `buildPaneRenderEntry(...)` 这类函数改成 context struct / options object
  - 完成标准：核心构造函数不再靠十几个位置参数维持正确性

- [ ] 4.3 明确 cache 失效边界
  - frame cache、line cache、body canvas cache、sprite cache 各自的 key 和生效前提写清
  - 避免“为修 artifact 继续局部打补丁”
  - 最少补这些断言：
  - state 不变时 frame/line cache 命中
  - term size 变化时 cache 失效
  - active pane / cursor blink / overlay 变化时只打掉该打掉的 cache
  - floating overlap 与 non-overlap 走不同 body canvas 路径
  - 完成标准：上述 miss/hit 条件有单测覆盖，且文档能解释每类 cache 的失效触发器

## Phase 5: 主题与视觉 token 收口

- [ ] 5.1 收口固定色 fallback 策略
  - 先审查 `uiThemeFromHostColors()` 中的固定 fallback 是否必须保留
  - 若保留，限定只作为 palette 缺失时的语义 accent fallback，并写清原因
  - 若可替换，优先改成基于 host FG/BG 推导的语义色
  - 完成标准：视觉规则与 `AGENTS.md` 一致，且测试覆盖 fallback 行为

- [ ] 5.2 清理 modal/surface 背景一致性
  - tree、preview、footer、empty preview 的底色模型统一
  - terminal preview 空白优先回宿主默认背景
  - 完成标准：overlay 内不再出现“左边一个底、右边一个底”的拼贴感

## Phase 6: App 层瘦身

- [ ] 6.1 继续拆 `app` 里的 god flow
  - 目标优先级：
  - `update_actions_modal.go`
  - `update_mouse.go`
  - `update_runtime.go`
  - `session_sync.go`
  - 完成标准：按领域或 surface 拆成更小文件，避免一个文件同时承担事件分发、事务、UI side state

- [ ] 6.2 明确 UI state / domain state / runtime state 边界
  - `UIState` 只管 mode / modal / surface state
  - domain state 由 `workbench` 承载
  - live terminal/runtime state 由 `runtime` 承载
  - 完成标准：`Model` 字段明显瘦身，兼容 alias 可以继续删

## Phase 7: 稳定性与收尾

- [ ] 7.1 修复并压实 flaky tests
  - shared terminal owner/resize
  - floating redraw / background artifact 相关回归
  - 任何本轮重构中暴露的时序问题
  - 完成标准：相关用例 `-count=10` 稳定通过

- [ ] 7.2 清理临时 TODO、回滚痕迹、兼容注释
  - 去掉空实现、临时注释、仅为过渡保留的 compatibility alias
  - 完成标准：核心路径不再留“之后再拆”的假门面

- [ ] 7.3 补最终文档
  - 更新架构说明
  - 更新交互/模式说明
  - 更新重构后的 review 规则和测试指引
  - 完成标准：新同学可以只靠文档理解 `tuiv2` 新分层

## 推荐推进顺序

为了尽量“一把过”，建议严格按这个主线执行：

1. Phase 1
2. Phase 2
3. Phase 3
4. Phase 4
5. Phase 6
6. Phase 5
7. Phase 7

原因：

- 先收口事务边界，不然后面的 render/app 拆分都会继续踩双写时序坑。
- 先做纯读 projection，再做 render 拆分，不然 cache/key/projection 会一起脏。
- 先拆 render 对 input 的反依赖，再拆 coordinator，能少搬一次代码。
- 样式收口放在后面，避免在结构还不稳定时反复重做 UI token。

## 每阶段提交模板

每完成一个主任务，提交说明至少要包含：

- 本任务目标
- touched files
- 新增/修复的测试
- subagent review 结论
- 剩余风险

建议 commit/PR 说明结构：

```text
Goal
- <what changed>

Files
- <file-1>
- <file-2>

Tests
- <command>

Subagent review
- explorer: <summary of findings / no findings>

Risks
- <remaining risk>
```
