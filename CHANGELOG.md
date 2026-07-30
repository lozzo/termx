# 变更记录

本文件记录尚未发布的开发变更。当前没有公开发布版本；项目状态和切片边界见 [workflow.md](workflow.md)，架构背景见 [ARCHITECTURE.md](ARCHITECTURE.md) 与 [CONNECTION_ARCHITECTURE.md](CONNECTION_ARCHITECTURE.md)。

## Unreleased

### Added

- 增加仓库入口、贡献、安全报告和变更记录文档。
- 增加 repository layout guard 行为 fixtures 和根 workspace 脚本覆盖测试。

### Changed

- repository layout guard 改为检查必要文档、错误路径和构建产物，不再限制可跟踪的 Markdown 文件集合。
- 根 typecheck、test 和 build 入口统一覆盖 UI、Mobile 与 Cloud Web，并由 Linux CI 运行 repository doctor。
