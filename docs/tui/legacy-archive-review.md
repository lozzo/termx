# termx Legacy TUI 整理稿

状态：供过目
日期：2026-03-23

这份文档只做 3 件事：

1. 盘点 `deprecated/tui-legacy/` 里到底留下了什么
2. 提炼旧版里已经比较稳定的产品结论
3. 把旧资产按“可继承 / 可参考 / 应重写”分层，方便后续重构

---

## 1. 当前仓库状态

当前工作树里，原来的主线 TUI 代码和文档已经被你移出主路径：

- 旧代码归档到：`deprecated/tui-legacy/`
- 原 `tui/` 目录当前为空
- 原 `docs/tui/` 主文档当前已删除，尚未重建

从当前 `git status` 看，下面这些文件是“已从主路径删除、尚未提交”的状态：

- `tui/*.go`
- `docs/tui/*.md`
- `e2e_real_test.go`
- `tui_e2e_test.go`

因此，`deprecated/tui-legacy/` 现在可以视为：

- 一份完整的历史快照
- 一份产品语义和测试资产参考库
- 不是应该直接搬回来的新主线实现

---

## 2. 归档内容总览

`deprecated/tui-legacy/` 当前分成 3 块：

1. `docs/`
   - 产品、交互、架构、路线图、todo、场景、线稿、e2e 计划
2. `pkg/`
   - 旧版 TUI 的主要实现和单元测试
3. `root-tests/`
   - 挂在仓库根包下的 legacy e2e / real-program 测试

其中 `pkg/` 代码总量约 `28k` 行，核心体量集中在：

- `model.go`：`8623` 行
- `model_test.go`：`10443` 行
- `render.go`：`1697` 行
- `picker.go`：`1265` 行
- `workspace_state.go`：`485` 行
- `input.go`：`485` 行
- `terminal_manager.go`：`539` 行

这个分布本身已经说明旧版问题核心：

- 功能不是没有做出来
- 而是大量能力被压进了超大 `Model`
- 输入、状态、副作用、渲染缓存已经明显耦合

---

## 3. 文档资产盘点

### 3.1 主文档

`deprecated/tui-legacy/docs/` 里最重要的 6 份主文档是：

- `product-spec.md`
- `interaction-spec.md`
- `architecture-refactor.md`
- `current-status.md`
- `implementation-roadmap.md`
- `refactor-roadmap.md`

这些文档已经把旧版收敛到一套比较明确的统一口径：

- 用户主概念只有 `workspace / tab / pane / terminal`
- `pane` 是工作位，`terminal` 是运行实体
- `close pane` 不等于 `stop terminal`
- `stop terminal` 后原位置保留为 `saved pane`
- `fit / fixed` 是显示属性
- `owner / follower` 是共享连接关系
- 不建议继续在旧结构上打补丁

### 3.2 辅助文档

辅助文档包括：

- `wireframes-v2.md`
- `scenarios.md`
- `e2e-plan.md`
- `todo.md`

它们的价值主要在：

- 还原当时 UI 和交互想解决的问题
- 补足 e2e 覆盖意图
- 帮后续重写时校验“有没有漏掉用户场景”

### 3.3 文档结论

旧文档里真正值得继承的，不是具体页面长什么样，而是下面两层：

1. 产品术语已经收口
2. 重构方向已经说对了

也就是说，文档层最该保留的是“概念和边界”，不是“旧实现细节”。

---

## 4. 代码资产盘点

### 4.1 底层与边界定义

这些文件相对独立，说明旧版并不是一团完全无法分拆的代码：

- `client.go`
  - 定义了 `Client` 接口，是旧版 TUI 与 `protocol.Client` 的解耦层
- `layout.go`
  - tiled pane 布局树、分割、相邻 pane 查找、边界调整
- `layout_decl.go`
  - layout yaml 的导入导出、匹配策略、等待 pane / 复用逻辑
- `workspace_state.go`
  - workspace 持久化与恢复结构
- `connection_state.go`
  - shared terminal 的 owner/follower 快照归一化
- `prefix_input.go`
  - key/event 共享的前缀输入归一化

这批文件里有不少“可以抽象保留”的东西。

### 4.2 交互层实现

这几块对应用户可见功能面：

- `picker.go`
  - terminal picker、创建、attach、layout resolve
- `terminal_manager.go`
  - terminal pool 浏览与操作
- `workspace_picker.go`
  - workspace 切换和管理
- `input.go`
  - keyboard / mouse / paste / raw input 入口

这批文件体现了大量产品路径，但实现方式依赖旧版大 `Model`，不能直接搬。

### 4.3 核心耦合区

旧版最大的问题集中在两处：

- `model.go`
- `render.go`

其中 `model.go` 实际同时承担了：

