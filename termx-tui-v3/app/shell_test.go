package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestShellReducerHandlesPanelPresentationSemanticActions(t *testing.T) {
	reducer := NewShellReducer()
	root, effects := reducer(state.Root{}, ShellTogglePanelPresentationMsg{})
	if root.Generation != 1 || root.Shell.PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("expected split line after toggle, got %#v", root)
	}
	if len(effects) != 1 {
		t.Fatalf("expected pane command feedback effect, got %#v", effects)
	}

	root, _ = reducer(root, ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationCard})
	if root.Generation != 2 || root.Shell.PanelPresentation != state.PanelPresentationCard {
		t.Fatalf("expected card after set, got %#v", root)
	}
}

func TestShellReducerHandlesHeaderFooterVisibilitySemanticActions(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell()}

	root, _ = reducer(root, ShellSetHeaderVisibleMsg{Visible: false})
	root, _ = reducer(root, ShellSetFooterVisibleMsg{Visible: false})
	if root.Shell.HeaderVisible || root.Shell.FooterVisible {
		t.Fatalf("expected hidden header/footer, got %#v", root.Shell)
	}

	root, _ = reducer(root, ShellToggleHeaderVisibleMsg{})
	root, _ = reducer(root, ShellToggleFooterVisibleMsg{})
	if !root.Shell.HeaderVisible || !root.Shell.FooterVisible {
		t.Fatalf("expected restored header/footer, got %#v", root.Shell)
	}
}

func TestShellReducerHandlesToastLifecycleSemanticActions(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell()}

	root, _ = reducer(root, ShellAddToastMsg{Toast: state.ToastSpec{ID: "a", Severity: state.ToastInfo, Title: "ready"}})
	root, _ = reducer(root, ShellAddToastMsg{Toast: state.ToastSpec{ID: "b", Severity: state.ToastWarning, Title: "wait", Pending: true, DismissAfterTicks: 2}})
	if len(root.Shell.Toasts) != 2 || !root.Shell.Toasts[1].Pending {
		t.Fatalf("expected two toasts, got %#v", root.Shell.Toasts)
	}

	root, _ = reducer(root, ShellTickToastsMsg{Ticks: 2})
	if len(root.Shell.Toasts) != 1 || root.Shell.Toasts[0].ID != "a" {
		t.Fatalf("expected auto-dismissed pending toast, got %#v", root.Shell.Toasts)
	}

	root, _ = reducer(root, ShellAddToastMsg{Toast: state.ToastSpec{ID: "c", Severity: state.ToastSuccess, Title: "done"}})
	root, _ = reducer(root, ShellCloseCurrentToastMsg{})
	if len(root.Shell.Toasts) != 1 || root.Shell.Toasts[0].ID != "a" {
		t.Fatalf("expected close current toast, got %#v", root.Shell.Toasts)
	}

	root, _ = reducer(root, ShellClearToastsMsg{})
	if len(root.Shell.Toasts) != 0 {
		t.Fatalf("expected clear all toasts, got %#v", root.Shell.Toasts)
	}
}

func TestShellReducerHandlesTerminalPickerOverlaySemanticActions(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell()}

	root, _ = reducer(root, ShellOpenTerminalPickerMsg{})
	if !root.Shell.Overlay.Open || root.Shell.Overlay.Kind != state.OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", root.Shell.Overlay)
	}
	if root.Shell.Overlay.TargetID != state.DefaultPaneID {
		t.Fatalf("expected overlay target active pane, got %#v", root.Shell.Overlay)
	}

	root, _ = reducer(root, ShellCloseOverlayMsg{})
	if root.Shell.Overlay.Open {
		t.Fatalf("expected closed overlay, got %#v", root.Shell.Overlay)
	}
}

