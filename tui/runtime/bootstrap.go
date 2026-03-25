package runtime

import (
	"context"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

type BootstrapConfig struct {
	DefaultShell string
	Workspace    string
	AttachID     string
}

// Client 抽成 runtime 层自己的接口，避免和根包形成循环依赖。
type Client interface {
	Close() error
	Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error)
	SetTags(ctx context.Context, terminalID string, tags map[string]string) error
	SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error
	List(ctx context.Context) (*protocol.ListResult, error)
	Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error)
	Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error)
	Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error)
	Input(ctx context.Context, channel uint16, data []byte) error
	Resize(ctx context.Context, channel uint16, cols, rows uint16) error
	Stream(channel uint16) (<-chan protocol.StreamFrame, func())
	Kill(ctx context.Context, terminalID string) error
}

func Bootstrap(ctx context.Context, client Client, cfg BootstrapConfig) (app.Model, error) {
	model := app.NewModel()
	wsName := cfg.Workspace
	if wsName == "" {
		wsName = "main"
	}
	ws := workspace.NewTemporary(wsName)

	service := NewTerminalService(client)
	model.IntentExecutor = NewModelIntentExecutor(service)
	terminalID := cfg.AttachID
	var attach *protocol.AttachResult
	var snapshot *protocol.Snapshot
	var err error
	if terminalID == "" {
		command := []string{cfg.DefaultShell}
		if len(command) == 1 && command[0] == "" {
			command = []string{"/bin/sh"}
		}
		created, createErr := service.Create(ctx, command, "shell", protocol.Size{Cols: 80, Rows: 24})
		if createErr != nil {
			return model, createErr
		}
		terminalID = created.TerminalID
	}

	attach, err = service.Attach(ctx, terminalID, "rw")
	if err != nil {
		return model, err
	}
	snapshot, err = service.Snapshot(ctx, terminalID, 0, 0)
	if err != nil {
		return model, err
	}

	tab := ws.ActiveTab()
	pane, _ := tab.ActivePane()
	pane.TerminalID = types.TerminalID(terminalID)
	pane.SlotState = types.PaneSlotLive
	tab.TrackPane(pane)
	tab.ActivePaneID = pane.ID

	model.Workspace = ws
	model.Terminals[pane.TerminalID] = stateterminal.Metadata{
		ID:              pane.TerminalID,
		Name:            "shell",
		Command:         []string{cfg.DefaultShell},
		State:           stateterminal.StateRunning,
		OwnerPaneID:     pane.ID,
		AttachedPaneIDs: []types.PaneID{pane.ID},
	}
	model.Sessions[pane.TerminalID] = app.TerminalSession{
		TerminalID: pane.TerminalID,
		Channel:    attach.Channel,
		Attached:   true,
		Snapshot:   snapshot,
	}
	return model, nil
}
