# 场景 05：Terminal Pool 总览

## 目标

定义 Terminal Pool 独立页面的三栏式主画面。

## 状态前提

- 当前已进入 Terminal Pool 页面
- 左栏有 terminal 列表
- 中栏显示所选 terminal 的实时内容
- 右栏显示 metadata 与连接关系

## 线框图

```text
termx  [main]  [Pool]                                            screen:terminal-pool  selected:t-022
 Terminal Pool  •  global terminal registry                                        search:"api"
┌─ TERMINALS ───────────────────────────────┬─ LIVE PREVIEW ───────────────────────────────────┬─ DETAILS ──────┐
│ query: api                                │ api-dev                                          │ name: api-dev  │
│                                           │                                                   │ state: running │
│ VISIBLE                                   │ $ npm run dev                                     │ owner: pane-3  │
│ > ● api-dev         #backend  shown:2     │ ready on :3000                                    │ tags: backend  │
│   ● api-log         #backend  shown:1     │ GET /health 200                                   │ cwd: ~/api     │
│                                           │                                                   │ cmd: npm run…  │
│ PARKED                                    │                                                   │                │
│   ● worker-tail     #ops      shown:0     │                                                   │ connections    │
│   ● htop-prod       #ops      shown:0     │                                                   │ - ws-1/tab-1   │
│                                           │                                                   │   /pane-3      │
│ EXITED                                    │                                                   │ - ws-1/tab-2   │
│   ○ old-api         #legacy   shown:1     │                                                   │   /float-1     │
│                                           │                                                   │                │
│ ↑↓ move  / filter by name tags command    │ read-only live observe                            │ metadata first │
└───────────────────────────────────────────┴───────────────────────────────────────────────────┴────────────────┘
 <enter> OPEN HERE  <t> NEW TAB  <o> FLOAT  <e> EDIT  <k> KILL  <d> REMOVE  <esc> BACK
```

## 关键规则

- 左栏分组为 `visible / parked / exited`
- 中栏默认只读实时观察
- 右栏先显示 metadata，再显示连接关系
- 这个页面是独立页面，不是 connect dialog 的放大版
- 中栏允许看 live 内容，但第一阶段不把日常输入焦点交给这里
- 搜索优先覆盖 terminal `name / tags / command`

## 流转

- 可进入 terminal action
- 可返回 workbench
- 可进入 metadata/tags 编辑
- 可把选中的 terminal 打开到当前 pane、新 tab、floating pane
