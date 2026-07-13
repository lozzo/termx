# tui UI 交互层规格

状态：草案
日期：2026-06-04

## 1. 文档目的

本文档定义 `tui` 的产品级 UI 交互规格。

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

`tuiv2` 只能作为产品形态和交互经验的参考。`tui` 不应把 `tuiv2` 的旧状态模型、旧历史来源或旧实现结构直接带入新主线。

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
- pane 可以为空、通过 `TerminalView/Attachment` 连接 terminal、共享 terminal、显示 exited terminal 的最后状态。
- pane 支持水平和垂直分割。
- pane 支持 focus、close、detach view、zoom、attach existing terminal、duplicate terminal view、create new terminal。
- pane 必须有可识别边界、标题、局部状态和必要动作。

交互语义：

- focus pane 只改变 UI active view，不等于创建新 terminal。
- close pane 只移除该工作位；如果 pane 绑定了 terminal view，只 detach 当前 view，不 kill terminal。
- detach view 明确断开当前 pane 与 terminal 的连接，pane 变为空态。
- close and kill / kill terminal 是破坏性动作，会终止 terminal 并影响所有连接同 terminal 的 pane/floating view。
- duplicate terminal view 会在当前 tab 的新 split、新 tab 或 floating 中连接同一个 terminal，不创建新 process。
- 同一个 terminal 的多个 view 必须有可识别的 shared 状态；但 shared 状态不能替代 terminal lifecycle 状态。

### 3.4 Floating Pane

`floating pane` 是覆盖在 tiled pane grid 上的完整 pane。

产品要求：

- 可以创建、移动、resize、置顶、居中、折叠、关闭。
- floating pane 必须有明确 z-order。
- 鼠标点击 floating pane 时应聚焦并提升到最前。
- active floating pane 获得视觉焦点时，后方 tiled pane 不得继续显示 active 高亮边框；tiled pane 只保留业务 active target，视觉上必须降级为 inactive / muted。
- floating pane 打开后，鼠标点击未被遮挡的 tiled pane content、chrome 或 action 必须能把视觉焦点切回 tiled pane；floating pane 保持打开但清空 active 状态，边框和标题样式降级为 inactive / muted。
- floating pane 的边界和 tiled pane 的边界必须一眼可区分。

### 3.5 Terminal

`terminal` 是全局运行实体。

产品要求：

- 可以 attach 到当前 pane。
- 可以 attach 为 split。
- 可以 attach 到新 tab。
- 可以 attach 为 floating pane。
- 可以 duplicate 为当前 terminal 的另一个 view。
- 可以编辑 metadata。
- 可以 kill。
- running、exited、unavailable 等生命周期状态必须可见。

`terminal` 与 `TerminalView/Attachment` 的区别：

- terminal 是全局运行实体，拥有 process、PTY size、history truth 和 lifecycle。
- view 是某个 pane/floating/tab 对 terminal 的连接，拥有 focus、content rect、copy mode 绑定、input channel、resize role 和 view-local error。
- 同一 terminal 多 view 共享输入目标和历史 truth；用户当前 active view 决定键盘输入送到哪个 attachment channel。
- resize owner view 可以改变 terminal PTY size；follower/observer view 不能因为自身布局变化覆盖 terminal size。

### 3.6 Terminal Pool

`Terminal Pool` 是全局 terminal 管理页面，不是 picker 放大版。

产品要求：

- 查看 terminal 列表。
- 搜索 terminal。
- 预览 terminal 当前状态。
- 对 terminal 执行 attach、attach as tab、attach as floating、edit metadata、kill。
- 对 terminal 执行 duplicate into split/tab/floating、detach selected view、edit metadata、kill。
- 展示 terminal 是否 visible、parked、exited、bound/shared，以及当前 view role 是 owner、follower 还是 observer。

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

`tui` 的 UI 分为三类 surface。

### 4.1 Workbench

默认主界面，承载日常工作。

必须支持：

- workspace / tab / pane 主工作流。
- tiled pane grid。
- floating pane overlay。
- terminal attach / duplicate view / split / detach / create。
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

顶栏产品化后必须表达：

- 当前 workspace。
- 当前 tab 或 tab strip 摘要。
- active pane 的短标识。
- terminal 数量和 floating 数量摘要。
- 短 notice / error。

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

底栏产品化后必须表达：

- 当前 mode。
- 当前 mode 下可执行的快捷键提示。
- active pane / terminal 的短状态。
- workspace、terminal、floating 的短摘要。

底栏提示必须按 mode 变化：

- live mode 显示 pane、resize、copy、picker、global 的入口。
- pane mode 显示 split、close、focus、zoom、balance、presentation。
- resize mode 显示方向 resize、balance、退出。
- global mode 显示 header/footer hide、Help、Terminal Pool、Workbench Tree、quit 等全局产品入口；toast 清理不作为底栏主路径展示。
- copy / overlay mode 显示退出、选择、提交或关闭等当前上下文动作。

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

### 10.5 一期落地边界

Terminal Pool 管理页一期的目标是让用户离开小型 picker，进入一个可持续停留的 terminal 管理 surface。

一期必须可见：

- 页面打开和关闭。
- search field。
- terminal list。
- list loading / empty / error 状态。
- selected row。
- selected terminal detail。
- preview 区域。
- Attach Here、Edit、Kill 三类核心动作。
- 在 80x24 级别的常规 viewport 中，上述 list、selected detail、preview 摘要和核心 footer action 不得被 overlay/page 高度裁掉。
- 窄高不足时按优先级退化：保留 search、至少一个 list row、selected row 状态和核心 action；detail 可以压缩为单行摘要，preview 可以显示 pending/summary 占位。

一期必须可操作：

- 键盘输入进入 search，不漏发给底层 terminal。
- 上下选择 terminal row。
- 鼠标点击 row 后切换 selected row。
- 鼠标点击 footer action 执行动作。
- `Esc` 关闭页面并回到原 workbench。
- `Enter` 对 selected row 执行 Attach Here。
- Edit 和 Kill 至少接入 service/effect/result 边界，并通过 toast 或页面状态反馈结果；不得在 result 到达前改写列表或 terminal lifecycle。
- empty pane 的 manager action 可以打开 Terminal Pool Page，但不得绕过同一页面状态和 list loading 语义。

一期不要求：

- 跨 remote terminal 管理。
- attach as new tab / attach as floating 的完整闭环。
- metadata 表单的最终 Prompt 体验。
- kill destructive confirm 的最终样式。
- preview 使用完整 terminal emulator cell model。

产品边界：

- Terminal Pool Page 是全局资源管理页面，不是 Terminal Picker overlay 的放大版。
- Terminal Picker 保持快速 attach / create 入口；Terminal Pool Page 负责更完整的列表、详情、预览和管理动作。
- Terminal Pool Page 的 action 结果必须通过 toast 或页面内状态反馈给用户，不得静默修改列表。
- kill、edit、attach 结果到达前不得伪造 terminal lifecycle。
- 页面内容由 render framework 分配的 page/overlay content rect 承载；content renderer 不得直接覆盖 pane border、top bar、bottom bar、toast 或其他 overlay。
- 页面 hit region 必须优先于 terminal input forwarding；row、footer action、close 区域命中后不得生成 terminal input。

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

pane、tab、workspace、floating 等结构操作必须先定义为稳定动作语义，再映射到快捷键、鼠标或后续 CLI mini command。快捷键只是入口，不是产品语义本身。

### 13.2 Root Shortcuts

第一阶段 root shortcuts：

- `Ctrl-p`：pane mode。
- `Ctrl-r`：resize mode。
- `Ctrl-t`：tab mode。
- `Ctrl-w`：workspace mode。
- `Ctrl-o`：floating mode。
- `Ctrl-v`：display / copy mode。
- `Ctrl-f`：terminal picker。
- `Ctrl-g`：global mode。
- `Esc`：退出当前 mode / modal。

当前已落地的第一版入口：