func TestShellReducerHandlesPaneSplitSemanticAction(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell()}

	root, _ = reducer(root, ShellSplitActivePaneMsg{
		Pane:      state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive},
		Direction: state.SplitDirectionVertical,
	})

	tab := root.Shell.Workspace.Tabs[0]
	if root.Shell.ActivePaneID != "pane-2" || len(tab.Panes) != 2 {
		t.Fatalf("expected split pane state, got %#v", root.Shell)
	}
	if tab.RootSplit.Direction != state.SplitDirectionVertical {
		t.Fatalf("expected vertical split, got %#v", tab.RootSplit)
	}
}

func TestShellReducerRoutesUnifiedPaneCommandContract(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell()}

	root, effects := reducer(root, ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:         state.PaneCommandSplit,
		SplitDirection: state.SplitDirectionHorizontal,
		NewPane:        state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive},
		Source:         state.PaneCommandSourceTest,
	}})

	if root.Shell.ActivePaneID != "pane-2" || len(root.Shell.Workspace.Tabs[0].Panes) != 2 {
		t.Fatalf("expected split through unified contract, got %#v", root.Shell)
	}
	if len(effects) != 1 {
		t.Fatalf("expected command feedback effect, got %#v", effects)
	}
	feedback, ok := effects[0].(PaneCommandFeedbackEffect)
	if !ok || feedback.Result.Status != state.PaneCommandOK || feedback.Command.Target.PaneID != state.DefaultPaneID {
		t.Fatalf("unexpected command feedback %#v", effects[0])
	}
}

func TestShellReducerWorkbenchCommandAndPromptRename(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell()}

	root, effects := reducer(root, ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"}})
	if len(effects) != 0 {
		t.Fatalf("workbench command should not emit effects, got %#v", effects)
	}
	if len(root.Shell.Workspace.Tabs) != 2 || root.Shell.Workspace.Tabs[1].Title != "logs" {
		t.Fatalf("expected created tab, got %#v", root.Shell.Workspace.Tabs)
	}

	root.Shell = root.Shell.OpenPrompt(state.PromptState{Purpose: "tab.rename", Value: "构建"})
	root, effects = reducer(root, ShellPromptSubmitMsg{})
	if len(effects) != 1 {
		t.Fatalf("expected prompt rename effect, got %#v", effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, msg)
	if root.Shell.Workspace.Tabs[1].Title != "构建" {
		t.Fatalf("expected prompt to rename active tab, got %#v", root.Shell.Workspace.Tabs)
	}

	root, _ = reducer(root, ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: "remote"}})
	if len(root.Shell.Workspaces) != 2 || root.Shell.Workspace.Name != "remote" {
		t.Fatalf("expected created workspace, got %#v", root.Shell)
	}
}

func TestShellReducerRejectsInvalidPaneCommandWithoutStateMutation(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell()}

	next, effects := reducer(root, ShellPaneCommandMsg{Command: state.PaneCommand{
		Action: state.PaneCommandFocus,
		Target: state.PaneCommandTarget{PaneID: "missing"},
		Source: state.PaneCommandSourceMouse,
	}})

	if next.Generation != root.Generation+1 || next.Shell.ActivePaneID != root.Shell.ActivePaneID || len(next.Shell.Workspace.Tabs[0].Panes) != len(root.Shell.Workspace.Tabs[0].Panes) {
		t.Fatalf("invalid command must not mutate pane tree, got %#v", next)
	}
	if len(next.Shell.Toasts) != 1 || next.Shell.Toasts[0].Body != "target pane not found" {
		t.Fatalf("invalid command should show toast feedback, got %#v", next.Shell.Toasts)
	}
	if len(effects) != 1 {
		t.Fatalf("expected one feedback effect, got %#v", effects)
	}
	feedback, ok := effects[0].(PaneCommandFeedbackEffect)
	if !ok || feedback.Result.Status != state.PaneCommandInvalid {
		t.Fatalf("expected invalid feedback, got %#v", effects[0])
	}
}

