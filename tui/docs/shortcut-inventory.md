# TUI 快捷键最终审计

## 当前结论

机器可读契约基线位于 `shortcut-contract-debt.json`。默认 scene+key catalog 经统一输入路由进入 `tui/action` canonical registry，覆盖 keyboard、mouse-only、drag 与内容 CTA；旧 footer、chrome 和 content 名称不再冒充 action identity。render 只保留有真实视觉或几何消费者的本地 `ProjectionID`，每个可执行投影都单向引用 canonical action。

当前运行基线：`default_entries=203; routed_bindings=166; action_specs=159; render_projections=34; scenes=15`。

这行统计由 `tui/config/shortcut_docs_test.go` 直接根据运行 catalog 校验。本文不保存逐键默认表；默认 binding 的唯一来源是 `tui/shortcut`，用户可见目录由运行时 Help 投影，避免文档成为第二份快捷键真值。

最终 manifest 中所有审计 surface 均为 `conforming`，没有遗留到后续切片的 debt。`tui/app/shortcut_contract_audit_test.go` 会重新扫描生产源码中的 input producer、hit-region producer、展示键位字面量和各 surface 锚点，并用独立 digest 阻止未分类项或静默新增债务。

## Truth Owner

- raw TTY bytes 分帧和 Kitty CSI-u、传统 CSI/SS3、SGR mouse、OSC、bracketed paste 规范化由 `tui/terminalhost` 持有。
- keyboard、mouse、drag 和 CTA 共用的 action identity、参数 schema、invocation 与默认语义 label 由 `tui/action` 持有。
- 内置 scene、默认 scene+key binding、action 允许绑定的 scene、默认 footer/Help visibility 和 routable policy 由 `tui/shortcut` 持有。
- 用户 `tui.shortcuts` 的解析与校验由 `tui/config` 持有，解析结果由 reducer-owned `tui/state` 保存；binding/action label override、`show` 以及省略/空 map、action-only、显式 scene 替换语义不能由调用方重新解释。
- canonical key、用户/默认 catalog 编译和 `RequiresKeyboardDisambiguation` 标记由 `tui/input` 持有；它只生成 binding/invocation，不拥有 action identity。
- 宿主键盘能力 truth 来自 `tui/terminalhost` 并进入 reducer-owned `tui/state`；`tui/render` 只按编译 binding、用户 visibility 与 capability 投影 footer/Help，不反向定义可触发性。
- handler、组合步骤和失败语义由 `tui/app` 持有；endpoint-aware action 必须保留完整 `TerminalRef`。
- `tui/render` 只持有视觉与几何 `ProjectionSpec`，消费 view-model 中的 canonical invocation，不声明 action 是否存在或如何执行。
- 未修饰 `Esc` 不属于 shortcut catalog；返回层级由 reducer-owned `tui/state` 与 `tui/app` 持有，没有可返回层时才透传 PTY。

## 消息链路

```text
raw host bytes
  -> terminalhost InputEvent
  -> shortcut scene+key binding
  -> action.Invocation
  -> app handler / reducer
  -> effect / service owner
  -> result message
  -> reducer-owned state
  -> render view-model / feedback
```

键盘与可点击 surface 在 `action.Invocation` 处汇合。footer、Help、overlay、header、pane/floating chrome 和内容 CTA 不允许把 `ProjectionID`、展示 label 或硬编码键位反向解析成执行身份；聚合多个 invocation 的提示只能是 hint-only。

## 自动守卫

- `tui/app/shortcut_contract_audit_test.go`：运行 catalog、生产 producer、surface 分类、源码锚点与独立 digest 闭集。
- `tui/action/registry_test.go`：canonical spec、alias、参数范围以及 action domain 的依赖边界。
- `tui/app/action_catalog_test.go`：全部默认 binding 经正式 reducer/effect 链到达真实 owner，并精确核对 terminal mutation 与 `TerminalRef`。
- `tui/render/shortcut_invocation_test.go` 与 `tui/app/shortcut_surface_equivalence_test.go`：键盘/点击 invocation 同源、target fail closed、空 catalog、窄屏和 capability 条件。
- `tui/app/shortcut_legacy_guard_test.go`：禁止旧 dispatcher、render action bridge 和 footer action identity 返回。
- `tui/config/config_test.go` 与 `tui/config/shortcut_docs_test.go`：配置示例真实加载、替换语义、文档 contract、运行统计及本文不得恢复手写键表。
- `scripts/termx_shortcut_smoke.sh`：隔离 daemon、配置、日志和 tmux socket，验证普通快捷键与 CSI-u 的真实 TUI 链路。

## 迁移结论

旧 `tui.keymap`、分散的输入 binding、固定 footer/Help 元数据、render action dispatcher、surface 字符串桥接和无消费者 projection 已删除。本文原有的 KS001 逐键盘点只属于迁移输入，完成收口后继续保留会形成第二份过期 catalog，因此已删除；历史需要通过 Git 查看，不进入当前运行或维护 contract。
