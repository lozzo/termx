package state

import "testing"

func TestDefaultShellOwnsWorkbenchTreeAndChromeState(t *testing.T) {
	shell := DefaultShell()

	if shell.Workspace.ID != DefaultWorkspaceID || shell.Workspace.ActiveTabID != DefaultTabID {
		t.Fatalf("unexpected default workspace %#v", shell.Workspace)
	}
	if len(shell.Workspace.Tabs) != 1 || len(shell.Workspace.Tabs[0].Panes) != 1 {
		t.Fatalf("expected one tab and one pane, got %#v", shell.Workspace)
	}
	if shell.ActivePaneID != DefaultPaneID {
		t.Fatalf("unexpected active pane %q", shell.ActivePaneID)
	}
	if shell.PanelPresentation != PanelPresentationCard {
		t.Fatalf("expected card presentation, got %q", shell.PanelPresentation)
	}
	if !shell.HeaderVisible || !shell.FooterVisible {
		t.Fatalf("header/footer must default visible, got header=%v footer=%v", shell.HeaderVisible, shell.FooterVisible)
	}
	if !shell.Workspace.Tabs[0].Panes[0].Active {
		t.Fatalf("default pane must be active %#v", shell.Workspace.Tabs[0].Panes[0])
	}
}

func TestShellPanelPresentationCanToggleBetweenCardAndSplit(t *testing.T) {
	shell := DefaultShell()

	shell = shell.TogglePanelPresentation()
	if shell.PanelPresentation != PanelPresentationSplitLine {
		t.Fatalf("expected split line, got %q", shell.PanelPresentation)
	}
	shell = shell.TogglePanelPresentation()
	if shell.PanelPresentation != PanelPresentationCard {
		t.Fatalf("expected card, got %q", shell.PanelPresentation)
	}
	shell = shell.SetPanelPresentation("invalid")
	if shell.PanelPresentation != PanelPresentationCard {
		t.Fatalf("invalid presentation should be ignored, got %q", shell.PanelPresentation)
	}
}

func TestShellHeaderFooterVisibilityCanBeHiddenAndRestored(t *testing.T) {
	shell := DefaultShell()

	shell = shell.SetHeaderVisible(false).SetFooterVisible(false)
	if shell.HeaderVisible || shell.FooterVisible {
		t.Fatalf("expected hidden header/footer, got header=%v footer=%v", shell.HeaderVisible, shell.FooterVisible)
	}
	shell = shell.ToggleHeaderVisible().ToggleFooterVisible()
	if !shell.HeaderVisible || !shell.FooterVisible {
		t.Fatalf("expected restored header/footer, got header=%v footer=%v", shell.HeaderVisible, shell.FooterVisible)
	}
}

func TestShellToastLifecycle(t *testing.T) {
	shell := DefaultShell().
		AddToast(ToastSpec{ID: "keep", Severity: ToastInfo, Title: "sync"}).
		AddToast(ToastSpec{ID: "drop", Severity: ToastWarning, Title: "warn", DismissAfterTicks: 2})

	shell = shell.TickToasts(1)
	if len(shell.Toasts) != 2 || shell.Toasts[1].AgeTicks != 1 {
		t.Fatalf("expected both toasts after one tick, got %#v", shell.Toasts)
	}
	shell = shell.TickToasts(1)
	if len(shell.Toasts) != 1 || shell.Toasts[0].ID != "keep" {
		t.Fatalf("expected auto-dismissed toast, got %#v", shell.Toasts)
	}
	shell = shell.AddToast(ToastSpec{ID: "close", Severity: ToastSuccess, Title: "done"})
	shell = shell.CloseCurrentToast()
	if len(shell.Toasts) != 1 || shell.Toasts[0].ID != "keep" {
		t.Fatalf("expected close current to remove latest toast, got %#v", shell.Toasts)
	}
	shell = shell.ClearToasts()
	if len(shell.Toasts) != 0 {
		t.Fatalf("expected clear all toasts, got %#v", shell.Toasts)
	}
}

func TestShellToastAutoIDAndDefaultSeverity(t *testing.T) {
	shell := DefaultShell().AddToast(ToastSpec{Title: "hello"})
	if len(shell.Toasts) != 1 {
		t.Fatalf("expected one toast, got %#v", shell.Toasts)
	}
	if shell.Toasts[0].ID != "toast-1" || shell.Toasts[0].Severity != ToastInfo {
		t.Fatalf("unexpected generated toast %#v", shell.Toasts[0])
	}
}

func TestShellTerminalPickerOverlayTargetsActivePane(t *testing.T) {
	shell := DefaultShell().OpenTerminalPicker()

	if !shell.Overlay.Open || shell.Overlay.Kind != OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", shell.Overlay)
	}
	if shell.Overlay.TargetID != DefaultPaneID {
		t.Fatalf("expected overlay target active pane, got %#v", shell.Overlay)
	}
	shell = shell.CloseOverlay()
	if shell.Overlay.Open || shell.Overlay.Kind != OverlayNone {
		t.Fatalf("expected closed overlay, got %#v", shell.Overlay)
	}
}

