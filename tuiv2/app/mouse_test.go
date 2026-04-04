package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
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
		Y:      6,  // tab bar 高度为 1，所以内容区域 Y=5 对应屏幕 Y=6
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
		Y:      11, // 向下移动 5
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
		Y:      11,
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
		Y:      5,
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
		Y:      7,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	if tab.ActivePaneID != "float-1" {
		t.Fatalf("expected active pane float-1 after click, got %q", tab.ActivePaneID)
	}
	if m.mouseDragPaneID != "float-1" {
		t.Fatalf("expected drag target float-1 after click, got %q", m.mouseDragPaneID)
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
	if strings.Count(before, "unconnected") < 2 {
		t.Fatalf("expected floating panes visible before click:\n%s", before)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      2,
		Y:      3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	after := m.View()
	if strings.Count(after, "unconnected") < 2 {
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
	if !strings.Contains(before, "term-f1") || !strings.Contains(before, "term-f2") {
		t.Fatalf("expected floating terminal panes visible before click:\n%s", before)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      2,
		Y:      3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	after := m.View()
	if !strings.Contains(after, "term-f1") || !strings.Contains(after, "term-f2") {
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
	if strings.Count(before, "unconnected") < 2 {
		t.Fatalf("expected floating panes visible before split-pane click:\n%s", before)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      90,
		Y:      5,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	after := m.View()
	if strings.Count(after, "unconnected") < 2 {
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

	bodyRect := workbench.Rect{W: maxInt(1, model.width), H: maxInt(1, model.height-2)}
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

	_, cmd := model.Update(tea.MouseMsg{
		X:      buttonRect.X,
		Y:      buttonRect.Y + 1,
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
	if !strings.Contains(model.View(), "become owner") {
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
		Y:      buttonRect.Y + 1,
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
