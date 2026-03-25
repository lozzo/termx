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
	if pane.TerminalID != "" {
		t.Fatalf("expected pane to stay unconnected before runtime success, got %q", pane.TerminalID)
	}
	if pane.SlotState != types.PaneSlotUnconnected {
		t.Fatalf("expected pane to stay unconnected, got %q", pane.SlotState)
	}
	if len(next.PendingEffects) != 1 {
		t.Fatalf("expected one runtime effect, got %d", len(next.PendingEffects))
	}
	effect, ok := next.PendingEffects[0].(CreateTerminalEffect)
	if !ok {
		t.Fatalf("expected create effect, got %T", next.PendingEffects[0])
	}
	if effect.Name != "shell-2" {
		t.Fatalf("expected create effect to keep name, got %#v", effect)
	}
}

func TestConfirmConnectExistingTerminalBindsSelectedTerminalFromSharedDialog(t *testing.T) {
	model := newWorkbenchModelForIntentTest().Apply(IntentSplitVertical)

	next := model.Apply(ConfirmConnectExistingIntent{TerminalID: types.TerminalID("term-2")})
	pane, _ := next.Workspace.ActiveTab().ActivePane()
	if pane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected pane to bind term-2, got %q", pane.TerminalID)
	}
	if pane.SlotState != types.PaneSlotLive {
		t.Fatalf("expected connected pane to be live, got %q", pane.SlotState)
	}
	if next.Terminals[types.TerminalID("term-2")].OwnerPaneID != pane.ID {
		t.Fatalf("expected unowned terminal to grant owner, got %#v", next.Terminals[types.TerminalID("term-2")])
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
	if slicesContainsPane(reconnected.Terminals[types.TerminalID("term-1")].AttachedPaneIDs, reconnectedPane.ID) {
		t.Fatalf("expected old terminal to drop pane binding, got %#v", reconnected.Terminals[types.TerminalID("term-1")])
	}
	if reconnected.Terminals[types.TerminalID("term-1")].OwnerPaneID == reconnectedPane.ID {
		t.Fatalf("expected old terminal owner to clear, got %#v", reconnected.Terminals[types.TerminalID("term-1")])
	}

	killed := newLivePaneModelForIntentTest().Apply(IntentClosePaneAndKillTerminal)
	if killed.Terminals[types.TerminalID("term-1")].State != stateterminal.StateRunning {
		t.Fatal("expected terminal to stay running before runtime kill success")
	}
	killedPane, _ := killed.Workspace.ActiveTab().ActivePane()
	if killedPane.SlotState != types.PaneSlotLive {
		t.Fatalf("expected pane to stay live before runtime kill success, got %q", killedPane.SlotState)
	}
	if len(killed.PendingEffects) != 1 {
		t.Fatalf("expected one kill effect, got %d", len(killed.PendingEffects))
	}
	if _, ok := killed.PendingEffects[0].(KillTerminalEffect); !ok {
		t.Fatalf("expected kill effect, got %T", killed.PendingEffects[0])
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

	restartBase := newExitedPaneModelForIntentTest()
	restarted := restartBase.Apply(RestartTerminalIntent{TerminalID: types.TerminalID("term-1")})
	if restarted.Terminals[types.TerminalID("term-1")].State != stateterminal.StateExited {
		t.Fatal("expected restart to remain exited until real runtime path exists")
	}
	restartedPane, _ := restarted.Workspace.ActiveTab().ActivePane()
	if restartedPane.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected restart to preserve terminal id, got %q", restartedPane.TerminalID)
	}
	if restarted.Notice == nil || !strings.Contains(restarted.Notice.Message, "not wired") {
		t.Fatalf("expected conservative restart notice, got %#v", restarted.Notice)
	}
}

func TestRemoteRemoveNoticeUsesAnyVisiblePaneInActiveTab(t *testing.T) {
	model := newSharedTerminalModelForIntentTest()
	tab := model.Workspace.ActiveTab()
	tab.ActivePaneID = types.PaneID("pane-2")

	removed := model.Apply(RemoveTerminalIntent{
		TerminalID: types.TerminalID("term-1"),
		Name:       "api-dev",
	})
	if removed.Notice == nil {
		t.Fatal("expected visible notice when another visible pane in active tab is affected")
	}
}

