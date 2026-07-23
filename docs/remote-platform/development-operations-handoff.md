# Muxvia 开发、测试与部署迁移交接

## 1. 文档目的

本文是把 Muxvia 的开发环境、测试环境和公网 bootstrap staging 迁移到新服务器时的执行基线。信息来自仓库、当前开发机以及 2026-07-23 对两台公网服务器的只读核查。

本文不保存任何明文密码、access token、refresh token、数据库 DSN、Cloudflare API token、R2 secret、TLS private key、Controller signing private key、Edge control private key或 Android Keystore 内容。迁移这些内容时必须使用加密传输和受控 secret storage。

当前仓库活动范围、任务顺序和准入仍以根目录 `workflow.md` 为准。当前最新提交为：

```text
e89cf233 规划多 Hub 注册选址切片
```

## 2. 源码与 Git

当前实际 Git 远端仍是：

```text
git@github.com:lozzo/termx.git
```

当前分支是 `master`。虽然产品组织已经建立为 `github.com/muxvia`，本地 `origin` 尚未切换到该组织。换机时不要根据品牌名称猜测仓库地址，应先按当前远端克隆：

```bash
git clone git@github.com:lozzo/termx.git
cd termx
git switch master
git status --short --branch
```

若后续把仓库迁到 `github.com/muxvia/muxvia`，应单独完成远端切换和权限验证，不要在服务器迁移过程中同时改 Git 历史或 module identity。

## 3. 当前架构

公网 Cloud 只有两个业务二进制：

```text
muxvia-cloud-controller
  = Control Plane + Web Controller + Operator API + PostgreSQL owner

muxvia-cloud-edge
  = memory-only Hub + Relay runtime + durable Relay usage outbox
```

关键边界：

- Controller 是账号、Session、Subscription、Entitlement、Hub registry、HubAssignment、CommandOutbox、quota 和 usage ledger 的持久真值。
- PostgreSQL 使用标准 pgx/PostgreSQL 协议；Supabase 只是托管 PostgreSQL，不使用 Supabase Auth、Realtime、PostgREST 或 Edge Functions。
- Hub 的 policy、assignment projection、Presence、signaling、topology 和 command delivery 状态只在内存中，Edge 重启后从 Controller full sync。
- Relay 只允许 `/var/lib/muxvia/edge/usage.outbox` 落盘，迁移或重装时不能随意丢弃未确认记录。
- daemon Go runtime 是 authenticated PeerSession 和 terminal truth 的 owner。
- Android 通过 Go C ABI/JNI 使用同一个 Client Engine；Kotlin/TypeScript 不拥有连接、认证或重连状态机。

## 4. 公网部署实况

### 4.1 服务拓扑

| 主机 | 当前角色 | 公网入口 |
| --- | --- | --- |
| `155.94.155.192` | Controller、Web、Operator、US West Edge、US Relay | `muxvia.com`、`operator.muxvia.com`、`control.muxvia.com`、`us1.edge.muxvia.com`、`155.94.155.192:41003/udp` |
| `114.66.58.243` | China East Edge、China Relay | `cn1.edge.muxvia.com:41102`、`114.66.58.243:41003/udp` |
| Supabase Singapore | Controller PostgreSQL | project ref `avdjhfkmswaozpoysqrz`，IPv4 Session pooler `aws-0-ap-southeast-1.pooler.supabase.com:5432` |

Controller 使用独立 schema：

```text
muxvia_staging
```

当前可用的运维入口：

```bash
ssh root@155.94.155.192
ssh root@114.66.58.243
```

换机前应确认新服务器已经安装相同的运维公钥，并记录旧、新主机的 SSH host key fingerprint。不要复制开发机的 SSH private key到普通部署目录。

### 4.2 155 主机

```text
hostname: racknerd-1f52f49
OS: Ubuntu 24.04, x86_64
root filesystem: 43G, used 21G, available 21G
```

systemd 服务：

```text
muxvia-cloud-controller.service  enabled, active
muxvia-cloud-edge.service        enabled, active
```

当前运行二进制：

```text
/opt/muxvia/bin/muxvia-cloud-controller
SHA-256 0f7672035877bae7888dd517db620fd69bf7002ecd98cc66cfb97908488d686d

/opt/muxvia/bin/muxvia-cloud-edge
SHA-256 49b10d6c5c3d2bce2cba42f6add988bd778e7fcbbe4c91d775b8f7083a3e27e0
```

