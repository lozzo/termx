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
| R8 | 已完成 | Edge 证书上传、档案绑定和自动更新 | `1ddf231f` 已推送并在线部署；真实 PostgreSQL、TLS 热切换、Playwright 与双 Agent 审查均 PASS |
| R9 | 待开始 | 上线门禁与正式发布 | R8 完成后启动 |
| WEB001 | 延后 | 浏览器 Web/WASM terminal 产品 | 仅用户明确解冻后启动 |

当前最早未完成切片是 `R9`，状态仍为待开始；R8 已关闭，不得继续向证书领域增加 release/rollout 能力。R9 开始前必须先把上线门禁拆成可执行切片，WEB001 继续冻结。

## R8 产品范围

### 证书档案与自动更新

- 运营人员只上传一组匹配的证书链文件和私钥文件，并填写档案名称；Controller 从证书中解析 DNS SAN、指纹和有效期，不要求运营人员填写 CSR、签名算法、版本号、健康检查地址、公钥或其它部署内部信息。
- Controller 在接受上传前必须校验证书与私钥匹配、证书当前有效、证书链可解析，并在绑定 Edge 时校验证书 DNS SAN 覆盖该 Edge 的公开域名。校验失败不能改变当前档案或任何 Edge 的在用证书。
- 一个证书档案可以绑定多个 Edge；同一 Edge 只绑定一个当前证书档案。绑定关系是持久配置，Edge 启动时不需要人工重复选择。
- 证书档案只保留当前内容和单调递增的 `revision`，不向产品暴露历史版本、灰度发布、手工发布或回滚。运营人员替换档案文件后，Controller 自动向所有在线绑定 Edge 下发新 revision；离线 Edge 在重连时自动收敛。
- Controller 将证书和私钥保存在专用 secret 目录，目录权限为 `0700`、文件权限为 `0600`；PostgreSQL 只保存档案元数据和不可猜测的 secret 引用。私钥不得进入数据库、普通 HTTP API 响应、运营页面、审计详情或日志。
- Controller 与 Edge 之间只通过既有 mTLS `EdgeControl` 传输目标 Edge 的证书包。Edge 必须再次校验证书、私钥、域名和有效期，原子写入本地 `0600` 文件并热切换 TLS loader；任一步失败都继续使用当前已加载证书并上报错误。这是失败保护，不是产品回滚能力。
- Edge 上报已应用的档案 ID、revision 和最近错误；Controller 重启后以 PostgreSQL 的绑定和 desired revision 为真值，Edge 重连后以本机 applied revision 对账，不持久化在线连接或实时 topology。

### Edge 程序部署边界

- R8 不建设二进制 artifact、release channel、在线升级、灰度发布、drain、systemd 更新器或自动回滚平台。
- Edge 程序继续通过已签名安装产物和人工 SSH/systemd 部署；运营页面只展示 Edge 实际上报的软件版本，不提供升级按钮和目标版本状态。
- 后续确有批量升级需求时另开切片，不能把它与证书更新复用成通用发布框架。

### 运营管理与生产门禁

- Edge 列表和详情展示实际上报的软件版本、绑定证书档案、desired/applied revision、证书到期时间和最近错误；编辑 Edge 时可以选择证书档案，但不能编辑 ID、密钥、Controller 地址或健康路径。
- `/app/admin/certificates` 使用真实独立 API 展示档案名称、域名、指纹、有效期、revision、绑定 Edge 和应用状态，并提供上传、替换和绑定操作；不得继续使用 Edge 列表拼接占位数据。
- 所有 mutation 都使用现有 operator session、CSRF 和 revision fence，并写入持久审计；页面展示真实成功、失败和离线待同步状态。
- 日志、HTTP 响应、数据库检查和构建产物 secret audit 不得出现证书私钥；本切片不顺手扩展指标平台、公开入口限流或数据库备份系统。

### 视觉与交互

- 延续 CLOUDP007 已验收的 Cloud Shell、中文文案、色彩、字体、Lucide 图标、0-4px 圆角和移动抽屉，不重新设计普通用户页面或导航结构。
- 证书和应用状态以紧凑表格、状态标识和明确操作呈现；私钥选择框只能显示文件名或“已选择”，不得回显文件内容。
- 模块切换继续保留 Shell 与缓存；不得因新增证书请求恢复全屏 loading、卡片套卡片、横向溢出或把所有状态堆在一个页面。

