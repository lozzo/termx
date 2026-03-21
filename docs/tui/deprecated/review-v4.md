# TUI 设计四次评审

评审对象：`docs/tui/interaction-v2.md`（分层 prefix 系统），以及当前 `tui/` 实现代码与 v2 规格的对齐程度。

评审结论：**interaction-v2 的分层 prefix 设计方向正确，解决了 v1 的核心痛点。但当前实现仍然是 v1 的键位体系，v2 尚未落地。本轮评审重点是：v2 方案本身的质量评估 + 实现迁移的风险分析。**

## interaction-v2 解决了什么

### 1. 彻底消除了 Ctrl/Alt 组合键的兼容性风险

这是 v2 最大的贡献。review-v3 的 V11 指出 `C-a Ctrl-hjkl`、`C-a Alt-hjkl`、`C-a Ctrl-Arrow` 在不同终端里的可识别性不稳定。v2 的分层 prefix 把这些全部变成了子模式下的普通字母键：

```
v1: C-a Ctrl-h  → 平移 offset（Ctrl-h = Backspace 冲突）
v2: C-a v o → h  → 平移 offset（普通字母 h，零风险）

v1: C-a Alt-h   → 移动浮窗（Alt 的 ESC 前缀差异）
v2: C-a o → h   → 移动浮窗（普通字母 h，零风险）
```

这不是风格偏好，是工程可行性的根本改善。roadmap.md 里的 S4 spike（组合键可识别性验证）在 v2 下基本可以跳过了。

### 2. 浮窗操作从"每次都要 prefix"变成了 sticky 模式

v1 的浮窗操作每次都要 `C-a Alt-h`，连续移动 3 次就是 `C-a Alt-h C-a Alt-h C-a Alt-h`，9 次按键。v2 的 `C-a o → h h h → Esc` 只要 7 次，而且中间的 `h h h` 是连续的，手感好很多。

这个设计选择的理由也写得很清楚：浮窗操作通常是连续的。

### 3. 语义分组让键位更容易记忆

v1 的 30+ 个键全部挤在 `C-a` 下，没有分组。v2 按语义分成了 4 个子模式：

```
C-a 直接键  → 高频 viewport 操作（两次按键）
C-a t       → tab 管理（三次按键）
C-a w       → workspace 管理（三次按键）
C-a o       → floating 操作（sticky）
C-a v       → viewport 设置（三次按键）
```

高频操作不加按键成本，低频操作多一次按键但更好找。这个权衡是合理的。

### 4. review-v3 提的 `C-a _` 可发现性问题也顺带解决了

v1 的"降到最底层"绑定到 `C-a _`，review-v3 指出不够直觉。v2 把它移到了 `C-a o → [`，在 floating 子模式里 `]` 升 `[` 降，对称且直觉。

## v2 方案本身的问题

### 1. Offset Pan 模式的进入路径太深

`C-a v o` 进入 offset pan 模式，需要 3 次按键才能开始平移。对于"我想看一下 fixed viewport 左边的内容"这种操作，3 次按键 + 操作 + Esc 退出，总共至少 5 次按键。

v1 的 `C-a Ctrl-h` 虽然有兼容性问题，但只要 2 次按键就能平移一次。

这不是 blocker，但建议考虑：
- 如果 offset pan 的使用频率比预期高，可以给它一个更短的入口
- 或者在 `C-a v` 子模式里直接支持 `hjkl` 平移（不需要先按 `o` 进入 sticky 模式），只有需要连续平移时才用 `C-a v o`

### 2. `C-a t c` 和 `C-a c` 的关系需要明确

v2 把"新建 Tab"从 `C-a c` 移到了 `C-a t c`。但当前实现里 `C-a c` 仍然是新建 Tab 的入口（`input.go:111`）。

更重要的是：v2 的"未变更"列表里保留了 `C-a c` 作为直接键，但快捷键总览里 `C-a c` 没有出现在直接键列表中。对照表里写的是 `C-a c → C-a t c`。

