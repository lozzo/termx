
# 临时废弃,勿读
# TUI 客户端

termx 内置一个全功能 TUI 终端复用器，基于 [bubbletea](https://github.com/charmbracelet/bubbletea) 框架。它通过 Unix socket 连接本地 termx daemon，提供分屏布局、多 tab、workspace 管理和浮动面板等功能。

与 tmux/zellij 的关键差异：布局完全在客户端侧实现，服务端只有扁平的 Terminal 池。这意味着不同的 TUI 客户端可以对同一组 terminal 有不同的视图，且 workspace 配置可以声明式定义、版本控制。

## 当前评审结论（2026-03-18）

基于对 tmux 和 zellij 实现模式的复盘，termx 当前 TUI 的主要性能风险已经明确：

- 问题重点不在协议层 JSON，而在客户端渲染热路径
- 分屏/浮窗并不会天然导致性能差，真正的问题是**是否仍然用整屏全量重绘**
- 如果继续沿用“每次输出都驱动整 tab 合成，再整体转 ANSI 字符串”的模式，pane 数量和 overlay 层数一上来，性能会快速恶化

因此，本 spec 对 TUI 的目标补充如下：termx 必须逐步从“UI 框架驱动的整屏字符串渲染”演进到“screen/grid + dirty region + batching + backpressure”的渲染模型。

## 核心概念

```
Workspace (≈ tmux session)
  └─ Tab (≈ tmux window)
       └─ Layout Tree (binary split tree)
            └─ Pane (= termx Terminal)
                 + Floating Panes (overlay)
```

- **Workspace**：命名的工作空间，包含多个 Tab。可在 workspace 间快速切换。
- **Tab**：一个标签页，包含一个分屏布局树。Tab 有名称和编号。
- **Pane**：布局树的叶节点，对应一个 termx Terminal。支持水平/垂直分割。
- **Floating Pane**：悬浮在当前 Tab 上方的临时终端窗口，可移动、调整大小。

所有概念都是 TUI 客户端的视图逻辑，服务端对此一无所知。

## 渲染架构原则

TUI 的实现允许继续使用 Bubble Tea 管理状态、快捷键和模式切换，但**高频终端内容渲染**必须遵守下面的原则。

### 1. Pane 是独立渲染单元

- 每个 Pane 维护自己的本地 VT/screen/grid
- 终端输出先更新 pane 自身状态，再决定是否需要触发界面刷新
- 分屏、tab、浮窗都只是这些 pane 的组合和裁剪，不应改变服务端模型

### 2. 以 dirty 信息驱动绘制

- 最小更新单元优先为 dirty line，其次是 dirty rect
- 允许 pane 级 full redraw，但不能把“整个 tab 重绘”当作常态
- 浮窗和边框也应具备自己的 dirty 区域，而不是每次都重算完整背景

### 3. 批量刷新而不是逐字节刷新

- PTY 输出不应一帧一帧直达最终 View
- 客户端需要在短时间窗口内做合并（例如 16ms 或 33ms 一次 flush）
- resize、全屏程序切换、批量输出时应优先选择“延迟一个 tick 统一刷新”

### 4. 必须有背压退化路径

- 当客户端 renderer 跟不上输出速度时，允许跳过中间增量
- 在必要时退化为“标记 SyncLost/dirty-all，再做一次最新状态 full redraw”
- 慢终端、SSH 链路或复杂布局不能把 TUI 拖入无限追帧状态

### 5. 尽量利用终端原生能力

- 优先考虑 scroll、insert/delete line、cursor motion、synchronized updates 等终端能力
- 避免在逻辑上只是滚动一屏内容时，退化为逐 cell 全量重发

## 参考实现带来的具体启发

### tmux 的启发

- redraw 是分层和可延期的：pane、window、client 分开判断
- 输出缓冲未排空时会 defer redraw，而不是继续制造更多 redraw
- 极端情况下允许丢弃中间输出，等缓冲排空后 full redraw
- 会尽量利用终端原生编辑能力，而不是只做字符级重画

### zellij 的启发

- 内部维护 grid 和 output buffer，优先按 changed chunks 输出
- 复杂 UI（边框、浮窗、fake cursor）也是在 chunk/裁剪模型上叠加
- 即便 UI 功能较多，热点仍然是 dirty region 管理，而不是控制消息编码格式

这些结论适用于 termx，且与 termx 的“服务端扁平、客户端自由组织”哲学并不冲突。

## 界面布局

```
┌─ Tab bar ────────────────────────────────────────┐
│  [1:editor] [2:build] [3:logs]  ws:dev           │
├──────────────────────────┬───────────────────────┤
│                          │                       │
│  ~/project $ vim .       │  ~/project $ make     │
│  ...                     │  building...          │
│  ...                     │  [OK] done            │
│                          │                       │
│                          ├───────────────────────┤
│                          │                       │
│                          │  $ tail -f app.log    │
│                          │  ...                  │
│                          │                       │
├──────────────────────────┴───────────────────────┤
│  [ws:dev] 1:editor | 2:build* | 3:logs     C-a ? │
└──────────────────────────────────────────────────┘
```

- **顶部 Tab 栏**：显示当前 workspace 所有 tab，高亮活跃 tab，右侧显示 workspace 名称
- **中间区域**：分屏布局（二叉树分割），活跃 pane 边框高亮
- **底部状态栏**：workspace 名、tab 列表缩略、当前 pane 信息、快捷键提示

## 界面模式

TUI 有四个主要模式：

| 模式 | 说明 | 进入方式 |
|------|------|----------|
| Normal | 键盘输入发送给当前 pane | 默认模式 |
| Copy/Scroll | 浏览 scrollback、选择文本 | `C-a [` |
| Command | 类 vim 命令行输入 | `C-a :` |
| Picker | 交互式列表选择（workspace/terminal） | `C-a s` / `C-a f` |

## Prefix Key

**默认 prefix key: `Ctrl-a`**

选择理由：
- 左手单手可按（Ctrl + A），Mac/Linux 通用
- GNU Screen 用户熟悉
- 与 tmux 的 `Ctrl-b` 区分，可无冲突共存

冲突处理：
- readline 的行首跳转被占用 → 按两次 `Ctrl-a Ctrl-a` 发送原始 `Ctrl-a` 给 terminal
- 可通过配置文件修改 prefix key

## 快捷键

所有快捷键均为 prefix key + 后续按键的组合。

### Pane 操作

| 快捷键 | 动作 |
|---------|------|
| `C-a "` | 水平分割（上下） |
| `C-a %` | 垂直分割（左右） |
| `C-a h/j/k/l` | 在 pane 间导航（vim 方向键） |
| `C-a ←/↓/↑/→` | 在 pane 间导航（方向键） |
| `C-a x` | 关闭当前 pane（kill terminal） |
| `C-a z` | 当前 pane 全屏切换（zoom toggle） |
| `C-a Space` | 循环切换预定义布局（even-horizontal → even-vertical → main-horizontal → main-vertical → tiled） |
| `C-a {` | 当前 pane 与前一个 pane 交换位置 |
| `C-a }` | 当前 pane 与后一个 pane 交换位置 |
| `C-a H/J/K/L` | 调整 pane 边界（大写 = resize，每次 2 行/列） |

### 浮动面板

| 快捷键 | 动作 |
|---------|------|
| `C-a w` | 弹出浮动终端（居中，默认 80% 宽高） |
| `C-a W` | 切换所有浮动面板可见性 |

### Tab 操作

| 快捷键 | 动作 |
|---------|------|
| `C-a c` | 新建 tab |
| `C-a ,` | 重命名当前 tab |
| `C-a 1-9` | 跳转到第 N 个 tab |
| `C-a n` | 下一个 tab |
| `C-a p` | 上一个 tab |
| `C-a &` | 关闭当前 tab（需确认，会 kill 所有 pane） |

### Workspace 操作

| 快捷键 | 动作 |
|---------|------|
| `C-a s` | 列出 workspace（交互式 picker 选择） |
| `C-a $` | 重命名当前 workspace |
| `C-a d` | Detach（退出 TUI，所有 terminal 继续运行） |

### Copy/Scroll 模式

| 快捷键 | 动作 |
|---------|------|
| `C-a [` | 进入 copy/scroll 模式 |

进入后的按键（不需要 prefix）：

| 按键 | 动作 |
|------|------|
| `j`/`k` 或 `↑`/`↓` | 上下滚动 |
| `Ctrl-u` / `Ctrl-d` | 半页滚动 |
| `g` | 跳到 scrollback 顶部 |
| `G` | 跳到底部（最新输出） |
| `v` | 开始选择 |
| `y` | 复制选中文本到系统剪贴板 |
| `/` | 向下搜索 |
| `?` | 向上搜索 |
| `n` / `N` | 跳转到下一个/上一个搜索匹配 |
| `q` 或 `Esc` | 退出 copy/scroll 模式 |

### 其他

| 快捷键 | 动作 |
|---------|------|
| `C-a ?` | 显示快捷键帮助（浮动面板） |
| `C-a :` | 进入命令模式 |
| `C-a C-a` | 发送原始 `Ctrl-a` 给 terminal |
| `C-a f` | 模糊搜索所有 terminal（跨 workspace） |

## 浮动面板（Floating Pane）

浮动面板是覆盖在当前 tab 布局之上的独立终端窗口。

### 行为

- `C-a w` 创建一个新的浮动面板，居中显示，默认 80% 宽高
- 浮动面板有边框和标题栏（显示 terminal 命令/名称）
- 多个浮动面板可堆叠，最后创建的在最前面
- `C-a W` 切换所有浮动面板的显示/隐藏（toggle）
- 关闭浮动面板中的 terminal（进程退出或 `C-a x`）时面板自动消失
- 焦点在浮动面板时，pane 导航键（`C-a h/j/k/l`）不生效；按 `Esc` 将焦点返回到下方的布局 pane

### 鼠标操作

- 拖动标题栏移动面板位置
- 拖动面板边缘调整大小
- 点击面板切换焦点

### 用途

- 临时查看日志、运行一次性命令
- 快速查看帮助文档
- 不打断当前布局的情况下执行临时操作

## 分屏与浮窗的性能约束

termx 支持分屏和浮窗，但实现时必须满足以下约束，否则功能越多越容易放大卡顿：

- **分屏**：活跃 pane 输出不应导致所有兄弟 pane 重建内容缓存
- **浮窗**：overlay 必须支持 z-order 裁剪，避免每次都完整重绘被遮挡区域
- **边框/标题栏**：这些 UI 元素应当是轻量附加层，不应成为主要重绘成本
- **resize**：拖动窗口大小时应采用“事务式 resize + 单次刷新”，避免中间态撕裂
- **全屏程序**：`vim`、`less`、`htop`、`top` 等场景必须优先保证正确性和稳定性，其次才是装饰性 UI

换句话说，分屏和浮窗不是问题本身；“以全量重绘方式实现分屏和浮窗”才是问题。

## 鼠标支持

默认启用，可通过配置关闭。

| 操作 | 效果 |
|------|------|
| 点击 pane | 切换焦点到该 pane |
| 拖动 pane 边界 | 调整 pane 分割比例 |
| 点击 tab 栏 | 切换到对应 tab |
| 拖动浮动面板标题栏 | 移动浮动面板 |
| 拖动浮动面板边缘 | 调整浮动面板大小 |
| 鼠标滚轮 | 进入 scrollback 模式并滚动 |

## 命令模式

按 `C-a :` 进入，类似 vim 的命令行输入。Tab 自动补全可用。

### 命令列表

```
:new-window [-n name] [command]     新建 tab，可选指定名称和命令
:split -h                           水平分割（上下）
:split -v                           垂直分割（左右）
:rename-window <name>               重命名当前 tab
:rename-session <name>              重命名当前 workspace
:save-layout <name>                 保存当前布局为配置文件
:load-layout <name>                 加载布局配置
:set <option> <value>               运行时修改配置（如 :set mouse off）
:kill-pane                          关闭当前 pane
:kill-window                        关闭当前 tab
:resize-pane -D/-U/-L/-R [n]       调整 pane 大小（方向 + 行/列数）
:select-pane -D/-U/-L/-R           选择相邻 pane
:swap-pane -D/-U                   交换 pane 位置
:list-terminals                     列出所有 terminal（来自 server）
:attach <terminal-id>               将已有 terminal 放入当前 pane
```

## 推荐演进路线

### 近期（在现有实现上继续优化）

- 保留当前服务端、协议和本地 VT 架构
- 引入 pane output batching
- 引入 dirty pane / dirty line
- 为 resize 增加统一刷新阶段
- 增加 renderer 背压策略

### 中期（如现有 UI 框架热路径仍不足）

- 保留 Bubble Tea 负责状态管理、键位和模式
- 将高频 pane 区域渲染迁移到更底层的终端 writer 或 `tcell` 类 renderer
- 把 `View()` 产整屏字符串的模式限制在低频 UI 区域，而不是 pane 热路径

termx 的目标不是成为”功能很多但 pane 一多就卡的 TUI”，而是成为一个真正可承载终端工作流的客户端。

## 渲染管线实现细节

本节补充前面渲染架构原则的具体实现方案，解决”怎么做”的问题。

### Batching 机制

**问题**：当前每收到一条 `paneOutputMsg`，bubbletea 就调用一次 `View()`，导致高频输出时 View 调用远超屏幕刷新率。

**方案**：全局单一 render tick，所有 pane 共享。

```
stream goroutine(s)                         bubbletea event loop
  │                                              │
  ├─ paneOutputMsg → Update()                    │
  │   └─ pane.VTerm.Write(data)                  │
  │   └─ pane.dirty = true                       │
  │   └─ return nil (不触发 View)                 │
  │                                              │
  └─ ... 更多 output 继续累积 ...                  │
                                                 │
                           renderTickMsg (16ms) ──┤
                             └─ 检查任意 pane.dirty?
                                 ├─ 有 → return
                                 │   → bubbletea 调 View()
                                 │   → 只重建 dirty pane 的内容
                                 └─ 无 → return nil (跳过 View)
```

**关键设计点**：

1. **tick 频率**：默认 16ms（~60fps）。可配置为 33ms（~30fps）用于低性能场景或 SSH 链路。
2. **tick 在哪层**：`Model.Init()` 启动 `tea.Tick(16ms, renderTickMsg)`，每次 renderTickMsg 在 Update 中续签下一个 tick。
3. **paneOutputMsg 不再触发 View**：Update 处理 paneOutputMsg 时只写入 VTerm 并标记 dirty，返回 `nil` cmd（不是 tea.Tick、不是 tea.Batch），bubbletea 不会调用 View。
4. **renderTickMsg 触发条件 View**：Update 处理 renderTickMsg 时，检查是否有任何 dirty pane。如果有，触发 `m.needsRender = true` 并返回；bubbletea 的 View() 中检查此标志决定是否重建内容。如果没有 dirty pane，View() 返回上次缓存的字符串。

```go
// 核心流程伪代码
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case paneOutputMsg:
        pane := findPane(msg.paneID)
        pane.VTerm.Write(msg.frame.Payload)
        pane.dirty = true
        return m, nil  // 不触发 View

    case renderTickMsg:
        cmd := tea.Tick(m.renderInterval, func(time.Time) tea.Msg {
            return renderTickMsg{}
        })
        if m.anyDirty() {
            // bubbletea 会在 Update 返回后调用 View()
            return m, cmd
        }
        // 没有 dirty pane，跳过这次 View
        return m, cmd
    }
}
```

### Dirty 跟踪策略

**问题**：`charmbracelet/x/vt` 不暴露行级 dirty 信息，无法直接知道哪些行变了。

**方案**：客户端侧 cell-diff，分两级。

#### 第一级：Pane 级 dirty（近期）

- 每个 Pane 维护 `dirty bool`
- paneOutputMsg → `dirty = true`
- renderTick 时只重建 dirty pane 的 cell grid，非 dirty pane 复用上一帧的 cellCache
- canvas 合成时，非 dirty pane 直接 blit 缓存内容到 canvas

这已经比当前实现好很多：当前即使 pane 没有新输出，View() 仍然每次重建所有 pane 的 grid。

#### 第二级：行级 dirty（中期）

- Pane 额外维护 `dirtyLines bitset`（一个 `[]uint64`，每 bit 代表一行）
- paneCells() 重建 grid 时，对比新旧 cellCache，记录哪些行发生了变化
- canvas 合成时，只有 dirtyLines 标记的行才更新到 canvas
- canvas.String() 生成 ANSI 时，只输出变化的行（通过 cursor motion 跳过未变行）

```go
type Pane struct {
    // ... 现有字段
    dirty       bool
    dirtyLines  []uint64   // bitset, bit i = line i dirty
    prevCache   [][]drawCell
    cellCache   [][]drawCell
}

func (p *Pane) markDirtyLines() {
    if p.prevCache == nil || len(p.prevCache) != len(p.cellCache) {
        // 尺寸变了或首次渲染，全部 dirty
        p.setAllLinesDirty()
        return
    }
    for y := range p.cellCache {
        if !rowEqual(p.prevCache[y], p.cellCache[y]) {
            p.setLineDirty(y)
        }
    }
}
```

#### 为什么不换 VT 库？

`charmbracelet/x/vt` 在功能和兼容性上已经够用。cell-diff 的成本可控（一次 diff 是 O(rows×cols) 的内存比较，远小于重建 ANSI 字符串的开销），且不引入新依赖。如果未来发现 diff 本身成为瓶颈，再考虑换库或 patch。

### 背压与退化机制

**问题**：当 PTY 输出速度 > TUI 渲染速度时，paneOutputMsg 会在 bubbletea 消息队列中无限堆积。

**触发条件与退化路径**：

```
正常路径                         退化路径
─────────                      ─────────
stream → VTerm.Write           stream channel 满
→ dirty = true                 → fanout 丢弃数据，计入 droppedBytes
→ renderTick 渲染               → fanout 下次 Broadcast 发送 StreamSyncLost
                                → 客户端收到 SyncLost
                                → 标记 pane.syncLost = true
                                → renderTick 看到 syncLost：
                                    1. 请求 Snapshot (获取当前屏幕完整状态)
                                    2. 用 snapshot 重置本地 VTerm
                                    3. 标记 dirty = true, syncLost = false
                                    4. 下一个 renderTick 做一次 full redraw
```

**关键参数**：

| 参数 | 值 | 说明 |
|------|-----|------|
| fanout subscriber channel buffer | 256 | 服务端 fanout 的 per-subscriber buffer（现有值） |
| renderTick 间隔 | 16ms | 客户端 render 频率 |
| SyncLost 恢复方式 | Snapshot | 客户端请求服务端当前 VTerm 的完整屏幕状态 |

**客户端额外保护**：如果 renderTick 检测到某个 pane 连续 N 个 tick（如 30 次 = 500ms）都是 dirty，说明该 pane 输出极快且持续，此时：
- 降低该 pane 的渲染优先级（每隔一个 tick 才渲染）
- 或暂时标记该 pane 为 “catching up” 状态，只在 tick 间隔内处理最后一批数据

### 浮窗 z-order 渲染

**近期方案**：画家算法（painter's algorithm），从底到顶逐层绘制。

```
渲染顺序：
1. 布局 pane 层 → 按 layout tree 将所有 pane 绘制到 canvas
2. 浮窗层 → 按 z-order（创建顺序）依次绘制到 canvas，后绘制的覆盖先绘制的
```

这意味着被遮挡的 cell 仍会被计算和写入 canvas（随后被覆盖），但实现最简单。对于 1-3 个浮窗的典型场景，多余开销可忽略。

**中期优化**（如果浮窗数量或面积导致性能问题）：
- 维护 `visibleMask [][]bool`，标记每个 cell 是否被上层浮窗遮挡
- 布局 pane 渲染时跳过被遮挡的 cell
- 仅在浮窗打开/关闭/移动/resize 时重算 mask

### 与 Bubble Tea 的兼容性边界

**近期能做的**（不冲突）：
- render tick batching — 通过 `tea.Tick` 消息控制 View() 调用频率
- pane 级 dirty — 在 View() 内部决定哪些 pane 需要重建 cell grid
- cell-diff — 在 canvas.String() 内部减少 ANSI 输出量
- 浮窗画家算法 — 在 View() 内部的 canvas 合成逻辑

**近期不能做的**（与 bubbletea 冲突）：
- 终端原生 scroll/insert-line — bubbletea 的 renderer 不支持增量输出，View() 必须返回完整字符串
- 按行跳过输出 — bubbletea 的 alt screen renderer 会对整个 View() 输出做 diff，但其 diff 粒度是行级字符串比较，不支持 cursor motion 优化

**结论**：渲染架构原则第 5 条（利用终端原生能力）明确推迟到中期。近期通过原则 1-4 已经能获得主要的性能收益。

### 服务端配合改动

渲染优化不仅是客户端的事，服务端需要配合修复以下问题：

#### 1. fanout 数据竞争修复

`fanout.Broadcast()` 在 `RLock` 下修改 `sub.droppedBytes`，多个并发 Broadcast 会导致数据损坏。

修复：将 `droppedBytes` 从 `uint64` 改为 `atomic.Uint64`。

#### 2. EventBus 数据竞争修复

`EventBus.Publish()` 在 `RLock` 下修改 `sub.dropped`，同样的竞争问题。

修复：将 `dropped` 从 `int` 改为 `atomic.Int32`。

#### 3. Resize 零值校验

`Terminal.Resize()` 接受 0×0 尺寸，可能导致 VTerm 除零或异常状态。

修复：在 Resize 入口校验 cols > 0 && rows > 0，返回 error。

#### 4. readLoop 错误可观测性

`terminal.readLoop()` 静默吞掉所有非 EOF 错误，订阅者无法区分正常退出和异常断开。

修复：非 EOF 错误通过 EventBus 发布一个 warning 级别事件，方便客户端日志诊断。

## 配置

配置文件路径：`~/.config/termx/config.yaml`

```yaml
# Prefix key，可选值: ctrl-a, ctrl-b, ctrl-t, ctrl-s, ...
prefix_key: ctrl-a

# 鼠标支持
mouse: true

# 状态栏位置: top | bottom | off
status_bar: bottom

# Tab 栏位置: top | bottom | off
tab_bar: top

# 默认 shell
default_shell: /bin/zsh

# Scrollback 行数
scrollback_lines: 10000

# 主题
theme:
  status_bg: "#1e1e2e"
  status_fg: "#cdd6f4"
  tab_active: "#89b4fa"
  tab_inactive: "#585b70"
  pane_border: "#585b70"
  pane_active_border: "#89b4fa"
  floating_border: "#f5c2e7"
```

## Workspace 布局文件

布局可以声明式定义，保存在 `~/.config/termx/layouts/` 目录下。

### 格式

```yaml
# ~/.config/termx/layouts/dev.yaml
name: dev
tabs:
  - name: editor
    layout:
      split: horizontal           # 左右分割
      ratio: 0.6
      children:
        - command: vim .
          cwd: ~/project
        - split: vertical          # 右侧上下分割
          ratio: 0.5
          children:
            - command: make watch
              cwd: ~/project
            - command: tail -f logs/app.log
  - name: git
    layout:
      command: lazygit
  - name: scratch
    layout:
      command: zsh
```

### 使用方式

```bash
# 从布局文件启动
termx --layout dev

# 在 TUI 中保存当前布局
# C-a : save-layout dev

# 在 TUI 中加载布局
# C-a : load-layout dev
```

布局文件是纯 YAML，可以放进版本控制、团队间共享。

## bubbletea 架构

### 数据模型

```go
type Model struct {
    // 全局
    config     Config
    client     *protocol.Client   // 到 daemon 的连接
    mode       Mode               // normal | copy | command | picker

    // Workspace 管理
    workspaces []*Workspace
    activeWS   int

    // 命令模式输入
    cmdInput   textinput.Model

    // 屏幕尺寸
    width, height int
}

type Workspace struct {
    name       string
    tabs       []*Tab
    activeTab  int
}

type Tab struct {
    name       string
    root       *LayoutNode         // 二叉分割树
    floating   []*FloatingPane
    focusedID  string              // 当前焦点 pane 的 terminal ID
    zoomed     *string             // 如果有 zoom 的 pane，记录其 terminal ID
}

type LayoutNode struct {
    // 叶节点（terminalID 非空时为叶节点）
    terminalID string

    // 分支节点
    split      SplitDir            // horizontal | vertical
    ratio      float64             // 0.0 ~ 1.0，左/上子节点占比
    children   [2]*LayoutNode      // [0]=左/上, [1]=右/下
}

type SplitDir int
const (
    SplitHorizontal SplitDir = iota  // 左右分割
    SplitVertical                     // 上下分割
)

type FloatingPane struct {
    terminalID    string
    x, y          int              // 左上角绝对坐标
    width, height int
    visible       bool
}
```

### 消息类型

```go
// Terminal 输出
type TerminalOutputMsg struct {
    TerminalID string
    Data       []byte
}

// Terminal 关闭
type TerminalExitedMsg struct {
    TerminalID string
    ExitCode   int
}

// Server 事件（terminal 创建/销毁等）
type ServerEventMsg struct {
    Event Event
}

// 窗口大小变化
type WindowSizeMsg tea.WindowSizeMsg
```

### 消息流

```
bubbletea event loop
  │
  ├─ KeyMsg → Update()
  │    │
  │    ├─ (prefix key 未激活) → 发送给当前 pane 的 terminal (WriteInput)
  │    │
  │    ├─ (prefix key 激活) → 解析后续按键
  │    │    ├─ pane 操作 → 修改 LayoutNode 树 / 切换焦点
  │    │    ├─ tab 操作 → 修改 Tab 列表 / 切换 active tab
  │    │    ├─ workspace 操作 → 切换 workspace / 进入 picker
  │    │    └─ 模式切换 → 进入 copy/command/picker 模式
  │    │
  │    ├─ (copy 模式) → 滚动、选择、搜索
  │    ├─ (command 模式) → 输入命令、执行
  │    └─ (picker 模式) → 模糊搜索、选择
  │
  ├─ TerminalOutputMsg → 更新对应 pane 的显示缓冲区
  ├─ TerminalExitedMsg → 移除 pane（如果是最后一个 pane 则移除 tab）
  ├─ ServerEventMsg → 更新内部 terminal 列表
  └─ WindowSizeMsg → 重新计算所有 pane 的尺寸
```

### 布局计算

`LayoutNode` 的尺寸递归计算：

```go
func (n *LayoutNode) Layout(x, y, w, h int) []PaneRect {
    if n.terminalID != "" {
        // 叶节点：返回一个 pane 矩形
        return []PaneRect{{
            TerminalID: n.terminalID,
            X: x, Y: y, W: w, H: h,
        }}
    }
    // 分支节点：按 ratio 分割，递归
    switch n.split {
    case SplitHorizontal:
        leftW := int(float64(w) * n.ratio)
        left := n.children[0].Layout(x, y, leftW, h)
        right := n.children[1].Layout(x+leftW+1, y, w-leftW-1, h) // +1 for border
        return append(left, right...)
    case SplitVertical:
        topH := int(float64(h) * n.ratio)
        top := n.children[0].Layout(x, y, w, topH)
        bottom := n.children[1].Layout(x, y+topH+1, w, h-topH-1)
        return append(top, bottom...)
    }
}
```

### 分割操作

水平分割（`C-a %`）：将当前叶节点替换为分支节点，原 terminal 成为左子节点，新 terminal 成为右子节点。

```go
func (t *Tab) SplitHorizontal(newTermID string) {
    leaf := t.findFocusedLeaf()
    oldTermID := leaf.terminalID
    leaf.terminalID = ""
    leaf.split = SplitHorizontal
    leaf.ratio = 0.5
    leaf.children = [2]*LayoutNode{
        {terminalID: oldTermID},
        {terminalID: newTermID},
    }
    t.focusedID = newTermID
}
```

### Pane 导航

pane 导航（`C-a h/j/k/l`）基于几何位置：

1. 计算所有 pane 的矩形（通过 `Layout()`）
2. 找到当前焦点 pane 的矩形
3. 在指定方向上找到最近的 pane
4. 切换焦点

### Zoom

zoom（`C-a z`）不修改布局树，而是临时让某个 pane 占据整个 tab 区域：

- `tab.zoomed = &termID` → 渲染时只渲染该 pane，占满整个区域
- 再次 `C-a z` → `tab.zoomed = nil` → 恢复正常布局
- 切换 tab 后 zoom 状态保留在原 tab

## Pane 尺寸同步

每个 pane 渲染时，TUI 根据分配到的矩形大小调用 `Resize(terminalID, cols, rows)`，使 server 端的 Terminal PTY 尺寸与 TUI 中显示的区域一致。

当以下情况发生时触发 resize：
- TUI 窗口大小变化
- 分割/关闭 pane 导致布局重新计算
- 拖动 pane 边界调整比例
- 进入/退出 zoom 模式

## 预定义布局

`C-a Space` 循环切换 5 种预定义布局（作用于当前 tab 的所有 pane）：

1. **even-horizontal**：所有 pane 水平等分
2. **even-vertical**：所有 pane 垂直等分
3. **main-horizontal**：第一个 pane 占左侧大半，其余在右侧垂直等分
4. **main-vertical**：第一个 pane 占上方大半，其余在下方水平等分
5. **tiled**：网格排列

实现方式：收集当前 tab 所有叶节点的 terminal ID，按照预定义模式重建 `LayoutNode` 树。

## 模糊搜索（`C-a f`）

弹出全屏 picker，搜索所有 workspace/tab/pane 中的 terminal：

```
┌─ Find Terminal ──────────────────────────────────┐
│  > build                                         │
│                                                  │
│  ws:dev / tab:editor   vim .           ● running │
│  ws:dev / tab:build    make watch      ● running │
│  ws:ops / tab:logs     tail -f app.log ● running │
│                                                  │
│  3 matches                                       │
└──────────────────────────────────────────────────┘
```

- 搜索范围：terminal name、command、workspace 名、tab 名
- 选中后跳转到对应的 workspace/tab 并聚焦该 pane
- 也可以搜索 server 上未被任何 pane 使用的 terminal，选中后在当前 pane 中 attach

## CLI 集成

TUI 是默认的 CLI 行为（无子命令时启动）：

```bash
# 启动 TUI（默认 workspace）
termx

# 从布局文件启动
termx --layout dev

# 直接 attach 单个 terminal（全屏，不启动完整 TUI）
termx attach <id>

# 其他子命令不涉及 TUI
termx new -- bash
termx ls
termx kill <id>
```

启动时行为：
1. 连接到 daemon（如果 daemon 未运行，自动启动）
2. 如果指定了 `--layout`，从布局文件创建 workspace
3. 如果存在之前的本地 workspace 状态文件，尝试按其中记录的 terminal ID 恢复
4. 对于 server 上仍在运行但未被本地布局引用的 terminal，把它们作为 orphan terminal 暴露给 picker
5. 否则创建一个默认 workspace，包含一个 tab 和一个 pane（运行 default_shell）

## 与 tmux/zellij 的差异化卖点

1. **服务端无布局限制**：布局完全客户端侧，不同 TUI 实例可以对同一组 terminal 有不同的视图
2. **声明式布局配置**：workspace 配置是纯 YAML，可版本控制、团队共享
3. **浮动面板**：tmux 没有原生支持
4. **模糊搜索**（`C-a f`）：跨所有 workspace/tab 快速定位 terminal
5. **多客户端同时连接**：server 保持 terminal 存活，多个 TUI 可以同时 attach 同一组 terminal，各自拥有独立布局
6. **零配置重连**：TUI 重启后，server 端的 terminal 仍在运行，可直接恢复

## 不做的事情

- 不做插件系统
- 不做自定义 key table（只支持修改 prefix key）
- 不做 pane 间同步输入（broadcast input）
- 不做 tmux-like control mode

## 相关文档

- [Terminal 模型](spec-terminal.md) — TUI 显示的数据
- [传输层](spec-transport.md) — TUI 通过 Unix socket 连接
- [线协议](spec-protocol.md) — TUI 使用的消息格式
- [快照](spec-snapshot.md) — Scrollback/Copy 模式的数据来源
- [事件系统](spec-events.md) — TUI 订阅的 server 事件
