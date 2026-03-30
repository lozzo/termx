package workbench

import "testing"

func TestWorkbenchAddRemoveAndListWorkspaces(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{Name: "main"})
	wb.AddWorkspace("ops", &WorkspaceState{Name: "ops"})

	listed := wb.ListWorkspaces()
	if len(listed) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(listed))
	}
	if !wb.SwitchWorkspace("ops") {
		t.Fatal("expected switch to ops to succeed")
	}
	if current := wb.CurrentWorkspace(); current == nil || current.Name != "ops" {
		t.Fatalf("expected current workspace ops, got %#v", current)
	}

	wb.RemoveWorkspace("ops")
	if wb.SwitchWorkspace("ops") {
		t.Fatal("expected removed workspace switch to fail")
	}
}
