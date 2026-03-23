# termx TUI 主线 Todo

状态：2026-03-22

这份清单只记录当前 TUI 主线待办，不写已经封存的旧补丁项。

## P0 当前主线

1. 完成 `R2 输入统一` 的下一步，把 active prefix 的剩余模式继续收口到统一前缀输入层
2. 把 pane mode 和更复杂的 prefix 组合键继续统一成共享 dispatch，减少 key/event 双实现
3. 把鼠标输入也纳入类似 intent/dispatch 入口，避免继续散落在 `input.go`
4. 为 prefix 输入层补更多单测，覆盖非法按键、空 token、shift 组合、tab/esc 边界
5. 开始 `R3 reducer / runtime effect` 分离，先从 prefix timer、help、prompt 这种低耦合 effect 切开
6. 把 render invalidation 责任逐步从业务逻辑里拔出来，建立更明确的 render-layer 失效规则
7. 为各 mode 的 unknown/no-op 行为补一层统一约束，避免 direct-mode keep 规则再次散落回 apply 分支
8. 继续扩 shared terminal 的复杂 e2e，重点盯 tiled + floating + alt-screen + owner migration
9. 补 shared terminal 多 pane 场景下的 resize/close/exit 交错回归，防止残影和串屏回归
10. 为 terminal manager 加更完整的状态操作回归测试，包括 attach、bring here、new tab、floating、metadata edit
11. 收紧 help / status / mode 文案，让 `Ctrl-p/r/t/w/o/v/f/g` 的学习成本继续下降

## P1 结构与产品演进

12. 继续明确 pane/runtime/render 的分层边界，避免 `Pane` 继续承载过多混合状态
13. 为 saved pane / exited pane / removed terminal 的生命周期补充更系统的状态图与测试
14. 继续完善 terminal metadata 模型，明确 name/tags 在 manager、picker、workspace/layout 中的行为
15. 为 workspace restore / layout restore 补齐 shared terminal 与 metadata 组合场景测试
16. 继续优化 terminal manager 的信息分组与交互路径，使其更像真正的资源管理界面

## P2 后续封存后的优化空间

17. 为 render cache 建 benchmark 基线，覆盖大输出、overlay 开关、浮窗拖拽、共享全屏程序
18. 再做一轮 UI polish，统一 modal / picker / manager / pane chrome / bottom bar 的视觉细节
19. 评估是否需要 terminal-only 专门管理页的更深能力，例如批量操作、批量标签过滤、批量停止
