# termx Workspace / Layout 规格

状态：Draft v1

本文件定义 `workspace` 与 `layout` 的产品语义、关系和后续文件模型方向。

它解决的问题是：

- workspace 和 layout 到底分别是什么
- 哪个是模板，哪个是实体
- 启动、保存、恢复、派生时分别落在哪一层

---

## 1. 基本定义

## 1.1 Layout

layout 是模板。

它描述的是：

- pane 结构
- split 关系
- floating 结构
- 默认焦点建议
- terminal 匹配规则
- terminal 创建策略

layout 不代表某次真实运行现场。

它更像：

- blueprint
- template
- preset

## 1.2 Workspace

workspace 是实体。

它描述的是某一次真实工作现场，包括：

- workspace 名称
- 当前 tabs
- 当前 panes
- 当前 floating panes
- active tab / active pane / floating focus
- 当前附着到哪些 terminal
- 当前恢复状态

workspace 更像：

- live session
- runtime state
- working context

---

## 2. 两者关系

关系结论：

- layout = 模板
- workspace = 模板实例化后的真实现场

规则：

- 一个 workspace 可以从某个 layout 派生
- 多个 workspace 可以复用同一个 layout
- workspace 运行过程中发生的变化，不自动回写 layout
- 用户如果想沉淀可复用结构，应显式导出为 layout
- 用户如果想恢复当前现场，应保存 workspace state

---

## 3. 为什么必须分离

如果不分离，系统会混淆两件事：

- “我想复用一种布局和匹配策略”
- “我想回到刚才那个真实工作现场”

termx 又有一个更强的前提：

- terminal 由 server 托管
- workspace 只是附着和组织这些 terminal

因此：

- layout 负责说明“应该怎样组织 terminal”
- workspace 负责记录“这次实际上组织成了什么样子”

---

## 4. Workspace 的产品语义

workspace 对用户的感知更接近：

- zellij/tmux 的 session
- 某个项目、某个环境、某个值班任务的工作现场

用户会对 workspace 有以下期待：

- 能命名
- 能切换
- 能恢复
- 能保留 tab/pane/floating 上下文
- 能在重新进入 TUI 时继续 attach 原 terminal

---

## 5. Layout 的产品语义

layout 对用户的感知更接近：

- 启动模板
- 项目预设
- 常用工作面板组合

典型用途：

- “打开 backend 项目时给我 1 个 dev pane + 1 个 log pane + 1 个 floating htop”
- “如果找到 tag=backend 的 terminal 就 attach，否则 create”
- “prod 巡检时默认开 4 个观察 pane”

---

## 6. Workspace 包含什么

workspace 至少包含：

- `name`
- `tabs`
- `active_tab`
- 每个 tab 的：
  - tiled pane 树
  - floating pane 列表
  - active pane
  - zoom / preset 等运行态
- pane 到 terminal 的绑定信息
- 可能的恢复辅助信息

workspace 不应把 terminal 本体复制进去。

它只保存：

- terminal 标识
- metadata 快照或恢复辅助信息
- 当前如何 attach 到这些 terminal

---

## 7. Layout 包含什么

layout 至少包含：

- `name`
- tabs 结构
- pane 树结构
- floating pane 定义
- terminal resolve 规则
- create/attach/prompt/skip 策略
- 初始焦点建议

layout 可包含但不必须包含：

- 推荐 terminal name/tag
- 启动命令模板
- 环境/角色标签

---

## 8. 启动规则

## 8.1 `termx`

无参数启动时：

- 创建临时 workspace
- 默认创建首个 shell terminal
- 不要求先有 layout

## 8.2 `termx --layout <file>`

语义：

- 用某个 layout 创建一个新的 workspace 实例

执行过程：

1. 读取 layout
2. 生成 workspace 结构
3. 按 resolve 规则尝试 attach/create terminal
4. 进入实例化后的 workspace

## 8.3 `termx --workspace <file>`

语义：

- 直接恢复或打开某个 workspace 实体

执行过程：

1. 读取 workspace state
2. 恢复 tabs/panes/floating/focus
3. 尝试重新附着 terminal
4. 若部分 terminal 不可用，走恢复降级策略

---

## 9. 保存规则

## 9.1 保存为 workspace

用于：

- 保存当前真实现场
- 之后恢复继续工作

应保存：

- 当前结构
- 当前焦点
- 当前 pane/terminal 绑定关系
- 必要的恢复辅助信息

## 9.2 导出为 layout

用于：

- 复用当前结构
- 变成下次项目启动模板

应保存：

- pane/floating 结构
- resolve 规则
- 默认行为

不应保存过强的运行时偶然状态，如：

