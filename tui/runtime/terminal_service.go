package runtime

import (
	"context"

	"github.com/lozzow/termx/protocol"
)

type TerminalService struct {
	client Client
}

func NewTerminalService(client Client) TerminalService {
	return TerminalService{client: client}
}

func (s TerminalService) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return s.client.Create(ctx, command, name, size)
}

func (s TerminalService) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return s.client.Attach(ctx, terminalID, mode)
}

func (s TerminalService) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return s.client.Snapshot(ctx, terminalID, offset, limit)
}

func (s TerminalService) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	return s.client.Events(ctx, params)
}

func (s TerminalService) Input(ctx context.Context, channel uint16, data []byte) error {
	return s.client.Input(ctx, channel, data)
}

func (s TerminalService) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return s.client.Resize(ctx, channel, cols, rows)
}

func (s TerminalService) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	return s.client.Stream(channel)
}

func (s TerminalService) Kill(ctx context.Context, terminalID string) error {
	return s.client.Kill(ctx, terminalID)
}

type PendingWorkbenchActionKind string

const (
	PendingWorkbenchActionCreateTerminal PendingWorkbenchActionKind = "create-terminal"
	PendingWorkbenchActionKillTerminal   PendingWorkbenchActionKind = "kill-terminal"
)

type PendingWorkbenchAction struct {
	Kind       PendingWorkbenchActionKind
	TerminalID string
	Command    []string
	Name       string
	Size       protocol.Size
}

type WorkbenchActionResult struct {
	TerminalID string
}

type workbenchActionService interface {
	Create(context.Context, []string, string, protocol.Size) (*protocol.CreateResult, error)
	Kill(context.Context, string) error
}

// ExecuteWorkbenchAction 把 reducer 产出的副作用描述下放到 runtime 服务。
// 这里先锁住 create/kill 两条真实契约链路，避免 UI 逻辑直接触 client。
func ExecuteWorkbenchAction(ctx context.Context, service workbenchActionService, action PendingWorkbenchAction) (WorkbenchActionResult, error) {
	switch action.Kind {
	case PendingWorkbenchActionCreateTerminal:
		size := action.Size
		if size.Cols == 0 || size.Rows == 0 {
			size = protocol.Size{Cols: 80, Rows: 24}
		}
		created, err := service.Create(ctx, action.Command, action.Name, size)
		if err != nil {
			return WorkbenchActionResult{}, err
		}
		return WorkbenchActionResult{TerminalID: created.TerminalID}, nil
	case PendingWorkbenchActionKillTerminal:
		if err := service.Kill(ctx, action.TerminalID); err != nil {
			return WorkbenchActionResult{}, err
		}
		return WorkbenchActionResult{}, nil
	default:
		return WorkbenchActionResult{}, nil
	}
}
