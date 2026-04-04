package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestMouseDragFloatingPane(t *testing.T) {
	// 创建一个带有浮动窗口的 workbench
	wb := workbench.NewWorkbench()
	ws := &workbench.WorkspaceState{
		Name:      "default",
		ActiveTab: 0,
	}
	wb.AddWorkspace("default", ws)

	// 创建一个 tab
	tabID := "tab-1"
	if err := wb.CreateTab("default", tabID, "1"); err != nil {
		t.Fatal(err)
	}

	// 创建一个浮动窗口
	paneID := "pane-1"
	rect := workbench.Rect{X: 10, Y: 5, W: 40, H: 20}
	if err := wb.CreateFloatingPane(tabID, paneID, rect); err != nil {
		t.Fatal(err)
	}

	// 创建 model
	m := New(shared.Config{}, wb, nil)
	m.width = 100
	m.height = 30

	// 模拟鼠标点击浮动窗口
	clickMsg := tea.MouseMsg{
		X:      15, // 在浮动窗口内
		Y:      screenYForBodyY(m, 5),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}

	model, _ := m.Update(clickMsg)
	m = model.(*Model)

	// 验证拖动状态已设置
	if m.mouseDragPaneID != paneID {
		t.Errorf("expected mouseDragPaneID=%q, got %q", paneID, m.mouseDragPaneID)
	}
	if m.mouseDragMode != mouseDragMove {
		t.Errorf("expected mouseDragMode=mouseDragMove, got %v", m.mouseDragMode)
	}

	// 模拟鼠标拖动
	dragMsg := tea.MouseMsg{
		X:      25, // 向右移动 10
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	}

	model, _ = m.Update(dragMsg)
	m = model.(*Model)

	// 验证浮动窗口位置已更新
	tab := wb.CurrentTab()
	if tab == nil {
		t.Fatal("tab is nil")
	}

	var floating *workbench.FloatingState
	for _, f := range tab.Floating {
		if f != nil && f.PaneID == paneID {
			floating = f
			break
		}
	}

	if floating == nil {
		t.Fatal("floating pane not found")
	}

	expectedX := 20 // 25 - (15 - 10) = 20
	expectedY := 8  // bounded by content height: 28 - pane height 20 = 8
	if floating.Rect.X != expectedX || floating.Rect.Y != expectedY {
		t.Errorf("expected position (%d, %d), got (%d, %d)",
			expectedX, expectedY, floating.Rect.X, floating.Rect.Y)
	}

	// 模拟鼠标释放
	releaseMsg := tea.MouseMsg{
		X:      25,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	}

	model, _ = m.Update(releaseMsg)
	m = model.(*Model)

	// 验证拖动状态已清除
	if m.mouseDragPaneID != "" {
		t.Errorf("expected mouseDragPaneID to be empty, got %q", m.mouseDragPaneID)
	}
	if m.mouseDragMode != mouseDragNone {
		t.Errorf("expected mouseDragMode=mouseDragNone, got %v", m.mouseDragMode)
	}
}

func TestMouseClickSelectsNonFloatingPane(t *testing.T) {
	m := setupTwoPaneModel(t)

	model, _ := m.Update(tea.MouseMsg{
		X:      90,
		Y:      screenYForBodyY(m, 4),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("tab is nil")
	}
	if tab.ActivePaneID != "pane-2" {
		t.Fatalf("expected active pane pane-2 after click, got %q", tab.ActivePaneID)
	}
}

func TestMouseDragSplitDividerResizesTiledPanes(t *testing.T) {
	m := setupTwoPaneModel(t)
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      59,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	if m.mouseDragMode != mouseDragResizeSplit {
		t.Fatalf("expected split drag mode, got %v", m.mouseDragMode)
	}
	if m.mouseDragSplit == nil {
		t.Fatal("expected split drag target")
	}

	model, cmd := m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 {
		t.Fatal("expected visible workbench")
	}
	panes := visible.Tabs[visible.ActiveTab].Panes
	if len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %#v", panes)
	}
	if panes[0].Rect.W != 50 || panes[1].Rect.W != 70 {
		t.Fatalf("expected pane widths 50/70 after drag, got %#v %#v", panes[0].Rect, panes[1].Rect)
	}
	if len(client.resizes) < 2 {
		t.Fatalf("expected resize calls after tiled drag, got %#v", client.resizes)
	}

	model, _ = m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = model.(*Model)
	if m.mouseDragMode != mouseDragNone || m.mouseDragSplit != nil {
		t.Fatalf("expected split drag state cleared, mode=%v split=%#v", m.mouseDragMode, m.mouseDragSplit)
	}
}

func TestMouseClickSelectsFloatingPane(t *testing.T) {
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("tab is nil")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 20, H: 8}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      12,
		Y:      screenYForBodyY(m, 6),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	if tab.ActivePaneID != "float-1" {
		t.Fatalf("expected active pane float-1 after click, got %q", tab.ActivePaneID)
	}
}

