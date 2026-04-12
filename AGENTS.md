# termx Agent Notes

## TUI / UI Rules

- `tuiv2` 的所有新 UI 配色都必须从宿主终端主题推导，不要引入与外部终端模拟器无关的固定主题。
- 配色入口统一以 `tuiv2/render/styles.go` 中的 `uiThemeFromHostColors` 为中心；新视觉 token 应优先复用或扩展这一层。
- overlay / modal / tree / picker 等新组件在做视觉强化时，必须继续遵守“host terminal theme + semantic accent”的模型。
- modal 内不要展示快捷键字符串；快捷键属于全局输入系统，应放在 status bar / help / 外部文档。
- 选中态、高亮态、状态标签尽量通过 `lipgloss/v2` 的 style/token/chip 语法表达，而不是仅靠裸文本前缀。
- tree/list/item 的差异优先使用文本颜色、粗细、下划线、前导 marker 表达，不要依赖背景色块来区分不同 item 状态。
- 同一个 modal/surface 内，主体区域的空白背景应保持一致；不要出现树区是一个底色、未填充预览区又是另一个底色的情况。
- 如果某块内容要模拟“真实 terminal 预览”，其默认空白应尽量回到宿主终端默认背景，不要额外铺一层人工灰底。
