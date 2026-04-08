package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	"github.com/lozzow/termx/workbenchdoc"
)

func TestModelViewShowsProjectedState(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "demo"
	rt.Registry().Get("term-1").State = "running"

	model := New(shared.Config{}, wb, rt)
	model.width = 100
	model.height = 30
	view := xansi.Strip(model.View())
	// tab bar contains workspace name and tab name
	for _, want := range []string{"main", "tab 1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestModelViewKeepsCursorInlineEvenWithWriter(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 10, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "h", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
	}

	model := New(shared.Config{}, wb, rt)
	model.width = 100
	model.height = 30
	writer := &recordingControlWriter{}
	model.SetCursorWriter(writer)

	view := model.View()
	if !strings.Contains(view, "\x1b[?25h") {
		t.Fatalf("expected view content with embedded host cursor sequence, got %q", view)
	}
	if writer.cursor != "" {
		t.Fatalf("expected split cursor writer projection to stay disabled, got %q", writer.cursor)
	}
}

func TestModelViewInlinesCursorForPromptModeEvenWithWriter(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.width = 80
	model.height = 24
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:  "rename-tab",
		Title: "Rename Tab",
		Value: "demo",
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})
	writer := &recordingControlWriter{}
	model.SetCursorWriter(writer)

	view := model.View()

	if !strings.Contains(view, "\x1b[?25h") {
		t.Fatalf("expected prompt mode to inline host cursor for IME positioning, got %q", view)
	}
	if writer.cursor != "" {
		t.Fatalf("expected prompt mode to suppress split cursor writer projection, got %q", writer.cursor)
	}
}

func TestModelInitBootstrapsDefaultWorkspace(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	if model.workbench.CurrentWorkspace() != nil {
		t.Fatal("expected empty workbench before Init bootstrap")
	}
	cmd := model.Init()
	if cmd != nil {
		_ = cmd()
	}
	ws := model.workbench.CurrentWorkspace()
	if ws == nil {
		t.Fatal("expected workspace after Init bootstrap")
	}
	if ws.Name != "main" {
		t.Fatalf("expected workspace main, got %q", ws.Name)
	}
	if len(ws.Tabs) != 1 {
		t.Fatalf("expected 1 tab after bootstrap, got %d", len(ws.Tabs))
	}
	if len(ws.Tabs[0].Panes) != 1 {
		t.Fatalf("expected 1 pane after bootstrap, got %d", len(ws.Tabs[0].Panes))
	}
}

func TestModelViewKeepsCursorInlineWhenFrameDoesNotChange(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 10, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{{{Content: "$", Width: 1}}},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true, Shape: "block"},
	}

	model := New(shared.Config{}, wb, rt)
	model.width = 100
	model.height = 30
	writer := &recordingControlWriter{}
	model.SetCursorWriter(writer)

	first := model.View()
	if got := len(writer.controls); got != 0 {
		t.Fatalf("expected no direct control writes on first frame, got %#v", writer.controls)
	}

	rt.Registry().Get("term-1").Snapshot.Cursor.Col = 1
	model.render.Invalidate()

	second := model.View()
	if first == second {
		t.Fatalf("expected cursor-only move to update the embedded cursor sequence, got first=%q second=%q", first, second)
	}
	if got := len(writer.controls); got != 0 {
		t.Fatalf("expected no direct cursor projection when frame stays unchanged, got %#v", writer.controls)
	}
	if !strings.Contains(second, "\x1b[?25h\x1b[3;3H") {
		t.Fatalf("expected embedded cursor projection onto the moved blank cell, got %q", second)
	}
	if writer.cursor != "" {
		t.Fatalf("expected split cursor writer projection to remain disabled, got %q", writer.cursor)
	}
}

func TestModelInitRestoresWorkspaceStateFromConfigPath(t *testing.T) {
	source := workbench.NewWorkbench()
	source.AddWorkspace("dev", &workbench.WorkspaceState{
		Name:      "dev",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-dev",
			Name:         "code",
			ActivePaneID: "pane-dev",
			Panes: map[string]*workbench.PaneState{
				"pane-dev": {ID: "pane-dev", Title: "shell", TerminalID: "term-restore"},
			},
			Root: workbench.NewLeaf("pane-dev"),
		}},
	})
	data, err := persist.Save(source)
	if err != nil {
		t.Fatalf("persist.Save: %v", err)
	}
	statePath := t.TempDir() + "/workspace-state.json"
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &recordingBridgeClient{attachErr: errors.New("terminal not found")}
	model := New(shared.Config{WorkspaceStatePath: statePath}, workbench.NewWorkbench(), runtime.New(client))
	if cmd := model.Init(); cmd != nil {
		if msg := cmd(); msg != nil {
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, item := range batch {
					_ = item()
				}
			} else if _, ok := msg.(reattachFailedMsg); ok {
				// expected: single-pane reattach failed; picker opens on Update
			} else {
				t.Fatalf("expected reattach batch or nil on restore, got %#v", msg)
			}
		}
	}

	ws := model.workbench.CurrentWorkspace()
	if ws == nil || ws.Name != "dev" {
		t.Fatalf("expected restored workspace dev, got %#v", ws)
	}
	if model.modalHost.Session != nil {
		t.Fatalf("expected restore path not to open startup picker, got %#v", model.modalHost.Session)
	}
	tab := model.workbench.CurrentTab()
	if tab == nil || tab.ActivePaneID == "" {
		t.Fatalf("expected restored active pane, got %#v", tab)
	}
	if pane := model.workbench.ActivePane(); pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected failed auto-reattach to clear restored binding, got %#v", pane)
	}
}

func TestModelHostCursorPositionProbeSelectsStableFallbackForOneColumnAmbiguousEmojiHosts(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.hostEmojiProbePending = true

	_, cmd := model.Update(hostCursorPositionMsg{X: 1, Y: 0})
	if cmd != nil {
		_ = cmd()
	}

	visible := model.runtime.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if visible.HostEmojiVS16Mode != shared.AmbiguousEmojiVariationSelectorStrip {
		t.Fatalf("expected host probe to select the stable fallback for one-column hosts, got %q", visible.HostEmojiVS16Mode)
	}
	if model.hostEmojiProbePending {
		t.Fatal("expected host emoji probe to be marked complete")
	}
}

func TestModelHostCursorPositionProbeAcceptsNonOriginRowForAmbiguousEmoji(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.hostEmojiProbePending = true

	_, cmd := model.Update(hostCursorPositionMsg{X: 2, Y: 7})
	if cmd != nil {
		_ = cmd()
	}

	visible := model.runtime.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if visible.HostEmojiVS16Mode != shared.AmbiguousEmojiVariationSelectorRaw {
		t.Fatalf("expected host probe to accept non-origin row and select raw mode, got %q", visible.HostEmojiVS16Mode)
	}
	if model.hostEmojiProbePending {
		t.Fatal("expected host emoji probe to be marked complete")
	}
}

func TestModelHostEmojiProbeGiveUpKeepsStableFallback(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.hostEmojiProbePending = true

	_, _ = model.Update(hostEmojiProbeGiveUpMsg{})

	visible := model.runtime.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if visible.HostEmojiVS16Mode != shared.AmbiguousEmojiVariationSelectorStrip {
		t.Fatalf("expected give-up path to keep stable fallback, got %q", visible.HostEmojiVS16Mode)
	}
	if model.hostEmojiProbePending {
		t.Fatal("expected probe to stop pending after give-up")
	}
}

func TestModelHostCursorPositionProbeIgnoresUnexpectedColumnForAmbiguousEmoji(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.runtime.SetHostAmbiguousEmojiVariationSelectorMode(shared.AmbiguousEmojiVariationSelectorStrip)
	model.hostEmojiProbePending = true

	_, cmd := model.Update(hostCursorPositionMsg{X: 17, Y: 7})
	if cmd != nil {
		_ = cmd()
	}

	visible := model.runtime.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if visible.HostEmojiVS16Mode != shared.AmbiguousEmojiVariationSelectorStrip {
		t.Fatalf("expected invalid host probe column to keep conservative mode, got %q", visible.HostEmojiVS16Mode)
	}
	if !model.hostEmojiProbePending {
		t.Fatal("expected invalid host probe column to keep probe pending")
	}
}

func TestModelHostEmojiProbeRetriesUntilGiveUp(t *testing.T) {
	originalMaxAttempts := hostEmojiProbeMaxAttempts
	originalRetryDelay := hostEmojiProbeRetryDelay
	t.Cleanup(func() {
		hostEmojiProbeMaxAttempts = originalMaxAttempts
		hostEmojiProbeRetryDelay = originalRetryDelay
	})
	hostEmojiProbeMaxAttempts = 2
	hostEmojiProbeRetryDelay = time.Millisecond

	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	writer := &recordingControlWriter{}
	model.SetCursorWriter(writer)
	model.hostEmojiProbePending = true

	_, cmd := model.Update(hostEmojiProbeMsg{Attempt: 1})
	if len(writer.controls) != 1 || writer.controls[0] != hostEmojiVariationProbeSequence {
		t.Fatalf("expected first probe write %#v, got %#v", hostEmojiVariationProbeSequence, writer.controls)
	}
	if !model.hostEmojiProbePending {
		t.Fatal("expected probe to stay pending after first attempt")
	}
	if cmd == nil {
		t.Fatal("expected retry command after first attempt")
	}

	_, cmd = model.Update(hostEmojiProbeMsg{Attempt: 2})
	if len(writer.controls) != 2 || writer.controls[1] != hostEmojiVariationProbeSequence {
		t.Fatalf("expected second probe write %#v, got %#v", hostEmojiVariationProbeSequence, writer.controls)
	}
	if !model.hostEmojiProbePending {
		t.Fatal("expected probe to stay pending until give-up window expires")
	}
	if cmd == nil {
		t.Fatal("expected give-up command after final attempt")
	}

	_, _ = model.Update(hostEmojiProbeGiveUpMsg{})
	if model.hostEmojiProbePending {
		t.Fatal("expected probe to stop pending after give-up")
	}
}

