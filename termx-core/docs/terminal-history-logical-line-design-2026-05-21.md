# Terminal History 逻辑行设计文档（2026-05-21）

## 1. 文档目标

这份文档用于定义 `termx-core` 后续终端历史模型的设计方向，重点回答下面几个问题：

- 终端历史的“真相”到底应该按什么粒度存储？
- `screen`、`hot`、`cold` 这些概念分别表示什么？
- resize、attach、alt-screen、clear-screen、process exit 这类边界事件应该如何理解？
- 哪些事件会创造历史，哪些事件只是投影或重建视图？

这份文档是语义设计稿，不是实现方案，也不要求当前代码已经满足这里的所有约束。

当前补充决议：

- 第一阶段不再采用渐进迁移策略。
- `termx-core` 内部必须一次性切换到显式的 `persisted history store / mutable live tail / screen projection` 模型。
- `mutable live tail` 内部采用 segment 结构。
- `hot/cold` 术语可以保留，但必须作为三层模型下的派生或兼容表述使用。

## 2. 背景与动机

当前终端历史存在一个核心问题：存储模型仍然过于依赖“视觉折行”。

这样会带来几个直接问题：

- resize 会影响历史边界，历史容易和显示行为耦合；
- copy mode 需要依赖 wrapped 元数据去反推逻辑行；
- history replay / paging 很难天然按逻辑行工作；
- 热区 / 冷区的边界容易随着窗口变化而抖动；
- 极端场景下，已经提交的历史和当前 live 屏幕之间的 ownership 不清晰。

因此，这一轮设计的目标不是单纯“把视觉行换成逻辑行”，而是把以下三件事拆开：

- 历史存储，
- 当前可变真相，
- 当前可见投影。

## 3. 设计目标

### 3.1 主要目标

- 普通历史应以 `logical line` 为主语义，而不是以 wrapped 视觉行为主语义。
- resize 应尽可能只影响投影，不直接重写历史事实。
- copy mode、history replay、paging 应天然按逻辑行边界工作。
- attach / bootstrap / recovery / full-replace 不应被当成创造历史的事件。
- alt-screen 不应污染 primary shell history。

### 3.2 非目标

- 不要求终端的 live VT surface 也改成纯 logical-line 模型。
- 不要求 wire/runtime/app 在这一版文档里就已经跟进所有 contract。
- 不要求本文直接规定最终数据结构和编码格式。

## 4. 基本术语

### 4.1 Logical Line

这里的 `logical line` 指“假设终端宽度无限时，用户应该看到的一整行输出”。

它和视觉折行不同：

- 一个 logical line 在窄终端里可能投影成多条 wrapped visual rows，
- 在宽终端里也可能投影成一条视觉行。

### 4.2 Screen

`screen` 指当前可见的 2D 终端表面。

它仍然必须是 VT 语义上的二维结构，因为：

- 光标移动是按行列寻址的，
- erase / insert / delete / repaint 都是 2D surface 操作，
- 全屏程序和 alt-screen 也依赖这种模型。

因此，`screen` 不是历史真相本身，而是当前 live 状态的可见表面。

### 4.3 Persisted History

`persisted history` 指已经作为历史保存的逻辑行内容，用于：

- replay，
- paging，
- retention，
- recovery。

它是历史存储层，不等于“当前绝不可能再显示”的数据。

### 4.4 Mutable Live Tail

`mutable live tail` 指当前终端仍处在可变语义中的尾部 backing store。

它至少可能包含：

- 当前最新那条尚未最终稳定的 open logical line，
- 因为 grow resize 而从历史尾部重新卷回当前 live 窗口的 sealed suffix。

这层是当前可变真相的一部分，后续程序仍可能继续改写它。

## 5. 为什么原来的 screen + hot + cold 表述不够

一个过于直觉的三层表述是：

