package app

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/terminalmeta"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// ─── Test helpers ───────────────────────────────────────────────────────────

type modelOpts struct {
	client         *recordingBridgeClient
	workspaces     map[string]*workbench.WorkspaceState // defaults to single "main" workspace
	statePath      string
	width, height  int
	attachTerminal string // if set, register terminal in runtime with this ID
}

func setupModel(t *testing.T, opts modelOpts) *Model {
	t.Helper()
	if opts.width == 0 {
		opts.width = 120
	}
	if opts.height == 0 {
		opts.height = 40
	}
	if opts.client == nil {
		opts.client = &recordingBridgeClient{
			attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
			snapshotByTerminal: map[string]*protocol.Snapshot{},
		}
	}
	rt := runtime.New(opts.client)
	wb := workbench.NewWorkbench()

	if opts.workspaces != nil {
		for name, ws := range opts.workspaces {
			wb.AddWorkspace(name, ws)
		}
	} else {
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
		rt.Registry().GetOrCreate("term-1").Name = "shell"
		rt.Registry().Get("term-1").State = "running"
		rt.Registry().Get("term-1").Channel = 1
		binding := rt.BindPane("pane-1")
		binding.Channel = 1
		binding.Connected = true
	}

	if opts.attachTerminal != "" {
		tr := rt.Registry().GetOrCreate(opts.attachTerminal)
		tr.Name = opts.attachTerminal
		tr.State = "running"
		tr.Channel = 2
		opts.client.snapshotByTerminal[opts.attachTerminal] = &protocol.Snapshot{
			TerminalID: opts.attachTerminal,
			Size:       protocol.Size{Cols: 80, Rows: 24},
			Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
		}
	}

	model := New(shared.Config{WorkspaceStatePath: opts.statePath}, wb, rt)
	model.width = opts.width
	model.height = opts.height
	return model
}

// drainMsg recursively processes a tea.Msg, handling nested BatchMsg and
// skipping prefixTimeoutMsg to avoid blocking on tea.Tick(1.5s).
func drainMsg(t *testing.T, model *Model, msg tea.Msg, depth int) {
	t.Helper()
	if msg == nil || depth <= 0 {
		return
	}
	if _, ok := msg.(prefixTimeoutMsg); ok {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, item := range batch {
			if item == nil {
				continue
			}
			next := item()
			drainMsg(t, model, next, depth-1)
		}
		return
	}
	_, nextCmd := model.Update(msg)
	drainCmd(t, model, nextCmd, depth-1)
}

// drainCmd executes a tea.Cmd and recursively processes results.
// Skips slow async cmds such as prefix timeout ticks so feature tests stay fast.
func drainCmd(t *testing.T, model *Model, cmd tea.Cmd, maxDepth int) {
	t.Helper()
	if cmd == nil || maxDepth <= 0 {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()
	select {
	case msg := <-done:
		drainMsg(t, model, msg, maxDepth)
	case <-time.After(20 * time.Millisecond):
		return
	}
}

// dispatchAction sends a SemanticAction through Update and drains the cmd chain.
func dispatchAction(t *testing.T, model *Model, action input.SemanticAction) {
	t.Helper()
	_, cmd := model.Update(action)
	drainCmd(t, model, cmd, 20)
}

// dispatchKey sends a tea.KeyMsg through Update. If the result is a SemanticAction
// (as returned by the input router), it feeds it back through Update and drains.
func dispatchKey(t *testing.T, model *Model, msg tea.KeyMsg) {
	t.Helper()
	_, cmd := model.Update(msg)
	if cmd == nil {
		return
	}
	result := cmd()
	if result == nil {
		return
	}
	drainMsg(t, model, result, 20)
}

func assertMode(t *testing.T, model *Model, expected input.ModeKind) {
	t.Helper()
	if got := model.input.Mode().Kind; got != expected {
		t.Fatalf("expected mode %q, got %q", expected, got)
	}
}

func assertPaneCount(t *testing.T, model *Model, expected int) {
	t.Helper()
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("no current tab")
	}
	if got := len(tab.Panes); got != expected {
		t.Fatalf("expected %d panes, got %d", expected, got)
	}
}

func assertViewContains(t *testing.T, model *Model, substrings ...string) {
	t.Helper()
	view := xansi.Strip(model.View())
	for _, s := range substrings {
		if !strings.Contains(view, s) {
			t.Fatalf("view missing %q:\n%s", s, view)
		}
	}
}

func assertTabCount(t *testing.T, model *Model, expected int) {
	t.Helper()
	ws := model.workbench.CurrentWorkspace()
	if ws == nil {
		t.Fatal("no current workspace")
	}
	if got := len(ws.Tabs); got != expected {
		t.Fatalf("expected %d tabs, got %d", expected, got)
	}
}

func assertActivePane(t *testing.T, model *Model, expectedID string) {
	t.Helper()
	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("no active pane")
	}
	if pane.ID != expectedID {
		t.Fatalf("expected active pane %q, got %q", expectedID, pane.ID)
	}
}

func createWorkspaceViaPrompt(t *testing.T, model *Model, name string) {
	t.Helper()
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCreateWorkspace})
	assertMode(t, model, input.ModePrompt)
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "rename-workspace" || model.modalHost.Prompt.Original != "" {
		t.Fatalf("expected create-workspace prompt state, got %#v", model.modalHost.Prompt)
	}
	model.modalHost.Prompt.Value = name
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})
	assertMode(t, model, input.ModeNormal)
}

func findFloating(tab *workbench.TabState, paneID string) *workbench.FloatingState {
	for _, f := range tab.Floating {
		if f.PaneID == paneID {
			return f
		}
	}
	return nil
}

func ctrlKey(k tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: k}
}

func runeKeyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// setupTwoPaneModel creates a model with two panes split vertically.
func setupTwoPaneModel(t *testing.T) *Model {
	t.Helper()
	wb := workbench.NewWorkbench()
	root := &workbench.LayoutNode{
		Direction: workbench.SplitVertical,
		Ratio:     0.5,
		First:     workbench.NewLeaf("pane-1"),
		Second:    workbench.NewLeaf("pane-2"),
	}
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "logs", TerminalID: "term-2"},
			},
			Root: root,
		}},
	})
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	rt := runtime.New(client)
	rt.Registry().GetOrCreate("term-1").Name = "shell"
	rt.Registry().Get("term-1").State = "running"
	rt.Registry().Get("term-1").Channel = 1
	rt.Registry().GetOrCreate("term-2").Name = "logs"
	rt.Registry().Get("term-2").State = "running"
	rt.Registry().Get("term-2").Channel = 2
	rt.BindPane("pane-1").Channel = 1
	rt.BindPane("pane-1").Connected = true
	rt.BindPane("pane-2").Channel = 2
	rt.BindPane("pane-2").Connected = true

	model := New(shared.Config{}, wb, rt)
	model.width = 120
	model.height = 40
	return model
}

// ─── Group 1: Pane Operations ───────────────────────────────────────────────

func TestFeaturePaneSplitVertical(t *testing.T) {
	model := setupModel(t, modelOpts{})
	assertPaneCount(t, model, 1)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSplitPane, PaneID: "pane-1"})

	assertPaneCount(t, model, 2)
	// Split opens picker for the new pane
	assertMode(t, model, input.ModePicker)
}

func TestFeaturePaneSplitHorizontal(t *testing.T) {
	model := setupModel(t, modelOpts{})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSplitPaneHorizontal, PaneID: "pane-1"})

	assertPaneCount(t, model, 2)
	assertMode(t, model, input.ModePicker)
}

func TestFeaturePaneSplitRefreshesPickerFromDaemon(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-live", Name: "live-shell", State: "running"},
			},
		},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})
	model.modalHost.Picker = &modal.PickerState{
		Items:    []modal.PickerItem{{TerminalID: "term-stale", Name: "stale", State: "running"}},
		Filtered: []modal.PickerItem{{TerminalID: "term-stale", Name: "stale", State: "running"}},
		Selected: 0,
		Query:    "stale",
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSplitPane, PaneID: "pane-1"})

	assertPaneCount(t, model, 2)
	assertMode(t, model, input.ModePicker)
	if client.listCalls == 0 {
		t.Fatal("expected split-opened picker to query daemon")
	}
	if model.modalHost.Picker == nil {
		t.Fatal("expected picker state")
	}
	if model.modalHost.Picker.Query != "" {
		t.Fatalf("expected picker query cleared on open, got %q", model.modalHost.Picker.Query)
	}
	items := model.modalHost.Picker.VisibleItems()
	if len(items) != 2 {
		t.Fatalf("expected live terminal plus create row, got %#v", items)
	}
	if !items[0].CreateNew {
		t.Fatalf("expected create row first, got %#v", items)
	}
	if items[1].TerminalID != "term-live" {
		t.Fatalf("expected picker to use daemon terminal list, got %#v", items)
	}
}

func TestFeaturePaneFocusDirections(t *testing.T) {
	model := setupTwoPaneModel(t)
	assertActivePane(t, model, "pane-1")

	// Focus right should move to pane-2
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionFocusPaneRight, PaneID: "pane-1"})
	assertActivePane(t, model, "pane-2")

	// Focus left should move back to pane-1
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionFocusPaneLeft, PaneID: "pane-2"})
	assertActivePane(t, model, "pane-1")
}

func TestFeaturePaneClose(t *testing.T) {
	model := setupTwoPaneModel(t)
	assertPaneCount(t, model, 2)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionClosePane, PaneID: "pane-2"})

	assertPaneCount(t, model, 1)
	// Remaining pane should become active
	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected an active pane after close")
	}
}

func TestFeaturePaneCloseCleansRuntimeBindingState(t *testing.T) {
	model := setupTwoPaneModel(t)
	term := model.runtime.Registry().Get("term-2")
	if term == nil {
		t.Fatal("expected term-2 runtime")
	}
	term.OwnerPaneID = "pane-2"
	term.BoundPaneIDs = []string{"pane-2"}
	binding := model.runtime.Binding("pane-2")
	if binding == nil {
		t.Fatal("expected pane-2 binding")
	}
	binding.Role = runtime.BindingRoleOwner

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionClosePane, PaneID: "pane-2"})

	if got := model.runtime.Binding("pane-2"); got != nil {
		t.Fatalf("expected pane-2 runtime binding removed, got %#v", got)
	}
	term = model.runtime.Registry().Get("term-2")
	if term == nil {
		t.Fatal("expected term-2 runtime after pane close")
	}
	if term.OwnerPaneID != "" {
		t.Fatalf("expected term-2 owner cleared, got %q", term.OwnerPaneID)
	}
	if len(term.BoundPaneIDs) != 0 {
		t.Fatalf("expected term-2 bound panes cleared, got %#v", term.BoundPaneIDs)
	}
}

func TestFeaturePaneDetach(t *testing.T) {
	model := setupModel(t, modelOpts{})
	term := model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected term-1 runtime")
	}
	term.OwnerPaneID = "pane-1"
	term.BoundPaneIDs = []string{"pane-1"}
	binding := model.runtime.Binding("pane-1")
	if binding == nil {
		t.Fatal("expected pane-1 binding")
	}
	binding.Role = runtime.BindingRoleOwner

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionDetachPane, PaneID: "pane-1"})

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane after detach")
	}
	if pane.TerminalID != "" {
		t.Fatalf("expected pane detached, got terminal %q", pane.TerminalID)
	}
	if got := model.runtime.Binding("pane-1"); got != nil {
		t.Fatalf("expected pane binding cleared, got %#v", got)
	}
	term = model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected terminal runtime retained after detach")
	}
	if term.OwnerPaneID != "" || len(term.BoundPaneIDs) != 0 {
		t.Fatalf("expected detached terminal runtime cleaned up, got owner=%q bound=%#v", term.OwnerPaneID, term.BoundPaneIDs)
	}
	assertMode(t, model, input.ModeNormal)
}

func TestFeaturePaneSwap(t *testing.T) {
	t.Skip("ActionSwapPaneLeft not yet implemented")
	model := setupTwoPaneModel(t)
	assertActivePane(t, model, "pane-1")

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSwapPaneLeft, PaneID: "pane-1"})

	// After swap, pane should still exist and be active
	tab := model.workbench.CurrentTab()
	if tab == nil || len(tab.Panes) != 2 {
		t.Fatal("expected 2 panes after swap")
	}
}

func TestFeaturePaneZoom(t *testing.T) {
	model := setupTwoPaneModel(t)
	tab := model.workbench.CurrentTab()
	if tab.ZoomedPaneID != "" {
		t.Fatal("expected no zoom initially")
	}

	// Must be in display mode for zoom action to reach orchestrator
	model.input.SetMode(input.ModeState{Kind: input.ModeDisplay})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionZoomPane, PaneID: "pane-1"})

	tab = model.workbench.CurrentTab()
	if tab.ZoomedPaneID != "pane-1" {
		t.Fatalf("expected zoomed pane-1, got %q", tab.ZoomedPaneID)
	}
	assertMode(t, model, input.ModeNormal)

	// Toggle zoom off
	model.input.SetMode(input.ModeState{Kind: input.ModeDisplay})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionZoomPane, PaneID: "pane-1"})

	tab = model.workbench.CurrentTab()
	if tab.ZoomedPaneID != "" {
		t.Fatalf("expected zoom cleared, got %q", tab.ZoomedPaneID)
	}
	assertMode(t, model, input.ModeNormal)
}