func TestModelInitBootstrapsFromSessionSnapshot(t *testing.T) {
	client := &recordingBridgeClient{
		getSessionErr: errors.New("session not found"),
		sessionSnapshot: &protocol.SessionSnapshot{
			Session: protocol.SessionInfo{ID: "main", Revision: 1},
			View:    &protocol.ViewInfo{ViewID: "view-1", SessionID: "main"},
			Workbench: &workbenchdoc.Doc{
				CurrentWorkspace: "main",
				WorkspaceOrder:   []string{"main"},
				Workspaces: map[string]*workbenchdoc.Workspace{
					"main": {
						Name: "main",
						Tabs: []*workbenchdoc.Tab{{
							ID:           "1",
							Name:         "1",
							Root:         workbenchdoc.NewLeaf("1"),
							Panes:        map[string]*workbenchdoc.Pane{"1": {ID: "1"}},
							ActivePaneID: "1",
						}},
						ActiveTab: 0,
					},
				},
			},
		},
	}
	model := New(shared.Config{SessionID: "main"}, workbench.NewWorkbench(), runtime.New(client))
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected init cmd after session bootstrap")
	}
	if len(client.createSessionCalls) != 1 || client.createSessionCalls[0].SessionID != "main" {
		t.Fatalf("expected create session call for main, got %#v", client.createSessionCalls)
	}
	if len(client.attachSessionCalls) != 1 || client.attachSessionCalls[0].SessionID != "main" {
		t.Fatalf("expected attach session call for main, got %#v", client.attachSessionCalls)
	}
	if model.sessionID != "main" || model.sessionViewID != "view-1" || model.sessionRevision != 1 {
		t.Fatalf("unexpected session state: id=%q view=%q rev=%d", model.sessionID, model.sessionViewID, model.sessionRevision)
	}
	if ws := model.workbench.CurrentWorkspace(); ws == nil || ws.Name != "main" {
		t.Fatalf("expected imported session workspace, got %#v", ws)
	}
}

func TestSaveStateCmdReplacesSessionWhenSessionAttached(t *testing.T) {
	client := &recordingBridgeClient{
		sessionSnapshot: &protocol.SessionSnapshot{
			Session: protocol.SessionInfo{ID: "main", Revision: 2},
			View:    &protocol.ViewInfo{ViewID: "view-1", SessionID: "main"},
		},
	}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "1",
			Name:         "1",
			ActivePaneID: "1",
			Panes:        map[string]*workbench.PaneState{"1": {ID: "1", TerminalID: "term-1"}},
			Root:         workbench.NewLeaf("1"),
		}},
	})
	model := New(shared.Config{SessionID: "main"}, wb, runtime.New(client))
	model.sessionID = "main"
	model.sessionRevision = 1

	cmd := model.saveStateCmd()
	if cmd == nil {
		t.Fatal("expected session replace cmd")
	}
	msg := cmd()
	snapshot, ok := msg.(sessionSnapshotMsg)
	if !ok {
		t.Fatalf("expected sessionSnapshotMsg, got %#v", msg)
	}
	if snapshot.Err != nil {
		t.Fatalf("unexpected session replace error: %v", snapshot.Err)
	}
	if len(client.replaceCalls) != 1 {
		t.Fatalf("expected one replace call, got %d", len(client.replaceCalls))
	}
	if client.replaceCalls[0].SessionID != "main" || client.replaceCalls[0].BaseRevision != 1 {
		t.Fatalf("unexpected replace params: %#v", client.replaceCalls[0])
	}
}

func TestSaveStateCmdSuppressesRecoverableSessionRevisionConflict(t *testing.T) {
	client := &recordingBridgeClient{
		sessionSnapshot: &protocol.SessionSnapshot{
			Session: protocol.SessionInfo{ID: "main", Revision: 10},
			View:    &protocol.ViewInfo{ViewID: "view-1", SessionID: "main"},
		},
		replaceSessionErr: fmt.Errorf("protocol error 409: workbenchsvc: session revision conflict: expected 9, got 10"),
	}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "1",
			Name:         "1",
			ActivePaneID: "1",
			Panes:        map[string]*workbench.PaneState{"1": {ID: "1", TerminalID: "term-1"}},
			Root:         workbench.NewLeaf("1"),
		}},
	})
	model := New(shared.Config{SessionID: "main"}, wb, runtime.New(client))
	model.sessionID = "main"
	model.sessionRevision = 9

	cmd := model.saveStateCmd()
	if cmd == nil {
		t.Fatal("expected session replace cmd")
	}
	msg := cmd()
	snapshot, ok := msg.(sessionSnapshotMsg)
	if !ok {
		t.Fatalf("expected sessionSnapshotMsg, got %#v", msg)
	}
	if snapshot.Err != nil {
		t.Fatalf("expected revision conflict to be absorbed, got %v", snapshot.Err)
	}
	if snapshot.Snapshot == nil || snapshot.Snapshot.Session.Revision != 10 {
		t.Fatalf("expected latest session snapshot after conflict, got %#v", snapshot.Snapshot)
	}
	if len(client.getSessionCalls) != 1 || client.getSessionCalls[0] != "main" {
		t.Fatalf("expected conflict path to fetch latest session, got %#v", client.getSessionCalls)
	}
}

func TestApplySessionSnapshotKeepsLocalProjectionForCurrentView(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:              "tab-1",
					Name:            "tab 1",
					ActivePaneID:    "pane-1",
					FloatingVisible: true,
					Panes: map[string]*workbench.PaneState{
						"pane-1":  {ID: "pane-1", Title: "tiled"},
						"float-1": {ID: "float-1", Title: "float"},
					},
					Root: workbench.NewLeaf("pane-1"),
					Floating: []*workbench.FloatingState{{
						PaneID: "float-1",
						Rect:   workbench.Rect{X: 10, Y: 5, W: 20, H: 8},
						Z:      0,
					}},
				}},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"

	model.applySessionSnapshot(&protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "main", Revision: 2},
		View: &protocol.ViewInfo{
			ViewID:              "view-local",
			SessionID:           "main",
			ActiveWorkspaceName: "main",
			ActiveTabID:         "tab-1",
			FocusedPaneID:       "float-1",
		},
		Workbench: &workbenchdoc.Doc{
			CurrentWorkspace: "main",
			WorkspaceOrder:   []string{"main"},
			Workspaces: map[string]*workbenchdoc.Workspace{
				"main": {
					Name:      "main",
					ActiveTab: 0,
					Tabs: []*workbenchdoc.Tab{{
						ID:              "tab-1",
						Name:            "tab 1",
						ActivePaneID:    "float-1",
						FloatingVisible: true,
						Root:            workbenchdoc.NewLeaf("pane-1"),
						Panes: map[string]*workbenchdoc.Pane{
							"pane-1":  {ID: "pane-1", Title: "tiled"},
							"float-1": {ID: "float-1", Title: "float"},
						},
						Floating: []*workbenchdoc.FloatingPane{{
							PaneID: "float-1",
							Rect:   workbenchdoc.Rect{X: 10, Y: 5, W: 20, H: 8},
							Z:      0,
						}},
					}},
				},
			},
		},
	})

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab after snapshot apply")
	}
	if tab.ActivePaneID != "pane-1" {
		t.Fatalf("expected local focused pane to win for current view, got %q", tab.ActivePaneID)
	}
	if !tab.FloatingVisible {
		t.Fatal("expected floating layer visibility to remain enabled")
	}
	if visible := model.workbench.VisibleWithSize(model.bodyRect()); visible == nil || len(visible.FloatingPanes) != 1 {
		t.Fatalf("expected floating pane to stay projected after snapshot, got %#v", visible)
	}
}

func TestModelUpdatePickerNavigation(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{Items: []modal.PickerItem{{TerminalID: "t1"}, {TerminalID: "t2"}}}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	msg := cmd()
	action, ok := msg.(input.SemanticAction)
	if !ok || action.Kind != input.ActionPickerDown {
		t.Fatalf("expected picker down action, got %#v", msg)
	}
	_, _ = model.Update(action)
	if model.modalHost.Picker.Selected != 1 {
		t.Fatalf("expected selected index 1, got %d", model.modalHost.Picker.Selected)
	}
}

