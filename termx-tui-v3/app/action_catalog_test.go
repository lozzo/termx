package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestActionCatalogDispatchAppActionsReachReducerAdapter(t *testing.T) {
	reducer := NewShellReducer()
	for _, spec := range render.ActionSpecCatalog() {
		if spec.Dispatch != render.ActionDispatchApp || spec.HasSurface(render.ActionSurfaceInput) {
			continue
		}
		root := actionCatalogDispatchRoot()
		next, _ := reducer(root, ShellContentActionMsg{ActionID: spec.ID.String(), PaneID: state.DefaultPaneID, Row: 0})
		if hasUnknownActionToast(next, spec.ID.String()) {
			t.Fatalf("catalog action %q declares app dispatch but reducer falls through to unknown action toast", spec.ID)
		}
	}
}

func TestInputShellActionsMapToActionCatalog(t *testing.T) {
	for _, binding := range input.BindingCatalog() {
		if binding.Intent != input.IntentShellAction {
			continue
		}
		actionID, ok := actionIDForShellAction(binding.Action, binding.Reason)
		if !ok {
			t.Fatalf("shell binding %q action=%q reason=%q must map to ActionSpecCatalog", binding.ID, binding.Action, binding.Reason)
		}
		spec, ok := render.ActionSpecByID(actionID)
		if !ok {
			t.Fatalf("shell binding %q maps to unregistered action id %q", binding.ID, actionID)
		}
		if spec.Dispatch == render.ActionDispatchNone {
			t.Fatalf("shell binding %q maps to action without dispatch semantics: %#v", binding.ID, spec)
		}
	}
}

func actionCatalogDispatchRoot() state.Root {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		OpenTerminalPool()
	pool := state.TerminalPoolStore{}
	pool = pool.RequestList()
	pool, _ = pool.ApplyList(pool.RequestSeq, []state.TerminalPoolItem{{TerminalID: "term-1", Title: "main", State: "running"}}, "")
	return state.Root{
		Shell:        shell,
		Viewport:     state.ViewportStore{Cols: 80, Rows: 24, Valid: true},
		TerminalPool: pool,
		Session:      state.TerminalSessionStore{TerminalID: "term-1"},
	}
}

func hasUnknownActionToast(root state.Root, actionID string) bool {
	for _, toast := range root.Shell.Toasts {
		if toast.Title == "content action" && strings.Contains(toast.Body, "unknown "+actionID) {
			return true
		}
	}
	return false
}

func TestActionCatalogCopyAndModeAdapterEffectsRemainMessages(t *testing.T) {
	reducer := NewShellReducer()
	root := actionCatalogDispatchRoot()
	next, effects := reducer(root, ShellContentActionMsg{ActionID: render.ActionFooterCopyMode.String()})
	if next.Generation != root.Generation || len(effects) != 1 {
		t.Fatalf("copy footer action should remain a message adapter effect, root=%#v effects=%#v", next, effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background())
	inputMsg, ok := msg.(InputMsg)
	if !ok || inputMsg.Event.Kind != input.EventKindKey || inputMsg.Event.Char != "v" || !inputMsg.Event.Ctrl {
		t.Fatalf("copy footer action should emit ctrl-v input msg, got %#v", msg)
	}
}
