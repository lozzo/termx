# Terminal History 逻辑行 RFC 结构稿（2026-05-21）

## 说明

- 这不是正式 RFC。
- 这是一份 RFC 风格的结构稿，只提供章节骨架和建议填法。
- 目的是让后续讨论可以逐步收敛成 RFC，而不是现在就把现有设计文档硬改成 RFC。
- 现有语义内容请优先参考：
  - [逻辑行设计文档](./terminal-history-logical-line-design-2026-05-21.md)
  - [逻辑行模型复盘](./terminal-history-logical-line-model-review-2026-05-21.md)

## 建议文件头

```md
# RFC: Terminal History Logical-Line Model

- Status: Draft
- Authors:
- Created:
- Updated:
- Scope: termx-core / termx-vterm / tuiv2
- Depends on:
- Supersedes:
- Superseded by:
```

## 建议目录

```md
1. 摘要
2. 背景
3. 问题定义
4. 目标
5. 非目标
6. 术语
7. 设计总览
8. 数据模型
9. Ownership 模型
10. 状态转移与事件语义
11. 历史提交规则
12. 投影与 resize 规则
13. 特殊场景
14. 对外 contract 影响
15. 备选方案与取舍
16. 未决问题
17. 验证方案
18. 迁移计划
19. 风险
20. 附录
```

## 章节结构建议

### 1. 摘要

这一节建议只回答三件事：

- 我们要把什么模型改掉；
- 要引入什么新模型；
- 这个新模型解决什么核心问题。

建议控制在 5 到 10 行。

可直接参考下面这种写法：

```md
当前终端历史模型过度依赖视觉折行，导致 resize、paging、copy mode 和 hot/cold 边界都与显示宽度耦合。本文提出以 logical line 为历史语义主单位，并区分 persisted history store、mutable live tail 与 screen projection。该模型的目标是把 resize 收敛为投影问题，把 attach/bootstrap/recovery 从历史创建语义中分离出来，并为 copy mode / replay / paging 提供稳定的逻辑行边界。
```

### 2. 背景

这一节回答：

- 现状是什么；
- 当前代码为什么会走到这里；
- 之前模型解决了什么，又留下了什么问题。

建议写法：

- 当前 `termx-core` 冷层仍然保留 wrapped 视觉行语义；
- `hotAppendRows` 等机制已经在逼近逻辑行模型；
- `tuiv2` copy mode 目前仍依赖 wrapped 元数据恢复逻辑行边界。

### 3. 问题定义

这一节不要讲方案，先把问题钉死。

建议按下面几个问题展开：

- resize 为什么会让历史语义和 observer 宽度耦合；
- 为什么“screen + hot + cold”这版直觉模型不够；
- 为什么 `1x1 -> 10000x10000` 会打穿“一路 hot->cold”的假设；
- 为什么 attach / bootstrap / full-replace 不应该被视为历史创建事件；
- 为什么 alt-screen 必须和 primary history 隔离。

### 4. 目标

建议拆成两类目标：

- 历史语义目标
- 运行时行为目标

历史语义目标示例：

- committed history 以 logical line 为核心语义单位；
- replay / paging / copy mode 天然按逻辑行工作；
- resize 不直接重写历史事实。

运行时行为目标示例：

- attach 只读取当前真相；
- process exit 是显式 mutability 边界；
- alt-screen 不污染 primary history。

### 5. 非目标

建议明确写出来，避免后续 scope 膨胀。

示例：

- 本 RFC 不要求把 VT live surface 改成纯 logical-line 结构；
- 本 RFC 不立即规定 wire/protocol 的最终编码；
- 本 RFC 不要求第一阶段就重写 tuiv2 所有消费路径。

### 6. 术语

建议把这几个术语显式定义：

- logical line
- visual row
- screen
- persisted history
- mutable live tail
- open logical line
- sealed suffix
- primary screen
- alternate screen

术语这一节要尽量短，但必须非常准。

### 7. 设计总览

这一节给出一句话总览，不展开细节。

建议用这样的结构：

```md
本设计把终端历史分成三层：

- persisted history store：持久化历史层
- mutable live tail：当前可变尾窗
- screen projection：当前二维显示投影
```

然后补一句：

- 逻辑行是历史语义单位，
- screen 是 live tail 的二维投影，
- resize 主要改变投影与 ownership 边界，而不是重写历史。

### 8. 数据模型

这一节才开始讲模型对象。

建议分成三部分：

- persisted history store 应包含什么
- mutable live tail 应包含什么
- screen projection 负责什么

建议注意：

- 这里先定义语义结构，不一定要马上贴最终 Go struct；
- 如果要贴结构体，建议放在附录。

### 9. Ownership 模型

这一节是关键章节。

建议明确：