- `Ctrl-p`：pane mode，承载 close / focus next / focus previous / zoom / balance / card / split presentation；split right / split down 只通过 pane chrome、鼠标 hit region 或 semantic command 入口触发。
- `Ctrl-r`：resize mode，承载方向 resize 和 balance。
- `Ctrl-g`：global mode，承载 header/footer hide、Terminal Pool、Workbench Tree、Help、quit 等全局入口；toast 清理保留为次级维护快捷键。
- `Ctrl-o`：floating mode，承载 create、move、resize、center、collapse / restore 和 close。
- `Ctrl-t`：tab mode，承载 create、switch、rename 和 close。
- `Ctrl-w`：workspace mode，承载 create、switch、rename 和 Workbench Tree。
- `Ctrl-f`：Terminal Picker overlay，承载 query、过滤、键盘选择和确认 attach/focus。
- `Ctrl-v`：Display / Copy authoritative history 路径。
- `Esc`：退出 mode 或关闭 overlay，不得漏发给 terminal。

当前尚未产品化的入口只包括更完整的 command palette 和跨 workspace terminal attach；可配置 shortcuts 已由 `tui/action`、`tui/shortcut`、配置 loader、footer/Help 与 app dispatcher 形成闭环。

### 13.2.1 当前快捷键实现核查

当前快捷键实现核查表维护在 `tui/docs/shortcut-inventory.md`。canonical identity 来自 `tui/action`，scene+key 真值来自 `tui/shortcut`，配置后的输入路由与 footer/Help 都消费同一编译 catalog；inventory 只记录审计结论和历史迁移背景，不作为第二份运行时键表。

本 spec 和 inventory 都不维护完整快捷键表；新增、删除或改名快捷键时只修改 owning runtime catalog，并补充对应 input/render/app harness。inventory 的数量由测试从运行 catalog 自动投影，不随按键手工更新。

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
- 打开 Workbench Tree。
- 退出 TUI。
- 隐藏 / 恢复 header/footer。
- toast 清理只作为次级维护快捷键保留，不进入主 footer / Help 路径。

### 13.4 Pane 结构命令

pane 结构命令是 Workbench 的核心操作面，不应只存在于快捷键 handler 中。

这些命令必须能被多个入口触发：

- pane mode；不包含 split right/down 键盘入口。
- resize mode。
- 鼠标点击 pane chrome、resize handle 或 split divider。
- 测试和 smoke harness。
- 后续 CLI mini command 或 command palette。

这些入口触发后应表现为同一类用户动作，不应出现“快捷键能做、鼠标做的是另一套逻辑、CLI mini command 又绕过状态”的分裂。

第一阶段必须覆盖：

- split right / split down：在当前 pane 周围创建左右或上下分屏。
- close pane：关闭当前 pane，但不 kill terminal。
- close and kill：关闭 pane 并 kill 其绑定 terminal，属于破坏性动作。
- focus pane：切换 active pane。
- zoom / unzoom：最大化当前 pane 或恢复原布局。
- resize by direction：按方向和步长调整 pane 大小。
- set pane size：按比例或固定 cell size 设置 pane 大小。
- balance / equalize：重新均分当前 split group。
- switch panel presentation：在 card panel 和 split line 之间切换。

命令成功后必须有可见反馈：

- active pane 变化必须立即通过 pane chrome 表达。
- split、close、resize、zoom 后主体区域必须立即重新布局。
- close and kill 必须进入确认或明确 danger 反馈。
- 无效命令必须显示短提示或 toast，不得静默破坏状态。
- resize / move / focus 这类直接操控成功结果应优先通过 pane/floating 几何、active border、footer active target 等视觉反馈表达，不应在连续拖动时刷出 toast。错误、确认、copy、split/close 等离散结果仍可以显示 toast。

命令至少需要表达：

- 目标 workspace / tab / pane。
- split orientation。
- resize direction。
- resize delta。
- size ratio 或 fixed cell size。
- 是否需要 destructive confirm。
- 命令来源。

命令来源只用于反馈、审计或冲突处理，不应改变同一个命令的核心行为。

产品规则：

- split 可以创建空 pane、attach existing terminal 或 create new terminal，但这些是后续 attach 语义，不应和 split layout 本身混成一个动作。
- close pane 默认只关闭当前工作位。
- kill terminal 必须显式表达，不得作为 close pane 的隐含副作用。
- resize pane 后，绑定 terminal 的内容区域大小必须随之变化。
- copy mode 中的 pane 宽度变化后，历史窗口必须按新宽度重新绑定。

## 14. 鼠标交互

鼠标交互必须基于可见对象。

必须支持：

- 点击 workspace label 打开 Workbench Tree 或 workspace 入口。
- 点击 tab 切换。
- 点击 tab close token 关闭 tab。
- 点击 tab create token 创建 tab。
- 点击 pane interior 聚焦 pane。
- 点击 pane chrome action 执行动作。
- 点击 split divider 或 resize handle 进入 resize 语义。
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

当前已落地的鼠标基础：

- app/runtime 已把真实鼠标坐标派发到最新 render hit region。
- 点击 pane content 或 pane chrome 可以切换 active pane，并立即改变 active / inactive pane 视觉状态。
- 点击 pane action slot 可以执行同一 semantic command；当前可见 pane action 只包含真实接通的 split down、split right 和 close，zoom 等未恢复接线的 action 不绘制。
- 按住并拖动 pane split divider 会持续派发同一 `PaneCommandResize` 语义；拖动时绑定起始命中的 divider。多列同轴 pane 中优先只调整 divider 两侧的视觉相邻叶 pane，不得把第 3/4 列这类非相邻 pane 一起缩放；复杂嵌套分屏至少不得误改外层 split；拖动过程中不会漏发到底层 terminal。
- 点击 floating pane title/content 会聚焦并提升 z-order。
- 点击 floating pane 顶部 close action 会关闭 floating pane。
- 按住 floating pane 标题栏并拖动会持续派发同一 `FloatingCommandMove` 语义；按住右下 resize handle 并拖动会持续派发同一 `FloatingCommandResize` 语义；release 后清理 runtime drag state。
- toast、overlay、floating、pane action、split divider resize、pane chrome、pane content 的命中优先级已经固定，UI chrome 命中不得继续转发到 terminal；toast/overlay 遮挡 floating 时不允许鼠标穿透到底层 floating。pane action 必须优先于 split divider resize，避免横向分屏下方 pane 顶边的 split 图标被 resize 抢占。

当前后续必须补齐：

- Prompt input click 定位输入光标。

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
- pane split / close / resize / zoom 结果。
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

当前视觉参考补充：

- 视觉验收基线见 `tui/docs/visual-alignment-audit.md`。当前 v3 已有基础 styled chrome 和可操作产品壳，但尚未达到用户提供的 `tuiv2` 截图级视觉效果；后续不得把现有 smoke 线框误判为视觉完成。
- 顶栏和底栏参考 `tuiv2` 的产品形态：高信息密度、整行稳定占位、左侧工作区和 tab token、右侧短摘要或状态 token，隐藏后 body 必须真实回收空间。
- pane 参考 `tuiv2` 的 square 细线 panel 风格：边框连续、顶边 title/state/action 槽位稳定，active pane 使用 accent，inactive pane 使用 muted；默认界面不得退回 ASCII `+ - |` 或无样式 Unicode 线框。
- 单个 pane 的产品风格参考 `tuiv2` 截图中的紫色 accent 细边框、顶部 owner/action token 和内容区裁切；切片 88 后默认 theme 已改为紫色 accent + 深色 chrome，但仍必须通过真实截图级复核确认是否达标。
- 右上角消息参考现代 CLI/TUI 的 toast：实体卡片、短文本、severity 或 accent 侧边，不改变 pane layout；复制成功等短反馈可以使用这种形态。
- modal/overlay 参考现代 command palette 的简单弹出框：前景可以有标题、搜索行和必要列表项，但快速入口默认不做 page-sized 大卡片或重背景填充。
- Terminal Picker、Terminal Pool、Workbench Tree、Prompt、Help 和 copy-history 的内容层不能退回工程表格：搜索行统一使用短 search affordance，selected row 必须有强视觉 marker。Terminal Picker 保持 compact search/list/create，create row 显示短说明，terminal row 可显示短 terminal id、title、state 和 `@location/source`；不显示 selected hint、target、detail/preview 或内容区 action row。Terminal Pool、Workbench Tree、Prompt、Help 和 copy-history 才按各自页面语义显示 detail / preview / context / input 或 action row。
- copy-history 的内容层必须有清晰的 search row、match state、scrollbar/status row 和历史行层级；这些视觉元素只能来自 authoritative `HistoryWindow` 投影，不能为了显示效果从 live surface fallback。
- overlay 不要求灰度遮罩背景；中文、emoji、CJK、combining mark 或 ambiguous width 字符若无法安全套用 dim 样式，必须优先保证文本可见和宽度正确，不得为了背景灰度让非英文文本消失。
- floating pane、Prompt、Help 和 Workbench Tree 后续都必须使用 styled chrome；它们可以有不同尺寸和内容密度，但不得绕过 render framework 直接写临时线框。

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
- 默认 pane、floating、overlay、toast 线框使用直角 Unicode box drawing；split line 使用连续 Unicode box drawing；默认 UI chrome 不使用 ASCII 线框。
- emoji、CJK、combining mark 和 ANSI styled text 不得破坏 pane、split line、toast、overlay 或整行宽度。