func TestFeaturePaneReconnect(t *testing.T) {
	model := setupModel(t, modelOpts{})
	term := model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected term-1 runtime")
	}
	term.OwnerPaneID = "pane-1"
	term.BoundPaneIDs = []string{"pane-1"}
	binding := model.runtime.Binding("pane-1")
	if binding == nil {
		t.Fatal("expected pane-1 binding")
	}
	binding.Role = runtime.BindingRoleOwner

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionReconnectPane, PaneID: "pane-1"})

	// Reconnect should detach and open picker
	pane := model.workbench.ActivePane()
	if pane.TerminalID != "" {
		t.Fatalf("expected pane detached after reconnect, got %q", pane.TerminalID)
	}
	if got := model.runtime.Binding("pane-1"); got != nil {
		t.Fatalf("expected pane binding cleared during reconnect, got %#v", got)
	}
	term = model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected terminal runtime retained after reconnect")
	}
	if term.OwnerPaneID != "" || len(term.BoundPaneIDs) != 0 {
		t.Fatalf("expected previous terminal runtime cleaned during reconnect, got owner=%q bound=%#v", term.OwnerPaneID, term.BoundPaneIDs)
	}
	assertMode(t, model, input.ModePicker)
}

func TestFeatureExitedPaneReconnectKeepsBindingAndOpensPicker(t *testing.T) {
	model := setupModel(t, modelOpts{})
	term := model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected term-1 runtime")
	}
	term.State = "exited"
	term.OwnerPaneID = "pane-1"
	term.BoundPaneIDs = []string{"pane-1"}
	binding := model.runtime.Binding("pane-1")
	if binding == nil {
		t.Fatal("expected pane-1 binding")
	}
	binding.Role = runtime.BindingRoleOwner

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionReconnectPane, PaneID: "pane-1"})

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" {
		t.Fatalf("expected exited pane to stay bound during picker reopen, got %#v", pane)
	}
	if got := model.runtime.Binding("pane-1"); got == nil {
		t.Fatal("expected pane binding retained for exited reconnect")
	}
	term = model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected terminal runtime retained after exited reconnect")
	}
	if term.OwnerPaneID != "pane-1" || len(term.BoundPaneIDs) != 1 || term.BoundPaneIDs[0] != "pane-1" {
		t.Fatalf("expected exited terminal binding preserved, got owner=%q bound=%#v", term.OwnerPaneID, term.BoundPaneIDs)
	}
	assertMode(t, model, input.ModePicker)
}

func TestFeatureBecomeOwnerPromotesActivePane(t *testing.T) {
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
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}

	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil {
		terminal = model.runtime.Registry().GetOrCreate("term-1")
	}
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	_ = model.workbench.FocusPane(tab.ID, "pane-2")
	model.input.SetMode(input.ModeState{Kind: input.ModePane})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionBecomeOwner, PaneID: "pane-2"})

	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 to become owner, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower {
		t.Fatalf("expected pane-1 demoted to follower, got %q", ownerBinding.Role)
	}
	if followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected pane-2 promoted to owner, got %q", followerBinding.Role)
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected resize after owner takeover, got %#v", client.resizes)
	}
	if client.resizes[0].channel != 2 {
		t.Fatalf("expected pane-2 channel to drive resize, got %#v", client.resizes[0])
	}
	assertViewContains(t, model, "follow", "owner")
}

func TestFeatureSessionBecomeOwnerAcquiresLeaseExplicitly(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
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
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "main", ViewID: "view-remote", PaneID: "pane-x"},
	}

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	_ = model.workbench.FocusPane("tab-1", "pane-2")
	model.input.SetMode(input.ModeState{Kind: input.ModePane})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionBecomeOwner, PaneID: "pane-2"})

	if len(client.acquireLeaseCalls) != 1 {
		t.Fatalf("expected one explicit lease acquire, got %#v", client.acquireLeaseCalls)
	}
	if got := client.acquireLeaseCalls[0]; got.ViewID != "view-local" || got.PaneID != "pane-2" || got.TerminalID != "term-1" {
		t.Fatalf("unexpected lease acquire params: %#v", got)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 promoted after explicit lease acquire, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower || followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected lease acquire to update local roles, owner=%#v follower=%#v", ownerBinding, followerBinding)
	}
	if len(client.resizes) != 1 || client.resizes[0].channel != 2 {
		t.Fatalf("expected resize from pane-2 channel after lease acquire, got %#v", client.resizes)
	}
}

func TestFeatureZoomOnFollowerPaneTakesOwnershipAndResizes(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
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
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlP))
	dispatchKey(t, model, runeKeyMsg('z'))

	tab := model.workbench.CurrentTab()
	if tab == nil || tab.ZoomedPaneID != "pane-2" {
		t.Fatalf("expected pane-2 zoomed, got %#v", tab)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected zoom to promote pane-2 owner, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower || followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected bindings to swap owner/follower, owner=%#v follower=%#v", ownerBinding, followerBinding)
	}
	if len(client.resizes) == 0 {
		t.Fatal("expected resize after follower zoom takeover")
	}
	last := client.resizes[len(client.resizes)-1]
	if last.channel != 2 || last.cols != 120 || last.rows != 40 {
		t.Fatalf("expected follower channel fullscreen resize after zoom, got %#v", last)
	}
}

func TestFeaturePaneCloseAndKill(t *testing.T) {
	model := setupModel(t, modelOpts{})
	assertPaneCount(t, model, 1)
	term := model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected term-1 runtime")
	}
	term.OwnerPaneID = "pane-1"
	term.BoundPaneIDs = []string{"pane-1"}
	binding := model.runtime.Binding("pane-1")
	if binding == nil {
		t.Fatal("expected pane-1 binding")
	}
	binding.Role = runtime.BindingRoleOwner

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionClosePaneKill, PaneID: "pane-1"})

	// Pane should be closed and terminal kill effect should have been issued
	client := model.runtime.Client().(*recordingBridgeClient)
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-1" {
		t.Fatalf("expected kill call for term-1, got %v", client.killCalls)
	}
	if got := model.runtime.Binding("pane-1"); got != nil {
		t.Fatalf("expected runtime binding removed after close+kill, got %#v", got)
	}
	if model.workbench.CurrentTab() != nil && len(model.workbench.CurrentTab().Panes) != 0 {
		t.Fatalf("expected pane removed after close+kill, got %#v", model.workbench.CurrentTab().Panes)
	}
}

func TestFeatureTabSwitchPromotesSharedTerminalOwnerOnLocalTabSwitch(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{
					{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-1"),
					},
					{
						ID:           "tab-2",
						Name:         "tab 2",
						ActivePaneID: "pane-2",
						Panes: map[string]*workbench.PaneState{
							"pane-2": {ID: "pane-2", Title: "shared", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-2"),
					},
				},
			},
		},
	})

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Name = "shared"
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 118, Rows: 36},
	}

	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	cmd := model.switchTabByIndexMouse(1)
	drainCmd(t, model, cmd, 20)

	tab := model.workbench.CurrentTab()
	if tab == nil || tab.ID != "tab-2" {
		t.Fatalf("expected tab-2 active after switch, got %#v", tab)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 promoted to owner on local tab switch, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower {
		t.Fatalf("expected pane-1 demoted after tab switch, got %q", ownerBinding.Role)
	}
	if followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected pane-2 promoted after tab switch, got %q", followerBinding.Role)
	}

	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatalf("expected visible active tab after switch, got %#v", visible)
	}
	var target *workbench.VisiblePane
	for i := range visible.Tabs[visible.ActiveTab].Panes {
		pane := &visible.Tabs[visible.ActiveTab].Panes[i]
		if pane.ID == "pane-2" {
			target = pane
			break
		}
	}
	if target == nil {
		t.Fatalf("expected visible pane-2 after switch, got %#v", visible.Tabs[visible.ActiveTab].Panes)
	}
	if len(client.resizes) == 0 {
		t.Fatalf("expected shared local tab switch to issue a resize, got %#v", client.resizes)
	}
	last := client.resizes[len(client.resizes)-1]
	if last.channel != 2 {
		t.Fatalf("expected tab switch resize on pane-2 channel, got %#v", last)
	}
}

func TestFeatureTabSwitchPromotesSharedTerminalOwnerWhenGeometryDiffers(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{
					{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-1"),
					},
					{
						ID:           "tab-2",
						Name:         "tab 2",
						ActivePaneID: "pane-2",
						Panes: map[string]*workbench.PaneState{
							"pane-2": {ID: "pane-2", Title: "shared", TerminalID: "term-1"},
							"pane-3": {ID: "pane-3", Title: "side"},
						},
						Root: &workbench.LayoutNode{
							Direction: workbench.SplitVertical,
							Ratio:     0.5,
							First:     workbench.NewLeaf("pane-2"),
							Second:    workbench.NewLeaf("pane-3"),
						},
					},
				},
			},
		},
	})

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Name = "shared"
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 118, Rows: 36},
	}

	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	cmd := model.switchTabByIndexMouse(1)
	drainCmd(t, model, cmd, 20)

	tab := model.workbench.CurrentTab()
	if tab == nil || tab.ID != "tab-2" {
		t.Fatalf("expected tab-2 active after switch, got %#v", tab)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 promoted to owner after geometry-changing tab switch, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower || followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected roles swapped after tab switch, owner=%#v follower=%#v", ownerBinding, followerBinding)
	}
	if len(client.resizes) == 0 {
		t.Fatal("expected resize after owner promotion on tab switch")
	}
	last := client.resizes[len(client.resizes)-1]
	if last.channel != 2 || last.cols >= 118 {
		t.Fatalf("expected resized pane-2 geometry on follower channel, got %#v", last)
	}
}

func TestFeatureTabSwitchPromotesSharedTerminalOwnerWhileBootstrapPendingUpdatesSnapshotSize(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{
					{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-1"),
					},
					{
						ID:           "tab-2",
						Name:         "tab 2",
						ActivePaneID: "pane-2",
						Panes: map[string]*workbench.PaneState{
							"pane-2": {ID: "pane-2", Title: "shared", TerminalID: "term-1"},
							"pane-3": {ID: "pane-3", Title: "side"},
						},
						Root: &workbench.LayoutNode{
							Direction: workbench.SplitVertical,
							Ratio:     0.5,
							First:     workbench.NewLeaf("pane-2"),
							Second:    workbench.NewLeaf("pane-3"),
						},
					},
				},
			},
		},
	})

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Name = "shared"
	terminal.State = "running"
	terminal.Channel = 1
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.BootstrapPending = true
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 118, Rows: 36},
	}

	ownerBinding := model.runtime.BindPane("pane-1")
	ownerBinding.Channel = 1
	ownerBinding.Connected = true
	ownerBinding.Role = runtime.BindingRoleOwner

	followerBinding := model.runtime.BindPane("pane-2")
	followerBinding.Channel = 2
	followerBinding.Connected = true
	followerBinding.Role = runtime.BindingRoleFollower

	cmd := model.switchTabByIndexMouse(1)
	drainCmd(t, model, cmd, 20)

	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 promoted to owner after tab switch, got %q", terminal.OwnerPaneID)
	}
	if ownerBinding.Role != runtime.BindingRoleFollower || followerBinding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected roles swapped after tab switch, owner=%#v follower=%#v", ownerBinding, followerBinding)
	}
	if len(client.resizes) == 0 {
		t.Fatal("expected resize after owner promotion on tab switch")
	}
	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols >= 118 {
		t.Fatalf("expected bootstrap-pending resize to refresh snapshot size, got %#v", terminal.Snapshot)
	}
	if terminal.PendingOwnerResize {
		t.Fatalf("expected pending owner resize cleared after bootstrap-pending resize, got %#v", terminal)
	}
}

func TestFeatureSessionTabSwitchDoesNotImplicitlyAcquireLease(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{
					{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-1"),
					},
					{
						ID:           "tab-2",
						Name:         "tab 2",
						ActivePaneID: "pane-2",
						Panes: map[string]*workbench.PaneState{
							"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-2"),
					},
				},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "main", ViewID: "view-remote", PaneID: "pane-remote"},
	}

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 118, Rows: 36}}

	binding1 := model.runtime.BindPane("pane-1")
	binding1.Channel = 1
	binding1.Connected = true
	binding2 := model.runtime.BindPane("pane-2")
	binding2.Channel = 2
	binding2.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	cmd := model.switchTabByIndexMouse(1)
	drainCmd(t, model, cmd, 20)

	if terminal.OwnerPaneID != "pane-remote" {
		t.Fatalf("expected remote lease to preserve global owner, got owner=%q", terminal.OwnerPaneID)
	}
	if len(client.acquireLeaseCalls) != 0 {
		t.Fatalf("expected tab switch not to acquire lease implicitly, got %#v", client.acquireLeaseCalls)
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected follower-only tab switch not to resize PTY, got %#v", client.resizes)
	}
	if binding2.Role != runtime.BindingRoleFollower {
		t.Fatalf("expected pane-2 to stay follower after tab switch, got %#v", binding2)
	}
}

// ─── Group 2: Tab Operations ────────────────────────────────────────────────

