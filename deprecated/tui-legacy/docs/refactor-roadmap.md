# termx TUI 重构路线图

状态：In Progress
日期：2026-03-22

这份 roadmap 只描述“内部重构如何推进”。

如果你想看产品功能顺序，请先读 [`implementation-roadmap.md`](implementation-roadmap.md)。

---

## 1. 总原则

重构按下面原则推进：

1. 不停机重写
2. 每一阶段都保持 `go test ./... -count=1` 通过
3. 每一阶段先补测试，再抽实现
4. 不把 UI polish 和架构重构混在一个大提交里
5. owner/follower、saved/exited、manager/picker 主路径优先级高于视觉优化

---

## 1.1 当前进度快照

截至 2026-03-22，staged refactor 的真实进度是：

- `R0 文档冻结`：完成
- `R1 连接状态抽离`：完成第一阶段，已抽出 `connection_state.go`，owner/follower 规则已有独立单测
- `R2 输入统一为 intent`：进行中，键盘 key/event 主要模式已共享 action 映射，prefix 状态进入/退出已收口到统一 transition 入口
- `R3+`：尚未开始

当前最小下一步：

1. 继续把 pane mode 与鼠标输入推进到更明确的 intent / dispatch 入口
2. 再把 reducer / runtime effect 分离出来
3. 最后才动 pane/runtime/render 的大拆分

---

## 2. 分阶段计划

### R0. 文档冻结

目标：

- 统一术语
- 明确现状和问题
- 给后续代码重构一个稳定口径

交付：

- `product-spec.md`
- `interaction-spec.md`
- `current-status.md`
- `implementation-roadmap.md`
- `architecture-refactor.md`
- `refactor-roadmap.md`

测试要求：

- 无新增代码
- 只检查文档与当前行为不冲突

### R1. 连接状态抽离

目标：

- 把 shared terminal 的连接关系抽成显式模型
- 把 owner/follower 规则从零散标志收口出来

交付：

- 独立的 connection state
- owner 迁移规则
- attach 默认 follower 规则
- stop/remove 后连接清理规则

测试要求：

- 单测覆盖 owner 选举、迁移、attach、remove
- e2e 覆盖 tiled + floating 共享 attach

### R2. 输入统一为 intent

目标：

- 键盘、鼠标、异步消息统一收口
- 去掉重复状态机

交付：

- 统一 intent 定义
- 输入适配层
- mode/prefix 定义统一来源
- prefix 状态切换统一 transition 入口

测试要求：

- 单测覆盖主要 intent 映射
- e2e 覆盖 prefix hold、Esc、错误按键忽略、连续 resize/move

当前已完成的子项：

- key/event 主要模式的共享 action 映射
- root prefix 与 direct mode 的共享 shortcut 规格
- exited pane shortcut 的共享处理
- prefix enter/clear 的统一 transition 入口与单测
- root/tab/workspace/global/resize/offset-pan/viewport/floating/pane 已开始共享 `prefix input` 归一化层
- `Alt` 浮窗 move/resize 组合键也已开始走统一 prefix input 映射，而不是独立硬编码判断
- `dispatchPrefixKey/Event` 已开始回落到共享 `dispatchPrefixInput` 入口
- prefix 路径已经出现第一层显式 `prefixIntent`，开始从“共享 helper”转向“显式 intent”
- `applyActivePrefixResult` 已拆出 `prefixRuntimePlan`，开始从“直接执行”转向“plan + apply”
- `global mode action` 也已开始复制 `action -> runtime plan -> apply` 这一套低风险分离模式
- `workspace mode action` 也已开始复制 `action -> runtime plan -> apply`，说明这条分离路径可以继续扩展
- `tab mode action` 也已开始复制 `action -> runtime plan -> apply`，低耦合 mode 已逐步进入统一分离模式
- `floating mode action` 也已开始复制 `action -> runtime plan -> apply`，并已覆盖 move/resize 这类更接近真实交互的路径
- `viewport mode action` 也已开始复制 `action -> runtime plan -> apply`，已经覆盖 acquire/toggle/pan/follow/offset-mode
- `resize mode action` 也已开始复制 `action -> runtime plan -> apply`，说明带 rearm 的 sticky mode 也能进入同一分离模式
- `offset-pan mode action` 也已开始复制 `action -> runtime plan -> apply`，sticky viewport navigation 也进入了同一分离模式
- `viewport mode` 与 `offset-pan mode` 已进一步抽出共享的 `viewportNavigationRuntimePlan`，开始从“每个 mode 各自 plan”走向“公共 runtime 行为层”
- `floating mode` 的 direct-mode keep 策略已下沉到 runtime plan 生成层，开始从“action 后置补丁”转向“plan 层统一表达行为”
- `tab mode` / `workspace mode` 中无效动作的 keep 行为已用单测锁定，并删除无实际行为贡献的 direct-mode 冗余分支

