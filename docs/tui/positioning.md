# 产品定位

## 一句话

termx 是一个终端运行时（Terminal Runtime），不是终端复用器（Terminal Multiplexer）。

## 核心区别

```
tmux / zellij                          termx
─────────────                          ─────
终端复用器                              终端运行时
pane 就是 terminal，生死与共             terminal 独立存活，客户端只是视图
布局拥有 terminal                       terminal 是一等公民，布局只是镜头
关闭 pane = 杀进程                      关闭视图 ≠ 杀 terminal
一个 terminal 只能在一个 pane 里         一个 terminal 可以同时出现在任意多个视图中
session 是组织单位也是生命周期单位        workspace 纯粹是视图组织，不影响 terminal 生命周期
```

类比：

```
tmux   ≈ 文档编辑器     文档只能在一个窗口里打开，关窗口 = 关文档
termx  ≈ 数据库 + 视图   数据（terminal）是一份，视图（layout）随便建
```

## 为什么需要 termx

### tmux 的三个结构性限制

**1. Terminal 被困在 pane 里**

tmux 的 pane 和 PTY 是 1:1 绑定的。你想在另一个 window 里看同一个 terminal 的输出？
要么 `join-pane` 把它搬过去（原来的位置就没了），要么开一个新 pane 跑 `tmux capture-pane`（不是实时的）。

termx 里，同一个 terminal 可以同时出现在：
- workspace A 的平铺布局里（fit 模式，跟随 layout 大小）
- workspace B 的浮动窗口里（fixed 模式，保持自己的尺寸）
- 外部 App 客户端的 webview 里

```
tmux:
  Session "dev"
    └─ Window 1
         ├─ Pane A ←→ PTY (bash)     1:1 绑定
         └─ Pane B ←→ PTY (vim)      1:1 绑定

  想在 Window 2 也看 Pane A 的输出？
  → join-pane -s 1.0 -t 2    （A 从 Window 1 消失）
  → 或者 link-window          （共享整个 window，不能单独看一个 pane）

termx:
  Terminal Pool (server)
    ├─ T1 (bash)  ──┬── Viewport A (TUI workspace "dev")
    │               └── Viewport X (TUI workspace "ops")    同时！
    ├─ T2 (vim)  ───── Viewport B
    └─ T3 (htop) ───── (无人观察，仍在运行)
```

**2. 布局和生命周期绑死**

tmux 的 session 既是组织单位（"这些 window 属于同一个项目"），也是生命周期单位（`kill-session` 杀掉所有 window 和 pane）。你不能说"我要换一套布局但保留所有 terminal"。

termx 里，workspace 只是视图配置。切换 workspace = 换一套布局，terminal 不受影响。删除 workspace = 删除一个配置文件，terminal 继续跑。

```
tmux:
  kill-session -t dev
  → 所有 window、pane、PTY 全部销毁
  → 工作丢失

termx:
  关闭 workspace "dev"
  → 只是关闭了一个视图
  → 所有 terminal 仍在 server 上运行
  → 打开另一个 workspace 或重新打开 "dev"，terminal 还在
```

**3. 多客户端只能共享整个 session**

tmux 的 `attach` 是共享整个 session——两个人看到一模一样的画面，光标同步，window 切换同步。想要不同的视图？只能建不同的 session，但 session 间不能共享 pane。

termx 里，每个客户端有自己独立的 workspace 和布局，但共享同一个 terminal 池。A 在写代码，B 在看日志，各自的布局完全独立，但可以观察同一个 terminal。

```
tmux:
  User A: tmux attach -t dev    ──┐
  User B: tmux attach -t dev    ──┤── 看到完全一样的画面
                                  │   光标同步，window 同步
                                  │
  想要不同视图？
  → 只能建不同 session
  → 但 session 间不能共享 pane

termx:
  User A: termx (workspace "coding")   ── editor + build + shell
  User B: termx (workspace "ops")      ── 3x log tail + metrics
                                           │
                                           └── 共享同一个 log terminal
                                               各自布局完全独立
```

## 目标用户

### 主要用户

**重度终端用户**——已经在用 tmux/zellij，管理 5+ 个长期运行的 terminal，经常需要在不同上下文间切换。

termx 对他们的价值：terminal 不再被困在某个 pane 里，布局可以随时切换，不丢失任何运行中的进程。

### 扩展用户

**AI Agent 开发者**——需要给 AI agent 分配独立的终端环境，同时人类可以实时观察和干预。

termx 对他们的价值：terminal 作为可编程的 I/O 通道，天然支持多客户端订阅，API 干净。

### 不是目标用户

- 偶尔用终端的人（不需要复用器）
- 只用 IDE 内置终端的人（IDE 已经够了）
- 需要远程 session 共享的团队（termx 当前只做本地 daemon）

## 核心用户价值

三句话：

1. **Terminal 不被困在 pane 里** — 同一个 terminal 可以出现在任意多个视图中，跨 workspace、跨客户端
2. **布局是声明式的、可切换的** — terminal 是零件，layout 是组装图，随时换图不丢零件
3. **天然适合 AI agent** — terminal 作为独立的可编程 I/O 通道，支持多方同时读写

## 与 tmux 的兼容策略

termx 不试图取代 tmux 的所有功能。对于 tmux 已经做得很好的部分（prefix key、分屏导航、copy mode），termx 直接复用相同的交互模式，降低迁移成本。

termx 的差异化不在交互层面，而在模型层面：

```
交互层：跟 tmux 尽量一致（prefix key、hjkl 导航、分屏快捷键）
         ↓
模型层：完全不同（terminal 独立、viewport 解耦、声明式布局）
         ↓
体验层：日常使用感觉像 tmux，但能做到 tmux 做不到的事
```

用户不需要重新学习快捷键，但会逐渐发现：
- "咦，我关了 pane 但 terminal 还在跑"
- "咦，我可以在两个 tab 里同时看同一个 terminal"
- "咦，我换了一套布局，所有 terminal 自动归位了"

## 不做的事情

- **不做远程 session 共享** — 当前只做本地 daemon，远程场景通过 SSH + 本地 termx 解决
- **不做插件系统** — 保持核心简单，扩展通过 API 和外部工具实现
- **不做内置 AI** — termx 是 AI agent 的运行时，不是 AI 本身
- **不做 tmux 兼容层** — 不支持 tmux 的配置文件或命令语法
- **不做 pane 间同步输入** — broadcast input 是小众需求，不值得增加复杂度
