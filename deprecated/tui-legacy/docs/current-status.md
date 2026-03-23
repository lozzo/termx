# termx TUI 当前状态

状态：2026-03-22

这份文档只讲“现在代码做到什么程度了”。

当前额外结论：

- 界面主框架已经可用
- 但内部实现已经积累明显架构债
- 后续必须按 staged refactor 继续推进，不能再只靠补丁开发
- staged refactor 已经真正开始，不再只是文档计划

---

## 0. staged refactor 当前落点

截至 2026-03-22，当前代码里的真实重构落点是：

- `R0 文档冻结`：已完成
- `R1 连接状态抽离`：已完成第一阶段
- `R2 输入统一`：进行中，已经先把 key/event 的主要 mode action 对齐，并把 prefix 状态切换收口到统一 transition 入口

当前已经落到代码里的重构支点：

- [`tui/connection_state.go`](/home/lozzow/workdir/termx/tui/connection_state.go)：shared terminal 连接快照与 owner/follower 规则
- [`tui/connection_state_test.go`](/home/lozzow/workdir/termx/tui/connection_state_test.go)：owner 迁移与归一化测试
- [`tui/model.go`](/home/lozzow/workdir/termx/tui/model.go)：prefix state transition 统一入口
- [`tui/prefix_input.go`](/home/lozzow/workdir/termx/tui/prefix_input.go)：prefix key/event 的统一前缀输入归一化
- [`tui/model_test.go`](/home/lozzow/workdir/termx/tui/model_test.go)：prefix transition 与 key/event parity 测试

当前在输入重构上又新增一层：

- `dispatchPrefixInput`
- `prefixIntent`
- `prefixRuntimePlan`
- `floatingModeRuntimePlan` 已开始承接 direct-mode keep 这类原本散落在 action apply 后的策略

当前 runtime plan 化已经开始复制到非 prefix-result 路径：

- `globalModeRuntimePlan`
- `workspaceModeRuntimePlan`
- `tabModeRuntimePlan`
- `floatingModeRuntimePlan`
- `viewportModeRuntimePlan`
- `resizeModeRuntimePlan`
- `offsetPanModeRuntimePlan`

也就是说，当前已经不是只有“共享 token/action”，而是开始进入“共享 dispatch 入口”阶段。

当前又多了一层真正的公共 runtime 行为抽取：

- `viewportNavigationRuntimePlan`

它把 `viewport mode` 和 `offset-pan mode` 共享的偏移导航逻辑抽成了一层公共 apply，而不再只是两个 mode 各自重复写一份。

当前这层已经覆盖到的 prefix mode：

- root
- pane
- tab
- workspace
- global
- resize
- offset-pan
- viewport
- floating

当前统一输入层还额外覆盖：

- `Alt+h/j/k/l`
- `Alt+H/J/K/L`

这些事件型组合输入已经不再只靠 `input.go` 中的硬编码分支解释，而是先归一化成统一的 prefix input，再映射成浮窗动作。

当前这一轮又额外消掉了一类“伪重构”分支：

- `tab mode` / `workspace mode` 的 unknown-action keep 行为已经由单测明确锁定
- 两处原本看似在处理 direct-mode、实际上不改变结果的冗余 override 已移除
- `floating mode` 的 unknown-action direct keep 已改为在 runtime plan 生成时表达，而不是在 apply 之后临时补丁

---

## 1. 当前已经稳定存在的能力

### 启动与基础工作流

- `termx` 直接进入可工作的 workspace
- 默认会有一个可输入 shell pane
- tab / split / floating 基本链路都已经存在
- tiled 与 floating 可以共存

### terminal 复用与管理

- terminal picker 可 attach 已有 terminal
- terminal manager 已经存在，而且是全屏页，不是左抽屉
- terminal manager 支持：
  - bring here
  - open in new tab
  - open in floating pane
  - edit metadata
  - stop terminal
- terminal manager 已按 `NEW / VISIBLE / PARKED / EXITED` 分组

### 当前 UI 信息分配

- pane 标题默认显示 terminal 真名
- pane 标题栏右侧已承担主要关系状态：
  - `live / saved / exit`
  - `owner / follower`
  - `fit / fixed`
  - `share / obs / ro / pin / lock`
- 底栏左侧已使用连续 segment 风格快捷键带
- 普通态底栏左侧只展示 `Ctrl+` 模式入口，不再夹带 exited/unbound 的直达动作
- 底栏右侧已压缩为当前焦点的极简摘要，不再堆叠 display/share/lock 等关系信息
- floating pane 已支持移出主视口后通过快捷键居中呼回
- help / prompt / picker 已统一走居中 overlay 路径，而不是整屏说明页
- help 内容已按 `Most used / Concepts / Shared terminal / Floating / Exit` 分组

### 当前快捷键行为

- 用户态主入口是 `Ctrl-p / Ctrl-r / Ctrl-t / Ctrl-w / Ctrl-o / Ctrl-v / Ctrl-f / Ctrl-g`
- 用户态 `Ctrl-a` 已移除，按下时直接透传给当前 terminal
- mode hold 默认 3 秒，可通过 `--prefix-timeout` 调整
- `resize / floating move / floating resize / pane focus / viewport pan` 等连续操作会在每次有效动作后续期 3 秒
- exited pane 的 `r / attach / close` 提示已回到 pane 内文案，不再占用普通态底栏

### metadata

