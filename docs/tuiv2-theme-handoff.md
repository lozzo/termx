# termx tuiv2 Theme / Color Handoff

状态：Working Notes  
日期：2026-04-05

## 1. 目的

这份文档用于把 tuiv2 当前的配色实现、视觉层级、交互 token、布局分区整理给后续负责视觉和配色的人。

目标不是在这里定最终视觉，而是说明：

- 现在 UI 的颜色从哪里来
- 哪些元素共用同一套 token
- 哪些元素应该被视为一组设计对象
- 改视觉时优先改哪里，哪些地方不要直接动

## 1.1 这份文档是给谁看的

这份文档的主要读者不是 Go 工程师，而是：

- 视觉设计师
- 配色负责人
- 会使用 Gemini 一类 Agent 工具产出设计规范的人

也就是说，这份文档的用途不是让对方改代码，而是让对方：

- 看懂当前界面有哪些区域和对象
- 知道每块界面现在大概是怎么表现的
- 在不理解 Go 的前提下，产出一份新的配色 / 视觉规范文档

后续流程是：

1. Gemini 或设计负责人根据这份文档，产出一份新的视觉 / 配色方案文档
2. 我再根据那份文档修改代码

## 1.2 Gemini Agent 需要做什么

如果这份文档被交给 Gemini 之类的 Agent，它的任务不是解释代码，而是产出一份可执行的设计文档。

Gemini 需要输出的内容应该包括：

- 整体视觉方向
- 语义色体系
- 顶栏 / 底栏 / pane / overlay / empty state / terminal pool 的分区视觉规则
- 主按钮 / 次按钮 / 危险按钮 / 状态标签的统一语言
- 图标和 token 的处理建议
- 如果要调整层级，也要用人话写清楚“谁比谁更强”

Gemini 不需要做的事情：

- 不需要写 Go 代码
- 不需要重写布局逻辑
- 不需要改交互逻辑
- 不需要给出具体函数修改方案

Gemini 最重要的产出不是“代码建议”，而是一份设计规范文档。

## 2. 当前设计原则

当前实现不是完全固定主题，而是“宿主终端主题 + UI 自己的强调层”。

### 2.1 两层颜色来源

第一层：宿主主题跟随

- 读取 host terminal 的默认前景色 / 背景色
- 读取 host terminal 的 palette
- 用这些颜色决定大部分 surface 背景、正文、弱分隔

第二层：semantic accent

- 专门服务提示、边框强调、按钮高亮、mode chip、危险动作
- 优先从宿主 palette 取亮色位
- palette 不够用时，退回 fallback accent
- 所有 accent 都会额外做对比度修正

这样做的原因是：

- 页面整体氛围继续跟着外部终端主题
- 但 panel 边框、提示、按钮不会跟着一起发灰
- 暗色主题下仍能保留“有主张的高亮层”

## 3. 核心代码入口

如果后续配色负责人不熟 Go，可以把下面这些文件和函数理解成“视觉开关面板”。

阅读方式：

- 文件名里有 `styles` 的，基本是“颜色和样式定义”
- 文件名里有 `frame` 的，基本是“页面上下框架”
- 文件名里有 `coordinator` 的，基本是“把很多块拼成最终界面”
- 文件名里有 `overlays` 的，基本是“弹层和卡片”
- 文件名里有 `hit_regions` 的，基本是“哪些文字或图标可点击”

最重要的入口如下：

- `tuiv2/render/styles.go`
  - `uiTheme`
  - `uiThemeForState`
  - `uiThemeFromHostColors`
  - 所有 lipgloss style helper
- `tuiv2/runtime/runtime.go`
  - `Visible()`
  - 把 `HostDefaultFG` / `HostDefaultBG` / `HostPalette` 暴露给 render 层
- `tuiv2/runtime/visible.go`
  - `VisibleRuntime`
- `tuiv2/render/frame.go`
  - 顶栏 / 底栏的渲染
- `tuiv2/render/hit_regions_workbench.go`
  - workspace token / tab token / top bar action token
- `tuiv2/render/coordinator.go`
  - pane frame
  - empty pane CTA
  - overlay 合成
- `tuiv2/render/overlays.go`
  - picker / prompt / workspace picker / help / terminal manager overlay
- `tuiv2/render/hit_regions_pane.go`
  - empty pane 的按钮定义
- `tuiv2/render/hit_regions_pane_chrome.go`
  - pane 顶边按钮和 icon token 定义
- `docs/tuiv2-chrome-layout-spec.md`
  - 现有 chrome 分区原则

