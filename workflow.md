# Muxvia 当前工作流

## 文档地位

本文档是当前分支唯一有效的活动驱动文件，决定工作范围、执行顺序、测试准入和提交边界。`ARCHITECTURE.md` 保存 Cloud 重构的稳定架构背景；当其里程碑状态或旧部署记录与本文冲突时，以本文为准。

当前仍处于私有开发阶段。浏览器 Web/WASM terminal 产品冻结；本轮 Web 只负责 Muxvia Cloud 的公开介绍、账号、设备、订阅、用量和运营管理，不能承载 terminal 数据面或建立第二套客户端运行时。

## 当前状态

| 切片 | 状态 | 目标 | 完成证据 |
| --- | --- | --- | --- |
| BRAND001-BRAND005 | 已完成 | Muxvia 品牌与发布身份迁移 | 历史提交与现有构建门禁 |
| R1-R6 | 已完成 | Cloud 契约、Controller/Edge、注册、P2P、Relay、用量纵向链路 | 现有集成测试、在线部署与 `ARCHITECTURE.md` 记录 |
| R7 | 已完成但产品验收已并入 CLOUDP007 | 账号、交易、中文运营 API 和管理模块 | 现有账号/交易集成测试与运营路由 Playwright；不能据此宣称普通用户产品完成 |
| CLOUDP007 | 已完成 | Development 全产品 E2E | `f11ef1f6` 已推送并在线部署；Web 三视口、最终 ARM64 APK 全 Route、真实 PostgreSQL 与双 Agent 审查全部 PASS |
| R8 | 待开始 | Edge 证书档案、证书轮换、升级、回滚和生产运维收口 | CLOUDP007 完成后启动 |
| R9 | 待开始 | 上线门禁与正式发布 | R8 完成后启动 |
| WEB001 | 延后 | 浏览器 Web/WASM terminal 产品 | 仅用户明确解冻后启动 |

当前最早未完成切片是 `R8`；本轮尚未启动 R8，不得跳到 R9 或 WEB001。

## CLOUDP007 产品范围

### 统一公开入口

- `https://cloud.muxvia.com/` 是 Muxvia Cloud 公开落地页，首屏直接展示真实产品和清晰的注册、登录入口。
- `/login` 是普通用户和管理员共用的登录页，不提供角色选择器。登录后统一进入 `/app/overview`。
- `/register` 完成普通用户注册；注册、登录、错误和成功反馈全部使用简体中文。
- 普通用户和管理员共享同一个用户工作台。管理员只是在普通用户导航之后额外看到“运营管理”分组，不能进入另一套全屏程序。

### 用户工作台

- `/app/overview`：账号、当前套餐、设备在线摘要、周期用量和待处理订单。
- `/app/devices`：只显示当前账号的 daemon；支持生成一次性 enrollment 命令、查看在线 Edge/状态和撤销自己的 daemon。
- `/app/subscription`：展示可购买套餐、当前 Subscription/Entitlement、周期和配额；支持开发环境可重复验证的下单与生效链路。
- `/app/orders`：只显示当前账号订单和支付尝试，状态、金额、周期与结果清晰可见。
- `/app/usage`：展示当前周期 Relay 用量、额度和剩余量。
- `/app/security`：展示账号、近期认证和会话；支持修改密码、退出当前会话和撤销其它会话。
- 用户 API 必须从已认证 session 推导 `account_id`，不得接受用户提交任意账号 ID 越权查询或创建资源。

### 运营管理

- 管理员继续使用现有 Edge、daemon、连接、账号、套餐、订阅、订单、证书、用量、审计和系统模块。
- 桌面端左侧侧栏始终存在，右侧只渲染当前模块；移动端使用用户主导航和可关闭的管理抽屉。
- 每个左侧菜单项必须有独立 URL、独立数据请求和独立内容；不能要求手工输入路径，不能把所有模块堆在同一页。
- 路由切换保留 Shell 和已加载缓存，后台静默刷新；不能每次点击菜单都回到全屏 loading。
- 非管理员访问 `/app/admin/*` 返回明确的无权限页面，不能泄露运营数据，也不能静默跳到错误模块。

### 视觉与交互

- Web 与 APK 使用同一产品语言：浅灰背景 `#f1f3f6`、白色表面 `#ffffff`、主文字 `#111418`、次要文字 `#626d7a`、品牌蓝 `#246bed`、成功 `#087a45`、危险 `#c52b32`。
- 使用系统中文字体栈，命令、ID 和数字使用等宽字体；不依赖中国大陆不可稳定加载的在线字体。
- 控件和内容面板使用 0-4px 小圆角；不使用渐变、玻璃、装饰光球、卡片套卡片或深绿单色后台主题。
- 图标统一使用 Lucide；触控目标至少 44px，键盘焦点清晰，正文对比度满足 WCAG AA，动效限制在 150-250ms 并尊重 `prefers-reduced-motion`。
- 手机宽度不得横向溢出；固定导航不得遮挡正文；加载、空状态、错误和危险操作确认必须完整。

## CLOUDP007 技术顺序

