# Muxvia 多 Route 配对与客户端 Route 管理设计

## 文档状态

- 状态：`PAIRROUTE001` 已完成实现与 Android 验收。
- 目标：让首次配对和已配对设备的 Route 更新都以 daemon DeviceIdentity 为归并锚点，并允许 Direct、SSH、Muxvia Cloud 中任一路径成功即完成配对。
- 产品真值：本设计服从 `workflow.md`、`product-prd.md`、`security-protocol-spec.md`、`tui/docs/multi-endpoint-transport-plan.md` 和 Proto API 架构。
- 非目标：不增加自建 managed Cloud provider，不让 UI 拥有 Route/session truth，不改变 CapabilityGrant scope，不把 SSH secret、Cloud token、PairingTicket 或 grant 放入二维码。

## 1. 现场问题与结论

development Cloud 数据清空后，Mac daemon 仍保留旧 enrollment，手机和 Mac 同处 `192.168.123.0/24`。实机证明手机可连接 daemon Direct signaling `41120` 和 ICE-TCP `41121`，但 `pair create` 因 daemon runtime record 的 `CloudEnabled=true` 把 Cloud 放在 Route 列表首位。`PairingClaimOfferV1` 只保留第一个 seed，App 因而只请求 Cloud resolve/Relay，最终得到 `client session is unavailable`，没有尝试可达 Direct。

这不是局域网、相机或二维码解析失败，而是三个模型错误：

1. “Cloud 功能已启用”被误当成“当前 enrollment + assignment + Presence 可用于配对”。
2. 完整 bundle 支持多个 Route，但短码 bootstrap 只允许一个 Route，形成首连单点故障。
3. `pair create` 的 Direct 零散 flags、隐式 Cloud 和 App 的后续连接策略没有形成同一个 Route 配置模型。

产品结论固定为：

- 零参数 `pair create` 生成当前 daemon 可证明可用的有界 Route seed 集合；Direct 与 Cloud 都可用时由 Go Client Engine 竞速。
- 显式、可重复 `--route` 只限定本次配对允许使用和安装的 Route；任一路径成功即进入同一个端到端 pairing DataChannel。
- 已配对设备再次扫描同 DeviceIdentity 的二维码时执行 Endpoint Route diff/merge，不创建第二台设备，也不静默扩大权限。
- Endpoint 名称、Cloud 设备名称和 Route 名称具有不同作用域，任何名称都不参与身份、鉴权或归并。

## 2. 领域模型与真值

```text
Cloud device record
  DeviceIdentity fingerprint
  cloud_display_name

Client Endpoint
  EndpointID
  verified DeviceIdentity fingerprint
  client_label + label_source
  Route[]
  SelectionPolicy

Route
  route_id
  display_name
  kind-specific config
  enabled/manual_only/priority
```

归属规则：

- daemon DeviceIdentity/fingerprint 是跨 Cloud、pairing、share 和手工 Route 的唯一机器归并锚点。
- EndpointID 是当前客户端的本地稳定引用；不同客户端无需共享同一个 EndpointID。
- `route_id` 在一个 Endpoint 内稳定唯一；连接、优先级、诊断和更新引用 `route_id`，不引用名称或地址。
- IP、域名、SSH host、Cloud DeviceID、Endpoint label 和 Route display name 都不是身份。
- `client/endpoint` 和 `client/runtime` 分别拥有 Route 配置与连接生命周期；Android/TypeScript 只消费 generated Proto projection。

## 3. 名称作用域与冲突

三个名称必须在 UI 和 CLI 文案中明确区分：

| 名称 | owner | 作用域 | 更新影响 |
| --- | --- | --- | --- |
| Cloud device name | Control Plane device metadata | 一个 Cloud 账号 | Web 和未本地改名的客户端目录 |
| Endpoint label | 当前客户端 Endpoint registry | 单个客户端安装 | 只改变该客户端展示 |
| Route display name | 当前客户端 Endpoint Route | 单个 Endpoint | 只改变路径展示 |

