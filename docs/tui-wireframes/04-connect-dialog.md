# 场景 04：Connect Dialog

## 目标

定义 `split / new tab / new float` 共用的连接对话框。

## 状态前提

- 已先创建目标 pane slot
- 当前弹出轻量 dialog
- 用户要选择创建新 terminal 或连接已有 terminal

## 线框图

```text
TODO
```

## 关键规则

- `split / new tab / new float` 全部共用这一套流程
- 取消后保留为 `unconnected pane`
- 连接已有 terminal 默认看全局 terminal 池

## 流转

- create new terminal
- connect existing terminal
- cancel -> unconnected pane
