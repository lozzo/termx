# TUI 线稿集（Wireframes）

本文档是 termx TUI 各状态的视觉参考。所有线稿使用 Unicode box drawing 字符绘制。

## Frame 1：初始状态（单 Viewport）

```
┌─ [1:shell] ──────────────────────────────────────── ws:default ─┐
│                                                                 │
│                                                                 │
│                                                                 │
│  ~/project $                                                    │
│  ▉                                                              │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ T1 zsh │ fit │ 0m03s │                              C-a ? help │
└─────────────────────────────────────────────────────────────────┘
 ▲ tab bar                                            status bar ▲
```

## Frame 2：垂直分屏（C-a %）

```
┌─ [1:shell] ──────────────────────────────────────── ws:default ─┐
│                              │                                  │
│  ~/project $ vim .           │  ~/project $ make build          │
│                              │  building...                     │
│  ~ VIM                       │  [1/5] compiling main.go         │
│  ~                           │  [2/5] compiling server.go       │
│  ~                           │  [3/5] compiling handler.go      │
│  ~                           │  [4/5] linking                   │
│  ~                           │  [5/5] done ✓                    │
│  ~                           │                                  │
│  ~                           │  ~/project $                     │
│  ~                           │  ▉                               │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  -- INSERT --                │                                  │
├─────────────────────────────────────────────────────────────────┤
│ T2 make build │ fit │ 1m42s │ role=build │              C-a ? │
└─────────────────────────────────────────────────────────────────┘
                                ▲ 活跃 Viewport 的 Terminal 信息
```

## Frame 3：三分屏（左大右二）

```
┌─ [1:coding] [2:git] ────────────────────────────────── ws:dev ─┐
│                              │                                  │
│  ~/project $ vim .           │  ~/project $ make watch          │
│                              │  watching for changes...         │
│  ~ VIM                       │  [rebuild] OK 0.3s              │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           ├──────────────────────────────────┤
│  ~                           │                                  │
│  ~                           │  $ tail -f logs/app.log          │
│  ~                           │  [INFO] request /api/users 200   │
│  ~                           │  [INFO] request /api/items 200   │
│  ~                           │  [WARN] slow query 1.2s          │
│  ~                           │  [INFO] request /api/auth 200    │
│  ~                           │  ▉                               │
│  ~                           │                                  │
│  -- INSERT --                │                                  │
├─────────────────────────────────────────────────────────────────┤
│ T3 tail -f logs/app.log │ fit │ 2h13m │ role=log │      C-a ? │
└─────────────────────────────────────────────────────────────────┘
  ▲ tab 1 活跃    tab 2 非活跃
```

## Frame 4：浮动 Viewport（C-a w）

```
┌─ [1:coding] [2:git] ────────────────────────────────── ws:dev ─┐
│                              │                                  │
│  ~/project $ vim .           │  ~/project $ make watch          │
│                              │  watching for changes...         │
│  ~ VIM        ┌─ T5 claude-code ─── [floating] ──────────┐     │
│  ~            │                                           │     │
│  ~            │  claude> I'll fix the race condition in   │     │
│  ~            │  server.go. Let me read the file first... │     │
│  ~            │                                           │     │
│  ~            │  $ cat server.go                          │     │
│  ~            │  ...                                      │     │
│  ~            │  $ vim server.go                          │     │
│  ~            │  ...editing...                            │     │
│  ~            │                                           │     │
│  ~            │  claude> Done. Running tests...           │     │
│  ~            │  $ make test                              │     │
│  ~            └───────────────────────────────────────────┘     │
│  -- INSERT --                │                                  │
├─────────────────────────────────────────────────────────────────┤
│ T5 claude-code │ fixed 80x16 │ 5m21s │ role=ai-agent │  C-a ? │
└─────────────────────────────────────────────────────────────────┘
                   ▲ 浮动 Viewport 聚焦时显示其 Terminal 信息
```

## Frame 5：多个浮动 Viewport（z-order 堆叠）

```
┌─ [1:coding] ─────────────────────────────────────────── ws:dev ─┐
│                                                                  │
│  ~/project $ vim .                                               │
│                                                                  │
│  ~ VIM    ┌─ T6 htop ─── [floating z:1] ──────────────────┐     │
│  ~        │                                                │     │
│  ~        │  1  [||||||||||||||||       62%]  Tasks: 142   │     │
│  ~        │  2  [||||||||||             41%]  Load: 1.23   │     │
│  ~        │  3  [|||||                  22%]                │     │
│  ~        │  4  [|||                    15%]                │     │
│  ~        │                                                │     │
│  ~        │  PID  USER  CPU%  MEM%  COMMAND                │     │
│  ~        │  1234 root  45.2  3.1   node server.js         │     │
│  ~        └──────────────────────────┐                     │     │
│  ~            ┌─ T5 bash ─── [z:2] ──┼─────────────┐      │     │
│  ~            │                      │              │      │     │
│  ~            │  $ docker ps         │              │      │     │
│  ~            │  CONTAINER  STATUS   │              │      │     │
│  ~            │  api        Up 2h    │              │      │     │
│               │  db         Up 2h    │              │      │     │
│               └──────────────────────┴──────────────┘      │     │
│                                                                  │
├──────────────────────────────────────────────────────────────────┤
│ T5 bash │ fit │ 0m15s │                                   C-a ? │
└──────────────────────────────────────────────────────────────────┘
            ▲ z:2 在 z:1 上面，部分遮挡
```

