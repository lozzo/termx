# Terminal 模型

Terminal 是 termx 的**唯一服务端实体**。服务端没有 session、window、pane、group，也不保存客户端布局或索引元数据；它只维护一个扁平的 Terminal 池。

## 结构

```go
type Terminal struct {
    ID        string            // 唯一标识符
    Name      string            // 可选的人类可读名称
    Command   []string          // 运行的命令（如 ["bash"] 或 ["python3", "app.py"]）
    Tags      map[string]string // 可选的 key-value 元数据，服务端只存储不解释
    Size      Size              // 当前终端尺寸（cols x rows）
    State     TerminalState     // 状态机状态
    CreatedAt time.Time         // 创建时间
    ExitCode  *int              // 仅 exited 状态有值
}

type Size struct {
    Cols uint16
    Rows uint16
}
```

## 状态机

Terminal 有三个状态，状态转换是单向的：

```
starting -> running -> exited
```

### starting

Terminal 已创建，PTY 进程正在启动。这个状态通常非常短暂（毫秒级）。

- 进入条件：`Server.Create()` 被调用且 PTY 已成功创建
- 可执行操作：无（等待进程进入 running）
- 退出条件：PTY 进程启动完成 -> `running`

### running

PTY 进程正在运行，可以正常交互。

- 进入条件：PTY 进程启动成功
- 可执行操作：`WriteInput`、`Resize`、`Subscribe`、`Snapshot`、`Kill`
- 退出条件：进程自然退出或被 Kill -> `exited`

### exited

进程已结束。Terminal 对象保留在池中一段时间（默认 5 分钟），供客户端查看最终输出和退出码。

- 进入条件：进程退出（自然退出或 Kill）
- 可执行操作：`Snapshot`
- 退出条件：超时后从池中移除（触发 `TerminalRemoved` 事件）
- ExitCode：进程退出码（自然退出时为进程返回值，Kill 时为 -1）

### 创建失败

如果 PTY 创建或命令启动失败，`Create()` 直接返回错误，**不会**在池中留下一个 `exited` Terminal。

## ID 生成

Terminal ID 使用 **nanoid** 格式，8 字符，字母表为 `0123456789abcdefghijklmnopqrstuvwxyz`：

```
示例：a7k2m9x1, p3b8n5q0
```

选择理由：
- 8 字符足够短，方便在 CLI 中输入（`termx attach a7k2m9x1`）
- `36^8` 约等于 2.8 万亿种组合，单机场景碰撞概率可忽略
- 全小写 + 数字，不会因为大小写混淆

客户端也可以在 `Create` 时指定自定义 ID（需唯一）。

## 名称

Name 是可选的人类可读标识，不要求唯一：

```go
srv.Create(ctx, termx.CreateOptions{
    Name:    "dev-server",
    Command: []string{"npm", "run", "dev"},
})
```

- 如果未指定，自动生成（如 `terminal-1`, `terminal-2`）
- Name 是服务端字段，用于基础可读性
- 如果客户端需要可变名称、分组、workspace、tab、业务标签等元数据，应保存在客户端自己的布局文档或业务存储中
- TUI 客户端在列表中优先显示 Name，没有则显示 ID

## Tags

Tags 是 Terminal 上可选的 key-value 元数据。服务端只存储，不解释其含义：

```go
srv.Create(ctx, termx.CreateOptions{
    Command: []string{"npm", "run", "dev"},
    Tags:    map[string]string{"group": "dev", "role": "frontend"},
})

// 按 tag 过滤
terminals := srv.List(ctx, termx.ListOptions{Tags: map[string]string{"group": "dev"}})

// 更新 tag
srv.SetTags(ctx, termID, map[string]string{"status": "idle"})
```

Tags 的主要价值：
- **重连识别**：客户端断连重连后，可以通过 tag 找回"自己之前创建的那些 terminal"，而不是只靠 ID 映射表
- **批量操作**：按 tag 过滤后批量 Kill、批量 Subscribe
- **调试友好**：`termx ls --tag group=dev` 快速定位

设计约束：
- key 和 value 均为 string，最大长度 128 字节
- 单个 Terminal 最多 32 个 tag
- key 不能以 `_` 开头（保留给未来内部使用）
- `SetTags` 是合并语义：只更新传入的 key，不影响其他 key；value 为空字符串表示删除该 key

## 客户端持有的布局与索引元数据

termx 服务端**不存储** workspace、tab、pane 树。Tags 提供轻量级的 key-value 标记（用于过滤和重连识别），但所有组织关系都由客户端自己持有：

