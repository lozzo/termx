# 工作流：终端历史逻辑行模型

本文件是当前仓库这条开发线的唯一活动驱动文件。

所有相关分析、设计、实现、测试、文档更新，都必须先读取本文件，并严格以本文件为基准。

本文件必须保持**全中文**。后续如需补充、修改、压缩，也必须继续使用中文，不允许只写英文条目或只写英文摘要。

## 1. 仓库工作范围总表

本节显式列出仓库顶层工作范围，避免后续工作自行发散。

### 1.1 当前主线范围

下面这些目录是当前主线，允许主动分析、设计、实现和测试：

- `termx-core/`
- `termx-vterm/`
- `tuiv2/`

### 1.2 受限联动范围

下面这些目录不是当前主线，但当且仅当 `termx-core/`、`termx-vterm/`、`tuiv2/` 的契约变化确实需要它们时，才允许最小化触及：

- `internal/protocol/`
- `termx-proto/`
- `termx-cli/`
- `termx-shared/`
- `termx-testkit/`
- `scripts/`
- 根目录下与本主线直接相关的：
  - `AGENTS.md`
  - `workflow.md`
  - `go.work`
  - `go.work.sum`
  - `Makefile`
  - 必要的顶层说明文档

### 1.3 当前冻结范围

下面这些目录当前明确冻结，不允许主动扩展工作范围：

- `remote-ui/`
- `termx-app/`
- `web-control/`
- `termx-hub/`
- `termx-remote/`

### 1.4 默认不修改范围

下面这些路径默认不作为本条开发线的工作对象，除非本文件以后显式重新打开：

- `bin/`
- `.claude/`
- 顶层可执行产物与测试产物：
  - `termx`
  - `*.test`
  - `termx-check-*`
- 任何未在本文件明确列出的新范围

### 1.5 范围判断规则

- 如果一个目录没有在本文件里被明确列入“当前主线范围”或“受限联动范围”，默认视为**不在当前工作范围**。
- 任何人不得在聊天、临时说明或代码实现里自行补充工作范围。
- 如确需扩展范围，必须先修改本文件，再开展对应工作。

## 2. 当前唯一主题

当前唯一主题是：

**把终端历史模型从视觉折行主导，收敛到逻辑行主导的历史语义模型。**

这条线关注的是：

- 什么是历史真相；
- 什么是当前可变真相；
- 什么是当前可见投影；
- 哪些事件创造历史；
- 哪些事件只改变归属关系或投影关系。

## 3. 已定死的语义

下面这些条款已经拍板，后续开发不得绕开。

### 3.1 历史语义

- primary history 的核心语义单位固定为“逻辑行”。
- `persisted history` 只承载 `sealed logical lines`。
- wrapped visual row 不再作为历史主语义单位。

### 3.2 三层职责

- `persisted history store` 是历史存储层。
- `mutable live tail` 是当前可变真相层。
- `screen` 只是 `mutable live tail` 的二维投影。

### 3.3 可变尾部

- `mutable live tail` 允许同时包含：
  - open logical lines
  - sealed logical lines
- `mutable live tail` 允许包含从 `persisted history` 尾部 reclaim 回来的 sealed suffix。
- `open/sealed` 与 `origin=live/reclaimed` 是正交属性。
- 不再允许把旧 `hot` 概念作为当前实现主语义；当前实现必须直接使用 `mutable live tail` 及其 segment 属性。

### 3.4 非历史创建事件

下面这些都不是历史创建事件：

- `attach`
- `reattach`
- `bootstrap`
- `recovery`
- `full-replace`
- `clear screen`
- `resize`

这些事件可以重建视图、恢复状态、切换投影，但不能仅因为这些动作就创造新历史。

### 3.5 备用屏语义

- `alt-screen` 不得写入 primary history。
- 进入 `alt-screen` 时，primary history 语义冻结。
- 退出 `alt-screen` 时，恢复 primary surface，不把 alt 内容混入 primary history。

### 3.6 进程退出语义

- `process exit` 是显式 mutability 边界。
- `process exit` 时，primary live tail 中剩余内容一律 `force seal` 进入 persisted history。
- 如果 terminal 退出时仍处于 `alt-screen`，则 alt 内容直接丢弃，不进入 primary history。

### 3.7 缩放语义

- `resize` 不是历史创建事件。
- `resize` 不是历史重写事件。
- `grow resize` 的固定语义是：
  `persisted history tail -> mutable live tail -> screen`