当前 Edge runtime：

```text
edge_deployment_id: edge-us-sjc-1
hub_id: hub-us-sjc-1
relay_id: relay-us-sjc-1
region: us-west-1
hub_url: https://us1.edge.muxvia.com
relay_url: turn:155.94.155.192:41003?transport=udp
```

2026-07-23 只读快照中，US Edge 的 Hub control generation 为 `342`、Relay generation 为 `46`、projection revision 为 `207`。这些数字会随重连和 projection 更新继续递增，只用于证明当时链路活跃，不能作为新服务器必须复现的固定值。

### 4.3 114 主机

```text
hostname: RainYun-ntzoCjzi
OS: Ubuntu 24.04, x86_64
root filesystem: 30G, used 20G, available 8.5G
```

systemd 服务：

```text
muxvia-cloud-edge.service  enabled, active
```

当前运行二进制：

```text
/opt/muxvia/bin/muxvia-cloud-edge
SHA-256 49b10d6c5c3d2bce2cba42f6add988bd778e7fcbbe4c91d775b8f7083a3e27e0
```

当前 Edge runtime：

```text
edge_deployment_id: edge-cn-nbo-1
hub_id: hub-cn-nbo-1
relay_id: relay-cn-nbo-1
region: cn-east-1
hub_url: https://cn1.edge.muxvia.com:41102
relay_url: turn:114.66.58.243:41003?transport=udp
```

2026-07-23 只读快照中，China Edge 的 Hub control generation 为 `17`、Relay generation 为 `42`、projection revision 为 `208`。迁移后只检查 generation 有效且继续推进，不要求等于这些历史值。

114 上已有 FRP 服务占用公网 `80/tcp` 和 `443/tcp`，所以 China Edge 使用 `41102/tcp`。迁移时不能假设可以直接占用 443。

## 5. DNS、Cloudflare 与 TLS

当前 DNS 期望值：

| 名称 | 目标 | Cloudflare 模式 |
| --- | --- | --- |
| `muxvia.com` | `155.94.155.192` | Proxied |
| `www.muxvia.com` | `155.94.155.192` | Proxied |
| `operator.muxvia.com` | `155.94.155.192` | Proxied |
| `control.muxvia.com` | `155.94.155.192` | DNS-only |
| `us1.edge.muxvia.com` | `155.94.155.192` | DNS-only |
| `cn1.edge.muxvia.com` | `114.66.58.243` | DNS-only |

Hub、Controller control stream 和 TURN 不应经过 Cloudflare HTTP Proxy。Relay 是 UDP 直连。

证书：

```text
muxvia.com / operator / control / us1 SAN certificate
notAfter: 2026-10-19 14:39:20 UTC

cn1.edge.muxvia.com certificate
notAfter: 2026-10-19 14:40:38 UTC
```

当前没有发现 certbot/systemd 自动续期 timer。迁移后必须显式配置续期并做一次 `--dry-run`，否则 2026-10-19 后 HTTPS 会中断。

## 6. 端口与防火墙

### 6.1 155

```text
127.0.0.1:42001/tcp  Controller public/Web
127.0.0.1:42002/tcp  Controller internal Hub/Relay control
127.0.0.1:42003/tcp  Operator API/Web
127.0.0.1:42101/tcp  US Hub HTTP
127.0.0.1:42102/tcp  US Edge health
0.0.0.0:41003/udp    US TURN Relay
0.0.0.0:80/tcp       shared Nginx container
0.0.0.0:443/tcp      shared Nginx container
```

主机当前没有 UFW，iptables INPUT 默认 ACCEPT。云厂商安全组仍需确认开放 `80/tcp`、`443/tcp` 和 `41003/udp`。

### 6.2 114

```text
127.0.0.1:42101/tcp  China Hub HTTP
127.0.0.1:42102/tcp  China Edge health
0.0.0.0:41003/udp    China TURN Relay
0.0.0.0:41102/tcp    China Edge HTTPS Nginx
0.0.0.0:80/tcp       existing FRP
0.0.0.0:443/tcp      existing FRP
```

UFW 当前 inactive，iptables INPUT 默认 ACCEPT。云安全组需要开放 `41102/tcp` 和 `41003/udp`。

## 7. 安装目录和 systemd

两台服务器统一使用：

