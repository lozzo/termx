# TUI 会话交接摘要

日期：2026-03-25

## 1. 当前目标

当前主线不是继续修旧 TUI，而是：

- 已归档当前失败重写主线
- 基于新的产品定义重新建立 TUI
- 先把产品口径、技术边界、线框图和实现计划固定
- 再进入按任务执行

## 2. 关键资产路径

### 产品定义

- [TUI 产品定义设计](/home/lozzow/workdir/termx/docs/superpowers/specs/2026-03-25-tui-product-definition-design.md)

### 实现计划

- [TUI 第一阶段重建实现计划](/home/lozzow/workdir/termx/docs/superpowers/plans/2026-03-25-tui-phase1-rebuild.md)

### 线框图目录

- [线框图 README](/home/lozzow/workdir/termx/docs/tui-wireframes/README.md)
- [线框图索引](/home/lozzow/workdir/termx/docs/tui-wireframes/00-index.md)

### 参考区

- [旧版 TUI 参考区](/home/lozzow/workdir/termx/deprecated/tui-legacy)
- [本轮放弃重写归档区](/home/lozzow/workdir/termx/deprecated/tui-reset-2026-03-25)

## 3. 已确认的产品口径

### 总体定位

- `termx` 对外定义为“更现代的终端复用器”
- 但底层本体是全局 `terminal pool`
- `TUI` 是 terminal pool 的第一方界面
- 使用体验优先接近 `tmux / zellij`

### 顶层形态

- 默认入口是 `Workbench`
- 另有独立 `Terminal Pool` 页面
- 高频局部动作通过 `picker / dialog / prompt / help`
- 第一阶段不做 `Settings` 页面

### 核心对象

- `terminal` 是持续运行、可复用的实体
- `pane` 是工作位，不是 terminal 本体
- `workspace` 是全局组织视图，不是 session 所有权模型
- `tab` 第一阶段先等同传统 window
- `floating pane` 是完整 pane 的另一种摆放方式

### pane 状态

第一阶段只保留三种主状态：

- `live pane`
- `exited pane`
- `unconnected pane`

定义：

- `exited pane`：terminal 对象仍存在，但状态为 exited
- `unconnected pane`：当前没有 terminal 对象与 pane 绑定

### kill / remove / restart

- `kill terminal`
  - terminal 对象保留
  - 状态进入 `exited`
  - 所有绑定 pane 一起进入 `exited pane`
- `remove terminal`
  - terminal 对象从 pool 中移除
  - 所有绑定 pane 变成 `unconnected pane`
- `R`
  - 对原 terminal 对象执行 restart
  - 不是新建替换另一个 terminal

### tab 最后一个 pane

- tab 中最后一个 pane 消失时，不自动关闭 tab
- 立即补成一个 `unconnected pane`
- 只有显式关闭 tab，这个 tab 才真的退出

### shared terminal / owner / follower

- 一个 terminal 同时最多一个 owner
- 新 pane 连接已有 terminal 时：
  - 无 owner 则自动成为 owner
  - 否则默认 follower
- owner 关闭或解绑后，不自动迁移
- 其他 pane 需显式执行 `Become Owner`
- `Become Owner` 直接抢占 owner，原 owner 自动降为 follower

### remove 确认与远端 notice

- 非 shared terminal 可以直接 remove
- shared terminal 必须确认
- 远端 remove 只对当前可见受影响 terminal 弹通用 notice
- notice 要写明 terminal 名称
- 不显示操作者身份

### metadata / tags

- 第一阶段就做
- 先作为 terminal 原生信息
- 主要用于显示、编辑、搜索
- 暂不扩展成复杂规则系统

### help

- 第一阶段就做
- 不是纯快捷键表
- 采用分组式帮助层

### floating pane

- 允许拖出主视口
- 但必须始终保留左上角拖动锚点在大窗口内
- 最近操作的 floating pane 自动置顶
- 不做手动 raise/lower
- 必须支持“呼回并居中”

## 4. 已确认的显示与恢复规则

### 全屏程序

- `htop / vim / less / alt-screen` 是第一阶段硬要求
- 优先保证内容正确、状态连续、切回后可恢复

### stream / snapshot

- 平时以 `stream` 为主
- 失步或恢复时以 `snapshot` 纠偏
- 恢复完再继续 `stream`
- 恢复期间继续接流，但不重复发起恢复

### live session 保持

- 只要 pane 或页面仍在当前界面模型里，对应 terminal 的 live session 与流订阅就保持
- 不因失焦、切页、遮挡自动降级成 snapshot-only

