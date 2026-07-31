# AnyTTY 全项目审核与整改汇总

审计日期：2026-07-31
基线：`master@f0ce4dff`
整改分支：`codex/remediation-integration`
代码整改提交：`98499119`

## 1. 范围与结论

本轮覆盖安全与权限、可靠性与并发、架构与代码质量、过度设计与过度防御、产品体验、UI 与可访问性、性能以及工程质量。支付、订单和订阅业务明确排除；TUI 默认占用大量 Ctrl 快捷键保留为待议项。

产品边界已经统一：移动 App 不登录、不承载账号业务、不自动发现设备，也不提供手工输入设备入口；设备只能通过扫描服务端生成的 QR 添加，本地保存授权信息。Cloud 账号业务与 App 设备列表互不冒充同一套发现机制。

终端输出已经统一为每个 terminal 的有界共享 buffer。`block` 策略允许上游变慢，`drop` 策略产生有序 gap 并让消费者显式感知同步丢失；不会再为每个消费者无限复制输出。

| 状态 | 数量 | 含义 |
| --- | ---: | --- |
| 已关闭 | 72 | 已修复、已删除，或经调用证据确认应保留 |
| 排除 | 1 | `SEC-01` 支付业务 |
| 待议 | 1 | `UX-02` TUI Ctrl 快捷键 |
| 外部阻断 | 1 | `SEC-12` 仓库外发布身份、签名与保护规则 |

## 2. 关键决策

1. 不做旧实现兼容：项目尚未上线，删除旧 TS history facade、snake/camel 双结构解析、`before_offset` 和恒为零的 `row_index`，只保留 generated Proto 契约。
2. 不提前优化：仅在构建预算、内存上限或可复现交互问题有证据时修改；当前 chunk 在预算内，不为消除普通 bundler warning 拆分代码。
3. 不引入通用框架：并发、deadline、generation、actor 和 WebRTC 生命周期只在对应领域内实现，不抽成通用 registry。
4. 删除可证明无用的代码：移除死 port、旧 helper、无效 recovery、重复 protobuf clone 和未消费 DTO 字段；保留能阻止依赖方向回退的债务 ratchet。
5. 冻结历史只有一个契约：公共 latest 先 Freeze；older/newer/oldest/copy 必须携带 token。daemon 内部即时 latest 可无 token，但只做 consumer fence，不执行 `file.Sync`。
6. 只处理正常业务路径上可稳定复现的问题：不为畸形极端输入、理论时序或尚不存在的扩展场景增加分支、兼容层和通用抽象。

## 3. 安全与权限

