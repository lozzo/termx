# 私有 Artifact 与企业交付门禁

状态：LIC001 工程发布门禁

本文件定义 Cloud Companion、Official App 私有模块、托管服务和 Enterprise bundle 的最低分发条件。它不构成最终 EULA、服务条款或企业合同；法定主体和目标法域未确认前，不在仓库内虚构可签署合同。

## 1. Artifact 边界

| Artifact | 交付模型 | 自有代码授权 | 必须附带 |
| --- | --- | --- | --- |
| `termx-cloud` Companion | 单独签名 executable/package；可由 meta package 依赖 | 终端用户专有软件条款 | 第三方 notices、版本、hash、签名、SBOM、隐私说明 |
| Official App private module | 只进入官方签名移动包 | Official App 用户条款；不得覆盖 public component 权利 | public Apache 文本、全部 npm/Android/native/font notice、商店披露与隐私政策 |
| managed cloud service | 官方托管，不下发服务端源码 | 服务条款/订阅条款 | 隐私政策、计费与退款披露、数据处理说明、适用 SLA |
| Enterprise bundle | 私有 registry/image/Helm/ops assets | 双方签署商业协议 | OSS notices、SBOM、镜像摘要、许可计量、支持/SLA、安全和数据附件 |

## 2. Sidecar IPC 结论

`termx` 与 `termx-cloud` 是独立 executable、独立升级和独立签名，public process 不 import、link、embed 或动态加载 private module。这个边界减少源码和发布耦合，也让 Community 用户无需安装付费组件。

它不是“只要进程外就自动可以闭源”的法律规则。当前 future public snapshot 选择 permissive Apache-2.0，因此 private Companion 不依赖 copyleft linking 例外；如果公开许可证、IPC 形态或 Official App 组合分发发生变化，必须重新审查。

公开 IPC contract 允许第三方兼容实现。官方签名只证明 publisher 和更新来源，不能把公开 contract、DeviceIdentity、CapabilityGrant、WebRTC 或 terminal protocol 变成私有授权面。

## 3. Consumer Release Gate

`termx-cloud` 或 Official App 首次对外分发前，release approver 必须记录：

1. 经确认的法定发布主体、版权名称、联系渠道和目标国家/商店。
2. 经专业审核的 EULA/用户条款、隐私政策、订阅/退款披露和第三方 notice bundle。
3. artifact version、SBOM、hash、publisher signature、provenance 与 malware scan 结果。
4. 账号、设备 metadata、网络质量 summary、账单和日志的收集/保留/删除规则。
5. public/free local/SSH/direct 能力不因未接受 private 条款而失效的验证结果。

任何一项缺失时，源码可以继续开发和测试，但不得把 artifact 标记为 production release。

## 4. Enterprise Contract Gate

Enterprise bundle 不通过个人 `termx cloud install` 下发，不以 README、invoice 或口头承诺代替商业授权。至少需要书面确定：

- 被许可主体、关联方、部署环境、region、节点/并发/流量等计量口径和期限。
- self-host server image、升级、备份、灾备、漏洞修复、支持时间和 SLA/SLO。
- 客户数据归属、processor/controller 角色、DPA、subprocessor、保留与删除。
- 审计日志、遥测、远程支持访问和 incident notification 边界。
- OSS 第三方条款继续有效，客户不得被要求放弃上游许可权利。
- 终止后的镜像使用、数据导出、密钥轮换和支持退出安排。

不同客户需要的 SSO、合规认证、出口控制或行业附件属于销售和法律审批，不写死在 public protocol。

## 5. Release Record

私有发布审批记录至少绑定：source commit、build workflow run、artifact digest、SBOM digest、notice digest、signing key ID、条款版本、批准人和时间。记录保存在受控发布系统，不把合同、客户信息或签名私钥提交到未来 public repository。
