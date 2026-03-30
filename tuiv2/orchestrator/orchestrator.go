package orchestrator

import (
	"context"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type Orchestrator struct {
	workbench *workbench.Workbench
	runtime   *runtime.Runtime
	modalHost *modal.ModalHost
}

func New(wb *workbench.Workbench, rt *runtime.Runtime, mh *modal.ModalHost) *Orchestrator {
	return &Orchestrator{workbench: wb, runtime: rt, modalHost: mh}
}

func (o *Orchestrator) HandleSemanticAction(action input.SemanticAction) []Effect {
	switch action.Kind {
	case input.ActionOpenPicker:
		if o.modalHost != nil {
			o.modalHost.Open(input.ModePicker, action.TargetID)
		}
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: action.TargetID}},
			OpenPickerEffect{RequestID: action.TargetID},
		}
	default:
		return nil
	}
}

func (o *Orchestrator) AttachAndLoadSnapshot(ctx context.Context, paneID, terminalID, mode string, offset, limit int) ([]any, error) {
	terminal, err := o.runtime.AttachTerminal(ctx, paneID, terminalID, mode)
	if err != nil {
		return nil, err
	}
	snapshot, err := o.runtime.LoadSnapshot(ctx, terminalID, offset, limit)
	if err != nil {
		return nil, err
	}
	msgs := []any{
		TerminalAttachedMsg{PaneID: paneID, TerminalID: terminalID, Channel: terminal.Channel},
		SnapshotLoadedMsg{TerminalID: terminalID, Snapshot: snapshot},
	}
	if o.modalHost != nil && o.modalHost.Session != nil {
		o.modalHost.MarkReady(o.modalHost.Session.Kind, o.modalHost.Session.RequestID)
	}
	return msgs, nil
}
