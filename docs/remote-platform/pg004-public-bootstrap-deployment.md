# PG004 公网 bootstrap staging 部署证据

## 结论

2026-07-21 至 2026-07-22 已完成一个真实 Supabase、一个 Controller 和两个独立 Edge 的公网 bootstrap staging。服务使用专用 `muxvia` 用户和 systemd 运行，没有 Muxvia Docker runtime。

该环境已经支持 Web 注册/登录、账号中心、移动端扫码批准、daemon enrollment、managed P2P、单节点 TURN Relay 和 Android Go/JNI 连接。Android 公网 HTTPS staging profile 已真实安装到 ARM64/API 35 模拟器；它不是 production profile，也没有改变正式构建的 fail-closed 默认值。

该环境仍不是正式商业生产：支付仍为测试 provider，R2 备份恢复、账号长期续期和完整 Android file/lifecycle E2E 尚未通过。

当前公网 Controller 已把 daemon enrollment 收敛为内存持有的十分钟 128-bit 单次 flow：任意已登录账号创建 code，daemon 提交公开 metadata、DeviceIdentity public key 和 device ID，Web 核对后批准，CLI 再以 DeviceIdentity proof 完成。pending flow 不进入 PostgreSQL，Controller 重启后统一失效；完成后的设备归属、Hub assignment 和 session 继续持久化。手机 activation 使用相同的 Web 核对批准语义，二维码与手工输入的 `MXA-...` 登录码指向同一 flow。

## 部署拓扑

| 位置 | 服务 | 公网入口 |
| --- | --- | --- |
| `155.94.155.192` | `muxvia-cloud-controller` | `https://muxvia.com`、`https://operator.muxvia.com`、`https://control.muxvia.com` |
| `155.94.155.192` | `muxvia-cloud-edge` / US West | `https://us1.edge.muxvia.com`、`turn:155.94.155.192:41003?transport=udp` |
| `114.66.58.243` | `muxvia-cloud-edge` / China East | `https://cn1.edge.muxvia.com:41102`、`turn:114.66.58.243:41003?transport=udp` |
| Supabase Singapore | Controller PostgreSQL | IPv4 Session pooler `:5432`、TLS、独立 `muxvia_staging` schema |

