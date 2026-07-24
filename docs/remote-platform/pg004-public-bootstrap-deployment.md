# PG004 公网 bootstrap staging 部署证据

## 结论

2026-07-21 至 2026-07-22 已完成一个真实 Supabase、一个 Controller 和两个独立 Edge 的公网 bootstrap staging。服务使用专用 `muxvia` 用户和 systemd 运行，没有 Muxvia Docker runtime。

该环境已经支持 Web 注册/登录、账号中心、移动端扫码批准、daemon enrollment、managed P2P、单节点 TURN Relay 和 Android Go/JNI 连接。Android 公网 HTTPS staging profile 已真实安装到 ARM64/API 35 模拟器，并在 ARM64/Android 16 实体手机完成 AUTO 跨 NAT terminal UI 验收；它不是 production profile，也没有改变正式构建的 fail-closed 默认值。

该环境仍不是正式商业生产：支付仍为测试 provider，R2 备份恢复尚未通过。Android account refresh、Edge 重启后的 daemon Presence 恢复、活跃 session 网络切换和完整 Android file E2E 已经完成真实公网验收。

`PG004-HUBSEL` 已于 2026-07-23 完成真实验收。仓库实现接通 Proto 候选/观测、Controller active/capacity 筛选与最终 assignment、Go Client Engine 16-worker health 探测、Companion daemon 动态 Hub directory 和 daemon refresh；提交 `dc9705802bc4987db4f17d5ef1f8284483f9fa22` 已推送到 `origin/master`，并部署到公网 Controller 与 US/CN 两台 Edge。真实 daemon enrollment 从两个独立 Edge 取得 health observation，最终由 Controller 唯一选择 US Hub；Supabase assignment epoch、返回目录和 EdgeAccess token audience 一致。实体 ARM64 Android App 又独立完成账号 activation、设备目录恢复、US Hub endpoint resolve 和双端 signaling，证明客户端消费的是 Controller assignment，而不是自行选择 Hub。

该结果只完成 `PG004-HUBSEL`，不把整个 `PG004` 标记完成。2026-07-24 已补齐最终 Android file E2E，R2 独立恢复仍是当前切片门禁。

当前公网 Controller 已把 daemon enrollment 收敛为内存持有的十分钟 128-bit 单次 flow：任意已登录账号创建 code，daemon 提交公开 metadata、DeviceIdentity public key 和 device ID，Web 核对后批准，CLI 再以 DeviceIdentity proof 完成。pending flow 不进入 PostgreSQL，Controller 重启后统一失效；完成后的设备归属、Hub assignment 和 session 继续持久化。手机 activation 使用相同的 Web 核对批准语义，二维码与手工输入的 `MXA-...` 登录码指向同一 flow。

2026-07-24 又把 daemon enrollment 的最终持久化收敛为单个 PostgreSQL 事务：事务内锁定并校验账号 auth revision，以旧 ownership/assignment 为 CAS 条件，同时提交新 ownership、唯一 assignment、旧设备 session 撤销、跨账号旧 topology 清理、新 refresh session 与审计；任一步失败全部回滚。Web 批准、DeviceIdentity proof、Hub 候选探测、EdgeAccess 与 refresh 明文生成仍只存在于内存，policy 只在事务提交后发布。完成响应在原 flow 有效期内按完整请求 digest 提供 delivery grace，HTTP 响应丢失不会再次轮换 session。Controller 单元/竞态测试、PostgreSQL 最后一步故障回滚、`make test-private` 和全仓 Go 回归通过；公网 Controller SHA-256 为 `736b33ede2d80a083537f9be0f4b6d2e22eb2ecaf9a167543fd8a966be1bccf4`，回滚资产位于 `/opt/muxvia/rollback/pre-atomic-enrollment-20260724-1350/`。

