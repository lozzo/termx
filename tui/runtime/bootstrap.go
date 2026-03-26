package runtime

import (
	"context"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
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
		meta, snapshot, err := loadTerminalRuntime(ctx, client, types.TerminalID(cfg.AttachID))
		if err != nil {
			return app.Model{}, err
		}
		model.Workbench.BindActivePane(meta)
		model.Workbench.SetSessionSnapshot(meta.ID, snapshot)
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
	meta, snapshot, err := loadTerminalRuntime(ctx, client, types.TerminalID(created.TerminalID))
	if err != nil {
		return app.Model{}, err
	}
	if len(meta.Command) == 0 {
		meta.Command = append([]string(nil), command...)
	}
	if meta.Name == "" {
		meta.Name = "shell"
	}
	model.Workbench.BindActivePane(meta)
	model.Workbench.SetSessionSnapshot(meta.ID, snapshot)
	return model, nil
}
