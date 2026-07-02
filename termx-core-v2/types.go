package termxcorev2

import (
	"errors"
	"time"
)

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
	TerminalID          string
	HistoryEnabled      bool
	AppliedSeq          uint64
	TargetSeq           uint64
	CatchupPending      bool
	PendingTransactions int
	InFlight            bool
	Closed              bool
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
