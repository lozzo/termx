# Flow 05：Overlay

## 目标

描述 connect dialog、help、prompt 等 overlay 的打开、焦点和关闭顺序。

## 起点

- 用户在 workbench 中触发 overlay

## 流程

```text
Workbench focus on pane
  -> open overlay

overlay types
  -> connect dialog
  -> prompt
  -> help

focus priority
  prompt
    > help / picker / manager
    > floating
    > tiled

overlay open
  -> lower layers stop receiving keyboard input

Esc
  -> closes current temporary top layer only
  -> returns focus to previous valid pane or page focus
```

## 关键状态变化

- workbench -> overlay
- overlay focus priority
- Esc close
- return to previous valid focus
