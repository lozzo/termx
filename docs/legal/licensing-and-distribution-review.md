# TermX 许可证与闭源分发审查

状态：LIC001 工程审查基线

日期：2026-07-11

本文件记录源码和 artifact 的许可边界、第三方 notice 责任与发布阻断条件。它不是法律意见；首次对外发布、建立收费合同或进入新法域前，仍需由具备资质的专业人员审核实际 artifact、条款和主体信息。

## 1. 决策

### 1.1 当前 private monorepo

- 根 `LICENSE` 为保留全部权利的 private monorepo notice。
- “计划未来开源”的目录在当前仓库内不自动获得开源授权。
- `private/`、内部配置、部署资产、签名材料、云服务实现和当前 Git 历史不得进入 public snapshot。
- 第三方代码和资产继续适用其原始许可证；根 notice 不覆盖或收回上游权利。

### 1.2 Future public snapshot

- 公开仓库根许可证选择 Apache License 2.0，模板位于 `docs/legal/public-snapshot/LICENSE`。
- 选择理由是允许商业使用与再分发、包含明确专利授权，并且不会把与公开 contract 互操作的独立私有服务强制置于同一 copyleft 条款下。
- Apache-2.0 不授予 TermX 名称、标识或服务商标权利；品牌政策在正式公开前单独确认。
- public snapshot 必须同时复制 `NOTICE`、`public-snapshot/THIRD_PARTY_NOTICES.md`、`DCO` 与 `CONTRIBUTING.md`，并从空 Git 仓库建立新历史。

### 1.3 Contributions

- 初始公开治理使用 DCO 1.1 和每 commit `Signed-off-by`。
- 初始阶段不要求 CLA，避免在没有明确版权主体和签约流程时收集无效协议。
- 如果未来需要双重许可、版权转让或企业 CLA，必须先形成新的治理决策，不能把既有 DCO contribution 默认为已转让版权。

## 2. Artifact 许可矩阵

| Artifact | 自有代码许可 | 必须随附 | 禁止项 |
| --- | --- | --- | --- |
| public source snapshot | Apache-2.0 | LICENSE、NOTICE、DCO、CONTRIBUTING、third-party inventory 与保留的上游 license | `private/`、内部历史、secret、不可再分发资产 |
| public `termx` binary/package | Apache-2.0 | Apache LICENSE、NOTICE、精确构建版本的第三方 license bundle、SBOM/provenance | 只给下载链接而不提供许可文本；未审计依赖 |
| Community App | Apache-2.0 public App | Web/npm/Android/native/font/WebRTC notice assets、商店或包内可访问入口 | private cloud module、遗漏 WebRTC 原生第三方清单 |
| Official App | public Apache-2.0 组件 + proprietary official module | public Apache LICENSE/NOTICE、全部第三方 notice、适用的用户条款和隐私政策 | 把整个 APK 描述为纯 Apache；把 public grant/DeviceIdentity 逻辑改成私有授权 |
| `termx-cloud` Companion | proprietary | proprietary distribution terms、内嵌或同包第三方 notices、版本/SBOM/provenance、签名 | 暗示 Apache-2.0 授权 private binary；无 notice 的单 binary 分发 |
| managed Control Plane/Hub/Relay | proprietary hosted service | 内部 SBOM、部署镜像 notices、供应链记录；对用户提供适用服务条款/隐私政策 | 把 terminal grant 或用户内容纳入云服务所有权 |
| Enterprise bundle | proprietary commercial delivery | 双方书面协议、许可计量范围、支持/SLA、OSS notices、SBOM、镜像摘要、出口/隐私/安全附件（按需） | 仅靠 README 形成商业许可；混入未授权第三方 source/binary |

## 3. Cloud Companion 与 IPC

Cloud Companion 使用独立 executable、独立签名和 versioned local protobuf IPC。public namespace 不 import、link、embed 或动态加载 private module；public process 可以使用 fake 或其他实现完成公开 contract 测试。

