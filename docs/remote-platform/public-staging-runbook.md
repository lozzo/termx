# 公网 Cloud staging 状态

## 当前结论

旧的单进程公网 staging 装配已经退役，本文不再提供可执行运维步骤。

旧装配同时承载 Control Plane、Hub 与 Relay，并依赖 Hub policy 文件 snapshot；该模型与当前双二进制架构冲突，不得继续部署或作为测试真值。

当前 development 唯一入口是：

```bash
make cloud-dev
```

它启动：

- 一个 `termx-cloud-controller`；
- 至少两个 `termx-cloud-edge`；
- 每个 Edge 内组合纯内存 Hub 与 Relay；
- Controller 与 Edge 通过真实 Proto HTTP control stream 通信。

Hub 不持久化 policy、assignment、Presence、signaling、topology 或 command dedupe。Edge 只允许 Relay usage outbox 落盘，重启后必须重新取得 full projection。

公网 staging、HTTPS、正式 identity 配置、稳定域名、systemd unit、Nginx 和真实 provider 属于 `CLOUDP008` Production Cloud 装配范围。在该切片开始前，不得恢复旧 `termx-staging-cloud.service`、旧单进程参数或 `hub-policy.snapshot`。

历史部署细节仍可从 Git 历史查阅，但不能覆盖 `workflow.md`、`cloud-product-spec.md`、`multi-hub-control-topology-spec.md` 和 `multi-hub-technical-plan.md` 的当前结论。
