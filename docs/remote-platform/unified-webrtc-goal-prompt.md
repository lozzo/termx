# 统一 WebRTC Route /goal Prompt

把下面内容作为 `/goal` 的任务说明。活动范围、任务顺序和完成条件始终以仓库根目录 `workflow.md` 为准；本 Prompt 只固定执行目标和不可偏离的门禁。

```text
持续执行 TermX 当前统一 WebRTC Route 主线，直到 workflow.md 中最早未完成切片及其后续活动切片全部完成，或者出现 workflow.md 定义的真实阻塞。

每轮必须先读取 AGENTS.md 和 workflow.md，再检查 git status --short --branch。只执行任务队列中最早的进行中或待开始切片，不跳过阻塞，不抢做 WEB001、iOS/Desktop、插件、开源发布、多区域 Cloud、KCP/QUIC 或其它未排期能力。发现未识别的用户或其他 Agent 未提交改动时停止说明，不得混入当前切片。

架构强约定：所有远程业务连接最终进入 Go-owned reliable ordered WebRTC DataChannel ReadyPeerSession。Direct 使用 daemon embedded signaling + ICE-TCP；SSH 使用 Go SSH client/direct-tcpip tunnel + daemon loopback ICE-TCP；Cloud 使用 TermX Cloud signaling + ICE-UDP 或 TURN Relay。Endpoint/Route、pairing、credential reference、planner、session generation、remote auth、Hello、Proto command/event、resource、取消和重连真值属于 Go Client Engine。Android 只能通过稳定 C ABI + 薄 JNI/Capacitor bridge 使用 Go；Kotlin/TypeScript 不得复制网络或 session 状态机。Web/WASM 当前冻结，只维持现有 contract 不回归。

API 强约定：跨边界 API 必须按 proto schema -> generated code -> compatibility harness -> API Layer/API Mapping/runtime/adapter -> binding/platform consumer 的顺序实现。跨 JNI 的业务 payload 只能是 versioned protobuf bytes 和 opaque handle，不得暴露 Go pointer、core struct 或平台 DTO。

实现纪律：只实现当前切片完成条件、现有契约直接要求、可复现失败或准入测试证明的问题。禁止提前优化、过度优化、假设性 hardening、未来通用框架、无关目录整理、旧路径 fallback 和为了 reviewer PASS 扩大切片。先说明 domain owner、truth source、消息链路和失败条件，再写最小真实 harness 和实现；删除被新模型替代的旧代码，不保留双路径。

Android 完成标准：凡 workflow.md 要求 Android 纵向验收，必须构建并安装本切片最终 APK 到仓库指定的 ARM64 Android 模拟器，并由真实 App UI 发起操作。最终 APK 必须至少覆盖：添加或导入 Endpoint；Direct、SSH、Cloud 和 LAN/TCP mapping 建连；terminal 列表；打开 terminal；两次以上输入命令、验证可识别输出并持续交互；从 App UI 上传和下载文件并校验长度与 SHA-256；取消进行中的操作并验证资源释放；锁屏/后台/WebView freeze 后旧 generation/handle 失效并由新 generation 重连；网络切换；受控弱网；logcat、AndroidRuntime、ANR 和 native crash 扫描。UI 自动化可以使用 UIAutomator、ADB 输入或 WebView DevTools/CDP，但用户动作必须由已安装 APK 的 UI 发起。daemon capture、文件系统、摘要和日志只能作为 UI 操作后的结果 oracle，不得直接调用 Go/JNI/binding 或手工修改客户端状态冒充 App E2E。物理设备只能补充，不能替代模拟器门禁。

APK 证据要求：在进入双 Agent 审查前，按 workflow.md 的 RTC010 最终证据矩阵逐项记录 APK 路径与 SHA-256、构建 profile、ARM64 AVD/ABI/API、Route/网络条件、App UI 操作、结果 oracle、日志或产物位置、实际结果和结论。较早切片 APK、单元测试、instrumentation、仅检查 DOM 状态或“代码路径可达”都不能替代最终 APK E2E；任何强制项缺失时不得把切片标记完成或请求 reviewer PASS。

双 Agent 审查：只在 workflow.md 明确要求的切片执行，并且必须在该切片全部测试准入和最终 APK 证据矩阵完成后开始。同时启动相互独立的只读架构 reviewer 与代码 reviewer。reviewer 只能依据当前切片范围、契约、完成条件、实现 diff、可复现行为和测试证据判定 PASS/FAIL，并把输出明确分为“当前切片阻塞 finding”和“不阻塞的 deferred observation”。每个阻塞 finding 必须指出具体代码或契约位置、触发条件、当前可观察影响和最小复现方式；缺少证据的“可能”“理论上”或未来规模风险不得阻塞，也不得要求主 Agent 为证明纯假设不存在而增加抽象、fallback、registry、锁或 hardening。未来扩展、理论优化、命名偏好、Web/iOS/Desktop、多区域和更通用抽象只能记录为 deferred observation，且只有这类观察时结论必须 PASS。主 Agent 独立核实 finding；实质修复后重新运行受影响测试并交原 reviewer 复审。两个 reviewer 都明确 PASS 后才允许机械更新 workflow 状态并提交。

每个切片必须运行 workflow.md 的全部准入，更新可复现证据和状态，运行 git diff --check，并使用中文提交信息提交。测试无法运行或真实环境阻塞时，记录具体命令、失败条件和已完成证据；不得用 fake、文档、接口或构建成功替代用户可观察的纵向闭环。没有真实阻塞时持续进入下一活动切片，不要求用户确认普通实现细节。
```
