# termx TUI 线框图

状态：Draft v1
日期：2026-03-23

说明：

- 使用纯文本线框表达层级和布局
- 重点看信息分配、焦点和状态表达
- 不表达最终视觉细节

---

## 1. 默认启动

目标：

- 直接进入可工作 workspace
- 默认有一个 live shell pane

```text
[main]  [1:shell]                                                     pane:1 term:1 float:0
┌─ shell                                                               owner ─────────────────────┐
│ $                                                                                               │
│                                                                                                 │
│                                                                                                 │
│                                                                                                 │
│                                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
<p> PANE  <t> TAB  <w> WS  <o> FLOAT  <f> PICK  <g> GLOBAL                    shell  ▣ tiled
```

---

## 2. 常规 tiled workspace

```text
[project-api]  [1:dev]  2:logs  3:build                                      pane:3 term:3 float:0
┌─ api-dev                                                         owner ──────┬─ watcher  follower ┐
│ $ npm run dev                                                                     │ > tsc -w       │
│ ready on :3000                                                                    │ Found 0 errors │
│                                                                                   │                │
├─ git-shell                                                       owner ──────────────────────────┤
│ $ git status                                                                                    │
│ On branch main                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
<p> PANE  <t> TAB  <r> RESIZE  <f> PICK  <g> GLOBAL                         git-shell  ▣ tiled
```

设计要点：

- active pane 明显
- pane 标题默认展示 terminal 名称
- terminal 状态和连接关系放在标题栏右侧

---

## 3. split chooser

```text
[project-api]  [1:dev]
┌─ api-dev [Muted] ───────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                   │
│                                                                                                 │
│                          ┌─ Open Pane ──────────────────────────────────┐                        │
│                          │ [selected] + new terminal                    │                        │
│                          │            connect existing terminal         │                        │
│                          │                                              │                        │
│                          │ Enter confirm   Esc cancel                   │                        │
│                          └──────────────────────────────────────────────┘                        │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
focus: overlay
```

---

## 4. terminal picker

```text
[project-api]  [1:dev]
┌─ api-dev [Muted] ───────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                   │
│                                                                                                 │
│                    ┌─ Choose Terminal ────────────────────────────────────┐                      │
│                    │ search: api_                                         │                      │
│                    │                                                      │                      │
│                    │   + new terminal                                     │                      │
│                    │ [selected] ● api-dev      #backend   running         │                      │
│                    │            ● api-log      #backend   running         │                      │
│                    │            ○ old-api      #legacy    exited          │                      │
│                    │                                                      │                      │
│                    │ Enter connect/create   Esc close                     │                      │
│                    └──────────────────────────────────────────────────────┘                      │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
focus: picker
```

---

## 5. terminal manager

```text
[project-api]  [1:dev]
┌─ Terminal Pool ───────────────────────────────────────┬─ Terminal Details ──────────────────────┐
│ search: api_                                          │ api-dev                                  │
│                                                       │                                          │
│ NEW                                                   │ state: running                           │
│   + new terminal                                      │ visibility: visible                      │
│                                                       │ command: npm run dev                     │
│ VISIBLE                                               │ id: T-12                                 │
│ [selected] ● api-dev                                  │ connected panes: 2                       │
│                                                       │ shown in:                                │
│ PARKED                                                │ - ws:project-api / tab:dev / pane:api   │
│   ● api-log                                           │ - ws:project-api / tab:dev / float:log  │
│                                                       │                                          │
│ EXITED                                                │ Enter connect here  t new tab  o float  │
│   ○ old-api                                           │ e edit  k stop                           │
└───────────────────────────────────────────────────────┴──────────────────────────────────────────┘
```

设计要点：

- picker 是“快速 connect”
- manager 是“terminal 池管理”
- manager 也支持直接把当前 pane connect 到选中的 terminal

---

## 6. floating pane

```text
[project-api]  [1:dev]                                                     pane:2 term:1 float:1
┌─ api-dev [Muted] ───────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                   │
│ ready on :3000                                                                                  │
│                                                                                                 │
│                    ┌─ htop                                          owner  ◫ float ┐            │
│                    │ PID   CPU   MEM                                                     │       │
│                    │ ...                                                                  │       │
│                    │                                                                      │       │
│                    └──────────────────────────────────────────────────────────────────────┘       │
│                                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
<Tab> NEXT  <h/j/k/l> MOVE  <H/J/K/L> SIZE  <[/>] Z  <c> CENTER  <x> CLOSE     htop  ◫ float
```

---

## 7. 未连接 terminal 的 pane

```text
[project-api]  [1:dev]
┌─ unconnected pane ────────────────────────────────────────────────────────────────────────────────┐
│ terminal removed or not connected                                                                 │
│                                                                                                   │
│ actions:                                                                                          │
│   [n] start new terminal                                                                          │
│   [a] connect existing terminal                                                                   │
│   [m] open terminal manager                                                                       │
│   [x] close pane                                                                                  │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 8. terminal 中程序已退出的 pane

```text
[project-api]  [1:logs]
┌─ deploy-log ──────────────────────────────────────────────────────────────────────────────────────┐
│ terminal program exited with status 1                                                                │
│                                                                                                      │
│ history retained                                                                                     │
│                                                                                                      │
│ actions:                                                                                             │
│   [r] restart terminal                                                                               │
│   [a] connect another terminal                                                                       │
│   [x] close pane                                                                                     │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 9. workspace picker

```text
[project-api]  [1:dev]
┌─ api-dev [Muted] ───────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                   │
│                                                                                                 │
│                     ┌─ Choose Workspace ─────────────────────────────────────────┐               │
│                     │ search: prod_ / api-dev                                   │               │
│                     │                                                           │               │
│                     │ [selected] + create workspace                             │               │
│                     │            prod-main                                      │               │
│                     │              ├─ dev                                       │               │
│                     │              │  ├─ api-dev                               │               │
│                     │              │  └─ watcher                               │               │
│                     │              └─ logs                                      │               │
│                     │                 └─ deploy-log                            │               │
│                     │            staging-api                                   │               │
│                     │                                                           │               │
│                     │ Enter open/jump   Esc close                              │               │
│                     └───────────────────────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 10. layout resolve

```text
[workspace-from-layout]  [1:dev]
┌─ waiting slot [Muted] ──────────────────────────────────────────────────────────────────────────┐
│ layout requires a terminal for role: backend-dev                                                │
│ hint: tags env=dev service=api                                                                  │
│                                                                                                 │
│                         ┌─ Resolve Terminal ─────────────────────────────┐                       │
│                         │ [selected] connect existing                    │                       │
│                         │            create new                          │                       │
│                         │            skip                                │                       │
│                         └────────────────────────────────────────────────┘                       │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 11. 帮助页

```text
┌─ Help ───────────────────────────────────────────────────────────────────────────────────────────┐
│ Most used                                                                                        │
│   Ctrl-p pane   Ctrl-t tab   Ctrl-w workspace   Ctrl-f picker                                   │
│                                                                                                 │
│ Concepts                                                                                         │
│   pane = 工作位                                                                                   │
│   terminal = 运行实体                                                                             │
│                                                                                                 │
│ Shared terminal                                                                                  │
│   owner controls terminal-level operations                                                       │
│   follower is connected without control                                                          │
│                                                                                                 │
│ Exit                                                                                             │
│   close pane != stop terminal != detach TUI                                                     │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```
