# termx TUI 当前状态

状态：Direction Reset
日期：2026-03-25

---

## 1. 当前结论

termx TUI 当前不能继续沿最近一段时间的“modern shell / modal / card / deck / rail”方向推进。

原因不是这些实现完全没价值，而是它们已经开始反客为主：

- modal 与 chrome 的视觉层次做得越来越完整
- 但主工作台没有先回到真正可用的 `terminal-first workbench`
- 结果是界面“看起来像产品”，实际却还不像一个能持续工作的 TUI

因此，从本文件开始，主线正式重置为：

1. 主界面必须是 `pane-first`
2. pane 必须首先是 terminal 的真实观察/操作表面
3. overlay 只能覆盖在工作台之上，不能替代工作台主体
4. floating / tiled / z-order / clipping / focus 才是当前主工作台的第一优先级
5. modal、美化、颜色、性能优化全部后置

---

## 2. 已确认可保留的部分

下面这些成果继续保留，不回退：

- `workspace / tab / pane / terminal` 四个主概念
- `owner / follower` 共享 terminal 语义
- `connect` 作为 pane 与 terminal 的连接关系
- startup / restore / runtime bootstrap 主线
- intent / reducer / runtime 的分层方向
- overlay 的领域模型与动作模型
- 现有 E2E harness 和 Go 测试骨架

这些部分依然是新主线的地基。

---

## 3. 已确认偏离主线的部分

下面这些东西不再作为主工作台方向继续扩展：

- 把主工作台实现成 `card / deck / rail / dashboard`
- 用大量 summary/context panel 替代真实 pane surface
- 让 overlay/modal 视觉优先级高于工作台主体
- 以 renderer 字段可见性断言代替产品级可用性推进
- 围绕 `modern shell chrome` 做长时间碎片化收口

这些实现可以保留为局部资产或过渡代码，但不再代表最终产品方向。

---

## 4. 新主线口径

### 4.1 工作台优先级

后续实现优先级固定为：

1. tiled pane surface
2. split layout projection
3. floating compositor
4. overlay on top of workbench
5. picker / manager / prompt 接回真实工作台
6. 颜色、性能、残影、细节 polish

### 4.2 主界面职责

主界面必须先做到：

- 启动即进入可输入 shell pane
- split 后两个 pane 都是真实 terminal 表面
- active pane 一眼可见
- floating pane 真正叠放在 tab 主内容区上
- overlay 打开时，底下工作台仍然可辨认

### 4.3 明确禁止

禁止再出现以下方向漂移：

- “先把 modal 做漂亮，再回头补工作台”
- “先把主壳说明 panel 收口，再补 pane surface”
- “先做字段可见性和 chrome 文案，再补真实交互”
- “主界面主要靠 summary 解释当前状态，而不是靠 pane 布局本身表达”

---

## 5. 旧版参考资产的正确用法

`deprecated/tui-legacy/` 不是禁区，而是工作台层的重点参考区。

后续允许直接参考的旧资产包括：

- pane frame 与 title bar 的产品布局
- tiled split 的空间分配
- floating rect / z-order / clipping / overlap
- composed canvas / damage / incremental redraw 的思路
- 与这些工作台能力强绑定的 E2E 场景

后续明确不回收的旧资产包括：

- 大一统 `Model`
- 领域状态、运行时状态、渲染缓存混在一起的结构
- 补丁式输入分发和 ad-hoc 过程状态

---

## 6. 下一阶段工作块

### 第一块：terminal-first tiled compositor

目标：

- 默认启动即得到真实可工作的单 pane
- split 后保持 pane frame、focus 和 terminal surface 可直接使用

必须包含：

- 单 pane surface
- 双 pane / 多 pane split surface
- active/inactive pane frame
- 最小 header/footer
- 与 terminal snapshot 的真实贴合

### 第二块：floating compositor

目标：

- 让 floating 真正成为工作台层的一部分，而不是右侧摘要卡

必须包含：

- floating rect
- z-order
- clipping
- move / resize / raise / lower / center
- tiled 与 floating 焦点切换

### 第三块：overlay 盖板层

目标：

- overlay 回到辅助层，不再替代主体

必须包含：

- mask / backdrop
- 居中 modal
- 关闭后不残影
- 底层 workbench 仍可辨认

---

## 7. 测试口径重置

后续每一轮 TUI 推进，验证重点必须先看这些产品级问题：

- 是否已经更接近“可以直接在里面工作”
- 是否让 pane surface 更完整
- 是否让 floating / overlay 更像真实层叠关系
- 是否补上对应 E2E，而不是只补 renderer 断言

