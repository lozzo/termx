# Flow 04：Floating

## 目标

描述 floating pane 的创建、移动、遮挡、呼回与关闭。

## 起点

- 用户在 workbench 中创建 floating pane

## 流程

```text
Workbench live pane
  -> create floating pane slot
  -> connect new or existing terminal
  -> floating pane appears above tiled layer

while floating is active
  -> move
  -> resize
  -> may cross viewport boundary
  -> top-left drag anchor must remain visible

focus another floating
  -> newly focused floating auto-raises to top

recall and center
  -> active floating moves back to safe center position

close floating pane
  -> only this pane closes
  -> terminal may continue running elsewhere
```

## 关键状态变化

- new float
- move out of viewport
- keep top-left anchor visible
- recall and center
- close float != remove terminal