func TestShellReducerKeepsCloseAndKillBehindConfirmPolicy(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2", TerminalID: "term-2"}, state.SplitDirectionVertical)}

	next, effects := reducer(root, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandCloseAndKill, Target: state.PaneCommandTarget{PaneID: "pane-2"}}})
	if next.Generation != root.Generation+1 || len(next.Shell.Workspace.Tabs[0].Panes) != len(root.Shell.Workspace.Tabs[0].Panes) {
		t.Fatalf("unconfirmed kill must not mutate pane tree, got %#v", next)
	}
	if len(next.Shell.Toasts) != 1 || !next.Shell.Toasts[0].Pending {
		t.Fatalf("unconfirmed kill should show pending confirmation toast, got %#v", next.Shell.Toasts)
	}
	if len(effects) != 1 {
		t.Fatalf("expected confirmation feedback only, got %#v", effects)
	}
	feedback, ok := effects[0].(PaneCommandFeedbackEffect)
	if !ok || feedback.Result.Status != state.PaneCommandNeedsConfirmation {
		t.Fatalf("expected confirmation feedback, got %#v", effects[0])
	}

	next, effects = reducer(root, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandCloseAndKill, Target: state.PaneCommandTarget{PaneID: "pane-2"}, Confirm: state.PaneConfirmAccepted}})
	if next.Generation != root.Generation+1 {
		t.Fatalf("accepted command should advance state boundary, got %#v", next)
	}
	if len(next.Shell.Workspace.Tabs[0].Panes) != 1 || next.Shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("accepted close-and-kill should remove pane from shell state, got %#v", next.Shell)
	}
	if len(effects) != 2 {
		t.Fatalf("expected feedback and terminal kill effect boundary, got %#v", effects)
	}
	kill, ok := effects[1].(PaneTerminalKillEffect)
	if !ok || kill.TerminalID != "term-2" || kill.PaneID != "pane-2" {
		t.Fatalf("expected terminal kill effect boundary, got %#v", effects[1])
	}
}

func TestShellReducerHandlesCloseFocusAndZoomPaneCommands(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs"}, state.SplitDirectionHorizontal)}

	next, effects := reducer(root, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandFocus, Target: state.PaneCommandTarget{PaneID: state.DefaultPaneID}}})
	if next.Shell.ActivePaneID != state.DefaultPaneID || len(effects) != 1 {
		t.Fatalf("expected focus command, root=%#v effects=%#v", next, effects)
	}

	next, effects = reducer(next, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandZoom, Target: state.PaneCommandTarget{PaneID: "pane-2"}}})
	if next.Shell.ZoomedPaneID != "pane-2" || next.Shell.ActivePaneID != "pane-2" || len(effects) != 1 {
		t.Fatalf("expected zoom command, root=%#v effects=%#v", next, effects)
	}

	next, effects = reducer(next, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandClose, Target: state.PaneCommandTarget{PaneID: "pane-2"}}})
	if next.Shell.ZoomedPaneID != "" || next.Shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) || len(next.Shell.Workspace.Tabs[0].Panes) != 1 || len(effects) != 1 {
		t.Fatalf("expected close to prune pane and zoom state, root=%#v effects=%#v", next, effects)
	}
}