func TestMouseClickNonFloatingKeepsFloatingPanesVisible(t *testing.T) {
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("tab is nil")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 20, H: 8}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-2", workbench.Rect{X: 40, Y: 8, W: 20, H: 8}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	before := m.View()
	if countFloatingPaneMarkers(before) < 2 {
		t.Fatalf("expected floating panes visible before click:\n%s", before)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      2,
		Y:      screenYForBodyY(m, 3),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	after := m.View()
	if countFloatingPaneMarkers(after) < 2 {
		t.Fatalf("expected floating panes to remain visible after tiled click:\n%s", after)
	}
}

func TestMouseClickNonFloatingKeepsFloatingTerminalPanesVisibleWithExtentHints(t *testing.T) {
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("tab is nil")
	}
	for _, spec := range []struct {
		paneID string
		termID string
		rect   workbench.Rect
	}{
		{paneID: "float-1", termID: "term-f1", rect: workbench.Rect{X: 10, Y: 5, W: 20, H: 8}},
		{paneID: "float-2", termID: "term-f2", rect: workbench.Rect{X: 40, Y: 8, W: 20, H: 8}},
	} {
		if err := m.workbench.CreateFloatingPane(tab.ID, spec.paneID, spec.rect); err != nil {
			t.Fatalf("create floating pane: %v", err)
		}
		if err := m.workbench.BindPaneTerminal(tab.ID, spec.paneID, spec.termID); err != nil {
			t.Fatalf("bind floating pane: %v", err)
		}
		tr := m.runtime.Registry().GetOrCreate(spec.termID)
		tr.Name = spec.termID
		tr.State = "running"
		tr.Snapshot = &protocol.Snapshot{
			TerminalID: spec.termID,
			Size:       protocol.Size{Cols: 4, Rows: 2},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
				{{Content: "a", Width: 1}, {Content: "b", Width: 1}},
				{{Content: "c", Width: 1}, {Content: "d", Width: 1}},
			}},
		}
	}

	before := m.View()
	if strings.Count(before, "◎") < 2 {
		t.Fatalf("expected floating terminal panes visible before click:\n%s", before)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      2,
		Y:      screenYForBodyY(m, 3),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	after := m.View()
	if strings.Count(after, "◎") < 2 {
		t.Fatalf("expected floating terminal panes to remain visible after tiled click:\n%s", after)
	}
}

func TestMouseClickSplitPaneKeepsFloatingPanesVisible(t *testing.T) {
	m := setupTwoPaneModel(t)
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("tab is nil")
	}
	for _, spec := range []struct {
		paneID string
		rect   workbench.Rect
	}{
		{paneID: "float-1", rect: workbench.Rect{X: 10, Y: 5, W: 20, H: 8}},
		{paneID: "float-2", rect: workbench.Rect{X: 50, Y: 10, W: 20, H: 8}},
	} {
		if err := m.workbench.CreateFloatingPane(tab.ID, spec.paneID, spec.rect); err != nil {
			t.Fatalf("create floating pane: %v", err)
		}
	}

	before := m.View()
	if countFloatingPaneMarkers(before) < 2 {
		t.Fatalf("expected floating panes visible before split-pane click:\n%s", before)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      90,
		Y:      screenYForBodyY(m, 4),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	after := m.View()
	if countFloatingPaneMarkers(after) < 2 {
		t.Fatalf("expected floating panes to remain visible after split-pane click:\n%s", after)
	}
}

