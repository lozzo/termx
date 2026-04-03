package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	expectedY := 10 // 11 - 1 - (6 - 1 - 5) = 10
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