同日真实旧 daemon 迁移复测又发现：ownership 已撤销，但 24 小时旧 assignment 已过期时，Controller 会在进入上述事务前统一拒绝。修复后只有“已撤销、DeviceID/public key 连续、旧 ownership/assignment CAS 匹配”的跨账号迁移可以续签原 Hub；活跃 owner 和其它过期 assignment 仍 fail closed。新增 harness 明确把旧 assignment 设为过期并验证迁移后同 Hub、epoch 递增、lease 恢复；Controller 与 PostgreSQL 目标测试及 Controller race 通过。公网 Controller SHA-256 更新为 `c8d87ec06e30fb8fd3174ebeaa6f034c9b06e2788ffa327aa677603cdfbebd77`，回滚资产位于 `/opt/muxvia/rollback/pre-expired-assignment-enrollment-20260724-1425/`。本轮 `make test-private` 的领域包与 Controller 通过，但三个 devcloud E2E 因本机 `127.0.0.1:55432` PostgreSQL 密码认证环境故障未启动子进程；该环境失败未通过修改业务代码规避。

同日清空本地 device session 后，同账号 daemon 重新 enrollment 真实触发 `HUB_CANDIDATE_STALE`：候选 provider 把已达到 `MaxAssignments` 的 US Hub 过滤掉，而该 daemon 正是已有 US assignment 的占用者；completion 又禁止 enrollment 静默迁移到 CN Hub，导致所有可提交候选均失败。修复后 provider 只为持有该 existing assignment 的 daemon 保留自己的满载 Hub，最终校验也不把同一 assignment 的续签重复计入容量；首次注册和其他设备仍不能选择满载 Hub。provider/selection/controller race 测试通过，公网 Controller SHA-256 更新为 `384073c04186c546c5f3c5bd80b2bf155e74e28c6bb8faebc49b182e6a7bf61a`，服务和 `muxvia.com`/US Edge health 均恢复，回滚资产位于 `/opt/muxvia/rollback/pre-enrollment-existing-capacity-20260724-1655/`。

修复部署后，同一 Mac 在 Clash fake-IP/TUN 下继续真实触发 `NO_REACHABLE_HUB`：`us1.edge.muxvia.com` 最终返回 `204`，但首次 TLS/health 建连约 4.05 秒，超过 Go enrollment probe 原 3 秒预算；CN 的非标准 TLS 端口同时被当前 Clash 路径阻断，因而全部候选被误报不可达。候选继续最多 16 路并行，单 Hub/整个并行阶段预算调整为 8 秒；3.2 秒代理慢握手 harness 和 `client/adapter/managed` 回归通过，不改变 ICE、DataChannel 或普通连接 deadline。

同日又修复设备目录把 activation 事务误当设备实体的问题：daemon 继续由固定 DeviceIdentity 的 `device_id` 归并；Official App 改为持久安装级 `client_device_id`，不同 MXA code 只原子撤销旧 device session 并签发同一设备的新 session。logout 只删除账号 session、不删除安装身份。当前尚未正式发布，因此实现不读取或迁移旧随机 client ID，也不保留旧请求格式兼容。Controller 两次 daemon enrollment、两次手机 activation、唯一账号设备投影、旧 refresh token 失效、Proto descriptor、Android gateway 和 ARM64 模拟器冷启动/crash scan 均通过。公网 Controller SHA-256 为 `4bd14ac1190890f945b373515a80d07aee36f1dceea9a0930c261f5fdde15374`，公网 HTTPS ARM64 APK SHA-256 为 `3c65aab0fd756d2fb04cb472d6a2f11b9f39fc65c5b8af0d3fbe3d18eefe56f2`；回滚资产位于 `/opt/muxvia/rollback/pre-stable-device-identity-20260724-1743/`。

## 部署拓扑

