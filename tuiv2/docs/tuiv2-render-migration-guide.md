# TUIV2 渲染层迁移指南

状态：Draft
日期：2026-03-30

---

## 1. 背景

当前 `tuiv2/render/coordinator.go` 的渲染实现是 Phase A 的临时骨架——纯手工拼接字符串，没有样式、没有动态尺寸、没有 compositor。这份文档指导如何从旧 `tui/` 复刻成熟的渲染能力到 `tuiv2/render/`。

### 1.1 当前 tuiv2 渲染状态

| 组件 | 状态 | 问题 |
|------|------|------|
| `renderHeader` | 占位 | 硬编码文本，没有 tab bar |
| `renderWorkbench` | 占位 | 纯文本 pane 列表，没有 cell compositor |
| `renderPaneBox` | 占位 | 固定宽度 box drawing，不响应终端尺寸 |
| `renderPickerOverlay` | 占位 | 硬编码 40 列框线，无搜索/样式/动态尺寸 |
| `snapshotPreview` | 半成品 | 能读 snapshot cells 但只取前 6 行 |

### 1.2 旧 tui/ 中已验证的渲染资产

| 文件 | 能力 |
|------|------|
| `tui/render.go` | Cell-based compositor、row cache、pane cache、viewport 裁剪、光标映射 |
| `tui/layout.go` | Pane 几何计算、split/adjust |
| `tui/picker.go` | Terminal picker：lipgloss 样式、搜索过滤、行缓存、居中 modal、动态宽高 |
| `tui/workspace_picker.go` | Workspace picker：同样的 modal 框架 |
| `tui/workbench.go` / `tui/workbench_view.go` | Tab bar 渲染、status bar 渲染 |

---

## 2. 迁移原则

1. **复制-改造，不 import**：从 `tui/` 复制代码到 `tuiv2/render/`，重组后断掉对旧包的依赖
2. **数据驱动**：所有渲染函数接收 `VisibleRenderState`（或其子结构），不访问 `app.Model` 内部状态
3. **纯函数优先**：渲染函数 `(state) -> string`，副作用归零
4. **分层复刻**：先 picker overlay，再 tab bar / status bar，最后 cell compositor

---

## 3. 渲染管线目标架构

```
VisibleRenderState
├── Workbench  *workbench.VisibleWorkbench
├── Runtime    *runtime.VisibleRuntime
├── Picker     *modal.PickerState          // terminal picker
├── WsPicker   *modal.WorkspacePickerState  // workspace picker（后续）
├── Prompt     *modal.PromptState           // 输入提示（后续）
└── TermSize   TermSize{Width, Height}      // 终端尺寸

RenderFrame():
  1. tabBar   := renderTabBar(state)        // 1 行
  2. body     := renderBody(state)           // height-2 行
  3. statusBar := renderStatusBar(state)     // 1 行
  4. if modal active:
       body = compositeModalOverlay(body, modal, termSize)
  5. return join(tabBar, body, statusBar)
```

### 3.1 关键区别

旧 `tui/` 的渲染直接访问 `Model` 上几十个字段。tuiv2 的渲染层只能通过 `VisibleRenderState` 拿到已投影的只读数据。这是架构约束，不可退让。

---

## 4. 分步迁移计划

### Phase R1：Picker Overlay（优先）

**目标**：用正式的居中 modal + lipgloss 样式替换 `renderPickerOverlay` 的硬编码框线。

#### 4.1 需要从 tui/ 复刻的代码

| 源文件 | 函数/逻辑 | 目标位置 | 改造要点 |
|--------|-----------|----------|----------|
| `tui/picker.go:1192-1229` | `renderCenteredPickerModal` | `tuiv2/render/overlays.go` | 去掉 `m.` 引用，改为接收 `PickerRenderInput` 参数 |
| `tui/picker.go:1231-1248` | `centeredPickerBorderLine` / `centeredPickerContentLine` | `tuiv2/render/overlays.go` | 原样复制，纯函数 |
| `tui/picker.go:1186-1189` | `centeredPickerInnerWidth` | `tuiv2/render/overlays.go` | 改为 `func pickerInnerWidth(termWidth int) int` |
| `tui/picker.go:392-417` | `terminalPickerItem.line()` | `tuiv2/modal/picker.go` | 复刻到 `PickerItem` 上，增加 `RenderLine(width int, selected bool) string` |
| `tui/picker.go:20-32` | lipgloss 样式常量 | `tuiv2/render/styles.go`（新建） | 原样复制 |

#### 4.2 需要扩展的 tuiv2 类型

**`tuiv2/modal/picker.go`** — 扩展 `PickerItem`：

```go
// 当前
type PickerItem struct {
    TerminalID string
    Name       string
    State      string
}

// 扩展为
type PickerItem struct {
    TerminalID  string
    Name        string
    State       string
    Command     string   // 显示用命令摘要
    Location    string   // 所在 workspace/tab 位置
    Observed    bool     // 是否已被某 pane 绑定
    Orphan      bool     // 是否无 pane 绑定
    CreateNew   bool     // "新建终端" 特殊行
    Description string   // CreateNew 行的描述
    CreatedAt   time.Time

    // 行渲染缓存（与旧 tui 同策略）
    lineBody  string
    lineWidth int
    lineNorm  string
    lineActive string
}

// 扩展 PickerState
type PickerState struct {
    Title    string
    Footer   string
    Items    []PickerItem
    Filtered []PickerItem  // 新增：过滤后列表
    Selected int
    Query    string
}
```

