# termx TUI 交互规格

状态：Draft v1

本文件定义 termx TUI 的交互规则、模式规则、焦点规则和 pane 生命周期规则。

它是 `docs/tui/product-spec.md` 的实现级补充文档。

---

## 1. 目标

本规格要解决 4 件事：

- 用户按键之后，系统怎么反应
- 焦点在 tiled / floating / picker / prompt 间怎么流转
- pane 和 terminal 的关系在交互层怎么表现
- 错误输入、取消输入、terminal 消失时，界面怎么收口

---

## 2. 交互原则

## 2.1 直接可用

- 用户进入 `termx` 后应直接处于可工作的 Normal 状态
- 默认已有一个可输入 pane
- 用户不需要先理解模式系统

## 2.2 模式是辅助手段，不是产品本体

- mode 只用于聚类高频操作
- mode 不能成为理解产品的前提
- mode 不能把用户锁死

## 2.3 `Esc` 是统一退路

- 退出 picker
- 退出 prompt
- 退出 help
- 从 floating 焦点层退回 tiled
- 退出临时 mode

如果当前已经在 Normal 且没有 overlay，`Esc` 不做破坏性动作。

## 2.4 错误按键必须无害

- 未识别按键直接忽略
- 错误组合键不能卡死
- 输入被忽略时不应破坏当前布局和焦点

---

## 3. 交互层级

termx 的交互层级从高到低如下：

1. fatal / blocking error
2. modal prompt
3. picker
4. help / command line
5. floating focus layer
6. tiled workspace
7. terminal input

规则：

- 高层级打开时，低层级不直接接收键盘输入
- 关闭高层级后，焦点回到最近的合法下层

---

## 4. 核心状态

## 4.1 Normal

默认状态。

行为：

- 普通输入发送给当前 active pane 的 terminal
- 少量全局快捷键可直接触发
- 状态栏展示最常用操作

## 4.2 Mode

termx 可以保留 mode-based 交互，但 mode 必须是“短驻留”。

要求：

- 明确显示当前 mode badge
- mode 下的可用动作数量有限
- mode 应可连续操作
- `Esc` 退出 mode

## 4.3 Overlay

overlay 包括：

- terminal picker
- workspace picker
- metadata prompt
- create prompt
- help

要求：

- 居中显示
- 不透明遮挡
- 关闭后不残留视觉污染

---

## 5. 焦点模型

## 5.1 焦点实体

任一时刻，用户焦点必须明确落在以下之一：

- 一个 tiled pane
- 一个 floating pane
- 一个 picker
- 一个 prompt
- help / command line

## 5.2 Tiled 焦点

- workspace 正常工作时，默认焦点在 tiled pane
- active tiled pane 有明确边框高亮

## 5.3 Floating 焦点

- 当用户进入 floating 层时，焦点落在某个 floating pane
- active floating pane 在视觉上高于其他 floating pane
- 状态栏明确显示当前为 floating layer

## 5.4 焦点回退

关闭当前焦点对象时：

- 如果关闭的是 prompt/picker/help：回到之前的 pane 焦点
- 如果关闭的是 floating pane：
  - 若仍有其他 floating pane，焦点切到下一个 floating pane
  - 否则回到 tiled pane
- 如果关闭的是 tiled pane：
  - 优先切到同 tab 内最近的 pane
  - 若 tab 已无 pane，则进入 tab empty state 或触发新建路径

---

## 6. Pane 生命周期

## 6.1 Pane 创建

pane 可通过以下路径创建：

- 默认启动创建第一个 pane
- split 创建 tiled pane
- 新 tab 创建首个 pane
- new floating 创建 floating pane
- attach existing terminal 到当前/新 pane

## 6.2 Pane 绑定 terminal

规则：

- pane 是 terminal 的展示入口
- pane 只绑定一个 terminal
- terminal 可被多个 pane 同时绑定

## 6.3 Pane 关闭

关闭 pane 只关闭展示入口：

- 不默认 kill terminal
- 不默认删除 terminal runtime

## 6.4 Terminal exited

当 terminal 自然退出但仍被保留时：

- pane 显示 exited 状态
- pane 可展示 exit code
- 历史内容保留，但渲染应回到中性前景色
- pane 可提供 restart/rebuild

## 6.5 Terminal removed/killed

当 terminal 被明确移除时：

- 所有绑定该 terminal 的 pane 自动关闭
- 不保留空壳 pane
- tab 内其余 pane 自动重排

这条规则优先遵循 zellij/tmux 用户预期。

## 6.5.1 多客户端共享下的通知语义

当多个客户端同时 attach 到同一个 terminal 时，需要区分：

