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

func TestWorkbenchStorageSnapshotV2RoundTripsTerminalViews(t *testing.T) {
	shell := DefaultShell().SplitActivePane(PaneState{ID: "pane-logs", Title: "logs", Kind: PaneTerminalLive, TerminalID: "term-1"}, SplitDirectionVertical)
	views := TerminalViewStore{}
	views = views.BindPane(NewPaneTerminalView(DefaultPaneID, "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface", TerminalPaneViewID(DefaultPaneID), true))
	views = views.BindPane(NewPaneTerminalView("pane-logs", "term-1", 8, 40, 12, TerminalResizeRoleFollower, "surface", TerminalPaneViewID("pane-logs"), false))
	snapshot := SnapshotRootWorkbenchForStorage(Root{Shell: shell, TerminalViews: views})

	if snapshot.SchemaVersion != WorkbenchStorageSchemaV2 || len(snapshot.TerminalViews) != 2 {
		t.Fatalf("expected v2 snapshot with terminal views, got %#v", snapshot)
	}
	data, err := EncodeWorkbenchStorageSnapshotValue(snapshot)
	if err != nil {
		t.Fatalf("encode v2 snapshot: %v", err)
	}
	decoded, err := DecodeWorkbenchStorageSnapshot(data)
	if err != nil {
		t.Fatalf("decode v2 snapshot: %v", err)
	}
	restored, err := decoded.ToTerminalViewStore()
	if err != nil {
		t.Fatalf("restore terminal views: %v", err)
	}
	if bindings := restored.BindingsForTerminal("term-1"); len(bindings) != 2 {
		t.Fatalf("expected restored shared terminal bindings, got %#v", bindings)
	}
}

func TestWorkbenchStorageSnapshotScrubsRuntimeAttachmentIdentity(t *testing.T) {
	shell := DefaultShell()
	binding := NewPaneTerminalView(DefaultPaneID, "term-1", 7, 80, 24, TerminalResizeRoleOwner, "surface-live", TerminalPaneViewID(DefaultPaneID), true)
	binding.OwnerSurfaceID = "surface-live"
	binding.OwnerViewID = binding.ViewID
	binding.ResizeEpoch = 3
	binding.LastError = "stale"
	binding.SizeLocked = true
	views := TerminalViewStore{}.BindPane(binding)
	snapshot := SnapshotRootWorkbenchForStorage(Root{Shell: shell, TerminalViews: views})

	if len(snapshot.TerminalViews) != 1 {
		t.Fatalf("expected one stored terminal view, got %#v", snapshot.TerminalViews)
	}
	stored := snapshot.TerminalViews[0]
	if stored.Channel != 0 || stored.Attached || stored.CanResize || stored.SizeLocked || stored.OwnerViewID != "" || stored.ResizeEpoch != 0 || stored.LastError != "" {
		t.Fatalf("snapshot must not persist protocol session identity, got %#v", stored)
	}

	stored.Channel = 99
	stored.Attached = true
	stored.CanResize = true
	stored.OwnerViewID = stored.ViewID
	stored.ResizeEpoch = 9
	stored.LastError = "old-session"
	stored.SizeLocked = true
	snapshot.TerminalViews[0] = stored
	restored, err := snapshot.ToTerminalViewStore()
	if err != nil {
		t.Fatalf("restore terminal views: %v", err)
	}
	binding, ok := restored.PaneBinding(DefaultPaneID)
	if !ok || binding.Channel != 0 || binding.Attached || binding.CanResize || binding.SizeLocked || binding.OwnerViewID != "" || binding.ResizeEpoch != 0 || binding.LastError != "" {
		t.Fatalf("restore must scrub old protocol session identity, binding=%#v ok=%v", binding, ok)
	}
	if binding.TerminalID != "term-1" || binding.ResizeRole != TerminalResizeRoleOwner || binding.DesiredCols != 80 || binding.DesiredRows != 24 {
		t.Fatalf("restore should keep workbench connection intent, binding=%#v", binding)
	}
}

