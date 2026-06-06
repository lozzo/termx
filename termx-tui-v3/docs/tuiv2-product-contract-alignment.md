# tuiv2 产品契约与 tui-v3 对齐矩阵

状态：基准文档
日期：2026-06-06

## 1. 来源与边界

本文档只从 `tuiv2` 只读参考中抽取产品设计，不迁移旧 runtime、旧 model、Bubble Tea contract 或旧历史实现。

已通读和抽取的主要来源：

- `tuiv2/docs/tui-product-definition-design.md`
- `tuiv2/docs/tuiv2-keybinding-spec.md`
- `tuiv2/docs/tuiv2-mouse-support-matrix.md`
- `tuiv2/docs/tuiv2-chrome-layout-spec.md`
- `tuiv2/docs/tuiv2-workbench-tree-modal-design.md`
- `tuiv2/input/catalog.go`
- `tuiv2/render/hit_regions.go`
- `tuiv2/render/hit_regions_pane_chrome.go`
- `tuiv2/render/hit_regions_workbench.go`
- `tuiv2/render/overlay_footer_actions.go`
- `tuiv2/render/terminal_pool_layout.go`
- `tuiv2/app/update_mouse_click.go`
- `tuiv2/app/update_mouse_actions.go`
- `tuiv2/app/status_hints.go`

本文档是 `termx-tui-v3/docs/ui-interaction-spec.md` 的补充：后续 v3 对齐 tuiv2 时，必须优先满足这里列出的可见控件、动作语义、命中区和反馈规则。

## 2. 产品原则

- `terminal` 是 daemon 管理的全局运行实体，`pane` 是工作位或观察位；关闭 pane 默认不 kill terminal。
- workspace / tab / pane / floating pane 是工作台结构，不应只存在单个 TUI 客户端内存中；多客户端场景必须消费 daemon-owned snapshot/event。
- UI 上画出的按钮必须有稳定 hit region、semantic action、reducer/effect 路径和用户反馈；未接线动作不得提前画成可点击按钮。
- 快捷键、鼠标和测试入口必须进入同一 semantic action，不允许 header、pane、overlay 各自写一套业务逻辑。
- chrome 采用固定槽位。长文本只进 workspace/tab/pane title 或主内容区；角落和右侧状态区只放短 token。
- overlay/modal 不展示快捷键教学文案；快捷键提示属于 footer/status bar 和 Help。
- terminal 内容必须按 content rect 渲染、裁切和 resize；chrome hit region 优先于 terminal mouse passthrough。

## 3. 对象模型

| 对象 | tuiv2 产品语义 | tui-v3 对齐要求 |
| --- | --- | --- |
| Workspace | 工作现场，包含 tab、pane、floating pane；可创建、切换、重命名、删除 | 必须迁到 daemon workbench truth，TUI 只消费 snapshot/event 并提交 mutation |
| Tab | pane 的组织层；可创建、切换、跳转、重命名、关闭/kill | header tab strip、键盘 mode、鼠标 close/create 都必须走同一 workbench command/apply |
| Pane | terminal 的工作位；可空、连接、共享、exited；支持 split/focus/close/zoom/resize | pane command 是唯一结构操作入口，关闭 pane 和 kill terminal 必须区分 |
| Floating Pane | 覆盖在 tiled grid 上的完整 pane；有 z-order、focus、move、resize、collapse | 不参与 tiled split layout；点击 tiled pane 可让 floating 失焦但保持打开 |
| Terminal | daemon 运行实体；可 attach here/tab/floating、edit、kill/remove | 创建、attach、resize、input、kill 均走 core-v2 protocol/effect result |
| Terminal Pool | 全局 terminal 管理页面，不是 picker 放大版 | 独立 page/surface，支持列表、搜索、详情、预览和 footer action |
| Overlay | 临时选择/输入层，包括 picker、tree、prompt、help、floating overview | overlay active 时阻断底层 terminal input；点击遮挡区不能穿透 |

## 4. 输入模式契约