### close pane

- 仅关闭当前客户端的当前 pane
- 不通知其他客户端

### detach TUI

- 当前客户端离开 TUI
- 不向其他客户端发送强提示
- 可通过状态或 attached count 静默反映

### kill/remove terminal

- 当前客户端销毁共享 terminal
- 其他客户端必须收到 notice/warning
- 若带身份信息，应提示操作者

其他客户端建议行为：

- 若 pane 立即关闭：
  - 先显示短暂 notice，再关闭 pane
- 若存在过渡态：
  - 可短暂显示 `terminal removed by another client`
  - 然后执行 pane 关闭与重排

权限建议：

- observer / readonly 客户端不能执行 kill/remove
- collaborator / controller 才能执行 kill/remove
- 多客户端共享时，kill/remove 先弹确认更安全

当前落地约束：

- server/protocol 层已拦截 observer attachment 的 `kill/remove`
- TUI 层已拦截 readonly pane 的 `kill/remove`
- 未 attach 的独立管理型请求暂不纳入这条 observer 约束
- TUI 右侧状态区会显示 `access:observer|collab`
- observer / readonly pane title 目前会显示 `[obs]` / `[ro]` 轻量标记

## 6.6 Shared terminal resize acquire

当多个 pane 绑定同一个 terminal 时：

- terminal 真实尺寸只有一份
- pane 自身几何变化，不自动改写 PTY 尺寸

因此交互规则改为：

- resize 必须显式 acquire
- 未 acquire 时，pane 只是观察 terminal
- acquire 成功后，当前 pane 的尺寸才可同步到 terminal

## 6.7 acquire 触发规则

以下动作才可触发 resize acquire：

- 用户显式执行 acquire resize
- tab 配置开启“进入 tab 自动 acquire resize”后，切回 tab 时自动 acquire
- 用户在 floating pane 上显式获取尺寸控制后执行缩放

以下动作默认不自动 acquire：

- 仅仅获得焦点
- 仅仅开始输入
- 普通 pane 几何变化

## 6.8 未 acquire pane 行为

未 acquire 的 pane：

- 继续显示同一 terminal 的输出
- 可以有局部裁剪或留白
- 可以通过滚动/偏移来帮助观察
- 不因为自身几何变化自动改写 terminal size

## 6.9 size lock 提示

terminal 支持可配置的 size lock 提示标签。

建议标签：

- `termx.size_lock=off`
- `termx.size_lock=warn`

语义：

- `off`
  - acquire/resize 时不提示
- `warn`
  - acquire/resize 前先提示用户
  - 明确说明“变更 size 可能影响内部输出”

这不是硬锁，而是保护性提示。

## 6.10 acquire pane 消失时

当最近一次 acquire 的 pane 被关闭、失效或删除时：

- terminal 保持当前尺寸
- 其他 pane 不自动接管 resize
- 后续必须再次显式 acquire 才改变 terminal size

---

## 7. 默认启动交互

用户执行 `termx` 时：

1. 创建临时 workspace
2. 创建默认 shell terminal
3. 默认 terminal 继承当前 cwd 和环境变量
4. 创建并聚焦首个 pane
5. 用户可直接输入

退出规则：

- detach/quit TUI：仅退出前端
- 不自动删除默认 terminal
- 用户显式 kill terminal 才删除 terminal

---

## 8. Split 交互

## 8.1 触发

用户从当前 pane 发起 split。

## 8.2 系统提供的动作

split 后至少提供：

- create new terminal
- attach existing terminal

## 8.3 完成后的焦点

- 新 pane 创建成功后，焦点落在新 pane
- 用户可以立即输入

## 8.4 失败处理

- create/attach 失败时不应破坏原 pane
- 焦点回到原 pane
- 给出 notice 或 error

---

## 9. Tab 交互

## 9.1 新建 tab

新 tab 可以：

- 创建新 terminal
- attach 现有 terminal
- 保留为空 tab（如产品后续仍保留该能力）

建议默认优先进入“创建或选择 terminal”的路径，而不是空白页。

## 9.2 切换 tab

- 切换 tab 时保留该 tab 上次 active pane
- 若 tab 有 floating panes，切回时恢复其可见状态

## 9.3 关闭 tab

- 关闭 tab 等于关闭该 tab 下所有 pane 入口
- 不默认 kill 这些 pane 对应的 terminal

---

## 10. Floating 交互

## 10.1 创建

floating pane 可通过：

- create new terminal
- attach existing terminal

## 10.2 焦点切换

需要支持：

- tiled -> floating
- floating -> floating
- floating -> tiled

其中：

- `Esc` 是 floating -> tiled 的标准退路