- `screen`：当前屏幕，
- `hot`：最新还没封口的一条逻辑行，
- `cold`：已经封口的逻辑行历史。

这个表述的问题在于，它隐含了两个错误假设：

- `hot` 只是一个单向流向 `cold` 的队列，
- 一旦数据进入 `cold`，以后就不需要再回到 live / mutable 区域。

极端 resize 会打穿这个假设。

### 5.1 极端例子：1x1 -> 10000x10000

场景如下：

1. 终端先运行在 `1x1` 的 PTY 下，每次 screen 只能显示一个字。
2. 因为窗口极小，大量输出很快就会离开可见 screen，并被提交到历史。
3. 另一个 client 后来拿到了 resize 权限，把 PTY 拉到 `10000x10000`。
4. 此时为了得到正确的当前 screen，系统必须把已经在历史里的尾部一大段重新展示出来。
5. 而这些重新出现的数据，之后仍然可能被程序改写。

这说明：

- `cold` 不能简单理解成“永远不会再回到 screen 的冻结数据”，
- `hot` 也不能只是一条等待 seal 的 open line，
- `screen` 不是独立真相，而是当前 live tail 的投影。

## 6. 建议的概念模型

建议把模型重新表述为三层：

### 6.1 Persisted History Store

表示：

- 已经作为历史保存的 logical lines，
- 用于 replay / paging / retention / recovery。

它是历史存储层。

### 6.2 Mutable Live Tail

表示：

- 当前仍处在 live 语义里的尾部 backing store，
- 包含 open logical line，
- 必要时也包含从 persisted history 尾部 reclaim 回来的 sealed suffix。

它是当前可变真相的一部分。

### 6.3 Screen Projection

表示：

- 以当前 `cols x rows` 为约束，
- 将 `mutable live tail` 投影成 2D 可见终端表面。

它是显示层，而不是历史层。

## 7. 不变量

下面这些不变量是后续设计必须尽量满足的：

### 7.1 关于历史

- 历史语义应尽可能以 logical line 为单位。
- persisted history 不应直接依赖 observer 宽度。
- resize 不应直接重写已经确认的历史语义。

### 7.2 关于显示

- `screen` 始终是 2D VT surface。
- `screen` 是 live truth 的投影，不是独立历史真相。

### 7.3 关于 ownership

- attach / bootstrap / recovery / full-replace 这类事件，本质是读取或重建视图，不是历史创建。
- process exit 是一个真正的 mutability 边界。
- alt-screen 的历史和 primary history 必须隔离。

### 7.4 当前已定死的语义

下面这些点不再作为开放问题讨论，而作为当前开发前提：

- 普通 primary history 的语义单位固定为 `logical line`。
- `persisted history` 只承载 `sealed logical lines`。
- `mutable live tail` 是当前可变真相，允许同时包含：
  - open logical line；
  - reclaimed sealed suffix。
- `screen` 不是独立真相，只是 `mutable live tail` 的 2D 投影。
- `attach / reattach / bootstrap / recovery / full-replace` 都不是历史创建事件。
- `clear screen` 不是历史提交事件，也不是默认的历史清空事件。
- `alt-screen` 不写入 primary history。
- `process exit` 时，primary live tail 中剩余内容一律 force seal 进入 persisted history。
- 若退出时仍处于 alt-screen，则 alt 内容直接丢弃，不进入 primary history。
- `grow resize` 固定理解为：
  `persisted history tail -> mutable live tail -> screen`
- `shrink resize` 固定理解为：
  `screen -> hidden mutable live tail`
- shrink 不因为“当前看不见了”就自动把内容打成 immutable cold history。
- grow reclaim 的基本单位固定为**完整 logical line**，不得只 reclaim 半条逻辑行。
- grow reclaim 的范围固定为“最小充分 logical-line 后缀”。
- 第一阶段不扩展 wire/runtime/app contract 来表达完整 ownership 语义。

## 8. 事件分类

