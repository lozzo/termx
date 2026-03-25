# 场景 08：Shared Terminal Remove 确认

## 目标

定义 shared terminal 执行 remove 时的确认弹窗和后续状态变化。

## 状态前提

- 当前 terminal 被多个 pane 或客户端共享
- 用户尝试执行 remove

## 线框图

```text
termx  [main]  [Pool]                                            screen:terminal-pool  selected:t-022
 Terminal Pool  •  actions for api-dev
┌─ Pool [dim]───────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                                              │
│                         ┌─ Remove Terminal ─────────────────────────────────────────┐                        │
│                         │ terminal: api-dev                                        │                        │
│                         │ state: running  •  attached panes: 3                     │                        │
│                         │                                                          │                        │
│                         │ Removing this terminal will:                             │                        │
│                         │ - delete it from the pool                                │                        │
│                         │ - turn attached panes into unconnected pane              │                        │
│                         │ - keep workspace/tab structure intact                    │                        │
│                         │                                                          │                        │
│                         │ [Remove terminal]   [Cancel]                             │                        │
│                         └──────────────────────────────────────────────────────────┘                        │
│                                                                                                              │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <enter> CONFIRM REMOVE  <esc> CANCEL                                             shared  attached:3
```

## 关键规则

- 非 shared terminal 可直接 remove
- shared terminal 必须确认
- remove 后相关 pane 进入 `unconnected pane`
- 这里只确认 `remove`，不混入 `kill` 语义
- 确认后不自动关闭受影响 tab；只替换正文状态

## 流转

- confirm -> unconnected pane
- cancel -> 返回原画面
- 若某个受影响 pane 当前不可见，则不即时打扰，等切过去再直接看到 unconnected pane