当前 styled chrome 视觉目标：

- 默认界面已从纯文本线框推进到 styled chrome renderer 工程基线；根据视觉审计，后续仍必须继续把 header/footer、pane chrome、toast、overlay、floating 推进到 `tuiv2` 截图级视觉效果。
- `RenderResult`、`Frame` 和真实 `FrameSink` 已保留 ANSI styled frame；非交互 smoke 已能验证 styled frame 没有退化成纯文本。
- tiled pane 默认已完成第一轮 `tuiv2` 风格 pane chrome 重绘：card panel 与 split line 都使用 square Unicode 细线、active pane accent / strong border、inactive pane muted border、连续外框和连接点合成。
- pane 顶部 chrome 已有稳定 title、state、action 槽位；owner/follower、copy/resize 等更细 token 和目标截图级布局后续逐步接入。
- split line 使用共享外框加内部共享 divider，content rect 会避开外框和 divider；terminal resize 与 copy-history rebind 必须使用新的 content cols/rows。
- top bar 和 bottom bar 已在切片 80 重绘为分段产品栏：背景填满整行，workspace/tab/mode/action/active/summary 通过稳定 token 输出，accent/muted/warning 语义可见，Unicode `│` 分隔，窄屏下快捷键按优先级压缩且 error/exited 关键状态优先保留。
- shell/pane 已在切片 88 完成二轮视觉重绘；toast/floating/overlay title 使用 `·` 分层。切片 166 后 header/footer 按 `tuiv2` 实际 tab/status bar 纠偏为单行 bar，不再绘制整屏 shell 外框、双行 header/footer 或 `┬/┴` 连接点；bottom bar key 使用 `[Ctrl] • [P] pane` 这类分段状态栏语义。tiled pane 顶边继续显示真实接通的 split-down、split-right、close action，按钮文本与 hit region 同源，pane action 优先于 shared divider resize；仍不提前画状态 Nerd Font、`⇄2`、`◆ owner`、`1/31`、zoom 或 owner/follower token。
- toast、Terminal Picker、Terminal Pool、Workbench Tree、Prompt/Help 和 floating 已在切片 82 完成第一轮实体 card 视觉对齐：toast 具备 severity accent 竖条、右上角留白、close action 和 title/body 合并裁切；overlay/floating 具备 title/state/action 槽位、content padding、active/focus token、ANSI reset 和宽字符安全。
- Terminal Picker、Terminal Pool、Workbench Tree、Prompt、Help 和 copy-history 已在切片 86 完成第一轮内容层产品化 polish：搜索行、selected row、detail/preview/context/input、action row、copy search/match/scrollbar/status 已统一为更接近目标截图的产品语言。
- 切片 87 已完成真实视觉复核未通过归档：`termx v3 smoke` 固定 case 覆盖 Terminal Pool Page、Workbench Tree Page、copy-history 和 `120x40` visual review baseline，`termx v3 e2e-smoke` 覆盖默认 attach 装配、viewport、resize、content rect terminal resize、copy rebind 和 pane command。
- 切片 88 已完成二轮视觉重绘和自动准入，但当前 TUI 仍不能宣称达到目标截图；切片 89 必须在真实默认入口中复核。
- pane split、close、focus、zoom、resize、set size、balance、presentation 已有统一 semantic command 基础，快捷键、鼠标、测试和 CLI mini command 只能作为 adapter。
- floating pane 已使用独立 styled bordered chrome，具备 reducer-owned state、z-order、active 状态、keyboard create/move/resize/center/collapse/close、mouse raise/close、标题栏连续拖动移动、resize handle 连续拖动 resize 和 content rect 裁切。
- `Ctrl-p` pane mode、`Ctrl-r` resize mode、`Ctrl-g` global mode 已作为第一版键盘产品入口落地，footer 能显示当前 mode。
- `termx v3 smoke` 和 `termx v3 e2e-smoke` 已覆盖 styled frame、pane command feedback、行宽恒等、content rect terminal resize 和 copy rebind。
- terminal-live 内容 renderer 一期必须在当前 styled chrome 基线上工作：真实 live 行只进入 pane content slot，基础 ANSI SGR 映射为 semantic style token，live cursor 表达为 content-local cursor，pending、empty、exited 状态显示在所属 pane 内，emoji/CJK/combining mark 裁切不得破坏 pane 边框。
- copy-history 内容 renderer 已从一期推进到深化阶段，必须继续只消费 core-v2 authoritative `HistoryWindow`：历史行显示 logical-line、continuation 和 clipped marker，selection 与 active match 使用 styled cell 表达，copy cursor 是 content-local cursor，顶部 search row、底部 scrollbar/status、滚动、match navigation、content-local mouse selection 和 position token 都在 content rect 内工作；resize 或 content cols 变化后仍重新绑定 authoritative window，不显示旧 cols rows。
- empty/exited/Terminal Picker 内容 renderer 一期已把旧 placeholder 推进为可操作内容：empty pane 显示 attach/create/manager/close CTA，exited pane 显示 last state 与 restart/reconnect/close CTA，Terminal Picker overlay 显示 search、当前 workspace terminal list、selected row、new terminal row 和 action hit region。
- Terminal Pool 数据源与 Picker 服务接线一期已完成：Terminal Picker 可以请求 terminal list，展示 loading/empty/error，把 pool row 与当前 workspace pane row 合并去重，并把 attach/create/restart/reconnect 接到 service result 反馈。
- Terminal Pool 管理页一期已完成：独立页面、搜索、列表、selected row、detail、metadata、preview 摘要、Attach/Edit/Kill action、键盘/鼠标操作、service/effect/result 反馈、常规 viewport 下关键内容可见和 no terminal input leak 已落地。
- Prompt / Help overlay 一期已完成：Prompt 具备 title/context/input/submit/cancel/destructive confirm 边界，Help 可按 Most used、Pane、Tab、Workspace、Floating、Terminal Pool、Display/Copy 展示概念和动作，键盘、鼠标 close、overlay cursor 和 no terminal input leak 已接入。
- Tab / Workspace 产品入口一期已完成：`Ctrl-t` 和 `Ctrl-w` 已进入 reducer-owned interaction mode，支持 tab create/switch/rename/close、workspace create/switch/rename 和 Workbench Tree 入口；rename 走同一个 Prompt 提交流程，header 展示 tab strip，footer 展示 mode-specific hints，输入不漏发到底层 terminal。
- TUI 产品壳总验收已完成的是第一版功能闭环：除 terminal-live/copy-history 的深层内容体验仍可继续深化外，header/footer、pane、floating、Terminal Pool、Workbench Tree、Prompt/Help、Tab/Workspace、toast、overlay、快捷键、鼠标和 no terminal input leak 已可基本操作。该结论不表示视觉已经达到用户截图目标。

