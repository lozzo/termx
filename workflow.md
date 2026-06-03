# 工作流：完整逻辑行历史模型落地

本文件是当前分支唯一有效的活动驱动文件。后续所有分析、删除、实现、测试、提交都必须先读取本文件，并以本文件为准。

本文件必须保持全中文。不得在聊天、临时说明或局部注释里替代本文件定义工作范围。

## 1. 当前唯一目标

当前唯一目标是：把终端历史记录处理完整切换为以逻辑行为基本单位的模型。

完成后的系统必须满足：

- `termx-core` 拥有唯一历史真相。
- 历史的基本单位是 logical line，不是 visual row，不是 wrapped row，不是 snapshot scrollback。
- `persisted history store` 只存已封口的 logical line。
- `mutable live tail` 保存当前仍可被终端指令修改的尾部 logical line，包括 open line、sealed line、从 persisted history reclaim 回来的 sealed suffix。
- `screen projection` 只是当前宽度下的二维投影，不产生历史真相。
- `tuiv2` 只消费 core 返回的 authoritative history window，不再本地重建历史真相。
- copy mode、鼠标滚轮、page up/down、older prepend、latest replace、stale response guard 都围绕 authoritative history window 工作。

## 2. 工作范围

### 2.1 当前主线范围

允许主动修改、删除、重写、测试：

- `termx-core/`
- `termx-vterm/`
- `tuiv2/`

### 2.2 受限联动范围

只有当 core、vterm、tuiv2 契约变化确实需要时，才允许最小化触及：

- `internal/protocol/`
- `termx-proto/`
- `termx-cli/`
- `termx-shared/`
- `termx-testkit/`
- `scripts/`
- 根目录直接相关文件：`workflow.md`、`AGENTS.md`、`go.work`、`go.work.sum`、`Makefile`、必要顶层说明文档

### 2.3 冻结范围

不得主动触碰：

- `remote-ui/`
- `termx-app/`
- `web-control/`
- `termx-hub/`
- `termx-remote/`
- `bin/`
- `.claude/`
- 顶层可执行产物和测试产物
- 未在本文件列出的任何新范围

如需扩展范围，必须先修改本文件的范围表，再进入实现。

## 3. 已定死语义

### 3.1 历史真相

- primary history 的基本单位是 logical line。
- logical line 必须有稳定身份，不能只靠当前窗口内 row index 表达。
- visual rows 只是某个宽度下的投影结果。
- wrapped metadata 可以作为投影辅助信息，但不能作为最终历史真相来源。
- snapshot、grid viewport、TUI runtime scrollback 都不能作为 committed history truth。

### 3.2 三层模型

- `persisted history store` 是已封口 logical line 的存储层。
- `mutable live tail` 是当前可变真相层。
- `screen projection` 是 `mutable live tail` 在当前尺寸下的显示投影。

### 3.3 可变尾部

- `mutable live tail` 可包含 open logical line。
- `mutable live tail` 可包含 sealed logical line。
- `mutable live tail` 可包含从 persisted history reclaim 回来的 sealed suffix。
- `open/sealed` 与 `origin=live/reclaimed` 是正交属性。
- 已落盘 logical line 后续仍可能因为终端行为被 reclaim、修改、重新提交。
- 落盘、mmap、内存驻留只是 residency 策略，不是语义边界。

### 3.4 非历史创建事件

下面事件不得凭空创造 persisted history：

- attach
- reattach
- bootstrap
- recovery
- full replace
- clear screen
- resize

这些事件可以重建投影、恢复状态、切换归属，但不能把现有 screen 内容当作新历史提交。

### 3.5 resize 语义

- resize 不是历史创建事件。
- resize 不是历史重写事件。
- grow resize 必须按完整 logical line reclaim persisted history tail 的最小充分后缀。
- shrink resize 必须表达 `screen -> hidden mutable live tail`。
- 不允许只 reclaim 半条 logical line。

### 3.6 alt-screen 与进程退出

- alt-screen 不写入 primary history。
- 进入 alt-screen 时 primary history 语义冻结。
- 退出 alt-screen 时恢复 primary surface，不把 alt 内容混入 primary history。
- process exit 是显式 mutability 边界。
- process exit 时 primary mutable live tail 中剩余内容必须 force seal 后进入 persisted history。
- 如果退出时仍在 alt-screen，alt 内容直接丢弃。

### 3.7 TUI 语义

- `tuiv2` 不拥有 committed history truth。
- `tuiv2` 不得用本地 VTerm scrollback、snapshot totals、row ownership 数量、LoadedRows、hasMore、wrapped 拼接结果推断历史真相。
- `tuiv2` copy mode 只保留交互态：光标、选区、viewport top、当前 authoritative window token。
- latest window 使用 replace。
- older window 使用 prepend。
- stale response guard 使用 core 返回的 token、generation、logical line 边界，不使用本地深度计数。

## 4. 必删代码清单

本轮不保留旧实现兼容。下面列出的代码形态如果仍存在，必须删除或改写，不能继续修补。

### 4.1 core 侧必删或改写

- 任何以 `hot/cold` 作为当前实现主语义的命名、helper、测试断言。
- 任何以 visual row 作为 persisted history 主键或唯一边界的实现。
- 任何只根据 wrapped row 反推出最终 logical line truth 的存储层实现。
- 任何 resize 中只 reclaim 半条 logical line 的路径。
- 任何 attach/bootstrap/recovery/full replace/clear screen 创建新 persisted history 的路径。

### 4.2 protocol 侧必删或限制

- 任何让 client 自己判断 latest replace 或 older prepend 的 contract。
- 任何把 `snapshot` / `grid.viewport` 描述为 TUI 历史真相来源的 contract。
- 任何只传 row-level wrapped 而不传 authoritative logical line boundary 的新接口。
- 如果保留 legacy `snapshot` / `grid.viewport`，必须明确它们只是兼容投影接口，不可作为新 TUI history path。

### 4.3 tuiv2 侧必删或改写

- 本地 committed history depth 状态机。
- 本地 history loading depth 状态。
- 本地 history exhausted 状态。
- copy mode frozen snapshot 分页合并旧路径。
- runtime 本地 viewport merge / snapshotFromGridViewport 旧路径。
- canonical row ref 本地推断旧路径。
- 任何通过 `wrapped` 自行拼完整 logical line 的 copy mode history truth 路径。
- 任何用 `snapshot.ScrollbackTotal`、`ScrollbackLoadedRows`、`HistoryGeneration` 空值、row count 推断 older/latest 接纳规则的路径。
- 任何把 local VTerm scrollback 当 committed history 的滚轮或 page up/down 主路径。
- 相关旧测试语义必须删除或重写，不能作为回归基准。

