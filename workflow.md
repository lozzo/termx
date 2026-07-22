# 工作流：Muxvia 品牌迁移与 Cloud Development 产品闭环

## 当前结论

- 用户已暂停 PG004 剩余部署验收，当前活动主线切换为产品体验整改。顺序固定为：`UX001` 中英文与文案基线 -> `QR001` 现有二维码输出可用性 -> `APPFIX001` 正式 App 测试信息与 loading 修正 -> `APPFIX002` 扫码登录 Session 稳定性 -> `CLOUDAUTH001` Hub 离线认证与本地授权解耦 -> `QR002` Proto-first 短码配对 -> `WEBUX001` Web Controller 账号/设备流程 -> `APPUX001` Android 信息架构与首次使用 -> `UXE2E001` 双端真实验收。完成后再恢复 PG004 的 R2、refresh、Presence、网络恢复和文件校验。
- 首期正式 UI 语言只承诺英文与简体中文。Web Controller 现有俄文资源保留为历史输入但从可选语言中暂时移除，直到键集合、关键流程和布局验收达到与英文/中文相同的完成条件；不得以 fallback 英文冒充俄文支持。
- `UX001` 已完成英文/简体中文基础设施与首批关键流程迁移：Web Controller 登录/账号/设备/激活和 Android 首页/设备/配对/设置均使用 locale key，语言默认跟随系统并可持久切换；App 143 个、Web 365 个 locale key 对称，稳定 native error code 不再把底层英文 message 直接投影到 UI；ARM64 模拟器已验证中文设备页和设置页。terminal/file 剩余文案按计划留给 `APPUX001`，不在本切片提前扩大。
- 二维码整改必须区分两类载荷：Web 手机 activation 只携带短期 `MXA` code，当前约 60 字符；daemon pairing 当前把约 599-byte signed bundle 编码成约 826 字符 URI，达到 QR Version 23 / 109x109 modules，是显示不全的根因。`QR001` 只提供终端尺寸检查、文本和图片 fallback；`QR002` 才通过 daemon-owned 内存 claim 缩短真实配对二维码。
- `QR001` 已完成现有载荷的可用输出：`muxvia pair create --text` 输出 portable URI，`--qr-file FILE` 原子写入 `0600` 的 `1024x1024` 正方形 PNG；默认终端渲染会在输出前计算完整 preview、二维码和提示所需行列，空间不足时零输出并提示替代命令。Web 手机 activation 继续使用短码、2-module quiet zone、响应式正方形图片和同级手工码。CLI 全包、App/Mobile/Web 客户端门禁和实际 PNG 检查通过；二维码密度本身留给 `QR002` 解决。
- `APPFIX002` 已修复扫码登录后状态丢失：Hub 设备目录同步不再拥有账号 Session 真值，也不会因新 Session 的 policy 投影尚未同步而清除 Android Keystore。线上 Web Controller 批准真实 `MXA` activation 后，ARM64 模拟器 App 展示账号与设备投影；强制停止并重启 App 后仍恢复同一登录态，重启期间 Cloud Route 短暂不可达也未触发 logout，且未发现 Java/native crash。
- `CLOUDAUTH001` 已完成 Hub 离线认证与本地授权解耦：Controller 私钥签发的 EdgeAccess token 由 Hub 公钥独立验证签名、有效期、Hub audience、账号、设备和 role；新 client 尚未进入 policy projection 时设备目录和 managed P2P 不再返回 unauthenticated。projection 只判断账号 revoke/auth epoch、套餐、target ownership、并发与 Relay 额度；无效 token、policy lag、明确撤销、entitlement 和 quota 已使用不同 Proto 错误。相同 Linux Edge 二进制已滚动部署到 US/CN，ARM64 公网 HTTPS APK 完成全新 `MXA` 登录、批准后首次目录同步、进程重启恢复和 crash scan。
- `APPFIX001` 已删除正式 App 的 `114.66.58.243:12306` fallback，原生 Cloud 设置页不再展示 Web Control/Hub 技术地址；显式 legacy HTTP staging 参数和测试 fixture 仍可用于受控测试，但不会进入默认产品 UI。统一 loading 改为固定方形外框与内部旋转指示器，reduced-motion 下停止内部动画；Playwright 两帧像素检查证明外框变化 `0`、内部变化 `227`。ARM64 模拟器设置页、客户端总门禁、APK 构建和安装通过。
- 短码配对不得把 CapabilityGrant、PairingTicket bundle、DeviceIdentity private key 或 terminal 信息交给 Controller、Hub、Relay 或 Web。新增跨边界消息仍必须 proto-first；Cloud 只允许转发 signaling，完整签名 bundle 只能在 App/客户端与 owning daemon 的端到端配对链路中取得。
- 产品正式名称为 `Muxvia`，主域名为 `muxvia.com`，GitHub 组织为 `github.com/muxvia`。首发前必须完成无兼容层的全量发布身份迁移：`Muxvia`、`Muxvia Cloud`、`github.com/muxvia/muxvia`、CLI `muxvia`、Android `com.muxvia.app`、URI `muxvia://`、npm scope `@muxvia`、Proto namespace `muxvia.*`、C ABI `muxvia_*`、环境变量 `MUXVIA_*`。
- `BRAND001-BRAND005` 已完成 Muxvia 全量发布身份迁移、活动残留收口、standard/devcloud APK、真实 ARM64 Direct terminal UI smoke 和双 Agent 审查；证据见 `docs/development/muxvia-brand005-e2e.md`。品牌迁移未改变 Proto/API/Core、Endpoint/Route/session、安全或目录 ownership。
- 用户已明确要求在继续 `CLOUDP007` 前完成 Controller 持久化迁移。`PG001-PG003` 已完成领域 Store 契约、PostgreSQL schema/pgx adapter、Controller/devcloud/test 全量切换和 SQLite 删除；`make test-private` 与独立双 Edge 进程门禁通过。`PG004` 已取得真实 Supabase Session pooler DSN，并从 `155.94.155.192` 验证 IPv4、TLS、账号和只读查询成功。用户要求先完成公网可用装配，再补 R2；因此当前顺序固定为 Supabase 独立 schema + `155` Controller/Edge A + `114` Edge B + HTTPS/DNS + bootstrap 账号纵向验收，服务可用后立即补 R2 上传与独立恢复验收。不得用本地 PostgreSQL 冒充云端验收，也不得把 bootstrap staging 宣称为商业生产发布。
- Controller 正式持久化契约固定为标准 PostgreSQL；首个生产托管实例使用 Supabase PostgreSQL，但代码、schema、migration、连接和事务不得依赖 Supabase Auth、Realtime、PostgREST、Edge Functions 或其它专有业务能力。Supabase 只是 PostgreSQL 托管商，不是账号、session、Subscription、Hub assignment、CommandOutbox、Relay quota 或 UsageLedger 的领域 owner。
- PostgreSQL/Supabase 迁移的事务矩阵、伪代码、删除项和 staging 验收见 `docs/remote-platform/postgresql-supabase-migration.md`。
- Supabase 连接方式、secret、双 Edge staging、R2 备份和恢复步骤见 `docs/remote-platform/supabase-staging-runbook.md`。
- `RTC001-RTC010` 已完成统一 WebRTC Route、Android JNI、Direct/SSH/Cloud、Endpoint、文件、生命周期、弱网和最终 APK E2E；证据见 `docs/remote-platform/rtc010-android-final-e2e.md`。
- Cloud 产品真值见 `docs/remote-platform/cloud-product-spec.md`；多 Hub assignment、纯内存 Hub、daemon topology、CommandOutbox 和 Web 管理真值见 `docs/remote-platform/multi-hub-control-topology-spec.md`；具体 Proto、package、存储、伪代码、迁移删除项和测试矩阵见 `docs/remote-platform/multi-hub-technical-plan.md`。
- 多 Hub 的 assignment、topology、安全和 runtime 核心规划此前已经过四维度 reviewer 复审；最新部署决策进一步收敛为两个二进制：`muxvia-cloud-controller` 组合 Control Plane + Web Controller，`muxvia-cloud-edge` 组合 Hub + Relay，但四个领域 owner、身份、generation、状态机和存储边界不合并。
- `HUB001` 已完成 Edge/Hub/Relay control、topology、management Proto，双 TypeScript consumer、descriptor/compatibility 门禁和 daemon ManagedPeerSession registry 纯模型。
- `CLOUDP001` 已完成 PlanCapability、versioned Subscription、Entitlement 与 Hub policy 的统一能力模型；catalog 不再按套餐名分支，devcloud 不再按有效期猜 Relay 配额。
- `HUB002` 已完成 Controller/Edge 双 composition、一个 Controller + 两个 Edge 独立进程、真实 Proto Hub control、strict assignment epoch fencing、纯内存 full/delta/reconciliation、Hub public signaling、无 snapshot 重启和 Relay usage outbox 恢复。
- `HUB003` 已完成 daemon auth + protocol Hello 后 READY、完整 peer teardown 后 CLOSED、单 reporter full inventory、Hub 内存 topology、Controller assignment/ownership 校验与 PostgreSQL replacement，并删除 Web 在线状态直写。
- `CLOUDP003` 已完成持久 Entitlement 到 signed per-Hub policy、周期 fresh full、Hub 内存 managed P2P reservation、稳定拒绝分类及 signaling 到 daemon runtime inventory 的生命周期转交。
- `HUB005` 已完成 enrollment control key、daemon 持久控制回执、opaque terminal access inventory、Web 查询、Controller 签名 revoke、AccessStore 原子撤销和关联 session close 的真实闭环。
- `CLOUDP004` 已完成 Proto-first Relay 周期额度、PostgreSQL 原子 reservation、账号/设备并发、region 与 per-lease clamp、refresh 复核、取消和延迟过期释放，并接通 Controller-Edge-Companion caller-specific TURN credential 纵向链路。
- `CLOUDP005` 已完成 Proto-first signed usage record、独立 Relay control key、Edge durable outbox/pump、Controller 双签名验证、PostgreSQL event journal/sequence 幂等、period/session 聚合、reservation settlement 与重启补报。
- `HUB006` 已完成独立 Relay control identity/generation/stream、lease/session allocation remote close、final usage drain、Controller settlement 与 CommandOutbox PARTIAL/APPLIED 收口。
- `CLOUDP006` 已完成 Controller 同 composition Web build、generated Proto JSON 用户账号中心与 operator 工作台、账号隔离、角色/CSRF/近期认证、套餐/usage/device/topology/command/fleet 页面、development 凭据和旧 Web DTO/API 删除。
- `HUB007` 已完成双 Edge 控制面 E2E：assignment migration、Edge restart、Controller outage、HubControl network outage、inventory full replacement、stale/replay fencing、命令链路、Playwright Operator UI 和隐私扫描均通过；证据见 `docs/remote-platform/hub007-control-plane-e2e.md`，架构与代码 reviewer 均 PASS。
- `CLOUDP007` 已恢复 Web 已登录账号创建短时二维码、Android App 扫码认领、Web 展示手机元数据并明确批准、App 单次兑换/轮换 Cloud session 的真实 UI 闭环；移动端 refresh 复用 commerce + PostgreSQL 持久 session owner，Controller 重启后可继续轮换，HubID/URL/region 由显式 development directory 原子绑定。ARM64/API 35 模拟器已看到同账号 Cloud daemon 列表，并完成 Direct terminal 的 HOME 后台恢复与飞行模式网络切换回归：旧 binding generation 的迟到结果不再覆盖新 registry，一次前台恢复只创建一个 bridge，新 session 成功后不残留旧 `Go binding backend is closed` 错误，恢复后终端输入输出继续成功；该生命周期子范围架构与代码 reviewer 均 PASS。完整 managed P2P/Relay terminal/file、quota、suspend、topology/management command、真实锁屏恢复与最终切片双审查仍未完成，因此切片继续保持进行中。
- `PG004` 公网 bootstrap staging 已部署：155 使用 systemd 运行 Controller + US West Edge，114 使用 systemd 运行 China East Edge，真实 Supabase `muxvia_staging` schema、Cloudflare DNS、Let's Encrypt、Web 登录/二维码、operator 双 fleet 和三进程重启恢复均通过。Android ARM64/API 35 公网 HTTPS profile 已完成 Web 扫码批准、Go-owned managed P2P、Relay 双端租约、Relay terminal attach/input 和远端文件浏览；daemon enrollment session 已补 refresh token 轮换。多用户 enrollment 子任务已完成并部署：bootstrap 启动配置、credentials 和 Companion manifest 不再包含静态 daemon code；任意已登录账号可在 Web Devices 页创建 Controller 内存持有、十分钟、128-bit、单次、账号与 Hub 绑定 flow，daemon 提交公开 metadata、device ID 与 DeviceIdentity public key 后必须等待 Web 核对批准，再以 DeviceIdentity proof 单次完成。手机二维码与手工输入的 `MXA-...` 登录码指向同一 activation flow并保持 Web 批准。两类短期 flow 均使用 TTL 到期队列和百万容量上限，不进入 PostgreSQL；Controller 重启使 pending flow 失效，已完成后的账号、设备归属、assignment 和 session 继续由 PostgreSQL 持久化。R2 独立恢复、文件上传/下载内容校验、网络切换后 workspace 自动重建、Android account session 自动续期和 Edge 重启后的 daemon Presence 自动恢复仍未通过，因此 PG004 继续保持进行中；证据见 `docs/remote-platform/pg004-public-bootstrap-deployment.md`。
- 多 Hub 基础和产品能力存在交叉依赖，必须按本文件交错推进，不能先写完所有 Hub 再补套餐，也不能继续在单进程 devcloud 上堆硬编码。
- development 必须走完整账号、交易、Subscription、Entitlement、managed P2P/Relay、周期 quota、usage、topology 和管理链路；外部 provider 可以使用显式测试实现。
- Web/WASM terminal 产品、iOS/Desktop GUI、多区域数据库、Relay Mesh、真实支付 provider 和复杂计费平台继续延后。当前 PostgreSQL 迁移只覆盖单区域单写 Controller，不建设多区域复制、分布式锁、读写分离或数据库抽象平台。