从语义上看，事件可以分成三类。

### 8.1 历史创造事件

这些事件可能真正影响 persisted history：

- 普通 primary screen 输出在满足提交条件后进入历史，
- process exit 导致最后 open line 被强制 seal，
- 显式 history reset / trim / retention。

### 8.2 投影变化事件

这些事件不应该创造历史，只改变当前显示或 ownership 边界：

- resize，
- attach / reattach，
- clear screen，
- viewport 改变，
- copy mode 进入 / 退出。

### 8.3 surface 切换 / 重建事件

这些事件主要是 surface 语义切换，不应被当成普通历史追加：

- enter alt-screen，
- exit alt-screen，
- bootstrap，
- recovery，
- full-replace。

## 9. 各类边界事件的设计语义

### 9.1 Attach / Reattach

attach 的本质是：

- 拿当前权威状态，
- 投影给新客户端，
- 开始跟 live stream。

因此 attach 的语义应该是：

- 它是读取事件，
- 它不创造新历史，
- 它不应该因为 bootstrap 就把 mutable live tail seal 掉。

### 9.2 Process Exit

process exit 的语义是：

- 当前 terminal 的可变性结束。

因此它和 attach 不同，它是一个真实边界：

- 退出前尚未最终稳定的最后 logical line，可能要在此处被强制 seal，
- 否则用户看到的 terminal 最后一段输出会长期悬浮在 live tail 中，不真正进入历史。

这里已经定死的语义是：

- 对 primary history 来说，process exit 时剩余 live tail 内容一律 force seal；
- 如果退出时仍处于 alt-screen，则 alt 内容不进入 primary history。

### 9.3 Alternate Screen

alt-screen 不属于普通 shell history。

因此：

- 进入 alt-screen 时，primary persisted history 应冻结，
- alt 内部可以有自己的临时 screen / 临时 scrollback，
- 但 alt 不应写入 primary history，
- 退出 alt-screen 时，应恢复 primary screen，而不是把 alt 内容混入主历史。

### 9.4 Clear Screen

clear screen 的语义只是：

- 修改当前 surface。

它不是：

- history reset，
- persisted history 清理，
- 或 logical line 提交事件。

除非有显式的 terminal reset / clear-history 语义，否则 clear screen 不应影响普通历史。

### 9.5 Resize

resize 是本设计里最重要的事件之一。

正确语义应该是：

- resize 尽量只改变 projection，
- resize 不直接创造新历史，
- resize 不直接重写 persisted history，
- resize 可以改变 `mutable live tail` 与 `screen` 的 ownership 边界。

特别是 grow resize：

- 可能需要把 persisted history 尾部一段 reclaim 回 mutable tail，
- 再基于新的尺寸把这段 live tail 投影回 screen。

这意味着“已经在历史里”并不等于“永远不可能重新参与当前 live screen”。

这里已经定死的语义是：

- grow reclaim 的单位必须是完整 logical line；
- grow reclaim 的范围固定为“最小充分 logical-line 后缀”。

反过来，shrink resize 也需要明确：

- `10000x10000 -> 1x1` 不应被理解成“原来可见、现在看不见的内容立刻变成 cold history”；
- 更合理的语义是：原来可见、现在因为 shrink 退出可见区的那部分，先回退到 hidden `mutable live tail`；
- shrink 本身不是历史提交事件；
- 只有在后续满足独立的提交条件时，这部分 live tail 才会继续下沉到 persisted history。

也就是说：

- grow 更像 `persisted history tail -> mutable live tail -> screen`
- shrink 更像 `screen -> hidden mutable live tail`

两者都主要是在调整 projection 和 ownership，而不是直接改写历史事实。

## 10. 对 hot / cold 术语的建议

如果继续使用 `hot` / `cold` 术语，需要先收紧定义。

### 10.1 不推荐的旧理解

- `hot`：唯一一条等待进入 cold 的 open line，
- `cold`：一旦提交就永不回流的冻结历史。

