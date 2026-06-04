package app

import (
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestShellReducerHandlesPanelPresentationSemanticActions(t *testing.T) {
	reducer := NewShellReducer()
	root, effects := reducer(state.Root{}, ShellTogglePanelPresentationMsg{})
	if len(effects) != 0 {
		t.Fatalf("shell reducer should not emit effects, got %#v", effects)
	}
	if root.Generation != 1 || root.Shell.PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("expected split line after toggle, got %#v", root)
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

func TestShellReducerIgnoresUnknownMessages(t *testing.T) {
	reducer := NewShellReducer()
	root, effects := reducer(state.Root{}, NoopMsg{})
	if root.Generation != 0 || len(effects) != 0 {
		t.Fatalf("unknown shell message should be ignored root=%#v effects=%#v", root, effects)
	}
}
