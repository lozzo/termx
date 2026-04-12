# TUIV2 Workbench Tree Modal Design

状态：Draft  
日期：2026-04-11

## 1. 问题重述

当前的 `workspace picker` 有三个根本问题：

- 它只是 workspace 名称列表，信息过薄。
- 它把管理范围限制在 workspace，无法自然覆盖 tab / pane。
- 它在 modal 内展示快捷键，和“快捷键是全局输入系统的一部分”这个原则冲突。

因此这一层不应该继续被视为“workspace picker 增强版”，而应该被重定义为：

- 一个 **workbench tree modal**
- 语义接近 `tmux choose-tree`
- 负责浏览、切换、管理 `workspace / tab / pane`
- 在右侧展示所选对象的 snapshot / summary

## 2. 核心原则

### 2.1 Modal 不展示快捷键

modal 内不展示：

- `[Enter] open`
- `[Ctrl-N] new`
- `[Ctrl-R] rename`

这类 key hint。

原因：

- 快捷键属于全局输入系统，不属于 modal 内容本身。
- modal 的任务是“看对象、选对象、点对象”，不是“解释键盘系统”。
- 快捷键提示应继续留在 status bar / help overlay / 全局文档中。

modal 内可以保留：

- 纯动作词按钮
- 纯语义标签
- 鼠标可点击 action chip

例如：

- `Open`
- `Rename`
- `Delete`
- `Preview`

但不附带具体快捷键文案。

### 2.2 这是 workbench modal，不是 workspace modal

该 modal 的对象层级应至少覆盖：

- workspace
- tab
- pane

其中 workspace 只是第一层节点，不是唯一管理对象。

### 2.3 结构优先于动作

用户首先应该看到结构树：

- 哪些 workspace
- 每个 workspace 下有哪些 tab
- 每个 tab 下当前有哪些 pane / floating pane

再决定对当前选中对象做什么。

### 2.4 Snapshot 是核心信息，不是附属信息

如果只是显示名字列表，这个 modal 永远不够强。

右侧必须提供所选对象的预览：

- workspace 预览
- tab 预览
- pane 预览

这样用户才能真正把它当作“工作现场导航器”而不是“名字切换器”。

## 3. 新对象定义

建议废弃“workspace picker”这个产品命名，改成以下之一：

- `Workbench Tree`
- `Session Tree`
- `Choose Tree`

代码层可命名为：

- `WorkbenchPickerState`
- `WorkbenchTreeModal`

`workspace picker` 可保留为兼容文件名，但产品文案不再继续强调它只处理 workspace。

## 4. 交互模型

## 4.1 左侧树

左侧为层级树：

- workspace row
- tab row
- pane row

层级关系：

```text
workspace
  tab
    pane
    pane
  tab
workspace
  tab
    pane
```

每行是一个稳定节点，不是自由文案。

每个节点都有：

- `NodeKind`
- `ID`
- `ParentID`
- `Depth`
- `Expanded`
- `Selectable`

### 4.2 选中行为

- 选中 workspace：右侧显示 workspace summary + active tab snapshot
- 选中 tab：右侧显示 tab summary + active pane snapshot
- 选中 pane：右侧显示 pane snapshot

### 4.3 打开行为

- `Enter` 在 workspace 上：切换到该 workspace
- `Enter` 在 tab 上：切换 workspace 并激活 tab
- `Enter` 在 pane 上：切换 workspace / tab 并 focus pane

这里的具体快捷键不显示在 modal 内，但语义仍保留。

### 4.4 展开/收起

树节点支持展开/收起：

- workspace 展开后显示其 tab
- tab 展开后显示其 pane

默认策略建议：

- 当前 workspace 默认展开
- 当前 tab 默认展开
- 其他 workspace 默认折叠

## 5. 布局方案

### 5.1 宽屏：两栏

适用于宽度充足场景。

推荐比例：

- 左栏树：55%
- 右栏预览：45%

布局：

```text
╭ Workbench ──────────────────────────────┬ Snapshot / Details ─────────────────╮
│ search field                            │ selected object title                │
│                                         │ status / summary                     │
│ workspace tree                          │                                      │
│   workspace                             │ snapshot preview                     │
│   tab                                   │                                      │
│   pane                                  │ metadata / actions                   │
│                                         │                                      │
│ action chips                            │ context actions                      │
╰─────────────────────────────────────────┴──────────────────────────────────────╯
```

### 5.2 窄屏：树 + 下方详情

当宽度不够时退化为单栏：

- 上半：树
- 下半：所选对象详情 / snapshot

### 5.3 不做纯中间小卡片

这类 modal 不应再像“terminal picker 小卡片”那样只占中间一小块。

建议：

- 更宽
- 更高
- 接近一个轻量 surface

也就是说，它仍是 modal，但视觉上应是“大型工作台弹层”，而不是输入框式弹窗。

## 6. 左侧树的行设计

每种节点都必须一眼可区分。

### 6.1 Workspace Row

