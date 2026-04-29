# termx Agent Notes

当前项目根目录：`termx-core/`

## TUI / UI Rules

- `tuiv2` 的所有新 UI 配色都必须从宿主终端主题推导，不要引入与外部终端模拟器无关的固定主题。
- 配色入口统一以 `tuiv2/render/styles.go` 中的 `uiThemeFromHostColors` 为中心；新视觉 token 应优先复用或扩展这一层。
- overlay / modal / tree / picker 等新组件在做视觉强化时，必须继续遵守“host terminal theme + semantic accent”的模型。
- modal 内不要展示快捷键字符串；快捷键属于全局输入系统，应放在 status bar / help / 外部文档。
- 选中态、高亮态、状态标签尽量通过 `lipgloss/v2` 的 style/token/chip 语法表达，而不是仅靠裸文本前缀。
- tree/list/item 的差异优先使用文本颜色、粗细、下划线、前导 marker 表达，不要依赖背景色块来区分不同 item 状态。
- 同一个 modal/surface 内，主体区域的空白背景应保持一致；不要出现树区是一个底色、未填充预览区又是另一个底色的情况。
- 如果某块内容要模拟“真实 terminal 预览”，其默认空白应尽量回到宿主终端默认背景，不要额外铺一层人工灰底。

## Architecture / Refactor Rules

- `tuiv2/app` 不要直接把同一个业务事务同时散落写入 `workbench` 与 `runtime`；pane-terminal 绑定、owner handoff、resize 协调必须优先收口到 `orchestrator` 或明确的 service。
- `Visible*` / `AdaptVisibleState*` / render projection 路径必须保持纯读；禁止在这些路径里做 normalize、补状态、修 cache 或任何隐式 mutation。
- `render` 层不要直接依赖 `input.DefaultBindingCatalog()` 这类输入绑定文档来拼 modal/footer 文案；render 只消费已经整理好的语义 view-model，快捷键说明放在 status/help。
- `render/coordinator.go` 不要继续叠加新的业务编排、输入语义分支或状态修复逻辑；新增逻辑优先拆到独立 projection/layout/overlay/hit-testing 模块。
- screen update / snapshot / bootstrap 相关传输协议必须保持二进制编码；不要把这条链路改成 JSON，也不要为兼容或调试回退到 JSON 作为线上传输格式。
- 如果宿主终端没有返回 palette，新样式 fallback 也必须先从 host FG/BG 推导；引入固定品牌色作为默认视觉基底需要显式说明理由。
- 提交代码时，commit message 必须尽可能详细，准确写清动机、范围、关键实现与行为变化；不要用过于简短或模糊的提交说明。
