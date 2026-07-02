# SemanticTap history/copy 执行计划

状态：R372-R377 执行设计。本文只支撑根目录 `workflow.md`，不替代 `workflow.md` 的任务队列、范围和准入规则。

## 1. 当前错误链路

R358 后的实现为了解决 live 压力显示，把真实 PTY 输出拆成两条 vterm 消费链：

```text
PTY bytes -> live SurfaceTrack -> vterm.WriteForLatestFrame -> latest native screen
PTY bytes -> history semantic worker -> vterm.SemanticSource -> history renderer/store
```

这条链路的问题不是单点性能，而是 owner 错误：

- double vterm：live consumer 与 history consumer 各自维护 emulator state。
- history consumer replay raw PTY：history worker backlog 的单位仍是 raw text，追平时再交给 `vterm.SemanticSource`。
- resize 不是同一 sequence：live resize 与 history resize 分别进入两套 vterm，old-size pending bytes 可能被 new-size parser 解释。
- response owner 在 live surface：OSC/DA/DSR 等 response 由 live vterm 回写 PTY；history vterm 虽然禁用 response，但目标架构不能依赖两套 vterm 的人为禁用。
- copy enter 绑定 full history flush：现有 latest/freeze 路径仍以完整 history worker 追平作为 authoritative 入口，压力输出时会卡住交互。
- live native screen 曾被误当作 history 近路：任何用 snapshot、grid rows、LoadedRows、local vterm scrollback 或 current live rows 推断 copy/search/page truth 的方案都属于错误模型。
- 旧 snapshot/history 拼接 fallback、重复同步和局部状态修正不能继续扩展；需要删除时按 `workflow.md` 当前切片边界处理。

## 2. 目标链路

目标链路是 single SemanticTap：

```text
PTY bytes / resize
  -> single SemanticTap
       owns termx-vterm parser, cursor, modes, alt state, resize order, OSC/DA/DSR response
       maintains latest native screen
       emits immutable TerminalSemanticTransaction
  -> live consumer
       latest invalidation / snapshot only; may coalesce render wakeups and stale snapshot responses
  -> history consumer
       complete semantic transaction stream; may async, batch, spill, catch up
```

`SemanticTap` 之前不得按 consumer 分叉。live 可以丢弃 downstream render wakeup 或过期 snapshot response，但不能丢 vterm semantic input 后再维护另一份 screen state。history 可以异步追平，但 backlog 单位必须是 semantic transaction 或等价 durable event，不能回到 raw PTY replay。

## 3. SemanticTap 合同

- domain owner：core-v2 terminal ingest。
- truth source：同一个 `termx-vterm` emulator 解释后的 `TerminalSemanticTransaction` 与同一 emulator 的 latest native screen。
- 消息链路：PTY bytes/resize 顺序进入 `SemanticTap`；tap 先更新 vterm，再把 immutable transaction 给 history，并把 latest revision/invalidation 给 live。
- response owner：OSC/DA/DSR response 只由 `SemanticTap` 持有的 vterm 回写 PTY；history consumer 不得回写。
- 失败条件：tap 输入顺序错乱、同一 PTY bytes 被多个 vterm 解释、resize 脱离 PTY sequence、response 重复回写、history 接收 raw PTY backlog、live 丢 emulator state 后自己补屏，均为合同失败。

## 4. Consumer 边界

live consumer：

- 只能读取 `SemanticTap` 的 latest native screen snapshot 或 invalidation revision。
- 可以 latest-only 合并 wakeup，也可以丢弃 stale snapshot response。
- 不能启动第二个 vterm，也不能从 history transaction 反推 renderer frame backlog。

history consumer：

- 只消费 `TerminalSemanticTransaction`。
- 不直接读 live snapshot、native screen rows、TUI rows、renderer rows 或 process raw PTY。
- backlog 可以落盘或批量追平，但必须保存 semantic transaction 或等价 durable event。

## 5. Copy/history 入口状态

- preview：进入 copy/history 的即时显示上下文，只能展示和吞输入；不能 search/copy/page authoritative cursor。
- frozen：`FrozenHistorySnapshot` token，表示稳定权威边界；不得改造成半追平 token。

R376/R377 曾尝试增加独立 materialized/copy-entry projection 合同；该方案已废弃。copy/history 入口
只能请求统一的 `history.window`，任何快速返回或 backlog 调度都必须封装在该 API 内部。

## 6. R372-R377 验收

R372 single SemanticTap 合同与 harness：

- harness 证明 PTY bytes/resize 只有一个 vterm owner。
- harness 证明 live/history 不各自解释 PTY；history consumer 只接 transaction。
- harness 证明 resize 与 PTY 在同一 tap sequence。
- harness 证明 OSC/DA/DSR response exactly once。
- harness 证明 live 丢弃/合并的是 wakeup/snapshot，不会丢 emulator state。
- R372 只做合同与最小隔离，不把生产 fan-out 全量切换到真实 PTY。

R373 SemanticTap fan-out 接入：

- 真实 PTY 输出进入 single tap。
- live publisher 只发 latest invalidation/snapshot。
- history queue 接完整 semantic transaction，不再把 raw PTY text 交给第二个 `vterm.SemanticSource` replay。
- 保留 history disabled live-only 模式。

R374 history semantic transaction backlog 与追平边界：

- backlog 从 raw PTY spool 切为 semantic transaction 或等价 durable event。
- 暴露 applied/target seq。
- public authoritative history/window/freeze 仍可 flush；copy/history 入口优化必须封装在 `history.window` 内部。

R375 ordinary LogicalLineSink fast lane：

- 普通 stdout 走 logical-line-first fast lane。
- screen-app、alt、ED2/ED3、full-replace、touched rows 保持正确 classifier/frame reducer 边界。
- 禁止用 raw parser fallback 补普通输出。

R376 copy/history 统一入口合同：

- 不新增独立 copy-entry/materialized projection API。
- 不复用 `Freeze` 表达半追平 token。
- 不把 live native screen 提升为可复制/search/page history truth。

R377 TUI copy preview/frozen 两态：

- preview 只展示和处理输入上下文。
- frozen token 继续服务 older/newer/search/full copy。

## 7. 禁止方案

- history 专用第二 vterm。
- raw PTY history replay 或 raw parser fallback。
- snapshot/history 拼接 fallback。
- copy enter 为了拿 latest 强制 full flush 后把 freeze 当半追平 token。
- storage scrub、重复 attach、重复同步、定时刷新或程序名特殊分支。
- 让 live native screen 伪装 authoritative history。
