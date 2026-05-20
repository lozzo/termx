# termx tuiv2 UI/UX Designer Handoff

状态：Draft  
日期：2026-04-11

## 1. 文档目的

这份文档是给设计师或 AI 设计师的输入材料。

目标不是解释 Go 代码，而是把以下内容整理清楚：

- 当前系统真实有哪些界面
- 每个界面承担什么功能
- 当前视觉风格和交互风格是什么
- 当前界面的线稿长什么样
- 下一版希望设计师重点重做哪些区域
- 哪些终端/TUI 约束不能忽略

这份文档应被视为：

- `设计输入`
- `线稿需求说明`
- `界面重设计边界`

而不是工程实现文档。

这份文档应该是自包含的。

- 设计师不需要再去查看仓库内其他文档
- 需要的背景、约束、现状、方向都应直接从这一份文档获得

## 2. 产品定位

`termx` 对外是一个更现代的终端复用器。

它的使用体验应接近：

- `tmux`
- `zellij`

但它的机制并不等同于传统终端复用器：

- `terminal` 是全局运行实体
- `pane` 是 terminal 的工作位/观察位
- 一个 terminal 可以被多个 pane 复用
- TUI 是 terminal pool 的第一方工作台

因此设计师应把它理解为：

- 主界面上像终端复用器
- 底层上是建立在 terminal pool 之上的 workbench

## 3. 核心对象

设计时必须围绕这些对象组织界面：

- `workspace`
- `tab`
- `pane`
- `floating pane`
- `terminal`
- `terminal pool`
- `overlay modal`

补充语义：

- workspace 是工作现场，不天然绑定项目目录
- tab 主要组织 pane
- pane 不等于 terminal
- floating pane 是完整 pane 的另一种摆放方式
- terminal pool 是全局资源页，不是 picker 放大版

## 4. 当前一级信息架构

当前系统分为三类主界面层：

### 4.1 Workbench

默认主界面，承载日常工作。

负责：

- workspace / tab / pane 主工作流
- floating pane
- attach / split / zoom / focus
- unconnected / exited pane 状态

### 4.2 Terminal Pool

独立页面/Surface，用来查看和管理 terminal 本体。

负责：

- 查看 terminal 列表
- 搜索 terminal
- attach 到 pane / 新 tab / floating
- edit terminal metadata
- kill terminal
- 预览 snapshot / detail

### 4.3 Overlay

当前包括：

- terminal picker
- workspace picker
- prompt
- help
- floating overview

负责：

- 高频局部动作
- 轻量选择
- 输入表单

## 5. 当前真实界面清单

这部分描述的是“现在已经存在”的 UI，不是理想状态。

### 5.1 Top Bar

当前位置：

- 左侧：workspace 名称
- 中左：tab strip
- 右侧：短 notice / error

视觉特征：

- workspace 为 badge/token
- active tab 和 inactive tab 有轻微层级差异
- create tab 为 `+` token

### 5.2 Main Workbench Body

当前位置：

- 中央是 pane grid
- 支持 zoomed pane
- 支持 floating pane 覆盖在 pane grid 之上

视觉特征：

- pane 有边框
- pane 顶边有标题与状态
- 右上角有 pane action token

### 5.3 Bottom Bar

当前位置：

- 左侧：当前 mode 和 hint
- 右侧：workspace / float / terminals 摘要

视觉特征：

- 使用 token/chip 语法
- 当前偏工程化，信息密度较高

### 5.4 Empty Pane

未连接 terminal 的 pane 不是空白。

当前会展示：

- `Attach existing terminal`
- `Create new terminal`
- `Open terminal manager`
- `Close pane`

### 5.5 Exited Pane

terminal 退出后 pane 保留。

当前会展示：

- 历史内容
- recovery action

### 5.6 Picker Modal

当前 terminal picker 是居中的卡片式 overlay。

包含：

- title
- search field
- list
- 有些场景会有 footer action

### 5.7 Workspace Picker

当前 workspace picker 也是卡片式 overlay。

现状问题：

- 语义偏薄
- 更像 workspace switcher
- 不够像 tree chooser / manager

### 5.8 Prompt Modal

当前 prompt 主要负责：

- rename
- create terminal
- edit terminal metadata

### 5.9 Help Modal

帮助页以 section + key/action 列表呈现。

### 5.10 Floating Overview

当前有 floating panes 的概览 overlay。