## Frame 6：Terminal Picker（C-a f）

```
┌─ [1:coding] [2:git] ────────────────────────────────── ws:dev ─┐
│                                                                 │
│  ┌─ Find Terminal ──────────────────────────────────────────┐   │
│  │  > log                                                   │   │
│  │                                                          │   │
│  │  ● T3  tail -f app.log     ws:dev / coding    fit  2h13m│   │
│  │  ● T7  tail -f worker.log  ws:ops / logs      fit  1h05m│   │
│  │  ○ T9  tail -f nginx.log   (orphan)                45m  │   │
│  │  ○ T11 tail -f redis.log   (orphan)                22m  │   │
│  │                                                          │   │
│  │  ● = Viewport 观察中                                      │   │
│  │  ○ = 无人观察（orphan）                                    │   │
│  │                                                          │   │
│  │  [Enter] attach   [Tab] 分屏打开   [C-k] kill            │   │
│  │                                                          │   │
│  │  4 matches                                               │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ T3 tail -f app.log │ fit │ 2h13m │ role=log │           C-a ? │
└─────────────────────────────────────────────────────────────────┘
```

## Frame 7：Workspace Picker（C-a s）

```
┌─ [1:coding] [2:git] ────────────────────────────────── ws:dev ─┐
│                                                                 │
│  ┌─ Switch Workspace ──────────────────────────────────────┐    │
│  │                                                         │    │
│  │  ▸ dev          3 tabs   8 terminals                    │    │
│  │    ops          2 tabs   5 terminals                    │    │
│  │    monitoring   1 tab    4 terminals                    │    │
│  │                                                         │    │
│  │  ──────────────────────────────────────                 │    │
│  │  [Enter] 切换   [n] 新建   [d] 删除   [r] 重命名        │    │
│  │                                                         │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ ws:dev │ 3 tabs │ 8 terminals │                          C-a ? │
└─────────────────────────────────────────────────────────────────┘
```

## Frame 8：Zoom 模式（C-a z）

```
┌─ [1:coding] [2:git] ──────────────────────── [ZOOM] ── ws:dev ─┐
│                                                                 │
│  ~/project $ vim .                                              │
│                                                                 │
│  ~ VIM - server.go                                              │
│  ~                                                              │
│    1  package main                                              │
│    2                                                            │
│    3  import (                                                  │
│    4      "context"                                             │
│    5      "fmt"                                                 │
│    6      "net/http"                                            │
│    7  )                                                         │
│    8                                                            │
│    9  func main() {                                             │
│   10      srv := &http.Server{Addr: ":8080"}                   │
│   11      srv.ListenAndServe()                                  │
│   12  }                                                         │
│  -- INSERT --                                                   │
├─────────────────────────────────────────────────────────────────┤
│ T1 vim . │ fit │ 15m03s │ role=editor │ C-a z to unzoom │      │
└─────────────────────────────────────────────────────────────────┘
                                          ▲ zoom 提示
```

## Frame 9：Copy/Scroll 模式（C-a [）

```
┌─ [1:coding] ──────────────────────────── [COPY] ─────── ws:dev ─┐
│                              │                                   │
│  ~/project $ make test       │  ~/project $ make watch           │
│  === RUN   TestServer        │  watching...                      │
│  --- PASS: TestServer (0.3s) │                                   │
│  === RUN   TestClient        │                                   │
│  --- PASS: TestClient (0.1s) │                                   │
│  ████████████████████████████│                                   │
│  === RUN   TestResize        │                                   │
│  --- FAIL: TestResize (0.0s) │                                   │
│      resize_test.go:42:      │                                   │
│      expected 80, got 0      │                                   │
│  ████████████████████████████│                                   │
│  FAIL                        │                                   │
│  exit status 1               │                                   │
│                              │                                   │
│  ~/project $                 │                                   │
│                              │                                   │
├──────────────────────────────────────────────────────────────────┤
│ COPY │ line 142/380 │ search: /TestResize │ [v]sel [y]copy [q]uit│
└──────────────────────────────────────────────────────────────────┘
  ▲ 模式标记    滚动位置    搜索高亮         ▲ 操作提示
  ████ = 选中区域（反色显示）
```

