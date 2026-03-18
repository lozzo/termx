# 虚拟终端（VTerm）

VTerm 解析 PTY 输出的 ANSI 转义序列，维护一个内存中的屏幕缓冲区。它是快照和重连恢复的基础。

## 职责

1. **解析 PTY 输出**：处理所有 ANSI/VT100/xterm 转义序列
2. **维护屏幕缓冲区**：主屏幕（main screen）和备用屏幕（alternate screen）
3. **跟踪终端状态**：光标位置、文字属性、终端模式
4. **管理滚动回看**：scrollback buffer（主屏幕溢出的历史行）
5. **提供快照数据**：供 Snapshot 模块序列化

## 包结构

```
vterm/
├── vterm.go      # VTerm 类型，封装底层库
└── snapshot.go   # 快照数据提取
```

## 库选型

### 结论：使用 `charmbracelet/x/vt`

经过对 Go 生态中主要虚拟终端库的调研，选择 `charmbracelet/x/vt` 作为底层实现。

| 库 | 屏幕缓冲区 | Cell 模型 | Scrollback | 维护状态 |
|---|---|---|---|---|
| **charmbracelet/x/vt** | Grid of Cell | grapheme + style + link | 内置，可配置上限 | 活跃（2026-03） |
| ActiveState/vt10x | Grid of glyph | rune + fg/bg | 可选，无上限 | 停滞（2023-12） |
| ricochet1k/termemu | Span-based lines | span + style | 需自行实现 | 活跃（2026-03） |
| danielgatis/go-vte | 无（纯解析器） | N/A | N/A | 半活跃 |

选择理由：

1. **功能最完整**——内置 scrollback（可配上限）、备用屏幕、grapheme cluster、24-bit 颜色、超链接、脏行追踪
2. **API 完美匹配 termx 模型**——`NewSafeEmulator(w, h)` 无需真实终端（headless），`Write()` 喂 PTY 输出，`CellAt()` / `String()` / `Render()` 读取状态
3. **线程安全**——`SafeEmulator` 内置 `sync.RWMutex`，与 termx 的并发需求天然契合
4. **Charm 团队活跃维护**——同一团队开发 bubbletea/lipgloss/TUIOS，生态兼容性好
5. **Callbacks 机制**——标题变更、bell、光标移动、备用屏幕切换等事件可直接映射到 termx 事件

已知风险：
- 需要 Go 1.24+（tgent-go 目前使用的版本需确认）
- 在 `charmbracelet/x` 实验命名空间下，API 可能变化
- 依赖链较重（ultraviolet、ansi 等）

备选方案：`ricochet1k/termemu`——Go 版本要求更低，但需自行实现 scrollback，随机 Cell 访问不如 grid 模型方便。

## 封装层

termx 不直接暴露 `charmbracelet/x/vt` 的类型，而是封装一层薄接口，隔离底层依赖：

```go
package vterm

import (
    "github.com/charmbracelet/x/vt"
)

// VTerm 封装虚拟终端模拟器
type VTerm struct {
    emu *vt.SafeEmulator
}

func New(cols, rows int, scrollbackSize int) *VTerm {
    emu := vt.NewSafeEmulator(cols, rows)
    emu.SetScrollbackSize(scrollbackSize)
    return &VTerm{emu: emu}
}

// Write 处理 PTY 输出（实现 io.Writer）
func (v *VTerm) Write(data []byte) (int, error) {
    return v.emu.Write(data)
}

// Resize 调整终端尺寸
func (v *VTerm) Resize(cols, rows int) {
    v.emu.Resize(cols, rows)
}

// CellAt 读取指定位置的 Cell
func (v *VTerm) CellAt(x, y int) Cell {
    c := v.emu.CellAt(x, y)
    return convertCell(c) // 转换为 termx 的 Cell 类型
}

// ScreenContent 返回当前屏幕内容（快照用）
func (v *VTerm) ScreenContent() ScreenData {
    // 遍历 emu 的 rows × cols，提取所有 Cell
    // 返回 termx 内部类型，不暴露 charmbracelet 类型
}

// ScrollbackContent 返回 scrollback 内容
func (v *VTerm) ScrollbackContent() [][]Cell {
    // 遍历 emu.ScrollbackLen()，提取所有行
}

// CursorState 返回光标状态
func (v *VTerm) CursorState() CursorState {
    pos := v.emu.CursorPosition()
    return CursorState{
        Row:     pos.Y,
        Col:     pos.X,
        Visible: v.emu.CursorVisible(),
    }
}

// IsAltScreen 是否在备用屏幕
func (v *VTerm) IsAltScreen() bool {
    return v.emu.IsAltScreen()
}
```