func TestModelUpdateEmptyPaneKeyboardNavigationEnterOpensPicker(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "empty"},
					},
					Root: workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session after enter, got %#v", model.modalHost.Session)
	}
	if got := model.emptyPaneSelectionIndex; got != 0 {
		t.Fatalf("expected default empty-pane selection 0, got %d", got)
	}
}

func TestModelUpdateEmptyPaneKeyboardNavigationDownEnterOpensCreatePrompt(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "empty"},
					},
					Root: workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if got := model.emptyPaneSelectionIndex; got != 1 {
		t.Fatalf("expected empty-pane selection 1 after down, got %d", got)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create-terminal prompt after down+enter, got %#v", model.modalHost.Prompt)
	}
}

func TestModelUpdateExitedPaneKeyboardNavigationDownEnterOpensPicker(t *testing.T) {
	model := setupModel(t, modelOpts{})
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected term-1 runtime")
	}
	terminal.State = "exited"

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if got := model.exitedPaneSelectionIndex; got != 1 {
		t.Fatalf("expected exited-pane selection 1 after down, got %d", got)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker after exited-pane down+enter, got %#v", model.modalHost.Session)
	}
}

func TestTerminalAttachedResetsTabScrollOffset(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	tab.ScrollOffset = 3

	_, _ = model.Update(orchestrator.TerminalAttachedMsg{PaneID: "pane-1", TerminalID: "term-1", Channel: 7})

	if got := tab.ScrollOffset; got != 0 {
		t.Fatalf("expected attach to reset scroll offset, got %d", got)
	}
}

func TestModelUpdateSemanticActionMsgUsesSameLocalActionPath(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))

	updated, cmd := model.Update(SemanticActionMsg{Action: input.SemanticAction{Kind: input.ActionEnterPaneMode}})
	if updated != model {
		t.Fatal("expected model pointer to remain stable")
	}
	if cmd == nil {
		t.Fatal("expected prefix timeout command from pane mode entry")
	}
	if model.input.Mode().Kind != input.ModePane {
		t.Fatalf("expected pane mode, got %q", model.input.Mode().Kind)
	}
}

func TestAttachPaneTerminalShowsTerminalNameImmediately(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-1",
			Name:  "shell",
			State: "running",
		}}},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := New(shared.Config{}, wb, runtime.New(client))
	model.width = 100
	model.height = 30

	cmd := model.attachPaneTerminalCmd("", "pane-1", "term-1")
	if cmd == nil {
		t.Fatal("expected attach command")
	}
	drainCmd(t, model, cmd, 20)

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if pane.Title != "shell" {
		t.Fatalf("expected pane title initialized from terminal name, got %q", pane.Title)
	}
	if view := xansi.Strip(model.View()); !strings.Contains(view, "shell") {
		t.Fatalf("expected view to contain terminal name immediately, got:\n%s", view)
	}
}

func TestModelTerminalAttachedBindsPaneAndClosesPicker(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{Items: []modal.PickerItem{{TerminalID: "term-1", Name: "shell", State: "running"}}}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})
	if pane := model.workbench.ActivePane(); pane != nil {
		pane.TerminalID = "term-1"
	}

	_, _ = model.Update(orchestrator.TerminalAttachedMsg{PaneID: "pane-1", TerminalID: "term-1", Channel: 1})
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" {
		t.Fatalf("expected active pane bound to term-1, got %#v", pane)
	}
	if model.modalHost.Session != nil {
		t.Fatal("expected picker session to close after attach")
	}
	if model.input.Mode().Kind != input.ModeNormal {
		t.Fatalf("expected input mode normal after attach, got %q", model.input.Mode().Kind)
	}
}

func TestModelTerminalAttachedResizesLocalPaneBeforeReady(t *testing.T) {
	client := &recordingBridgeClient{}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(client)
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 7
	terminal.BoundPaneIDs = []string{"pane-1"}
	terminal.OwnerPaneID = "pane-1"
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	model := New(shared.Config{}, wb, rt)
	model.width = 100
	model.height = 30

	_, cmd := model.Update(orchestrator.TerminalAttachedMsg{PaneID: "pane-1", TerminalID: "term-1", Channel: 7})
	drainCmd(t, model, cmd, 20)

	if len(client.resizes) != 1 || client.resizes[0].channel != 7 {
		t.Fatalf("expected local attach to resize pane once, got %#v", client.resizes)
	}
}

func TestHandleKeyMsgInjectsActivePaneIntoTerminalInput(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	client := &recordingBridgeClient{}
	rt := runtime.New(client)
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	model := New(shared.Config{}, wb, rt)
	cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("expected queued terminal input command")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if client.inputCalls[0].channel != 7 {
		t.Fatalf("expected pane-1 binding channel 7, got %#v", client.inputCalls[0])
	}
	if string(client.inputCalls[0].data) != "a" {
		t.Fatalf("expected input data 'a', got %q", client.inputCalls[0].data)
	}
}

func TestModelUpdateTerminalInputMsgUsesSameRuntimePath(t *testing.T) {
	client := &recordingBridgeClient{}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(client)
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	model := New(shared.Config{}, wb, rt)

	updated, cmd := model.Update(TerminalInputMsg{
		Input: input.TerminalInput{
			PaneID: "pane-1",
			Data:   []byte("pwd\n"),
		},
	})
	if updated != model {
		t.Fatal("expected model pointer to remain stable")
	}
	if cmd == nil {
		t.Fatal("expected terminal input command")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if client.inputCalls[0].channel != 7 || string(client.inputCalls[0].data) != "pwd\n" {
		t.Fatalf("unexpected input call: %#v", client.inputCalls[0])
	}
}

func TestHandleKeyMsgUsesApplicationCursorEncoding(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Modes:      protocol.TerminalModes{ApplicationCursor: true},
	}
	client := &recordingBridgeClient{}
	rt.SetInvalidate(func() {})
	rt2 := runtime.New(client)
	rt2.Registry().GetOrCreate("term-1").Snapshot = rt.Registry().Get("term-1").Snapshot
	binding := rt2.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	model := New(shared.Config{}, wb, rt2)

	cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyUp})
	if cmd == nil {
		t.Fatal("expected terminal input command")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "\x1bOA" {
		t.Fatalf("expected application cursor up sequence, got %q", string(client.inputCalls[0].data))
	}
}

func TestHandleKeyMsgInterceptsNormalModeRestartForExitedPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.runtime.Registry().GetOrCreate("term-1").State = "exited"

	cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if cmd == nil {
		t.Fatal("expected restart action command for exited pane")
	}
	msg := cmd()
	action, ok := msg.(input.SemanticAction)
	if !ok {
		t.Fatalf("expected restart semantic action, got %#v", msg)
	}
	if action.Kind != input.ActionRestartTerminal || action.PaneID != "pane-1" {
		t.Fatalf("unexpected restart action: %#v", action)
	}
}