### 3.1 给不懂 Go 的人看的“函数翻译表”

这一节是最重要的交接部分。  
可以把“函数”理解成“负责某块界面的绘制器”。

| 代码点 | 人话解释 | 它会在界面上产生什么效果 | 如果配色方案要改，应该给什么意见 |
| --- | --- | --- | --- |
| `uiThemeFromHostColors` | 主题总配色生成器 | 决定整套 UI token 从宿主终端颜色如何推导出来 | 提供“基础色、强调色、危险色、弱化色”的规则 |
| `workspaceLabelStyle` | workspace 名称块样式 | 左上角 workspace 标签的底色、前景色、粗细 | 指定 workspace 标签要更像 badge、pill，还是纯文本块 |
| `tabActiveStyle` / `tabInactiveStyle` | tab 样式 | 顶栏 tab 的活跃态和非活跃态 | 指定 active / inactive 的层级对比 |
| `tabCreateStyle` | 新建 tab 按钮样式 | 顶栏 `+` 或 create token 的存在感 | 指定它是不是一级 CTA |
| `tabActionStyle` / `tabActionActiveStyle` | 顶栏动作按钮样式 | 顶栏 action token 的普通态和激活态 | 指定 action 是否要弱于 create，激活后多明显 |
| `renderTabBar` | 生成页面最上面整行 | 整个顶栏长什么样 | 指定顶栏整体应该更稳重还是更跳 |
| `renderStatusBar` | 生成页面最下面整行 | mode badge、hint、meta、error/notice 的表现 | 指定底栏是“命令行提示感”还是“状态条感” |
| `drawPaneFrame` | 画单个 pane 的边框 | pane 边框、活跃 pane 边框、溢出边界的观感 | 指定 pane 是更硬朗还是更弱化 |
| `drawPaneTopBorderLabels` | 画 pane 顶边文字和 icon | pane 标题、状态、shared、owner/follow、右侧 icon 的层级 | 指定标题和状态的主次关系 |
| `drawEmptyPaneContent` | 画空 pane 的占位内容 | 空 pane 中央的文案和 4 个动作按钮 | 指定空状态 CTA 的优先级 |
| `emptyPaneActionDrawStyle` | 空 pane CTA 的分级器 | attach / create / manager / close 各自亮度和危险级别 | 给出主按钮、次按钮、危险按钮的视觉规范 |
| `renderPickerOverlayWithTheme` | 画终端 picker 弹层 | picker 卡片的标题、搜索框、列表、footer | 指定卡片感、边框感、搜索输入行的样式 |
| `renderPromptOverlayWithTheme` | 画表单/输入弹层 | prompt 表单字段、active field、footer | 指定表单输入态和普通态的区别 |
| `renderWorkspacePickerOverlayWithTheme` | 画 workspace picker 弹层 | workspace 列表、搜索、footer | 指定 workspace 选择器的视觉风格 |
| `renderTerminalManagerOverlayWithTheme` | 画 terminal manager 弹层 | terminal manager 的列表和 footer 动作 | 指定管理页应该更偏控制台还是更偏工具面板 |
| `renderHelpOverlayWithTheme` | 画 help 弹层 | help 的 section、快捷键、说明文案 | 指定帮助页是否要更强的文档感 |
| `renderOverlayFooterActionLabel` | 画 footer 动作 token | `[Enter] submit` 这一类命令提示长什么样 | 指定 key 部分和文字部分是否要区分明显 |
| `paneChromeActionTokensForFrame` | 定义 pane 顶边动作 icon | `⛶` `│` `─` `×` `◎` 这些 icon 会不会出现 | 如果设计师想改 icon 语言，这里是重点 |
| `emptyPaneActionSpecs` | 定义空 pane 的按钮文案和顺序 | attach/create/manager/close 的文本和顺序 | 如果设计师提议改按钮顺序，要改这里 |
| `terminalManagerFooterActionSpecs` | 定义 terminal manager footer 动作 | `[Enter] here` `[Ctrl-T] tab` 这类 footer 提示 | 如果设计师需要统一 footer 语气，要改这里 |

### 3.2 交接给配色负责人的最小理解模型

如果对方完全不看 Go，只需要理解下面这件事：

- `styles.go` 决定“颜色词典”
- `frame.go` 决定“最上面一行和最下面一行”
- `coordinator.go` 决定“中间 pane 和空状态”
- `overlays.go` 决定“所有弹层卡片”
- `hit_regions_*.go` 决定“哪些文字/图标算按钮，以及按钮文案是什么”