- `shrink resize` 的固定语义是：
  `screen -> hidden mutable live tail`
- shrink 不得仅因为内容退出 screen 可见区，就把该内容立刻视为 immutable cold history。

### 3.8 回卷语义

- grow resize 时，系统必须从 `persisted history` 尾部 reclaim **最小充分 logical-line 后缀**，以便正确重建当前 screen。
- reclaim 的基本单位必须是**完整逻辑行**。
- 不允许只 reclaim 半条逻辑行。

### 3.9 第一阶段边界

- 第一阶段不扩展 wire/runtime/app contract 来表达完整 ownership 模型。
- 第一阶段只要求在 `termx-core` 内部定死 ownership 语义。
- 对外仍允许以保守的 snapshot / viewport / update 语义消费。

### 3.10 第二阶段边界

- 第二阶段开始主动重构 `tuiv2` 与直接相关的 protocol/runtime contract。
- 第二阶段目标是让 TUI 直接消费 logical-line ownership 语义，而不是继续依赖 `LoadedRows=0`、row count、generation 是否为空等间接推断 live tail / persisted history 边界。
- 允许最小化修改 `internal/protocol` 与 `termx-proto`，但只服务于 `termx-core`、`termx-vterm`、`tuiv2` 的当前主线。
- 第二阶段不保留旧 TUI 历史对接路径兼容；旧 snapshot/viewport 推断 helper、状态字段和测试语义可以直接删除或重写。
- 第二阶段仍不得触碰当前冻结范围。

## 4. 当前开发原则

### 4.1 先语义，后实现

- 如果实现与本文件语义冲突，应先修正文档或先停下来讨论，不允许直接靠代码默认行为绕过去。

### 4.2 先核心，后扩散

- 第一阶段已经完成 `termx-core` 内部模型收敛。
- 第二阶段允许扩展 protocol/runtime/app contract，但必须只围绕 logical-line ownership、persisted history、mutable live tail、screen projection 展开。

### 4.3 先测试，后重构

- 每一个语义切片都应先补或先确认测试，再推进实现。
- 优先覆盖 resize、attach、alt-screen、exit、copy mode 边界。

### 4.5 开发阶段不保留旧实现兼容

- 当前处于逻辑行模型开发切换阶段，不要求保留旧内部实现兼容。
- 对本文件已经拍板要替换的旧路径、旧结构、旧 helper、旧测试语义，可以直接删除或重写。
- 不为了兼容旧的 `hot -> cold` 单向实现而保留双路径、适配层或临时桥接代码。
- 从本阶段起，`hot/cold` 只能出现在旧模型问题说明或迁移记录中，不得继续作为代码、测试 helper、内部 contract 或运行时状态的主语义命名。
- 新实现命名统一收敛到 `persisted history store / mutable live tail / screen projection` 三层模型。
- 如果删除旧代码会改变已定死语义，必须先更新本文件和 RFC；如果只是落实已定死语义，可以直接改代码并提交。

### 4.4 文档优先级

如果出现文档冲突，当前优先级如下：

1. 本 `workflow.md`
2. 当前 RFC 与设计文档
3. 子目录 `AGENTS.md`
4. 现有代码行为

现有代码如果与本文件冲突，不代表代码语义自动正确。

## 5. 当前参考文档

当前这条线的核心文档是：

- [RFC：终端历史逻辑行模型](./termx-core/docs/rfc-terminal-history-logical-line-model-2026-05-21.md)
- [逻辑行设计文档](./termx-core/docs/terminal-history-logical-line-design-2026-05-21.md)
- [逻辑行与驻留分层架构](./termx-core/docs/terminal-logical-line-residency-architecture-2026-06-02.md)
- [逻辑行模型复盘](./termx-core/docs/terminal-history-logical-line-model-review-2026-05-21.md)
- [RFC 结构稿](./termx-core/docs/terminal-history-logical-line-rfc-outline-2026-05-21.md)

后续工作应优先更新 RFC 与设计文档，而不是在聊天里维持隐含规则。

## 6. 当前未决问题

下面这些问题已经做出当前阶段决议：

- `mutable live tail` 内部采用显式 segment 结构维护。
- 第一阶段必须把 `termx-core` 内部旧的单向 `hot -> cold` 过渡实现一次性补充为 `persisted history / mutable live tail / screen projection` 模型。
- segment 至少必须表达：
  - logical-line seal 状态；
  - `origin=live/reclaimed`；
  - visual rows 及其 timestamp / row kind / wrapped 元数据；
  - wrap-pending 状态。