### 主题同步

- 宿主终端主题变化时，termx 内部也要同步变化
- 正文、旧 snapshot、chrome 都要跟着默认色和 palette 重新解释

### 裁切与观察偏移

- pane 小于 terminal 时，默认左上角裁切
- 支持短暂 `viewport move` 模式
- 支持鼠标拖拽移动内部观察位置
- 被裁切侧显示 `+`
- pane 大于 terminal 时，空白区显示小圆点
- 观察偏移要跟 workspace 一起保存恢复
- 宽字符、emoji、powerline 在边界处宁可留空，也不显示半个字符

## 5. 已确认的技术边界

### terminal 状态模型

- 以 `stream` 为主维护本地终端状态
- 客户端维护长期存活的本地 terminal 状态模型
- `snapshot` 仅用于纠偏与恢复

### terminal 与 pane 的状态归属

- terminal 持有一份主状态
- pane 不复制 terminal 正文
- pane 只保存：
  - 几何/布局位置
  - 焦点与浮动态
  - 内部观察偏移
  - 显示与连接状态

### shared 连接关系归属

- `owner / follower / attached pane ids` 这类 shared 连接关系归在 terminal 侧
- 第一阶段 UI 不强调展示其他客户端连接细节
- 但底层状态应保留这类信息

### reducer / runtime 分层

- reducer 只产出纯 effect 描述
- 不直接调用 daemon/service
- runtime 层统一执行 create/connect/kill/become-owner 等副作用

### 渲染链路

固定链路：

- `state`
- `screen snapshot`
- `canvas composition`
- `terminal output`

要求：

- 先整理最终可渲染输入
- renderer 不现场推理业务状态
- renderer 只按 screen snapshot 画 canvas

### 鼠标命中优先级

- `overlay`
- `floating`
- `tiled`
- `pane 正文`

## 6. 线框图目录当前状态

已建目录骨架：

- [docs/tui-wireframes](/home/lozzow/workdir/termx/docs/tui-wireframes)

已建场景文件：

- `01-workbench-default.md`
- `02-pane-unconnected.md`
- `03-pane-exited.md`
- `04-connect-dialog.md`
- `05-terminal-pool-overview.md`
- `06-terminal-pool-actions.md`
- `07-shared-terminal-owner-follower.md`
- `08-remove-terminal-shared.md`
- `09-floating-single.md`
- `10-floating-overlap.md`
- `11-help-overlay.md`
- `12-viewport-crop.md`
- `13-theme-sync.md`
- `14-remote-remove-notice.md`
- `15-tab-last-pane-closes.md`
- `16-metadata-tags-edit.md`

已建 flow 文件：

- `flows/01-workbench-primary-flow.md`
- `flows/02-terminal-lifecycle-flow.md`
- `flows/03-shared-terminal-flow.md`
- `flows/04-floating-flow.md`
- `flows/05-overlay-flow.md`

当前状态不是骨架了，而是已经补入首版正文，覆盖了：

- 默认 workbench 主画面
- unconnected / exited pane
- connect dialog
- Terminal Pool 三栏页面与动作页
- shared terminal 的 owner / follower
- shared remove 确认
- 单浮窗与多浮窗叠放
- help overlay
- viewport 裁切与偏移
- 主题同步
- remote remove notice
- tab 最后一个 pane 关闭
- metadata / tags 编辑
- 5 个主 flow

## 7. 下一步优先顺序

如果继续当前方向，建议按这个顺序推进：

1. 继续把线框图做深
   - 增加同一场景的更多前后对照图
   - 把极端边界案例单独拆文件
2. 拿线框图反推更细的实现任务
   - 先从 workbench / connect / terminal pool 三块拆分
3. 保持文档与产品口径同步
   - 一旦术语或交互变更，先改线框图再改实现计划
   - `08-remove-terminal-shared.md`
3. 再补共享 terminal / floating / viewport / help
4. 线框图稳定后，再回头微调 spec / plan
5. 最后再进入按 plan 执行 Task 1

## 8. 最近相关提交

- `f35c37c` `归档当前TUI重写并重置主线壳层`
- `8c9d2b0` `收紧TUI产品定义文档边界`
- `b474d56` `明确TUI计划运行时职责边界`
- `4800a5a` `同步TUI最新产品与技术定义`
- `6cd188a` `建立TUI线框图场景骨架`

## 9. 当前工作树注意事项

- 当前未提交变更不应包含本文件之外的 TUI 内容
- `AGENTS.md` 有用户自己的修改，默认不要动它