字段可见性测试、chrome 文案测试、细粒度 renderer 断言只能作为辅助，不能再当主线进展。

---

## 8. 当前阶段结论

当前阶段不再定义为“modern shell 收口期”，而定义为：

- `工作台方向纠偏期`
- `terminal-first renderer 重启期`

接下来所有文档、编码、测试和状态汇报，都必须围绕这条新主线展开。

---

## 9. 第 210 轮 TDD

这一轮开始把新文档主线真正落回代码，不再继续扩展 single/split 的 summary rail，而是直接把默认 modern 的主工作台压回 `terminal-first tiled compositor`：

1. single workbench 默认主路径去掉右侧说明栏
   - `tui/runtime_modern_renderer.go`
   - 现在 single pane 在默认 modern 路径下直接使用全宽 pane canvas
   - header / context bar / footer 继续负责最小导航信息
   - 不再把右侧 `Workbench / Context` rail 当成主界面一部分
2. split workbench 默认主路径去掉 layout / panes / context 侧栏
   - 默认 modern split 现在直接把宽度让给两个 pane surface
   - 不再为 `Layout / Panes / Context / Action` 这些说明栏切走正文宽度
   - split 仍然保留现有 pane frame、ANSI 边框和 terminal body
3. 对应 E2E 护栏迁移到“pane surface 优先”
   - `tui/runtime_test.go`
   - 单 pane 与 split 的默认 modern E2E 不再要求 `WORKBENCH / CONTEXT / ACTIVE / ROUTE / LAYOUT / PANES` 这类 rail 文本
   - 改为锁：
     - 顶栏/上下文栏导航仍在
     - pane title / terminal body / ANSI 边框仍在
     - 主体不再出现 single/split 侧栏

本轮验证命令：

- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./tui -run 'Test(E2ERunScenarioDefaultModern(SplitWorkbenchUsesFullWidthPaneCanvas|SingleWorkbenchKeepsTerminalSurfacePrimary|TopChromeSummarizesWorkspaceTabsAndContext)|RunOrchestratesStartupPlanBootstrapAndSessionLifecycle)' -count=1`
- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./tui -count=1`
- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./... -count=1`

当前阶段推进结果：

- 默认 modern 的 single/split 主体已经不再被说明栏持续切开
- pane surface 开始重新成为默认首屏的主要面积
- 下一块应继续推进：
  - floating compositor
  - z-order / clipping / overlap
  - overlay 作为真正覆盖在工作台上的辅助层

---

## 10. 第 211 轮 TDD

这一轮把上一轮留下的“实现已改、测试口径还停在旧侧栏时代”的尾巴一次性收口，并顺手修复真正的 split/floating 鼠标命中缺陷：

1. 工作台 pane 标题点击从“按行唯一标题”升级为“标题文本 + X 坐标”
   - `tui/bt/mouse_hit.go`
   - 之前 split pane 标题出现在同一行时，`clickedWorkbenchPaneID` 会因为一行里有两个标题而返回失败
   - 现在优先按鼠标 `X` 命中标题区间，再回退到旧的单标题行语义
   - 这让 split / floating / mixed 的 pane title click 都能稳定命中具体 pane
2. 工作台鼠标相关测试切到真实布局
   - `tui/bt/intent_mapper_test.go`
   - `tui/runtime_test.go`
   - split pane 点击测试不再假设标题分成两行，而是直接在同一条 title bar 上按 `X/Y` 点击目标 pane
   - floating / mixed floating 的标题点击 E2E 也统一补上 `X` 坐标
3. renderer 测试口径同步到 terminal-first 主线
   - `tui/runtime_renderer_test.go`
   - 不再要求 single/split 默认 modern 路径出现 `WORKBENCH / CONTEXT / ACTIVE / ROUTE / LAYOUT / PANES` 等 side rail
   - 改为锁定：
     - 顶部最小 chrome 仍在
     - path/context bar 仍在
     - pane frame / ANSI / terminal body 仍在
     - single/split 主体保持全宽 pane canvas

本轮验证命令：

- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./tui -count=1`
- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./... -count=1`

当前阶段推进结果：

- 默认工作台的测试和实现主线重新一致，不再一边做全宽 pane、一边让测试继续卡在旧 rail 语义
- split/floating pane title 的鼠标切焦点具备了继续扩展真实 UI 交互的基础
- 下一阶段可以继续进入：
  - floating compositor 的产品化收口
  - z-order / clipping / overlap
  - overlay 盖板与工作台的真实叠放关系

---

## 11. 第 212 轮 TDD

这一轮把 `floating compositor` 的默认 modern 主路径真正往产品态推进，不再让右侧 `deck / rail` 继续替代主工作台：

1. pure floating 默认主路径改为全宽 composited canvas
   - `tui/runtime_modern_renderer.go`
   - 纯 floating 工作台不再切出右侧 `Floating / Context / Window Deck`
   - 主体直接使用现有 `renderWorkbenchCanvas(...)` 的叠放结果
   - 额外保留一条最小 `floating status strip`，只提示：
     - floating 数量
     - active pane
     - top pane
     - z-order
     - offscreen recall / center
2. mixed workbench 默认主路径改为 detached strip + full canvas
   - mixed 不再右侧展开 `Mixed / Panes / Context / Window Deck`
   - 保留一条 detached floating strip 作为“当前存在浮窗”的轻提示
   - 下方主体直接回到 tiled + floating 的真实合成画布
3. floating 相关 E2E 与 renderer 口径同步到新主线
   - `tui/runtime_test.go`
   - `tui/runtime_renderer_test.go`
   - 不再要求 `WINDOW DECK / FLOATING / CONTEXT / MIXED / PANES` 这类旁侧说明栏
   - 改为锁：
     - top chrome/context 仍能说明当前 active/top/float count
     - floating status strip / detached strip 可见
     - overlapping bodies 仍保留
     - offscreen recall / z-order 信息仍可见

本轮验证命令：

- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./tui -count=1`
- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./... -count=1`

当前阶段推进结果：

- 默认 modern 的 single / split / floating / mixed 四条主工作台路径已经都不再把右侧说明栏当主体
- floating compositor 现在真正开始以“画布叠放结果”而不是“旁侧 deck 摘要”表达自身
- 下一块应继续推进：
  - overlap / clipping 的产品态细化
  - overlay 真正覆盖在 composited workbench 之上
  - 渲染稳定性与残影清理

---

## 12. 第 213 轮 TDD

这一轮把 `overlay on top of workbench` 往前推进了一大步，重点不是继续雕 modal 文案，而是让 overlay 真正覆盖“当前整个工作台 body”：

1. overlay backdrop 从 pane canvas 升级为整块 workbench body
   - `tui/runtime_modern_renderer.go`
   - 之前 overlay 只基于 `renderWorkbenchCanvasLines(...)` 洗出 backdrop
   - 这会导致 floating status strip、mixed detached strip 在 overlay 打开时直接消失
   - 现在 overlay backdrop 直接基于 `renderWorkbench(...)` 的完整 body 结果
   - 因此 single / split / floating / mixed 当前工作台 body 的真实结构都会被一起盖住
2. overlay backdrop chrome 开始描述 floating / mixed 的真实工作台状态
   - `workbench paused` 行现在能区分：
     - floating layer：`floating N  •  top ...`
     - mixed/tiled with detached floating：`detached N`
   - workspace/tab/layer 行也会同步带上 floating/detached 语义
3. 增加 floating / mixed 的 overlay 产品级验证
   - `tui/runtime_renderer_test.go`
   - `tui/runtime_test.go`
   - 新护栏覆盖：
     - floating help overlay 打开时保留 floating status strip 语义
     - mixed help overlay 打开时保留 detached strip 语义
     - overlay 关闭后工作台恢复干净，没有残留 shadow/backdrop 文本

本轮验证命令：

- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./tui -count=1`
- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./... -count=1`

当前阶段推进结果：

- overlay 不再只像“盖在某个 pane 上”，而是开始真正盖在当前整个 composited workbench body 上
- floating / mixed 的 overlay backdrop 已经能保留当前工作台结构语义
- 下一块应继续推进：
  - overlap / clipping 的产品态细化
  - overlay close / resize / viewport 变化下的残影治理
  - manager / picker / prompt 在 floating/mixed backdrop 下的更多真实回归

---

## 13. 第 214 轮 TDD

这一轮把主线重新拉回“可工作的 TUI 产品骨架”，不再继续让 modern chrome 维持 panel / chip 主导的观感，同时把 overlay 内帮助恢复链补齐：

1. help overlay 支持临时盖住其他 overlay，并在关闭后恢复
   - `tui/domain/types/types.go`
   - `tui/app/reducer/reducer.go`
   - `tui/bt/intent_mapper.go`
   - help 现在可以从 `terminal manager / workspace picker / terminal picker / layout resolve / prompt` 内直接通过 `?` 打开
   - 如果 help 是从其他 overlay 内打开，关闭 help 会先恢复原 overlay，而不是直接回到工作台
   - prompt / picker / manager 的 overlay data 也会随着恢复链一起深拷贝，避免 reducer clone 污染
2. 默认 modern 主 chrome 改回更接近 legacy 的顶栏/状态栏/底栏骨架
   - `tui/runtime_modern_renderer.go`
   - 顶栏不再以 chip 组合为主，而是回到：
     - 左侧 `workspace + tab strip`
     - 右侧 `pane / term / float` 摘要
   - 第二行改为工作台状态行：
     - 左侧 pane path
     - 右侧 runtime / role / floating summary
   - 底栏右侧改成短摘要：
     - `pane title`
     - `▣ tiled / ◫ float`
     - `● run / ○ exit / ◌ wait`
     - `owner/follower`
   - compact 宽度下优先保留 active tab 可见性，不再把 tab strip 本身压没
3. 补齐对应的 renderer / E2E 回归
   - `tui/runtime_renderer_test.go`
   - `tui/runtime_test.go`
   - 新护栏覆盖：
     - overlay 内 `?` 打开 help
     - help 关闭后恢复 manager / workspace picker / prompt
     - 默认 modern 的顶栏/状态栏/底栏开始向 legacy 体验靠拢
     - split / floating / mixed 的鼠标聚焦、tab 点击、offscreen recall 仍保持可用

本轮验证命令：

- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./tui -count=1`
- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./... -count=1`

当前阶段推进结果：

- overlay 不再只是“能打开帮助”，而是具备了对其他 overlay 的临时覆盖与恢复能力
- 默认 modern 首屏已经从信息面板式壳子，开始回到更接近 legacy 的 `top chrome + workbench + footer` 产品骨架
- pane canvas、floating compositor、overlay 背板这些已完成的主工作台能力继续保留

下一块应继续推进：

- 继续把 pane title chrome、split 结构、floating frame 的视觉语言往 legacy 收拢
- 让 workspace/tab/pane 鼠标命中区域和真实边框表达更统一
- 最后再处理颜色、性能、重叠渲染和残影优化

---

## 14. 第 215 轮 TDD

这一轮继续推进“真实工作台 renderer 收口”，重点不再放在 overlay 文案，而是直接把 single/split/floating/mixed 的 pane/frame 语言拉回更接近 `deprecated/tui-legacy` 的可工作骨架：

1. pane/frame 边框回拉到 legacy 风格的单线盒模型
   - `tui/runtime_modern_renderer.go`
   - tiled 与 floating 不再靠两套重字符集区分主工作台层次
   - active/inactive、tiled/floating 的差异主要改由颜色语义、状态 badge、strip 提示承担
   - 这样 split 与 floating 同屏时，视觉语言更统一，也更接近旧版可用工作台的布局感
2. floating 与 mixed 的顶部条带改成更短、更像工作台信号
   - floating strip 改为：
     - `◫ float N`
     - `active xxx`
     - `[top] xxx`
   - mixed detached strip 改为：
     - `◫ detached`
     - `[active] / [top]` 浮窗标签
     - 右侧只保留 `float N`
   - 不再继续使用偏说明性的 `Detached windows / floating N / top xxx` 旧文案组合
3. empty / waiting / exited pane 的正文改成更接近产品态的短句
   - `no terminal connected`
   - `waiting for connect`
   - `process exited`
   - action line 统一改成短操作提示：
     - `n new`
     - `a connect`
     - `r restart`
     - `m manager`
4. 对应 renderer / E2E 护栏同步到新的工作台骨架
   - `tui/runtime_renderer_test.go`
   - `tui/runtime_test.go`
   - 新护栏重点锁定：
     - pane 标题栏仍可读，并开始呈现 `┌─ title` 的 legacy 风格
     - split / floating / mixed 继续保持 full canvas，不回到 rail/deck 主导
     - floating / detached strip 使用新的短状态语义
     - empty / waiting / exited pane 的产品文案与动作提示同步更新

本轮验证命令：

- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./tui -count=1`
- `export PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH"; go test ./... -count=1`

当前阶段推进结果：

- 主工作台的 pane/frame 语言进一步从“工程态拼装”回到“可工作的终端工作台”
- split / floating / mixed 的视觉骨架开始统一，不再让不同布局像三套不同产品
- mixed 的 waiting / exited / empty pane 文案更接近用户在真实工作流中能直接理解的表达

下一块应继续推进：

- pane title bar 的命中区域、active 高亮和真实边框交互进一步统一
- overlay 盒模型和 backdrop 继续往旧版的可辨认叠层体验靠拢
- 性能、颜色、重叠渲染优化继续放在全局 TODO 的后段处理
