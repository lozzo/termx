# termx TUI 线稿

状态：Draft v1

说明：

- 本文档使用纯文本线稿表达布局、层级和交互状态
- 结构目标尽量接近真实 TUI
- 颜色无法在 Markdown 中精确还原，因此使用标注表达视觉语义

图例：

- `[Primary]`：主焦点，高亮 pane / active tab
- `[Accent]`：浮窗焦点 / 需要特别注意的前景层
- `[Muted]`：非焦点 pane / 背景信息
- `[Warn]`：风险提示 / 危险提示
- `[Invert]`：选中项反色块

---

## 1. 默认启动：直接进入可工作 workspace

目标：

- 用户执行 `termx`
- 直接进入临时 workspace
- 默认有一个可输入 shell pane

```text
[tmp-workspace]  [1:shell]                                                pane:1  term:1
┌─ shell-1                                                            ● run  ⇄ fit ─────────────────┐
│ $                                                                                                 │
│                                                                                                   │
│                                                                                                   │
│                                                                                                   │
│                                                                                                   │
│                                                                                                   │
│                                                                                                   │
│                                                                                                   │
│                                                                                                   │
│                                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
Ctrl + <g> LOCK  <p> PANE  <t> TAB  <r> RESIZE  <f> PICK    shell-1  ▣ tiled
```

要点：

- 不再先落到说明页
- 用户直接能输入
- 默认 terminal 继承当前 cwd 和 env
- pane 标题直接使用 terminal 真名，不强调 pane 独立命名
- pane 顶部 chrome 使用单线边框表达

---

## 2. 常规 tiled workspace

目标：

- 像 zellij 一样直观
- 有明显 active pane

```text
[project-api]  [1:dev]  2:logs  3:build                                   pane:3  term:3  float:0
┌─ api-dev                                                        ● run  ⇄ fit ─┬─ watcher  ● run  ⇄ fit ─┐
│ $ npm run dev                                     │ > tsc -w                                      │
│ ready on :3000                                    │ Found 0 errors.                               │
│                                                   │                                                │
│                                                   │                                                │
├─ git-shell                                                      ● run  ⇄ fit ─────────────────────┤
│ $ git status                                                                                      │
│ On branch main                                                                                    │
│ nothing to commit, working tree clean                                                             │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
Ctrl + <g> LOCK  <p> PANE  <t> TAB  <w> WS  <f> PICK    api-dev  ▣ tiled
```

---

## 3. split chooser

目标：

- split 时允许新建 terminal 或复用 terminal

```text
[project-api]  [1:dev]                                                             ws:project-api
┌─ api-dev [Muted] ─────────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                     │
│ ready on :3000                                                                                    │
│                                                                                                   │
│                           ┌─ Open Pane ───────────────────────────────────┐                        │
│                           │ [Invert] + new terminal                       │                        │
│                           │           attach existing terminal            │                        │
│                           │                                               │                        │
│                           │ [Enter] confirm   [Esc] cancel               │                        │
│                           └───────────────────────────────────────────────┘                        │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ PANE ]  split current pane                                                            focus:modal
```

---

## 4. Terminal picker

目标：

- 中央 modal
- attach existing terminal
- 搜索 name/tag/command

```text
[project-api]  [1:dev]                                                             ws:project-api
┌─ api-dev [Muted] ─────────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                     │
│                                                                                                   │
│                         ┌─ Choose Terminal ─────────────────────────────────────┐                  │
│                         │ search: api_                                         │                  │
│                         │                                                      │                  │
│                         │   + new terminal                                     │                  │
│                         │ [Invert] ● api-dev        #backend   running         │                  │
│                         │          ● api-log        #backend   running         │                  │
│                         │          ○ old-api        #legacy    exited          │                  │
│                         │                                                      │                  │
│                         │ [Enter] attach/create   [Esc] close                  │                  │
│                         └──────────────────────────────────────────────────────┘                  │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ PICKER ]  filter terminals                                                           focus:picker
```

---

## 4.1 Terminal manager

目标：

- 全屏管理 terminal pool
- 左侧选 terminal，右侧看详情
- 从当前 pane / 新 tab / floating 打开 terminal

