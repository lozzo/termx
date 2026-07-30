# AnyTTY

AnyTTY 是一个仍在开发、尚未发布的远程终端项目。仓库包含 Go CLI/TUI、Cloud Controller 与 Edge、共享 Web UI，以及移动客户端。

当前架构和连接边界分别见 [ARCHITECTURE.md](ARCHITECTURE.md) 与 [CONNECTION_ARCHITECTURE.md](CONNECTION_ARCHITECTURE.md)。当前开发范围、执行顺序和验收基线以 [workflow.md](workflow.md) 为准。

## 开发验证

安装仓库锁定的工具链和 npm 依赖后运行：

```sh
npm ci
make doctor
make test
npm test
npm run typecheck
npm run build
```

`make build` 构建 Go CLI，`make build-cloud` 构建 Cloud Web、Controller 和 Edge。仓库生成的发布前构建产物统一写入 `.artifacts/`；Android 完整构建与边界验证使用 `make test-android`。

贡献和安全报告流程见 [CONTRIBUTING.md](CONTRIBUTING.md) 与 [SECURITY.md](SECURITY.md)，未发布变更记录在 [CHANGELOG.md](CHANGELOG.md)。
