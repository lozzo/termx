# TUIv2 渲染管线：剩余收尾任务

## 背景

tuiv2 的渲染管线重构已经完成核心部分：

- `Coordinator` 内部已全面切换到 `RenderVM`（`coordinator.go` 通过 `RenderVMFn` 拉取 VM）
- `RenderResult{Lines, Cursor, Blink}` 作为单一输出类型，双轨路径已合并
- Overlay opaque fast path 已实现（全遮罩 modal 跳过 body render）
- `paneEntriesForTab` 散参数已收进 `bodyProjectionOptions` struct

**剩余工作**：hit region API 和 `buildStatusHints` 还绑着旧的 `VisibleRenderState`，导致一条隐性的 round-trip 转换路径尚未清除。

---

## 当前的遗留结构

### 问题一：hit region API 还取 VisibleRenderState

以下公开函数的签名仍然是 `VisibleRenderState`：

```go
// tuiv2/render/hit_regions_workbench.go
func TabBarHitRegions(state VisibleRenderState) []HitRegion
func StatusBarHitRegions(state VisibleRenderState) []HitRegion

// tuiv2/render/hit_regions_overlay.go
func OverlayHitRegions(state VisibleRenderState) []HitRegion
func TerminalPoolHitRegions(state VisibleRenderState) []HitRegion
```

调用方（`app/` 内的鼠标处理代码）因此必须先把 VM 转回 VisibleRenderState：

```go
// tuiv2/app/render_state.go
func (m *Model) visibleRenderState() render.VisibleRenderState {
    return render.VisibleStateFromRenderVM(m.renderVM()) // ← 无意义的 round-trip
}

// 约 20 处调用点：update_mouse_click.go, update_mouse_wheel.go,
// update_mouse_pane_surface.go, update_helpers.go, update_mouse.go 等
state := m.visibleRenderState()
regions := render.TabBarHitRegions(state)
```

### 问题二：buildStatusHints 还取 VisibleRenderState

```go
// tuiv2/app/status_hints.go
func (m *Model) buildStatusHints(state render.VisibleRenderState) []string

// tuiv2/app/render_state.go:33
return render.WithRenderStatusHints(vm, m.buildStatusHints(render.VisibleStateFromRenderVM(vm)))
//                                                         ↑ VM 建好了又转回 State
```

`statusHintContext`（`status_hints.go:11-21`）内部持有 `state *render.VisibleRenderState`，但它实际用到的字段全部都在 `RenderVM` 里有对应。

### 问题三：statusBarCacheKey 仍有业务字段

```go
// tuiv2/render/coordinator.go:60-78
type statusBarCacheKey struct {
    FloatingTotal     int   // ← 业务状态
    FloatingCollapsed int   // ← 业务状态
    FloatingHidden    int   // ← 业务状态
    TerminalCount     int   // ← 业务状态
    SelectedTreeSig   string
    ...
}
```

这些字段要求 coordinator 直接读懂 workbench 的 floating 统计，不符合"coordinator 只做调度"的目标。

### 问题四：测试仍全部走兼容层

所有 `coordinator_test.go`、`compositor_test.go`、`overlays_test.go` 等测试文件用的是：

```go
NewCoordinator(func() VisibleRenderState { return state })
```

走兼容层 `RenderVMFromVisibleState(fn())`，没有直接构造 `RenderVM` 的测试。

---

## 目标态

完成后应达到：

1. `visibleRenderState()` 方法可以从 `app/` 中完全删除
2. `VisibleStateFromRenderVM()` 转换函数不再有生产调用方（可保留供测试，或一并删除）
3. hit region 函数签名接受 `RenderVM`（或其中具体的子 VM）
4. `buildStatusHints` 接受 `RenderVM` 而非 `VisibleRenderState`
5. `statusBarCacheKey` 中的业务字段从 coordinator 中挪走，由 VM 的 `StatusHintSig` 覆盖

---

## 任务清单

### Task A：迁移 hit region API 到 RenderVM

**涉及文件**：
- `tuiv2/render/hit_regions_workbench.go`
- `tuiv2/render/hit_regions_overlay.go`
- `tuiv2/render/hit_regions_workbench_test.go`
- `tuiv2/render/hit_regions_overlay_test.go`
- `tuiv2/app/update_mouse_click.go`
- `tuiv2/app/update_mouse_wheel.go`
- `tuiv2/app/update_mouse_pane_surface.go`
- `tuiv2/app/update_helpers.go`
- `tuiv2/app/update_mouse.go`
- `tuiv2/app/update_mouse_overlay.go`
- `tuiv2/app/e2e_test.go`
- `tuiv2/app/mouse_test.go`

**做法**：

将以下函数签名从 `VisibleRenderState` 改为 `RenderVM`：

```go
func TabBarHitRegions(vm RenderVM) []HitRegion
func StatusBarHitRegions(vm RenderVM) []HitRegion
func OverlayHitRegions(vm RenderVM) []HitRegion
func TerminalPoolHitRegions(vm RenderVM) []HitRegion
```

