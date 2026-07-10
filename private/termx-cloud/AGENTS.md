# 私有云服务代理说明

## 范围

- 本目录保存当前 private monorepo 中不进入未来公开快照的官方云服务实现。
- `control-plane`、`web-controller`、后续 Hub、Relay 和 route-planner 必须保持独立 Go module 与部署边界。
- 私有实现可以依赖 public contract；public namespace 不得反向依赖本目录。

## 安全边界

- Control Plane 只拥有账号、设备目录、entitlement、managed session、服务准入票据、Relay 租约和 usage 结算。
- 本目录不得保存或解释 CapabilityGrant、terminal scope、terminal inventory、history、输入、屏幕内容或 daemon 私钥。
- 订阅和套餐只能决定新的付费服务准入，不得修改 daemon terminal authorization，也不得按 heartbeat 踢掉 daemon。
- Hub admission、Relay lease 和 usage event 必须使用彼此隔离的签名输入，不能复用旧 session token、长期 TURN secret 或 caller-selected algorithm。

## 实现纪律

- 新导出声明和关键消息链路写清晰中文注释。
- 服务之间通过显式领域 contract 或签名凭据协作，不直接 import 其他服务的数据库模型。
- 手工修改使用 `apply_patch`，每个 module 运行 `go test ./... -count=1`，提交前运行 `git diff --check`。
