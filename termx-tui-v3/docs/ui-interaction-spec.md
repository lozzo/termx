# termx-tui-v3 UI 交互层规格

状态：草案
日期：2026-06-04

## 1. 文档目的

本文档定义 `termx-tui-v3` 的产品级 UI 交互规格。

本文档回答：

- TUI 启动后用户应该看到什么。
- 主界面由哪些区域组成。
- workspace、tab、pane、floating pane、terminal pool、copy mode、overlay modal 分别承担什么功能。
- 快捷键、鼠标、状态提示和页面退化应该遵循什么产品规则。
- 后续 render 架构和实现必须满足哪些可见行为。

本文档不回答：

- Go 包如何拆分。
- `RenderVM` 应该有哪些字段。
- 线框应该由什么算法绘制。
- 哪些 `tuiv2` 文件要复制、迁移或重写。
- renderer、canvas、cache、hit region 的技术实现方案。

`tuiv2` 只能作为产品形态和交互经验的参考。`termx-tui-v3` 不应把 `tuiv2` 的旧状态模型、旧历史来源或旧实现结构直接带入新主线。

## 2. 产品定位

`termx` 是一个面向长期工作现场的现代终端复用器。

它对用户表现得像：

- `tmux`
- `zellij`
- 一个带 terminal pool 的本地终端工作台

但产品概念上不完全等同于传统终端复用器：

- `terminal` 是全局运行实体。
- `pane` 是 terminal 的工作位或观察位。
- 一个 terminal 可以被多个 pane 复用。
- TUI 是 terminal pool 的第一方工作台。
- copy mode 和历史浏览必须来自 core-v2 的 authoritative history window，而不是 TUI 本地 scrollback。

因此 UI 必须同时满足两类体验：

- 日常工作时像终端复用器，终端输入直通、pane 清晰、切换快速。
- 管理 terminal 时像资源工作台，可以查找、预览、复用、kill、编辑 metadata。

## 3. 核心对象

### 3.1 Workspace

`workspace` 表示一个工作现场。

产品要求：

- 可以创建、切换、重命名、删除。
- 顶栏必须能显示当前 workspace。
- workspace 不天然绑定项目目录。
- workspace 下包含 tab、pane 和 floating pane 的组织状态。

### 3.2 Tab

`tab` 用来组织 pane。

产品要求：

- 可以创建、切换、跳转、重命名、关闭。
- 顶栏必须展示 tab strip。
- active tab 必须与 inactive tab 有明确视觉区分。
- tab 名过长时可以截断，但 tab 的身份和 active 状态不能丢失。

### 3.3 Pane

`pane` 是 terminal 的可视工作位。

产品要求：

- pane 不等于 terminal。
- pane 可以为空、连接 terminal、共享 terminal、显示 exited terminal 的最后状态。
- pane 支持水平和垂直分割。
- pane 支持 focus、close、zoom、attach existing terminal、create new terminal。
- pane 必须有可识别边界、标题、局部状态和必要动作。

### 3.4 Floating Pane

`floating pane` 是覆盖在 tiled pane grid 上的完整 pane。

产品要求：

- 可以创建、移动、resize、置顶、居中、折叠、关闭。
- floating pane 必须有明确 z-order。
- 鼠标点击 floating pane 时应聚焦并提升到最前。
- floating pane 的边界和 tiled pane 的边界必须一眼可区分。

### 3.5 Terminal

`terminal` 是全局运行实体。

产品要求：

- 可以 attach 到当前 pane。
- 可以 attach 为 split。
- 可以 attach 到新 tab。
- 可以 attach 为 floating pane。
- 可以编辑 metadata。
- 可以 kill。
- running、exited、unavailable 等生命周期状态必须可见。

### 3.6 Terminal Pool

`Terminal Pool` 是全局 terminal 管理页面，不是 picker 放大版。

产品要求：

- 查看 terminal 列表。
- 搜索 terminal。
- 预览 terminal 当前状态。
- 对 terminal 执行 attach、attach as tab、attach as floating、edit metadata、kill。
- 展示 terminal 是否 visible、parked、exited、bound。

