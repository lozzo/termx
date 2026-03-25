package app

import (
	"strings"
	"testing"
	"time"

	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

func TestSplitCreatesPaneSlotAndOpensConnectDialogThenCancelKeepsUnconnectedPane(t *testing.T) {
	model := newWorkbenchModelForIntentTest()

	next := model.Apply(IntentSplitVertical)
	if next.Overlay.Active().Kind != OverlayConnectDialog {
		t.Fatalf("expected connect dialog, got %q", next.Overlay.Active().Kind)
	}
	if next.Workspace.ActiveTab().PaneCount() != 2 {
		t.Fatalf("expected pane slot before dialog resolves, got %d panes", next.Workspace.ActiveTab().PaneCount())
	}
	pane, _ := next.Workspace.ActiveTab().ActivePane()
	if pane.SlotState != types.PaneSlotUnconnected {
		t.Fatalf("expected new pane to stay unconnected, got %q", pane.SlotState)
	}

	cancelled := next.Apply(IntentCancelOverlay)
	if cancelled.Overlay.HasActive() {
		t.Fatal("expected overlay to close after cancel")
	}
	active, _ := cancelled.Workspace.ActiveTab().ActivePane()
	if !active.IsUnconnected() {
		t.Fatal("expected pane to remain unconnected after cancel")
	}
}

func TestNewTabAndNewFloatOpenTheSameConnectDialog(t *testing.T) {
	base := newWorkbenchModelForIntentTest()

	nextTab := base.Apply(IntentNewTab)
	if nextTab.Overlay.Active().Kind != OverlayConnectDialog {
		t.Fatalf("expected connect dialog for new tab, got %q", nextTab.Overlay.Active().Kind)
	}
	if got := nextTab.Overlay.Active().Connect.Target; got != ConnectTargetNewTab {
		t.Fatalf("expected new-tab target, got %q", got)
	}

	nextFloat := base.Apply(IntentNewFloat)
	if nextFloat.Overlay.Active().Kind != OverlayConnectDialog {
		t.Fatalf("expected connect dialog for new float, got %q", nextFloat.Overlay.Active().Kind)
	}
	if got := nextFloat.Overlay.Active().Connect.Target; got != ConnectTargetNewFloat {
		t.Fatalf("expected new-floating target, got %q", got)
	}
}

func TestCreateNewTerminalBranchCreatesAndBindsTerminal(t *testing.T) {
	model := newWorkbenchModelForIntentTest().Apply(IntentSplitVertical)

	next := model.Apply(ConfirmCreateTerminalIntent{
		Command: []string{"/bin/sh"},
		Name:    "shell-2",
	})
	pane, _ := next.Workspace.ActiveTab().ActivePane()
	if pane.TerminalID == "" {
		t.Fatal("expected pane to bind the newly created terminal")
	}
	if pane.SlotState != types.PaneSlotLive {
		t.Fatalf("expected live pane, got %q", pane.SlotState)
	}
	meta := next.Terminals[pane.TerminalID]
	if meta.Name != "shell-2" || meta.State != stateterminal.StateRunning {
		t.Fatalf("expected created terminal metadata, got %#v", meta)
	}
}

func TestClosePaneDisconnectReconnectKillRemoveAndRestartLifecycle(t *testing.T) {
	model := newSharedTerminalModelForIntentTest()

	closed := model.Apply(IntentClosePane)
	if closed.Workspace.ActiveTab().PaneCount() != 1 {
		t.Fatalf("expected one pane left after close, got %d", closed.Workspace.ActiveTab().PaneCount())
	}
	if closed.Terminals[types.TerminalID("term-1")].State != stateterminal.StateRunning {
		t.Fatal("expected close pane to keep terminal running")
	}

	disconnectBase := newLivePaneModelForIntentTest()
	disconnected := disconnectBase.Apply(IntentDisconnectPane)
	pane, _ := disconnected.Workspace.ActiveTab().ActivePane()
	if !pane.IsUnconnected() {
		t.Fatal("expected pane to become unconnected")
	}

	reconnectBase := disconnected.Apply(OpenReconnectIntent{})
	reconnected := reconnectBase.Apply(ConfirmReconnectIntent{TerminalID: types.TerminalID("term-2")})
	reconnectedPane, _ := reconnected.Workspace.ActiveTab().ActivePane()
	if reconnectedPane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected reconnect to term-2, got %q", reconnectedPane.TerminalID)
	}

	killed := newLivePaneModelForIntentTest().Apply(IntentClosePaneAndKillTerminal)
	if killed.Terminals[types.TerminalID("term-1")].State != stateterminal.StateExited {
		t.Fatal("expected terminal to become exited after kill")
	}
	killedPane, _ := killed.Workspace.ActiveTab().ActivePane()
	if killedPane.SlotState != types.PaneSlotExited {
		t.Fatalf("expected exited pane after kill, got %q", killedPane.SlotState)
	}

	removed := newLivePaneModelForIntentTest().Apply(RemoveTerminalIntent{
		TerminalID: types.TerminalID("term-1"),
		Visible:    true,
		Name:       "api-dev",
	})
	removedPane, _ := removed.Workspace.ActiveTab().ActivePane()
	if !removedPane.IsUnconnected() {
		t.Fatal("expected pane to become unconnected after remove")
	}
	if removed.Notice == nil || !strings.Contains(removed.Notice.Message, "api-dev") {
		t.Fatalf("expected visible remove notice, got %#v", removed.Notice)
	}

	restarted := killed.Apply(RestartTerminalIntent{TerminalID: types.TerminalID("term-1")})
	if restarted.Terminals[types.TerminalID("term-1")].State != stateterminal.StateRunning {
		t.Fatal("expected terminal to return to running after restart")
	}
	restartedPane, _ := restarted.Workspace.ActiveTab().ActivePane()
	if restartedPane.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected restart to preserve terminal id, got %q", restartedPane.TerminalID)
	}
}

