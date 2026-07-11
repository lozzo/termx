# TermX 统一文件能力规范

状态：FILE001 活动基线

生效日期：2026-07-12

## 1. 目标

TermX 文件能力允许客户端浏览和操作 endpoint 所属 daemon 机器上的文件，并在 local、SSH、WebRTC direct、single Relay 和未来 Relay Mesh 上复用同一公开协议。文件管理是免费公开客户端与 daemon 能力；托管 Relay 可以按实际转发的加密流量计费，但不能改变文件语义或权限。

本规范替代共享 UI 中旧 `/files/*` runtime API 和独立 file DataChannel 的架构假设。现有 UI、DTO 和测试只能作为交互需求输入，不能作为 daemon contract 或兼容门禁。

## 2. 领域归属

- daemon 所在机器的文件系统是路径、目录项、metadata 和文件内容的唯一真值。
- core protocol service 负责请求校验、授权、文件系统调用、transfer 生命周期和协议错误映射，但不复制持久化文件内容。
- client endpoint runtime 负责把文件请求路由到 owning endpoint，并持有列表、预览、选择和 transfer 进度 projection。
- Transport 只负责承载同一 termx protocol。local、SSH、direct、Relay 不产生不同文件 API。
- Hub、Relay、Control Plane、Companion 和 Route Planner 不拥有文件状态，也不能看到路径、metadata、内容、摘要或 resume offset。

文件能力是 daemon-level capability，不从 `TerminalID` 推导。terminal-scoped grant 默认不得访问文件；只有显式授予文件 scope 的 daemon capability 才能调用文件方法。云订阅、RelayLease 或 HubAdmissionTicket 不能扩大文件 scope。

## 3. 路径与安全模型

- wire path 使用 UTF-8 绝对路径；`~`、相对路径和环境变量展开只允许在客户端输入辅助层或显式 resolve 方法中发生。
- daemon 必须清理路径并按实际 OS 语义访问，不能通过字符串前缀判断安全边界。
- 默认 daemon-level 文件 capability 以 daemon 进程用户权限为上限，不提权、不绕过 ACL、不跟随客户端提供的身份切换。
- 后续如支持 root allowlist，必须以打开后的文件句柄或可靠的 real-path 边界校验处理 symlink；FILE002 不通过脆弱前缀匹配实现 sandbox。
- 所有 mutation 方法必须明确返回目标路径和逐项错误；批量操作不能用部分成功伪装整体成功。
- 日志和指标不得记录文件内容、CapabilityGrant、完整路径或摘要；允许记录 method、结果码、字节数、耗时和脱敏 endpoint/session id。

## 4. 协议能力

### 4.1 Metadata 方法

FILE002 使用公开 wire method，不保留 HTTP path：

| Method | 语义 |
| --- | --- |
| `file.list` | 分页列出目录，返回稳定单次响应内的 name/type/size/mode/mtime/link target metadata |
| `file.stat` | 对单一路径执行 lstat 语义查询，不隐式读取内容 |
| `file.preview` | 有上限地读取普通文件前缀，返回 MIME hint、截断标志和 bytes |
| `file.mkdir` | 创建单个目录；是否递归必须由请求字段显式表达 |
| `file.rename` | 同一 daemon 文件系统内重命名；覆盖策略必须显式表达 |
| `file.delete` | 删除单个目标；目录递归必须显式表达 |
| `file.copy` | 复制一组目标到目录，逐项报告结果 |
| `file.move` | 移动一组目标到目录，逐项报告结果 |

列表分页 cursor 由 daemon 生成，只保证当前目录枚举窗口内有效。目录变化导致 cursor 失效时返回明确 stale 错误，客户端重新从首页加载，不在本地拼接猜测。

### 4.2 Transfer 方法与 stream

FILE003 在现有单一 termx protocol DataChannel 上增加复用文件 stream，不创建 `api`、`file-*` 或旧 runtime DataChannel：