### 3.7 Overlay Modal

`overlay modal` 用于短任务和局部选择。

第一阶段至少包括：

- Terminal Picker
- Workbench Tree
- Prompt
- Help
- Floating Overview

产品要求：

- overlay 是当前工作现场之上的临时层。
- overlay 关闭后必须回到原来的工作现场。
- overlay 内不展示快捷键文案，快捷键提示属于底部 status bar 和 Help。

## 4. 一级信息架构

`termx-tui-v3` 的 UI 分为三类 surface。

### 4.1 Workbench

默认主界面，承载日常工作。

必须支持：

- workspace / tab / pane 主工作流。
- tiled pane grid。
- floating pane overlay。
- terminal attach / split / create。
- live terminal display。
- copy mode 入口。
- empty pane 状态。
- exited pane 状态。

### 4.2 Terminal Pool Page

全局 terminal 管理页面。

必须支持：

- terminal list。
- 搜索。
- 当前选中 terminal 的 detail 和 preview。
- attach / new tab / floating / edit / kill 等动作。
- 页面级 footer action。

### 4.3 Overlay

临时弹层，用于轻量选择和局部任务。

必须支持：

- terminal picker。
- workbench tree。
- prompt。
- help。
- floating overview。

结构型管理不应长期停留在小 modal 中。workspace/tab/pane 导航应优先使用 Workbench Tree 这类大型结构弹层。

## 5. Workbench 主界面

### 5.1 顶栏

顶栏负责全局上下文。

左侧必须承载：

- workspace 名称。
- tab strip。
- create tab token。

右侧只承载：

- 短 notice。
- 短 error。

顶栏右侧不承载：

- 当前 pane 的状态。
- terminal owner/follower。
- 长帮助文案。
- 长运行态摘要。

顶栏可以被用户隐藏。

隐藏顶栏时：

- 主体区域向上扩展，占用原顶栏高度。
- workspace、tab、notice、error 不再常驻占用第一行。
- workspace / tab 仍必须能通过底栏、临时浮层、命令入口或 Workbench Tree 被识别和操作。
- 全局错误和通知优先进入右上角弹出消息系统。

### 5.2 主体区域

主体区域默认是 pane grid。

必须支持：

- 单 pane。
- 左右 split。
- 上下 split。
- 多层 split。
- zoomed pane。
- floating pane 覆盖。

pane 内容区显示：

- live terminal surface。
- copy mode history projection。
- empty pane CTA。
- exited pane recovery state。

tiled pane 支持两种呈现模式：

- `card panel`：每个 pane 都有独立完整包围，类似当前 tuiv2 的卡片式 pane。
- `split line`：pane 之间像 tmux 一样共享分割线，减少重复边框，提升内容利用率。

两种模式只改变 tiled pane 的视觉呈现，不改变 pane、terminal、copy mode、鼠标命中和快捷键语义。

floating pane 不跟随 tiled pane 呈现模式变化，始终保持独立带边框的卡片式 panel。

### 5.3 底栏

底栏负责当前 mode 和全局短摘要。

左侧必须承载：

- 当前 mode。
- 当前 mode 下最相关的 hints。

右侧必须承载：

- workspace 摘要。
- terminal 数量。
- floating pane 数量。
- 其他全局短 token。

底栏不承载：

- 长帮助文案。
- 当前 pane 的详细状态。
- 当前 terminal 的长标题。

底栏可以被用户隐藏。

隐藏底栏时：

- 主体区域向下扩展，占用原底栏高度。
- mode hint、workspace summary、terminal summary 不再常驻显示。
- 当前 mode 必须仍可通过短暂浮层、右上角消息、或 Help 被用户识别。
- 需要长期存在的结构信息不应依赖隐藏后的底栏作为唯一入口。

## 6. Pane 规格

### 6.1 Pane Chrome

pane chrome 负责局部上下文。

顶边左侧：

- pane title。
- terminal title。
- unconnected / exited 等短状态标题。

顶边右侧：

- lifecycle token。
- share count token。
- owner / follower / follow action token。
- pane action token。

底角：

