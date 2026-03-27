# TerminalStore Migration Phase 3 Design

状态：Draft
日期：2026-03-28

## 1. 背景

Phase 1 已完成 `Workbench` 落地，Phase 2 已完成 `App` 高层协调入口落地。

当前 TUI 顶层关系已经开始收敛为：

- `Model` 逐步退化为 Bubble Tea shell 与 runtime/live state 承载体
- `App` 作为高层协调入口开始成立
- `Workbench` 作为主工作流对象开始稳定

但 terminal 语义仍然分散在多个对象上：

- `Pane` 上带着 terminal identity / metadata / state 的一部分
- `Viewport` 上带着 runtime / snapshot / vterm 的一部分
- `Model` 上还承载部分 terminal 级协作逻辑
- 未来 `TerminalPoolPage` 还没有统一 terminal 数据源

这意味着当前 terminal 仍然不是 TUI 内部的一等对象，而是多处字段拼合出来的“半对象”。

因此下一阶段的目标不是继续在 `Pane` 和 `Viewport` 上打补丁，而是：

> 正式建立 `TerminalStore` 和 TUI 本地 `Terminal` 代理对象，
> 让 terminal 在 TUI 内成为一等对象。

## 2. 本阶段目标

Phase 3 的主目标是：

- 正式建立 `TerminalStore`
- 正式建立较重的 TUI 本地 `Terminal` 代理对象
- 让 `Pane -> *Terminal` 关系开始成立
- 把 terminal 的身份 / 元数据 / 状态 / 部分镜像数据从 `Pane / Viewport / Model` 开始收拢到 `Terminal`
- 为 `TerminalPoolPage` 的统一数据源铺路

这一阶段仍然不是 terminal runtime 协调迁移，也不是 renderer 主线迁移。

## 3. 核心对象关系

本阶段完成后的推荐关系应为：

```text
App
  ├── Workbench
  └── TerminalStore

Workbench
  └── Workspace -> Tab -> Pane

Pane
  └── *Terminal

TerminalStore
  └── *Terminal[*]
```

这里的含义是：

- `App` 持有高层主对象：`Workbench` 与 `TerminalStore`
- `Workbench` 继续只负责主工作流对象树
- `Pane` 是工作位，直接引用 terminal 对象
- `TerminalStore` 是 terminal 代理对象的唯一注册表

## 4. 职责边界

### 4.1 TerminalStore

`TerminalStore` 的职责是：

- 统一注册 / 获取 / 枚举 / 删除 `*Terminal`
- 保证同一个 terminal ID 在 TUI 内只对应一份本地代理对象
- 为 `Pane`、未来的 `TerminalPoolPage`、以及读取逻辑提供共享引用

`TerminalStore` 在本阶段不负责：

- socket 同步
- attach / stream / snapshot 拉取
- bind / unbind 协调
- resize 决策

也就是说：

- `TerminalStore` 是 registry / cache
- 不是 coordinator

### 4.2 Terminal

`Terminal` 是 daemon terminal 的本地代理 / 镜像对象。

它在本阶段应承载：

- identity
  - ID
  - name
  - command
  - tags / metadata
- state
  - running / exited / removed 等
  - exit code（如适用）
- runtime mirror（本轮只收字段归属，不收协调）
  - snapshot
  - vterm
  - stream 相关镜像状态
  - attach / channel 相关信息
- connection context 的正式承载位置
  - owner / follower
  - 谁连接了它
  - 哪些 pane / 客户端在看它

`Terminal` 在本阶段不是：

- terminal pool owner
- terminal runtime coordinator

### 4.3 Pane

`Pane` 在本阶段后应尽量只保留 pane 自己的语义：

- pane identity
- 当前绑定的 `*Terminal`
- viewport offset
- viewport move mode
- pane 级显示状态

`Pane` 不应继续作为 terminal identity / metadata / state 的主归属对象。

### 4.4 Viewport

`Viewport` 当前仍承载较多 runtime 细节。

本阶段不要求彻底重构 `Viewport`，但要开始明确：

- 哪些字段本质属于 `Terminal`
- 哪些字段本质属于 `Pane`
- 哪些字段只是过渡期仍留在 `Viewport`

目标方向是：

- `Terminal` 变成 terminal 本体镜像
- `Pane` 变成工作位与观察状态宿主
- `Viewport` 逐步退化为实现细节承载

### 4.5 TerminalPoolPage

本阶段不要求正式把 `TerminalPoolPage` 做完，但要为它准备正确的数据源：

- 页面未来应直接读取 `TerminalStore`
- 页面只保留筛选 / 排序 / 选中 / 局部 UI 状态
- terminal 数据本身不再由页面维护副本

## 5. 迁移范围

本阶段迁移的是：

- `TerminalStore` 类型本身
- `Terminal` 类型本身
- `Pane -> *Terminal` 关系的开始建立
- 一批 terminal identity / metadata / state / mirror 字段的正式归属收拢
- 一批读取路径开始从 `Pane / Viewport` 转到 `Terminal`

本阶段明确不迁：

- `TerminalCoordinator`
- `Resizer`
- 完整 attach / stream / snapshot / recovery 协调迁移
- renderer / render loop 重构
- owner / follower 全量正式架构闭环
- 一口气清空所有 `Pane` / `Viewport` 上的 terminal 相关字段

## 6. 文件结构建议

本阶段建议新增：

