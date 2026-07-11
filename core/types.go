package core

import (
	"errors"
	"time"
)

const (
	// DefaultHistoryBackpressureBufferBytes 是有界 history 背压策略的默认
	// pending bytes 上限。它只约束 history 输出调度队列，不改变 linehist
	// authoritative truth；低延迟策略下该值仅作为诊断展示和切换策略时的默认值。
	DefaultHistoryBackpressureBufferBytes int64 = 32 * 1024 * 1024
)

// HistoryBackpressureMode 描述 history consumer 落后时 PTY 输出调度如何处理。
// domain owner 是 core-v2 Terminal 输出链路：该策略只控制 pending bytes
// 是否形成背压，不能丢弃 EvictedRows、不能从 live snapshot 或 TUI rows 补历史。
type HistoryBackpressureMode string

const (
	// HistoryBackpressureLowLatency 保持既有 live 优先行为：history backlog
	// 只影响 history.window/freeze/copy 的追平等待，不在入队边界主动阻塞 PTY 输出。
	HistoryBackpressureLowLatency HistoryBackpressureMode = "low-latency"
	// HistoryBackpressureBounded 表示 history pending bytes 到达配置上限后
	// 必须对上游 PTY 输出消费施加背压；R447 负责落地真正阻塞语义。
	HistoryBackpressureBounded HistoryBackpressureMode = "bounded"
)

// HistoryBackpressureConfig 是 terminal history 输出背压配置。
// Mode 决定调度策略，BufferBytes 是有界策略允许驻留在 history pending
// 队列里的 PTY payload 字节数。配置只作用于输出调度与诊断，不是 history
// payload truth，也不能替代 linehist 的文件存储和 cursor/window 边界。
type HistoryBackpressureConfig struct {
	Mode        HistoryBackpressureMode
	BufferBytes int64
}

// Normalize 返回可执行的 history 背压配置。空 mode 沿用低延迟默认；
// 非法 mode 保守回到低延迟；非正 buffer 使用默认上限，避免有界策略
// 被配置成无界或立即自锁。
func (cfg HistoryBackpressureConfig) Normalize() HistoryBackpressureConfig {
	switch cfg.Mode {
	case "", HistoryBackpressureLowLatency:
		cfg.Mode = HistoryBackpressureLowLatency
	case HistoryBackpressureBounded:
	default:
		cfg.Mode = HistoryBackpressureLowLatency
	}
	if cfg.BufferBytes <= 0 {
		cfg.BufferBytes = DefaultHistoryBackpressureBufferBytes
	}
	return cfg
}

type TerminalCreateOptions struct {
	Dir                string
	Env                []string
	ScrollbackSize     int
	ScrollbackMaxBytes int64
	ScrollbackMaxAge   time.Duration
}

func cloneTerminalCreateOptions(options TerminalCreateOptions) TerminalCreateOptions {
	options.Env = append([]string(nil), options.Env...)
	return options
}

type Size struct {
	Cols uint16
	Rows uint16
}

func (size Size) Valid() bool {
	return size.Cols > 0 && size.Rows > 0
}

type TerminalState string

const (
	TerminalStateCreated TerminalState = "created"
	TerminalStateRunning TerminalState = "running"
	TerminalStateExited  TerminalState = "exited"
	TerminalStateRemoved TerminalState = "removed"
)

type TerminalInfo struct {
	ID        string
	Name      string
	Command   []string
	Tags      map[string]string
	Size      Size
	State     TerminalState
	CWD       string
	LiveCWD   string
	CreatedAt time.Time
	ExitCode  *int
	ExitedAt  time.Time
	Resources TerminalResourceUsage
}

func (info TerminalInfo) Clone() TerminalInfo {
	info.Command = append([]string(nil), info.Command...)
	info.Tags = cloneStringMap(info.Tags)
	if info.ExitCode != nil {
		code := *info.ExitCode
		info.ExitCode = &code
	}
	return info
}

// TerminalResourceUsage 是 core-v2 对 terminal 进程资源的采样结果；真值来自当前
// TerminalProcess 的 OS 进程采样，只作为 Terminal Manager 诊断投影，不参与 terminal 生命周期判断。
type TerminalResourceUsage struct {
	PID            int
	CPUPercentX100 int
	MemoryBytes    uint64
	SampledAt      time.Time
}

type TerminalRecord struct {
	ID      string
	Name    string
	Command []string
	Tags    map[string]string
	Size    Size
	Options TerminalCreateOptions
}

// HistoryBacklogStatus 描述 terminal history consumer 当前追平边界。
// domain owner 是 core-v2 Terminal ingest；AppliedSeq 表示已由 history worker
// 交给 authoritative store 处理完成的 backlog 序号，TargetSeq 表示已经进入
// SemanticTap 后 history backlog 的最高序号。它只服务诊断和 history.window
// 内部调度判断，不能当作 history payload truth。
type HistoryBacklogStatus struct {
	TerminalID            string
	HistoryEnabled        bool
	AppliedSeq            uint64
	TargetSeq             uint64
	CatchupPending        bool
	PendingTransactions   int
	PendingBytes          int64
	BackpressureMode      HistoryBackpressureMode
	BufferLimitBytes      int64
	BackpressureEvents    uint64
	BackpressureWaitNanos int64
	InFlight              bool
	Closed                bool
}

var (
	ErrServerClosed               = errors.New("core-v2 server closed")
	ErrInvalidTerminalID          = errors.New("invalid terminal id")
	ErrInvalidCommand             = errors.New("invalid command")
	ErrDuplicateTerminal          = errors.New("duplicate terminal")
	ErrTerminalNotFound           = errors.New("terminal not found")
	ErrTerminalExited             = errors.New("terminal exited")
	ErrHistoryNotRebuilt          = errors.New("history not rebuilt")
	ErrHistoryDisabled            = errors.New("history disabled")
	ErrInvalidServerSize          = errors.New("invalid server size")
	ErrNilListenerFactory         = errors.New("nil listener factory")
	ErrInvalidStorageKey          = errors.New("invalid storage key")
	ErrStorageEntryNotFound       = errors.New("storage entry not found")
	ErrStorageVersionConflict     = errors.New("storage version conflict")
	ErrInvalidWorkbenchMutation   = errors.New("invalid workbench mutation")
	ErrWorkbenchNotFound          = errors.New("workbench resource not found")
	ErrDuplicateWorkbenchResource = errors.New("duplicate workbench resource")
	ErrWorkbenchVersionConflict   = errors.New("workbench version conflict")
)

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