当前未完成但产品要求仍保留：

- 视觉对齐返工：top bar、bottom bar、pane chrome、split line、toast、overlay、floating、copy-history 和 Terminal Pool/Workbench Tree 内容层已有固定 smoke 回归证据，切片 88 已完成二轮 shell/pane 重绘；后续必须按 `workflow.md` 切片 89 做真实默认入口截图级验收。
- terminal-live 内容 renderer 深化：selection/search、content-local hit region、状态 metadata、复杂 SGR/truecolor、终端模式 token、clipped markers 和 richer terminal cell attributes。
- copy-history 最终 polish：logical-line 拼接提示、跨 logical-line selection affordance、窄屏退化和最终视觉层级。
- Terminal Pool 深化：跨 workspace terminal source、attach as tab、attach as floating、metadata edit 业务表单接线、kill confirm 和更完整 preview。
- Prompt/Help 深化：命令面板、Help 搜索/分页、Prompt input click 光标定位和更多真实业务命令执行。
- Floating Overview overlay。
- attach as floating 和 Floating Overview。
- 多层 split、复杂 pane 管理和 resize affordance 的产品化。
- tab/workspace 深化：workspace delete、tab reorder、鼠标点击 tab strip、跨 workspace terminal attach 和更完整的 rename/confirm 策略；当前已落地的 pane/resize/global/floating/tab/workspace 第一版入口不得被局部 handler 分叉实现。

## 21. 当前 UI Framework 产品化验收线

当前 UI framework 产品化验收线已经完成。后续 terminal-live 内容 renderer 必须以这条产品壳为前提继续深化，而不是绕过 shell、pane command、layout measurement 或 styled chrome。

该验收线的目标：

- 用户能通过键盘完成 pane split、close、focus、zoom、resize、balance、card/split presentation 切换。
- 用户能通过鼠标点击 pane content 或 pane chrome 聚焦 pane。
- 用户能通过鼠标点击 pane action 执行同一套 pane semantic command。
- 用户能通过鼠标命中 resize handle 或 split divider 进入 resize 语义或执行 resize command。
- 用户能隐藏和恢复 header/footer，并看到 body 空间真实回收。
- 用户能看到 mode-specific footer hints，知道当前 mode 下可执行什么动作。
- 用户能关闭当前 toast 或清空 toast，并看到 toast 按真实 runtime timer 自动消失且不改变 pane layout。
- 每次结构操作后，active pane border/title、footer active target、content rect terminal resize 和 copy mode rebind 必须同步发生；toast feedback 只用于离散结果、错误或确认，不用于连续拖动和普通 focus 成功。

这条验收线不要求：

- terminal-live 内容已经具备完整 styled cell、cursor、selection 或 search。
- copy-history 内容已经具备最终 visual polish、完整 logical-line 拼接提示或跨 logical-line selection affordance。
- Terminal Picker 已经接入完整跨 workspace 过滤或 Terminal Pool 管理页能力。
- Terminal Pool、Workbench Tree、floating pane、Prompt、Help 已经完整产品化。

基本手工测试入口：

- `go run ./cmd/termx v3 smoke`：查看静态 smoke 中 styled chrome、header/footer、card/split、toast、overlay 和 pane command case。
- `go run ./cmd/termx v3 e2e-smoke`：查看端到端 smoke 中 split、resize、zoom、close、content rect resize 和 copy rebind。
- 真实 TUI 中 `Ctrl-p` 进入 pane mode，执行 split、close、focus、zoom、balance、card/split。
- 真实 TUI 中 `Ctrl-r` 进入 resize mode，使用方向键调整 pane size。
- 真实 TUI 中 `Ctrl-g` 进入 global mode，确认 footer 展示 header/footer、Help、Terminal Pool、Workbench Tree、quit 等核心入口；toast 维护快捷键不进入主 footer / Help 路径。
- 鼠标点击 pane 内容区或 pane chrome 时，active pane 必须切换并立即高亮。

推荐真实 TUI 验收步骤：

- 启动：`go run ./cmd/termx`。
- 分屏：点击 pane 顶部的 split right / split down action token，或通过测试、smoke harness、后续 CLI mini command / command palette 发起同一 semantic command。新 pane 应立即成为 active pane，边框变为 accent，footer active target 同步更新。横向分屏后，下方 pane 顶边 action token 必须仍可点击分屏，不能被 divider resize 命中抢占。`Ctrl-p v`、`Ctrl-p s`、`Ctrl-p %`、`Ctrl-p "` 不作为分屏键盘入口。
- 焦点：在 pane mode 中按 `n` / `N` 切换焦点，或鼠标点击另一个 pane 的内容区 / chrome；active pane 边框、标题和 footer 必须同步变化；普通 focus 成功不应额外弹出 toast。
- 关闭：先聚焦目标 pane，在 pane mode 中按 `x` 关闭 pane，或点击 pane 顶部 action slot；关闭后 active pane 必须稳定落到仍存在的 pane，不得留下已删除 pane 的高亮或 footer target。
- resize：按 `Ctrl-r` 进入 resize mode，使用方向键或 `h` / `j` / `k` / `l` 调整 active pane；也可以用鼠标按住 split divider 并拖动。拖动某条 divider 时只能调整视觉相邻 pane 对：例如四列布局中拖第 2 列左边线向左，只允许第 1 列变小、第 2 列变大，第 3/4 列不变；拖第 2 列右边线向右，只允许第 2 列变大、第 3 列变小，第 4 列不变；pane 尺寸、content rect terminal resize 和 active 高亮必须同步变化，拖动事件不得漏发给 terminal，也不得连续刷出 toast。
- zoom：在 pane mode 中按 `z` zoom / unzoom；zoom 后只显示目标 pane，unzoom 后恢复 split layout，footer 和 toast 必须显示对应反馈。
- card/split：在 pane mode 中按 `c` 切到 card panel，按 `p` 切到 split line；presentation 变化不得改变 pane id、terminal binding、active pane 或 copy mode 语义。
- header/footer：按 `Ctrl-g` 进入 global mode，按 `h` 隐藏/恢复 header，按 `f` 隐藏/恢复 footer；隐藏后 body 必须回收空间，pane frame 仍填满 viewport。
- toast：维护快捷键仍可关闭或清空 toast；同内容 toast 会去重并刷新生命周期，普通反馈会按真实 runtime timer 自动消失，pending 或错误反馈保留更久但也有明确生命周期；toast 不得改变 pane layout，也不得把操作绕过 shell message。
- 中文输入法：真实 TUI 中 host cursor 默认隐藏；FrameSink 必须在写帧前隐藏 cursor，并在写帧后把隐藏 host cursor 停在全局 cursor rect。有 pane、floating、overlay、Prompt、live cursor、empty/exited pane fallback cursor 时都必须有稳定 anchor。切到中文输入法输入拼音字母时，预编辑文本不应出现在窗口底部并顶起整个界面；创建 active floating 后输入中文候选时，候选区应跟随 floating 内容区。
- copy rebind：进入 copy mode 后，执行 resize、header/footer hide 或 pane size change；历史窗口必须按新的 content cols 重新请求 authoritative window，不得显示旧 cols 的历史。

如果上述基本操作出现回归，应先修复 UI framework 产品壳，再继续 terminal-live、copy-history 或 Terminal Pool 等内容深化。否则真实 terminal 内容会掩盖 chrome、hit region、layout/effect 同步问题。

## 22. Terminal-Live 内容 Renderer 一期验收线

terminal-live 内容 renderer 一期的目标是把真实 terminal live 内容接入现有 render framework，而不是重写 renderer 主路径。

一期必须满足：

