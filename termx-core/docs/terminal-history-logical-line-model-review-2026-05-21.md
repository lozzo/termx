# Terminal History 逻辑行模型复盘（2026-05-21）

## 范围

- 这份文档记录当前 `termx-core` 历史模型的设计复盘结论。
- 这里只讨论语义和 ownership，不讨论具体实现方案。
- 这不是最终实现设计稿，只是把目前已经确认的问题和边界条件先固定下来。

## 背景

这一轮工作的目标，是让终端历史真正变成 `logical-line aware`，这样：

- resize 变成投影问题，而不是历史重写问题，
- history replay / paging 可以按逻辑行边界工作，
- copy mode 可以按逻辑行而不是按 wrapped 视觉行工作。

在讨论过程中，我们先假设一个简单三层模型：

- `screen`：当前可见终端表面，
- `hot`：最新一条还没封口、等待进入冷区的逻辑行，
- `cold`：已经封口的逻辑行历史。

结论是：这个表述还不够。

## 核心问题

这个简化模型的问题，来自两个假设：

- `hot` 只是一个单向流向 `cold` 的队列，
- 一旦数据进入 `cold`，以后就不需要再回到 live / mutable 区域。

极端 resize 场景会直接打穿这个假设：

1. 终端先运行在 `1x1` 的 PTY 下，每次屏幕上只能显示一个字。
2. 因为屏幕极小，大量输出很快就会被提交到 `cold`。
3. 之后另一个 client 拿到了 resize 权限，把 PTY 拉到 `10000x10000`。
4. 为了得到正确的当前 screen，系统必须把已经提交到 `cold` 的尾部一段重新展示出来。
5. 而这些重新进入 screen 的内容，后续程序仍然可能继续改写。

这说明：

- `cold` 不能简单定义成“永远不会再出现在 screen 上的数据”，
- `hot` 不能简单定义成“只有一条等待 seal 的 open logical line”，
- `screen` 也不能被理解成“只投影当前 open line”。

## 修正后的概念模型

能够覆盖这些边界场景的概念模型，更接近下面三层：

- `persisted history store`
  已经持久化的逻辑行历史，用于 replay、paging、retention、recovery。
- `live tail`
  当前可变的尾窗 backing store。
  它不仅包含最新那条 open logical line，也可能包含因为 grow resize 而从历史尾部回卷出来的 sealed suffix。
- `screen`
  `live tail` 在当前 `cols x rows` 下投影出来的 2D 终端表面。

换句话说，下面三件事必须分开：

- 历史存储，
- 当前可变真相，
- 当前可见投影。

## Ownership 结论

### 1. Attach / Reattach

- 新 client attach 本质上是一次读取 / bootstrap。
- 它应该把当前真相投影给客户端。
- 它不应该创造新历史。
- 它也不应该因为 bootstrap 就把 `live tail` seal 进历史。

### 2. Process Exit

- 进程退出意味着可变性结束。
- 最后一条 open logical line 可能需要在这个边界被强制 seal。
- 这个事件和 attach / bootstrap 不一样，它是一个真正的状态转移。

### 3. Alternate Screen

- alt-screen 不是普通 shell history。
- 进入 alt-screen 时，不能往 primary persisted history 里写内容。
- 退出 alt-screen 时，应该恢复 primary surface，而不是污染 primary history。

### 4. Clear Screen

- clear screen 只是在改当前 surface。
- 它本身不是 history reset。
- 它不应该悄悄清掉已经提交的历史。

### 5. Resize

- resize 不应该创造新的历史语义。
- resize 不应该直接重写 committed history。
- resize 可以重分配 `live tail` 和 `screen` 的 ownership 边界。
- 极端 grow resize 时，可能必须先把已 seal 的历史尾部一段 reclaim 到 mutable tail，再去投影 screen。

## 对 Hot / Cold 术语的影响

原来的 `hot -> cold` 单向表达太窄了。

更准确的术语应该是：

- `persisted history`
- `mutable live tail`
- `screen projection`

如果后面还继续保留 `hot` 这个词，那它更适合表示 `mutable live tail`，而不是“唯一那条还没封口的 latest line”。

## 仍然成立的结论

虽然上面的简化模型不够，但下面这些方向仍然成立：

- committed history 应该是 logical-line aware 的，
- resize 应该尽量只是投影问题，而不是历史重写问题，
- copy mode 应该按逻辑行导航，而不是按 wrapped 视觉行导航，
- attach / bootstrap / full-replace / recovery 不应该被当成创造历史的事件。

## 仍未解决的问题

- grow resize 之后，必须 reclaim 最小充分完整 logical-line 后缀。
- process exit 时，primary live tail 剩余内容一律 force seal。
- 内部模型必须显式区分：
  - persisted history store，
  - mutable live tail，
  - screen projection。
- `mutable live tail` 内部采用 segment 结构，并用属性表达：
  - open / sealed，
  - live / reclaimed，
  - rows 与 metadata。

仍留到后续阶段的问题是：

- reclaim 的独立内存预算和淘汰策略；
- wire / runtime / app 层未来是否也要显式表达这个 ownership 模型，而不是只传现有的 hot/cold hint？

## 当前实现决议

此前小切片已经把 process exit、attach/bootstrap/recovery/full-replace、grow/shrink/reclaim、clear screen 的关键语义用测试钉住。

下一步不再继续做渐进式过渡，而是进入一次性 `termx-core` 内部模型切换：

- 补齐旧单向 `hot -> cold` 模型缺失的 live-tail ownership 结构；
- 引入显式 `mutable live tail` segment 结构；
- 让 write / resize / latest snapshot / viewport / process-exit 路径统一通过三层模型表达；
- 第一阶段仍不扩展 wire/runtime/app contract。

## 本次复盘结论

当前这个“`screen + 单向 hot 队列 + immutable cold history`”的三层表述，在语义上是不完整的。

后续设计应该往下面这个方向收敛：

- persisted logical-line history，
- mutable live tail，
- screen 作为 projection，

而不是继续建立在：

- screen，
- 单向流向 cold 的 hot 队列，
- 永不回流的 cold history

这样的简化模型上。
