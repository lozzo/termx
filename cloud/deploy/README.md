# AnyTTY Cloud 部署与升级

本文面向现有 Controller、海外 Edge 和国内 Edge 的原地升级。PostgreSQL、证书、签名密钥、Edge identity 和既有 enrollment 必须保留；升级不是重新安装，也不能覆盖 Edge 已完成 bootstrap 的 `config.yaml`。

## 拓扑与持久数据

| 节点 | 进程 | 持久数据 |
| --- | --- | --- |
| Controller | `anytty-cloud-controller`、PostgreSQL、Nginx | PostgreSQL、Controller/Edge CA、签名密钥、托管 Edge 证书 |
| 海外 Edge | `anytty-cloud-edge` | identity/public key、证书、desired config、KeyBundle、Relay journal |
| 国内 Edge | `anytty-cloud-edge` | 与海外 Edge 相同；公网域名使用已备案域名，例如 `cn1.anytty-edge.omscd.com` |

Controller 保存账号、配置和 daemon 状态真值。Edge 的 Presence 与策略投影在内存中，控制链断开时清空，重连后从 Controller snapshot 恢复。Edge 本地密钥、证书、KeyBundle 和未结 Relay journal 仍需持久化。

## 文件布局

```text
/opt/anytty-cloud-controller/releases/<FULL_GIT_COMMIT>/anytty-cloud-controller
/opt/anytty-cloud-controller/current -> releases/<FULL_GIT_COMMIT>
/opt/anytty/artifacts/releases/<FULL_GIT_COMMIT>/anytty-cloud-edge
/opt/anytty/artifacts/current -> releases/<FULL_GIT_COMMIT>
/etc/anytty/cloud/controller.env
/etc/anytty/cloud/{tls,pki,secrets}/
/var/lib/anytty-cloud-controller/certificates/

/opt/anytty-cloud-edge/releases/<FULL_GIT_COMMIT>/anytty-cloud-edge
/opt/anytty-cloud-edge/current -> releases/<FULL_GIT_COMMIT>
/etc/anytty-cloud-edge/config.yaml
/var/lib/anytty-cloud-edge/
```

[controller.env.example](controller.env.example) 和 [edge.config.example.yaml](edge.config.example.yaml) 只提供字段结构，不能直接用于生产。生产配置和秘密不得提交到仓库。

## 构建

在干净的 `master` 提交上构建。Cloud Web 会嵌入 Controller，所以先构建 Web：

```sh
npm ci
npm run build:cloud-web
./cloud/deploy/build-linux-amd64.sh
```

脚本会拒绝 tracked 或 untracked 变更，把干净工作区的完整 Git commit 注入 Controller 默认 Edge artifact version 和 Edge 上报版本。产物默认位于 `.artifacts/cloud-linux-amd64/`：

```text
anytty-cloud-controller
anytty-cloud-edge
anytty-cloud-controller.service
anytty-cloud-edge.service
SHA256SUMS
```

上传到每台 Linux 主机的 staging 目录后运行 `sha256sum -c SHA256SUMS`。release 目录只使用完整 commit，不使用可变的 `latest` 名称。

## 首次安装

1. 创建专用 `anytty` 和 `anytty-edge` 服务账号。
2. 创建 PostgreSQL 数据库、TLS/CA、四个独立用途的 Ed25519 签名密钥和 operator password file。账号 JWT 私钥保存为 `/etc/anytty/cloud/pki/account-token-signing-key.pem`，不得复用 config、artifact 或 binding 密钥。
3. 从 `controller.env.example` 创建 `/etc/anytty/cloud/controller.env`，权限设为 `0600`。
4. 安装 systemd 与 Nginx 模板，按实际域名调整证书路径。
5. 运行数据库 migration，再启动 Controller。
6. 在运营控制台创建 Edge，使用控制台返回的一次性安装命令完成 bootstrap。

首次注册后 Edge 会原子改写 `config.yaml`：清除 `bootstrap_token`，写入 Controller address、server name 和 config key ID。后续升级只替换二进制，不得重新写入示例配置或重新消费安装 token。正常生产安装以 Controller 返回并经过签名校验的安装脚本为准。