- 只消费 `TerminalSurfaceStore` / terminal session 的实时投影，不读取 core client，不请求 history window。
- 只绘制到 active pane 的 content rect 内；不得覆盖 pane border、split line、header/footer、toast 或 overlay。
- raw live 行中的基础 ANSI SGR 被转换为 semantic style token，真实 TTY 通过 `Frame.ANSILines` 输出 styled frame；plain snapshot 不包含 raw ANSI 控制序列。
- live cursor 作为 content-local cursor 输出；没有精确 cursor 时可使用 live 行尾作为保守 fallback，但不得使用 copy mode cursor 或 history cursor 伪装。
- live surface 未到达时显示 pending；已到达但无行时显示 empty；active pane 是 exited/empty 时显示对应 pane 状态，不用 live 行覆盖。
- emoji、CJK、combining mark 和 ANSI styled text 必须按 terminal cell width 裁切，resize 后仍保持 frame 行宽等于 viewport cols。
- copy mode 仍只消费 authoritative `HistoryWindow`，不得从 live surface fallback。

一期不要求：

- 完整 terminal emulator style model。
- truecolor、underline、reverse、link、OSC metadata 的完整还原。
- selection/search、content-local mouse hit region、scrollbar 或 clipped marker。
- 从 protocol 扩展精确 styled cell stream；当前可先基于 raw live 行和基础 SGR 做等价投影。

## 23. Copy-History 内容 Renderer 一期验收线

copy-history 内容 renderer 一期的目标是把 authoritative `HistoryWindow` 变成可浏览、可选择、可复制且不会破坏 chrome 的内容视图。

一期必须满足：

- 只消费 `HistoryStore + CopyModeStore`，并且只有 terminal id、bound token、bound cols 与 authoritative window 一致时才渲染历史内容。
- 缺 authoritative window、绑定不一致或 resize 后等待新 window 时，只显示 pending、empty 或 error；不得从 live surface、snapshot、grid viewport 或 local VTerm scrollback fallback。
- 每个 visual row 显示 logical-line marker：新 logical line、continuation row、clipped before、clipped after 必须可见。
- copy selection 显示为 styled cells，跨 visual row 时仍按 authoritative row 投影工作。
- copy cursor 是 content-local cursor，必须考虑左侧 marker 宽度，不得复用 live cursor。
- status/footer hint 必须包含当前 row、总 row、line id 和 cols 摘要，方便用户确认当前位置。
- copy/yank 成功必须有 toast 反馈；clipboard IO 仍通过 effect/result message，不由 renderer 直接执行。
- resize、header/footer hide 或 pane size change 导致 copy content cols 改变时，旧 window、selection、cursor 必须失效并重新请求 authoritative latest window。
- emoji、CJK、combining mark 和 styled selection 必须按 terminal cell width 裁切，不得破坏 pane border 或 split line。

一期不要求：

- 完整 scrollbar。
- 搜索和过滤。
- content-local mouse selection。
- logical-line 级拼接提示的最终视觉 polish。

## 23.1 Copy-History 内容 Renderer 深化验收线

copy-history 内容 renderer 深化的目标是把一期内容视图推进到可搜索、可滚动、可用鼠标定位且仍严格依赖 authoritative `HistoryWindow` 的产品体验。

深化阶段必须满足：

- 顶部显示 search row；用户在 copy mode 中输入字符会更新 query，Backspace 删除 query，Enter 或有 query 时的上下方向键在匹配之间移动。
- 匹配结果必须来自当前 authoritative rows；active match 会移动 copy cursor 并保证 cursor 进入 viewport。
- PageDown 和鼠标滚轮可以滚动 copy viewport；底部 status/scrollbar 显示当前可见范围、总 row、row/line/part/cols/span/search/older 摘要。
- selection 与 active match 有不同样式层级；selection 仍按 authoritative row/col 投影，不从 visual text 反推历史。
- 鼠标点击 copy history row 会把 cursor/selection 移到对应 authoritative row；该事件不得漏发为 terminal input。
- content 高度变化只更新可见行数并夹紧 viewport；content 宽度变化必须重新绑定 authoritative window。
- search row、history rows、scrollbar/status 都必须被 content rect 裁切，不得覆盖 pane border、header/footer、toast 或 overlay。

深化阶段仍不宣称完成：

- logical-line 拼接提示的最终视觉 polish。
- 跨 logical-line selection affordance 的最终形态。
- 搜索高亮之外的高级过滤、正则搜索或搜索历史。
- 复制内容的复杂格式化策略。

## 24. Empty / Exited / Terminal Picker 内容 Renderer 一期验收线

empty/exited/Terminal Picker 内容 renderer 一期的目标是把旧 placeholder 变成用户可识别、可点击且不会漏发到底层 terminal 的产品内容。

一期必须满足：

- empty pane 只消费当前 `PaneState`，显示 pane title、empty 状态和 attach/create/manager/close CTA。
- exited pane 只消费当前 `PaneState`，显示 pane title、last state 或 terminal id，以及 restart/reconnect/close CTA。
- Terminal Picker 只消费 reducer-owned `ShellStore`、当前 workspace panes、active session/surface/history terminal id 和 overlay query；不得伪造 Terminal Pool store。
- Terminal Picker overlay 必须显示 search row、第一行默认选中的 `+ new terminal` row 和当前 workspace terminal list；不得显示 Terminal Pool 式 detail/preview 管理块。
- Terminal Picker 搜索框 cursor 归属于 overlay content；overlay 打开时 cursor 不得继续落到 pane 内容。
- content action hit region 必须先于 broad pane content 或 overlay background 命中，点击 CTA 或 picker row 不得漏发到底层 terminal。
- picker row 点击可以执行当前已可证明的 pane focus / pool attach / create request；Terminal Pool 完整管理动作不得塞进 picker。
- 所有文本必须按 terminal cell width 裁切，emoji、CJK 和 combining mark 不得破坏 pane border、overlay border 或整行宽度。

一期不要求：

- 完整 Terminal Pool。
- 跨 workspace terminal source。
- 真实 create/restart/reconnect 服务接线。
- 搜索过滤和键盘移动选择。
- preview/detail panel。

## 25. Terminal Picker 真实交互深化验收线

Terminal Picker 真实交互深化的目标是把一期静态列表推进为可键盘操作、可过滤、可确认选择且不会漏发到底层 terminal 的 overlay。

本阶段必须满足：

- `Ctrl-f` 打开 Terminal Picker 后，普通字符输入进入 picker query，不得继续发送到 terminal input。
- Backspace 修改 picker query，`Esc` 关闭 overlay，关闭时不得产生 terminal input。
- Terminal Picker 行来源仍只允许是 reducer-owned 当前 workspace panes、现有 session/surface/history terminal id 与 reducer-owned Terminal Pool store；不得由 renderer 直接读服务。
- query 必须能过滤 title、pane id、terminal id 和 pane kind；过滤后 selected row 回到第一项。
- 上下方向键移动 selected row，并且在列表内循环；selected row 必须有明确高亮。
- `Enter` 确认 selected row 后，当前可证明的行为是 focus 对应 pane、关闭 overlay 并显示 toast 反馈。
- 点击 picker row 与 `Enter` 使用同一 attach/focus/close overlay 语义；不得写第二套路由。
- picker 只显示 compact search/list rows；行内可显示短 terminal id、title、state 和 `@location/source`，但不得显示 `DETAIL/TARGET/TERMINAL` 大块、target、selected hint 或内容区 `[attach]/[new]` 按钮。
- `+ new terminal` row 必须通过 terminal service create effect/result 接线；result 到达前不得伪造 terminal 生命周期。
- overlay cursor、列表行和 row hit region 必须按 terminal cell width 裁切，emoji、CJK、combining mark 和 styled cell 不得破坏 overlay border 或整行宽度。

本阶段不要求：

- 跨 workspace terminal source。
- 完整 Terminal Pool 管理页。
- Terminal Pool preview 面板或详情编辑。

## 26. Terminal Pool 数据源与 Picker 服务接线一期验收线

Terminal Pool 数据源与 Picker 服务接线一期的目标是让 Terminal Picker 可以消费真实 terminal list，并把 attach/create/restart/reconnect 作为 service/effect/result message 表达，而不是继续显示未实现 toast。

本阶段必须满足：