- `muxvia.com`、`www.muxvia.com`、`operator.muxvia.com` 使用 Cloudflare Proxy。
- `control.muxvia.com`、`us1.edge.muxvia.com`、`cn1.edge.muxvia.com` 使用 DNS-only。
- Controller public/internal/operator 和 Edge Hub/health listener 只监听 host loopback；Nginx 负责 TLS 和 SNI。
- 两个 Relay 都直接监听 `41003/udp`，不经过 Cloudflare。

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
- Companion `staging-public-https` manifest 只接受 canonical HTTPS Controller/Hub origin；Android `muxviaPublicHTTPSStaging` 构建从 Controller 签发的 `AccountSession.hub_url` 获取实际 Hub，不再把编译期 Hub 地址作为登录后的连接真值。
- daemon 通过公网 Companion 完成 enrollment，Controller 同时签发短期 daemon access token 与可轮换 refresh token；daemon refresh harness 覆盖单次轮换、旧 token replay 拒绝、ownership/auth revision/Hub assignment 复核。
- Web UI 创建移动端激活二维码，Android App 通过真实扫码输入完成认领，Web 明确批准后 App 获得账号 session；APK 重装后账号与 Endpoint 安全状态仍可恢复。
- 公网 Web/API 创建 `MXA-...` 后，ARM64/API 35 模拟器未使用摄像头，直接在 App 设置页输入同一码；Web 投影显示 `unknown Android SDK built for arm64`、`android`、`1.0`，批准后 flow 被单次消费，App 返回 Machines 并显示 `bootstrap` 账号。提交前的无效码得到显式错误，logcat 未发现 Java/native crash。
- 公网 Web/API 创建 `MXD-...` 后，development bundled CLI 提交本机 `RedmiBook.local`、`darwin/arm64`、稳定 DeviceID 和 DeviceIdentity public key；Web 批准前 CLI 保持等待，批准后 proof 完成并签发 daemon session，同一码重放被 `DEVICE_ENROLLMENT_REQUIRED` 拒绝。
- 创建未认领 `MXD-...` 后重启 Controller，health 恢复为 `204`，该 pending flow 查询返回 `404`；已完成 daemon 仍存在于 PostgreSQL account device projection。服务器配置已确认不含三个旧 `development_enrollment_*` 字段。
- 当前 Controller SHA-256 为 `4ee4299bc9edea12d388fc8db03c67ae98e5442383b8ad563d1818f3dbf5611a`；当前 ARM64 HTTPS staging APK SHA-256 为 `2905b6ace56b77934f09085ff1bcd5c35eaa692c711defd6c780fdf5b846ec72`。
- Android managed P2P 已完成 endpoint resolve、signaling、terminal list、打开 terminal、输入输出和锁屏恢复 smoke。
- Pro/Team catalog 升级到 version `2`，Relay region 显式允许 `local-1`、`us-west-1` 和 `cn-east-1`；现有 bootstrap staging entitlement 已一次性迁移到同版本 projection。
- Android 选择 `Use relay` 后，client 与 daemon 分别成功取得 `/v1/relay/leases/acquire`，随后完成 signaling；真实 TURN Relay session 保持超过 40 秒后仍可打开 `android-relay-success` terminal，terminal channel attach 和逐字符 input 均成功。
- Relay workspace 的后台 inventory subscription 不再把“未指定 route policy”误当成 `AUTO`。只有用户明确选择 Relay/P2P 才更新 Go-owned Endpoint registry，避免后台订阅提升 generation 并使当前 Relay session stale。
- Relay session 内远端文件面板成功列出 daemon 主机 `/` 与 `/tmp`；上传/下载内容校验未完成，不计为 file E2E PASS。
- Android `minSdk 24` 的 Cloud module 使用 `java.time`，APK 已启用 core library desugaring；Android 7.0/7.1 不再因 factory 加载期缺少 `java.time.Instant` 被误报为 managed cloud module 未安装。初始化失败会保留原始异常到 `ManagedCloudAssembly` logcat。
- 最终复测 APK 为 ARM64 debug staging artifact，SHA-256 `2905b6ace56b77934f09085ff1bcd5c35eaa692c711defd6c780fdf5b846ec72`；APK 已确认包含 Official factory、HTTPS staging BuildConfig、`j$.time` 和最新 mobile UI assets，覆盖安装后完成手工登录码流程并正常进入 Machines 页面。本轮 logcat 未发现 `FATAL EXCEPTION`、ANR、`SIGSEGV` 或 native fatal signal。
- `CLOUDAUTH001` 后，Hub 使用 Controller 公钥离线验证 EdgeAccess token，client 设备尚未进入 policy projection 时也能立即读取同账号设备目录；projection lag 返回可重试错误，明确撤销返回 `AUTHORIZATION_REVOKED`，不再冒充登录失效。Linux Edge SHA-256 `a7f83ca5c5a1445e4955687b4da40da45bb8ad65650ed6e997bee69fa1fc38d9` 已滚动部署到 US/CN，两端 health 恢复 `204`。ARM64 公网 HTTPS APK SHA-256 `5adedd4bf0480687d3d0e89d49dc5898c2270857fb5a271b7cf7737c728cf170` 完成全新 `MXA` 手工登录、Web 批准后首次目录同步、强制停止与重启恢复；logcat 未出现 `unauthenticated`、Java 或 native crash。

浏览器截图保存在本地 ignored artifact：

```text
.artifacts/cloud-deploy/e2e/account.png
.artifacts/cloud-deploy/e2e/operator.png
.artifacts/cloud-deploy/e2e/android-managed-terminal.png
.artifacts/cloud-deploy/e2e/android-managed-after-lock.png
.artifacts/cloud-deploy/e2e/android-relay-terminal.png
```

## 当前限制

1. Android account access session 到期时曾退出登录，没有观察到成功的 `/v1/sessions/refresh`；daemon refresh contract 已通过，但 Android account 自动续期仍需单独修复和真实长时验证。
2. Edge 重启后 daemon Presence 收到的通用 `UNAUTHENTICATED` 不能区分 cold-start transient 与真实 revoke。当前 daemon 不会盲目重试所有鉴权错误，因此 Edge 重启后的自动 Presence 恢复仍未通过。
3. 模拟器发生 Wi-Fi -> cellular -> Wi-Fi 真实网络切换时，native generation 正确失效，旧 Relay session 没有复活；但 workspace 停在 `client session is unavailable`，没有自动创建新 session。该项不计为 lifecycle PASS。
4. Relay terminal attach/input 与远端文件浏览通过；上传、下载、取消和内容摘要校验尚未完成。
5. 当前部署 credential window 到 `2026-08-20T15:41:28Z`。到期前必须完成正式 key 配置/轮换或重新生成 staging 资产。
6. Let's Encrypt 证书到期日为 2026-10-19；当前已删除临时 Cloudflare credential，自动续期尚未配置。
7. R2 age 加密上传和独立恢复仍是 PG004 的未完成门禁。
8. 真实支付、邮件验证和密码找回未接入；bootstrap staging 不得作为商业生产发布。

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