| 位置 | 服务 | 公网入口 |
| --- | --- | --- |
| `155.94.155.192` | `muxvia-cloud-controller` | `https://muxvia.com`、`https://operator.muxvia.com`、`https://control.muxvia.com` |
| `155.94.155.192` | `muxvia-cloud-edge` / US West | `https://us1.edge.muxvia.com`、`turn:155.94.155.192:41003?transport=udp`、`turn:155.94.155.192:41003?transport=tcp` |
| `114.66.58.243` | `muxvia-cloud-edge` / China East | `https://cn1.edge.muxvia.com:41102`、`turn:114.66.58.243:41003?transport=udp`、`turn:114.66.58.243:41003?transport=tcp` |
| Supabase Singapore | Controller PostgreSQL | IPv4 Session pooler `:5432`、TLS、独立 `muxvia_staging` schema |

- `muxvia.com`、`www.muxvia.com`、`operator.muxvia.com` 使用 Cloudflare Proxy。
- `control.muxvia.com`、`us1.edge.muxvia.com`、`cn1.edge.muxvia.com` 使用 DNS-only。
- Controller public/internal/operator 和 Edge Hub/health listener 只监听 host loopback；Nginx 负责 TLS 和 SNI。
- 两个 Relay 都在同一个公开端口直接监听 `41003/udp` 与 `41003/tcp`，不经过 Cloudflare。deployment manifest 的 `relay_url` 仍是 UDP primary；caller-specific lease 返回同一 credential 下按 UDP、TCP 排序的两个 URL。

## systemd

155：

```text
muxvia-cloud-controller.service
muxvia-cloud-edge.service
```

114：

```text
muxvia-cloud-edge.service
```

Controller 与 Edge 使用独立进程、配置、identity、state 目录和 restart policy。配置位于 `/opt/muxvia/config/`，运行状态位于 `/var/lib/muxvia/`；secret 文件保持 `0600`。

## 验收证据

