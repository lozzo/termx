# RFC: Terminal History Logical-Line Model

- Status: Draft
- Date: 2026-05-21
- Scope: `termx-core` / `termx-vterm` / `tuiv2`
- Authors: TBD

## 1. 摘要

当前终端历史模型仍然过度依赖视觉折行，导致 resize、paging、copy mode 与 hot/cold 边界都和显示宽度耦合。本文提议将终端历史的核心语义单位明确为 `logical line`，并将整体模型收敛为三层：

- `persisted history store`
- `mutable live tail`
- `screen projection`

该模型的目标是：

- 让 resize 主要成为投影和 ownership 变化问题，而不是历史重写问题；
- 让 replay、paging、copy mode 天然按逻辑行边界工作；
- 明确 attach、bootstrap、full-replace、recovery、alt-screen、process exit 等事件到底是不是“创造历史”的事件。

## 2. 背景

当前实现已经出现了明显的“逻辑行化”需求：

- `termx-core` 冷层仍主要按 wrapped 视觉行存储，只是附带了 `wrapped` 元数据；
- `hotAppendRows` 一类机制已经在尝试维护“最新未封口尾部”的独立语义；
- `tuiv2` copy mode 已经需要通过 `wrapped` 元数据反推逻辑行边界。

这种状态说明：

- 现有模型并非完全错误，
- 但它已经不够表达当前系统需要处理的 ownership 和 resize 语义。

## 3. 问题定义

### 3.1 当前模型的根问题

当前模型的问题，不只是“冷层还在按视觉行存储”，而是更深一层：

- 历史存储、
- 当前可变真相、
- 当前屏幕投影

这三者还没有被完全分开。

### 3.2 旧的直觉三层表述不够

一个非常直觉的模型是：

- `screen`：当前屏幕
- `hot`：最新一条还没封口、等着进入冷层的逻辑行
- `cold`：已经封口的逻辑行历史

这个表述的问题在于，它隐含了两个假设：

- `hot` 是单向流向 `cold` 的；
- 一旦进入 `cold`，数据就不会再回到 live / mutable 区域。

这两个假设都不稳。

### 3.3 极端 grow resize 例子

下面这个场景能直接打穿“一路 hot -> cold”的想法：

1. 终端先运行在 `1x1` 的 PTY 下，每次 screen 只能显示一个字。
2. 由于窗口极小，大量输出很快离开 screen，并被提交到历史。
3. 之后另一个 client 拿到 resize 权限，把 PTY 拉到 `10000x10000`。
4. 为了得到正确的当前 screen，系统必须把已经提交的历史尾部大段重新展示出来。
5. 这段重新显示出来的内容，后续程序仍可能继续改写。

这说明：

- 已进入历史，不等于永远不会再参与当前 live screen；
- `hot` 不能只表示“一条尚未 seal 的 latest line”；
- `screen` 也不能被视为独立真相，它只是某个 live backing store 的投影。

## 4. 目标

### 4.1 主要目标

- 将 `logical line` 定义为终端普通历史的核心语义单位。
- 让 replay、paging、copy mode 天然按逻辑行边界工作。
- 让 resize 主要影响投影和 ownership，而不是直接重写历史语义。
- 让 attach / bootstrap / recovery / full-replace 明确成为“读取或重建视图”事件，而不是“创造历史”事件。
- 让 alt-screen 与 primary shell history 明确隔离。

### 4.2 次要目标

- 为后续 canonical row identity / generation / paging contract 提供更稳定的历史真相模型。
- 降低未来在 runtime / app 层处理热区、冷区、copy mode 边界时的复杂度。

## 5. 非目标

- 本 RFC 不要求把 VT live surface 改造成纯 logical-line 数据结构。
- 本 RFC 不要求第一阶段立即改完所有 wire/runtime/app contract。
- 本 RFC 不直接规定最终 protobuf / binary payload 的编码形式。
- 本 RFC 不处理 remote 产品层面的历史语义扩展。

## 6. 术语

### 6.1 Logical Line

假设终端宽度无限时，用户应看到的一整行输出。

### 6.2 Visual Row

在某个特定 `cols` 下，logical line 被折叠投影后的视觉行。