当前未完成的子项：

- 还没有把全部输入真正收束成显式 intent struct
- 鼠标与异步消息还没有进入同一 reducer 风格入口

### R3. reducer / effect 分离

目标：

- 让业务状态迁移尽量纯化
- 副作用由 runtime 承接

交付：

- reducer 返回 state delta + effects
- runtime 负责 server / timer / resize / logger 调用

测试要求：

- 单测覆盖 reducer
- effect 层用替身验证调用

### R4. pane/runtime/render 状态拆分

目标：

- 从 pane 中拆开领域状态、运行时状态、渲染状态

交付：

- `PaneState`
- `PaneRuntime`
- `ScreenModel`

测试要求：

- 单测覆盖状态转换
- 渲染测试覆盖 overlay、title chrome、saved/exited pane

### R5. render cache 收口

目标：

- 把缓存失效责任拉回 render layer
- 降低闪烁、残影、重复重绘风险

交付：

- 局部缓存策略
- 明确 dirty 传播规则
- benchmark 基线

测试要求：

- benchmark 覆盖大输出、shared alt-screen、overlay 开关、鼠标拖动
- e2e 覆盖拖动残影和 shared TUI 程序复用

### R6. 交互与视觉收尾

目标：

- 在新结构上继续做 UI/interaction polish
- 不再被旧架构牵制

交付：

- 更稳定的 terminal manager
- 更清晰的 status 分配
- 更统一的底栏和 pane chrome
- 更完整的 help / shortcut 呈现

测试要求：

- e2e 覆盖新用户主路径
- 渲染测试覆盖关键布局页

---

## 3. 当前推荐执行顺序

建议严格按下面顺序推进：

1. `R0 文档冻结`
2. `R1 连接状态抽离`
3. `R2 输入统一为 intent`
4. `R3 reducer / effect 分离`
5. `R4 pane/runtime/render 状态拆分`
6. `R5 render cache 收口`
7. `R6 视觉与交互收尾`

原因：

- shared terminal 是当前最容易继续失控的核心复杂度
- 输入系统不统一，后面所有 mode/keymap 优化都会返工
- render cache 如果太早单独动，容易继续和业务逻辑缠死

---

## 4. 每阶段的提交策略

每一阶段尽量拆成 3 类提交：

1. `test:` 先补测试
2. `refactor:` 抽模型或搬逻辑
3. `fix:` 收尾兼容和边界修正

这样做的好处是：

- 回归时更容易定位问题
- 方便把结构性提交和功能修复分开阅读

---

## 5. 风险控制

重构期间最需要盯住的风险：

1. shared terminal owner 丢失或重复
2. floating 拖动/resize 产生残影
3. alt-screen 程序在复用场景下渲染串屏
4. mode/prefix 因状态机重复再次出现卡死
5. render cache 调整后性能回退或闪烁加重

对应原则：

- 每个风险都要有至少一个 e2e 或专项测试

---

## 6. 完成后的预期收益

完成这轮重构后，termx TUI 应该获得：

- 更稳定的 shared terminal 语义
- 更容易继续扩 terminal manager 和 metadata
- 更可控的 UI/交互演进空间
- 更明确的性能优化边界
- 更低的后续维护成本

## 7. 工作逻辑
如果我没有主动介入,请连续工作直到任务完成,中途不应该让人类输入确认,直接完成
