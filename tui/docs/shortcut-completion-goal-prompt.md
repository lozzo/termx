# 快捷键完整收口 `/goal` Prompt

把下面内容作为新的 `/goal` 请求发送给 Codex：

```text
/goal

目标：严格按照仓库根 workflow.md 依次完成 KS012-KS017，把 TUI 快捷键系统收口为可配置、可执行、可展示、可点击、可测试的单一交互 action 与快捷键真值体系。中立 tui/action 拥有 keyboard/mouse/drag/CTA 共用 action identity 与 invocation，tui/shortcut 只拥有 scene+key 编译和快捷键展示覆盖。不要恢复旧 tui.keymap、旧 TUI、legacy fallback 或第二套 action/key catalog。

执行要求：

1. 每轮首先完整读取 workflow.md、AGENTS.md、tui/AGENTS.md（若存在）、tui/docs/shortcut-system-plan.md、tui/docs/architecture.md 和当前切片直接涉及的文档。
2. 检查 git status --short --branch。不得覆盖、回退或混入未识别的用户/其他 Agent 改动。
3. 只选择 workflow.md 中最早的进行中或待开始 KS 切片；CLI002-CLI006 和其他暂停/延后切片不得并行推进。
4. 待开始切片先改为进行中。一次只实现一个 KS 切片，不跨阶段提前补后续功能。
5. 开始编码前，明确本阶段的 domain owner、truth source、raw input -> InputEvent -> binding -> ActionInvocation -> reducer/effect -> render feedback 消息链路，以及失败条件。
6. 架构正确性优先于改动规模。若旧代码、重复 registry、fallback、adapter 或历史测试阻止单一真值，可以在 workflow 允许范围内直接删除或重写；宁可删除大量旧代码，也不得叠加补丁、双路径、兼容分支或 case-specific if。
7. 先写能证明当前切片目标模型的最小 harness，再实现真实行为。KS012 只完整分类 debt 并禁止未分类/新增 gap，不提前修复 KS013-KS016；从 KS015 起，每个默认 binding 要么到达真实 reducer/effect/service 并产生可观察结果，要么从默认 catalog 删除。最终不得保留 placeholder、只显示提示但不工作的 action。
8. renderer 只消费 view-model/invocation projection，不读取配置或 runtime service；service 不得直接修改 reducer-owned state；terminal lifecycle/history/endpoint truth 继续遵守现有架构边界。
9. 运行 workflow.md 为当前 KS 切片规定的全部测试准入。测试失败必须定位 owner/contract 根因，禁止用 fallback、刷新、重复 dispatch 或放宽断言掩盖。

每阶段强制双 Agent 门禁：

10. 实现和测试完成后，在提交前同时启动两个独立、只读的子 Agent。
11. 架构 reviewer 提示词：
    “只读审核当前 KS 切片的阶段实现 diff；排除 reviewer PASS 后机械回填的 workflow 状态/审查证据。读取 workflow.md、AGENTS.md、tui/docs/shortcut-system-plan.md、tui/docs/architecture.md 和相关实现。重点检查 domain owner、单一 truth、输入到 reducer 的消息链路、render/app/config 边界、重复 registry/fallback/adapter、旧代码删除是否彻底、是否存在为局部 case 写的补丁。按严重度给 findings；无 finding 时明确输出 PASS。不要修改文件。”
12. 代码 reviewer 提示词：
    “只读审核当前 KS 切片的阶段实现 diff 和测试；排除 reviewer PASS 后机械回填的 workflow 状态/审查证据。重点检查行为 bug、按键规范化、scene 优先级、键盘/点击等价、PTY 输入泄漏、状态竞态、错误处理、endpoint 路由、配置替换语义、窄屏/隐藏状态回归、测试是否真正覆盖失败条件。按严重度给 findings；无 finding 时明确输出 PASS。不要修改文件。”
13. 主 Agent 独立判断两个 reviewer 的 findings。修复所有有效 finding，重跑受影响测试，然后把更新后的阶段实现 diff 交给原 reviewer 复审。
14. 架构 reviewer 和代码 reviewer 都对阶段实现 diff 明确 PASS 前，不得标记完成、不得提交、不得进入下一阶段。子 Agent 不可用时将切片标记阻塞，不得以自审或单 reviewer 降级。
15. 两个 reviewer PASS 后，只允许在 workflow.md 机械记录 reviewer PASS、已处理 findings 摘要并把切片改为完成；运行 git diff --check 后直接使用中文提交信息提交，不得 amend。若同时修改任何实现、测试、其他文档或非审查元数据，必须把变化交原两个 reviewer 复审。
16. 提交后确认工作树干净；若 /goal 仍继续，重新从第 1 步进入下一个 KS 切片，直到 KS012-KS017 全部完成或出现 workflow 定义的真实阻塞。

最终完成标准：

- 中立 tui/action 是 keyboard、mouse、drag、CTA 共用 action identity、invocation、参数 schema 和默认语义 label 的唯一 domain owner；app handler registry 拥有 handler contract、组合步骤和失败语义。
- tui/shortcut 只拥有 scene+key binding、快捷键 label override/show/capability；用户 tui.shortcuts 编译结果是键盘路由与快捷键操作提示的唯一真值，Esc 仅保留为 reducer-owned 全局返回导航。
- 默认 catalog 中不存在 placeholder 或无 handler action。
- keyboard、mouse、drag、footer、Help、overlay、chrome 和 content CTA 对同一动作使用 tui/action 的同一 canonical ActionInvocation。
- 普通 TTY、Kitty CSI-u、修饰键、shortcut lock、sticky/copy/overlay scene 和 PTY passthrough 均有端到端守卫。
- 所有硬编码可操作键位文案、重复 action registry、legacy fallback 和被替代的旧代码已经删除。
- 完整示例可加载，文档与运行 catalog 有自动一致性测试，规定的全量/黑盒测试全部通过。
```
