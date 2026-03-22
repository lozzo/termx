# termx TUI 实施路线图

状态：2026-03-22

这份 roadmap 不再按旧设计稿追历史，而是只基于“当前代码 + 当前产品结论”继续推进。

---

## 1. 当前总体判断

当前 TUI 不需要推倒重写。

正确策略是：

- 保留已稳定的 server / protocol / vterm / e2e 基础设施
- 在现有 TUI 上继续做 TDD 增量重构
- 先收口文档和交互，再做进一步视觉与能力扩展

原因：

- 基础主链路已经能跑
- 生命周期语义已经初步正确
- terminal manager / picker / metadata / restore 都已有真实代码
- 全重写会丢失已经修好的复杂边界

---

## 2. 当前已完成

### M1. 基础工作台

已完成：

- 默认启动进入可工作 workspace
- 默认 shell pane
- split / tab / floating 基本可用
- 顶栏 / pane 标题栏 / 底栏 基础结构已存在

### M2. terminal 复用与管理

已完成：

- terminal picker
- terminal manager 全屏页
- bring here / new tab / floating
- terminal manager 分组：`NEW / VISIBLE / PARKED / EXITED`
- terminal manager 专用状态栏

### M3. 生命周期语义

已完成：

- close pane 保留 terminal
- stop terminal 先确认
- stop/remove 后保留 saved pane
- remote remove notice
- exited terminal 保留历史并进入 exited 状态

### M4. metadata

已完成：

- terminal name / tags 编辑
- picker 可进入 metadata 编辑
- terminal manager 可进入 metadata 编辑
- 两步 prompt：name -> tags
- prompt 显示 step / terminal id / command
- parked terminal metadata 编辑可用

### M5. restore / layout 基线

已完成：

- workspace state 恢复基础
- layout create / prompt / skip 基础
- 重复 `_hint_id` 的共享复用基线

### M6. 测试基线

已完成：

- 单测大量覆盖 TUI 状态机与渲染规则
- e2e 已覆盖主线场景和若干复杂场景
- 当前全量 `go test ./... -count=1` 通过

---

## 3. 当前在做但还没完全封口的事情

### R1. 快捷键与帮助系统收口

目标：

- 降低模式心智负担
- 让新用户更容易理解 `Ctrl-p/r/t/w/o/v/f/g`
- 移除用户态 legacy `Ctrl-a`
- 把底栏快捷键提示改成接近 zellij 的连续 segment 样式

当前状态：

- 基本结构已存在
- 底栏左侧 segment 化已经落地
- 普通态底栏只保留 `Ctrl+` 模式入口，不再混入 exited/unbound 直达动作
- 用户态 `Ctrl-a` 已移除，按下时直接透传给 terminal
- mode hold 默认 3 秒，可通过 `--prefix-timeout` 调整
- `resize / floating move / floating resize / pane focus / viewport pan` 等连续动作会续期 3 秒窗口
- help 已进入居中 overlay 体系，不再是整屏说明文字
- help 已初步改成分组式操作卡片，不再是线性文案堆叠
- 但 help、mode 文案、用户记忆成本仍偏高

### R2. shared terminal 的最终一致性

目标：

- 让 resize / acquire / auto-acquire / size-lock 语义稳定且可理解
- 把复杂共享场景进一步锁进 e2e

当前状态：

- 已有基线
- `shared + floating + fit + acquire + alt-screen` 组合场景已补进回归
- 仍需继续补复杂边界测试

### R3. UI 视觉统一

目标：

- modal / picker / manager 风格统一
- 顶栏、pane 标题栏、底栏信息进一步重新分配
- 降低信息堆叠感
- pane 顶部 chrome 收口成单线表达
- pane 标题默认显示 terminal 真实名称，不强调 pane 独立命名
- floating pane 补“呼回并居中”快捷动作

当前状态：

- 结构已成型
- pane 标题默认显示 terminal 真名已落地
- floating pane 呼回并居中已落地
- 底栏右侧焦点摘要已压缩，关系状态主要上移到 pane chrome
- modal / picker / manager 的统一视觉还没完全收口

---

## 4. 下一阶段推荐顺序

### Phase 1. 文档收口

目标：

- 以新整理的 4 份主文档为准
- 清理旧补丁式文档
- 确认术语、心智模型、当前状态描述一致

完成标准：

- 用户确认主文档没问题
- 非主文档整体归档或删除

### Phase 2. 交互减负

目标：

- 重写 help 的表达方式
- 收口 mode 文案
- 把更多状态上移到顶栏和 pane 标题栏
- 继续压缩底栏
- 明确 pane 标题展示 terminal 真实名称
- 给 floating pane 增加呼回并居中的标准快捷键

建议测试：

- 单测：状态栏与 help 文案
- e2e：新用户主路径可发现性

### Phase 3. shared terminal 边界补测

目标：

- 扩大复杂共享场景覆盖
- 尤其是 tiled + floating + full-screen TUI 程序复用场景

建议重点：

- htop / alt-screen 类程序
- 同 terminal 多 pane 同时显示
- resize / acquire / stop / exit 混合场景

### Phase 4. UI 视觉统一与 modernize

目标：

- 统一 modal / picker / manager 的背景、边框、留白、状态表达
- 继续优化 pane 标题栏状态布局
- 让 terminal manager 看起来更像真正的资源管理页
- 底栏左侧快捷键统一为 zellij 风格 segment 带
- pane 顶部 chrome 统一为单线边框

### Phase 5. terminal 更完整管理模型

目标：

- 继续明确 tags / rules / workspace/layout 的关系
- 决定是否引入更完整的 terminal-only 管理面
- 决定终端属性编辑的长期模型

---

## 5. 当前明确暂缓

这些事情现在不建议先做：

- 全量重写 TUI
- 再造一套全新概念体系
- 过早扩展过多 terminal rules UI
- 脱离测试直接大改输入系统
- 先做花哨视觉而不先收口交互

---

## 6. 当前推荐开发原则

后续继续开发时，统一遵守：

1. 先补测试，再改实现
2. 优先补 e2e 锁主场景
3. 用户可见概念只用 `workspace / tab / pane / terminal`
4. 所有复杂行为都要能用一句用户语言解释清楚
5. 不再新增补丁式文档；改动必须回到主文档

---

## 7. 当前一句话路线图

termx TUI 下一阶段的主线不是“继续发散设计”，而是：

- `先把文档和概念彻底收口`
- `再把交互负担和复杂边界继续压平`
- `最后把视觉和高级管理能力做完整`