- 打开 Terminal Picker 可以触发 terminal list service request；list 结果必须先进入 reducer-owned `TerminalPoolStore`，renderer 不直接读取 service。
- Picker row 必须合并当前 workspace panes 与 Terminal Pool items，并按 terminal id 去重；已有 pane 绑定的 terminal 不应重复出现 pool row。
- Terminal Pool list loading、empty、error 必须在 overlay 内容中可见。
- Query 必须同时过滤 pane row 和 pool row 的 title、pane id、terminal id、kind/state。
- 对 pane row 执行 attach 仍然只做 focus pane / close overlay；对 pool row 执行 attach 必须通过 terminal service attach result 更新 session/surface，并显示 toast。
- `new terminal` 必须通过 terminal service create result 反馈，并触发 list refresh；result 到达前不得伪造 terminal 生命周期。
- restart / reconnect 必须存在 service/effect/result 边界和失败反馈，不得由 renderer 或 service 直接改 state。
- stale list result 必须被拒绝；失败必须进入 reducer-owned error/toast。
- 所有键盘和鼠标路径仍不得漏发到底层 terminal input。

本阶段不要求：

- 完整 Terminal Pool 管理页。
- 跨 workspace 或跨 remote terminal 管理。
- metadata edit、kill/remove UI。
- Terminal Pool detail/preview 面板的最终产品视觉。

## 27. Terminal Pool 管理页一期验收线

Terminal Pool 管理页一期的目标是实现独立 Terminal Pool page/content，让用户能在一个稳定页面中查看、搜索、选择和管理 terminal。

本阶段必须满足：

- 页面可以从全局入口或 empty pane manager action 打开，并可以用 `Esc` 关闭。
- 页面打开时触发 terminal list loading，list 结果进入页面状态后展示。
- loading、empty、error 三种列表状态必须清晰显示在页面内。
- search query 过滤 terminal id、title、state、cwd 或 metadata 摘要；普通字符和 Backspace 不得漏发给底层 terminal。
- 上下方向键移动 selected row；过滤后 selected row 必须稳定落在第一条可见 row 或 empty 状态。
- 鼠标点击 row 与键盘选择使用同一 selected row 语义。
- detail 区显示 selected terminal 的 id、title、state、cwd、size、attached/bound 摘要和 metadata 摘要。
- preview 区允许先显示 summary / last known live preview / pending placeholder，但必须在页面内，不得覆盖 chrome。
- Attach Here 执行后必须反馈结果，并把成功 attach 反映到当前 active pane/session。
- Edit 执行后必须有明确反馈；一期可以只接 metadata edit service 边界或 Prompt stub，但不得静默成功。
- Kill 执行后必须有明确 danger 反馈；结果到达前不得从本地列表伪造 terminal 已退出或已删除。
- 所有 action 结果必须通过 toast 或页面内状态反馈。
- 页面内容、row、detail、preview、footer action 必须按 terminal cell width 裁切，emoji、CJK、combining mark 和 ANSI styled text 不得破坏页面边框或整行宽度。
- 常规 viewport 中必须能同时看到 list、selected detail、preview 摘要和 Attach/Edit/Kill action；如果高度不足，必须按产品优先级压缩内容，而不是直接裁掉 action 或 detail。
- 鼠标 action hit region、键盘 Enter、empty manager action 和 global mode 入口必须最终进入同一 app message / reducer / effect 边界，不能各自实现局部逻辑。
- 页面打开期间，普通字符、Backspace、方向键、Enter、footer action click 和 row click 都不得漏发到底层 terminal。

本阶段不要求：

- Workbench Tree。
- floating pane 管理。
- remote terminal 管理。
- attach as tab / attach as floating 的完整闭环。
- metadata edit 的最终表单体验。
- kill destructive confirm 的最终交互样式。
- 完整 terminal emulator preview。

本阶段完成后的基本手工测试入口：

- `go run ./cmd/termx` 进入默认 TUI。
- 按 `Ctrl-g` 进入 global mode，再按 Terminal Pool 对应入口键打开 Terminal Pool Page。
- 或在 empty pane 中点击 manager action 打开 Terminal Pool Page。
- 在页面中输入搜索词，确认 terminal 不收到这些字符。
- 使用上下方向键移动 selected row，按 `Enter` attach 到当前 pane。
- 点击 row、Attach Here、Edit、Kill，确认都有可见反馈。
- 按 `Esc` 关闭页面并回到 workbench。

## 28. Workbench Tree overlay 一期验收线

Workbench Tree overlay 一期的目标是实现 workspace / tab / pane / floating 的结构导航层，让用户能搜索当前 workbench 结构、选择 row，并把焦点/open 操作回投到当前 workbench。

本阶段必须满足：

- 可以从全局入口打开，并可以用 `Esc` 关闭。
- overlay 采用页面级实体卡片，不是 Terminal Picker 小弹层；打开期间 cursor 归属于 search field，不复用底层 pane cursor。
- row 至少包含 workspace、tab、pane、floating 四类结构。floating row 必须能展示当前 floating 数量或具体 floating 摘要；Workbench Tree 不直接实现 floating move/resize。
- search query 过滤 workspace 名、tab 标题、pane 标题、pane id、terminal id、pane kind 和 summary；普通字符和 Backspace 不得漏发给底层 terminal。
- 上下方向键移动 selected row；过滤后 selected row 必须稳定落在第一条可见 row 或 empty 状态。
- 鼠标点击 row 与键盘选择使用同一 selected row 语义。
- detail/preview 区展示 selected row 的结构路径、类型、active 状态、terminal binding 或 floating 摘要。
- pane row 的 open/focus action 必须聚焦对应 pane、关闭 overlay 并显示 toast 反馈。
- tab row 的 open/focus action 必须聚焦该 tab 的 active pane 或第一个 pane、关闭 overlay 并显示 toast 反馈。
- workspace row 和 floating row 可以只显示结构反馈 toast，不得伪造 workspace/floating 生命周期。
- row、detail、preview、action 和 cursor 必须按 terminal cell width 裁切，emoji、CJK、combining mark 和 ANSI styled text 不得破坏 overlay 边框或整行宽度。
- 鼠标 row/action hit region、键盘 Enter 和全局入口必须最终进入同一 app message / reducer 边界，不能各自实现局部逻辑。
- overlay 打开期间，普通字符、Backspace、方向键、Enter、row click 和 action click 都不得漏发到底层 terminal。

本阶段不要求：

- Terminal Pool 数据混入 Workbench Tree。
- floating pane 创建、拖拽、resize、z-order 管理。
- remote workspace 或 remote terminal 管理。
- workspace/tab rename/create/delete 的最终入口。

本阶段完成后的基本手工测试入口：

- `go run ./cmd/termx` 进入默认 TUI。
- 按 `Ctrl-g` 进入 global mode，再按 Workbench Tree 对应入口键打开结构导航。
- 在页面中输入中文或 emoji 搜索词，确认 terminal 不收到这些字符。
- 使用上下方向键移动 selected row，按 `Enter` 聚焦 pane 或 tab。
- 点击 row 和 Open / Focus action，确认都有可见反馈。
- 按 `Esc` 关闭页面并回到 workbench。

## 29. Floating Pane 一期验收线

Floating Pane 一期的目标是让 floating pane 成为可操作的 workbench 结构对象，而不是只在 Workbench Tree 中显示一个占位摘要。

本阶段必须满足：