- TUI 客户端可以把布局保存在本地 YAML / JSON 文件
- tgent-app 可以把 workspace / window / pane 关系保存在自己的 store 或数据库中
- 自动化脚本可以维护 `terminalID -> job` 的映射表

这样有几个直接好处：
- 同一个 Terminal 可以被多个 workspace 或视图引用
- 不同客户端可以对同一组 Terminal 使用完全不同的组织方式
- 服务端模型保持极简，不需要处理布局冲突或元数据 schema 演进

termx 唯一负责的是：给客户端稳定的 `terminalID`，让客户端可以在自己的元数据里引用它。

## Attach 模式

termx 区分两种远程 attach 模式：

| 模式 | 能力 | 数量限制 |
|------|------|----------|
| **observer** | 只读：接收输出、获取快照、查看基础元数据 | 无限制 |
| **collaborator** | 读写：observer 的一切 + 发送输入 + resize | 无限制 |

多个 collaborator 可以同时操作同一个 Terminal（类似 tmux 的多人共享模型），适用于结对编程、教学演示等场景。

### Attach 请求

远程客户端通过协议的 `attach` 方法选择模式：

```json
// 请求 collaborator 权限
{"id": 1, "method": "attach", "params": {"terminal_id": "a7k2m9x1", "mode": "collaborator"}}

// 响应（mode 始终等于请求的 mode，不存在降级）
{"id": 1, "result": {"mode": "collaborator", "channel": 3}}
```

- `mode` 可选值：`observer` | `collaborator`
- 服务端不做降级，请求什么就给什么

### I/O 通道权限

- **observer** 的 channel：服务端忽略 `Input(0x11)` 和 `Resize(0x12)` 帧
- **collaborator** 的 channel：正常处理所有帧类型

### Go API 嵌入者（Owner）

进程内嵌入的 Go 调用方是 Terminal 的 **owner**，权限不受 transport 模式限制：

- Owner 始终可以调用 `WriteInput` / `Resize` / `SendKeys`
- Owner 可以调用 `RevokeCollaborators()` 将所有 transport collaborator 降级为 observer
- Owner 可以调用 `Attached()` 查询当前所有 attach 的客户端信息

典型场景：tgent-go daemon 嵌入 termx，daemon 内部代码（如 AI agent）通过 Go API 写入命令，同时多个 tgent-app 用户通过 WebSocket 或其他 transport 也在操作同一个 Terminal。所有写入共享同一个 PTY，last write wins。

## 设计约束

- 多个 observer 和 collaborator 可以同时连接同一个 Terminal
- collaborator 断开后不影响其他 collaborator；Terminal 保持最后一次尺寸继续运行
- 同一个 Terminal 可以被多个 workspace 或视图引用，这些引用关系只存在于客户端侧

## 配置选项

```go
type CreateOptions struct {
    // 必选
    Command []string // 要运行的命令

    // 可选
    ID   string // 自定义 ID（默认自动生成）
    Name string // 人类可读名称

    Size Size     // 初始尺寸（默认 80x24）
    Dir  string   // 工作目录（默认当前目录）
    Env  []string // 额外环境变量（KEY=VALUE 格式）

    // 高级
    ScrollbackSize int           // 滚动回看行数（默认 10000）
    KeepAfterExit  time.Duration // 进程退出后保留时间（默认 5 分钟）
}
```

## 并发安全

- Terminal 的所有操作都是并发安全的
- 多个客户端可以同时 Subscribe 同一个 Terminal
- 多个 collaborator 可以同时 `WriteInput` / `Resize`；多个 observer 只读
- 多个 collaborator 的 `WriteInput` 直接写入 PTY fd，内核处理序列化
- 多个 collaborator 的 `Resize` 直接调 `pty.Setsize`，last write wins
- 进程内嵌入的 Go 调用方被视为 owner，可直接调用 `WriteInput` / `Resize`
- 不需要应用层锁（与 owner + transport 并发写入的策略一致）
- State 转换是原子的

## 相关文档

- [PTY 管理](spec-pty-manager.md) — Terminal 底层进程的生命周期管理
- [虚拟终端](spec-vterm.md) — 屏幕缓冲区的维护
- [快照](spec-snapshot.md) — 基于 VTerm 的屏幕快照
- [事件系统](spec-events.md) — Terminal 状态变更事件
- [Go API](spec-go-api.md) — 完整的 API 定义
