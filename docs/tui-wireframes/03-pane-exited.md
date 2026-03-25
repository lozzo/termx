# 场景 03：Exited Pane

## 目标

定义 terminal 已退出但对象仍保留时，pane 的表达方式。

## 状态前提

- 绑定的 terminal 状态为 exited
- pane 保留原位
- 历史内容继续保留

## 线框图

```text
termx  [main]  [1:ops]                                            pane:ws-1-tab-2-pane-1  term:t-019  float:0
 main / ops / tiled / ws-1-tab-2-pane-1                                            exited  share:2
┌─ deploy-watch────────────────────────────────────────────────────────────────────────────────────────────────┐
│$ ./deploy.sh                                                                                                 │
│syncing assets...                                                                                             │
│restarting service...                                                                                         │
│done                                                                                                          │
│                                                                                                              │
│──────────────────────────────────────────────────────────────────────────────────────────────────────────────│
│ terminal exited  •  press R to restart  •  press Enter to reconnect another terminal                        │
│                                                                                                              │
│ [R] Restart terminal   [Enter] Connect another   [p] Open Terminal Pool   [x] Close pane                    │
│                                                                                                              │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <r> RESTART  <enter> RECONNECT  <p> POOL  <x> CLOSE                               deploy-watch  exited
```

## 关键规则

- 明确展示 exited 状态
- 提示可按 `R` restart
- 多个 pane 绑定同一 terminal 时，应一起进入 exited 状态
- 历史正文继续保留，可读但不再接收输入
- `R` 是重启原 terminal 对象，不是新建并替换绑定

## 流转

- 可 restart 为 live pane
- 可改绑其他 terminal
- 可关闭 pane
- 若 terminal 后续被 remove，则从 exited pane 转成 unconnected pane