## 10.3 层级

- 新 floating pane 默认错开摆放
- active floating pane 置顶
- raise/lower 后的显示结果要与 z-order 一致

## 10.4 鼠标

至少支持：

- 拖动标题区域移动
- 拖动右下角缩放

补充：

- 若 floating pane 绑定共享 terminal，拖拽缩放前应先 acquire resize
- 未 acquire 时，拖拽只改变 pane 外框，不直接提交 terminal resize

## 10.5 隐藏

- hide/show floating layer 不销毁 floating pane
- 再次显示时恢复原位置和 z-order

---

## 11. Picker 交互

## 11.1 Terminal picker

用途：

- attach existing terminal
- create new terminal
- 搜索 terminal

规则：

- 默认焦点在搜索框/首项
- `Enter` 执行当前选择
- `Esc` 关闭 picker
- picker 关闭后回到原 pane 焦点

## 11.2 Workspace picker

用途：

- 切换 workspace
- 创建 workspace

规则：

- 切换成功后恢复 workspace 的 active tab / active pane
- 关闭后不残留边框或文字污染

---

## 12. Prompt 交互

## 12.1 创建 terminal prompt

推荐为两步：

1. 输入 terminal name
2. 输入 terminal tags

规则：

- `Enter` 前进或提交
- `Esc` 取消
- 默认 name 应是友好名称，不是随机短串

## 12.2 编辑 metadata prompt

编辑 terminal metadata 时：

1. 编辑 terminal name
2. 编辑 terminal tags
3. 保存后刷新所有 attach pane

注意：

- UI 必须表达“你正在改 terminal metadata”
- 不能让用户误会是只改当前 pane 标题

## 12.3 Command line

command line 是全局入口，不应常驻。

可用于：

- edit-terminal
- load-layout
- save-layout
- 其他低频高级命令

---

## 13. Help 交互

help 的目标是：

- 帮用户建立当前 keymap 的认知
- 解释各 mode 的核心操作
- 给新用户一个短路径

help 规则：

- 必须可随时关闭
- 关闭后回到之前焦点
- 内容按“高频 -> 低频”组织

---

## 14. 输入路由规则

键盘输入按以下顺序路由：

1. prompt
2. picker
3. help
4. active mode
5. active pane terminal

鼠标输入按以下顺序路由：

1. visible overlay
2. active floating pane
3. floating hit-test
4. tiled pane hit-test

---

## 15. 错误与边界条件

必须明确处理：

- 错误 mode key
- picker/filter 为空
- attach 到 exited terminal
- attach 到已被移除 terminal
- workspace 恢复失败
- layout 只匹配到部分 terminal
- 当前 active pane 在异步事件中消失
- 多个 pane 绑定同一 terminal 时 terminal 被 kill
- 多个 pane 绑定同一 terminal 时 acquire pane 被关闭
- tiled 和 floating 间反复 acquire resize
- floating resize 与 tab auto-acquire 交替发生时的尺寸抖动

原则：

- 优先稳定
- 不 panic
- 不黑屏
- 不留脏焦点

---

## 16. 共享 terminal 的动作矩阵

### 场景 A：关闭某个共享 pane

- 只关闭当前 pane
- terminal 不删除
- 其他 pane 继续显示该 terminal

### 场景 B：kill terminal

- terminal 被销毁
- 所有共享 pane 同时关闭
- 当前 tab 自动重排
- 其他客户端收到“被另一客户端移除”的提示

### 场景 C：terminal exited retained

- 所有共享 pane 进入 exited 状态
- 任一 pane 可触发 restart

### 场景 D：共享 floating pane 被缩放

- 需先显式 acquire resize
- terminal 真实尺寸更新
- 其他 tiled/floating pane 继续 observer 展示

---

## 17. 当前待落地的实现约束

当前实现应继续向以下规则收口：

- 用户可见术语统一为 `workspace / tab / pane / terminal`
- `view` 只保留为内部实现属性
- terminal removed 时 pane 自动消失
- 默认启动直接给 shell，不再给“空说明页”
- mode 提示继续收短，避免底栏过载
- 共享 terminal 的 acquire/lock 逻辑要落地到代码与 e2e

---

## 18. 与测试的对应关系

本规格优先通过 e2e 锁定以下主线：

- 直接启动进入可工作 workspace
- split 并继续输入
- attach existing terminal 到新 tab
- attach existing terminal 到 floating pane
- metadata 编辑同步刷新
- exited terminal restart
- workspace 切换
- floating 焦点切换、隐藏、缩放
- 共享 terminal 的 resize acquire、size lock、pane close、terminal remove
