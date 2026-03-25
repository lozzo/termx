# 场景 14：Remote Remove Notice

## 目标

定义其他客户端 remove terminal 时，本客户端的反馈方式。

## 状态前提

- 某个 terminal 被其他客户端 remove
- 当前客户端里可能有可见或不可见的受影响 pane

## 线框图

```text
TODO
```

## 关键规则

- 只对当前可见受影响 terminal 显示通用 notice
- notice 要写明 terminal 名称
- 不显示操作者身份
- 不可见受影响 terminal 不即时打扰

## 流转

- visible affected -> notice + unconnected pane
- not visible affected -> later direct state reveal