func TestFeatureTabCreate(t *testing.T) {
	model := setupModel(t, modelOpts{})
	assertTabCount(t, model, 1)

	// Must be in tab mode for CreateTab to reach orchestrator
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCreateTab})

	assertTabCount(t, model, 2)
	// New tab opens picker for its single pane
	assertMode(t, model, input.ModePicker)
}

// createSecondTab enters tab mode and creates a second tab, then cancels the picker.
func createSecondTab(t *testing.T, model *Model) {
	t.Helper()
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCreateTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})
}

func TestFeatureTabClose(t *testing.T) {
	model := setupModel(t, modelOpts{})
	createSecondTab(t, model)
	assertTabCount(t, model, 2)

	// Close the active tab
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCloseTab})

	assertTabCount(t, model, 1)
}

func TestFeatureTabSwitchNextPrev(t *testing.T) {
	model := setupModel(t, modelOpts{})
	createSecondTab(t, model)
	assertTabCount(t, model, 2)

	ws := model.workbench.CurrentWorkspace()
	initialTab := ws.ActiveTab

	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPrevTab})

	ws = model.workbench.CurrentWorkspace()
	if ws.ActiveTab == initialTab {
		t.Fatal("expected tab to change after prev")
	}

	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionNextTab})

	ws = model.workbench.CurrentWorkspace()
	if ws.ActiveTab != initialTab {
		t.Fatal("expected tab to return after next")
	}
}

func TestFeatureTabJumpByNumber(t *testing.T) {
	model := setupModel(t, modelOpts{})
	createSecondTab(t, model)
	assertTabCount(t, model, 2)

	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionJumpTab, Text: "1"})

	ws := model.workbench.CurrentWorkspace()
	if ws.ActiveTab != 0 {
		t.Fatalf("expected tab 0 after jump to 1, got %d", ws.ActiveTab)
	}
}

func TestFeatureTabRename(t *testing.T) {
	model := setupModel(t, modelOpts{})

	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionRenameTab})

	assertMode(t, model, input.ModePrompt)
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "rename-tab" {
		t.Fatalf("expected rename-tab prompt, got %#v", model.modalHost.Prompt)
	}

	// Type new name and submit
	model.modalHost.Prompt.Value = "my-tab"
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	assertMode(t, model, input.ModeNormal)
	tab := model.workbench.CurrentTab()
	if tab.Name != "my-tab" {
		t.Fatalf("expected tab name my-tab, got %q", tab.Name)
	}
}

func TestFeatureTabKill(t *testing.T) {
	model := setupModel(t, modelOpts{})
	createSecondTab(t, model)
	assertTabCount(t, model, 2)

	// Switch to first tab and kill it
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionJumpTab, Text: "1"})
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionKillTab})

	assertTabCount(t, model, 1)
	client := model.runtime.Client().(*recordingBridgeClient)
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-1" {
		t.Fatalf("expected kill call for pane's terminal, got %v", client.killCalls)
	}
}

// ─── Group 3: Workspace Operations ──────────────────────────────────────────

func TestFeatureWorkspaceCreate(t *testing.T) {
	model := setupModel(t, modelOpts{})
	initialWs := model.workbench.CurrentWorkspace().Name

	createWorkspaceViaPrompt(t, model, "dev")

	ws := model.workbench.CurrentWorkspace()
	if ws.Name == initialWs {
		t.Fatal("expected new workspace after create")
	}
	names := model.workbench.ListWorkspaces()
	if len(names) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(names))
	}
}

func TestFeatureWorkspaceCreateRejectsEmptyName(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCreateWorkspace})

	assertMode(t, model, input.ModePrompt)
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "rename-workspace" || model.modalHost.Prompt.Original != "" {
		t.Fatalf("expected create-workspace prompt state, got %#v", model.modalHost.Prompt)
	}
	model.modalHost.Prompt.Value = "   "
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	assertMode(t, model, input.ModePrompt)
	if ws := model.workbench.CurrentWorkspace(); ws == nil || ws.Name != "main" {
		t.Fatalf("expected current workspace to remain main, got %#v", ws)
	}
	if got := len(model.workbench.ListWorkspaces()); got != 1 {
		t.Fatalf("expected no new workspace on empty name, got %d", got)
	}
}

func TestFeatureWorkspaceSwitch(t *testing.T) {
	model := setupModel(t, modelOpts{})
	createWorkspaceViaPrompt(t, model, "dev")
	newWs := model.workbench.CurrentWorkspace().Name

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSwitchWorkspace, Text: "main"})

	ws := model.workbench.CurrentWorkspace()
	if ws.Name != "main" {
		t.Fatalf("expected main workspace, got %q", ws.Name)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSwitchWorkspace, Text: newWs})
	ws = model.workbench.CurrentWorkspace()
	if ws.Name != newWs {
		t.Fatalf("expected %q workspace, got %q", newWs, ws.Name)
	}
}

func TestFeatureWorkspaceDelete(t *testing.T) {
	model := setupModel(t, modelOpts{})
	createWorkspaceViaPrompt(t, model, "dev")
	if len(model.workbench.ListWorkspaces()) != 2 {
		t.Fatal("expected 2 workspaces before delete")
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionDeleteWorkspace})

	if len(model.workbench.ListWorkspaces()) != 1 {
		t.Fatalf("expected 1 workspace after delete, got %d", len(model.workbench.ListWorkspaces()))
	}
}

func TestFeatureWorkspaceRename(t *testing.T) {
	model := setupModel(t, modelOpts{})

	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionRenameWorkspace})

	assertMode(t, model, input.ModePrompt)
	if model.modalHost.Prompt.Kind != "rename-workspace" {
		t.Fatalf("expected rename-workspace prompt, got %q", model.modalHost.Prompt.Kind)
	}

	model.modalHost.Prompt.Value = "dev"
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	assertMode(t, model, input.ModeNormal)
	ws := model.workbench.CurrentWorkspace()
	if ws.Name != "dev" {
		t.Fatalf("expected workspace name dev, got %q", ws.Name)
	}
}

func TestFeatureWorkspaceNextPrev(t *testing.T) {
	model := setupModel(t, modelOpts{})
	createWorkspaceViaPrompt(t, model, "dev")
	secondWs := model.workbench.CurrentWorkspace().Name

	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPrevWorkspace})
	ws := model.workbench.CurrentWorkspace()
	if ws.Name == secondWs {
		t.Fatal("expected prev workspace to change")
	}

	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionNextWorkspace})
	ws = model.workbench.CurrentWorkspace()
	if ws.Name != secondWs {
		t.Fatalf("expected next workspace to return, got %q", ws.Name)
	}
}

// ─── Group 4: Floating Pane Operations ──────────────────────────────────────

func TestFeatureFloatingCreate(t *testing.T) {
	model := setupModel(t, modelOpts{})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCreateFloatingPane})

	tab := model.workbench.CurrentTab()
	if len(tab.Floating) == 0 {
		t.Fatal("expected floating pane after create")
	}
	assertMode(t, model, input.ModePicker)
}

func TestFeatureFloatingMoveAndResize(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterFloatingMode})

	initialFloat := findFloating(tab, "float-1")
	if initialFloat == nil {
		t.Fatal("expected floating pane")
	}
	origX := initialFloat.Rect.X

	dispatchKey(t, model, runeKeyMsg('l'))

	updated := findFloating(tab, "float-1")
	if updated.Rect.X <= origX {
		t.Fatalf("expected float X to increase after move right, got %d (was %d)", updated.Rect.X, origX)
	}

	origW := updated.Rect.W
	dispatchKey(t, model, runeKeyMsg('L'))

	updated = findFloating(tab, "float-1")
	if updated.Rect.W <= origW {
		t.Fatalf("expected float W to increase after resize right, got %d (was %d)", updated.Rect.W, origW)
	}
}

func TestFeatureFloatingActionReordersPaneToTop(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 1, Y: 1, W: 40, H: 20})
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-2", workbench.Rect{X: 5, Y: 5, W: 40, H: 20})
	_ = model.workbench.FocusPane(tab.ID, "float-1")
	model.input.SetMode(input.ModeState{Kind: input.ModeFloating})

	dispatchKey(t, model, runeKeyMsg('l'))

	if got := tab.Floating[len(tab.Floating)-1].PaneID; got != "float-1" {
		t.Fatalf("expected moved floating pane to reorder to top, got %#v", tab.Floating)
	}
}

func TestFeatureEnterFloatingModeFocusesTopmostFloatingPane(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 1, Y: 1, W: 30, H: 10})
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-2", workbench.Rect{X: 4, Y: 3, W: 30, H: 10})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterFloatingMode})

	if got := tab.ActivePaneID; got != "float-2" {
		t.Fatalf("expected topmost floating pane to become active, got %q", got)
	}
	assertMode(t, model, input.ModeFloating)
}

func TestFeatureFloatingModeCyclesFloatingSelection(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 1, Y: 1, W: 30, H: 10})
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-2", workbench.Rect{X: 4, Y: 3, W: 30, H: 10})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterFloatingMode})
	if got := tab.ActivePaneID; got != "float-2" {
		t.Fatalf("expected topmost floating pane to become active, got %q", got)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := tab.ActivePaneID; got != "float-1" {
		t.Fatalf("expected shift-tab to select previous floating pane, got %q", got)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if got := tab.ActivePaneID; got != "float-2" {
		t.Fatalf("expected tab to select next floating pane, got %q", got)
	}
	if got := tab.Floating[len(tab.Floating)-1].PaneID; got != "float-2" {
		t.Fatalf("expected selected floating pane to be reordered on top, got %#v", tab.Floating)
	}
}

func TestFeatureFloatingCenter(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 0, Y: 0, W: 40, H: 20})
	_ = model.workbench.FocusPane(tab.ID, "float-1")
	model.input.SetMode(input.ModeState{Kind: input.ModeFloating})

	dispatchKey(t, model, runeKeyMsg('c'))

	updated := findFloating(tab, "float-1")
	if updated.Rect.X == 0 && updated.Rect.Y == 0 {
		t.Fatal("expected floating pane to be centered (not at 0,0)")
	}
}

func TestFeatureFloatingToggleVisibility(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20})

	if !tab.FloatingVisible {
		t.Fatal("expected floating visible by default")
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionToggleFloatingVisibility})

	tab = model.workbench.CurrentTab()
	if tab.FloatingVisible {
		t.Fatal("expected floating hidden after toggle")
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionToggleFloatingVisibility})

	tab = model.workbench.CurrentTab()
	if !tab.FloatingVisible {
		t.Fatal("expected floating visible after second toggle")
	}
}

func TestFeatureFloatingClose(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 40, H: 20})
	if len(tab.Floating) != 1 {
		t.Fatal("expected 1 floating pane")
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCloseFloatingPane, PaneID: "float-1"})

	tab = model.workbench.CurrentTab()
	if len(tab.Floating) != 0 {
		t.Fatalf("expected 0 floating panes after close, got %d", len(tab.Floating))
	}
}

func TestFeatureFloatingOverviewOpensWithItems(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 30, H: 10})
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-2", workbench.Rect{X: 20, Y: 8, W: 30, H: 10})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenFloatingOverview})

	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModeFloatingOverview {
		t.Fatalf("expected floating overview modal, got %#v", model.modalHost)
	}
	if model.modalHost.FloatingOverview == nil || len(model.modalHost.FloatingOverview.Items) != 2 {
		t.Fatalf("expected 2 floating overview items, got %#v", model.modalHost.FloatingOverview)
	}
}

func TestFeatureFloatingOverviewCloseLastItemClosesOverview(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 30, H: 10}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenFloatingOverview})
	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModeFloatingOverview {
		t.Fatalf("expected floating overview modal before close, got %#v", model.modalHost)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCloseFloatingPane, PaneID: "float-1"})

	if model.modalHost != nil && model.modalHost.Session != nil && model.modalHost.Session.Kind == input.ModeFloatingOverview {
		t.Fatalf("expected floating overview to close after last floating pane removed, got %#v", model.modalHost.Session)
	}
	tab = model.workbench.CurrentTab()
	if tab == nil || len(tab.Floating) != 0 {
		t.Fatalf("expected all floating panes removed, got %#v", tab)
	}
}

func TestFeatureSummonCollapsedFloatingPane(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 30, H: 10})
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-2", workbench.Rect{X: 20, Y: 8, W: 30, H: 10})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCollapseFloatingPane, PaneID: "float-2"})

	if got := model.workbench.FloatingState(tab.ID, "float-2"); got == nil || got.Display != workbench.FloatingDisplayCollapsed {
		t.Fatalf("expected float-2 collapsed, got %#v", got)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSummonFloatingPane, Text: "1"})

	if got := model.workbench.FloatingState(tab.ID, "float-2"); got == nil || got.Display != workbench.FloatingDisplayExpanded {
		t.Fatalf("expected float-2 restored by summon, got %#v", got)
	}
	if got := tab.ActivePaneID; got != "float-2" {
		t.Fatalf("expected summon to focus float-2, got %q", got)
	}
}

