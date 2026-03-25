# 场景 14：Remote Remove Notice

## 目标

定义其他客户端 remove terminal 时，本客户端的反馈方式。

## 状态前提

- 某个 terminal 被其他客户端 remove
- 当前客户端里可能有可见或不可见的受影响 pane

## 线框图

```text
Case A: 当前可见 terminal 被其他客户端 remove

termx  [main]  [1:dev]                                            pane:ws-1-tab-1-pane-1
┌─ unconnected──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ notice: terminal "api-dev" was removed from the pool                                                        │
│                                                                                                              │
│ [Enter] Connect existing terminal                                                                            │
│ [n]     Create new terminal                                                                                  │
│ [p]     Open Terminal Pool                                                                                   │
│                                                                                                              │
│ Workspace and tab layout were kept.                                                                          │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

Case B: 当前不可见 terminal 被 remove

No popup now.
When user later focuses that pane/tab, it directly renders as `unconnected pane`.
```

## 关键规则

- 只对当前可见受影响 terminal 显示通用 notice
- notice 要写明 terminal 名称
- 不显示操作者身份
- 不可见受影响 terminal 不即时打扰
- notice 只是一次性的轻反馈，不额外打断当前键盘流
- 进入 unconnected pane 后，后续行为与普通 unconnected pane 一致

## 流转

- visible affected -> notice + unconnected pane
- not visible affected -> later direct state reveal
