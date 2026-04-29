# TUIV2 设计与实现差异分析

日期：2026-04-04

对照文档：

- `tui-product-definition-design.md`
- `tuiv2-keybinding-spec.md`
- `tuiv2-chrome-layout-spec.md`
- `tuiv2-current-status.md`

## 1. 设计出入

| 优先级 | 设计点 | 当前实现 | 判断 | 代码位置 |
| --- | --- | --- | --- | --- |
| P0 | owner 关闭/解绑后，不应自动迁移到其他 pane；接管必须显式 `Become Owner` | `syncTerminalOwnership()` 在 owner 失效时会自动挑第一个 bound pane 变成新 owner | 明显偏离 shared terminal 的 owner/follower 产品语义 | `tuiv2/runtime/ownership.go:9` |
| P0 | follower 的几何变化不应改写 terminal PTY size | `resizeVisiblePanesCmd()` 对所有可见 pane/floating 都发 resize；共享 terminal 时最后一个 pane 会覆盖前一个 | 共享终端的 size contract 不正确，follower 会影响 owner | `tuiv2/app/update_runtime.go:147` |
| P1 | pane 内部观察偏移应是 pane/workspace 级可恢复状态 | 现在只有 `TabState.ScrollOffset`；scroll 改 tab 级字段；persist schema 里没有 pane 级 viewport state；`FloatingVisible` 也未持久化，restore 时直接按是否有 floating 推导 | display model 仍是 tab 级临时状态，不符合产品定义 | `tuiv2/workbench/types.go:20` `tuiv2/app/update_actions_local.go:257` `tuiv2/persist/schema_v2.go:14` `tuiv2/bootstrap/restore.go:70` |
| P1 | Terminal Pool 应是三栏 page：左列表 / 中 live attach view / 右 metadata & relationships | 当前 page 是“分组列表 + 下方 preview/detail”的单流式页面；preview 依赖 runtime 中已有 snapshot，没有页面自己的 attach/live session | 已升格为 page，但还未达到产品定义的 first-class Terminal Pool | `tuiv2/render/coordinator.go:468` `tuiv2/modal/terminal_manager.go:5` |
| P2 | Terminal Pool 默认排序应按“最近用户交互”，不是按名字或纯输出 | 当前按 group 后再按 name 排序 | 主排序语义尚未落地 | `tuiv2/app/update_actions_terminal_manager.go:66` |
| P1 | floating pane 可拖出主视口，但必须保留左上角拖动锚点在大窗口内 | 现在 move/resize 后会整体 clamp 到 bounds 内，整窗不能越界 | 实现比设计更保守，和文档不一致 | `tuiv2/workbench/mutate.go:431` `tuiv2/workbench/mutate.go:585` |
| P1 | floating pane 内部不应继续 split | split 路径忽略 `SplitPane()` 的错误，即使 active pane 是 floating，也继续开 picker | 应禁止但当前可能进入坏状态 | `tuiv2/orchestrator/orchestrator_pane.go:11` |

## 2. 设计过度 / 迁移残留

| 优先级 | 项目 | 当前实现 | 判断 | 代码位置 |
| --- | --- | --- | --- | --- |
| P1 | picker 应保持轻量 attach surface，terminal 管理应回到 Terminal Pool | picker 里仍有 `Ctrl-E edit`、`Ctrl-K kill`，并且实际会打开编辑 prompt / 触发 kill | picker 被继续做成 mini-manager，信息架构回退 | `tuiv2/input/catalog.go:146` `tuiv2/app/update_actions_modal.go:89` |
| P2 | Terminal Pool 已是 page，但内部状态仍沿用 modal-era 方案 | 仍使用 `TerminalManagerState`、`terminalPoolPageModeToken`、`promptReturnMode()` 处理 page/prompt 往返 | 不是直接功能错误，但说明 page 形态还没完全脱离 modal 语义 | `tuiv2/modal/terminal_manager.go:5` `tuiv2/app/update_modal_prompt_openers.go:116` |
| P3 | 存在重复或未使用的迁移残留抽象 | `ModePrefix` 仍在定义里；`AttachTerminalManager()` / `VisibleOverlayTerminalManager` 还保留，但主路径已是 page surface | 低优先级清理项 | `tuiv2/input/mode.go:7` `tuiv2/render/adapter.go:91` |

## 3. 未实现功能

| 优先级 | 功能 | 现状 | 判断 | 代码位置 |
| --- | --- | --- | --- | --- |
| P1 | `remove terminal` | `ActionRemoveTerminal` 只有 action 常量，没有 keymap / handler / orchestrator 路径 | 产品文档明确区分 `kill` 与 `remove`，当前只做了 `kill` | `tuiv2/input/actions.go:55` |
| P1 | exited pane 上的 `R restart` | `tuiv2` 里没有 restart action / keymap / handler | 产品文档点名的恢复动作未落地 | 缺失对应实现 |
| P2 | `swap pane` | 测试直接 `t.Skip("ActionSwapPaneLeft not yet implemented")` | 明确未实现 | `tuiv2/app/feature_test.go:428` `tuiv2/input/actions.go:17` |
| P1 | viewport move / 鼠标拖拽观察内部内容 / 横向 display 行为 | display mode 现在只有 `u/d` scroll 和 `z` zoom | 产品定义中的 viewport move 工作流未实现 | `tuiv2/input/catalog.go:134` |
| P2 | Terminal Pool 真正的 live attach preview | 当前 preview 只是读 runtime 中已有 snapshot；若未 attach 过则显示 `(no live preview)` | 距离“中栏默认实时 attach view”还有一段 | `tuiv2/render/coordinator.go:504` |

## 4. 补充观察

- canonical root keymap 已基本回到 `Ctrl-p/r/t/w/o/v/f/g`，主方向是对的。
- `split / new tab / new float` 基本都已统一到“先建 pane slot，再开 picker”的链路。
- `unconnected pane` 空态已存在，方向与产品定义一致。
- `Terminal Pool` 已经从 overlay 升格为 page surface，但内部数据结构和渲染形态还处于过渡态。
