# tui-v3 Unicode 界面线稿

状态：候选稿
日期：2026-06-06

## 1. 目的

本文档用 Unicode box drawing 先把 `termx-tui-v3` 的目标界面画出来，供人工对齐。它不是实现结果，也不代表所有按钮已经接线。

线稿规则：

- 只画当前产品契约已经定义的结构：workspace、tab、pane、floating、Terminal Picker、Terminal Pool、Workbench Tree、footer/status。
- 不画 owner/share/lifecycle/Nerd Font 状态 token；这些必须等语义、字形、fallback 和 hit region 单独设计后再进入界面。
- 可见按钮只使用已经定义的动作词或基础符号：`×`、`＋`、`↔`、`↕`、`Attach`、`New`、`Close`。
- daemon 不理解 workbench；workspace/tab/pane 是 TUI schema 对 daemon storage 的客户端投影。

## 2. 默认 Workbench

目标：默认进入后是一张稳定的终端工作台，不是调试信息页。Header 只表达全局上下文，Pane 只表达局部工作位，Footer 只表达 mode 和短摘要。

```text
┌ main ─┬─ 1:shell × ─┬─ 2:logs × ─┬─ ＋ ──────────────────────────────── termx ┐
│       │             │            │                                           │
├───────┴─────────────┴────────────┴───────────────────────────────────────────┤
│┌ shell ──────────────────────────────────────────────── ↕  ↔  × ┐┌ logs ─ × ┐│
││ termx git:termx-core-v2-tui-v3-migration  go v1.26.0            ││          ││
││ > ls -l                                                         ││ tail -f  ││
││ drwxr-xr-x  termx-core-v2                                      ││ app.log  ││
││ drwxr-xr-x  termx-tui-v3                                       ││          ││
││ drwxr-xr-x  termx-cli                                          ││          ││
││ >                                                               ││          ││
││                                                                 ││          ││
││                                                                 ││          ││
│└─────────────────────────────────────────────────────────────────┘└──────────┘│
├───────────────────────────────────────────────────────────────────────────────┤
│ [Ctrl+P] pane  [Ctrl+R] resize  [Ctrl+F] picker  [Ctrl+G] global   ws:main 2p │
└───────────────────────────────────────────────────────────────────────────────┘
```

对齐点：

- Header tab 的 `×` 和 `＋` 是稳定命中区。
- Pane action 只显示当前已设计并应接线的 split-down、split-right、close。
- Pane title 左侧允许可变文本；右侧动作槽位固定。
- Footer 左侧是当前可用入口，右侧是短摘要；不放长帮助和调试文案。

## 3. Split Line 模式

目标：减少重复边框，但仍保留 active pane 识别、content rect 和 action hit region。

```text
┌ main ─┬─ 1:shell × ─┬─ 2:logs × ─┬─ ＋ ──────────────────────────────── termx ┐
├───────┴─────────────┴────────────┴───────────────────────────────────────────┤
│ shell ─────────────────────────────────────────────── ↕  ↔  × │ logs ─── ×   │
│ termx git:termx-core-v2-tui-v3-migration  go v1.26.0           │ tail -f      │
│ > make test                                                     │ app.log      │
│ ok   termx-tui-v3/render                                        │              │
│ ok   termx-tui-v3/app                                           │              │
│ >                                                               │              │
│                                                                 │              │
│─────────────────────────────────────────────────────────────────┼──────────────│
│ editor ──────────────────────────────────────────────────── ×    │ shell ─ ×    │
│ README.md                                                       │ >            │
│ workflow.md                                                     │              │
├───────────────────────────────────────────────────────────────────────────────┤
│ [Pane] h/j/k/l focus  ↕ split-down  ↔ split-right  × close        ws:main 4p │
└───────────────────────────────────────────────────────────────────────────────┘
```

对齐点：

- split line 不改变 pane/terminal 绑定语义。
- 共享分隔线可拖动 resize；chrome 区域命中优先于 terminal mouse passthrough。
- action token 必须来自同一 action item 列表，不能按字符串临时搜索。

## 4. Empty Pane

目标：未连接 terminal 的 pane 不是空白，必须给出最短恢复路径。