| ID | 问题 | 处理与证据 | 状态 |
| --- | --- | --- | --- |
| SEC-01 | 支付链路不在本轮授权范围 | 未把支付、订单、订阅问题纳入整改或完成声明 | 排除 |
| SEC-02 | Relay 配额仅靠内存预留，崩溃或跨实例可能超卖 | 建立 durable reservation/journal、事务锁序、跨账期结算与物理分配隔离；见 `cloud/edge/relay`、Controller usage store 及 relay 测试 | 已关闭 |
| SEC-03 | grant 撤销/过期后既有传输可能继续存活 | 授权状态变更与 transport owner 绑定，撤销、过期和 store 失败按确切所有权关闭；见 `core/grant_transport*` | 已关闭 |
| SEC-04 | 公开账号入口可被枚举、暴力尝试或耗尽 limiter | 统一公开入口、一次性 setup、per-IP/per-account limiter、快速拒绝和错误去敏；见 `cloud/controller/account`、`cloud/controller/apihttp` | 已关闭 |
| SEC-05 | 依赖和供应链缺少持续门禁 | 固定 Actions SHA，加入 npm audit、govuln、generated、workspace、Gradle dependency verification 和 APK 边界检查；见 `.github/workflows/security.yml` | 已关闭 |
| SEC-06 | Cloud hello/challenge 可重放，boot identity 不完整 | challenge 一次性消费，绑定 host boot ID 与生命周期，重复/过期响应 fail closed；见 Cloud gateway 与对应 replay 测试 | 已关闭 |
| SEC-07 | Direct 信令在认证前可无限占用连接和 goroutine，peer/handler 关闭可能提前返回 | 增加 pre-auth/per-IP 准入、消息/连接预算和 deadline；Direct Serve 显式等待信令 worker、Pion peer 与 DataChannel handler，写响应失败立即归还 slot；见 `remote/webrtc`、`internal/protocol/directsignal` | 已关闭 |
| SEC-08 | Android cleartext、WebView 和 CSP 边界过宽 | 限制 loopback transport、禁止非预期 cleartext、收紧 WebView 导航/origin 与 CSP，release 资源复验；见 Android manifest/config 和 APK boundary 脚本 | 已关闭 |
| SEC-09 | release 可能保留诊断日志或敏感上下文 | 构建期 stripping、日志去敏、release bundle/APK 扫描以及错误 correlation ID；见 `scripts/verify-android-apk-boundary.sh` 和 mobile build tests | 已关闭 |
| SEC-10 | Android Bridge 缺 origin/token/frame/deadline 边界 | 绑定允许 origin、短期 token、frame size、operation deadline 与 accepted/closing slot 所有权；见 Android bridge 源码和单元测试 | 已关闭 |
| SEC-11 | 公共错误可能泄露内部原因 | API 错误统一映射为稳定 code、公开 message 和 correlation，内部日志保留但不回传；见 `api_mapping`、Cloud HTTP error mapping | 已关闭 |
| SEC-12 | release 签名、发布身份和仓库保护无法仅靠本地代码证明 | 仓库内已完成 pinned actions、doctor、audit、generated、Android release artifact gates；外部签名密钥、provenance identity、branch protection 和 hosted settings 仍需发布环境确认 | 外部阻断 |
| SEC-13 | Edge binding keyset 仅内存或非原子落盘 | key bundle 原子发布、父目录同步、权限收紧、Windows cache 创建窗口封闭并锁定 ownership health；见 `cloud/edge/bindingkeys` | 已关闭 |
| SEC-14 | HTTP request body 可绕过大小限制或错误返回不稳定 | 对 JSON、压缩/分块、已声明超限和中途超限统一有界读取、413/408 映射及 abort path；见 `cloud/controller/apihttp` | 已关闭 |
| SEC-15 | Android backup/data extraction 可能导出授权材料 | release manifest 禁止不受控 backup/data extraction，APK 解析脚本验证最终合并 manifest | 已关闭 |
| SEC-16 | certificate secret 根可误认文件系统根、符号链接父级或非受管目录，Windows 权限语义不足 | 创建前拒绝文件系统根和不可信现有目录，以固定 marker 认领根目录，使用 `os.Root` 固定物理路径；Unix 校验 owner/mode，Windows 校验并收紧 DACL，Reconcile 只操作受管状态 | 已关闭 |

## 4. 可靠性与并发