该工程边界支持独立许可和独立交付，但“进程外 IPC”本身不是法律结论。当前 public snapshot 选择 permissive Apache-2.0，因此没有依赖 copyleft linkage 例外来维持闭源 Companion；如果未来改变 public license，必须重新审核 IPC、Official App 组合分发和 SDK linking。

公开 contract 不包含 private service implementation、账号 token、CapabilityGrant、DeviceIdentity private key、DataChannel 或 terminal payload。官方 release root 只证明 publisher，不赋予 private source 使用权，也不能限制第三方实现兼容 public contract。

## 4. Third-Party Review

当前实际入口扫描结果见 `third-party-inventory.md`：

- public CLI 与 private Companion 的 Go 三平台 graph 只发现 MIT、BSD-2-Clause、BSD-3-Clause 和 Apache-2.0；完整文本已分别嵌入两个 binary package。
- 两个 production npm lockfile 当前覆盖 117 个精确 package/version，只发现 MIT、ISC、0BSD、Unlicense、BlueOak-1.0.0、BSD-3-Clause、Apache-2.0 及一个 Apache-2.0 AND BSD-3-Clause 组合项。
- Android `releaseRuntimeClasspath` 当前解析出 55 个 Maven component，分类为 Apache-2.0、MIT 和单独固定的 WebRTC composite bundle。
- `termx-vterm/internal/vt` 保留 Charmbracelet MIT 文件；App 字体保留 OFL-1.1 attribution。
- `io.github.webrtc-sdk:android:125.6422.07` 的 Maven AAR 没有内置 notice，且 POM 的 BSD-3-Clause 与 wrapper repository 根 MIT 文本不同。对应 tag `v125.6422.07` 提供完整 `Licenses/WEBRTC.md`，发布必须按固定 commit 和 SHA-256 获取并随 App 分发。

自动扫描不能覆盖汇编、预编译 `.so`、字体、已构建 JS bundle 或没有 package metadata 的资产。CI 的自动报告是门禁输入，不是最终法律判断。

仓库统一审计入口是 `scripts/license-audit.sh`。它不会自动接受新 license、新 Maven group、WebRTC 版本变化或缺失文本；这些变化必须先更新清单并重新人工审查。

## 5. Release Gates

任一对外 source、binary、APK、container 或 enterprise bundle 发布前必须满足：

1. 从干净 checkout 解析实际 lockfile、Go build tags、Gradle resolved runtime graph 和 container packages。
2. license classifier 不含 unknown、noncommercial、source-available、restricted 或未经批准的 reciprocal 条款。
3. 生成并随 artifact 提供精确版本的 license/notice bundle；不能只提供 SPDX 名称或网页链接。
4. native/assembly/wasm/font/generated bundle 逐项有 provenance 与 notice；WebRTC pinned notice hash 必须匹配。
5. source snapshot 与 binary 执行 public/private dependency guard 和 secret scan。
6. artifact 生成 SBOM、hash、签名、provenance 和 malware scan 记录。
7. public release 使用已经确认的版权主体和品牌政策；private/enterprise release 使用已签署的适用商业条款。
8. Official App 具有适用的商店披露、用户条款和隐私政策；这些文档不由本工程许可文件替代。

任何一项失败都阻断对应 artifact，不允许通过删除 notice、改写 license 名称或把依赖移入 sidecar 来绕过。

## 6. External Sign-Off

首次公开或商业发布前必须由权利人确认：

- 对外使用的法定个人或实体名称、版权年份和签约主体。
- TermX 名称/标识的商标策略。
- Companion/Official App/Enterprise 的 EULA、服务条款、隐私政策、DPA、SLA 与出口合规需求。
- Apache-2.0 public snapshot、DCO 模式和当前第三方 notice bundle 在目标法域内可接受。

确认结果属于 release approval 记录，不应把合同、身份证明或签名私钥提交到 public repository。
