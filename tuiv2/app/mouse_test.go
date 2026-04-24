package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/vterm"
)

func resetMouseQueueState() {
	mouseDebugSeq.Store(0)
	latestQueuedMouseMotionSeq.Store(0)
}

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

	// 验证拖拽预览位置已更新，但 committed rect 仍保持不变
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
	if floating.Rect != rect {
		t.Fatalf("expected committed rect unchanged during drag, got %#v", floating.Rect)
	}

	expectedX := 20        // 25 - (15 - 10) = 20
	expectedPreviewY := 10 // drag preview follows pointer during motion; bounds clamp happens on commit
	if !m.floatingDragPreview.Active {
		t.Fatalf("expected floating drag preview active, got %#v", m.floatingDragPreview)
	}
	if got := m.floatingDragPreview.Rect; got.X != expectedX || got.Y != expectedPreviewY {
		t.Errorf("expected preview position (%d, %d), got (%d, %d)",
			expectedX, expectedPreviewY, got.X, got.Y)
	}

	// 模拟鼠标释放
	releaseMsg := tea.MouseMsg{
		X:      25,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	}

	model, cmd := m.Update(releaseMsg)
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	expectedCommittedY := 8
	if floating.Rect.X != expectedX || floating.Rect.Y != expectedCommittedY {
		t.Errorf("expected committed position (%d, %d), got (%d, %d)",
			expectedX, expectedCommittedY, floating.Rect.X, floating.Rect.Y)
	}

	// 验证拖动状态已清除
	if m.mouseDragPaneID != "" {
		t.Errorf("expected mouseDragPaneID to be empty, got %q", m.mouseDragPaneID)
	}
	if m.mouseDragMode != mouseDragNone {
		t.Errorf("expected mouseDragMode=mouseDragNone, got %v", m.mouseDragMode)
	}
}

func TestMouseReleaseWithNoButtonClearsFloatingDragState(t *testing.T) {
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0

	model, _ := m.Update(tea.MouseMsg{
		X:      15,
		Y:      screenYForBodyY(m, 5),
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionRelease,
	})
	m = model.(*Model)

	if m.mouseDragPaneID != "" || m.mouseDragMode != mouseDragNone {
		t.Fatalf("expected no-button release to clear drag state, pane=%q mode=%v", m.mouseDragPaneID, m.mouseDragMode)
	}
}

func TestMouseMotionWithNoButtonClearsStaleDragState(t *testing.T) {
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0

	model, _ := m.Update(tea.MouseMsg{
		X:      15,
		Y:      screenYForBodyY(m, 5),
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionMotion,
	})
	m = model.(*Model)

	if m.mouseDragPaneID != "" || m.mouseDragMode != mouseDragNone {
		t.Fatalf("expected no-button motion to clear stale drag state, pane=%q mode=%v", m.mouseDragPaneID, m.mouseDragMode)
	}
}

func TestStaleQueuedMouseMotionIsDroppedDuringDrag(t *testing.T) {
	resetMouseQueueState()
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0
	m.floatingDragPreview = floatingDragPreviewState{Active: true, PaneID: "float-1", Rect: workbench.Rect{X: 10, Y: 5, W: 40, H: 20}}
	noteQueuedMouseMotion(2)

	before := m.workbench.FloatingState(tab.ID, "float-1").Rect
	cmd, handled := m.handleInteractionMessage(queuedMouseMsg{
		Seq:  1,
		Kind: "motion",
		Msg: tea.MouseMsg{
			X:      25,
			Y:      screenYForBodyY(m, 10),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		},
	})
	if !handled {
		t.Fatal("expected stale queued motion handled")
	}
	if cmd != nil {
		t.Fatalf("expected stale queued motion to drop before scheduling flush, got cmd=%#v", cmd)
	}
	after := m.workbench.FloatingState(tab.ID, "float-1").Rect
	if after != before {
		t.Fatalf("expected stale queued motion not to move floating pane, before=%#v after=%#v", before, after)
	}
}

func TestLaggedQueuedMouseMotionIsDroppedBeforeDragCoalescing(t *testing.T) {
	resetMouseQueueState()
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0

	before := m.workbench.FloatingState(tab.ID, "float-1").Rect
	cmd, handled := m.handleInteractionMessage(queuedMouseMsg{
		Seq:      1,
		Kind:     "motion",
		QueuedAt: time.Now().Add(-staleMouseMotionThreshold - 10*time.Millisecond),
		Msg: tea.MouseMsg{
			X:      25,
			Y:      screenYForBodyY(m, 10),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		},
	})
	if !handled {
		t.Fatal("expected lagged queued motion handled")
	}
	if cmd != nil {
		t.Fatalf("expected lagged queued motion to drop before scheduling flush, got cmd=%#v", cmd)
	}
	if m.pendingMouseMotion != nil {
		t.Fatalf("expected no pending mouse motion after lagged drop, got %#v", m.pendingMouseMotion)
	}
	if m.mouseMotionFlushPending {
		t.Fatal("expected no pending mouse motion flush after lagged drop")
	}
	after := m.workbench.FloatingState(tab.ID, "float-1").Rect
	if after != before {
		t.Fatalf("expected lagged queued motion not to move floating pane, before=%#v after=%#v", before, after)
	}
}

func TestLatestQueuedMouseMotionDropsLaggedOlderDragBeforeFlush(t *testing.T) {
	resetMouseQueueState()
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0
	m.floatingDragPreview = floatingDragPreviewState{Active: true, PaneID: "float-1", Rect: workbench.Rect{X: 10, Y: 5, W: 40, H: 20}}
	noteQueuedMouseMotion(2)

	oldMsg := queuedMouseMsg{
		Seq:      1,
		Kind:     "motion",
		QueuedAt: time.Now().Add(-staleMouseMotionThreshold - 10*time.Millisecond),
		Msg: tea.MouseMsg{
			X:      25,
			Y:      screenYForBodyY(m, 10),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		},
	}
	latestMsg := queuedMouseMsg{
		Seq:      2,
		Kind:     "motion",
		QueuedAt: time.Now(),
		Msg: tea.MouseMsg{
			X:      35,
			Y:      screenYForBodyY(m, 12),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		},
	}

	cmd, handled := m.handleInteractionMessage(oldMsg)
	if !handled {
		t.Fatal("expected old queued motion handled")
	}
	if cmd != nil {
		t.Fatalf("expected stale old motion to drop immediately, got cmd=%#v", cmd)
	}
	cmd, handled = m.handleInteractionMessage(latestMsg)
	if !handled || cmd == nil {
		t.Fatalf("expected latest queued motion to schedule flush, got handled=%v cmd=%#v", handled, cmd)
	}
	cmd, handled = m.handleInteractionMessage(mouseMotionFlushMsg{})
	if !handled {
		t.Fatal("expected mouseMotionFlushMsg handled")
	}
	if cmd != nil {
		drainCmd(t, m, cmd, 10)
	}

	if !m.floatingDragPreview.Active {
		t.Fatalf("expected floating drag preview active after flush, got %#v", m.floatingDragPreview)
	}
	if got := m.floatingDragPreview.Rect; got.X != 30 || got.Y != 12 {
		t.Fatalf("expected latest preview drag position applied, got %#v", got)
	}
}