这三处信息互相矛盾。需要明确：
- `C-a c` 是否保留为直接键（作为 `C-a t c` 的快捷方式）？
- 还是完全移除，只保留 `C-a t c`？

我的建议：保留 `C-a c` 作为直接键。新建 Tab 是高频操作，tmux 用户肌肉记忆很强，移除会造成不必要的迁移痛苦。

### 3. `C-a w` 的语义变了但没有充分说明迁移影响

v1 里 `C-a w` 是"新建浮动 Viewport"，v2 里变成了"workspace 管理子模式"。这是一个破坏性变更。

当前实现里 `C-a w` 仍然是打开浮动 terminal picker（`input.go:159`）。迁移时需要注意：
- 已有用户的肌肉记忆会被打破
- 新建浮窗从 `C-a w` 变成了 `C-a o n`，多了一次按键

建议在迁移时提供一个过渡期提示，或者在 help 里特别标注这个变更。

### 4. Workspace 子模式的 `C-a w d` 和 `C-a d` 容易混淆

`C-a d` 是 detach TUI（直接键），`C-a w d` 是删除 workspace。两个 `d` 在不同层级有完全不同的含义。

如果用户按 `C-a w` 后犹豫了一下（超时回到 Normal），再按 `d`，就会 detach 而不是删除 workspace。这个时序问题在 v1 里不存在。

建议把 workspace 删除改成 `C-a w x`（和 tab 关闭 `C-a t x` 对称），避免和 detach 混淆。

## 当前实现与 v2 规格的差距

### 已实现（v1 键位，仍在代码中）

对照 `input.go` 的 `handlePrefixEvent`：

```
v1 实现                          v2 规格
─────────────────────────────────────────────
C-a "/%/x/X/z                   不变 ✓
C-a h/j/k/l 导航                不变 ✓
C-a H/J/K/L 调整边界            不变 ✓
C-a {/} 交换                    不变 ✓
C-a Space 循环布局               不变 ✓
C-a 1-9/n/p tab 切换            不变 ✓
C-a f picker                    不变 ✓
C-a [/:/? 模式                  不变 ✓
C-a d detach                    不变 ✓
C-a C-a 发送原始                 不变 ✓
```

### 需要迁移的（v1 → v2）

```
v1 实现                          v2 目标
─────────────────────────────────────────────
C-a c (new tab)                 → C-a t c
C-a , (rename tab)              → C-a t ,
C-a & (close tab)               → C-a t x
C-a w (new floating)            → C-a o n
C-a W (toggle floating)         → C-a o v
C-a Tab (cycle float focus)     → C-a o Tab
C-a ] (raise z-order)           → C-a o ]
C-a _ (lower z-order)           → C-a o [
C-a Alt-hjkl (move float)       → C-a o hjkl
C-a Alt-HJKL (resize float)     → C-a o HJKL
C-a M (toggle fit/fixed)        → C-a v m
C-a R (toggle readonly)         → C-a v r
C-a P (toggle pin)              → C-a v p
C-a Ctrl-hjkl (pan offset)      → C-a v o → hjkl
```

### 需要新增的

```
子 prefix 分发逻辑：
  C-a t → 进入 tab 子模式（one-shot）
  C-a w → 进入 workspace 子模式（one-shot）
  C-a o → 进入 floating 子模式（sticky）
  C-a v → 进入 viewport 设置子模式（one-shot）

Floating sticky 模式：
  进入/退出状态管理
  Esc 退出回到 Normal
  状态栏切换显示

Offset Pan sticky 模式：
  C-a v o 进入
  0/$/g/G 跳转
  Esc 退出

Workspace 操作（尚未实现）：
  C-a w s (workspace picker)
  C-a w c (new workspace)
  C-a w r (rename workspace)
  C-a w d (delete workspace)
```

## 实现迁移建议

