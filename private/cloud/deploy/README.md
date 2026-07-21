# Muxvia Cloud bootstrap staging deployment

本目录使用 systemd 装配一个 Controller 和两个独立 Edge，不合并 Control Plane、Hub、Relay 或 Web 的领域真值。Go 二进制直接运行，Nginx 负责 TLS/SNI；本部署不引入 Muxvia Docker 容器。

首轮公网环境是 bootstrap staging：真实 Supabase、HTTPS、账号、扫码、Hub 和 Relay 都走生产形态，但 daemon enrollment 仍是单账号一次性 development code，支付仍是测试 provider。它不能标记为正式商业生产。

## 部署单元

- `155.94.155.192`：Controller、Web Controller、Edge primary、UDP Relay primary。
- `114.66.58.243`：Edge secondary、UDP Relay secondary。
- Supabase：`muxvia_staging` 独立 schema。

`build-bundle.sh` 交叉编译 Linux/amd64 二进制并打包 Web 与 catalog。Controller 和 Edge 使用独立 systemd unit；两台服务器都使用专用 `muxvia` 用户，配置保持 `0600`，运行状态只写 `/var/lib/muxvia/`。

## 端口

- Controller public/operator/control：仅 host loopback `42001-42003/tcp`。
- Edge Hub/health：仅 host loopback `42101-42102/tcp`。
- 两台 Relay：`41003/udp`。
- 155 HTTPS：`443/tcp`，按 hostname 分发。
- 114 secondary Hub HTTPS：`41102/tcp`，避免占用现有 FRP `443/tcp`。

## Secret 边界

- `MUXVIA_CONTROLLER_POSTGRES_DSN` 只传给一次性 bootstrap 命令。
- `controller-config.json` 和 `credentials.json` 必须保持 `0600`，不得提交或进入镜像。
- Edge config 不包含 PostgreSQL 密码，但包含独立 control private key，同样按 secret 处理。
- bootstrap 生成器只接受空输出目录和新 schema；失败会删除本次新 schema。

## 当前后置项

- R2 age 加密备份与独立恢复。
- 正式 daemon enrollment 创建/撤销入口。
- 真实支付 provider、邮件验证与密码找回。
- Android production origin/signing 和完整 APK E2E。