### 6.3 Screen

当前可见的 2D VT surface。它仍然必须按行列寻址，支持 cursor move、erase、insert、delete、repaint、alt-screen 等行为。

### 6.4 Persisted History Store

已经持久化的逻辑行历史，用于 replay、paging、retention、recovery。

### 6.5 Mutable Live Tail

当前仍处在可变语义中的尾部 backing store。它至少可能包含：

- 当前尚未完全稳定的 open logical line；
- 因 grow resize 而从历史尾部重新回卷出来、重新参与 live 投影的 sealed suffix。

### 6.6 Screen Projection

把 `mutable live tail` 按当前 `cols x rows` 投影到 `screen` 的过程和结果。

### 6.7 Primary History

普通 shell / primary screen 所对应的历史。

### 6.8 Alternate Screen

全屏程序使用的独立 surface。它不应污染 primary history。

## 7. 设计总览

本文提议的模型是：

- `persisted history store`
  保存历史语义，核心单位是 logical line。
- `mutable live tail`
  保存当前可变尾部真相，供 screen 和后续写入继续共享。
- `screen projection`
  把当前 live tail 投影成 2D VT surface。

这个模型的关键点是：

- 历史语义与 observer 宽度解耦；
- `screen` 不是历史层，也不是独立真相层；
- resize 主要重算投影和 live ownership 边界；
- attach / bootstrap / recovery 读取当前真相，而不是制造新历史。

## 7.1 规范性条款

本节给出当前已经拍板、用于指导后续开发的规范性语义约束。

### 7.1.1 历史语义

- 普通 primary history 的核心语义单位必须是 `logical line`。
- `persisted history store` 只承载 `sealed logical lines`。
- wrapped visual rows 不得再作为历史主语义单位。

### 7.1.2 三层职责

- `persisted history store` 是历史存储层。
- `mutable live tail` 是当前可变真相层。
- `screen` 只是 `mutable live tail` 的 2D 投影。

### 7.1.3 Mutable Live Tail

- `mutable live tail` 允许同时包含 `sealed` 与 `open` 的 logical lines。
- `mutable live tail` 允许包含从 persisted history 尾部 reclaim 回来的 sealed suffix。
- `open/sealed` 与 `origin=live/reclaimed` 应被视为正交属性，不得再把 `hot` 简化成“唯一一条未封口 latest line”。

### 7.1.4 Attach / Bootstrap / Recovery / Full-Replace

- `attach / reattach / bootstrap / recovery / full-replace` 都不是历史创建事件。
- 这些事件可以读取、恢复或重建当前视图，但不得仅因为重建视图就 seal 当前 live tail。

### 7.1.5 Clear Screen

- `clear screen` 不是历史提交事件。
- `clear screen` 也不是默认的历史清空事件。

### 7.1.6 Alternate Screen

- `alt-screen` 不得写入 primary history。
- 进入 alt-screen 时，primary history 语义必须冻结。
- 退出 alt-screen 时，必须恢复 primary surface，而不是把 alt 内容混入 primary history。

### 7.1.7 Process Exit

- `process exit` 是显式 mutability 边界。
- `process exit` 时，primary live tail 中剩余内容必须 `force seal` 进入 persisted history。
- 如果 terminal 退出时仍处于 alt-screen，则 alt 内容不得进入 primary history。

### 7.1.8 Resize

- `resize` 不是历史创建事件。
- `resize` 不是历史重写事件。
- `grow resize` 的固定语义是：
  `persisted history tail -> mutable live tail -> screen`
- `shrink resize` 的固定语义是：
  `screen -> hidden mutable live tail`
- shrink 不得仅因为内容退出 screen 可见区，就立刻把该内容视为 immutable cold history。

### 7.1.9 Reclaim

- grow resize 时，系统必须从 persisted history 尾部 reclaim **最小充分 logical-line 后缀**，以便正确重建当前 screen。
- reclaim 的基本单位必须是**完整 logical line**，不得只 reclaim 半条逻辑行。

### 7.1.10 第一阶段边界

