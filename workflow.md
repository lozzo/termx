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
| R8 | 进行中 | Edge 证书档案、证书轮换、升级、回滚和生产运维收口 | 当前活动切片，按下述纵向链路实现和验收 |
| R9 | 待开始 | 上线门禁与正式发布 | R8 完成后启动 |
| WEB001 | 延后 | 浏览器 Web/WASM terminal 产品 | 仅用户明确解冻后启动 |

当前最早未完成切片是 `R8`；本轮只执行 R8，不得跳到 R9 或 WEB001。

## R8 产品范围

### 证书档案与轮换

- 证书档案持久化名称、域名集合、签发模式、版本、证书链、指纹、有效期、绑定 Edge、desired/applied 版本、发布状态和错误；私钥不得进入 Controller、PostgreSQL、HTTP/Proto 响应、日志或运营页面。
- R8 首发签发模式固定为 `EDGE_CSR`：Edge 为每个档案在本机生成或复用不可导出的 `0600` 私钥，通过既有 mTLS `EdgeControl` 提交 CSR，Controller CA 校验目标 Edge、域名与 CSR 后签发证书链。ACME 自动续期和 KMS 托管导入私钥作为后续扩展，不得为它们提前增加 secret storage 或通用 provider 框架。
- 一个证书档案可以绑定一个或多个 Edge；同一 Edge 首发只绑定一个当前公网档案。运营人员可以创建档案、绑定 Edge、签发新版本、逐节点发布和回滚到仍在有效期内的历史版本。
- Edge 收到证书发布后必须验证独立签名域、目标 Edge、版本、私钥匹配、DNS SAN、有效期和证书链，先写临时文件并 `fsync + rename`，再通过 TLS certificate loader 原子切换；任一步失败都继续使用旧证书并上报稳定错误码。
- Edge 重连时通过 `EdgeHello.certificate_version` 和持久 applied 状态完成收敛；Controller 重启不能丢失 desired 版本和发布记录，Edge 离线时发布保持待处理而不是伪造成功。

### Edge 版本升级与回滚

- Controller 持久化不可变 artifact、SHA-256、独立 release 签名、版本通道和每个 Edge 的 rollout 状态；安装和升级复用同一份 manifest 验证语义。
- 升级由运营人员从 Edge 页面发起，固定顺序是 `requested -> staged -> draining -> activating -> healthy`。Edge 先下载到本机 state directory 并验签，报告 staged 后才停止接收新的 agent/client/Relay，现有工作只等待有界 drain，不承诺单 Edge 零中断。
- 安装脚本同时装配 root-owned systemd 更新器。Edge 进程只能在 state directory 写入已验签的激活请求；更新器再次验签后安装到 `/opt/muxvia-cloud-edge/releases/<version>/`、原子切换 `current`、重启并等待新进程完成 Controller snapshot。
- 新版本未在期限内报告 healthy 或启动失败时，更新器自动恢复前一个 `current` symlink 并重启旧版本；证书、EdgeIdentity 和 usage outbox 不随二进制回滚。回滚结果必须回传 Controller 并进入审计。
- R8 只做 Linux/amd64、单区域、逐 Edge 升级，不做 Relay Mesh、多节点并发灰度、零停机替换、动态通道策略或通用发布平台。

### 运营管理与生产门禁

