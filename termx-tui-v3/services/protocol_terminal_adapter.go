package services

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/internal/protocol"
)

type ProtocolTerminalClient interface {
	AttachWithOptions(context.Context, protocol.AttachParams) (*protocol.AttachResult, error)
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
