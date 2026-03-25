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