函数内部从 `vm.Workbench`、`vm.Overlay`、`vm.Surface`、`vm.TermSize` 取字段，替换原来的 `state.Workbench`、`state.Overlay` 等。

调用方改为：

```go
vm := m.renderVM()
regions := render.TabBarHitRegions(vm)
```

完成后：`m.visibleRenderState()` 在生产代码中的调用数量应降为 0。

**验收**：`go test ./...` 全部通过，`visibleRenderState()` 无生产调用。

---

### Task B：迁移 buildStatusHints 到 RenderVM

**涉及文件**：
- `tuiv2/app/status_hints.go`
- `tuiv2/app/render_state.go`
- `tuiv2/app/status_hints_test.go`

**做法**：

将 `buildStatusHints` 签名改为：

```go
func (m *Model) buildStatusHints(vm render.RenderVM) []string
```

将 `statusHintContext` 中的 `state *render.VisibleRenderState` 字段删除，改为直接从传入的 `vm.Workbench`、`vm.Overlay`、`vm.Status` 等取字段。

`render_state.go:33` 改为：

```go
return render.WithRenderStatusHints(vm, m.buildStatusHints(vm))
```

（不再需要 `VisibleStateFromRenderVM(vm)` 的 round-trip。）

**注意**：`statusHintContext` 内部用到哪些 `state` 字段，一一对应到 `RenderVM` 里的等价字段，不要遗漏。重点检查 `selectedTreeKind`、`activeRole`、`hasFloating` 这几个字段的来源。

**验收**：`go test ./app/...` 全部通过，`VisibleStateFromRenderVM` 无生产调用。

---

### Task C：清理 statusBarCacheKey 中的业务字段

**涉及文件**：
- `tuiv2/render/coordinator.go`

**做法**：

`statusBarCacheKey` 里的 `FloatingTotal`、`FloatingCollapsed`、`FloatingHidden`、`TerminalCount`、`SelectedTreeSig` 这些字段，其目的是让 status bar 在这些状态变化时重新渲染。

检查这些字段是否已经被 `RenderVM.Status.Hints`（`StatusHintSig`）覆盖：

- 如果 status hints 已经包含了这些状态的变化感知（即任何 floating count / terminal count 变化都会导致 hints 内容变化），则 `StatusHintSig` 已经隐含了这些字段，可以从 `statusBarCacheKey` 中删除。
- 如果 hints 不足以覆盖某个字段，需要先确认 `buildStatusHints` 是否应该感知这个状态，再决定是加进 hints 还是保留在 key 里（保留要有明确理由）。

目标：`statusBarCacheKey` 只保留直接影响 status bar **渲染输出**的字段，不保留业务统计字段。

**验收**：`go test ./render/...` 全部通过；`statusBarCacheKey` 中无 `FloatingTotal` 等业务统计字段。

---

### Task D：把关键测试迁移到直接构造 RenderVM

**涉及文件**：
- `tuiv2/render/coordinator_test.go`（选择性迁移，不要求全量）
- `tuiv2/render/overlays_test.go`
- `tuiv2/render/coordinator_overlay_blink_test.go`

**做法**：

对以下场景的测试，改用 `NewCoordinatorWithVM` + 直接构造 `RenderVM` 而非走兼容层：

1. overlay opaque fast path（全遮罩时不渲染 body）
2. overlay blink 行为
3. RenderResult cache 命中/未命中条件

其他大量使用 `NewCoordinator(VisibleStateFn)` 的测试可以保留，不强制全量迁移。

**验收**：新增的测试直接构造 `RenderVM`，不依赖 `VisibleRenderState`；`go test ./render/...` 全部通过。

---

## 执行顺序建议

```
Task B（status hints）→ Task A（hit regions）→ Task C（statusBarCacheKey）→ Task D（测试）
```

B 先于 A，因为 A 完成后 `visibleRenderState()` 就可以删除，B 如果还没迁移就会编译失败。

C 依赖 B 先完成（B 完成后 hints 内容会更全，才能判断 key 字段是否可删）。

D 最后，结构稳定后再补测试。

---

## 完成的判定标准

- `m.visibleRenderState()` 方法从生产代码中消失（可保留供测试辅助，但不应在 `update_*.go` 等路径调用）
- `render.VisibleStateFromRenderVM()` 无生产调用方
- `go test ./...` 全量通过
- `go build ./...` 无编译错误

---

## 重要约束

- **不要引入新的兼容层或双路径**：这次收尾的目标是消除 round-trip，不是新增适配代码。
- **不要修改 `VisibleRenderState` 本身的结构**：它仍作为测试构造入口使用，改动它会破坏大量测试。
- **不要在任务进行中创建 commit**，只有在用户明确要求时才提交。
- **不要修改 `bodyRenderCache`、`body_canvas_render.go` 的核心逻辑**：body cache 的 overlap 检测留在 canvas 层是合理的设计，不是这次的工作范围。
