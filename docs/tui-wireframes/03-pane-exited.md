# 场景 03：Exited Pane

## 目标

定义 terminal 已退出但对象仍保留时，pane 的表达方式。

## 状态前提

- 绑定的 terminal 状态为 exited
- pane 保留原位
- 历史内容继续保留

## 线框图

```text
TODO
```

## 关键规则

- 明确展示 exited 状态
- 提示可按 `R` restart
- 多个 pane 绑定同一 terminal 时，应一起进入 exited 状态

## 流转

- 可 restart 为 live pane
- 可改绑其他 terminal
- 可关闭 pane