```text
/opt/muxvia/bin/       二进制
/opt/muxvia/config/    配置、plans.json、bootstrap credentials
/opt/muxvia/web/       Web Controller 静态资源
/var/lib/muxvia/       runtime manifest 和 Relay usage outbox
```

运行用户：

```text
user: muxvia
group: muxvia
```

systemd unit 来源：

```text
private/cloud/deploy/muxvia-cloud-controller.service
private/cloud/deploy/muxvia-cloud-edge.service
```

安装后执行：

```bash
systemctl daemon-reload
systemctl enable muxvia-cloud-controller.service
systemctl enable muxvia-cloud-edge.service
systemctl start muxvia-cloud-controller.service
systemctl start muxvia-cloud-edge.service
systemctl status muxvia-cloud-controller.service --no-pager
systemctl status muxvia-cloud-edge.service --no-pager
```

114 只安装和启动 Edge unit。

## 8. Nginx 的实际装配差异

### 8.1 155 是共享 Docker Nginx

155 没有 host `nginx.service`。公网 80/443 来自：

```text
container: nginx-proxy
image: nginx:alpine
restart policy: unless-stopped
```

挂载：

```text
/root/nginx-proxy/nginx.conf -> /etc/nginx/nginx.conf
/root/nginx-proxy/conf.d     -> /etc/nginx/conf.d
/root/nginx-proxy/ssl        -> /etc/nginx/ssl
```

Muxvia 文件：

```text
/root/nginx-proxy/conf.d/muxvia.conf
/root/nginx-proxy/ssl/muxvia.com/fullchain.pem
/root/nginx-proxy/ssl/muxvia.com/privkey.pem
```

该容器还承载 `omscd.com`、`onemoresec.com` 和其他服务。迁移 Muxvia 时只能迁移 Muxvia server block 和证书，不能覆盖整个共享 `conf.d`、`ssl` 或容器配置。

校验和 reload：

```bash
docker exec nginx-proxy nginx -t
docker exec nginx-proxy nginx -s reload
```

### 8.2 114 是 host Nginx

114 使用 host Nginx 和 `nginx.service`：

```text
/etc/nginx/ssl/muxvia-edge-secondary/fullchain.pem
/etc/nginx/ssl/muxvia-edge-secondary/privkey.pem
```

配置基准：

```text
private/cloud/deploy/nginx-edge-secondary.conf
```

校验和 reload：

```bash
nginx -t
systemctl reload nginx
```

## 9. 不能靠 Git 恢复的资产

迁移前必须加密备份以下文件。不要把它们提交到仓库。

### 9.1 155

```text
/opt/muxvia/config/controller-config.json
/opt/muxvia/config/edge-config.json
/opt/muxvia/config/credentials.json
/opt/muxvia/config/plans.json
/var/lib/muxvia/edge/usage.outbox
/root/nginx-proxy/conf.d/muxvia.conf
/root/nginx-proxy/ssl/muxvia.com/fullchain.pem
/root/nginx-proxy/ssl/muxvia.com/privkey.pem
/etc/systemd/system/muxvia-cloud-controller.service
/etc/systemd/system/muxvia-cloud-edge.service
```

`controller-config.json` 包含 PostgreSQL DSN、Controller projection private key、daemon-control private key 和 operator token。`edge-config.json` 包含 Hub/Relay control private key。`credentials.json` 包含 bootstrap 账号密码和 operator token。

### 9.2 114

```text
/opt/muxvia/config/edge-config.json
/var/lib/muxvia/edge/usage.outbox
/etc/nginx/ssl/muxvia-edge-secondary/fullchain.pem
/etc/nginx/ssl/muxvia-edge-secondary/privkey.pem
/etc/systemd/system/muxvia-cloud-edge.service
```

### 9.3 开发机

```text
.artifacts/
~/.android/avd/termx-pa005n1.avd/
~/.android/avd/termx-pa005n1.ini
Android debug keystore
SSH private keys 和 known_hosts
未提交的 Cloudflare、Supabase、R2、测试账号 secret
```

`.artifacts/` 中包含多代 Controller/Edge config、development credentials、Companion manifest、APK、截图和日志，其中部分文件含 secret。推荐整目录加密归档后迁移，不要上传到普通对象存储或 Git。

## 10. Secret 清单

新服务器至少需要从安全渠道重新注入或迁移：