- reclaim 的基本语义已经定死：按完整 logical line reclaim，且只 reclaim 最小充分 logical-line 后缀。
- reclaim 的内存上限和长期淘汰策略暂时沿用现有 scrollback / retention 约束，不在本轮扩展新策略。
- 第一阶段仍不扩展 protocol / runtime / app contract。
- 旧 `hot/cold` 术语不再作为当前代码、测试 helper、内部 contract 或运行时状态命名保留；只允许在解释旧模型缺陷时出现。

下面这些已进入第二阶段当前工作范围：

- ownership 模型需要跨 protocol 最小显式暴露；
- `core` 需要对外提供 authoritative history projection；
- client/runtime/app 不再本地重建 history truth；
- copy mode 需要改为消费 `core` 提供的 authoritative history window，而不是继续本地推断 logical-line 边界。

## 7. 当前阶段基线

当前第一阶段已经完成到可收口状态。下面记录的是当前代码与测试应继续遵守的基线，不再是待开始计划。

### 第一阶段：核心内部语义收敛

第一阶段已经完成的核心目标是：

- 在 `termx-core` 内部对齐 `persisted history / mutable live tail / screen projection` 三层语义；
- 补足 grow / shrink / attach / exit / alt-screen / clear-screen 的语义测试；
- 一次性补齐 `termx-core` 内部 `hot/cold` 旧模型缺失的 live-tail ownership 结构；
- 暂不扩展 wire/runtime/app contract。

### 7.1 当前完成基线

- `termx-core` 已引入显式 `primaryLiveTail` / segment 结构；
- live-tail segment 已表达 `open/sealed`、`origin=live/reclaimed`、rows metadata 和 wrap-pending；
- write / snapshot / viewport / restart / process-exit 路径已经改为通过 `primaryLiveTail` 读写尾部状态；
- primary 普通输出先进入 live tail / screen projection 语义，可提交的 sealed logical lines 再进入 `persisted history store`；
- `process exit` 对 primary live tail 执行 force seal，再进入 persisted history；退出时仍在 `alt-screen`，alt 内容直接丢弃；
- `attach / bootstrap / recovery / full-replace / clear screen / resize` 均已有测试约束为非历史创建事件；
- `alt-screen` 写入不进入 primary live tail 或 primary persisted history；
- grow resize 已把 `persisted history` 尾部的最小充分完整 logical-line 后缀写入 `primaryLiveTail` 的 `origin=reclaimed` segment；
- shrink resize 已通过 hidden live tail 表达 `screen -> hidden mutable live tail`，older offset 只能读取已提交 persisted history；
- latest snapshot / viewport 已改为通过 `primaryLiveTail` 中的 reclaimed segment 投影，不再只靠读路径临时回卷 persisted suffix；
- `termx-vterm` 到 `termx-core` 的内部 hint 已删除 `HotAppendRows` / `ResizeHotOwnedRows` 旧字段名，改为 `LiveTailAppendRows` / `ResizeLiveTailRows`，没有保留旧字段别名。
- `termx-core` 相关测试文件、用例名和断言文本已从旧 `hot/cold` 场景词收敛到 `live-tail`、`persisted history`、`committed history` 表述。
- `tuiv2` 现有 copy mode / runtime 历史本地状态仍处于待删除过渡态；后续不再继续增强这套本地 history truth 重建路径，而是准备整体删除并切换到 `core` authoritative history projection。

### 7.2 第二阶段当前执行目标

- `core` 继续保有唯一的 logical-line history truth；logical-line 语义、committed/live-tail 边界、latest replace / older prepend 的 authoritative 判定，不再下放给 client 本地重建。
- protocol/runtime/app 第二阶段的主目标从“让 `tuiv2` 更聪明地消费 row ownership metadata”调整为“让 `core` 对外提供 authoritative history projection，client 只消费结果和最小边界元数据”。
- `tuiv2` runtime/app/copy mode 现有基于 `wrapped`、`ownership`、`CommittedLoadedDepth`、`row ref` 的 history truth 重建逻辑，统一视为待删除路径；不再继续在这条路径上做修补或增强。
- client 仍然负责 PTY bytes 的本地 VT 解析和 live surface 渲染；但 history paging、copy-mode backing window、latest replace / older prepend 接纳规则，必须改为以 `core` 返回的 authoritative history window 为准。
- `tuiv2` 本地 VTerm scrollback、snapshot totals、`hasMore`、`LoadedRows`、latest replace 形态、`wrapped` 拼接结果，都不得再作为 committed history truth 的来源。
- 后续新的 protocol contract 应优先表达：
  - authoritative projected history window；
  - window replace / prepend 语义；
  - committed paging cursor 或等价边界 token；
  - row-group / line-span 这类足以支撑 copy mode 交互但不要求 client 重建 logical-line truth 的最小投影元数据。
