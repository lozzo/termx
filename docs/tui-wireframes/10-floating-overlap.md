# 场景 10：多浮窗重叠

## 目标

定义多个 floating pane 重叠时的 z-order 与焦点表达。

## 状态前提

- 当前 tab 中存在多个 floating pane
- 有遮挡关系

## 线框图

```text
TODO
```

## 关键规则

- 最近被聚焦或操作的 floating pane 自动置顶
- 不做额外 raise/lower 管理模型

## 流转

- focus next floating
- automatic raise to top