| ID | 问题 | 处理与证据 | 状态 |
| --- | --- | --- | --- |
| REL-01 | terminal 输出随消费者数量无界增长 | 实现 terminal 级有界共享 buffer、全局 resident budget、`block`/`drop` 策略和 gap 语义；见 `core/terminal_output_buffer.go` | 已关闭 |
| REL-02 | 慢消费者、consumer failure 与 flush 缺少可观察契约 | 每类 consumer 有 cursor/error/gap 统计，失败释放容量；历史 snapshot/copy 有界且失败可恢复；live read 只等 consumer fence，Freeze 才做一次 durable sync | 已关闭 |
| REL-03 | Server/connection 请求和资源可无限并发 | 加入全局 request budget 与每 session request/resource/attachment/file/subscription 上限；见 `core/server.go`、`core/protocol_resource_limits_test.go` | 已关闭 |
| REL-04 | malformed、duplicate 和 pending protocol frame 可耗尽内存 | 限制 frame/payload/pending bytes、重复 ID、未完成请求和 cleanup accounting；见 `internal/protocol`、client protocol tests | 已关闭 |
| REL-05 | Relay 预留、结算和 shutdown 交错可丢账 | durable reservation、idempotent settlement、retryable shutdown 与冻结失败 ownership；见 relay journal/server tests | 已关闭 |
| REL-06 | certificate secret 发布成功但父目录项未持久化 | 文件和目录 fsync 后 rename，再同步父目录；失败通过 tombstone 与 `Reconcile` 按数据库真值恢复或清理，已删除无生产消费者的公开 Delete | 已关闭 |
| REL-07 | event stream 无界或 replay 边界不清 | event buffer 有界，订阅资源计数，overflow/close/replay 明确；见 Core event subscription tests | 已关闭 |
| REL-08 | queued command cancel 与 committed-after-execute 状态混淆 | operation ownership、commit/rollback 与 cancellation result 分离，已经提交的结果不伪装成取消；见 API layer/client runtime tests | 已关闭 |
| REL-09 | Core/Edge shutdown 忽略 deadline 或留下 owner | Core 单一关闭所有权；Edge/Relay 支持 deadline、重试和未完成 owner 保留；见 server shutdown tests | 已关闭 |
| REL-10 | channel ID 回绕或复用可能关联错误资源 | ID 单调分配、耗尽 fail closed、关闭后不复用仍在清理的 slot | 已关闭 |
| REL-11 | close/join 可能重复释放或漏等 goroutine | accepted/closing slot、exact detach、generation fence 与 join barrier 覆盖 Direct、Cloud WebRTC 和 Edge runtime 的实际 owner；定向 race tests 已覆盖 | 已关闭 |
| REL-12 | runtime agent/generation 状态可无界增长 | 限制 agent 数量和代际，拒绝路径保持原子，旧 generation 不污染新 session；见 `cloud/edge/runtime` | 已关闭 |
| REL-13 | endpoint acquire lock 在 cancel/close/时钟交错下泄漏 | acquisition 响应 context，owner Close 回收 waiter，锁 mutation 窗口有定向测试；见 `client/runtime/session_owner*` | 已关闭 |
| REL-14 | Controller 先串行关闭 HTTP 或 runtime 时会相互耗尽同一 deadline，SSE 可能拖住退出 | HTTPS 与 runtime 在同一全局 context 下并发 Shutdown 并合并错误；API server 先广播 lifecycle close，使 SSE 主动退出后再等待 HTTP shutdown | 已关闭 |

## 5. 架构与扩展性

| ID | 问题 | 处理与证据 | 状态 |
| --- | --- | --- | --- |
| EXT-01 | command glue 多处手写，扩展新命令易漏 | `application_commands.csv` 作为单一规格生成 API layer、mapping、binding 和 runtime glue；generated check 阻止漂移 | 已关闭 |
| EXT-02 | TUI product/runtime/history/live 职责集中 | 拆分 product content、runtime owner、history window state 和 live render projection，保持 port 有真实消费者 | 已关闭 |
| EXT-03 | App 账号/发现/添加入口边界冲突 | 统一为 accountless QR-only，本地授权存储；测试禁止账号、自动发现和手工添加入口 | 已关闭 |
| EXT-04 | 平台 CI 覆盖不完整 | Linux correctness/security、Windows、Android、targeted race 和 macOS build/test jobs 均已入仓；hosted runner 实际执行仍属于外部验证 | 已关闭 |
| EXT-05 | TUI port 或 DTO 无生产消费者 | 删除无消费 port、旧历史投影字段和只被测试保活的入口；dependency/import guards 防止回流 | 已关闭 |
| EXT-06 | 文档、脚本与真实目录结构漂移 | doctor、repository layout guard、workspace guard、generated descriptors 与 fixture isolation 形成可执行文档 | 已关闭 |

## 6. 过度设计与过度防御

