# 事件系统

事件系统通知客户端 Terminal 池的变化，使客户端能够保持与服务端状态大致同步。

## 事件类型

```go
type EventType int

const (
    EventTerminalCreated EventType = iota + 1
    EventTerminalStateChanged
    EventTerminalResized
    EventTerminalRemoved
    EventCollaboratorsRevoked // 所有 collaborator 被降级为 observer
)

type Event struct {
    Type       EventType
    TerminalID string
    Timestamp  time.Time

    // 根据 Type，以下字段只有一个非 nil
    Created              *TerminalCreatedData
    StateChanged         *TerminalStateChangedData
    Resized              *TerminalResizedData
    Removed              *TerminalRemovedData
    CollaboratorsRevoked *CollaboratorsRevokedData
}
```

### TerminalCreated

新 Terminal 被创建。

```go
type TerminalCreatedData struct {
    Name    string
    Command []string
    Size    Size
}
```

### TerminalStateChanged

Terminal 状态转换（starting -> running -> exited）。

```go
type TerminalStateChangedData struct {
    OldState TerminalState
    NewState TerminalState
    ExitCode *int // 仅 exited 时有值
}
```

### TerminalResized

Terminal 尺寸变化。

```go
type TerminalResizedData struct {
    OldSize Size
    NewSize Size
}
```

### TerminalRemoved

Terminal 从池中被移除（进程退出后超过保留时间，或被显式删除）。

```go
type TerminalRemovedData struct {
    Reason string // "expired" 或 "killed"
}
```

### CollaboratorsRevoked

Owner 调用 `RevokeCollaborators()` 后，所有被降级的 collaborator 收到此事件。

```go
type CollaboratorsRevokedData struct {
    // 无额外字段；收到此事件的客户端应将自身视为 observer
}
```

## 订阅

### 订阅所有事件

```go
ch := srv.Events(ctx)
for event := range ch {
    switch event.Type {
    case termx.EventTerminalCreated:
        fmt.Printf("created: %s, command: %v\n", event.TerminalID, event.Created.Command)
    case termx.EventTerminalStateChanged:
        fmt.Printf("state: %s -> %s\n", event.StateChanged.OldState, event.StateChanged.NewState)
    case termx.EventCollaboratorsRevoked:
        fmt.Printf("collaborators revoked on %s\n", event.TerminalID)
    }
}
```

- 返回一个 `<-chan Event`
- ctx 取消时 channel 关闭
- channel 缓冲区大小为 64

### 按 Terminal 过滤

```go
ch := srv.Events(ctx, termx.WithTerminalFilter(terminalID))
```

- 只接收指定 Terminal 的事件

### 按类型过滤

```go
ch := srv.Events(ctx, termx.WithTypeFilter(
    termx.EventTerminalCreated,
    termx.EventTerminalRemoved,
))
```

- 只接收指定类型的事件

### 组合过滤

```go
ch := srv.Events(ctx,
    termx.WithTerminalFilter(terminalID),
    termx.WithTypeFilter(termx.EventTerminalResized),
)
```

## 内部实现：EventBus

```go
type EventBus struct {
    mu          sync.RWMutex
    subscribers []*subscriber
}

type subscriber struct {
    ch     chan Event
    filter eventFilter
    ctx    context.Context
}
```

### 投递策略

- **best-effort 投递**：使用 `select` + `default`，如果订阅者的 channel 满了，**丢弃该事件**（不阻塞其他订阅者）
- **理由**：事件是通知，不是状态真相；客户端可以随时调用 `List()` / `Get()` 做对账
- **慢消费者警告**：如果连续丢弃超过 10 个事件，在日志中记录警告

### 生命周期

1. `srv.Events(ctx)` 创建 subscriber 并注册到 EventBus
2. 有事件发生时，EventBus 遍历所有 subscriber，匹配 filter 后投递
3. ctx 取消时，subscriber 从 EventBus 注销，channel 关闭

### 并发安全

- subscriber 列表使用 `sync.RWMutex` 保护
- 投递时持有读锁（多个事件源可以并发投递）
- 注册/注销时持有写锁

## 事件顺序保证

- **单 Terminal 内有序**：同一个 Terminal 的事件按发生顺序投递
- **跨 Terminal 无序**：不同 Terminal 的事件之间没有顺序保证
- **因果一致**：`Created` 一定在该 Terminal 的其他事件之前，`Removed` 一定在最后

## 与 Transport 的关系

- Go API `Events()` 和远程协议里的 `Event` 消息共享同一套 best-effort 语义
- 请求/响应类控制消息是可靠交付的；事件通知不是
- 远程客户端如果怀疑漏事件，应调用 `list` / `get` 重建本地状态

## 线协议中的事件

通过 Transport 连接的远程客户端，事件作为控制通道消息传输：

```
EventMessage {
    Type:       EVENT
    EventType:  <event type>
    TerminalID: <id>
    Data:       <序列化的事件数据>
}
```

详见 [线协议](spec-protocol.md)。

## 相关文档

- [Terminal 模型](spec-terminal.md) — 事件的来源
- [Go API](spec-go-api.md) — Events 方法签名
- [线协议](spec-protocol.md) — 事件在协议中的编码
