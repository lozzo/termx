# TUI 设计二次评审

评审对象：当前 `docs/tui/*.md`

评审结论：**这一版明显比上一版成熟，已经从“有想法的草案”推进到了“接近可实现规格”；但仍有几处硬冲突和几条尚未定死的规则，建议再做一轮收口。**

## 这次改得好的地方

相比上一版，这次最明显的进步有 6 点：

### 1. 补上了 lifecycle / GC

`docs/tui/model.md` 现在有了完整的状态机、`refcount` 和 GC 规则，`exited` / `killed` / `gone` 的语义清楚很多。这一块比上一版扎实不少。

### 2. 补上了多写入者策略

`docs/tui/model.md` 明确写了“**不仲裁，raw 透传，last-byte-wins**”，这至少把系统行为定死了，不再停留在抽象卖点上。

### 3. 补上了只读模式

`readonly` 是这版很有价值的补充。它虽然简单，但确实给“观察 AI / 多人围观同一个 terminal”提供了最低限度的误触保护。

### 4. layout 匹配比上一版稳定得多

`docs/tui/layout.md` 现在补了排序规则和 `_hint_id`，对“同 tag 多实例”的不确定性修复是有效的。

### 5. 启动优先级终于写清楚了

`--layout`、`workspace state`、项目级自动发现、用户级默认，这几个来源的优先级终于落成了明确规则，这很重要。

### 6. 新增 `verification.md` 很对

把 P0/P1 假设单独拎出来，而不是假装都已经被证明，是这轮改动里最成熟的部分。说明作者已经在区分“规格”与“待验证假设”。

## 还存在的硬问题

### 1. 快捷键出现了硬冲突：`C-a [`

`docs/tui/interaction.md` 里：

- `C-a [` 已经用于进入 Copy/Scroll 模式
- 同时又被用于“将当前浮动 Viewport 降到最底层”

这是直接冲突，必须改一个。这个不是风格问题，是实现不了的问题。

### 2. `Restart` 语义前后冲突

`docs/tui/model.md` 的生命周期状态机写的是：

- `Restart()` 后，**所有观察旧 Terminal 的 Viewport 自动绑定到新的**

但后面的“程序退出后的 Viewport 行为”又写：

- 只有当前触发 restart 的那个 Viewport 绑定到新 Terminal
- 其他 Viewport 仍然停留在旧的 `[exited]`

这两种语义差别非常大，必须统一。我更推荐后者：**谁点 restart，谁拿到新 Terminal；其他 Viewport 保持 exited**，这样副作用更小。

### 3. tag 匹配排序规则和 AND 语义有矛盾

`docs/tui/layout.md` 一边定义：

- 多 tag 是 AND 语义，所有 tag 都必须匹配

另一边又举例说：

- 声明 `role=editor,project=api`
- 只带 `role=editor` 的 Terminal 匹配度较低，但仍参与排序

这两者冲突。按 AND 语义，后者根本不该进入候选集。

建议改成两步：

- 第一步：先按匹配表达式过滤候选集
- 第二步：只在候选集中做稳定排序

“匹配度”这个概念如果保留，应该改成“表达式特异性”或“hint 命中优先级”，不要再用当前这个例子。

### 4. `readonly` 和 AI 场景里的“随时 Ctrl-C 中断”冲突

当前规格写的是：

- `readonly` 下所有输入都不发送
- `Ctrl-C` 也不发送

但 AI 场景又一直强调：

- 人类随时可以 `Ctrl-C` 中断 agent

这两个放在一起会冲突：如果我为了避免误触，把 agent 的 viewport 设成 `readonly`，那我反而失去了紧急中断能力。

建议二选一：

- 要么 `Ctrl-C` 是 readonly 下唯一允许透传的控制键
- 要么明确说 readonly 观察者不能中断，只能先退出 readonly

我更建议前者。

### 5. `Alt-h/j/k/l` 的含义在某些场景下冲突

`docs/tui/interaction.md` 里：

- pinned 的 fixed viewport 用 `C-a Alt-h/j/k/l` 平移 offset
- 浮动层又用 `C-a Alt-h/j/k/l` 移动浮窗位置

如果当前焦点正好是一个 **floating + fixed + pinned** 的 viewport，这套键位到底是移动“视角 offset”，还是移动“浮窗位置”，目前没有定义优先级。

这会直接造成实现歧义。建议明确：

- 先移动浮窗，还是
- 先平移 viewport 内容，还是
- 需要进入单独的“move window”子模式

## 还需要收口的点

### 1. `last_attached` 作为 layout 排序条件，产品语义有点危险

它确实能让“最近用过的 terminal”更容易被选中，但副作用是：

- layout 恢复结果会受最近浏览行为影响
- 不同客户端的 attach 行为会干扰同一份 layout 的回放

对于 `save-layout` 生成的模板，`_hint_id` 已经足够强了；对通用手写 layout，我反而建议更偏静态规则，而不是引入“最近 attach”这种动态信号。

### 2. 自动 tag 的语义还停留在 `verification.md`

`ws=<name>` / `tab=<name>` 到底是：

- 创建时上下文（出生证明）
- 还是当前位置（实时地址）

这件事在 `verification.md` 提了，但还没进入正式规格。这个要尽快定，不然 Picker 搜索和 `save-layout` 语义会继续摇摆。

### 3. `rendering.md` 还是写得太像“已定方案”

现在有 `verification.md` 做缓冲，这很好；但 `rendering.md` 主文档仍然容易让人误以为 batching 方案已经被验证过。

建议把 P0 假设在正文里显式标一下，比如：

- “目标方案”
- “需先通过 V1/V4/V5 验证”

这样实现者不会把它当成无条件真理。

### 4. 命令模式清单还没同步

新文档里已经出现了这些命令或能力：

- `:tag`
- `:untag`
- `:list-orphans`
- `:edit-layout`
- `:delete-layout`

但 `docs/tui/interaction.md` 的命令模式列表还没同步完整，容易让人以为这些不是正式接口。

## 这版我的结论

如果上一版是 **8/10**，这一版我会给 **8.8/10**。

原因很简单：

- 大方向没变，而且是对的
- 之前我提的大部分结构性问题，这次都认真补了
- 文档已经开始像“系统规格”而不只是“产品构想”

但在进入实现前，我还是建议先修完上面 5 件事：

1. 修掉 `C-a [` 快捷键冲突
2. 统一 `Restart` 语义
3. 修正 tag 匹配排序与 AND 语义的矛盾
4. 明确 `readonly` 下 `Ctrl-C` 的规则
5. 明确 floating+fixed+pinned 时 `Alt-h/j/k/l` 的优先级

这 5 件改完，这套方案就基本可以进实现了。
