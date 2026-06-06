package termxcorev2

import (
	"errors"
	"time"
)

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
	CreatedAt time.Time
	ExitCode  *int
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
}

var (
	ErrServerClosed               = errors.New("core-v2 server closed")
	ErrInvalidTerminalID          = errors.New("invalid terminal id")
	ErrInvalidCommand             = errors.New("invalid command")
	ErrDuplicateTerminal          = errors.New("duplicate terminal")
	ErrTerminalNotFound           = errors.New("terminal not found")
	ErrTerminalExited             = errors.New("terminal exited")
	ErrStaleHistoryWindow         = errors.New("stale history window")
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
