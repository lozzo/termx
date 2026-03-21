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
[tmp-workspace]  [1:shell]                                                             ws:tmp-workspace
┌─ shell-1 [Primary] ────────────────────────────────────────────────────────────────────────────────┐
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
[ NORMAL ]  pane split  tab new  float new  picker terminal                             focus:tiled
```

要点：

- 不再先落到说明页
- 用户直接能输入
- 默认 terminal 继承当前 cwd 和 env

---

## 2. 常规 tiled workspace

目标：

- 像 zellij 一样直观
- 有明显 active pane

```text
[project-api]  [1:dev]  2:logs  3:build                                                 ws:project-api
┌─ api-dev [Primary] ───────────────────────────────┬─ watcher [Muted] ────────────────────────────┐
│ $ npm run dev                                     │ > tsc -w                                      │
│ ready on :3000                                    │ Found 0 errors.                               │
│                                                   │                                                │
│                                                   │                                                │
├─ git-shell [Muted] ───────────────────────────────┴───────────────────────────────────────────────┤
│ $ git status                                                                                      │
│ On branch main                                                                                    │
│ nothing to commit, working tree clean                                                             │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ NORMAL ]  pane split  tab switch  workspace switch  terminal picker                    focus:tiled
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
[project-api]  [1:dev]                                                             ws:project-api
┌─ api-dev [Muted] ─────────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                     │
│ ready on :3000                                                                                    │
│                                                                                                   │
│                    ┌─ float:htop [Accent] [floating] ─────────────────────────┐                  │
│                    │  1  1234 user   20   0  321m  42m R  12.0  1.1 node       │                  │
│                    │  2  9911 user   20   0  111m  11m S   4.0  0.2 bash       │                  │
│                    │                                                            │                  │
│                    │                                                            │                  │
│                    └────────────────────────────────────────────────────────────┘                  │
│                                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ FLOAT ]  focus float  move  resize  hide  close                                        focus:float
```

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
[ FLOAT ]  cycle  raise  lower  move  resize                                               floating:2
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
│                        │ updates all panes attached to this terminal       │                      │
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
│ updates all panes attached to this terminal                          │
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

## 14. kill/remove terminal 后 pane 自动消失

目标：

- terminal 被明确移除
- 所有绑定 pane 一并关闭

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
┌─ logs [Primary] ─────────────────────────────────────────────────────────────────────────────────┐
│ tail -f app.log                                                                                    │
│                                                                                                   │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
[ notice ] terminal 'shared-shell' removed; closed 2 bound panes
```

如果是另一个客户端执行的 remove：

```text
[ notice ] terminal 'shared-shell' was removed by another client
```

如果系统能识别身份：

```text
[ notice ] terminal 'shared-shell' was removed by lozzow@host
```

多人共享时的确认弹窗建议：

```text
┌─ Remove Shared Terminal [Warn] ─────────────────────────────────────────┐
│ This terminal is attached by 2 clients and 3 panes.                    │
│ Removing it will close all bound panes for everyone.                   │
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
