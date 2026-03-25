# 场景 11：Help Overlay

## 目标

定义第一阶段 help 的内容组织与展示方式。

## 状态前提

- 用户显式打开 help
- 当前进入 overlay 层

## 线框图

```text
termx  [main]  [1:dev]  2:logs
┌─ shell-dev [dim]─────────────────────────────────────────────────────────────────────────────────────────────┐
│$ npm run dev                                                                                                 │
│                                                                                                              │
│               ┌─ Help ─────────────────────────────────────────────────────────────────────┐                 │
│               │ Most Used                                                                  │                 │
│               │  c-f connect pane   c-o new float   c-t tab actions                       │                 │
│               │                                                                            │                 │
│               │ Pane / Tab / Workspace                                                     │                 │
│               │  split pane   close pane   move focus   switch tab   switch workspace      │                 │
│               │                                                                            │                 │
│               │ Shared Terminal                                                            │                 │
│               │  connect existing   become owner   kill vs remove   restart exited         │                 │
│               │                                                                            │                 │
│               │ Floating                                                                    │                 │
│               │  move   resize   recall center   close float                               │                 │
│               │                                                                            │                 │
│               │ Exit / Close                                                                │                 │
│               │  close pane keeps terminal   close tab removes tab   Esc closes overlay    │                 │
│               │                                                                            │                 │
│               │ Esc close  •  returns to previous valid focus                              │                 │
│               └────────────────────────────────────────────────────────────────────────────┘                 │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <esc> CLOSE HELP                                                                       overlay  help
```

## 关键规则

- help 不是纯快捷键表
- 采用分组式说明
- 至少覆盖 `Most used`
- 至少覆盖 `Pane / Tab / Workspace`
- 至少覆盖 `Shared terminal`
- 至少覆盖 `Floating`
- 至少覆盖 `Exit / Close`
- 文案优先解释语义差异，例如 `close pane` 与 `kill terminal`

## 流转

- Esc close
- return to previous focus