```text
MUXVIA_CONTROLLER_POSTGRES_DSN
Controller projection private key
daemon-control private key
两个 Edge 的 Hub control private key
两个 Edge 的 Relay control private key
operator access token
bootstrap account password
Cloudflare DNS API token
Let's Encrypt/ACME account material或重新签发能力
MUXVIA_BACKUP_AGE_RECIPIENT
MUXVIA_BACKUP_AGE_IDENTITY
MUXVIA_R2_BUCKET
MUXVIA_R2_ENDPOINT_URL
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
Android release signing material（当前尚未建立正式 production signing）
```

Supabase 项目 URL：

```text
https://avdjhfkmswaozpoysqrz.supabase.co
```

当前 IPv4 Session pooler 用户形式：

```text
postgres.avdjhfkmswaozpoysqrz
```

数据库密码不得写入本文。远程 DSN 必须使用 `sslmode=require`、`verify-ca` 或 `verify-full`，不得使用 transaction pooler `:6543`。

## 11. Credential 有效期风险

当前 Controller 和两个 Edge 的部署 credential window 一致：

```text
notAfter: 2026-08-20 15:41:28 UTC
```

该日期到期后，Controller/Edge control identity 和签发链路会失败。服务器迁移不能顺带生成一套不匹配的新 key。短期迁移应原样保留现有 Controller/Edge配置和 key；随后单独规划 key rotation 或重新 bootstrap staging。

## 12. 开发环境依赖

当前已验证工具版本：

```text
Go 1.26.4
Node.js 26.5.0
npm 11.17.0
protoc 35.1
protoc-gen-go 1.36.11
OpenJDK 21.0.11
PostgreSQL client 17.10
age 1.3.1
jq 1.8.1
ripgrep 15.1.0
Android SDK / adb 37.0.0
```

新开发机准备完成后：

```bash
npm ci
./scripts/doctor.sh
```

`doctor.sh` 会检查 Go、Node、npm、Java、protoc、protoc-gen-go、Android SDK、Gradle wrapper、generated code 和仓库目录守卫。

Linux 服务器只负责构建 Go/Web 时不需要 Android SDK；承担 Android E2E 的开发服务器必须能够运行 ARM64 Android emulator，或者保留一台独立 ARM64/macOS 测试机。

## 13. 本地 development Cloud

唯一当前 development 入口：

```bash
make cloud-dev
```

它通过本地 PostgreSQL fixture 启动：

- 一个 Controller；
- 两个独立 Edge；
- 每个 Edge 的 Hub + Relay；
- 真实 control stream；
- runtime manifest、config、日志和 usage outbox。

默认产物目录：

```text
.artifacts/cloud-dev/
```

使用远程 Supabase 做 development 验证时：

```bash
export MUXVIA_DEV_POSTGRES_DSN='<secure Supabase DSN>'
make cloud-dev
```

不要恢复旧 `muxvia-staging-cloud.service`、旧单进程 Cloud 或 `hub-policy.snapshot`。

## 14. 构建公网部署包

```bash
private/cloud/deploy/build-bundle.sh
```

输出：

```text
.artifacts/cloud-deploy/bundle/muxvia-cloud-linux-amd64.tar.gz
```

内容：

```text
bin/muxvia-cloud-controller
bin/muxvia-cloud-edge
bin/muxvia-cloud-bootstrap
web/
config/plans.json
```

构建过程使用：

```text
CGO_ENABLED=0 GOOS=linux GOARCH=amd64
```

部署前必须记录每个二进制和 tarball 的 SHA-256。

## 15. Bootstrap 与原样迁移的区别

`muxvia-cloud-bootstrap` 只适合创建新的空 schema 和全新部署身份。它会生成新的 Controller signing key、Edge identity、operator token 和 bootstrap 密码。

对于本次“把现有 staging 搬到新服务器”，默认应原样迁移现有配置和密钥，而不是重新运行 bootstrap。否则现有 Edge identity、token audience、assignment 和账号 session 会失配。

只有明确决定建立全新 staging 时才执行：

```bash
export MUXVIA_CONTROLLER_POSTGRES_DSN='<secure base DSN>'
.artifacts/cloud-deploy/bundle/bin/muxvia-cloud-bootstrap \
  --output-dir '<new empty directory>' \
  --schema '<new unused schema>' \
  --public-url 'https://muxvia.com' \
  --operator-url 'https://operator.muxvia.com' \
  --controller-url 'https://control.muxvia.com' \
  --primary-hub-url 'https://us1.edge.muxvia.com' \
  --secondary-hub-url 'https://cn1.edge.muxvia.com:41102' \
  --primary-public-ip '<new primary IP>' \
  --secondary-public-ip '<new secondary IP>'
```