也就是说，后续对方如果给你一份配色文档，通常会落到两类修改：

- 改 `styles.go`：换 token、换层级、换对比度
- 改少量 render 函数：让某些块更像按钮、更像标签、或者更像状态

### 3.3 Gemini 可以忽略哪些技术细节

如果 Gemini 觉得代码信息太多，可以只抓这些事实，不需要深究 Go 实现：

- 有一套中央主题 token 系统
- 顶栏、底栏、pane、overlay、empty pane、terminal pool 都已经拆成单独区域
- 这些区域已经能分别套不同的视觉规则
- 代码已经支持“主强调 / 次强调 / 危险 / 弱提示”这类层级
- 后续实现时可以比较精确地把设计文档落到代码里

换句话说，Gemini 可以把当前系统理解为：

- 不是从零开始设计
- 也不是完全自由发挥
- 而是在一个已经拆分清楚的 TUI 视觉系统上重新定义视觉语言

## 4. 当前颜色模型

### 4.1 `uiTheme` 里最重要的 token 分组

#### Host / Surface

- `hostBG`
- `hostFG`
- `chromeBG`
- `chromeAltBG`
- `panelBG`
- `panelAltBG`
- `panelStrong`

用途：

- 页面基础底色
- top bar / status bar 底色
- overlay 卡片底色
- panel 内部背景

#### Text / Low-emphasis

- `chromeText`
- `chromeMuted`
- `panelText`
- `panelMuted`
- `metaText`

用途：

- 正文
- 次级说明
- 弱化标签
- 元信息

#### Accent / Semantic

- `chromeAccent`
- `success`
- `warning`
- `danger`
- `info`
- `fieldAccent`

用途：

- 当前轮里最应该给设计师重点关注的一组
- 它们决定 UI 是否有“主题感”和“强调点”

#### Borders / Structural emphasis

- `panelBorder`
- `panelBorder2`

用途：

- pane 边框
- overlay 边框
- terminal extent hint 的点状填充

#### Hint / Footer / Chip

- `hintKeyBG`
- `hintKeyFG`
- `hintTextBG`
- `hintTextFG`
- `footerKeyFG`
- `footerTextFG`
- `footerPlainFG`

用途：

- status bar 左侧 mode/hint chip
- overlay footer
- terminal pool footer action

#### Tab / Top bar

- `tabWorkspaceBG`
- `tabWorkspaceFG`
- `tabActiveBG`
- `tabActiveFG`
- `tabInactiveBG`
- `tabInactiveFG`
- `tabCreateBG`
- `tabCreateFG`
- `tabActionBG`
- `tabActionFG`
- `tabActionOnBG`
- `tabActionOnFG`

用途：

- workspace token
- tab token
- create tab token
- 顶栏 action token

### 4.2 当前层级关系

当前代码里，视觉层级大致是：

一级强调：

- `chromeAccent`
- `tabCreateBG`
- active top-bar action
- active pane title
- empty pane 的主 CTA

二级强调：

- `tabActionBG`
- `panelBorder`
- `hintKeyBG`
- `hintTextBG`
- empty pane 的 secondary CTA

语义强调：

- `success`
- `warning`
- `danger`
- `info`

弱提示：

- `panelMuted`
- `chromeMuted`
- `footerPlainFG`

## 5. 页面结构和布局分区

### 5.1 总体框架

见 `tuiv2/render/frame.go`：

- 顶栏：1 行
- 主体：中间区域
- 底栏：1 行

对应常量：

- `TopChromeRows = 1`
- `BottomChromeRows = 1`

### 5.2 顶栏

左侧：

- workspace 名称
- tab strip
- create tab token
- top bar action token

右侧：

- error
- notice

实现入口：

- `renderTabBar`
- `buildTabBarLayout`
- `renderTabBarLeft`
- `tabBarPaletteForState`

### 5.3 主体 Workbench

主体是 pane grid / floating pane 的合成画布。

每个 pane 主要有：

- 边框
- 顶边 chrome
- 内容区
- 为空时的 CTA 列表

实现入口：

- `drawPaneFrame`
- `drawPaneTopBorderLabels`
- `drawPaneContentWithKey`
- `drawEmptyPaneContent`

### 5.4 Overlay / Modal

当前 overlay 是居中的 card。

类型包括：

- picker
- prompt
- workspace picker
- terminal manager
- help

共同结构：

- 顶边标题
- 搜索或输入行
- 内容列表 / 表单
- footer action

实现入口：

