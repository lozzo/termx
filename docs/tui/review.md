# TUI 设计评审

评审对象：`docs/tui/README.md` 及其引用的设计文档

评审结论：**方向正确，核心模型有明显差异化，值得继续推进；但当前文档还不能直接作为实现蓝图，建议先完成一轮收口后再进入大规模实现。**

## 总评

这套设计最强的部分，不是快捷键或线稿，而是底层心智模型：

- `Terminal` 是一等公民，`Viewport` 只是观察它的镜头
- `fit / fixed` 模式把"控制 PTY 尺寸"和"观察 PTY 内容"拆开了
- `Workspace / Tab / Layout` 都是纯客户端视图，不再绑定 terminal 生命周期

如果这个模型成立，termx 和 tmux/zellij 就不是同一类产品了。这个判断在 `docs/tui/positioning.md`、`docs/tui/model.md`、`docs/tui/layout.md` 三份文档里是一致的，这是当前设计最好的地方。

但文档也存在几类明显问题：

- 个别关键交互存在**规格冲突**
- 多客户端 / 多写入者场景只讲了价值，**没定义仲裁规则**
- 布局恢复和 terminal 匹配存在**不确定性**
- 渲染章节对 Bubble Tea 的假设较激进，**需要先验证再承诺**

所以我的判断不是"推翻重做"，而是：**保留模型，补齐规则，收紧 MVP。**

## 做得好的地方

### 1. 产品定位清楚

`docs/tui/positioning.md` 把 termx 和 tmux/zellij 的差异讲清楚了，尤其是这几条：

- terminal 独立存活，客户端只是视图
- 关闭 viewport 不等于 kill terminal
- 一个 terminal 可以同时出现在多个视图中

这几条不是文案层面的差异，而是产品边界的重新定义。

### 2. `fit / fixed` 抽象很强

`docs/tui/model.md` 里对 `fit` 和 `fixed` 的区分是整套方案的关键：

- `fit` 负责日常主操作面
- `fixed` 负责旁观、复用、AI 观察、非主控客户端

这套设计同时解决了：

- 多客户端观察同一个 terminal
- 小窗观察大 terminal
- 人类观察 AI agent 但不抢 resize 权

这是 termx 最值得做出来的能力之一。

### 3. Terminal Picker 是核心亮点

`docs/tui/interaction.md` 中的 `C-a f` 不是一个普通的 "find pane"，而是把 terminal 复用做成了主入口：

- attach 到当前 viewport
- 在新 viewport 中打开
- 搜索 terminal command / tags / workspace / tab

这一点非常有产品差异化，建议继续强化。

### 4. 线稿覆盖面足够

`docs/tui/wireframes.md` 不只画了正常态，还覆盖了：

- copy/scroll
- command mode
- exited
- prefix wait
- fixed + pinned
- 最小尺寸折叠

这能显著减少实现时的歧义，说明方案作者已经在想状态空间，而不是只画 happy path。

## 必须修改的问题

### 1. AI 协作场景的导航描述前后冲突

`docs/tui/ai-scenarios.md` 中写的是：

- `C-a l` 切到 agent
- `C-a h` 切回 shell

但 `docs/tui/interaction.md` 的 AI 协作流程里写的是：

- `C-a h` 切到 agent
- `C-a l` 切回 shell

这是硬冲突，必须统一。建议以空间方向为准：左边 `h`，右边 `l`，不要在示例里反着写。

### 2. 人和 AI 共同写入同一个 Terminal，但没有定义写入仲裁

`docs/tui/ai-scenarios.md` 把“人机共写同一 terminal”当成重要卖点，这是成立的；但目前只描述了能力，没有定义规则。

至少要补以下问题：

- 谁是当前 writer，是否存在主控方概念
- 两边同时输入时如何表现
- `Ctrl-C` 是否总是无条件发给 PTY
- 是否需要输入来源标识（human / agent）
- 是否提供只读观察模式，避免误触

如果这些不定义，AI 场景越重要，风险越高。

### 3. layout 的 tag 匹配规则不够确定

`docs/tui/layout.md` 现在的规则是：

- 匹配多个时，取第一个未被同一布局使用的 terminal
- 如果都已被使用，创建新的

问题是“第一个”没有稳定定义。它可能随 server 返回顺序变化，导致：

- 同一份 layout 在不同时间 attach 到不同 terminal
- 保存/恢复结果不稳定
- 用户难以理解为什么这次连到 T3、下次连到 T8

建议补稳定规则，例如：

- 先按精确 tag 数量匹配度排序
- 再按最近 attach 时间 / 创建时间排序
- 再按 terminal ID 排序兜底

或者干脆把“多匹配”定义成需要用户确认的情况。

### 4. exited terminal 的回收语义不清楚

`docs/tui/model.md` 一方面强调一个 terminal 可以被多个 viewport 同时观察，另一方面又写“关闭 viewport 时清理 exited terminal”。

这里需要明确：