- 几何提示。
- overflow 提示。
- resize affordance。

产品规则：

- 可点击区域必须稳定，不得随文案长度漂移。
- 长标题只能占用标题区。
- 右侧状态优先使用短 token。
- 宽度不足时优先隐藏低优先级状态，不挤压内容区。

### 6.2 Tiled Pane 呈现模式

tiled pane 必须支持两种产品形态。

card panel：

- 每个 pane 都有自己的完整外框。
- 相邻 pane 之间可以出现双边框或间隔感。
- pane 的标题、状态和 action 明确属于该 pane。
- 适合默认可读性、鼠标操作和多 pane 管理。

split line：

- 相邻 pane 共享分割线。
- 视觉上接近 tmux。
- 尽量减少 chrome 占用，把更多行列留给 terminal 内容。
- pane 的标题和状态可以采用更紧凑的 inline token 或仅在 active pane 上强化显示。

两种模式的共同约束：

- active pane 必须可识别。
- pane 边界必须可识别。
- pane action 的命中区域必须稳定。
- copy mode、empty pane、exited pane、loading / pending 都必须仍显示在所属 pane 内。
- 切换模式不能改变 pane 与 terminal 的绑定关系。

floating pane 不参与该模式切换，始终使用独立完整边框。

### 6.3 Connected Pane

连接 terminal 的 pane 必须显示：

- terminal 内容。
- pane title 或 terminal title。
- terminal 生命周期状态。
- shared / owner / follower 状态。

如果 terminal 正在运行但 live surface 尚未到达，pane 内容区可以显示 pending，但 pending 必须位于 pane 内，不能让整屏退化成裸文本。

### 6.4 Empty Pane

未连接 terminal 的 pane 不能是空白。

必须展示：

- 当前 pane 未连接 terminal 的说明。
- Attach existing terminal。
- Create new terminal。
- Open terminal manager。
- Close pane。

动作层级：

- Attach existing terminal 是 primary。
- Create new terminal 是 secondary。
- Open terminal manager 是 secondary。
- Close pane 是 danger 或低优先级破坏动作。

### 6.5 Exited Pane

terminal 退出后 pane 不应立即变成空白。

必须展示：

- 最后输出或最后 live surface。
- exited 状态。
- exit code 或简短原因，如果 core 提供。
- restart / reconnect / close / close and kill 等 recovery 动作。

### 6.6 Copy Mode Pane

copy mode 激活后，pane 内容区显示 authoritative history window。

必须展示：

- 当前浏览位置。
- 光标。
- selection。
- clipped before / clipped after 的可见提示。
- logical line 相关位置提示。

硬约束：

- copy mode 不得从 live surface、snapshot、grid viewport 或 TUI 本地 scrollback fallback。
- 如果 authoritative history window 尚未返回，pane 内显示 copy history loading / pending。
- 如果 authoritative history window 为空，pane 内显示 empty history。

## 7. Floating Pane 规格

floating pane 是完整 pane，不是装饰层。

必须支持：

- 独立边框。
- 独立标题。
- 独立 pane action。
- 点击聚焦。
- 拖动移动。
- 右下角或明确 handle resize。
- z-order 提升。
- collapse / restore。

拖动体验要求：

- 点击标题区域或明确拖动区域可以 move。
- 点击 resize handle 可以 resize。
- 拖动时不能移出屏幕负坐标。
- 必须保留最小宽高。
- 拖动过程中应有可识别的当前对象反馈。

## 8. Terminal Picker

Terminal Picker 是从当前 pane 出发的快速选择器。

职责：

- attach existing terminal。
- create new terminal。
- 用最短路径把当前 pane 连接到 terminal。

展示内容：

- title。
- search field。
- terminal list。
- selected row。
- new terminal row。
- 必要时的短 footer action。

列表行至少包含：

- terminal 名称。
- lifecycle 状态。
- bound / visible / parked 状态。
- 简短位置提示。

Terminal Picker 不承担：

- 全量 terminal 管理。
- 复杂 metadata 编辑。
- 多栏详情页。

这些功能属于 Terminal Pool。

## 9. Workbench Tree

