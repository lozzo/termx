# termx 配置说明

`termx` 当前把用户偏好配置集中放在一个文件里：

```text
$XDG_CONFIG_HOME/termx/termx.yaml
~/.config/termx/termx.yaml
```

如果文件不存在，启动 TUI 时会自动创建一份带注释的默认配置。

当前配置主要面向 `tuiv2`，支持两类能力：

- `chrome`：控制 pane / status / tab 顶栏里展示哪些槽位，以及顺序
- `theme`：在宿主终端主题推导结果之上做少量 token 覆盖

## 设计原则

配置系统当前遵循两个原则：

- 默认外观仍然以宿主终端主题为准，不引入固定品牌主题
- 配置只做“用户偏好覆盖”，不把 render 层的 host-aware 主题模型推翻

这意味着：

- 如果你完全不写 `theme`，`termx` 会继续从 host terminal 的前景、背景和 palette 推导 UI
- 如果你只想调布局，不需要碰颜色
- 如果你只想覆盖少数 token，可以只写那几个字段

## 文件格式

当前解析器支持一个较小的 YAML 子集，最稳的写法是：

- 顶层 section 使用 `chrome:` / `theme:`
- `chrome` 里的列表使用 inline list：`[a, b, c]`
- `theme` 里的值使用简单字符串
- 可以写注释

建议使用这种格式：

```yaml
chrome:
  paneTop: [pane.title, pane.actions]
  statusLeft: [status.mode, status.hints]
  statusRight: []
  tabLeft: [tab.workspace, tab.tabs]

theme:
  accent: "#8b5cf6"
  panelBorder: "#4b5563"
```

不建议把 `chrome` 的数组改成多行 `- item` 的 YAML list，因为当前实现并不按完整 YAML 解析器处理。

## chrome

`chrome` 用来决定各个 UI 区域里展示哪些槽位，以及顺序。

规则：

- 省略某个字段：使用内建默认值
- 调整数组顺序：表示重排
- 显式写成 `[]`：表示隐藏该区域
- 未知槽位：当前会被忽略
- 重复槽位：当前会自动去重

### paneTop

控制 pane 顶栏。

可用槽位：

- `pane.title`
- `pane.state`
- `pane.share`
- `pane.role`
- `pane.copy_time`
- `pane.copy_row`
- `pane.actions`

默认值：

```yaml
paneTop: [pane.title, pane.state, pane.share, pane.role, pane.copy_time, pane.copy_row, pane.actions]
```

示例，做一份更干净的 pane 顶栏：

```yaml
paneTop: [pane.title, pane.state, pane.role, pane.actions]
```

### statusLeft

控制底部 status bar 左侧。

可用槽位：

- `status.mode`
- `status.hints`

默认值：

```yaml
statusLeft: [status.mode, status.hints]
```

### statusRight

控制底部 status bar 右侧。

可用槽位：

- `status.tokens`

默认值：

```yaml
statusRight: [status.tokens]
```

如果你想隐藏右侧 token：

```yaml
statusRight: []
```

### tabLeft

控制顶部 tab bar 左侧内容。

可用槽位：

- `tab.workspace`
- `tab.tabs`
- `tab.create`
- `tab.actions`

默认值：

```yaml
tabLeft: [tab.workspace, tab.tabs, tab.create, tab.actions]
```

如果你想更克制一点：

```yaml
tabLeft: [tab.workspace, tab.tabs, tab.create]
```

## theme

`theme` 用来覆盖 render 层推导出的少数 token。

如果某个字段为空，就继续使用默认 host-aware 结果。

当前支持的字段：

- `accent`
- `success`
- `warning`
- `danger`
- `info`
- `panelBorder`
- `panelBorder2`
- `tabActiveBG`
- `tabActiveFG`
- `tabInactiveBG`
- `tabInactiveFG`
- `tabCreateBG`
- `tabCreateFG`

示例：

```yaml
theme:
  accent: "#8b5cf6"
  panelBorder: "#4b5563"
  panelBorder2: "#6b7280"
  tabActiveBG: "#111827"
  tabActiveFG: "#f9fafb"
```

建议：

- 优先少量覆盖，不要一上来把所有字段都写满
- 先让 `termx` 跑在你自己的终端主题上看基线，再决定是否需要覆盖
- 如果你的目标只是“让顶栏更简洁”，优先改 `chrome`，不是 `theme`

## 一个推荐起步配置

下面这份配置比较适合日常使用：减少 pane / tab 顶栏噪音，但保留状态信息。

```yaml
chrome:
  paneTop: [pane.title, pane.state, pane.share, pane.role, pane.actions]
  statusLeft: [status.mode, status.hints]
  statusRight: [status.tokens]
  tabLeft: [tab.workspace, tab.tabs, tab.create]

theme:
```

## 常见用法

### 1. 极简布局

```yaml
chrome:
  paneTop: [pane.title, pane.actions]
  statusLeft: [status.hints]
  statusRight: []
  tabLeft: [tab.workspace, tab.tabs]
```

### 2. 保持布局默认，只改强调色

```yaml
theme:
  accent: "#8b5cf6"
```

### 3. 让 tab 更深一点

```yaml
theme:
  tabActiveBG: "#111827"
  tabInactiveBG: "#0f172a"
  tabInactiveFG: "#94a3b8"
```

## 当前限制

当前配置系统还比较小，已知边界包括：

- 只支持 `chrome` 和 `theme` 两段
- `chrome` 只支持当前列出来的槽位
- 解析器是轻量实现，不是完整 YAML 语义
- 更复杂的绑定、插件、行为偏好目前还没有开放到配置文件里

实现入口见 [../tuiv2/shared/config_file.go](../tuiv2/shared/config_file.go)。