func TestMouseClickOwnerButtonPromotesPaneAndResizesTerminal(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	root := &workbench.LayoutNode{
		Direction: workbench.SplitVertical,
		Ratio:     0.5,
		First:     workbench.NewLeaf("pane-1"),
		Second:    workbench.NewLeaf("pane-2"),
	}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-2",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "follower", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = "owner"

	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = "follower"

	bodyRect := model.bodyRect()
	visible := model.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || len(visible.Tabs[visible.ActiveTab].Panes) < 2 {
		t.Fatal("expected visible split panes")
	}
	pane := visible.Tabs[visible.ActiveTab].Panes[1]
	buttonRect, ok := render.PaneOwnerButtonRect(pane, model.runtime.Visible(), "")
	if !ok {
		t.Fatal("expected owner action hit box")
	}
	initialButtonRect := buttonRect
	if !model.mouseHitsOwnerButton(pane, buttonRect.X, buttonRect.Y) {
		t.Fatalf("expected owner action click at %+v to hit", buttonRect)
	}
	_ = model.View()

	_, cmd := model.Update(tea.MouseMsg{
		X:      buttonRect.X,
		Y:      screenYForBodyY(model, buttonRect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if cmd == nil {
		t.Fatal("expected first owner action click to produce command")
	}
	drainCmd(t, model, cmd, 20)
	if terminal.OwnerPaneID != "pane-1" {
		t.Fatalf("expected first click to keep existing owner, got %q", terminal.OwnerPaneID)
	}
	if model.ownerConfirmPaneID != "pane-2" {
		t.Fatalf("expected first click to arm owner confirmation for pane-2, got %q", model.ownerConfirmPaneID)
	}
	if !strings.Contains(model.View(), "◆ owner?") {
		t.Fatalf("expected armed owner confirmation in view:\n%s", model.View())
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected first click not to resize terminal, got %#v", client.resizes)
	}

	visible = model.workbench.VisibleWithSize(bodyRect)
	pane = visible.Tabs[visible.ActiveTab].Panes[1]
	buttonRect, ok = render.PaneOwnerButtonRect(pane, model.runtime.Visible(), model.ownerConfirmPaneID)
	if !ok {
		t.Fatal("expected confirm owner action hit box")
	}
	if buttonRect.X != initialButtonRect.X || buttonRect.W != initialButtonRect.W {
		t.Fatalf("expected owner action hit box to stay stable after label change, before=%+v after=%+v", initialButtonRect, buttonRect)
	}

	_, cmd = model.Update(tea.MouseMsg{
		X:      buttonRect.X,
		Y:      screenYForBodyY(model, buttonRect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if cmd == nil {
		t.Fatal("expected second owner action click to produce command")
	}
	drainCmd(t, model, cmd, 20)

	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 promoted to owner, got %q", terminal.OwnerPaneID)
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected one resize after owner takeover, got %#v", client.resizes)
	}
	call := client.resizes[0]
	if call.channel != 2 {
		t.Fatalf("expected pane-2 channel to drive resize, got %#v", call)
	}
	if wantCols, wantRows := uint16(maxInt(2, pane.Rect.W-2)), uint16(maxInt(2, pane.Rect.H-2)); call.cols != wantCols || call.rows != wantRows {
		t.Fatalf("expected resize to %dx%d, got %#v", wantCols, wantRows, call)
	}
}

func TestMouseClickTabSwitchesActiveTab(t *testing.T) {
	m := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{
					{
						ID:           "tab-1",
						Name:         "one",
						ActivePaneID: "pane-1",
						Panes:        map[string]*workbench.PaneState{"pane-1": {ID: "pane-1"}},
						Root:         workbench.NewLeaf("pane-1"),
					},
					{
						ID:           "tab-2",
						Name:         "two",
						ActivePaneID: "pane-2",
						Panes:        map[string]*workbench.PaneState{"pane-2": {ID: "pane-2"}},
						Root:         workbench.NewLeaf("pane-2"),
					},
				},
			},
		},
	})

	state := m.visibleRenderState()
	regions := render.TabBarHitRegions(state)
	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionTabSwitch && region.TabIndex == 1 {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected tab switch region for second tab")
	}

	_, cmd := m.Update(tea.MouseMsg{X: target.Rect.X, Y: target.Rect.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if ws := m.workbench.CurrentWorkspace(); ws == nil || ws.ActiveTab != 1 {
		t.Fatalf("expected active tab 1 after click, got %#v", ws)
	}
}

func TestMouseClickPaneChromeZoomTogglesTargetPane(t *testing.T) {
	m := setupTwoPaneModel(t)
	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || len(visible.Tabs[visible.ActiveTab].Panes) < 2 {
		t.Fatal("expected visible split panes")
	}
	pane := visible.Tabs[visible.ActiveTab].Panes[1]
	regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), "")

	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionPaneZoom {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected pane zoom chrome region")
	}

	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if tab.ActivePaneID != pane.ID {
		t.Fatalf("expected zoom target pane to gain focus, got %q", tab.ActivePaneID)
	}
	if tab.ZoomedPaneID != pane.ID {
		t.Fatalf("expected zoomed pane %q, got %q", pane.ID, tab.ZoomedPaneID)
	}
}

func TestMouseClickFloatingPaneChromeCloseDoesNotStartDrag(t *testing.T) {
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("tab is nil")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 20, H: 8}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || len(visible.FloatingPanes) == 0 {
		t.Fatal("expected visible floating pane")
	}
	pane := visible.FloatingPanes[0]
	regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), "")

	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionPaneClose {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected floating pane close chrome region")
	}

	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	if m.mouseDragMode != mouseDragNone || m.mouseDragPaneID != "" {
		t.Fatalf("expected pane chrome close click not to arm drag, mode=%v pane=%q", m.mouseDragMode, m.mouseDragPaneID)
	}
	if tab.Panes["float-1"] != nil {
		t.Fatalf("expected floating pane to close, panes=%#v", tab.Panes)
	}
	if len(tab.Floating) != 0 {
		t.Fatalf("expected floating entry removed after close, got %#v", tab.Floating)
	}
}

