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

func TestTabWorkspaceFooterHintsMatchInputBindings(t *testing.T) {
	shell := state.DefaultShell()
	var result state.WorkbenchCommandResult
	shell, result = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("create tab fixture: %#v", result)
	}
	shell, result = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: "remote"})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("create workspace fixture: %#v", result)
	}

	cases := []struct {
		name   string
		mode   state.InteractionMode
		input  input.InteractionMode
		expect map[string]string
	}{
		{
			name:  "tab",
			mode:  state.InteractionModeTab,
			input: input.InteractionModeTab,
			expect: map[string]string{
				render.ActionTabCreate.String():   "tab create",
				render.ActionTabPrevious.String(): "tab previous",
				render.ActionTabNext.String():     "tab next",
				render.ActionTabRename.String():   "tab rename",
				render.ActionTabClose.String():    "tab close",
			},
		},
		{
			name:  "workspace",
			mode:  state.InteractionModeWorkspace,
			input: input.InteractionModeWorkspace,
			expect: map[string]string{
				render.ActionFooterNewWorkspace.String():      "workspace create",
				render.ActionFooterPreviousWorkspace.String(): "workspace previous",
				render.ActionFooterNextWorkspace.String():     "workspace next",
				render.ActionFooterRenameWorkspace.String():   "workspace rename",
				render.ActionFooterDeleteWorkspace.String():   "workspace delete confirm=accepted",
			},
		},
	}

	for _, tc := range cases {
		caseShell := shell.SetInteractionMode(tc.mode)
		if tc.mode == state.InteractionModeTab {
			var switchResult state.WorkbenchCommandResult
			caseShell, switchResult = caseShell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceSwitch, TargetID: state.DefaultWorkspaceID})
			if switchResult.Status != state.WorkbenchCommandOK {
				t.Fatalf("switch tab footer fixture: %#v", switchResult)
			}
			caseShell = caseShell.SetInteractionMode(tc.mode)
		}
		vm := render.NewRenderVMBuilder().Build(state.Root{Shell: caseShell})
		for _, token := range vm.Shell.Footer.ActionTokens {
			command, ok := tc.expect[token.ActionID]
			if !ok {
				continue
			}
			intent := input.RouteWithMode(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: token.Key}, false, tc.input)
			if intent.Kind != input.IntentWorkbenchCommand || intent.Command != command {
				t.Fatalf("%s footer key %q action %q should route to %q, got %#v", tc.name, token.Key, token.ActionID, command, intent)
			}
			delete(tc.expect, token.ActionID)
		}
		if len(tc.expect) != 0 {
			t.Fatalf("%s footer missed expected actions %#v in %#v", tc.name, tc.expect, vm.Shell.Footer.ActionTokens)
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

func TestActionCatalogPaneDetachAndQuitAdapterEffectsRemainMessages(t *testing.T) {
	reducer := NewShellReducer()
	root := actionCatalogDispatchRoot()

	next, effects := reducer(root, ShellContentActionMsg{ActionID: render.ActionPaneFooterDetach.String()})
	if len(effects) != 1 {
		t.Fatalf("pane detach footer action should persist through workbench command, root=%#v effects=%#v", next, effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background())
	persist, ok := msg.(WorkbenchStoragePersistRequestMsg)
	if !ok || persist.Reason != string(state.WorkbenchCommandPaneDetach) {
		t.Fatalf("pane detach footer action should emit detach persist request, got %#v", msg)
	}
	if next.Shell.EnsureDefaults().Workspace.Tabs[0].Panes[0].Kind != state.PaneEmpty {
		t.Fatalf("pane detach footer action should leave active pane unconnected, got %#v", next.Shell.Workspace.Tabs[0].Panes[0])
	}

	next, effects = reducer(root, ShellContentActionMsg{ActionID: render.ActionFooterQuit.String()})
	if next.Generation != root.Generation || len(effects) != 1 {
		t.Fatalf("quit footer action should stay as runtime message effect, root=%#v effects=%#v", next, effects)
	}
	if _, ok := effects[0].(FuncEffect).Run(context.Background()).(QuitMsg); !ok {
		t.Fatalf("quit footer action should emit QuitMsg, got %#v", effects[0])
	}
}

func TestActionCatalogPanePresentationFooterActionsUsePaneCommands(t *testing.T) {
	reducer := NewShellReducer()
	root := actionCatalogDispatchRoot()

	next, _ := reducer(root, ShellContentActionMsg{ActionID: render.ActionPaneFooterSplitLine.String()})
	if next.Shell.EnsureDefaults().PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("split-line footer action should update panel presentation, got %q", next.Shell.PanelPresentation)
	}
	next, _ = reducer(next, ShellContentActionMsg{ActionID: render.ActionPaneFooterCard.String()})
	if next.Shell.EnsureDefaults().PanelPresentation != state.PanelPresentationCard {
		t.Fatalf("card footer action should update panel presentation, got %q", next.Shell.PanelPresentation)
	}
}
