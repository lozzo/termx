# 线协议（Protocol）

线协议定义了 Transport 上传输的消息格式。所有消息都封装在统一的帧格式中。

## 帧格式

每个帧的二进制结构：

```
┌─────────┬─────────┬──────────┬─────────────────┐
│ Channel │  Type   │  Length  │     Payload     │
│ (2 B)   │ (1 B)   │ (4 B)    │   (variable)    │
└─────────┴─────────┴──────────┴─────────────────┘
```

| 字段 | 大小 | 说明 |
|------|------|------|
| Channel | uint16 | 逻辑通道 ID（0 = 控制通道，1..N = 终端 I/O） |
| Type | uint8 | 消息类型 |
| Length | uint32 | Payload 长度（字节） |
| Payload | variable | 消息体（格式取决于 Type） |

- 所有多字节整数使用**大端序**（big endian）
- 最大帧大小：4 MB（`Length <= 4194304`）
- 超大帧拒绝并关闭连接

## 控制通道（Channel 0）

控制通道传输 Terminal 管理消息和系统事件。消息体使用 **JSON** 编码。

### 性能定位

这里使用 JSON 是有意为之：

- 控制通道承担的是低频管理操作，而不是高频终端字节流
- 控制消息更重视可调试性、可读性和跨客户端实现成本
- 高负载终端场景的性能热点通常不在这里，而在本地渲染链路

因此，协议层的性能边界应理解为：

- **控制面可以是 JSON**
- **终端数据面必须保持二进制**
- 如果出现 `htop`、`vim`、resize、分屏/浮窗卡顿，应优先审查 TUI 渲染和背压策略，而不是先推翻本协议结构

### 消息类型

| Type 值 | 名称 | 方向 | 说明 |
|---------|------|------|------|
| 0x01 | Request | C->S | 客户端请求 |
| 0x02 | Response | S->C | 服务端响应 |
| 0x03 | Event | S->C | 服务端事件推送 |
| 0x04 | Error | S->C | 错误响应 |

### Request / Response

请求-响应模式，通过 `id` 字段匹配：

```json
// Request (C->S)
{
    "id": 1,
    "method": "create",
    "params": {
        "command": ["bash"],
        "name": "dev",
        "size": {"cols": 80, "rows": 24}
    }
}

// Response (S->C)
{
    "id": 1,
    "result": {
        "terminal_id": "a7k2m9x1",
        "state": "running"
    }
}
```

### 支持的方法（Method）

| Method | 说明 | Params | Result |
|--------|------|--------|--------|
| `create` | 创建 Terminal | command, name?, size?, dir?, env? | terminal_id, state |
| `kill` | 终止 Terminal | terminal_id | {} |
| `list` | 列出 Terminal | state? | terminals: [{id, name, state, size}] |
| `get` | 获取 Terminal 信息 | terminal_id | id, name, state, size, created_at, exit_code? |
| `resize` | 调整尺寸 | terminal_id, cols, rows | {} |
| `attach` | 附加到 Terminal | terminal_id, mode (observer/collaborator) | mode, channel |
| `detach` | 从 Terminal 分离 | terminal_id | {} |
| `snapshot` | 请求快照 | terminal_id, scrollback_limit?, scrollback_offset? | (JSON 快照数据) |
| `events` | 订阅事件 | types?, terminal_id? | {} |

`attach` 是唯一的 I/O 入口：
- `observer` attach 后只接收输出，不允许写入
- `collaborator` attach 后既能收输出，也能发送输入和 resize
- 协议中不再单独定义 `subscribe` / `unsubscribe`

### Event

```json
{
    "event": "terminal_created",
    "terminal_id": "a7k2m9x1",
    "data": {
        "name": "dev",
        "command": ["bash"],
        "size": {"cols": 80, "rows": 24}
    },
    "timestamp": "2026-03-17T10:30:00Z"
}
```

### Error

```json
{
    "id": 1,
    "error": {
        "code": 404,
        "message": "terminal not found: a7k2m9x1"
    }
}
```

错误码：

| Code | 说明 |
|------|------|
| 400 | 无效请求（参数错误） |
| 404 | Terminal 不存在 |
| 409 | 冲突（如重复 ID） |
| 500 | 内部错误 |

## 终端 I/O 通道（Channel 1..N）

I/O 通道传输终端的输入/输出数据。消息体使用**二进制格式**。