func TestShellSplitActivePaneCreatesMinimalPaneTree(t *testing.T) {
	shell := DefaultShell().SplitActivePane(PaneState{
		ID:         "pane-2",
		Title:      "logs",
		Kind:       PaneTerminalLive,
		TerminalID: "term-2",
	}, SplitDirectionHorizontal)

	tab := shell.Workspace.Tabs[0]
	if len(tab.Panes) != 2 {
		t.Fatalf("expected two panes, got %#v", tab.Panes)
	}
	if shell.ActivePaneID != "pane-2" || tab.ActivePaneID != "pane-2" {
		t.Fatalf("expected new pane active, shell=%#v tab=%#v", shell, tab)
	}
	if tab.RootSplit.Direction != SplitDirectionHorizontal || len(tab.RootSplit.Children) != 2 {
		t.Fatalf("expected horizontal root split, got %#v", tab.RootSplit)
	}
	if tab.RootSplit.Children[0].PaneID != DefaultPaneID || tab.RootSplit.Children[1].PaneID != "pane-2" {
		t.Fatalf("unexpected split children %#v", tab.RootSplit.Children)
	}
	if !tab.Panes[1].Active || tab.Panes[0].Active {
		t.Fatalf("expected only new pane active, got %#v", tab.Panes)
	}
}

func TestShellSplitActivePaneSupportsVerticalDirection(t *testing.T) {
	shell := DefaultShell().SplitActivePane(PaneState{ID: "pane-2"}, SplitDirectionVertical)
	if shell.Workspace.Tabs[0].RootSplit.Direction != SplitDirectionVertical {
		t.Fatalf("expected vertical split, got %#v", shell.Workspace.Tabs[0].RootSplit)
	}
}

func TestShellFocusCloseAndZoomPaneState(t *testing.T) {
	shell := DefaultShell().
		SplitActivePane(PaneState{ID: "pane-2", Title: "logs", Kind: PaneTerminalLive, TerminalID: "term-2"}, SplitDirectionVertical).
		SplitActivePane(PaneState{ID: "pane-3", Title: "build", Kind: PaneTerminalLive, TerminalID: "term-3"}, SplitDirectionHorizontal)

	shell = shell.FocusPane(PaneCommandTarget{PaneID: DefaultPaneID})
	if shell.ActivePaneID != DefaultPaneID || !shell.Workspace.Tabs[0].Panes[0].Active {
		t.Fatalf("expected focused default pane, got %#v", shell)
	}

	shell = shell.ZoomPane(PaneCommandTarget{PaneID: "pane-2"})
	if shell.ZoomedPaneID != "pane-2" || shell.ActivePaneID != "pane-2" {
		t.Fatalf("expected zoomed pane-2, got %#v", shell)
	}
	shell = shell.UnzoomPane()
	if shell.ZoomedPaneID != "" {
		t.Fatalf("expected unzoomed shell, got %#v", shell)
	}

	shell = shell.ClosePane(PaneCommandTarget{PaneID: "pane-2"})
	tab := shell.Workspace.Tabs[0]
	if len(tab.Panes) != 2 || shell.HasPane(PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("expected pane-2 closed, got %#v", shell)
	}
	if containsPaneID(tab.RootSplit, "pane-2") {
		t.Fatalf("closed pane must be removed from split tree, got %#v", tab.RootSplit)
	}
}

func TestShellCloseLastPaneIsIgnoredByStateMethod(t *testing.T) {
	shell := DefaultShell().ClosePane(PaneCommandTarget{PaneID: DefaultPaneID})
	if len(shell.Workspace.Tabs[0].Panes) != 1 || shell.ActivePaneID != DefaultPaneID {
		t.Fatalf("last pane close must be ignored, got %#v", shell)
	}
}

func TestShellUpdatesDoNotMutatePreviousSlices(t *testing.T) {
	original := DefaultShell().AddToast(ToastSpec{ID: "old"})
	next := original.AddToast(ToastSpec{ID: "new"})
	next.Workspace.Tabs[0].Panes[0].Title = "mutated"

	if len(original.Toasts) != 1 || original.Toasts[0].ID != "old" {
		t.Fatalf("original toasts mutated: %#v next=%#v", original.Toasts, next.Toasts)
	}
	if original.Workspace.Tabs[0].Panes[0].Title != "shell" {
		t.Fatalf("original pane mutated: %#v", original.Workspace.Tabs[0].Panes[0])
	}
}

func containsPaneID(node SplitNode, paneID string) bool {
	if node.PaneID == paneID {
		return true
	}
	for _, child := range node.Children {
		if containsPaneID(child, paneID) {
			return true
		}
	}
	return false
}