```text
┌ main ─┬─ 1:shell × ─┬─ ＋ ───────────────────────────────────────────── termx ┐
├───────┴─────────────┴───────────────────────────────────────────────────────┤
│┌ shell ───────────────────────────────────────────────────────── ↕  ↔  × ┐  │
││ >                                                                        │  │
││                                                                          │  │
│└──────────────────────────────────────────────────────────────────────────┘  │
│┌ empty ─────────────────────────────────────────────────────────────── × ┐   │
││                                                                          │  │
││   No terminal attached                                                   │  │
││                                                                          │  │
││   Attach existing        New terminal        Terminal Pool        Close   │  │
││                                                                          │  │
│└──────────────────────────────────────────────────────────────────────────┘  │
├───────────────────────────────────────────────────────────────────────────────┤
│ [Ctrl+F] picker  [Ctrl+G] global                                ws:main 2p  │
└───────────────────────────────────────────────────────────────────────────────┘
```

对齐点：

- `Attach existing` 是 primary。
- `New terminal` 必须带默认 shell 或明确 command，不能再触发 invalid command。
- `Terminal Pool` 进入管理页。
- `Close` 关闭工作位，不 kill terminal。

## 5. Floating Pane

目标：floating 是完整 pane。它打开时可以聚焦；点击未遮挡 tiled pane 时 floating 失焦但保持打开。

```text
┌ main ─┬─ 1:shell × ─┬─ 2:logs × ─┬─ ＋ ──────────────────────────────── termx ┐
├───────┴─────────────┴────────────┴───────────────────────────────────────────┤
│┌ shell ──────────────────────────────────────────────── ↕  ↔  × ┐┌ logs ─ × ┐│
││ > make dev                                                      ││ tail -f  ││
││                                                                 ││ app.log  ││
││                 ┌ floating:scratch ─────────────────────── × ┐  ││          ││
││                 │ > notes                                   │  ││          ││
││                 │                                           │  ││          ││
││                 │                                           │  ││          ││
││                 │                                      resize│  ││          ││
││                 └────────────────────────────────────────────┘  ││          ││
│└─────────────────────────────────────────────────────────────────┘└──────────┘│
├───────────────────────────────────────────────────────────────────────────────┤
│ [Ctrl+O] floating  h/j/k/l move  H/J/K/L resize  x close         ws:main 2p │
└───────────────────────────────────────────────────────────────────────────────┘
```

对齐点：

- floating title bar 可作为 raise/move drag 区。
- 右下角 resize handle 可拖动。
- floating active 时后方 tiled pane 视觉降级；点击 tiled pane 后 floating 变 inactive。

## 6. Terminal Picker

目标：快速把当前 pane 连接到 terminal。它是 page-sized overlay，不是小输入框。

```text
┌ main ─┬─ 1:shell × ─┬─ ＋ ───────────────────────────────────────────── termx ┐
├───────┴─────────────┴───────────────────────────────────────────────────────┤
│┌ Terminal Picker ─────────────────────────────────────────────────────────┐│
││ Search  shell                                                            ││
│├──────────────────────────────────────────────────────────────────────────┤│
││ Name                 State       Source        Target                     ││
││ shell-main           running     pool          current pane               ││
││ logs                 running     pool          split or attach            ││
││ scratch              exited      pool          restart or reconnect        ││
││                                                                          ││
│├──────────────────────────────────────────────────────────────────────────┤│
││ Selected: shell-main                                                     ││
││ Attach Selected        Split + Attach        New Shell        Close       ││
│└──────────────────────────────────────────────────────────────────────────┘│
├───────────────────────────────────────────────────────────────────────────────┤
│ [Picker] type filter  ↑/↓ select  Enter attach  Esc close          ws:main   │
└───────────────────────────────────────────────────────────────────────────────┘
```

对齐点：

- row click 选择或提交，不穿透到底层 terminal。
- `Split + Attach`、edit、kill、delete 等动作显示前必须接线。
- footer 不显示长帮助句，只显示当前模式短动作。

## 7. Terminal Pool

目标：全局 terminal 管理页，负责列表、详情、预览和 terminal lifecycle 动作。

