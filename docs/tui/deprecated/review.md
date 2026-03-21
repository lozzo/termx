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

## 附录：`Obsidian Clarity` UI 提案评审

评审对象：用户提供的外部设计稿《termx TUI 产品设计规格书 (Obsidian Clarity)》

评审结论：**这是一份方向不错的视觉/氛围草案，但还不是一份可直接落地的 termx TUI 产品规格。适合吸收其视觉原则，不适合原样照搬成交互规范。**

### 这份提案里值得吸收的部分

- `色彩断层` 的思路是对的
  - 通过让背景 pane / 非焦点 pane 退居二线，突出当前工作层，能明显改善“多 pane + 多浮窗”下的视觉混乱
- `绝对遮挡` 是必须坚持的实现原则
  - modal / picker 必须先用实色背景覆盖底层，再绘制文字，不能出现底层字符透出来的情况
- `零 ID 显示策略` 值得保留
  - UI 应优先展示 human-friendly name / tags / command label，而不是随机 ID
- picker 居中 + 选中行整行反色，这个方向也正确
  - 选择器的核心不只是功能，而是要让用户一眼看出“我当前选中了谁”

### 这份提案不合理的地方

#### 1. 更像视觉草案，不是完整交互规格

这份稿子对“长什么样”描述较多，但对“怎么工作”定义不足。

尤其缺少以下几类关键规则：

- terminal metadata 修改后，哪些 pane 会同步更新
- workspace / layout 依赖 tags 时，tag 变更后现有 workspace 是否重排
- attach / split / floating / new-tab 复用同一 terminal 时，系统如何提示
- exited terminal 恢复后，name / tags / attach 关系如何处理

对于 termx 这种以 `terminal` 为一等公民的产品，这些规则比边框颜色更重要。

#### 2. 与当前 v3 mode 模型存在语义冲突

提案里把“编辑 terminal metadata”放到 `Ctrl-v (View mode) -> e`，这不合理。

原因是：

- `View mode` 应只负责 pane 的显示行为
  - `fit / fixed`
  - `readonly`
  - `pin`
  - offset / pan
- terminal metadata 是 terminal 级别动作，不是 view 级别动作

更合理的入口应该是：

- 在 `Ctrl-f` 的 terminal picker 中编辑选中的 terminal
- 或放到 `Ctrl-g` 的 global actions 中编辑当前 terminal

否则会把 `pane/view` 和 `terminal` 的职责重新搅混。

#### 3. “编辑态变红”不合理

提案建议 metadata 编辑时把 pane 边框变红，这会误导用户。

红色在 TUI 里更适合表示：

- 错误
- 危险动作
- destructive 操作（kill / remove / force close）

而修改 terminal `name/tags` 并不是危险动作。它更像“普通编辑态”，不应该默认使用警示色。

#### 4. 过度依赖颜色，缺少非颜色备份语义

“蓝=平铺，黄=浮窗，红=编辑”作为设计语言可以保留，但不能只靠颜色表达状态。

termx 必须同时提供：

- 边框/标题状态
- badge 文本
- 焦点文案
- mode bar 提示

原因很现实：

- 终端主题不一致
- ANSI 色域不稳定
- 色盲 / 低对比度环境会削弱颜色语义
- 仅靠颜色很容易在复杂场景下失效

#### 5. Emoji 不适合作为核心布局元素

提案里的 `🔍`、`🔒`、`📌` 等视觉上直观，但不适合作为主方案。

原因：

- 很多终端里的 emoji 宽度并不稳定
- 不同字体/平台会导致布局错位
- 会增加 TUI 对齐和裁剪的复杂度

更稳妥的主方案应该是：

- 优先 ASCII / 单宽字符
- emoji 仅作为可选增强，而不是布局依赖

#### 6. “无状态不提示”过度极端

提案强调未激活组件尽量变灰，这个方向没错；但如果做得过头，会直接损害可用性。

在 termx 里，用户需要持续感知这些东西：

- 还有哪些 pane 在运行
- 当前 tab 里有哪些 terminal
- 浮窗是否存在
- 哪个 terminal 是复用的

所以合理目标不是“未激活就几乎看不见”，而是：

- 焦点组件强强调
- 非焦点组件弱可见
- 结构信息永远不丢

#### 7. 空状态方案和 termx 的产品方向不完全匹配