- `renderPickerOverlayWithTheme`
- `renderPromptOverlayWithTheme`
- `renderWorkspacePickerOverlayWithTheme`
- `renderTerminalManagerOverlayWithTheme`
- `renderHelpOverlayWithTheme`

### 5.5 Terminal Pool Surface

Terminal Pool 不是 modal，而是整页 surface。

特点：

- 自己有页面 header / list / footer
- 不应和全局底栏争抢同一批视觉元素

实现入口：

- `buildTerminalPoolPageLayout`
- `layoutTerminalPoolFooterActionsWithTheme`

## 6. 可见交互对象清单

这一节是给视觉设计师最直接的交付清单。

### 6.1 顶栏对象

如果不懂代码，可以把顶栏理解成 4 类对象：

- workspace 标签
- tab
- create tab
- action token

#### Workspace token

视觉对象：

- workspace 名称块

代码：

- `renderWorkspaceToken`
- `workspaceLabelStyle`

#### Tab token

视觉对象：

- active tab
- inactive tab
- close button
- create tab

代码：

- `renderTabSwitchToken`
- `renderTabCloseToken`
- `renderTabCreateToken`
- `tabActiveStyle`
- `tabInactiveStyle`
- `tabCloseStyle`
- `tabCreateStyle`

当前图形细节：

- active tab 前面有 `▎`
- close token 使用 ``

这意味着：

- active tab 不只是换底色，还会多一个“左边缘强调”
- close 不是独立按钮框，而是 tab 内的一个紧凑 icon token

#### Top bar action token

视觉对象：

- 顶栏右侧或 tab 后面的 action button
- inactive / active 两种状态

代码：

- `renderTopBarActionTokenWithPalette`
- `tabActionStyle`
- `tabActionActiveStyle`

这意味着：

- 顶栏 action 更像“标签型按钮”，不是传统填充按钮
- 如果设计师想统一按钮语言，这里要和 empty pane CTA、overlay footer 一起考虑

### 6.2 状态栏对象

视觉对象：

- mode badge
- 快捷键 hint key
- 快捷键 hint text
- 分隔符
- 右侧 meta
- error / notice

代码：

- `renderStatusBar`
- `renderDesktopHint`
- `renderModeBadge`
- `renderModeHints`
- `statusHintKeyStyle`
- `statusHintTextStyle`
- `statusPartErrorStyle`
- `statusPartNoticeStyle`
- `statusMetaStyle`

这意味着：

- 底栏其实是 token 串，不是普通段落文字
- key 部分和描述文字部分可以拆开定义视觉语言
- 如果设计师要做一版更“专业工具型”的底栏，应该优先提 mode chip 和 key token 的样式规范

### 6.3 Pane chrome

#### Pane border

视觉对象：

- active border
- inactive border
- overflow edge

代码：

- `drawPaneFrame`

这意味着：

- active pane 和 inactive pane 的区别主要在这里
- 如果设计师觉得 pane 层次太弱或太吵，优先提这一块

#### Pane top chrome

视觉对象：

- 标题
- 生命周期槽位
- 共享计数槽位
- owner / follow 槽位
- pane action icon

代码：

- `paneBorderInfo`
- `paneTopBorderLabelsLayout`
- `drawPaneTopBorderLabels`
- `paneChromeActionTokensForFrame`

当前状态 token：

- lifecycle: `●` `○` `…` `✕`
- shared count: `⇄2` 这类短 token
- role: `◆ owner` / `◆ owner?` / `◇ follow`

当前动作 icon：

- `⛶` zoom
- `│` split vertical
- `─` split horizontal
- `×` close
- floating pane 还有 `◎` center

这意味着：

- 这块不是“按钮栏”，而是“标题 + 状态 + icon 操作”的混合 chrome
- 如果设计师不喜欢单字符 icon 语言，可以保留布局，只更换 icon 或 token 包裹方式

### 6.4 Empty pane

视觉对象：

- headline
- attach existing terminal
- create new terminal
- open terminal manager
- close pane

代码：

- `emptyPaneActionSpecs`
- `drawEmptyPaneContent`
- `emptyPaneActionDrawStyle`

当前文案：

- `[ Attach existing terminal ]`
- `[ Create new terminal ]`
- `[ Open terminal manager ]`
- `[ Close pane ]`

当前层级：

- attach: 主 CTA
- create: success 语义 CTA
- manager: 次级 CTA
- close: danger CTA

这意味着：

- 空 pane 是当前最接近传统“按钮设计”的地方
- 如果设计师要先选一块做视觉试点，建议优先从这里开始

