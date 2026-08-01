# 贡献指南

AnyTTY 仍处于未发布开发阶段。开始修改前先阅读 [README](README.md)、[架构](ARCHITECTURE.md) 和对应的 [专题文档](docs/README.md)。当前代码、协议、测试和稳定文档必须在同一个变更中保持一致。

## 环境

基础 CLI/TUI 开发使用 Go 1.26.5 和 Node.js 24（CI 基线），并需要根目录 npm 依赖：

```sh
npm ci
make build
```

完整仓库门禁还需要 Java 21、Android SDK、包含 `apkanalyzer` 的 build tools、protoc 35.1、`protoc-gen-go` 1.36.11 和 `protoc-gen-go-grpc` 1.6.2。运行以下命令检查工具版本、生成代码和仓库布局：

```sh
make doctor
```

## 变更顺序

跨层协议变更按以下顺序完成：

```text
proto/schema -> generated code -> store/runtime -> API mapping -> client/UI -> tests -> docs
```

- 复用现有包边界和小型具体类型；只有真实重复或复杂度需要时才提取抽象。
- 不为假设需求提前建立通用事件总线、兼容层、迁移适配器或兜底分支。
- 当前未发布，旧协议、旧 YAML 和开发数据库不做兼容；删除被替代的代码和文档。
- 防御检查应覆盖真实的权限、资源、并发和外部输入边界，不修复没有合理触发路径的理论问题。
- UI 改动必须同时验证桌面、窄屏、键盘焦点、触摸目标和无横向溢出；可见界面变化附真实截图。
- 不提交 `.artifacts/`、APK、AAB、`dist/`、Gradle 输出或其它本地构建产物。

## 测试

先运行改动直接影响的测试，再运行对应产品门禁：

```sh
# 全部 Go 包
make test

# UI、Mobile、Cloud Web 的生成、测试、类型检查与构建
make test-clients
npm run lint

# Android unit、release build 与 APK 边界
make test-android

# 已连接设备或模拟器上的 instrumentation 与历史 E2E
make test-android-device

# 顺序执行 Go、Web clients 和 Android release 门禁
make test-all
```

Cloud Web 的浏览器与无障碍测试：

```sh
npm run test:e2e --workspace @anytty/cloud-web
npm run test:axe --workspace @anytty/cloud-web
```

protobuf 变化后必须重新生成并检查差异：

```sh
npm run proto
scripts/check-generated-code.sh
```

## 提交

一个提交只承载一个可解释的行为范围。提交说明应写清用户可观察变化和关键验证；不要把格式化、生成物或无关重构混入功能提交。合并前确认 `git status` 中没有凭据、生产配置、截图中的敏感数据或本地构建产物。

安全问题不要创建公开 issue 或 pull request，按 [SECURITY.md](SECURITY.md) 私下报告。
