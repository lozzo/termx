# 场景 15：Tab 最后一个 Pane 消失

## 目标

定义 tab 内最后一个 pane 关闭或解绑后的行为。

## 状态前提

- 当前 tab 中只剩最后一个 pane

## 线框图

```text
TODO
```

## 关键规则

- 不自动关闭 tab
- 立即补成一个 `unconnected pane`
- 只有显式关闭 tab，这个 tab 才真正退出

## 流转

- last pane close -> unconnected pane
- explicit close tab -> tab removed