## 5. 新模型必须提供的对象

### 5.1 core logical line store

core 必须显式维护 logical line 记录。每条 logical line 至少包含：

- stable logical line id
- seal 状态：open 或 sealed
- origin：live 或 reclaimed
- logical text/cell 内容或可重放 cell runs
- 当前投影所需的 row segments
- row kind
- timestamp 范围
- dirty 状态
- generation 或版本
- 与 persisted store 的 residency 信息

### 5.2 authoritative history window

core 对外返回的 history window 必须包含：

- terminal id
- window token
- op：replace 或 prepend
- size
- visual rows
- row metadata
- logical line spans
- stable logical line ids 或等价边界标识
- span 是否 clipped before / clipped after
- before cursor 或 older cursor
- loaded logical lines
- total logical lines
- has more
- generation
- first/last logical line boundary
- timestamp

### 5.3 TUI history store

`tuiv2` 需要有独立的 authoritative history window store，只保存 core 返回的窗口和交互态。它不得把窗口反写成 local runtime truth。

TUI store 至少表达：

- 当前 terminal id
- window token
- rows
- line spans
- logical line ids
- op 接纳结果
- has more
- viewport top
- cursor
- selection
- pending request token

## 6. 必做 harness

测试必须准，不能只测 happy path。以下 harness 必须逐步做出来。

### 6.1 core logical line harness

新增或收敛 core harness，用于构造终端事件序列并断言 logical line store：

- 普通输出无换行
- 普通输出带换行
- 自动折行
- 宽字符与组合字符
- 光标移动后覆写
- 清屏
- alt-screen 进入与退出
- grow resize reclaim
- shrink resize hidden live tail
- attach/bootstrap/recovery
- process exit force seal
- persisted tail reclaim 后修改再提交

断言必须覆盖：

- logical line id 稳定性
- open/sealed 状态
- origin live/reclaimed
- persisted store 内容
- mutable live tail 内容
- screen projection 内容
- generation 变化
- 不产生历史的事件确实不产生新 logical line

### 6.2 projection harness

新增 projection harness，把同一组 logical lines 投影到不同宽度，断言：

- visual row 内容
- wrapped flags
- line spans
- clipped before/after
- logical line ids
- row 到 line 的映射
- grow/shrink 前后不会创造新 history

### 6.3 protocol harness

新增 protocol round-trip 与 contract harness，断言：

- `history.window` params 编解码
- `history.window` payload 编解码
- replace/prepend op 由 core 决定
- token/generation/boundary 稳定
- clipped span 不丢失
- logical line id 不丢失
- legacy snapshot/grid viewport 不被新 TUI history path 使用

### 6.4 tuiv2 history harness

新增 tuiv2 history harness，不依赖真实 PTY，使用 fake history source 驱动：

- latest replace 初始化窗口
- older prepend 前插窗口
- stale older response 被丢弃
- token 改变后旧 response 被丢弃
- page up 触发 older 请求
- page down 不请求本地历史真相
- 鼠标滚轮进入 copy mode 后消费 authoritative window
- clipped line 不能被当作完整 line 选择
- copy selection 跨多个 logical line 正确
- resize 后 latest replace 重置窗口

### 6.5 端到端 harness

必要时补最小 e2e，但 e2e 只验证集成，不替代上述精确 harness：

- 运行产生多屏输出后滚轮上滚能看到历史
- page up/down 能移动 copy mode viewport
- resize 后历史不重复、不丢失、不凭空增加
- alt-screen 应用退出后 primary history 正常

## 7. 实施顺序

### 切片一：冻结旧路径并补准 harness

- 更新/新增 core logical line harness。
- 更新/新增 projection harness。
- 更新/新增 tuiv2 fake history source harness。
- 把旧 TUI snapshot/grid viewport history truth 测试删除或标记重写。
- 本切片可以先让部分新测试失败，但提交前必须明确失败测试对应的后续切片；如果仓库不允许失败测试提交，则先提交 harness helper 和禁用说明，再逐切片启用。

### 切片二：core 显式 logical line store

- 把 persisted history store 从 visual row 主导改为 logical line 主导。
- 给 logical line 分配 stable id。
- 把 open/sealed/origin/dirty/generation 纳入核心结构。
- 删除或改写仍以 wrapped row 作为 store truth 的代码。
- 通过 core logical line harness。

### 切片三：projection 从 logical line 生成

- 从 logical line store 生成 visual rows。
- 从 logical line store 生成 line spans。
- 不再从局部 wrapped rows 反推出最终 logical line truth。
- 支持不同 cols 下重投影。
- 支持 clipped before/after。
- 支持 row 到 logical line id 映射。
- 通过 projection harness。

### 切片四：history.window contract 完整化

- 扩展 `termx-proto` 与 `internal/protocol`，传递 logical line id 或等价边界。
- 明确 cursor/token/generation/boundary 语义。
- latest replace 与 older prepend 由 core 决定。
- legacy `snapshot` / `grid.viewport` 继续保留时，只作为兼容投影接口。
- 通过 protocol harness。

### 切片五：tuiv2 authoritative history store

- 新增 `tuiv2/historyview` store 与 source 实现。
- bridge client 暴露 `HistoryWindow`。
- app model 注入 history source/store。
- 删除 copy mode 对 frozen snapshot 分页合并的依赖。
- 删除 runtime 本地 history truth 相关入口。
- 通过 tuiv2 fake history source harness。

### 切片六：copy mode 和滚动接入

- copy mode backing 改为 authoritative history window。
- 鼠标滚轮上滚进入 copy mode 时加载 latest history window。
- page up/top 触发 older prepend。
- page down/bottom 只在已有 authoritative window 内移动，必要时 latest replace。
- selection 使用 logical line spans 和 logical line ids。
- clipped span 不当作完整 logical line。
- 删除剩余 snapshot scrollback truth 依赖。
- 通过 tuiv2 copy mode harness。

### 切片七：端到端收口

- 补最小 e2e。
- 跑 core、protocol、tuiv2 相关测试。
- 更新文档。
- 删除不再使用的旧 helper、旧测试 fixture、旧字段。

## 8. 测试准入

每个有效切片提交前至少运行与切片相关的测试：

- core 改动：`go test ./termx-core/ -count=1`
- protocol 改动：`go test ./internal/protocol/ ./termx-proto/... -count=1`
- tuiv2 改动：`go test ./tuiv2/... -count=1`
- 跨层改动：运行以上全部相关测试

如果存在已知偶发测试，必须记录具体测试名、失败现象、重跑结果，不得把真实语义失败当偶发。

