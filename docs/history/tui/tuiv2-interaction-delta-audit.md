# tuiv2 交互设计到 tui-v3 差异审计

状态：独立审计
日期：2026-06-16

## 1. 目的和边界

本文档只整理 `tuiv2` 中可取的交互设计，并对照 `tui` 当前实现列出差异和修复队列。

本任务不属于 `workflow.md` 当前 history/copy 主线切片，不修改 `workflow.md`。`tuiv2/` 仍为只读参考，不能把旧 runtime、Bubble Tea contract、旧 history truth 或旧 scrollback fallback 迁入 v3。

已参考的主要来源：

- `tuiv2/docs/tuiv2-keybinding-spec.md`
- `tuiv2/docs/tuiv2-mouse-support-matrix.md`
- `tuiv2/docs/tui-product-definition-design.md`
- `tuiv2/docs/tuiv2-chrome-layout-spec.md`
- `tuiv2/docs/tuiv2-workbench-tree-modal-design.md`
- `tuiv2/input/catalog.go`
- `tuiv2/app/update_mouse.go`
- `tuiv2/app/update_mouse_click.go`
- `tuiv2/app/update_mouse_overlay.go`
- `tuiv2/app/update_mouse_pane_surface.go`
- `tuiv2/app/status_hints.go`
- `tui/docs/shortcut-inventory.md`
- `tui/docs/tuiv2-product-contract-alignment.md`
- `tui/app/runtime.go`
- `tui/app/ui_input.go`
- `tui/render/layout_hit_regions.go`
- `tui/render/vm.go`
- `tui/render/action_ids.go`

## 2. tuiv2 可取交互原则

| 编号 | tuiv2 设计 | 要保留的原因 | v3 对齐要求 |
| --- | --- | --- | --- |
| T2-01 | normal mode 只保留少量 root 入口，普通键默认直通 terminal | 日常 shell/vim/htop 输入不被 TUI 抢走 | root keymap 继续只保留 `Ctrl-P/R/T/W/O/V/F/G` 等明确入口；UI mode 下未绑定键吞掉 |
| T2-02 | mode 是短驻留工具层，不是产品本体 | 用户不应长期处于结构模式，也不应因误按卡死 | v3 当前能进入/退出 mode，但仍缺 mode timeout/续期审计；后续应决定是否恢复短驻留模型 |
| T2-03 | binding catalog 是 status/help/router 的单一来源 | 防止 footer 画出不可用动作或帮助列错键 | v3 已有 `BindingCatalog` 和 `ActionSpecCatalog`，但 footer 仍是手写 `footerActionCatalog`，需要引入上下文过滤 |
| T2-04 | 鼠标交互基于 render hit region，再转 semantic action | 几何和业务不散落，鼠标/键盘/测试走同一语义 | v3 已采用 hit region，但部分区域只取第一个 region，缺少 pane focus 这种前置语义 |
| T2-05 | pane content click 永远先 focus/raise 所属 pane | pane 切换必须跟手，点击文本也不能被内容动作吃掉 | v3 当前 copy/history row 命中会绕过 focus，是 P0 bug |
| T2-06 | terminal mouse passthrough 只给 active pane content，UI chrome 优先 | 终端 app 鼠标功能和 TUI chrome 不互相抢 | v3 基本一致，但受 hit region 排序影响，需要补完整回归 |
| T2-07 | overlay/page 打开后阻断底层输入和鼠标穿透 | modal 是明确操作上下文，不允许误点底层 pane | v3 基本一致；需确认非 opaque overlay 与 toast/floating 的穿透边界 |
| T2-08 | prompt/query input 支持鼠标点击定位 cursor | 搜索和输入是可编辑字段，不只是追加文本 | v2 已实现；v3 当前多数字段仍是追加式 query，需要补 cursor 模型或明确降级 |
| T2-09 | status hints 按上下文过滤不可用动作 | 用户只看到当前真的能做的事 | v3 footer 当前按 mode 静态列动作，未充分按 active pane/floating/selection 过滤 |
| T2-10 | chrome 固定槽位，长文本只放 title/content | 避免状态文字撑动动作热区，降低误点 | v3 部分继承，但 pane meta 仍会出现 `size:... layout:...` 这类长 token |
| T2-11 | Empty / Exited pane 不是死空白，有直接 action | pane 状态本身可操作，减少迷路 | v3 已有 empty/exited 内容和 action，但鼠标/键盘/显示文案仍需统一 polish |
| T2-12 | Workbench Tree 是结构导航，不是 workspace name picker | workspace/tab/pane/floating 都应能被浏览和操作 | v3 已有 Workbench Tree 一期；需按树/预览/动作词规范继续对齐 |

## 3. 当前差异矩阵