func TestBecomeOwnerExplicitlyMovesOwnership(t *testing.T) {
	model := newSharedTerminalModelForIntentTest()
	tab := model.Workspace.ActiveTab()
	tab.ActivePaneID = types.PaneID("pane-2")

	next := model.Apply(BecomeOwnerIntent{TerminalID: types.TerminalID("term-1")})
	if next.Terminals[types.TerminalID("term-1")].OwnerPaneID != types.PaneID("pane-2") {
		t.Fatalf("expected owner to move to pane-2, got %#v", next.Terminals[types.TerminalID("term-1")])
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

func TestTerminalPoolSelectionSwitchesReadonlyLivePreviewSubscription(t *testing.T) {
	model := newTerminalPoolModelForIntentTest().Apply(OpenTerminalPoolIntent{})
	if model.Pool.SelectedTerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected initial pool selection to use recent interaction, got %q", model.Pool.SelectedTerminalID)
	}

	selected := model.Apply(SelectTerminalPoolIntent{TerminalID: types.TerminalID("term-1")})
	if selected.Pool.SelectedTerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected selected terminal to switch, got %q", selected.Pool.SelectedTerminalID)
	}
	if selected.Pool.PreviewTerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected preview terminal to switch immediately, got %q", selected.Pool.PreviewTerminalID)
	}
	if !selected.Pool.PreviewReadonly {
		t.Fatal("expected preview to remain readonly")
	}
	if selected.Pool.PreviewSubscriptionRevision != 2 {
		t.Fatalf("expected preview subscription refresh, got %d", selected.Pool.PreviewSubscriptionRevision)
	}
	if len(selected.PendingEffects) != 1 {
		t.Fatalf("expected one preview effect, got %d", len(selected.PendingEffects))
	}
	if _, ok := selected.PendingEffects[0].(RefreshPreviewEffect); !ok {
		t.Fatalf("expected preview effect, got %T", selected.PendingEffects[0])
	}
}

func TestTerminalPoolActionsRenameKillRemoveAndOpenTargetPane(t *testing.T) {
	model := newTerminalPoolModelForIntentTest().Apply(OpenTerminalPoolIntent{})

	editor := model.Apply(OpenTerminalMetadataEditorIntent{})
	if editor.Overlay.Active().Kind != OverlayTerminalMetadataEditor {
		t.Fatalf("expected metadata editor overlay, got %q", editor.Overlay.Active().Kind)
	}
	edited := editor.Apply(UpdateTerminalMetadataDraftIntent{
		Name:     "worker-renamed",
		TagsText: "ops,prod",
	})
	saved := edited.Apply(SaveTerminalMetadataIntent{})
	if len(saved.PendingEffects) != 1 {
		t.Fatalf("expected metadata save effect, got %d", len(saved.PendingEffects))
	}
	metadataEffect, ok := saved.PendingEffects[0].(UpdateTerminalMetadataEffect)
	if !ok {
		t.Fatalf("expected metadata effect, got %T", saved.PendingEffects[0])
	}
	if metadataEffect.Name != "worker-renamed" || metadataEffect.Tags["tag:0"] != "ops" || metadataEffect.Tags["tag:1"] != "prod" {
		t.Fatalf("expected metadata effect to keep edits, got %#v", metadataEffect)
	}

	killed := model.Apply(KillSelectedTerminalIntent{})
	if len(killed.PendingEffects) != 1 {
		t.Fatalf("expected kill effect, got %d", len(killed.PendingEffects))
	}
	if _, ok := killed.PendingEffects[0].(KillTerminalEffect); !ok {
		t.Fatalf("expected kill effect, got %T", killed.PendingEffects[0])
	}

	removed := model.Apply(RemoveSelectedTerminalIntent{})
	if len(removed.PendingEffects) != 1 {
		t.Fatalf("expected remove effect, got %d", len(removed.PendingEffects))
	}
	if _, ok := removed.PendingEffects[0].(RemoveTerminalEffect); !ok {
		t.Fatalf("expected remove effect, got %T", removed.PendingEffects[0])
	}

	openHere := model.Apply(OpenSelectedTerminalHereIntent{})
	pane, _ := openHere.Workspace.ActiveTab().ActivePane()
	if openHere.Screen != ScreenWorkbench || pane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected open-here to bind selected terminal in workbench, got screen=%q pane=%+v", openHere.Screen, pane)
	}

	openTab := model.Apply(OpenSelectedTerminalInNewTabIntent{})
	if openTab.Screen != ScreenWorkbench {
		t.Fatalf("expected new-tab open to return to workbench, got %q", openTab.Screen)
	}
	tabPane, _ := openTab.Workspace.ActiveTab().ActivePane()
	if tabPane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected new tab to bind selected terminal, got %+v", tabPane)
	}

	openFloat := model.Apply(OpenSelectedTerminalInFloatingIntent{})
	floatPane, _ := openFloat.Workspace.ActiveTab().ActivePane()
	if openFloat.Screen != ScreenWorkbench || floatPane.Kind != types.PaneKindFloating || floatPane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected floating open target, got screen=%q pane=%+v", openFloat.Screen, floatPane)
	}
}