| 模式 | 入口 | tuiv2 动作 | tui-v3 状态 |
| --- | --- | --- | --- |
| Normal | 默认 | terminal input 直通；`Ctrl-P/R/T/W/O/V/F/G` 进入 pane/resize/tab/workspace/floating/copy/picker/global | 已有 root binding，需继续保证未绑定键直通 terminal |
| Pane | `Ctrl-P` | focus h/j/k/l/arrows；split `%`/`"`；detach `d`；reconnect `r`；restart `R`；owner `a`；size lock `s`；close `w`；close+kill `X`；zoom `z` | split/focus/close/close+kill/zoom 部分已有；detach/reconnect/restart/owner/size lock 仍缺产品闭环 |
| Resize | `Ctrl-R` | 小步 h/j/k/l/arrows；大步 H/J/K/L；balance `=`；cycle layout Space；content pan/align/center/reset | pane resize/balance 已有；cycle layout 和 content placement 未完整对齐 |
| Tab | `Ctrl-T` | create `c`；rename `r`；next/prev `n/p`；jump `1-9`；kill `x` | create/rename/next/prev/close 部分已有；jump/kill 语义和 daemon apply 仍需补齐 |
| Workspace | `Ctrl-W` | tree/picker `f/s`；create `c`；rename `r`；delete `x`；next/prev `n/p` | create/rename/next/prev/tree 部分已有；delete 和 daemon apply 仍需补齐 |
| Floating | `Ctrl-O` | create `n`；cycle Tab/ShiftTab；move h/j/k/l；resize H/J/K/L；center `c`；collapse `m`；overview `o`；summon 1-9；owner `a`；toggle all `v`；fit `=`；auto-fit `s`；close `x`；picker `f` | create/move/resize/center/collapse/close 部分已有；overview/summon/toggle/fit/owner/picker 未完整 |
| Copy/Display | `Ctrl-V` | cursor arrows/hjkl；PgUp/PgDn；u/d；Home/End；g/G；Space mark/copy；y copy；Enter copy+exit；p/P paste；H history | copy 浏览/选择/复制已有；paste/history 仍需补齐 |
| Global | `Ctrl-G` | `?` Help；`t` Terminal Pool；`q` Quit；Esc back | Help/Pool 已有但 v3 当前也有 header/footer/toast 调试类入口，后续 footer 需按产品重新整理 |
| Picker | `Ctrl-F` | filter；Up/Down；Enter attach；Tab split+attach；Ctrl-E edit；Ctrl-K kill；Ctrl-X remove；Esc close | attach/new 已有；split+attach/edit/kill/remove 不完整 |
| Terminal Pool | global `t` | filter；Up/Down；Enter attach here；Ctrl-T attach tab；Ctrl-O attach floating；Ctrl-E edit；Ctrl-K kill；Ctrl-X delete | page 已有；attach tab/floating/edit/delete 仍缺完整服务闭环 |
| Workbench Tree | workspace `f/s` | tree select；Enter open/focus；Ctrl-N new；Ctrl-R rename；Ctrl-X remove；Ctrl-D detach；Ctrl-Z zoom | tree 一期已有；多对象 CRUD/动作和 daemon truth 仍需补齐 |

## 5. Workbench Chrome

### 5.1 Header / Tab Bar

| 可见区域 | 动作 | 鼠标效果 | 对齐状态 |
| --- | --- | --- | --- |
| workspace label | open Workbench Tree / workspace picker | 点击打开结构导航 | v3 有入口，后续应绑定 daemon snapshot |
| tab switch token | jump/switch tab | 点击切换对应 tab | v3 应走 workbench apply，不再只改本地 reducer |
| tab close token `×` | close tab | 点击关闭目标 tab；不能静默失败 | v3 已修复本地点击，后续接 daemon |
| tab create token `＋` | create tab | 点击创建并激活新 tab；tuiv2 语义上后续应引导 attach 初始 terminal | v3 已修复本地点击，后续接 daemon 和初始 pane/terminal 流程 |
| tab rename/kill actions | rename / kill tab terminals then close | 可见时必须真实接线 | v3 不应提前画未接线 token |
| workspace prev/next/create/rename/delete | workspace CRUD/navigation | 可见 token 必须走同一 semantic action | v3 待 daemon workbench 对齐 |

Header 规则：

- 左侧承载 workspace + tab strip + create token。
- 右侧只放短 notice/error，不放长帮助、不放 pane owner 细节。
- token 宽度和命中区从同一 layout 计算；不能靠字符串搜索临时点位。

### 5.2 Footer / Status Bar

| 区域 | tuiv2 设计 | tui-v3 对齐要求 |
| --- | --- | --- |
| 左侧 mode | 当前 mode badge 和当前 mode 下可用动作 hints | hints 必须来自 binding/action catalog，并按上下文过滤不可用动作 |
| 右侧摘要 | `ws:*`、floating summary、terminal count 等全局短 token | 不放当前 pane 长标题，不放未设计的 owner/share token |
| footer action | overlay/pool 的 action chips 只写动作词，如 attach、split+attach、edit、kill、delete、close | 可点击 footer action 必须进入 render.ActionID/semantic action catalog |

Footer 规则：

- 不展示长句帮助。
- 不把 debug state 当产品摘要。
- 当前 mode 下不可用动作隐藏，不做假占位。

## 6. Pane Chrome 与 Pane 内容状态

### 6.1 Tiled Pane Chrome

| 槽位 | tuiv2 产品设计 | tui-v3 对齐要求 |
| --- | --- | --- |
| 左侧 title | terminal title / pane title / unconnected / exited | 可变长文本只在 title 区，宽度不足截断 |
| 右侧 lifecycle | running / pending / exited 极短 token | 未设计字形前不画 Nerd Font 状态 token |
| 右侧 share count | `x2` 等短 token | 需要真实 shared terminal 数据后再显示 |
| 右侧 role | owner / follower / follow/become owner 固定槽位 | 需要 owner/follower 语义和动作闭环后再显示 |
| action cluster | split-v、split-h、close；zoom/kill/detach/reconnect 仅在已接线时出现 | v3 当前只应显示已接线 split-down、split-right、close |
| resize affordance | 边框/分隔线可拖动 resize | v3 已有 divider/edge resize，需继续防止 terminal mouse 穿透 |

