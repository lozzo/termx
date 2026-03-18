# 渲染架构

本文档定义 TUI 客户端的渲染管线：batching、dirty tracking、背压、viewport 裁剪、浮窗合成。

## 当前问题

```
当前渲染流程（每次 paneOutputMsg）：

  stream goroutine → paneOutputMsg → Update()
    → pane.VTerm.Write(data)
    → bubbletea 调用 View()
    → 重建所有 pane 的 cell grid
    → 合成整个 tab 的 canvas
    → 转成 ANSI 字符串
    → 输出到终端

问题：
  1. 每条 output 都触发一次完整 View()，高频输出时 View 调用远超屏幕刷新率
  2. 即使只有一个 pane 有新输出，所有 pane 的 grid 都被重建
  3. canvas → ANSI 字符串是全量转换，没有增量优化
```

## 目标渲染流程

> **待验证**：以下方案依赖 Bubble Tea 的特定行为假设，需先通过 [verification.md](verification.md) 中的 V1、V2 验证后再实现。

```
stream goroutine(s)                         bubbletea event loop
  │                                              │
  ├─ paneOutputMsg → Update()                    │
  │   └─ viewport.VTerm.Write(data)              │
  │   └─ viewport.dirty = true                   │
  │   └─ return nil (不触发 View)                 │
  │                                              │
  └─ ... 更多 output 继续累积 ...                  │
                                                 │
                           renderTickMsg (16ms) ──┤
                             └─ 检查任意 viewport.dirty?
                                 ├─ 有 → bubbletea 调 View()
                                 │   → 只重建 dirty viewport 的 grid
                                 │   → 合成 canvas
                                 │   → 输出 ANSI
                                 └─ 无 → return nil (跳过 View)
```

## Batching 机制

> **待验证 (V1)**：依赖 Bubble Tea 在 Update 返回 nil cmd 时跳过 View() 的假设。

### render tick

全局单一 render tick，所有 Viewport 共享。

```go
// Model.Init() 启动第一个 tick
func (m *Model) Init() tea.Cmd {
    return tea.Tick(m.renderInterval, func(time.Time) tea.Msg {
        return renderTickMsg{}
    })
}

// Update 处理
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {

    case paneOutputMsg:
        vp := m.findViewport(msg.viewportID)
        vp.VTerm.Write(msg.frame.Payload)
        vp.dirty = true
        return m, nil              // 关键：不触发 View

    case renderTickMsg:
        // 续签下一个 tick
        cmd := tea.Tick(m.renderInterval, func(time.Time) tea.Msg {
            return renderTickMsg{}
        })
        if m.anyDirty() {
            return m, cmd          // 有 dirty → bubbletea 会调 View()
        }
        return m, cmd              // 无 dirty → 跳过 View
    }
}
```

**tick 频率**：

| 场景 | 间隔 | 帧率 |
|------|------|------|
| 默认 | 16ms | ~60fps |
| 低性能 / SSH | 33ms | ~30fps |
| 可配置 | `:set render-interval 33` | |

## Dirty 跟踪

分两级，近期实现第一级，中期实现第二级。

### 第一级：Viewport 级 dirty（近期）

```
Viewport A: dirty=true   → 重建 cell grid
Viewport B: dirty=false  → 复用上一帧的 cellCache
Viewport C: dirty=true   → 重建 cell grid

renderTick:
  → 只重建 A 和 C 的 grid
  → B 直接 blit 缓存到 canvas
```

```go
type Viewport struct {
    // ...
    dirty     bool
    cellCache [][]drawCell    // 上一帧的 grid 缓存
}

func (m *Model) View() string {
    canvas := newCanvas(m.width, m.height)

    for _, vp := range m.visibleViewports() {
        rect := vp.rect
        if vp.dirty {
            vp.cellCache = vp.buildGrid(rect.W, rect.H)
            vp.dirty = false
        }
        canvas.drawGrid(rect.X, rect.Y, vp.cellCache)
        canvas.drawBorder(rect, vp.active)
    }

    return canvas.String()
}
```

### 第二级：行级 dirty（中期）

> **待验证 (V2)**：依赖 Bubble Tea renderer 的行级 diff 行为假设。

