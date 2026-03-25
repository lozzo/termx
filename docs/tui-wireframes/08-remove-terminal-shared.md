# 场景 08：Shared Terminal Remove 确认

## 目标

定义 shared terminal 执行 remove 时的确认弹窗和后续状态变化。

## 状态前提

- 当前 terminal 被多个 pane 或客户端共享
- 用户尝试执行 remove

## 线框图

```text
TODO
```

## 关键规则

- 非 shared terminal 可直接 remove
- shared terminal 必须确认
- remove 后相关 pane 进入 `unconnected pane`

## 流转

- confirm -> unconnected pane
- cancel -> 返回原画面