- floating pane state 归 reducer-owned shell 管理，至少包含 id、title、pane content、rect、z-order、active 和 collapsed。
- `Ctrl-o` 进入 floating mode 后，`n` 创建 floating pane，方向键或 `h/j/k/l` 移动 active floating，`H/J/K/L` 调整 active floating 尺寸，`c` 居中，`z` collapse / restore，`x` 关闭。
- floating pane 必须有独立 styled bordered chrome，active 使用 accent，inactive 使用 muted，且不受 tiled pane card / split line 呈现模式影响。
- active floating pane 存在时，后方 tiled pane 的 active chrome 必须降级为 inactive/muted；关闭或失焦 floating 后，tiled pane active chrome 恢复。
- floating pane 必须位于 tiled pane 之上、overlay / toast 之下；toast 或 overlay 遮挡 floating 时鼠标不得穿透到底层 floating。
- mouse click floating title 或内容区域必须 focus / raise 该 floating pane。
- mouse click floating close action 必须关闭该 floating pane。
- mouse drag floating title 必须进入同一 floating move 语义；mouse drag floating 右下 resize handle 必须进入同一 floating resize 语义。
- floating content 必须复用已有 content renderer，只能绘制在 floating content rect 内，不得覆盖 floating border、tiled pane、header/footer、overlay 或 toast。
- move / resize 必须 clamp 到当前 viewport 内，并保留最小宽高。
- Workbench Tree 的 floating row 必须能反映当前 floating 数量或结构摘要。
- 所有标题、content、toast 和边框必须继续按 terminal cell width 裁切，emoji、CJK、combining mark 和 ANSI styled text 不得破坏 floating 边框或整行宽度。

本阶段不要求：

- attach as floating 的真实 terminal lifecycle。
- Floating Overview overlay。
- remote floating 管理。

本阶段完成后的基本手工测试入口：

- `go run ./cmd/termx` 进入默认 TUI。
- 按 `Ctrl-o` 进入 floating mode，再按 `n` 创建 floating pane；页面应出现独立带边框 floating，footer mode 显示 floating。
- 在 floating mode 中按方向键移动，按 `H/J/K/L` resize，按 `c` 居中，按 `z` collapse / restore，按 `x` close。
- 用鼠标点击 floating title 或内容区域，floating 必须成为 active 并提升到最前。
- 用鼠标按住 floating title 并拖动，floating 位置必须变化且 clamp 在 viewport 内。
- 用鼠标按住 floating 右下角 resize handle 并拖动，floating 尺寸必须变化且保持最小尺寸。
- 用鼠标点击 floating 顶部 close action，floating 必须关闭。
- 当右上角 toast 遮挡 floating 时，点击 toast 区域只处理 toast，不得穿透并操作 floating。

## 30. Prompt / Help Overlay 一期验收线

Prompt / Help overlay 一期的目标是补齐全局短输入和帮助入口，让 UI 产品壳具备可操作的基础对话层。

本阶段必须满足：

- Prompt state 归 reducer-owned shell 管理，至少包含 title、context、input value、placeholder、submit/cancel 状态和 destructive confirm 边界。
- Prompt 打开期间普通字符、Backspace 和 Enter 都只作用于 Prompt，不得漏发到底层 terminal。
- Prompt 提交必须通过 shell message / reducer 路径反馈；本阶段可以显示 toast 或状态，不直接执行业务 IO。
- destructive Prompt 在确认文本不匹配时必须保持打开并给出 warning feedback，不得误提交。
- Prompt cancel、Esc 和鼠标 cancel action 必须能关闭 overlay，不得改变 terminal lifecycle。
- Help 必须按 Most used、Pane、Tab、Workspace、Floating、Terminal Pool、Display/Copy 等分类展示概念和动作，而不是只堆快捷键。
- Help 打开期间普通输入不得漏发到底层 terminal；Enter、Esc 或 mouse close 可以关闭。
- Prompt / Help 都必须使用 styled overlay chrome，内容和 cursor 必须按 terminal cell width 裁切，emoji、CJK、combining mark 和 ANSI styled text 不得破坏边框或整行宽度。
- footer 在 Prompt / Help 打开时必须显示 mode-specific hint，用户能看到 submit/cancel 或 close 行为。
- `termx v3 smoke` 必须覆盖 Prompt / Help overlay，避免后续退回为只存在状态但默认渲染不可见。

本阶段不要求：

- 命令面板和命令执行器。
- 多字段表单。
- Prompt input click 精确移动光标。
- Help 搜索、分页或分类折叠；可配置 shortcuts 的输入与展示同源已在后续快捷键切片完成。
- metadata edit、kill confirm 等更多业务动作全部接入 Prompt；这些由后续 Terminal Pool 深化切片接入。

本阶段完成后的基本手工测试入口：

- `go run ./cmd/termx` 进入默认 TUI。
- 按 `Ctrl-g` 进入 global mode，再按 `:` 打开 Prompt；输入中文或 emoji，确认 terminal 不收到这些字符。
- 在 Prompt 中按 Backspace 和 Enter，确认 input 编辑、提交反馈和 overlay 关闭正常。
- 打开 destructive Prompt 的测试入口或 harness，输入非确认文本时应保持打开并显示 warning feedback。
- 按 `Ctrl-g` 进入 global mode，再按 `?` 打开 Help；确认能看到 Most used、Pane、Tab、Workspace、Floating、Terminal Pool 和 Display/Copy 分类。
- 在 Help 中按 Enter、Esc 或点击 close action，确认 overlay 关闭且底层 terminal 不收到输入。

## 31. Tab / Workspace 产品入口一期验收线

Tab / Workspace 产品入口一期的目标是让 `Ctrl-t` 和 `Ctrl-w` 从声明入口变成可操作的 workbench 结构入口。

本阶段必须满足：

- Tab 和 Workspace 操作必须通过 reducer-owned workbench command 或 shell message，不得在快捷键 handler 里直接临时改字段。
- `Ctrl-t` 进入 tab mode，footer 显示 tab-specific hints。
- tab mode 至少支持 create、next/previous switch、rename 和 close。
- tab create 必须创建新 tab、切换 active tab、生成默认 pane，并让 header tab strip 和 active pane 同步更新。
- tab switch 必须更新 active tab、active pane、header/footer 和 content rect，不得改变 terminal lifecycle。
- tab rename 必须走同一个 Prompt overlay 提交流程，提交后通过 workbench command 更新 tab title。
- tab close 必须拒绝关闭最后一个 tab；关闭成功后 active tab 和 active pane 必须稳定落到仍存在的 tab。
- `Ctrl-w` 进入 workspace mode，footer 显示 workspace-specific hints。
- workspace mode 至少支持 create、next/previous switch、rename 和 Workbench Tree 入口。
- workspace create 必须创建 reducer-owned workspace、切换 active workspace、生成默认 tab/pane，并让 header workspace、tab strip 和 footer summary 同步更新。
- workspace switch 必须保留原 workspace 的 tab/pane 状态，切回后能恢复原 active tab 和 tab title。
- workspace rename 必须走同一个 Prompt overlay 提交流程，提交后通过 workbench command 更新 workspace name。
- tab/workspace mode 下普通结构按键、Prompt 输入、Enter 和 Esc 不得漏发到底层 terminal。
- `termx v3 smoke` 必须覆盖 tab/workspace case，证明默认 render 路径可见。

本阶段不要求：

- workspace delete。
- tab reorder。
- 鼠标点击 tab strip 创建、切换或关闭 tab。
- 跨 workspace terminal attach。
- session persist / restore 的完整 workspace/tab 树。

本阶段完成后的基本手工测试入口：

- `go run ./cmd/termx` 进入默认 TUI。
- 按 `Ctrl-t` 进入 tab mode，再按 `n` 创建 tab；header tab strip 应显示新 active tab。
- 在 tab mode 中按 `h` / `l` 或 `[` / `]` 切换 tab；active pane、header 和 footer 必须同步变化。
- 在 tab mode 中按 `r` 打开 Prompt，输入中文或 emoji 后提交；tab title 必须更新且 terminal 不收到这些字符。
- 在 tab mode 中按 `x` 关闭当前 tab；关闭最后一个 tab 时必须显示 warning feedback，不得破坏 workbench。
- 按 `Ctrl-w` 进入 workspace mode，再按 `n` 创建 workspace；header workspace 名称和 footer summary 必须同步更新。
- 在 workspace mode 中按 `h` / `l` 或 `[` / `]` 切换 workspace；切回原 workspace 后 tab/pane 状态必须恢复。
- 在 workspace mode 中按 `r` 打开 Prompt，输入中文或 emoji 后提交；workspace name 必须更新。
- 在 workspace mode 中按 `t` 打开 Workbench Tree；关闭后仍回到稳定 workbench。