func TestOpenHelpOverlayAndFloatingAnchorLimitWithCenterRecall(t *testing.T) {
	model := newFloatingModelForIntentTest()

	help := model.Apply(IntentOpenHelp)
	if help.Overlay.Active().Kind != OverlayHelp {
		t.Fatalf("expected help overlay, got %q", help.Overlay.Active().Kind)
	}

	moved := model.Apply(MoveFloatingPaneIntent{
		PaneID: types.PaneID("float-1"),
		DeltaX: -999,
		DeltaY: -999,
	})
	floatPane, _ := moved.Workspace.ActiveTab().Pane(types.PaneID("float-1"))
	if !floatPane.AnchorVisible(DefaultFloatingViewport()) {
		t.Fatalf("expected floating anchor to remain visible, got %+v", floatPane.Rect)
	}

	centered := moved.Apply(CenterFloatingPaneIntent{PaneID: types.PaneID("float-1")})
	centerPane, _ := centered.Workspace.ActiveTab().Pane(types.PaneID("float-1"))
	if !centerPane.IsCentered(DefaultFloatingViewport()) {
		t.Fatalf("expected centered float, got %+v", centerPane.Rect)
	}
}

func newWorkbenchModelForIntentTest() Model {
	model := NewModel()
	ws := workspace.NewTemporary("main")
	tab := ws.ActiveTab()
	pane, _ := tab.ActivePane()
	pane.SlotState = types.PaneSlotLive
	pane.TerminalID = types.TerminalID("term-1")
	tab.TrackPane(pane)

	model.Workspace = ws
	model.Terminals[types.TerminalID("term-1")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-1"),
		Name:            "api-dev",
		Command:         []string{"/bin/sh"},
		State:           stateterminal.StateRunning,
		OwnerPaneID:     pane.ID,
		AttachedPaneIDs: []types.PaneID{pane.ID},
		LastInteraction: time.Unix(10, 0),
	}
	model.Sessions[types.TerminalID("term-1")] = TerminalSession{
		TerminalID: types.TerminalID("term-1"),
		Channel:    7,
		Attached:   true,
	}
	model.Terminals[types.TerminalID("term-2")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-2"),
		Name:            "worker-tail",
		Command:         []string{"/bin/sh"},
		State:           stateterminal.StateRunning,
		LastInteraction: time.Unix(9, 0),
	}
	return model
}

func newSharedTerminalModelForIntentTest() Model {
	model := newWorkbenchModelForIntentTest()
	tab := model.Workspace.ActiveTab()
	_ = tab.Layout.Split(types.PaneID("pane-1"), types.SplitDirectionVertical, types.PaneID("pane-2"))
	tab.TrackPane(workspace.PaneState{
		ID:         types.PaneID("pane-2"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotLive,
		TerminalID: types.TerminalID("term-1"),
	})
	tab.ActivePaneID = types.PaneID("pane-2")
	meta := model.Terminals[types.TerminalID("term-1")]
	meta.AttachedPaneIDs = []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")}
	model.Terminals[types.TerminalID("term-1")] = meta
	return model
}

func newLivePaneModelForIntentTest() Model {
	return newWorkbenchModelForIntentTest()
}

func newFloatingModelForIntentTest() Model {
	model := newWorkbenchModelForIntentTest()
	tab := model.Workspace.ActiveTab()
	tab.TrackPane(workspace.PaneState{
		ID:        types.PaneID("float-1"),
		Kind:      types.PaneKindFloating,
		SlotState: types.PaneSlotUnconnected,
		Rect:      types.Rect{X: 40, Y: 10, W: 32, H: 12},
	})
	tab.ActivePaneID = types.PaneID("float-1")
	return model
}