该命令要求空输出目录和新 schema；失败时会删除本次新 schema。

## 16. 推荐迁移顺序

### 阶段 A：冻结和备份

1. 确认 Git 已推送，记录 commit SHA。
2. 对 Supabase `muxvia_staging` 做 schema-scoped `pg_dump`。
3. 加密备份第 9 节全部 secret 文件。
4. 复制两个 Edge 的 `usage.outbox`。
5. 记录当前 Controller/Edge 二进制 SHA、systemd 状态、DNS 和证书日期。
6. 不立即关闭旧服务器。

### 阶段 B：准备新服务器

1. 创建 `muxvia` system user/group。
2. 创建 `/opt/muxvia/{bin,config,web}` 和 `/var/lib/muxvia/{controller,edge}`。
3. 安装 systemd unit、Nginx/反向代理和证书。
4. 部署同一构建产物。
5. 以 `0600 muxvia:muxvia` 写入 Controller/Edge config。
6. 恢复 Edge `usage.outbox`。

### 阶段 C：修改网络相关配置

若公网 IP 变化，只修改必要字段：

```text
Edge relay_public_ip
Edge public_hub_url（域名变化时）
Edge controller_url（域名变化时）
Controller development_mobile_hub_url/region（目标变化时）
Nginx control allowlist
Cloudflare DNS A/AAAA 记录
TURN URL 对应的公网 IP
```

不要修改 deployment ID、Hub ID、Relay ID、identity fingerprint 或 private key。

### 阶段 D：启动顺序

1. 启动 Controller。
2. 验证 PostgreSQL migration 和 public/operator health。
3. 启动 US Edge。
4. 启动 China Edge。
5. 等两个 Edge 重新取得 control generation 和 full projection。
6. 验证 Web/Operator fleet。
7. 再切 DNS。

### 阶段 E：切流与回滚窗口

1. DNS TTL 提前降低。
2. 更新 DNS 后同时观察旧、新服务器日志。
3. 完成账号登录、daemon Presence、Android managed P2P 和 Relay smoke。
4. 至少保留旧服务器和旧配置一个完整观察窗口。
5. 只有新环境稳定后才 disable 旧 systemd unit。

回滚只需要恢复旧 DNS，并重新启动旧 Controller/Edge；不能同时让同一 Hub identity 在新旧两台机器长期并行运行，否则会持续推进 control generation 并互相顶替。

## 17. 健康检查和运维命令

公网检查：

```bash
curl -fsS -o /dev/null -w '%{http_code}\n' https://muxvia.com/healthz
curl -fsS -o /dev/null -w '%{http_code}\n' https://operator.muxvia.com/healthz
curl -fsS -o /dev/null -w '%{http_code}\n' https://us1.edge.muxvia.com/healthz
curl -fsS -o /dev/null -w '%{http_code}\n' https://cn1.edge.muxvia.com:41102/healthz
```

成功值当前为 `204`。`control.muxvia.com` 是受 IP allowlist 保护的内部 control origin，不应作为普通公网健康端点。

systemd：

```bash
systemctl status muxvia-cloud-controller.service --no-pager
systemctl status muxvia-cloud-edge.service --no-pager
journalctl -u muxvia-cloud-controller.service -f
journalctl -u muxvia-cloud-edge.service -f
```

监听端口：

```bash
ss -lntup | rg '41003|41102|42001|42002|42003|42101|42102'
```

安全日志中不得出现数据库 DSN、access/refresh token、CapabilityGrant、DeviceIdentity private key、pairing bundle 或 terminal 输入内容。

## 18. PostgreSQL 备份与恢复

所需工具：

```text
pg_dump
pg_restore
age
sha256sum/shasum
AWS CLI（使用 R2 时）
```

加密备份：

```bash
export MUXVIA_CONTROLLER_POSTGRES_DSN='<secure DSN>'
export MUXVIA_BACKUP_AGE_RECIPIENT='age1...'
export MUXVIA_R2_BUCKET='muxvia-controller-backups'
export MUXVIA_R2_ENDPOINT_URL='https://<account-id>.r2.cloudflarestorage.com'
export AWS_ACCESS_KEY_ID='<secret>'
export AWS_SECRET_ACCESS_KEY='<secret>'
scripts/backup-controller-postgres.sh
```