### 迁移策略

不建议一次性迁移所有键位。建议分两步：

**第一步：引入子 prefix 分发框架**

在 `handlePrefixEvent` 里加入 `t`/`w`/`o`/`v` 四个子 prefix 入口，进入对应的子模式处理函数。同时保留所有 v1 直接键作为兼容。

这一步的改动集中在 `input.go`，需要新增：
- `floatingMode bool` 字段（sticky 模式状态）
- `offsetPanMode bool` 字段（sticky 模式状态）
- `subPrefix rune` 字段（one-shot 子模式等待状态）
- 对应的 `handleTabSubPrefix`、`handleWorkspaceSubPrefix`、`handleFloatingMode`、`handleViewportSubPrefix`、`handleOffsetPanMode` 函数

**第二步：逐步移除 v1 直接键**

在子模式稳定后，逐步移除 v1 的直接键绑定（`C-a c`、`C-a w`、`C-a M` 等）。每移除一个，在 help 里标注新路径。

### 需要同步更新的文档

1. `wireframes.md` 的 Frame 13（Help 浮窗）仍然是 v1 键位，需要更新为 v2
2. `wireframes.md` 需要新增 Floating 子模式和 Offset Pan 模式的状态栏线稿
3. `verification.md` 的 V11 已经更新了 v2 的风险评估，但优先级表里 V11 的描述仍然写着"P0"然后说"降为 P2"，格式不一致

## 与其他文档的一致性

### interaction-v2.md 与 model.md

model.md 里的快捷键引用仍然是 v1 的：
- `C-a P`（model.md:329）→ v2 应该是 `C-a v p`
- `C-a R`（model.md:503）→ v2 应该是 `C-a v r`
- `C-a W`（model.md:396）→ v2 应该是 `C-a o v`

这些引用需要同步更新，否则实现者会困惑。

### interaction-v2.md 与 roadmap.md

roadmap.md 的"当前进展"里记录的是 v1 键位：
- `C-a M fit/fixed 切换` → v2 是 `C-a v m`
- `C-a P pin / unpin` → v2 是 `C-a v p`
- `C-a Ctrl-h/j/k/l 与 Ctrl-Arrow 平移 offset` → v2 是 `C-a v o → hjkl`
- `C-a R toggle readonly` → v2 是 `C-a v r`
- `C-a w 通过 chooser 创建 / attach floating viewport` → v2 是 `C-a o n`
- `C-a W toggle floating layer show/hide` → v2 是 `C-a o v`
- `C-a Tab cycle floating focus` → v2 是 `C-a o Tab`
- `C-a ] / C-a _ 调整 floating z-order` → v2 是 `C-a o ] / C-a o [`
- `C-a Alt-h/j/k/l 移动浮窗` → v2 是 `C-a o hjkl`
- `C-a Alt-H/J/K/L 调整浮窗大小` → v2 是 `C-a o HJKL`

roadmap 记录的是已完成的实现，所以这些 v1 键位是准确的。但需要在 roadmap 里加一条待办：迁移到 v2 键位体系。

## 我现在的判断

interaction-v2 作为设计文档，我给 **9.0/10**。

它解决了 v1 最大的工程风险（组合键兼容性），引入了合理的语义分组，sticky 模式的设计也很实用。剩下的问题（offset pan 路径深度、`C-a c` 去留、`C-a w d` 混淆）都是可以在实现过程中微调的，不影响整体架构。

但从"规格到实现"的角度看，当前代码和 v2 规格之间有一个不小的 gap。建议：

1. 明确 `C-a c` 是否保留为直接键
2. 把 `C-a w d` 改成 `C-a w x`
3. 同步 model.md、wireframes.md、roadmap.md 里的键位引用
4. 在 roadmap 里加入"迁移到 v2 键位体系"的里程碑
5. 考虑 offset pan 是否需要更短的入口

这些补完后，v2 就可以开始落地了。
