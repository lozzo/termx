# termx TUI 当前状态

状态：TDD State Machine Stage
日期：2026-03-24

---

## 1. 当前判断

termx TUI 当前处于“文档主线已稳定，领域骨架、主入口 overlay、恢复入口状态机、启动规划层、启动任务执行层、restore store 读写闭环、runtime session bootstrap、最小 Bubble Tea 运行主线，以及 active pane 的 terminal snapshot/input、stream/event 增量消费与关键 runtime 观测/控制状态链路已按 TDD 落地”的阶段。

现状可以概括为：

- 旧版 TUI 已归档到 `deprecated/tui-legacy/`
- 新主线文档已经建立并持续作为实现约束
- 新主线代码已进入 reducer / state machine 落地期
- 当前已进入 bubbletea shell 的恢复入口、启动规划、启动任务执行、restore store 读写闭环、runtime session bootstrap、最小运行主线，以及 active pane 的 terminal snapshot/input、stream/event 增量消费与关键 runtime 观测/控制状态阶段

---

## 2. 已完成

当前已经完成的事情：

1. 旧版资产已归档
2. legacy 设计和代码已做第一轮整理
3. 新主线产品概念已重新收口
4. 新主线交互规则、线框、架构、测试策略、交付计划已成文
5. 第一批 TDD 代码骨架已经落地
6. 第二轮 TDD 已补上 `close pane` 语义和 workspace picker 树状态机
7. 第三轮 TDD 已补上 `overlay / mode / workspace picker navigation` 状态迁移
8. 第四轮 TDD 已补上 `terminal manager here / new tab / floating` effect 契约和管理状态骨架
9. 第五轮 TDD 已补上 `workspace picker query / backspace / create row prompt handoff`
10. 第六轮 TDD 已补上 `terminal manager search / edit / stop`
11. 第七轮 TDD 已补上 `prompt overlay` 和 `create workspace submit/cancel`
12. 第八轮 TDD 已补上 `metadata prompt submit/cancel`
13. 第九轮 TDD 已补上 `terminal manager` 的分组 / details / create row
14. 第十轮 TDD 已补上 `terminal manager details` 的位置列表投影
15. 第十一轮 TDD 已补上 `prompt draft` 输入模型
16. 第十二轮 TDD 已补上 `terminal manager details` 的 `visibility / owner / tags` 投影
17. 第十三轮 TDD 已补上 `create terminal` 的默认参数策略
18. 第十四轮 TDD 已补上 `prompt` 的结构化字段模型
19. 第十五轮 TDD 已补上 `prompt` 的反向字段切换和深拷贝语义
20. 第十六轮 TDD 已补上最小 bubbletea 输入映射层
21. 第十七轮 TDD 已补上最小 bubbletea shell 容器
22. 第十八轮 TDD 已补上最小 runtime effect executor 回流链路
23. 第十九轮 TDD 已补上 terminal manager 动作键映射
24. 第二十轮 TDD 已补上 runtime feedback 错误与 notice 通道
25. 第二十一轮 TDD 已补上 terminal picker 主线接线
26. 第二十二轮 TDD 已补上 notice timeout / 清理策略
27. 第二十三轮 TDD 已补上 `layout resolve` 最小恢复闭环
28. 第二十四轮 TDD 已补上 startup planner 与 layout YAML 最小导入
29. 第二十五轮 TDD 已补上 startup task executor 和 attach 启动最小闭环
30. 第二十六轮 TDD 已补上 restore store 最小加载与降级链路
31. 第二十七轮 TDD 已补上 restore save 最小持久化闭环
32. 第二十八轮 TDD 已补上 runtime session bootstrap 的 attach/event 最小闭环
33. 第二十九轮 TDD 已补上 `Run()` 最小生命周期编排
34. 第三十轮 TDD 已补上最小 Bubble Tea program runner 与 runtime renderer
35. 第三十一轮 TDD 已补上 active pane 的最小 terminal snapshot 渲染与输入转发
36. 第三十二轮 TDD 已补上 runtime stream/event 增量更新与 snapshot recovery 主线
37. 第三十三轮 TDD 已补上关键 runtime 状态向 reducer/domain 的回灌闭环
38. 第三十四轮 TDD 已补上 `state_changed` 剩余状态的 runtime 同步闭环
39. 第三十五轮 TDD 已补上 `resized / collaborators_revoked` 的 runtime 观测与输入阻断闭环
40. 第三十六轮 TDD 已补上 `cmd/termx attach` 的 `PrefixTimeout` 配置透传闭环
41. 第三十七轮 TDD 已补上 `EventTerminalCreated` 到 `register_terminal` 的 runtime 回灌闭环
42. 第三十八轮 TDD 已补上 notice 聚合/去重与 timeout 刷新闭环
43. 第三十九轮 TDD 已补上 runtime renderer 的 notice 可见性闭环
44. 第四十轮 TDD 已补上 workspace picker 的 runtime renderer 可视化闭环
45. 第四十一轮 TDD 已补上 terminal manager 的 runtime renderer 可视化闭环
46. 第四十二轮 TDD 已补上 prompt overlay 的 runtime renderer 可视化闭环
47. 第四十三轮 TDD 已补上 terminal picker 的 runtime renderer 可视化闭环
48. 第四十四轮 TDD 已补上 layout resolve 的 runtime renderer 可视化闭环
49. 第四十五轮 TDD 已补上 runtime mode 状态的 renderer 可见性闭环
50. 第四十六轮 TDD 已补上 terminal manager detail 连接信息的 renderer 可视化闭环
51. 第四十七轮 TDD 已补上 runtime focus 状态的 renderer 可见性闭环
52. 第四十八轮 TDD 已补上 exited pane 终态信息的 renderer 可见性闭环
53. 第四十九轮 TDD 已补上 active pane owner/follower 连接角色的 renderer 可见性闭环
54. 第五十轮 TDD 已补上 active pane 连接计数的 renderer 可见性闭环
55. 第五十一轮 TDD 已补上 active terminal 元数据的 renderer 可见性闭环
56. 第五十二轮 TDD 已补上 active pane 形态的 renderer 可见性闭环
57. 第五十三轮 TDD 已补上 active tab layer 的 renderer 可见性闭环
58. 第五十四轮 TDD 已补上 prompt handoff 目标 terminal 的 renderer 可见性闭环
59. 第五十五轮 TDD 已补上 layout resolve 目标 pane 的 renderer 可见性闭环
60. 第五十六轮 TDD 已补上 terminal manager detail terminal id 的 renderer 可见性闭环
61. 第五十七轮 TDD 已补上 terminal picker 选中 terminal 的 renderer 可见性闭环
62. 第五十八轮 TDD 已补上 workspace picker 选中节点的 renderer 可见性闭环
63. 第五十九轮 TDD 已补上 terminal picker 选中终端状态的 renderer 可见性闭环
64. 第六十轮 TDD 已补上 terminal manager 选中 terminal 的 renderer 可见性闭环
65. 第六十一轮 TDD 已补上 layout resolve 选中动作的 renderer 可见性闭环
66. 第六十二轮 TDD 已补上 terminal manager 选中终端状态的 renderer 可见性闭环
67. 第六十三轮 TDD 已补上 prompt 活动字段的 renderer 可见性闭环
68. 第六十四轮 TDD 已补上 workspace picker 选中节点类型的 renderer 可见性闭环
69. 第六十五轮 TDD 已补上 terminal picker 选中终端标签的 renderer 可见性闭环
70. 第六十六轮 TDD 已补上 terminal manager 选中终端标签的 renderer 可见性闭环
71. 第六十七轮 TDD 已补上 layout resolve 选中动作标签的 renderer 可见性闭环
72. 第六十八轮 TDD 已补上 workspace picker 选中节点标签的 renderer 可见性闭环
73. 第六十九轮 TDD 已补上 terminal manager 选中终端分区的 renderer 可见性闭环
74. 第七十轮 TDD 已补上 terminal picker 选中行类型的 renderer 可见性闭环
75. 第七十一轮 TDD 已补上 terminal manager 选中行类型的 renderer 可见性闭环
76. 第七十二轮 TDD 已补上 prompt 活动字段标签的 renderer 可见性闭环
77. 第七十三轮 TDD 已补上 terminal manager 选中行连接计数的 renderer 可见性闭环
78. 第七十四轮 TDD 已补上 terminal manager 选中行命令的 renderer 可见性闭环
79. 第七十五轮 TDD 已补上 prompt 活动字段值的 renderer 可见性闭环
80. 第七十六轮 TDD 已补上 terminal manager 选中行可见性的 renderer 可见性闭环
81. 第七十七轮 TDD 已补上 workspace picker 选中节点展开态的 renderer 可见性闭环
82. 第七十八轮 TDD 已补上 workspace picker 选中节点命中态的 renderer 可见性闭环
83. 第七十九轮 TDD 已补上 workspace picker 选中节点深度的 renderer 可见性闭环
84. 第八十轮 TDD 已补上 prompt 活动字段索引的 renderer 可见性闭环
85. 第八十一轮 TDD 已补上 prompt 字段数量的 renderer 可见性闭环
86. 第八十二轮 TDD 已补上 active terminal 可见性的 renderer 可见性闭环
87. 第八十三轮 TDD 已补上 terminal manager detail 可见性的 renderer 可见性闭环
88. 第八十四轮 TDD 已补上 terminal manager detail 位置数量的 renderer 可见性闭环
89. 第八十五轮 TDD 已补上 layout resolve 选项数量的 renderer 可见性闭环
90. 第八十六轮 TDD 已补上 terminal picker 数量的 renderer 可见性闭环
91. 第八十七轮 TDD 已补上 workspace picker 数量的 renderer 可见性闭环
92. 第八十八轮 TDD 已补上 terminal manager 数量的 renderer 可见性闭环
93. 第八十九轮 TDD 已补上 terminal picker 命令的 renderer 可见性闭环
94. 第九十轮 TDD 已补上 terminal picker 可见性的 renderer 可见性闭环
95. 第九十一轮 TDD 已补上 terminal picker 标签的 renderer 可见性闭环
96. 第九十二轮 TDD 已补上 terminal picker 连接数量的 renderer 可见性闭环
97. 第九十三轮 TDD 已补上 terminal manager 标签的 renderer 可见性闭环
98. 第九十四轮 TDD 已补上 terminal manager owner 的 renderer 可见性闭环
99. 第九十五轮 TDD 已补上 terminal manager visibility 标签的 renderer 可见性闭环
100. 第九十六轮 TDD 已补上 terminal manager 位置数量的 renderer 可见性闭环
101. 第九十七轮 TDD 已补上 terminal manager detail owner 的 renderer 可见性闭环
102. 第九十八轮 TDD 已补上 terminal manager detail visibility 标签的测试闭环
103. 第九十九轮 TDD 已补上 terminal manager detail state 的测试闭环
104. 第一百轮 TDD 已补上 terminal manager detail command 的测试闭环
105. 第一百零一轮 TDD 已补上 terminal manager parked detail visibility 的测试闭环
106. 第一百零二轮 TDD 已补上 terminal manager parked detail 连接数量的测试闭环
107. 第一百零三轮 TDD 已补上 terminal manager parked detail 位置数量的测试闭环
108. 第一百零四轮 TDD 已补上 terminal manager parked detail 剩余字段的测试闭环
109. 第一百零五轮 TDD 已补上 terminal manager parked selected 剩余字段的测试闭环
110. 第一百零六轮 TDD 已补上 terminal manager overlay 结构字段的测试闭环
111. 第一百零七轮 TDD 已补上 terminal manager edit 与 stop 场景的测试闭环
112. 第一百零八轮 TDD 已补上 terminal manager search 与 create row 场景的测试闭环
113. 第一百零九轮 TDD 已补上 terminal manager 剩余动作键路径的测试闭环
114. 第一百一十轮 TDD 已补上 terminal picker 主要动作路径的测试闭环
115. 第一百一十一轮 TDD 已补上 workspace picker 主要动作路径与 terminal picker submit 的测试闭环
116. 第一百一十二轮 TDD 已补上 prompt 主要交互路径的测试闭环
117. 第一百一十三轮 TDD 已补上 layout resolve 主要动作路径的测试闭环
118. 第一百一十四轮 TDD 已补上 workspace picker 树展开收起的测试闭环
119. 第一百一十五轮 TDD 已补上 shell mode/global 路径的测试闭环
120. 第一百一十六轮 TDD 已补上 runtime 观测字段剩余可见性的测试闭环
121. 第一百一十七轮 TDD 已补上 runtime status 与初始状态投影的测试闭环
122. 第一百一十八轮 TDD 已补上 runtime overlay 进入与退出投影的测试闭环
123. 第一百一十九轮 TDD 已补上 runtime 剩余 prompt 与 resolve 字段投影的测试闭环
124. 第一百二十轮 TDD 已补上 runtime effect 执行链路的测试闭环
125. 第一百二十一轮 TDD 已补上 runtime effect 失败路径的测试闭环
126. 第一百二十二轮 TDD 已补上 runtime 拓扑动作不支持时的显式失败语义
127. 第一百二十三轮 TDD 已补上 runtime 窗口尺寸变化到 resize 下发闭环
128. 第一百二十四轮 TDD 已补上 shared terminal 下 owner/follower 的 runtime resize 约束
129. 第一百二十五轮 TDD 已补上 terminal manager 的 acquire owner 最小闭环
130. 第一百二十六轮 TDD 已补上 terminal manager 跟随 runtime/domain 变化的投影刷新
131. 第一百二十七轮 TDD 已补上 metadata 控制面的 owner 权限约束
132. 第一百二十八轮 TDD 已补上 workspace tree jump 的 tab auto-acquire owner
133. 第一百二十九轮 TDD 已补上 stop terminal 控制面的 owner 权限约束
134. 第一百三十轮 TDD 已补上 stop / metadata 控制面的成功回灌语义
135. 第一百三十一轮 TDD 已补上 create / new-tab / floating 控制面的成功回灌语义
136. 第一百三十二轮 TDD 已补上 runtime 主视图的固定状态头与 screen 预览裁剪
137. 第一百三十三轮 TDD 已补上 overlay 列表与 notice 的预览裁剪
138. 第一百三十四轮 TDD 已补上 runtime 主视图的 section 分区
139. 第一百三十五轮 TDD 已补上 runtime 主视图的紧凑骨架与空区块占位
140. 第一百三十六轮 TDD 已补上 runtime 主视图的 chrome header/body/footer 外壳
141. 第一百三十七轮 TDD 已补上 overlay 优先的 body 让路压缩
142. 第一百三十八轮 TDD 已补上 header/footer 语义状态栏
143. 第一百三十九轮 TDD 已补上 body 主体状态栏语义
144. 第一百四十轮 TDD 已补上 terminal/screen/overlay section bar
145. 第一百四十一轮 TDD 已补上 overlay 内部 summary bar
146. 第一百四十二轮 TDD 已补上 notices 分层摘要栏
147. 第一百四十三轮 TDD 已补上 runtime 栏位长文本裁剪
148. 第一百四十四轮 TDD 已补上 runtime 摘要行宽度预算
149. 第一百四十五轮 TDD 已补上 runtime detail 行宽度预算
150. 第一百四十六轮 TDD 已补上 runtime program alt-screen 外壳
151. 第一百四十七轮 TDD 已补上 runtime program 鼠标事件外壳
152. 第一百四十八轮 TDD 已补上 overlay 最小鼠标滚轮导航
153. 第一百四十九轮 TDD 已补上 overlay 可见行的最小鼠标点击选中
154. 第一百五十轮 TDD 已补上 overlay 已选中行的最小鼠标点击提交
155. 第一百五十一轮 TDD 已补上 prompt 结构化字段的最小鼠标点击切换
156. 第一百五十二轮 TDD 已补上 picker / resolve 非当前行的单击直达默认动作
157. 第一百五十三轮 TDD 已补上 terminal manager create row 的单击直达创建
158. 第一百五十四轮 TDD 已补上 terminal manager 详情动作的可点击操作行
159. 第一百五十五轮 TDD 已补上 prompt 动作行的鼠标点击提交与取消
160. 第一百五十六轮 TDD 已补上 floating connect 成功后的本地 pane 落地与鼠标入口闭环

