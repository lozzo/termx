# PTY 管理

PTY 管理器负责 Terminal 的底层进程生命周期：创建 PTY、读写 I/O、调整尺寸、清理进程。

## 包结构

```
pty/
├── pty.go        # PTY 类型和核心逻辑
└── pty_unix.go   # Unix 平台实现
```

## 依赖

使用 [`creack/pty`](https://github.com/creack/pty) 库进行 PTY 操作，这是 Go 生态中最成熟的 PTY 库。

## PTY 生命周期

### 创建（Spawn）

```go
type PTY struct {
    pty  *os.File     // PTY master 端
    cmd  *exec.Cmd    // 子进程
    done chan struct{} // 进程退出信号
}

type SpawnOptions struct {
    Command  []string // 命令和参数
    Dir      string   // 工作目录
    Env      []string // 额外环境变量
    Size     Size     // 初始尺寸
}

func Spawn(opts SpawnOptions) (*PTY, error)
```

创建流程：

1. 构建 `exec.Cmd`，设置 `Dir` 和 `Env`
2. 设置 `Setpgid: true`（创建独立进程组，便于后续整组 kill）
3. 调用 `creack/pty.StartWithSize()` 启动进程并获取 PTY master fd
4. 启动 goroutine 等待进程退出（`cmd.Wait()`），完成后关闭 `done` channel
5. 返回 `PTY` 实例

### 读取输出（Read）

```go
func (p *PTY) Read(buf []byte) (int, error)
```

- 直接从 PTY master fd 读取
- 阻塞直到有数据或 PTY 关闭
- 返回 `io.EOF` 表示进程已退出

调用者（Terminal）负责将读到的数据送入 VTerm 解析和 Fan-out 分发。

### 写入输入（Write）

```go
func (p *PTY) Write(data []byte) (int, error)
```

- 直接写入 PTY master fd
- 数据会出现在子进程的 stdin 中
- 并发安全（PTY fd 本身支持并发写入）

### 调整尺寸（Resize）

```go
func (p *PTY) Resize(cols, rows uint16) error
```

- 调用 `creack/pty.Setsize()` 设置 PTY 窗口大小
- 内核会向子进程发送 `SIGWINCH` 信号
- 子进程（如 vim、less）收到信号后会重新查询窗口大小并重绘

### 终止（Kill）

```go
func (p *PTY) Kill() error
```

三阶段终止策略，确保进程被完全清理：

```
SIGHUP → (等待 500ms) → SIGTERM → (等待 2s) → SIGKILL
```

1. **SIGHUP**：通知进程终端断开。大多数 shell 和应用会响应此信号自行退出
2. **SIGTERM**：如果 SIGHUP 后进程仍在运行，发送标准终止信号
3. **SIGKILL**：最后手段，强制杀死进程

使用 **进程组** 确保子进程创建的所有后代进程都被一起终止：

```go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Setpgid: true,
}

// Kill 时对整个进程组发送信号
syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
```

### 等待退出（Wait）

```go
func (p *PTY) Wait() <-chan struct{}
func (p *PTY) ExitCode() int
```

- `Wait()` 返回一个 channel，在进程退出时关闭
- `ExitCode()` 在进程退出后返回退出码

### 关闭（Close）

```go
func (p *PTY) Close() error
```

- 关闭 PTY master fd
- 如果进程仍在运行，先调用 `Kill()`
- 清理所有资源

## 环境变量

创建 PTY 时，环境变量的处理顺序：

1. 继承当前进程的环境变量（`os.Environ()`）
2. 设置 `TERM=xterm-256color`（覆盖继承的值）
3. 设置 `TERMX=1`（标记在 termx 中运行）
4. 设置 `TERMX_TERMINAL_ID=<id>`（当前 Terminal 的 ID）
5. 应用用户指定的额外环境变量（`SpawnOptions.Env`，可覆盖上述任何值）

## 工作目录

- 如果 `SpawnOptions.Dir` 非空，使用指定目录
- 如果为空，使用 termx 进程自身的工作目录
- 如果指定的目录不存在，返回错误（不自动创建）

## 错误处理

| 场景 | 处理 |
|------|------|
| 命令不存在 | `Spawn` 返回 error |
| 工作目录不存在 | `Spawn` 返回 error |
| PTY 创建失败 | `Spawn` 返回 error |
| 进程启动后立即退出 | 正常流程：通过 `done` channel 通知，Terminal 进入 `exited` 状态 |
| Read 时进程退出 | 返回 `io.EOF` |
| Write 到已退出进程 | 返回 error（PTY fd 已关闭） |
| Resize 已退出进程 | 返回 error |

## 未来扩展

以下功能在初版不实现，但架构上预留扩展空间：

- **cgroup v2 资源限制**：通过 Linux cgroup 限制 CPU/内存/进程数
- **沙盒隔离（Linux Namespaces）**：通过 PID/mount/network namespace 隔离 Terminal 进程

这些功能适用于多用户共享、CI 任务、不可信代码执行等场景，初版的 tgent 个人设备场景不需要。

## 相关文档

- [Terminal 模型](spec-terminal.md) — Terminal 结构和状态机
- [虚拟终端](spec-vterm.md) — PTY 输出的解析和缓冲
- [Go API](spec-go-api.md) — 高级 API 如何调用 PTY 管理器