- exited terminal 是否仍然保留在 server 的 terminal pool 中
- 如果有多个 viewport 观察它，关闭其中一个是否能清理
- orphan + exited 的 terminal 什么时候 GC
- restart 是“原 terminal 复活”还是“创建新 terminal 并重新绑定”

建议把 terminal lifecycle 单独画成状态机，不要只放一段文字。

### 5. fixed 模式的交互没有闭环

`docs/tui/model.md` 说：

- `C-a P` 可以 pin
- pinned 后可手动平移 viewport

但 `docs/tui/interaction.md` 没定义平移按键，也没定义：

- 浮动层里多个 fixed viewport 如何仅用键盘切换焦点
- 如何提升当前浮窗到最上层
- pinned 状态下边界超出时如何处理 offset

如果 fixed 模式是核心能力，这些不能只停在模型层，要落到可执行交互。

### 6. 渲染方案对 Bubble Tea 的假设需要先做 spike

`docs/tui/rendering.md` 假设：

- output 到来时只标记 dirty，不立即触发完整渲染
- render tick 控制 View 调用频率
- 后续还能做更细粒度的 ANSI 输出优化

方向没问题，但这里有两层风险：

- Bubble Tea 的渲染触发机制未必完全按文档设想工作
- 即便 viewport 级 dirty 可行，最终瓶颈也可能仍在全屏字符串生成

建议把这部分从“设计已定”改成“待验证方案”，并把验证目标写清楚：

- 60fps 下高频输出时 CPU 占用
- 4~8 个 viewport 同时刷屏时的表现
- Bubble Tea 是否允许当前节奏的批处理模型

## 应该修改的问题

### 1. 默认行为还不够“防误杀”

当前方案里可见的 kill 操作很多：

- `C-a X`
- Picker 中 `C-k`
- `:kill-terminal`

虽然文档已经区分了 close viewport 和 kill terminal，这是对的；但考虑到 termx 的 terminal 是持久资源，误 kill 的代价比 tmux 大。

建议至少定义一种保护：

- 对运行中的 terminal 二次确认
- 或只对带 `role=ai-agent` / `long-running=true` 等 tag 的 terminal 确认
- 或支持 undo / recent kills 列表

### 2. workspace 恢复优先级需要更明确

`docs/tui/layout.md` 里启动顺序是：

1. 有 `--layout` 就加载 layout
2. 否则恢复上次 workspace 状态
3. 再否则创建默认 workspace

但还缺两个边界问题：

- 指定 `--layout` 时，是否完全忽略上次状态
- 多个 `--layout` 和已有工作区状态冲突时如何合并

建议加一个优先级表，避免实现者自由发挥。

### 3. save-layout 的输出不一定可稳定回放

目前 `save-layout` 记录的是：

- terminal tags
- command
- mode
- 分割信息

问题在于：如果 tag 很宽泛，保存出来的 layout 再加载时未必能匹配回原 terminal。

建议在文档里明确：

- `save-layout` 更偏向“生成模板”
- 不是“精确快照”

如果要支持精确恢复，应该另有“workspace state”文件，语义和 layout 区分开。

## MVP 范围建议

建议把第一阶段收紧到“证明核心模型成立”，而不是一次把所有 UI 状态都做满。

### 第一阶段建议保留

- 单客户端 TUI
- Workspace / Tab / split
- Terminal Picker
- close viewport vs kill terminal
- fit / fixed
- exited 状态和 restart
- 基本的 layout load/save

### 第一阶段建议下放

- 多浮窗复杂 z-order 操作
- 鼠标拖拽全套交互
- 自动网格的动态重排
- 行级 dirty / writer 绕过 Bubble Tea
- 人机共写同 terminal 的复杂仲裁

原因不是这些不重要，而是核心问题应先验证：

- terminal 能否真正独立于视图存在
- 一个 terminal 能否稳定被多个 viewport 观察
- fixed 是否真的解决 resize 冲突
- picker 是否足够自然地支撑 terminal 复用

如果这四点成立，后面的复杂度才值得加。

## 建议作者下一轮补的内容

建议下一版文档至少新增或补全以下内容：

1. 一张完整的 terminal 生命周期状态机
2. 一张多客户端 / 多 writer 的仲裁规则表
3. 一张 startup / restore / layout load 的优先级表
4. 一张 layout 匹配的稳定排序规则
5. fixed 模式的完整键盘交互说明
6. 渲染方案的 spike 验证结论和边界条件

## 最终结论

这套设计最大的价值，不在于“做了一个更现代的 tmux UI”，而在于它真的试图把 **terminal、layout、client、lifecycle** 四件事拆开。

这个方向我认可，而且我认为值得继续投入。

但要注意，当前文档的弱点也集中在这个“拆开以后”的地方：一旦生命周期、匹配、仲裁规则没定死，系统虽然概念上先进，落地时却很容易变成行为不稳定、边界含糊的产品。

所以结论是：

- **模型保留**
- **规则补齐**
- **MVP 收紧**
- **渲染先验证再承诺**

这样改完，这套方案就会从“很有想法的设计稿”变成“可以推进实现的设计稿”。