1. **契约与安全 harness**：盘点现有 Account、Commerce、Enrollment、Directory 和 Operator Proto/API；先补用户自助 daemon、session/password 和 Subscription transition 所缺 Proto，再生成代码并补权限测试。
2. **统一 Cloud Shell**：建立公开路由、认证守卫、角色守卫和共享用户导航，把现有运营页面迁入 `/app/admin/*`，删除旧 `OperatorShell` 和旧顶层运营路由。
3. **用户账号面**：完成落地页、统一登录、注册、Overview 和 Security 的真实 API 链路。
4. **设备纵向**：当前账号生成 enrollment、daemon 注册上线、用户列表观察 Presence、撤销后不可重连；持久身份和内存在线拓扑边界保持不变。
5. **商业纵向**：套餐、订单、开发支付、Subscription/Entitlement、周期 quota 和 usage 从真实 PostgreSQL 状态投影，不得在 Web 硬编码套餐或权益。
6. **全产品 E2E**：独立 Controller、至少一个真实 Edge、daemon、本地 CLI/TUI 和同一最终 ARM64 APK；Web 行为只用 Playwright，Android 行为必须从真实 App UI 发起。
7. **双审查与提交**：架构 reviewer 和代码 reviewer 只按本切片现实链路、契约和门禁审查；两者明确 PASS 后更新状态、中文提交并推送。

新增或修改跨边界 API 的固定顺序是：`proto schema -> generated code -> compatibility harness -> API Layer/handler -> store/runtime -> Web/client consumer`。

## CLOUDP007 完成条件

- 新用户可从 `/register` 注册，使用统一 `/login` 登录并进入 `/app/overview`；刷新和重新打开深链接仍保持正确身份与路由。
- 普通用户能在 Web 生成不含手填密钥、Controller 地址或健康检查地址的一次性 daemon enrollment 命令；daemon 注册后在该用户设备页出现并显示真实在线 Edge。
- 用户可通过现有配对链路在最终 Android APK 中连接自己的 daemon，完成 Cloud P2P 与强制 Relay terminal 输入输出、文件上传/下载和取消；Direct/SSH 回归继续通过。
- 用户可查看真实套餐、创建自己的订单，并通过明确标识为 Development 的测试支付使 Subscription/Entitlement 生效；quota、usage、suspend/恢复和账号隔离有真实跨组件证据。
- 管理员从同一登录页进入同一用户工作台，并额外看到全部 11 个运营模块；桌面固定侧栏和移动管理抽屉均可逐项进入独立页面。
- Controller 重启后持久业务恢复，Edge 全量上报重建内存 Presence；Edge 重启后旧 generation 被清理，daemon 重连；数据库不存在实时 topology snapshot/WAL。
- Web Playwright 至少覆盖 390x844、768x1024 和 1440x900，包含注册、登录、用户导航、管理员导航、深链接、权限、表单错误、loading/cache、横向溢出、console/page error 和真实在线数据。
- Android 证据矩阵记录 APK 路径与 SHA-256、AVD/ABI/API、Route/网络条件、App UI 动作、结果 oracle、关键日志、实际结果和结论；扫描 Java/native crash 与 secret 泄漏。
- 运行受影响 Go tests/race、Proto generated/descriptor、Cloud Web unit/typecheck/build/Playwright、Android build/test 和 `git diff --check`。
- 架构 reviewer 与代码 reviewer 都给出带现实证据的 `PASS`。

## 当前允许修改

- 主范围：`workflow.md`、`proto/cloud/v1/`、对应生成代码、`cloud/controller/`、`cloud/web/`、`cloud/integration/`。
- 设备与客户端纵向联动：`cloud/daemon/`、`client/`、`remote/`、`cmd/muxvia/`、`clients/ui/`、`clients/mobile/`，只允许 CLOUDP007 E2E 已证明需要的最小改动。
- 部署和准入联动：`cloud/deploy/`、`scripts/`、`Makefile`、`go.work*`，只允许构建、测试和当前开发部署需要的最小改动。
- `ARCHITECTURE.md` 仅在稳定架构决策或切片状态必须同步时修改。
- 用户现有未跟踪截图和 `test-results/` 不属于本切片，不得添加、删除或覆盖。

## 测试与提交规则

1. 每轮先读取本文并检查 `git status --short --branch`。
2. 只执行 `CLOUDP007`，不提前实现 R8/R9 或 Web terminal。
3. 先用测试证明当前缺口，再做最小但完整的纵向实现；静态页面、fake ack、手工写数据库和直接调用 JNI/Go 都不能冒充 E2E。
4. 浏览器验收只使用 Playwright，不使用 browser 插件或人工浏览器点击作为通过证据。
5. 每个独立、可回滚的纵向阶段使用中文提交信息提交；部署前必须推送对应提交，线上二进制和静态资源必须可追溯到该提交。
6. 不提交凭据、token、私钥、数据库口令或测试产生的截图/trace；用户在聊天中暴露过的 token 视为已泄露，不能复用到仓库或日志。
7. 工作树出现来源不明且与当前文件重叠的改动时停止；无关未跟踪文件保持原样。
