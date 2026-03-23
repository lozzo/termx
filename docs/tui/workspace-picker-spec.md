# termx TUI Workspace Picker 规范

状态：Draft v1
日期：2026-03-23

这份文档面向 AI 编码实现。

目标：

1. 把 workspace picker 做成树形导航器
2. 支持直接跳到某一个 pane
3. 明确搜索、展开、折叠、焦点和回车行为

---

## 1. 定位

workspace picker 不是简单列表。

它是一个树形导航 overlay，至少展示：

- workspace
- tab
- pane

用户可以：

- 搜索 workspace
- 展开 tab
- 定位 pane
- 直接跳到目标 pane

---

## 2. 数据模型

```go
type WorkspacePickerState struct {
    Query           string
    RootNodes       []WorkspaceTreeNode
    VisibleNodes    []WorkspaceTreeRow
    SelectedIndex   int
    Expanded        map[string]bool
    MatchMode       WorkspacePickerMatchMode
}
```

```go
type WorkspaceTreeNode struct {
    Key        string
    Kind       WorkspaceTreeNodeKind
    WorkspaceID WorkspaceID
    TabID      TabID
    PaneID     PaneID
    Label      string
    SearchText string
    Children   []WorkspaceTreeNode
}
```

```go
type WorkspaceTreeNodeKind string

const (
    WorkspaceTreeNodeWorkspace WorkspaceTreeNodeKind = "workspace"
    WorkspaceTreeNodeTab       WorkspaceTreeNodeKind = "tab"
    WorkspaceTreeNodePane      WorkspaceTreeNodeKind = "pane"
    WorkspaceTreeNodeCreate    WorkspaceTreeNodeKind = "create"
)
```

```go
type WorkspaceTreeRow struct {
    Node      WorkspaceTreeNode
    Depth     int
    Expanded  bool
    Match     bool
}
```

---

## 3. 树结构规则

### 3.1 层级

固定层级：

1. workspace
2. tab
3. pane

### 3.2 根节点

顶部始终保留：

- `+ create workspace`

随后是 workspace 根节点列表。

### 3.3 pane label 规则

pane 行的显示优先级：

1. connected terminal name
2. `unconnected pane`
3. `program exited pane`
4. `waiting slot`

---

## 4. 搜索规则

### 4.1 搜索范围

搜索必须覆盖：

- workspace name
- tab name
- pane label
- connected terminal name
- pane path

### 4.2 搜索行为

- 输入 query 后，不是只过滤 workspace 根节点
- 必须允许直接命中深层 pane
- 命中深层 pane 时，其祖先节点自动可见

### 4.3 路径搜索

支持类似：

- `prod-main`
- `prod-main/dev`
- `prod-main/dev/api-dev`

不要求完整路径匹配，但路径片段应优先命中。

---

## 5. 展开与折叠规则

### 5.1 默认展开

- 当前 active workspace 默认展开
- 当前 active tab 默认展开
- 其他 workspace 默认折叠

### 5.2 搜索时展开

- 当 query 非空且命中深层节点时，祖先路径自动展开
- 清空 query 后，恢复到“默认展开 + 用户手动展开”的合并状态

### 5.3 手动展开

键盘行为：

- `Right` 或 `l`
  - 展开当前 workspace/tab
- `Left` 或 `h`
  - 折叠当前 workspace/tab

pane 叶子节点不响应展开/折叠。

---

## 6. 选择与回车行为

### 6.1 create 行

当选中 `+ create workspace` 并回车：

- 进入 create workspace 流程

### 6.2 workspace 行

当选中 workspace 并回车：

- 切换到该 workspace
- 焦点落到该 workspace 当前 active pane

### 6.3 tab 行

当选中 tab 并回车：

- 切换到该 workspace
- 切换到该 tab
- 焦点落到该 tab 当前 active pane

### 6.4 pane 行

当选中 pane 并回车：

- 直接执行 jump
- 切换到目标 workspace
- 切换到目标 tab
- 聚焦目标 pane
- 关闭 workspace picker

这条行为是当前规范的关键要求。

---

## 7. 焦点规则

### 7.1 打开时

workspace picker 打开时：

- overlay 获得焦点
- 默认选中当前路径对应的最近节点

### 7.2 关闭时

- `Esc` 关闭 picker
- 焦点返回打开前的 pane

### 7.3 jump 后

- 如果跳到 floating pane，`FocusLayer = floating`
- 如果跳到 tiled pane，`FocusLayer = tiled`

---

## 8. 键盘映射

第一版必须支持：

- `Up` / `k`
  - 上移
- `Down` / `j`
  - 下移
- `Left` / `h`
  - 折叠
- `Right` / `l`
  - 展开
- `Enter`
  - open / jump
- `Esc`
  - close
- `Backspace`
  - 删除 query
- 普通字符
  - 追加 query

---

## 9. 视图规则

### 9.1 缩进

建议缩进：

- workspace: `0`
- tab: `2`
- pane: `4`

### 9.2 前缀符号

建议：

- workspace
  - `▾` / `▸`
- tab
  - `▾` / `▸`
- pane
  - `•`

### 9.3 当前路径高亮

当前 active workspace/tab/pane 路径应有弱高亮。

当前选中行应使用强高亮。

---

## 10. 必须有的单测

1. query 命中 pane 时祖先自动展开
2. tab 行回车后跳到 tab 的 active pane
3. pane 行回车后直接 jump 到 pane
4. 搜索清空后恢复默认展开状态
5. `Esc` 关闭后焦点恢复

---

## 11. 必须有的 E2E

1. 打开 workspace picker
2. 搜索命中某个 pane
3. 回车直接跳到该 pane
4. 焦点正确落在目标 pane
5. overlay 正常关闭，无残影

建议测试名：

- `TestE2ETUI_ScenarioWorkspacePickerJumpsDirectlyToPane`

---

## 12. AI 编码顺序

1. 先写树节点构建函数
2. 再写 `VisibleRows()` 投影函数
3. 再写 query/filter
4. 再写 keyboard reducer
5. 最后接 overlay view