| 优先级 | 交互面 | tuiv2 语义 | tui-v3 当前状态 | 差异 / 风险 | 建议处理 |
| --- | --- | --- | --- | --- | --- |
| P0 | pane content click focus | 点击 pane interior/chrome 必须先 focus pane；floating content 先 raise | `HitRegionHistoryRow` 在 `HitRegionPaneContent` 前命中，runtime 直接转 `CopyModeMouseSelectMsg` | copy/history 文本点击不切 pane；非文本点击也可能被 copy mode 输入路由吞掉 | 鼠标左键命中 pane 范围时先 focus/raise pane，再分发 history row/select 或 content action |
| P0 | copy/history mouse row | history row selection 是当前 copy pane 内的后续动作 | history row hit region 没有 `PaneID`，无法判断命中哪个 pane | 点击其他 pane 的 history row 会丢 pane 归属，甚至可能改当前 copy cursor | history row hit region 注入 `PaneID`，runtime 校验 pane 归属；非当前 copy pane 只 focus，不 selection |
| P0 | hit region 优先级 | chrome/action/resize > pane focus > terminal passthrough/content action 的组合语义清晰 | v3 只取第一个命中 region，缺少“前置 focus + 后续 action”复合分发 | 背景 focus 区和前景内容 action 互斥 | 引入鼠标分发层：先找遮挡/drag/action，再找 enclosing pane/floating focus region |
| P0 | footer 可用性 | status hints 按 active pane、terminal role、floating 是否存在、selection kind 过滤 | `footerActionCatalog(mode)` 大多静态列出 mode 动作 | 无 active floating 时也可能展示 floating center/collapse/owner 等动作；没有多个 tab/workspace 仍展示 next/prev | 按 v2 `status_hints.go` 建立 v3 `FooterAvailabilityContext` |
| P1 | mode lifecycle | mode 短驻留，成功动作后续期，Esc 总能退出 | v3 mode 多数为显式状态，没有 timeout/续期模型 | 用户可能长期停在结构 mode；误按吞 terminal input | 明确产品选择：恢复 timeout，或文档化为显式 mode；若恢复则补 runtime timer |
| P1 | root/global footer | global mode 只承载 help、terminal pool、quit 等全局动作 | v3 global footer 混入 header/footer toggle、toast clear 等 chrome/debug 操作 | footer 信息密度偏工程化，弱化核心路径 | 将 header/footer/toast 控制降级为次级入口或隐藏在 Help/Prompt；global footer 默认回到 v2 核心 |
| P1 | prompt/query cursor | prompt、picker、workspace tree、terminal pool query 点击能定位 cursor | v3 overlay query 多数是简单字符串追加/退格模型 | 鼠标点击搜索字段不具备编辑体验，和 v2 交互不一致 | 为 overlay query 引入 reducer-owned cursor；mouse hit region 携带 col |
| P1 | overlay row click | picker row click 通常 select + submit；tree/pool row click按语义 select/open | v3 content action 能 select/open，但不同 overlay footer 只暴露部分 action | 鼠标和键盘动作覆盖不一致，Terminal Pool footer 没完全展示 tab/float/delete | 对 overlay footer 和 row click 做逐 surface 对表，补缺失 action chip |
| P1 | Workbench Tree | 大型结构 modal，左树右预览，不展示快捷键教学 | v3 已有 Workbench Tree，但 footer 只显示 open/focus，内容 action 有 rename/new/delete/detach/zoom | footer 和内容 action 不完全对齐，树语义仍偏一期列表 | footer action 按 selected kind 展示 Open/Rename/New/Delete/Detach/Zoom；保留纯动作词 |
| P1 | Terminal Pool | 资源管理 page，支持 here/tab/float/edit/kill/delete | v3 keyboard/action 已有多项，但 footer 只显示 attach/edit/kill | 鼠标用户发现不了 attach tab/floating/delete | footer 补 tab/float/delete，并按 selected terminal 可用性过滤 |
| P1 | pane chrome slots | 右侧 role/share/lifecycle/action 固定槽位，窄屏按优先级丢弃 | v3 右侧 meta 会展示 `size:follower lock layout:...` 长 token | 状态文本挤占标题/action，命中区和视觉不稳定 | 把 resize role/lock/layout 压成固定短 token；详情放 footer/help/tooltip 类路径 |
| P1 | pane chrome action visibility | 只画已接线动作；未实现动作不出现 | v3 默认 pane chrome 画 zoom/split/close，footer 画 detach/balance/card/line 等 | 部分动作虽已有实现，但显示策略缺上下文；empty/exited/copy pane 上也要审计 | 以 action availability 驱动 chrome/footer；为 empty/exited/copy 单独定义 action 集 |
| P1 | terminal mouse passthrough | active content 且 terminal mouse tracking 开启时 raw mouse 转发；UI chrome 不转发 | v3 有 `mouseEventCanPassthrough`，但依赖第一个 hit region 必须是 `PaneContent` | history row 或内容 action 覆盖区域会改变 passthrough 判断 | passthrough 判断应查 enclosing pane content rect，而不是只看第一个 hit region |
| P1 | wheel fallback | 非 terminal passthrough 时，wheel 可用于 copy/list/scroll；alt-screen 可走 cursor fallback | v3 copy wheel 已强化；overlay wheel/terminal alt-scroll 需逐项核对 | wheel 行为在不同 surface 可能不一致 | 补 overlay/pool/tree/help wheel 行为表和测试 |
| P2 | floating focus/raise | 点击 floating title/content focus 并置顶；点击未遮挡 tiled pane 可让 floating 失焦但保持打开 | v3 有 floating raise/content hit region | 需要确认 copy/history、terminal passthrough 和 floating active state 的组合 | 增加 floating + copy/history + tiled click 回归 |
| P2 | Empty pane CTA | empty pane action 可键盘选中/鼠标点击，attach/create/manager/close 分清 | v3 已有 empty CTA | 需要避免 content action 命中抢 pane focus；create/attach 文案和 action 是否一致需核对 | 纳入统一 pane click 前置 focus 机制 |
| P2 | Exited pane recovery | exited pane 保留最后内容，展示 restart/reconnect/close | v3 live exited 追加 restart/picker 文案，product content 也有 exited action | 可能存在两套 exited 表达，命中区/文案不统一 | 收敛到一套 exited content projector + action hit regions |
| P2 | Help overlay | Help 解释分组和概念，不是纯快捷键表，也不列不可用动作 | v3 help 受 ActionSpec/Binding 影响，需核对实际内容 | 帮助可能列出不可用或低优先级工程动作 | Help 按真实可用 action 和概念分组重建 |

