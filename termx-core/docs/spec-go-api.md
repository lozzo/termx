# Go 公开 API

termx 的 Go API 设计为可以直接作为库嵌入到其他 Go 程序中。API 围绕 `Server` 类型展开。

## Server

```go
package termx

// Server 是 termx 的入口，管理 Terminal 池并接受客户端连接
type Server struct {
    // 内部字段...
}

// NewServer 创建一个新的 Server 实例
func NewServer(opts ...ServerOption) *Server

// ListenAndServe 启动 Server，监听配置的 Transport 并服务请求
// 阻塞直到 ctx 取消
func (s *Server) ListenAndServe(ctx context.Context) error

// Shutdown 优雅关闭 Server
// 等待所有连接关闭，然后终止所有 Terminal
func (s *Server) Shutdown(ctx context.Context) error
```

### Server 选项（Functional Options）

```go
type ServerOption func(*serverConfig)

// WithSocketPath 设置 Unix socket 监听路径
// 默认：$XDG_RUNTIME_DIR/termx.sock 或 /tmp/termx-{uid}.sock
func WithSocketPath(path string) ServerOption

// WithWebSocket 设置 WebSocket 监听地址
func WithWebSocket(addr string, opts ...ws.Option) ServerOption

// WithDefaultSize 设置新建 Terminal 的默认尺寸
// 默认：80x24
func WithDefaultSize(cols, rows uint16) ServerOption

// WithDefaultScrollback 设置默认滚动回看行数
// 默认：10000
func WithDefaultScrollback(lines int) ServerOption

// WithDefaultKeepAfterExit 设置进程退出后保留时间
// 默认：5 分钟
func WithDefaultKeepAfterExit(d time.Duration) ServerOption

// WithLogger 设置日志记录器
func WithLogger(logger *slog.Logger) ServerOption
```

## Terminal CRUD

### Create

```go
type CreateOptions struct {
    Command []string          // 必选：要运行的命令
    ID      string            // 可选：自定义 ID（默认自动生成）
    Name    string            // 可选：人类可读名称
    Tags    map[string]string // 可选：key-value 元数据

    Size Size     // 可选：初始尺寸（默认使用 Server 配置）
    Dir  string   // 可选：工作目录
    Env  []string // 可选：额外环境变量（KEY=VALUE）

    ScrollbackSize int           // 可选：滚动回看行数
    KeepAfterExit  time.Duration // 可选：退出后保留时间
}

// Create 创建一个新的 Terminal
// 返回创建的 Terminal 信息（此时状态可能是 starting 或 running）
func (s *Server) Create(ctx context.Context, opts CreateOptions) (*TerminalInfo, error)
```

错误：
- `ErrInvalidCommand` — Command 为空
- `ErrDuplicateID` — 指定的 ID 已存在
- `ErrSpawnFailed` — PTY 创建或命令启动失败；不会留下一个失败的 Terminal 对象

### Get

```go
// Get 获取指定 Terminal 的信息
func (s *Server) Get(ctx context.Context, id string) (*TerminalInfo, error)
```

```go
type TerminalInfo struct {
    ID        string
    Name      string
    Command   []string
    Tags      map[string]string
    Size      Size
    State     TerminalState
    CreatedAt time.Time
    ExitCode  *int // 仅 exited 时有值
}

type TerminalState string

const (
    StateStarting TerminalState = "starting"
    StateRunning  TerminalState = "running"
    StateExited   TerminalState = "exited"
)
```

- `Get` / `List` 返回的 `TerminalInfo` 及其中的 `Command` / `Tags` 应视为只读快照
- 如果调用方需要修改这些字段，应先自行拷贝再修改
- 这样服务端可以复用 metadata 快照，降低高频读取时的分配成本

错误：
- `ErrNotFound` — Terminal 不存在

### List

```go
type ListOptions struct {
    State *TerminalState    // 按状态过滤
    Tags  map[string]string // 按 tag 过滤（所有指定的 key-value 都匹配才返回）
}

// List 列出所有匹配的 Terminal
func (s *Server) List(ctx context.Context, opts ...ListOptions) ([]*TerminalInfo, error)
```