Workbench Tree 是 workspace/tab/pane 的结构导航弹层。

它替代薄的 workspace picker，语义接近 `tmux choose-tree`。

### 9.1 布局

宽屏使用两栏：

- 左侧是 workspace / tab / pane 树。
- 右侧是所选对象的 summary 和 snapshot preview。

窄屏退化为：

- 上方树。
- 下方详情。

### 9.2 树节点

树至少包含：

- workspace row。
- tab row。
- pane row。
- floating pane row。

workspace row 展示：

- workspace 名。
- current 状态。
- tab 数。
- pane 数。
- floating 数。

tab row 展示：

- tab 名。
- active 状态。
- pane 数。
- zoom / floating 摘要。

pane row 展示：

- pane title 或 terminal title。
- owner / follower。
- exited / unconnected。
- floating 标记。

### 9.3 右侧预览

选中 workspace 时显示：

- workspace 名。
- tab / pane / floating 总数。
- active tab。
- active tab snapshot。

选中 tab 时显示：

- 所属 workspace。
- tab 名。
- pane 数。
- active pane。
- active pane snapshot。

选中 pane 时显示：

- 所属 workspace / tab。
- pane title / terminal title。
- lifecycle。
- snapshot preview。

### 9.4 动作

modal 内动作只显示动作词，不显示快捷键。

允许动作：

- Open。
- Rename。
- Delete。
- New Workspace。
- New Tab。
- Close Tab。
- Focus。
- Zoom。

快捷键提示不写在 modal 内，统一放在底栏和 Help。

## 10. Terminal Pool Page

Terminal Pool 是全局 terminal 资源页。

### 10.1 页面结构

推荐结构：

- 页面标题。
- 搜索区域。
- terminal list。
- selected terminal detail。
- snapshot preview。
- 页面 footer action。

### 10.2 列表行

terminal row 至少展示：

- name。
- command 或短描述。
- lifecycle。
- visible / parked。
- bound panes count。
- size。

### 10.3 Detail 区

detail 区至少展示：

- terminal id。
- name。
- command。
- state。
- size。
- bound panes。
- tags 或 metadata。
- snapshot preview。

### 10.4 Footer Action

footer action 使用纯动作词：

- Attach Here。
- New Tab。
- Floating。
- Edit。
- Kill。

footer action 在窄屏可以裁剪，但必须保持高优先级动作优先可见。

## 11. Prompt

Prompt 用于短输入任务。

适用场景：

- rename workspace。
- rename tab。
- create terminal。
- edit terminal metadata。
- confirm destructive action。

Prompt 必须包含：

- title。
- 简短上下文。
- one or more input field。
- submit / cancel 动作。

Prompt 内不显示快捷键字符串。

## 12. Help

Help 不是纯快捷键表。

Help 必须解释：

- Most used。
- Pane。
- Tab。
- Workspace。
- Floating。
- Terminal Pool。
- Display / Copy。
- Exit。

Help 应说明概念和动作，而不是只堆按键。

## 13. 快捷键交互

### 13.1 总体原则

normal 状态主要负责 terminal 输入直通。

结构操作必须通过 mode / prefix 进入，避免 normal 状态堆满直接快捷键。

无效按键应直接忽略，不得让 UI 进入异常状态。

`Esc` 必须能安全退出当前 mode 或 modal。

### 13.2 Root Keymap

第一阶段 root keymap：

- `Ctrl-p`：pane mode。
- `Ctrl-r`：resize mode。
- `Ctrl-t`：tab mode。
- `Ctrl-w`：workspace mode。
- `Ctrl-o`：floating mode。
- `Ctrl-v`：display / copy mode。
- `Ctrl-f`：terminal picker。
- `Ctrl-g`：global mode。
- `Esc`：退出当前 mode / modal。

### 13.3 Mode 职责

pane mode：

- split。
- close。
- focus。
- zoom。
- attach existing terminal。
- create new terminal。
- detach / reconnect / close and kill。

resize mode：

- 调整 pane 大小。
- 调整 terminal 内容在 pane 内的偏移。

tab mode：

- create。
- switch。
- jump。
- rename。
- close。

