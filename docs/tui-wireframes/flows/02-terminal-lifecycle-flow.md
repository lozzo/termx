# Flow 02：Terminal 生命周期

## 目标

描述 terminal 的 live / exited / removed 与 pane 状态的映射关系。

## 起点

- terminal 已绑定到 pane

## 流程

```text
live terminal
  -> pane renders live pane

kill terminal
  -> terminal object kept
  -> terminal state = exited
  -> all bound panes = exited pane
  -> user may press R
  -> restart same terminal object
  -> panes return to live pane

remove terminal
  -> terminal object removed from pool
  -> all bound panes = unconnected pane

close pane
  -> only pane disappears
  -> terminal keeps running
  -> if it was last pane in tab
     -> tab keeps one unconnected pane slot
```

## 关键状态变化

- live -> exited pane
- live -> unconnected pane
- restart exited terminal
- close pane != kill terminal
