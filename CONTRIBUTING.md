# 贡献指南

AnyTTY 仍处于未发布开发阶段。开始修改前先阅读 [workflow.md](workflow.md)，并以 [ARCHITECTURE.md](ARCHITECTURE.md) 和 [CONNECTION_ARCHITECTURE.md](CONNECTION_ARCHITECTURE.md) 中的当前边界为准。

## 环境

仓库当前使用 Go 1.26.5、Node.js 24、Java 21 和 protoc 35.1。生成代码检查还需要 `protoc-gen-go` 1.36.11、`protoc-gen-go-grpc` 1.6.2、根 workspace 的 npm 依赖，以及包含 `apkanalyzer` 的 Android SDK。先在仓库根目录运行 `npm ci`，再运行 `make doctor` 验证环境和生成代码。

## 构建与测试

提交前运行与改动相关的测试，并至少保持以下入口可执行：

```sh
make doctor
make test
npm test
npm run typecheck
npm run build
```

涉及 Android 时运行 `make test-android`；涉及全部产品构建时使用 `make test-all`。不要提交 `.artifacts/`、APK、AAB、Gradle 输出或其它本地构建产物。生成契约发生变化时，同时提交源文件、生成代码和对应测试基线。

提交应只包含一个明确范围，说明实际行为变化和已运行的验证。安全问题不要进入公开讨论，按 [SECURITY.md](SECURITY.md) 私下报告。
