# tuiv2 Render / Perf Notes (2026-04-09)

## Goal

记录当前 `termx` 在 `tuiv2` 路径上的渲染链路、已经落地的滚动性能优化，以及继续优化端到端输入延迟时应该优先看的位置。

这份文档对应的代码基线从提交 `334546e` 开始。

## Current Pipeline

### 1. Server-side PTY path

输入链路:

1. client 通过 protocol `Input` 把按键/鼠标发送到 server。
2. server 把字节写入 PTY。
3. PTY output 在 `Terminal.readLoop` 中被读取。

输出链路:

1. live output 先进入 terminal 的 live stream。
2. 连续 `StreamOutput` 会在 server 侧先合并，再往订阅者广播。
3. server 自己的 `VTerm` 不再追着每个小 chunk 立即更新，而是延迟到正确性边界时再 flush。

关键文件:

- `terminal.go`
- `protocol/client.go`

### 2. Client runtime path

attach 后，client runtime 维护一份本地权威可见状态:

1. `runtime/stream.go` 接收 protocol stream。
2. 连续 `TypeOutput` 在 client 侧再做一次短窗口合批。
3. 合并后的 output 只触发一次本地 `VTerm.Write()`。
4. stream 侧只在真正需要时请求 `Invalidate`。

关键文件:

- `tuiv2/runtime/stream.go`
- `tuiv2/runtime/runtime.go`
- `tuiv2/runtime/terminal_registry.go`

### 3. App / render path

前台渲染链路:

1. runtime 通过 `Model.queueInvalidate()` 发送 `InvalidateMsg`。
2. `Update()` 处理消息，必要时标记 render dirty。
3. `View()` 调 `render.Coordinator.RenderFrame()`。
4. 默认 TTY 路径下，frame 直接交给 `outputCursorWriter`，不再走 Bubble Tea 标准 renderer。

关键文件:

- `tuiv2/app/model.go`
- `tuiv2/app/update.go`
- `tuiv2/app/view.go`
- `tuiv2/app/cursor_writer.go`
- `tuiv2/render/coordinator.go`

## Render Ownership

当前系统里有两份 terminal state:

1. server 侧 `VTerm`
   用于 snapshot / bootstrap / restart / resize / late attach 的权威恢复。
2. client 侧 `VTerm`
   用于 live attach 时的本地即时渲染。

正常 live scroll 期间，用户看到的是 client 侧 `VTerm + render coordinator` 的结果。server 侧 `VTerm` 现在更偏向恢复和边界一致性，不再参与每个滚动 chunk 的热路径。

## What Was Slow

最初的热点不在 `Bubble Tea Update()`，而在下面几层:

1. `vterm.Write()`
   之前每次 write 都会整屏抓 `before/after` screen，再做 row metadata reconcile。
2. server/client 双重 `VTerm.Write()`
   同一批 PTY output 会在 server 写一遍、client 再写一遍。
3. 前台重复 `View()/RenderFrame()`
   即使 runtime 只 invalidate 一次，滚动输入本身仍会驱动多轮 `Update -> View`。
4. direct writer backlog 下的重复 frame flush。

## Optimizations Landed

### A. Stream / protocol batching

目标:

- 不再把一次 `nvim` scroll 拆成大量中间态 frame。

已落地:

1. protocol client 连续 `TypeOutput` 合并。
2. stream 溢出不再 silent drop，改为显式 `SyncLost`。
3. server live stream 合并连续 output。
4. client runtime stream 再合并连续 output。

效果:

- `runtime.stream.output` 从多次下降到大多数滚动场景 1 次。

### B. Server-side lazy VTerm flush

目标:

- 把 server `VTerm` 从 live scroll 热路径里挪开。

已落地:

1. 普通 live output 先缓存，不立即写 server `VTerm`。
2. `Subscribe / Snapshot / Resize / Restart / Exit` 等边界前强制 flush。
3. 保留阈值，避免 pending output 无界增长。

效果:

- 同一串 scroll 的 `vterm.write.count` 从 `2` 压到 `1`。

### C. VTerm metadata reconcile fingerprinting

目标:

- 去掉每次 `Write()` 的整屏 `[]Cell` materialize / compare。

已落地:

