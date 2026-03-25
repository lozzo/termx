# 场景 15：Tab 最后一个 Pane 消失

## 目标

定义 tab 内最后一个 pane 关闭或解绑后的行为。

## 状态前提

- 当前 tab 中只剩最后一个 pane

## 线框图

```text
Before: tab 内只剩最后一个 live pane

termx  [main]  1:dev  [2:logs]
┌─ deploy-log──────────────────────────────────────────────────────────────────running  owner──────────────────┐
│tail -f deploy.log                                                                                             │
│[12:01:03] build ok                                                                                            │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

After: user closes this pane

termx  [main]  1:dev  [2:logs]
┌─ unconnected──────────────────────────────────────────────────────────────────────────────unconnected────────┐
│ No terminal connected                                                                                         │
│ [Enter] Connect existing terminal                                                                             │
│ [n] Create new terminal                                                                                       │
│ [p] Open Terminal Pool                                                                                        │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

Only explicit close-tab removes the tab itself.
```

## 关键规则

- 不自动关闭 tab
- 立即补成一个 `unconnected pane`
- 只有显式关闭 tab，这个 tab 才真正退出
- 这样可以保持 tab 结构稳定，也更接近 tmux/zellij 的使用感
- tab 标题保持原位，直到用户显式关闭或重命名

## 流转

- last pane close -> unconnected pane
- explicit close tab -> tab removed