func TestMouseClickWorkspaceLabelOpensWorkspacePicker(t *testing.T) {
	m := setupModel(t, modelOpts{})
	state := m.visibleRenderState()
	regions := render.TabBarHitRegions(state)
	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionWorkspaceLabel {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected workspace label region")
	}

	_, cmd := m.Update(tea.MouseMsg{X: target.Rect.X, Y: target.Rect.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if m.modalHost == nil || m.modalHost.Session == nil || m.modalHost.Session.Kind != input.ModeWorkspacePicker {
		t.Fatalf("expected workspace picker modal, got %#v", m.modalHost)
	}
}

func TestMouseClickTabCreateOpensPickerForNewTab(t *testing.T) {
	m := setupModel(t, modelOpts{})
	state := m.visibleRenderState()
	regions := render.TabBarHitRegions(state)
	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionTabCreate {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected tab create region")
	}

	_, cmd := m.Update(tea.MouseMsg{X: target.Rect.X, Y: target.Rect.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 30)

	ws := m.workbench.CurrentWorkspace()
	if ws == nil || len(ws.Tabs) != 2 {
		t.Fatalf("expected a second tab after create click, got %#v", ws)
	}
	if m.modalHost == nil || m.modalHost.Session == nil || m.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker modal after tab create, got %#v", m.modalHost)
	}
}

func TestMouseClickTabCloseClosesActiveTab(t *testing.T) {
	m := setupModel(t, modelOpts{})
	createSecondTab(t, m)

	state := m.visibleRenderState()
	regions := render.TabBarHitRegions(state)
	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionTabClose && region.TabIndex == 1 {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected tab close region for second tab")
	}

	_, cmd := m.Update(tea.MouseMsg{X: target.Rect.X, Y: target.Rect.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	ws := m.workbench.CurrentWorkspace()
	if ws == nil || len(ws.Tabs) != 1 {
		t.Fatalf("expected one tab after mouse close, got %#v", ws)
	}
}

func TestMouseTopChromeDoesNotExposeTabManagementActions(t *testing.T) {
	m := setupModel(t, modelOpts{width: 180})
	state := m.visibleRenderState()
	for _, kind := range []render.HitRegionKind{
		render.HitRegionTabRename,
		render.HitRegionTabKill,
		render.HitRegionWorkspacePrev,
		render.HitRegionWorkspaceNext,
		render.HitRegionWorkspaceCreate,
		render.HitRegionWorkspaceRename,
		render.HitRegionWorkspaceDelete,
	} {
		for _, region := range render.TabBarHitRegions(state) {
			if region.Kind == kind {
				t.Fatalf("expected top chrome management region %q to be omitted, got %#v", kind, region)
			}
		}
	}
}

func TestMouseClickEmptyPaneAttachOpensPicker(t *testing.T) {
	m := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes:        map[string]*workbench.PaneState{"pane-1": {ID: "pane-1"}},
					Root:         workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})

	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	pane := visible.Tabs[visible.ActiveTab].Panes[0]
	regions := render.EmptyPaneActionRegions(pane)
	if len(regions) == 0 {
		t.Fatal("expected empty-pane action regions")
	}

	_, cmd := m.Update(tea.MouseMsg{
		X:      regions[0].Rect.X,
		Y:      screenYForBodyY(m, regions[0].Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	if m.modalHost == nil || m.modalHost.Session == nil || m.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker modal, got %#v", m.modalHost)
	}
}

func TestMouseClickPaneChromeSplitVerticalCreatesPaneAndOpensPicker(t *testing.T) {
	m := setupModel(t, modelOpts{})
	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || len(visible.Tabs[visible.ActiveTab].Panes) == 0 {
		t.Fatal("expected visible pane")
	}
	pane := visible.Tabs[visible.ActiveTab].Panes[0]
	regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), "")

	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionPaneSplitV {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected pane split-vertical chrome region")
	}

	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 30)

	tab := m.workbench.CurrentTab()
	if tab == nil || len(tab.Panes) != 2 {
		t.Fatalf("expected split click to create second pane, got %#v", tab)
	}
	if m.modalHost == nil || m.modalHost.Session == nil || m.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker modal after pane split click, got %#v", m.modalHost)
	}
}

func TestMousePaneChromeOmitsSecondaryTiledActions(t *testing.T) {
	m := setupModel(t, modelOpts{})
	term := m.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected term-1 runtime")
	}
	term.OwnerPaneID = "pane-1"
	term.BoundPaneIDs = []string{"pane-1"}

	binding := m.runtime.Binding("pane-1")
	if binding == nil {
		t.Fatal("expected pane-1 binding")
	}
	binding.Role = "owner"

	for _, kind := range []render.HitRegionKind{
		render.HitRegionPaneDetach,
		render.HitRegionPaneReconnect,
		render.HitRegionPaneCloseKill,
		render.HitRegionPaneBalancePanes,
		render.HitRegionPaneCycleLayout,
	} {
		if paneChromeRegionPresent(m, "pane-1", kind) {
			t.Fatalf("expected tiled pane chrome to omit %q", kind)
		}
	}
}