func TestHandleKeyMsgEncodesShiftTab(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	client := &recordingBridgeClient{}
	rt := runtime.New(client)
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	model := New(shared.Config{}, wb, rt)

	cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd == nil {
		t.Fatal("expected terminal input command")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "\x1b[Z" {
		t.Fatalf("expected shift-tab sequence, got %q", string(client.inputCalls[0].data))
	}
}

func TestHandleTerminalInputQueuesWhileSendInFlight(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	started := make(chan inputCall, 1)
	release := make(chan struct{})
	client := &recordingBridgeClient{inputStarted: started, inputBlock: release}
	rt := runtime.New(client)
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	model := New(shared.Config{}, wb, rt)

	firstCmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if firstCmd == nil {
		t.Fatal("expected first queued terminal input command")
	}
	done := make(chan tea.Msg, 1)
	go func() {
		done <- firstCmd()
	}()
	select {
	case call := <-started:
		if string(call.data) != "a" {
			t.Fatalf("expected first input call to send 'a', got %#v", call)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for first input send to start")
	}

	secondCmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if secondCmd != nil {
		t.Fatalf("expected second input to queue behind in-flight send, got cmd %#v", secondCmd)
	}
	if !model.terminalInputSending {
		t.Fatal("expected input queue to remain marked as sending")
	}
	if len(model.pendingTerminalInputs) != 1 || string(model.pendingTerminalInputs[0].Data) != "b" {
		t.Fatalf("expected second input to remain queued, got %#v", model.pendingTerminalInputs)
	}

	close(release)
	firstMsg := <-done
	_, nextCmd := model.Update(firstMsg)
	if nextCmd == nil {
		t.Fatal("expected queued follow-up input command after first send completed")
	}
	drainCmd(t, model, nextCmd, 10)

	if len(client.inputCalls) != 2 {
		t.Fatalf("expected two ordered input calls, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data) + string(client.inputCalls[1].data); got != "ab" {
		t.Fatalf("expected ordered queued input calls, got %q", got)
	}
	if model.terminalInputSending {
		t.Fatal("expected input queue to become idle after draining")
	}
}

func TestQueueInvalidateCoalescesPendingRedraws(t *testing.T) {
	model := New(shared.Config{}, nil, runtime.New(nil))
	sent := make(chan tea.Msg, 4)
	model.SetSendFunc(func(msg tea.Msg) {
		sent <- msg
	})

	model.queueInvalidate()
	model.queueInvalidate()
	select {
	case msg := <-sent:
		if _, ok := msg.(InvalidateMsg); !ok {
			t.Fatalf("expected invalidate message, got %#v", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for coalesced invalidate message")
	}
	select {
	case msg := <-sent:
		t.Fatalf("expected one coalesced invalidate message before update, got extra %#v", msg)
	case <-time.After(50 * time.Millisecond):
	}

	_, _ = model.Update(InvalidateMsg{})
	model.queueInvalidate()
	select {
	case msg := <-sent:
		if _, ok := msg.(InvalidateMsg); !ok {
			t.Fatalf("expected invalidate message after re-arm, got %#v", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for re-armed invalidate message")
	}
}

func TestQueueInvalidateDoesNotBlockOnSend(t *testing.T) {
	model := New(shared.Config{}, nil, runtime.New(nil))
	release := make(chan struct{})
	model.SetSendFunc(func(msg tea.Msg) {
		<-release
	})

	done := make(chan struct{})
	go func() {
		model.queueInvalidate()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("queueInvalidate blocked on send")
	}

	close(release)
}

func TestPendingAttachBuffersTerminalInputUntilAttachCompletes(t *testing.T) {
	client := &recordingBridgeClient{}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(client)
	model := New(shared.Config{}, wb, rt)
	model.width = 120
	model.height = 40
	model.markPendingPaneAttach("pane-1", "")

	for _, ch := range []rune{'A', 'B', 'C', 'D'} {
		cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		if cmd != nil {
			t.Fatalf("expected input %q to stay buffered while attach is pending, got cmd %#v", string(ch), cmd)
		}
	}
	if got := len(model.pendingTerminalInputs); got != 4 {
		t.Fatalf("expected four buffered inputs, got %d", got)
	}
	if model.modalHost.Session != nil {
		t.Fatalf("expected pending attach input not to reopen picker, got %#v", model.modalHost.Session)
	}

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.TerminalID = "term-1"
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	rt.Registry().GetOrCreate("term-1").State = "running"

	_, cmd := model.Update(orchestrator.TerminalAttachedMsg{PaneID: "pane-1", TerminalID: "term-1", Channel: 7})
	drainCmd(t, model, cmd, 20)

	if len(client.resizes) != 1 || client.resizes[0].channel != 7 {
		t.Fatalf("expected attach completion to resize pane before flushing input, got %#v", client.resizes)
	}
	if len(client.inputCalls) != 4 {
		t.Fatalf("expected four flushed input calls after attach, got %#v", client.inputCalls)
	}
	var got strings.Builder
	for _, call := range client.inputCalls {
		got.Write(call.data)
	}
	if got.String() != "ABCD" {
		t.Fatalf("expected buffered input order preserved, got %q", got.String())
	}
	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected pending attach marker cleared after attach")
	}
}

func TestAttachPaneTerminalFailureClearsPendingAttach(t *testing.T) {
	client := &recordingBridgeClient{
		attachErr:          errors.New("attach failed"),
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	drainCmd(t, model, model.attachPaneTerminalCmd("tab-1", "pane-1", "term-2"), 20)

	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected pending attach marker cleared after attach failure")
	}
	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-2" {
		t.Fatalf("expected attach attempt for term-2, got %#v", client.attachCalls)
	}
	if model.err == nil || !strings.Contains(model.err.Error(), "attach failed") {
		t.Fatalf("expected attach failure surfaced in model error, got %#v", model.err)
	}
}

func TestRestartPaneTerminalFailureClearsPendingAttach(t *testing.T) {
	client := &recordingBridgeClient{
		attachErr:          errors.New("attach after restart failed"),
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	exitCode := 23
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "exited"
	terminal.ExitCode = &exitCode

	dispatchKey(t, model, runeKeyMsg('R'))

	if len(client.restartCalls) != 1 || client.restartCalls[0] != "term-1" {
		t.Fatalf("expected restart for term-1, got %#v", client.restartCalls)
	}
	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-1" {
		t.Fatalf("expected attach retry for term-1 after restart, got %#v", client.attachCalls)
	}
	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected pending attach marker cleared after restart attach failure")
	}
	if model.err == nil || !strings.Contains(model.err.Error(), "attach after restart failed") {
		t.Fatalf("expected restart attach failure surfaced in model error, got %#v", model.err)
	}
}

func TestModelViewShowsPickerItems(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.width = 100
	model.height = 30
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{
		Title:    "Terminal Picker",
		Selected: 1,
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "running"},
			{TerminalID: "term-2", Name: "logs", State: "exited"},
		},
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"Terminal Picker", "shell", "logs"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker view missing %q:\n%s", want, view)
		}
	}
}

func TestModelSubmitCreateNewPickerSelectionOpensPrompt(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{
		Selected: 0,
		Items: []modal.PickerItem{
			{CreateNew: true, Name: "new terminal", Description: "Create a new terminal in this pane"},
		},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected local prompt open without async msg, got %#v", msg)
		}
	}
	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePrompt {
		t.Fatalf("expected prompt session, got %#v", model.modalHost.Session)
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create-terminal-form prompt, got %#v", model.modalHost.Prompt)
	}
	if model.input.Mode().Kind != input.ModePrompt {
		t.Fatalf("expected input mode prompt, got %q", model.input.Mode().Kind)
	}
}

func TestModelPromptSubmitCreateTerminalFormRequiresName(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:        "create-terminal-form",
		Title:       "Create Terminal",
		PaneID:      "pane-1",
		Command:     []string{"/bin/sh"},
		DefaultName: "shell",
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Required: true},
			{Key: "command", Label: "command", Placeholder: "/bin/sh"},
			{Key: "workdir", Label: "workdir"},
			{Key: "tags", Label: "tags"},
		},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})
	if cmd != nil {
		if msg := cmd(); msg == nil {
			t.Fatal("expected validation error for empty name")
		}
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create-terminal-form prompt to stay open, got %#v", model.modalHost.Prompt)
	}
}

func TestModelPromptSubmitCreateTerminalFormRejectsDuplicateName(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{{ID: "term-1", Name: "demo", State: "running"}},
		},
	}
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(client))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:        "create-terminal-form",
		Title:       "Create Terminal",
		PaneID:      "pane-1",
		Command:     []string{"/bin/sh"},
		DefaultName: "shell",
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Value: "demo", Cursor: 4, Required: true},
			{Key: "command", Label: "command", Placeholder: "/bin/sh"},
			{Key: "workdir", Label: "workdir"},
			{Key: "tags", Label: "tags"},
		},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})
	if cmd == nil {
		t.Fatal("expected duplicate-name validation command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected duplicate-name validation error")
	}
	if len(client.createCalls) != 0 {
		t.Fatalf("expected create not to be called, got %#v", client.createCalls)
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create-terminal-form prompt to stay open, got %#v", model.modalHost.Prompt)
	}
}

func TestModelPromptSubmitAdvancesEditTerminalToTags(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:        "edit-terminal-name",
		Title:       "Edit Terminal",
		Value:       "renamed",
		Original:    "shell",
		DefaultName: "shell",
		TerminalID:  "term-1",
		Tags:        map[string]string{"env": "test", "role": "dev"},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected local prompt advance without async msg, got %#v", msg)
		}
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "edit-terminal-tags" {
		t.Fatalf("expected edit-terminal-tags prompt, got %#v", model.modalHost.Prompt)
	}
	if model.modalHost.Prompt.Name != "renamed" {
		t.Fatalf("expected prompt to retain edited name, got %#v", model.modalHost.Prompt)
	}
	if got := model.modalHost.Prompt.Value; got != "env=test role=dev" {
		t.Fatalf("expected tags prompt to prefill stable tag text, got %q", got)
	}
}

func TestModelPromptSubmitEditTerminalNameRejectsDuplicateName(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", State: "running"},
			},
		},
	}
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(client))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:        "edit-terminal-name",
		Title:       "Edit Terminal",
		Value:       "logs",
		Original:    "shell",
		DefaultName: "shell",
		TerminalID:  "term-1",
		Tags:        map[string]string{"env": "test"},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd == nil {
		t.Fatal("expected duplicate-name validation command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected duplicate-name validation error")
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "edit-terminal-name" {
		t.Fatalf("expected edit-terminal-name prompt to stay open, got %#v", model.modalHost.Prompt)
	}
}