| ID | 问题 | 处理与证据 | 状态 |
| --- | --- | --- | --- |
| OVR-01 | 死 helper、旧 facade 和防御性 fallback 被测试保活 | 删除旧 TS history request/event facade、snake/camel/压缩字段兼容、死 Go helper 和无效 fallback；终审提交删除 950 行，净减少 466 行 | 已关闭 |
| OVR-02 | protobuf 在唯一所有权路径重复 clone | 删除 API layer 同步重复 clone 和 binding 刚 Unmarshal 后的 clone；跨 goroutine/接口且确有共享风险的 clone 保留 | 已关闭 |
| OVR-03 | 依赖债务测试看似固化旧结构 | 复核后保留 ratchet：新增具体依赖立即失败，债务消失只要求同提交删一行许可，防止陈旧 allowlist 掩盖回归 | 已关闭（保留） |
| OVR-04 | connection message/recovery 路径无效或制造假状态 | 删除不生效 recovery、旧 message 状态和重复 loading truth，错误恢复只留真实可重试入口 | 已关闭 |
| OVR-05 | generation/actor/WebRTC/history 等复杂度可能被误判为过度设计 | 调用与 race 证据证明这些结构分别承担 stale fence、owner isolation、传输生命周期和双消费者 truth；不抽象成通用框架 | 已关闭（保留） |
| OVR-06 | history/certificate 接口和旧 Machine 列表组件只有测试消费者，增加维护面 | 删除 `HistoryReadState`、`HistoryMutationBatch`、`Apply/ReadState`、secret `Delete`、旧 `MachineList`/`MachineBrowserShell` 及导出；终审净减少 857 行，不保留兼容入口 | 已关闭 |

## 7. 体验与产品流程

| ID | 问题 | 处理与证据 | 状态 |
| --- | --- | --- | --- |
| UX-01 | App 添加设备流程与安全边界不一致 | 首屏直接提供扫描服务 QR；相机按需加载、取消/失败可恢复、导入后本地保存，不提供登录/发现/手工添加 | 已关闭 |
| UX-02 | TUI 默认占用大量 Ctrl 快捷键 | 未擅自更改；需要单独确定默认键位、透传模式和迁移文案 | 待议 |
| UX-03 | 移动端启动失败只有空白或不可恢复 | 增加分阶段诊断、安全复制、reset/retry、safe-area 与 release 日志去敏 | 已关闭 |
| UX-04 | 移动 terminal 修饰键状态不明确 | 显式 sticky/one-shot 模式、视觉/ARIA 状态、串行输入 ownership 和失败恢复 | 已关闭 |
| UX-05 | Android native back 与 modal/terminal 层级冲突 | 建立 LIFO back owner：scanner、dialog、sheet、file、terminal、workspace 顺序关闭并恢复焦点；设备详情也使用 nested overlay，底部操作避开 safe-area | 已关闭 |
| UX-06 | Cloud 账号首次 setup/重置生命周期不完整 | 一次性 setup、密码边界、并发提交、过期状态和重新进入路径闭合 | 已关闭 |
| UX-07 | 请求失败后没有可用 retry 或错误关联 | loading/error/success 分离，retry 重建真实请求，公开错误带 correlation；Cloud 登录明确区分限流、服务端、认证和网络失败且不泄露内部错误 | 已关闭 |
| UX-08 | 品牌、文案和 i18n 不一致 | App/Cloud/TUI 品牌与 QR-only 文案统一；i18n key gate 覆盖 621 keys | 已关闭 |
| UX-09 | Cloud 管理导航和移动表格不可重复操作 | 响应式 drawer/bottom nav、44px 操作目标、可滚动/折叠数据布局和恢复状态 | 已关闭 |

## 8. UI 与可访问性