提案给了一个很漂亮的 launcher 面板，但它不一定适合作为 termx 的默认首页。

更符合 termx 实际语义的规则应该是：

- 如果 terminal pool 为空：可以直接创建一个默认 terminal，或者给一个极简二选一
- 如果 terminal pool 非空：优先弹出居中的 terminal picker，让用户 attach / reuse

也就是说，“空状态面板”更适合作为首次引导或特定条件下的入口，而不是恒定首页。

#### 8. 浮窗设计偏重“好看”，但操作闭环没定义完整

提案对浮窗层级、亮黄色边框、背景降权这些视觉方向描述不错，但没有把交互闭环补完整。

还需要明确：

- tiled pane 与 floating pane 如何切换焦点
- 多浮窗复用同一 terminal 时，用户如何识别
- 当前浮窗的 z-order / 序号如何显示
- move / resize 时状态反馈放在哪里
- Esc / Tab / picker / mouse 之间的优先级如何协调

没有这些，浮窗就只是“看起来像浮窗”，而不是“真的好用的浮窗系统”。

#### 9. Mode Bar 的方向对，但还不够工程化

提案提出“左侧模式 badge + 右侧快捷键提示”的方向是正确的，但还缺关键工程规则：

- 宽度不足时，哪些提示优先保留
- 多状态同时存在时，右侧如何裁剪
- pane / terminal / workspace 状态是放底栏右侧还是二级行
- notice / error / flash message 与 mode bar 如何共存

不写这些规则，最后实现时很容易又回到“底栏太长太乱”的老问题。

#### 10. `GlobalFocusMode` 全屏降权风险较大

提案建议 mode 切换时让整屏 UI 一起降权，这个想法视觉上强，但实现和体验上风险都比较大：

- mode 切换时更容易出现闪烁感
- 全屏统一降权后，用户反而可能失去上下文
- 对渲染性能和状态同步也更敏感

更稳妥的做法是“局部降权”：

- 只改变非焦点 pane / 浮窗的边框和标题
- 不要让整个界面在不同 mode 间像切换主题一样大幅闪动

### 这份提案缺失但必须补充的内容

如果要把这份稿子发展成可实现的产品规格，下一版至少要补以下内容：

1. terminal metadata 的完整编辑模型
   - 不只是 `name`
   - 还包括 `tags`
   - 以及“修改后影响所有 attach 到该 terminal 的 pane”的提示
2. metadata 变更语义
   - 已绑定 pane 不自动重排
   - 所有 attach pane 立即刷新 title / tags
   - 新 tags 只影响未来 picker / search / layout resolve
3. 空状态策略的条件分支
   - 无 terminal
   - 有 terminal
   - 有 workspace state
   - 有 startup layout
4. 浮窗的完整焦点流转
   - tiled -> floating
   - floating -> tiled
   - floating 内部循环
   - mouse / keyboard 协同
5. mode bar 的窄屏降级规则
6. 零 ID 展示策略的稳定性规则
   - 名称为空时如何命名
   - 临时短名是否跨会话稳定
   - picker / pane / logs / help 是否统一口径

### 对这份提案的建议吸收方式

推荐采用“吸收视觉原则，不照搬交互定义”的方式推进：

- 吸收：
  - 色彩断层
  - 绝对遮挡
  - picker 居中反色
  - 零 ID 展示
  - mode bar 结构分离
- 不直接照搬：
  - `Ctrl-v -> e` 的编辑入口
  - 红色编辑态
  - emoji 作为主布局元素
  - 全屏统一降权
  - launcher 作为默认首页

### 最终判断

`Obsidian Clarity` 更像是一份**很有感觉的视觉方向稿**，而不是一份**可直接用于 termx 实现的产品规格书**。

它最大的价值在于：

- 帮 termx 明确了一种更干净、更现代的视觉语言
- 提醒我们必须重做 picker / mode bar / 浮窗层次

它的主要不足在于：

- 没有真正处理 termx 最核心的复杂度
  - terminal 与 pane 的解耦
  - metadata 的跨 pane 语义
  - workspace / layout 与 tags 的关系
  - 多浮窗 / 多复用 / 多焦点的规则

因此结论是：

- **视觉原则可吸收**
- **交互定义需重写**
- **不能把它当作最终产品规格直接实现**
