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
- 不再允许把 `hot` 简化为“唯一一条未封口 latest line”。

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

## 4. 当前开发原则

### 4.1 先语义，后实现

- 如果实现与本文件语义冲突，应先修正文档或先停下来讨论，不允许直接靠代码默认行为绕过去。

### 4.2 先核心，后扩散

- 先收敛 `termx-core` 内部模型。
- 在没有明确必要之前，不主动扩展 protocol/runtime/app contract。

### 4.3 先测试，后重构

- 每一个语义切片都应先补或先确认测试，再推进实现。
- 优先覆盖 resize、attach、alt-screen、exit、copy mode 边界。

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
- `hot/cold` 术语可以继续作为派生或兼容表述保留，但不得再被理解为“唯一 open latest line / 永不回流 persisted history”的旧模型。

下面这些仍然是后续阶段问题，但它们不能阻塞本轮内部大重构：

- ownership 模型是否需要跨 protocol 显式暴露；
- runtime/app 层未来是否需要显式感知 live tail 与 persisted history 边界；
- copy mode 是否改为直接消费 logical-line ownership contract。

## 7. 当前开发起点

当前允许开始的开发阶段是：

### 第一阶段：核心内部语义收敛

重点包括：

- 在 `termx-core` 内部对齐 `persisted history / mutable live tail / screen projection` 三层语义；
- 补足 grow / shrink / attach / exit / alt-screen / clear-screen 的语义测试；
- 一次性补齐 `termx-core` 内部 `hot/cold` 旧模型缺失的 live-tail ownership 结构；
- 暂不扩展 wire/runtime/app contract。

### 7.1 当前完成结果

本轮第一阶段已经完成并提交下面这些切片：

1. `process exit` 语义闭环
   - `termx-core` 已在进程退出时对 primary `live tail` 做 `force seal`。
   - 退出时如果仍在 `alt-screen`，alt 内容会被丢弃，不进入 primary history。
2. `attach / bootstrap / recovery / full-replace` 非历史创建事件回归测试
   - 已补齐 core 侧测试，约束这些事件只重建投影，不创建 committed history。
3. `grow resize / shrink resize / reclaim` 第一轮边界收紧
   - grow latest 读路径已收紧为“最小充分完整 logical-line 后缀”。
   - shrink 隐藏到 live tail 的内容已被测试约束为不能被 older offset 误判成 cold history。
4. `clear screen` 与 primary history 冻结语义
   - 已补齐 core 侧测试，约束 clear screen 只改当前 surface，不创建 committed history，也不污染既有 committed history。

### 7.2 当前执行决议：全面内部模型切换

当前不再按渐进迁移推进。下一步必须做一次 module-sized 的 `termx-core` 内部大重构：

1. 新增显式 `mutable live tail` 内部结构
   - 以 segment 维护 live tail。
   - segment 必须保留 `open/sealed` 与 `origin=live/reclaimed` 两个正交维度。
   - 不得再把 `hot` 只理解为“唯一未封口 latest line”。
2. 改写 primary 写入归属路径
   - 普通 primary 输出先进入 live tail / screen projection 语义。
   - 可提交的 sealed logical lines 再进入 `persisted history store`。
   - `alt-screen` 写入不得进入 primary live tail 或 primary persisted history。
3. 改写 latest snapshot / viewport 组合路径
   - latest 读取必须显式组合 `persisted history store`、`mutable live tail`、`screen projection`。
   - older offset 只能读取已提交 persisted history，不得把 shrink hidden live tail 误当历史。
4. 改写 resize ownership 路径
   - grow 必须通过 live tail reclaim 表达 `persisted history tail -> mutable live tail -> screen`。
   - shrink 必须通过 hidden live tail 表达 `screen -> hidden mutable live tail`。
   - resize 仍不是历史创建事件。
5. 改写 process exit 路径
   - process exit 对 primary live tail 执行 force seal，再进入 persisted history。
   - 退出时仍在 `alt-screen`，alt 内容直接丢弃。
6. 清理内部命名和测试 helper
   - `termx-core` 内部可以保留 `hot/cold` 兼容命名，但必须让语义边界落在三层模型上。
   - 测试 helper 应改为 live-tail 语义命名。
   - 旧测试可以保留行为覆盖，但断言文本应贴近当前三层模型。

这次重构允许一次性修改较大范围；完成后再集中修补回归 bug。不得因此扩展 wire/runtime/app contract。

### 7.3 当前剩余缺口

本轮大重构完成前，第一阶段剩余缺口就是：

- `termx-core` 内部仍有旧单向 `hot -> cold` 过渡实现；
- reclaim ownership 仍主要散落在 latest projection / viewport 读路径；
- 测试命名仍保留旧模型痕迹。

本轮大重构完成后，应重新压缩本节，把已完成结果和仍需后续阶段处理的问题写成新基线。

### 当前不启动的工作

- protocol 层 ownership 扩展；
- runtime/app 全链路重构；
- 当前冻结范围内的产品层集成工作。

## 8. 维护规则

- 后续每完成一个 module-sized slice，都要先更新本文件，再继续扩散实现。
- 如果某项开发需要改变已定死语义，必须先修改本文件和 RFC，再进入代码实现。
- 如果某项工作不符合本文件范围，应先停下来确认是否真的需要扩 scope。
- 今后如果要修改本文件中的范围表，必须显式更新“仓库工作范围总表”，不允许只局部加一句模糊说明。