func TestModelPromptSubmitEditTerminalSavesMetadataAndState(t *testing.T) {
	statePath := t.TempDir() + "/workspace-state.json"
	client := &recordingBridgeClient{}
	rt := runtime.New(client)
	rt.Registry().GetOrCreate("term-1").Name = "shell"
	rt.Registry().GetOrCreate("term-1").Command = []string{"bash"}
	rt.Registry().GetOrCreate("term-1").Tags = map[string]string{"role": "dev"}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{WorkspaceStatePath: statePath}, wb, rt)
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:       "edit-terminal-tags",
		Title:      "Edit Terminal",
		Value:      "env=test role=ops",
		Name:       "renamed",
		Original:   "shell",
		TerminalID: "term-1",
		AllowEmpty: true,
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd == nil {
		t.Fatal("expected async edit metadata command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message from edit metadata command, got %#v", msg)
	}
	if len(client.setMetadataCalls) != 1 {
		t.Fatalf("expected one metadata call, got %#v", client.setMetadataCalls)
	}
	if client.setMetadataCalls[0].terminalID != "term-1" || client.setMetadataCalls[0].name != "renamed" {
		t.Fatalf("unexpected metadata target: %#v", client.setMetadataCalls[0])
	}
	if client.setMetadataCalls[0].tags["env"] != "test" || client.setMetadataCalls[0].tags["role"] != "ops" {
		t.Fatalf("unexpected metadata tags: %#v", client.setMetadataCalls[0].tags)
	}
	terminal := rt.Registry().Get("term-1")
	if terminal == nil || terminal.Name != "renamed" {
		t.Fatalf("expected runtime registry name updated, got %#v", terminal)
	}
	if terminal.Tags["env"] != "test" || terminal.Tags["role"] != "ops" {
		t.Fatalf("expected runtime registry tags updated, got %#v", terminal.Tags)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	file, err := persist.Load(data)
	if err != nil {
		t.Fatalf("persist.Load: %v", err)
	}
	_ = file
	if strings.Contains(string(data), "\"terminal_metadata\"") {
		t.Fatalf("expected workspace save to omit terminal metadata cache, got %s", string(data))
	}
}

func TestModelPromptSubmitEditTerminalTagsRejectsDuplicateName(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", State: "running"},
			},
		},
	}
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(client))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:       "edit-terminal-tags",
		Title:      "Edit Terminal",
		Value:      "env=test",
		Name:       "logs",
		Original:   "shell",
		TerminalID: "term-1",
		AllowEmpty: true,
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd == nil {
		t.Fatal("expected duplicate-name validation command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected duplicate-name validation error")
	}
	if len(client.setMetadataCalls) != 0 {
		t.Fatalf("expected metadata not to be saved, got %#v", client.setMetadataCalls)
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "edit-terminal-tags" {
		t.Fatalf("expected edit-terminal-tags prompt to stay open, got %#v", model.modalHost.Prompt)
	}
}

func TestModelPickerSubmitWithEmptyFilteredResultsIsNoop(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
			},
		},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{
		Items: []modal.PickerItem{{TerminalID: "term-1", Name: "shell", State: "running"}},
		Query: "missing",
	}
	model.modalHost.Picker.ApplyFilter()
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected nil submit result for empty filtered picker, got %#v", msg)
		}
	}
	if len(client.attachCalls) != 0 {
		t.Fatalf("expected no attach call for empty filtered picker, got %#v", client.attachCalls)
	}
	if model.input.Mode().Kind != input.ModePicker {
		t.Fatalf("expected picker to remain open, got mode %q", model.input.Mode().Kind)
	}
}

func TestModelTerminalManagerEditCancelReturnsToTerminalManagerMode(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", State: "running"},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	_, _ = model.Update(input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	drainCmd(t, model, cmd, 10)
	_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	_, _ = model.Update(input.SemanticAction{Kind: input.ActionEditTerminal})

	if model.input.Mode().Kind != input.ModePrompt {
		t.Fatalf("expected prompt mode after edit open, got %q", model.input.Mode().Kind)
	}

	_, _ = model.Update(input.SemanticAction{Kind: input.ActionCancelMode})

	if model.input.Mode().Kind != input.ModeTerminalManager {
		t.Fatalf("expected terminal-manager mode after cancel, got %q", model.input.Mode().Kind)
	}
	if model.terminalPage == nil {
		t.Fatal("expected terminal page to remain open after cancel")
	}
}

func TestModelTerminalManagerEditSaveReturnsToTerminalManagerMode(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", State: "running", Tags: map[string]string{"role": "ops"}},
			},
		},
	}
	rt := runtime.New(client)
	rt.Registry().GetOrCreate("term-2").Name = "logs"
	rt.Registry().GetOrCreate("term-2").Tags = map[string]string{"role": "ops"}
	model := New(shared.Config{WorkspaceStatePath: t.TempDir() + "/workspace-state.json"}, workbench.NewWorkbench(), rt)
	model.width = 120
	model.height = 40
	model.workbench.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	_, _ = model.Update(input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	drainCmd(t, model, cmd, 10)
	_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	_, _ = model.Update(input.SemanticAction{Kind: input.ActionEditTerminal})

	model.modalHost.Prompt.Value = "renamed"
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected local advance to tags prompt, got %#v", msg)
		}
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "edit-terminal-tags" {
		t.Fatalf("expected tags prompt, got %#v", model.modalHost.Prompt)
	}
	model.modalHost.Prompt.Value = "role=ops env=test"

	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd == nil {
		t.Fatal("expected async metadata save command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message from edit save command, got %#v", msg)
	}

	if model.input.Mode().Kind != input.ModeTerminalManager {
		t.Fatalf("expected terminal-manager mode after save, got %q", model.input.Mode().Kind)
	}
	if model.terminalPage == nil {
		t.Fatal("expected terminal page to remain open after save")
	}
}

func TestModelPromptSubmitRenameTabRejectsDuplicateName(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{
			{
				ID:           "tab-1",
				Name:         "one",
				ActivePaneID: "pane-1",
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1"},
				},
				Root: workbench.NewLeaf("pane-1"),
			},
			{
				ID:           "tab-2",
				Name:         "two",
				ActivePaneID: "pane-2",
				Panes: map[string]*workbench.PaneState{
					"pane-2": {ID: "pane-2"},
				},
				Root: workbench.NewLeaf("pane-2"),
			},
		},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:       "rename-tab",
		Title:      "rename tab",
		Value:      "two",
		Original:   "one",
		AllowEmpty: false,
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd == nil {
		t.Fatal("expected duplicate-name validation command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected duplicate-name validation error")
	}
	if got := model.workbench.CurrentTab().Name; got != "one" {
		t.Fatalf("expected current tab name to remain one, got %q", got)
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "rename-tab" {
		t.Fatalf("expected rename-tab prompt to stay open, got %#v", model.modalHost.Prompt)
	}
}

func TestModelPromptSubmitCreatesAndAttachesTerminal(t *testing.T) {
	client := &recordingBridgeClient{
		createResult: &protocol.CreateResult{TerminalID: "term-new"},
		attachResult: &protocol.AttachResult{Channel: 13, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}},
				},
			},
		},
	}
	rt := runtime.New(client)
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, rt)
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:        "create-terminal-form",
		Title:       "Create Terminal",
		DefaultName: "shell",
		PaneID:      "pane-1",
		Command:     []string{"/bin/sh"},
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Value: "demo", Cursor: 4, Required: true},
			{Key: "command", Label: "command"},
			{Key: "workdir", Label: "workdir", Value: "/tmp/demo", Cursor: 9},
			{Key: "tags", Label: "tags", Value: "role=dev env=test", Cursor: 17},
		},
		ActiveField: 3,
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})
	if cmd == nil {
		t.Fatal("expected async create command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected batch of attach/snapshot messages, got %#v", msg)
	}
	for _, item := range batch {
		if next := item(); next != nil {
			_, _ = model.Update(next)
		}
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call, got %d", len(client.createCalls))
	}
	if got := client.createCalls[0].params.Name; got != "demo" {
		t.Fatalf("expected create name demo, got %q", got)
	}
	if got := client.createCalls[0].params.Dir; got != "/tmp/demo" {
		t.Fatalf("expected create dir /tmp/demo, got %q", got)
	}
	if got := client.createCalls[0].params.Tags["role"]; got != "dev" || client.createCalls[0].params.Tags["env"] != "test" {
		t.Fatalf("unexpected create tags: %#v", client.createCalls[0].params.Tags)
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-new" {
		t.Fatalf("expected active pane attached to created terminal, got %#v", pane)
	}
	if model.modalHost.Session != nil {
		t.Fatalf("expected prompt session closed after create, got %#v", model.modalHost.Session)
	}
}

func TestModelPromptOverlayInputStillUpdatesValue(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:  "create-terminal-form",
		Title: "Create Terminal",
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Value: "de", Cursor: 2, Required: true},
			{Key: "command", Label: "command"},
			{Key: "workdir", Label: "workdir"},
			{Key: "tags", Label: "tags"},
		},
		PaneID: "pane-1",
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if got := model.modalHost.Prompt.Field("name").Value; got != "dem" {
		t.Fatalf("expected prompt value dem after rune input, got %q", got)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := model.modalHost.Prompt.Field("name").Value; got != "de" {
		t.Fatalf("expected prompt value de after backspace, got %q", got)
	}
}

func TestModelPickerKillTerminalRemovesSelectedItemAndInvokesBridgeClient(t *testing.T) {
	client := &recordingBridgeClient{}
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(client))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "picker-1"}
	model.modalHost.Picker = &modal.PickerState{
		Selected: 1,
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell"},
			{TerminalID: "term-2", Name: "logs"},
		},
	}
	model.modalHost.Picker.ApplyFilter()
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "picker-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionKillTerminal})
	if cmd == nil {
		t.Fatal("expected kill terminal command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message from kill command, got %#v", msg)
	}
	if len(client.killCalls) != 1 {
		t.Fatalf("expected one kill call, got %d", len(client.killCalls))
	}
	if client.killCalls[0] != "term-2" {
		t.Fatalf("expected terminal term-2 to be killed, got %#v", client.killCalls)
	}
	if len(model.modalHost.Picker.Items) != 1 {
		t.Fatalf("expected 1 picker item after removal, got %d", len(model.modalHost.Picker.Items))
	}
	if got := model.modalHost.Picker.Items[0].TerminalID; got != "term-1" {
		t.Fatalf("expected remaining terminal term-1, got %q", got)
	}
	if got := len(model.modalHost.Picker.VisibleItems()); got != 1 {
		t.Fatalf("expected 1 visible picker item after filter, got %d", got)
	}
}

