# 场景 01：默认 Workbench

## 目标

定义用户执行 `termx` 后进入的默认工作台主画面。

## 状态前提

- 默认进入可工作 workspace
- 至少有一个 live pane
- 顶栏、pane 标题栏、底栏都可见

## 线框图

```text
termx  [main]  [1:shell]  2:logs                                  pane:ws-1-tab-1-pane-1  term:t-001  float:0
 main / shell / tiled / ws-1-tab-1-pane-1                                           running  owner  connected
┌─ shell-dev────────────────────────────────────────────────────────────────────────────────────────────────────┐
│$                                                                                                             │
│                                                                                                              │
│                                                                                                              │
│                                                                                                              │
│                                                                                                              │
│                                                                                                              │
│                                                                                                              │
│                                                                                                              │
│                                                                                                              │
│                                                                                                              │
│                                                                                                              │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <c-p> pane  <c-t> tab  <c-w> workspace  <c-o> float  <c-f> connect  <?> help          shell-dev  ▣ tiled
```

## 关键规则

- 顶栏展示 workspace、tab strip、少量摘要
- pane 左侧标题优先展示 terminal 名称
- pane 标题右侧只放短状态，不塞过多全局信息
- 默认启动直接进入可工作的 live pane，不经过说明页
- 底栏左侧是操作带，右侧是当前焦点摘要
- 底栏左侧展示 mode 提示，右侧展示焦点摘要

## 流转

- 可进入 connect dialog
- 可进入 Terminal Pool
- 可进入 floating 场景
- 可 split 出新的 unconnected pane