对应文档：

- `product-spec.md`
- `interaction-spec.md`
- `wireframes.md`
- `architecture.md`
- `testing-strategy.md`
- `implementation-roadmap.md`

当前已经落到代码里的支点：

- `tui/domain/types`
- `tui/domain/layout`
- `tui/domain/connection`
- `tui/domain/workspace`
- `tui/domain/prompt`
- `tui/domain/terminalmanager`
- `tui/domain/terminalpicker`
- `tui/domain/layoutresolve`
- `tui/app/intent`
- `tui/app/reducer`
- `tui/bt`
- `tui/startup_plan.go`
- `tui/startup_bootstrap.go`
- `tui/runtime_session.go`
- `tui/runtime.go`
- `tui/runtime_program.go`
- `tui/runtime_renderer.go`
- `tui/client.go`

当前已经落到代码里的能力：

- `layout` 纯逻辑树和矩形投影
- `connection` 的 connect / owner / migrate 基线
- `workspace picker` 树构建与 query 命中祖先展开
- `workspace tree jump` 焦点决议
- `ConnectTerminalIntent`
- `StopTerminalIntent`
- `TerminalProgramExitedIntent`
- `ClosePaneIntent`
- `WorkspaceTreeJumpIntent`
- `OpenWorkspacePickerIntent`
- `CloseOverlayIntent`
- overlay 可见行的鼠标点击选中
- overlay 已选中行的鼠标点击提交默认动作
- prompt 结构化字段的鼠标点击切换
- picker / resolve 非当前行的单击直达默认动作
- terminal manager create row 的单击直达创建
- terminal manager 详情动作的可点击操作行
- `WorkspacePickerMoveIntent`
- `WorkspacePickerAppendQueryIntent`
- `WorkspacePickerBackspaceIntent`
- `WorkspacePickerExpandIntent`
- `WorkspacePickerCollapseIntent`
- `WorkspacePickerSubmitIntent`
- runtime `header/footer/body` 与各类 overlay / terminal / notice bar 的长文本中间裁剪
- section 首行与正文摘要保持完整拼接，不再被 bar 裁剪策略误伤
- runtime `status/section 首行/overlay 合并首行` 现在也有更宽的摘要宽度预算，只拦极端长文本，不截正常主线路径
- runtime 摘要行现在统一保留头尾关键语义，避免 workspace/tab/title/query 过长时把整行横向撑爆
- runtime `command/tags/value/hint/locations` 这类 detail 元数据行现在也有独立宽度预算，长自由文本不会再把 overlay/detail 区横向撑爆
- detail 宽度预算只作用在高风险自由文本字段，不影响 terminal manager 等正常短状态行的完整可见性
- runtime 的真实 Bubble Tea program 现在会进入并退出 alt-screen，避免启动后退化成普通 shell 滚屏
- 这让 `header/body/footer` chrome 壳层在真实运行时也有机会稳定留在可视区，而不只是停留在 `model.View()` 测试里
- runtime 的真实 Bubble Tea program 现在也会打开 mouse cell-motion，后续 pane/overlay 鼠标命中交互不需要再返工 program runner
- 运行壳层目前已经开始从“最小能跑”转向“真实交互容器”，重点收口 alt-screen、鼠标事件和可视稳定性
- `bt` 模型现在已经能接住 `tea.MouseMsg`，overlay 列表的滚轮上下会直接翻译成 move intent
- `workspace picker / terminal manager / terminal picker / layout resolve` 已经具备最小鼠标滚轮导航，不再只靠键盘上下键
- `OpenTerminalManagerIntent`
- `OpenPromptIntent`
- `TerminalManagerMoveIntent`
- `TerminalManagerAppendQueryIntent`
- `TerminalManagerBackspaceIntent`
- `TerminalManagerConnectHereIntent`
- `TerminalManagerConnectInNewTabIntent`
- `TerminalManagerConnectInFloatingPaneIntent`
- `TerminalManagerEditMetadataIntent`
- `TerminalManagerStopIntent`
- `TerminalManagerCreateTerminalIntent`
- `OpenTerminalPickerIntent`
- `TerminalPickerMoveIntent`
- `TerminalPickerAppendQueryIntent`
- `TerminalPickerBackspaceIntent`
- `TerminalPickerSubmitIntent`
- `OpenLayoutResolveIntent`
- `LayoutResolveMoveIntent`
- `LayoutResolveSubmitIntent`
- `SubmitPromptIntent`
- `CancelPromptIntent`
- `PromptAppendInputIntent`
- `PromptBackspaceIntent`
- `PromptNextFieldIntent`
- `PromptPreviousFieldIntent`
- `ActivateModeIntent`
- `ModeTimedOutIntent`

