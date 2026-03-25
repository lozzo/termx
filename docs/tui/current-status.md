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