- 哪一层拥有历史真相；
- 哪一层拥有当前可变真相；
- 哪一层只是投影；
- 哪些数据可以从 persisted history 回卷到 live tail；
- 哪些事件会终止 mutability。

这一节最好单独给一句核心原则：

```md
persisted history 是历史存储层，mutable live tail 是当前可变真相，screen 只是 live tail 的投影。
```

### 10. 状态转移与事件语义

建议按事件逐条写：

- 普通写入
- cursor move
- erase / insert / delete
- hard newline
- scroll out
- resize
- attach / reattach
- bootstrap
- recovery
- full-replace
- process exit
- enter alt-screen
- exit alt-screen
- clear screen

这一节不一定要写成形式化状态机，但建议至少有表格。

### 11. 历史提交规则

建议把“什么时候能进入 persisted history”单独抽出来。

这一节建议回答：

- 什么叫 seal；
- 什么叫 detach；
- 逻辑行在什么条件下进入 persisted history；
- process exit 是否算强制 seal；
- clear screen / attach / resize 为什么不是提交事件。

### 12. 投影与 resize 规则

这一节专门回答 resize。

建议覆盖：

- shrink 时会发生什么；
- grow 时会发生什么；
- 为什么 grow 可能需要从 persisted history reclaim sealed suffix；
- 为什么 resize 不应直接重写历史。

### 13. 特殊场景

建议把极端或容易误解的场景单独列成小节。

至少建议覆盖：

- `1x1 -> 10000x10000`
- 新客户端 attach 到已有 terminal
- terminal process exit
- alt-screen 下全屏程序
- clear screen
- recovery / bootstrap 后的 latest view

这一节最适合写“输入场景 -> 预期语义结果”。

### 14. 对外 contract 影响

这一节回答：

- `termx-core` 内部改完以后，哪些影响可能需要往外冒；
- 哪些 contract 暂时不动；
- 哪些 metadata 未来可能要显式化。

建议分成三层：

- core 内部
- vterm/core 边界
- runtime/app/wire 边界

### 15. 备选方案与取舍

建议至少比较下面几类方案：

- 继续以 wrapped visual rows 为主语义
- `screen + one-way hot + cold` 简化模型
- `persisted history + mutable live tail + screen projection`

每种方案建议写：

- 优点
- 缺点
- 为什么最后不选或暂不选

### 16. 未决问题

这一节只放真正还没拍板的问题。

建议把问题写成 decision point，不要写成散文。

例如：

- grow resize 时 reclaim 边界按什么算；
- process exit 是否总是 force seal；
- live tail 内部是否继续细分 open line 和 reclaimed suffix；
- 是否要跨 wire 暴露 ownership 语义。

### 17. 验证方案

这一节建议写：

- 单元测试方向
- 集成测试方向
- 极端场景测试方向
- 回归重点

至少建议覆盖：

- exact-width newline
- wrapped prompt + resize
- attach / reattach latest semantics
- alt-screen enter/exit
- `1x1 -> 10000x10000`

### 18. 迁移计划

这一节建议按阶段写，而不是一次写完。

推荐结构：

- Phase 0: 术语与语义冻结
- Phase 1: core 内部模型收敛
- Phase 2: snapshot / viewport / paging 适配
- Phase 3: copy mode / runtime 消费路径适配
- Phase 4: 是否扩展 protocol contract

### 19. 风险

建议按风险类型展开：

- 语义风险
- 性能风险
- 回归风险
- contract 风险

例如：

- reclaim live tail 可能导致额外内存驻留；
- 历史 / live tail ownership 不清会重新引入重复显示或缺行；
- 若 protocol 暂不显式化 ownership，runtime 仍可能保守回退。

### 20. 附录

这一节可以留给：

- 示例状态图
- 示例数据结构
- 术语速查
- 与现有代码路径的映射

## 推荐写法顺序

如果后续你要把这份结构稿真正填成 RFC，建议按这个顺序写：

1. 摘要
2. 问题定义
3. 术语
4. 设计总览
5. Ownership 模型
6. 状态转移与事件语义
7. 特殊场景
8. 对外 contract 影响
9. 未决问题
10. 迁移计划

这个顺序比先写数据结构更稳，因为当前真正难的是语义边界，而不是 struct 形状。

## 与现有文档的关系

建议把当前三份文档分工固定成：

- [逻辑行设计文档](./terminal-history-logical-line-design-2026-05-21.md)
  当前语义设计正文。
- [逻辑行模型复盘](./terminal-history-logical-line-model-review-2026-05-21.md)
  讨论结论和为什么旧模型不够。
- 本文
  RFC 风格骨架，供后续收敛成正式 RFC 使用。

这样你后面如果想删、并、改，都比较自由。