### 6.5 Overlay footer / Surface footer

视觉对象：

- footer key
- footer text
- footer plain spacer

代码：

- `renderOverlayFooterActionLabel`
- `overlayFooterKeyStyle`
- `overlayFooterTextStyle`
- `overlayFooterPlainStyle`
- `layoutOverlayFooterActionsWithTheme`
- `layoutTerminalPoolFooterActionsWithTheme`

典型文案格式：

- `[Enter] submit`
- `[Esc] cancel`
- `[Ctrl-T] tab`
- `[Ctrl-O] float`
- `[Ctrl-K] kill`

这意味着：

- footer 不是说明文字，而是“命令提示按钮”
- 中括号里的 key 和后面的 action text，可以被当成两个不同层级的对象

### 6.6 Help overlay

视觉对象：

- section title
- key token
- action text

代码：

- `overlaySectionTitleStyle`
- `overlayHelpKeyStyle`
- `overlayHelpActionStyle`

## 7. 当前实现里的“按钮 / 图标 / 布局”关系

这个项目里，很多“按钮”并不是传统按钮，而是文本 token 或 icon token。做视觉时应该按交互角色分组，而不是按 HTML 意义分组。

建议把对象按下面方式理解：

### 7.1 主操作对象

- create tab
- attach terminal
- create terminal
- submit / open / here

这组应该共享“主动作”的视觉语言。

### 7.2 次操作对象

- top bar action
- open terminal manager
- edit
- float
- tab
- split
- zoom

这组应该共享“次级动作”的视觉语言。

### 7.3 危险操作对象

- close pane
- close tab
- kill terminal
- delete workspace

这组应该共享明显不同于主操作的危险语言。

### 7.4 状态对象

- running / exited / waiting / killed
- owner / follow / shared count
- status bar meta

这组不应该和 CTA 看起来一样，更接近 badge / status token。

## 8. 当前配色负责人最适合改的层

这一节是给后续协作流程用的。

建议分工：

- 配色负责人负责提出“每组对象应该长什么样”
- 我来根据那份文档改代码

所以对方给出的文档，最好不要直接写“改 `tabActionBG`”，而是写人话规则，例如：

- 顶栏 create 按钮要比普通 action 明显 30%
- active pane 标题要比右侧状态更亮
- 危险动作统一使用单独色相
- overlay footer 的 key token 要比动作描述更突出

### 8.1 建议优先只改 `styles.go`

如果目标是“换一套视觉语言但不重写布局”，最优先改：

- `uiThemeFromHostColors`
- `workspaceLabelStyle`
- `tab*Style`
- `status*Style`
- `overlay*Style`

这样影响最大，风险最小。

### 8.2 如需调整层级，也可以改这些点

- `drawPaneFrame`
- `drawPaneTopBorderLabels`
- `emptyPaneActionDrawStyle`
- `renderTabSwitchToken`
- `renderTabCreateToken`
- `renderTopBarActionTokenWithPalette`

这些地方决定“同一个区域里谁更亮、谁更弱”。

### 8.3 不建议先动的部分

如果只是做主题 / 配色，不建议一上来就改：

- `paneTopBorderLabelsLayout`
- `buildPickerCardLayout`
- `buildTerminalPoolPageLayout`
- hit region 的矩形计算
- input catalog 的动作定义

这些更偏结构和交互，不是纯视觉层。

## 8.4 最适合让对方产出的文档格式

为了方便我后续直接实现，建议对方的配色文档按下面格式写：

### A. 全局语义色

- 主强调色
- 次强调色
- 成功色
- 警告色
- 危险色
- 弱提示色
- 边框色
- 卡片底色

### B. 按区域写视觉规则

- 顶栏
- 底栏
- pane 边框
- pane 顶边
- empty pane
- overlay 卡片
- overlay footer
- terminal pool 页面

### C. 按对象写状态规则

每个对象最好写：

- 默认态
- active 态
- selected 态
- danger 态

### D. 如果要改 icon，也单独列

例如：

- tab close icon
- pane close icon
- pane split icon
- pane zoom icon
- floating center icon
- running / exited / waiting / killed 状态符号

## 8.5 一句话协作约定

你可以直接把这句话转给对方：

“你不用看懂 Go，只要按这个文档把各个区域和对象的配色、层级、按钮语言写清楚；代码实现我这边会根据你的文档来改。”

## 8.6 最适合 Gemini 产出的文档格式

