# Muxvia Cloud Development Supervisor

`muxvia-cloud-dev` 是 development-only 多进程 supervisor。

它会构建并启动：

- 一个 `muxvia-cloud-controller` 进程；
- 两个具有独立 Hub/Relay identity 的 `muxvia-cloud-edge` 进程。

Controller、Edge 配置和 runtime manifest 都写入指定 artifact 目录。Hub policy、assignment、Presence 和 signaling 不在 Edge 落盘；每个 Edge 只预留自己的 Relay usage outbox 路径。

旧的单进程 Control Plane + Hub + Relay runtime 已删除，不再提供 direct Go pointer fallback。

进程装配门禁：

```bash
export MUXVIA_DEV_POSTGRES_DSN='postgres://127.0.0.1:5432/postgres?sslmode=disable'
make test-cloud-controller-edge
```

测试 manifest 包含三个独立 PID、Controller/Edge listener、每个子进程的 binary/config/manifest/log 路径、Controller 数据库引擎和 Edge usage outbox。PostgreSQL DSN 只写入 0600 Controller config，不进入 manifest。精确故障注入与 assignment migration 在 `HUB007` 基于该 manifest 扩展。