- `tuiv2` copy mode 后续只允许保留交互态：
  - 光标；
  - 选区；
  - viewport top；
  - 当前 authoritative window token；
  不再长期保有本地 committed history 深度推断状态机。
- runtime transaction restore、attach / re-entry、stale-page guard 后续都必须围绕 authoritative history window token 工作，而不是围绕本地 `CommittedLoadedDepth` 或 row-level snapshot 推断工作。
- 所有 stale response 过滤、latest boundary reset、older-page continuity 判定，应尽可能前移到 `core`/protocol authoritative 结果中；client 不再承担二次发明历史真相的责任。
- 当前决议改为先破后立：`tuiv2` 中本地 history truth 重建、runtime 本地 viewport merge、copy-mode frozen snapshot 分页合并、`CommittedLoadedDepth` / `CommittedLoadingDepth` / `CommittedHistoryExhausted` 状态机，统一物理删除，不再作为过渡实现保留。
- 新的 `tuiv2` 终端呈现接口先拆成两个输入面：实时 surface 用于本地 VT 解析后的当前屏幕渲染；authoritative history window 用于历史窗口、copy mode backing、older/latest 接纳规则。二者只在 render projection 层组合，不在 client 内部重新发明 history truth。
- 新接口只表达 `core` 返回的窗口、窗口 token、窗口操作语义、最小 row/line span 元数据；实现未完成前，相关旧测试语义视为待删除或待重写，不作为回归基准。
- 当前架构进一步明确：不再把“冷热数据”作为语义边界；落盘、mmap、内存驻留只是 residency / IO 策略。逻辑行才是历史真相单位，已落盘逻辑行后续仍可被终端指令修改，修改通过 resident cache、dirty tracking、page update 或 compaction 等存储策略实现。
- 已破旧收口：`tuiv2` app/runtime 侧本地 history truth 重建、canonical row ref 推断、scrollback 分页空实现、`LoadGridViewport`/`snapshotFromGridViewport` 转换、`preserveSnapshotHistoryMetadataFromProjection` 历史坐标延续均已物理删除；`refreshSnapshot` 现在只做纯 VTerm 投影。
- 已开始立新（第一刀，core 侧）：`termx-core` 新增 `HistoryWindow` / `HistoryLineSpan` / window token / `HistoryWindowOptions`，并提供 `Server.HistoryWindow`，内部复用 `GridViewportWithOptions` 投影出权威窗口；`Op`（replace/prepend）、line span（按 wrapped 归并逻辑行）、token（按 generation+row id 边界）由 core 生成。
- 已完成立新（第二刀，protocol 侧）：`HistoryWindow` 已经经由 `termx-proto` / `internal/protocol` 暴露为 `history.window` 方法，payload 包含 rows、row metadata、line spans、window token、replace/prepend op、paging boundary 与 generation；legacy `snapshot` / `grid.viewport` 暂留，但不再作为 `tuiv2` 历史真相来源。
- `HistoryLineSpan` 当前明确表达的是“窗口内逻辑行片段”，不是最终完整 logical line truth；span 已带 `ClippedBefore` / `ClippedAfter` 裁断标记，`tuiv2` 不得把被裁断片段当作完整逻辑行。后续如需跨页完整 logical line 操作，必须继续由 core 提供 stable logical line id 或等价边界语义。
- 下一刀：让 `tuiv2/historyview.Source` 消费 `history.window`，把 authoritative history window 接入 copy mode backing、older/latest 接纳规则和 render projection。
- 每个子切片都必须保持主线测试可运行，并形成中文提交。

### 当前不启动的工作

- remote / web / app 壳层适配；
- 当前冻结范围内的产品层集成工作。

## 8. 维护规则

- 后续每完成一个 module-sized slice，都要先更新本文件，再继续扩散实现。
- 如果某项开发需要改变已定死语义，必须先修改本文件和 RFC，再进入代码实现。
- 如果某项工作不符合本文件范围，应先停下来确认是否真的需要扩 scope。
- 今后如果要修改本文件中的范围表，必须显式更新“仓库工作范围总表”，不允许只局部加一句模糊说明。