- 第一阶段不得扩展 wire/runtime/app contract 来表达完整 ownership 模型。
- 第一阶段 ownership 模型只要求在 `termx-core` 内部定死，对外仍允许保守消费 snapshot / viewport / update 语义。

## 8. 数据模型提案

### 8.1 Persisted History Store

语义上承载：

- sealed logical lines；
- replay / paging 所需的顺序和 identity；
- retention / trim 所需的边界信息。

它不应再以 wrapped 视觉行为主要事实单位。

### 8.2 Mutable Live Tail

第一阶段 `termx-core` 内部必须把 `mutable live tail` 落成显式 segment 结构，而不是继续把 `hot` 当作“唯一未封口 latest line”。

它语义上承载：

- 最新 open logical line；
- 当前尚可能被程序继续改写的尾部内容；
- 必要时从 persisted history 尾部 reclaim 回来的 sealed suffix。

它的本质是：

- 当前 live truth 的 backing store，
- 而不是单纯“等待进入 cold 的队列”。

segment 至少需要表达：

- logical-line seal 状态：`open` 或 `sealed`；
- origin：`live` 或 `reclaimed`；
- visual rows 及其 timestamp / row kind / wrapped 元数据；
- wrap-pending 状态。

`open/sealed` 与 `origin=live/reclaimed` 是正交维度。一个 reclaimed segment 可以是 sealed，一个 live segment 也可以因为 hard newline 已 sealed 但仍处于 live tail 中。

### 8.3 Screen

`screen` 仍然是 2D surface。

它应满足：

- VT 语义完整；
- 可由 live tail 在当前尺寸下投影得到；
- 不直接承担历史真相职责。

## 9. Ownership 模型

### 9.1 核心原则

核心原则只有一句：

`persisted history` 是历史存储层，`mutable live tail` 是当前可变真相，`screen` 只是 live tail 的投影。

### 9.2 Ownership 直觉

- 一段内容能否继续被程序改写，决定它是否仍在 live truth 中。
- 一段内容是否已经被保存为历史，不决定它永远不能再次参与 screen。
- grow resize 允许 persisted history 尾部的一部分重新参与 live projection。

### 9.3 为什么要允许“回卷”

如果不允许从历史尾部回卷到 live tail，就无法正确处理：

- 极端 grow resize；
- attach 到超大窗口时需要恢复更大 current screen；
- 某些程序在扩大 PTY 后继续改写此前刚刚离开 screen 的尾部内容。

## 10. 状态转移与事件语义

### 10.1 普通写入

- 普通字符输出、cursor move、erase、insert、delete、repaint 首先作用于 live tail / screen 语义。
- 它们本身不是历史提交事件。

### 10.2 Hard Newline

- hard newline 使一个 logical line 在语义上到达终止点。
- 但“已经终止”不自动等于“已经进入 persisted history”。
- 是否可提交，还取决于它是否脱离当前 live mutable ownership。

### 10.3 Scroll Out

- 内容离开当前 visible screen，是重要的 ownership 变化事件。
- 但“离开 screen”本身不自动等于“直接进入最终冷层”。
- 若该内容仍属于 live tail 范围，则仍可能保留为 mutable truth。

### 10.4 Resize

- resize 不应直接定义新的历史事实。
- resize 可以改变：
  - 哪部分 live tail 当前可见；
  - 哪部分 live tail 当前不可见；
  - 是否需要从 persisted history 尾部 reclaim sealed suffix。

### 10.5 Attach / Reattach

- attach 是读取事件。
- attach 获取当前权威状态并投影给新客户端。
- attach 不应 seal live tail，不应制造新历史。

### 10.6 Bootstrap / Recovery / Full-Replace

- 这些事件本质上是 surface 或状态重建事件。
- 它们可以重建视图，但不应被理解为“新增了一段历史”。

### 10.7 Process Exit

- process exit 是 mutability 边界。
- 它必须触发 primary live tail 中剩余内容的强制 seal。
- 如果退出时仍在 alt-screen，则 alt 内容直接丢弃，不进入 primary history。

### 10.8 Enter Alternate Screen

- 进入 alt-screen 时，应冻结 primary history 语义。
- alt-screen 可拥有自己的独立 surface。

### 10.9 Exit Alternate Screen