workspace mode：

- switch。
- create。
- rename。
- delete。
- open Workbench Tree。

floating mode：

- create。
- move。
- resize。
- center。
- collapse。
- close。

display / copy mode：

- 请求 latest history window。
- page up / page down。
- wheel scroll。
- cursor movement。
- selection。
- copy。

global mode：

- 打开 Terminal Pool。
- 打开 Help。
- 保存 / 退出。
- 其他全局动作。

## 14. 鼠标交互

鼠标交互必须基于可见对象。

必须支持：

- 点击 workspace label 打开 Workbench Tree 或 workspace 入口。
- 点击 tab 切换。
- 点击 tab close token 关闭 tab。
- 点击 tab create token 创建 tab。
- 点击 pane interior 聚焦 pane。
- 点击 pane chrome action 执行动作。
- 点击 floating pane 聚焦并提升。
- 拖动 floating pane 移动。
- 拖动 floating pane resize handle 调整大小。
- 点击 picker row 选择并提交。
- 点击 prompt input 定位输入光标。
- 点击 overlay 外部关闭 overlay。
- Terminal Pool row click 选择。
- Terminal Pool footer action click 执行动作。

鼠标 wheel：

- 在普通 pane 内容区，优先遵循 terminal mouse tracking。
- 未转发给 terminal 时，可用于 scrollback / copy mode / list navigation。
- 在 picker 和 Workbench Tree 中用于移动选择。
- 在 Terminal Pool 中用于移动列表选择。

UI chrome 优先级高于 terminal mouse forwarding。点击边框、标题、按钮、footer 等 UI 区域时，不应把事件发给 terminal。

## 15. 状态与反馈

### 15.1 Notice / Error

notice / error 是全局短反馈。

位置：

- 顶栏右侧。
- 顶栏隐藏时进入右上角弹出消息系统。

规则：

- 内容必须短。
- error 优先级高于 notice。
- 消失后不使用其他内容补位。

### 15.2 右上角弹出消息系统

TUI-v3 需要一个现代化消息系统，而不是只把消息写成一段简单文本。

消息系统用于展示：

- command 成功反馈。
- attach / create / kill / remove 等操作结果。
- warning。
- error。
- copy 完成提示。
- daemon / connection / resize 等系统状态变化。

默认位置：

- 右上角。
- 浮在主界面之上。
- 不改变 pane layout。
- 不抢占 terminal 内容的永久高度。

消息形态：

- 类似现代 CLI/TUI 工具的 toast。
- 有短标题。
- 有正文摘要。
- 有 severity 标识。
- 可以有进度或 pending 状态。
- 多条消息按时间堆叠，但数量必须受限。

severity：

- info。
- success。
- warning。
- error。

产品规则：

- error 停留时间应长于 info / success。
- 操作成功消息可以自动消失。
- 用户必须能通过统一动作关闭当前消息或清空消息。
- 消息不应遮挡 active pane 的关键输入位置过久。
- 在极窄屏下，消息可以退化为单行顶部或底部提示。
- 当全局顶栏隐藏时，消息系统承担 notice / error 的主要可见反馈职责。

消息系统不替代：

- pane 内 loading / pending。
- copy mode 状态。
- Terminal Pool 页面内的列表状态。
- Help。

### 15.3 Mode Feedback

mode feedback 在底栏左侧。

必须包含：

- 当前 mode 名。
- 该 mode 下最相关动作。

不应显示：

- 全部快捷键表。
- 与当前 mode 无关的动作。

### 15.4 Pane State

pane state 在 pane chrome 内表达。

状态包括：

- running。
- exited。
- unconnected。
- loading / pending。
- owner。
- follower。
- shared。
- copy mode。

### 15.5 Loading / Pending

loading / pending 必须归属于具体区域。

示例：

- live surface pending 位于 pane 内容区。
- copy history loading 位于 copy mode pane 内容区。
- terminal pool loading 位于 terminal pool list 或 detail 区。

不允许整屏只显示裸 pending 文本，除非整屏 surface 本身就是错误页或恢复页。

## 16. 线稿