```text
[project-api]  [1:dev]                                                             ws:project-api
┌─ Running Terminals ───────────────────────────────────────┬─ Terminal Details ───────────────────┐
│ search: api_                                              │ api-dev                              │
│                                                           │                                      │
│ NEW                                                       │ state: running                       │
│   + new terminal                                          │ visibility: visible                  │
│                                                           │ command: npm run dev                 │
│ VISIBLE                                                   │ id: T-12                             │
│ [Invert] ● api-dev                                        │ open panes: 2                        │
│                                                           │ shown in:                            │
│ PARKED                                                    │ - ws:project-api / tab:dev / pane:api-dev │
│   ● api-log                                               │ - ws:project-api / tab:dev / float:api-log │
│                                                           │                                      │
│ EXITED                                                    │ Enter brings this terminal here      │
│   ○ old-api                                               │ Ctrl-t opens in new tab              │
└───────────────────────────────────────────────────────────┴──────────────────────────────────────┘
Ctrl + <Enter> HERE  <t> NEW TAB  <o> FLOAT  <e> EDIT  <k> STOP    api-dev  visible  shown:2
```

说明：

- 默认优先选中“当前 pane 没在看的 terminal”
- stop 后列表立即刷新
- 如果 terminal 被移除，当前布局中的 pane 会保留成 saved pane

---

## 5. Workspace picker

目标：

- session-like 体验
- 中央 modal

```text
[project-api]  [1:dev]                                                             ws:project-api
┌─ api-dev [Muted] ─────────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                     │
│                                                                                                   │
│                       ┌─ Choose Workspace ─────────────────────────────────────┐                   │
│                       │ search: prod_                                         │                   │
│                       │                                                       │                   │
│                       │ [Invert] + create workspace                           │                   │
│                       │          prod-main                                    │                   │
│                       │          staging-api                                  │                   │
│                       │          personal-scratch                             │                   │
│                       │                                                       │                   │
│                       │ [Enter] open/create   [Esc] close                     │                   │
│                       └───────────────────────────────────────────────────────┘                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ PICKER ]  filter workspaces                                                          focus:picker
```

---

## 6. 单浮窗：焦点在 floating 层

目标：

- 浮窗明显高于 tiled
- 状态栏明确当前 focus 在 floating

```text
[project-api]  [1:dev]                                                pane:2  term:1  float:1
┌─ api-dev [Muted] ─────────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                     │
│ ready on :3000                                                                                    │
│                                                                                                   │
│                    ┌─ htop                                                  ● run  ⇄ fit  ◫ float ┐                  │
│                    │  1  1234 user   20   0  321m  42m R  12.0  1.1 node       │                  │
│                    │  2  9911 user   20   0  111m  11m S   4.0  0.2 bash       │                  │
│                    │                                                            │                  │
│                    │                                                            │                  │
│                    └────────────────────────────────────────────────────────────┘                  │
│                                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
Ctrl + <Tab> NEXT  <h/j/k/l> MOVE  <H/J/K/L> SIZE  <c> CENTER  <x> CLOSE    htop  ◫ float  ⇄ fit
```

补充说明：

- 顶栏右侧是 workspace 级计数/notice，不再重复显示 workspace 名称
- pane 标题栏右侧吸收运行态 badge
- 底栏右侧只保留当前焦点 pane 的短摘要
- active pane 边框是高亮绿色，inactive pane 边框是亮灰色
- 若启用 Nerd Font，可把 `● / ⇄ / ◫ / ▣` 替换为更强的图标集
- floating pane 可以移动到 tab 主内容区域之外
- `center` 表示把当前 floating pane 呼回并居中

---

## 7. 多浮窗叠放

目标：

- 可看清 z-order
- 新浮窗有错开摆放

```text
[project-api]  [1:dev]                                                             ws:project-api
┌─ api-dev [Muted] ─────────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                     │
│                                                                                                   │
│             ┌─ float:logs [Muted] [floating z:1] ────────────────────────┐                       │
│             │ tail -f app.log                                            │                       │
│             │ [12:31:02] GET /health 200                                 │                       │
│       ┌─────┴─ float:htop [Accent] [floating z:2] ───────────────────────┴────┐                  │
│       │  PID   CPU   MEM                                                       │                  │
│       │  ...                                                                   │                  │
│       │                                                                        │                  │
│       └────────────────────────────────────────────────────────────────────────┘                  │
│                                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
Ctrl + <Tab> NEXT  <[/>] Z  <h/j/k/l> MOVE  <H/J/K/L> SIZE  <c> CENTER    floating:2
```

