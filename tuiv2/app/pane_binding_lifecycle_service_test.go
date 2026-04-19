package app

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/runtime"
)

func TestPaneBindingLifecycleServiceDetachClearsWorkbenchAndRuntime(t *testing.T) {
	model := setupModel(t, modelOpts{})
	term := model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected term-1 runtime")
	}
	term.OwnerPaneID = "pane-1"
	term.BoundPaneIDs = []string{"pane-1"}
	binding := model.runtime.Binding("pane-1")
	if binding == nil {
		t.Fatal("expected pane-1 binding")
	}
	binding.Role = runtime.BindingRoleOwner

	service := model.paneBindingLifecycleService()
	target, err := service.detach("pane-1")
	if err != nil {
		t.Fatalf("detach: %v", err)
	}
	if target.PaneID != "pane-1" || target.TerminalID != "term-1" {
		t.Fatalf("unexpected detach target: %#v", target)
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected pane-1 detached in workbench, got %#v", pane)
	}
	if got := model.runtime.Binding("pane-1"); got != nil {
		t.Fatalf("expected runtime binding removed, got %#v", got)
	}
	if term.OwnerPaneID != "" || len(term.BoundPaneIDs) != 0 {
		t.Fatalf("expected detached terminal ownership cleared, got %#v", term)
	}
}

func TestPaneBindingLifecycleServiceReconnectKeepsExitedBinding(t *testing.T) {
	model := setupModel(t, modelOpts{})
	term := model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected term-1 runtime")
	}
	term.State = "exited"
	term.OwnerPaneID = "pane-1"
	term.BoundPaneIDs = []string{"pane-1"}
	binding := model.runtime.Binding("pane-1")
	if binding == nil {
		t.Fatal("expected pane-1 binding")
	}
	binding.Role = runtime.BindingRoleOwner

	service := model.paneBindingLifecycleService()
	result, err := service.reconnect("pane-1")
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if !result.KeptExitedTerminal {
		t.Fatalf("expected exited binding to be preserved, got %#v", result)
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" {
		t.Fatalf("expected exited pane to remain bound, got %#v", pane)
	}
	if got := model.runtime.Binding("pane-1"); got == nil {
		t.Fatal("expected runtime binding retained for exited reconnect")
	}
	if term.OwnerPaneID != "pane-1" || len(term.BoundPaneIDs) != 1 || term.BoundPaneIDs[0] != "pane-1" {
		t.Fatalf("expected exited terminal ownership preserved, got %#v", term)
	}
}

func TestPaneBindingLifecycleServiceCloseRemovesPaneAndUnbindsRuntime(t *testing.T) {
	model := setupTwoPaneModel(t)
	term := model.runtime.Registry().Get("term-2")
	if term == nil {
		t.Fatal("expected term-2 runtime")
	}
	term.OwnerPaneID = "pane-2"
	term.BoundPaneIDs = []string{"pane-2"}
	binding := model.runtime.Binding("pane-2")
	if binding == nil {
		t.Fatal("expected pane-2 binding")
	}
	binding.Role = runtime.BindingRoleOwner

	service := model.paneBindingLifecycleService()
	target, err := service.close("pane-2")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if target.PaneID != "pane-2" || target.TerminalID != "term-2" {
		t.Fatalf("unexpected close target: %#v", target)
	}
	tab := model.workbench.CurrentTab()
	if tab == nil || len(tab.Panes) != 1 || tab.Panes["pane-2"] != nil {
		t.Fatalf("expected pane-2 removed from workbench, got %#v", tab)
	}
	if got := model.runtime.Binding("pane-2"); got != nil {
		t.Fatalf("expected runtime binding removed, got %#v", got)
	}
	if term.OwnerPaneID != "" || len(term.BoundPaneIDs) != 0 {
		t.Fatalf("expected closed terminal ownership cleared, got %#v", term)
	}
}
