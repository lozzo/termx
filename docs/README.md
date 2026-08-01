# AnyTTY 文档

本目录只保存当前代码仍然成立的稳定行为。一次性整改计划、Agent 审核记录、提交清单和已经完成的设计草案不作为长期文档保留。

## 产品与开发

- [项目 README](../README.md)：产品边界、快速开始、命令、构建和测试入口。
- [架构](../ARCHITECTURE.md)：组件、信任、持久化、资源与故障边界。
- [贡献指南](../CONTRIBUTING.md)：开发环境、变更顺序和提交门禁。
- [安全策略](../SECURITY.md)：私密报告渠道和敏感数据要求。
- [变更记录](../CHANGELOG.md)：尚未发布的用户可见变化。

## 稳定协议

- [终端实时画面与历史](TERMINAL_DELIVERY.md)
- [扫码配对](PAIRING_PROTOCOL.md)
- [Cloud daemon 生命周期](CLOUD_DAEMON_LIFECYCLE.md)
- [TUI 完整配置模板](../tui/docs/tui-v3.example.yaml)

## 运维

- [Cloud 构建、部署、升级与回滚](../cloud/deploy/README.md)

## 文档维护

行为变化必须在同一个提交中更新相关文档。文档不能把未实现方案写成当前能力，也不能继续保留已经完成的“当前问题”或阶段状态。跨协议变更按 `schema -> generated code -> runtime/store -> API/client -> tests -> docs` 更新。
