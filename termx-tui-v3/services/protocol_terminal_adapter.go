package services

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type ProtocolTerminalClient interface {
	AttachWithOptions(context.Context, protocol.AttachParams) (*protocol.AttachResult, error)
	Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error)
	List(context.Context) (*protocol.ListResult, error)
	Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error)
	Restart(context.Context, string) error
	Kill(context.Context, string) error
	SetMetadata(context.Context, string, string, map[string]string) error
	Input(context.Context, uint16, []byte) error
	Resize(context.Context, uint16, uint16, uint16) error
	EnsureResize(context.Context, protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error)
	Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error)
}

// ProtocolTerminalServiceAdapter 把 TUI-v3 terminal service 契约映射到 termx protocol。
type ProtocolTerminalServiceAdapter struct {
	Client ProtocolTerminalClient
}

func (adapter ProtocolTerminalServiceAdapter) Attach(ctx context.Context, req TerminalAttachRequest) (TerminalAttachResult, error) {
	if adapter.Client == nil {
		return TerminalAttachResult{}, ErrMissingTerminalClient
	}
	mode := req.Mode
	if mode == "" {
		mode = "collaborator"
	}
	result, err := adapter.Client.AttachWithOptions(ctx, protocol.AttachParams{
		TerminalID:   req.TerminalID,
		Mode:         mode,
		ResizePolicy: req.ResizePolicy,
		SurfaceID:    req.SurfaceID,
		ViewID:       req.ViewID,
	})
	if err != nil {
		return TerminalAttachResult{}, err
	}
	out := TerminalAttachResult{
		TerminalID: req.TerminalID,
		Channel:    result.Channel,
		Cols:       req.Cols,
		Rows:       req.Rows,
		CanResize:  true,
	}
	if result.ResizeControl != nil {
		out.CanResize = result.ResizeControl.CanResize
		if result.ResizeControl.ResizeOwnership != nil && result.ResizeControl.ResizeOwnership.Size != (protocol.Size{}) {
			out.Cols = int(result.ResizeControl.ResizeOwnership.Size.Cols)
			out.Rows = int(result.ResizeControl.ResizeOwnership.Size.Rows)
		}
	}
	return out, nil
}

func (adapter ProtocolTerminalServiceAdapter) List(ctx context.Context, _ TerminalListRequest) (TerminalListResult, error) {
	if adapter.Client == nil {
		return TerminalListResult{}, ErrMissingTerminalClient
	}
	result, err := adapter.Client.List(ctx)
	if err != nil {
		return TerminalListResult{}, err
	}
	items := make([]TerminalPoolItem, 0, len(result.Terminals))
	for _, terminal := range result.Terminals {
		items = append(items, TerminalPoolItem{
			TerminalID: terminal.ID,
			Title:      terminalPoolTitleFromProtocol(terminal),
			State:      terminal.State,
			CWD:        terminal.CWD,
			Tags:       cloneStringMap(terminal.Tags),
			Cols:       int(terminal.Size.Cols),
			Rows:       int(terminal.Size.Rows),
		})
	}
	return TerminalListResult{Items: items}, nil
}

func (adapter ProtocolTerminalServiceAdapter) Create(ctx context.Context, req TerminalCreateRequest) (TerminalCreateResult, error) {
	if adapter.Client == nil {
		return TerminalCreateResult{}, ErrMissingTerminalClient
	}
	command := append([]string(nil), req.Command...)
	if len(command) == 0 {
		command = DefaultTerminalCommand()
	}
	result, err := adapter.Client.Create(ctx, protocol.CreateParams{
		ID:      req.TerminalID,
		Name:    req.Title,
		Command: command,
		Size:    protocol.Size{Cols: uint16(req.Cols), Rows: uint16(req.Rows)},
	})
	if err != nil {
		return TerminalCreateResult{}, err
	}
	return TerminalCreateResult{TerminalID: result.TerminalID, State: result.State}, nil
}

func (adapter ProtocolTerminalServiceAdapter) Restart(ctx context.Context, req TerminalRestartRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.Restart(ctx, req.TerminalID)
}

func (adapter ProtocolTerminalServiceAdapter) Reconnect(ctx context.Context, req TerminalReconnectRequest) (TerminalAttachResult, error) {
	return adapter.Attach(ctx, TerminalAttachRequest{
		TerminalID:   req.TerminalID,
		Cols:         req.Cols,
		Rows:         req.Rows,
		Mode:         req.Mode,
		ResizePolicy: req.ResizePolicy,
		SurfaceID:    req.SurfaceID,
		ViewID:       req.ViewID,
	})
}

func (adapter ProtocolTerminalServiceAdapter) Kill(ctx context.Context, req TerminalKillRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.Kill(ctx, req.TerminalID)
}

func (adapter ProtocolTerminalServiceAdapter) EditMetadata(ctx context.Context, req TerminalEditMetadataRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.SetMetadata(ctx, req.TerminalID, req.Title, cloneStringMap(req.Tags))
}

