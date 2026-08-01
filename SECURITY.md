# 安全策略

AnyTTY 尚未发布，目前没有受支持的公开版本、漏洞奖励或响应时限承诺。安全边界以 [ARCHITECTURE.md](ARCHITECTURE.md)、[配对协议](docs/PAIRING_PROTOCOL.md) 和 [Cloud daemon 生命周期](docs/CLOUD_DAEMON_LIFECYCLE.md) 为准。

## 私密报告

不要创建公开 issue、discussion 或包含漏洞细节的公开 pull request。当前唯一受支持的入口是 [GitHub 私密漏洞报告](https://github.com/lozzo/termx/security/advisories/new)。仓库转为公开或发布产品前，维护者必须确认该入口已启用并配置受监控的备用安全邮箱；在此之前不要向未经确认的邮箱发送漏洞细节。

报告应包含：

- 受影响提交和组件。
- 最小复现、前置权限和实际影响。
- 是否涉及凭据泄露、越权、远程执行、数据破坏或资源耗尽。
- 已知缓解措施和建议的修复方向。

不要附带真实私钥、密码、pairing claim、CapabilityGrant、生产数据库、个人数据或终端内容。需要样本时使用新生成的测试凭据和脱敏数据。

## 安全边界

- daemon 是 terminal、file、DeviceIdentity、AccessStore 和客户端授权的最终所有者。
- Controller 管理账号、注册和策略真值；它不能签发 terminal/file 权限，也不接收终端内容。
- Edge 做公网准入、信令和 Relay；Edge 策略不能提升 daemon 已授予的能力。
- App 不登录、不自动发现设备，只接受用户主动扫描的一次性 pairing offer。
- AgentGateway 和 ClientGateway 使用 challenge-first 握手；身份、proof、locator 和 generation 必须绑定当前连接上下文。
- Cloud 策略状态缺失或 Controller stream sequence 不连续时 fail closed；per-daemon `state_revision` 只要求单调，不要求逐一连续。

## 敏感数据

以下内容不得写入日志、截图、测试 fixture、错误响应或版本控制：

- 私钥、完整证书凭据和数据库连接密码。
- pairing URI/claim、enrollment code、bootstrap token。
- CapabilityGrant、ClientAccess credential、Cloud issuer material。
- terminal/file 内容和未经脱敏的生产标识。

生产密钥、证书和密码文件应由专用服务账号以最小文件权限读取。部署模板只允许放占位符；升级前必须备份数据库、证书和密钥目录。

## 修复与披露

维护者确认后在当前开发主线修复并记录验证。由于没有公开发布版本，当前不维护历史发行分支的补丁或兼容升级路径。是否公开披露、何时轮换凭据和是否需要清理测试/生产数据，由受影响范围确认后单独决定。
