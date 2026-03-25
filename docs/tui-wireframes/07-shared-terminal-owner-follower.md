# 场景 07：Shared Terminal / Owner / Follower

## 目标

定义共享 terminal 时的 owner/follower 表达，以及 `Become Owner` 动作。

## 状态前提

- 同一个 terminal 同时被多个 pane 绑定
- 当前最多一个 owner
- 其他均为 follower

## 线框图

```text
termx  [main]  [1:dev]  2:ops                                    pane:ws-1-tab-1-pane-2  term:t-022  float:0
 main / dev / tiled / shared terminal api-dev                                       running  share:2
┌─ api-dev─────────────────────────────────────────────────┬─ api-dev──────────────────────────────────────────┐
│$ npm run dev                                            │$ npm run dev                                       │
│ready on :3000                                           │ready on :3000                                      │
│GET /health 200                                          │GET /health 200                                     │
│                                                         │                                                    │
│                                                         │                                                    │
│                                                         │                                                    │
│ owner  •  this pane drives PTY size                     │ follower  •  read/live observe                     │
│                                                         │ [b] Become Owner                                   │
└─────────────────────────────────────────────────────────┴────────────────────────────────────────────────────┘
 <b> BECOME OWNER  <c-f> RECONNECT  <x> CLOSE PANE                              api-dev  share:2  follower
```

## 关键规则

- 一个 terminal 同时最多一个 owner
- 新连接已有 terminal 时：
- 无 owner 则自动成为 owner
- 否则默认 follower
- owner 关闭/解绑后不自动迁移
- `Become Owner` 直接抢占 owner
- 抢占后原 owner 立刻降为 follower，不走中间确认状态
- follower 仍保持 live 连接，只是不能提交 PTY size

## 流转

- follower -> Become Owner -> owner
- owner close/unbind -> no owner
- no owner + next attach -> new owner
- remove shared terminal -> all bound panes become unconnected pane