- 从 155 到 Supabase Session pooler 的 IPv4、TCP、TLS、账号和只读 SQL 验证通过。
- Controller public/operator health 返回 `204`。
- US West 和 China East Edge public health 返回 `204`，TLS 验证通过。
- bootstrap 账号通过真实 Web UI 登录，账号、Subscription 和 Entitlement 在 Controller 重启后保持。
- Web UI 在 Devices 页创建二维码；浏览器读到的二维码自然尺寸为 `256 x 256`。
- operator UI 显示 bootstrap 账号以及 US West、China East 两个 fleet item，二者均为 `FRESH`、Hub ready、Relay ready。
- Controller 与两台 Edge 依次重启后重新接入；最终两个 Edge 都达到 Hub generation `3`、Relay generation `3`、projection revision `6`。
- 155 上 Controller 常驻内存约 3.5 MB，Edge 约 6.9 MB；114 Edge 约 7.4 MB。
- Nginx 配置测试、外部 HTTPS、静态资源 immutable cache 和 systemd restart 均通过。
- 2026-07-23 部署来源为 `dc9705802bc4987db4f17d5ef1f8284483f9fa22`；部署 bundle SHA-256 为 `1eefa96b07c8e4e7391405c34cdb411aa3c55fce0d0ef3fc3166385e3c673893`，Controller SHA-256 为 `ec1ed5b66e272d2cf666649b95922c438c4c318df85d9e0fef4896a950e1f401`，US/CN Edge SHA-256 均为 `37e7a0b645fedfd5cabc0b75e3dbc1eb44be503ec021b2de3cfae2f9116a99c3`。三台服务滚动重启后 health 均为 `204`；回滚资产保存在两台服务器的 `/opt/muxvia/rollback/pre-dc970580-20260723/`。
- 公网 Web 创建并批准真实 daemon enrollment 后，Controller 返回 US West 与 China East 两个 active/capacity 候选，Go Client Engine 对两个 HTTPS health endpoint 形成 observation，Controller 最终选择 `hub-us-sjc-1`。Supabase 中该 daemon 的 `HubAssignment` 为同一 Hub、epoch `1`；enrollment session 的 Hub URL/region 与 EdgeAccess token audience 均指向该 assignment。daemon 随后在 US Hub 建立持续 Presence。
- 本机使用同一 Supabase staging 运行一个 Controller + 两个独立 Edge 的 devcloud enrollment harness；托管数据库首次建隔离 schema、迁移和三进程启动实测需要约 24 秒，因此 harness 的 manifest 上限从 15 秒调整为 45 秒。测试结束后只清理本轮新建的 `muxvia_dev_*` 隔离 schema，不触碰 `muxvia_staging`。
- 实体设备 `Xiaomi 24129PN74C`、ARM64、Android 16/API 36 安装 `app-arm64-v8a-debug.apk`，APK 与设备 `base.apk` SHA-256 均为 `315b308629145d2d029fc21346198a2cf93170d6dc85f3a7ca508a77b2b74f58`。App UI 完成真实 `MXA` activation、Web 批准、账号和四台设备目录展示；强制停止并重启后账号恢复。随后从 App UI 提交 daemon pairing，Nginx 证据显示客户端请求 `https://us1.edge.muxvia.com/v1/endpoints/resolve` 和 `/v1/signaling/create`，daemon 在同一 US Hub 完成 `/v1/signaling/complete`。本轮 logcat 未发现 Java/native crash。
- Companion `staging-public-https` manifest 只接受 canonical HTTPS Controller/Hub origin；Android `muxviaPublicHTTPSStaging` 构建从 Controller 签发的 `AccountSession.hub_url` 获取实际 Hub，不再把编译期 Hub 地址作为登录后的连接真值。
- daemon 通过公网 Companion 完成 enrollment，Controller 同时签发短期 daemon access token 与可轮换 refresh token；daemon refresh harness 覆盖单次轮换、旧 token replay 拒绝、ownership/auth revision/Hub assignment 复核。
- Web UI 创建移动端激活二维码，Android App 通过真实扫码输入完成认领，Web 明确批准后 App 获得账号 session；APK 重装后账号与 Endpoint 安全状态仍可恢复。
- 公网 Web/API 创建 `MXA-...` 后，ARM64/API 35 模拟器未使用摄像头，直接在 App 设置页输入同一码；Web 投影显示 `unknown Android SDK built for arm64`、`android`、`1.0`，批准后 flow 被单次消费，App 返回 Machines 并显示 `bootstrap` 账号。提交前的无效码得到显式错误，logcat 未发现 Java/native crash。
- 公网 Web/API 创建 `MXD-...` 后，development bundled CLI 提交本机 `RedmiBook.local`、`darwin/arm64`、稳定 DeviceID 和 DeviceIdentity public key；Web 批准前 CLI 保持等待，批准后 proof 完成并签发 daemon session，同一码重放被 `DEVICE_ENROLLMENT_REQUIRED` 拒绝。
- 创建未认领 `MXD-...` 后重启 Controller，health 恢复为 `204`，该 pending flow 查询返回 `404`；已完成 daemon 仍存在于 PostgreSQL account device projection。服务器配置已确认不含三个旧 `development_enrollment_*` 字段。
- 该轮 Controller SHA-256 为 `4ee4299bc9edea12d388fc8db03c67ae98e5442383b8ad563d1818f3dbf5611a`；该轮 ARM64 HTTPS staging APK SHA-256 为 `2905b6ace56b77934f09085ff1bcd5c35eaa692c711defd6c780fdf5b846ec72`。
- Android managed P2P 已完成 endpoint resolve、signaling、terminal list、打开 terminal、输入输出和锁屏恢复 smoke。
- Pro/Team catalog 升级到 version `2`，Relay region 显式允许 `local-1`、`us-west-1` 和 `cn-east-1`；现有 bootstrap staging entitlement 已一次性迁移到同版本 projection。
- Android 选择 `Use relay` 后，client 与 daemon 分别成功取得 `/v1/relay/leases/acquire`，随后完成 signaling；真实 TURN Relay session 保持超过 40 秒后仍可打开 `android-relay-success` terminal，terminal channel attach 和逐字符 input 均成功。
- Relay workspace 的后台 inventory subscription 不再把“未指定 route policy”误当成 `AUTO`。只有用户明确选择 Relay/P2P 才更新 Go-owned Endpoint registry，避免后台订阅提升 generation 并使当前 Relay session stale。
- Relay session 内远端文件面板成功列出 daemon 主机 `/` 与 `/tmp`；2026-07-24 的最终 APK 已进一步完成上传、下载、取消和内容摘要校验，详见下方最终候选证据。
- Android `minSdk 24` 的 Cloud module 使用 `java.time`，APK 已启用 core library desugaring；Android 7.0/7.1 不再因 factory 加载期缺少 `java.time.Instant` 被误报为 managed cloud module 未安装。初始化失败会保留原始异常到 `ManagedCloudAssembly` logcat。
- 该轮复测 APK 为 ARM64 debug staging artifact，SHA-256 `2905b6ace56b77934f09085ff1bcd5c35eaa692c711defd6c780fdf5b846ec72`；APK 已确认包含 Official factory、HTTPS staging BuildConfig、`j$.time` 和最新 mobile UI assets，覆盖安装后完成手工登录码流程并正常进入 Machines 页面。本轮 logcat 未发现 `FATAL EXCEPTION`、ANR、`SIGSEGV` 或 native fatal signal。
- `CLOUDAUTH001` 后，Hub 使用 Controller 公钥离线验证 EdgeAccess token，client 设备尚未进入 policy projection 时也能立即读取同账号设备目录；projection lag 返回可重试错误，明确撤销返回 `AUTHORIZATION_REVOKED`，不再冒充登录失效。Linux Edge SHA-256 `a7f83ca5c5a1445e4955687b4da40da45bb8ad65650ed6e997bee69fa1fc38d9` 已滚动部署到 US/CN，两端 health 恢复 `204`。ARM64 公网 HTTPS APK SHA-256 `5adedd4bf0480687d3d0e89d49dc5898c2270857fb5a271b7cf7737c728cf170` 完成全新 `MXA` 手工登录、Web 批准后首次目录同步、强制停止与重启恢复；logcat 未出现 `unauthenticated`、Java 或 native crash。
- Android account access session 在 refresh window 内真实请求 `https://muxvia.com/v1/sessions/refresh`，refresh token 轮换后继续完成 Hub 设备目录、endpoint resolve 和 signaling；账号状态没有因 Hub 临时错误被清空。
- Edge Presence cold-start 错误不再折叠成永久 admission failure；daemon 按 `PresenceReady.heartbeat_seconds` 的两周期 deadline 识别反向代理保留的半开 stream。US Edge 重启后，同一 daemon/Companion 进程重新建立到新 Edge PID 的 Presence upstream。相同 Linux Edge 二进制 SHA-256 `49b10d6c5c3d2bce2cba42f6add988bd778e7fcbbe4c91d775b8f7083a3e27e0` 已部署到 US/CN，两端 health 为 `204`。
- Android native generation 会在默认网络变化时关闭旧 Go engine、session/resource/event pump，并重建 Go binding。Hub 明确返回、且证明没有创建 signaling session 的 retryable P2P quota conflict 由 Go managed Dialer 在 75 秒窗口内有界重试；网络超时、认证、协议和其他结果不确定失败不自动重放。ARM64/API 35 模拟器从活跃 Wi-Fi session 切到 cellular 后约 32 秒恢复同一 terminal inventory，cellular 切回 Wi-Fi 约 2 秒恢复；旧 generation 遮罩被新 inventory 成功提交清除，logcat 无 Java/native crash。
- 网络恢复验收 APK SHA-256 为 `b3f42aaa69129bc79ebfc502c5ba67eafdebd18a76abdb2ba19f1dbbbcb0a2fa`。
- AUTO 跨 NAT 修复由 Go Client Engine 在 `STANDARD_RELAY` 下主动取得 client caller-specific TURN，并把 STUN/P2P 与 TURN material 同时交给 Pion；daemon 从 offer 为自己的 principal 取得同一 RelayIntent 下的 TURN。Hub 现在让 AUTO signaling 沿用 resolve 返回的 `managed_session_id`，同时保持 `relay_only=false` 和 managed P2P reservation，避免 daemon 因 correlation 改写而取得 `401`。`managedPeer.WaitReady` 在 PeerConnection failed/closed、DataChannel close 或 15 秒 deadline 时显式返回；generated `UNAVAILABLE` 保留到 UI 并显示“检查两端网络后重试”。
- 修复前实体 Android 16/API 36 手机在移动网络上复现 client Relay lease `200`、daemon Relay lease `401` 与持续等待；修复后的 Go client/daemon/Hub regression、`make test-private`、双 Edge 进程门禁、Client/UI/Android 门禁均通过。公网 US/CN Edge 已滚动更新为 SHA-256 `e7e5c31fd2665602f682f58a5a23095e859060fbe7a9ac4417a723ba4e6c8b9d`，服务端本机与公网 health 均为 `204`，旧版本保存在 `/opt/muxvia/rollback/pre-pg004-auto-turn-20260723/`。本轮 ARM64 APK 与实体设备 `base.apk` SHA-256 均为 `17ff87eebf654aa93cff260b89455f61d6b482a96ce66392e0d980879b6e83ac`，实体 App 已确认失败后停止 loading 并显示上述本地化提示。
- 2026-07-23 实体 AUTO 复测使用 `Xiaomi 24129PN74C`、ARM64、Android 16/API 36；手机关闭 Wi-Fi，默认网络为 `MOBILE[NR] ctnet`，并关闭手机 VPN。目标 Mac 的 Clash 暂时切到 `direct`，用于排除测试机 VPN 对 UDP 回程的影响。真实 App UI 提交 `MXP1` 后依次完成 endpoint resolve、client/daemon caller-specific Relay lease、双端 signaling、DataChannel 配对、terminal list 和 terminal open。脱敏 TURN 摘要证明 client 与 daemon 都完成 `401 -> authenticated Allocate -> CreatePermission/ChannelBind`，并出现双向 ChannelData；AUTO 保持 `relay_only=false`，实际数据路径由单个 US Relay 承载。
- 同一次实体 App UI 在 `phone-auto-pure5g-20260723` 终端输入 `printf 'MUXVIA_AUTO_5G_OK_20260723\n'`，App 屏幕与 daemon authoritative live capture 都得到 `MUXVIA_AUTO_5G_OK_20260723`。全局 logcat 扫描未发现 `FATAL EXCEPTION`、ANR、`SIGSEGV` 或 native fatal signal。此前保留 Clash `rule` 的失败对照中，两端都只重传匿名 Allocate；Relay 返回的 `401` 含完整 Realm/Nonce 且事务 ID 匹配，但代理 UDP 路径当时未把响应交回 Pion。与 `../tgent` 对照后确认这不是可以忽略的测试环境问题：`tgent` 同端口提供 TURN/UDP 与 TURN/TCP，当时 Muxvia 只提供 UDP，因此缺少代理和受限网络所需的 transport fallback。
- 提交 `65f34123275478a84baff3a0c31bb39d1d9c7365` 为 Relay 增加同端口 TURN/TCP：Relay startup 只有在 UDP 与 TCP 都成功绑定后才提交，二者共享同一 authority、caller credential、allocation/usage owner 和 relay generator；lease 返回 `transport=udp` 后跟 `transport=tcp`。测试覆盖 TCP-only Pion DataChannel、TCP bind 失败时 UDP 回滚、client/daemon caller lease URL 顺序、真实 Relay data/usage/close 和 Pion 配置透传。`make test-private`、`make test`、Relay/Edge race test 均通过。
- Linux/amd64 Edge artifact SHA-256 为 `62e61181735d93e530cd5e802977f729e027d1962fbf6edca9e91f758ab8266f`，已滚动部署到 US/CN；两端 `systemd` active，`ss` 同时显示 `0.0.0.0:41003/udp` 与 `0.0.0.0:41003/tcp`。回滚二进制位于两台服务器的 `/opt/muxvia/rollback/pre-pg004-turn-tcp-65f34123/`。
- 2026-07-23 TURN/TCP 实体验收使用同一台 `Xiaomi 24129PN74C`、ARM64、Android 16/API 36，以及 SHA-256 为 `17ff87eebf654aa93cff260b89455f61d6b482a96ce66392e0d980879b6e83ac` 的已安装 APK。电脑和手机 Clash 均保持 `rule`；手机关闭 Wi-Fi，Android 默认网络为 `MOBILE[NR] ctnet`，Clash `tun0` 的 underlying network 为该 cellular network。App UI 提交新 `MXP1` 后完成 AUTO pairing、terminal list、打开 `phone-auto-clash-rule-pure5g-tcp-20260723`，并输入 `printf 'MUXVIA_TURN_TCP_PURE5G_DATA_OK_20260723\n'`；屏幕得到同名输出。
- 上述稳定 DataChannel 抓包在 45 秒内记录 `1153` 个 TURN/TCP packet 和 `867` 个 TURN/UDP packet；TCP leg payload `562140` bytes，UDP leg payload `560188` bytes，流量在 Relay 两条 leg 之间成对转发。它证明 TCP URL 不只是候选或认证 smoke，而是进入了真实 terminal application data 链路；另一端继续使用 UDP，符合 ICE 独立选择每端可达 transport、UDP 优先且 TCP fallback 的设计。抓包 SHA-256 为 `f384757aeda1cf14d2a6e80a63ab00bb3d839c52f53db1119a20854219ab6eec`。全局 logcat 未发现 `FATAL EXCEPTION`、ANR、`SIGSEGV`、`SIGABRT` 或 native fatal signal；测试后已恢复手机 Wi-Fi、关闭移动数据并保留用户开启的 Clash VPN。
- 2026-07-24 最终可靠性候选使用仓库指定 ARM64 模拟器 `termx-pa005n1`、API 35、`arm64-v8a`。APK 路径为 `clients/mobile/android/app/build/outputs/apk/debug/app-arm64-v8a-debug.apk`，构建产物与设备 `base.apk` SHA-256 均为 `293e97a7e1ddbfb1a2b4b260941bab267bdeb72d0abc2c98a35af85147490bc2`。宿主机保留 Clash；模拟器对 Muxvia 域名使用真实公网地址，避免 Clash fake-IP 未经本机代理映射直接进入 Android DNS 结果。连接策略由 App UI 保持 `Muxvia Cloud / 仅 Relay / 仅 TCP`，当前连接投影两次分别得到 `Relay 中转 / TCP / 435 ms` 和 `Relay 中转 / TCP / 825 ms`。
- 公网 control resolve、双端 lease 与 signaling 在实测中可先消耗约 15 秒，原 binding 20 秒总 deadline 会在 TURN/ICE 仍正常推进时取消连接。最终候选把 Go binding 完整 open window 调整为 40 秒，Android 外层窗口为 45 秒，Pion ICE/DataChannel ready deadline 仍保持 15 秒；PeerConnection final failure/close 和可靠有序 DataChannel send failure 都会关闭精确 protocol channel，使 binding session generation 失效，不再留下持续等待的半开 application session。
- 最终 APK 从真实 App UI 打开 daemon 和 `pg004-relay-tcp-e2e`，通过真实 terminal input 发送 `echo PG004_RELAY_TCP_OK`；daemon authoritative live capture 得到同名输出。返回设备列表再进入后 terminal inventory 成功恢复，旧 bridge/session 错误被当前 refresh sequence 清除。
- 文件 E2E 全部由 App UI 发起：上传手机 `muxvia-pg004-upload.txt` 到 `/tmp/muxvia-pg004-file-e2e/` 后，两端 SHA-256 均为 `0a762dfab6650aea63118e3fea08ae76dbf2664c7f10679138133457718a1e36`；下载 `download-source.txt` 到 `Downloads/Muxvia/` 后，两端 SHA-256 均为 `a61813ffaf8c010dba65287b16b308a814f8855f80742d5cbf75af4101df2838`。64 MiB `muxvia-pg004-upload-cancel.bin` 在 UI 显示已传输 9.1 MiB 后点击取消，任务显示 `cancelled`，daemon 目标目录无最终文件或临时残留。
- Android SAF 会按安全规则冻结 Activity 并关闭旧 Go engine。最终候选在打开 picker 前显式建立 foreground generation barrier，picker 返回结果只有在 `handleForegroundResume -> 新 bridge -> runtime reset` 完成后才交给 transfer store；实机流程证明上传使用新 session 完成，而不是复用 `Go binding backend is closed` 的旧 handle。一次 managed connect 返回 `client session is unavailable` 时传输中心结束等待并提供“全部继续”，下一次有界建连成功后完成传输。
- 最终门禁通过：`go test -race ./client/adapter/managed/pion ./remote/webrtc ./client/binding`、`go test ./cmd/muxvia`、Mobile 39 项、UI 163 项、UI typecheck、Mobile production build、Android `testDebugUnitTest assembleDebug` 和 `git diff --check`。测试结束时 App PID 仍存活，按该 PID 扫描 logcat 未发现 `FATAL EXCEPTION`、ANR、`SIGSEGV`、`SIGABRT` 或 native fatal signal。

