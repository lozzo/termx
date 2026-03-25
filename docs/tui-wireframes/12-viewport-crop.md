# 场景 12：Viewport 裁切与内部观察偏移

## 目标

定义 pane 小于 terminal 时的裁切、偏移和边界标记。

## 状态前提

- pane 可见区域小于 terminal 实际内容
- 默认左上角裁切

## 线框图

```text
Case A: pane 小于 terminal，发生裁切与偏移

termx  [main]  [1:ops]                                            pane:ws-1-tab-2-pane-1  term:t-050
 main / ops / tiled / viewport offset x:18 y:6                                         move mode
┌+top-cpu──────────────────────────────────────────────────────────────────────────────+┐
│ Tasks: 312 total, 2 running, 310 sleeping                                            │
│ Load average: 0.58 0.41 0.33                                                         +│
│ Mem[|||||||||||||     ]  3.1G/8.0G                                                   │
│ Swp[|||               ]  0.4G/4.0G                                                   │
│ PID   USER   CPU%  MEM%  TIME+   Command                                             │
│ 1992  dev    18.0   4.1   01:21  node server.js                                      │
└+++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++┘
 <v> MOVE VIEWPORT  <mouse-drag> SHIFT  <0> RESET OFFSET                              crop:top/right/bottom

Case B: pane 大于 terminal，空白区用小圆点填充

┌─ tiny-task────────────────────────────────────────────────────────────────────────────┐
│ done                                                                                 │
│ exit 0                                                                               │
│··················································································│
│··················································································│
└──────────────────────────────────────────────────────────────────────────────────────┘
```

## 关键规则

- 默认左上角裁切
- 支持 viewport move 模式
- 支持鼠标拖拽移动内部观察位置
- 被裁切侧显示 `+`
- pane 大于 terminal 时，空白区显示小圆点
- 偏移属于 workspace 可恢复状态
- 边界遇到宽字符、emoji、powerline 时，宁可留空也不画半个字符

## 流转

- enter viewport move mode
- drag to shift viewport
- restore saved offset
- 切换 tab/workspace 后再回来，应恢复上次观察偏移