- Bubble Tea `Update/View`
- prefix mode 状态机
- pane/tab/workspace 业务状态变更
- prompt / picker / manager 状态
- terminal attach / create / kill / resize 等副作用触发
- render ticker、render dirty、backpressure、统计日志

而 `render.go` 又把下面这些揉在一起：

- pane frame/chrome 渲染
- viewport/snapshot/vterm 内容绘制
- composed canvas
- 局部重绘和 damage 计算
- 多层缓存和 dirty 传播

这就是旧版最不应该原样复用的地方。

---

## 5. 旧版已经验证过的产品结论

下面这些结论，在旧文档和旧代码里是一致的，可以直接视为重构输入，而不是再重新发明：

### 5.1 概念层

- 主概念只有 `workspace / tab / pane / terminal`
- `view / viewport / panel` 只该留在实现层
- `pane` 默认不应被当成独立命名对象
- pane 标题默认展示 terminal 真名

### 5.2 生命周期

- 默认启动应直接进入可工作的 workspace
- 默认应有一个可输入 shell pane
- `close pane` 只关闭视图入口，不 kill terminal
- `stop terminal` 需要确认，并把 pane 留成 `saved pane`
- `exited pane` 保留历史，可 restart 或重新 attach

### 5.3 共享 terminal

- 一个 terminal 可被多个 pane attach
- terminal 的真实 PTY size 只有一份
- `owner / follower` 和 `fit / fixed` 必须分开
- `owner` 决定谁能提交 resize
- `follower` 只观察

### 5.4 UI 分工

- 顶栏负责 workspace/tab/global summary
- pane 标题栏负责 terminal 名称和关系状态
- 底栏左侧只放当前 mode 快捷键
- 底栏右侧只放当前焦点的极简摘要
- picker / help / prompt / manager 应该是 overlay，而不是散落的整屏页

这些东西不需要在重构阶段再次争论，可以当成现成约束。

---

## 6. 为什么旧版不能直接回收

旧版已经不只是“代码有点乱”，而是架构边界已经实质性混合。

### 6.1 `Pane` / `Viewport` 混合过多职责

旧版 `Viewport` 和 `Pane` 同时承载了：

- terminal 绑定
- terminal metadata
- viewport mode / offset / readonly / pin
- stream 生命周期
- vterm / snapshot
- render cache / dirty 标记 / row damage

这意味着：

- 领域状态和渲染状态缠在一起
- 任何交互改动都容易碰到渲染副作用

### 6.2 输入系统虽然开始收口，但还停在过渡态

旧版已经出现下面这些正确方向：

- `prefix_input.go`
- `dispatchPrefixInput`
- `prefixIntent`
- `xxxRuntimePlan`

但本质上仍然是：

- key 路径和 event 路径都还在
- 鼠标路径还在 `input.go`
- 真正统一的 `intent -> reducer -> effect` 结构没有成型

### 6.3 shared terminal 规则仍依赖补丁式状态

虽然有了 `connection_state.go`，但底层 ownership 仍然落回了 `Pane.ResizeAcquired` 这样的字段。

这说明：

- 产品规则已经对了
- 模型化还没彻底完成

### 6.4 render cache 已经侵入业务逻辑

旧版里 render dirty、局部重绘、frame key、damage 计算都已经和业务更新强绑定。

继续在这套结构上加东西，很容易出现：

- 残影
- 闪烁
- 串屏
- 修一个场景破另一个场景

---

## 7. 按价值分层：哪些能继承

### 7.1 可直接继承思路，建议优先保留

这些资产适合当新主线的第一批输入：

- `deprecated/tui-legacy/docs/product-spec.md`
  - 作为产品词汇表和生命周期规则来源
- `deprecated/tui-legacy/docs/interaction-spec.md`
  - 作为焦点模型、布局模型、快捷键分工来源
- `deprecated/tui-legacy/docs/architecture-refactor.md`
  - 作为重构边界说明来源
- `deprecated/tui-legacy/pkg/client.go`
  - `Client` 接口抽象值得保留
- `deprecated/tui-legacy/pkg/layout.go`
  - 布局树能力可直接迁移或轻改迁移
- `deprecated/tui-legacy/pkg/layout_decl.go`
  - layout schema 和解析流程可复用
- `deprecated/tui-legacy/pkg/workspace_state.go`
  - workspace state schema 和导入导出值得保留

### 7.2 可继承语义，但实现建议重写

这些东西有明确产品价值，但实现不要直接搬：

- `deprecated/tui-legacy/pkg/connection_state.go`
  - 保留 owner/follower 规则，不保留 `ResizeAcquired` 式落点
- `deprecated/tui-legacy/pkg/prefix_input.go`
  - 保留 key/event 归一化方向，不保留旧 dispatch 链路
- `deprecated/tui-legacy/pkg/picker.go`
  - 保留 picker 产品路径和筛选逻辑，重写状态与渲染组织