## 架构链路

```text
PlanCatalog -> Subscription -> Entitlement
                         |
                         v
muxvia-cloud-controller
  Control Plane + Web/API + persistent store
                         |
              HubControl | RelayControl
                         v
muxvia-cloud-edge × N
  memory-only Hub + Relay runtime/usage outbox
                         |
                    daemon Presence
                         |
                 daemon PeerSession owner
                         |
              P2P DataChannel or Edge Relay
```

- P2P 数据不经过 Hub；Web 分开显示 control owner Hub 和 observed data path。
- Hub 不落盘 policy、Presence、signaling、topology 或 command dedupe。
- Relay 与 Hub 同进程部署，但只允许 Relay usage outbox 落盘；Hub/Relay 不共享业务 map、identity、generation 或 command state。
- daemon Go runtime 是 authenticated managed PeerSession 的 owner。
- Cloud command 只能减少服务或权限，不能扩大 daemon CapabilityGrant。
- Local、Direct、SSH、terminal 和 file 不依赖 Cloud 套餐或 Hub。

## 当前允许范围

- 主动范围：`AGENTS.md`、`workflow.md`、`docs/remote-platform/`、`proto/cloudpb/`、`private/cloud/`（包括后续 `controller/`、`edge/` composition root）、`remote/daemon/`、`remote/webrtc/` 和当前切片测试。
- Client/Companion 联动：`private/cloud/companion/`、`client/adapter/managed/`、`client/runtime/`、`client/binding/`、`cmd/muxvia/`，只在当前消息链路需要时修改。
- Android/Web 管理联动：`clients/mobile/android/`、`clients/ui/`、`private/cloud/web-controller/`，只在对应登录、topology、management 或 E2E 切片修改。
- 受限联动：`core/` 只允许为 daemon-owned PeerSession lifecycle 和 deny-only AccessStore command 增加最小 port；`scripts/`、`Makefile`、`go.work*` 只用于测试装配。
- 冻结：Web/WASM terminal consumer、iOS/Desktop GUI、插件、KCP/QUIC、Relay Mesh、多区域数据库、开源发布工程和 archive。