func (adapter ProtocolTerminalServiceAdapter) SendInput(ctx context.Context, req TerminalInputRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	if req.Channel == 0 {
		return fmt.Errorf("terminal input requires attached channel")
	}
	return adapter.Client.Input(ctx, req.Channel, req.Bytes)
}

func (adapter ProtocolTerminalServiceAdapter) Resize(ctx context.Context, req TerminalResizeRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	if req.Channel == 0 {
		return fmt.Errorf("terminal resize requires attached channel")
	}
	cols := uint16(req.Cols)
	rows := uint16(req.Rows)
	if req.SurfaceID != "" || req.ViewID != "" {
		_, err := adapter.Client.EnsureResize(ctx, protocol.EnsureResizeParams{
			TerminalID:   req.TerminalID,
			Channel:      req.Channel,
			Cols:         cols,
			Rows:         rows,
			ResizePolicy: protocol.ResizePolicyOwner,
			SurfaceID:    req.SurfaceID,
			ViewID:       req.ViewID,
		})
		return err
	}
	return adapter.Client.Resize(ctx, req.Channel, cols, rows)
}

func (adapter ProtocolTerminalServiceAdapter) LiveSurface(ctx context.Context, req TerminalSurfaceRequest) (TerminalSurfaceResult, error) {
	if adapter.Client == nil {
		return TerminalSurfaceResult{}, ErrMissingTerminalClient
	}
	limit := req.Rows
	if limit <= 0 {
		limit = 24
	}
	snapshot, err := adapter.Client.Snapshot(ctx, req.TerminalID, 0, limit)
	if err != nil {
		return TerminalSurfaceResult{}, err
	}
	lines := liveSurfaceLinesFromSnapshot(snapshot)
	return TerminalSurfaceResult{
		Ready: true,
		Snapshot: state.LiveSurfaceSnapshot{
			TerminalID: req.TerminalID,
			Cols:       int(snapshot.Size.Cols),
			Rows:       int(snapshot.Size.Rows),
			Lines:      lines,
			Cursor: state.LiveCursor{
				Visible: snapshot.Cursor.Visible,
				Row:     snapshot.Cursor.Row,
				Col:     snapshot.Cursor.Col,
				Shape:   snapshot.Cursor.Shape,
			},
		},
	}, nil
}

func (adapter ProtocolTerminalServiceAdapter) LiveEvents(ctx context.Context, req TerminalLiveEventRequest) (<-chan TerminalLiveEvent, error) {
	if adapter.Client == nil {
		return nil, ErrMissingTerminalClient
	}
	events, err := adapter.Client.Events(ctx, protocol.EventsParams{
		TerminalID: req.TerminalID,
		Types: []protocol.EventType{
			protocol.EventTerminalStateChanged,
			protocol.EventTerminalResized,
			protocol.EventTerminalMetadataChanged,
			protocol.EventTerminalReadError,
		},
	})
	if err != nil {
		return nil, err
	}
	out := make(chan TerminalLiveEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				liveEvent := adapter.liveEventFromProtocol(ctx, req, event)
				select {
				case out <- liveEvent:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (adapter ProtocolTerminalServiceAdapter) liveEventFromProtocol(ctx context.Context, req TerminalLiveEventRequest, event protocol.Event) TerminalLiveEvent {
	out := TerminalLiveEvent{TerminalID: req.TerminalID}
	if event.TerminalID != "" {
		out.TerminalID = event.TerminalID
	}
	if event.ReadError != nil {
		out.Err = fmt.Errorf("%s", event.ReadError.Error)
		return out
	}
	if event.StateChanged != nil && event.StateChanged.NewState == "exited" {
		out.Exited = true
		if event.StateChanged.ExitCode != nil {
			out.ExitCode = *event.StateChanged.ExitCode
		}
		out.Reason = "exited"
	}
	surface, err := adapter.LiveSurface(ctx, TerminalSurfaceRequest{TerminalID: out.TerminalID, Cols: req.Cols, Rows: req.Rows})
	if err != nil {
		out.Err = err
		return out
	}
	out.Snapshot = surface.Snapshot
	out.Ready = surface.Ready
	return out
}

func liveSurfaceLinesFromSnapshot(snapshot *protocol.Snapshot) []string {
	if snapshot == nil || len(snapshot.Screen.Cells) == 0 {
		return nil
	}
	lines := make([]string, len(snapshot.Screen.Cells))
	for rowIndex, row := range snapshot.Screen.Cells {
		for _, cell := range row {
			lines[rowIndex] += cell.Content
		}
	}
	return lines
}

func terminalPoolTitleFromProtocol(terminal protocol.TerminalInfo) string {
	if terminal.Name != "" {
		return terminal.Name
	}
	if terminal.ID != "" {
		return terminal.ID
	}
	return "terminal"
}