## 32. TUI 产品壳总验收线

TUI 产品壳总验收的目标是确认当前 goal 完成后，除 terminal-live/copy-history 的深层内容体验仍可继续深化外，默认 TUI 的界面、壳层功能和基础交互已经是可基本操作的产品闭环。

本验收线已经完成，当前可用能力包括：

- header/footer：显示 workspace、tab、mode、active pane、terminal/floating 摘要和 mode-specific hints；可隐藏并真实回收 body 空间。
- pane：支持 split right/down、close、focus、zoom/unzoom、resize、balance、card/split presentation；split right/down 只通过 pane chrome、鼠标或 semantic command 入口触发，其余 pane mode 键盘入口走统一 semantic command。
- floating：支持创建、移动、resize、居中、collapse/restore、close、鼠标 raise、标题栏拖动移动、resize handle 拖动 resize 和 close，并始终使用独立 styled bordered chrome。
- Terminal Pool：支持全局页面打开、搜索、列表、selected row、detail/preview、Attach/Edit/Kill action、service/effect/result 反馈和 no terminal input leak。
- Workbench Tree：支持 workspace/tab/pane/floating 结构导航、搜索、selected row、Open/Focus action、键盘/鼠标操作和 no terminal input leak。
- Prompt/Help：Prompt 支持短输入、提交、取消和 destructive confirm 边界；Help 展示 Most used、Pane、Tab、Workspace、Floating、Terminal Pool、Display/Copy 分类。
- Tab/Workspace：`Ctrl-t` 支持 tab create/switch/rename/close；`Ctrl-w` 支持 workspace create/switch/rename 和 Workbench Tree 入口；rename 走同一 Prompt。
- toast/overlay：toast 可 close current / clear all，overlay 可关闭且拥有自己的 cursor；toast 和 overlay 命中优先级高于底层 pane/terminal。
- 鼠标：pane content/chrome focus、pane action split/close、floating raise/move-drag/resize-drag/close、Terminal Pool row/action、Workbench Tree row/action、Prompt/Help close 均走最新 hit region。
- no terminal input leak：UI mode、overlay、Prompt、Terminal Pool、Workbench Tree、tab/workspace 等交互输入不会误发给底层 terminal。

本验收线完成后，terminal live 连接展示与交互前推也已经完成当前阶段：

- attach 后会从 core-v2 live snapshot 初始化 panel 内 live rows。
- 普通键盘输入只发送给 terminal service，不做 TUI 本地假回显；输入内容必须来自 live surface 回投后显示。
- pending、empty、attached、exited、error 状态都在所属 pane/footer 中表达，不退化成裸文本整屏。
- live 内容继续只绘制在 pane content slot 内，resize 使用 content rect，不使用外部 viewport 总尺寸。

本验收线完成后，copy-history content renderer 深化也已经完成当前阶段：

- copy mode 顶部 search row、底部 scrollbar/status 和 position token 已进入 content renderer。
- 用户可以通过键盘 query、Backspace、Enter、方向键、PageDown 和鼠标滚轮浏览 authoritative history。
- 鼠标点击 copy history row 可以移动 copy cursor/selection，且不会漏发到底层 terminal。
- 高度变化保留 authoritative window 并夹紧 viewport；宽度变化仍重新绑定 authoritative history window。

本验收线和 terminal live 前推仍不宣称已经完成：

- terminal live 的最终内容体验，例如 streaming event loop、truecolor、underline、reverse、link、复杂 terminal mode、selection/search、content-local hit region 和 richer terminal cell attributes。
- copy-history 的最终内容体验，例如 logical-line 拼接提示、跨 logical-line selection affordance、搜索历史、复杂复制格式和最终视觉 polish。
- Terminal Pool 的跨 workspace/remote 管理、attach as tab、attach as floating、metadata 表单和 kill confirm。
- floating 连续拖拽、attach as floating 和 Floating Overview。
- tab reorder、workspace delete、鼠标 tab strip 和完整 session persist/restore 树。

当前 goal 完成后的基本手工测试入口：

- 启动：`go run ./cmd/termx`。
- 分屏与 pane：分屏通过 pane 顶部 split right / split down action token 或 semantic command 入口触发；按 `Ctrl-p` 后用 `n` / `N` 切焦点，`z` zoom，`x` 关闭，`c` / `p` 切 card/split。
- resize：按 `Ctrl-r`，再用方向键或 `h/j/k/l` 调整 active pane；边框、footer、terminal content rect resize 必须同步。
- floating：按 `Ctrl-o`，再用 `n` 创建，方向键移动，`H/J/K/L` resize，`c` 居中，`z` collapse/restore，`x` 关闭；也可用鼠标拖动 floating title/resize handle，或点击 close。
- Terminal Pool：按 `Ctrl-g p` 打开，输入搜索词，使用上下键和 `Enter` attach；点击 row、Attach/Edit/Kill 必须有可见反馈，输入不得漏给 terminal。
- Workbench Tree：按 `Ctrl-g w` 打开，输入搜索词，使用上下键和 `Enter` 聚焦结构对象；点击 Open/Focus 必须有反馈。
- Prompt/Help：按 `Ctrl-g :` 打开 Prompt，输入中文或 emoji 并 `Enter` 提交；按 `Ctrl-g ?` 打开 Help，确认分类可见并可关闭。
- Tab/Workspace：按 `Ctrl-t` 后用 `n/h/l/r/x` 操作 tab；按 `Ctrl-w` 后用 `n/h/l/r/t` 操作 workspace 和 Workbench Tree。
- Toast/Header/Footer：按 `Ctrl-g h` / `Ctrl-g f` 隐藏或恢复 header/footer；toast close/clear 保留为维护快捷键，不作为 global footer 主路径。
- Copy History：按 `Ctrl-v` 进入 copy mode，输入搜索词，使用 `Enter` 或上下方向键在匹配间移动，使用 `PageDown` 或鼠标滚轮滚动，点击历史行时 cursor/selection 应移动且 terminal 不应收到鼠标事件。
- 非交互回归：`go run ./cmd/termx v3 smoke` 和 `go run ./cmd/termx v3 e2e-smoke`。

render 兼容投影清理与性能基线已经完成：旧 `RenderVM{Lines, Status}` 兼容输入语义已删除，large terminal output benchmark 已建立。后续如果要把项目往前推，应新增切片，并在不破坏上述产品壳、terminal live 前推、copy-history 深化和 `RenderResult` 单一路径的前提下，继续 terminal-live rich attributes、copy-history 最终 polish 或 render performance 优化。

## 33. 默认视觉复核结论

当前默认 TUI 已经可以通过上述交互入口完成基本操作。切片 83 和切片 87 的真实视觉复核结论均是未通过：界面仍不像用户给出的 `tuiv2` 目标截图。切片 88 已完成二轮视觉重绘，但不是最终截图级验收。

复核记录见 `docs/history/tui/default-tui-visual-review.md`。后续产品验收必须区分两个层级：

- 工程可运行：header/footer、pane、overlay、toast、floating、copy mode 和鼠标/键盘入口可操作。
- 截图级视觉完成：shell bar、pane chrome、active/inactive、toast、overlay、floating 和内容页视觉密度接近目标截图。

当前满足第一层级，并已完成第二层级的二轮重绘实现。第二层级是否真正通过，必须由 workflow 中的切片 89 真实默认入口截图级验收决定；如果仍不一致，需要新增后续视觉返工切片。

## 34. 后续讨论入口

后续讨论 render 架构时，应以本文档为产品基准。

允许讨论：

- 如何把上述页面映射到 `tui` 的状态、view-model 和 renderer。
- 哪些 `tuiv2` 的产品行为应保持。
- 哪些 `tuiv2` 的实现结构不能迁入。
- 如何分阶段实现 Workbench、Terminal Pool、Workbench Tree 和 Copy Mode。

不允许绕过本文档直接补临时线框，把默认入口显示成“看起来有东西”后就宣称完成。
