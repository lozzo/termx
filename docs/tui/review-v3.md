# TUI 设计三次评审

评审对象：当前 `docs/tui/*.md`

评审结论：**这一版已经基本收口，可以进入实现了。** 上一轮我提的几个硬问题，这次大部分都修掉了。剩下的问题主要从“模型冲突”降到了“文档同步”和“输入可实现性验证”。

## 这次修得很好的地方

### 1. 上一轮的 5 个核心问题，基本都处理了

- `C-a [` 冲突已修，浮窗降层改成了 `C-a _`，`docs/tui/interaction.md:225`
- `Restart` 语义已统一为“只有触发 restart 的 Viewport 绑定新 Terminal”，`docs/tui/model.md:164`
- tag 匹配与 AND 语义的矛盾已修，先过滤再排序，`docs/tui/layout.md:193`
- `readonly` 下 `Ctrl-C` 的规则已定死，且和 AI 中断场景一致，`docs/tui/model.md:490`
- floating + fixed + pinned 的键位优先级已明确，`docs/tui/interaction.md:202`

这说明方案作者这轮不是在“润色”，而是在认真消除规格冲突。

### 2. 自动 tag 的语义终于定了

`docs/tui/model.md:97` 直接把自动 tag 定义成“**出生证明，不是当前地址**”，这很好。这样：

- `save-layout` 的匹配基础稳定
- 多客户端下不用做 tag 同步
- Picker 再结合当前位置搜索即可

这是一个很关键的设计定稿点。

### 3. `rendering.md` 和 `verification.md` 的边界更健康了

`docs/tui/rendering.md:26`、`docs/tui/rendering.md:49`、`docs/tui/rendering.md:136`、`docs/tui/rendering.md:234` 都开始明确标“待验证”，这比上一版成熟很多。现在主文档不像是在偷偷把假设伪装成事实了。

## 还剩下的主要问题

### 1. 线稿和帮助页没有跟上最新规格

`docs/tui/wireframes.md:333` 的 help 浮窗仍然是旧版键位，没反映这些新能力：

- `C-a R` readonly
- `C-a Ctrl-h/j/k/l` fixed offset 平移
- `C-a ]` / `C-a _` 浮窗 z-order
- `:tag` / `:untag` / `:list-orphans`
- `:edit-layout` / `:delete-layout`

这不是模型问题，但已经是明显的文档漂移。建议把 wireframes/help overlay 同步一次，不然实现者会不知道该信哪份。

### 2. 新增的输入组合需要单独验证可实现性

这版加了不少比较“重”的组合键：

- `C-a Ctrl-h/j/k/l`
- `C-a Ctrl-←/→/↑/↓`
- `C-a Alt-h/j/k/l`
- `C-a Tab`

这些键在不同终端、平台、TTY 输入层里的可识别性不一定稳定，尤其是：

- `Ctrl-h` 常常等价于 Backspace
- `Alt-*` 在不同终端里可能表现成 ESC 前缀
- `Ctrl-Arrow` 支持并不统一

我建议把这件事正式加入 `verification.md`，作为一个独立验证项。现在 `verification.md` 偏重渲染和状态同步，但“按键能不能稳定收到”其实同样是 P0/P1 级风险。

### 3. `C-a _` 虽然不冲突，但可发现性一般

把“降到最底层”绑定到 `C-a _` 能解决冲突，但它有两个问题：

- 不够直觉
- 在不少键盘布局上不够顺手

这不是 blocker，但我会建议作者再想一下有没有更自然的按键，或者至少在 help/wireframe 里强化说明。

## 我现在的判断

如果上一版我给到 8.8/10，这一版我会给 **9.3/10**。

现在这套方案已经具备几个很重要的特征：

- 核心模型稳定
- 关键生命周期清楚
- 多写入规则清楚
- layout 恢复语义清楚
- 待验证事项有单独清单

对我来说，这已经不是“能不能实现”的问题了，而是“先实现哪一层、先验证哪几个假设”的问题。

## 建议进入实现前再补的最后两件事

1. 同步 `docs/tui/wireframes.md` 的 help/frame，避免文档漂移
2. 在 `docs/tui/verification.md` 新增“输入组合键可识别性”验证项

这两件补完，我认为就可以放心开做了。