- 无参数时返回所有 Terminal
- State 过滤：只返回指定状态的 Terminal
- 返回值中的 metadata 采用只读快照语义；不要原地修改 `Command` / `Tags`

### Kill

```go
// Kill 终止指定的 Terminal
// 对 exited 状态的 Terminal 调用会立即从池中移除
func (s *Server) Kill(ctx context.Context, id string) error
```

错误：
- `ErrNotFound` — Terminal 不存在

### SetTags

```go
// SetTags 更新 Terminal 的 tags（合并语义）
// 只更新传入的 key，不影响其他 key；value 为空字符串表示删除该 key
func (s *Server) SetTags(ctx context.Context, id string, tags map[string]string) error
```

错误：
- `ErrNotFound` — Terminal 不存在

## Terminal I/O

### WriteInput

```go
// WriteInput 向 Terminal 写入输入数据
// data 会直接写入 PTY stdin
func (s *Server) WriteInput(ctx context.Context, id string, data []byte) error
```

错误：
- `ErrNotFound` — Terminal 不存在
- `ErrTerminalExited` — Terminal 已退出

### SendKeys

```go
// SendKeys 向 Terminal 发送按键序列
// 支持特殊键名："Enter", "Tab", "Escape", "Ctrl-C", "Ctrl-D" 等
func (s *Server) SendKeys(ctx context.Context, id string, keys ...string) error
```

内部将 key 名称转换为对应的字节序列后调用 `WriteInput`。

便利方法，常用于编程场景：

```go
srv.SendKeys(ctx, id, "ls -la", "Enter")
srv.SendKeys(ctx, id, "Ctrl-C")
```

### Resize

```go
// Resize 调整 Terminal 的尺寸
func (s *Server) Resize(ctx context.Context, id string, cols, rows uint16) error
```

- 同时更新 PTY 窗口大小和 VTerm 缓冲区
- 触发 `TerminalResized` 事件

错误：
- `ErrNotFound` — Terminal 不存在
- `ErrTerminalExited` — Terminal 已退出

## 输出订阅

### Subscribe

```go
type StreamMessageType int

const (
    StreamOutput StreamMessageType = iota + 1
    StreamSyncLost
    StreamClosed
)

type StreamMessage struct {
    Type StreamMessageType

    // Type == StreamOutput 时有效
    Output []byte

    // Type == StreamSyncLost 时有效
    DroppedBytes uint64

    // Type == StreamClosed 时有效
    ExitCode *int
}

// Subscribe 订阅 Terminal 的输出流
// ctx 取消时 channel 关闭
func (s *Server) Subscribe(ctx context.Context, id string) (<-chan StreamMessage, error)
```

- 返回的 channel 缓冲区大小为 256
- `StreamOutput` 携带 PTY 原始输出字节
- 如果消费者跟不上，服务端会丢弃部分输出，并在下一次成功投递时发送 `StreamSyncLost`
- 需要恢复屏幕状态时，调用 `Snapshot()`；Subscribe 不再内嵌快照数据
- 终端退出时，订阅者会收到 `StreamClosed`

错误：
- `ErrNotFound` — Terminal 不存在

## 快照

### Snapshot

```go
// Snapshot 获取 Terminal 的当前屏幕快照
func (s *Server) Snapshot(ctx context.Context, id string) (*Snapshot, error)
```

```go
type Snapshot struct {
    TerminalID string
    Size       Size
    Screen     ScreenData
    Scrollback [][]Cell
    Cursor     CursorState
    Modes      TerminalModes
    Timestamp  time.Time
}
```

- 对 running 和 exited 状态的 Terminal 都可以获取
- 返回的是深拷贝，调用者可以安全地持有和修改

**重连恢复的正确调用顺序**：必须先 `Subscribe()` 再 `Snapshot()`，否则两者之间的输出会丢失。