| Method/frame | 语义 |
| --- | --- |
| `file.download.open` | 打开只读 transfer，固定文件 identity、size、mtime、offset 和 server transfer id |
| `file.upload.open` | 在目标目录创建 daemon-owned 临时文件，返回 transfer id 和已确认 offset |
| `file.transfer.cancel` | 幂等取消 transfer 并释放句柄；上传临时文件按策略删除 |
| stream data | 携带 transfer id、offset 和 bounded bytes；必须按 offset 顺序确认 |
| stream ack | 返回连续已落地 offset 和接收窗口，形成显式背压 |
| stream finish | 携带最终 size 与 SHA-256；daemon 校验后原子发布上传文件 |

下载续传必须重新 `open` 并同时验证 size、mtime 和可选 identity token；源文件变化返回 stale，不继续拼接。上传只允许恢复 daemon 已保留的临时 transfer，且必须绑定同一授权 session/设备主体和目标路径。断线后 transfer 默认进入短期可恢复状态，超过 TTL 清理。

发送方在未收到窗口额度时不得继续发送。实现必须限制单 frame、单 transfer 和单 session 的缓冲上限；慢消费者只能阻塞对应 transfer，不能阻塞 terminal input、live screen、heartbeat 或其它 transfer。

## 5. 错误与失败条件

协议至少区分：`invalid_argument`、`permission_denied`、`not_found`、`already_exists`、`not_directory`、`is_directory`、`stale`、`resource_exhausted`、`cancelled`、`checksum_mismatch`、`unsupported` 和 `internal`。

- transport 断开不等于上传成功；只有 daemon 返回 finish success 后客户端才能标记完成。
- Relay、Hub 或 Companion 失败只能让 managed endpoint transport 断开，不能触发 local/SSH fallback，也不能绕过文件授权。
- 客户端取消必须停止 UI 进度、停止发送并最终释放 daemon transfer；重复取消保持幂等。
- 文件系统 mutation 发生部分成功时必须返回逐项结果，客户端不得自动重放非幂等操作。

## 6. Capability 与 transport scope

CapabilityGrant 增加独立文件权限集合，至少区分：

- `file.read_metadata`
- `file.read_content`
- `file.write_content`
- `file.mutate`

`file.preview` 和 download 需要 `file.read_content`；list/stat 需要 `file.read_metadata`；upload 需要 `file.write_content`；mkdir/rename/delete/copy/move 需要 `file.mutate`，其中复制还需要源路径的 read 权限。

本地完整 daemon transport 可以显式拥有全部文件权限。terminal-scoped transport 一律拒绝文件方法和 stream。remote daemon-level grant 必须把文件权限映射为 `TransportScope` 的显式字段，不能继续用 `AllowDaemon=true` 隐式代表未来所有能力。

## 7. 客户端迁移

- `clients/ui` 的 `FileApi` 从 HTTP-like method/path DTO 改为 typed termx file client。
- `RtcSession.openApi()` 不再承载文件方法；terminal management 后续可独立收敛，但不属于 FILE 主线扩展范围。
- `RtcSession.openFileTransfer()` 和 Android 旧 file channel manager 在 FILE004 删除，不做双路径兼容。
- Android native 与 TypeScript 共享同一 wire schema；native 负责 OS picker、content URI、后台生命周期和本地落盘，不能定义第二套 daemon 文件协议。
- Community、Official、local、SSH 和 managed WebRTC 使用同一公开文件 contract。Official 私有模块只提供 cloud endpoint discovery/signaling，不能实现文件 RPC。

## 8. 验收顺序

1. FILE002 用临时目录 harness 验证 metadata、preview、mutation、权限拒绝、symlink 和逐项错误。
2. FILE003 用内存 transport 和真实文件验证上传下载、慢消费者、取消、断线续传、源文件变化、摘要不匹配和并发隔离。
3. FILE004 先迁移共享 UI，再接 Android picker/background store；删除所有旧 `/files/*` 请求和独立 file channel。
4. 真机依次验证 direct 与显式 single Relay：浏览、文本/图片预览、上传、下载、取消、后台恢复和断线续传。

FILE004 完成前，产品必须把文件入口视为 unavailable，而不是把旧 UI 存在误报为文件能力可用。