func TestModelUpdateWindowSizeAndError(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	_, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if model.width != 120 || model.height != 40 {
		t.Fatalf("unexpected size: %dx%d", model.width, model.height)
	}
	boom := errors.New("boom")
	_, _ = model.Update(boom)
	if !errors.Is(model.err, boom) {
		t.Fatalf("expected stored error, got %v", model.err)
	}
}

func TestModelUpdateWindowSizeResizesActivePaneTerminals(t *testing.T) {
	client := &recordingBridgeClient{}
	rt := runtime.New(client)
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt.Registry().GetOrCreate("term-1").Channel = 7
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true

	model := New(shared.Config{}, wb, rt)
	_, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd == nil {
		t.Fatal("expected resize command")
	}
	if msg := cmd(); msg != nil {
		if err, ok := msg.(error); ok {
			t.Fatalf("unexpected resize error: %v", err)
		}
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected exactly one resize call, got %d", len(client.resizes))
	}
	got := client.resizes[0]
	visible := wb.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || len(visible.Tabs[visible.ActiveTab].Panes) == 0 {
		t.Fatalf("expected visible pane after resize, got %#v", visible)
	}
	wantRect, ok := paneContentRectForVisible(visible.Tabs[visible.ActiveTab].Panes[0])
	if !ok {
		t.Fatal("expected visible pane content rect")
	}
	if got.channel != 7 || got.cols != uint16(wantRect.W) || got.rows != uint16(wantRect.H) {
		t.Fatalf("unexpected resize call: %+v", got)
	}
}

func TestModelTerminalAttachedInSessionDoesNotAcquireLeaseImplicitly(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(client)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Channel = 7
	terminal.State = "running"
	terminal.BoundPaneIDs = []string{"pane-1"}
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true

	model := New(shared.Config{SessionID: "main"}, wb, rt)
	model.sessionID = "main"
	model.sessionViewID = "view-1"

	_, cmd := model.Update(orchestrator.TerminalAttachedMsg{PaneID: "pane-1", TerminalID: "term-1", Channel: 7})
	drainCmd(t, model, cmd, 10)

	if len(client.acquireLeaseCalls) != 0 {
		t.Fatalf("expected terminal attach not to acquire a lease implicitly, got %#v", client.acquireLeaseCalls)
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected terminal attach not to trigger resize, got %#v", client.resizes)
	}
}

func TestModelUpdateWindowSizeReflowsFloatingPaneRects(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.width = 100
	model.height = 42

	tab := model.workbench.CurrentTab()
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 50, Y: 10, W: 30, H: 12}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	_, _ = model.Update(tea.WindowSizeMsg{Width: 50, Height: 22})

	floating := findFloating(tab, "float-1")
	if floating == nil {
		t.Fatal("expected floating pane after resize")
	}
	if floating.Rect != (workbench.Rect{X: 25, Y: 5, W: 15, H: 6}) {
		t.Fatalf("expected floating pane rect to be reflowed, got %#v", floating.Rect)
	}
}

func TestModelUpdateWindowSizeDoesNotReflowFloatingPaneOnInitialSizing(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.width = 0
	model.height = 0

	tab := model.workbench.CurrentTab()
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 30, H: 12}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	_, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 42})

	floating := findFloating(tab, "float-1")
	if floating == nil {
		t.Fatal("expected floating pane after initial sizing")
	}
	if floating.Rect != (workbench.Rect{X: 10, Y: 5, W: 30, H: 12}) {
		t.Fatalf("expected initial sizing to preserve floating rect, got %#v", floating.Rect)
	}
}

func TestModelUpdateWindowSizeClampsOversizedFloatingPaneOnInitialSizing(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.width = 0
	model.height = 0

	tab := model.workbench.CurrentTab()
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 70, Y: 20, W: 50, H: 20}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	_, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 14})

	floating := findFloating(tab, "float-1")
	if floating == nil {
		t.Fatal("expected floating pane after initial sizing")
	}
	if floating.Rect != (workbench.Rect{X: 1, Y: 1, W: 39, H: 11}) {
		t.Fatalf("expected initial sizing to clamp oversized floating rect, got %#v", floating.Rect)
	}
}

func TestInvalidateRenderEffectClampsFloatingPaneBelowViewportSize(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 0, Y: 0, W: 500, H: 500}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	_ = model.applyEffects([]orchestrator.Effect{orchestrator.InvalidateRenderEffect{}})

	floating := findFloating(tab, "float-1")
	if floating == nil {
		t.Fatal("expected floating pane to exist")
	}
	if floating.Rect.W != 119 || floating.Rect.H != 37 {
		t.Fatalf("expected floating pane clamped below viewport size, got %#v", floating.Rect)
	}
}

func TestModelLocalScrollActionsAndQuit(t *testing.T) {
	statePath := t.TempDir() + "/workspace-state.json"
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "demo shell"
	rt.Registry().GetOrCreate("term-1").Command = []string{"bash", "-lc", "htop"}
	rt.Registry().GetOrCreate("term-1").Tags = map[string]string{"role": "dev"}
	model := New(shared.Config{WorkspaceStatePath: statePath}, wb, rt)

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionScrollUp})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected no async msg from scroll up, got %#v", msg)
		}
	}
	if got := model.workbench.CurrentTab().ScrollOffset; got != 1 {
		t.Fatalf("expected scroll offset 1 after scroll up, got %d", got)
	}

	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionScrollDown})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected no async msg from scroll down, got %#v", msg)
		}
	}
	if got := model.workbench.CurrentTab().ScrollOffset; got != 0 {
		t.Fatalf("expected scroll offset 0 after scroll down, got %d", got)
	}

	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(prefixTimeoutMsg); !ok {
			t.Fatalf("expected prefix timeout rearm from entering global mode, got %#v", msg)
		}
	}
	if got := model.input.Mode().Kind; got != input.ModeGlobal {
		t.Fatalf("expected global mode after enter-global action, got %q", got)
	}

	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionQuit})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if msg := cmd(); msg != nil {
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, item := range batch {
				_ = item()
			}
		}
	}
	if !model.quitting {
		t.Fatal("expected model.quitting to be set")
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected quit to save state file, stat err: %v", err)
	}
}

func TestModelSaveStateCmdWritesWorkspaceStateFile(t *testing.T) {
	statePath := t.TempDir() + "/workspace-state.json"
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "demo shell"
	rt.Registry().GetOrCreate("term-1").Command = []string{"bash", "-lc", "htop"}
	rt.Registry().GetOrCreate("term-1").Tags = map[string]string{"role": "dev"}
	model := New(shared.Config{WorkspaceStatePath: statePath}, wb, rt)

	cmd := model.saveStateCmd()
	if cmd == nil {
		t.Fatal("expected save state command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message from save state command, got %#v", msg)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	file, err := persist.Load(data)
	if err != nil {
		t.Fatalf("persist.Load: %v", err)
	}
	if len(file.Data) != 1 || file.Data[0].Name != "main" {
		t.Fatalf("expected saved main workspace, got %#v", file.Data)
	}
	if strings.Contains(string(data), "\"terminal_metadata\"") {
		t.Fatalf("expected save to omit terminal metadata cache, got %s", string(data))
	}
}

func TestModelUpdateTerminalAttachedSavesState(t *testing.T) {
	statePath := t.TempDir() + "/workspace-state.json"
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{WorkspaceStatePath: statePath}, wb, runtime.New(nil))
	if pane := model.workbench.ActivePane(); pane != nil {
		pane.TerminalID = "term-9"
	}

	_, cmd := model.Update(orchestrator.TerminalAttachedMsg{PaneID: "pane-1", TerminalID: "term-9", Channel: 7})
	if cmd == nil {
		t.Fatal("expected save command after terminal attach")
	}
	// cmd is a batch of save + resize; drain it so the save runs.
	if msg := cmd(); msg != nil {
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, item := range batch {
				_ = item()
			}
		}
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	file, err := persist.Load(data)
	if err != nil {
		t.Fatalf("persist.Load: %v", err)
	}
	if got := file.Data[0].Tabs[0].Panes[0].TerminalID; got != "term-9" {
		t.Fatalf("expected saved terminal binding term-9, got %q", got)
	}
}

