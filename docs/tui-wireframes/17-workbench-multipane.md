# 场景 17：Workbench 多 Pane 常态

## 目标

定义最常见的多 pane 平铺工作台，明确 active pane、非 active pane、标题信息密度与底栏焦点摘要。

## 状态前提

- 当前 tab 中有多个 tiled pane
- 至少一个 pane 是 active
- pane 之间可以绑定不同 terminal

## 线框图

```text
termx  [main]  [1:dev]  2:logs  3:ops
┌─ api-dev────────────────────────────running  owner─────┬─ watcher────────────────running  owner────────────┐
│$ npm run dev                                           │> tsc -w                                             │
│ready on :3000                                          │Found 0 errors.                                      │
│GET /health 200                                         │                                                     │
│                                                        │                                                     │
├─ git-shell────────────────────────────────────────────────────────────────running  owner──────────────────────┤
│$ git status                                                                                            │
│On branch main                                                                                          │
│nothing to commit, working tree clean                                                                   │
│                                                                                                        │
└────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 <c-p> pane  <c-t> tab  <c-w> workspace  <c-f> connect  <c-o> float  <?> help        git-shell  ▣ tiled
```

## 关键规则

- active pane 必须通过边框或标题高亮被清楚识别
- pane 左侧展示 terminal 名称，不额外强调 pane 自己的名字
- pane 标题右侧只放短状态，例如 `running / owner / follower / exited`
- 底栏右侧只跟随当前 active pane，不展示整屏所有 pane 的状态

## 流转

- move focus -> 切换 active pane
- split active pane -> 打开 connect dialog
- close active pane -> 其他 pane 顶上成为 active