本轮新增并通过测试的能力：

- 已补上一条 runtime 场景型 E2E：metadata prompt submit 失败后主视图显示错误 notice
- 已补上一条 runtime 场景型 E2E：terminal manager stop 失败后主视图显示错误 notice
- 已补上一条 runtime 场景型 E2E：terminal manager connect in new tab 失败后主视图显示错误 notice
- 已补上一条 runtime 场景型 E2E：terminal manager connect in floating pane 失败后主视图显示错误 notice
- 已补上一条 runtime 场景型 E2E：terminal manager create row submit 失败后主视图显示错误 notice
- 已补上一条 runtime 场景型 E2E：terminal picker create row submit 失败后主视图显示错误 notice
- runtime effect 的成功/失败两侧现在都被场景测试锁住，失败时会通过 notice 回灌到 runtime 主视图
- runtime 现在通过 `runtimeDependencies.RuntimeExecutor` 显式接线 effect executor，不再固定为空执行器
- 已补上一条 runtime 场景型 E2E：metadata prompt submit 后会真正调用 terminal metadata 更新服务
- 已补上一条 runtime 场景型 E2E：terminal manager stop 后会真正调用 terminal kill 服务
- 已补上一条 runtime 场景型 E2E：terminal manager connect in new tab 后会真正调用 new-tab 服务
- 已补上一条 runtime 场景型 E2E：terminal manager connect in floating pane 后会真正调用 floating-pane 服务
- 已补上一条 runtime 场景型 E2E：terminal manager create row submit 后会真正调用 terminal create 服务
- 已补上一条 runtime 场景型 E2E：terminal picker create row submit 后会真正调用 terminal create 服务
- runtime 的 effect 触发现在不只验证 overlay 关闭，也验证服务调用参数被正确发出
- 已补上一条 runtime 场景型 E2E：terminal manager 初始详情区现在显式锁住 `detail_locations:`
- 已补上一条 runtime 场景型 E2E：terminal manager create row 选中态显式锁住 `overlay: terminal_manager` 与 `focus_overlay_target: terminal_manager`
- 已补上一条 runtime 场景型 E2E：metadata prompt `Tab` 到第二字段后主视图显式锁住 `prompt_terminal/prompt_active_field=tags/prompt_active_label/prompt_active_value/prompt_active_index`
- 已补上一条 runtime 场景型 E2E：terminal picker missing query create row 显式锁住 `overlay: terminal_picker` 与 `focus_overlay_target: terminal_picker`
- 已补上一条 runtime 场景型 E2E：layout resolve 初始主视图显式锁住 `layout_resolve_role` 与 `layout_resolve_hint`
- 已补上一条 runtime 场景型 E2E：layout resolve create new / skip 关闭后主视图不再残留 `focus_overlay_target` 与 `mode`
- runtime 剩余的 prompt 第二字段、resolve role/hint、manager detail locations 与 picker create row 打开态现在被场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-w` 打开 workspace picker 时主视图显式显示 `overlay: workspace_picker`
- 已补上一条 runtime 场景型 E2E：workspace picker create row 打开 prompt 时主视图显式显示 `focus_overlay_target: prompt`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开 terminal manager 时主视图显式显示 `overlay: terminal_manager` 与 `focus_overlay_target: terminal_manager`
- 已补上一条 runtime 场景型 E2E：terminal manager edit 打开 prompt 时主视图显式显示 `overlay: prompt`
- runtime terminal service 对 `new tab / floating pane` 的客户端能力缺失不再静默成功，改为显式错误返回
- 已补上 runtime handler 单测：`WindowSizeMsg` 会真正下发 `Resize(channel, cols, rows)`，并同步刷新 runtime store 中的 `runtime_size`
- 已补上一条 runtime 场景型 E2E：活动 terminal 收到窗口尺寸变化后，主视图会显示更新后的 `runtime_size: 120x40`
- runtime 的窗口尺寸变化现在会遵守 shared terminal 的 owner/follower 规则，follower pane 不再隐式下发 terminal resize
- 已补上 runtime handler 单测：共享 terminal 的 follower 收到 `WindowSizeMsg` 时不会发出 `Resize`，也不会改写 `runtime_size`
- 已补上一条 runtime 场景型 E2E：follower pane 成为活动 pane 时，窗口尺寸变化不会越权改写 shared terminal 的运行尺寸
- terminal manager 现在已支持对当前选中 terminal 发起 `acquire owner`
- reducer 现在会把 owner 转移到 overlay 的 return-focus pane，并重建 terminal manager 投影，保证 `selected_owner/detail_owner` 同步更新
- 已补上一条 runtime 场景型 E2E：follower pane 可先通过 terminal manager 获取 owner，再继续触发 runtime resize
- reducer 现在会在 terminal manager 打开期间跟随 domain 变化自动重建 overlay 投影，避免 terminal state/selection/detail 陈旧
- 已补上 reducer 测试：terminal removed 与 terminal stopped 都会刷新 terminal manager 的 selected row/detail
- 已补上一条 runtime 场景型 E2E：terminal manager 打开时收到 runtime removed event，主视图会直接切到新的选中 terminal 详情
- terminal metadata 现在被明确收紧到 owner 控制面：有连接关系时，只有 owner pane 可以发起或提交 metadata 更新
- 已新增本地 `notice effect`，用于把 reducer 里的权限拒绝回流到 runtime notice，而不是静默失败
- 已补上一条 runtime 场景型 E2E：follower pane 直接编辑 metadata 会留在 terminal manager 并显示 acquire-owner notice；获取 owner 后再编辑可正常进入 prompt
- `WorkspaceTreeJump` 现在会在目标 tab 开启 `AutoAcquireOwner` 时自动迁移 shared terminal 的 owner 到目标 pane
- 已补上 reducer 测试：开启 auto-acquire 时 workspace jump 会转移 owner，关闭时保持原 owner
- 已补上一条 runtime 场景型 E2E：workspace picker 跳转到配置了 auto-acquire 的目标 tab 后，主视图直接显示目标 pane 已成为 `connection_role: owner`
- `stop terminal` 现在也被收紧到 owner 控制面：shared terminal 的 follower 不能直接 stop，必须先 acquire owner
- 已补上 reducer 测试：terminal manager 在无 owner 权限时 stop 会保留 overlay，并回流 acquire-owner notice
- 已补上两条 runtime 场景型 E2E：follower 直接 stop 会显示 owner notice；获取 owner 后再 stop 会真正 kill shared terminal 并清空全部连接 pane
- `stop terminal` 与 metadata submit 现在都改成 runtime service 成功后再回灌 success intent，不再由 reducer 先做乐观本地提交
- metadata 更新失败时，prompt 会保留并显示错误 notice，避免出现“标题已变但服务失败”的假成功
- stop terminal 失败时，terminal manager 会保留并显示错误 notice，避免出现“pane 已清空但 kill 失败”的假成功
- `create terminal`、`connect in new tab`、`connect in floating pane` 现在也改成 runtime service 成功后再回灌 success intent，失败时 overlay 保持原位等待重试
- create terminal 成功后，reducer 会立即把新 terminal 注册进 domain，避免在 event 到达前 manager/picker 视图完全无感
- 已补上 reducer / bt / runtime 场景测试：manager、picker、layout resolve 的 create 成功后关闭 overlay 并注册 terminal，失败时保留原 overlay 并显示 notice
- runtime 主视图现在会把 active pane 的 `screen` 限制为尾部预览，而不是整屏全量展开，避免 shell 内容把顶部 `workspace/tab/pane/overlay` 状态直接顶出可视区
- renderer 现在额外输出 `screen_rows` 与 `screen_truncated`，显式说明当前看到的是 screen 预览而不是完整快照
- 已补上 renderer 测试：长 snapshot 只保留最后 8 行，同时保持头部状态字段稳定可见
- workspace picker、terminal manager、terminal picker、layout resolve、prompt、notices 现在都会输出受限预览，不再把长列表整段展开到主视图
- overlay 预览会尽量围绕当前选中项或活动字段截取，保证焦点仍然处在可视窗口内
- notices 现在默认只保留最近几条在主视图里显示，避免高频错误把上半屏全部刷掉
- 已补上 renderer 测试：workspace picker 预览会保留底部选中项，prompt 预览会保留活动字段，notice 预览会保留最新几条
- runtime 主视图现在会显式输出 `summary` 和 `section_status/section_terminal/section_screen/section_overlay/section_notices`，把状态、正文、overlay、notice 分成稳定区块
- 已补上 renderer 测试：active pane、workspace picker、prompt、notice 在 section 分区下仍然保持原有关键字段可见
- runtime 主视图现在会把 `status/terminal` 元数据压成紧凑多字段行，并为 `terminal/screen/overlay/notices` 始终保留固定区块，占位态下也不会整段消失
- connected pane 在没有 snapshot 时，主视图现在改为显示 `screen: <unavailable>`，而不是直接缺失 screen 区块
- 已补上 renderer / runtime 场景测试：active pane 主视图行数被收紧，terminal manager 重 overlay 视图也被压到稳定预算内
- runtime 主视图现在进一步分成 `chrome_header / chrome_body / chrome_footer` 三层外壳，`summary + status` 固定在 header，`terminal/screen/overlay` 固定在 body，`notices` 固定在 footer
- 已补上 renderer / runtime 场景测试：header/body/footer 顺序稳定，overlay 不会掉进 footer，footer 的 notices 占位会始终留在底部
- overlay 打开时，runtime body 现在会切到压缩模式：screen 预览改成 `screen: <suppressed by overlay>`，terminal 只保留最小上下文，优先让 overlay 本身保持可读
- terminal manager 的 rows 预览窗口进一步收紧，detail locations 也压成单行摘要，避免 overlay 自己再次把 body 撑爆
- 已补上 renderer 测试：overlay active 时 screen 行正文不会继续展开，terminal 非关键细节会被抑制，整体行数预算继续受控
- runtime `chrome_header` 现在不再只放原始 summary，而是显式输出 `header_bar`，把 `workspace/tab/pane/slot/overlay/focus/mode` 汇总成真正的顶栏语义
- runtime `chrome_footer` 现在显式输出 `footer_bar`，把 `notices` 数量、最后一条级别和当前 overlay 汇总成底栏语义
- 已补上 renderer / runtime 场景测试：active pane、empty pane、notice 场景都锁住 `header_bar/footer_bar`，防止状态栏语义再次退化成零散文本
- runtime `chrome_body` 现在也显式输出 `body_bar`，把 `terminal/screen/overlay` 的主体状态统一汇总成一行，先回答“正文现在在看什么”
- `body_bar` 会区分 `preview/unavailable/suppressed` 三种 screen 状态，也会在 active terminal 上显式汇总 `terminalID:runState`
- 已补上 renderer / runtime 场景测试：active pane、empty pane、overlay 压缩态都锁住 `body_bar`，避免 body 重新退化成只剩 section 名称
- `section_terminal/section_screen/section_overlay` 现在各自也有 `terminal_bar/screen_bar/overlay_bar`，把 section 的第一层摘要稳定下来，再往后才是细节
- `terminal_bar` 会汇总 `id/title/state/role`，`screen_bar` 会汇总 `preview/unavailable/suppressed + rows`，`overlay_bar` 会汇总 `kind/focus`
- 已补上 renderer / runtime 场景测试：active pane、empty pane、overlay 压缩态都会显式锁住这三类 section bar，防止主体 section 再次退化成只有明细字段
- `workspace picker / terminal manager / terminal picker / prompt / layout resolve` 现在各自也有顶层 `*_bar`，把 overlay 内部的“当前选中了什么、当前是哪种交互”先汇总出来
- `workspace_picker_bar` 会汇总 `selected/kind/depth`，`terminal_manager_bar` 会汇总 `selected/section/kind`，`terminal_picker_bar` 会汇总 `query/selected/kind`
- `prompt_bar` 会汇总 `kind/terminal/active`，`layout_resolve_bar` 会汇总 `pane/role/selected`
- 已补上 renderer / runtime 场景测试：workspace picker、terminal manager、terminal picker、prompt、layout resolve 都锁住各自的 overlay summary bar，避免 overlay 内部重新退化成只有长字段
- `section_notices` 现在也分成 `notice_bar` 与 `notice_group_bar` 两层摘要，前者汇总 `total/showing/last`，后者汇总按级别的 notice 数量
- 空 notice 时会直接落成 `notice_bar: total=0 | showing=0 | notices: 0`，有 notice 时会保留截断元数据和逐条明细
- 已补上 renderer / runtime 场景测试：空态 notice、截断 notice、重复错误聚合、read error notice 都锁住 `notice_bar/notice_group_bar`
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 打开 terminal picker 时主视图显式显示 `overlay: terminal_picker` 与 `focus_overlay_target: terminal_picker`
- 已补上一条 runtime 场景型 E2E：workspace picker / prompt / terminal manager / terminal picker / layout resolve 关闭后主视图不再残留 `focus_overlay_target`
- 已补上一条 runtime 场景型 E2E：terminal manager 与 layout resolve 关闭后主视图不再残留临时 `mode`
- runtime overlay 进入时的 `overlay/focus_overlay_target` 与退出后的清理现在被场景测试锁住
- 已补上一条 runtime 场景型 E2E：connected pane 在没有 snapshot 时主视图会保留 `section_screen` 并显示 `screen: <unavailable>`
- 已补上一条 runtime 场景型 E2E：closed frame 后主视图显式显示 `runtime_state/runtime_exit_code`
- 已补上一条 runtime 场景型 E2E：sync lost 期间主视图显示 `runtime_sync_lost`，snapshot refresh 后自动清除
- 已补上一条 runtime 场景型 E2E：resized event 后主视图显示 `runtime_size: 120x40`
- 已补上一条 runtime 场景型 E2E：removed event 在 reducer 清空 pane 前主视图先显示 `runtime_removed`
- 已补上一条 runtime 场景型 E2E：read error event 后主视图显示 `runtime_read_error`
- 已补上一条 runtime 场景型 E2E：layout resolve 初始主视图显式锁住 `focus_overlay_target: layout_resolve` 与 `mode: picker`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g` 后主视图显式锁住 `mode_sticky: false`
- runtime status 的 `closed/sync_lost/size/removed/read_error` 与初始 `layout resolve/global mode` 投影现在被场景测试锁住
- 已补上一条 runtime 场景型 E2E：active pane 主视图现在显式锁住 `title/tab_layer/pane_kind/terminal_state/screen`
- 已补上一条 runtime 场景型 E2E：重复 notice 在主视图中聚合为 `(x2)` 显示
- 已补上一条 runtime 场景型 E2E：owner 连接角色在主视图中显示 `connection_role: owner`
- runtime 剩余的 owner/active snapshot/notice 聚合可见性现在被场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> Esc` 后主视图不再显示 `mode: global`
- 已补上一条 runtime 场景型 E2E：global mode timeout 后主视图不再显示 `mode: global`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 后主视图显示 `mode: picker` 且不再显示 `mode: global`
- shell 的 mode/global 激活、取消、超时与进入 overlay 的切换路径现在被 runtime 场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-w -> ops -> Right` 后主视图显示 `workspace_picker_selected_expanded: true`
- 已补上一条 runtime 场景型 E2E：`Ctrl-w -> ops -> Right -> Left` 后主视图显示 `workspace_picker_selected_expanded: false`
- workspace picker 的树展开与收起路径现在被 runtime 场景测试锁住
- 已补上一条 runtime 场景型 E2E：`layout resolve -> Enter` 后主视图显示 `overlay: terminal_picker`
- 已补上一条 runtime 场景型 E2E：`layout resolve -> ↓ -> Enter` 后主视图显示 `overlay: none`
- 已补上一条 runtime 场景型 E2E：`layout resolve -> ↓ -> ↓ -> Enter` 后主视图显示 `overlay: none`
- 已补上一条 runtime 场景型 E2E：`layout resolve -> Esc` 后主视图显示 `overlay: none`
- layout resolve 的 connect existing、create new、skip 与关闭路径现在被 runtime 场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-w -> ↑ -> Enter -> 输入/退格 -> Enter` 后主视图显示 `workspace: ops-center`
- 已补上一条 runtime 场景型 E2E：`Ctrl-w -> ↑ -> Enter -> Esc` 后主视图显示 `workspace: main`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓ -> e -> Tab -> Enter` 后状态中的 `term-2` 会更新为 `build-log-v2`
- prompt 的提交、取消、字段切换与草稿输入路径现在被 runtime 场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-f -> ops -> Enter` 后主视图显示 `terminal: term-3`
- 已补上一条 runtime 场景型 E2E：`Ctrl-w -> ops -> Backspace` 后主视图显示 `workspace_picker_query: op`
- 已补上一条 runtime 场景型 E2E：`Ctrl-w -> ops -> Enter` 后主视图显示 `workspace: ops`
- 已补上一条 runtime 场景型 E2E：`Ctrl-w -> ↑ -> Enter` 后主视图显示 `prompt_title: create workspace`
- 已补上一条 runtime 场景型 E2E：`Ctrl-w -> Esc` 后主视图显示 `overlay: none`
- workspace picker 的查询回退、jump、create row 与关闭路径现在被 runtime 场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-f -> ops -> Backspace` 后主视图显示 `terminal_picker_query: op`
- 已补上一条 runtime 场景型 E2E：`Ctrl-f -> missing` 后主视图显示 `> [create] + new terminal`
- 已补上一条 runtime 场景型 E2E：`Ctrl-f -> missing -> Enter` 后主视图显示 `overlay: none`
- 已补上一条 runtime 场景型 E2E：`Ctrl-f -> Esc` 后主视图显示 `overlay: none`
- terminal picker 的查询回退、create row 与关闭路径现在被 runtime 场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ops -> Backspace` 后主视图显示 `terminal_manager_query: op`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓ -> Enter` 后主视图显示 `terminal: term-2`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓ -> t` 后主视图显示 `overlay: none`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓ -> o` 后主视图显示 `overlay: none`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> Esc` 后主视图显示 `overlay: none`
- terminal manager 剩余动作键的真实 runtime 路径现在被场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ops` 后主视图显示 `terminal_manager_query: ops`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ops` 后主视图显示 `terminal_manager_selected: term-3`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ops` 后主视图显示 `detail_tags: team=ops`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↑` 后主视图显示 `> [create] + new terminal`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↑ -> Enter` 后主视图显示 `overlay: none`
- 已补上一条 reducer/model/runtime 联动闭环：terminal manager 的 create row 现在可通过真实 `Enter` 路径触发
- terminal manager 的 search 过滤与 create row 提交结果现在被场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 后主视图显示 `prompt_kind: edit_terminal_metadata`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 后主视图显示 `> [name] Name: api-dev`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 后主视图显示 `  [tags] Tags: `
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> k` 后主视图显示 `overlay: none`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> k` 后主视图显示 `focus_layer: tiled`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> k` 后主视图显示 `slot: empty`
- terminal manager 的 edit prompt 结构字段与 stop 收口结果现在被 runtime 场景测试锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 后主视图显示 `terminal_manager_query: `
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 后主视图显示 `> [terminal] api-dev`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `terminal_manager_query: `
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `terminal_manager_row_count: 7`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `terminal_manager_rows:`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `> [terminal] build-log`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `terminal_manager_detail: build-log`
- terminal manager overlay 的结构字段现在被两个 runtime 场景共同锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `terminal_manager_selected_kind: terminal`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `terminal_manager_selected_state: running`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `terminal_manager_selected_visible: false`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `terminal_manager_selected_visibility: hidden`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `terminal_manager_selected_connected_panes: 0`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `terminal_manager_selected_location_count: 0`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `terminal_manager_selected_owner: `
- terminal manager 停放 terminal 的 selected 剩余关键字段现在被场景测试统一锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `detail_terminal: term-2`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `detail_state: running`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `detail_visible: false`
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `detail_owner: `
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图同时显示 `detail_command: tail -f build.log`
- terminal manager 停放 terminal 的 detail 剩余关键字段现在被场景测试统一锁住
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `detail_location_count: 0`
- terminal manager 停放 terminal 的 detail 位置数量现在被场景测试锁住
- parked terminal detail 的位置数量不再只靠 renderer 单测兜底
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `detail_connected_panes: 0`
- terminal manager 停放 terminal 的 detail 连接数量现在被场景测试锁住
- parked terminal detail 的连接数不再只靠 renderer 单测兜底
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `detail_visibility: hidden`
- terminal manager 停放 terminal 的 detail 可见性标签现在被场景测试锁住
- parked terminal detail 的 `hidden` 标签不会再只靠 renderer 单测兜底
- 已补上 terminal manager detail 的 `detail_command` runtime 场景断言
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `detail_command: npm run dev`
- terminal manager detail 的命令文本现在被 renderer 单测和 runtime E2E 同时锁住
- 已补上 terminal manager detail 的 `detail_state` renderer 断言
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `detail_state: running`
- terminal manager detail 的运行状态现在被 renderer 单测和 runtime E2E 同时锁住
- 已补上 terminal manager detail 的 `detail_visibility` renderer 断言
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `detail_visibility: visible`
- terminal manager detail 的可见性标签现在被 renderer 单测和 runtime E2E 同时锁住
- runtime renderer 已显式展示 terminal manager detail 的 `detail_owner`
- terminal manager detail 区域现在始终保留 owner 槽位字段，即使当前 terminal 没有 owner 也能稳定观察
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `detail_owner: pane:pane-1`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_location_count`
- terminal manager 打开后当前主视图可直接看到当前选中 terminal 的位置数量
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_location_count: 1`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_visibility`
- terminal manager 打开后当前主视图可直接看到当前选中 terminal 的可见性标签
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_visibility: visible`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_owner`
- terminal manager 打开后当前主视图可直接看到当前选中 terminal 的 owner 槽位
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_owner: pane:pane-1`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_tags`
- terminal manager 选择移动后当前主视图可直接看到当前选中 terminal 的标签
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> ↓` 后主视图显示 `terminal_manager_selected_tags: group=build`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_selected_connected_panes`
- terminal picker 搜索后当前主视图可直接看到当前选中 terminal 的连接 pane 数量
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_selected_connected_panes: 0`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_selected_tags`
- terminal picker 搜索后当前主视图可直接看到当前选中 terminal 的标签
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_selected_tags: team=ops`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_selected_visible`
- terminal picker 搜索后当前主视图可直接看到当前选中 terminal 的可见性布尔状态
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_selected_visible: false`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_selected_command`
- terminal picker 搜索后当前主视图可直接看到当前选中 terminal 的命令
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_selected_command: journalctl -f`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_row_count`
- terminal manager 打开后当前主视图可直接看到当前可见行数量
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_row_count: 7`
- runtime renderer 已显式展示 workspace picker 的 `workspace_picker_row_count`
- workspace picker 打开后当前主视图可直接看到当前可见行数量
- 已补上一条 runtime 场景型 E2E：`Ctrl-w` 打开 picker 后主视图显示 `workspace_picker_row_count: 5`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_row_count`
- terminal picker 打开后当前主视图可直接看到当前可选行数量
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_row_count: 2`
- runtime renderer 已显式展示 layout resolve 的 `layout_resolve_row_count`
- layout resolve 打开后当前主视图可直接看到可选动作数量
- 已补上一条 runtime 场景型 E2E：waiting pane 的 resolve 选择移动后主视图显示 `layout_resolve_row_count: 3`
- runtime renderer 已显式展示 terminal manager detail 的 `detail_location_count`
- terminal manager 打开后当前主视图可直接看到详情 terminal 的位置数量
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `detail_location_count: 1`
- runtime renderer 已显式展示 terminal manager detail 的 `detail_visible`
- terminal manager 打开后当前主视图可直接看到详情 terminal 的可见性布尔状态
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `detail_visible: true`
- runtime renderer 已显式展示 active terminal 的 `terminal_visibility`
- 当前主视图可直接看到 active terminal 的可见性布尔状态
- 已补上一条 runtime 场景型 E2E：active terminal metadata 场景主视图显示 `terminal_visibility: true`
- runtime renderer 已显式展示 prompt overlay 的 `prompt_field_count`
- prompt 打开后当前主视图可直接看到结构化字段数量
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 进入 prompt 后主视图显示 `prompt_field_count: 2`
- runtime renderer 已显式展示 prompt overlay 的 `prompt_active_index`
- prompt 打开后当前主视图可直接看到当前活动字段索引
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 进入 prompt 后主视图显示 `prompt_active_index: 0`
- runtime renderer 已显式展示 workspace picker 的 `workspace_picker_selected_depth`
- workspace picker 打开后当前主视图可直接看到当前选中节点在树中的深度
- 已补上一条 runtime 场景型 E2E：`Ctrl-w` 打开 picker 后主视图显示默认选中节点深度 `workspace_picker_selected_depth: 2`
- runtime renderer 已显式展示 workspace picker 的 `workspace_picker_selected_match`
- workspace picker 打开后当前主视图可直接看到当前选中节点是否命中 query
- 已补上一条 runtime 场景型 E2E：`Ctrl-w` 打开 picker 后主视图显示默认选中节点命中态 `workspace_picker_selected_match: false`
- runtime renderer 已显式展示 workspace picker 的 `workspace_picker_selected_expanded`
- workspace picker 打开后当前主视图可直接看到当前选中节点是否处于展开态
- 已补上一条 runtime 场景型 E2E：`Ctrl-w` 打开 picker 后主视图显示默认选中节点展开态 `workspace_picker_selected_expanded: false`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_visible`
- terminal manager 打开后当前主视图可直接看到当前选中 terminal 的可见性布尔状态
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_visible: true`
- runtime renderer 已显式展示 prompt overlay 的 `prompt_active_value`
- prompt 打开后当前主视图可直接看到当前活动字段的值
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 进入 prompt 后主视图显示 `prompt_active_value: api-dev`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_command`
- terminal manager 打开后当前主视图可直接看到当前选中 terminal 的命令
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_command: npm run dev`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_connected_panes`
- terminal manager 打开后当前主视图可直接看到当前选中 terminal 的连接 pane 数量
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_connected_panes: 1`
- runtime renderer 已显式展示 prompt overlay 的 `prompt_active_label`
- prompt 打开后当前主视图可直接看到当前活动字段的标签
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 进入 prompt 后主视图显示 `prompt_active_label: Name`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_kind`
- terminal manager 打开后当前主视图可直接看到当前选中行的类型
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_kind: terminal`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_selected_kind`
- terminal picker 搜索后当前主视图可直接看到当前选中行的类型
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_selected_kind: terminal`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_section`
- terminal manager 打开后当前主视图可直接看到当前选中 terminal 所在分区
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_section: VISIBLE`
- runtime renderer 已显式展示 workspace picker 的 `workspace_picker_selected_label`
- workspace picker 打开后当前主视图可直接看到当前选中节点的标签
- 已补上一条 runtime 场景型 E2E：`Ctrl-w` 打开 picker 后主视图显示默认选中节点标签 `workspace_picker_selected_label: unconnected pane`
- runtime renderer 已显式展示 layout resolve 的 `layout_resolve_selected_label`
- layout resolve 打开后当前主视图可直接看到当前选中动作的标签
- 已补上一条 runtime 场景型 E2E：waiting pane 的 resolve 选择移动后主视图显示 `layout_resolve_selected_label: create new`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_label`
- terminal manager 打开后当前主视图可直接看到当前选中 terminal 的标签
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_label: api-dev`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_selected_label`
- terminal picker 搜索后当前主视图可直接看到当前选中 terminal 的标签
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_selected_label: ops-watch`
- runtime renderer 已显式展示 workspace picker 的 `workspace_picker_selected_kind`
- workspace picker 打开后当前主视图可直接看到当前选中节点类型
- 已补上一条 runtime 场景型 E2E：`Ctrl-w` 打开 picker 后主视图显示默认选中节点类型 `workspace_picker_selected_kind: pane`
- runtime renderer 已显式展示 prompt overlay 的 `prompt_active_field`
- 结构化 prompt 打开后当前主视图可直接看到当前活动字段 key
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 进入 prompt 后主视图显示 `prompt_active_field: name`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected_state`
- terminal manager 打开后当前主视图可直接看到选中 terminal 的运行状态
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected_state: running`
- runtime renderer 已显式展示 layout resolve 的 `layout_resolve_selected`
- layout resolve 打开后当前主视图可直接看到当前选中的动作
- 已补上一条 runtime 场景型 E2E：移动选择后主视图显示 `layout_resolve_selected: create_new`
- runtime renderer 已显式展示 terminal manager 的 `terminal_manager_selected`
- terminal manager 打开后当前主视图可直接看到当前选中的 terminal ID
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开后主视图显示 `terminal_manager_selected: term-1`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_selected_state`
- terminal picker 搜索后当前主视图可直接看到选中 terminal 的运行状态
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_selected_state: running`
- runtime renderer 已显式展示 workspace picker 的 `workspace_picker_selected`
- workspace picker 打开后当前主视图可直接看到当前选中节点 key
- 已补上一条 runtime 场景型 E2E：`Ctrl-w` 打开 picker 后主视图显示默认选中节点 `ws-1/tab-1/pane-1`
- runtime renderer 已显式展示 terminal picker 的 `terminal_picker_selected`
- terminal picker 打开后当前主视图可直接看到当前选中的 terminal ID
- 已补上一条 runtime 场景型 E2E：`Ctrl-f` 搜索后主视图显示 `terminal_picker_selected: term-3`
- runtime renderer 已显式展示 terminal manager detail 的 `detail_terminal`
- terminal manager 详情区现在可直接看到选中 terminal 的稳定 ID
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t` 打开 terminal manager 时详情区显示 `detail_terminal: term-1`
- runtime renderer 已显式展示 layout resolve overlay 的 `layout_resolve_pane`
- waiting pane 做 resolve 时，当前主视图可直接看到目标 pane ID
- 已补上一条 runtime 场景型 E2E：layout resolve 选择移动时视图同时保留 `layout_resolve_pane: pane-1`
- runtime renderer 已显式展示 prompt overlay 的 `prompt_terminal`
- `edit terminal metadata` prompt 现在可直接看见当前 handoff 目标 terminal
- 已补上一条 runtime 场景型 E2E：`Ctrl-g -> t -> e` 进入 prompt 后主视图显示 `prompt_terminal: term-1`
- runtime renderer 已显式展示 active tab 的 `tab_layer`
- 当前主视图现在可区分“当前焦点层”和“tab 本身停留层”
- 已补上两条 runtime 场景型 E2E：`Ctrl-w` 打开 overlay 时保留 `tab_layer: tiled`，以及 floating tab 启动时显示 `tab_layer: floating`
- runtime renderer 已显式展示 active pane 的 `pane_kind`
- 当前主视图现在可直接区分 `tiled / floating` pane
- 已补上一条 runtime 场景型 E2E：floating pane 启动后主视图可直接看到 `pane_kind: floating`
- runtime renderer 已显式展示 active terminal 的 `terminal_command`
- runtime renderer 已显式展示 active terminal 的 `terminal_tags`
- 已补上一条 runtime 场景型 E2E：启动后主视图可直接看到 active terminal 的命令和标签
- runtime renderer 已显式展示 active pane 所连接 terminal 的 `connected_panes`
- `shared terminal` 下当前 pane 现在可直接看到共享连接数量
- 已补上一条 runtime 场景型 E2E：follower pane 启动后主视图可同时看到 `connection_role: follower` 和 `connected_panes: 2`
- runtime renderer 已显式展示 active pane 的 `connection_role`
- `shared terminal` 下当前 pane 已可区分 `owner / follower`
- 已补上一条 runtime 场景型 E2E：follower pane 启动后主视图可直接看到 `connection_role: follower`
- runtime renderer 已显式展示 `terminal_state`
- runtime renderer 已显式展示 `terminal_exit_code`
- runtime renderer 已显式展示 `pane_exit_code`
- 已补上一条 runtime 场景型 E2E：`closed frame -> exited pane` 时终态和退出码在视图中可见
- runtime renderer 已显式展示 `focus_layer`
- runtime renderer 已显式展示 `focus_overlay_target`
- 已补上两条 runtime 场景型 E2E：`Ctrl-w -> workspace picker`、`Ctrl-g -> t -> e -> prompt` 时焦点状态在视图中可见
- `UIState` 已包含 `OverlayState` 和 `ModeState`
- `FocusState` 已补上 `OverlayTarget`
- `workspace picker` 已支持默认选中当前 active pane
- `workspace picker` 已支持选择移动、展开、折叠
- 搜索清空后会恢复“默认展开 + 手动展开/折叠”状态
- 打开 overlay 时会保存返回焦点
- 关闭 overlay 时会恢复原 pane 焦点
- picker 回车已能跳转 workspace / tab / pane，并在成功后关闭 overlay
- 非 sticky mode 超时后会自动清空
- `terminal manager` 已支持独立 overlay 状态对象和默认选中当前 pane 所连接 terminal
- `terminal manager` 已支持选择移动和稳定排序
- `terminal manager` 已支持 `connect here`
- `terminal manager` 已支持产出 `new tab / floating` 的 effect plan
- pane 改连新 terminal 时，旧 terminal 的 connection snapshot 会同步清理，避免 owner/follower 脏引用
- `workspace picker` 已支持 query 追加输入
- `workspace picker` 已支持 backspace 回退 query
- query 命中后会把选择移动到首个匹配节点，便于直接回车 jump
- 选中 `+ create workspace` 回车时，reducer 已能关闭 picker 并产出 `OpenPromptEffect{create_workspace}`
- 已补上一条 reducer 场景型 E2E：搜索后直接跳到目标 pane
- `terminal manager` 已支持 query 追加输入和 backspace
- `terminal manager` search 已支持匹配 terminal name / id / command / tags
- `terminal manager` 已支持对选中 terminal 发起 metadata prompt handoff
- `terminal manager` 已支持 stop 选中 terminal，并同步更新 reducer 内 terminal 状态
- 已补上一条 reducer 场景型 E2E：搜索后直接 stop 目标 terminal
- 已补上独立的 `prompt overlay` 状态对象
- `OpenPromptIntent` 已能把焦点切到 `prompt` layer
- `CancelPromptIntent` 已能关闭 prompt 并恢复原 pane 焦点
- `SubmitPromptIntent` 已支持 `create workspace`
- create workspace 会建立最小可工作骨架：默认 tab + 未连接 pane
- 已补上一条 reducer 场景型 E2E：workspace picker create row -> prompt -> create workspace
- `SubmitPromptIntent` 已支持 `edit terminal metadata`
- metadata prompt 已能更新 terminal `name / tags`
- metadata prompt 提交后会关闭 prompt、恢复原 pane 焦点，并产出 `UpdateTerminalMetadataEffect`
- 已补上一条 reducer 场景型 E2E：terminal manager edit metadata -> prompt -> submit
- `terminal manager` 已支持稳定的分组投影：`NEW / VISIBLE / PARKED / EXITED`
- `terminal manager` 已支持顶部 `+ new terminal` 入口
- `terminal manager` 已支持当前选中 terminal 的 details 投影
- `terminal manager` 的选择模型现在覆盖 create row 与 terminal 行，header 行保持非可选
- 已补上一条 reducer 场景型 E2E：terminal manager create row -> create terminal effect
- `terminal manager details` 已支持列出 terminal 当前出现的位置
- 位置列表已能区分 `pane:<id>` 和 `float:<id>`
- details 的连接计数现在和位置投影保持一致
- 已补上一条 reducer 测试覆盖 details 中的 pane / float 位置投影
- `prompt` 已持有独立 `draft`，不再只依赖 `SubmitPromptIntent{Value}`
- `PromptAppendInputIntent` 和 `PromptBackspaceIntent` 已能直接驱动 prompt draft
- 打开 metadata prompt 时会自动用当前 terminal 的 `name/tags` 预填 draft
- `SubmitPromptIntent` 在未显式传值时会直接提交当前 draft
- 已补上一条 reducer 场景型 E2E：workspace create flow 直接走 prompt draft 提交
- `terminal manager details` 已支持 `visibility label`
- `terminal manager details` 已支持 `owner slot label`
- `terminal manager details` 已支持稳定排序后的 `tags` 投影
- 已补上一条 reducer 测试覆盖 details 中的 `visibility / owner / tags`
- `terminal manager create row` 产出的 `CreateTerminalEffect` 已带默认 command
- `CreateTerminalEffect` 已带稳定默认 name：`workspace-tab-pane`
- 已补上一条 reducer 场景型 E2E：create row -> create effect 时默认参数完整
- `prompt` 已支持结构化字段模型
- metadata prompt 已拆成 `name / tags` 两个字段
- `PromptNextFieldIntent` 已能在结构化字段间切换焦点
- `PromptPreviousFieldIntent` 已能在结构化字段间反向切换焦点
- prompt 输入现在优先写入当前字段，`SubmitPromptIntent` 可直接从字段模型生成提交值
- 已补上一条 reducer 测试覆盖 metadata prompt 的字段切换与结构化提交
- `prompt overlay` clone 已改为深拷贝字段切片，避免 reducer 纯状态克隆时共享底层字段数据
- 已补上一条 reducer 测试覆盖 metadata prompt 的反向字段切换
- 已补上一条 prompt 单元测试覆盖结构化字段深拷贝
- 已新增 `tui/bt` 输入映射层，负责把 `bubbletea.KeyMsg` 翻译成显式 intent
- 根层已支持最小主入口映射：`Ctrl-w -> workspace picker`，`Ctrl-g` 进入 global 前缀，随后 `t -> terminal manager`
- `workspace picker` 已接上键盘映射：移动、展开、折叠、提交、关闭、query 输入
- `terminal manager` 已接上最小键盘映射：移动、query 输入、connect here、关闭
- `prompt` 已接上键盘映射：输入、回退、提交、取消、`Tab/Shift-Tab` 字段切换
- 已补上一条跨层场景型 E2E：`KeyMsg -> intent mapper -> reducer -> workspace picker jump`
- `tui/bt` 已补上最小 `tea.Model` 容器，串起 `KeyMsg -> mapper -> reducer -> effect handler`
- shell 容器当前已抽出 `EffectHandler` / `Renderer` 接口，后续可继续接 runtime executor 和真实 render
- `Model` 已支持对非键盘消息保持稳定忽略，避免输入层误改状态
- 已补上一条跨层场景型 E2E：`KeyMsg -> Model.Update -> workspace picker jump`
- `tui/bt` 已补上 `RuntimeExecutor` 和 `RuntimeEffectHandler`
- `OpenPromptEffect` 现在已能回流成 `OpenPromptIntent` 并重新进入 reducer
- `ConnectTerminal / CreateTerminal / StopTerminal / UpdateTerminalMetadata / new tab / floating` effect 已有稳定的 runtime service 接口落点
- `Model.Update` 已支持消费 effect feedback message，形成 `key -> effect -> feedback intent -> reducer` 闭环
- 已补上一条跨层场景型 E2E：`workspace picker create row -> OpenPromptEffect -> prompt overlay`
- `terminal manager` 已补上动作键映射：`t new tab`、`o floating`、`e edit`、`k stop`
- `terminal manager` 的 `edit metadata` 现在已能从 shell 容器中走完整 prompt handoff
- 已补上一条跨层场景型 E2E：`Ctrl-g -> t -> e -> metadata prompt`
- `tui/bt` 已补上最小 `Notice` 模型，当前由 shell 容器持有和追加
- runtime effect 执行失败不再静默吞掉，`RuntimeEffectHandler` 现在会把错误转换成 `error notice`
- `Model.Update` 已支持消费 notice feedback message，并保留当前 notice 列表供后续 renderer 接线
- 已补上一条跨层场景型 E2E：`terminal manager stop` 失败后记录 error notice
- 已新增 `tui/domain/terminalpicker`，提供最小 terminal picker 列表态、搜索、选择和 create row
- 根层已支持 `Ctrl-f -> terminal picker`
- `terminal picker` 已接上键盘映射：移动、query 输入、回退、提交、关闭
- `terminal picker` 已支持 `connect existing terminal`
- `terminal picker` 已支持 `+ new terminal` 入口并复用统一默认参数策略
- 已补上一条跨层场景型 E2E：`Ctrl-f -> query -> connect selected terminal`
- `Notice` 已补上稳定 `ID` 和创建时间，便于后续 renderer 展示与精确清理
- shell 容器已接上可替换的 `NoticeScheduler`，默认通过 `tea.Tick` 调度 timeout
- `Model.Update` 已支持消费 `notice timeout` message，并按 notice ID 清理过期项
- 已补上一条跨层场景型 E2E：error notice 经 timeout 后自动清理
- 已新增 `tui/domain/layoutresolve`，提供 waiting pane 的最小 resolve 选择状态
- `layout resolve` 已支持三种显式动作：`connect existing / create new / skip`
- `layout resolve` 已接入 reducer，可把 waiting pane handoff 到 `terminal picker`
- `layout resolve` 已支持直接创建新 terminal 的 effect 产出，以及 `skip` 后保留 waiting pane
- `tui/bt` 已接上 `layout resolve` 的移动、提交、关闭键映射
- 已补上一条 reducer 场景型 E2E：waiting pane 从 resolve 进入 terminal picker 并 connect terminal
- 已补上一条 shell 场景型 E2E：`layout resolve -> terminal picker -> connect existing`
- 已新增 `StartupPlanner / LayoutLoader` 接口，先把启动决策从 runtime 壳中抽离
- 默认启动现在可生成最小 workspace 骨架和 `create terminal` 启动任务
- `--layout` 启动现在可加载最小 YAML，并直接落到 waiting pane + `layout resolve`
- layout 加载失败时，若启用 `StartupAutoLayout`，会稳定降级到默认 workspace 启动
- 已补上一条启动层 E2E：`layout file -> resolve overlay -> terminal picker -> connect existing`
- startup planner 现在已支持 `attach` 启动路径，并产出独立 `AttachTerminalTask`
- 已新增 `StartupTaskExecutor`，负责执行启动任务并把 runtime 结果回填到纯状态
- 默认启动的 `CreateTerminalTask` 现在可以真正调用 client create 并把 pane 回填成 connected
- `attach` 启动现在会通过 `List` 校验目标 terminal，并把 metadata / state 回填到 pane 和 terminal ref
- 已补上一条启动层 E2E：默认启动经 startup bootstrap 后直接进入 connected pane
- 已新增 `WorkspaceStore` 接口和文件加载实现，先把 workspace restore 从 runtime 壳中抽出来
- startup planner 现在会在无 `attach` / `layout` 时优先尝试 `WorkspaceStatePath` restore
- restore 成功时会从持久化 domain state 推导 `UI.Focus`，并避免再生成启动任务
- restore 文件缺失时会静默回落到默认启动；restore 解码失败时会记录 warning 并降级
- 已补上一条 workspace store 单测覆盖 JSON round-trip
- 已补上一条 restore 启动测试覆盖“成功恢复后无 bootstrap task”
- `WorkspaceStore` 现在已支持 `SaveWorkspace`，可把当前 domain state 稳定落盘
- 已补上一条 store 单测覆盖 `SaveWorkspace -> LoadWorkspace` round-trip
- 已补上一条跨层 E2E：默认启动 -> bootstrap -> save -> restore reload
- 已新增 `RuntimeSessionBootstrapper`，负责从当前 connected terminal 集合建立 runtime attach session
- runtime session bootstrap 现在会先订阅全局 `Events`，再为每个 connected terminal 建立 `Attach + Snapshot + Stream`
- shared terminal 在 runtime session bootstrap 中会按 terminal 去重，只 attach 一次
- attach / snapshot 失败时会主动清理已建立的 stream stop 句柄，避免半连接泄漏
- 已补上一条 runtime E2E：restore plan 产出的 connected pane 可直接 bootstrap 出 attach channel 和 event stream
- `Run()` 现在已能串起 `startup planner -> startup task executor -> runtime session bootstrap`
- `Run()` 现在会在完成前置编排后启动最小 Bubble Tea program runner
- 已新增最小 `runtimeRenderer`，先稳定展示 `workspace / tab / pane / slot / overlay / terminal`
- 已新增 `RuntimeTerminalStore`，把 runtime session 的 snapshot/channel 以接口形式提供给 renderer 与输入层复用
- `runtimeRenderer` 现在已能渲染 active pane 所连接 terminal 的初始 snapshot 文本内容
- `tui/bt.Model` 已补上 `UnmappedKeyHandler`，让未命中 intent 的按键可安全下沉到 runtime terminal 输入层
- 已新增最小 `RuntimeTerminalInputHandler`，会把 active pane 的字符输入转发到 attach channel
- 输入转发当前会在 `overlay == none` 且 `mode == none` 时生效，避免打穿 picker / prompt / prefix mode
- runtime input 失败现在会回流成 `FeedbackMsg` notice，而不是静默丢失
- 已补上一条 runtime E2E：`Run()` 已能同时渲染 active pane snapshot，并把按键输入转发到 runtime session channel
- `tui/bt.Model` 已补上 `InitCmd` 和 `MessageHandler` 扩展点，运行时异步消息不再需要塞进 reducer
- 已新增 `runtimeUpdateHandler`，负责把 terminal `stream` 和全局 `event` fan-in 成 Bubble Tea 消息
- `RuntimeTerminalStore` 现在已内建最小 `vterm`，可把初始 snapshot seed 成终端状态，再持续重放 `TypeOutput`
- `runtimeRenderer` 现在会读取 store 的最新 snapshot，而不再只停留在 startup 时刻的静态快照
- `TypeOutput` 增量输出现在已能驱动 active pane 二次重绘
- `TypeSyncLost` 现在会标记 runtime 状态，并触发一次 `Snapshot` recovery；recovery 成功后会清掉 sync lost 标记
- `EventTerminalReadError` 现在会回流成 runtime notice
- `EventTerminalResized / EventTerminalStateChanged / EventTerminalRemoved / TypeClosed` 已有最小 runtime 状态落点，便于后续接到 reducer/domain
- 已补上一条 runtime E2E：stream 输出进入 attach channel 后可驱动 view 刷新
- 已新增 `TerminalRemovedIntent`，明确表达“terminal 已被移除，pane 需要退回 empty”
- `runtimeUpdateHandler` 现在会把 `TypeClosed` 回流成 `TerminalProgramExitedIntent`
- `runtimeUpdateHandler` 现在会把 `EventTerminalStateChanged(new_state=exited)` 回流成 `TerminalProgramExitedIntent`
- `runtimeUpdateHandler` 现在会把 `EventTerminalRemoved` 回流成 `TerminalRemovedIntent`
- reducer 现在已能消费 `TerminalRemovedIntent`，同步清理 pane 连接、terminal ref 和 connection snapshot
- 已补上一条 runtime E2E：`TypeClosed` 经 runtime feedback 回流 reducer 后，pane 会进入 `exited`
- 已新增 `SyncTerminalStateIntent`，用于表达 runtime 对 terminal `running / stopped / exited` 状态的无副作用同步
- reducer 现在已能消费 `SyncTerminalStateIntent`
- `running` 同步会清掉 terminal 的旧 exit code，但不会误断开当前 pane 连接
- `stopped` 同步会把已连接 pane 退回 empty，并清理 connection snapshot
- `exited` 在没有 exit code 时也能稳定落成 exited pane，保留“可见历史但无退出码”的状态
- `runtimeUpdateHandler` 现在会把 `EventTerminalStateChanged(new_state=running|stopped|exited(nil))` 回流成 `SyncTerminalStateIntent`
- 已补上一条 runtime E2E：`EventTerminalStateChanged(stopped)` 经 runtime feedback 回流 reducer 后，pane 会退回 `empty`
- `RuntimeTerminalStatus` 现在已补上 `Size` 和 `ObserverOnly`
- `EventTerminalResized` 现在会同步更新 runtime snapshot size 和 runtime status size
- `runtimeRenderer` 现在会展示 `runtime_size`
- `EventCollaboratorsRevoked` 现在会把 terminal 标记成 `observer_only`
- `runtimeRenderer` 现在会展示 `runtime_access: observer_only`
- `RuntimeTerminalInputHandler` 现在会在 observer-only 状态下阻断输入，并回流 notice
- 已补上一条 runtime E2E：`EventCollaboratorsRevoked` 后后续输入不再下发到 attach channel
- `Run()` 成功退出时会主动清理已建立的 runtime session，避免 stream 句柄泄漏
- 已补上一组 runtime 编排测试覆盖 planner/task/session 的调用顺序和错误传播
- 已补上一条 runtime 测试覆盖 program runner 调用和 renderer 输出
- `cmd/termx attach` 现在会把 CLI 的 `--prefix-timeout` 继续透传到 `tui.Run`，与 root TUI 入口保持一致按键前缀超时语义
- 已新增 `RegisterTerminalIntent`，用于表达 runtime 期间新出现但尚未 connect 的 terminal
- reducer 现在已能消费 `RegisterTerminalIntent`，把 detached terminal 注册进 `Domain.Terminals`
- `runtimeUpdateHandler` 现在会把 `EventTerminalCreated` 回流成 `RegisterTerminalIntent`
- 已补上一条 runtime E2E：`EventTerminalCreated` 经 runtime feedback 回流 reducer 后，detached terminal 会进入 domain 状态
- `bt.Model` 现在会按 `notice level + text` 聚合同类 notice，避免重复 runtime 错误刷屏
- notice 聚合后会刷新 timeout 身份，旧 timeout 不会把新一轮聚合 notice 提前删掉
- 已补上一条 model E2E：重复失败的 stop 流程只保留一条 notice，并累计重复次数
- `bt.Renderer` 现在会收到当前 notice 列表，view 不再只能看到纯 domain state
- `runtimeRenderer` 现在会渲染 `notices:` 区段，并展示聚合后的重复次数
- 已补上一条 runtime E2E：observer-only 阻断输入后的 notice 会直接出现在 runtime view 中
- `runtimeRenderer` 现在会渲染 `workspace_picker_query` 和 `workspace_picker_rows`
- workspace picker 树行现在会展示选中标记、层级缩进和节点类型标签
- 已补上一条 runtime E2E：`Ctrl-W` 打开 picker 后，view 会直接出现 workspace tree 内容
- `runtimeRenderer` 现在会渲染 `terminal_manager_query`、`terminal_manager_rows` 和当前选中 terminal 的 detail
- terminal manager 行现在会展示 header/create/terminal 三类投影，并标出当前选择
- 已补上一条 runtime E2E：`Ctrl-G` -> `t` 打开 terminal manager 后，view 会直接出现 manager 内容
- `runtimeRenderer` 现在会渲染 `prompt_title`、`prompt_kind` 和 `prompt_fields`
- prompt 结构化字段现在会展示 active field 标记，便于后续真实 TUI 壳继续套样式
- 已补上一条 runtime E2E：terminal manager `e` 打开的 metadata prompt 会直接出现在 view 中
- `runtimeRenderer` 现在会渲染 `terminal_picker_query` 和 `terminal_picker_rows`
- terminal picker 行现在会展示 create/terminal 两类投影，并标出当前选择
- 已补上一条 runtime E2E：`Ctrl-F` 打开 terminal picker 并输入查询后，view 会直接出现 picker 内容
- `runtimeRenderer` 现在会渲染 `layout_resolve_role`、`layout_resolve_hint` 和 `layout_resolve_rows`
- layout resolve 行现在会展示动作类型标签，并标出当前选择
- 已补上一条 runtime E2E：layout resolve 中的选择移动会直接反映到 view 中
- `runtimeRenderer` 现在会渲染 `mode` 和 `mode_sticky`
- `Ctrl-G` 进入 global 前缀模式后，运行态 view 现在能直接看到当前 mode
- 已补上一条 runtime E2E：`Ctrl-G` 后全局模式会直接反映到 view 中
- `runtimeRenderer` 现在会渲染 terminal manager detail 的 `connected_panes` 和 `locations`
- terminal manager 详情区现在能直接看到 terminal 当前出现的位置
- 已补上一条 runtime E2E：打开 terminal manager 后，当前选中 terminal 的连接信息会直接出现在 view 中