```text
┌ main ─┬─ 1:shell × ─┬─ ＋ ───────────────────────────────────────────── termx ┐
├───────┴─────────────┴───────────────────────────────────────────────────────┤
│┌ Terminal Pool ───────────────────────────────┬ Details ──────────────────┐│
││ Search  shell                                │ shell-main                 ││
││                                              │ state: running             ││
││ RUNNING                                      │ visible: yes               ││
││ shell-main                         current   │ attached panes: 1          ││
││ logs                               parked    │                            ││
││                                              │ ┌ preview ───────────────┐ ││
││ EXITED                                       │ │ > ls -l                │ ││
││ scratch                            exited    │ │ termx-core-v2          │ ││
││                                              │ └────────────────────────┘ ││
│├──────────────────────────────────────────────┴────────────────────────────┤│
││ Attach Here        New Tab        Floating        Edit        Kill        ││
│└───────────────────────────────────────────────────────────────────────────┘│
├───────────────────────────────────────────────────────────────────────────────┤
│ [Pool] type filter  ↑/↓ select  Enter attach  Esc close            terms:3  │
└───────────────────────────────────────────────────────────────────────────────┘
```

对齐点：

- Terminal Pool 是 page/surface，不是 picker 放大版。
- attach here/tab/floating/edit/kill/delete 都必须走 terminal service/effect result。
- terminal lifecycle 来自 daemon terminal pool，不来自 TUI workbench schema。

## 8. Workbench Tree

目标：结构导航层。它展示 TUI 对 storage value 的 workspace/tab/pane 投影，daemon 不理解这棵树。

```text
┌ main ─┬─ 1:shell × ─┬─ ＋ ───────────────────────────────────────────── termx ┐
├───────┴─────────────┴───────────────────────────────────────────────────────┤
│┌ Workbench Tree ─────────────────────────────┬ Snapshot ──────────────────┐│
││ Search  main                                │ main / shell                ││
││                                              │ tabs: 2                     ││
││ ● main                                      │ panes: 3                    ││
││   ├─ shell                                  │ floating: 1                 ││
││   │  ├─ pane:shell-main                     │                            ││
││   │  └─ pane:logs                           │ ┌ preview ───────────────┐ ││
││   └─ scratch                                │ │ > make test            │ ││
││      └─ pane:notes                          │ │ ok termx-tui-v3        │ ││
││   floating                                  │ └────────────────────────┘ ││
││      └─ scratch                             │                            ││
│├──────────────────────────────────────────────┴────────────────────────────┤│
││ Open        Rename        New        Delete        Close                  ││
│└───────────────────────────────────────────────────────────────────────────┘│
├───────────────────────────────────────────────────────────────────────────────┤
│ [Tree] type filter  ↑/↓ select  Enter open  Esc close              ws:main  │
└───────────────────────────────────────────────────────────────────────────────┘
```

对齐点：

- 左侧是结构树，右侧是 snapshot/details。
- modal 内不展示快捷键教学长文。
- create/rename/delete 走 TUI schema mutation，再通过 daemon storage CAS 写回。

## 9. Prompt / Confirm

目标：短任务输入层，居中但不是装饰卡片。

```text
┌ main ─┬─ 1:shell × ─┬─ ＋ ───────────────────────────────────────────── termx ┐
├───────┴─────────────┴───────────────────────────────────────────────────────┤
│                                                                              │
│                  ┌ Rename Tab ─────────────────────────────┐                 │
│                  │ Name                                    │                 │
│                  │ shell-main                              │                 │
│                  │                                         │                 │
│                  │ Submit                         Cancel   │                 │
│                  └─────────────────────────────────────────┘                 │
│                                                                              │
├───────────────────────────────────────────────────────────────────────────────┤
│ [Prompt] type input  Enter submit  Esc cancel                       ws:main  │
└───────────────────────────────────────────────────────────────────────────────┘
```

对齐点：

- prompt input 可点击定位 cursor。
- destructive confirm 要明确对象名和动作结果。
- overlay 区域阻断底层 terminal input。

## 10. 窄屏退化

目标：在窄屏下保留身份、可关闭动作和当前 mode，不挤压 terminal 内容。

```text
┌ main ┬ 1:shell × ┬ ＋ ───────────────┐
├──────┴───────────┴──────────────────┤
│┌ shell ──────────────────────── × ┐ │
││ >                                │ │
││                                  │ │
│└──────────────────────────────────┘ │
│┌ logs ───────────────────────── × ┐ │
││ tail -f app.log                  │ │
│└──────────────────────────────────┘ │
├─────────────────────────────────────┤
│ [P] pane  [F] picker        ws:main │
└─────────────────────────────────────┘
```

窄屏规则：

- 优先保留 active identity、close token、mode 入口。
- 隐藏低优先级动作，不缩成不可读碎片。
- 不出现横向溢出，也不让 terminal 内容推开边框。