func TestFeatureFloatingFitOnceUsesSnapshotExtent(t *testing.T) {
	client := &recordingBridgeClient{
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-float": {
				TerminalID: "term-float",
				Size:       protocol.Size{Cols: 48, Rows: 14},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	tab := model.workbench.CurrentTab()
	_ = model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 10, Y: 5, W: 20, H: 8})
	if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-float"); err != nil {
		t.Fatalf("bind floating terminal: %v", err)
	}
	model.runtime.Registry().GetOrCreate("term-float").Snapshot = &protocol.Snapshot{
		TerminalID: "term-float",
		Size:       protocol.Size{Cols: 48, Rows: 14},
	}
	binding := model.runtime.BindPane("float-1")
	binding.Role = runtime.BindingRoleOwner
	binding.Channel = 7
	binding.Connected = true

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionAutoFitFloatingPane, PaneID: "float-1"})

	if got := model.workbench.FloatingState(tab.ID, "float-1"); got == nil || got.Rect.W != 50 || got.Rect.H != 16 {
		t.Fatalf("expected fit-once rect 50x16, got %#v", got)
	}
}

// ─── Group 5: Resize Operations ─────────────────────────────────────────────

func TestFeatureResizeAdjustRatio(t *testing.T) {
	model := setupTwoPaneModel(t)
	tab := model.workbench.CurrentTab()
	if tab.Root == nil || tab.Root.Ratio != 0.5 {
		t.Fatal("expected initial ratio 0.5")
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionResizePaneRight, PaneID: "pane-1"})

	tab = model.workbench.CurrentTab()
	if tab.Root.Ratio == 0.5 {
		t.Fatal("expected ratio to change after resize")
	}
	client := model.runtime.Client().(*recordingBridgeClient)
	if len(client.resizes) != 2 {
		t.Fatalf("expected both resized panes to update their PTYs, got %#v", client.resizes)
	}
}

func TestFeatureResizeAdjustRatioPersistsSession(t *testing.T) {
	model := setupTwoPaneModel(t)
	model.sessionID = "main"
	model.sessionRevision = 7
	model.sessionViewID = "view-1"

	client := model.runtime.Client().(*recordingBridgeClient)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionResizePaneRight, PaneID: "pane-1"})

	if len(client.replaceCalls) != 1 {
		t.Fatalf("expected one replace call after resize, got %d", len(client.replaceCalls))
	}
	if got := client.replaceCalls[0]; got.SessionID != "main" || got.BaseRevision != 7 || got.ViewID != "view-1" {
		t.Fatalf("unexpected replace params: %#v", got)
	}
}

func TestFeatureResizeBalance(t *testing.T) {
	model := setupTwoPaneModel(t)
	// First change the ratio
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionResizePaneRight, PaneID: "pane-1"})
	tab := model.workbench.CurrentTab()
	if tab.Root.Ratio == 0.5 {
		t.Fatal("expected ratio changed before balance")
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionBalancePanes})

	tab = model.workbench.CurrentTab()
	if tab.Root.Ratio != 0.5 {
		t.Fatalf("expected ratio 0.5 after balance, got %f", tab.Root.Ratio)
	}
}

func TestFeatureResizeBalancePersistsSession(t *testing.T) {
	model := setupTwoPaneModel(t)
	model.sessionID = "main"
	model.sessionRevision = 3

	client := model.runtime.Client().(*recordingBridgeClient)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionBalancePanes})

	if len(client.replaceCalls) != 1 {
		t.Fatalf("expected one replace call after balance, got %d", len(client.replaceCalls))
	}
}

func TestFeatureResizeCycleLayout(t *testing.T) {
	model := setupTwoPaneModel(t)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCycleLayout})

	// CycleLayout should change the layout direction or preset — just verify it doesn't crash
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected tab to exist after cycle layout")
	}
}

func TestFeatureResizeCycleLayoutPersistsSession(t *testing.T) {
	model := setupTwoPaneModel(t)
	model.sessionID = "main"
	model.sessionRevision = 5

	client := model.runtime.Client().(*recordingBridgeClient)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCycleLayout})

	if len(client.replaceCalls) != 1 {
		t.Fatalf("expected one replace call after cycle layout, got %d", len(client.replaceCalls))
	}
}

// ─── Group 6: Display/Scroll Operations ─────────────────────────────────────

func TestFeatureScrollUpDown(t *testing.T) {
	model := setupModel(t, modelOpts{})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionScrollUp})
	tab := model.workbench.CurrentTab()
	if tab.ScrollOffset != 1 {
		t.Fatalf("expected scroll offset 1, got %d", tab.ScrollOffset)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionScrollDown})
	tab = model.workbench.CurrentTab()
	if tab.ScrollOffset != 0 {
		t.Fatalf("expected scroll offset 0, got %d", tab.ScrollOffset)
	}
}

func TestFeatureScrollToTopBottom(t *testing.T) {
	t.Skip("scroll-to-top/bottom actions are not part of the current baseline")
}

// ─── Group 7: Terminal Picker Workflow ───────────────────────────────────────

func TestFeaturePickerOpenNavigateAttach(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", State: "running"},
			},
		},
		attachResult:       &protocol.AttachResult{Channel: 5, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})
	// Detach first so we can re-attach
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionDetachPane, PaneID: "pane-1"})
	assertMode(t, model, input.ModeNormal)

	// Open picker via action
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: "req-1"})
	assertMode(t, model, input.ModePicker)

	if model.modalHost.Picker == nil {
		t.Fatal("expected picker state")
	}
	// Wait for picker items to load (they come via effectCmd → pickerItemsLoadedMsg)
	if len(model.modalHost.Picker.Items) < 2 {
		t.Fatalf("expected at least 2 picker items, got %d", len(model.modalHost.Picker.Items))
	}

	// Navigate to term-2 and submit.
	for {
		selected := model.modalHost.Picker.SelectedItem()
		if selected != nil && selected.TerminalID == "term-2" {
			break
		}
		dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPickerDown})
	}

	// Submit to attach (selected is term-2)
	selected := model.modalHost.Picker.SelectedItem()
	if selected == nil || selected.TerminalID != "term-2" {
		t.Fatalf("expected selected term-2, got %#v", selected)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1", TargetID: "term-2"})

	// After attach, pane should be bound
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-2" {
		t.Fatalf("expected pane bound to term-2, got %#v", pane)
	}
	assertMode(t, model, input.ModeNormal)
}

func TestFeaturePickerSearchFilter(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell"},
			{TerminalID: "term-2", Name: "logs"},
			{TerminalID: "term-3", Name: "htop"},
		},
	}
	model.modalHost.Picker.ApplyFilter()
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})

	// Type "lo" to filter
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	visible := model.modalHost.Picker.VisibleItems()
	if len(visible) != 1 || visible[0].TerminalID != "term-2" {
		t.Fatalf("expected filter to show only logs, got %d items", len(visible))
	}

	// Backspace to remove filter
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	visible = model.modalHost.Picker.VisibleItems()
	if len(visible) != 3 {
		t.Fatalf("expected all 3 items after clearing filter, got %d", len(visible))
	}
}

func TestFeatureWorkspacePickerSearchFilter(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModeWorkspacePicker, Phase: modal.ModalPhaseReady, RequestID: "ws-1"}
	model.modalHost.WorkspacePicker = &modal.WorkspacePickerState{
		Items: []modal.WorkspacePickerItem{
			{Name: "main", Description: "default"},
			{Name: "logs", Description: "tail"},
			{Name: "ops", Description: "alerts"},
		},
	}
	model.modalHost.WorkspacePicker.ApplyFilter()
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspacePicker, RequestID: "ws-1"})

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	if got := model.modalHost.WorkspacePicker.Query; got != "lo" {
		t.Fatalf("expected workspace picker query lo, got %q", got)
	}
	visible := model.modalHost.WorkspacePicker.VisibleItems()
	if len(visible) != 2 || visible[0].Name != "logs" || !visible[1].CreateNew {
		t.Fatalf("expected filter to show only logs, got %#v", visible)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if got := model.modalHost.WorkspacePicker.Query; got != "" {
		t.Fatalf("expected workspace picker query to clear, got %q", got)
	}
	visible = model.modalHost.WorkspacePicker.VisibleItems()
	if len(visible) != 4 || !visible[len(visible)-1].CreateNew {
		t.Fatalf("expected all 3 workspaces plus create row after clearing filter, got %#v", visible)
	}
}

func TestFeaturePickerCreateFlow(t *testing.T) {
	client := &recordingBridgeClient{
		createResult:       &protocol.CreateResult{TerminalID: "term-new"},
		attachResult:       &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	// Setup picker with create-new item selected
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{
		Selected: 0,
		Items:    []modal.PickerItem{{CreateNew: true, Name: "new terminal"}},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})

	// Submit create-new
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})

	assertMode(t, model, input.ModePrompt)
	if model.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create-terminal-form prompt, got %q", model.modalHost.Prompt.Kind)
	}
	model.modalHost.Prompt.Field("name").Value = "my-term"
	model.modalHost.Prompt.Field("name").Cursor = len([]rune("my-term"))
	model.modalHost.Prompt.Field("tags").Value = "env=test"
	model.modalHost.Prompt.Field("tags").Cursor = len([]rune("env=test"))
	model.modalHost.Prompt.ActiveField = 3
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})

	// After create, the pane should be bound
	if len(client.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(client.createCalls))
	}
	if client.createCalls[0].params.Name != "my-term" {
		t.Fatalf("expected create name my-term, got %q", client.createCalls[0].params.Name)
	}
}

func TestFeaturePickerSplitCreateFlowSetsSplitTarget(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{
		Selected: 0,
		Items:    []modal.PickerItem{{CreateNew: true, Name: "new terminal"}},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPickerAttachSplit, PaneID: "pane-1"})

	assertMode(t, model, input.ModePrompt)
	if model.modalHost.Prompt == nil {
		t.Fatal("expected create-terminal prompt")
	}
	if model.modalHost.Prompt.CreateTarget != modal.CreateTargetSplit {
		t.Fatalf("expected split create target, got %q", model.modalHost.Prompt.CreateTarget)
	}
	if model.modalHost.Prompt.PaneID != "pane-1" {
		t.Fatalf("expected prompt pane target pane-1, got %q", model.modalHost.Prompt.PaneID)
	}
}

// ─── Group 8: Workspace Picker Workflow ─────────────────────────────────────

func TestFeatureWorkspacePickerOpenSwitchClose(t *testing.T) {
	model := setupModel(t, modelOpts{})
	// Create a second workspace
	createWorkspaceViaPrompt(t, model, "dev")
	secondWs := model.workbench.CurrentWorkspace().Name
	// Switch back to main
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSwitchWorkspace, Text: "main"})
	assertMode(t, model, input.ModeNormal)

	// Enter workspace mode, then open workspace picker
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenWorkspacePicker})
	assertMode(t, model, input.ModeWorkspacePicker)

	if model.modalHost.WorkspacePicker == nil {
		t.Fatal("expected workspace picker state")
	}
	items := model.modalHost.WorkspacePicker.VisibleItems()
	if len(items) < 2 {
		t.Fatalf("expected at least 2 workspace items, got %d", len(items))
	}

	// Navigate to second workspace and submit
	for i, item := range items {
		if item.Name == secondWs {
			model.modalHost.WorkspacePicker.Selected = i
			break
		}
	}
	// Submit switches workspace
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd != nil {
		msg := cmd()
		if action, ok := msg.(input.SemanticAction); ok {
			dispatchAction(t, model, action)
		}
	}

	ws := model.workbench.CurrentWorkspace()
	if ws.Name != secondWs {
		t.Fatalf("expected workspace %q, got %q", secondWs, ws.Name)
	}
}

func TestFeatureWorkspacePickerCreateRowCreatesWorkspace(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenWorkspacePicker})
	assertMode(t, model, input.ModeWorkspacePicker)

	if model.modalHost.WorkspacePicker == nil {
		t.Fatal("expected workspace picker state")
	}
	items := model.modalHost.WorkspacePicker.VisibleItems()
	createIdx := -1
	for i, item := range items {
		if item.CreateNew {
			createIdx = i
			break
		}
	}
	if createIdx < 0 {
		t.Fatalf("expected create row in workspace picker, got %#v", items)
	}
	model.modalHost.WorkspacePicker.Selected = createIdx

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt})
	if cmd != nil {
		drainCmd(t, model, cmd, 20)
	}
	assertMode(t, model, input.ModePrompt)
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "rename-workspace" || model.modalHost.Prompt.Original != "" {
		t.Fatalf("expected create-workspace prompt state, got %#v", model.modalHost.Prompt)
	}
	model.modalHost.Prompt.Value = "dev"
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	ws := model.workbench.CurrentWorkspace()
	if ws == nil || ws.Name != "dev" {
		t.Fatalf("expected newly created workspace to become current, got %#v", ws)
	}
	assertMode(t, model, input.ModeWorkspacePicker)
	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModeWorkspacePicker {
		t.Fatalf("expected workspace picker modal to remain open after create, got %#v", model.modalHost.Session)
	}
}

// ─── Group 9: Terminal Manager Workflow ─────────────────────────────────────

func TestFeatureTerminalManagerOpenAndKill(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", State: "running"},
			},
		},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	// Must enter global mode first
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	assertMode(t, model, input.ModeGlobal)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	assertMode(t, model, input.ModeTerminalManager)

	if model.terminalPage == nil {
		t.Fatal("expected terminal pool page state")
	}
	if model.modalHost.Session != nil {
		t.Fatalf("expected terminal pool to be page surface, got modal session %#v", model.modalHost.Session)
	}

	// Find the first selectable terminal item.
	items := model.terminalPage.VisibleItems()
	targetIdx := -1
	for i, item := range items {
		if item.TerminalID != "" {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		t.Fatal("no selectable terminal in manager items")
	}
	// Navigate to the target item
	for model.terminalPage.Selected != targetIdx {
		dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPickerDown})
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionKillTerminal})

	if len(client.killCalls) == 0 {
		t.Fatal("expected at least one kill call")
	}
}

