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
	Remove(ctx context.Context, terminalID string) error
}

func Bootstrap(ctx context.Context, client Client, cfg BootstrapConfig) (app.Model, error) {
	model := app.NewModel()
	wsName := cfg.Workspace
	if wsName == "" {
		wsName = "main"
	}
	ws := workspace.NewTemporary(wsName)

	service := NewTerminalService(client)
	store := NewSessionStore()
	model.IntentExecutor = modelIntentExecutor{service: service, store: store}
	terminalID := cfg.AttachID
	var attach *protocol.AttachResult
	var snapshot *protocol.Snapshot
	var err error
	var hydrated *protocol.TerminalInfo
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
	} else {
		list, listErr := client.List(ctx)
		if listErr != nil {
			return model, listErr
		}
		hydrated = findTerminalInfo(list, terminalID)
	}

	attach, err = service.Attach(ctx, terminalID, "collaborator")
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
	pane.SlotState = paneSlotStateFromMetadata(hydrated)
	tab.TrackPane(pane)
	tab.ActivePaneID = pane.ID

	model.Workspace = ws
	model.Terminals[pane.TerminalID] = metadataFromBootstrap(cfg.DefaultShell, pane, hydrated)
	model.Sessions[pane.TerminalID] = app.TerminalSession{
		TerminalID: pane.TerminalID,
		Channel:    attach.Channel,
		Attached:   pane.SlotState == types.PaneSlotLive,
		Snapshot:   snapshot,
	}
	if pane.SlotState == types.PaneSlotLive {
		stream, cancel := service.Stream(attach.Channel)
		store.BindLive(pane.TerminalID, attach.Channel, snapshot, stream, cancel)
		model.PreviewStreamNext = store.NextStreamMessageCmd
	}
	return model, nil
}

func metadataFromBootstrap(defaultShell string, pane workspace.PaneState, info *protocol.TerminalInfo) stateterminal.Metadata {
	meta := stateterminal.Metadata{
		ID:              pane.TerminalID,
		Name:            "shell",
		Command:         []string{defaultShell},
		State:           stateterminal.StateRunning,
		OwnerPaneID:     pane.ID,
		AttachedPaneIDs: []types.PaneID{pane.ID},
	}
	if info == nil {
		return meta
	}
	meta.Name = info.Name
	meta.Command = append([]string(nil), info.Command...)
	meta.Tags = cloneProtocolTags(info.Tags)
	switch info.State {
	case "exited":
		meta.State = stateterminal.StateExited
	default:
		meta.State = stateterminal.StateRunning
	}
	return meta
}

func findTerminalInfo(list *protocol.ListResult, terminalID string) *protocol.TerminalInfo {
	if list == nil {
		return nil
	}
	for _, info := range list.Terminals {
		if info.ID == terminalID {
			copyInfo := info
			return &copyInfo
		}
	}
	return nil
}

func cloneProtocolTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}

func paneSlotStateFromMetadata(info *protocol.TerminalInfo) types.PaneSlotState {
	if info != nil && info.State == "exited" {
		return types.PaneSlotExited
	}
	return types.PaneSlotLive
}