**`tuiv2/render/adapter.go`** — 扩展 `VisibleRenderState`：

```go
type VisibleRenderState struct {
    Workbench *workbench.VisibleWorkbench
    Runtime   *VisibleRuntimeStateProxy
    Picker    *modal.PickerState
    TermSize  TermSize  // 新增
}

type TermSize struct {
    Width  int
    Height int
}
```

#### 4.3 新建文件

**`tuiv2/render/styles.go`**：

从 `tui/picker.go:20-32` 复制 lipgloss 样式定义。这些是纯值常量，不依赖任何 tui 内部类型。

**`tuiv2/render/overlays.go`**：

picker overlay 的正式渲染实现。核心函数签名：

```go
// renderPickerOverlay 产出居中 modal 字符串，可直接合成到 body 上
func renderPickerOverlay(picker *modal.PickerState, termSize TermSize) string

// compositeOverlay 将 modal 字符串合成到 body 区域的中央
func compositeOverlay(body string, overlay string, termSize TermSize) string
```

#### 4.4 实现步骤

1. **新建 `tuiv2/render/styles.go`**，从 `tui/picker.go` 复制 lipgloss 样式
2. **扩展 `tuiv2/modal/picker.go`**：
   - 增加 `PickerItem` 字段
   - 增加 `Filtered` / `Title` / `Footer`
   - 从 `tui/picker.go` 复刻 `applyFilter()` 逻辑
   - 从 `tui/picker.go` 复刻 `line()` 渲染缓存逻辑（改名为 `RenderLine`）
3. **新建 `tuiv2/render/overlays.go`**：
   - 从 `tui/picker.go:1192-1248` 复刻 `renderCenteredPickerModal` 系列函数
   - 参数全部改为显式传入，不引用 `Model`
4. **扩展 `tuiv2/render/adapter.go`**：增加 `TermSize`
5. **修改 `tuiv2/render/coordinator.go`**：
   - `RenderFrame()` 调用新的 `renderPickerOverlay` 替换旧占位
   - modal overlay 合成到 body 区域中央
6. **写测试**：`tuiv2/render/overlays_test.go`

#### 4.5 从旧代码复刻时的关键改造

旧 `renderCenteredPickerModal` 的签名是：
```go
func (m *Model) renderCenteredPickerModal(title, query string, items []string, footer string) string
```

它内部访问了 `m.width`、`m.height`、`m.renderTabBar()`、`m.renderStatus()`。

tuiv2 版本必须：
- `width` / `height` 从 `TermSize` 参数取
- tab bar / status bar 由 `RenderFrame` 外部合成，overlay 函数只负责产出居中 card
- `lipgloss.Place` 居中逻辑保留，但不再拼接 tab bar / status bar

改造后的函数结构：
```go
func renderPickerCard(title, query string, items []string, footer string, width, height int) string {
    innerWidth := pickerInnerWidth(width)
    maxListHeight := max(4, min(10, height-8))
    listHeight := min(max(4, len(items)), maxListHeight)
    modalHeight := min(max(8, listHeight+4), max(8, height-2))
    listHeight = max(1, modalHeight-4)

    lines := make([]string, 0, modalHeight)
    lines = append(lines, borderLine("top", innerWidth, title))
    lines = append(lines, contentLine("", innerWidth))
    lines = append(lines, contentLine(queryStyle.Render(forceWidthANSI("search: "+query+"_", innerWidth)), innerWidth))
    for i := 0; i < listHeight; i++ {
        content := ""
        if i < len(items) {
            content = items[i]
        }
        lines = append(lines, contentLine(content, innerWidth))
    }
    lines = append(lines, contentLine("", innerWidth))
    lines = append(lines, contentLine(footerStyle.Render(forceWidthANSI(footer, innerWidth)), innerWidth))
    lines = append(lines, borderLine("bottom", innerWidth, ""))

    card := strings.Join(lines, "\n")
    return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
        lipgloss.WithWhitespaceChars(" "),
        lipgloss.WithWhitespaceBackground(lipgloss.Color("#020617")),
    )
}
```

#### 4.6 辅助函数复刻清单

从 `tui/picker.go` 和 `tui/model.go` 复刻以下纯函数（它们不依赖 Model）：

| 函数 | 作用 | 改造 |
|------|------|------|
| `forceWidthANSI(text, width)` | ANSI-aware 宽度填充/截断 | 原样复制 |
| `centeredPickerBorderLine` | 顶/底边框行 | 原样复制 |
| `centeredPickerContentLine` | 内容行（带左右 `│`） | 原样复制 |
| `terminalDisplayLabel` | 从 name/command 生成显示标签 | 原样复制 |
| `formatTerminalAge` | 时间间隔格式化 | 原样复制 |
| `formatTerminalTags` | tags 格式化 | 原样复制 |
| `terminalInfoStateLabel` | 状态标签 | 原样复制 |