## 6. 当前功能清单

这是给设计师理解“界面需要承载哪些动作”的最小清单。

### 6.1 Workspace

- create
- switch
- rename
- delete
- prev / next

### 6.2 Tab

- create
- switch
- jump
- rename
- close
- kill

### 6.3 Pane

- split vertical
- split horizontal
- close
- focus
- zoom
- attach existing terminal
- create new terminal

### 6.4 Floating Pane

- create
- move
- resize
- center
- collapse
- close

### 6.5 Terminal

- attach
- attach as split
- attach as new tab
- attach as floating
- edit metadata
- kill

### 6.6 Display / Copy

- scrollback
- copy mode
- clipboard history

## 7. 当前视觉风格

### 7.1 总体风格

当前风格偏：

- 工程工具
- 终端原生
- 高密度信息
- 深色宿主终端跟随

不是：

- 插画型
- 大留白 GUI
- 强拟物设计

### 7.2 颜色来源

当前实现采用两层颜色模型：

第一层：

- 跟随宿主终端主题

第二层：

- UI 自己的 semantic accent

因此设计稿不应假设固定品牌色背景，而应适配：

- 深色 terminal
- 浅色 terminal
- 不同宿主 palette

### 7.3 当前问题

当前视觉虽然已经可用，但主要问题是：

- 层级不够稳
- CTA 不够像 CTA
- 一些 modal 更像文本框而不是工作台卡片
- 管理型选择器信息密度不足
- 结构视图不够强，缺少 tmux choose-tree 那种“工作现场导航感”

## 8. 当前硬约束

设计师必须考虑这些约束，不能忽略。

### 8.1 这是 TUI，不是 GUI

- 只能用字符、边框、前景色、背景色、间距、token 语言来塑造层级
- 动画和自由布局能力有限

### 8.2 宽窄屏都要成立

- 大终端下可以两栏、多区块
- 小终端下必须有可退化方案

### 8.3 命中区域必须稳定

- 可点击元素不能因为文案变长就漂移
- pane chrome 特别需要稳定槽位

### 8.4 Modal 不应长期承担重管理逻辑

当前有些管理动作在 modal 里，但未来更重的管理应回到 surface/page。

### 8.5 快捷键属于全局输入系统

这是新的设计要求，不是当前实现现状：

- modal 内不要展示快捷键文案
- 快捷键提示应留在 status bar / help / 外部文档
- modal 内只显示动作词或动作 chip，不显示 `[Ctrl-X]` 这种字符串

## 9. 当前设计线稿

下面的线稿描述“现在大致长什么样”，不是最终目标。

## 9.1 当前 Workbench 主界面

```text
╭ workspace badge ─ tab 1 ─ tab 2 ─ [+] ───────────────────── notice/error ╮
│ pane title                                    state share role [Z][|][-][x]│
│ ┌──────────────────────────────┬──────────────────────────────────────────┐ │
│ │ terminal content             │ terminal content                         │ │
│ │                              │                                          │ │
│ │                              │                                          │ │
│ └──────────────────────────────┴──────────────────────────────────────────┘ │
│                                                                            │
│                (floating pane may appear above tiled panes)                │
│                                                                            │
╰ mode chip • hints • hints • hints ───────── ws:main  float:3  terminals:4 ╯
```

## 9.2 当前 Empty Pane

```text
┌────────────────────────────────────────────────────────────────────────────┐
│ unconnected / no terminal attached                                         │
│                                                                            │
│                    [ Attach existing terminal ]                            │
│                    [ Create new terminal ]                                 │
│                    [ Open terminal manager ]                               │
│                    [ Close pane ]                                          │
└────────────────────────────────────────────────────────────────────────────┘
```

## 9.3 当前 Terminal Picker Modal

```text
╭ Terminal Picker ───────────────────────────────────────────────╮
│ search:                                                        │
│ ▸ shell  running                                               │
│   logs   running                                               │
│   + new terminal                                               │
│                                                                │
│ [footer action may appear here in some modes]                  │
╰────────────────────────────────────────────────────────────────╯
```

## 9.4 当前 Workspace Picker Modal

```text
╭ Workspaces ────────────────────────────────────────────────────╮
│ search:                                                        │
│ ▸ main                                                         │
│   dev                                                          │
│   ops                                                          │
│   + new workspace                                              │
╰────────────────────────────────────────────────────────────────╯
```

## 9.5 当前 Prompt Modal