这种定义在 grow resize 下不成立。

### 10.2 更合理的理解

- `hot` 更接近 `mutable live tail`，
- `cold` 更接近 `persisted history store`。

也就是说：

- `hot` 不一定只有一条 open line，
- `hot` 可以包含 reclaim 回来的 sealed suffix，
- `cold` 也不再意味着“永不重新参与 live 投影”。

### 10.3 当前实现决议

第一阶段内部重构不再继续沿用旧的单向 `hot -> cold` 作为主实现模型。

`termx-core` 中应显式引入 `mutable live tail` 结构，并用 segment 表达：

- logical-line seal 状态；
- `origin=live/reclaimed`；
- visual rows 及 timestamp / row kind / wrapped 元数据；
- wrap-pending 状态。

旧的 `hot` 相关 helper 和测试命名应随实现一起补充 live-tail 语义。若某些局部变量为了小范围算法可读性继续保留旧词，也必须服从三层模型边界。

## 11. Copy Mode / Replay / Paging 的语义收益

如果后续按这个模型推进，会有几个直接收益。

### 11.1 Copy Mode

- 可以直接按 logical line 工作，
- 不再依赖 wrapped visual rows 去反推真实行边界，
- 行首 / 行尾 / 整行复制的语义更稳定。

### 11.2 History Replay / Paging

- 可以天然按逻辑行边界裁剪，
- 不会因为 observer 宽度不同而把同一条逻辑行切碎。

### 11.3 Resize

- resize 更接近“重新投影当前 truth”，
- 而不是“重写历史并重新定义冷热边界”。

这一点在两个方向上都成立：

- grow 不等于“把 cold 直接画到 screen 上”，而是先 reclaim 到 live tail，再投影；
- shrink 不等于“把 screen 上退出可见区的内容直接打成 cold”，而是先退回 hidden live tail。

## 12. 当前仍未决的问题

这份设计文档确认了方向，但还有几个问题没有定死。

### 12.1 Live Tail 的精确定义

当前阶段已经明确：

- live tail 至少覆盖当前仍可能参与 screen projection 的 primary tail；
- live tail 可以同时包含 open logical line 和 sealed logical-line suffix；
- live tail 可以包含从 persisted history 尾部 reclaim 回来的 segment；
- 内部采用 segment 结构维护。

仍留到后续阶段的问题是：

- reclaim 的缓存上限、内存预算、淘汰策略是否需要独立于现有 retention 另行定义。

### 12.2 Exit Seal 语义

这一点已经定死，不再开放：

- process exit 时，primary live tail 剩余内容一律 force seal；
- alt-screen 退出态内容不进入 primary history。

### 12.3 Wire / Runtime / App Contract

需要明确：

- 这种 ownership 模型是否最终要跨过 core/vterm 边界，
- 以及在哪个阶段才值得跨过 wire/runtime/app 暴露出去。

### 12.4 Internal Structure

当前阶段已经明确：

- 内部必须显式区分 `persisted history store`、`mutable live tail`、`screen projection`；
- `mutable live tail` 用统一 segment 列表管理；
- segment 内部用属性区分 open logical line、sealed suffix、reclaimed suffix，而不是只依赖旧单向 hot/cold 路径表达。

## 13. 本文结论

`termx-core` 的终端历史设计，不应继续建立在下面这个过于简化的模型上：

- `screen`
- 一条单向流向 `cold` 的 `hot` latest line
- 永不回流的 `cold` history

更可靠的设计方向应是：

- `persisted history store`
- `mutable live tail`
- `screen as projection`

其中：

- logical line 是历史语义的核心单位，
- resize 主要是投影和 ownership 变化，
- attach / bootstrap / recovery / full-replace 不是历史创建事件，
- alt-screen 与 primary history 必须严格隔离。

这就是当前逻辑行设计的落盘版本。