func TestTerminalPoolSupportsMetadataTagsSearchAndEdit(t *testing.T) {
	model := newTerminalPoolModelForIntentTest().Apply(OpenTerminalPoolIntent{})

	searched := model.Apply(SearchTerminalPoolIntent{Query: "ops"})
	if searched.Pool.Query != "ops" {
		t.Fatalf("expected pool query to persist, got %q", searched.Pool.Query)
	}
	if searched.Pool.SelectedTerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected search to select matching terminal, got %q", searched.Pool.SelectedTerminalID)
	}

	editor := searched.Apply(OpenTerminalMetadataEditorIntent{})
	edited := editor.Apply(UpdateTerminalMetadataDraftIntent{
		Name:     "ops-primary",
		TagsText: "ops,priority",
	})
	saved := edited.Apply(SaveTerminalMetadataIntent{})
	effect, ok := saved.PendingEffects[0].(UpdateTerminalMetadataEffect)
	if !ok {
		t.Fatalf("expected metadata save effect, got %T", saved.PendingEffects[0])
	}
	if effect.TerminalID != types.TerminalID("term-2") || effect.Name != "ops-primary" {
		t.Fatalf("expected terminal metadata target to be term-2, got %#v", effect)
	}
}

func TestTerminalPoolSearchSwitchesPreviewAndRefreshesSubscription(t *testing.T) {
	model := newTerminalPoolModelForIntentTest().Apply(OpenTerminalPoolIntent{})

	searched := model.Apply(SearchTerminalPoolIntent{Query: "backend"})
	if searched.Pool.SelectedTerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected search to select term-1, got %q", searched.Pool.SelectedTerminalID)
	}
	if searched.Pool.PreviewTerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected search to switch preview terminal, got %q", searched.Pool.PreviewTerminalID)
	}
	if searched.Pool.PreviewSubscriptionRevision != 2 {
		t.Fatalf("expected search to refresh preview subscription, got %d", searched.Pool.PreviewSubscriptionRevision)
	}
	if len(searched.PendingEffects) != 1 {
		t.Fatalf("expected one preview refresh effect, got %d", len(searched.PendingEffects))
	}
	if _, ok := searched.PendingEffects[0].(RefreshPreviewEffect); !ok {
		t.Fatalf("expected preview refresh effect, got %T", searched.PendingEffects[0])
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
		OwnerPaneID:     "",
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

func newExitedPaneModelForIntentTest() Model {
	model := newWorkbenchModelForIntentTest()
	tab := model.Workspace.ActiveTab()
	pane, _ := tab.ActivePane()
	pane.SlotState = types.PaneSlotExited
	tab.TrackPane(pane)
	meta := model.Terminals[types.TerminalID("term-1")]
	meta.State = stateterminal.StateExited
	model.Terminals[types.TerminalID("term-1")] = meta
	return model
}

func newTerminalPoolModelForIntentTest() Model {
	model := newWorkbenchModelForIntentTest()
	model.Terminals[types.TerminalID("term-2")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-2"),
		Name:            "worker-tail",
		Command:         []string{"bash", "-lc", "tail -f worker.log"},
		Tags:            map[string]string{"team": "ops"},
		State:           stateterminal.StateRunning,
		LastInteraction: time.Unix(20, 0),
	}
	meta := model.Terminals[types.TerminalID("term-1")]
	meta.Tags = map[string]string{"team": "backend"}
	meta.LastInteraction = time.Unix(10, 0)
	model.Terminals[types.TerminalID("term-1")] = meta
	return model
}

func slicesContainsPane(items []types.PaneID, target types.PaneID) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
