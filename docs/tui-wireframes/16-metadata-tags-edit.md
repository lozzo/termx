# 场景 16：Metadata / Tags 编辑

## 目标

定义 terminal 的 name/tags 编辑入口与编辑提示。

## 状态前提

- 用户从 Terminal Pool 或其他入口进入 terminal 编辑
- 当前编辑对象是 terminal，而不是 pane

## 线框图

```text
Step 1/2: rename terminal

termx  [main]  [Pool]
┌─ Terminal Pool [dim]──────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                                              │
│                     ┌─ Edit Terminal 1/2 ───────────────────────────────────────┐                            │
│                     │ terminal id: t-022                                        │                            │
│                     │ command: npm run dev                                      │                            │
│                     │                                                            │                            │
│                     │ name: api-dev                                              │                            │
│                     │                                                            │                            │
│                     │ Enter next  •  Esc cancel                                  │                            │
│                     └────────────────────────────────────────────────────────────┘                            │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

Step 2/2: edit tags

┌─ Edit Terminal 2/2 ─────────────────────────────────────────────┐
│ terminal id: t-022                                              │
│ tags: backend,prod,owner:alice                                  │
│                                                                  │
│ Enter save  •  Esc cancel                                        │
└──────────────────────────────────────────────────────────────────┘
```

## 关键规则

- 第一阶段就支持 metadata/tags 编辑
- 先作为原生信息处理，用于显示、编辑、搜索
- 不在本阶段扩展成复杂规则系统
- 编辑对象必须明确是 terminal，而不是 pane
- 保存后所有已绑定 pane 的标题和相关摘要立即刷新

## 流转

- open editor
- save
- pane title refresh
- cancel -> 返回来源页面，不改动 terminal
