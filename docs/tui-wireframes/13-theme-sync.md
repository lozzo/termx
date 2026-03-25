# 场景 13：主题同步

## 目标

定义宿主终端主题变化时，正文、snapshot 与 chrome 的同步规则。

## 状态前提

- 宿主终端发生主题或 palette 变化
- 当前已有 live 内容与历史 snapshot

## 线框图

```text
Before: host theme = dark

┌─ shell-dev [chrome uses dark defaults]────────────────────────────┐
│ default fg/bg interpreted with dark palette                       │
│ old snapshot line still readable under dark defaults              │
└───────────────────────────────────────────────────────────────────┘

Host terminal theme changed -> light

┌─ shell-dev [chrome repainted with light defaults]─────────────────┐
│ same cells, reinterpreted with new default fg/bg                 │
│ same snapshot history, repainted with new palette                 │
└───────────────────────────────────────────────────────────────────┘

Rule:
  explicit RGB / indexed colors stay explicit
  only default fg/bg and palette mapping follow host theme
```

## 关键规则

- 宿主终端主题变化时，默认色与 palette 色应同步重解释
- 正文、旧 snapshot、外层 chrome 都应一起跟随
- 显式指定的颜色不做二次篡改
- 同步发生后需要整屏重绘，不能只改 chrome 不改正文
- 历史 snapshot 也必须跟随，否则主题切换后会出现断层

## 流转

- host theme update
- repaint with new defaults
