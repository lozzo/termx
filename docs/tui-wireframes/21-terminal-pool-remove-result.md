# 场景 21：Terminal Pool 执行 Remove 之后

## 目标

定义从 Terminal Pool 对 terminal 执行 `remove` 后，Pool 页面与 Workbench 绑定 pane 的结果表达。

## 状态前提

- terminal 存在于 pool 中
- 可能被一个或多个 pane 绑定
- 用户已确认 remove

## 线框图

```text
Before: Terminal Pool

┌─ TERMINALS ───────────────────────────────┬─ LIVE PREVIEW ───────────────────────┬─ DETAILS ──────┐
│ > ● worker-tail     #ops      shown:2     │ tail -f worker.log                    │ state: running │
│   ● api-dev         #backend  shown:1     │ [12:31:02] job ok                     │ owner: pane-5  │
└───────────────────────────────────────────┴───────────────────────────────────────┴────────────────┘

After: Terminal Pool

┌─ TERMINALS ───────────────────────────────┬─ LIVE PREVIEW ───────────────────────┬─ DETAILS ──────┐
│ > ● api-dev         #backend  shown:1     │ $ npm run dev                         │ state: running │
│                                           │ ready on :3000                        │ owner: pane-3  │
└───────────────────────────────────────────┴───────────────────────────────────────┴────────────────┘

Workbench 里的原绑定 pane：

┌─ unconnected──────────────────────────────────────────────────────────────────────┐
│ notice: terminal "worker-tail" was removed from the pool                         │
│                                                                                  │
│ [Enter] Connect existing terminal                                                │
│ [n]     Create new terminal                                                      │
│ [p]     Open Terminal Pool                                                       │
└──────────────────────────────────────────────────────────────────────────────────┘
```

## 关键规则

- `remove` 会删除 terminal 对象本体，不再保留 exited terminal
- 所有绑定 pane 统一变成 `unconnected pane`
- workspace、tab、pane 结构保持不变
- 如果当前 pane 不可见，不需要即时弹 notice

## 流转

- remove terminal -> terminal leaves pool
- attached panes -> unconnected pane
- user reconnects -> bind another or new terminal