- `deprecated/tui-legacy/pkg/terminal_manager.go`
  - 保留 manager 的职责边界，重写页面状态和 effect 层
- `deprecated/tui-legacy/pkg/workspace_picker.go`
  - 保留 workspace store/workspace switch 的产品能力，重写耦合实现

### 7.3 只建议当参考，不建议迁回

这些文件应该视为“历史样本”，不是迁移对象：

- `deprecated/tui-legacy/pkg/model.go`
- `deprecated/tui-legacy/pkg/render.go`
- `deprecated/tui-legacy/pkg/input.go`

理由很简单：

- 结构负债已经集中在这里
- 迁回来只会把旧问题带回主线

---

## 8. 测试资产怎么处理

### 8.1 单测

旧版单测体量很大，说明旧行为边界其实写得比较多。

最值得保留的是“行为意图”，不是测试文件原封不动复制：

- `connection_state_test.go`
- `layout_test.go`
- `layout_decl_test.go`
- `workspace_state_test.go`
- `workspace_picker_test.go`
- `model_test.go` 里跟 prefix / shared terminal / lifecycle 强相关的用例

建议后续做法：

- 先抽出纯逻辑层
- 再把对应测试意图迁到新结构

### 8.2 e2e

`root-tests/` 里的 e2e 很有价值，尤其是这些方向：

- floating overlay 残影
- floating z-order
- floating center/recall
- shared terminal 复用
- real-program 场景
  - `python3`
  - `vi`
  - `seq`

但这批测试现在都带了：

- `//go:build legacy_tui_archive`

说明它们已经被降级成归档参考测试，而不是当前主线测试。

建议后续处理方式：

1. 先按主题把旧 e2e 列成清单
2. 新 TUI 每恢复一条主路径，就迁一批对应 e2e
3. 不要先把所有 legacy e2e 机械搬回来

---

## 9. 建议的新主线继承顺序

如果后续要正式开始重构，建议按这个顺序吸收旧资产：

1. 先恢复文档主线
   - 至少先恢复产品规格、交互规格、重构边界三份文档
2. 先恢复纯模型和边界
   - `Client`
   - `Layout`
   - `WorkspaceState`
   - 新版 `ConnectionState`
3. 再做输入层
   - 直接按 `intent -> reducer -> effect` 起新结构
4. 再做最小可用渲染
   - 先保证主工作流，不先追旧版复杂缓存
5. 最后再逐步迁回 picker / manager / workspace switch / floating 高级能力

这个顺序的目的只有一个：

- 只继承旧版已经验证过的产品结论
- 不把旧版的大 `Model` 和渲染耦合一起带回来

---

## 10. 我对这份归档的结论

`deprecated/tui-legacy/` 不是废料，它至少保留了 4 类有价值的东西：

1. 已经收口过的产品语言
2. 已经证明过可工作的用户路径
3. 一批可抽离的边界模型
4. 一批说明复杂场景曾经踩过坑的测试样本

但它也明确不适合作为“直接恢复上线”的代码基底，因为：

- 架构债主要集中在 `model.go` / `render.go` / `input.go`
- shared terminal 和 render cache 的复杂度已经被补丁化
- 继续在旧实现上叠补丁，基本等于把问题重新带回主线

所以更准确的定位应该是：

- `deprecated/tui-legacy/docs/`：保留为产品和迁移参考
- `deprecated/tui-legacy/pkg/`：拆着用，按层吸收
- `deprecated/tui-legacy/root-tests/`：作为新主线 e2e 回迁清单

---

## 11. 建议你过目时重点看什么

如果你只是想先快速确认“旧版哪些还值得要”，建议你优先看这几份：

1. `deprecated/tui-legacy/docs/product-spec.md`
2. `deprecated/tui-legacy/docs/interaction-spec.md`
3. `deprecated/tui-legacy/docs/architecture-refactor.md`
4. `deprecated/tui-legacy/pkg/layout.go`
5. `deprecated/tui-legacy/pkg/layout_decl.go`
6. `deprecated/tui-legacy/pkg/workspace_state.go`
7. `deprecated/tui-legacy/pkg/connection_state.go`

如果你想确认“为什么不能直接把旧版搬回来”，重点看这几份：

1. `deprecated/tui-legacy/pkg/model.go`
2. `deprecated/tui-legacy/pkg/render.go`
3. `deprecated/tui-legacy/pkg/input.go`

---

## 12. 后续可直接接的动作

你过目完这份整理稿后，后面我可以直接接下面三种工作中的任意一种：

1. 继续把旧文档提炼成新的 `docs/tui/` 主文档骨架
2. 先从 `layout / workspace_state / client / connection_state` 起一个新的最小 TUI 核心骨架
3. 先把 legacy 测试整理成“新主线迁移清单”