func TestMouseClickFloatingPaneChromeActions(t *testing.T) {
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 0, Y: 0, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := m.workbench.FocusPane(tab.ID, "float-1"); err != nil {
		t.Fatalf("focus floating pane: %v", err)
	}

	m.workbench.MoveFloatingPane(tab.ID, "float-1", 0, 0)
	center := visiblePaneChromeRegion(t, m, "float-1", render.HitRegionPaneCenterFloating)
	beforeRect := findFloating(tab, "float-1").Rect
	_, cmd := m.Update(tea.MouseMsg{X: center.Rect.X, Y: screenYForBodyY(m, center.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)
	after := findFloating(tab, "float-1")
	if after == nil {
		t.Fatal("expected floating pane after center click")
	}
	if beforeRect == after.Rect {
		t.Fatalf("expected center click to move floating pane, got %+v", after.Rect)
	}

	for _, kind := range []render.HitRegionKind{
		render.HitRegionPaneOpenPicker,
		render.HitRegionPaneToggleFloating,
	} {
		if paneChromeRegionPresent(m, "float-1", kind) {
			t.Fatalf("expected floating pane chrome to omit %q", kind)
		}
	}
}

func TestMousePaneChromeOmitsLayoutActions(t *testing.T) {
	m := setupTwoPaneModel(t)
	tab := m.workbench.CurrentTab()
	if tab == nil || tab.Root == nil {
		t.Fatal("expected current split tab")
	}

	dispatchAction(t, m, input.SemanticAction{Kind: input.ActionResizePaneRight, PaneID: "pane-1"})
	if tab.Root.Ratio == 0.5 {
		t.Fatal("expected ratio changed before balance click")
	}
	for _, kind := range []render.HitRegionKind{
		render.HitRegionPaneBalancePanes,
		render.HitRegionPaneCycleLayout,
	} {
		if paneChromeRegionPresent(m, "pane-1", kind) {
			t.Fatalf("expected split pane chrome to omit %q", kind)
		}
	}
}

func TestMouseClickPickerItemAttachesSelectedTerminal(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{"term-2": {TerminalID: "term-2", Size: protocol.Size{Cols: 80, Rows: 24}}},
	}
	m := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes:        map[string]*workbench.PaneState{"pane-1": {ID: "pane-1"}},
					Root:         workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	m.runtime.Registry().GetOrCreate("term-2").Name = "term-2"
	m.runtime.Registry().Get("term-2").State = "running"
	m.modalHost.Open(input.ModePicker, "pane-1")
	m.modalHost.Picker = &modal.PickerState{
		Items: []modal.PickerItem{{TerminalID: "term-2", Name: "term-2", State: "running"}},
	}
	m.modalHost.Picker.ApplyFilter()
	m.modalHost.MarkReady(input.ModePicker, "pane-1")
	m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "pane-1"})

	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionPickerItem && region.ItemIndex == 0 {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected picker item region")
	}

	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 30)

	tab := m.workbench.CurrentTab()
	if tab == nil || tab.Panes["pane-1"] == nil || tab.Panes["pane-1"].TerminalID != "term-2" {
		t.Fatalf("expected pane attached to term-2, got %#v", tab)
	}
}

func TestMouseClickOverlayDismissClosesPicker(t *testing.T) {
	m := setupModel(t, modelOpts{})
	m.modalHost.Open(input.ModePicker, "pane-1")
	m.modalHost.Picker = &modal.PickerState{Items: []modal.PickerItem{{TerminalID: "term-1", Name: "term-1"}}}
	m.modalHost.Picker.ApplyFilter()
	m.modalHost.MarkReady(input.ModePicker, "pane-1")
	m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "pane-1"})

	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	var dismiss render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionOverlayDismiss {
			dismiss = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected overlay dismiss region")
	}

	_, cmd := m.Update(tea.MouseMsg{
		X:      dismiss.Rect.X,
		Y:      screenYForBodyY(m, dismiss.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	if m.modalHost != nil && m.modalHost.Session != nil {
		t.Fatalf("expected picker modal closed, got %#v", m.modalHost.Session)
	}
}

func TestMouseClickTerminalPoolRowSelectsItem(t *testing.T) {
	m := setupModel(t, modelOpts{})
	m.terminalPage = &modal.TerminalManagerState{
		Title: "Terminal Pool",
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "running"},
			{TerminalID: "term-2", Name: "logs", State: "running"},
		},
		Selected: 0,
	}
	m.terminalPage.ApplyFilter()
	m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})

	state := m.visibleRenderState()
	regions := render.TerminalPoolHitRegions(state)
	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionTerminalPoolItem && region.ItemIndex == 1 {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected terminal pool row region for item 1, got %#v", regions)
	}

	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	if m.terminalPage == nil || m.terminalPage.Selected != 1 {
		t.Fatalf("expected selected row index 1 after click, got %#v", m.terminalPage)
	}
	if m.input.Mode().Kind != input.ModeTerminalManager {
		t.Fatalf("expected terminal manager mode to stay active, got %q", m.input.Mode().Kind)
	}
}

func TestMouseClickTerminalPoolFooterAttachHereDispatchesModalAction(t *testing.T) {
	m := setupModel(t, modelOpts{attachTerminal: "term-2"})
	m.terminalPage = &modal.TerminalManagerState{
		Title: "Terminal Pool",
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "running"},
			{TerminalID: "term-2", Name: "logs", State: "running"},
		},
		Selected: 1,
	}
	m.terminalPage.ApplyFilter()
	m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})

	state := m.visibleRenderState()
	regions := render.TerminalPoolHitRegions(state)
	var target render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionTerminalPoolAction && region.Action.Kind == input.ActionSubmitPrompt {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected terminal pool footer attach-here region, got %#v", regions)
	}

	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 40)

	tab := m.workbench.CurrentTab()
	if tab == nil || tab.Panes["pane-1"] == nil || tab.Panes["pane-1"].TerminalID != "term-2" {
		t.Fatalf("expected footer attach-here click to attach term-2, got %#v", tab)
	}
	if m.terminalPage != nil {
		t.Fatalf("expected terminal pool page closed after attach-here, got %#v", m.terminalPage)
	}
	if m.input.Mode().Kind != input.ModeNormal {
		t.Fatalf("expected mode normal after attach-here, got %q", m.input.Mode().Kind)
	}
}