## 升级预检

### 协议版本

EdgeControl v8 不接受旧协议。先从当前日志、部署 commit 和运营控制台确认两台 Edge 是否已经运行 v8：

- 已经是 v8：可先升级 Controller，再逐台 Edge 验证。
- 任一节点仍是旧协议：这不是无中断滚动升级。先把新二进制分发到三个节点，在维护窗口内快速切换 Controller 和两台 Edge；切换完成前旧 Edge 会离线。

不要为了混合版本上线而增加协议兼容层。

### Schema 7 数据检查

先通过 root-only PostgreSQL service file 和 `PGPASSFILE` 连接，避免把数据库密码放进命令参数或 shell history：

```sh
export PGSERVICEFILE=/root/.pg_service.conf
export PGPASSFILE=/root/.pgpass
export PGSERVICE=anytty-production
chmod 0600 "$PGSERVICEFILE" "$PGPASSFILE"

psql -c 'SELECT version, applied_at FROM anytty_schema_migrations ORDER BY version'
psql -c "SELECT
  to_regclass('public.relay_usage_events') AS events_table,
  to_regclass('public.relay_usage_aggregates') AS aggregates_table"
```

`0007_account_credentials_relay_reservations.sql` 会删除旧的 `relay_usage_events`、`relay_usage_aggregates` 和 `usage_periods`，不会自动迁移其中的历史。migration runner 会在版本 7 前检查三张表，只要任一非空就 fail closed。如果 migration ledger 尚无版本 7：

1. 查询三个旧表的行数和业务保留要求。
2. 有数据时停止升级，先编写并用生产快照验证明确的数据转换或归档方案；不能直接运行 `--migrate`。
3. 确认旧数据可放弃时也要记录批准和归档结果，再显式清空三张旧表；否则 migration runner 仍会拒绝版本 7。

`0009_edge_deletion.sql` 不删除 Relay 历史，只解除历史记录对 Edge deployment 的外键占用。新 Relay reservation 仍会先锁定并校验有效 Edge；删除旧 Edge 后，历史记录继续保留原 Edge UUID 用于归因。

`0010_account_refresh_tokens.sql` 把账号登录表缩减为 Refresh Token 持久记录并删除 Access Token 摘要。现有 Refresh Token 摘要会保留，Web 在旧 Access Cookie 收到 401 后可轮换得到 JWT。执行迁移前必须已经创建账号 JWT 私钥、配置 `ANYTTY_CLOUD_ACCOUNT_TOKEN_KEY_ID`，并安装包含 `--account-token-signing-key` 参数的新 systemd 模板。

`0012_edge_identity_recovery.sql` 扩展 Edge 一次性 claim 的用途，用于已经无法通过 mTLS 的 EdgeIdentity 恢复。它不保存恢复 token 明文、证书正文或 Edge 私钥。

## 私密备份

以下示例必须在 Bash 中运行。它使用 `pipefail` 保留 `pg_dump`/`tar` 的失败状态，并用 `age` 直接加密输出，不在当前目录落明文 dump 或私钥包：

```sh
set -euo pipefail
umask 077
export PGSERVICEFILE=/root/.pg_service.conf
export PGPASSFILE=/root/.pgpass
export PGSERVICE=anytty-production
command -v age >/dev/null

backup_dir="/var/backups/anytty/$(date -u +%Y%m%dT%H%M%SZ)"
install -d -m 0700 "$backup_dir"
backup_recipient=REPLACE_WITH_AGE_RECIPIENT

pg_dump --format=custom | age -r "$backup_recipient" \
  >"$backup_dir/postgres.dump.age"
tar -C / -czf - etc/anytty/cloud var/lib/anytty-cloud-controller | \
  age -r "$backup_recipient" >"$backup_dir/controller-secrets.tgz.age"
test -s "$backup_dir/postgres.dump.age"
test -s "$backup_dir/controller-secrets.tgz.age"
```

每台 Edge 分别加密备份 `/etc/anytty-cloud-edge` 和 `/var/lib/anytty-cloud-edge`。备份后在隔离 PostgreSQL 和临时目录实际演练解密与恢复；只看到文件存在不算可恢复验证。

