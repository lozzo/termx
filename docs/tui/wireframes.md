# termx TUI 线框图

状态：Draft v2
日期：2026-03-25

说明：

- 这份线框图只表达真正的产品主界面方向
- 主界面必须是 `terminal-first`、`pane-first`
- overlay 只能盖在工作台上，不能替代工作台主体

---

## 1. 默认启动

目标：

- 执行 `termx` 后直接进入可工作的 workspace
- 默认有一个 live shell pane

```text
[main]  [1:shell]                                                        pane:1  term:1
┌─ shell-1                                                          ● run  owner ─────────────────┐
│ $                                                                                               │
│                                                                                                 │
│                                                                                                 │
│                                                                                                 │
│                                                                                                 │
│                                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
Ctrl-p PANE  Ctrl-t TAB  Ctrl-w WS  Ctrl-o FLOAT  Ctrl-f PICK  Ctrl-g GLOBAL     shell-1  tiled
```

要点：

- 不先显示说明页
- terminal 内容是主内容
- 顶栏和底栏只做最小导航，不抢正文空间

---

## 2. 常规 tiled workspace

目标：

- split 后仍然是“两个真正能工作的 terminal pane”

```text
[project-api]  [1:dev]  2:logs  3:build                                  pane:3  term:3  float:0
┌─ api-dev                                                        ● run  owner ─┬─ watcher  follow ┐
│ $ npm run dev                                                                     │ > tsc -w     │
│ ready on :3000                                                                    │ Found 0 err  │
│                                                                                   │              │
├─ git-shell                                                      ● run  owner ───────────────────┤
│ $ git status                                                                                     │
│ On branch main                                                                                   │
│ nothing to commit                                                                                │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
Ctrl-p PANE  Ctrl-t TAB  Ctrl-w WS  Ctrl-o FLOAT  Ctrl-f PICK                   git-shell  tiled
```

要点：

- active pane 一眼可见
- pane 标题默认展示 terminal 名称
- `owner / follower` 放在标题栏右侧，不另外占正文

---

## 3. split chooser

目标：

- split 时只做最少打断
- overlay 盖在工作台上，底下 pane 仍然可辨认

```text
[project-api]  [1:dev]
┌─ api-dev [muted] ───────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                   │
│ ready on :3000                                                                                  │
│                                                                                                 │
│                        ┌─ Open Pane ─────────────────────────────────┐                           │
│                        │ [selected] + new terminal                   │                           │
│                        │            connect existing terminal        │                           │
│                        │                                             │                           │
│                        │ Enter confirm   Esc cancel                 │                           │
│                        └─────────────────────────────────────────────┘                           │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
focus: overlay
```

---

## 4. terminal picker

目标：

- picker 是覆盖层
- 工作台上下文还在

```text
[project-api]  [1:dev]
┌─ api-dev [muted] ───────────────────────────────────────────────────────────────────────────────┐
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
│                    │ Enter connect/create   Esc close                    │                      │
│                    └──────────────────────────────────────────────────────┘                      │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
focus: picker
```

---

## 5. floating pane

目标：

- floating 是真正叠在工作台上的窗口
- 不是右侧摘要卡，也不是单独页面

```text
[project-api]  [1:dev]                                                     pane:2  term:2  float:1
┌─ api-dev [muted] ───────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                   │
│ ready on :3000                                                                                  │
│                                                                                                 │
│                    ┌─ htop                                          ● run  owner  [floating] ┐  │
│                    │ PID   CPU   MEM                                                        │  │
│                    │ ...                                                                     │  │
│                    │                                                                         │  │
│                    └─────────────────────────────────────────────────────────────────────────┘  │
│                                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
Tab next  h/j/k/l move  H/J/K/L size  [/] z-order  c center  x close             htop  floating
```

---

## 6. 多浮窗叠放

目标：

- z-order、遮挡、活动浮窗必须直觉可见

```text
[project-api]  [1:dev]
┌─ api-dev [muted] ───────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                   │
│                                                                                                 │
│             ┌─ logs [floating z:1] ────────────────────────────────┐                            │
│             │ tail -f app.log                                      │                            │
│             │ [12:31:02] GET /health 200                           │                            │
│       ┌─────┴─ htop [active floating z:2] ─────────────────────────┴────┐                       │
│       │ PID   CPU   MEM                                                   │                       │
│       │ ...                                                               │                       │
│       └───────────────────────────────────────────────────────────────────┘                       │
│                                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
Tab next  [/] z-order  h/j/k/l move  H/J/K/L size  c center                           floating:2
```

---

## 7. 未连接 terminal 的 pane

```text
[project-api]  [1:dev]
┌─ unconnected pane ───────────────────────────────────────────────────────────────────────────────┐
│ no terminal connected                                                                            │
│                                                                                                  │
│ n start new terminal                                                                             │
│ a connect existing terminal                                                                      │
│ m open terminal manager                                                                          │
│ x close pane                                                                                     │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 8. terminal 中程序已退出的 pane

```text
[project-api]  [1:logs]
┌─ deploy-log ─────────────────────────────────────────────────────────────────────────────────────┐
│ process exited with status 1                                                                     │
│                                                                                                  │
│ history retained                                                                                 │
│                                                                                                  │
│ r restart terminal                                                                               │
│ a connect another terminal                                                                       │
│ x close pane                                                                                     │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 9. terminal manager

目标：

- manager 是资源管理 overlay，不是主界面

```text
[project-api]  [1:dev]
┌─ api-dev [muted] ───────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                   │
│                                                                                                 │
│           ┌─ Running Terminals ─────────────────────┬─ Terminal Detail ──────────────────────┐ │
│           │ search: api_                            │ api-dev                                 │ │
│           │                                         │ state: running                          │ │
│           │ [selected] ● api-dev                    │ command: npm run dev                    │ │
│           │            ● api-log                    │ shown in: main/dev/api-dev              │ │
│           │            ○ old-api                    │ Enter here  t new-tab  o floating       │ │
│           └─────────────────────────────────────────┴─────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
focus: manager
```

---

## 10. 设计硬约束

后续实现必须满足：

1. pane surface 始终优先于说明面板
2. floating 必须画在工作台上，而不是工作台旁边
3. overlay 必须盖在工作台上，而不是切走工作台
4. terminal 内容必须是主内容，summary 只能是辅助信息
