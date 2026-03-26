package runtime

import (
	"context"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

type terminalClient interface {
	Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error)
	Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error)
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
	List(ctx context.Context) (*protocol.ListResult, error)
	Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error)
	Input(ctx context.Context, channel uint16, data []byte) error
	Resize(ctx context.Context, channel uint16, cols, rows uint16) error
	Stream(channel uint16) (<-chan protocol.StreamFrame, func())
	Kill(ctx context.Context, terminalID string) error
	Remove(ctx context.Context, terminalID string) error
}

type BootstrapConfig struct {
	Workspace    string
	DefaultShell string
	AttachID     string
}

// Bootstrap 只负责把 daemon 侧 terminal 装配成 app 初始状态，不直接启动事件循环。
func Bootstrap(ctx context.Context, client terminalClient, cfg BootstrapConfig) (app.Model, error) {
	model := app.NewModel(cfg.Workspace)
	if client == nil {
		return model, nil
	}

	if cfg.AttachID != "" {
		meta := coreterminal.Metadata{
			ID:    types.TerminalID(cfg.AttachID),
			Name:  cfg.AttachID,
			State: coreterminal.StateRunning,
		}
		model.Workbench.BindActivePane(meta)
		return model, nil
	}

	command := []string{cfg.DefaultShell}
	if len(command[0]) == 0 {
		command = []string{"/bin/sh"}
	}
	created, err := client.Create(ctx, command, "shell", protocol.Size{Cols: 80, Rows: 24})
	if err != nil {
		return app.Model{}, err
	}
	meta := coreterminal.Metadata{
		ID:      types.TerminalID(created.TerminalID),
		Name:    "shell",
		Command: append([]string(nil), command...),
		State:   coreterminal.StateRunning,
	}
	if created.State == string(coreterminal.StateExited) {
		meta.State = coreterminal.StateExited
	}
	model.Workbench.BindActivePane(meta)
	return model, nil
}