## 任务队列

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| BRAND001 | 已完成 | Muxvia 品牌与发布身份基线 | AGENTS/workflow 冻结正式名称、域名、GitHub module、CLI、Android、URI、npm、Proto、C ABI、环境变量、二进制和历史排除边界；全仓迁移矩阵明确 |
| BRAND002 | 已完成 | Proto、Go module 与生成代码迁移 | module/import/go_package/proto package/type URL 全部迁移到 `github.com/muxvia/muxvia` 与 `muxvia.*`，字段编号和语义不变；generated、binding generated、descriptor/round-trip、`make test`、`make test-private`、`make test-clients` 通过 |
| BRAND003 | 已完成 | CLI、C ABI 与 runtime identity 迁移 | `muxvia` CLI、`muxvia://`、`MUXVIA_*`、C ABI/JNI/native library、WASM export、配置/socket/log/state 路径和 Cloud 二进制完成破坏性迁移；ARM64/x86_64 native build、`make test`、`make test-private`、Node test/typecheck/build、generated 与 doctor 通过 |
| BRAND004 | 已完成 | Android、npm/UI、Cloud 与活动文档迁移 | `com.muxvia.app`、`@muxvia/*`、Muxvia/Muxvia Cloud 文案、下载目录、Web/Android origin、法律/发布资产和活动文档完成迁移；Node、Go、private Cloud、doctor、standard/devcloud APK 与边界校验通过，历史目录保持只读 |
| BRAND005 | 已完成 | 全仓残留与发布候选验收 | 活动源码/配置/测试/文档无旧品牌残留，允许项仅限明确历史引用；Go/Node/Android/Cloud 全量门禁、真实 ARM64 APK Direct terminal smoke、双 Agent 审查通过 |
| CBASE001 | 已完成 | Cloud 产品文档基线 | `cloud-product-spec.md` 与 AGENTS/PRD/README 一致 |
| HBASE001 | 已完成 | 多 Hub 控制面设计基线 | `multi-hub-control-topology-spec.md` 经分布式、安全、运行时、产品/API 四角度审核收口；旧 Hub WAL 目标降为历史 |
| TBASE001 | 已完成 | 多 Hub 技术实施规划 | `multi-hub-technical-plan.md` 明确 Proto、代码 owner、控制 transport、PostgreSQL 事务、daemon lifecycle、切片修改范围、旧路径删除和 E2E 证据；四个独立 reviewer 均 PASS |
| DBASE001 | 已完成 | Controller/Edge 双二进制部署基线 | 稳定架构与技术规划冻结 `muxvia-cloud-controller = Control Plane + Web Controller`、`muxvia-cloud-edge = Hub + Relay`；只合并 composition/deployment，不合并领域真值或安全身份 |
| HUB001 | 已完成 | Proto 与 daemon registry contract | Edge deployment metadata、独立 Hub/Relay identity/control generation、HubAssignment、per-Hub revision、Presence availability/freshness、PeerSession/access inventory、parent/child CommandOutbox/result、Web/Operator API 全部 proto-first；daemon registry port/harness 证明 revision、READY/CLOSED、Presence replacement 和精确 close |
| CLOUDP001 | 已完成 | PlanCapability 与 Entitlement | `cloud_product.proto` 定义 versioned catalog/Subscription/Entitlement 与统一 PlanCapability；catalog、commerce、Control Plane normalization、signed Hub policy 共用 generated capability；删除 plan/有效期 quota 推断；P2P-only、P2P+Relay、suspended fixture 与 generated/private/race/Web 门禁通过 |
| HUB002 | 已完成 | Controller/Edge composition、assignment 与纯内存 Hub 同步 | 两个 composition root、一个 Controller + 两个 Edge 独立进程、Hub identity/generation、assignment epoch admission/fencing、full/delta/reconciliation、Hub public signaling、无 snapshot 重启与 Relay-only usage outbox 恢复均有真实 socket/process harness；generated、public/private、race、Web 与 layout 门禁通过 |
| HUB003 | 已完成 | daemon ManagedPeerSession 与 topology | Hello 后 READY、完整关闭后 CLOSED；统一 registry revision、上行 runtime report、inventory replacement、Hub topology snapshot、CP 账号/epoch校验和 unknown/stale projection；generated/public/private/race/Web/layout 与双 Edge process harness 通过 |
| CLOUDP002 | 已完成 | 账号、Subscription 与交易 | Proto Price/PaymentAttempt/账号/session/订单/event/transition；PostgreSQL 原子 journal、revision fencing、精确 replay 与重启；Controller Cookie/CSRF Proto JSON；旧 Web 账号交易真值和 direct Entitlement 写入口已删除；generated/public/private/race/Web/双 Edge/doctor 门禁通过 |
| CLOUDP003 | 已完成 | managed P2P 准入与并发 | 持久 Entitlement/auth revision 生成 signed per-Hub policy并在 TTL 内刷新；Hub 原子校验 P2P enabled、ownership、revoke、auth epoch、assignment 与账号并发；reservation 从 signaling 转交 daemon 完整 PeerSession inventory，空 replacement/pending TTL/精确 fence 释放且新 Hub 可重建，Relay-only 保持认证但不占 P2P 名额；public/private/race/客户端生成/双 Edge/doctor 门禁通过 |
| HUB004 | 已完成 | CommandOutbox 与运行时控制 | PostgreSQL parent/child/result journal 与 device revoke 原子事务；Web Proto JSON 创建/查询；KickPresence、daemon revoke 单 Hub、client revoke 跨 Hub fan-out、签名 CloseManagedPeerSession；generation/epoch/Presence/runtime/session/replay/expiry fencing、parent 聚合和独立 daemon ack；真实 Controller-Edge-Presence HTTP harness、public/private/race/client/双 Edge/doctor 门禁通过 |
| HUB005 | 已完成 | daemon deny-only grant revoke | enrollment 返回受窗口约束的 daemon control key；daemon 持久化 enrollment 与精确 replay receipt；双 revision runtime report 上报无 secret 的 opaque access inventory；Web 账号隔离查询并创建 fenced terminal revoke；Controller 签名、Hub 精确转发、daemon AccessStore 原子撤销并关闭关联 session，结果持久回传；真实 Controller-Edge-daemon-Web harness 与 public/private/race/generated/doctor 门禁通过 |
| CLOUDP004 | 已完成 | Relay 周期 quota 与 reservation | generated RelayQuotaPeriod/RelayLeaseReservation/Edge reservation contract；PostgreSQL 原子维护 used/reserved/remaining、精确 replay、账号/设备并发、region、单 lease clamp、取消和 expiry+report-grace 释放；Hub 短期 relay intent 让 client/daemon 共用同一 reservation 并近到期轮换；Controller 重新验证 edge principal、ownership、assignment 与 Entitlement，Edge 离线验签并派生隔离 TURN credential；真实 Controller-Edge-Presence harness、public/private/race/client/双 Edge/doctor 门禁通过 |
| CLOUDP005 | 已完成 | durable UsageLedger 与 settlement | generated RelayUsageEvent/Record/Report/Ack 与 RelayUsageAggregate；Edge 使用稳定独立 Relay control key，authority 先写 durable outbox 再清 pending bytes，at-least-once pump 仅在 Controller 事务提交后 ack；Controller 重验 signed lease 和 Relay event，PostgreSQL 原子提交 event journal、严格 sequence、精确 replay、period/session aggregate、reservation used/release；same-second shutdown、Controller outage、Edge/Controller/PostgreSQL 重启和真实 Pion relay-only DataChannel usage E2E 通过；public/private/race/client/双 Edge/doctor 门禁通过 |
| HUB006 | 已完成 | Edge 内 Relay allocation remote revoke | generated Relay challenge/report/settlement contract；Controller 与 Edge 使用独立 Relay identity、generation、sender sequence 和 result cursor；reservation 持久绑定 account/Hub/Relay/route，planner/dispatcher 不经过 Hub command；Relay 按 lease/session 精确关闭真实 socket，零字节或有流量都只生成一次 final usage，Edge 串行等待 Controller ack/PostgreSQL release 后回传 RelayCommandResult；单 child PARTIAL、真实 Pion remote close、public/private/race/client/双 Edge/doctor 门禁通过 |
| CLOUDP006 | 已完成 | Controller 用户账号中心与运营管理面 | Web/API 与 Control Plane 同一 Controller composition；generated Proto JSON 用户/运营页面覆盖套餐、usage、device、topology、command、订单、审计和 fleet；账号隔离、readonly/admin、CSRF、五分钟近期认证、0600 development 凭据、旧 DTO/API 删除；public/private/race/client/双 Edge/doctor/Web build 门禁通过 |
| HUB007 | 已完成 | 双 Edge 控制面 E2E | 一个 Controller + 两个 Edge 独立进程、assignment migration、Controller outage、Edge restart、inventory recovery、四类 command、P2P/Relay close 和隐私扫描；双 Agent 审查 |
| PG001 | 已完成 | PostgreSQL Store 契约与 schema 基线 | 已复用并补齐按领域划分的 Store/transaction port，Controller runtime/Relay handler 不再依赖 concrete PostgreSQL；建立 versioned PostgreSQL initial migration、静态 schema 门禁和事务/迁移设计文档；Control Plane 与 Controller 全模块测试通过 |
| PG002 | 已完成 | PostgreSQL/pgx 持久化实现 | `pgx v5` adapter、advisory-lock versioned migration、PostgreSQL placeholder/error/transaction 实现完成；真实 PostgreSQL 17 上通过 CommandOutbox、assignment、commerce rollback/restart、Relay quota/usage restart、并发 generation 与十轮并发 reservation contract harness |
| PG003 | 已完成 | Controller 全量切换与 SQLite 删除 | Controller 使用 secret `postgres_dsn` 打开 pgx Store，manifest 只公开 `database_engine=postgresql`；devcloud 为 artifact 创建隔离 schema；全部 Cloud 测试改用 PostgreSQL fixture；SQLite package/driver/config/fallback 已删除，`make test-private` 与双 Edge 门禁通过 |
| UX001 | 已完成 | 英文/简体中文与产品文案基线 | Web Controller 登录/账号/设备/激活流程与 Android 首页/设备/配对/设置主流程进入统一 locale；默认跟随系统语言并允许持久切换；稳定错误码、日期、数字和状态使用本地化 projection；App 143 个、Web 365 个英文/中文键一致，UI/Mobile/Web 测试与构建、Android ARM64 中文 smoke 通过；terminal/file 剩余文案在 APPUX001 收口 |
| QR001 | 已完成 | 现有二维码输出可用性 | CLI 已增加 `--text` 与 owner-only `--qr-file`，默认渲染前检查完整终端行列且空间不足零输出；Web activation QR 保持正方形、2-module quiet zone、响应式尺寸和同级手工码；CLI 全包、App 138 项、Mobile 27 项、Web typecheck/build 与实际 1024x1024 PNG 检查通过 |
| APPFIX001 | 已完成 | 正式 App 测试信息与 loading 修正 | 已删除移动 App 的 `114.66.58.243` fallback，native Cloud 设置页不再展示 Web Control/Hub 地址；loading 外框固定、内部指示器旋转并遵守 reduced-motion；外框/内部两帧像素变化为 `0/227`，ARM64 设置页、UI 138 项、Mobile 28 项、构建和 APK 安装通过 |
| APPFIX002 | 已完成 | 扫码登录 Session 稳定性 | Hub 设备目录同步失败不再清除 Android Keystore 账号 Session；账号登录真值仅由 native session owner、显式 logout 和 Controller refresh 失效决定；线上扫码批准后账号/设备 UI、同步竞态、强制停止与进程重启恢复、ARM64 APK 和 crash scan 通过 |
| CLOUDAUTH001 | 已完成 | Hub 离线认证与本地授权解耦 | Controller 签发的 EdgeAccess token 由 Hub 公钥离线完成身份认证；新 client 尚未进入 policy projection 时设备目录和 managed P2P 均可继续；projection 只判断账号 revoke/auth epoch、套餐、target ownership、并发和 Relay 额度；无效 token、policy lag、明确撤销、entitlement、quota 错误分类稳定；Hub 不为普通登录回查 Controller；Go/Private/Client/Android、双 Edge 和线上 ARM64 登录/重启门禁通过 |
| QR002 | 待开始 | Proto-first daemon 短码配对 | proto 定义 daemon-owned pairing claim create/claim；128-bit、十分钟、单次、内存持有并绑定 DeviceIdentity/scope；QR 不再承载完整 bundle，目标不高于 QR Version 10；无摄像头可输入短码；Cloud 不接触 bundle/grant；Direct 与 Cloud managed pairing E2E 通过 |
| WEBUX001 | 待开始 | Web Controller 账号与设备添加重构 | 普通导航收敛为概览/设备/套餐/账号，高级 topology/command 降级；单一“添加设备”向导覆盖手机与 daemon 的创建、等待、核对、批准和完成；危险操作按具体动作近期认证；友好名称优先、技术身份进入详情；桌面/移动响应式验收通过 |
| APPUX001 | 待开始 | Android 首次使用与设备信息架构 | 未登录首屏优先登录/添加本地设备；扫码与短码同级；Machines 页收敛主操作与状态层级，友好名称优先、技术 ID 进入详情；完成 terminal/file 剩余用户文案迁移；中英文、大字体、竖横屏、无摄像头流程通过真实 ARM64 模拟器验收 |
| UXE2E001 | 待开始 | Web/App 产品体验 E2E | Web 360/768/1280/1440 与 Android ARM64 覆盖中英文、150% 字体/缩放、注册登录、手机 activation、daemon enrollment、短码/扫码 pairing、terminal 输入输出和文件操作；无裁切、重叠、不可达操作或 raw internal state；双 Agent 审查 |
| PG004 | 待继续 | Supabase staging、公网 bootstrap 装配与备份恢复验收 | Supabase Session pooler、systemd Controller + 双 Edge、HTTPS/DNS、真实 schema、Web/Android 扫码、daemon enrollment refresh、managed P2P、Relay 租约/terminal 与远端文件浏览已通过；多用户 Web 创建 Controller 内存 TTL/容量受限的 128-bit 单次 daemon enrollment flow、Web 核对批准、DeviceIdentity proof、固定 bootstrap code 删除、重启使 pending flow 失效和 Android 手工输入同一 mobile code 已通过公网验收。UXE2E001 后恢复 R2 加密上传/独立恢复、Android account refresh、Edge 重启 Presence 恢复、网络切换 workspace 重建及文件上传/下载内容校验。测试支付必须明确标记 staging，不得宣称正式商业生产 |
| CLOUDP007 | 待开始 | Development 全产品 E2E | PostgreSQL 迁移后从现有进度恢复；Web UI 注册/交易/管理 + Android ARM64 真实 APK P2P/Relay terminal/file、quota、suspend、topology、命令、重启恢复、Direct/SSH 回归；双 Agent 审查 |
| CLOUDP008 | 延后 | Production Cloud 装配与发布 | 仅 PG004/CLOUDP007 完成后启动；HTTPS、Companion 签名、Android production origin、真实 provider；正式存储已由 PG001-PG004 完成，不重复建设第二套数据库路径 |
| WEB001 | 延后 | Web/WASM terminal 产品 | 仅用户明确恢复后启动 |