本地 ignored 证据产物：

```text
.artifacts/cloud-deploy/e2e/account.png
.artifacts/cloud-deploy/e2e/operator.png
.artifacts/cloud-deploy/e2e/android-managed-terminal.png
.artifacts/cloud-deploy/e2e/android-managed-after-lock.png
.artifacts/cloud-deploy/e2e/android-relay-terminal.png
.artifacts/pg004/android-active-wifi-to-cellular-retry.png
.artifacts/pg004/android-active-cellular-to-wifi-retry.png
.artifacts/pg004-auto/physical-auto-5g-terminal.png
.artifacts/pg004-auto/muxvia-pg004-turn-17.pcap
.artifacts/pg004-turn-tcp/phone-pure5g-pairing-45s.png
.artifacts/pg004-turn-tcp/phone-pure5g-terminal-command-final.png
.artifacts/pg004-turn-tcp/muxvia-pg004-turn-tcp-pure5g.pcap
.artifacts/pg004-turn-tcp/muxvia-pg004-turn-tcp-pure5g-data.pcap
.artifacts/pg004/final/final-relay-tcp-connection-info.png
```

## 当前限制

1. 当前部署 credential window 到 `2026-08-20T15:41:28Z`。到期前必须完成正式 key 配置/轮换或重新生成 staging 资产。
2. Let's Encrypt 证书到期日为 2026-10-19；当前已删除临时 Cloudflare credential，自动续期尚未配置。
3. R2 age 加密上传和独立恢复仍是 PG004 的未完成门禁；现有 Cloudflare token 只有 DNS 权限，k8s、开发机和 155 服务器均未发现 R2/S3 access key。
4. 真实支付、邮件验证和密码找回未接入；bootstrap staging 不得作为商业生产发布。

## 仓库门禁

```text
go test -timeout 90s ./private/cloud/controller ./private/cloud/edge ./private/cloud/devcloud/cmd/muxvia-cloud-bootstrap
go test ./client/binding/... ./remote/daemon ./private/cloud/companion/... ./private/cloud/controller
npm --prefix clients/ui test
npm --prefix clients/ui run typecheck
cd clients/mobile/android && ./gradlew -PmuxviaPublicHTTPSStaging=true -PmuxviaArmOnly=true testDebugUnitTest assembleDebug
private/cloud/deploy/build-bundle.sh
git diff --check
```

测试同时修复了 `postgrestest` 在 PostgreSQL 不可用时持锁调用 `t.Skipf` 导致后续测试死锁的问题；现在会先释放 fixture mutex 再 skip。