func TestMouseClickTerminalPoolFooterAttachFloatingCreatesFloatingPane(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", State: "running"},
			},
		},
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-2": {TerminalID: "term-2", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	m := setupModel(t, modelOpts{client: client})
	m.terminalPage = &modal.TerminalManagerState{
		Title: "Terminal Pool",
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "running"},
			{TerminalID: "term-2", Name: "logs", State: "running"},
		},
		Selected: 1,
	}
	m.terminalPage.ApplyFilter()
	m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})

	target := terminalPoolFooterActionRegion(t, m, input.ActionAttachFloating)
	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 40)

	tab := m.workbench.CurrentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected one floating pane after footer attach-floating, got %#v", tab)
	}
	pane := tab.Panes[tab.Floating[0].PaneID]
	if pane == nil || pane.TerminalID != "term-2" {
		t.Fatalf("expected floating pane attached to term-2, got %#v", pane)
	}
}

func TestMouseClickTerminalPoolFooterEditOpensPrompt(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", Command: []string{"tail", "-f", "/tmp/app.log"}, State: "running", Tags: map[string]string{"role": "ops"}},
			},
		},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	m := setupModel(t, modelOpts{client: client})
	m.terminalPage = &modal.TerminalManagerState{
		Title: "Terminal Pool",
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "running"},
			{TerminalID: "term-2", Name: "logs", Command: "tail -f /tmp/app.log", State: "running"},
		},
		Selected: 1,
	}
	m.terminalPage.ApplyFilter()
	m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})

	target := terminalPoolFooterActionRegion(t, m, input.ActionEditTerminal)
	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	if m.modalHost == nil || m.modalHost.Prompt == nil || m.modalHost.Prompt.Kind != "edit-terminal-name" {
		t.Fatalf("expected edit-terminal-name prompt after footer edit click, got %#v", m.modalHost)
	}
}

func TestMouseClickTerminalPoolFooterKillRemovesItemAndInvokesBridgeClient(t *testing.T) {
	client := &recordingBridgeClient{}
	m := setupModel(t, modelOpts{client: client})
	m.terminalPage = &modal.TerminalManagerState{
		Title: "Terminal Pool",
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "running"},
			{TerminalID: "term-2", Name: "logs", State: "running"},
		},
		Selected: 1,
	}
	m.terminalPage.ApplyFilter()
	m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})

	target := terminalPoolFooterActionRegion(t, m, input.ActionKillTerminal)
	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	if m.terminalPage == nil || len(m.terminalPage.Items) != 1 || m.terminalPage.Items[0].TerminalID != "term-1" {
		t.Fatalf("expected selected terminal removed after footer kill, got %#v", m.terminalPage)
	}
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-2" {
		t.Fatalf("expected kill call for term-2, got %#v", client.killCalls)
	}
}

