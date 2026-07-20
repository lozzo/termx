# TermX Cloud Development Supervisor

`termx-cloud-dev` 是 development-only 多进程 supervisor。

它会构建并启动：

- 一个 `termx-cloud-controller` 进程；
- 两个具有独立 Hub/Relay identity 的 `termx-cloud-edge` 进程。

Controller、Edge 配置和 runtime manifest 都写入指定 artifact 目录。Hub policy、assignment、Presence 和 signaling 不在 Edge 落盘；每个 Edge 只预留自己的 Relay usage outbox 路径。

旧的单进程 Control Plane + Hub + Relay runtime 已删除，不再提供 direct Go pointer fallback。

进程装配门禁：

```bash
make test-cloud-controller-edge
```

测试 manifest 包含三个独立 PID、Controller/Edge listener、每个子进程的 binary/config/manifest/log 路径、Controller SQLite 和 Edge usage outbox。精确故障注入与 assignment migration 在 `HUB007` 基于该 manifest 扩展。