- 退出 alt-screen 时，应恢复 primary screen / live tail 视图。
- alt 内容不应被追加进 primary history。

### 10.10 Clear Screen

- clear screen 是 surface 改写事件。
- 它不是普通历史提交事件。
- 它也不应默认清掉 persisted history。

## 11. 历史提交规则

本文不在这一版 RFC 里把提交条件完全形式化，但先给出高层约束：

- 历史提交以 logical line 为基本单位。
- “逻辑行到终止点”是必要条件之一。
- “这段内容不再需要保留为当前 mutable live truth”是另一个必要条件。
- process exit 是必须触发强制 seal 的边界。
- attach、resize、bootstrap、clear screen、alt-screen 切换都不应直接被视为历史提交事件。

## 12. 投影与 Resize 规则

### 12.1 Shrink

窗口变窄时：

- 更多 logical line 内容可能从可见区退回到不可见 live tail；
- persisted history 不应因此被重写；
- 原来可见、现在因为 shrink 退出 screen 的内容，不应仅仅因为“当前看不见”就立刻视为 immutable cold history。

更准确地说，`10000x10000 -> 1x1` 的语义更接近：

- `screen` 缩小，
- 原 screen 中退出可见区的那部分先回退到 hidden `mutable live tail`，
- shrink 本身不自动成为历史提交事件。

### 12.2 Grow

窗口变宽或变大时：

- 当前可见 screen 可能需要展示更长的 live tail；
- 必要时还要从 persisted history 尾部 reclaim sealed suffix；
- reclaim 的基本单位必须是完整 logical line；
- reclaim 的范围固定为“最小充分 logical-line 后缀”。

### 12.3 核心约束

- resize 改变的是投影和 ownership 边界；
- resize 不应直接成为“新历史写入”或“历史重排提交”事件。
- grow 更像 `persisted history tail -> mutable live tail -> screen`；
- shrink 更像 `screen -> hidden mutable live tail`。

## 13. 特殊场景

### 13.1 1x1 -> 10000x10000

预期语义：

- 先前离开 screen 的大量尾部内容可能重新参与当前 screen；
- 这部分不应通过“重写冷层事实”实现；
- 更合理的方式是 reclaim 到 mutable live tail，再重新投影。

### 13.2 10000x10000 -> 1x1

预期语义：

- 大量原本可见的 screen 内容退出可见区；
- 它们先进入 hidden mutable live tail，而不是因为 shrink 立即变成 immutable cold history；
- 如果后面再次 grow，这些内容可以重新参与 screen projection；
- 只有在满足独立的提交条件时，这部分内容才继续下沉到 persisted history。

### 13.3 新客户端 Attach 到已有 Terminal

预期语义：

- 获取当前权威视图；
- 不创造新历史；
- 不改变 live tail 的 seal 状态。

### 13.4 Terminal Process Exit

预期语义：

- 当前可变性结束；
- 最后一段 open logical line 需要明确是否 force seal。

### 13.5 Alt-Screen 下的全屏程序

预期语义：

- alt-screen 行为不污染 primary history；
- 全屏 clear / redraw / scroll 属于 alt surface 内语义。

### 13.6 Clear Screen

预期语义：

- 只改当前 surface；
- 不清 primary persisted history；
- 不应隐式触发历史 seal。

## 14. 对外 Contract 的影响

### 14.1 Core 内部

第一阶段 core 内部必须显式区分：

- persisted history；
- mutable live tail；
- screen projection。

这不是后续可选项，而是当前 Phase 1 的完成条件。实现上应一次性移除以 `hot/cold` 为主语义的过渡层；如果局部变量因为兼容旧测试或局部算法暂时保留旧词，也不得再承担模型定义职责。

### 14.2 Core / VTerm 边界

当前已有一些 hot/cold hint，但语义仍偏局部。

未来若要进一步稳定 resize 和 tail ownership，可能需要更明确的内部 contract，而不只是局部的 append suffix 提示。

### 14.3 Runtime / App / Wire

本 RFC 不要求第一阶段立即暴露完整 ownership 模型，但要承认：