func TestFeatureTerminalManagerLoadsRichGroupedItems(t *testing.T) {
	exited := 23
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-visible", Name: "shell", Command: []string{"bash", "-lc", "htop"}, State: "running"},
				{ID: "term-parked", Name: "logs", Command: []string{"tail", "-f", "/tmp/app.log"}, State: "running"},
				{ID: "term-exited", Name: "job", Command: []string{"make", "test"}, State: "exited", ExitCode: &exited},
			},
		},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	workspaces := map[string]*workbench.WorkspaceState{
		"main": {
			Name:      "main",
			ActiveTab: 0,
			Tabs: []*workbench.TabState{
				{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-visible"},
					},
					Root: workbench.NewLeaf("pane-1"),
				},
				{
					ID:           "tab-2",
					Name:         "tab 2",
					ActivePaneID: "pane-2",
					Panes: map[string]*workbench.PaneState{
						"pane-2": {ID: "pane-2", Title: "logs", TerminalID: "term-parked"},
					},
					Root: workbench.NewLeaf("pane-2"),
				},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client, workspaces: workspaces})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenTerminalManager})

	items := model.terminalPage.VisibleItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 manager items, got %#v", items)
	}
	if items[0].TerminalID != "term-visible" || items[0].State != "visible" || !items[0].Observed {
		t.Fatalf("expected first manager item to be visible terminal, got %#v", items[0])
	}
	if !strings.Contains(items[0].Command, "htop") || !strings.Contains(items[0].Location, "main/tab 1/pane-1") {
		t.Fatalf("expected rich visible item details, got %#v", items[0])
	}
	if !strings.Contains(items[0].Description, "running") || !strings.Contains(items[0].Description, "1 pane bound") {
		t.Fatalf("expected visible item description to include runtime and binding count, got %#v", items[0])
	}
	if items[1].TerminalID != "term-parked" || items[1].State != "parked" || items[1].Observed {
		t.Fatalf("expected second manager item to be parked terminal, got %#v", items[1])
	}
	if !strings.Contains(items[1].Description, "1 pane bound") {
		t.Fatalf("expected parked item description to include binding count, got %#v", items[1])
	}
	if items[2].TerminalID != "term-exited" || items[2].State != "exited" {
		t.Fatalf("expected last manager item to be exited terminal, got %#v", items[2])
	}
	if !strings.Contains(items[2].Description, "exited (23)") {
		t.Fatalf("expected exited item description to include exit detail, got %#v", items[2])
	}
}

func TestFeatureTerminalManagerEnterAttachesSelectedTerminalHere(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", Command: []string{"tail", "-f", "/tmp/app.log"}, State: "running"},
			},
		},
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-2": {TerminalID: "term-2", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPickerDown})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	if pane := model.workbench.ActivePane(); pane == nil || pane.TerminalID != "term-2" {
		t.Fatalf("expected active pane to attach term-2, got %#v", pane)
	}
	assertMode(t, model, input.ModeNormal)
	if model.modalHost.Session != nil {
		t.Fatalf("expected terminal pool page not to leave modal session behind, got %#v", model.modalHost.Session)
	}
}

func TestFeaturePickerExitedItemBindsPaneAndLoadsSnapshot(t *testing.T) {
	exited := 23
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "done", State: "exited", ExitCode: &exited},
			},
		},
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-2": {
				TerminalID: "term-2",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "done", Width: 4}}},
				},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenPicker, PaneID: "pane-1", TargetID: "pane-1"})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(client.attachCalls) != 0 {
		t.Fatalf("expected exited picker selection not to attach, got %#v", client.attachCalls)
	}
	if pane := model.workbench.ActivePane(); pane == nil || pane.TerminalID != "term-2" {
		t.Fatalf("expected active pane rebound to exited term-2, got %#v", pane)
	}
	terminal := model.runtime.Registry().Get("term-2")
	if terminal == nil || terminal.State != "exited" {
		t.Fatalf("expected exited runtime cached for term-2, got %#v", terminal)
	}
	if terminal.ExitCode == nil || *terminal.ExitCode != exited {
		t.Fatalf("expected exit code retained on selected terminal, got %#v", terminal)
	}
	if terminal.Snapshot == nil || len(terminal.Snapshot.Screen.Cells) == 0 {
		t.Fatalf("expected exited terminal snapshot loaded after selection, got %#v", terminal)
	}
	assertMode(t, model, input.ModeNormal)
}

func TestFeatureTerminalPoolExitedItemBindsPaneAndClosesManager(t *testing.T) {
	exited := 23
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "done", State: "exited", ExitCode: &exited},
			},
		},
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-2": {
				TerminalID: "term-2",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "done", Width: 4}}},
				},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPickerDown})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	if len(client.attachCalls) != 0 {
		t.Fatalf("expected exited terminal manager selection not to attach, got %#v", client.attachCalls)
	}
	if pane := model.workbench.ActivePane(); pane == nil || pane.TerminalID != "term-2" {
		t.Fatalf("expected active pane rebound to exited term-2, got %#v", pane)
	}
	terminal := model.runtime.Registry().Get("term-2")
	if terminal == nil || terminal.State != "exited" || terminal.Snapshot == nil {
		t.Fatalf("expected exited terminal cached with snapshot, got %#v", terminal)
	}
	assertMode(t, model, input.ModeNormal)
	if model.terminalPage != nil {
		t.Fatalf("expected terminal manager to close after selection, got %#v", model.terminalPage)
	}
}

func TestFeatureZoomPaneBlockedByTerminalSizeLock(t *testing.T) {
	model := setupModel(t, modelOpts{})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Tags = map[string]string{"termx.size_lock": "lock"}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionZoomPane, PaneID: "pane-1"})

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if tab.ZoomedPaneID != "" {
		t.Fatalf("expected zoom to stay blocked for locked terminal, got %q", tab.ZoomedPaneID)
	}
	if got := strings.TrimSpace(model.notice); got != terminalSizeLockedNotice {
		t.Fatalf("expected lock notice %q, got %q", terminalSizeLockedNotice, got)
	}
}

func TestFeatureToggleTerminalSizeLockWithKeyboardSavesMetadata(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})
	if before := xansi.Strip(model.View()); !strings.Contains(before, terminalmeta.SizeLockButtonLabel(false)+" shell") {
		t.Fatalf("expected unlocked size lock button before toggle, got:\n%s", before)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterPaneMode})
	dispatchKey(t, model, runeKeyMsg('s'))

	if len(client.setMetadataCalls) != 1 {
		t.Fatalf("expected one metadata toggle call, got %#v", client.setMetadataCalls)
	}
	if got := client.setMetadataCalls[0].tags["termx.size_lock"]; got != "lock" {
		t.Fatalf("expected size lock tag to be saved, got %#v", client.setMetadataCalls[0].tags)
	}
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil || !terminalmeta.SizeLocked(terminal.Tags) {
		t.Fatalf("expected runtime registry tags to reflect locked state, got %#v", terminal)
	}
	if got := strings.TrimSpace(model.notice); got != terminalSizeLockedNotice {
		t.Fatalf("expected notice %q, got %q", terminalSizeLockedNotice, got)
	}
	if afterLock := xansi.Strip(model.View()); !strings.Contains(afterLock, terminalmeta.SizeLockButtonLabel(true)+" shell") {
		t.Fatalf("expected locked size lock button after toggle, got:\n%s", afterLock)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterPaneMode})
	dispatchKey(t, model, runeKeyMsg('s'))

	if len(client.setMetadataCalls) != 2 {
		t.Fatalf("expected second metadata toggle call, got %#v", client.setMetadataCalls)
	}
	if _, ok := client.setMetadataCalls[1].tags["termx.size_lock"]; ok {
		t.Fatalf("expected unlock to clear size lock tag, got %#v", client.setMetadataCalls[1].tags)
	}
	if terminalmeta.SizeLocked(model.runtime.Registry().Get("term-1").Tags) {
		t.Fatalf("expected runtime registry tags to reflect unlocked state, got %#v", model.runtime.Registry().Get("term-1"))
	}
	if afterUnlock := xansi.Strip(model.View()); !strings.Contains(afterUnlock, terminalmeta.SizeLockButtonLabel(false)+" shell") {
		t.Fatalf("expected unlocked size lock button after second toggle, got:\n%s", afterUnlock)
	}
}

func TestFeatureExitedPaneRestartReattachesSameTerminal(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", Command: []string{"bash"}, State: "running"},
			},
		},
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
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
		t.Fatalf("expected restart flow to reattach term-1, got %#v", client.attachCalls)
	}
	if terminal.State != "running" {
		t.Fatalf("expected terminal state refreshed to running, got %#v", terminal)
	}
	if binding := model.runtime.Binding("pane-1"); binding == nil || binding.Channel != 9 || !binding.Connected {
		t.Fatalf("expected pane binding updated to restarted channel, got %#v", binding)
	}
}

func TestFeatureTerminalManagerAttachTabCreatesTabAndAttachesTerminal(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", Command: []string{"tail", "-f", "/tmp/app.log"}, State: "running"},
			},
		},
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-2": {TerminalID: "term-2", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPickerDown})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionAttachTab})

	assertTabCount(t, model, 2)
	if pane := model.workbench.ActivePane(); pane == nil || pane.TerminalID != "term-2" {
		t.Fatalf("expected new tab active pane to attach term-2, got %#v", pane)
	}
	assertMode(t, model, input.ModeNormal)
}

func TestFeatureTerminalManagerAttachFloatingCreatesFloatingPaneAndAttachesTerminal(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", Command: []string{"tail", "-f", "/tmp/app.log"}, State: "running"},
			},
		},
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-2": {TerminalID: "term-2", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPickerDown})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionAttachFloating})

	tab := model.workbench.CurrentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected one floating pane, got %#v", tab)
	}
	floatingPane := tab.Panes[tab.Floating[0].PaneID]
	if floatingPane == nil || floatingPane.TerminalID != "term-2" {
		t.Fatalf("expected floating pane to attach term-2, got %#v", floatingPane)
	}
	assertMode(t, model, input.ModeNormal)
}

func TestFeatureTerminalManagerEditOpensTerminalMetadataPrompt(t *testing.T) {
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
	model := setupModel(t, modelOpts{client: client})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPickerDown})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEditTerminal})

	assertMode(t, model, input.ModePrompt)
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "edit-terminal-name" {
		t.Fatalf("expected edit-terminal-name prompt, got %#v", model.modalHost.Prompt)
	}
	if model.modalHost.Prompt.TerminalID != "term-2" || !strings.Contains(strings.Join(model.modalHost.Prompt.Command, " "), "tail -f /tmp/app.log") {
		t.Fatalf("expected prompt to target term-2 with command summary, got %#v", model.modalHost.Prompt)
	}
}

func TestFeatureTerminalPoolEscReturnsToWorkbenchWithoutLosingState(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "logs", State: "running"},
			},
		},
	}
	workspaces := map[string]*workbench.WorkspaceState{
		"main": {
			Name:      "main",
			ActiveTab: 1,
			Tabs: []*workbench.TabState{
				{
					ID:           "tab-1",
					Name:         "one",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
					},
					Root: workbench.NewLeaf("pane-1"),
				},
				{
					ID:           "tab-2",
					Name:         "two",
					ActivePaneID: "pane-2",
					Panes: map[string]*workbench.PaneState{
						"pane-2": {ID: "pane-2", Title: "logs", TerminalID: "term-2"},
					},
					Root: workbench.NewLeaf("pane-2"),
				},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client, workspaces: workspaces})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterGlobalMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenTerminalManager})
	if model.terminalPage == nil {
		t.Fatal("expected terminal pool page state")
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})

	assertMode(t, model, input.ModeNormal)
	if model.terminalPage != nil {
		t.Fatalf("expected terminal pool page to close, got %#v", model.terminalPage)
	}
	tab := model.workbench.CurrentTab()
	if tab == nil || tab.ID != "tab-2" || tab.ActivePaneID != "pane-2" {
		t.Fatalf("expected workbench focus to survive page exit, got %#v", tab)
	}
}

func TestFeatureTerminalManagerSearchFilter(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.terminalPage = &modal.TerminalManagerState{
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell"},
			{TerminalID: "term-2", Name: "logs"},
			{TerminalID: "term-3", Name: "htop"},
		},
	}
	model.terminalPage.ApplyFilter()
	model.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager})

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	if got := model.terminalPage.Query; got != "lo" {
		t.Fatalf("expected terminal manager query lo, got %q", got)
	}
	visible := model.terminalPage.VisibleItems()
	if len(visible) != 1 || visible[0].TerminalID != "term-2" {
		t.Fatalf("expected filter to show only logs terminal, got %#v", visible)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if got := model.terminalPage.Query; got != "" {
		t.Fatalf("expected terminal manager query to clear, got %q", got)
	}
	visible = model.terminalPage.VisibleItems()
	if len(visible) != 3 {
		t.Fatalf("expected all 3 terminals after clearing filter, got %d", len(visible))
	}
}