### 为什么要封装

- **隔离依赖**：如果未来需要更换底层库（如从 charmbracelet 切换到自行实现），只需修改 vterm 包
- **类型统一**：termx 的 Cell、CursorState、ScreenData 使用自己的类型定义，不暴露第三方类型
- **API 简化**：只暴露 termx 需要的操作，隐藏底层库的复杂性

## 核心数据结构

### Cell（单元格）

```go
type Cell struct {
    Content string    // 字符内容（支持 grapheme cluster，如 emoji 组合）
    Width   int       // 显示宽度（1 或 2）
    Style   CellStyle // 文字样式
}

type CellStyle struct {
    FG        Color // 前景色
    BG        Color // 背景色
    Bold      bool
    Italic    bool
    Underline bool
    Blink     bool
    Reverse   bool
    Strikethrough bool
}
```

注意 `Content` 是 `string` 而非 `rune`——现代终端需要支持 grapheme cluster（如 emoji 组合字符 👨‍👩‍👧‍👦），单个 rune 无法表达。

### CursorState

```go
type CursorState struct {
    Row     int
    Col     int
    Visible bool
    Shape   CursorShape // block, underline, bar
}
```

### TerminalModes

```go
type TerminalModes struct {
    AlternateScreen   bool
    MouseTracking     bool
    BracketedPaste    bool
    ApplicationCursor bool
    AutoWrap          bool
}
```

## 事件回调

通过 `charmbracelet/x/vt` 的 Callbacks 机制捕获终端状态变化：

```go
func New(cols, rows int, scrollbackSize int) *VTerm {
    emu := vt.NewSafeEmulator(cols, rows)
    emu.SetScrollbackSize(scrollbackSize)

    v := &VTerm{emu: emu}

    // 注册回调
    emu.SetCallbacks(vt.Callbacks{
        Bell:              func() { v.onBell() },
        Title:             func(title string) { v.onTitleChange(title) },
        AltScreen:         func(on bool) { v.onAltScreenChange(on) },
        CursorVisibility:  func(visible bool) { v.onCursorVisibilityChange(visible) },
    })

    return v
}
```

这些回调可以向上传播给 Terminal，最终作为事件通知客户端。

## 滚动回看缓冲区

- 默认最大 10000 行（通过 `CreateOptions.ScrollbackSize` 配置）
- 只在主屏幕模式下累积（备用屏幕没有 scrollback）
- 当 scrollback 超过最大值时，底层库自动移除最旧的行

## 并发安全

- 使用 `vt.SafeEmulator`，所有操作内置 `sync.RWMutex`
- `Write` 获取写锁
- `CellAt`、`ScreenContent` 等读取操作获取读锁
- 快照操作需要短暂持有读锁，快照数据是深拷贝

## 数据流架构

VTerm **不在输出分发的关键路径上**。数据流如下：

```
PTY read goroutine:
  buf := pty.Read()
  fanout.Broadcast(buf)   // 1. 先分发给所有订阅者（低延迟）
  vterm.Write(buf)         // 2. 再异步喂给 VTerm 解析（仅维护屏幕状态）
```

VTerm 的唯一消费者是 `Snapshot()`。订阅者收到的是 PTY 原始字节，由客户端自己解析 ANSI 序列。这样即使 VTerm 解析较慢（如大量滚动输出），也不影响订阅者的实时性。

## 性能考虑

- **不阻塞分发**：VTerm 解析在 Fan-out 之后，不影响订阅者延迟
- **批量处理**：`Write` 一次处理整个 `[]byte`，不是逐字节调用
- **脏行追踪**：`charmbracelet/x/vt` 的 `Touched()` 返回脏行列表，未来可用于增量快照
- **Scrollback 上限**：防止长时间运行的进程占用过多内存

## 相关文档

- [Terminal 模型](spec-terminal.md) — VTerm 是 Terminal 的组成部分
- [快照](spec-snapshot.md) — 基于 VTerm 数据的快照序列化
- [PTY 管理](spec-pty-manager.md) — VTerm 的输入来源
