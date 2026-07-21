# PG004 公网 bootstrap staging 部署证据

## 结论

2026-07-21 已完成一个真实 Supabase、一个 Controller 和两个独立 Edge 的公网 bootstrap staging。服务使用专用 `muxvia` 用户和 systemd 运行，没有 Muxvia Docker runtime。

该环境已经支持 Web 注册/登录、账号中心、移动端扫码二维码创建、operator 登录与双 Edge fleet 查询。它仍不是正式商业生产：daemon enrollment 还是单账号一次性 development code，Android/Companion 尚未接入 HTTPS production origin，支付仍为测试 provider，R2 备份恢复尚未执行。

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

浏览器截图保存在本地 ignored artifact：

```text
.artifacts/cloud-deploy/e2e/account.png
.artifacts/cloud-deploy/e2e/operator.png
```

## 当前限制

1. `httpapi` development manifest 仍只接受 canonical HTTP；Official Android 非 development 构建仍 fail closed。因此当前 APK 不能直接连接这套 HTTPS staging，必须在后续 Android production origin/TLS 切片处理。
2. daemon enrollment 是 bootstrap 单账号一次性 code，Controller 重启会重建该 development flow；不能开放给任意注册账号。
3. 当前部署 credential window 到 `2026-08-20T15:41:28Z`。到期前必须完成正式 key 配置/轮换或重新生成 staging 资产。
4. Let's Encrypt 证书到期日为 2026-10-19；当前已删除临时 Cloudflare credential，自动续期尚未配置。
5. R2 age 加密上传和独立恢复仍是 PG004 的未完成门禁。
6. 尚未执行 Android managed P2P/Relay terminal/file E2E，也未接入真实支付、邮件验证或密码找回。

## 仓库门禁

```text
go test -timeout 90s ./private/cloud/controller ./private/cloud/edge ./private/cloud/devcloud/cmd/muxvia-cloud-bootstrap
private/cloud/deploy/build-bundle.sh
git diff --check
```

测试同时修复了 `postgrestest` 在 PostgreSQL 不可用时持锁调用 `t.Skipf` 导致后续测试死锁的问题；现在会先释放 fixture mutex 再 skip。