## 关键准入

### UX001-UXE2E001

- 不建立通用设计系统、跨产品 CMS、服务端语言偏好、远程翻译平台或与当前页面无关的组件库；locale 只属于客户端表现层，账号和 Control Plane 不拥有语言真值。
- 首期 locale 固定为 `en` 与 `zh-CN`；所有新增或修改的用户可见文本必须使用 locale key，技术日志、Proto enum 名和测试 fixture 不得直接进入 UI。
- Web Controller 与 App 可以保留各自页面文案 owner，但核心产品术语必须共享同一词汇表：Device/设备、Daemon/守护进程、Pair/配对、Direct/直连、Relay/中转、Terminal/终端、File/文件。
- i18n 改造不得改变 Proto/API/Core、Endpoint/Route/session generation、授权或网络行为；错误本地化只能映射稳定错误 code，不能按服务端英文 message 分支。
- `QR001` 不改变 pairing 安全模型；`QR002` 修改配对协议时固定执行 proto -> generated -> compatibility/security harness -> daemon/API Layer -> Go Client Engine/binding -> App/CLI。
- pairing claim 只能由 owning daemon 在内存中持有；过期、重复 claim、错误 daemon、错误 scope、错误 DeviceIdentity binding 和迟到 callback 必须显式拒绝。Cloud 不能存储、解析或签发完整 pairing bundle。
- 每个 Web/App 切片都必须至少覆盖 360px 手机宽度和 1280px 桌面宽度；最终切片使用真实 ARM64 Android 模拟器，并从 UI 发起登录、添加设备、配对和 terminal/file 用户动作。