在 Viewport 级 dirty 基础上，进一步追踪哪些行变了。

```go
type Viewport struct {
    // ...
    dirty      bool
    dirtyLines []uint64       // bitset，bit i = 第 i 行 dirty
    prevCache  [][]drawCell   // 上上帧
    cellCache  [][]drawCell   // 上一帧
}

func (vp *Viewport) markDirtyLines() {
    if vp.prevCache == nil || len(vp.prevCache) != len(vp.cellCache) {
        vp.setAllLinesDirty()  // 尺寸变了，全部 dirty
        return
    }
    for y := range vp.cellCache {
        if !rowEqual(vp.prevCache[y], vp.cellCache[y]) {
            vp.setLineDirty(y)
        }
    }
}
```

canvas 合成时只更新 dirty 行：

```
canvas.String() 生成 ANSI 时：
  行 0: dirty  → 输出完整行内容
  行 1: clean  → cursor motion 跳过
  行 2: clean  → 跳过
  行 3: dirty  → 输出完整行内容
  ...

减少 ANSI 输出量，降低终端带宽压力
```

## Viewport 裁剪渲染

fixed 模式的 Viewport 需要裁剪 Terminal 的输出。

### fit 模式（简单）

```
Terminal PTY 80x24 = Viewport 80x24
  → 1:1 映射，直接渲染 VTerm 的完整 screen grid
```

### fixed 模式（裁剪）

```
Terminal PTY 120x40
Viewport 显示区域 60x20
Offset (30, 10)

Terminal screen:
     0         30        90       120
  0  ┌──────────┬─────────┬────────┐
     │          │         │        │
 10  │          ┌─────────┐        │
     │          │ Viewport│        │
     │          │ 60x20   │        │
     │          │ 只渲染   │        │
     │          │ 这个区域 │        │
 30  │          └─────────┘        │
     │          │         │        │
 40  └──────────┴─────────┴────────┘

buildGrid 时：
  for y := offset.Y; y < offset.Y + vp.H; y++ {
      for x := offset.X; x < offset.X + vp.W; x++ {
          grid[y-offset.Y][x-offset.X] = vtermScreen[y][x]
      }
  }
```

### 光标跟随

fixed 模式默认跟随光标：

```
光标移动到 (100, 35)
Viewport 大小 60x20
  → offset 自动调整，保证光标在可见区域内

计算逻辑：
  if cursor.X < offset.X           → offset.X = cursor.X
  if cursor.X >= offset.X + vp.W   → offset.X = cursor.X - vp.W + 1
  if cursor.Y < offset.Y           → offset.Y = cursor.Y
  if cursor.Y >= offset.Y + vp.H   → offset.Y = cursor.Y - vp.H + 1

锚定（pin）模式下不跟随，用户手动控制 offset
```

## 背压与退化

> **待验证 (V3)**：消息队列堆积行为和 SyncLost 恢复路径需压力测试验证。

### 问题

PTY 输出速度 > TUI 渲染速度时，paneOutputMsg 在 bubbletea 消息队列中堆积。

### 退化路径

```
正常路径                         退化路径
─────────                      ─────────
stream → VTerm.Write           stream channel 满
→ dirty = true                 → fanout 丢弃数据，计入 droppedBytes
→ renderTick 渲染               → fanout 下次 Broadcast 发送 StreamSyncLost
                                → 客户端收到 SyncLost
                                → 标记 viewport.syncLost = true
                                → renderTick 看到 syncLost：
                                    1. 请求 Snapshot
                                    2. 用 snapshot 重置本地 VTerm
                                    3. dirty = true, syncLost = false
                                    4. 下一个 renderTick full redraw
```

### 持续高频输出保护

```
如果某个 Viewport 连续 30 个 tick（~500ms）都是 dirty：
  → 该 Viewport 进入 "catching up" 状态
  → 降低渲染优先级：每隔一个 tick 才渲染
  → 状态栏显示 [catching up] 提示
  → 连续 5 个 tick 不再 dirty 后恢复正常
```

### 关键参数