func TestWorkbenchStorageSnapshotScrubsTransientPaneKinds(t *testing.T) {
	shell := DefaultShell().
		SplitActivePane(PaneState{ID: "pane-history", Title: "history", Kind: PaneCopyHistory, TerminalID: "term-history"}, SplitDirectionVertical)
	shell.Workspace.Tabs[0].Panes[0] = PaneState{ID: DefaultPaneID, Title: "old shell", Kind: PaneExited, TerminalID: "term-main", Active: true}
	shell, floatingResult := shell.ApplyFloatingCommand(FloatingCommand{
		Action:   FloatingCommandCreate,
		TargetID: "float-exited",
		Pane:     PaneState{ID: "float-exited-pane", Title: "float", Kind: PaneExited, TerminalID: "term-float"},
		Title:    "float",
		Rect:     FloatingRect{X: 4, Y: 3, W: 40, H: 10},
	})
	if floatingResult.Status != FloatingCommandOK {
		t.Fatalf("create floating: %#v", floatingResult)
	}

	snapshot := SnapshotRootWorkbenchForStorage(Root{Shell: shell})
	restored, err := snapshot.ToShellStore()
	if err != nil {
		t.Fatalf("restore shell: %v", err)
	}

	if got := snapshot.Workspace.Tabs[0].Panes[0]; got.Kind != PaneTerminalLive || got.TerminalID != "term-main" {
		t.Fatalf("storage should keep terminal intent but not PaneExited, got %#v", got)
	}
	if got := snapshot.Workspace.Tabs[0].Panes[1]; got.Kind != PaneTerminalLive || got.TerminalID != "term-history" {
		t.Fatalf("storage should keep terminal intent but not PaneCopyHistory, got %#v", got)
	}
	if len(snapshot.Floatings) != 1 || snapshot.Floatings[0].Pane.Kind != PaneTerminalLive || snapshot.Floatings[0].Pane.TerminalID != "term-float" {
		t.Fatalf("storage should scrub floating transient pane kind, got %#v", snapshot.Floatings)
	}
	if pane, ok := restored.Pane(PaneCommandTarget{PaneID: DefaultPaneID}); !ok || pane.Kind != PaneTerminalLive || pane.TerminalID != "term-main" {
		t.Fatalf("restored tiled pane should await core lifecycle, pane=%#v ok=%v", pane, ok)
	}
	if floating, ok := restored.FloatingByPaneID("float-exited-pane"); !ok || floating.Pane.Kind != PaneTerminalLive || floating.Pane.TerminalID != "term-float" {
		t.Fatalf("restored floating pane should await core lifecycle, floating=%#v ok=%v", floating, ok)
	}
}

func TestWorkbenchStorageRestoreScrubsLegacyTransientPaneKinds(t *testing.T) {
	snapshot := WorkbenchStorageSnapshot{
		Schema:        WorkbenchStorageSchema,
		SchemaVersion: WorkbenchStorageSchemaV2,
		Workspace: WorkspaceState{
			ID:          DefaultWorkspaceID,
			ActiveTabID: DefaultTabID,
			Tabs: []TabState{{
				ID:           DefaultTabID,
				ActivePaneID: DefaultPaneID,
				RootSplit:    SplitNode{PaneID: DefaultPaneID},
				Panes: []PaneState{{
					ID:         DefaultPaneID,
					Title:      "old shell",
					Kind:       PaneExited,
					TerminalID: "term-main",
					Active:     true,
				}},
			}},
		},
		Workspaces: []WorkspaceState{{
			ID:          DefaultWorkspaceID,
			ActiveTabID: DefaultTabID,
			Tabs: []TabState{{
				ID:           DefaultTabID,
				ActivePaneID: DefaultPaneID,
				RootSplit:    SplitNode{PaneID: DefaultPaneID},
				Panes: []PaneState{{
					ID:         DefaultPaneID,
					Title:      "old shell",
					Kind:       PaneExited,
					TerminalID: "term-main",
					Active:     true,
				}},
			}},
		}},
		Floatings: []FloatingPaneState{{
			ID:    "floating-1",
			Title: "float",
			Pane:  PaneState{ID: "floating-1-pane", Title: "float", Kind: PaneCopyHistory, TerminalID: "term-float"},
			Rect:  FloatingRect{X: 1, Y: 1, W: 40, H: 10},
		}},
		PanelPresentation: PanelPresentationCard,
		ActivePaneID:      DefaultPaneID,
		HeaderVisible:     true,
		FooterVisible:     true,
	}

	restored, err := snapshot.ToShellStore()
	if err != nil {
		t.Fatalf("restore shell: %v", err)
	}

	if pane, ok := restored.Pane(PaneCommandTarget{PaneID: DefaultPaneID}); !ok || pane.Kind != PaneTerminalLive || pane.TerminalID != "term-main" {
		t.Fatalf("legacy PaneExited must restore as terminal-live intent, pane=%#v ok=%v", pane, ok)
	}
	if floating, ok := restored.FloatingByPaneID("floating-1-pane"); !ok || floating.Pane.Kind != PaneTerminalLive || floating.Pane.TerminalID != "term-float" {
		t.Fatalf("legacy floating transient kind must restore as terminal-live intent, floating=%#v ok=%v", floating, ok)
	}
}

func TestWorkbenchStorageSnapshotRejectsLegacyV1(t *testing.T) {
	_, err := DecodeWorkbenchStorageSnapshot([]byte(`{"schema":"termx.tui.v3.workbench","schemaVersion":1,"workspace":{"ID":"workspace-main"},"workspaces":[{"ID":"workspace-main"}],"panelPresentation":"card"}`))
	if !errors.Is(err, ErrInvalidWorkbenchSnapshot) {
		t.Fatalf("expected legacy v1 rejection, got %v", err)
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