### BRAND001-BRAND005

- 正式标识固定为 `Muxvia`、`Muxvia Cloud`、`github.com/muxvia/muxvia`、`muxvia`、`com.muxvia.app`、`muxvia://`、`@muxvia`、`muxvia.*`、`muxvia_*`、`MUXVIA_*`。
- 当前尚未公开发布，不保留旧 CLI、URI、applicationId、环境变量、配置目录、socket、C ABI、Proto namespace、npm scope、Go import 或运行数据兼容层；开发数据允许重置。
- Proto 字段号、枚举值、消息语义、API Layer/Core 边界、Endpoint/Route/session generation 与安全模型不得因改名变化。
- `private/archive/`、`docs/history/` 和 Git 历史保留旧名称作为历史事实，不进入活动残留门禁；其它活动代码、配置、测试、生成代码、法律文本和产品文档必须完成迁移。
- 迁移完成后运行旧品牌残留扫描、generated/descriptor、全部 Go workspace、Node workspace、Cloud 双 Edge、Android standard/devcloud APK 和真实 ARM64 UI smoke；默认双 Agent 审查。

### HUB001

- 新 contract 位于 `proto/cloudpb/`，generated/descriptor/round-trip/unknown-field 通过。
- Edge deployment metadata 不得替代 Hub/Relay 独立 identity、sender role、control generation 和 sequence。
- 明确 `assignment_epoch`、`presence_session_id`、`session_incarnation`、`daemon_runtime_generation`、`registry_revision` 和 per-Hub `projection_revision`。
- Presence availability 与 freshness 分离；command authority/delivery/execution/effect 分离。
- Browser management query/command、pagination/filter/error 也必须 proto-first。
- parent/child command、`TerminalAccessInventorySnapshot` 和 `ListDaemonTerminalAccess` 必须进入 Proto；Web 不从 active session 猜 revoke reference。
- daemon registry fake harness 证明 READY/CLOSED/inventory 线性化和精确 close result。
- `git diff --check`。