### 消息类型

| Type 值 | 名称 | 方向 | 说明 |
|---------|------|------|------|
| 0x10 | Output | S->C | 终端输出数据 |
| 0x11 | Input | C->S | 终端输入数据 |
| 0x12 | Resize | C->S | 调整终端尺寸 |
| 0x16 | SyncLost | S->C | 数据丢失通知 |
| 0x17 | Closed | S->C | 终端已关闭 |

### Output（0x10）

```
Payload: raw bytes (PTY 输出数据)
```

- 直接是 PTY 输出的原始字节，不做任何转换
- 客户端负责解析 ANSI 转义序列
- 这一通道是性能敏感路径，设计目标是“少复制、少编码、少语义变换”

### Input（0x11）

```
Payload: raw bytes (用户输入数据)
```

- 直接写入 PTY stdin
- 可以是单个按键、粘贴的文本、或者转义序列
- observer attach 的 channel 上发送该帧会被服务端忽略

### Resize（0x12）

```
Payload: [cols: uint16, rows: uint16] (4 bytes)
```

- observer attach 的 channel 上发送该帧会被服务端忽略

### SyncLost（0x16）

```
Payload: [dropped_bytes: uint32] (4 bytes)
```

通知客户端有终端输出数据被丢弃（背压）。客户端应请求 `snapshot` 恢复。

这类设计是高性能终端系统的必要组成部分：

- 慢消费者不应拖垮整条数据链路
- 当客户端来不及处理连续增量时，允许丢弃中间帧并退化为“重新抓取最新快照”
- 这比无限缓存、无限追帧更符合终端复用器的实际行为

### Closed（0x17）

```
Payload: [exit_code: int32] (4 bytes)
```

通知客户端终端已关闭，包含退出码。

## 通道分配与回收

1. 客户端通过控制通道发送 `attach` 请求
2. 服务端分配一个 I/O 通道号并在 Response 中返回
3. 后续该 Terminal 的 I/O 消息使用该通道号
4. `detach` 后通道号释放

### 回收策略

使用 **free list** 管理通道号：

- 初始为空，新分配的通道号从 1 开始递增
- 释放时将通道号放入 free list
- 新分配时优先从 free list 取，free list 为空时才递增
- uint16 范围 1~65534，单连接最多 65534 个并发 I/O 通道（远超实际需求）

```go
type channelAllocator struct {
    mu       sync.Mutex
    next     uint16
    freeList []uint16
}

func (a *channelAllocator) Alloc() uint16 {
    a.mu.Lock()
    defer a.mu.Unlock()
    if len(a.freeList) > 0 {
        ch := a.freeList[len(a.freeList)-1]
        a.freeList = a.freeList[:len(a.freeList)-1]
        return ch
    }
    a.next++
    return a.next
}

func (a *channelAllocator) Free(ch uint16) {
    a.mu.Lock()
    a.freeList = append(a.freeList, ch)
    a.mu.Unlock()
}
```

## 协议版本协商

连接建立后的第一个帧是版本协商：

```
Client -> Server:
  Channel: 0, Type: 0x00 (Hello)
  Payload: {"version": 1, "client": "termx-tui/0.1.0", "capabilities": []}

Server -> Client:
  Channel: 0, Type: 0x00 (Hello)
  Payload: {"version": 1, "server": "termx/0.1.0", "capabilities": []}
```

如果版本不兼容，服务端返回 Error 并关闭连接。

### Capabilities

`capabilities` 是一个字符串数组，用于增量协商可选功能，避免频繁升级 version：

- 初版 capabilities 为空数组
- 未来可增加如 `"snapshot-binary"`、`"incremental-snapshot"` 等
- 双方只使用两者都支持的 capabilities

## 编解码

```go
package protocol

type Encoder struct {
    w io.Writer
}

func (e *Encoder) WriteFrame(channel uint16, typ uint8, payload []byte) error

type Decoder struct {
    r io.Reader
}

func (d *Decoder) ReadFrame() (channel uint16, typ uint8, payload []byte, err error)
```

## 相关文档

- [传输层](spec-transport.md) — 帧在哪里传输
- [快照](spec-snapshot.md) — 快照数据的格式
- [事件系统](spec-events.md) — 事件在协议中的编码
- [Go API](spec-go-api.md) — 协议对应的 Go API