## Frame 10：Command 模式（C-a :）

```
┌─ [1:coding] [2:git] ────────────────────────────────── ws:dev ─┐
│                              │                                  │
│  ~/project $ vim .           │  ~/project $ make watch          │
│                              │  watching for changes...         │
│  ~ VIM                       │  [rebuild] OK 0.3s              │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           ├──────────────────────────────────┤
│  ~                           │                                  │
│  ~                           │  $ tail -f logs/app.log          │
│  ~                           │  [INFO] request /api/users 200   │
│  ~                           │  [INFO] request /api/items 200   │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  -- INSERT --                │                                  │
├─────────────────────────────────────────────────────────────────┤
│ :split -v█                                                      │
└─────────────────────────────────────────────────────────────────┘
  ▲ 命令输入替代状态栏
```

## Frame 11：程序退出（[exited] 状态）

```
┌─ [1:coding] [2:git] ────────────────────────────────── ws:dev ─┐
│                              │                                  │
│  ~/project $ vim .           │  ~/project $ make watch          │
│                              │  watching for changes...         │
│  ~ VIM                       │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           ├──────────────────────────────────┤
│  ~                           │                                  │
│  ~                           │  ┌────────────────────────────┐  │
│  ~                           │  │                            │  │
│  ~                           │  │  [exited] exit code 1      │  │
│  ~                           │  │                            │  │
│  ~                           │  │  (r) restart               │  │
│  ~                           │  │  (c) close viewport        │  │
│  ~                           │  │                            │  │
│  -- INSERT --                │  └────────────────────────────┘  │
├─────────────────────────────────────────────────────────────────┤
│ T3 tail -f app.log │ exited │ code=1 │ role=log │       C-a ? │
└─────────────────────────────────────────────────────────────────┘
                       ▲ 状态变为 exited
```

## Frame 12：Prefix Key 激活状态

```
┌─ [1:coding] [2:git] ────────────────────────────────── ws:dev ─┐
│                              │                                  │
│  ~/project $ vim .           │  ~/project $ make watch          │
│                              │  watching for changes...         │
│  ~ VIM                       │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           ├──────────────────────────────────┤
│  ~                           │                                  │
│  ~                           │  $ tail -f logs/app.log          │
│  ~                           │  [INFO] request /api/users 200   │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  -- INSERT --                │                                  │
├─────────────────────────────────────────────────────────────────┤
│ C-a ▎waiting for key...                                         │
└─────────────────────────────────────────────────────────────────┘
  ▲ prefix 激活，等待后续按键
```

## Frame 13：Help 浮窗（C-a ?）

```
┌─ [1:coding] ─────────────────────────────────────────── ws:dev ─┐
│                                                                  │
│  ┌─ Keybindings ────────────────────────────────────────────┐    │
│  │                                                          │    │
│  │  Viewport                    Tab                         │    │
│  │  ─────────                   ───                         │    │
│  │  C-a "    split horizontal   C-a c    new tab            │    │
│  │  C-a %    split vertical     C-a ,    rename tab         │    │
│  │  C-a x    close viewport     C-a 1-9  go to tab N        │    │
│  │  C-a X    kill terminal      C-a n/p  next/prev tab      │    │
│  │  C-a z    zoom toggle        C-a &    close tab          │    │
│  │  C-a hjkl navigate                                       │    │
│  │  C-a HJKL resize            Workspace                    │    │
│  │  C-a {}   swap position     ──────────                   │    │
│  │  C-a Space cycle layout      C-a s    switch workspace   │    │
│  │                              C-a $    rename workspace   │    │
│  │  Floating                    C-a d    detach             │    │
│  │  ────────                                                │    │
│  │  C-a w    new floating       Mode                        │    │
│  │  C-a W    toggle floating    ────                        │    │
│  │  C-a Tab  cycle float focus  C-a M    toggle fit/fixed   │    │
│  │  C-a ]    raise to top       C-a R    toggle readonly    │    │
│  │  C-a _    lower to bottom    C-a P    pin viewport       │    │
│  │  Esc      back to tiling     C-a C-hjkl  pan offset     │    │
│  │                                                          │    │
│  │  Other                                                   │    │
│  │  ─────                                                   │    │
│  │  C-a f    find terminal      C-a :    command mode       │    │
│  │  C-a [    copy/scroll mode   C-a C-a  send literal C-a  │    │
│  │  C-a ?    this help                                      │    │
│  │                                                          │    │
│  │                                          [q] close help  │    │
│  └──────────────────────────────────────────────────────────┘    │
│                                                                  │
├──────────────────────────────────────────────────────────────────┤
│ help │ press q to close │                                        │
└──────────────────────────────────────────────────────────────────┘
```

## Frame 14：Fixed 模式 Viewport（裁剪显示）