本轮验证：

- `go test ./tui -run 'TestRuntimeRendererRendersTerminalManagerOverlay|TestE2ERunScenarioCtrlGTOpensTerminalManagerInView' -count=1`
- `go test ./tui -count=1`
- `go test ./tui -run 'TestRuntimeRendererRendersActiveMode|TestE2ERunScenarioCtrlGShowsGlobalModeInView' -count=1`
- `go test ./tui -count=1`
- `go test ./tui -run 'TestRuntimeRendererRendersLayoutResolveOverlay|TestE2ERunScenarioLayoutResolveMoveUpdatesView' -count=1`
- `go test ./tui -count=1`
- `go test ./tui -run 'TestRuntimeRendererRendersTerminalPickerOverlay|TestE2ERunScenarioCtrlFOpensTerminalPickerInView' -count=1`
- `go test ./tui -count=1`
- `go test ./tui -run 'TestRuntimeRendererRendersPromptOverlay|TestE2ERunScenarioTerminalManagerEditOpensPromptInView' -count=1`
- `go test ./tui -count=1`
- `go test ./tui -run 'TestRuntimeRendererRendersTerminalManagerOverlay|TestE2ERunScenarioCtrlGTOpensTerminalManagerInView' -count=1`
- `go test ./tui -count=1`
- `go test ./tui -run 'TestRuntimeRendererRendersWorkspacePickerOverlay|TestE2ERunScenarioCtrlWOpensWorkspacePickerInView' -count=1`
- `go test ./tui ./tui/bt -count=1`
- `go test ./tui ./tui/bt -run 'TestModelViewPassesCurrentNoticesToRenderer|TestRuntimeRendererRendersNoticeSection|TestE2ERunScenarioBlockedInputNoticeAppearsInView' -count=1`
- `go test ./tui ./tui/bt -count=1`
- `go test ./tui/bt -run 'TestModelUpdateDeduplicatesMatchingNoticesAndBumpsCount|TestModelUpdateStaleNoticeTimeoutDoesNotRemoveDeduplicatedNotice|TestE2EModelScenarioRepeatedFailedStopDeduplicatesErrorNotice' -count=1`
- `go test ./tui/bt -count=1`
- `go test ./tui ./tui/bt -count=1`
- `go test ./tui ./tui/app/reducer -count=1`
- `go test ./tui/... -run 'TestReducerRegisterTerminalAddsDetachedTerminalRef|TestRuntimeUpdateHandlerCreatedEventFeedsRegisterTerminalIntent|TestE2ERunScenarioCreatedEventRegistersDetachedTerminal' -count=1`
- `go test ./cmd/termx -count=1`
- `go test ./tui/... -run 'TestRuntimeUpdateHandlerResizedEventUpdatesStoreSnapshotSize|TestRuntimeUpdateHandlerCollaboratorsRevokedMarksObserverOnlyAndNotice|TestRuntimeTerminalInputHandlerBlocksObserverOnlyTerminalInput|TestE2ERunScenarioCollaboratorsRevokedBlocksSubsequentInput' -count=1`
- `go test ./tui/... -run 'TestReducerSyncTerminalState|TestRuntimeUpdateHandlerStateChangedStoppedFeedsSyncStateIntent|TestRuntimeUpdateHandlerStateChangedRunningFeedsSyncStateIntent|TestRuntimeUpdateHandlerStateChangedExitedWithoutCodeFeedsSyncStateIntent|TestE2ERunScenarioStateChangedStoppedFeedsReducerAndClearsPane' -count=1`
- `go test ./tui/... -run 'TestReducerTerminalRemoved|TestRuntimeUpdateHandlerTypeClosedFeedsProgramExitedIntent|TestRuntimeUpdateHandlerRemovedEventFeedsTerminalRemovedIntent|TestRuntimeUpdateHandlerStateChangedExitedFeedsProgramExitedIntent|TestE2ERunScenarioClosedFrameFeedsReducerAndMarksPaneExited' -count=1`
- `go test ./tui -run 'TestRun|TestRuntimeSession|TestRuntimeRenderer|TestRuntimeTerminalInputHandler|TestRuntimeUpdateHandler|TestE2ERunScenario' -count=1`
- `go test ./tui/bt -run 'TestModelInitReturnsConfiguredInitCommand|TestModelUpdateDelegatesUnhandledMessageToMessageHandler|TestModel' -count=1`
- `go test ./tui -run 'TestRun|TestRuntimeSession|TestRuntimeRenderer|TestRuntimeTerminalInputHandler' -count=1`
- `go test ./tui -run 'TestRun|TestRuntimeSession' -count=1`
- `go test ./tui -run TestRuntimeSession -count=1`
- `go test ./tui -count=1`
- `go test ./tui/... -count=1`
- `go test ./tui ./tui/bt -count=1`
- `go test ./tui/domain/layoutresolve ./tui/app/reducer ./tui/bt -count=1`
- `go test ./tui/bt -count=1`
- `go test ./tui/domain/terminalpicker ./tui/app/reducer ./tui/bt -count=1`
- `go test ./tui/bt -run TestE2E -count=1`
- `go test ./tui/bt -count=1`
- `go test ./tui/bt -run TestE2EModelScenario -count=1`
- `go test ./tui/bt -run TestE2EIntentMapperScenario -count=1`
- `go test ./tui/domain/prompt ./tui/app/reducer -count=1`
- `go test ./tui/app/reducer -run TestE2EReducerScenario -count=1`
- `go test ./tui/domain/terminalmanager ./tui/app/reducer -count=1`
- `go test ./tui/domain/workspace ./tui/app/reducer -count=1`
- `go test ./tui/... -count=1`
- `go test ./... -count=1`

