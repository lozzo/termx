# 场景 04：Connect Dialog

## 目标

定义 `split / new tab / new float` 共用的连接对话框。

## 状态前提

- 已先创建目标 pane slot
- 当前弹出轻量 dialog
- 用户要选择创建新 terminal 或连接已有 terminal

## 线框图

```text
termx  [main]  [1:shell]                                          pane:ws-1-tab-1-pane-2  term:-  float:0
 main / shell / tiled / ws-1-tab-1-pane-2                                           overlay:connect_dialog
┌─ shell-dev [dim]─────────────────────────────────────────────────────────────────────────────────────────────┐
│$ npm run dev                                                                                                 │
│ready on :3000                                                                                                │
│                                                                                                              │
│                           ┌─ Connect Pane ───────────────────────────────────────────┐                       │
│                           │ target: split-right  •  pane ws-1-tab-1-pane-2          │                       │
│                           │                                                          │                       │
│                           │ query: api                                               │                       │
│                           │                                                          │                       │
│                           │ Selection                                                │                       │
│                           │ > + new terminal                                         │                       │
│                           │   api-dev            running   owner elsewhere           │                       │
│                           │   api-log            running   no owner                  │                       │
│                           │   old-api            exited                              │                       │
│                           │                                                          │                       │
│                           │ Enter confirm  •  Tab switch focus  •  Esc cancel        │                       │
│                           └──────────────────────────────────────────────────────────┘                       │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <enter> CONFIRM  <esc> CANCEL  <tab> NEXT  <n> NEW                                overlay  connect
```

## 关键规则

- `split / new tab / new float` 全部共用这一套流程
- 取消后保留为 `unconnected pane`
- 连接已有 terminal 默认看全局 terminal 池
- 轻量弹窗只负责“选哪个 terminal / 是否新建”，不替代 Terminal Pool 独立页面
- 选中已存在 terminal 时，需要在列表里直接看见其 `running / exited / owner` 摘要

## 流转

- create new terminal
- connect existing terminal
- cancel -> unconnected pane
- 若连接 running terminal 且无 owner，则当前 pane 成为 owner
- 若连接 running terminal 且已有 owner，则当前 pane 默认为 follower