- 临时焦点位置
- 某次运行的瞬时 notice
- 瞬时尺寸竞争结果

---

## 10. 恢复与降级

workspace 恢复时，可能遇到：

- terminal 仍存在
- terminal 已 exited 但 retained
- terminal 已 removed
- metadata 已变化
- 只有部分 terminal 可恢复

要求：

- 恢复过程不能导致 TUI 崩溃
- 部分失败时，保留可恢复部分
- 对不可恢复部分给出明确动作：
  - retry attach
  - create replacement
  - skip
  - prompt user

---

## 11. Resolve 规则

layout/workspace 在启动时都可能涉及 terminal resolve。

候选策略：

- `attach`
  - 找到匹配 terminal 就直接 attach
- `create`
  - 找不到就新建 terminal
- `prompt`
  - 让用户选择 attach existing / create new / skip
- `skip`
  - 缺失则留空或跳过该 pane

匹配维度可包括：

- terminal id
- terminal name
- tags
- command

建议优先级：

- 对模板类 layout：优先依赖 `tags/name/rules`
- 对 workspace 恢复：优先依赖 `terminal_id`，再退化到 metadata 匹配

补充约束：

- 若 layout 显式把同一个 `_hint_id` 写到多个 pane，表示“有意复用同一个 terminal”
- 这种显式 hint 复用应允许跨 tiled / floating 同时绑定
- 若该 hint 当前未匹配到 terminal：
  - `create` 只创建一次，再把结果 attach 到所有同 hint pane
  - `prompt` 只提示一次，用户的 attach/create 选择应传播到所有同 hint pane
  - `skip` 也应一次性跳过这一组同 hint pane
- 只有普通匹配流程，才默认避免重复消费同一个 terminal

---

## 12. 临时 workspace

临时 workspace 是默认启动产物。

规则：

- 无参数执行 `termx` 时自动创建
- 可直接工作
- 可后续重命名
- 可保存为正式 workspace
- 可导出为 layout

退出 TUI 时：

- 临时 workspace 是否保存由用户决定
- 其默认 terminal 不因 TUI 退出而自动删除

---

## 13. Pane 与 terminal 绑定规则

在 workspace 中：

- pane 保存 terminal 绑定关系
- pane 关闭不默认删除 terminal
- terminal removed 时 pane 自动消失
- terminal exited retained 时 pane 保持 exited 状态
- 同一 terminal 可被多个 pane 同时绑定

对于共享 terminal，还需要记录恢复语义：

- 哪些 pane 共同绑定同一个 terminal
- 该 terminal 是否启用了 `termx.size_lock`
- 哪些 tab 配置了“进入 tab 自动 acquire resize”
- 是否存在 floating/tiled 的混合观察关系

这意味着 workspace 恢复时要区分两类失败：

- “terminal 不存在了”
- “terminal 还在，但现在是 exited/readonly/metadata changed”

## 13.1 共享 terminal 的恢复规则

恢复 workspace 时，如果多个 pane 绑定同一个 terminal：

- 优先恢复“多 pane 共同 attach 到同一 terminal”这一关系
- 允许恢复后重新进行 resize acquire
- 不要求恢复旧的临时 acquire 状态
- 但要保证：
  - 共享关系正确
  - 不出现重复创建多个 terminal
  - floating/tiled 关系正确

---

## 14. 后续文件模型建议

当前阶段先定语义，不急着把文件格式一次性锁死。

但建议方向如下：

## 14.1 layout 文件

偏声明式：

- 结构
- 规则
- 模板默认值

## 14.2 workspace 文件

偏状态式：

- 当前结构
- 当前 focus
- 当前 binding
- 恢复辅助信息

---

## 15. 用户可感知的产品规则

用户层面必须能理解：

- 用 layout 启动，是“按模板开一个现场”
- 用 workspace 启动，是“回到某个现场”
- workspace 变化不会偷偷改模板
- terminal 是 server 上的资源，不是 workspace 私有进程

---

## 16. 对后续实现的要求

代码后续要向这些规则收口：

- `termx --layout` 创建 workspace 实例
- `termx --workspace` 恢复 workspace 实体
- 临时 workspace 是默认入口
- workspace/picker 文案尽量使用 session-like 直觉
- layout 文案尽量强调 template/preset

---

## 17. 对测试的要求

后续至少补以下 e2e：

- 从 layout 启动并 attach/create terminal
- 从 workspace 恢复并恢复焦点
- layout 部分 resolve 失败时 prompt/skip 降级
- terminal removed 后 workspace 自动关闭对应 pane
- 临时 workspace 保存为正式 workspace
- workspace 导出为 layout
