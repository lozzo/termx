# 场景 18：Workbench Tab 切换

## 目标

定义 tab strip 的基本表达、tab 切换后的正文切换，以及 tab 不随最后一个 pane 消失而自动关闭的感知。

## 状态前提

- workspace 中至少有多个 tab
- 当前处于 workbench，而不是 Terminal Pool

## 线框图

```text
Before: 当前在 dev tab

termx  [main]  [1:dev]  2:logs  3:ops
┌─ api-dev────────────────────────────────────────────────────────────────────running  owner────────────────────┐
│$ npm run dev                                                                                                 │
│ready on :3000                                                                                                │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

After: 切到 logs tab

termx  [main]  1:dev  [2:logs]  3:ops
┌─ deploy-log──────────────────────────────────────────────────────────────────running  owner──────────────────┐
│tail -f deploy.log                                                                                            │
│[12:31:02] build ok                                                                                            │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

## 关键规则

- tab 切换优先给出接近 tmux / zellij 的使用感
- tab 名称是工作组织名，不必与 terminal 名称一致
- tab 的正文状态独立保存，切回时恢复各自 pane 布局、焦点和观察偏移
- 若某个 tab 当前只剩 `unconnected pane`，它仍然保留在 tab strip 中

## 流转

- switch tab -> 切换正文与 active pane
- close tab -> 只有显式 close tab 才从 tab strip 消失
- restore tab -> 恢复该 tab 上次状态
