# Flow 03：Shared Terminal

## 目标

描述 shared terminal 下 owner/follower、Become Owner 和 remove 的流转。

## 起点

- 一个 terminal 被多个 pane 绑定

## 流程

```text
TODO
```

## 关键状态变化

- first bind -> owner
- later bind -> follower
- Become Owner -> owner handoff
- remove shared terminal -> unconnected panes