// ─── Group 11: Mode Transitions ─────────────────────────────────────────────

func TestFeatureAllModeEntryAndExit(t *testing.T) {
	model := setupModel(t, modelOpts{})

	tests := []struct {
		enter  input.ActionKind
		expect input.ModeKind
	}{
		{input.ActionEnterPaneMode, input.ModePane},
		{input.ActionEnterResizeMode, input.ModeResize},
		{input.ActionEnterTabMode, input.ModeTab},
		{input.ActionEnterWorkspaceMode, input.ModeWorkspace},
		{input.ActionEnterFloatingMode, input.ModeFloating},
		{input.ActionEnterDisplayMode, input.ModeDisplay},
		{input.ActionEnterGlobalMode, input.ModeGlobal},
	}

	for _, tc := range tests {
		model.input.SetMode(input.ModeState{Kind: input.ModeNormal})

		// Don't drain — just call Update and ignore the timeout cmd
		_, _ = model.Update(input.SemanticAction{Kind: tc.enter})
		assertMode(t, model, tc.expect)

		_, _ = model.Update(input.SemanticAction{Kind: input.ActionCancelMode})
		assertMode(t, model, input.ModeNormal)
	}
}

func TestFeatureStickyModeTimeout(t *testing.T) {
	model := setupModel(t, modelOpts{})

	// Enter pane mode (sticky) — don't drain timeout
	_, _ = model.Update(input.SemanticAction{Kind: input.ActionEnterPaneMode})
	assertMode(t, model, input.ModePane)

	// Simulate timeout message with current seq
	_, _ = model.Update(prefixTimeoutMsg{seq: model.prefixSeq})
	assertMode(t, model, input.ModeNormal)
}

func TestFeatureStickyModeRearmOnAction(t *testing.T) {
	model := setupTwoPaneModel(t)

	// Enter pane mode — don't drain
	_, _ = model.Update(input.SemanticAction{Kind: input.ActionEnterPaneMode})
	seqBefore := model.prefixSeq

	// Perform a focus action — don't drain timeout
	_, _ = model.Update(input.SemanticAction{Kind: input.ActionFocusPaneRight, PaneID: "pane-1"})

	if model.prefixSeq == seqBefore {
		t.Fatal("expected prefix seq to increment after action in sticky mode")
	}
	// Old timeout should be ignored
	_, _ = model.Update(prefixTimeoutMsg{seq: seqBefore})
	assertMode(t, model, input.ModePane)
}

// ─── Group 12: Render Integration ───────────────────────────────────────────

func TestFeatureRenderTabBarShowsAllTabs(t *testing.T) {
	model := setupModel(t, modelOpts{})
	createSecondTab(t, model)
	assertTabCount(t, model, 2)

	model.render.Invalidate()
	view := xansi.Strip(model.View())
	if !strings.Contains(view, "tab 1") {
		t.Fatalf("view missing tab 1:\n%s", view)
	}
}

func TestFeatureRenderStatusBarShowsModeHints(t *testing.T) {
	model := setupModel(t, modelOpts{width: 200})

	tests := []struct {
		mode input.ModeKind
		hint string
	}{
		{input.ModePane, "PANE"},
		{input.ModeResize, "RESIZE"},
		{input.ModeTab, "TAB"},
		{input.ModeWorkspace, "WORKSPACE"},
		{input.ModeFloating, "FLOATING"},
		{input.ModeDisplay, "DISPLAY"},
		{input.ModeGlobal, "GLOBAL"},
	}

	for _, tc := range tests {
		model.input.SetMode(input.ModeState{Kind: tc.mode})
		model.render.Invalidate()
		view := xansi.Strip(model.View())
		if !strings.Contains(view, tc.hint) {
			t.Fatalf("mode %q: view missing hint %q", tc.mode, tc.hint)
		}
	}
}

func TestFeatureRenderPickerOverlayUsesUnifiedBottomStatusHints(t *testing.T) {
	model := setupModel(t, modelOpts{width: 220})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})
	model.modalHost.Picker = &modal.PickerState{
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell"},
		},
	}
	model.modalHost.Picker.ApplyFilter()
	model.render.Invalidate()

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "Terminal Picker") {
		t.Fatalf("expected picker overlay to render:\n%s", view)
	}
	for _, unwanted := range []string{"[Enter] attach", "[Tab] split+attach", "[Ctrl-E] edit", "[Ctrl-K] kill", "[Esc] close"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("expected picker overlay to omit footer shortcut %q:\n%s", unwanted, view)
		}
	}
	for _, want := range []string{"PICKER", "[UP/DOWN] MOVE", "[TYPE] FILTER", "[Enter] HERE", "[Tab] SPLIT", "[Ctrl-E] EDIT", "[Ctrl-K] KILL", "[Esc] BACK"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected unified picker shortcut hint %q:\n%s", want, view)
		}
	}
}

func TestFeatureFloatingModeShowsCanonicalHints(t *testing.T) {
	model := setupModel(t, modelOpts{width: 200})
	model.input.SetMode(input.ModeState{Kind: input.ModeFloating})
	model.render.Invalidate()
	view := xansi.Strip(model.View())
	for _, want := range []string{"[N] NEW FLOAT", "[Esc] BACK"} {
		if !strings.Contains(view, want) {
			t.Fatalf("floating mode view missing %q:\n%s", want, view)
		}
	}
	for _, hidden := range []string{"h/j/k/l", "H/J/K/L", "c CENTER"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("floating mode view unexpectedly contains %q without active floating pane:\n%s", hidden, view)
		}
	}
}

func TestFeatureWorkspaceModeShowsLegacyAlignedHints(t *testing.T) {
	model := setupModel(t, modelOpts{width: 220})
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
	model.render.Invalidate()

	view := xansi.Strip(model.View())
	for _, want := range []string{"[F] PICK", "[C] NEW", "[R] RENAME", "[X] DELETE"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace mode view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "N/P NEXT/PREV") {
		t.Fatalf("workspace mode should hide next/prev when only one workspace exists:\n%s", view)
	}
}

func TestFeatureTabModeShowsLegacyAlignedHints(t *testing.T) {
	model := setupModel(t, modelOpts{width: 220})
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	model.render.Invalidate()

	view := xansi.Strip(model.View())
	for _, want := range []string{"[C] NEW", "[R] RENAME", "[X] KILL"} {
		if !strings.Contains(view, want) {
			t.Fatalf("tab mode view missing %q:\n%s", want, view)
		}
	}
	for _, hidden := range []string{"N/P NEXT/PREV", "1-9 JUMP"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("tab mode should hide %q when only one tab exists:\n%s", hidden, view)
		}
	}
}

func TestFeatureRenderPaneWithSnapshot(t *testing.T) {
	model := setupModel(t, modelOpts{})
	rt := model.runtime
	terminal := rt.Registry().Get("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				{{Content: "h", Width: 1}, {Content: "e", Width: 1}, {Content: "l", Width: 1}, {Content: "l", Width: 1}, {Content: "o", Width: 1}},
			},
		},
	}
	model.render.Invalidate()
	view := xansi.Strip(model.View())
	if !strings.Contains(view, "hello") {
		t.Fatalf("view missing snapshot content 'hello':\n%s", view)
	}
}

func TestFeatureRenderOverlayComposition(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{
		Title: "Terminal Picker",
		Items: []modal.PickerItem{
			{TerminalID: "term-1", Name: "shell", State: "running"},
		},
	}
	model.render.Invalidate()
	view := xansi.Strip(model.View())
	// Should see picker overlay in the view
	if !strings.Contains(view, "Terminal Picker") {
		t.Fatalf("view missing picker overlay:\n%s", view)
	}
	// Tab bar should still be visible at top
	if !strings.Contains(view, "main") {
		t.Fatalf("view missing workspace name in tab bar:\n%s", view)
	}
}

func TestFeatureRenderHelpOverlay(t *testing.T) {
	model := setupModel(t, modelOpts{})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenHelp})

	model.render.Invalidate()
	view := xansi.Strip(model.View())
	if !strings.Contains(view, "Help") {
		t.Fatalf("view missing help overlay:\n%s", view)
	}
	if !strings.Contains(view, "Ctrl-P") {
		t.Fatalf("view missing keybinding in help:\n%s", view)
	}
	if model.modalHost.Help == nil {
		t.Fatal("expected help state")
	}
	wantBindings := map[string]bool{
		"split current pane and attach selected terminal": false,
		"edit terminal metadata":                          false,
	}
	for _, section := range model.modalHost.Help.Sections {
		for _, binding := range section.Bindings {
			if _, ok := wantBindings[binding.Action]; ok {
				wantBindings[binding.Action] = true
			}
		}
	}
	for want, ok := range wantBindings {
		if !ok {
			t.Fatalf("help state missing restored capability %q: %#v", want, model.modalHost.Help.Sections)
		}
	}
}

func TestFeatureRenderPromptOverlay(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{
		Selected: 0,
		Items:    []modal.PickerItem{{CreateNew: true, Name: "new terminal"}},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})

	model.render.Invalidate()
	view := xansi.Strip(model.View())
	if !strings.Contains(view, "Create Terminal") {
		t.Fatalf("view missing create-terminal prompt overlay:\n%s", view)
	}
}

func TestFeatureRenderUnboundPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
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
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.width = 120
	model.height = 40

	view := xansi.Strip(model.View())
	for _, want := range []string{"unconnected", "Attach existing terminal", "Create new terminal", "Open terminal manager", "Close pane"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing unbound pane state %q:\n%s", want, view)
		}
	}
}

func TestFeatureRenderEmptyWorkspace(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: -1,
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.width = 120
	model.height = 40

	view := xansi.Strip(model.View())
	for _, want := range []string{"main", "No tabs in this workspace", "Ctrl-F open terminal picker", "Ctrl-T then c create a new tab"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing empty-workspace state %q:\n%s", want, view)
		}
	}
}

func TestFeatureRenderEmptyTab(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:    "tab-1",
			Name:  "tab 1",
			Panes: map[string]*workbench.PaneState{},
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.width = 120
	model.height = 40

	view := xansi.Strip(model.View())
	for _, want := range []string{"tab 1", "No panes in this tab", "Ctrl-F create the first pane via terminal picker"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing empty-tab state %q:\n%s", want, view)
		}
	}
}

func TestFeatureOpenPickerSeedsEmptyWorkspace(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: -1,
			},
		},
	})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenPicker})

	assertMode(t, model, input.ModePicker)
	assertTabCount(t, model, 1)
	pane := model.workbench.ActivePane()
	if pane == nil || pane.ID == "" {
		t.Fatalf("expected seeded pane after opening picker from empty workspace, got %#v", pane)
	}
	if model.modalHost.Session == nil || model.modalHost.Session.RequestID != pane.ID {
		t.Fatalf("expected picker request to target seeded pane %q, got %#v", pane.ID, model.modalHost.Session)
	}
}

func TestFeatureOpenPickerSeedsEmptyTab(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:    "tab-1",
					Name:  "tab 1",
					Panes: map[string]*workbench.PaneState{},
				}},
			},
		},
	})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenPicker})

	assertMode(t, model, input.ModePicker)
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab after opening picker from empty tab")
	}
	if len(tab.Panes) != 1 {
		t.Fatalf("expected seeded first pane in empty tab, got %#v", tab.Panes)
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.ID == "" {
		t.Fatalf("expected active pane after seeding empty tab, got %#v", pane)
	}
	if model.modalHost.Session == nil || model.modalHost.Session.RequestID != pane.ID {
		t.Fatalf("expected picker request to target seeded pane %q, got %#v", pane.ID, model.modalHost.Session)
	}
}

func TestFeatureRenderZoomedPaneShowsSinglePane(t *testing.T) {
	model := setupTwoPaneModel(t)
	// Zoom pane-1
	dispatchKey(t, model, ctrlKey(tea.KeyCtrlP))
	dispatchKey(t, model, runeKeyMsg('z'))

	model.render.Invalidate()
	view := xansi.Strip(model.View())
	if strings.Contains(view, "logs") {
		t.Fatalf("expected non-zoomed pane to be hidden:\n%s", view)
	}
	if strings.Contains(view, "Ctrl") || strings.Contains(view, "terminals:") || strings.Contains(view, "tab 1") {
		t.Fatalf("expected zoom to hide top/bottom chrome:\n%s", view)
	}
	if strings.Contains(view, "┌") || strings.Contains(view, "│") || strings.Contains(view, "┘") {
		t.Fatalf("expected zoom to hide pane borders:\n%s", view)
	}
}

// ─── Group 13: Terminal Resize Propagation ──────────────────────────────────

func TestFeatureWindowResizePropagatesToTerminal(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	drainCmd(t, model, cmd, 10)

	if len(client.resizes) == 0 {
		t.Fatal("expected resize call after window size change")
	}
	got := client.resizes[len(client.resizes)-1]
	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || len(visible.Tabs[visible.ActiveTab].Panes) == 0 {
		t.Fatalf("expected visible pane after resize, got %#v", visible)
	}
	wantRect, ok := paneContentRectForVisible(visible.Tabs[visible.ActiveTab].Panes[0])
	if !ok {
		t.Fatal("expected visible pane content rect")
	}
	if got.cols != uint16(wantRect.W) || got.rows != uint16(wantRect.H) {
		t.Fatalf("expected resize %dx%d, got %dx%d", wantRect.W, wantRect.H, got.cols, got.rows)
	}
}