如果是 Gemini 来产出设计文档，建议它直接输出下面这种结构。

### 1. Design Goal

写清楚这版主题的目标，例如：

- 更偏专业工具感
- 更偏强对比的 dark theme
- 更偏克制、稳定、冷静
- 更偏有动漫主题感但不花

### 2. Global Color System

用人话说明这些颜色：

- app background
- chrome background
- panel background
- panel border
- primary accent
- secondary accent
- success
- warning
- danger
- muted text
- normal text

### 3. Component Rules

按组件逐块写：

- top bar
- status bar
- active pane
- inactive pane
- pane title/meta/action
- empty pane
- overlays
- overlay footer
- terminal pool page

### 4. Interaction Hierarchy

必须明确写：

- 什么是 primary action
- 什么是 secondary action
- 什么是 danger action
- 什么是 passive status
- 什么是 current/active state

### 5. Icon / Token Language

如果要调整 icon 或 token 风格，单独写：

- 保留字符型 icon，还是改成更明确的 token 风格
- close / kill / split / zoom / create 是否要统一
- 状态符号 `● ○ … ✕` 是否继续保留

### 6. Implementation Notes For Engineer

这部分不要写代码，只要写工程可执行规则，例如：

- 顶栏 create token 要比普通 action 更亮
- active pane 标题要高于 meta
- close / kill / delete 必须统一 danger 色
- footer 里的 key token 要比说明文字更突出
- overlay 边框要比 pane 边框更明确或更柔和

### 7. Open Questions

如果 Gemini 拿不准，应该把问题列出来，而不是自己假设，例如：

- 是否保留字符 icon 体系
- 是否让 workspace 标签也吃 accent
- overlay 是否需要比 pane 更高对比

## 8.7 给 Gemini 的核心要求

如果把这份文档丢给 Gemini，请它遵守下面几点：

- 产出的是设计规范，不是代码解释
- 用人话描述视觉规则，不要用 Go 术语组织文档
- 要覆盖按钮、图标、状态、边框、标签、卡片、底栏、空状态
- 要明确“谁比谁更亮、更弱、更危险、更像当前选中项”
- 如果有多个方案，最多给 2 套，不要发散太多
- 输出内容最终要能被工程师直接映射回现有 UI 区块

## 9. 当前已知视觉问题 / 可继续打磨的点

以下是当前实现里还没有完全定稿的地方：

### 9.1 Pane top chrome 仍然是字符型 UI

- 很多操作目前是单字符 icon
- 可继续决定是否要保留纯字符风格，还是增加更明确的 token 包裹感

### 9.2 Overlay 和 Workbench 的语言还没有完全统一

- overlay footer 目前更像 command hint
- top bar action 更像 compact token
- pane action 更像纯 icon

后续可以统一成更清晰的一套组件语言。

### 9.3 危险动作的全局统一还不完整

- 有些 close / kill / delete 还只是依靠文案区分
- 可以继续统一危险色、危险 icon、危险 hover / active 逻辑

### 9.4 当前没有独立 hover 主题层

- 现在主要区分 normal / active / selected
- 未来如果鼠标交互更重，可能要补 hover token

## 10. 推荐给新配色负责人的工作顺序

建议顺序：

1. 先只确定语义色体系
2. 再确定 top bar / status bar / pane / overlay 的层级关系
3. 然后统一主动作 / 次动作 / 危险动作的视觉语言
4. 最后再决定是否要调整 icon 文法和 token 包裹样式

更具体一点：

1. 先定 `chromeAccent / success / warning / danger / info`
2. 再定 `panelBorder / panelBorder2 / panelMuted`
3. 再定 `tabCreate / tabAction / tabActionOn`
4. 再定 `hint / footer / overlay`
5. 最后收 pane top chrome 和 empty pane CTA

## 11. 参考文档

- `docs/tuiv2-chrome-layout-spec.md`
- `docs/tuiv2-render-migration-guide.md`

## 12. 附：本轮主题实现测试入口

如果配色负责人改完主题，建议至少看这些测试：

- `tuiv2/render/styles_test.go`
- `tuiv2/render/coordinator_test.go`
- `tuiv2/render/frame_test.go`
- `tuiv2/render/hit_regions_workbench_test.go`

当前已经有的测试覆盖了部分内容：

- 暗色主题下 accent 是否仍然有颜色
- host palette 是否会驱动 semantic color
- top bar 的 create / action / active action 是否有层级
- active pane chrome 是否有 title / meta / action 的层级
- empty pane CTA 是否区分 primary / secondary / danger
