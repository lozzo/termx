# 场景 10：多浮窗重叠

## 目标

定义多个 floating pane 重叠时的 z-order 与焦点表达。

## 状态前提

- 当前 tab 中存在多个 floating pane
- 有遮挡关系

## 线框图

```text
termx  [main]  [1:ops]                                          pane:float-2  term:t-044  float:2
 main / ops / mixed / ws-1-tab-2                                                     floating stack
┌─ deploy-log [dim]────────────────────────────────────────────────────────────────────────────────────────────┐
│tail -f deploy.log                                                                                           │
│[12:01:03] build ok                                                                                          │
│                                                                                                              │
│        ┌─ top-cpu [z:1]──────────────────────────────────┐                                                   │
│        │ PID   CPU   MEM                                 │                                                   │
│        │ 1992  18%   422m                                │                                                   │
│   ┌────┴─ htop [z:2 active]────────────────────────────────────────────────────┐                              │
│   │ Tasks: 312  Load: 0.58 0.41 0.33                                        │                              │
│   │ CPU[||||||||    ]                                                        │                              │
│   │ Mem[|||||||     ]                                                        │                              │
│   └───────────────────────────────────────────────────────────────────────────┘                              │
│                                                                                                              │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <tab> NEXT FLOAT  <mouse> FOCUS/DRAG  <c> CENTER  <x> CLOSE                        htop  top z
```

## 关键规则

- 最近被聚焦或操作的 floating pane 自动置顶
- 不做额外 raise/lower 管理模型
- z-order 是结果状态，不是用户长期维护的对象
- 新建 float 默认错位落点，避免完全重叠

## 流转

- focus next floating
- automatic raise to top
- close top float -> reveal next visible float