func TestShellReducerAppliesPaneResizeSetSizeAndBalanceGeometry(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2"}, state.SplitDirectionVertical)}

	next, effects := reducer(root, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandResize, Target: state.PaneCommandTarget{PaneID: "pane-2"}, ResizeDirection: state.PaneResizeLeft, Delta: 3, Source: state.PaneCommandSourceKeyboard}})
	if next.Generation != root.Generation+1 || next.Shell.Workspace.Tabs[0].RootSplit.BiasCells != -3 {
		t.Fatalf("expected resize geometry command, root=%#v effects=%#v", next, effects)
	}
	feedback, ok := effects[0].(PaneCommandFeedbackEffect)
	if len(effects) != 1 || !ok || feedback.Result.Status != state.PaneCommandOK {
		t.Fatalf("expected ok resize feedback, got %#v", effects)
	}

	next, effects = reducer(next, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandSetSize, Target: state.PaneCommandTarget{PaneID: "pane-2"}, SizeMode: state.PaneSizeRatio, Ratio: 0.4, Source: state.PaneCommandSourceCLIMini}})
	if next.Shell.Workspace.Tabs[0].RootSplit.Ratio != 0.6 {
		t.Fatalf("expected set-size ratio geometry, got %#v effects=%#v", next.Shell.Workspace.Tabs[0].RootSplit, effects)
	}

	next, effects = reducer(next, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandBalance, Target: state.PaneCommandTarget{PaneID: "pane-2"}, Source: state.PaneCommandSourceMouse}})
	rootSplit := next.Shell.Workspace.Tabs[0].RootSplit
	if rootSplit.Ratio != 0 || rootSplit.BiasCells != 0 || rootSplit.FixedPaneID != "" {
		t.Fatalf("expected balanced geometry, got %#v effects=%#v", rootSplit, effects)
	}
}

func TestShellReducerSuppressesLowValueSuccessToasts(t *testing.T) {
	reducer := NewShellReducer()
	root := state.Root{Shell: state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2"}, state.SplitDirectionVertical)}

	next, _ := reducer(root, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandResize, Target: state.PaneCommandTarget{PaneID: "pane-2"}, ResizeDirection: state.PaneResizeLeft, Delta: 2, Source: state.PaneCommandSourceMouse}})
	if len(next.Shell.Toasts) != 0 {
		t.Fatalf("mouse resize success should not show toast, got %#v", next.Shell.Toasts)
	}

	next, _ = reducer(next, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandFocus, Target: state.PaneCommandTarget{PaneID: state.DefaultPaneID}, Source: state.PaneCommandSourceMouse}})
	if len(next.Shell.Toasts) != 0 {
		t.Fatalf("focus success should be visual/footer feedback only, got %#v", next.Shell.Toasts)
	}

	next, _ = reducer(next, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandSplit, Target: state.PaneCommandTarget{PaneID: state.DefaultPaneID}, SplitDirection: state.SplitDirectionHorizontal, NewPane: state.PaneState{ID: "pane-3"}, Source: state.PaneCommandSourceMouse}})
	if len(next.Shell.Toasts) != 1 || next.Shell.Toasts[0].Title != string(state.PaneCommandSplit) {
		t.Fatalf("discrete split action should keep toast feedback, got %#v", next.Shell.Toasts)
	}

	floating, result := next.Shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "floating"},
		BoundsW:  80,
		BoundsH:  24,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating setup failed: %#v", result)
	}
	next.Shell = floating.ClearToasts()
	next, _ = reducer(next, ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandMove, TargetID: "floating-1", DeltaX: 1, Source: state.PaneCommandSourceMouse}})
	next, _ = reducer(next, ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandResize, TargetID: "floating-1", DeltaW: 1, Source: state.PaneCommandSourceMouse}})
	if len(next.Shell.Toasts) != 0 {
		t.Fatalf("floating drag move/resize success should not show toast, got %#v", next.Shell.Toasts)
	}

	next, _ = reducer(next, ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandMove, TargetID: "missing", DeltaX: 1, Source: state.PaneCommandSourceMouse}})
	if len(next.Shell.Toasts) != 1 || next.Shell.Toasts[0].Severity != state.ToastWarning {
		t.Fatalf("invalid floating command should still show warning toast, got %#v", next.Shell.Toasts)
	}
}

func TestShellReducerIgnoresUnknownMessages(t *testing.T) {
	reducer := NewShellReducer()
	root, effects := reducer(state.Root{}, NoopMsg{})
	if root.Generation != 0 || len(effects) != 0 {
		t.Fatalf("unknown shell message should be ignored root=%#v effects=%#v", root, effects)
	}
}