```text
╭ Prompt / Rename / Create ──────────────────────────────────────╮
│ step / metadata summary                                        │
│                                                                │
│ name: value                                                    │
│ tags: value                                                    │
│                                                                │
│ [Enter] ...  [Esc] ...                                         │
╰────────────────────────────────────────────────────────────────╯
```

## 9.6 当前 Terminal Pool Page

```text
╭ Terminal Pool ──────────────────────────────────────────────────────────────╮
│ search:                                                                    │
│                                                                            │
│ list of terminals                                                          │
│   shell                                                                    │
│   logs                                                                     │
│   worker                                                                   │
│                                                                            │
│ detail / preview area                                                      │
│                                                                            │
╰ page footer actions ────────────────────────────────────────────────────────╯
```

## 10. 设计师重点重做区域

这是当前最值得投入设计精力的部分。

### 10.1 Workbench Tree Modal

希望重做成：

- 两栏或树状结构
- 语义接近 `tmux choose-tree`
- 同时覆盖 `workspace + tab`
- 右侧可展示 snapshot / summary

而不是当前这种薄的 workspace 列表。

### 10.2 Pane Chrome

需要设计更稳定的：

- 标题层级
- 状态 token
- action token
- owner/follower/share/lifecycle 表达方式

### 10.3 Empty Pane

需要把当前纯工程态 CTA，设计成更清晰的：

- primary
- secondary
- danger

按钮语言。

### 10.4 Terminal Pool

需要从“功能页”提升成更像资源管理工作台的页面。

### 10.5 Modal 系统整体风格

需要统一：

- card 尺寸
- 标题样式
- 搜索字段样式
- 列表选中态
- detail / preview 布局

## 11. 希望设计师产出的内容

希望设计师输出：

### 11.1 当前界面对照线稿

把当前主要界面重新画成更清晰的线稿：

- workbench
- terminal picker
- workspace picker
- prompt
- terminal pool

### 11.2 新方案线稿

重点输出下一版线稿：

- workbench 主界面
- workbench tree modal
- terminal pool page
- pane chrome
- empty pane

### 11.3 组件规范

至少定义：

- top bar token
- tab token
- status chip
- pane chrome action
- list row
- selected row
- empty state CTA
- modal header
- preview panel

### 11.4 退化策略

必须说明：

- 宽屏长什么样
- 窄屏怎么退化
- 小 terminal 尺寸下哪些元素优先隐藏

## 12. 新方向的明确要求

以下是下一版设计必须明确回应的问题：

### 12.1 Modal 中不显示快捷键

设计稿中不要在 modal 内写：

- `[Enter] open`
- `[Ctrl-X] delete`

这类 key hint。

### 12.2 结构型 modal 要像 choose-tree

对于 workspace/tab 导航与管理：

- 应优先采用树状或两栏结构
- 不再停留在简单列表切换器

### 12.3 允许展示 snapshot

对于 workspace / tab / pane 的选择器：

- 右侧或下方应允许展示 snapshot preview
- 这会显著提升“工作现场导航”的理解成本表现

## 13. 建议给 AI 设计师的任务描述

可以直接把下面这段给 AI 设计师：

> 请基于 termx tuiv2 的真实终端工作台结构，为一个 TUI 系统设计线稿方案。  
> 这个系统的核心对象是 workspace / tab / pane / floating pane / terminal pool。  
> 当前主界面是 workbench，另有 terminal pool page 和多种 overlay modal。  
> 请先理解当前界面线稿，再输出一套新的线稿方案，重点重做：
> 1. workbench 主界面
> 2. 类似 tmux choose-tree 的 workbench tree modal
> 3. terminal pool page
> 4. pane chrome
> 5. empty pane
>
> 约束：
> - 这是 TUI，不是 GUI
> - 要支持宽屏与窄屏退化
> - modal 内不要展示快捷键文案
> - 结构型 modal 应优先采用两栏或树状布局
> - 可以在右侧展示 snapshot / preview
> - 请输出线稿、信息层级说明、状态说明、以及组件级规范

## 14. 使用方式

如果要把这份文档复制给外部设计师或 AI 设计师，直接复制全文即可。

它应已经包含：

- 当前系统是什么
- 当前界面长什么样
- 当前功能有哪些
- 设计约束是什么
- 下一版重点该重做什么

不要求设计师再去读取任何仓库内路径或额外参考文档。