func TestModelWorkspacePickerActions(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{Name: "main"})
	wb.AddWorkspace("dev", &workbench.WorkspaceState{Name: "dev"})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModeWorkspacePicker, Phase: modal.ModalPhaseReady, RequestID: "workspace-picker-1"}
	model.modalHost.WorkspacePicker = &modal.WorkspacePickerState{
		Items:    []modal.WorkspacePickerItem{{Name: "main"}, {Name: "dev"}},
		Filtered: []modal.WorkspacePickerItem{{Name: "main"}, {Name: "dev"}},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspacePicker, RequestID: "workspace-picker-1"})

	_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	if got := model.modalHost.WorkspacePicker.Selected; got != 1 {
		t.Fatalf("expected selection to move to 1, got %d", got)
	}

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd == nil {
		t.Fatal("expected workspace switch command")
	}
	msg := cmd()
	action, ok := msg.(input.SemanticAction)
	if !ok || action.Kind != input.ActionSwitchWorkspace || action.Text != "dev" {
		t.Fatalf("expected switch workspace action for dev, got %#v", msg)
	}

	_, _ = model.Update(input.SemanticAction{Kind: input.ActionCancelMode})
	if model.modalHost.Session != nil {
		t.Fatalf("expected workspace picker modal to close, got %#v", model.modalHost.Session)
	}
	if model.input.Mode().Kind != input.ModeNormal {
		t.Fatalf("expected normal mode after cancel, got %q", model.input.Mode().Kind)
	}
}

func TestModelModeCancelReturnsToNormal(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))

	_, _ = model.Update(input.SemanticAction{Kind: input.ActionEnterPaneMode})
	if got := model.input.Mode().Kind; got != input.ModePane {
		t.Fatalf("expected pane mode, got %q", got)
	}

	_, _ = model.Update(input.SemanticAction{Kind: input.ActionCancelMode})
	if got := model.input.Mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected normal mode after cancel, got %q", got)
	}
}

func TestModelViewShowsModeSpecificStatusHints(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.width = 180
	model.height = 30
	model.input.SetMode(input.ModeState{Kind: input.ModePane})

	view := xansi.Strip(model.View())
	for _, want := range []string{"PANE", "[h/j/k/l] FOCUS", "[%] VSPLIT", "[d] DETACH", "[r] RECONNECT", "[X] CLOSE+KILL", "[w] CLOSE", "[Esc] BACK"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
	}
	if strings.Contains(view, "[a] OWNER") {
		t.Fatalf("expected owner action hidden without follower context, got:\n%s", view)
	}
}

func TestModelHelpActionsOpenAndCloseOverlay(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionOpenHelp})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected help open without async msg, got %#v", msg)
		}
	}
	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModeHelp {
		t.Fatalf("expected help modal session, got %#v", model.modalHost.Session)
	}
	if model.modalHost.Help == nil || len(model.modalHost.Help.Sections) == 0 {
		t.Fatalf("expected default help sections, got %#v", model.modalHost.Help)
	}
	if model.input.Mode().Kind != input.ModeHelp {
		t.Fatalf("expected input mode help, got %q", model.input.Mode().Kind)
	}

	_, _ = model.Update(input.SemanticAction{Kind: input.ActionCancelMode})
	if model.modalHost.Session != nil {
		t.Fatalf("expected help modal to close, got %#v", model.modalHost.Session)
	}
	if model.input.Mode().Kind != input.ModeNormal {
		t.Fatalf("expected input mode normal after help close, got %q", model.input.Mode().Kind)
	}
}

func TestModelInitRestoreAutoReattachesPersistedPanes(t *testing.T) {
	statePath := t.TempDir() + "/workspace-state.json"
	source := workbench.NewWorkbench()
	source.AddWorkspace("dev", &workbench.WorkspaceState{
		Name:      "dev",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-dev",
			Name:         "code",
			ActivePaneID: "pane-dev",
			Panes: map[string]*workbench.PaneState{
				"pane-dev": {ID: "pane-dev", Title: "shell", TerminalID: "term-restore"},
			},
			Root: workbench.NewLeaf("pane-dev"),
		}},
	})
	data, err := persist.Save(source)
	if err != nil {
		t.Fatalf("persist.Save: %v", err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 17, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-restore": {
				TerminalID: "term-restore",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}}},
			},
		},
	}
	model := New(shared.Config{WorkspaceStatePath: statePath}, workbench.NewWorkbench(), runtime.New(client))

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected init command for restore auto-reattach")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected batch from init restore auto-reattach, got %#v", msg)
	}
	for _, item := range batch {
		if next := item(); next != nil {
			_, nextCmd := model.Update(next)
			if nextCmd != nil {
				if nested := nextCmd(); nested != nil {
					if nestedBatch, ok := nested.(tea.BatchMsg); ok {
						for _, nestedItem := range nestedBatch {
							if final := nestedItem(); final != nil {
								_, _ = model.Update(final)
							}
						}
					} else {
						_, _ = model.Update(nested)
					}
				}
			}
		}
	}

	if len(client.attachCalls) != 1 {
		t.Fatalf("expected one auto-reattach call, got %d", len(client.attachCalls))
	}
	if client.attachCalls[0].terminalID != "term-restore" {
		t.Fatalf("expected reattach terminal term-restore, got %#v", client.attachCalls)
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.ID == "" || pane.TerminalID != "term-restore" {
		t.Fatalf("expected restored pane bound to term-restore, got %#v", pane)
	}
	if model.modalHost.Session != nil {
		t.Fatalf("expected no startup picker during successful restore reattach, got %#v", model.modalHost.Session)
	}
}

func TestModelInitRestoreAutoReattachClearsMissingTerminalBinding(t *testing.T) {
	statePath := t.TempDir() + "/workspace-state.json"
	source := workbench.NewWorkbench()
	source.AddWorkspace("dev", &workbench.WorkspaceState{
		Name:      "dev",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-dev",
			Name:         "code",
			ActivePaneID: "pane-dev",
			Panes: map[string]*workbench.PaneState{
				"pane-dev": {ID: "pane-dev", Title: "shell", TerminalID: "term-missing"},
			},
			Root: workbench.NewLeaf("pane-dev"),
		}},
	})
	data, err := persist.Save(source)
	if err != nil {
		t.Fatalf("persist.Save: %v", err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &recordingBridgeClient{attachErr: errors.New("terminal not found")}
	model := New(shared.Config{WorkspaceStatePath: statePath}, workbench.NewWorkbench(), runtime.New(client))

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected init command for failed restore auto-reattach")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, item := range batch {
			_ = item()
		}
	}
	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected restored pane to exist")
	}
	if pane.ID == "" || pane.TerminalID != "" {
		t.Fatalf("expected missing terminal binding to be cleared, got %#v", pane)
	}
}

func TestModelInitAttachIDBootstrapsAndAttachesTerminal(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-attach": {
				TerminalID: "term-attach",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}},
				},
			},
		},
	}
	model := New(shared.Config{AttachID: "term-attach"}, workbench.NewWorkbench(), runtime.New(client))

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected init command for attach bootstrap")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected attach batch from init, got %#v", msg)
	}
	for _, item := range batch {
		if next := item(); next != nil {
			_, _ = model.Update(next)
		}
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-attach" {
		t.Fatalf("expected active pane attached to term-attach, got %#v", pane)
	}
	if model.modalHost.Session != nil {
		t.Fatalf("expected attach bootstrap not to leave modal open, got %#v", model.modalHost.Session)
	}
}

func TestModelInitAttachIDDefersResizeUntilFirstWindowSize(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-attach": {
				TerminalID: "term-attach",
				Size:       protocol.Size{Cols: 80, Rows: 24},
			},
		},
	}
	model := New(shared.Config{AttachID: "term-attach"}, workbench.NewWorkbench(), runtime.New(client))

	drainCmd(t, model, model.Init(), 20)

	if got := len(client.resizes); got != 0 {
		t.Fatalf("expected no resize before first window size, got %#v", client.resizes)
	}

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	drainCmd(t, model, cmd, 20)

	if got := len(client.resizes); got != 1 {
		t.Fatalf("expected exactly one resize after window size, got %#v", client.resizes)
	}
	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || len(visible.Tabs[visible.ActiveTab].Panes) == 0 {
		t.Fatalf("expected visible pane after window size, got %#v", visible)
	}
	wantRect, ok := paneContentRectForVisible(visible.Tabs[visible.ActiveTab].Panes[0])
	if !ok {
		t.Fatal("expected visible pane content rect")
	}
	if resize := client.resizes[0]; resize.cols != uint16(wantRect.W) || resize.rows != uint16(wantRect.H) {
		t.Fatalf("expected resize to visible pane %#v, got %#v", wantRect, resize)
	}

	_, cmd = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	drainCmd(t, model, cmd, 20)

	if got := len(client.resizes); got != 1 {
		t.Fatalf("expected duplicate window size to avoid extra resize, got %#v", client.resizes)
	}
}