```go
// 正确：先订阅，再快照，subscriber 缓冲区会暂存 Snapshot 期间的输出
stream, _ := srv.Subscribe(ctx, id)
snap, _ := srv.Snapshot(ctx, id)
renderScreen(snap)      // 用快照初始化屏幕
for msg := range stream { // 然后消费流式输出（包含 Snapshot 之后的增量）
    handleStreamMessage(msg)
}

// 错误：先快照再订阅，snap 和 stream 之间的输出丢失
// snap, _ := srv.Snapshot(ctx, id)   // ✗
// stream, _ := srv.Subscribe(ctx, id) // ✗
```

由于 `Subscribe` 返回的 channel 有 256 缓冲区，在 `Subscribe` 和 `Snapshot` 之间产生的输出会暂存在 channel 中，不会丢失。客户端渲染时，Snapshot 提供初始屏幕，随后的 stream 消息提供增量更新。

错误：
- `ErrNotFound` — Terminal 不存在

## 事件

### Events

```go
type EventsOption func(*eventsConfig)

// WithTerminalFilter 只接收指定 Terminal 的事件
func WithTerminalFilter(id string) EventsOption

// WithTypeFilter 只接收指定类型的事件
func WithTypeFilter(types ...EventType) EventsOption

// Events 订阅 Terminal 池的事件
// ctx 取消时 channel 关闭
func (s *Server) Events(ctx context.Context, opts ...EventsOption) <-chan Event
```

详见 [事件系统](spec-events.md)。

## 控制权管理

### RevokeCollaborators

```go
// RevokeCollaborators 将指定 Terminal 的所有 transport collaborator 降级为 observer
// 仅 Go API（owner）可调用，transport 客户端不能调用此方法
// 被降级的 collaborator 收到 CollaboratorsRevoked 事件
func (s *Server) RevokeCollaborators(ctx context.Context, id string) error
```

错误：
- `ErrNotFound` — Terminal 不存在

### Attached

```go
type AttachInfo struct {
    RemoteAddr string    // 客户端连接地址
    Mode       string    // "observer" | "collaborator"
    AttachedAt time.Time // attach 时间
}

// Attached 查询指定 Terminal 当前所有 attach 的客户端信息
func (s *Server) Attached(ctx context.Context, id string) ([]AttachInfo, error)
```

错误：
- `ErrNotFound` — Terminal 不存在

## 错误类型

```go
var (
    ErrNotFound       = errors.New("termx: terminal not found")
    ErrDuplicateID    = errors.New("termx: terminal ID already exists")
    ErrInvalidCommand = errors.New("termx: command is required")
    ErrTerminalExited = errors.New("termx: terminal has exited")
    ErrSpawnFailed    = errors.New("termx: failed to spawn process")
    ErrServerClosed   = errors.New("termx: server is closed")
)
```

所有错误都可以用 `errors.Is` 检查。

## 完整示例

### 嵌入使用

```go
srv := termx.NewServer()

term, err := srv.Create(ctx, termx.CreateOptions{
    Name:    "dev",
    Command: []string{"bash"},
})
if err != nil {
    return err
}

snap, err := srv.Snapshot(ctx, term.ID)
if err != nil {
    return err
}
_ = snap

stream, err := srv.Subscribe(ctx, term.ID)
if err != nil {
    return err
}

go func() {
    for msg := range stream {
        switch msg.Type {
        case termx.StreamOutput:
            os.Stdout.Write(msg.Output)
        case termx.StreamSyncLost:
            log.Printf("output dropped: %d bytes", msg.DroppedBytes)
        case termx.StreamClosed:
            log.Printf("terminal closed: %v", msg.ExitCode)
        }
    }
}()

if err := srv.SendKeys(ctx, term.ID, "echo hello world", "Enter"); err != nil {
    return err
}
```

### Daemon 模式

```go
srv := termx.NewServer(
    termx.WithSocketPath("/tmp/termx.sock"),
    termx.WithWebSocket(":8080"),
    termx.WithDefaultScrollback(5000),
)

if err := srv.ListenAndServe(ctx); err != nil {
    log.Fatal(err)
}
```

## 相关文档

- [Terminal 模型](spec-terminal.md) — 服务端的核心实体
- [传输层](spec-transport.md) — 远程客户端如何接入
- [事件系统](spec-events.md) — Events 方法详情
- [快照](spec-snapshot.md) — Snapshot 的格式