Endpoint label 应用顺序保持 `Cloud < Bootstrap < Manual < User`：

1. 新 Endpoint 优先采用 pairing bundle 的 `suggested_label`。
2. bundle 没有名称时才采用 Cloud device name。
3. 用户在 App/CLI/TUI 改名后写 `label_source=USER`。
4. 后续 Cloud 同步、再次 enrollment、再次扫码或 Route 更新不得覆盖 USER label。
5. `pair create --label` 只提供接收客户端的建议 Endpoint 名称，不修改 Cloud device name。

Route 同名不影响安全，但 UI 应在同一 Endpoint 内提示重复；稳定引用始终是 `route_id`。Route 改名不触发新 generation；locator、credential descriptor 或 kind 改变才使后续连接使用新配置。

## 4. Route 分类

正式 Route kind 仍只有：

```text
local-unix
direct-webrtc-tcp
ssh-webrtc-tcp
managed-webrtc
```

- Local Unix 不进入移动配对。
- LAN 和 FRP/公网 TCP 映射都是 Direct Route，只是 `route_id`、locator 和 display name 不同。
- SSH 使用 Go SSH `direct-tcpip` 到 daemon loopback signaling/ICE-TCP；二维码只描述 host、用户、host-key pin 和 credential kind，不携带 secret。
- Muxvia Cloud 内的 P2P、Relay、TURN UDP/TCP 是 managed Route 策略和观察结果，不建立额外 Route kind。
- WebRTC application data 在所有远程 Route 上都经过 DTLS。若未来需要为 embedded signaling 增加 TLS listener，必须单独增加 Proto 字段和验收，不能把现有 `server_name` 文案写成已经具备 TLS 信任。

## 5. CLI 产品契约

### 5.1 默认行为

```bash
muxvia pair create --label "My Mac"
```

未提供 `--route` 表示 Auto：

1. daemon Direct listeners 已就绪且能投影至少一个非 wildcard locator 时加入 Direct LAN seed。
2. 只有当前 enrollment credential、owning Hub assignment 和 Presence session 均为 READY 时才加入 Cloud seed。
3. Direct 与 Cloud 同时可用时不人为制造优先级，Go Client Engine full race。
4. Cloud disabled、revoked、unauthenticated 或没有当前 Presence 时不加入 Cloud，但不能阻止 Direct。
5. 没有任何合格 Route 时拒绝创建 claim，并逐 Route 输出稳定原因。

Cloud READY 由 daemon managed runtime 投影，不能由 CLI 读取 runtime record 的 `CloudEnabled` 猜测。网络瞬时失败可标记 temporarily unavailable；明确 revoked/unauthenticated 必须停止发布 Cloud seed，且不删除 DeviceIdentity 或 Direct/SSH 能力。

### 5.2 显式 Route

`--route` 可重复；显式顺序映射为配对优先级，较前 Route 先启动，后续 Route 按统一 hedge policy 启动：

```bash
muxvia pair create --route direct
muxvia pair create --route cloud
muxvia pair create --route direct --route cloud
```

日常手工输入优先使用普通参数。一个命令可以参数化一个 Direct 和一个 SSH Route，并可同时加入 Cloud：

```bash
muxvia pair create \
  --route direct \
  --direct-id frp \
  --direct-name "FRP Public" \
  --signaling-address frp.example.com:443 \
  --ice-tcp-address frp.example.com:444 \
  --server-name mac.example.com \
  --route cloud

muxvia pair create \
  --route ssh \
  --ssh-id office \
  --ssh-name "Office SSH" \
  --ssh-host mac.example.com \
  --ssh-port 22 \
  --ssh-user lozzow \
  --ssh-host-key SHA256:...
```

参数化 Route 的规则：

