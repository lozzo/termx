# termx TUI 线框图

状态：Draft v3
日期：2026-03-25

## 1. 默认单 pane

```text
[main] [1:shell]                                             pane:shell-1  term:term-1
┌─ shell-1                                              ● running  owner ─────────────┐
│ $                                                                             │
│                                                                               │
│                                                                               │
│                                                                               │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
Ctrl-p pane  Ctrl-t tab  Ctrl-w ws  Ctrl-o float  Ctrl-f pick  Ctrl-g global
```

要求：

- 首屏必须直接可输入
- pane 是主视觉主体
- 顶栏和底栏只保留最小导航

## 2. split workbench

```text
[main] [1:dev] [2:logs]                                      pane:api-dev  term:term-1
┌─ api-dev                                      ● running owner ─┬─ build-log ● running owner ─┐
│ $ npm run dev                                                    │ tail -f build.log          │
│ ready on :3000                                                   │ [12:01] ok                │
│                                                                  │                           │
│                                                                  │                           │
└──────────────────────────────────────────────────────────────────┴───────────────────────────┘
Ctrl-p pane  Ctrl-t tab  Ctrl-w ws  Ctrl-o float  Ctrl-f pick  Ctrl-g global
```

要求：

- split 后两个 pane 都是真实 terminal 表面
- active pane 边框和标题必须一眼可见

## 3. floating workbench

```text
[main] [1:dev]                                               pane:float-1  term:term-2  float:1
┌─ api-dev [Muted] ──────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                 │
│ ready on :3000                                                                                │
│                                                                                               │
│                 ┌─ htop                                         ● running owner  z:2 ───────┐ │
│                 │ cpu  mem  proc                                                           │ │
│                 │ ...                                                                       │ │
│                 └───────────────────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────────────────────────────┘
Ctrl-o float  Tab next  hjkl move  HJKL size  [ ] z  c center  x close
```

要求：

- floating 必须压在 tiled workbench 上
- 需要真实 overlap / clipping / z-order

## 4. overlay

```text
[main] [1:dev]
┌─ api-dev [Muted] ──────────────────────────────────────────────────────────────────────────────┐
│ $ npm run dev                                                                                 │
│                                                                                               │
│                         ┌─ Terminal Picker ─────────────────────────────────┐                  │
│                         │ search: api_                                      │                  │
│                         │                                                    │                  │
│                         │ > api-dev                                          │                  │
│                         │   api-log                                          │                  │
│                         │                                                    │                  │
│                         │ Enter attach   Ctrl-t new tab   Esc close          │                  │
│                         └────────────────────────────────────────────────────┘                  │
└───────────────────────────────────────────────────────────────────────────────────────────────┘
```

要求：

- overlay 是盖板，不是主工作台
- 底下 workbench 仍可辨认
- 关闭后不残影
