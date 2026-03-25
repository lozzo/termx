# 场景 13：主题同步

## 目标

定义宿主终端主题变化时，正文、snapshot 与 chrome 的同步规则。

## 状态前提

- 宿主终端发生主题或 palette 变化
- 当前已有 live 内容与历史 snapshot

## 线框图

```text
TODO
```

## 关键规则

- 宿主终端主题变化时，默认色与 palette 色应同步重解释
- 正文、旧 snapshot、外层 chrome 都应一起跟随
- 显式指定的颜色不做二次篡改

## 流转

- host theme update
- repaint with new defaults