本节是产品线稿，不规定实现方式。

### 16.1 Workbench

card panel 模式：

```text
 workspace: main | tab 1 | tab 2 | + ---------------------- notice / error +
| shell                                          run x2 owner  [z][|][-][x] |
| +-------------------------------+----------------------------------------+ |
| | terminal content              | terminal content                       | |
| |                               |                                        | |
| |                               |                                        | |
| +-------------------------------+----------------------------------------+ |
|                                                                            |
|              floating pane can appear above the tiled pane grid             |
|                                                                            |
+ normal | Ctrl-p pane | Ctrl-f picker | Ctrl-v copy -------- ws:main terms:4 +
```

split line 模式：

```text
 workspace: main | tab 1 | tab 2 | + ---------------------- notice / error +
| shell run owner                      | logs run follower                    |
| terminal content                     | terminal content                      |
|                                      |                                       |
|                                      |                                       |
|--------------------------------------+---------------------------------------|
| worker run                           | htop run                              |
| terminal content                     | terminal content                      |
|                                      |                                       |
+ normal | Ctrl-p pane | Ctrl-f picker | Ctrl-v copy -------- ws:main terms:4 +
```

最大内容利用率模式可以隐藏 header 和 footer：

```text
| shell run owner                      | logs run follower                    |
| terminal content                     | terminal content                      |
|                                      |                                       |
|                                      |                                       |
|--------------------------------------+---------------------------------------|
| worker run                           | htop run                              |
| terminal content                     | terminal content                      |
|                                      |                                       |
```

右上角消息可以浮在 workbench 上方，不改变布局：

```text
 workspace: main | tab 1 | tab 2 | + --------------------------------------+
| shell                                          run x2 owner  [z][|][-][x] |
| +-------------------------------+----------------------------------------+ |
| | terminal content              | + success ---------------------------+ | |
| |                               | | Terminal created: worker           | | |
| |                               | +------------------------------------+ | |
| +-------------------------------+----------------------------------------+ |
+ normal | Ctrl-p pane | Ctrl-f picker | Ctrl-v copy -------- ws:main terms:4 +
```

### 16.2 Empty Pane

```text
+ shell -------------------------------------------------------------- pane --+
|                                                                            |
|                         No terminal attached                               |
|                                                                            |
|                         [ Attach existing terminal ]                       |
|                         [ Create new terminal ]                            |
|                         [ Open terminal manager ]                          |
|                         [ Close pane ]                                     |
|                                                                            |
+----------------------------------------------------------------------------+
```

### 16.3 Copy Mode

```text
+ shell -------------------------------------------------------------- copy --+
| clipped before                                                              |
| old output line                                                             |
| selected output line                                                        |
| cursor line                                                                 |
| clipped after                                                               |
+ copy | line 120/480 | token current ------------------------- copied: none --+
```

### 16.4 Terminal Picker

```text
+ Terminal Picker -----------------------------------------------------------+
| search:                                                                    |
| > shell          running   visible   main/tab 1/pane 1                     |
|   logs           running   parked    unbound                               |
|   worker         exited    parked    unbound                               |
|   + new terminal                                                           |
+----------------------------------------------------------------------------+
```

### 16.5 Workbench Tree

```text
+ Workbench --------------------------------+ Snapshot / Details ------------+
| search:                                   | main                            |
| > workspace main        tabs 3 panes 7    | tabs 3   panes 7   float 2      |
|     tab deploy          active panes 3    |                                  |
|       pane shell        owner             | +------------------------------+ |
|       pane logs         follower          | | active tab snapshot          | |
|     tab ops                    panes 2    | |                              | |
|   workspace dev         tabs 2 panes 4    | +------------------------------+ |
|                                           | Open   Rename   Delete   New Tab |
+-------------------------------------------+----------------------------------+
```

### 16.6 Terminal Pool