| 参数 | 值 | 说明 |
|------|-----|------|
| fanout subscriber buffer | 256 | 服务端 per-subscriber buffer |
| renderTick 间隔 | 16ms | 客户端 render 频率 |
| catching up 阈值 | 30 ticks | 连续 dirty 触发降级 |
| SyncLost 恢复 | Snapshot | 请求完整屏幕状态 |

## 浮窗 z-order 渲染

### 近期：画家算法

从底到顶逐层绘制，后绘制的覆盖先绘制的。

```
渲染顺序：

  1. 平铺层
     ┌──────────┬──────────┐
     │ VP-A     │ VP-B     │
     │          ├──────────┤
     │          │ VP-C     │
     └──────────┴──────────┘

  2. 浮动层（按 z-order）
          ┌──────────────┐
          │ VP-D (z:1)   │  ← 覆盖平铺层的部分内容
          └──────────────┘
     ┌────────────┐
     │ VP-E (z:2) │  ← 覆盖 VP-D 的部分内容
     └────────────┘

canvas 上被遮挡的 cell 仍然被计算和写入（随后被覆盖）
对 1-3 个浮窗的典型场景，多余开销可忽略
```

### 中期：可见性 mask

如果浮窗数量或面积导致性能问题：

```go
// 维护 visibleMask，标记每个 cell 是否被上层遮挡
type visibleMask [][]bool

// 仅在浮窗打开/关闭/移动/resize 时重算
func (m *Model) rebuildVisibleMask() {
    // 初始化全部可见
    for y := range m.mask {
        for x := range m.mask[y] {
            m.mask[y][x] = true
        }
    }
    // 从最顶层浮窗开始，标记遮挡区域
    for i := len(floating) - 1; i >= 0; i-- {
        f := floating[i]
        for y := f.Y; y < f.Y+f.H; y++ {
            for x := f.X; x < f.X+f.W; x++ {
                m.mask[y][x] = false  // 被遮挡
            }
        }
    }
}

// 平铺层渲染时跳过被遮挡的 cell
if !m.mask[y][x] {
    continue  // 不写入 canvas，反正会被浮窗覆盖
}
```

## 与 Bubble Tea 的兼容性

### 近期能做的（不冲突）

| 优化 | 实现方式 |
|------|----------|
| render tick batching | `tea.Tick` 消息控制 View() 频率 |
| Viewport 级 dirty | View() 内部决定哪些 Viewport 重建 grid |
| 行级 cell-diff | canvas.String() 内部减少 ANSI 输出 |
| 浮窗画家算法 | View() 内部的 canvas 合成逻辑 |
| viewport 裁剪 | buildGrid 时按 offset 取子区域 |

### 近期不能做的（与 bubbletea 冲突）

| 优化 | 原因 |
|------|------|
| 终端原生 scroll/insert-line | bubbletea renderer 不支持增量输出 |
| 按行跳过 ANSI 输出 | bubbletea alt screen renderer 做行级字符串 diff，不支持 cursor motion |

### 中期演进

如果 bubbletea 的 View() → 全量字符串模式成为瓶颈：

```
保留 Bubble Tea 负责：状态管理、键位、模式切换
迁移到底层 writer：高频 Viewport 内容渲染

View() 只负责低频 UI（tab bar、status bar、help overlay）
Viewport 内容通过直接写终端的方式更新（绕过 bubbletea renderer）
```

## 服务端配合改动

### 1. fanout 数据竞争修复

`fanout.Broadcast()` 在 `RLock` 下修改 `sub.droppedBytes`。

修复：`droppedBytes` 从 `uint64` 改为 `atomic.Uint64`。

### 2. EventBus 数据竞争修复

`EventBus.Publish()` 在 `RLock` 下修改 `sub.dropped`。

修复：`dropped` 从 `int` 改为 `atomic.Int32`。

### 3. Resize 零值校验

`Terminal.Resize()` 接受 0×0 尺寸。

修复：入口校验 `cols > 0 && rows > 0`，否则返回 error。

### 4. readLoop 错误可观测性

`terminal.readLoop()` 静默吞掉非 EOF 错误。

修复：非 EOF 错误通过 EventBus 发布 warning 事件。