```
┌─ [1:monitor] ────────────────────────────────────────── ws:ops ─┐
│                                                                  │
│  ┌─ T2 htop ─ [fit] ────────────────────────────────────────┐   │
│  │                                                           │   │
│  │  1  [||||||||||||||||       62%]  Tasks: 142, 3 running   │   │
│  │  2  [||||||||||             41%]  Load: 1.23 0.98 0.76    │   │
│  │  3  [|||||                  22%]  Uptime: 14 days         │   │
│  │  4  [|||                    15%]  Mem: 3.2G/16G           │   │
│  │                                                           │   │
│  │  PID   USER   CPU%  MEM%  TIME+    COMMAND                │   │
│  │  1234  root   45.2  3.1   12:34.5  node server.js         │   │
│  │  5678  www    12.1  1.2   05:21.3  nginx: worker          │   │
│  │                                                           │   │
│  └───────────────────────────────────────────────────────────┘   │
│  ┌─ T8 vim ─ [fixed 120x40] ─ offset(20,5) ─────── [pinned] ┐  │
│  │                                                            │  │
│  │    22      srv.Handle("/api", apiHandler)                  │  │
│  │    23      srv.Handle("/ws", wsHandler)                    │  │
│  │    24  }   ← 只显示 Terminal 的一部分                       │  │
│  │    25                                                      │  │
│  └────────────────────────────────────────────────────────────┘  │
├──────────────────────────────────────────────────────────────────┤
│ T8 vim │ fixed 120x40 │ pinned │ offset(20,5) │          C-a ? │
└──────────────────────────────────────────────────────────────────┘
           ▲ fixed 模式信息         ▲ 锚定状态和偏移量
```

## Frame 15：Tab 重命名（C-a ,）

```
┌─ [1:█─────────────] [2:git] ────────────────────────── ws:dev ─┐
│                              │                                  │
│  ~/project $ vim .           │  ~/project $ make watch          │
│                              │  watching for changes...         │
│  ~ VIM                       │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  ~                           │                                  │
│  -- INSERT --                │                                  │
├─────────────────────────────────────────────────────────────────┤
│ rename tab: coding█                                  [Esc] cancel│
└─────────────────────────────────────────────────────────────────┘
  ▲ 内联编辑 tab 名称
```

## Frame 16：最小尺寸折叠

```
窗口很小时，空间不够的 Viewport 被折叠：

┌─ [1:coding] ──── ws:dev ─┐
│                           │
│  ~/project $ vim .        │
│                           │
│  ~ VIM                    │
│  ~                        │
│  ~                        │
│  ~              [··· ×2]  │
│  ~                ▲       │
│  ~          2 个 Viewport │
│  ~          被折叠         │
│  -- INSERT --             │
├───────────────────────────┤
│ T1 vim │ fit │ [2 hidden] │
└───────────────────────────┘
```

## 组件拆解

```
┌─ Tab Bar ──────────────────────────────────────── Workspace ─┐
│  [N:name]  活跃 tab 高亮                        ws:name 右对齐│
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─ Viewport ─────────────────────────────────────────┐     │
│  │                                                     │     │
│  │  边框：活跃=亮色  非活跃=暗色                         │     │
│  │  标题：Terminal command + mode 标记                   │     │
│  │                                                     │     │
│  │  内容区：                                            │     │
│  │    fit 模式 → 完整显示 Terminal 屏幕                  │     │
│  │    fixed 模式 → 裁剪显示，标题栏显示 offset           │     │
│  │                                                     │     │
│  │  光标：                                              │     │
│  │    活跃 Viewport → 显示闪烁光标                       │     │
│  │    非活跃 Viewport → 不显示光标                       │     │
│  │                                                     │     │
│  └─────────────────────────────────────────────────────┘     │
│                                                             │
│  ┌─ Floating Viewport ──── [floating] ──────────────┐       │
│  │  标题栏额外显示 [floating] 标记和 z-order          │       │
│  │  边框颜色区别于平铺层（如粉色）                     │       │
│  └──────────────────────────────────────────────────┘       │
│                                                             │
├─ Status Bar ────────────────────────────────────────────────┤
│  Terminal 信息 │ mode │ 运行时间 │ tags │        快捷键提示  │
└─────────────────────────────────────────────────────────────┘

颜色方案（默认）：
  Tab 活跃:        #89b4fa (蓝)
  Tab 非活跃:      #585b70 (灰)
  Viewport 活跃边框: #89b4fa (蓝)
  Viewport 非活跃:   #585b70 (灰)
  浮动边框:         #f5c2e7 (粉)
  状态栏背景:       #1e1e2e (深色)
  状态栏文字:       #cdd6f4 (浅色)
  [exited] 标记:   #f38ba8 (红)
  [ZOOM] 标记:     #a6e3a1 (绿)
  [COPY] 标记:     #fab387 (橙)
```