```text
+ Terminal Pool -------------------------------------------------------------+
| search:                                                                    |
|                                                                            |
| > shell       running   visible   panes 2   120x36                         |
|   logs        running   parked    panes 0   100x30                         |
|   worker      exited    parked    panes 0   80x24                          |
|                                                                            |
| shell                                                                      |
| command: /bin/zsh                                                          |
| bound: main/tab 1/pane 1, main/tab 2/pane 3                                |
| +------------------------------------------------------------------------+ |
| | snapshot preview                                                       | |
| +------------------------------------------------------------------------+ |
+ Attach Here | New Tab | Floating | Edit | Kill ----------------------------+
```

### 16.7 Prompt

```text
+ Rename Tab ---------------------------------------------------------------+
| current: deploy                                                            |
|                                                                            |
| name: deploy                                                               |
|                                                                            |
| Submit   Cancel                                                            |
+----------------------------------------------------------------------------+
```

### 16.8 Floating Pane

floating pane 始终保持独立带边框，不随 tiled pane 的 card / split line 模式变化。

```text
| tiled pane content                    | tiled pane content                    |
|                                       |                                       |
|             + floating: logs -----------------------------+               |
|             | terminal content                            |               |
|             |                                             |               |
|             +---------------------------------------------+               |
|                                       |                                       |
```

## 17. 宽窄屏退化

### 17.1 宽屏

宽屏优先展示：

- 完整 top bar。
- 多 pane grid。
- floating pane。
- Workbench Tree 两栏。
- Terminal Pool list + detail + preview。
- card panel 或 split line 模式都应成立。
- 右上角消息可以使用完整 toast 形态。

### 17.2 中等宽度

中等宽度优先保留：

- workspace。
- active tab。
- pane title。
- 当前 mode。
- primary action。

可以隐藏：

- 次要 tab action。
- 低优先级 pane state。
- terminal pool 低优先级字段。
- 底栏右侧低优先级摘要。
- 非关键 header/footer 内容。
- 低优先级 toast 正文。

### 17.3 窄屏

窄屏必须保证：

- terminal 内容仍可用。
- 当前 workspace / active tab 至少有短标识。
- active pane 边界可识别。
- 当前 mode 可识别。
- overlay 可退化为单栏。
- split line 模式可以优先用于提升内容利用率。
- header/footer 可以隐藏，但当前上下文必须能通过短标识、消息或命令入口恢复识别。

窄屏可以隐藏：

- inactive tab 长标题。
- pane action 的部分低优先级按钮。
- preview 详情。
- terminal pool 的部分 metadata。
- 顶栏。
- 底栏。
- toast 的正文，只保留 severity 和短标题。

## 18. 视觉语言

总体风格：

- 终端原生。
- 工程工具。
- 高信息密度。
- 清晰层级。
- 不依赖固定品牌背景。

颜色原则：

- 默认适配宿主终端主题。
- 允许少量 semantic accent。
- success / warning / danger / info 必须语义稳定。
- 不要求固定深色主题。

组件语言：

- token。
- chip。
- badge。
- toast。
- 短 action。
- 稳定槽位。
- 明确选中态。
- 明确焦点态。

## 19. 硬约束

- TUI-v3 主线不得引入 Bubble Tea runtime 或 Bubble Tea contract。
- modal 内不显示快捷键字符串。
- copy mode 历史来源只能是 core-v2 authoritative HistoryWindow。
- live surface pending、copy history loading 等状态必须显示在所属区域内。
- pane chrome 的点击区域必须稳定。
- tiled pane 必须支持 card panel 与 split line 两种呈现模式。
- floating pane 必须保持独立带边框，不受 tiled pane 呈现模式影响。
- 全局 header/footer 可以隐藏，但隐藏后不能导致 workspace、tab、mode、notice/error 彻底不可达。
- 右上角弹出消息系统不得永久改变 pane layout。
- 默认 TUI 必须支持 styled chrome renderer；纯文本 Unicode 线框不满足最终视觉要求。
- pane active/inactive 必须通过颜色、亮度或等价 style 明确区分，不能只靠标题文字。
- top bar、bottom bar、pane chrome、toast、overlay 的颜色和背景必须通过 semantic style token 输出到真实 TTY。
- 任何 UI 设计必须有宽窄屏退化策略。
- 任何会影响默认界面形态的实现，都必须先满足本文档。

