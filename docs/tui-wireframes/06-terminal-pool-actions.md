# 场景 06：Terminal Pool 动作

## 目标

定义 Terminal Pool 页面中的主要 terminal 管理动作。

## 状态前提

- 用户已在 Terminal Pool 中选中某个 terminal

## 线框图

```text
termx  [main]  [Pool]
┌─ TERMINALS ───────────────────────────────┬─ LIVE PREVIEW ───────────────────────────────────┬─ ACTIONS ──────┐
│ VISIBLE                                   │ api-dev                                          │ > Open Here    │
│ > ● api-dev         #backend  shown:2     │ $ npm run dev                                     │   Open New Tab │
│   ● api-log         #backend  shown:1     │ ready on :3000                                    │   Open Floating│
│                                           │ GET /health 200                                   │   Rename       │
│ PARKED                                    │                                                   │   Edit Tags    │
│   ● worker-tail     #ops      shown:0     │                                                   │   Kill         │
│                                           │                                                   │   Remove       │
│ EXITED                                    │                                                   │                │
│   ○ old-api         #legacy   shown:1     │                                                   │ Enter run      │
│                                           │                                                   │ Esc cancel     │
└───────────────────────────────────────────┴───────────────────────────────────────────────────┴────────────────┘
 <enter> SELECT  <esc> BACK  <e> EDIT  <k> KILL  <d> REMOVE                           api-dev
```

## 关键规则

- 第一阶段支持 `rename`
- 第一阶段支持 `edit metadata/tags`
- 第一阶段支持 `kill`
- 第一阶段支持 `remove`
- 第一阶段支持 `open here`
- 第一阶段支持 `open in new tab`
- 第一阶段支持 `open in floating`
- `kill` 与 `remove` 必须在动作层面明确分开，不能共用模糊文案

## 流转

- 可进入 metadata/tags 编辑
- 可触发 remove terminal shared 场景
- `kill` -> 相关 pane 转成 exited pane
- `remove` -> 相关 pane 转成 unconnected pane