func TestMouseClickPromptInputMovesCursorAndSubmitFooterDispatches(t *testing.T) {
	m := setupModel(t, modelOpts{})
	m.openRenameTabPrompt()

	inputRegion := overlayRegionByKind(t, m, render.HitRegionPromptInput)
	clickX := inputRegion.Rect.X + 1
	_, cmd := m.Update(tea.MouseMsg{X: clickX, Y: screenYForBodyY(m, inputRegion.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if m.modalHost == nil || m.modalHost.Prompt == nil {
		t.Fatalf("expected prompt state, got %#v", m.modalHost)
	}
	if got := m.modalHost.Prompt.Cursor; got != 1 {
		t.Fatalf("expected prompt cursor moved to 1, got %d", got)
	}

	dispatchKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if got := m.modalHost.Prompt.Value; !strings.Contains(got, "X") || got == "tab 1" {
		t.Fatalf("expected rune inserted at cursor, got %q", got)
	}

	submit := overlayRegionByKind(t, m, render.HitRegionPromptSubmit)
	_, cmd = m.Update(tea.MouseMsg{X: submit.Rect.X, Y: screenYForBodyY(m, submit.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	assertMode(t, m, input.ModeNormal)
	if tab := m.workbench.CurrentTab(); tab == nil || !strings.Contains(tab.Name, "X") {
		t.Fatalf("expected prompt submit click to rename tab, got %#v", tab)
	}
}

func TestMouseClickPromptCancelFooterClosesPrompt(t *testing.T) {
	m := setupModel(t, modelOpts{})
	m.openRenameWorkspacePrompt()

	cancel := overlayRegionByKind(t, m, render.HitRegionPromptCancel)
	_, cmd := m.Update(tea.MouseMsg{X: cancel.Rect.X, Y: screenYForBodyY(m, cancel.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	assertMode(t, m, input.ModeNormal)
	if m.modalHost != nil && m.modalHost.Session != nil {
		t.Fatalf("expected prompt closed after cancel click, got %#v", m.modalHost.Session)
	}
}

func TestMouseClickWorkspacePickerFooterNextSwitchesWorkspace(t *testing.T) {
	m := setupModel(t, modelOpts{width: 220})
	if err := m.workbench.CreateWorkspace("dev"); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if !m.workbench.SwitchWorkspace("main") {
		t.Fatal("switch workspace to main")
	}

	m.modalHost.Open(input.ModeWorkspacePicker, "workspace")
	m.modalHost.WorkspacePicker = &modal.WorkspacePickerState{
		Title: "Workspaces",
		Items: []modal.WorkspacePickerItem{
			{Name: "main"},
			{Name: "dev"},
		},
	}
	m.modalHost.WorkspacePicker.ApplyFilter()
	m.modalHost.MarkReady(input.ModeWorkspacePicker, "workspace")
	m.input.SetMode(input.ModeState{Kind: input.ModeWorkspacePicker, RequestID: "workspace"})

	next := overlayWorkspaceItemRegion(t, m, 1)
	_, cmd := m.Update(tea.MouseMsg{X: next.Rect.X, Y: screenYForBodyY(m, next.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if ws := m.workbench.CurrentWorkspace(); ws == nil || ws.Name != "dev" {
		t.Fatalf("expected next workspace footer click to switch to dev, got %#v", ws)
	}
	assertMode(t, m, input.ModeNormal)
	if m.modalHost != nil && m.modalHost.Session != nil {
		t.Fatalf("expected workspace picker closed after footer click, got %#v", m.modalHost.Session)
	}
}

func TestMouseClickPickerCreateRowOpensCreatePrompt(t *testing.T) {
	m := setupModel(t, modelOpts{width: 220})
	m.modalHost.Open(input.ModePicker, "pane-1")
	m.modalHost.Picker = &modal.PickerState{
		Selected: 0,
		Items: []modal.PickerItem{
			{CreateNew: true, Name: "new terminal"},
		},
	}
	m.modalHost.Picker.ApplyFilter()
	m.modalHost.MarkReady(input.ModePicker, "pane-1")
	m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "pane-1"})

	action := overlayPickerItemRegion(t, m, 0)
	_, cmd := m.Update(tea.MouseMsg{X: action.Rect.X, Y: screenYForBodyY(m, action.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	assertMode(t, m, input.ModePrompt)
	if m.modalHost == nil || m.modalHost.Prompt == nil {
		t.Fatalf("expected create prompt after picker create-row click, got %#v", m.modalHost)
	}
	if m.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create-terminal-form prompt, got %#v", m.modalHost.Prompt)
	}
	if m.modalHost.Prompt.CreateTarget != modal.CreateTargetReplace {
		t.Fatalf("expected replace create target, got %q", m.modalHost.Prompt.CreateTarget)
	}
}

func TestMouseWheelOnTabBarSwitchesTabs(t *testing.T) {
	m := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{
					{ID: "tab-1", Name: "one", ActivePaneID: "pane-1", Panes: map[string]*workbench.PaneState{"pane-1": {ID: "pane-1"}}, Root: workbench.NewLeaf("pane-1")},
					{ID: "tab-2", Name: "two", ActivePaneID: "pane-2", Panes: map[string]*workbench.PaneState{"pane-2": {ID: "pane-2"}}, Root: workbench.NewLeaf("pane-2")},
				},
			},
		},
	})

	_, cmd := m.Update(tea.MouseMsg{X: 1, Y: 0, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if ws := m.workbench.CurrentWorkspace(); ws == nil || ws.ActiveTab != 1 {
		t.Fatalf("expected wheel down on tab bar to switch to next tab, got %#v", ws)
	}
}

func TestMouseForwardsTerminalPressMotionReleaseWhenTrackingEnabled(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneMouseTracking(t, m, true)
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)
	_, cmd = m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	drainCmd(t, m, cmd, 20)
	_, cmd = m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 3 {
		t.Fatalf("expected 3 forwarded mouse events, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1b[<0;1;1M" {
		t.Fatalf("unexpected press payload %q", got)
	}
	if got := string(client.inputCalls[1].data); got != "\x1b[<32;1;1M" {
		t.Fatalf("unexpected motion payload %q", got)
	}
	if got := string(client.inputCalls[2].data); got != "\x1b[<3;1;1m" {
		t.Fatalf("unexpected release payload %q", got)
	}
}

func TestMouseDoesNotForwardContentClickWhenTrackingDisabled(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneMouseTracking(t, m, false)
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 0 {
		t.Fatalf("expected no forwarded mouse input with tracking off, got %#v", client.inputCalls)
	}
}

func TestMouseWheelFallsBackWhenTrackingDisabled(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneMouseTracking(t, m, false)
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if tab.ScrollOffset != 1 {
		t.Fatalf("expected wheel fallback scroll offset=1, got %d", tab.ScrollOffset)
	}
	if len(client.inputCalls) != 0 {
		t.Fatalf("expected no forwarded mouse wheel with tracking off, got %#v", client.inputCalls)
	}
}

func TestMouseWheelForwardsToTerminalWhenTrackingEnabled(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneMouseTracking(t, m, true)
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one forwarded mouse wheel event, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1b[<64;1;1M" {
		t.Fatalf("unexpected wheel payload %q", got)
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if tab.ScrollOffset != 0 {
		t.Fatalf("expected no fallback scrolling when wheel forwarded, got %d", tab.ScrollOffset)
	}
}

func TestMouseContentVsChromeBoundaryDoesNotForwardPaneChrome(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneMouseTracking(t, m, true)
	rect := activePaneRect(t, m)

	_, cmd := m.Update(tea.MouseMsg{
		X:      rect.X + 1,
		Y:      screenYForBodyY(m, rect.Y), // pane top border row in screen coordinates
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 0 {
		t.Fatalf("expected no forwarded mouse input on pane chrome, got %#v", client.inputCalls)
	}
}

func setActivePaneMouseTracking(t *testing.T, m *Model, enabled bool) {
	t.Helper()
	pane := m.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected active pane with terminal, got %#v", pane)
	}
	terminal := m.runtime.Registry().GetOrCreate(pane.TerminalID)
	if terminal.Snapshot == nil {
		terminal.Snapshot = &protocol.Snapshot{TerminalID: pane.TerminalID}
	}
	terminal.Snapshot.Modes.MouseTracking = enabled
}

func activePaneRect(t *testing.T, m *Model) workbench.Rect {
	t.Helper()
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatalf("expected visible state, got %#v", visible)
	}
	for _, pane := range visible.FloatingPanes {
		if pane.ID == tab.ActivePaneID {
			return pane.Rect
		}
	}
	for _, pane := range visible.Tabs[visible.ActiveTab].Panes {
		if pane.ID == tab.ActivePaneID {
			return pane.Rect
		}
	}
	t.Fatalf("active pane %q not visible", tab.ActivePaneID)
	return workbench.Rect{}
}

func activePaneContentScreenOrigin(t *testing.T, m *Model) (int, int) {
	t.Helper()
	rect := activePaneRect(t, m)
	contentRect, ok := paneContentRect(rect)
	if !ok {
		t.Fatalf("invalid content rect for pane %+v", rect)
	}
	return contentRect.X, screenYForBodyY(m, contentRect.Y)
}

func screenYForBodyY(m *Model, bodyY int) int {
	return bodyY + m.contentOriginY()
}

func terminalPoolFooterActionRegion(t *testing.T, m *Model, kind input.ActionKind) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.TerminalPoolHitRegions(state)
	for _, region := range regions {
		if region.Kind == render.HitRegionTerminalPoolAction && region.Action.Kind == kind {
			return region
		}
	}
	t.Fatalf("expected terminal pool footer action region %q, got %#v", kind, regions)
	return render.HitRegion{}
}

func tabBarRegionByKind(t *testing.T, m *Model, kind render.HitRegionKind) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.TabBarHitRegions(state)
	for _, region := range regions {
		if region.Kind == kind {
			return region
		}
	}
	t.Fatalf("expected tab bar region %q, got %#v", kind, regions)
	return render.HitRegion{}
}

func visiblePaneChromeRegion(t *testing.T, m *Model, paneID string, kind render.HitRegionKind) render.HitRegion {
	t.Helper()
	region, ok := findVisiblePaneChromeRegion(m, paneID, kind)
	if ok {
		return region
	}
	t.Fatalf("expected pane chrome region %q for pane %q", kind, paneID)
	return render.HitRegion{}
}

func paneChromeRegionPresent(m *Model, paneID string, kind render.HitRegionKind) bool {
	_, ok := findVisiblePaneChromeRegion(m, paneID, kind)
	return ok
}

func findVisiblePaneChromeRegion(m *Model, paneID string, kind render.HitRegionKind) (render.HitRegion, bool) {
	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil {
		return render.HitRegion{}, false
	}
	for _, pane := range visible.FloatingPanes {
		if pane.ID != paneID {
			continue
		}
		regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), m.ownerConfirmPaneID)
		for _, region := range regions {
			if region.Kind == kind {
				return region, true
			}
		}
	}
	if visible.ActiveTab >= 0 && visible.ActiveTab < len(visible.Tabs) {
		for _, pane := range visible.Tabs[visible.ActiveTab].Panes {
			if pane.ID != paneID {
				continue
			}
			regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), m.ownerConfirmPaneID)
			for _, region := range regions {
				if region.Kind == kind {
					return region, true
				}
			}
		}
	}
	return render.HitRegion{}, false
}

func overlayRegionByKind(t *testing.T, m *Model, kind render.HitRegionKind) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	for _, region := range regions {
		if region.Kind == kind {
			return region
		}
	}
	t.Fatalf("expected overlay region %q, got %#v", kind, regions)
	return render.HitRegion{}
}

func overlayFooterActionRegion(t *testing.T, m *Model, kind input.ActionKind) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	for _, region := range regions {
		if region.Kind == render.HitRegionOverlayFooterAction && region.Action.Kind == kind {
			return region
		}
	}
	t.Fatalf("expected overlay footer action region %q, got %#v", kind, regions)
	return render.HitRegion{}
}

func overlayWorkspaceItemRegion(t *testing.T, m *Model, index int) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	for _, region := range regions {
		if region.Kind == render.HitRegionWorkspaceItem && region.ItemIndex == index {
			return region
		}
	}
	t.Fatalf("expected workspace item region %d, got %#v", index, regions)
	return render.HitRegion{}
}

func countFloatingPaneMarkers(view string) int {
	return maxInt(strings.Count(view, "unconnected"), strings.Count(view, "No terminal attach"))
}