- `--route direct` 未提供 Direct locator flags 时表示自动 LAN locator。
- `--route ssh` 必须提供 `--ssh-host`、`--ssh-user` 和至少一个 `--ssh-host-key`。
- `--direct-*` 只允许同时存在一个参数化 Direct；`--ssh-*` 只允许同时存在一个参数化 SSH。
- 同一种 kind 需要多个实例时使用 URI；不得靠多个 slice flags 的位置隐式配对。

URI 作为脚本、配置生成器和同 kind 多实例的高级入口保留，使用 `net/url` 严格解析，不增加逗号 DSL 或 shell 拆分规则：

```bash
muxvia pair create \
  --route 'direct://lan'

muxvia pair create \
  --route 'direct://frp?name=FRP%20Public&signaling=frp.example.com:443&ice=frp.example.com:444&server-name=mac.example.com'

muxvia pair create \
  --route 'ssh://lozzow@mac.example.com:22?name=Office%20SSH&fingerprint=SHA256%3A...'

muxvia pair create \
  --route cloud
```

语义固定为：

- `direct` 是 `direct://lan` 的正式简写，不是兼容 alias。
- `cloud` 使用当前 daemon DeviceID 和有效 owning Hub session，不接受 Hub URL 或 token。
- `direct://ID` 的 ID 成为稳定 `route_id` 后缀；`signaling` 与 `ice` 必须同时出现或同时省略。
- `ssh://USER@HOST:PORT` 必须携带至少一个 host-key fingerprint；credential body 由接收平台安全存储提供。
- 一个命令最多产生 4 个 pairing seeds；重复 `route_id`、未知 query、空地址、wildcard、公钥 pin 缺失或显式不可用 Cloud 均在生成二维码前失败。
- `--label` 仍是 Endpoint suggested label；Route 的 `name` 只命名该 Route。

### 5.3 删除的旧入口

新入口接通的同一个切片内直接删除：

```text
pair create --no-cloud
v3RunningDaemonCloudEnabled
pair command 内隐式拼装 Cloud-first Route 的逻辑
```

`--signaling-address`、`--ice-tcp-address` 和 `--server-name` 保留为参数化 Direct 的普通字段，但必须与 `--route direct` 同时使用；删除它们原先“未声明 Route 也隐式覆盖唯一 Direct”的平行语义。当前尚未公开发布，不保留 deprecated flag、兼容解析、隐藏 fallback 或双写测试。daemon lifecycle 的 `--no-cloud` 是运行策略，不属于 pair Route 选择，不在本次删除范围。

## 6. Proto-first 修改

按仓库固定顺序实现：schema -> generated -> descriptor/security harness -> API Layer -> API Mapping -> daemon/core adapter -> Go Client Engine -> binding -> App。

### 6.1 Route schema

`EndpointRouteConfigV1` 增加稳定 Route display name。`route_id` 仍是机器引用，名称只用于展示。

`PairingRouteSeed` 增加：

- `route_id`
- `display_name`
- pairing priority/order
- SSH seed oneof

`PairingClaimOfferV1` 将单个 `route` 收敛为最多 4 个 `routes`。本项目未发布，不保留单 Route fallback 或双字段读取。

portable claim code 使用有版本的二进制 envelope：marker 区分 canonical raw protobuf 与 raw-DEFLATE。编码器只在压缩结果连同 marker 确实更短时选择压缩；解码器固定 4 KiB 输出上限、拒绝尾随数据和未知 marker，并在 Proto parse 后继续执行 canonical validation。解压只存在于共享 Go Client Engine；Android Kotlin/TypeScript 继续把 opaque `MXP1-...` 交给 binding，不复制 codec 或安全校验。

终端与 PNG 二维码使用 `go-qrcode` Low 等级，约 7% codeword recovery；当前 Medium 约 15%，库不存在“70%/95%容忍度”档位。Low 可以减少纠错 codeword 和 modules，但降低遮挡、污损和低清摄像头下的恢复能力。编码后仍必须满足既定二维码密度门禁；超限时 CLI 在输出前失败并建议减少 Route 或使用文本/PNG，不静默丢 Route。