## 9. 提交规则

- 当前工作区未提交改动必须先整理并提交，再继续下一切片。
- 每个有效切片必须形成中文提交。
- 不允许长期堆积未提交改动。
- 删除旧代码必须和对应新语义或 harness 同切片提交。
- 如果某切片扩大范围，必须先更新本文件。

## 10. 当前状态

- 已有 core `HistoryWindow` 初版。
- 已有 protocol `history.window` 初版。
- 已有 `HistoryLineSpan` clipped before/after 初版。
- 已扩展 protocol `history.window` line spans，新增 `LogicalLineID` 往返 harness；当前 core persisted projection 已提供非零 logical line boundary id，不再由当前窗口内 visual row 下标推断，但完整显式 logical line store 仍未落地。
- 已新增 `tuiv2/historyview` authoritative window store 初版与 fake source harness，覆盖 latest replace、older prepend、stale token/generation 丢弃、边界重叠拒绝、clipped span 保留、pending request token、viewport top、cursor 与 selection 交互态。
- 已新增 `tuiv2/historyview` protocol-backed source adapter，将 `snapshot` 仅用于 live surface 投影，将 `history.window` 映射为 authoritative window，并要求 older 请求显式携带 core 返回的 before cursor。
- 已在 `tuiv2/bridge` 暴露 protocol `HistoryWindow` 入口并补齐测试 fake client。
- 已把 `tuiv2/historyview` store/source 注入 `tuiv2/app` model 构造路径；当前只是持有 authoritative history 依赖，尚未接入 copy mode 与滚动行为。
- 已修正 core `HistoryWindow.BeforeOffset` 响应语义：返回窗口后可继续请求 older window 的 before cursor，而不是简单回显本次请求 offset。
- 已在 `tuiv2/app` 新增 authoritative history window 加载消息路径，支持 latest replace 请求、older prepend 请求、pending token 记录、错误清理与通过 `historyview.Store` 接纳窗口；当前尚未由 copy mode 与滚动行为触发。
- 已让 `tuiv2/app` copy mode buffer 在已有 authoritative history window 时优先使用 `historyview.Store`，并以 `HistoryWindow.Lines` 作为逻辑行边界；render 已改为直接消费 render-native authoritative projection，不再通过 `snapshotFromHistoryWindow` 把权威窗口伪装成 `protocol.Snapshot`。
- 已将 `tuiv2/app` 鼠标滚轮进入 copy mode 接到 authoritative history window latest replace 请求，并将 copy mode page up / half page up / top 的顶部边界接到 older prepend 请求。
- 已将 `tuiv2/app` copy mode 连续鼠标上滚的顶部边界接到 authoritative history window older prepend 请求；只有移动前已经处于 authoritative window 顶部时才触发 older，不用本地深度或 snapshot totals 推断。
- 已补齐 `tuiv2/app` copy mode page down / bottom 的 authoritative window harness，固定为只在当前 authoritative window 内移动并且不触发 older、本地 scrollback 或 snapshot totals 推断；如果后续需要 latest replace，必须基于明确 stale 或底部刷新信号实现。
- 已将 `tuiv2/app` copy mode selection 接到 authoritative line span 的 logical line id 与 clipped before/after：相邻 clipped segment 只有在 logical line id 相同且 clippedAfter/clippedBefore 连续时才作为同一逻辑行拼接，否则继续保留真实逻辑行换行。
- 已删除 `tuiv2/app` 中旧 snapshot 分页加载后用于调整 copy mode 的空实现、旧 frozen snapshot scrollback trim helper 与未使用 reanchor helper；`SnapshotLoadedMsg` 不再进入本地 copy mode history truth 调整路径。
- 已将 `tuiv2/app` 语义层 `scroll-up/scroll-down` 从本地 pane viewport offset 历史路径移出：scroll-up 进入 copy mode 并请求 authoritative latest window，scroll-down 在非 copy mode 下不再改本地 viewport，copy mode 内只移动 authoritative buffer 游标。
- 已删除 `tuiv2/app` 鼠标滚轮路径中最后的本地 pane viewport fallback；wheel-up 仍进入 copy mode 并请求 authoritative latest window，wheel-down 在非 copy mode 下不再改本地 viewport。
- 已删除 `tuiv2/app` copy mode state 的 frozen snapshot backing：进入 copy mode 只创建 pane/terminal 交互态并请求 authoritative latest window，history window replace 到达后绑定 token、光标与 viewport；copy mode buffer 只接受带 line spans 的 authoritative window，并删除旧 wrapped-row logical line helper。
- 已将 `tuiv2/app` 鼠标滚轮进入 copy mode 的剩余 `local scrollback` 策略命名收敛为 copy mode authoritative entry，并把 copy mode 测试 fixture 中的 snapshot 命名改为普通 terminal live surface fixture，删除手动跳过的旧本地 scrollback frame 审计基准。
- 已删除 `tuiv2/app` 语义 scroll-up 进入 copy mode 后残留的本地 pane viewport offset 清零操作，固定该入口只请求 authoritative latest window，不再触碰本地 viewport 历史状态。
- 已删除 `tuiv2/render` copy-mode VM 的 snapshot fallback 字段与 wrapped snapshot 推 logical line helper，render copy mode 只能通过 render-native authoritative projection 绘制。
- 已将 `tuiv2/app` 测试 fake 的 `GridViewport` 从 snapshot metadata 合成路径改为仅记录调用并返回空结果，避免测试继续把 snapshot totals、row ownership 或 wrapped 元数据伪装成 TUI history path。
- 已将 `tuiv2/app` 测试用 snapshot window fixture 从 copy mode helper 中剥离为 legacy protocol snapshot window fixture，并改写重启端到端测试中的兼容 snapshot 文案，避免把 legacy snapshot 误读为新的 TUI history truth。
- 已将 `tuiv2/runtime` 测试 fake 的 `GridViewport` 改为返回空结果，并删除未使用的 runtime grid viewport trace helper；TUI 测试层不再提供 grid viewport 历史数据占位。
- 已将 core authoritative `history.window` 从 legacy protocol `grid.viewport` 派生路径改为直接消费内部 grid viewport，并让 persisted store projection 为每条投影 row 携带非零 logical line boundary id；`HistoryLineSpan.LogicalLineID` 不再由当前窗口内 visual row 下标推断。
- 已在 core persisted history store 中新增内部 logical line record 视图，表达非零 logical line boundary id、start/end committed row、sealed 状态与 persisted origin；`LogicalLineCount` 与 logical-line retention 已开始消费该 record 视图。
- 已将 core persisted projection 的 row 到 logical line id 映射改为直接来自 logical line record 视图，`reflowTerminalGridRows` 不再根据窗口首 row 自行推断 logical line id。
- 已将 core persisted viewport/reclaim 的窗口起点扩展改为通过 logical line record 视图定位逻辑行边界，不再使用逐 row wrapped 回退 helper 作为窗口边界来源。
- 已删除 core persisted viewport/reclaim 中只接受 refs/wrapped 的旧窗口起点 helper；窗口起点调整现在只消费显式 logical line record，refs/wrapped 仅作为 metadata 缺失时生成 fallback record 的输入。
- 已将 core 中从 refs/wrapped 生成 persisted logical line record 的 helper 命名收敛为 fallback 语义；该路径只表示 metadata 缺失或损坏时的降级推导，不再作为正常 persisted store truth 命名。
- 已将 core 中基于 row wrapped flag 判断 continuation 的 helper 也收敛为 fallback 命名，并限制在 fallback logical line record 推导内部使用。
- 已将 core persisted retention 的 logical-line limit、byte limit、age limit 收敛为优先消费 persisted logical line record，并按完整 logical line record 保留或丢弃；metadata 缺失时才回退 index/wrapped 推导，byte limit 不再切半条逻辑行。
- 已为 core `mutable live tail` 新增内部 logical line record 视图，覆盖 reclaimed/live/resize segment 的 start/end、seal 状态、origin 与 logical line id；grow resize reclaim 会把 persisted projection 的 row 到 logical line id 映射带入 reclaimed live tail，combined viewport 与 history window 不再把 reclaimed suffix 的 logical line id 清零，live/resize open line 也已有 runtime stable logical line id。
- 已为 core `mutable live tail` 的 live/resize segment 分配 runtime stable logical line id，并在连续 live tail replacement、wrapped open line、resize hidden live tail 与 latest history window 中保留同一逻辑行 id；reclaimed suffix 继续使用 persisted logical line id。当前 runtime id 与 persisted id 的统一迁移仍只是 metadata 记录，尚未被完整消费为跨层统一身份。
- 已将 core `mutable live tail` logical line record 分段改为优先按 stable logical line id；只有 id 缺失时才回退 wrapped 元数据，避免 live tail record view 继续把 wrapped 当作首要边界来源。
- 已新增 core runtime live logical line id 到 persisted logical line id 的迁移记录：当 open live tail 被 hard newline seal 并写入 persisted store 时，会记录 runtime id 对应的新 persisted id，并写入 `grid.lines.json`；迁移关系必须是 runtime id 到 persisted id。
- 已将 core runtime live logical line id 到 persisted logical line id 的迁移记录分组改为优先按传入的 stable logical line id；同一 runtime id 的连续 rows 即使 wrapped=false，也只迁移到该逻辑行起始 persisted id。
- 已在 persisted logical line record metadata 恢复时消费 `grid.lines.json` 中的 runtime->persisted logical line id 迁移关系，避免已迁移的 runtime live id 在 persisted projection 中继续泄漏；命名空间不匹配的迁移会被忽略。
- 已为 core persisted store record 与 mutable live tail record 增加显式 residency 字段，区分 persisted 与 live-tail 层，并写入 `grid.lines.json`。
- 已为 core persisted store record 与 mutable live tail record 增加 dirty/generation 字段：persisted record 默认为 dirty=false 并携带 store generation，live/resize record 为 dirty=true，reclaimed live-tail record 为 dirty=false 并保留 reclaimed generation，并写入 `grid.lines.json`。
- 已新增 core grid line metadata sidecar，持久化 runtime live logical line id 到 persisted logical line id 的迁移关系，以及 persisted/live-tail records 的 residency、dirty、generation。
- 已将 persisted logical line record 的 residency、dirty、generation 写入 core grid line metadata sidecar。
- 已将 mutable live tail record 的 residency、dirty、generation 写入 core grid line metadata sidecar，并开始随 live row payload 一起作为恢复输入；当前恢复入口限定在 `history.window` latest store-only projection。
- 已让 core persisted store 在 replay/viewport/reclaim 路径优先使用 `grid.lines.json` 中完整有效的 persisted logical line record 恢复 logical line 边界、logical line id 与 logical total；metadata 缺失、损坏或与当前 index 不匹配时继续回退到 index/wrapped 推导。当前恢复范围覆盖 persisted screen projection。
- 已收紧 core persisted logical line record metadata 恢复校验：persisted record 必须是 sealed、clean、归一后的 persisted/reclaimed origin、persisted id namespace、persisted residency、连续覆盖当前 index 且 generation 匹配，并且不能携带 row 坐标；未封口 record、未迁移的 runtime id、live/resize origin、dirty、未知 origin 或 row 坐标 sidecar 会被视为损坏并回退。
- 已继续收紧 core persisted logical line record metadata 恢复校验：当当前 store generation 非零时，persisted record 必须携带完全匹配的 generation；generation=0 不再被当作可消费的 persisted sidecar truth。
- 已收紧 core persisted logical line record metadata 写出：只有 sealed persisted logical line records 才写入 `grid.lines.json`，尾部未封口 wrapped prefix 不再伪装成 persisted sidecar truth；如果 index 中存在 sealed prefix 加未封口尾部，只写 sealed prefix，恢复端仍必须完整覆盖当前 index 才消费为 persisted screen projection truth。
- 已让 core persisted retention 实际丢弃 rows 后立即刷新 `grid.lines.json` 中的 persisted logical line records，使 sidecar 在运行期跟随当前 index、base row id 与 generation 更新，不再只等 `Close()` 写出。
- 已让 core persisted append 完成后立即刷新 sealed-only persisted logical line records sidecar；正常运行期追加已封口 rows 后不再等 `Close()` 才生成 persisted logical line metadata。
- 已让 core persisted append 通道携带逐 row stable logical line id，并在写出 `grid.lines.json` 时把 runtime live logical line id 转为 persisted logical line id；append、retention 与 close 会优先保留这些显式 persisted records，不再在有显式 id 的正常路径立刻退回 index/wrapped fallback。
- 已让 core persisted explicit append 在旧 sidecar 缺失或前缀 metadata 不可用时，只对 append 前的既有前缀使用 index/wrapped fallback 重建；新追加的 sealed 后缀仍按 stable logical line id 写入 persisted records。
- 已让 core persisted explicit append 在校验旧 metadata 前缀时先消费 runtime->persisted logical line id 迁移，避免已迁移的显式前缀被误降级为 index/wrapped fallback。
- 已收紧 core persisted explicit append 的 logical line id 校验：同一 stable logical line id 只能形成连续 record，非连续重复 id 不再写入 persisted sidecar metadata。
- 已继续收紧 core persisted explicit append 的 logical line id 覆盖校验：同一追加 logical line record 内如果混入显式 stable id 与缺失 id，会拒绝该显式 metadata 并刷新为 persisted fallback records，不再把缺失 row 继承进伪权威 stable line。
- 已将 core persisted explicit append 的异常处理收敛为“rows append 成功优先”：显式 metadata 校验失败时刷新为 persisted fallback records，不再让已经落盘的 row append 对调用方表现为失败。
- 已让 core persisted viewport 在请求窗口完全落入 sealed persisted metadata prefix 时消费该 prefix 的 logical line id；窗口触及未封口尾部时仍不把 partial sidecar 当完整 persisted truth。
- 已将 mutable live tail row payload 随 `grid.lines.json` 的 live record 一起写入 sidecar，并在 terminal 不在内存时让 core `history.window` latest projection 从该 sidecar 恢复 mutable live tail rows、ownership 与 runtime logical line id；older offset 仍保持 persisted-only。当前恢复范围覆盖 `history.window` store-only latest projection，legacy snapshot/grid viewport 仍只是兼容投影接口。
- 已收紧 mutable live tail metadata 恢复校验：`live_records` 的 origin 必须是 live/reclaimed/resize 之一，且 dirty 必须与 origin 一致，live/resize 为 dirty 且使用 runtime logical line id，reclaimed 为 clean、sealed 且使用 persisted logical line id；未知 origin、dirty/origin 不一致、id namespace 不匹配或 open reclaimed record 会被视为损坏 metadata 并忽略恢复。
- 已继续收紧 mutable live tail metadata 恢复校验：如果 live/resize record 使用的 runtime logical line id 已存在于 runtime->persisted migration 中，说明该 runtime id 已完成落盘迁移，恢复端会拒绝这份 live-tail metadata，避免已提交逻辑行重新作为 mutable live tail 暴露。
- 已将 mutable live tail metadata 恢复的 record 内部边界校验收敛到 stable logical line id 与 seal 状态；同一 live record 覆盖多行时不再要求中间 visual row 带 wrapped continuation，wrapped 只用于表达末行 open/wrap pending 投影状态。
- 已收紧 mutable live tail metadata 写出校验：只有完整覆盖 live rows、id namespace 与 origin 匹配、residency/dirty/seal 状态一致的 live records 才写入 `grid.lines.json`；无效或不完整 live-tail record 会清空 recoverable live metadata，避免写出不可恢复伪真相。
- 已将 reclaimed mutable live tail record 的 persisted row 坐标显式写入 `grid.lines.json`，恢复时必须使用 sidecar 中的 first/last row id；row 坐标写出和恢复都只允许出现在已知 persisted logical line id 的 reclaimed record 上，不再通过 logical line id 反推出 reclaimed row 坐标。
- 已让 legacy `snapshot` / `grid.viewport` 的 store-only latest 兼容投影复用同一个 recovered live tail projection helper；它们仍不是新 TUI history truth，只是避免恢复场景下兼容投影丢失 mutable live tail。
- 已在 process exit force seal 后清空 recoverable mutable live tail metadata，避免已提交到 persisted store 的 live tail 又被 store-only latest projection 重复恢复；sidecar 写入同时跳过 closed/remove-on-close 临时 store。
- 已让 process exit force seal 在提交 primary mutable live tail 与剩余 screen projection 行时保留或分配 runtime logical line id；封口后的 persisted store append 会按实际落盘位置写出 runtime->persisted logical line id 迁移，并清空 recoverable live-tail metadata。
- 已补齐 process exit force seal harness，固定隐藏 mutable live tail 前缀与当前 screen continuation 行必须作为同一条 persisted logical line 提交，并只产生一条 runtime->persisted logical line id 迁移。
- 已将 process exit force seal 的 screen-only 行 runtime id 分配接入 mutable live tail 单调分配游标，避免此前已迁移的 runtime id 在退出封口时被 screen projection 行复用并覆盖迁移记录。
- 已将 restart preserved rows 的提交路径改为保留 `primaryLiveTailRowsForExit` 生成的 logical line id，并由 persisted store append 按实际落盘位置记录 runtime->persisted migration；restart marker 作为独立 sealed persisted record 写入，避免重启保留行通过无 id wrapped fallback 生成 sidecar。
- 已让 restart preserved rows 成功提交到 persisted store 后同步清空 mutable live tail 内存状态与 `grid.lines.json` 中的 recoverable live-tail metadata，避免重启保留行同时作为 persisted history 与 recovered live tail 暴露。
- 已将 process exit / restart preserved rows 中 screen-only 行的 runtime logical line id 分配改为推进 primary mutable live tail 的单调游标，避免封口迁移后下一条 live line 复用已迁移 runtime id。
- 已将 runtime live logical line id 到 persisted logical line id 的迁移写入下沉到 persisted store append 完成后按实际 append start 与最终 persisted records 生成；异步 grid appender 不再由 Terminal 按旧 row count 预估迁移目标。
- 已让生产写入路径中的普通 persisted append rows 在进入 persisted store 前先通过 mutable live tail 单调游标补齐 runtime logical line id；store append 再按实际落盘位置写出 runtime->persisted migration，避免新提交行以全零 id 直接落入 fallback metadata。
- 已让 mutable live tail 从 `grid.lines.json` 恢复时同时吸收 runtime->persisted migrations 中的最大 runtime id，避免恢复后的 live tail 分配复用已迁移 runtime logical line id。
- 已让 persisted line records sidecar 刷新时同步裁剪 runtime->persisted migrations，只保留仍指向当前 retained persisted logical line record 的迁移，避免 retention 后残留已丢弃 logical line 的 runtime migration。
- 已将 mutable live tail 的旧通用 replaceRows 入口收敛到新语义：live/resize segment 创建时即写入 runtime logical line id，不再先生成无 id segment 后依赖读取端补推。
- 已将 mutable live tail 的 live/resize segment ID 生成收敛为完整显式 stable logical line id 优先；当调用方已提供每 row logical line id 时，不再用 wrapped 元数据重新拆分或合并这些边界。
- 已将 mutable live tail 的 runtime logical line id 分配改为 tail 生命周期内单调推进；hard newline seal、process exit seal 或 reset 清空尾部内容后，后续独立 live logical line 不复用旧 runtime id，`grid.lines.json` 恢复 live tail 时也会从已恢复的最大 runtime id 继续分配。
- 已修正 reclaimed prefix 覆盖 resize live-tail 前缀后的裁剪逻辑：resize segment 裁剪 rows 时同步裁剪并保留 runtime logical line id，避免剩余 mutable live tail 回退成无 id 的 wrapped 推导。
- 已将 reclaimed prefix 裁剪 resize live-tail 后的缺失 runtime id 补齐也接入 mutable live tail 单调分配游标，避免裁剪后的 resize tail 重新使用固定基准 runtime id。
- 已收紧 mutable live tail record 生成：只有每个 live-tail row 都带 stable logical line id 时才生成 recoverable line records，缺 id 的 segment 不再通过 wrapped 元数据拼出可写 sidecar record。
- 已将 mutable live tail record view 收敛为全尾部连续语义：任一 segment 缺少完整 stable logical line id 时不返回 partial records，避免上层误消费非连续 recoverable metadata。
- 已收紧 mutable live tail window 投影的 logical line id 输出：segment 内存在 partial id 时不再把半条 id 暴露给 authoritative projection，而是整体压成无 id，避免伪造逻辑行边界。
- 已收紧 mutable live tail window 投影与 record metadata 的 logical line id 命名空间：live/resize 只能使用 runtime logical line id，reclaimed 只能使用 persisted logical line id；命名空间错误的 id 会被抑制，不暴露为权威边界。
- 已将 core latest history window 的 live tail 逻辑行总量改为按投影中的 stable logical line id 分组统计，不再依赖 recoverable live-tail records；metadata 全尾部收敛策略不会抹掉其他已有稳定 id 的投影逻辑行。
- 已将 core latest combined viewport 的 live-tail 起点扩展收敛为只按相邻非零 stable logical line id；缺少 logical line id 时不再通过 wrapped 元数据扩展 authoritative window 行集合。
- 已修正 core latest combined viewport 中 reclaimed mutable live tail 的 committed depth 计算：visible persisted prefix 与 reclaimed suffix 只各计一次，latest 返回的 older cursor 不再把 store viewport offset depth 与 reclaimed suffix 重复相加，older prepend 也不会重复返回已随 latest 投影暴露的 reclaimed rows。
- 已补齐 store-only recovered reclaimed mutable live tail 的 committed depth harness，固定重启恢复后 latest older cursor 同样只计 recovered reclaimed suffix 一次，older prepend 不重复返回已恢复到 live tail 的 persisted suffix。
- 已将 core `history.window` line span 生成改为只按 stable logical line id 归并 visual rows；projection 缺失 logical line id 时不再通过 wrapped 元数据伪造 authoritative line spans。
- 已将 core authoritative `history.window` 输出收敛为只包含带 stable logical line id 的 rows；screen/grid 兼容投影仍可保留缺 id 的 visual rows，但 history window 不再返回无法生成 authoritative line span 的 row-only 片段，避免 TUI authoritative store 拒绝 core 响应。
- 已将 core authoritative `history.window` 的全过滤空窗口边界收敛为无权威边界：当窗口内所有 visual rows 都缺 stable logical line id 被过滤后，会清空 token、generation、row id 与 logical total，只保留本次请求 before cursor，避免把无 authoritative row 的窗口暴露成可接纳历史边界。
- 已修正 core authoritative `history.window` 过滤缺 stable logical line id rows 后的 logical total：非权威 committed 片段会按独立缺 id 片段从 logical total / window logical total 扣除，并避免与 discontiguous stable id 过滤重复扣减。
- 已继续收紧 core authoritative `history.window` 行过滤：如果同一个 stable logical line id 在投影窗口内非连续出现，中间被缺 id 或其他 id 打断，则该 id 的所有 visual rows 都会从 authoritative window 中抑制，并同步从 authoritative logical total 中扣除，避免过滤后把不连续片段重新合并成伪完整逻辑行。
- 已收紧 core `history.window` 的 clipped-before 标记：older offset 本身不再表示首个 logical line span 被裁断，`historyLineSpans` 不再接收 offset 作为裁剪输入，只有 screen projection 明确给出 logical line 裁剪标记时才设置 clipped-before，避免完整 older prepend 窗口被少计 loaded logical line。
- 已将 core screen projection 裁剪后的 clipped-before 判定收敛为只看相邻 stable logical line id；缺少 logical line id 时不再通过 wrapped 元数据推断窗口切断了逻辑行。
- 已将 core grow resize reclaim 的投影裁剪起点收敛为只按 stable logical line id 向前扩展；缺少 logical line id 时不再通过 wrapped 元数据扩展 reclaim 窗口。
- 已将 core screen projection 裁剪后的 leading row kind 继承收敛为只在相邻 stable logical line id 连续时发生；缺少 logical line id 时不再通过 wrapped 元数据继承 clipped span 的 row kind。
- 已将 reclaimed mutable live tail 的 logical line id 输出收敛为只接受显式 persisted logical line id；即使带有 reclaimed row 坐标，也不再用 row id 与 wrapped 元数据 fallback 生成 authoritative logical line boundary。
- 已收紧 reclaimed mutable live tail metadata 的 generation 约束：写入和恢复都必须携带非零 reclaimed generation，并且恢复时必须与当前 persisted store generation 匹配。
- 已删除或改写 `tuiv2/app` 中旧滚动测试语义：不再保留依赖 snapshot scrollback wrapped、frozen snapshot 本地游标、正常模式本地 pane viewport offset 的 skipped 回归基准。
- 已新增 core projection harness，覆盖 persisted logical line 在不同宽度下重投影、wrapped flags、logical total、persisted ownership、clipped-before history window span 与非零 logical line boundary id，并修复 clipped 投影片段 row kind 继承。
- 已新增 core 宽字符与组合字符 logical line harness，覆盖 exact-width open line 不提前落盘、hard newline seal 为单条 persisted logical line、宽字符 continuation placeholder、组合字符规范化与按 cell width 重投影。
- 已新增 core 光标回到当前 visual row 后覆写的生产路径 harness，覆盖覆写仍停留在 mutable live tail、未 seal 前不产生 persisted logical line、hard newline 后作为一条 overwritten logical line 提交。
- 已新增 core alt-screen 非退出路径 harness，覆盖进入 alt-screen 后 primary persisted history/logical line 计数冻结、alt surface 不混入 primary history、退出 alt-screen 后 primary surface/viewport/replay 恢复。
- 已将 `tuiv2/app` 旧滚动测试基准收敛为 authoritative history window harness：snapshot scrollback wrapped guard、鼠标滚轮本地 snapshot 游标、copy mode page/halfpage/top/exit frozen snapshot、本地 pane viewport scroll up/down 均已删除或改写，不再作为新模型回归基准。
- 已修正 `tuiv2/historyview` protocol source 对 clipped-before-only history window 的映射：core 返回 `LoadedLines=0`、`FirstLineID=0`、`LastLineID=0` 时不再由 line span 或 row id 本地重建已加载逻辑行边界。
- 已收紧 `tuiv2/historyview` protocol source 的 line id 映射：`FirstLineID` / `LastLineID` 只来自 core 显式字段或 authoritative line spans，不再用 `FirstRowID` / `LastRowID` 伪造逻辑行边界。
- 已修正 `tuiv2/historyview` authoritative store 的 older prepend 合并计数：`LoadedLines` 只累计实际包含起点的逻辑行，clipped-before 片段不再被按 span 数当作新加载逻辑行。
- 已将 `tuiv2/render` copy mode projection 的逻辑行定位收敛到 authoritative line spans；projection 缺少 `Lines` 时不再根据 wrapped rows 反推出逻辑行数量、行号或 logical position。
- 已收紧 `tuiv2/historyview` authoritative store：带 visual rows 的 history window 必须同时带 authoritative line spans；缺少 `Lines` 的 row-only window 会被拒绝，避免 TUI store 保存无法支撑 copy mode logical-line 边界的伪权威窗口。
- 已继续收紧 `tuiv2/historyview` authoritative store：line spans 必须连续覆盖窗口内所有 visual rows，存在缺口、重叠或越界的窗口会被拒绝，避免 TUI 保存部分 authoritative、部分无 logical-line 边界的混合窗口。
- 已继续收紧 `tuiv2/historyview` authoritative store：带 visual rows 的 line span 必须携带非零 stable logical line id，避免 TUI 保存只有 row 覆盖、没有 logical-line 身份的伪权威窗口。
- 已继续收紧 `tuiv2/historyview` authoritative store：同一 stable logical line id 不能在窗口内非连续重复出现，避免 TUI 合并出不连续逻辑行片段。
- 已继续收紧 `tuiv2/historyview` authoritative store：older prepend 与 current window 合并后的窗口也必须重新通过 line span 覆盖和 logical line id 连续性校验，避免两个单独合法响应合并出伪权威窗口。
- 已为 core persisted logical line record 增加显式来源字段，区分 explicit 与 fallback；`history.window` 只消费 explicit authoritative logical line id，fallback/index/wrapped 推导继续只服务 legacy `snapshot` / `grid.viewport` 兼容投影。
- 已为临时和运行期 persisted store 增加内存态 explicit logical line record 与 runtime->persisted migration，覆盖 remove-on-close 临时 store 不写 sidecar 的场景；process exit 与 restart preserved rows 不再通过无 id append path 丢失 logical line id。
- 已将 process exit force seal 调整为先 flush persisted appender 再计算封口 rows，并让 screen continuation 继承已落盘未封口 persisted logical line id，避免退出封口把同一逻辑行拆成 fallback/non-authoritative rows。
- 已继续收紧 core screen projection 裁剪与 grow reclaim 边界：fallback/non-authoritative logical line id 不再参与 clipped-before、row kind 继承或 reclaim 起点扩展，只允许 explicit authoritative logical line id 表达这些逻辑行边界。
- 已继续收紧 core authoritative `history.window` 过滤后的 logical total 计算：被过滤的 fallback/non-authoritative committed rows 会按原始 logical line id 连续分组扣减，避免相邻多条 fallback 逻辑行被压成一个缺失 id 段后少扣。
- 已将 core `history.window` 的全局 logical total 来源收敛为 authoritative persisted logical line count 加 stable live tail count；store 级 `LogicalLineCount()` 可继续作为 legacy 投影兼容计数，但不再让窗口外 fallback/index/wrapped 推导污染 authoritative total。
- 已将 core `history.window` token 的 logical line boundary 收敛为只使用 authoritative logical line id；fallback/non-authoritative id 不再进入 token，避免 legacy 投影推导影响 stale response guard 边界。
- 已继续收紧 core latest `history.window` 的分页信号：当 latest window 已覆盖全部 authoritative logical lines 时，窗口外 fallback-only prefix 不再通过 `HasMore` 或 `TotalRows` 暴露为可继续请求的 older history。
- 已继续收紧 core `history.window` 过滤后的 row boundary：过滤 fallback/non-authoritative rows 后，`FirstRowID` / `LastRowID` 会重算为实际保留的 authoritative row 范围，不再保留 raw legacy viewport 的被过滤边界。
- 已将 core screen projection 的内部 row boundary 元数据改为随每个投影 row 携带真实 persisted row id 区间；过滤 fallback/non-authoritative rows 后按保留投影 row 的真实区间重算 `FirstRowID` / `LastRowID`，不再用 visual row index 推导，覆盖单个 persisted row 重投影成多行的场景。
- 已继续收紧 core `history.window` 最终 limit 裁剪后的 row boundary：latest replace 在输出阶段裁掉前缀投影 row 后会按保留 row 的真实 persisted row id 区间刷新 `FirstRowID` / `LastRowID`；reclaim prefix 裁剪同样同步刷新边界，避免 token 继续包含已丢弃的 raw row。
- 已将 core latest `history.window` 的 combined viewport 与 legacy `grid.viewport` 兼容投影分流：缺少显式 persisted logical line id 的 reclaimed mutable live tail 仍可用 row 坐标服务 legacy 投影去重，但不会遮蔽 authoritative history window 中 persisted store 的权威 logical line rows，也不会作为已加载 committed depth 扣减。
- 已收紧 core 非 resize full replace 语义：`broad_direct_cell_damage` 等 screen full replace 事件即使携带 `ScrollbackAppend`，也只进入 mutable live tail 投影，不写入 persisted history store，不凭空创建 committed logical line；resize full replace 仍按 resize 语义处理 hidden live tail。
- 已补齐 core 非 resize full replace 重启恢复 harness，并修正 live tail runtime logical line id 分配游标：Terminal 写入新 mutable live tail 前会吸收 grid sidecar 中已迁移或可恢复 live record 的最大 runtime id，避免恢复端因 runtime id 复用把 full replace live tail 误判为已提交逻辑行而丢弃；恢复后 latest `history.window` 仍只把 full replace 行作为 mutable live tail 投影，older cursor 不重复返回它。
- 已补齐 core persisted tail reclaim 后修改再提交 harness，并修正 process exit seal 语义：clean reclaimed suffix 不会被重复 append；如果 reclaimed suffix 已转成 dirty live tail 后再提交，会先从 persisted store 裁掉对应 tail rows，再把修改后的 live rows 作为同一 logical line 身份重新落盘，避免旧内容和新内容同时存在。
- 已继续收紧 core persisted tail 替换提交的版本语义：裁掉 reclaimed persisted tail 并写回修改内容时会 bump persisted store generation，刷新 `grid.meta` 与 persisted logical line metadata，使 `history.window` token/generation 能反映同一 logical line 边界内的内容变化，旧窗口响应可被 stale guard 识别。
- 已继续收紧 core process exit seal 的 reclaimed/live 分流：clean reclaimed suffix 后追加独立 dirty live tail 时不会裁掉 persisted suffix，dirty live rows 只有通过显式 reclaimed-tail replacement 标记才会替换 persisted tail；退出封口前只避让已迁移 runtime logical line id，不把当前 recoverable live-tail record 误当成冲突。
- 已修正 core 生产 resize 的 grow reclaim 顺序：`Terminal.Resize` 在更新 `t.size` 前会把目标 rows 显式传入 reclaimed suffix 计算，避免 grow resize 继续按旧窗口高度判断 `needed` 为 0 而漏回收 persisted logical line suffix。
- 已补齐 core process exit seal 的 reclaimed open tail + screen continuation harness：clean reclaimed open persisted suffix 退出封口时不重新追加 reclaimed rows，当前 screen continuation 会继承 reclaimed persisted logical line id 并作为同一 logical line 落盘。
- 已补齐 core resize full-replace 生产路径的 recoverable hidden live tail harness：shrink/resize 产生的 hidden mutable live tail 会以 `origin=resize`、dirty runtime logical line id 写入 `grid.lines.json`，重开 store 后 latest history projection 仍只作为 mutable live tail 暴露，不产生 committed depth。
- 已修正 core clear screen 对 mutable live tail 的边界处理：primary 全屏 clear 不创建 persisted history，但会清空当前 mutable live tail 与 `grid.lines.json` 中的 recoverable live-tail metadata，避免清屏前 hidden live tail 在重启恢复后重新出现。
- 已补齐 core alt-screen 冻结 primary mutable live tail 的 harness：进入/退出 alt-screen 不会把 alt 内容混入 primary persisted history，也不会替换进入前已有的 primary mutable live tail；primary latest authoritative projection 仍只暴露 primary live tail。
- 已补齐 core server 级 recovered reclaimed mutable live tail harness：重启后 `HistoryWindow` latest 只暴露一次 recovered reclaimed suffix，`BeforeOffset` 按 committed depth 推进，older prepend 不重复返回已由 latest 暴露的 reclaimed persisted rows。
- 已修正 core `history.window` 空 older 响应的 cursor 语义：空 authoritative window 不生成 token、row boundary 或 logical line，但会保留请求的 `BeforeOffset`/`LoadedRows`，避免 recovered mutable live tail 场景下 exhausted older 响应把分页 cursor 错误回退到 latest。
- 已继续收紧 core `history.window` 空 rows 响应：即使内部 viewport 携带 advisory `TotalRows`、generation 或 row boundary，只要没有 authoritative rows，对外窗口也只保留 exhausted cursor，不暴露 token、row boundary 或 logical total。
- 已补齐 core server 级 recovered resize mutable live tail harness：重启后 `HistoryWindow` latest 会把 `origin=resize` 的可恢复尾部作为 dirty runtime mutable live tail 暴露，不产生 committed depth；older 请求不会把 resize tail 当 persisted history 返回。
- 已补齐 core latest mutable live tail limit harness：latest replace 裁掉较旧 mutable live line 时只返回当前窗口内的 authoritative mutable 行，`LogicalTotal` 仍覆盖完整 live tail logical lines，但不生成 committed `BeforeOffset`、generation 或 row boundary。
- 已补齐 core latest mutable live tail 同一逻辑行 limit 裁剪 harness：latest replace 裁掉同一 runtime stable logical line 的前半段时会标记 `clipped-before`、`LoadedLines=0`、保留 `HasMore=true`，但不生成 committed cursor、generation、row boundary 或 loaded line boundary。
- 已补齐 core store-only recovered mutable live tail 行内 limit 裁剪 harness：从 `grid.lines.json` 恢复的 live tail 经 latest history projection 被同一逻辑行裁剪时，与内存态一致保留 `clipped-before` 和 `HasMore=true`，且不生成 committed 边界。
- 已补齐 core reclaimed mutable live tail 行内 limit 裁剪 harness：latest replace 裁掉 reclaimed persisted logical line 的前半段时只按已实际返回的 reclaimed committed row 推进 cursor，older 请求会补回未暴露的 persisted 前缀且不重复最新尾行。
- 已补齐 core store-only recovered reclaimed mutable live tail 行内 limit 裁剪 harness：从 `grid.lines.json` 恢复的 reclaimed suffix 被 latest 行内裁剪时，与内存态一致只按已返回 reclaimed row 推进 cursor，并让 older 补回未暴露前缀。
- 已补齐 core store-only recovered resize mutable live tail 行内 limit 裁剪 harness：从 `grid.lines.json` 恢复的 resize tail 被 latest 行内裁剪时，与 live origin 一致只暴露 dirty runtime mutable 投影，不生成 committed cursor、generation 或 row boundary。
- 已补齐 core persisted source row 重投影后 latest limit 裁剪 harness：单条 persisted row 在窄宽度下投影为多条 visual row 后被裁剪时，row boundary 保持真实 source row id，cursor 不因裁掉同一 source row 的前缀投影片段而回退，并保留 projection `TotalRows` 与 `HasMore`。
- 已补齐 core persisted source row 重投影后 older limit 裁剪 harness：older prepend 在跳过最新 source row 后裁进前一条 source row 的投影尾段时，row boundary 仍保持真实 source row id，cursor 按 committed source row depth 推进，clipped span 不被当作完整 loaded logical line。
- 已补齐 core persisted logical line 重投影 row-id range harness：单条 projected visual row 横跨多个 persisted source row 时，projection row boundary 会覆盖真实 source row id 区间，并保留 explicit authoritative logical line id。
- 当前仍不是完整 logical-line based history。
- 当前滚动不可用的根因已经从 TUI 本地历史路径转移到 core 侧历史真相尚未完整显式 logical-line 化：TUI 旧本地历史路径已删，copy mode buffer、鼠标滚轮、语义 scroll action、selection clipped span 约束、copy mode render 与 copy mode entry 均已能消费 authoritative history window；代码侧残留 `snapshot` / `grid.viewport` 入口目前只应作为 legacy 兼容投影接口继续受限保留。
- 下一步继续 core 显式 logical line store：把 persisted store / mutable live tail / screen projection 三层的 logical line 记录结构进一步收敛，补齐从 metadata 恢复 live tail 与 screen projection 的语义；不回退修补旧 TUI snapshot/grid viewport 滚动路径。