- terminal name / tags 可编辑
- picker 和 terminal manager 都能进入 metadata 编辑
- 编辑 prompt 已有两步流：name -> tags
- prompt 会显示 step、terminal id、command
- metadata 更新会同步到所有 attach pane
- parked terminal 的 metadata 也可以编辑

### 生命周期语义

- close pane 不会默认 kill terminal
- stop terminal 会先弹确认
- terminal 被 stop/remove 后，原 pane 会保留成 `saved pane`
- remote remove 会给其他客户端提示
- exited terminal 可以进入 exited 状态并保留历史

### restore / layout

- workspace state restore 已有基础
- layout restore / create / prompt / skip 已有基础路径
- 重复 `_hint_id` 的共享 attach / create 已有覆盖

### 可测试性

- 当前已有大量单测
- 已有屏幕级 e2e harness
- 最近新增能力基本都按 TDD 落地
- shared terminal 的 `floating + owner/follower + fit/fixed + acquire + alt-screen` 组合场景已有专门回归测试
- 当前 `go test ./... -count=1` 通过

---

## 2. 当前已经基本定下来的产品结论

### 结论 A：主概念已经收口

现在主概念就是：

- workspace
- tab
- pane
- terminal

`view / viewport / panel` 不再应作为用户主概念继续扩散。

### 结论 B：pane 和 terminal 明确解绑

现在正确语义已经比较明确：

- pane 是工作位
- terminal 是运行实体
- pane 标题默认应展示 terminal 真实名称，而不是把 pane 当独立命名对象
- 关闭 pane 不结束 terminal
- stop terminal 不自动删 pane，而是留下 saved pane

### 结论 C：terminal manager 已经成为独立角色

现在 terminal manager 不再只是 picker 的大版本。

它已经在承担：

- terminal pool 浏览
- terminal 可见性查看
- metadata 编辑
- terminal stop
- 复用入口

### 结论 D：当前 UI 结构大体成立

现在已经有：

- 顶栏：workspace + tabs + 摘要
- pane 标题栏：名称 + 状态关系
- 底栏：左快捷键 / 右状态摘要
- overlay：居中 modal / picker / manager

这说明主框架已经成型。

但同时也要明确：

- “不重来”不等于“继续堆补丁”
- 当前实现已经需要做显式架构重构
- 后续 UI 打磨必须建立在更稳定的状态分层之上

---

## 3. 当前仍然不够好的地方

### 快捷键认知负担仍偏大

虽然已经切到 `Ctrl-p / Ctrl-r / Ctrl-t / Ctrl-w / Ctrl-o / Ctrl-v / Ctrl-f / Ctrl-g` 结构，但：

- mode 仍偏多
- 部分动作仍需要记忆
- help 虽然能看，但还不够“上手即懂”

### UI 视觉还没有完全收口

已有基础，但还没达到最终状态：

- pane 顶部 chrome 还需要进一步收口成单线表达
- modal / picker / manager 还可以更统一
- 顶栏、pane 标题栏、底栏之间的信息分配还能继续优化

### resize / shared terminal 的最终心智还需要继续收口

当前方向已经对了，但还要继续保证：

- shared terminal 的 resize 规则足够稳定
- acquire / auto-acquire / size-lock 的行为足够好理解
- `owner / follower` 和 `fit / fixed` 的概念不再混淆
- 复杂共享场景的 e2e 继续补强
- floating pane 脱离 tab 主视口后的“呼回并居中”能力要补成正式交互

### 内部架构已经需要收口

当前主要问题：

- `tui/model.go` 过大
- 输入状态机分散在多个文件
- pane 混合领域状态、运行时状态、渲染状态
- render cache 失效分散在业务逻辑中

这意味着：

- 当前功能虽然可用
- 但后续要继续稳定 shared terminal、浮窗、terminal manager、渲染性能，必须先进 staged refactor

### terminal 更完整的管理模型还没完全成型

已经有 metadata，但还缺：

- 更完整的 terminal 属性编辑策略
- 更清楚的 tags / rules 在 workspace/layout 中的使用方式
- 更完整的 terminal-only 管理视图设计

---

## 4. 当前进度判断

如果把 TUI 拆成 4 层，目前大致是：

### 第一层：基础可用性

完成度：高

- 可以启动、分屏、切 tab、开浮窗、复用 terminal、保存基本现场

### 第二层：正确语义

完成度：中高

- pane vs terminal 的关系已经大体收口
- stop / close / saved pane / remote remove 语义已经成型

### 第三层：高级共享与恢复

完成度：中

- 已有可用基线
- 还需要继续做复杂场景和边界一致性

### 第四层：最终交互体验与视觉完成度

完成度：中低

- 主框架有了
- 但还需要继续做 UI、美化、降噪、术语统一、帮助系统优化

---

## 5. 当前最适合继续推进的方向

按优先级建议：

1. 继续完成 `R2`，把输入从共享 helper 推到显式 intent dispatch
2. 开始 `R3`，分离 reducer 与 runtime effects
3. 继续补 shared terminal / floating / resize / alt-screen 的复杂 e2e
4. 再做一轮 keymap / help / 新用户引导减负
5. 最后再做 UI 视觉统一和更完整 manager 能力

---

## 6. 一句话结论

当前 termx TUI 已经不是“概念混乱的试验品”了。

它已经进入下面这个阶段：

- 主结构成立
- 主语义基本成立
- 主路径可用
- 现在最需要的是继续完成 staged refactor 主线，再在更稳的结构上做交互减负、视觉统一和复杂场景补测