func TestModelAttachResizesPaneInHiddenTab(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-hidden": {
				TerminalID: "term-hidden",
				Size:       protocol.Size{Cols: 80, Rows: 24},
			},
		},
	}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{
			{
				ID:           "tab-1",
				Name:         "1",
				ActivePaneID: "pane-1",
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1"},
				},
				Root: workbench.NewLeaf("pane-1"),
			},
			{
				ID:           "tab-2",
				Name:         "2",
				ActivePaneID: "pane-2",
				Panes: map[string]*workbench.PaneState{
					"pane-2": {ID: "pane-2"},
				},
				Root: workbench.NewLeaf("pane-2"),
			},
		},
	})
	model := New(shared.Config{}, wb, runtime.New(client))
	model.width = 120
	model.height = 40

	drainCmd(t, model, model.attachPaneTerminalCmd("tab-2", "pane-2", "term-hidden"), 20)

	tab := wb.CurrentWorkspace().Tabs[1]
	if pane := tab.Panes["pane-2"]; pane == nil || pane.TerminalID != "term-hidden" {
		t.Fatalf("expected hidden-tab pane bound to term-hidden, got %#v", pane)
	}
	if got := len(client.resizes); got != 1 {
		t.Fatalf("expected one resize for hidden tab attach, got %#v", client.resizes)
	}
	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || len(visible.Tabs) < 2 || len(visible.Tabs[1].Panes) == 0 {
		t.Fatalf("expected visible hidden tab pane projection, got %#v", visible)
	}
	wantRect, ok := paneContentRectForVisible(visible.Tabs[1].Panes[0])
	if !ok {
		t.Fatal("expected hidden tab content rect")
	}
	if resize := client.resizes[0]; resize.cols != uint16(wantRect.W) || resize.rows != uint16(wantRect.H) {
		t.Fatalf("expected hidden tab resize to %#v, got %#v", wantRect, resize)
	}
}

func TestModelBootstrapHelperUsesStartup(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	result, err := bootstrap.Startup(bootstrap.Config{}, model.workbench, model.runtime)
	if err != nil {
		t.Fatalf("startup: %v", err)
	}
	if !result.ShouldOpenPicker {
		t.Fatal("expected bootstrap startup to request picker")
	}
}

type recordingBridgeClient struct {
	resizes            []resizeCall
	inputCalls         []inputCall
	inputStarted       chan inputCall
	inputBlock         <-chan struct{}
	inputErr           error
	listCalls          int
	createResult       *protocol.CreateResult
	attachResult       *protocol.AttachResult
	attachErr          error
	listResult         *protocol.ListResult
	snapshotByTerminal map[string]*protocol.Snapshot
	createCalls        []createCall
	attachCalls        []attachCall
	setTagsCalls       []setTagsCall
	setMetadataCalls   []setMetadataCall
	killCalls          []string
	restartCalls       []string
	sessionSnapshot    *protocol.SessionSnapshot
	sessionView        *protocol.ViewInfo
	getSessionErr      error
	replaceSessionErr  error
	createSessionCalls []protocol.CreateSessionParams
	getSessionCalls    []string
	attachSessionCalls []protocol.AttachSessionParams
	replaceCalls       []protocol.ReplaceSessionParams
	viewUpdateCalls    []protocol.UpdateSessionViewParams
	acquireLeaseCalls  []protocol.AcquireSessionLeaseParams
	releaseLeaseCalls  []protocol.ReleaseSessionLeaseParams
}

type resizeCall struct {
	channel uint16
	cols    uint16
	rows    uint16
}

type inputCall struct {
	channel uint16
	data    []byte
}

type createCall struct {
	params protocol.CreateParams
}

type setTagsCall struct {
	terminalID string
	tags       map[string]string
}

type setMetadataCall struct {
	terminalID string
	name       string
	tags       map[string]string
}

type attachCall struct {
	terminalID string
	mode       string
}

var _ bridge.Client = (*recordingBridgeClient)(nil)

func (c *recordingBridgeClient) Close() error { return nil }

func (c *recordingBridgeClient) Create(_ context.Context, params protocol.CreateParams) (*protocol.CreateResult, error) {
	cloned := params
	cloned.Command = append([]string(nil), params.Command...)
	cloned.Tags = cloneTags(params.Tags)
	c.createCalls = append(c.createCalls, createCall{params: cloned})
	return c.createResult, nil
}

func (c *recordingBridgeClient) SetTags(_ context.Context, terminalID string, tags map[string]string) error {
	c.setTagsCalls = append(c.setTagsCalls, setTagsCall{terminalID: terminalID, tags: cloneTags(tags)})
	return nil
}

func (c *recordingBridgeClient) SetMetadata(_ context.Context, terminalID string, name string, tags map[string]string) error {
	c.setMetadataCalls = append(c.setMetadataCalls, setMetadataCall{
		terminalID: terminalID,
		name:       name,
		tags:       cloneTags(tags),
	})
	return nil
}

func (c *recordingBridgeClient) List(context.Context) (*protocol.ListResult, error) {
	c.listCalls++
	if c.listResult == nil {
		return &protocol.ListResult{}, nil
	}
	return c.listResult, nil
}

func (c *recordingBridgeClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return nil, nil
}

func (c *recordingBridgeClient) Attach(_ context.Context, terminalID, mode string) (*protocol.AttachResult, error) {
	c.attachCalls = append(c.attachCalls, attachCall{terminalID: terminalID, mode: mode})
	if c.attachErr != nil {
		return nil, c.attachErr
	}
	return c.attachResult, nil
}

func (c *recordingBridgeClient) Snapshot(_ context.Context, terminalID string, _ int, _ int) (*protocol.Snapshot, error) {
	if c.snapshotByTerminal == nil {
		return nil, nil
	}
	return c.snapshotByTerminal[terminalID], nil
}

func (c *recordingBridgeClient) Input(_ context.Context, channel uint16, data []byte) error {
	call := inputCall{channel: channel, data: append([]byte(nil), data...)}
	c.inputCalls = append(c.inputCalls, call)
	if c.inputStarted != nil {
		select {
		case c.inputStarted <- call:
		default:
		}
	}
	if c.inputBlock != nil {
		<-c.inputBlock
	}
	return c.inputErr
}

func (c *recordingBridgeClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	c.resizes = append(c.resizes, resizeCall{channel: channel, cols: cols, rows: rows})
	return nil
}

func (c *recordingBridgeClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}

func (c *recordingBridgeClient) Kill(_ context.Context, terminalID string) error {
	c.killCalls = append(c.killCalls, terminalID)
	return nil
}

func (c *recordingBridgeClient) Restart(_ context.Context, terminalID string) error {
	c.restartCalls = append(c.restartCalls, terminalID)
	return nil
}

func (c *recordingBridgeClient) CreateSession(_ context.Context, params protocol.CreateSessionParams) (*protocol.SessionSnapshot, error) {
	c.createSessionCalls = append(c.createSessionCalls, params)
	if c.sessionSnapshot == nil {
		return &protocol.SessionSnapshot{}, nil
	}
	return c.sessionSnapshot, nil
}

func (c *recordingBridgeClient) ListSessions(context.Context) (*protocol.ListSessionsResult, error) {
	return &protocol.ListSessionsResult{}, nil
}

func (c *recordingBridgeClient) GetSession(_ context.Context, sessionID string) (*protocol.SessionSnapshot, error) {
	c.getSessionCalls = append(c.getSessionCalls, sessionID)
	if c.getSessionErr != nil {
		return nil, c.getSessionErr
	}
	if c.sessionSnapshot == nil {
		return &protocol.SessionSnapshot{}, nil
	}
	return c.sessionSnapshot, nil
}

func (c *recordingBridgeClient) AttachSession(_ context.Context, params protocol.AttachSessionParams) (*protocol.SessionSnapshot, error) {
	c.attachSessionCalls = append(c.attachSessionCalls, params)
	if c.sessionSnapshot == nil {
		return &protocol.SessionSnapshot{}, nil
	}
	return c.sessionSnapshot, nil
}

func (c *recordingBridgeClient) DetachSession(context.Context, string, string) error {
	return nil
}

func (c *recordingBridgeClient) ApplySession(context.Context, protocol.ApplySessionParams) (*protocol.SessionSnapshot, error) {
	if c.sessionSnapshot == nil {
		return &protocol.SessionSnapshot{}, nil
	}
	return c.sessionSnapshot, nil
}

func (c *recordingBridgeClient) ReplaceSession(_ context.Context, params protocol.ReplaceSessionParams) (*protocol.SessionSnapshot, error) {
	c.replaceCalls = append(c.replaceCalls, params)
	if c.replaceSessionErr != nil {
		return nil, c.replaceSessionErr
	}
	if c.sessionSnapshot == nil {
		return &protocol.SessionSnapshot{}, nil
	}
	return c.sessionSnapshot, nil
}

func (c *recordingBridgeClient) UpdateSessionView(_ context.Context, params protocol.UpdateSessionViewParams) (*protocol.ViewInfo, error) {
	c.viewUpdateCalls = append(c.viewUpdateCalls, params)
	if c.sessionView == nil {
		return &protocol.ViewInfo{ViewID: params.ViewID, SessionID: params.SessionID}, nil
	}
	return c.sessionView, nil
}

func (c *recordingBridgeClient) AcquireSessionLease(_ context.Context, params protocol.AcquireSessionLeaseParams) (*protocol.LeaseInfo, error) {
	c.acquireLeaseCalls = append(c.acquireLeaseCalls, params)
	return &protocol.LeaseInfo{
		TerminalID: params.TerminalID,
		SessionID:  params.SessionID,
		ViewID:     params.ViewID,
		PaneID:     params.PaneID,
	}, nil
}

func (c *recordingBridgeClient) ReleaseSessionLease(_ context.Context, params protocol.ReleaseSessionLeaseParams) error {
	c.releaseLeaseCalls = append(c.releaseLeaseCalls, params)
	return nil
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}