### CLOUDP001

- 同一 PlanCapability 生成 Entitlement 和 Hub policy；禁止 `if plan == "pro"`、按 `validUntil` 猜能力和固定 quota。
- P2P-only、P2P+Relay、suspended fixture 通过。
- 受影响 private Cloud module tests；`git diff --check`。

### HUB002

- `muxvia-cloud-controller` 只组合 Control Plane、Web/API、store 和独立 listener；`muxvia-cloud-edge` 只组合 Hub、Relay 和 health/listener。
- 至少启动一个 Controller 与两个 Edge 独立进程；禁止继续把 Controller、Hub、Relay 全放在同一进程或用 direct pointer 连接网络边界。
- 两个 Hub identity 不能冒充；同 Hub ID 旧 control generation 被 fencing。
- 新 assignment 只有旧 Hub fence ack 或旧 lease expiry 后生效，禁止跨 Hub 双活。
- Hub 必须使用 timer 或等价生命周期机制在 assignment expiry 主动关闭旧 Presence/signaling，不能只等下一次请求触发检查。
- 每 Hub projection revision 连续；同 revision/digest reconciliation 幂等，gap/rollback/digest conflict fail closed。
- Hub 无 snapshot store；CP 不可用重启时 readiness=false。
- Edge 重启时 Hub 必须 full sync；Relay 只能恢复未确认 usage event，不能恢复 allocation/connection。
- 相关 race；`git diff --check`。

