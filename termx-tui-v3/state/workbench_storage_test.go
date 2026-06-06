package state

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestWorkbenchStorageSnapshotRoundTripsShellStructure(t *testing.T) {
	shell := DefaultShell().
		SetPanelPresentation(PanelPresentationSplitLine).
		SetFooterVisible(false).
		SplitActivePane(PaneState{ID: "pane-logs", Title: "日志🚀", Kind: PaneTerminalLive, TerminalID: "term-logs"}, SplitDirectionVertical).
		FocusPane(PaneCommandTarget{PaneID: "pane-logs"}).
		AddToast(ToastSpec{Title: "runtime-only"})
	var result WorkbenchCommandResult
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandTabCreate, Name: "build"})
	if result.Status != WorkbenchCommandOK {
		t.Fatalf("create tab: %#v", result)
	}
	shell, result = shell.ApplyWorkbenchCommand(WorkbenchCommand{Action: WorkbenchCommandWorkspaceCreate, Name: "remote"})
	if result.Status != WorkbenchCommandOK {
		t.Fatalf("create workspace: %#v", result)
	}
	var floatingResult FloatingCommandResult
	shell, floatingResult = shell.ApplyFloatingCommand(FloatingCommand{
		Action: FloatingCommandCreate,
		Pane:   PaneState{ID: "float-pane", Title: "scratch", Kind: PaneEmpty},
		Title:  "scratch",
		Rect:   FloatingRect{X: 4, Y: 3, W: 40, H: 10},
	})
	if floatingResult.Status != FloatingCommandOK {
		t.Fatalf("create floating: %#v", floatingResult)
	}

	data, err := EncodeWorkbenchStorageSnapshot(shell)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	if json.Valid(data) == false {
		t.Fatalf("snapshot must be JSON: %s", data)
	}
	snapshot, err := DecodeWorkbenchStorageSnapshot(data)
	if err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	restored, err := snapshot.ToShellStore()
	if err != nil {
		t.Fatalf("restore shell: %v", err)
	}

	if restored.Workspace.ID != shell.Workspace.ID || restored.ActivePaneID != shell.ActivePaneID || restored.PanelPresentation != PanelPresentationSplitLine {
		t.Fatalf("restored shell mismatch, restored=%#v shell=%#v", restored, shell)
	}
	if restored.FooterVisible || !restored.HeaderVisible {
		t.Fatalf("chrome visibility must round trip, restored=%#v", restored)
	}
	if len(restored.Workspaces) != len(shell.Workspaces) || len(restored.Floatings) != 1 || restored.ActiveFloatingID != shell.ActiveFloatingID {
		t.Fatalf("workspace/floating structure mismatch, restored=%#v shell=%#v", restored, shell)
	}
	if len(restored.Toasts) != 0 || restored.Overlay.Open {
		t.Fatalf("storage snapshot must not persist runtime-only toast/overlay, restored=%#v", restored)
	}
}

func TestWorkbenchStorageSnapshotRejectsWrongSchema(t *testing.T) {
	_, err := DecodeWorkbenchStorageSnapshot([]byte(`{"schema":"daemon.workbench","schemaVersion":1}`))
	if !errors.Is(err, ErrInvalidWorkbenchSnapshot) {
		t.Fatalf("expected invalid snapshot error, got %v", err)
	}
}

func TestDefaultWorkbenchStorageRefUsesOpaqueStorageKey(t *testing.T) {
	ref := DefaultWorkbenchStorageRef("")
	if ref.AppID != WorkbenchStorageAppID || ref.Scope != WorkbenchStorageScopePublic || ref.OwnerID != DefaultWorkspaceID || ref.Key != WorkbenchStorageKeyRoot {
		t.Fatalf("unexpected default storage ref %#v", ref)
	}
	if ref.KeyPrefix() != "workbench/" {
		t.Fatalf("unexpected key prefix %q", ref.KeyPrefix())
	}
	if versioned := ref.WithVersion(12); versioned.Version != 12 || ref.Version != 0 {
		t.Fatalf("WithVersion should return versioned copy, ref=%#v versioned=%#v", ref, versioned)
	}
}
