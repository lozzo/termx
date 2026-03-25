# 场景 19：Connect Dialog 不同入口

## 目标

定义 `split / new tab / new float` 三种入口进入 connect dialog 时，弹窗正文如何提示当前目标工作位。

## 状态前提

- connect dialog 已经存在
- 这里补充不同来源进入时的目标摘要差异

## 线框图

```text
Case A: split-right

┌─ Connect Pane ───────────────────────────────────────────┐
│ target: split-right                                      │
│ destination: ws-1 / tab-1 / pane-4                      │
│                                                          │
│ > + new terminal                                         │
│   api-dev          running                               │
│   worker-tail      running                               │
└──────────────────────────────────────────────────────────┘

Case B: new tab

┌─ Connect Pane ───────────────────────────────────────────┐
│ target: new-tab                                          │
│ destination: ws-1 / tab-3                                │
│                                                          │
│ > + new terminal                                         │
│   api-dev          running                               │
│   worker-tail      running                               │
└──────────────────────────────────────────────────────────┘

Case C: new floating

┌─ Connect Pane ───────────────────────────────────────────┐
│ target: new-floating                                     │
│ destination: ws-1 / tab-1 / float-2                      │
│                                                          │
│ > + new terminal                                         │
│   api-dev          running   owner elsewhere             │
│   worker-tail      running   no owner                    │
└──────────────────────────────────────────────────────────┘
```

## 关键规则

- 三种入口共用一套选择模型，不分裂成三套产品流程
- 差异只体现在目标摘要上，而不是列表结构上
- 用户必须一眼知道自己是在创建 split、new tab 还是 new float
- 取消后统一保留目标工作位为 `unconnected pane`

## 流转

- split/new tab/new float -> 共用 connect dialog
- confirm -> 绑定 terminal 并落到对应目标
- cancel -> 保留 unconnected pane 或空新 tab 工作位