## 20. 第一阶段验收标准

第一阶段不要求所有高级功能都完成。当前最小 render framework 阶段已经落地，默认 `termx` 进入 TUI 后必须满足：

- 有顶栏。
- 有底栏。
- 有至少一个可识别 pane。
- pane 有边界、标题和状态。
- tiled pane 已支持 card panel 与 split line 两种呈现模式。
- 最小双 pane 横向和纵向 split 已可渲染。
- header/footer 已支持隐藏，隐藏后 body 回收空间。
- 右上角 toast 已作为全局 notice / error 的主要展示入口之一。
- live surface pending 显示在 pane 内。
- terminal 内容到达后显示在 pane 内容区。
- `Ctrl-f` 能进入 Terminal Picker。
- `Ctrl-v` 能进入 Display / Copy 路径，并且没有 authoritative history 时显示 pane 内 loading / empty。
- `Esc` 能退出当前 mode / modal。
- 非交互 smoke 不得把裸文本 frame 当作可用默认界面完成标准。

当前第一阶段 resize/UI 验收状态：

- 默认界面、`termx attach` 装配路径和非交互 smoke 均以 core-v2/tui-v3 为主路径。
- 默认界面不再以裸 `live surface pending` 文本作为可用 UI，而是显示 shell、header/footer、pane chrome、content slot、toast 或 overlay。
- 外部 terminal emulator 初始尺寸和 resize 后尺寸必须反映到整屏 frame；frame 行数等于 viewport rows，每行 display width 等于 viewport cols。
- terminal live 内容只显示在 pane content slot 内；content slot 尺寸变化后，terminal resize 使用 content rect，而不是外部 viewport 总尺寸。
- copy mode 进入后只显示 authoritative history window、pending、empty 或 error；resize 或 chrome 变化导致 cols 不一致时必须重新绑定 window，不显示旧 cols 的历史内容。
- 默认 card、overlay、toast 线框使用圆角 Unicode box drawing；split line 使用连续 Unicode box drawing；默认 UI chrome 不使用 ASCII 线框。
- emoji、CJK、combining mark 和 ANSI styled text 不得破坏 pane、split line、toast、overlay 或整行宽度。

当前 styled chrome 视觉目标：

- 默认界面应向 `tuiv2` 截图级视觉对齐，而不是停留在纯文本线框。
- tiled pane 默认使用 square Unicode 细线边框，active pane 使用 accent / strong border，inactive pane 使用 muted border。
- pane 顶部 chrome 必须有稳定槽位：title、state、owner/follower、action、copy/resize 等短 token 可逐步接入。
- top bar 和 bottom bar 必须是 styled bar，背景填满整行，token 有明确颜色层级。
- toast、overlay、floating/modal 可以使用 rounded card，但必须带独立 border/background/severity style。
- 真实 TTY 输出必须保留 ANSI 样式；非交互 smoke 也必须能验证 styled frame 没有退化成纯文本。

当前未完成但产品要求仍保留：

- styled chrome renderer：styled `RenderResult`、ANSI `FrameSink`、theme token、active/inactive pane border、styled top/bottom bar、styled toast/overlay。
- floating pane 完整交互、z-order、drag/resize 和带边框渲染。
- Terminal Pool 完整页面。
- Workbench Tree 完整 overlay。
- Prompt、Help、Floating Overview overlay。
- 多层 split、pane resize 和复杂 pane 管理。
- header/footer hide、card/split、toast close/clear 的最终产品快捷键；当前只允许通过 semantic action 或测试消息接入，不能临时发明快捷键。

## 21. 后续讨论入口

后续讨论 render 架构时，应以本文档为产品基准。

允许讨论：

- 如何把上述页面映射到 `termx-tui-v3` 的状态、view-model 和 renderer。
- 哪些 `tuiv2` 的产品行为应保持。
- 哪些 `tuiv2` 的实现结构不能迁入。
- 如何分阶段实现 Workbench、Terminal Pool、Workbench Tree 和 Copy Mode。

不允许绕过本文档直接补临时线框，把默认入口显示成“看起来有东西”后就宣称完成。