## 执行升级

### 1. Migration

只有 Schema 7 检查和恢复演练通过后才执行。先停止旧 Controller，避免它在检查与删表之间继续写入旧 schema。migration runner 会在同一事务中用 `ACCESS EXCLUSIVE` 锁住三张旧表、确认它们为空，再执行版本 7。Controller migration 从环境文件读取数据库 URL，不把密码放到命令行，并给生产数据显式超时：

```sh
set -a
. /etc/anytty/cloud/controller.env
set +a
systemctl stop anytty-cloud-controller
if ! /opt/anytty-cloud-controller/staging/anytty-cloud-controller \
  --migrate --startup-timeout=10m; then
  systemctl start anytty-cloud-controller
  exit 1
fi
```

超时值必须在生产快照演练中确认。migration 失败时恢复旧 Controller 并停止升级；成功后保持 Controller 停止，继续切换新 release。

### 2. Controller

首次从旧 `/opt/anytty/bin` 布局升级时，先把正在运行的 Controller 和旧 Edge artifact 保存为按内容摘要命名的 immutable release。已有 `current` symlink 时只记录它们的当前目标：

```sh
set -eu
if [ ! -e /opt/anytty-cloud-controller/current ] && [ -x /opt/anytty/bin/anytty-cloud-controller ]; then
  legacy_controller_sha="$(sha256sum /opt/anytty/bin/anytty-cloud-controller | awk '{print $1}')"
  install -d -m 0755 "/opt/anytty-cloud-controller/releases/legacy-$legacy_controller_sha"
  install -m 0755 /opt/anytty/bin/anytty-cloud-controller \
    "/opt/anytty-cloud-controller/releases/legacy-$legacy_controller_sha/anytty-cloud-controller"
  ln -sfn "/opt/anytty-cloud-controller/releases/legacy-$legacy_controller_sha" \
    /opt/anytty-cloud-controller/current
fi
if [ ! -e /opt/anytty/artifacts/current ] && [ -f /opt/anytty/artifacts/anytty-cloud-edge ]; then
  legacy_artifact_sha="$(sha256sum /opt/anytty/artifacts/anytty-cloud-edge | awk '{print $1}')"
  install -d -m 0755 "/opt/anytty/artifacts/releases/legacy-$legacy_artifact_sha"
  install -m 0755 /opt/anytty/artifacts/anytty-cloud-edge \
    "/opt/anytty/artifacts/releases/legacy-$legacy_artifact_sha/anytty-cloud-edge"
  ln -sfn "/opt/anytty/artifacts/releases/legacy-$legacy_artifact_sha" \
    /opt/anytty/artifacts/current
fi
readlink -f /opt/anytty-cloud-controller/current
readlink -f /opt/anytty/artifacts/current
grep '^ANYTTY_CLOUD_EDGE_ARTIFACT_VERSION=' /etc/anytty/cloud/controller.env
```

把上面三个旧值写进本次变更记录，然后安装新 release 和对应 Edge artifact：

```sh
version=REPLACE_WITH_FULL_GIT_COMMIT
install -d -m 0755 "/opt/anytty-cloud-controller/releases/$version"
install -m 0755 /opt/anytty-cloud-controller/staging/anytty-cloud-controller \
  "/opt/anytty-cloud-controller/releases/$version/anytty-cloud-controller"
install -d -m 0755 "/opt/anytty/artifacts/releases/$version"
install -m 0755 /opt/anytty-cloud-controller/staging/anytty-cloud-edge \
  "/opt/anytty/artifacts/releases/$version/anytty-cloud-edge"
ln -sfn "/opt/anytty-cloud-controller/releases/$version" \
  /opt/anytty-cloud-controller/current
ln -sfn "/opt/anytty/artifacts/releases/$version" \
  /opt/anytty/artifacts/current
```

把 `/etc/anytty/cloud/controller.env` 中 `ANYTTY_CLOUD_EDGE_ARTIFACT_VERSION` 设为同一个完整 commit。每次升级都安装随 release 分发的 systemd 模板，避免服务定义落后于二进制：

