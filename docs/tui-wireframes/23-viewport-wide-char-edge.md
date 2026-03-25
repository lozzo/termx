# 场景 23：Viewport 宽字符与 Emoji 边界

## 目标

定义裁切边界遇到宽字符、emoji、powerline 片段时的显示策略，避免半个字符或错位残影。

## 状态前提

- pane 正在对 terminal 做裁切显示
- 裁切边界命中了双宽字符或组合字符

## 线框图

```text
原 terminal 内容（逻辑上）：

│ build: 成功 🚀  branch: main   deploy  prod │

错误示例：不要这样画

│ build: 成功 �  branch: main  � deploy  prod │

正确示例：宁可留空也不画半个字符

┌+status──────────────────────────────────────────────────────────+┐
│ build: 成功    branch: main    deploy  prod                   │
└++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++┘

说明：
- `🚀` 被边界切中时直接整字符不显示
- powerline 连接符被切中时，边界留空
- 由 `+` 告知用户此侧仍有被裁切内容
```

## 关键规则

- 不允许半个宽字符出现在边界
- 不允许 emoji 组合序列被截成脏字符
- 不允许 powerline 片段切出乱码边
- 信息损失由 `+` 与可移动 viewport 来补偿

## 流转

- crop hits wide char -> leave blank at edge
- viewport shift -> may reveal full glyph
- restore offset -> restore prior safe crop