---

## 3. 尚未开始

当前还没有正式开始的部分：

1. 新版 renderer 深化
2. 真实 TUI E2E 壳与 renderer 结合
3. 把 runtime size / access / read error 这类观测状态是否进入 domain 继续收口

---

## 4. 当前最高优先级

下一阶段最高优先级不是补 UI，而是先把下面几个边界立住：

1. 更完整的 `intent -> reducer -> effect -> runtime feedback` 契约
2. 真实 TUI E2E 场景壳
3. 新版 renderer 深化
4. 把 runtime size / access / read error 这类观测状态是否需要 domain 建模继续收口

原因：

- 这些边界决定后续是否还会回到补丁式开发
- shared terminal 的复杂度必须先被模型化
- 输入路径必须先统一

---

## 5. 当前主要风险

### 5.1 文档和实现再次分叉

如果没有按新文档起代码骨架，很容易再次回到：

- 先做功能
- 后补设计
- 最后结构失控

### 5.2 shared terminal 复杂度再次失控

如果不先把 `ConnectionState` 做成一等模型，owner/follower 会再次散回 UI 和 runtime 逻辑。

### 5.3 渲染问题过早主导实现

如果过早恢复旧版那种复杂 render/cache 路线，会把新主线重新拖回旧结构。

---

## 6. 当前推荐动作

当前最合适的下一步是：

1. 继续扩真实 TUI E2E 场景壳
2. 推进新版 renderer 深化
3. 判断 `runtime_size / observer_only / read_error` 是否要进入 domain 统一建模

---

## 7. 当前一句话状态

termx TUI 现在已经进入“picker / manager / prompt / layout resolve 四条 overlay 主线、startup planner、startup task executor、restore store 读写闭环、runtime session bootstrap、最小 Bubble Tea 运行主线、关键 runtime 事件回灌，以及 notice 聚合/去重都已落地，下一步继续按 TDD 扩真实 TUI E2E 壳并深化 renderer”的阶段。
