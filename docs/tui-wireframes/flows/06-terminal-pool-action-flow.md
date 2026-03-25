# Flow 06：Terminal Pool 动作流

## 目标

描述 Terminal Pool 中最关键的 terminal 动作及其对 workbench 的影响。

## 起点

- 用户已进入 Terminal Pool
- 已选中某个 terminal

## 流程

```text
select terminal in pool
  -> open here
     -> bind selected terminal to current pane

  -> open new tab
     -> create tab with pane slot
     -> bind selected terminal

  -> open floating
     -> create floating pane slot
     -> bind selected terminal

  -> edit metadata/tags
     -> save
     -> refresh titles, list labels, search index

  -> kill
     -> terminal state = exited
     -> attached panes = exited pane

  -> remove
     -> if shared then confirm
     -> terminal removed from pool
     -> attached panes = unconnected pane
```

## 关键状态变化

- open here/new tab/floating -> 新建或复用 pane 工作位并绑定 terminal
- edit metadata/tags -> terminal 显示名与标签全局刷新
- kill -> exited pane
- remove -> unconnected pane
