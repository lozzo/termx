package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/tui/input"
	"github.com/lozzow/termx/tui/render"
	"github.com/lozzow/termx/tui/state"
)

func TestShortcutBindingsHaveCanonicalInvocationsAndDispatcherHandlers(t *testing.T) {
	for _, binding := range input.BindingCatalog() {
		intent, ok := shortcutIntentForInvocation(binding.Invocation, input.InputEvent{})
		if !ok {
			t.Fatalf("shortcut binding %q invocation=%#v has no app dispatcher handler", binding.ID, binding.Invocation)
		}
		if intent.Kind == input.IntentNone || intent.Kind == input.IntentShortcutAction {
			t.Fatalf("shortcut binding %q did not dispatch to reducer intent: %#v", binding.ID, intent)
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
			key := firstFooterShortcutKey(token.Key)
			intent := input.RouteWithMode(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: key}, false, tc.input)
			if intent.Kind == input.IntentShortcutAction {
				intent, ok = shortcutIntentForInvocation(intent.Invocation, intent.Event)
				if !ok {
					t.Fatalf("%s footer key %q action %q has no dispatcher", tc.name, token.Key, token.ActionID)
				}
			}
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

func firstFooterShortcutKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) == 0 {
		return key
	}
	return strings.TrimSpace(parts[0])
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
	actionMsg, ok := msg.(ShellShortcutActionMsg)
	if !ok || actionMsg.Invocation.ID != "menu.copy" {
		t.Fatalf("copy footer action should emit canonical copy invocation, got %#v", msg)
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