---

### Phase R2：Tab Bar + Status Bar

**目标**：用正式的 tab bar 和 status bar 替换 `renderHeader` 的占位文本。

#### 4.7 从旧 tui/ 复刻

Tab bar 渲染逻辑主要在 `tui/workbench.go` 的 `renderTabBar` 和 `tui/model.go` 的 `renderStatus`。

| 源 | 目标 | 改造 |
|----|------|------|
| `renderTabBar` | `tuiv2/render/frame.go` `renderTabBar(state)` | 从 `VisibleWorkbench.Tabs` + `ActiveTab` 驱动 |
| `renderStatus` | `tuiv2/render/frame.go` `renderStatusBar(state)` | 从 `VisibleRenderState` 驱动 |

需要扩展 `VisibleRenderState`：

```go
type VisibleRenderState struct {
    // ...existing...
    Notice    string   // 临时通知
    Error     string   // 错误信息
    InputMode string   // 当前输入模式名称（prefix / resize / ...）
}
```

---

### Phase R3：Cell-Based Compositor

**目标**：复刻旧 `tui/render.go` 的 cell-based compositor，让 pane 内容能正确渲染全屏程序。

这是最大的复刻工作，涉及：

- Cell grid 分配与 pane 区域裁剪
- VTerm screen → cell grid 映射
- Row cache / pane cache / dirty tracking
- 光标位置映射
- Resize 时的 reflow

#### 4.8 前置条件

- `tuiv2/workbench/visible.go` 的 `VisiblePane.Rect` 已能提供正确的 pane 几何
- `tuiv2/runtime/visible.go` 的 `VisibleTerminal.Snapshot` 已能提供 screen cells
- `tuiv2/render/` 的 `TermSize` 已就位

#### 4.9 复刻策略

1. 从 `tui/render.go` 整体复制 compositor 核心到 `tuiv2/render/compositor.go`（新建）
2. 将 `Model` 引用全部替换为 `VisibleRenderState` + `TermSize`
3. 将 VTerm screen 读取改为从 `VisibleTerminal.Snapshot` 读取
4. 保留 row cache / pane cache 优化，但 cache 实例挂在 `Coordinator` 上而非 `Model` 上

---

### Phase R4：Workspace Picker + Prompt Modal

**目标**：将 workspace picker 和输入 prompt 的渲染也正式化。

这些复用 Phase R1 建立的 `renderPickerCard` 基础设施，区别只在 `PickerItem` 的内容和样式。

---

## 5. 依赖方向验证

迁移完成后的 `tuiv2/render/` 依赖图：

```
render/
├── imports modal       (PickerState, WorkspacePickerState)
├── imports workbench   (VisibleWorkbench, VisibleTab, VisiblePane, Rect)
├── imports runtime     (VisibleRuntime, VisibleTerminal)
├── imports protocol    (Snapshot, Cell — 只用于 compositor 读取)
├── imports lipgloss    (样式)
├── imports x/ansi      (ANSI 字符串宽度)
└── does NOT import tui (硬约束)
```

---

## 6. 文件清单

Phase R1 完成后新增/修改的文件：

```
tuiv2/render/styles.go         # 新建：lipgloss 样式定义
tuiv2/render/overlays.go       # 新建：picker overlay 渲染
tuiv2/render/overlays_test.go  # 新建：overlay 测试
tuiv2/render/coordinator.go    # 修改：调用新 overlay
tuiv2/render/adapter.go        # 修改：增加 TermSize
tuiv2/modal/picker.go          # 修改：扩展 PickerItem/PickerState
tuiv2/modal/picker_render.go   # 新建：PickerItem.RenderLine 渲染缓存
```

Phase R2 新增：

```
tuiv2/render/frame.go          # 修改：tab bar + status bar
```

Phase R3 新增：

```
tuiv2/render/compositor.go     # 新建：cell-based compositor
tuiv2/render/compositor_test.go
tuiv2/render/cache.go          # 修改：row cache / pane cache
```

---

## 7. 验收标准

### Phase R1
- [ ] `renderPickerOverlay` 产出带 lipgloss 样式的居中 modal
- [ ] modal 宽度自适应终端宽度（min 54, max 84, 响应窄终端）
- [ ] 搜索过滤可用（输入 query → Filtered 更新 → 渲染更新）
- [ ] 选中行有明显视觉区分
- [ ] CreateNew 行有特殊样式
- [ ] `tuiv2/render/` 无任何对 `tui/` 的 import

### Phase R2
- [ ] tab bar 显示所有 tab，active tab 有视觉区分
- [ ] status bar 显示 workspace 名、输入模式、通知/错误

### Phase R3
- [ ] pane 内容通过 cell-based compositor 渲染
- [ ] 全屏程序（htop、vim、less）在 resize 时正确 reflow
- [ ] 光标位置正确映射到宿主终端
