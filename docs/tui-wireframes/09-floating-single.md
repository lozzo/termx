# 场景 09：单个 Floating Pane

## 目标

定义单个 floating pane 与 tiled pane 共存时的工作台画面。

## 状态前提

- 当前 tab 中同时存在 tiled pane 与 floating pane
- floating pane 处于活动状态

## 线框图

```text
termx  [main]  [1:dev]  2:logs
┌─ shell-dev [dim]─────────────────────────────────────────────────────────────────────────────────────────────┐
│$ npm run dev                                                                                                 │
│ready on :3000                                                                                                │
│                                                                                                              │
│         ┌─ htop────────────────────────────────────────────owner  float  active──┐                           │
│         │  1  node        12.0  321m                                             │                           │
│         │  2  bash         0.1   22m                                             │                           │
│         │  3  go test      8.2  188m                                             │                           │
│         │                                                                         │                           │
│         │ drag from title                                                         │                           │
│         └─────────────────────────────────────────────────────────────────────────┘                           │
│                                                                                                              │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <tab> NEXT FLOAT  <h/j/k/l> MOVE  <H/J/K/L> RESIZE  <c> CENTER  <x> CLOSE          htop  ◫ float
```

## 关键规则

- floating pane 是完整 pane
- 允许拖出主视口
- 但必须保留左上角锚点在大窗口内
- floating pane 内部第一阶段不再继续 split
- active floating pane 视觉上必须明显高于 tiled 层

## 流转

- move
- resize
- recall and center
- close float -> 仅关闭该 pane，不默认 kill terminal