- 现有 runtime / app 层在没有完整 ownership 信息时，只能做保守处理；
- 未来若跨层扩展 contract，应围绕 live tail / persisted history 的边界展开，而不是继续围绕 wrapped visual row 修补。

## 15. 备选方案与取舍

### 15.1 继续以视觉行作为主语义

优点：

- 和当前部分实现更接近。

缺点：

- resize、paging、copy mode 长期与 observer 宽度耦合；
- 逻辑行边界需要不断反推。

结论：

- 不推荐继续作为长期模型。

### 15.2 继续沿用 screen + one-way hot + cold

优点：

- 概念直观；
- 对“普通 open line”场景解释简单。

缺点：

- 无法正确覆盖 grow resize；
- 无法稳定表达历史尾部重新参与 live screen 的情况；
- `hot` 的含义过窄。

结论：

- 不足以作为最终语义模型。

### 15.3 persisted history + mutable live tail + screen projection

优点：

- 更能覆盖复杂 ownership 场景；
- 更符合 resize、attach、alt-screen、copy mode 的真实语义；
- 更适合后续 contract 收敛。

缺点：

- 概念比旧模型更复杂；
- 对实现边界提出了更明确要求。

结论：

- 作为推荐方向采纳。

## 16. 未决问题

- mutable live tail 内部已经决定采用 segment 结构，并至少区分：
  - open / sealed
  - live / reclaimed
  - rows 与 metadata
- reclaim 的缓存上限、内存预算和淘汰策略如何定义？
- 这种 ownership 模型在后续阶段是否最终需要跨 protocol 显式暴露？

## 17. 验证方案

后续验证建议至少覆盖：

- exact-width hard newline；
- wrapped prompt + resize；
- attach / reattach latest semantics；
- alt-screen enter/exit；
- clear screen 不污染 primary history；
- `1x1 -> 10000x10000` grow resize；
- history replay / paging 的 logical-line 边界；
- copy mode 对逻辑行的行首 / 行尾 / 整行行为。

## 18. 迁移计划

建议按阶段推进：

### Phase 0: 语义冻结

- 先统一术语和 ownership 模型；
- 先明确什么是历史，什么是 live tail，什么是投影。

### Phase 1: Core 内部模型收敛

- 在 `termx-core` 内部一次性切换到 persisted history / mutable live tail / screen projection 的显式模型。
- 移除以 `hot/cold` 为主语义的过渡实现。
- 让 write / resize / snapshot / viewport / process-exit 路径统一通过 live-tail ownership 结构表达。

### Phase 2: Snapshot / Viewport / Paging 适配

- 让对外读取路径逐步按 logical line 和 canonical history 窗口工作。

### Phase 3: Runtime / Copy Mode 适配

- 让 `tuiv2` 的 copy mode、paging ownership、latest semantics 与新模型对齐。

### Phase 4: Contract 审视

- 再决定是否需要把更多 ownership 语义往 wire/runtime/app 侧暴露。

## 19. 风险

### 19.1 语义风险

- 若 persisted history 和 live tail 的边界不清，可能重新引入重复显示、缺行或错误 seal。

### 19.2 性能风险

- grow resize reclaim 可能带来额外内存驻留；
- live tail 过大可能增加投影成本。

### 19.3 Contract 风险

- 若 protocol 不显式表达新的 ownership 模型，runtime 仍会保守回退，短期内行为可能不够优雅。

### 19.4 回归风险

- 复制、paging、attach、resize、alt-screen 这些路径的交叉场景很多，必须依赖系统性回归覆盖。

## 20. 结论

本文建议将 `termx` 的终端历史模型，正式从下面这个过于简化的理解中迁出：

- `screen`
- 一条单向流向 `cold` 的 `hot` latest line
- 永不回流的 `cold history`

并改为：

- `persisted history store`
- `mutable live tail`
- `screen projection`

在这个模型下：

- logical line 是历史语义核心单位；
- screen 是 live truth 的二维投影；
- resize 主要影响投影和 ownership 边界；
- attach / bootstrap / recovery / full-replace 不是历史创建事件；
- alt-screen 与 primary history 必须严格隔离。

这份 RFC 仍是 Draft，但它给出了后续收敛实现和 contract 的明确语义方向。