```sh
install -m 0644 /opt/anytty-cloud-controller/staging/anytty-cloud-controller.service \
  /etc/systemd/system/anytty-cloud-controller.service
systemctl daemon-reload
systemctl restart anytty-cloud-controller
curl --fail http://127.0.0.1:8081/healthz
curl --fail http://127.0.0.1:8081/readyz
```

同时检查 `journalctl -u anytty-cloud-controller`、Cloud 登录、`/docs` 和运营控制台 Edge 状态。

### 3. Edge

如果升级前已经是 v6，先海外、验证后再国内；旧协议升级则在同一维护窗口快速切换两台：

```sh
version=REPLACE_WITH_FULL_GIT_COMMIT
install -d -m 0755 "/opt/anytty-cloud-edge/releases/$version"
install -m 0755 /opt/anytty-cloud-edge/staging/anytty-cloud-edge \
  "/opt/anytty-cloud-edge/releases/$version/anytty-cloud-edge"
ln -sfn "/opt/anytty-cloud-edge/releases/$version" \
  /opt/anytty-cloud-edge/current
install -m 0644 /opt/anytty-cloud-edge/staging/anytty-cloud-edge.service \
  /etc/systemd/system/anytty-cloud-edge.service
systemctl daemon-reload
systemctl restart anytty-cloud-edge
```

验证日志、Controller 中 Edge online 状态和公网 ready 端点。Edge 公网证书由项目 Edge CA 签发，使用 bootstrap 保存的 CA：

```sh
curl --fail \
  --cacert /var/lib/anytty-cloud-edge/edge-ca.pem \
  https://REPLACE_EDGE_DOMAIN:REPLACE_EDGE_PORT/readyz
journalctl -u anytty-cloud-edge --since -10min
```

国内 Edge 额外确认 `cn1.anytty-edge.omscd.com` 解析到实际主机、证书 SAN 匹配域名，并从中国大陆网络测试 TCP/UDP Relay 端口。

### 4. 托管证书轮换

域名或证书内容变化时，在运营控制台替换当前 certificate profile；不要重新 bootstrap Edge。新证书的 SAN 只保留当前实际入口，不能为已废弃域名保留兼容 SAN。

1. 上传新证书链和匹配私钥，使用当前 profile revision 执行替换。
2. 等待目标 Edge 的 `desired_revision` 与 `applied_revision` 一致且结果成功。
3. 核对 `/var/lib/anytty-cloud-edge/managed-certificate.pb` 存在、权限正确并已更新。
4. 从公网读取握手证书，核对 SHA-256 指纹、有效期和完整 SAN；仅 `/readyz` 成功不能证明证书正确。
5. 重启 Edge 后再次核对 applied revision 和公网证书，确认没有回退到 bootstrap 证书。

严禁删除 `managed-certificate.pb` 或 bootstrap `public-cert.pem`。Edge 启动时先载入 bootstrap 证书，再由 managed state 覆盖；managed state 丢失会使进程继续使用 bootstrap 证书。需要清除旧 bootstrap SAN 时，必须先用同一 Edge 私钥重签并原子替换 `public-cert.pem`，随后重启和复验，不能通过删文件实现。

### 5. EdgeIdentity 自动轮换与恢复

Edge 连接 Controller 的 mTLS 身份证书由 Controller 的 Edge CA 签发，有效期 90 天。Edge 在剩余 30 天时通过已经认证的 EdgeControl v8 控制流自动生成新 P-256 私钥和 CSR；Controller 只收到 CSR。新证书和新私钥写入 `/var/lib/anytty-cloud-edge/managed-identity.pem` 后才切换后续 TLS 握手，当前控制流和已建立的手机连接不会因此中断。

正常轮换不需要运营操作，也不要重新 bootstrap。若证书已经过期，Edge 无法建立 mTLS 控制流，使用管理员最近认证后创建的 10 分钟一次性 recovery token：