---

## 8. Metadata 编辑

目标：

- 清楚表达“编辑 terminal，不是编辑 pane”

```text
[project-api]  [1:dev]                                                             ws:project-api
┌─ api-dev [Muted] ─────────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                     │
│                                                                                                   │
│                        ┌─ Edit Terminal [Warn] ────────────────────────────┐                      │
│                        │ name:  [ api-prod_ ]                              │                      │
│                        │                                                   │                      │
│                        │ step 1/2                                          │                      │
│                        │ terminal id: term-api-prod                        │                      │
│                        │ command: /bin/zsh                                 │                      │
│                        │ updates terminal metadata for every attached pane  │                      │
│                        │                                                   │                      │
│                        │ [Enter] next   [Esc] cancel                      │                      │
│                        └───────────────────────────────────────────────────┘                      │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ PROMPT ]  editing terminal metadata                                                    focus:prompt
```

下一步 tags：

```text
┌─ Edit Terminal [Warn] ───────────────────────────────────────────────┐
│ tags:  [ role=api team=infra termx.size_lock=warn_ ]                │
│                                                                      │
│ step 2/2                                                             │
│ terminal id: term-api-prod                                           │
│ name: api-prod                                                       │
│ command: /bin/zsh                                                    │
│ updates terminal metadata for every attached pane                    │
│                                                                      │
│ [Enter] save   [Esc] cancel                                          │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 9. 共享 terminal：未 acquire resize

目标：

- 同一个 terminal 出现在 tiled + floating
- 但当前 pane 只是观察，不会隐式改写 size

```text
[project-api]  [1:ops]                                                             ws:project-api
┌─ api-shell [Primary] ────────────────────────────────────────────────────────────────────────────┐
│ $                                                                                                 │
│                                                                                                   │
│                  ┌─ float:api-shell [Accent] [shared] ─────────────────────────┐                 │
│                  │ same terminal attached here                                  │                 │
│                  │ current terminal size: 120x32                                │                 │
│                  │ pane size: 72x18                                             │                 │
│                  │ resize control: not acquired                                 │                 │
│                  └───────────────────────────────────────────────────────────────┘                 │
│                                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ FLOAT ]  acquire resize  move  close                                                    shared:yes
```

---

## 10. Acquire resize 提示

目标：

- resize 必须主动获取

```text
┌─ Acquire Resize Control ─────────────────────────────────────────────────┐
│ This pane is observing a shared terminal.                               │
│                                                                         │
│ Acquire resize control and apply this pane size?                        │
│                                                                         │
│ terminal: api-shell                                                     │
│ current size: 120x32                                                    │
│ target size: 168x40                                                     │
│ size lock: off                                                          │
│                                                                         │
│ [Enter] acquire and resize   [Esc] cancel                               │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 11. Size lock warn 提示

目标：

- terminal 带 `termx.size_lock=warn`
- resize 前先提示风险

