package runtime

import (
	"context"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state/types"
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

type intentRuntimeService interface {
	workbenchActionService
	Attach(context.Context, string, string) (*protocol.AttachResult, error)
	Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error)
}

type modelIntentExecutor struct {
	service intentRuntimeService
}

func NewModelIntentExecutor(service intentRuntimeService) app.IntentExecutor {
	return modelIntentExecutor{service: service}
}

func (e modelIntentExecutor) ExecuteIntent(ctx context.Context, model app.Model, intent app.Intent) (app.Model, error) {
	return ApplyIntent(ctx, model, e.service, intent)
}

// ApplyIntent 负责串起“app reducer 产出 effect -> runtime 真执行 -> app 回填成功状态”。
// 当前只为 create/kill 打通真实闭环；remove/restart 仍停留在 reducer 的 state-only 边界。
func ApplyIntent(ctx context.Context, model app.Model, service intentRuntimeService, intent app.Intent) (app.Model, error) {
	next := model.Apply(intent)
	for _, effect := range next.PendingEffects {
		var err error
		next, err = applyEffect(ctx, next, service, effect)
		if err != nil {
			next.Notice = &app.NoticeState{Message: err.Error()}
			return next, err
		}
	}
	next.PendingEffects = nil
	return next, nil
}

func applyEffect(ctx context.Context, model app.Model, service intentRuntimeService, effect app.Effect) (app.Model, error) {
	switch typed := effect.(type) {
	case app.CreateTerminalEffect:
		result, err := ExecuteWorkbenchAction(ctx, service, PendingWorkbenchAction{
			Kind:    PendingWorkbenchActionCreateTerminal,
			Command: typed.Command,
			Name:    typed.Name,
			Size:    typed.Size,
		})
		if err != nil {
			return model, err
		}
		attach, err := service.Attach(ctx, result.TerminalID, "rw")
		if err != nil {
			_ = service.Kill(ctx, result.TerminalID)
			return model, err
		}
		snapshot, err := service.Snapshot(ctx, result.TerminalID, 0, 0)
		if err != nil {
			_ = service.Kill(ctx, result.TerminalID)
			return model, err
		}
		return model.Apply(app.CreateTerminalSucceededIntent{
			PaneID:     typed.PaneID,
			TerminalID: types.TerminalID(result.TerminalID),
			Command:    typed.Command,
			Name:       typed.Name,
			Channel:    attach.Channel,
			Snapshot:   snapshot,
		}), nil
	case app.KillTerminalEffect:
		if _, err := ExecuteWorkbenchAction(ctx, service, PendingWorkbenchAction{
			Kind:       PendingWorkbenchActionKillTerminal,
			TerminalID: string(typed.TerminalID),
		}); err != nil {
			return model, err
		}
		return model.Apply(app.KillTerminalSucceededIntent{TerminalID: typed.TerminalID}), nil
	default:
		return model, nil
	}
}