| ID | 问题 | 处理与证据 | 状态 |
| --- | --- | --- | --- |
| UI-01 | viewport/zoom 配置妨碍放大 | 允许系统缩放，移动布局以实际 viewport 和 safe-area 约束 | 已关闭 |
| UI-02 | terminal 对屏幕阅读器缺少可理解状态 | terminal label/status、连接变化 live region 和操作按钮名称完整；首屏未就绪时显示 polite status，并同步将底层 terminal 设为 inert/aria-hidden | 已关闭 |
| UI-03 | modal 缺 focus trap、Escape、inert 和 restore | 共用 modal surface 管理 portal、焦点循环、关闭顺序、背景 inert 与原触发点恢复 | 已关闭 |
| UI-04 | 移动键和关键触控目标小于 44px | keybar、重试、switch、表格操作、header/footer/auth 控件均以稳定 44px 约束 | 已关闭 |
| UI-05 | terminal 文本选择和复制入口不可靠 | selection、visible/all、copy/history 使用权威 history surface，不从 xterm cache 拼装业务结果 | 已关闭 |
| UI-06 | icon button 缺 label 或 focus 样式 | 使用现有 icon 库、`aria-label`/title 与一致 focus-visible 状态 | 已关闭 |
| UI-07 | 路由切换不更新 title/主内容焦点 | Cloud 页面设置具体 title，路由后聚焦 main，避免键盘用户落在旧页面 | 已关闭 |
| UI-08 | 菜单 ARIA 与 modal 实现分裂 | menu 状态、modal surface、close reason 和 overlay 顺序统一 | 已关闭 |
| UI-09 | mobile header/footer/auth 在窄屏或短横屏溢出 | 320px/360px、375px 竖屏和 812px 短横屏布局、safe-area、可换行文本与固定操作尺寸均有测试和实测 | 已关闭 |
| UI-10 | TUI glyph 在终端字体下不可移植 | 默认符号改为 portable glyph，不依赖特殊 Nerd Font | 已关闭 |
| UI-11 | 可访问性只靠人工抽查 | 加入 axe 路由审计并设置完整导航预算，关键 modal/表格/登录有组件测试 | 已关闭 |
| UI-12 | 官网首屏完全遮住下一段内容或桌面文案与终端重叠 | hero 在手机、短横屏和桌面均露出下一 section；`1440x900` 实测文案与产品终端无重叠、无横向溢出 | 已关闭 |
| UI-13 | loading 用文本或空白冒充稳定状态 | 统一 spinner/status、`aria-live`、不可操作状态和加载失败恢复 | 已关闭 |
| UI-14 | 颜色/对比度/假 retry 与 pending 状态不一致 | 语义色和暗色恢复统一，retry 只在真实可执行时出现；终端短横屏工具面板可纵向滚动 | 已关闭 |

## 9. 性能

| ID | 问题 | 处理与证据 | 状态 |
| --- | --- | --- | --- |
| PERF-01 | terminal 输出、history 与 protocol memory 无界 | buffer、history storage、request/frame/resource 与 aggregate resident 全部有显式上限和诊断 | 已关闭 |
| PERF-02 | 字体、FileManager、QR scanner 和 Three.js 首包过重 | 字体本地化/裁剪，FileManager、QR scanner、3D preview 按需加载；失败可重试且不破坏 lazy boundary | 已关闭 |
| PERF-03 | Cloud/UI route bundle 缺预算 | route lazy、manifest hash、initial/total raw+gzip budget 和构建检查已建立；当前在预算内，不做无证据拆分 | 已关闭 |

## 10. 工程质量

| ID | 问题 | 处理与证据 | 状态 |
| --- | --- | --- | --- |
| QLT-01 | 环境问题到构建后期才暴露 | `make doctor` 检查 Go/Node/Java/protoc/plugins、generated code、目录与平台前置条件 | 已关闭 |
| QLT-02 | Go 死代码与正确性检查不持续 | CI 执行全仓 tests、vet、pinned staticcheck `SA*,U1000` 和 govuln；本轮本地检查通过 | 已关闭 |
| QLT-03 | workspace 重复 React/Vite 或 fixture 污染真实仓库 | 单实例解析、workspace guard、隔离临时 fixture 和 clean-tree 检查 | 已关闭 |
| QLT-04 | Gradle 依赖与构建工具版本漂移 | wrapper/插件升级、dependency verification、offline release assemble 与最终 APK 复验 | 已关闭 |
| QLT-05 | APK/AAB 或本地构建产物混入源码 | layout guard 拒绝 tracked build output；用户根目录 `app-debug.apk` 明确不读取、不移动、不删除 | 已关闭 |
| QLT-06 | React 异步 refetch 测试未等待完成，产生 act warning 并掩盖真实失败 | 测试等待账号 mutation 后的 refetch 和最终 UI 状态；三端测试无遗留 warning | 已关闭 |
| QLT-07 | `make test-android` 构建 debug APK，却调用只允许 release 的安全边界脚本，正常执行必然失败 | target 改为 `testDebugUnitTest assembleRelease`，复制并验证 `app-release-unsigned.apk`；不弱化 release verifier，也不增加 debug 兼容分支 | 已关闭 |