- Edge 列表和详情展示当前软件版本、目标版本、升级状态、证书档案、desired/applied 证书版本、到期时间和最近错误；编辑 Edge 时可以选择证书档案和 release channel，但不能编辑 ID、密钥、Controller 地址或健康路径。
- `/app/admin/certificates` 使用真实独立 API 展示档案、域名、版本、有效期、Edge 绑定和发布进度，并提供创建档案、签发、发布和回滚操作；不得继续使用 Edge 列表拼接占位数据。
- 所有 mutation 都使用现有 operator session、CSRF、revision 或状态机 fence，并写入持久审计；页面展示真实 ACK、失败和离线待处理状态。
- Controller 与 Edge 分别暴露 loopback-only Prometheus 指标，至少覆盖连接/请求/错误、Directory 数量、证书有效期与 applied 版本、配置版本、release 状态、usage outbox 和重连；label 不得包含账号、daemon、session 或其它高基数 ID。
- 公开登录、注册、claim/install 和 Edge 注册入口具备有界速率限制；超限返回明确 HTTP 状态，不记录密码、token、票据、私钥、完整 SDP/ICE 或业务 payload。
- 提供可复现的 PostgreSQL 备份、校验和恢复脚本；恢复验证必须在独立数据库完成 schema、Edge/证书/release/账号交易数据查询，不得覆盖线上数据库。

### 视觉与交互

- 延续 CLOUDP007 已验收的 Cloud Shell、中文文案、色彩、字体、Lucide 图标、0-4px 圆角和移动抽屉，不重新设计普通用户页面或导航结构。
- 证书和发布状态以紧凑表格、状态标识、进度和明确操作呈现；高风险发布/回滚必须显示目标 Edge、当前版本、目标版本和确认对话框。
- 模块切换继续保留 Shell 与缓存；不得因新增证书或升级请求恢复全屏 loading、卡片套卡片、横向溢出或把所有状态堆在一个页面。

## R8 技术顺序

1. **契约与状态机 harness**：先新增证书、release 和 EdgeControl Proto，补生成/descriptor/round-trip 门禁，明确 persisted desired state、Edge-local applied state、控制流 ACK 和 systemd 激活结果的 owner。
2. **证书持久域**：增加 PostgreSQL migration、证书档案/版本/绑定/release store、独立证书发布签名和 operator service；先以 fake Edge control harness 证明 revision、离线待处理和有效版本回滚。
3. **证书真实控制流**：Edge 本机生成 CSR，Controller 签发并下发，Edge 完成校验、`fsync + rename`、TLS 原子热加载和 applied 上报；用真实 TLS 握手证明新证书生效和失败保留旧证书。
4. **artifact 发布域**：持久化并签名 release manifest，Edge 下载/验签/stage，控制流完成 drain 和 activate，systemd 更新器完成切换、healthy fence 和自动回滚。
5. **运营页面**：把证书占位页替换为真实档案/版本/绑定/发布页面，在 Edge 页展示和操作证书及软件 rollout；只用 Playwright 覆盖桌面、平板和手机。
6. **生产门禁**：补结构化指标、公开入口限流、secret audit、独立数据库备份恢复 harness、部署脚本和故障注入。
7. **线上纵向验收**：推送可追溯提交后，在现有海外 Controller 和国内 Edge 完成证书新版本热更新、无效证书失败保留、软件新版本升级、故意失败自动回滚、再升级到目标版本，并验证 P2P/Relay 基本 smoke 不回归。
8. **双审查与提交**：架构 reviewer 和代码 reviewer 只按 R8 的现实链路、契约和门禁审查；两者明确 PASS 后更新状态、中文提交并推送。

新增或修改跨边界 API 的固定顺序是：`proto schema -> generated code -> compatibility harness -> API Layer/handler -> store/runtime -> Web/client consumer`。

## R8 完成条件