展示：

- workspace 名
- 是否 current
- tab 数
- pane 数
- floating 数

示例：

```text
● main                  tabs 3   panes 7   float 2
  dev                   tabs 2   panes 4
```

### 6.2 Tab Row

展示：

- tab 名
- 是否 active
- pane 数
- zoom / floating 摘要

示例：

```text
  ├─ deploy             active   panes 3
  ├─ logs                        panes 1
```

### 6.3 Pane Row

展示：

- pane title / terminal title
- owner/follower / exited / unconnected
- floating 标记

示例：

```text
  │  ├─ shell           owner
  │  ├─ logs            follower
  │  └─ htop            floating
```

## 7. 右侧预览设计

右侧预览不是长篇描述，而是“短摘要 + snapshot”。

### 7.1 选中 workspace

展示：

- workspace 名
- tab / pane / floating 总数
- 当前 tab 名
- 当前 tab 的 snapshot

### 7.2 选中 tab

展示：

- 所属 workspace
- tab 名
- pane 数
- active pane 名
- active pane snapshot

### 7.3 选中 pane

展示：

- 所属 workspace / tab
- pane title / terminal title
- 状态
- snapshot 预览

### 7.4 Snapshot 来源

优先复用现有能力：

- runtime 已有 terminal snapshot / visible state
- terminal manager 已有 preview 思路

这一层不需要重新发明渲染器，只要把 snapshot 作为右栏内容接进来。

## 8. 动作设计

### 8.1 Modal 内动作不显示快捷键

底部或右栏动作栏只显示纯动作词：

- `Open`
- `Rename`
- `Delete`
- `New Workspace`
- `New Tab`
- `Close Tab`

这些动作可以是：

- 鼠标可点击 chip
- 或纯语义文案

但不显示 `[Ctrl-X]` 这种内容。

### 8.2 动作按节点类型变化

选中 workspace：

- Open
- Rename
- Delete
- New Workspace
- New Tab

选中 tab：

- Open
- Rename
- Close Tab
- New Tab

选中 pane：

- Open
- Focus
- Zoom

第一阶段可以先只落地：

- workspace: open / rename / delete / new
- tab: open / rename / close

pane 动作可后补。

## 9. 搜索 / 过滤

搜索范围不再只搜 workspace 名称，而应该覆盖：

- workspace 名
- tab 名
- pane title
- terminal title

过滤结果仍按树显示，但只保留命中的路径。

例如命中 pane 时，应保留其祖先：

```text
workspace
  tab
    pane  <- match
```

而不是直接扁平化成无层级列表。

## 10. 视觉语言

### 10.1 标题区

- 左上：`Workbench`
- 右上：当前选中对象类型 badge，例如 `workspace` / `tab` / `pane`

### 10.2 树区

- 选中行：整行背景 + 左侧 accent bar
- current workspace：独立 badge，不只靠颜色
- active tab：更强前景色
- exited / unconnected：使用语义色

### 10.3 预览区

- 标题
- 一行 summary
- snapshot box
- 下方 context actions

### 10.4 不再显示快捷键 footer

当前这类 footer：

```text
[Enter] open  [Ctrl-N] new  [Ctrl-R] rename
```

不再出现在 modal 内。

可替换为：

```text
Open   Rename   Delete   New Workspace
```

## 11. 与当前代码的关系

当前相关代码主要在：

- `tuiv2/modal/workspace_picker.go`
- `tuiv2/render/overlays.go`
- `tuiv2/render/hit_regions_overlay.go`
- `tuiv2/app/update_actions_modal.go`
- `tuiv2/app/update_effects.go`

但如果按本设计落地，建议不要继续在现有 `workspace picker` 上堆功能，而是直接升级为新的 modal 状态对象。

建议的新代码落点：

- `tuiv2/modal/workbench_picker.go`
- `tuiv2/render/workbench_tree_overlay.go`
- `tuiv2/render/hit_regions_workbench_tree.go`
- `tuiv2/app/update_actions_workbench_tree.go`

现有 `workspace_picker.go` 可以：

- 临时兼容
- 或最终删除

## 12. 第一阶段落地范围

本轮建议只做第一阶段，不一次做到最满：

1. 把 `workspace picker` 升级成 `workbench tree modal`
2. modal 内移除所有快捷键展示
3. 左侧支持 `workspace -> tab` 两级树
4. 右侧支持 selected workspace / tab 的 snapshot preview
5. workspace / tab 的 open / rename / delete / close 基础动作打通

第二阶段再补：

- pane 节点
- pane 级动作
- tree 展开/收起交互细化
- 更高级的 filter / fuzzy / recent ordering

## 13. 结论

我们不应该继续做“更复杂的 workspace picker”。

应该直接把这一层重定义为：

- 不展示快捷键的
- 两栏或树状的
- 带 snapshot preview 的
- 管理 `workspace + tab` 的
- 接近 `tmux choose-tree` 的

`workbench tree modal`。