恢复只能指向独立空数据库或独立 Supabase project：

```bash
export MUXVIA_RESTORE_POSTGRES_DSN='<independent restore DSN>'
export MUXVIA_BACKUP_AGE_IDENTITY='/secure/path/muxvia-controller-backup.agekey'
scripts/restore-controller-postgres.sh controller-backup.tar.age
```

本地门禁：

```bash
make test-postgres-backup
```

当前 R2 真实上传和独立 Supabase 恢复尚未完成，是 PG004 阻塞项。当前已知 Cloudflare token 只有 DNS 权限，尚未找到 R2/S3 access key。

## 19. 自动测试门禁

基础环境：

```bash
./scripts/doctor.sh
git diff --check
```

Go 与 Cloud：

```bash
make test
make test-private
make test-cloud-controller-edge
make test-postgres-backup
```

共享 UI、Proto 和前端：

```bash
make test-clients
```

Web Controller Playwright：

```bash
cd private/cloud/web-controller/web
npm run typecheck
npm run build
npm run test:e2e:webux001
npm run test:e2e:hub007
```

Android：

```bash
make test-android
```

公网 HTTPS ARM64 APK：

```bash
cd clients/mobile/android
./gradlew -PmuxviaPublicHTTPSStaging=true -PmuxviaArmOnly=true \
  clean testDebugUnitTest assembleDebug
```

当前最终发布前总门禁不是只跑 unit test。必须通过真实 Web/App UI 完成登录、添加设备、连接、terminal 输入输出、文件上传/下载/取消、锁屏恢复、网络切换和 crash scan。

## 20. Android 测试环境

当前可复现 AVD：

```text
name: termx-pa005n1
device: pixel_6
API: 35
ABI: arm64-v8a
system image: system-images/android-35/default/arm64-v8a/
data partition: 6G
resolution: 1080x2400
default font scale: 1.0
adb serial: emulator-5554
```

当前开发机 emulator executable：

```text
~/Library/Android/sdk/emulator/emulator
```

启动：

```bash
$ANDROID_HOME/emulator/emulator -avd termx-pa005n1
adb wait-for-device
adb devices -l
```

本地 dev cloud 需要：

```bash
adb reverse tcp:41001 tcp:<controller-port>
adb reverse tcp:41002 tcp:<hub-port>
adb install -r .artifacts/android/app-devcloud-debug.apk
```

公网 HTTPS APK 不使用 ADB reverse。

当前模拟器安装的 `com.muxvia.app`：

```text
versionCode: 1
minSdk: 24
targetSdk: 36
installed base.apk SHA-256:
b3f42aaa69129bc79ebfc502c5ba67eafdebd18a76abdb2ba19f1dbbbcb0a2fa
```

该 APK 是 PG004 网络恢复验证使用的精确产物。换机时可以从加密的 `.artifacts` 迁移，也可以在同一 commit 上重新构建并重新完成 E2E；不能只凭文件名认为二进制相同。

Crash scan：

```bash
adb logcat -c
adb logcat -d | rg 'FATAL EXCEPTION|ANR|SIGSEGV|SIGABRT|Fatal signal|Go runtime'
```

## 21. 已通过的公网能力

截至 2026-07-23 已有真实证据：

- Web 注册、登录、账号中心和测试交易。
- Web 创建手机 activation，Android 输入 `MXA` code，Web 批准后登录。
- Web 创建 daemon enrollment，CLI 输入 `MXD` code，Web 批准后 DeviceIdentity proof 完成。
- Account session refresh token 轮换。
- Controller/Edge 重启后 Hub full sync 和 daemon Presence 恢复。
- Android managed P2P：设备目录、resolve、signaling、terminal list、attach、input/output。
- Android single Relay：双端 lease、TURN、terminal attach/input。
- Android 远端文件目录浏览。
- Android Wi-Fi -> cellular -> Wi-Fi generation 重建。
- Android 锁屏/后台恢复和 crash scan。
- Web 多视口、中英文和 150% 缩放验收。

## 22. 当前未完成和已知风险

