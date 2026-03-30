package runtime

import (
	"context"
	"fmt"
	"slices"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared"
)

type Runtime struct {
	registry *TerminalRegistry
	bindings map[string]*PaneBinding
	client   bridge.Client
}

func New(client bridge.Client) *Runtime {
	return &Runtime{
		registry: NewTerminalRegistry(),
		bindings: make(map[string]*PaneBinding),
		client:   client,
	}
}

func (r *Runtime) Client() bridge.Client {
	if r == nil {
		return nil
	}
	return r.client
}

func (r *Runtime) Registry() *TerminalRegistry {
	if r == nil {
		return nil
	}
	return r.registry
}

func (r *Runtime) Binding(paneID string) *PaneBinding {
	if r == nil {
		return nil
	}
	return r.bindings[paneID]
}

func (r *Runtime) BindPane(paneID string) *PaneBinding {
	if r == nil || paneID == "" {
		return nil
	}
	binding := r.bindings[paneID]
	if binding != nil {
		return binding
	}
	binding = &PaneBinding{PaneID: paneID}
	r.bindings[paneID] = binding
	return binding
}

func (r *Runtime) ListTerminals(ctx context.Context) ([]protocol.TerminalInfo, error) {
	if r == nil || r.client == nil {
		return nil, shared.UserVisibleError{Op: "list terminals", Err: fmt.Errorf("runtime client is nil")}
	}
	result, err := r.client.List(ctx)
	if err != nil {
		return nil, shared.UserVisibleError{Op: "list terminals", Err: err}
	}
	for _, info := range result.Terminals {
		r.registry.UpsertTerminalInfo(info)
	}
	return append([]protocol.TerminalInfo(nil), result.Terminals...), nil
}

func (r *Runtime) Visible() *VisibleRuntime {
	if r == nil || r.registry == nil {
		return nil
	}
	visible := &VisibleRuntime{Terminals: make([]VisibleTerminal, 0, len(r.registry.terminals))}
	for _, terminalID := range r.registry.IDs() {
		terminal := r.registry.Get(terminalID)
		if terminal == nil {
			continue
		}
		visible.Terminals = append(visible.Terminals, VisibleTerminal{
			TerminalID:   terminal.TerminalID,
			Name:         terminal.Name,
			State:        terminal.State,
			AttachMode:   terminal.AttachMode,
			OwnerPaneID:  terminal.OwnerPaneID,
			BoundPaneIDs: slices.Clone(terminal.BoundPaneIDs),
		})
	}
	return visible
}
