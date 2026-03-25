# 场景 20：Terminal Pool 执行 Kill 之后

## 目标

定义从 Terminal Pool 对 running terminal 执行 `kill` 后，Pool 页面与 Workbench 绑定 pane 的结果表达。

## 状态前提

- terminal 当前是 running
- 至少有一个 pane 绑定该 terminal
- 用户在 Terminal Pool 中触发 `kill`

## 线框图

```text
Before: Terminal Pool

┌─ TERMINALS ───────────────────────────────┬─ LIVE PREVIEW ───────────────────────┬─ DETAILS ──────┐
│ > ● api-dev         #backend  shown:2     │ $ npm run dev                         │ state: running │
│   ● api-log         #backend  shown:1     │ ready on :3000                        │ owner: pane-3  │
└───────────────────────────────────────────┴───────────────────────────────────────┴────────────────┘

After: Terminal Pool

┌─ TERMINALS ───────────────────────────────┬─ SNAPSHOT PREVIEW ───────────────────┬─ DETAILS ──────┐
│ > ○ api-dev         #backend  shown:2     │ $ npm run dev                         │ state: exited  │
│   ● api-log         #backend  shown:1     │ ready on :3000                        │ exit code: 0   │
│                                           │ process exited                        │ restart: yes   │
└───────────────────────────────────────────┴───────────────────────────────────────┴────────────────┘

Workbench 里的绑定 pane：

┌─ api-dev──────────────────────────────────────────────────────────────────────────┐
│$ npm run dev                                                                     │
│ready on :3000                                                                    │
│──────────────────────────────────────────────────────────────────────────────────│
│ terminal exited  •  press R to restart                                           │
└──────────────────────────────────────────────────────────────────────────────────┘
```

## 关键规则

- `kill` 只改变 terminal 运行状态，不移除 terminal 对象
- Terminal Pool 中该 terminal 从 running 组进入 exited 组
- 所有绑定 pane 同步进入 `exited pane`
- `R` 重启的是原 terminal 对象

## 流转

- kill terminal -> terminal state: exited
- exited -> restart same terminal
- exited -> remove terminal -> unconnected pane