- 真实 PostgreSQL 中存在证书档案、不可变版本、Edge 绑定、release artifact/channel/rollout 和审计；不存在证书私钥、实时 topology snapshot/WAL 或可恢复在线状态。
- 真实 Edge 私钥始终在本机且权限为 `0600`；CSR 签发、正常发布、运行中 TLS 新握手使用新证书、进程重启后保持 applied 版本全部通过。
- 错误签名、错误域名、过期/未生效证书、私钥不匹配和离线 Edge 均返回明确状态；失败发布不替换旧文件、不改变 TLS 当前指针、不伪造 applied。
- 真实 artifact manifest 的 SHA-256 与独立签名经过 Controller 和本机更新器双重校验；逐节点升级经历 staged/drain/activate/healthy，版本和状态在页面、Directory 与 PostgreSQL 一致。
- 故意提供无法启动或无法变为 ready 的目标版本会自动恢复前一个 symlink 和旧进程；随后合法升级仍可成功，证书、EdgeIdentity 和 usage outbox 未回滚或丢失。
- `/app/admin/certificates` 和 Edge 详情/列表使用真实 API 支持创建、绑定、签发、发布、升级和回滚；Playwright 在 390x844、768x1024、1440x900 覆盖成功、失败、离线、确认、缓存、横向溢出及 console/page error。
- Controller/Edge 指标不含高基数或 secret label；登录/注册/install/register 限流可复现；日志和构建产物 secret audit 不出现密码、token、私钥或票据。
- PostgreSQL 备份产物有 SHA-256，并在独立空数据库完成恢复和关键数据查询；线上数据库不被恢复测试覆盖。
- 运行受影响 Go tests/race、Proto generated/descriptor、Cloud Web unit/typecheck/build/Playwright、部署 shell check、`make doctor`、`make test` 和 `git diff --check`。
- 线上 Controller/Edge 二进制、Web 静态资源、migration、artifact manifest、systemd 单元、证书版本、升级与回滚证据均可追溯到已推送提交；架构 reviewer 与代码 reviewer 都给出带现实证据的 `PASS`。

## 当前允许修改

- 主范围：`workflow.md`、`proto/cloud/v1/`、对应生成代码、`cloud/controller/certificate/`、`cloud/controller/release/`、`cloud/controller/control/`、`cloud/controller/postgres/`、`cloud/controller/apihttp/`、`cloud/edge/certificate/`、`cloud/edge/release/`、`cloud/edge/controllerlink/`、`cloud/edge/runtime/`、`cloud/web/`、`cloud/integration/`。
- 组装联动：`cloud/controller/edgeconfig/`、`cloud/controller/install/`、`cloud/edge/bootstrap/`、`cloud/processhealth/`、`cloud/securetransport/`、`cmd/muxvia-cloud-controller/`、`cmd/muxvia-cloud-edge/`，只允许 R8 真实纵向所需的最小改动。
- 部署和准入联动：`cloud/deploy/`、`scripts/`、`Makefile`、`go.work*`，只允许 artifact、systemd 更新器、备份恢复、指标/secret audit 和当前开发部署所需的最小改动。
- `client/`、`remote/`、`cloud/daemon/`、`clients/ui/` 与 `clients/mobile/` 默认只读；只有 R8 线上升级回归证明现有 P2P/Relay 契约必须联动时，才允许最小修复，不得增加客户端产品能力。
- `ARCHITECTURE.md` 仅在稳定架构决策或切片状态必须同步时修改。
- 用户现有未跟踪截图和 `test-results/` 不属于本切片，不得添加、删除或覆盖。

## 测试与提交规则

1. 每轮先读取本文并检查 `git status --short --branch`。
2. 只执行 `R8`，不提前实现 R9、普通用户产品扩展或 Web terminal。
3. 先用测试证明当前缺口，再做最小但完整的纵向实现；静态页面、fake ack、手工写数据库和直接调用 JNI/Go 都不能冒充 E2E。
4. 浏览器验收只使用 Playwright，不使用 browser 插件或人工浏览器点击作为通过证据。
5. 每个独立、可回滚的纵向阶段使用中文提交信息提交；部署前必须推送对应提交，线上二进制和静态资源必须可追溯到该提交。
6. 不提交凭据、token、私钥、数据库口令或测试产生的截图/trace；用户在聊天中暴露过的 token 视为已泄露，不能复用到仓库或日志。
7. 工作树出现来源不明且与当前文件重叠的改动时停止；无关未跟踪文件保持原样。
