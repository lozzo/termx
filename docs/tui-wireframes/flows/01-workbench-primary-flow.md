# Flow 01：Workbench 主路径

## 目标

描述用户从启动到进入日常工作台，再到连接 terminal 的主路径。

## 起点

- 启动 `termx`

## 流程

```text
start termx
  -> create or restore workspace shell
  -> enter Workbench
  -> focus current live pane

from Workbench
  -> split / new tab / new float
  -> create target pane slot
  -> open Connect Dialog
      -> choose + new terminal
         -> bind terminal to target pane
         -> enter live pane
      -> choose existing terminal
         -> bind terminal to target pane
         -> owner? if none => owner, else => follower
         -> enter live pane
      -> cancel
         -> keep unconnected pane
```

## 关键状态变化

- startup -> workbench
- create pane slot
- open connect dialog
- bind terminal -> live pane
- cancel -> unconnected pane