### 6.2 API Layer

`ClientAccessTicketCreateRequest` 不再让 `cmd/muxvia` 直接构造最终 Route config。CLI 只提交解析后的 generated Proto Route intent；daemon application service 负责：

1. 解析默认 Auto 或显式 Route 限定。
2. 从 Direct runtime 和 managed Presence owner 获取当前可发布 projection。
3. 校验显式 Route 的 locator、Cloud READY 和 SSH pin。
4. 生成完整 signed bundle routes 与紧凑 seeds。
5. 把 bundle、ticket 和 claim 作为一个 daemon-owned事务登记。

CLI 不读取 Cloud credential、Presence map 或 AccessStore，不建立第二份 eligibility truth。

## 7. 首次配对运行链路

```text
App scan/manual input
  -> Go binding ImportPairing
  -> parse + validate all pairing seeds
  -> credential availability projection
  -> pairing attempt plan
  -> Direct / SSH / Cloud connectors
  -> first authenticated pairing DataChannel wins
  -> DeviceHello + DTLS channel binding + ClientAccessIdentity proof
  -> owning daemon redeems claim exactly once
  -> PairingAccepted(full signed bundle + client-bound grant)
  -> atomic Endpoint assemble + credential commit
  -> loser cancellation and late-session close
```

失败规则：

- 单个 Route 失败只记录该 attempt 的稳定错误，不结束整体配对。
- 第一条完整 authenticated pairing session 到达即返回；loser cleanup 不阻塞用户。
- 所有 Route 都失败后才返回整体失败，结果包含每个 `route_id` 的 phase、错误分类和恢复动作。
- 不允许 Cloud 失败隐式改写用户的强制 `--route cloud`；多 Route fallback 只发生在 claim 明确携带的 seeds 内。
- daemon identity、answer signature、DTLS binding 或 host-key pin 不匹配是安全失败；同一可疑身份下不得继续尝试会弱化验证的 Route。

## 8. 已配对设备的增量合并

再次扫码先验证 DeviceIdentity，再按 fingerprint 查找 Endpoint：

```text
same fingerprint
  -> existing Endpoint
  -> show Route diff
  -> confirm
  -> atomic merge

different fingerprint
  -> new Endpoint / identity warning
```

合并规则：

- 新 `route_id`：添加。
- 相同 `route_id` 且 canonical config 一致：幂等，无变更。
- 相同 `route_id` 且 locator/config 改变：展示 before/after，确认后替换。
- USER source Route 不被 bootstrap 静默覆盖；必须逐项确认。
- 未出现在新二维码中的旧 Route 默认保留；删除必须是显式操作。
- 原 EndpointID、USER label、terminal references 和无关 Route 保持不变。
- 新 Route 默认加入 Auto；显式二维码顺序只为新 Route 提供建议 priority，不能重排既有 USER priority。

权限规则：

- Route 更新不扩大 CapabilityGrant scope。
- 同一 Android 安装使用同一不可导出 ClientAccessIdentity key；daemon 识别同一 subject 后执行幂等授权或原子 grant rotation，不创建多个并行活跃授权。
- 请求 scope 超出现有授权时必须显示独立权限差异并重新确认；不能借 Route 更新静默提权。
- 原子提交失败时 Endpoint config、credential refs 和 grant 均保持旧状态。

## 9. Android 产品设计

入口固定为：

```text
Device details
  Connection & network
    Connection preference
    Current connection
    Routes
    Diagnostics
```

普通用户默认只操作 Auto。Routes 是渐进披露的高级页面，每行展示 display name、kind、可用性、优先级和最近一次显式测试结果。

支持动作：