### HUB003

- daemon registry 在 auth + Hello 后 READY，资源全部结束后 CLOSED。
- inventory/event 共用 registry revision；旧 revision/epoch/presence/runtime generation 不覆盖当前 projection。
- CP 从持久 ownership/assignment 推导 account，不信任 Hub account 字段。
- online/offline/unknown 与 fresh/stale 场景测试。
- 相关 race；`git diff --check`。

### CLOUDP002-CLOUDP005

- 每个切片按 `cloud-product-spec.md` 的状态机、quota 和 usage 规则补 contract/harness。
- payment replay、Subscription transition、P2P concurrency、period reservation、signed usage、迟到/冲突/重启恢复必须分别证明。
- 不为真实支付、多区域或分布式 quota 扩大范围。

### HUB004-HUB006

- Kick 绑定精确 Presence；旧 command 不影响新 incarnation。
- persistent revoke commit 与 runtime enforcement 独立显示。
- client device revoke 根据当前 topology 对多个 Hub/session 生成 child command；一个 child 超时只使 parent PARTIAL，不回滚 authority revoke。
- P2P close 使用独立 daemon CommandResult，不用 topology CLOSED 冒充 ack。
- grant revoke 端到端验签、deny-only、daemon owner 执行。
- Relay close 必须精确关闭全部 lease allocation、drain usage 并 settlement；部分完成返回 PARTIAL。
- Hub 与 Relay 即使同 Edge 进程也必须通过独立 port/control owner 协作，禁止 Hub 直接修改 Relay allocation map。
- 错 Hub/epoch/auth/presence/session/replay/expiry 全部 fail closed。