- `tui/terminal_store.go`
  - `TerminalStore`
  - 注册 / 查找 / 列举 / 删除
- `tui/terminal_model.go`
  - `Terminal` 类型
  - terminal identity / metadata / state / mirror 字段
- `tui/terminal_store_test.go`
- `tui/terminal_model_test.go`

本阶段可能修改：

- `tui/model.go`
- `tui/picker.go`
- `tui/render.go`
- `tui/workbench.go`
- `tui/model_test.go`

如果 terminal metadata 更新、picker、或未来 terminal pool 相关逻辑集中在其他文件，也可以一并接入，但原则是：

- 先立 `TerminalStore` / `Terminal`
- 不把 coordinator 职责顺手塞进去

## 7. 数据流设计

本阶段推荐的数据流是：

```text
daemon terminal info / local runtime mirror
  -> TerminalStore
  -> *Terminal
  -> Pane / Workbench / future TerminalPoolPage / render reads
```

这里强调的是对象归属与读路径方向，不是本轮就把所有同步逻辑抽成新系统。

也就是说：

- 当前同步逻辑还能暂时留在旧位置
- 但它更新的目标对象应越来越偏向 `Terminal`

## 8. 迁移切刀

### 8.1 切刀 A：先立 TerminalStore 与 Terminal

目标：

- 新增 `TerminalStore`
- 新增 `Terminal`
- `App` 或 `Model` 正式持有 `terminalStore`
- 同一 terminal ID 在 TUI 内只对应一份代理对象

结果：

- `Terminal` 从概念和散落字段，变成正式对象
- 后续读写路径有合法归宿

### 8.2 切刀 B：让 Pane -> *Terminal 关系成立

目标：

- `Pane` 开始直接引用 `*Terminal`
- 一部分 terminal 读取路径改成从 `pane.Terminal` 取值
- 过渡字段仍可暂留，避免一次性掏空当前逻辑

结果：

- `Pane` 不再继续扮演半个 terminal
- terminal identity / metadata 开始统一从 `Terminal` 读取

### 8.3 切刀 C：收拢核心 terminal 镜像字段

目标：

- 把 terminal identity / metadata / state / snapshot mirror 等正式迁入 `Terminal`
- `Pane` 继续保留工作位和观察状态
- `Viewport` 开始失去领域中心地位

结果：

- terminal 语义主归属开始统一
- 后续 coordinator / renderer 有稳定依赖对象

### 8.4 切刀 D：让读路径开始信任 TerminalStore

目标：

- 一批 picker / render / metadata / terminal 读取路径开始从 `TerminalStore` / `*Terminal` 读取
- 为 `TerminalPoolPage` 铺路

结果：

- terminal 数据统一来源开始成立

## 9. 测试策略

### 9.1 TerminalStore 单元测试

验证：

- 同 ID 返回同一对象
- 注册 / 查找 / 删除行为正确
- 不会意外生成多份 terminal 对象

### 9.2 Terminal / Pane 关系测试

验证：

- `Pane` 能正确引用 `*Terminal`
- 修改 terminal 元数据后，多个 pane 看到的是同一份对象状态
- pane 自己的 viewport offset / move mode 不会跑到 terminal 上

### 9.3 回归测试

重点盯住：

- picker / attach / metadata 更新
- pane 标题 / terminal 名称读取
- 渲染侧读取 terminal 信息
- shared terminal 场景的基础可见行为

本阶段的测试重点不是“有一个 Store 和一个 struct”，而是：

- terminal 是否开始成为正式一等对象
- 读路径是否开始从正确对象读取
- 现有行为是否不回归

## 10. 风险与约束

### 风险 1：只加了 Store，没有真正收 terminal 语义

风险：

- 多了一个 registry
- 但 terminal 语义仍散落在原位置

应对：

- 计划里必须包含 terminal 字段主归属迁移
- 至少迁一批真实读路径，而不是只加基础设施

### 风险 2：过早卷入 coordinator 职责

风险：

- 顺手处理 attach / stream / recovery / resize-owner
- 直接越界到下一阶段

应对：

- 明确本阶段只迁对象与读路径主归属
- 协调逻辑仍保持现状，后续再正式迁

### 风险 3：一次性清空 Pane / Viewport 字段

风险：

- 改动面过大
- 回归风险高

应对：

- 先立正式对象
- 再逐步迁读写路径
- 最后再清理遗留字段

## 11. 成功标准

本阶段成功的判断标准是：

- `TerminalStore` 是正式对象
- `Terminal` 是正式对象
- `Pane -> *Terminal` 关系开始成立
- 一批核心 terminal 读路径已经迁到 `Terminal`
- `TerminalPoolPage` 已有统一数据源方向
- 没有提前把 coordinator / renderer 主线卷进来

## 12. 结论

Phase 3 的正确方向是：

> 先立 `TerminalStore + Terminal`，让 terminal 在 TUI 内成为正式一等对象，
> 但只迁对象归属与读取主线，不迁 runtime 协调主线。

这样可以在不打断现有 TUI 主线的前提下，进一步完成对象关系收敛：

- `App` 作为高层入口
- `Workbench` 作为主工作流对象
- `TerminalStore` 作为 terminal 统一注册表
- `Terminal` 作为本地 terminal 镜像对象
- `Pane` 作为引用 `*Terminal` 的工作位

这也是后续 `TerminalCoordinator + Resizer`、以及 renderer/render loop 稳定迁移的必要前提。