## R8 技术顺序

1. **回退复杂方案**：删除已进入本分支的 CSR、证书历史版本、release、rollout、systemd 更新器和自动回滚契约与代码，确认生成代码回到一致基线。
2. **简单契约与持久域**：先定义证书档案元数据、双文件上传、Edge 绑定、单一 desired/applied revision 和 mTLS 证书包 Proto，再增加 PostgreSQL metadata store 与 Controller `0600` secret file store。
3. **真实自动更新链路**：完成上传校验、原子替换、在线自动下发、离线重连收敛、Edge 二次校验、原子落盘、TLS 热加载和 applied/error 上报；用真实 TLS 握手证明成功更新与失败保留。
4. **运营页面**：把证书占位页替换为真实上传、替换、绑定和状态页面，在 Edge 页展示证书与实际软件版本；只用 Playwright 覆盖桌面、平板和手机。
5. **纵向验收**：使用真实 PostgreSQL 和 Controller/Edge mTLS harness 验证私钥不入库、不回显、不进日志，验证在线与重连更新，并回归现有 P2P/Relay smoke。
6. **双审查与提交**：架构 reviewer 和代码 reviewer 只按上述简化链路审查；两者明确 PASS 后更新状态、中文提交并推送。

新增或修改跨边界 API 的固定顺序是：`proto schema -> generated code -> compatibility harness -> API Layer/handler -> store/runtime -> Web/client consumer`。

## R8 完成条件

- 真实 PostgreSQL 中存在证书档案元数据、当前 revision、Edge 绑定、desired/applied revision、最近错误和审计；不存在证书或私钥 PEM、release/rollout 表、实时 topology snapshot/WAL 或可恢复在线状态。
- Controller secret 目录和文件权限分别为 `0700`、`0600`；普通 API、页面、日志和审计详情不回显私钥。上传无效 PEM、过期/未生效证书或不匹配私钥时，当前档案内容和 revision 不变。
- 在线绑定 Edge 会在档案替换后自动收到新 revision；离线 Edge 重连后自动收敛。Edge 校验、原子落盘、TLS 热切换和进程重启保持全部通过，运行中新 TLS 握手使用新证书。
- 域名不匹配或 Edge 应用失败时返回明确状态，不替换旧文件、不改变当前 TLS loader、不伪造 applied revision；修正档案后可以继续自动更新。
- `/app/admin/certificates` 和 Edge 详情/列表使用真实 API 支持双文件上传、替换、绑定和状态查看；不存在签发、发布、证书回滚、release channel 或程序升级入口。Playwright 在 390x844、768x1024、1440x900 覆盖成功、校验失败、离线待同步、缓存、横向溢出及 console/page error。
- 运行受影响 Go tests/race、Proto generated/descriptor、Cloud Web unit/typecheck/build/Playwright、部署 shell check、`make doctor`、`make test` 和 `git diff --check`。
- 线上 Controller/Edge 二进制、Web 静态资源、migration 和证书 revision 证据均可追溯到已推送提交；架构 reviewer 与代码 reviewer 都给出带现实证据的 `PASS`。

## 当前允许修改

- 主范围：`workflow.md`、`proto/cloud/v1/`、对应生成代码、`cloud/controller/certificate/`、`cloud/controller/control/`、`cloud/controller/postgres/`、`cloud/controller/apihttp/`、`cloud/edge/certificate/`、`cloud/edge/controllerlink/`、`cloud/edge/runtime/`、`cloud/web/`、`cloud/integration/`。
- 组装联动：`cloud/controller/edgeconfig/`、`cloud/edge/bootstrap/`、`cloud/securetransport/`、`cmd/muxvia-cloud-controller/`、`cmd/muxvia-cloud-edge/`，只允许 R8 真实纵向所需的最小改动。
- 部署和准入联动：`cloud/deploy/`、`scripts/`、`Makefile`、`go.work*`，只允许 secret 目录、当前开发部署和已有门禁所需的最小改动。
- `cloud/controller/release/`、`cloud/edge/release/`、程序在线升级和自动回滚代码明确不在 R8 范围；不得创建通用 certificate provider、CSR CA、artifact 或 rollout 框架。
- `client/`、`remote/`、`cloud/daemon/`、`clients/ui/` 与 `clients/mobile/` 默认只读；只有 R8 回归证明现有 P2P/Relay 契约必须联动时，才允许最小修复，不得增加客户端产品能力。
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