func TestQueuedMouseMotionFlushAppliesOnlyLatestDragPosition(t *testing.T) {
	resetMouseQueueState()
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0
	m.floatingDragPreview = floatingDragPreviewState{Active: true, PaneID: "float-1", Rect: workbench.Rect{X: 10, Y: 5, W: 40, H: 20}}
	noteQueuedMouseMotion(2)

	msg1 := queuedMouseMsg{
		Seq:      1,
		Kind:     "motion",
		QueuedAt: time.Now(),
		Msg: tea.MouseMsg{
			X:      25,
			Y:      screenYForBodyY(m, 10),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		},
	}
	msg2 := queuedMouseMsg{
		Seq:      2,
		Kind:     "motion",
		QueuedAt: time.Now(),
		Msg: tea.MouseMsg{
			X:      35,
			Y:      screenYForBodyY(m, 12),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		},
	}

	cmd, handled := m.handleInteractionMessage(msg1)
	if !handled {
		t.Fatal("expected first queued motion handled")
	}
	if cmd != nil {
		t.Fatalf("expected first non-latest motion to drop before flush, got cmd=%#v", cmd)
	}
	cmd2, handled := m.handleInteractionMessage(msg2)
	if !handled || cmd2 == nil {
		t.Fatalf("expected latest queued motion to schedule flush, got handled=%v cmd=%#v", handled, cmd2)
	}
	cmd3, handled := m.handleInteractionMessage(mouseMotionFlushMsg{})
	if !handled {
		t.Fatal("expected mouseMotionFlushMsg handled")
	}
	if cmd3 != nil {
		drainCmd(t, m, cmd3, 10)
	}

	if !m.floatingDragPreview.Active {
		t.Fatalf("expected drag preview active after flush, got %#v", m.floatingDragPreview)
	}
	if got := m.floatingDragPreview.Rect; got.X != 30 || got.Y != 12 {
		t.Fatalf("expected flush to apply latest preview drag position, got %#v", got)
	}
}

