# 场景 12：Viewport 裁切与内部观察偏移

## 目标

定义 pane 小于 terminal 时的裁切、偏移和边界标记。

## 状态前提

- pane 可见区域小于 terminal 实际内容
- 默认左上角裁切

## 线框图

```text
TODO
```

## 关键规则

- 默认左上角裁切
- 支持 viewport move 模式
- 支持鼠标拖拽移动内部观察位置
- 被裁切侧显示 `+`
- pane 大于 terminal 时，空白区显示小圆点
- 偏移属于 workspace 可恢复状态

## 流转

- enter viewport move mode
- drag to shift viewport
- restore saved offset
