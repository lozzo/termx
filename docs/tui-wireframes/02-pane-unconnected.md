# 场景 02：Unconnected Pane

## 目标

定义没有绑定 terminal 的 pane 在 workbench 中如何表达。

## 状态前提

- pane 当前没有 terminal 对象绑定
- 可能来自新建后未绑定
- 也可能来自 remove terminal 之后的保留工作位

## 线框图

```text
TODO
```

## 关键规则

- 不显示死空白
- 至少提供：
  - connect existing terminal
  - create new terminal
  - open terminal pool

## 流转

- 可进入 connect dialog
- 可进入 Terminal Pool
- 可转成 live pane
