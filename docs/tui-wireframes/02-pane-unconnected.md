# 场景 02：Unconnected Pane

## 目标

定义没有绑定 terminal 的 pane 在 workbench 中如何表达。

## 状态前提

- pane 当前没有 terminal 对象绑定
- 可能来自新建后未绑定
- 也可能来自 remove terminal 之后的保留工作位

## 线框图

```text
termx  [main]  [1:shell]                                          pane:ws-1-tab-1-pane-2  term:-  float:0
 main / shell / tiled / ws-1-tab-1-pane-2                                           unconnected
┌─ unconnected──────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                                              │
│                                        No terminal connected                                                 │
│                                                                                                              │
│                                  [Enter] Connect existing terminal                                           │
│                                  [n]     Create new terminal                                                 │
│                                  [p]     Open Terminal Pool                                                  │
│                                  [x]     Close pane                                                          │
│                                                                                                              │
│                           This pane is only a work slot. Attach a terminal to start.                         │
│                                                                                                              │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <enter> CONNECT  <n> NEW  <p> POOL  <x> CLOSE  <?> HELP                         unconnected  ▣ tiled
```

## 关键规则

- 不显示死空白
- 至少提供 `connect existing terminal`
- 至少提供 `create new terminal`
- 至少提供 `open terminal pool`
- 新建 pane 后若取消连接流程，保留为这个状态
- remove terminal 之后落到这个状态时，不继承 exited 提示语

## 流转

- 可进入 connect dialog
- 可进入 Terminal Pool
- 可转成 live pane
- 若它是 tab 中最后一个 pane，也继续保留这个工作位