func TestFeatureZoomResizePropagatesToTerminal(t *testing.T) {
	model := setupTwoPaneModel(t)
	client := model.runtime.Client().(*recordingBridgeClient)

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlP))
	dispatchKey(t, model, runeKeyMsg('z'))
	assertMode(t, model, input.ModeNormal)

	if len(client.resizes) == 0 {
		t.Fatal("expected resize call after zoom")
	}
	zoomResize := client.resizes[len(client.resizes)-1]
	if zoomResize.channel != 1 {
		t.Fatalf("expected zoom resize on active pane channel 1, got %#v", zoomResize)
	}
	if zoomResize.cols != 120 || zoomResize.rows != 40 {
		t.Fatalf("expected fullscreen zoom resize 120x40, got %#v", zoomResize)
	}

	resizeCountAfterZoom := len(client.resizes)
	dispatchKey(t, model, ctrlKey(tea.KeyCtrlP))
	dispatchKey(t, model, runeKeyMsg('z'))
	assertMode(t, model, input.ModeNormal)

	if len(client.resizes) <= resizeCountAfterZoom {
		t.Fatal("expected resize call after unzoom")
	}
	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatal("expected visible tab after unzoom")
	}
	var paneRect workbench.Rect
	for _, pane := range visible.Tabs[visible.ActiveTab].Panes {
		if pane.ID == "pane-1" {
			paneRect = pane.Rect
			break
		}
	}
	expectedRect, ok := model.terminalViewportRect("pane-1", paneRect)
	if !ok {
		t.Fatal("expected pane-1 terminal viewport after unzoom")
	}
	var paneResize *resizeCall
	for i := resizeCountAfterZoom; i < len(client.resizes); i++ {
		if client.resizes[i].channel == 1 {
			paneResize = &client.resizes[i]
			break
		}
	}
	if paneResize == nil {
		t.Fatalf("expected pane-1 resize after unzoom, got %#v", client.resizes[resizeCountAfterZoom:])
	}
	if paneResize.cols != uint16(expectedRect.W) || paneResize.rows != uint16(expectedRect.H) {
		t.Fatalf("expected unzoom resize %dx%d, got %#v", expectedRect.W, expectedRect.H, paneResize)
	}
}

func TestFeatureTabSwitchDoesNotResizeNewVisiblePanes(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	// Create a second tab
	createSecondTab(t, model)

	client.resizes = nil // clear

	// Switch to first tab using the implemented prev-tab action.
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPrevTab})

	if len(client.resizes) != 0 {
		t.Fatalf("expected tab switch not to resize panes, got %#v", client.resizes)
	}
}

// ─── Group 14: Terminal Lifecycle ───────────────────────────────────────────

func TestFeatureTerminalAttachBindsPaneAndClosesModal(t *testing.T) {
	model := setupModel(t, modelOpts{})
	// Detach pane and open picker
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionDetachPane, PaneID: "pane-1"})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})
	if pane := model.workbench.ActivePane(); pane != nil {
		pane.TerminalID = "term-99"
	}

	_, cmd := model.Update(orchestrator.TerminalAttachedMsg{PaneID: "pane-1", TerminalID: "term-99", Channel: 5})
	drainCmd(t, model, cmd, 10)

	pane := model.workbench.ActivePane()
	if pane.TerminalID != "term-99" {
		t.Fatalf("expected pane bound to term-99, got %q", pane.TerminalID)
	}
	if model.modalHost.Session != nil {
		t.Fatal("expected picker closed after attach")
	}
	assertMode(t, model, input.ModeNormal)
}

// ─── Group 15: Key-driven workflows (end-to-end from KeyMsg) ───────────────

func TestFeatureKeyDrivenPaneMode(t *testing.T) {
	model := setupTwoPaneModel(t)

	// Ctrl-P enters pane mode — don't drain timeout
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			if action, ok := msg.(input.SemanticAction); ok {
				_, _ = model.Update(action) // enters pane mode
			}
		}
	}
	assertMode(t, model, input.ModePane)

	// 'l' in pane mode focuses right — process action but not timeout
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			if action, ok := msg.(input.SemanticAction); ok {
				_, _ = model.Update(action)
			}
		}
	}
	assertActivePane(t, model, "pane-2")

	// Esc exits pane mode
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			if action, ok := msg.(input.SemanticAction); ok {
				_, _ = model.Update(action)
			}
		}
	}
	assertMode(t, model, input.ModeNormal)
}

func TestFeatureKeyDrivenTabMode(t *testing.T) {
	model := setupModel(t, modelOpts{})
	assertTabCount(t, model, 1)

	// Ctrl-T enters tab mode
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			if action, ok := msg.(input.SemanticAction); ok {
				_, _ = model.Update(action)
			}
		}
	}
	assertMode(t, model, input.ModeTab)

	// 'c' creates tab
	dispatchKey(t, model, runeKeyMsg('c'))
	assertTabCount(t, model, 2)
}

func TestFeatureKeyDrivenPickerOpen(t *testing.T) {
	client := &recordingBridgeClient{
		listResult:         &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "t1", Name: "s", State: "running"}}},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	// Ctrl-F opens picker
	dispatchKey(t, model, ctrlKey(tea.KeyCtrlF))
	assertMode(t, model, input.ModePicker)
}

func TestFeatureKeyDrivenResizeMode(t *testing.T) {
	model := setupTwoPaneModel(t)

	// Ctrl-R enters resize mode — don't drain timeout
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			if action, ok := msg.(input.SemanticAction); ok {
				_, _ = model.Update(action)
			}
		}
	}
	assertMode(t, model, input.ModeResize)

	tab := model.workbench.CurrentTab()
	origRatio := tab.Root.Ratio

	// 'l' in resize mode
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			if action, ok := msg.(input.SemanticAction); ok {
				_, _ = model.Update(action)
			}
		}
	}

	tab = model.workbench.CurrentTab()
	if tab.Root.Ratio == origRatio {
		t.Fatal("expected ratio to change after resize 'l' in resize mode")
	}
}

func TestFeatureKeyDrivenPaneDetach(t *testing.T) {
	model := setupModel(t, modelOpts{})
	term := model.runtime.Registry().Get("term-1")
	if term == nil {
		t.Fatal("expected term-1 runtime")
	}
	term.OwnerPaneID = "pane-1"
	term.BoundPaneIDs = []string{"pane-1"}
	binding := model.runtime.Binding("pane-1")
	if binding == nil {
		t.Fatal("expected pane binding")
	}
	binding.Role = runtime.BindingRoleOwner

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlP))
	assertMode(t, model, input.ModePane)
	dispatchKey(t, model, runeKeyMsg('d'))

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected detached active pane, got %#v", pane)
	}
}

func TestFeatureKeyDrivenDetachThenCtrlFReopensPicker(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{{ID: "term-1", Name: "shell", State: "running"}},
		},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlP))
	assertMode(t, model, input.ModePane)
	dispatchKey(t, model, runeKeyMsg('d'))

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected detached active pane, got %#v", pane)
	}

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlF))

	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session after Ctrl-F from sticky pane mode, got %#v", model.modalHost.Session)
	}
	assertMode(t, model, input.ModePicker)
	items := model.modalHost.Picker.VisibleItems()
	found := false
	for _, item := range items {
		if item.TerminalID == "term-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected detached terminal to be attachable in picker, got %#v", items)
	}
}

func TestFeatureKeyDrivenGlobalQuit(t *testing.T) {
	model := setupModel(t, modelOpts{})

	// Ctrl-G enters global mode
	dispatchKey(t, model, ctrlKey(tea.KeyCtrlG))
	assertMode(t, model, input.ModeGlobal)

	// q quits
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !model.quitting {
		t.Fatal("expected model.quitting after Ctrl-G q")
	}
}

func TestFeatureRepeatedRootShortcutPassthroughFromStickyMode(t *testing.T) {
	model := setupModel(t, modelOpts{})
	client := model.runtime.Client().(*recordingBridgeClient)

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlG))
	assertMode(t, model, input.ModeGlobal)

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlG))
	dispatchKey(t, model, ctrlKey(tea.KeyCtrlG))

	if len(client.inputCalls) != 2 {
		t.Fatalf("expected two passthrough input calls after repeated Ctrl-G, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "\a" || string(client.inputCalls[1].data) != "\a" {
		t.Fatalf("expected repeated Ctrl-G passthrough payloads, got %#v", client.inputCalls)
	}
	assertMode(t, model, input.ModeGlobal)
}

func TestFeatureRepeatedRootShortcutPassthroughFromPickerModal(t *testing.T) {
	client := &recordingBridgeClient{
		listResult:         &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "term-1", Name: "shell", State: "running"}}},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlF))
	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session after first Ctrl-F, got %#v", model.modalHost.Session)
	}

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlF))

	if len(client.inputCalls) != 1 {
		t.Fatalf("expected repeated Ctrl-F to reach the terminal once, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "\x06" {
		t.Fatalf("expected repeated Ctrl-F payload 0x06, got %q", string(client.inputCalls[0].data))
	}
	assertMode(t, model, input.ModePicker)
}

func TestFeatureRepeatedRootShortcutPassthroughClearedByPickerQueryInput(t *testing.T) {
	client := &recordingBridgeClient{
		listResult:         &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "term-1", Name: "shell", State: "running"}}},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlF))
	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session after first Ctrl-F, got %#v", model.modalHost.Session)
	}

	dispatchKey(t, model, runeKeyMsg('s'))
	if model.modalHost.Picker == nil || model.modalHost.Picker.Query != "s" {
		t.Fatalf("expected picker query to consume the intervening key, got %#v", model.modalHost.Picker)
	}

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlF))

	if len(client.inputCalls) != 0 {
		t.Fatalf("expected picker query input to clear repeated shortcut passthrough, got %#v", client.inputCalls)
	}
}

func TestFeatureKeyDrivenNormalPassthrough(t *testing.T) {
	model := setupModel(t, modelOpts{})
	client := model.runtime.Client().(*recordingBridgeClient)
	assertMode(t, model, input.ModeNormal)

	// Regular key in normal mode should produce terminal input, not action
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("expected terminal input command for 'a' in normal mode")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "a" {
		t.Fatalf("expected passthrough input 'a', got %q", string(client.inputCalls[0].data))
	}
}

func TestFeatureKeyDrivenNormalEnterPassthrough(t *testing.T) {
	model := setupModel(t, modelOpts{})
	client := model.runtime.Client().(*recordingBridgeClient)
	assertMode(t, model, input.ModeNormal)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected terminal input command for enter in normal mode")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "\r" {
		t.Fatalf("expected enter passthrough '\\r', got %q", string(client.inputCalls[0].data))
	}
}

func TestFeatureKeyDrivenNormalCtrlJPassthrough(t *testing.T) {
	model := setupModel(t, modelOpts{})
	client := model.runtime.Client().(*recordingBridgeClient)
	assertMode(t, model, input.ModeNormal)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if cmd == nil {
		t.Fatal("expected terminal input command for Ctrl-J in normal mode")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "\n" {
		t.Fatalf("expected Ctrl-J passthrough '\\n', got %q", string(client.inputCalls[0].data))
	}
}

func TestFeatureKeyDrivenNormalCtrlMPassthrough(t *testing.T) {
	model := setupModel(t, modelOpts{})
	client := model.runtime.Client().(*recordingBridgeClient)
	assertMode(t, model, input.ModeNormal)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlM})
	if cmd == nil {
		t.Fatal("expected terminal input command for Ctrl-M in normal mode")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "\r" {
		t.Fatalf("expected Ctrl-M passthrough '\\r', got %q", string(client.inputCalls[0].data))
	}
}

func TestFeatureKeyDrivenNormalArrowPassthrough(t *testing.T) {
	model := setupModel(t, modelOpts{})
	client := model.runtime.Client().(*recordingBridgeClient)
	assertMode(t, model, input.ModeNormal)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd == nil {
		t.Fatal("expected terminal input command for down in normal mode")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "\x1b[B" {
		t.Fatalf("expected down-arrow passthrough '\\x1b[B', got %q", string(client.inputCalls[0].data))
	}
}

// ─── Group 16: Edge cases ──────────────────────────────────────────────────

func TestFeatureCloseLastTabLeavesWorkspace(t *testing.T) {
	model := setupModel(t, modelOpts{})
	assertTabCount(t, model, 1)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCloseTab})

	ws := model.workbench.CurrentWorkspace()
	if ws == nil {
		t.Fatal("expected workspace to still exist after closing last tab")
	}
	if len(ws.Tabs) != 0 {
		t.Fatalf("expected 0 tabs after close, got %d", len(ws.Tabs))
	}
}

func TestFeatureMultipleWorkspacesWithTabs(t *testing.T) {
	model := setupModel(t, modelOpts{})

	// Create second workspace
	createWorkspaceViaPrompt(t, model, "dev")
	secondWsName := model.workbench.CurrentWorkspace().Name
	// Create a tab in second workspace
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCreateTab})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})

	// Switch to main
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSwitchWorkspace, Text: "main"})
	assertTabCount(t, model, 1)

	// Switch back to second
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSwitchWorkspace, Text: secondWsName})

	ws := model.workbench.CurrentWorkspace()
	if ws == nil || ws.Name != secondWsName {
		t.Fatal("expected to switch back to second workspace")
	}
}