## 11. 验证结果

截至代码整改提交 `98499119` 已完成：

- 前端测试：UI 63 个测试文件、327 个测试，Mobile 10 个测试文件、86 个测试，Cloud 9 个测试文件、35 个测试，合计 82 个文件、448 个测试全部通过。
- 前端静态检查：三端 lint、TypeScript typecheck 与 621 条共享 i18n key 检查通过。
- Go：`go test ./... -count=1 -timeout=20m` 全仓通过；Core/history、runtime、Direct/Cloud WebRTC、Relay、certificate、securefs、Controller API 等高风险包的定向 race 测试通过。
- 生成与静态检查：generated code、`go vet ./...`、pinned staticcheck `SA*,U1000` 全仓通过。
- 安全扫描：`npm audit --audit-level=high` 为 0 vulnerabilities；`govulncheck` 无可达漏洞、无 imported-package 漏洞，仅提示 1 条 required-module advisory，当前代码未导入或调用对应路径。
- 生产构建：UI、Mobile、Cloud production build 与 Capacitor build 通过。UI JS 为 205.63 kB raw / 64.70 kB gzip；Mobile initial 为 1,421,607 bytes raw / 376,538 bytes gzip，低于 1,550,000 / 430,000 门限；Cloud initial 为 406.9 KiB raw / 123.3 KiB gzip，total 为 523.4 KiB raw / 153.9 KiB gzip。
- Android：`make test-android` 完整通过 `testDebugUnitTest assembleRelease`、R8、lint vital、merged manifest、资源和 release APK boundary；APK 内生产资源为 25 个 JavaScript、2 个 CSS 文件。
- 浏览器验证：Cloud E2E 单独运行 60 passed / 75 skipped，axe 2 条路由审计通过；`1440x900`、`375x812`、`812x375` 实测无横向溢出、首屏遮挡或小触控目标。
- 环境检查：显式配置本机 Android SDK 后 `make doctor` 通过，覆盖 repository layout、workspace、generated code、Android source 与工具链。

## 12. 仍需外部完成或人工确认

1. 立即轮换审计期间曾出现在本地 tmux scrollback 中的真实认证凭据；本文不记录其内容。
2. 配置并验证 release signing identity、provenance、branch protection、required checks 和 hosted secret 权限。
3. 在真实 Android/iOS 设备完成 TalkBack/VoiceOver、相机权限、QR 扫描、横竖屏、safe-area 与系统返回键验收。
4. 在真实 Cloud/Edge/Relay 环境完成在线 E2E、断网恢复、跨进程配额结算与证书切换演练。
5. 决定 `UX-02` TUI Ctrl 默认策略后再单独实现，不与本轮已验证行为混改。

## 13. 不应回退的约束

- App 只能 QR 添加设备，不新增登录、账号同步、自动发现或手工地址入口。
- public history pagination/copy 必须使用 frozen token；禁止恢复 live fallback。
- 不恢复 `before_offset`、`row_index`、手写 TS history serializer 或 snake/camel 兼容解析。
- 不把 drop gap 静默当作完整 terminal output；不为慢消费者恢复无界队列。
- 不删除 dependency debt ratchet，除非由更强的 import-direction 规则替代。
- 不为了普通 bundle warning 提前拆包；先以显式 raw/gzip budget 和真实启动指标为准。
- 不为理论上的极端值、生产协议不可能产生的状态或尚未出现的兼容需求增加防御分支；先要求可复现证据。