1. 在目标主机停止 `anytty-cloud-edge`，并确认运营控制台显示该 Edge 离线且仍为启用状态。
2. 在运营控制台 Edge 的“配置与审计”页选择“恢复身份证书”并填写原因；等价 API 是 `POST /api/operator/edges/<edge-id>/identity-recovery`。该操作仅允许最近认证的 admin，token 只出现一次。
3. 把响应中的 token 写到 `/etc/anytty-cloud-edge/config.yaml` 的顶层字段 `identity_recovery_token`，保持文件权限 `0600`，然后启动 Edge。
4. Edge 本机生成新私钥，经 `POST /api/install/recover-identity` 消费该 token，严格校验证书的 CA、URI 身份、指纹和有效期，原子写入 `managed-identity.pem`，并自动从配置中清除 token。
5. 确认 Edge 恢复在线，检查 `managed-identity.pem` 权限为 `0600`，并在运营审计中核对 recovery create、consume 和 issuance 记录。

token 绑定 Edge 和本次 CSR，只能使用一次，10 分钟后失效。恢复失败时生成新的 token；不要复制其他 Edge 的 identity 文件，也不要上传或传输 Edge 私钥。完整边界见 [Edge 身份证书](../../docs/EDGE_IDENTITY_CERTIFICATES.md)。

### 6. Edge 生命周期

在运营控制台的 Edge“配置与审计”页执行生命周期操作：

1. 停用 Edge。Controller 会撤销当前控制连接，之后的重连会在准入阶段被拒绝。
2. 需要重新使用时选择恢复，Edge 可以继续使用原有身份和配置重连。
3. 永久退役时等待状态变为“已停用、离线”，再填写原因并删除。删除会清理 deployment、配置版本、未消费安装凭据和证书绑定；以后必须创建新的 Edge 并重新 bootstrap。

不要把停用当成远程关闭 systemd 服务。停用控制 Cloud 准入；退役主机时仍应在目标服务器停止并移除 `anytty-cloud-edge` 服务。

## 验收

- Controller `/healthz` 和 `/readyz` 均为 200。
- 两台 Edge 都以目标完整 commit 上报 online，desired config 已应用。
- Cloud Web 首页、`/docs`、登录和 daemon 管理页可访问。
- 现有 daemon 重新出现在 binding 指定的 Edge；Local、SSH、Direct 不受 Cloud 升级影响。
- 新 Cloud session 能优先 P2P，并在需要时建立受配额控制的 Relay。
- 阻断会关闭现有 Cloud session 并拒绝新连接；恢复在 daemon ACK 后生效，无需用户手工重连。
- 删除会让 daemon 清理 enrollment，旧凭据无法重新注册。

## 回滚

发布前记录 Controller、Controller 使用的 Edge artifact、artifact version 和每台 Edge 的上一 release 绝对路径。数据库 schema 未变化时，按记录恢复所有关联项，不能只回滚 Controller symlink：

```sh
ln -sfn REPLACE_PREVIOUS_CONTROLLER_RELEASE /opt/anytty-cloud-controller/current
ln -sfn REPLACE_PREVIOUS_EDGE_ARTIFACT_RELEASE /opt/anytty/artifacts/current
# 将 controller.env 中 ANYTTY_CLOUD_EDGE_ARTIFACT_VERSION 恢复为记录的旧值。
systemctl restart anytty-cloud-controller

ln -sfn REPLACE_PREVIOUS_EDGE_RELEASE /opt/anytty-cloud-edge/current
systemctl restart anytty-cloud-edge
```

执行 migration 后不承诺旧二进制兼容新 schema。完整回滚必须停止 Controller，恢复已演练的 PostgreSQL 备份、Controller 密钥/证书和对应 release，再依次启动 Controller 与 Edge。不要在新旧 Controller 同时写同一个数据库时恢复快照。

## 模板说明

- [systemd/anytty-cloud-controller.service](systemd/anytty-cloud-controller.service) 使用 immutable release symlink，默认关闭 development payment adapter。
- [systemd/anytty-cloud-edge.service](systemd/anytty-cloud-edge.service) 允许 Edge bootstrap 首次原子改写配置文件。
- [nginx/cloud.anytty.com.conf](nginx/cloud.anytty.com.conf) 只信任本机反向代理，并覆盖客户端提交的 forwarding headers。
- [nginx/renew-cloud.anytty.com.sh](nginx/renew-cloud.anytty.com.sh) 对应当前 Docker Nginx/Certbot 布局，迁移宿主布局时必须同步修改。