func TestFeatureTerminalInputOnUnboundPaneOpensPicker(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "empty"}, // no terminal
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	client := &recordingBridgeClient{
		listResult:         &protocol.ListResult{},
		attachResult:       &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := New(shared.Config{}, wb, runtime.New(client))
	model.width = 120
	model.height = 40

	cmd := model.handleTerminalInput(input.TerminalInput{PaneID: "pane-1", Data: []byte("a")})
	drainCmd(t, model, cmd, 10)

	assertMode(t, model, input.ModePicker)
}

func TestFeatureQuestionMarkPassesThroughInNormal(t *testing.T) {
	model := setupModel(t, modelOpts{})
	client := model.runtime.Client().(*recordingBridgeClient)

	_, cmd := model.Update(runeKeyMsg('?'))
	if cmd == nil {
		t.Fatal("expected queued terminal input command for '?' in normal mode")
	}
	drainCmd(t, model, cmd, 10)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one input call, got %#v", client.inputCalls)
	}
	if string(client.inputCalls[0].data) != "?" {
		t.Fatalf("expected question mark passthrough, got %q", string(client.inputCalls[0].data))
	}
	assertMode(t, model, input.ModeNormal)
}

func TestFeatureSessionTerminalInputDoesNotAcquireLeaseImplicitly(t *testing.T) {
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
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "main", ViewID: "view-remote", PaneID: "pane-remote"},
	}

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	binding1 := model.runtime.BindPane("pane-1")
	binding1.Channel = 1
	binding1.Connected = true
	binding2 := model.runtime.BindPane("pane-2")
	binding2.Channel = 2
	binding2.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	cmd := model.handleTerminalInput(input.TerminalInput{PaneID: "pane-2", Data: []byte("a")})
	drainCmd(t, model, cmd, 20)

	if len(client.acquireLeaseCalls) != 0 {
		t.Fatalf("expected terminal input not to acquire a lease implicitly, got %#v", client.acquireLeaseCalls)
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected follower terminal input not to resize PTY, got %#v", client.resizes)
	}
	if len(client.inputCalls) != 1 || client.inputCalls[0].channel != 2 || string(client.inputCalls[0].data) != "a" {
		t.Fatalf("expected terminal input forwarded without ownership change, got %#v", client.inputCalls)
	}
	if terminal.OwnerPaneID != "pane-remote" {
		t.Fatalf("expected terminal input to preserve global owner, got owner=%q", terminal.OwnerPaneID)
	}
}

func TestFeatureSessionTerminalInputReacquiresLeaseForSameOwnerPane(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
					},
					Root: workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "main", ViewID: "view-remote", PaneID: "pane-1"},
	}

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	binding := model.runtime.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	cmd := model.handleTerminalInput(input.TerminalInput{PaneID: "pane-1", Data: []byte("a")})
	drainCmd(t, model, cmd, 20)

	if len(client.acquireLeaseCalls) != 1 {
		t.Fatalf("expected same-pane terminal input to reacquire lease, got %#v", client.acquireLeaseCalls)
	}
	if got := client.acquireLeaseCalls[0]; got.ViewID != "view-local" || got.PaneID != "pane-1" || got.TerminalID != "term-1" {
		t.Fatalf("unexpected lease acquire params: %#v", got)
	}
	if len(client.resizes) != 1 || client.resizes[0].channel != 1 {
		t.Fatalf("expected same-pane terminal input to resize from pane-1 channel, got %#v", client.resizes)
	}
	if len(client.inputCalls) != 1 || client.inputCalls[0].channel != 1 || string(client.inputCalls[0].data) != "a" {
		t.Fatalf("expected terminal input forwarded after same-pane lease reacquire, got %#v", client.inputCalls)
	}
	if terminal.OwnerPaneID != "pane-1" {
		t.Fatalf("expected same-pane lease reacquire to restore pane-1 control, got %q", terminal.OwnerPaneID)
	}
}

func TestFeatureSessionTerminalInputKeepsFollowerStateWhenSizeMatches(t *testing.T) {
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
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "main", ViewID: "view-remote", PaneID: "pane-remote"},
	}

	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatalf("expected visible active tab, got %#v", visible)
	}
	var target *workbench.VisiblePane
	for i := range visible.Tabs[visible.ActiveTab].Panes {
		pane := &visible.Tabs[visible.ActiveTab].Panes[i]
		if pane.ID == "pane-2" {
			target = pane
			break
		}
	}
	if target == nil {
		t.Fatalf("expected visible pane-2, got %#v", visible.Tabs[visible.ActiveTab].Panes)
	}
	targetContent, ok := paneContentRectForVisible(*target)
	if !ok {
		t.Fatal("expected target pane content rect")
	}
	targetCols := uint16(maxInt(2, targetContent.W))
	targetRows := uint16(maxInt(2, targetContent.H))

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: targetCols, Rows: targetRows},
	}

	binding1 := model.runtime.BindPane("pane-1")
	binding1.Channel = 1
	binding1.Connected = true
	binding2 := model.runtime.BindPane("pane-2")
	binding2.Channel = 2
	binding2.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	cmd := model.handleTerminalInput(input.TerminalInput{PaneID: "pane-2", Data: []byte("a")})
	drainCmd(t, model, cmd, 20)

	if len(client.acquireLeaseCalls) != 0 {
		t.Fatalf("expected matching-size terminal input not to acquire a lease implicitly, got %#v", client.acquireLeaseCalls)
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected matching-size follower input to avoid resize, got %#v", client.resizes)
	}
	if len(client.inputCalls) != 1 || client.inputCalls[0].channel != 2 || string(client.inputCalls[0].data) != "a" {
		t.Fatalf("expected terminal input forwarded without resize, got %#v", client.inputCalls)
	}
	if terminal.OwnerPaneID != "pane-remote" {
		t.Fatalf("expected matching-size input to preserve global owner, got owner=%q", terminal.OwnerPaneID)
	}
}

func TestFeatureSessionTerminalInputReclaimsSamePaneLeaseAndResizesActivePane(t *testing.T) {
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
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "main", ViewID: "view-remote", PaneID: "pane-2"},
	}

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	binding1 := model.runtime.BindPane("pane-1")
	binding1.Channel = 1
	binding1.Connected = true
	binding2 := model.runtime.BindPane("pane-2")
	binding2.Channel = 2
	binding2.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	cmd := model.handleTerminalInput(input.TerminalInput{PaneID: "pane-2", Data: []byte("a")})
	drainCmd(t, model, cmd, 20)

	if len(client.acquireLeaseCalls) != 1 {
		t.Fatalf("expected one implicit lease acquire for the same pane, got %#v", client.acquireLeaseCalls)
	}
	if got := client.acquireLeaseCalls[0]; got.ViewID != "view-local" || got.PaneID != "pane-2" || got.TerminalID != "term-1" {
		t.Fatalf("unexpected lease acquire params: %#v", got)
	}
	if len(client.resizes) != 1 || client.resizes[0].channel != 2 {
		t.Fatalf("expected terminal input to resize from pane-2 channel, got %#v", client.resizes)
	}
	if len(client.inputCalls) != 1 || client.inputCalls[0].channel != 2 || string(client.inputCalls[0].data) != "a" {
		t.Fatalf("expected terminal input forwarded after same-pane lease reclaim, got %#v", client.inputCalls)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 restored as local owner after same-pane lease reclaim, got %q", terminal.OwnerPaneID)
	}
}

func TestFeatureSessionTerminalInputReclaimsSamePaneLeaseForcesResizeWhenSizeMatchesLocally(t *testing.T) {
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
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "main", ViewID: "view-remote", PaneID: "pane-2"},
	}

	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatalf("expected visible active tab, got %#v", visible)
	}
	var target *workbench.VisiblePane
	for i := range visible.Tabs[visible.ActiveTab].Panes {
		pane := &visible.Tabs[visible.ActiveTab].Panes[i]
		if pane.ID == "pane-2" {
			target = pane
			break
		}
	}
	if target == nil {
		t.Fatalf("expected visible pane-2, got %#v", visible.Tabs[visible.ActiveTab].Panes)
	}
	targetContent, ok := paneContentRectForVisible(*target)
	if !ok {
		t.Fatal("expected target pane content rect")
	}
	targetCols := uint16(maxInt(2, targetContent.W))
	targetRows := uint16(maxInt(2, targetContent.H))

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: targetCols, Rows: targetRows},
	}

	binding1 := model.runtime.BindPane("pane-1")
	binding1.Channel = 1
	binding1.Connected = true
	binding2 := model.runtime.BindPane("pane-2")
	binding2.Channel = 2
	binding2.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	cmd := model.handleTerminalInput(input.TerminalInput{PaneID: "pane-2", Data: []byte("a")})
	drainCmd(t, model, cmd, 20)

	if len(client.acquireLeaseCalls) != 1 {
		t.Fatalf("expected one implicit lease acquire for the same pane, got %#v", client.acquireLeaseCalls)
	}
	if len(client.resizes) != 1 || client.resizes[0].channel != 2 {
		t.Fatalf("expected same-pane lease reclaim to force resize despite matching local size, got %#v", client.resizes)
	}
	if len(client.inputCalls) != 1 || client.inputCalls[0].channel != 2 || string(client.inputCalls[0].data) != "a" {
		t.Fatalf("expected terminal input forwarded without resize, got %#v", client.inputCalls)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 restored as local owner after same-pane lease reclaim, got %q", terminal.OwnerPaneID)
	}
}

func TestFeatureWindowResizeReclaimsSamePaneSessionLeaseForActivePane(t *testing.T) {
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
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})
	model.sessionID = "main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "main", ViewID: "view-remote", PaneID: "pane-2"},
	}

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	binding1 := model.runtime.BindPane("pane-1")
	binding1.Channel = 1
	binding1.Connected = true
	binding2 := model.runtime.BindPane("pane-2")
	binding2.Channel = 2
	binding2.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	drainCmd(t, model, cmd, 20)

	if len(client.acquireLeaseCalls) != 1 {
		t.Fatalf("expected window resize to reclaim same-pane lease once, got %#v", client.acquireLeaseCalls)
	}
	if got := client.acquireLeaseCalls[0]; got.ViewID != "view-local" || got.PaneID != "pane-2" || got.TerminalID != "term-1" {
		t.Fatalf("unexpected lease acquire params: %#v", got)
	}
	if len(client.resizes) != 1 || client.resizes[0].channel != 2 {
		t.Fatalf("expected window resize to issue one resize from pane-2 channel, got %#v", client.resizes)
	}
	if terminal.OwnerPaneID != "pane-2" {
		t.Fatalf("expected pane-2 restored as local owner after window resize, got %q", terminal.OwnerPaneID)
	}
}

func TestFeatureTerminalResizeEventReloadsSnapshot(t *testing.T) {
	client := &recordingBridgeClient{
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1", Size: protocol.Size{Cols: 88, Rows: 26}},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.Channel = 1
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 118, Rows: 36}}

	_, cmd := model.Update(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalResized,
		TerminalID: "term-1",
	}})
	drainCmd(t, model, cmd, 10)

	if terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 88 || terminal.Snapshot.Size.Rows != 26 {
		t.Fatalf("expected terminal snapshot reloaded from resize event, got %#v", terminal.Snapshot)
	}
}

func TestFeatureErrorDisplayAndClear(t *testing.T) {
	model := setupModel(t, modelOpts{})
	prevDelay := errorClearDelay
	errorClearDelay = 0
	defer func() { errorClearDelay = prevDelay }()

	_, cmd := model.Update(inputError("test error"))
	if model.err == nil || model.err.Error() != "test error" {
		t.Fatalf("expected stored error, got %v", model.err)
	}

	// Error should be visible in view
	model.render.Invalidate()
	view := xansi.Strip(model.View())
	if !strings.Contains(view, "test error") {
		t.Fatalf("view missing error text:\n%s", view)
	}

	// clearErrorCmd should fire
	if cmd == nil {
		t.Fatal("expected clear error command")
	}

	msg := cmd()
	_, _ = model.Update(msg)
	if model.err != nil {
		t.Fatalf("expected error to clear, got %v", model.err)
	}
}

func TestFeatureStaleErrorClearDoesNotRemoveNewerError(t *testing.T) {
	model := setupModel(t, modelOpts{})
	prevDelay := errorClearDelay
	errorClearDelay = 0
	defer func() { errorClearDelay = prevDelay }()

	_, firstClear := model.Update(inputError("first error"))
	_, _ = model.Update(inputError("second error"))
	if model.err == nil || model.err.Error() != "second error" {
		t.Fatalf("expected latest error to be stored, got %v", model.err)
	}
	if firstClear == nil {
		t.Fatal("expected first clear command")
	}

	_, _ = model.Update(firstClear())
	if model.err == nil || model.err.Error() != "second error" {
		t.Fatalf("expected stale clear to keep newer error, got %v", model.err)
	}
}

func TestFeatureStatePersistenceRoundTrip(t *testing.T) {
	statePath := t.TempDir() + "/workspace-state.json"
	model := setupModel(t, modelOpts{statePath: statePath})

	// Create a second tab
	createSecondTab(t, model)
	assertTabCount(t, model, 2)

	// Save state
	cmd := model.saveStateCmd()
	if cmd != nil {
		_ = cmd()
	}

	// Verify file exists
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state file, got %v", err)
	}
}