### CLOUDP006

- 用户与运营权限矩阵、CSRF、近期重认证、账号隔离和稳定错误通过。
- Web 分开显示 signaling control relation 与 P2P/Relay data path。
- 超时只显示 UNKNOWN/STALE，不伪造 offline。
- UI 不接触 terminal/grant/payload。

### HUB007/CLOUDP007

- 使用独立进程的一个 Controller、至少两个 Edge、多个 daemon/client；每个 Edge 内 Hub 纯内存，Relay 只有 usage outbox 可持久化。
- Web 行为从真实 Web UI 发起；Android 行为从同一个最终 ARM64 APK UI 发起。
- 记录 APK SHA-256、AVD/ABI/API、Hub identity/assignment revision、command receipt 和 topology evidence。
- 覆盖 Controller outage、Edge restart、assignment migration、stale event、command replay、P2P/Relay close、grant revoke、quota、usage、Direct/SSH 回归、crash/secret scan。
- 架构 reviewer 与代码 reviewer 均 PASS。

### PG001-PG004

- PostgreSQL 是唯一正式数据库契约；Supabase 只提供托管 PostgreSQL，不接管产品身份、授权、交易、订阅、拓扑、命令、quota 或 usage 领域。
- `controller`、Web handler、领域 service 和 runtime 不得依赖 concrete PostgreSQL/PostgreSQL type；SQL、row mapping、transaction implementation 和 migration 只能位于持久化 adapter。
- 按领域定义最小 Store/transaction port，不建立通用 repository、ORM、query builder 或未来多数据库框架。
- 保持已有原子语义：payment event journal + transition、assignment fencing、CommandOutbox parent/child/result、Relay reservation、usage event sequence/aggregate/settlement 和 control ACK 必须在对应事务提交后生效。
- PostgreSQL adapter 使用标准 SQL 与 `pgx`；Supabase SDK、Auth、Realtime、PostgREST、浏览器直连数据库和 Edge Function 数据库代理禁止进入运行链路。
- 长驻 Controller 优先 TLS direct connection；仅在部署网络无 IPv6 时使用 Supavisor session mode，不使用 transaction mode。
- PG003 切换后删除 SQLite runtime、driver、schema、配置和 fallback。测试不得通过双写或 SQLite oracle 证明 PostgreSQL 正确性。
- PG004 至少使用一个真实 Controller、两个独立 Edge 和 PostgreSQL staging 实例，覆盖 Controller/Edge/数据库重启、assignment、command、commerce、quota、usage settlement 与备份恢复；`pg_dump` 备份必须异地保存并完成可验证恢复。
- 不在本轮实现多区域数据库、读副本路由、分布式锁、自动故障转移、零停机 schema 平台或供应商切换框架。

## 执行规则

1. 每轮先读取 `AGENTS.md`、`cloud-product-spec.md`、`multi-hub-control-topology-spec.md`、`multi-hub-technical-plan.md` 和本文件，再检查 `git status --short --branch`。
2. 只执行最早的 `进行中` 或 `待开始` 切片；当前固定依次完成 `UX001`、`QR001`、`APPFIX001`、`APPFIX002`、`CLOUDAUTH001`、`QR002`、`WEBUX001`、`APPUX001`、`UXE2E001`，再恢复 `PG004` 与 `CLOUDP007`；`待继续` 和 `延后` 不属于当前活动队列。
3. 待开始切片先标记 `进行中`，不得跨切片实现后续能力。
4. 新跨边界字段固定执行 `proto -> generated -> compatibility harness -> domain/runtime -> adapter -> UI/client`。
5. 先写最小真实 harness；不能用固定账号、直接写 store、手工改 projection 或 fake ack 冒充产品链路。
6. 不建设 Hub-to-Hub forwarding、多区域、真实支付、复杂优惠、Web terminal 或通用分布式平台。
7. 每个切片完成准入、更新状态并使用中文提交信息提交。
8. `HUB007`、`UXE2E001` 和 `CLOUDP007` 默认双 Agent 审查；其它切片仅用户明确要求时审查。