## 4. P0 鼠标焦点问题的具体结论

当前真实 bug：

1. `render.measureHitRegions` 按前景到背景顺序输出 hit region。
2. pane content 整块 focus region 在内容专属 hit region 之后追加。
3. copy/history 的 `HitRegionHistoryRow` 先于 `HitRegionPaneContent` 命中。
4. `AppRuntime.dispatchMouseHitRegion` 对 `HitRegionHistoryRow` 直接返回 `CopyModeMouseSelectMsg`。
5. `CopyModeMouseSelectMsg` 不携带 pane/floating id，只修改 copy cursor/mark，不 focus pane。

这违反了 tuiv2 的核心体验：点击 pane 内任意位置，焦点必须立即跟手切换；内容专属动作只能作为后续语义。

建议修正模型：

- 左键按下先解析遮挡层：toast、opaque overlay、floating 顶层、pane action、split divider。
- 如果坐标处于 pane/floating content 或 chrome，先生成 focus/raise 语义。
- 再根据更精细的 content hit region 执行后续动作：history cursor、empty CTA、exited recovery、pool/tree row action。
- 后续动作必须携带 pane/floating/view 归属；归属不匹配时只 focus，不修改 copy state。
- terminal mouse passthrough 只在 active pane 已确认且没有 UI chrome/action 吃掉事件时发生。

## 5. 推荐修复队列

| 顺序 | 切片建议 | 目标 | 最低测试 |
| --- | --- | --- | --- |
| 1 | `SK tui-v3 mouse focus priority` | 修复 pane content/history row 点击先 focus；history row 带 pane id；copy mode 点击其他 pane 不改旧 copy cursor | app runtime 鼠标点击 split pane、copy/history row、非文本 content、floating content |
| 2 | `SK tui-v3 mouse dispatch contract` | 抽出 mouse hit resolution，支持 enclosing pane focus + foreground action 的复合语义 | render hit region 顺序测试 + runtime no terminal leak |
| 3 | `SK tui-v3 footer availability` | footer 从静态 mode catalog 改为按上下文过滤 | active pane empty/exited/live/copy、无 floating、多 workspace/tab 的 footer snapshot |
| 4 | `SK tui-v3 overlay mouse parity` | picker/pool/tree/clipboard/floating overview 的 row click、footer click、wheel、query cursor 对齐 | overlay mouse/key parity tests |
| 5 | `SK tui-v3 chrome slot cleanup` | pane/floating chrome 右侧固定槽位，压缩 owner/share/lifecycle/layout token | narrow/wide chrome layout tests |
| 6 | `SK tui-v3 help-global cleanup` | global footer 和 Help 回到产品核心动作，不展示工程调试动作为主路径 | help/footer snapshot tests |

## 6. 实施约束

- 不能把 `tuiv2` 的 Bubble Tea 类型、model、history fallback 或 scrollback truth 迁入 v3。
- 修复应从 v3 自有 reducer/effect/message/render hit region contract 入手。
- 所有鼠标、键盘、footer、测试入口必须落到同一 semantic action。
- 可见按钮必须真实接线；没有 service/reducer 闭环的动作不显示。
- copy/history 仍只能消费 core-v2 authoritative `HistoryWindow`，不能因为修交互回退到 live/snapshot/local scrollback。
