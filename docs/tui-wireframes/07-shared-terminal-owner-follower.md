# 场景 07：Shared Terminal / Owner / Follower

## 目标

定义共享 terminal 时的 owner/follower 表达，以及 `Become Owner` 动作。

## 状态前提

- 同一个 terminal 同时被多个 pane 绑定
- 当前最多一个 owner
- 其他均为 follower

## 线框图

```text
TODO
```

## 关键规则

- 一个 terminal 同时最多一个 owner
- 新连接已有 terminal 时：
  - 无 owner 则自动成为 owner
  - 否则默认 follower
- owner 关闭/解绑后不自动迁移
- `Become Owner` 直接抢占 owner

## 流转

- follower -> Become Owner -> owner
- owner close/unbind -> no owner
