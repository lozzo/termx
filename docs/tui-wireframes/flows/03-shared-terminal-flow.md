# Flow 03：Shared Terminal

## 目标

描述 shared terminal 下 owner/follower、Become Owner 和 remove 的流转。

## 起点

- 一个 terminal 被多个 pane 绑定

## 流程

```text
first pane binds terminal
  -> terminal has no owner
  -> first pane becomes owner

second pane binds same terminal
  -> owner already exists
  -> second pane becomes follower

follower chooses Become Owner
  -> owner role moves immediately
  -> new pane = owner
  -> old owner = follower

owner closes or unbinds
  -> no automatic owner migration
  -> terminal may temporarily have no owner

shared terminal removed
  -> every bound pane becomes unconnected pane
```

## 关键状态变化

- first bind -> owner
- later bind -> follower
- Become Owner -> owner handoff
- remove shared terminal -> unconnected panes
- owner close/unbind -> no owner