func TestMouseMotionFlushLeavesNoTailAfterLatestMotionApplied(t *testing.T) {
	resetMouseQueueState()
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0
	noteQueuedMouseMotion(1)

	cmd, handled := m.handleInteractionMessage(queuedMouseMsg{
		Seq:      1,
		Kind:     "motion",
		QueuedAt: time.Now(),
		Msg:      tea.MouseMsg{X: 35, Y: screenYForBodyY(m, 12), Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
	})
	if !handled || cmd == nil {
		t.Fatalf("expected drag motion to schedule flush, got handled=%v cmd=%#v", handled, cmd)
	}
	cmd, handled = m.handleInteractionMessage(mouseMotionFlushMsg{})
	if !handled {
		t.Fatal("expected mouseMotionFlushMsg handled")
	}
	if cmd != nil {
		drainCmd(t, m, cmd, 10)
	}
	if m.pendingMouseMotion != nil {
		t.Fatalf("expected no pending mouse motion after flush, got %#v", m.pendingMouseMotion)
	}
	if m.mouseMotionFlushPending {
		t.Fatal("expected no pending mouse motion flush after latest motion applied")
	}

	before := m.workbench.FloatingState(tab.ID, "float-1").Rect
	cmd, handled = m.handleInteractionMessage(mouseMotionFlushMsg{})
	if !handled {
		t.Fatal("expected extra mouseMotionFlushMsg handled")
	}
	if cmd != nil {
		drainCmd(t, m, cmd, 10)
	}
	after := m.workbench.FloatingState(tab.ID, "float-1").Rect
	if after != before {
		t.Fatalf("expected no tail movement after drag queue drained, before=%#v after=%#v", before, after)
	}
}

func TestMouseMotionFlushCanceledByReleaseBoundary(t *testing.T) {
	resetMouseQueueState()
	m := setupModel(t, modelOpts{})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	m.mouseDragPaneID = "float-1"
	m.mouseDragMode = mouseDragMove
	m.mouseDragOffsetX = 5
	m.mouseDragOffsetY = 0
	noteQueuedMouseMotion(1)

	before := m.workbench.FloatingState(tab.ID, "float-1").Rect
	cmd, handled := m.handleInteractionMessage(queuedMouseMsg{
		Seq:      1,
		Kind:     "motion",
		QueuedAt: time.Now(),
		Msg: tea.MouseMsg{
			X:      35,
			Y:      screenYForBodyY(m, 12),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionMotion,
		},
	})
	if !handled || cmd == nil {
		t.Fatalf("expected drag motion to schedule flush, got handled=%v cmd=%#v", handled, cmd)
	}
	flushMsg, ok := cmd().(mouseMotionFlushMsg)
	if !ok {
		t.Fatalf("expected mouseMotionFlushMsg, got %#v", cmd())
	}

	cmd, handled = m.handleInteractionMessage(queuedMouseMsg{
		Seq:      2,
		Kind:     "release",
		QueuedAt: time.Now(),
		Msg: tea.MouseMsg{
			X:      15,
			Y:      screenYForBodyY(m, 5),
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionRelease,
		},
	})
	if !handled {
		t.Fatal("expected release boundary handled")
	}
	if cmd != nil {
		drainCmd(t, m, cmd, 10)
	}
	if m.mouseDragMode != mouseDragNone {
		t.Fatalf("expected release to clear drag state, got %v", m.mouseDragMode)
	}

	cmd, handled = m.handleInteractionMessage(flushMsg)
	if !handled {
		t.Fatal("expected stale mouseMotionFlushMsg handled")
	}
	if cmd != nil {
		drainCmd(t, m, cmd, 10)
	}

	after := m.workbench.FloatingState(tab.ID, "float-1").Rect
	if after != before {
		t.Fatalf("expected release boundary to invalidate pending drag tail, before=%#v after=%#v", before, after)
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
	if len(client.resizes) != 0 {
		t.Fatalf("expected split drag motion to avoid PTY resize until release, got %#v", client.resizes)
	}

	model, cmd = m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)
	if len(client.resizes) != 2 {
		t.Fatalf("expected both tiled panes to resize on drag release, got %#v", client.resizes)
	}
	if m.mouseDragMode != mouseDragNone || m.mouseDragSplit != nil {
		t.Fatalf("expected split drag state cleared, mode=%v split=%#v", m.mouseDragMode, m.mouseDragSplit)
	}
}

func TestMouseDragSplitDividerResetsAltScreenBaselineDuringPreviewAndRelease(t *testing.T) {
	m := setupTwoPaneModel(t)
	base := m.runtime.Registry().Get("term-1")
	if base == nil {
		t.Fatal("expected base terminal")
	}
	base.Snapshot = cursorWriterNvimLikeSnapshot("term-1", 58, 30, "#444444")

	writer := &resetProbeFrameWriter{}
	m.SetFrameWriter(writer)
	m.width = 120
	m.height = 36

	_ = m.View()
	if writer.resetCalls != 0 {
		t.Fatalf("expected baseline alt-screen view not to reset, got %d", writer.resetCalls)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      59,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	model, cmd := m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	_ = m.View()
	if writer.resetCalls != 1 {
		t.Fatalf("expected split drag preview over visible alt-screen to reset baseline once, got %d", writer.resetCalls)
	}

	model, cmd = m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)
	if writer.resetCalls != 2 {
		t.Fatalf("expected split drag release resize to reset baseline again, got %d", writer.resetCalls)
	}
}

func TestMouseDragSplitDividerPreviewRenderVMUsesLiveSurfaceWhenSnapshotLocked(t *testing.T) {
	m := setupTwoPaneModel(t)
	terminal := m.runtime.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected primary terminal")
	}
	screen := localvterm.ScreenData{
		IsAlternateScreen: true,
		Cells:             make([][]localvterm.Cell, 30),
	}
	for y := range screen.Cells {
		screen.Cells[y] = make([]localvterm.Cell, 58)
		for x := range screen.Cells[y] {
			cell := localvterm.Cell{Content: " ", Width: 1}
			if y == 10 {
				cell.Style.BG = "#3a3a3a"
			}
			screen.Cells[y][x] = cell
		}
	}
	vt := localvterm.New(58, 30, 100, nil)
	vt.LoadSnapshot(screen, localvterm.CursorState{Row: 10, Col: 4, Visible: true}, localvterm.TerminalModes{AlternateScreen: true, MouseTracking: true})
	terminal.VTerm = vt
	terminal.SurfaceVersion = 1
	terminal.PreferSnapshot = true
	terminal.Snapshot = cursorWriterNvimLikeSnapshot("term-1", 58, 30, "#111111")

	model, _ := m.Update(tea.MouseMsg{
		X:      59,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)
	model, cmd := m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	vm := m.renderVM()
	if vm.Runtime == nil {
		t.Fatal("expected runtime in render vm")
	}
	for _, visibleTerminal := range vm.Runtime.Terminals {
		if visibleTerminal.TerminalID != "term-1" {
			continue
		}
		if visibleTerminal.Surface == nil {
			t.Fatal("expected split drag preview to restore live surface into render vm")
		}
		return
	}
	t.Fatal("expected visible term-1 in render vm")
}

func TestMouseDragSplitDividerPreviewRenderUsesLiveSurfaceWhenSnapshotLocked(t *testing.T) {
	m := setupTwoPaneModel(t)
	terminal := m.runtime.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected primary terminal")
	}
	screen := localvterm.ScreenData{
		IsAlternateScreen: true,
		Cells:             make([][]localvterm.Cell, 30),
	}
	for y := range screen.Cells {
		screen.Cells[y] = make([]localvterm.Cell, 58)
		for x := range screen.Cells[y] {
			cell := localvterm.Cell{Content: " ", Width: 1}
			if y == 10 {
				cell.Style.BG = "#3a3a3a"
			}
			screen.Cells[y][x] = cell
		}
	}
	vt := localvterm.New(58, 30, 100, nil)
	vt.LoadSnapshot(screen, localvterm.CursorState{Row: 10, Col: 4, Visible: true}, localvterm.TerminalModes{AlternateScreen: true, MouseTracking: true})
	terminal.VTerm = vt
	terminal.SurfaceVersion = 1
	terminal.PreferSnapshot = true
	terminal.Snapshot = cursorWriterNvimLikeSnapshot("term-1", 58, 30, "#111111")

	model, _ := m.Update(tea.MouseMsg{
		X:      59,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)
	model, cmd := m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	frame := captureRenderFrameLines(t, m)
	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	if err := writer.WriteFrameLinesWithMeta(frame.lines, "", frame.meta); err != nil {
		t.Fatalf("write preview frame lines: %v", err)
	}
	sink.mu.Lock()
	stream := strings.Join(sink.writes, "")
	sink.mu.Unlock()
	host := localvterm.New(120, 36, 0, nil)
	if _, err := host.Write([]byte(stream)); err != nil {
		t.Fatalf("replay preview frame into host vterm: %v", err)
	}

	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatalf("expected visible workbench, got %#v", visible)
	}
	var pane workbench.VisiblePane
	found := false
	for _, candidate := range visible.Tabs[visible.ActiveTab].Panes {
		if candidate.ID == "pane-1" {
			pane = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected visible pane-1 after split drag preview")
	}
	contentRect, ok := paneContentRectForVisible(pane)
	if !ok {
		t.Fatalf("expected content rect for pane-1, got %#v", pane)
	}
	hostScreen := host.ScreenContent()
	expectedCount := 0
	for x := 0; x < minInt(contentRect.W, len(screen.Cells[10])); x++ {
		if screen.Cells[10][x].Style.BG == "#3a3a3a" {
			expectedCount++
		}
	}
	maxCount := 0
	for y := contentRect.Y; y < contentRect.Y+contentRect.H && y < len(hostScreen.Cells); y++ {
		count := 0
		for x := contentRect.X; x < contentRect.X+contentRect.W && x < len(hostScreen.Cells[y]); x++ {
			if hostScreen.Cells[y][x].Style.BG == "#3a3a3a" {
				count++
			}
		}
		if count > maxCount {
			maxCount = count
		}
	}
	if maxCount < expectedCount/2 {
		t.Fatalf("expected preview render to preserve live surface bg cells, expected about %d got max %d", expectedCount, maxCount)
	}
}

func TestMouseClickHiddenFloatingAreaFocusesUnderlyingPane(t *testing.T) {
	m := setupTwoPaneModel(t)
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 80, Y: 4, W: 20, H: 8}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	dispatchAction(t, m, input.SemanticAction{Kind: input.ActionToggleFloatingVisibility})

	if tab.FloatingVisible {
		t.Fatal("expected floating layer hidden")
	}
	if tab.ActivePaneID != "pane-1" {
		t.Fatalf("expected fallback focus on pane-1 after hiding float, got %q", tab.ActivePaneID)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      90,
		Y:      screenYForBodyY(m, 6),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	if tab.ActivePaneID != "pane-2" {
		t.Fatalf("expected click through hidden floating area to focus pane-2, got %q", tab.ActivePaneID)
	}
}

func TestMouseClickHiddenFloatingAreaStartsDividerDrag(t *testing.T) {
	m := setupTwoPaneModel(t)
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 50, Y: 4, W: 20, H: 8}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	dispatchAction(t, m, input.SemanticAction{Kind: input.ActionToggleFloatingVisibility})

	if tab.FloatingVisible {
		t.Fatal("expected floating layer hidden")
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      59,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	if m.mouseDragMode != mouseDragResizeSplit {
		t.Fatalf("expected hidden floating area click on divider to start split drag, got %v", m.mouseDragMode)
	}
	if m.mouseDragSplit == nil {
		t.Fatal("expected split drag target after clicking divider behind hidden float")
	}
}

func TestMouseDragSplitDividerResizesOwnerPaneWithoutActivatingIt(t *testing.T) {
	m := setupModel(t, modelOpts{
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
					Root: &workbench.LayoutNode{
						Direction: workbench.SplitVertical,
						Ratio:     0.5,
						First:     workbench.NewLeaf("pane-1"),
						Second:    workbench.NewLeaf("pane-2"),
					},
				}},
			},
		},
	})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}

	terminal := m.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 58, Rows: 36}}

	ownerBinding := m.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := m.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	model, _ := m.Update(tea.MouseMsg{
		X:      59,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	model, cmd := m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	if len(client.resizes) != 0 {
		t.Fatalf("expected split drag motion to avoid PTY resize until release, got %#v", client.resizes)
	}

	model, cmd = m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	if len(client.resizes) != 1 {
		t.Fatalf("expected owner pane to issue one resize on split drag release, got %#v", client.resizes)
	}
	if client.resizes[0].channel != 1 {
		t.Fatalf("expected owner pane channel to drive resize, got %#v", client.resizes[0])
	}
	if tab := m.workbench.CurrentTab(); tab == nil || tab.ActivePaneID != "pane-2" {
		t.Fatalf("expected drag not to require owner activation, active=%#v", tab)
	}
}

func TestMouseDragSplitDividerPersistsSessionOnRelease(t *testing.T) {
	m := setupTwoPaneModel(t)
	m.sessionID = "main"
	m.sessionRevision = 9
	m.sessionViewID = "view-1"

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

	model, cmd := m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	if len(client.replaceCalls) != 0 {
		t.Fatalf("expected no replace call during split drag motion, got %#v", client.replaceCalls)
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected no PTY resize during split drag motion, got %#v", client.resizes)
	}

	model, cmd = m.Update(tea.MouseMsg{
		X:      49,
		Y:      screenYForBodyY(m, 10),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	if len(client.replaceCalls) != 1 {
		t.Fatalf("expected one replace call on split drag release, got %d", len(client.replaceCalls))
	}
	if got := client.replaceCalls[0]; got.SessionID != "main" || got.BaseRevision != 9 || got.ViewID != "view-1" {
		t.Fatalf("unexpected replace params: %#v", got)
	}
}

func TestMouseDragFloatingMoveDefersRectCommitUntilRelease(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-float": {
				TerminalID: "term-float",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	m := setupModel(t, modelOpts{client: client})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 20, H: 8}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := m.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
		t.Fatalf("bind floating terminal: %v", err)
	}
	term := m.runtime.Registry().GetOrCreate("term-float")
	term.Name = "float"
	term.State = "running"
	term.Channel = 7
	term.Snapshot = client.snapshotByTerminal["term-float"]
	binding := m.runtime.BindPane("float-1")
	binding.Channel = 7
	binding.Connected = true

	model, _ := m.Update(tea.MouseMsg{X: 12, Y: screenYForBodyY(m, 5), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = model.(*Model)
	if m.mouseDragMode != mouseDragMove {
		t.Fatalf("expected floating move drag mode, got %v", m.mouseDragMode)
	}
	if !m.floatingDragPreview.Active || m.floatingDragPreview.PaneID != "float-1" {
		t.Fatalf("expected floating drag preview active, got %#v", m.floatingDragPreview)
	}

	model, cmd := m.Update(tea.MouseMsg{X: 32, Y: screenYForBodyY(m, 11), Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil || len(visible.FloatingPanes) == 0 {
		t.Fatalf("expected visible floating pane, got %#v", visible)
	}
	if got := visible.FloatingPanes[0].Rect; got != (workbench.Rect{X: 10, Y: 5, W: 20, H: 8}) {
		t.Fatalf("expected committed floating rect unchanged during drag, got %#v", got)
	}
	if got := m.floatingDragPreview.Rect; got.X == 10 && got.Y == 5 {
		t.Fatalf("expected preview rect to move, got %#v", got)
	}

	model, cmd = m.Update(tea.MouseMsg{X: 32, Y: screenYForBodyY(m, 11), Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	visible = m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil || len(visible.FloatingPanes) == 0 {
		t.Fatalf("expected visible floating pane after release, got %#v", visible)
	}
	if got := visible.FloatingPanes[0].Rect; got == (workbench.Rect{X: 10, Y: 5, W: 20, H: 8}) {
		t.Fatalf("expected committed floating rect to update on release, got %#v", got)
	}
	if m.floatingDragPreview.Active {
		t.Fatalf("expected floating drag preview cleared after release, got %#v", m.floatingDragPreview)
	}
}

func TestMouseDragFloatingResizeDefersPTYResizeUntilRelease(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1":     {TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
			"term-float": {TerminalID: "term-float", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	m := setupModel(t, modelOpts{client: client})
	tab := m.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := m.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 20, H: 8}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := m.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
		t.Fatalf("bind floating terminal: %v", err)
	}
	m.runtime.Registry().GetOrCreate("term-float").Name = "float"
	m.runtime.Registry().Get("term-float").State = "running"
	m.runtime.Registry().Get("term-float").Channel = 7
	binding := m.runtime.BindPane("float-1")
	binding.Channel = 7
	binding.Connected = true

	model, _ := m.Update(tea.MouseMsg{
		X:      29,
		Y:      screenYForBodyY(m, 12),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)
	if m.mouseDragMode != mouseDragResize {
		t.Fatalf("expected floating resize drag mode, got %v", m.mouseDragMode)
	}

	model, cmd := m.Update(tea.MouseMsg{
		X:      35,
		Y:      screenYForBodyY(m, 16),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	if len(client.resizes) != 0 {
		t.Fatalf("expected floating drag motion to avoid PTY resize until release, got %#v", client.resizes)
	}

	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil || len(visible.FloatingPanes) == 0 {
		t.Fatalf("expected visible floating pane, got %#v", visible)
	}
	model, cmd = m.Update(tea.MouseMsg{
		X:      35,
		Y:      screenYForBodyY(m, 16),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = model.(*Model)
	drainCmd(t, m, cmd, 20)

	if len(client.resizes) != 1 {
		t.Fatalf("expected one floating PTY resize on drag release, got %#v", client.resizes)
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
	if got := visibleFloatingPaneCount(m); got < 2 {
		t.Fatalf("expected floating panes visible before click, got %d", got)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      2,
		Y:      screenYForBodyY(m, 3),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	if got := visibleFloatingPaneCount(m); got < 2 {
		t.Fatalf("expected floating panes to remain visible after tiled click, got %d", got)
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

	if got := visibleFloatingPaneCount(m); got < 2 {
		t.Fatalf("expected floating terminal panes visible before click, got %d", got)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      2,
		Y:      screenYForBodyY(m, 3),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	if got := visibleFloatingPaneCount(m); got < 2 {
		t.Fatalf("expected floating terminal panes to remain visible after tiled click, got %d", got)
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

	if got := visibleFloatingPaneCount(m); got < 2 {
		t.Fatalf("expected floating panes visible before split-pane click, got %d", got)
	}

	model, _ := m.Update(tea.MouseMsg{
		X:      90,
		Y:      screenYForBodyY(m, 4),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = model.(*Model)

	if got := visibleFloatingPaneCount(m); got < 2 {
		t.Fatalf("expected floating panes to remain visible after split-pane click, got %d", got)
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
	buttonRect, ok := render.PaneOwnerButtonRect(pane, model.runtime.Visible(), "", model.chromeConfig())
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
	if !strings.Contains(xansi.Strip(model.View()), "◆ owner?") {
		t.Fatalf("expected armed owner confirmation in view:\n%s", model.View())
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected first click not to resize terminal, got %#v", client.resizes)
	}

	visible = model.workbench.VisibleWithSize(bodyRect)
	pane = visible.Tabs[visible.ActiveTab].Panes[1]
	buttonRect, ok = render.PaneOwnerButtonRect(pane, model.runtime.Visible(), model.ownerConfirmPaneID, model.chromeConfig())
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
	contentRect, ok := paneContentRectForVisible(pane)
	if !ok {
		t.Fatal("expected visible pane content rect")
	}
	if wantCols, wantRows := uint16(maxInt(2, contentRect.W)), uint16(maxInt(2, contentRect.H)); call.cols != wantCols || call.rows != wantRows {
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

	vm := m.renderVM()
	regions := render.TabBarHitRegions(vm)
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
	regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), "", m.chromeConfig())

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

func TestMouseClickPaneChromeSizeLockTogglesTerminalMetadata(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	m := setupModel(t, modelOpts{client: client})
	target := visiblePaneChromeRegion(t, m, "pane-1", render.HitRegionPaneSizeLock)

	_, cmd := m.Update(tea.MouseMsg{
		X:      target.Rect.X,
		Y:      screenYForBodyY(m, target.Rect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)

	if len(client.setMetadataCalls) != 1 {
		t.Fatalf("expected one metadata toggle call, got %#v", client.setMetadataCalls)
	}
	if got := client.setMetadataCalls[0].tags["termx.size_lock"]; got != "lock" {
		t.Fatalf("expected size lock tag to be saved, got %#v", client.setMetadataCalls[0].tags)
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
	regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), "", m.chromeConfig())

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
	vm := m.renderVM()
	regions := render.TabBarHitRegions(vm)
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
	vm := m.renderVM()
	regions := render.TabBarHitRegions(vm)
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

	vm := m.renderVM()
	regions := render.TabBarHitRegions(vm)
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
	vm := m.renderVM()
	for _, kind := range []render.HitRegionKind{
		render.HitRegionTabRename,
		render.HitRegionTabKill,
		render.HitRegionWorkspacePrev,
		render.HitRegionWorkspaceNext,
		render.HitRegionWorkspaceCreate,
		render.HitRegionWorkspaceRename,
		render.HitRegionWorkspaceDelete,
	} {
		for _, region := range render.TabBarHitRegions(vm) {
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
	regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), "", m.chromeConfig())

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

	vm := m.renderVM()
	regions := render.OverlayHitRegions(vm)
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

	vm := m.renderVM()
	regions := render.OverlayHitRegions(vm)
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

	vm := m.renderVM()
	regions := render.TerminalPoolHitRegions(vm)
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

	vm := m.renderVM()
	regions := render.TerminalPoolHitRegions(vm)
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

func TestMouseClickTerminalPoolFooterKillRefreshesItemAndInvokesBridgeClient(t *testing.T) {
	client := &recordingBridgeClient{listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{
		{ID: "term-1", Name: "shell", State: "running"},
		{ID: "term-2", Name: "logs", State: "exited"},
	}}}
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

	if m.terminalPage == nil || len(m.terminalPage.Items) != 2 {
		t.Fatalf("expected terminal pool refreshed after footer kill, got %#v", m.terminalPage)
	}
	exitedIndex := terminalManagerVisibleIndexByTerminalID(m.terminalPage.VisibleItems(), "term-2")
	if exitedIndex < 0 || m.terminalPage.VisibleItems()[exitedIndex].State != "exited" {
		t.Fatalf("expected killed terminal to remain in exited group after footer kill, got %#v", m.terminalPage.VisibleItems())
	}
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-2" {
		t.Fatalf("expected kill call for term-2, got %#v", client.killCalls)
	}
}

func TestMouseClickTerminalPoolQueryMovesCursorAndEditsAtPoint(t *testing.T) {
	m := setupModel(t, modelOpts{width: 220})
	m.terminalPage = &modal.TerminalManagerState{
		Title: "Terminal Pool",
		Query: "logs",
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "logs", State: "running"},
		},
	}
	m.terminalPage.ApplyFilter()
	m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})

	vm := m.renderVM()
	regions := render.TerminalPoolHitRegions(vm)
	var query render.HitRegion
	found := false
	for _, region := range regions {
		if region.Kind == render.HitRegionOverlayQueryInput {
			query = region
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected terminal pool query region, got %#v", regions)
	}

	clickX := query.Rect.X + 1
	_, cmd := m.Update(tea.MouseMsg{X: clickX, Y: screenYForBodyY(m, query.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if m.terminalPage == nil {
		t.Fatal("expected terminal pool state")
	}
	if got := m.terminalPage.Cursor; got != 1 || !m.terminalPage.CursorSet {
		t.Fatalf("expected terminal pool query cursor moved to 1 and marked explicit, got cursor=%d set=%v", m.terminalPage.Cursor, m.terminalPage.CursorSet)
	}

	dispatchKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if got := m.terminalPage.Query; got != "lXogs" {
		t.Fatalf("expected terminal pool query edited at cursor, got %q", got)
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

func TestMouseClickPickerQueryMovesCursorAndEditsAtPoint(t *testing.T) {
	m := setupModel(t, modelOpts{width: 220})
	m.modalHost.Open(input.ModePicker, "pane-1")
	m.modalHost.Picker = &modal.PickerState{
		Query: "term",
		Items: []modal.PickerItem{{TerminalID: "term-1", Name: "term-1"}},
	}
	m.modalHost.Picker.ApplyFilter()
	m.modalHost.MarkReady(input.ModePicker, "pane-1")
	m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "pane-1"})

	query := overlayRegionByKind(t, m, render.HitRegionOverlayQueryInput)
	clickX := query.Rect.X + 2
	_, cmd := m.Update(tea.MouseMsg{X: clickX, Y: screenYForBodyY(m, query.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if m.modalHost == nil || m.modalHost.Picker == nil {
		t.Fatalf("expected picker state, got %#v", m.modalHost)
	}
	if got := m.modalHost.Picker.Cursor; got != 2 {
		t.Fatalf("expected picker query cursor moved to 2, got %d", got)
	}

	dispatchKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if got := m.modalHost.Picker.Query; got != "teXrm" {
		t.Fatalf("expected picker query edited at cursor, got %q", got)
	}
}

func TestMouseClickWorkspacePickerQueryMovesCursorAndEditsAtPoint(t *testing.T) {
	m := setupModel(t, modelOpts{width: 220})
	m.modalHost.Open(input.ModeWorkspacePicker, "workspace")
	m.modalHost.WorkspacePicker = &modal.WorkspacePickerState{
		Query: "main",
		Items: []modal.WorkspacePickerItem{{Name: "main"}},
	}
	m.modalHost.WorkspacePicker.ApplyFilter()
	m.modalHost.MarkReady(input.ModeWorkspacePicker, "workspace")
	m.input.SetMode(input.ModeState{Kind: input.ModeWorkspacePicker, RequestID: "workspace"})

	query := overlayRegionByKind(t, m, render.HitRegionOverlayQueryInput)
	clickX := query.Rect.X + 1
	_, cmd := m.Update(tea.MouseMsg{X: clickX, Y: screenYForBodyY(m, query.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
		t.Fatalf("expected workspace picker state, got %#v", m.modalHost)
	}
	if got := m.modalHost.WorkspacePicker.Cursor; got != 1 {
		t.Fatalf("expected workspace picker query cursor moved to 1, got %d", got)
	}

	dispatchKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if got := m.modalHost.WorkspacePicker.Query; got != "mXain" {
		t.Fatalf("expected workspace picker query edited at cursor, got %q", got)
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
	if got := m.modalHost.WorkspacePicker.Selected; got != 1 {
		t.Fatalf("expected row click to select workspace row 1, got %d", got)
	}

	open := overlayFooterActionRegion(t, m, input.ActionSubmitPrompt)
	_, cmd = m.Update(tea.MouseMsg{X: open.Rect.X, Y: screenYForBodyY(m, open.Rect.Y), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if ws := m.workbench.CurrentWorkspace(); ws == nil || ws.Name != "dev" {
		t.Fatalf("expected open action to switch to dev, got %#v", ws)
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
	seedCopyModeSnapshot(t, m, []string{"hist-a", "hist-b", "hist-c"}, []string{"live-a", "live-b", "live-c"})
	setActivePaneMouseTracking(t, m, false)
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if got := m.mode().Kind; got != input.ModeDisplay {
		t.Fatalf("expected wheel fallback to enter display mode, got %q", got)
	}
	if m.copyMode.PaneID != "pane-1" {
		t.Fatalf("expected wheel fallback copy mode on pane-1, got %#v", m.copyMode)
	}
	if got := m.runtime.PaneViewportOffset("pane-1"); got <= 0 {
		t.Fatalf("expected wheel fallback to move into local scrollback, got %d", got)
	}
	if len(client.inputCalls) != 0 {
		t.Fatalf("expected no forwarded mouse wheel with tracking off, got %#v", client.inputCalls)
	}
}

func TestMouseWheelAlternateScreenFallsBackToCursorKeysWhenTrackingDisabled(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneTerminalModes(t, m, protocol.TerminalModes{
		AlternateScreen: true,
		MouseTracking:   false,
		AutoWrap:        true,
	})
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one alternate-screen wheel fallback input, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1b[A" {
		t.Fatalf("unexpected alternate-screen wheel fallback payload %q", got)
	}
	if got := m.runtime.PaneViewportOffset("pane-1"); got != 0 {
		t.Fatalf("expected no scrollback fallback in alternate screen, got %d", got)
	}
}

func TestMouseWheelAlternateScreenHonorsApplicationCursorMode(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneTerminalModes(t, m, protocol.TerminalModes{
		AlternateScreen:   true,
		MouseTracking:     false,
		ApplicationCursor: true,
		AutoWrap:          true,
	})
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one alternate-screen wheel fallback input, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1bOB" {
		t.Fatalf("unexpected application-cursor wheel fallback payload %q", got)
	}
}

func TestMouseWheelAlternateScrollFallsBackToCursorKeys(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneTerminalModes(t, m, protocol.TerminalModes{
		AlternateScroll: true,
		MouseTracking:   false,
		AutoWrap:        true,
	})
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one alternate-scroll wheel fallback input, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1b[A" {
		t.Fatalf("unexpected alternate-scroll wheel fallback payload %q", got)
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
	if got := m.runtime.PaneViewportOffset("pane-1"); got != 0 {
		t.Fatalf("expected no fallback scrolling when wheel forwarded, got %d", got)
	}
}

func TestMouseWheelForwardedPathBypassesWheelDispatchQueue(t *testing.T) {
	originalDelay := terminalWheelDispatchDelay
	terminalWheelDispatchDelay = time.Second
	defer func() { terminalWheelDispatchDelay = originalDelay }()

	m := setupModel(t, modelOpts{})
	setActivePaneMouseTracking(t, m, true)
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("expected wheel-forward command")
	}
	msg := cmd()
	if _, ok := msg.(terminalWheelDispatchMsg); ok {
		t.Fatalf("expected forwarded wheel to bypass dispatch queue, got %#v", msg)
	}
}

func TestMouseWheelForwardedPathSendsFirstWheelImmediatelyOnRemoteProfile(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")

	originalDelay := terminalWheelDispatchDelay
	originalRemoteDelay := remoteTerminalWheelDispatchDelay
	terminalWheelDispatchDelay = time.Second
	remoteTerminalWheelDispatchDelay = time.Millisecond
	defer func() {
		terminalWheelDispatchDelay = originalDelay
		remoteTerminalWheelDispatchDelay = originalRemoteDelay
	}()

	m := setupModel(t, modelOpts{})
	setActivePaneMouseTracking(t, m, true)
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("expected wheel-forward command")
	}
	msg := cmd()
	if _, ok := msg.(terminalWheelDispatchMsg); ok {
		t.Fatalf("expected remote forwarded wheel to skip initial dispatch queue, got %#v", msg)
	}
}

func TestForwardedWheelDirectPathExpandsRepeat(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneMouseTracking(t, m, true)
	cmd := m.handleForwardedTerminalWheelInput(input.TerminalInput{
		Kind:           input.TerminalInputWheel,
		PaneID:         "pane-1",
		Data:           []byte("\x1b[<64;1;1M"),
		Repeat:         3,
		WheelDirection: 1,
	})
	if cmd == nil {
		t.Fatal("expected direct forwarded wheel command")
	}
	drainCmd(t, m, cmd, 20)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one direct send, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1b[<64;1;1M\x1b[<64;1;1M\x1b[<64;1;1M" {
		t.Fatalf("expected repeated forwarded wheel payloads, got %q", got)
	}
}

func TestQueuedForwardedWheelUsesDirectSendWhenDequeuedEligible(t *testing.T) {
	originalTailDelay := terminalWheelTailDispatchDelay
	terminalWheelTailDispatchDelay = 0
	defer func() { terminalWheelTailDispatchDelay = originalTailDelay }()

	started := make(chan inputCall, 1)
	release := make(chan struct{})
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
		inputStarted:       started,
		inputBlock:         release,
	}
	m := setupModel(t, modelOpts{client: client})
	setActivePaneMouseTracking(t, m, true)

	firstCmd := m.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if firstCmd == nil {
		t.Fatal("expected initial terminal input command")
	}
	done := make(chan tea.Msg, 1)
	go func() {
		done <- firstCmd()
	}()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for initial input send to start")
	}

	wheelCmd := m.handleForwardedTerminalWheelInput(input.TerminalInput{
		Kind:           input.TerminalInputWheel,
		PaneID:         "pane-1",
		Data:           []byte("\x1b[<64;1;1M"),
		Repeat:         2,
		WheelDirection: 1,
	})
	if wheelCmd != nil {
		t.Fatalf("expected in-flight input to queue forwarded wheel, got cmd %#v", wheelCmd)
	}

	perftrace.Enable()
	perftrace.Reset()
	defer perftrace.Disable()

	close(release)
	firstMsg := <-done
	_, nextCmd := m.Update(firstMsg)
	if nextCmd == nil {
		t.Fatal("expected queued forwarded wheel command after first send completed")
	}
	drainCmd(t, m, nextCmd, 20)

	if len(client.inputCalls) != 2 {
		t.Fatalf("expected initial input and queued wheel send, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[1].data); got != "\x1b[<64;1;1M\x1b[<64;1;1M" {
		t.Fatalf("expected queued forwarded wheel payload to expand repeat, got %q", got)
	}
	snapshot := perftrace.SnapshotCurrent()
	if event, ok := snapshot.Event("app.input.wheel.direct_dequeue"); !ok || event.Count == 0 {
		t.Fatalf("expected queued forwarded wheel to use direct-dequeue path, got %#v", snapshot.Events)
	}
}

func TestMouseForwardsZoomedTerminalTopRowsWithoutFrameOffset(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}

	m.input.SetMode(input.ModeState{Kind: input.ModeDisplay})
	dispatchAction(t, m, input.SemanticAction{Kind: input.ActionZoomPane, PaneID: "pane-1"})
	tab := m.workbench.CurrentTab()
	if tab == nil || tab.ZoomedPaneID != "pane-1" {
		t.Fatalf("expected pane-1 zoomed, got %#v", tab)
	}
	m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
	if got := m.mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected normal mode after zoom setup, got %q", got)
	}

	setActivePaneMouseTracking(t, m, true)

	topMsg := tea.MouseMsg{X: 0, Y: screenYForBodyY(m, 0), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	topPaneID, topContentRect, ok := m.activeContentMouseTarget(topMsg.X, topMsg.Y)
	if !ok {
		t.Fatalf("expected zoomed top row to resolve content target, mode=%q pane=%q rect=%#v", m.mode().Kind, topPaneID, topContentRect)
	}
	topContentMsg := topMsg
	topContentMsg.Y -= m.contentOriginY()
	if encoded := m.encodeTerminalMouseInput(topContentMsg, topPaneID, topContentRect); len(encoded) == 0 {
		pane := m.workbench.ActivePane()
		t.Fatalf("expected zoomed top row to encode mouse input, pane=%#v modes=%+v rect=%#v", pane, m.terminalModesForPane(pane), topContentRect)
	}
	_, cmd := m.Update(topMsg)
	if cmd == nil {
		vm := m.renderVM()
		t.Fatalf("expected zoomed top-row click to produce terminal input command, surface=%q overlay=%q mode=%q", vm.Surface.Kind, vm.Overlay.Kind, m.mode().Kind)
	}
	drainCmd(t, m, cmd, 20)

	secondMsg := tea.MouseMsg{X: 0, Y: screenYForBodyY(m, 1), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	if paneID, contentRect, ok := m.activeContentMouseTarget(secondMsg.X, secondMsg.Y); !ok {
		t.Fatalf("expected zoomed second row to resolve content target, mode=%q pane=%q rect=%#v", m.mode().Kind, paneID, contentRect)
	}
	_, cmd = m.Update(secondMsg)
	if cmd == nil {
		t.Fatalf("expected zoomed second-row click to produce terminal input command")
	}
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 2 {
		t.Fatalf("expected 2 forwarded zoomed mouse events, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1b[<0;1;1M" {
		t.Fatalf("unexpected zoomed top-row payload %q", got)
	}
	if got := string(client.inputCalls[1].data); got != "\x1b[<0;1;2M" {
		t.Fatalf("unexpected zoomed second-row payload %q", got)
	}
}

func TestMouseWheelForwardsLegacyEncodingWhenSGRNotEnabled(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneTerminalModes(t, m, protocol.TerminalModes{
		MouseTracking: true,
		MouseNormal:   true,
	})
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one forwarded mouse wheel event, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1b[M`!!" {
		t.Fatalf("unexpected legacy wheel payload %q", got)
	}
}

func TestMouseMiddlePressForwardsToTerminalWhenTrackingEnabled(t *testing.T) {
	m := setupModel(t, modelOpts{})
	client, ok := m.runtime.Client().(*recordingBridgeClient)
	if !ok {
		t.Fatal("expected recording bridge client")
	}
	setActivePaneMouseTracking(t, m, true)
	x, y := activePaneContentScreenOrigin(t, m)

	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonMiddle, Action: tea.MouseActionPress})
	drainCmd(t, m, cmd, 20)

	if len(client.inputCalls) != 1 {
		t.Fatalf("expected middle press to be forwarded once, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "\x1b[<1;1;1M" {
		t.Fatalf("unexpected middle press payload %q", got)
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
	terminal.VTerm = nil
	if terminal.Snapshot == nil {
		terminal.Snapshot = &protocol.Snapshot{TerminalID: pane.TerminalID}
	}
	terminal.Snapshot.Modes.MouseTracking = enabled
	terminal.Snapshot.Modes.MouseButtonEvent = enabled
	terminal.Snapshot.Modes.MouseSGR = enabled
}

func setActivePaneTerminalModes(t *testing.T, m *Model, modes protocol.TerminalModes) {
	t.Helper()
	pane := m.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected active pane with terminal, got %#v", pane)
	}
	terminal := m.runtime.Registry().GetOrCreate(pane.TerminalID)
	terminal.VTerm = nil
	if terminal.Snapshot == nil {
		terminal.Snapshot = &protocol.Snapshot{TerminalID: pane.TerminalID}
	}
	terminal.Snapshot.Modes = modes
	terminal.Snapshot.Screen.IsAlternateScreen = modes.AlternateScreen
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
	contentRect, ok := m.activePaneContentRect()
	if !ok {
		t.Fatal("expected active pane content rect")
	}
	return contentRect.X, screenYForBodyY(m, contentRect.Y)
}

func screenYForBodyY(m *Model, bodyY int) int {
	return bodyY + m.contentOriginY()
}

func terminalPoolFooterActionRegion(t *testing.T, m *Model, kind input.ActionKind) render.HitRegion {
	t.Helper()
	vm := m.renderVM()
	regions := render.TerminalPoolHitRegions(vm)
	for _, region := range regions {
		if region.Kind == render.HitRegionTerminalPoolAction && region.Action.Kind == kind {
			return region
		}
	}
	t.Fatalf("expected terminal pool footer action region %q, got %#v", kind, regions)
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
		regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), m.ownerConfirmPaneID, m.chromeConfig())
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
			regions := render.PaneChromeHitRegions(pane, m.runtime.Visible(), m.ownerConfirmPaneID, m.chromeConfig())
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
	vm := m.renderVM()
	regions := render.OverlayHitRegions(vm)
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
	vm := m.renderVM()
	regions := render.OverlayHitRegions(vm)
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
	vm := m.renderVM()
	regions := render.OverlayHitRegions(vm)
	for _, region := range regions {
		if region.Kind == render.HitRegionWorkspaceItem && region.ItemIndex == index {
			return region
		}
	}
	t.Fatalf("expected workspace item region %d, got %#v", index, regions)
	return render.HitRegion{}
}

func countFloatingPaneMarkers(view string) int {
	// Check for floating-pane collapse button in both old ASCII and new NerdFont forms.
	byIcon := maxInt(strings.Count(view, "[_]"), strings.Count(view, "[\uf068]"))
	byContent := maxInt(strings.Count(view, "unconnected"), strings.Count(view, "No terminal attach"))
	return maxInt(byIcon, byContent)
}

func visibleFloatingPaneCount(m *Model) int {
	if m == nil || m.workbench == nil {
		return 0
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil {
		return 0
	}
	return len(visible.FloatingPanes)
}