```text
┌─ Size Change Warning [Warn] ────────────────────────────────────────────┐
│ This terminal may be running an interactive TUI program.                │
│ Changing terminal size can affect internal rendering or screen state.   │
│                                                                         │
│ terminal: prod-monitor                                                  │
│ requested size: 180x44                                                  │
│ lock mode: warn                                                         │
│                                                                         │
│ Continue to acquire and resize?                                         │
│                                                                         │
│ [Enter] continue   [Esc] cancel                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 12. Tab 配置：进入 tab 自动 acquire resize

目标：

- 某些 tab 开启 auto acquire

```text
[prod-main]  1:logs  [2:monitor]  3:shell                                               ws:prod-main
┌─ monitor [Primary] ───────────────────────────────────────────────────────────────────────────────┐
│ htop                                                                                              │
│                                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ NORMAL ]  auto-acquire:on  terminal:prod-monitor  size-lock:warn                     focus:tiled
```

切回该 tab 时的 notice：

```text
[ notice ] acquired resize control for terminal 'prod-monitor'
```

---

## 13. exited but retained

目标：

- terminal 退出但仍保留恢复能力

```text
[project-api]  [1:dev]                                                             ws:project-api
┌─ api-shell [Warn] [exited code=0] ───────────────────────────────────────────────────────────────┐
│ process exited                                                                                     │
│                                                                                                   │
│ terminal is retained on server                                                                     │
│ command: /bin/zsh                                                                                  │
│                                                                                                   │
│ [ r ] restart terminal                                                                             │
│ [ x ] close pane                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ NORMAL ]  exited retained  restart  close                                                 focus:tiled
```

如果该 terminal 同时被多个 pane attach：

```text
all attached panes show the same exited state
```

---

## 14. stop/remove terminal 后 pane 保留为 saved pane

目标：

- terminal 被明确移除
- 所有绑定 pane 保留为可继续复用的 saved pane

移除前：

```text
[project-api]  [1:ops]                                                             ws:project-api
┌─ shared-shell [Primary] ─────────────────────────────┬─ logs [Muted] ───────────────────────────┐
│ $                                                    │ tail -f app.log                            │
│                                                      │                                            │
└──────────────────────────────────────────────────────┴────────────────────────────────────────────┘
```

移除后：

```text
[project-api]  [1:ops]                                                             ws:project-api
┌─ shared-shell [Muted] ───────────────────────────────┬─ logs [Primary] ──────────────────────────┐
│ saved pane                                           │ tail -f app.log                            │
│ previous terminal was removed                        │                                             │
│ Ctrl-f attach terminal                               │                                             │
│ Ctrl-g then t open terminal manager                  │                                             │
└──────────────────────────────────────────────────────┴─────────────────────────────────────────────┘
[ notice ] terminal 'shared-shell' was removed by another client; left 2 saved panes
```

如果是另一个客户端执行的 remove：

```text
[ notice ] terminal 'shared-shell' was removed by another client; left 2 saved panes
```

如果系统能识别身份：

```text
[ notice ] terminal 'shared-shell' was removed by lozzow@host; left 2 saved panes
```

多人共享时的确认弹窗建议：

```text
┌─ Remove Shared Terminal [Warn] ─────────────────────────────────────────┐
│ This terminal is attached by 2 clients and 3 panes.                    │
│ Removing it will unbind those panes and save their slots.              │
│                                                                        │
│ terminal: shared-shell                                                 │
│                                                                        │
│ [Enter] remove terminal   [Esc] cancel                                 │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 15. 空 tab / 无 pane 状态

目标：

- 当 tab 中最后一个 pane 消失时，给出明确下一步

```text
[project-api]  [2:empty]                                                           ws:project-api


                           ┌─ Empty Tab ─────────────────────────────────┐
                           │ no pane is currently open                   │
                           │                                             │
                           │ [Enter] new terminal                        │
                           │ [Ctrl-f] attach existing terminal           │
                           │ [Ctrl-w] switch workspace                   │
                           └─────────────────────────────────────────────┘


[ NORMAL ]  empty tab                                                                focus:launcher
```

---

## 16. Help / Shortcut Map

目标：

- 结构清楚
- 不像一整屏噪音

```text
┌─ Help / Shortcut Map ─────────────────────────────────────────────────────────────────────────────┐
│ Most used                                                                                         │
│   Ctrl-p   pane actions                                                                           │
│   Ctrl-t   tab actions                                                                            │
│   Ctrl-w   workspace actions                                                                      │
│   Ctrl-o   floating actions                                                                       │
│   Ctrl-f   terminal picker                                                                        │
│                                                                                                   │
│ Shared terminal                                                                                   │
│   acquire resize       get resize control for current pane                                        │
│   size lock warn       warns before size changes on protected terminals                           │
│                                                                                                   │
│ Exit                                                                                              │
│   Esc      close current mode/modal                                                               │
│   detach   leave TUI, keep terminals alive                                                        │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 17. 视觉备注

本文档不能直接表达真实终端色彩，但最终实现应满足：

- active tiled pane：`[Primary]`
- active floating pane：`[Accent]`
- inactive pane：`[Muted]`
- metadata / size risk：`[Warn]`
- picker selected row：`[Invert]`

如果后续需要，我可以继续把这些线稿拆成：

- startup flows
- workspace/layout restore flows
- shared terminal resize flows
- readonly / observer / collaborator flows