1. screen / scrollback 改为 row fingerprint。
2. reconcile 只按指纹和尾部 scrollback 行对齐。
3. `perftrace` 对 `before_snapshot / emulator / reconcile` 分段计时。

关键文件:

- `vterm/vterm.go`
- `vterm/spike_test.go`
- `perftrace/perftrace.go`

效果:

- `vterm.write` 的大头从 metadata bookkeeping 转移到了 emulator 本体。

### D. Direct writer backlog / empty frame suppression

目标:

- writer 没 drain 时不继续生成旧的中间 redraw。

已落地:

1. frame writer backlog 会 defer invalidate。
2. 空 payload / cursor-only 无效同步帧被压掉。
3. batch delay 从固定节流改成更短窗口和更激进的 idle flush。

关键文件:

- `tuiv2/app/cursor_writer.go`

### E. Skip empty front-end re-render

目标:

- direct writer 模式下，输入消息不要把 `View()` 拖成重复整帧渲染。

已落地:

1. `Coordinator` 暴露 `CachedFrameAndCursor()`。
2. `View()` 在 `stateKey` 未变化且 render 不 dirty 时直接复用上一帧。
3. 保留 `stateKey` 检查，避免吞掉没有显式 `Invalidate()` 的可见状态变化。

关键文件:

- `tuiv2/render/coordinator.go`
- `tuiv2/app/view.go`

效果:

- `render.frame` 在 burst scroll 场景里从 `17/33` 次降到 `1` 次。

## Perf Harness

为了避免继续凭体感猜，仓库里现在有两套对照工具:

### 1. Native perf harness

文件:

- `tuiv2/app/perf_nvim_scroll_test.go`
- `scripts/nvim-scroll-perf.sh`
- `perftrace/perftrace.go`

用途:

- 起一个真实 `nvim`
- 执行固定 scroll action
- 输出每个 action 的 metric JSON

常用命令:

```bash
TERMX_RUN_NVIM_TRACE=1 TERMX_PERF_OUT=/tmp/termx-perf.json \
go test ./tuiv2/app -run TestPerfNvimScrollReport -count=1 -v
```

### 2. Web/xterm.js compare client

文件:

- `cmd/termx/web.go`
- `cmd/termx/web/index.html`

用途:

- 用浏览器侧 `xterm.js` attach 同一 terminal
- 和本地 TUI 做滚动手感对照

常用命令:

```bash
termx web -- nvim -u NONE your-file
```

## Latest Known Numbers

对比基线:

- 旧报告: `/tmp/termx-perf-single-vterm/nvim-scroll.json`
- 新报告: `/tmp/termx-perf-view-reuse/nvim-scroll.json`

关键变化:

1. `down_burst_8`
   - `app.update`: `17 -> 3`
   - `render.frame`: `17 -> 1`
   - `cursor_writer.direct_flush`: `3 -> 1`

2. `up_burst_8`
   - `app.update`: `17 -> 3`
   - `render.frame`: `17 -> 1`
   - `vterm.write.count`: 仍为 `1`

3. `alternating_16`
   - `app.update`: `33 -> 4`
   - `render.frame`: `33 -> 1`

这说明当前剩余问题已经不再主要是“重复整帧渲染”。

## Current Bottleneck Hypothesis

在当前基线下，下一阶段更应该看端到端输入延迟，而不是继续抠 render:

1. input forwarder 到 Bubble Tea 的排队时间
2. `runtime.SendInput()` 到 server PTY 的往返时间
3. server 从 PTY 收到 output 到 stream 广播的等待时间
4. client stream 收到 output 到 `VTerm.Write()` 的时间

换句话说，下一阶段应该回答的问题是:

“为什么 frame 已经只渲染一次了，滚动还是不够跟手？”

## Recommended Next Measurements

下一步建议直接做时间戳打点，而不是继续改缓存策略:

1. 在 input forwarder 给每个输入打本地 seq + timestamp。
2. 在 `runtime.SendInput()` 前后打点。
3. 在 server `Input` handler、PTY write、PTY output read 打点。
4. 在 client stream 收到对应 output 时闭环。

目标不是先做优化，而是先拿到:

- input enqueue delay
- network / protocol delay
- PTY response delay
- server batching delay
- client apply/render delay

只有这条链路完整后，后面的优化才不会继续靠猜。