### 6.2 Pane 内容状态

| 状态 | 可见内容 | 动作 |
| --- | --- | --- |
| connected | live terminal surface、cursor、terminal style、pane title/status | input/resize/mouse passthrough 只在 active content 且 terminal 开启 mouse tracking 时发生 |
| empty | 未连接说明、Attach existing、Create new、Open manager、Close pane | attach 是 primary；create/manager secondary；close 有反馈 |
| exited | 最后输出、exited 状态、exit code/原因、Restart/Reconnect/Close | restart/reconnect 不得伪造，要走 terminal service |
| copy mode | authoritative history rows、cursor、selection、clipped markers、position summary | 不得从 live surface 或 local scrollback fallback |

## 7. Overlay / Page

| Surface | tuiv2 契约 | tui-v3 对齐要求 |
| --- | --- | --- |
| Terminal Picker | page-sized 快速选择器；search、list、selected row、detail、attach/new、footer action | 补 split+attach/edit/kill/remove；创建必须带默认 shell 或显式 command |
| Terminal Pool | 独立 page；list + detail/preview；footer here/tab/float/edit/kill/delete | 补 attach tab/floating/edit/delete 服务闭环和多客户端刷新 |
| Workbench Tree | 大型结构 modal；workspace/tab/pane/floating tree + detail/snapshot；不展示快捷键教学 | 结构数据改从 daemon snapshot/event 来；补 delete/detach/zoom 等动作 |
| Prompt | input/form、submit/cancel、鼠标定位 cursor | v3 已有一期，需保持不漏发 terminal |
| Help | 基于 binding catalog 分组展示 | 后续应反映实际已接线动作，避免帮助列出不可用功能 |
| Floating Overview | 列出 floating pane；open/show all/collapse all/close/summon | v3 未完整对齐，不能提前在 footer/header 画假入口 |

Overlay 规则：

- overlay/card 区域拦截鼠标；关闭后恢复原工作现场。
- overlay footer action chip 只写动作词，不写快捷键说明。
- picker row click 是选择/提交语义，不应穿透到底层 pane。

## 8. 鼠标与可点击区域契约

tuiv2 的鼠标模型是：

1. renderer 生成稳定 hit region。
2. app 用最新 frame 的 hit region 命中鼠标坐标。
3. hit region 转成 semantic action。
4. action 进入 reducer/effect/service result。

v3 必须保持这个方向。

| 区域 | 鼠标行为 |
| --- | --- |
| header workspace | click open Workbench Tree |
| header tab name | click switch tab |
| header tab `×` | click close tab |
| header `＋` | click create tab |
| pane chrome | click focus pane；action token click 触发对应 pane command |
| pane content | click focus active pane；若 terminal mouse tracking 开启且是 active content，则 raw mouse passthrough |
| split divider / pane edge | drag resize，release 清理 drag state |
| floating title | click raise；drag move |
| floating resize handle | drag resize |
| floating close | close floating |
| overlay card | click inside 操作 overlay；outside dismiss 仅在明确允许时发生 |
| picker/pool/tree row | click select/open/attach，不穿透 |
| prompt input | click 定位输入 cursor |
| toast | close token 可关闭；toast 本体拦截遮挡不穿透 |

## 9. tui-v3 优先对齐队列

P0 必须先做：

- 不再画未接线按钮；所有可见按钮都能点击、能反馈、能走同一 semantic action。
- daemon workbench snapshot/event 接入 v3 workspace/tab/pane truth，header/tab/pane/tree 的结构动作统一走 `workbench.apply`。
- tab close/create、workspace create/delete、pane split/close/focus 等动作在键盘、鼠标、测试入口下语义一致。

P1 接着做：

- 补齐 tuiv2 输入目录中 v3 缺失的 tab jump、workspace delete、picker split+attach、pool attach tab/floating、terminal edit/delete、floating overview/summon/toggle/fit。
- footer/status hints 从 v3 binding/action catalog 和当前上下文生成，Help 只展示真实可用动作。

P2 再做：

- pane owner/follower/share/lifecycle token 的 Nerd Font 字形体系、fallback、槽位宽度和 hit region。
- Terminal Pool / Workbench Tree 的详情、preview、footer action 和 daemon event 多客户端刷新。

P3 最后做：

- floating overview 完整闭环。
- copy mode paste/clipboard history。
- workspace/tab/pane 高级 destructive confirm 与批量 kill。

## 10. 当前明确差距

- v3 有 action catalog，但还没有覆盖 tuiv2 的全部产品动作。
- v3 header tab close/create 本地已可用，但仍未以 daemon workbench truth 为最终来源。
- v3 pane chrome 当前只能显示 split-down、split-right、close；owner、share、lifecycle、zoom、detach、reconnect 等未完成前不应显示。
- v3 Terminal Picker/Pool/Workbench Tree 已有一期页面，但 footer actions 和 daemon 多客户端同步仍未完整。
- v3 Help/footer 若展示未接线动作，会制造产品误导；后续必须由真实 action availability 驱动。
