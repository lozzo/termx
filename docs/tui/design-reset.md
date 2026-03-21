# TUI 重置设计草案

## 1. 设计方向

目标不是做成 tmux 的复制品，而是：

- **看起来更接近 zellij**
- **用起来接近 zellij/tmux 的直觉**
- **底层仍然是 termx**

也就是说：

- 用户看到的是 session/tab/pane 风格的直觉
- 但系统内部仍支持 terminal reuse、layout matching、多处 attach

## 1.1 核心架构前提

这一轮设计必须明确一个根前提：

- `TI`（TUI 交互层）和 `TR`（server 托管的 terminal 运行时/资源层）是解绑的
- TUI 只是观察、选择、attach、操纵 terminal 的客户端
- terminal 本身由 server 托管，生命周期不依赖某个 TUI 进程

这意味着它和 tmux / zellij 有本质差异：

- tmux/zellij 更像“布局和 terminal 共生在一个会话系统里”
- termx 更像“server 上有 terminal 池，TUI 只是一个前端工作台”

因此后续设计要优先保证：

- detach TUI 不影响 terminal 持续运行
- 同一个 terminal 可以被多个 pane / tab / workspace 复用或观察
- workspace/layout 更像“如何附着到 terminal 池并组织视图”，而不是 terminal 的唯一宿主

## 2. 概念收敛

## 2.1 对用户暴露的概念

- `workspace`
  - 对外可以把它理解成“session/工作区”
  - 是用户进入 termx 后最外层的工作单元
- `tab`
  - 相当于一个 page / window 方式也可以相当于是一个大工作区间的一个小工作区间,例如一个同时存在前后端的项目,那么我们tab1是前端的工作区间,工作的terminal是前端项目vim和数据库terminal,tab2是后端的工作区间,工作的是terminal是后端的vim和数据库,甚至还有一个tab3是前后端对照的
- `pane`
  - 一个可见区域
  - tiled 和 floating 本质上都还是 pane，只是展示层不同
- `terminal`
  - 在 picker、metadata、layout resolve 这类场景中出现

## 2.2 尽量隐藏的概念

- `view`
  - 后续不再当作一等用户概念
  - `fit/fixed/readonly/pin` 更适合作为 pane 的属性或状态
- `panel`
  - 不再引入为新的独立概念

结论：

- 后续对用户文案尽量统一成 `workspace / tab / pane / terminal`
- `view` 不删除底层实现能力，但不再当作 UI 主概念

## 3. 入口重置

## 3.1 直接运行 `termx`

应默认创建并进入一个临时 workspace。

建议行为：

- 若没有显式指定 workspace/layout/state
- 直接进入一个临时 workspace
- 默认给出一个可立即使用的 shell pane

这条路径必须像 zellij 一样“直接能干活”。

## 3.2 从文件启动 workspace

workspace 应优先由指定文件或显式选择驱动，而不是先进入一个抽象空壳再让用户自己理解体系。

建议入口：

- `termx --workspace <file>`
- `termx --layout <file>`
- 或进入后打开 workspace picker 选择

## 4. 交互基线

## 4.1 UX 上尽量贴近 zellij

后续重构时应优先满足：

- 有一个默认可输入 pane
- tab/pane 结构直观
- close 与 kill 分离但易懂
- 模式键不要把用户绕晕

## 4.2 底层仍保留 termx 的差异化

不要丢掉：

- 一个 terminal 可被多 pane attach
- terminal 可被 picker 复用
- layout 可按 metadata 解析

所以是：

- UX 像 tmux
- 能力不像 tmux

## 5. Metadata 语义

terminal metadata 的规则继续保持：

- `name/tags` 属于 terminal
- 已存在 pane 继续按 `terminal_id` 绑定
- metadata 改动后：
  - 所有 attach pane 立即刷新标题和 tags
  - 当前 workspace 不自动重排
  - 新 tags 只影响未来的 picker/search/layout resolve

补充约束：

- metadata 属于 terminal runtime，不属于某个 pane
- pane 只是 terminal 的一个展示入口
- 因此 UI 文案要尽量避免让用户误以为“改的是 pane 名称”

## 6. Floating 语义

floating 不是新概念，只是 pane 的一种展示层。

后续 UI 规则：

- tiled pane = 主工作层
- floating pane = 覆盖层
- 焦点必须明确能在两层之间切换

## 6.1 对高频角色场景的覆盖要求

这轮设计至少要覆盖：

- 开发：默认进来即可工作，快速 split / tab / reuse
- OPS/SRE：稳定观察、只读 attach、临时浮窗巡检、workspace 环境切换
- 故障处理：快速呼出日志/监控浮窗，同时保留主操作 pane
- 恢复：detach 后再次进入，仍可 attach 到原 terminal 和原工作现场

## 7. 接下来先不做的决定

为了避免继续补丁叠补丁，以下内容先不急着定最终答案：

- 最终快捷键是否完全保留现在的 v3
- workspace 文件格式最终长什么样
- floating 鼠标交互是否还要继续扩展

这些都先让位给“场景 + e2e + 代码收口”。
