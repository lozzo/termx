# 场景 16：Metadata / Tags 编辑

## 目标

定义 terminal 的 name/tags 编辑入口与编辑提示。

## 状态前提

- 用户从 Terminal Pool 或其他入口进入 terminal 编辑
- 当前编辑对象是 terminal，而不是 pane

## 线框图

```text
TODO
```

## 关键规则

- 第一阶段就支持 metadata/tags 编辑
- 先作为原生信息处理，用于显示、编辑、搜索
- 不在本阶段扩展成复杂规则系统

## 流转

- open editor
- save
- pane title refresh