1. `PG004-HUBSEL` 尚未实现。当前新 daemon enrollment 仍固定使用 Controller 配置中的第一个 Hub。`workflow.md` 已登记 Controller 候选、Go 有界探测和 Controller 最终 assignment 的下一步。
2. R2 age 加密上传与独立恢复未完成。
3. 公网 Relay 文件上传、下载、取消和摘要校验未完成。
4. 长时间空闲 managed P2P 可能形成半开 application session；不能用 UI 定时刷新或盲目重放非幂等 command 掩盖。
5. 支付仍是 test provider；邮件验证和密码找回未接入。
6. 当前部署 key 在 2026-08-20 到期。
7. TLS 证书在 2026-10-19 到期，尚无自动续期。
8. 155 的 Docker Nginx 与其他业务共享 80/443，迁移时存在误覆盖其他站点的风险。
9. 114 磁盘使用率约 70%，迁移或构建大产物前应预留空间。
10. 当前 Git remote 仍在 `lozzo/termx`，尚未切换到 `muxvia` organization。

## 23. 迁移完成判定

只有以下全部满足，才算服务器迁移完成：

1. Git commit、构建产物 SHA 和部署配置来源可追踪。
2. Supabase 连接使用 TLS，schema migration 完整。
3. Controller 和两个 Edge systemd active。
4. 两个 Edge 在 Operator fleet 中 `FRESH`、Hub ready、Relay ready。
5. 四个公网 health endpoint 返回 204。
6. Web 登录、手机 activation 和 daemon enrollment可用。
7. Android ARM64 APK 从 UI 完成 managed P2P terminal 输入输出。
8. Android 从 UI 完成 Relay terminal 输入输出。
9. Edge 重启后 daemon Presence 自动恢复。
10. Controller 重启后账号、assignment、command、quota 和 usage 保持。
11. `usage.outbox` 没有因迁移丢失。
12. Nginx 配置测试通过，证书链和 SNI 正确。
13. 日志没有 secret、Java/native crash 或持续 generation 抖动。
14. 旧服务器仍可在观察窗口内回滚，随后才正式下线。

## 24. 共享宿主机上的非 Muxvia 服务

2026-07-23 在 155 上观察到以下 Docker 容器：

```text
kiro-go
kiro-gateway
nginx-proxy
sub2api-gpt-dev
sub2api-gpt-dev-postgres
sub2api-gpt-dev-redis
minio
frp-server
```

已知数据挂载包括：

```text
/path/to/data
/root/.aws/sso/cache
/opt/sub2api-gpt-dev/data
/opt/sub2api-gpt-dev/postgres_data
/opt/sub2api-gpt-dev/redis_data
/data/minio
/root/frps
/root/nginx-proxy
```

这些服务不属于 Muxvia 仓库，本文无法证明其数据库版本、数据一致性、账号、密钥或恢复流程。若“迁移所有开发服务”包含整台 155，必须在停机前分别执行：

1. 保存 `docker inspect`、image digest、restart policy、network 和完整 mount 列表。
2. 对 PostgreSQL、Redis 和 MinIO 使用各自一致性备份方式，不能只复制运行中的数据目录。
3. 迁移 `/root/.aws/sso/cache` 前确认是否允许复制，优先在新机重新登录。
4. 保存 FRP 配置和双方映射关系。
5. 复制共享 Nginx 时保留所有非 Muxvia 站点，或把 Muxvia 拆到独立 Nginx。
6. 在新机逐服务验收后再关闭旧容器。

114 上的 `80/tcp`、`443/tcp` 当前由既有 FRP server 占用；它同样不属于 Muxvia systemd unit，若迁移整台 114 必须单独备份 FRP 配置和客户端映射。

历史上提到的 `192.168.123.137` k8s 当前不在仓库运行配置或活动部署链路中。仓库扫描只发现“未在 k8s 找到 R2/S3 key”的历史记录，没有发现 Muxvia runtime 对该集群的当前依赖。若该集群还承载其他开发服务，应在集群侧独立执行 namespace、Secret、ConfigMap、PVC、Ingress、证书和镜像清单导出。

## 25. 相关文档

- `workflow.md`
- `AGENTS.md`
- `docs/remote-platform/pg004-public-bootstrap-deployment.md`
- `docs/remote-platform/supabase-staging-runbook.md`
- `docs/remote-platform/postgresql-supabase-migration.md`
- `docs/remote-platform/android-devcloud-manual-test.md`
- `docs/remote-platform/uxe2e001-product-experience-e2e.md`
- `docs/remote-platform/hub007-control-plane-e2e.md`
- `private/cloud/deploy/README.md`