- 添加 Direct LAN、Direct public mapping、SSH 或 Cloud Route。
- 编辑名称和 kind-specific 非秘密字段。
- 启用/禁用、测试、删除和调整优先级。
- 优先级提供可访问的上移/下移按钮，不以拖拽作为唯一交互。
- SSH credential 通过 Android Keystore/系统选择器准备；Cloud Route 只选择账号 profile 和 P2P/Relay/transport 策略。
- Route 测试必须完成 connector、daemon identity、remote auth admission 和 protocol Hello；TCP connect 只能作为诊断阶段，不能显示“可连接”。

扫码已有 Endpoint 时使用“更新连接方式”页面，而不是“添加设备”：

```text
发现已配对设备：我的开发机

+ FRP 公网直连
~ 公司 SSH 地址已更新

设备名称、现有授权和其他连接方式不会改变。
```

每个触控目标至少 48dp，错误显示在对应 Route 下并提供“重试/编辑/恢复 Auto”；支持中英文、最大字体、竖横屏、TalkBack 顺序和 reduced motion。UI 不展示 Cloud Hub URL、token、grant、credential ref 或未经证明的网络推断。

## 10. TUI 与 CLI 后续管理

TUI 已有共享 Go registry 的 Connections 页面和完整 priority 配置，本切片只增加同一 Proto Route 的 display name/编辑投影，不建设第二份 Route state。

配对之后的 Route 管理统一使用 `muxvia endpoint route ...` 与 TUI/App editor；`pair create --route` 只描述本次 bootstrap 和 daemon 建议安装的 Route。修改客户端 Route 不远程改变 daemon listener、FRP 服务或 SSH server。

## 11. 测试准入

### Proto 与安全

- descriptor 固定多 seeds、SSH seed、route ID/name/order，拒绝 ticket/scope/grant/secret。
- deterministic round-trip、最大 4 routes、重复 ID、未知 oneof、unknown query、payload/QR size。
- raw/DEFLATE 选择边界、压缩确定性、未知 marker、截断流、尾随数据、4 KiB 解压炸弹和 canonical re-encode。
- tamper：DeviceIdentity、Direct locator、SSH pin、Cloud target、priority 和 claim digest。

### Go 与 CLI

- 默认 Direct-only、Cloud-only、Direct+Cloud full race、无 Route 失败。
- Cloud enabled 但 Presence unauthenticated/revoked 时默认排除 Cloud；显式 Cloud 在 QR 输出前失败。
- 多 Direct（LAN + FRP）、SSH credential missing/present、首个 Ready 立即返回、loser cleanup。
- 同 fingerprint merge、新 Route add、same Route idempotent、conflict confirm、USER priority/label 保留、不同 fingerprint 拒绝合并。
- CLI help/usage 以 `--route` 声明 Route；普通 Direct/SSH 字段只在对应的显式 Route 作用域内生效，旧 `--no-cloud`、隐式 Direct override 和旧解析代码扫描为零。
- 普通 Direct/SSH flags、URI 高级入口、同 kind 多实例和参数歧义拒绝；CLI help 明确普通 flags 的 Route 作用域。

### Android

- ARM64 模拟器从真实 App UI 完成 Direct/Cloud 任一成功、另一条失败仍配对成功。
- ARM64 模拟器证明压缩与 raw claim 都由同一 Go binding 解码，Kotlin/TypeScript 不出现 codec 分支；Low 二维码可由真实扫描 UI 识别。
- 已配对设备扫码增加 FRP Route，只保留一个 Endpoint。
- Route 编辑、测试、优先级、SSH credential、失败恢复、中英文、大字体、横屏和 TalkBack。
- ARM64/API 35 模拟器从真实 App UI 完成配对导入、Route 管理与 crash scan；实体手机仅作为补充验收，不替代或阻塞可复现模拟器门禁。PG004 已保留同 Wi-Fi Direct 与纯 5G Cloud/Relay 的实体机证据。

## 12. 完成条件

`PAIRROUTE001` 只有同时满足以下条件才能完成：

