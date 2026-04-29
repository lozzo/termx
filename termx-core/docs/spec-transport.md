# 传输层（Transport）

传输层提供客户端与 termx Server 之间的通信通道。支持多种传输方式，统一抽象为 Transport 接口。

## Transport 接口

```go
// Transport 表示一个客户端连接
type Transport interface {
    // Send 发送一个消息帧给客户端
    Send(frame []byte) error

    // Recv 接收客户端发来的一个消息帧（阻塞）
    Recv() ([]byte, error)

    // Close 关闭连接
    Close() error

    // Done 返回一个 channel，在连接关闭时关闭
    Done() <-chan struct{}
}

// Listener 监听客户端连接
type Listener interface {
    // Accept 等待并返回一个新的客户端连接
    Accept(ctx context.Context) (Transport, error)

    // Close 停止监听
    Close() error

    // Addr 返回监听地址
    Addr() string
}
```

## 传输方式

### Unix Domain Socket

```
transport/unix/
├── listener.go   # UnixListener
└── transport.go  # UnixTransport
```

**主要用途**：本地 TUI 客户端 → termx daemon

```go
listener, err := unix.NewListener("/tmp/termx.sock")
```

- 使用 `SOCK_STREAM`（TCP 语义的 Unix socket）
- 帧格式：4 字节长度前缀 + 帧数据（length-prefixed framing）
- 权限：socket 文件权限 `0600`（只有当前用户可连接）
- 自动清理：启动时检查并删除残留的 socket 文件

### WebSocket

```
transport/ws/
├── listener.go   # WSListener
├── transport.go  # WSTransport
└── upgrader.go   # HTTP → WebSocket 升级
```

**主要用途**：远程客户端（tgent-app、Web）通过 WebSocket 连接

```go
listener, err := ws.NewListener(":8080", ws.WithPath("/ws"))
```

- 基于 `gorilla/websocket`（或 `nhooyr/websocket`）
- 帧格式：直接使用 WebSocket 的 binary frame（自带长度分隔）
- 支持 TLS
- 支持配置 CORS（`WithAllowedOrigins`）
- Ping/Pong 心跳：每 30 秒发送 Ping，90 秒无 Pong 则断开

#### 认证

WebSocket 连接暴露在网络上，需要认证机制防止未授权访问。初版支持 **Token 认证**：

```go
listener, err := ws.NewListener(":8080",
    ws.WithPath("/ws"),
    ws.WithAuth(ws.TokenAuth("my-secret-token")),
)
```

- 客户端在 WebSocket 握手时通过 `Authorization: Bearer <token>` 头或 `?token=<token>` 查询参数传递 token
- 服务端校验 token，不匹配则拒绝连接（HTTP 401）
- Token 由上层应用（如 tgent-go）生成和管理，termx 只负责校验

未来可扩展 mTLS、JWT 等认证方式，通过 `ws.WithAuth()` 的接口抽象支持。

### Go 内存（Memory）

```
transport/memory/
└── transport.go  # MemoryTransport
```

**主要用途**：Go 进程内嵌入使用（如 tgent-go 直接导入 termx 作为库）

```go
client, server := memory.NewPair()
```

- 使用 Go channel 实现，零拷贝
- 主要用于测试和嵌入场景
- 没有序列化开销

## 多路复用

一个 Transport 连接上需要传输多种消息（控制消息、多个 Terminal 的 I/O 数据）。使用**逻辑通道**进行多路复用：

```
┌──────────────────────────────────┐
│          Transport 连接           │
│                                  │
│  ┌──────────┐  ┌──────────────┐  │
│  │ 控制通道  │  │ 终端 I/O 通道 │  │
│  │ (ch: 0)  │  │ (ch: 1..N)  │  │
│  └──────────┘  └──────────────┘  │
└──────────────────────────────────┘
```

- **通道 0**：控制通道 —— Terminal CRUD、事件通知、系统消息
- **通道 1..N**：终端 I/O 通道 —— 每个 Terminal 的输入/输出/resize/快照

通道 ID 在帧头部指定，详见 [线协议](spec-protocol.md)。

## 背压控制

### 问题

如果客户端处理慢（如网络拥塞），服务端持续发送终端输出会导致缓冲区无限增长。

### 策略

背压在两层处理，各自独立但协调一致：

#### Fan-out 层（Server 内部）

Fan-out 向每个 subscriber 的 channel 投递数据：

- channel 缓冲区满时，**丢弃该消息**（不阻塞其他 subscriber）
- 每次丢弃时递增该 subscriber 的 `droppedBytes` 计数器
- 下次成功投递时，先插入一条 **SyncLost 标记**（携带 `droppedBytes` 值），然后重置计数器
- 这样客户端能精确知道丢失了多少数据

#### Transport 层（网络发送）

Transport 从 subscriber channel 读取数据并发送给远程客户端：

- 发送缓冲区有上限（默认 1 MB）
- 超出上限时优先丢弃最旧的终端输出帧
- 请求响应、错误响应、Hello、快照响应不丢弃
- 事件通知遵循 best-effort 语义，客户端应通过 list/get 对账
- 丢弃终端输出时同样生成 SyncLost 帧发给客户端

#### 客户端恢复

客户端收到 SyncLost 后：

1. 屏幕状态可能已不一致
2. 客户端应请求 snapshot 重建屏幕
3. snapshot 请求通过控制通道发送，不受 I/O 背压影响

```go
type TransportOptions struct {
    SendBufferSize int           // 发送缓冲区大小（默认 1 MB）
    ReadTimeout    time.Duration // 读超时（默认 0，不超时）
    WriteTimeout   time.Duration // 写超时（默认 5s）
}
```

### 优先级

发送队列按优先级排序：

1. **最高**：同步控制消息（Hello、Response、Error）—— 不可丢弃
2. **中等**：快照响应 —— 不可丢弃（客户端正在恢复）
3. **较低**：异步事件通知 —— best-effort
4. **最低**：终端输出流 —— 可丢弃（客户端可以通过快照恢复）

## Server 集成

Server 可以同时监听多种 Transport：

```go
srv := termx.NewServer(
    termx.WithSocketPath("/tmp/termx.sock"),
    termx.WithWebSocket(":8080"),
)
```

每个传入的 Transport 连接，Server 创建一个 session goroutine 来处理：

```go
func (s *Server) handleTransport(t transport.Transport) {
    // 1. 读取并处理客户端请求
    // 2. 将事件和订阅的输出推送给客户端
    // 3. 连接断开时清理订阅
}
```

## 相关文档

- [线协议](spec-protocol.md) — Transport 上的消息编码
- [快照](spec-snapshot.md) — 背压丢弃后的恢复机制
- [Go API](spec-go-api.md) — Server 配置选项
