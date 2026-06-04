package services

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/internal/protocol"
)

type ProtocolTerminalClient interface {
	AttachWithOptions(context.Context, protocol.AttachParams) (*protocol.AttachResult, error)
	List(context.Context) (*protocol.ListResult, error)
	Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error)
	Restart(context.Context, string) error
	Input(context.Context, uint16, []byte) error
	Resize(context.Context, uint16, uint16, uint16) error
	EnsureResize(context.Context, protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error)
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
		})
	}
	return TerminalListResult{Items: items}, nil
}

func (adapter ProtocolTerminalServiceAdapter) Create(ctx context.Context, req TerminalCreateRequest) (TerminalCreateResult, error) {
	if adapter.Client == nil {
		return TerminalCreateResult{}, ErrMissingTerminalClient
	}
	result, err := adapter.Client.Create(ctx, protocol.CreateParams{
		ID:      req.TerminalID,
		Name:    req.Title,
		Command: append([]string(nil), req.Command...),
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

func terminalPoolTitleFromProtocol(terminal protocol.TerminalInfo) string {
	if terminal.Name != "" {
		return terminal.Name
	}
	if terminal.ID != "" {
		return terminal.ID
	}
	return "terminal"
}