1. 文档、Proto、Go Client Engine、daemon、CLI、binding 和 App 使用同一 Route contract。
2. 默认配对不再由失效 Cloud 阻断可达 Direct。
3. 显式重复 `--route`、多 seed race 和逐 Route 错误可观察。
4. 同 DeviceIdentity 再扫码只做 Route diff/merge，不产生重复 Endpoint 或并行 grant。
5. 三层 label 按作用域和 source 稳定处理。
6. 旧 pair flags、单 seed code、隐式 Cloud-first helper 和兼容测试全部删除。
7. Go/CLI/Android ARM64 模拟器准入通过，再恢复 ENROLLUX005 最终 Web 移除后不可重连验收。

## 13. 2026-07-25 完成证据

Proto 多 seed、Route display name、Direct/SSH/Cloud claim 投影、同 generation pairing race、严格 URI 与普通参数解析、Low 二维码和有界 canonical raw-DEFLATE envelope 已接通。旧 pair `--no-cloud` 与 `v3RunningDaemonCloudEnabled` 已从运行代码删除；默认 Auto 生成 Direct + Cloud，显式 `--route` 只安装声明的 Route。

最终 devcloud APK 为 `clients/mobile/android/app/build/outputs/apk/debug/app-debug.apk`，SHA-256 为 `bb0c9c7515bc206562ffd3aedf598ae546d82fea3c4b463f454fa99917b61974`。验收设备为 `termx-pa005n1`、ARM64、API 35。此前真实 App UI 已先导入 marker `1` 的压缩 Direct claim，再对同一 DeviceIdentity 导入 Direct + Cloud claim；最终只保留一个 Endpoint，Direct 建立 P2P DataChannel，连接详情把 Cloud 投影为已配置但当前不可用。

本轮在同一 AVD 上通过 Android Emulator `imagefile` Camera2 后端向真实 App 相机流输入带静区的 Low 二维码。App 成功识别 marker `1` claim 并按 DeviceIdentity 合并到现有 Endpoint；截图证据为 `.artifacts/pairroute001/camera-low-scan-result.png`。随后把同一 daemon 正常签发的 claim payload 重新封装为 195 字符 marker `0` raw envelope，从 App 输入框提交；结果进入逐 Route 建连并返回本地化网络不可达，而不是 claim malformed，证明 raw 与 DEFLATE 都由同一个 Go binding 解码。证据为 `.artifacts/pairroute001/final-raw-claim-result.png`。

App Route 管理器已经覆盖 Direct/SSH/Cloud 新增、编辑、删除、启用、完整连接测试和优先级调整；Go Endpoint registry 在更新后原子删除已无引用的 capability/SSH credential。SSH 用户流程通过平台 `provisionSSHCredential` 生成不可导出私钥并展示 authorized key/fingerprint，UI 行为测试覆盖该链路。稳定 credential、authorization、entitlement、quota、identity、config、unavailable、cancelled 与 internal 错误只显示中英文行动文案，不泄露 provider/transport 原始文本。

真实 App UI 已覆盖英文、简体中文、150% 与 200% 系统字体、竖屏和横屏。Route 操作在窄宽度自动换行，元数据按词换行并保留长 ID 溢出兜底；截图位于 `.artifacts/pairroute001/final-route-manager-open.png`、`final-zh-font150-routes.png`、`final-zh-font150-landscape.png` 与 `final2-zh-font200-routes.png`。WebView Accessibility tree 证明关闭、刷新、重连、添加、编辑、删除、优先级和测试按钮均有可朗读名称，Route checkbox 名称包含 display name/kind/route ID。最终 App Java/native crash scan 为零。

准入命令 `make test-clients`、`go test ./client/... ./cmd/muxvia/... ./shared/remoteauth/... -count=1` 与 `make test-android` 通过；最终微调后再次通过 Route UI 5/5、TypeScript typecheck、devcloud Gradle assemble 和 APK 覆盖安装。用户已明确后续 Android 测试使用模拟器，因此实体机不再是本切片的重复阻塞门禁。
