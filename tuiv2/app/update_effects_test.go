package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestModelUpdateOpenPickerSetsModeAndInitializesPickerState(t *testing.T) {
	model := setupModel(t, modelOpts{})
	updated, cmd := model.Update(input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: "req-1"})
	if updated != model {
		t.Fatal("expected model pointer to remain stable")
	}
	if cmd == nil {
		t.Fatal("expected command from open picker action")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %#v", msg)
	}
	if len(batch) != 3 {
		t.Fatalf("expected 3 batched commands, got %d", len(batch))
	}
	first := batch[0]()
	if _, ok := first.(EffectAppliedMsg); !ok {
		t.Fatalf("expected first batched msg to be EffectAppliedMsg, got %#v", first)
	}
	if model.input.Mode().Kind != input.ModePicker {
		t.Fatalf("expected picker mode, got %q", model.input.Mode().Kind)
	}
	if model.modalHost == nil || model.modalHost.Session == nil {
		t.Fatal("expected modal session to be initialized")
	}
	if model.modalHost.Picker == nil {
		t.Fatal("expected picker state to be initialized")
	}
}

func TestModelUpdateEffectAppliedMsgAppliesPickerSideState(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))

	updated, cmd := model.Update(EffectAppliedMsg{Effect: orchestrator.OpenPickerEffect{RequestID: "req-2"}})
	if updated != model {
		t.Fatal("expected model pointer to remain stable")
	}
	if cmd != nil {
		t.Fatalf("expected no command when applying side state, got %#v", cmd)
	}
	if model.modalHost == nil || model.modalHost.Session == nil {
		t.Fatal("expected picker session after side-state application")
	}
	if model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session kind, got %q", model.modalHost.Session.Kind)
	}
	if !model.modalHost.Session.Loading || model.modalHost.Session.Phase != modal.ModalPhaseLoading {
		t.Fatalf("expected loading picker session, got %#v", model.modalHost.Session)
	}
	if model.modalHost.Picker == nil {
		t.Fatal("expected picker state initialized")
	}
}

func TestModelUpdateEffectAppliedMsgAppliesWorkspacePickerSideState(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))

	updated, cmd := model.Update(EffectAppliedMsg{Effect: orchestrator.OpenWorkspacePickerEffect{RequestID: "ws-req"}})
	if updated != model {
		t.Fatal("expected model pointer to remain stable")
	}
	if cmd != nil {
		t.Fatalf("expected no command when applying side state, got %#v", cmd)
	}
	if model.modalHost == nil || model.modalHost.Session == nil {
		t.Fatal("expected workspace picker session after side-state application")
	}
	if model.modalHost.Session.Kind != input.ModeWorkspacePicker {
		t.Fatalf("expected workspace picker session kind, got %q", model.modalHost.Session.Kind)
	}
	if !model.modalHost.Session.Loading || model.modalHost.Session.Phase != modal.ModalPhaseLoading {
		t.Fatalf("expected loading workspace picker session, got %#v", model.modalHost.Session)
	}
	if model.modalHost.WorkspacePicker == nil {
		t.Fatal("expected workspace picker state initialized")
	}
}

func TestEffectCmdSwitchTabEffectDoesNotResizeVisiblePanes(t *testing.T) {
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
	model.width = 120
	model.height = 40

	cmd := model.effectCmd(orchestrator.SwitchTabEffect{Delta: 1})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if err, ok := msg.(error); ok {
				t.Fatalf("unexpected switch-tab error: %v", err)
			}
		}
	}
	if len(client.resizes) != 0 {
		t.Fatalf("expected switch tab effect not to resize panes, got %d", len(client.resizes))
	}
}

func TestEffectCmdLoadPickerItemsAppendsCreateNewItem(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
			},
		},
	}
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(client))

	cmd := model.effectCmd(orchestrator.LoadPickerItemsEffect{})
	if cmd == nil {
		t.Fatal("expected picker load command")
	}
	msg := cmd()
	loaded, ok := msg.(pickerItemsLoadedMsg)
	if !ok {
		t.Fatalf("expected pickerItemsLoadedMsg, got %#v", msg)
	}
	if len(loaded.Items) != 2 {
		t.Fatalf("expected 2 picker items including create row, got %d", len(loaded.Items))
	}
	last := loaded.Items[len(loaded.Items)-1]
	if !last.CreateNew {
		t.Fatalf("expected final picker item to be create-new entry, got %#v", last)
	}
	if last.Name != "new terminal" {
		t.Fatalf("expected create row name new terminal, got %q", last.Name)
	}
}

func TestEffectCmdLoadPickerItemsIncludesExitedTerminals(t *testing.T) {
	exited := 23
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "done", State: "exited", ExitCode: &exited},
			},
		},
	}
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(client))

	cmd := model.effectCmd(orchestrator.LoadPickerItemsEffect{})
	if cmd == nil {
		t.Fatal("expected picker load command")
	}
	msg := cmd()
	loaded, ok := msg.(pickerItemsLoadedMsg)
	if !ok {
		t.Fatalf("expected pickerItemsLoadedMsg, got %#v", msg)
	}
	if len(loaded.Items) != 3 {
		t.Fatalf("expected running terminal, exited terminal, and create row, got %#v", loaded.Items)
	}
	if loaded.Items[0].TerminalID != "term-1" || loaded.Items[0].TerminalState != "running" {
		t.Fatalf("expected running terminal metadata, got %#v", loaded.Items[0])
	}
	if loaded.Items[1].TerminalID != "term-2" || loaded.Items[1].TerminalState != "exited" {
		t.Fatalf("expected exited terminal to remain selectable, got %#v", loaded.Items[1])
	}
	if loaded.Items[1].ExitCode == nil || *loaded.Items[1].ExitCode != exited {
		t.Fatalf("expected exited terminal exit code metadata, got %#v", loaded.Items[1])
	}
	if !loaded.Items[2].CreateNew {
		t.Fatalf("expected final picker row to remain create-new, got %#v", loaded.Items[2])
	}
}

func TestEffectCmdLoadPickerItemsIgnoresRegistryOnlyMetadataWhenListSucceeds(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
			},
		},
	}
	rt := runtime.New(client)
	stale := rt.Registry().GetOrCreate("term-stale")
	stale.Name = "stale"
	stale.State = "running"

	model := New(shared.Config{}, workbench.NewWorkbench(), rt)

	cmd := model.effectCmd(orchestrator.LoadPickerItemsEffect{})
	if cmd == nil {
		t.Fatal("expected picker load command")
	}
	msg := cmd()
	loaded, ok := msg.(pickerItemsLoadedMsg)
	if !ok {
		t.Fatalf("expected pickerItemsLoadedMsg, got %#v", msg)
	}
	if len(loaded.Items) != 2 {
		t.Fatalf("expected listed terminal plus create row, got %#v", loaded.Items)
	}
	if loaded.Items[0].TerminalID != "term-1" {
		t.Fatalf("expected picker to use server-listed terminal, got %#v", loaded.Items)
	}
	if !loaded.Items[1].CreateNew {
		t.Fatalf("expected final picker row to remain create-new, got %#v", loaded.Items[1])
	}
}

func TestEffectCmdAttachTerminalDelegatesToAttachService(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "viewer"},
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
	model := setupModel(t, modelOpts{client: client})

	cmd := model.effectCmd(orchestrator.AttachTerminalEffect{PaneID: "pane-1", TerminalID: "term-1", Mode: "viewer"})
	if cmd == nil {
		t.Fatal("expected attach effect command")
	}
	drainCmd(t, model, cmd, 20)

	if len(client.attachCalls) != 1 {
		t.Fatalf("expected one attach call, got %#v", client.attachCalls)
	}
	if client.attachCalls[0].terminalID != "term-1" || client.attachCalls[0].mode != "viewer" {
		t.Fatalf("expected attach effect to preserve request mode, got %#v", client.attachCalls[0])
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" {
		t.Fatalf("expected attach effect to bind pane, got %#v", pane)
	}
}

func TestEffectCmdLoadWorkspaceItemsPopulatesWorkspacePicker(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{Name: "main"})
	wb.AddWorkspace("dev", &workbench.WorkspaceState{Name: "dev"})
	model := New(shared.Config{}, wb, runtime.New(nil))
	model.modalHost.Open(input.ModeWorkspacePicker, "workspace-picker-1")
	model.modalHost.WorkspacePicker = &modal.WorkspacePickerState{}

	cmd := model.effectCmd(orchestrator.LoadWorkspaceItemsEffect{})
	if cmd == nil {
		t.Fatal("expected workspace picker load command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message from workspace load command, got %#v", msg)
	}
	if model.modalHost.WorkspacePicker == nil {
		t.Fatal("expected workspace picker state")
	}
	items := model.modalHost.WorkspacePicker.VisibleItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 workspace picker items, got %d", len(items))
	}
	if items[0].Name != "main" || items[1].Name != "dev" {
		t.Fatalf("unexpected workspace order: %#v", items)
	}
	if !items[0].Current {
		t.Fatalf("expected current workspace marker on first row, got %#v", items[0])
	}
	if !items[2].CreateNew || items[2].Name != "New workspace" {
		t.Fatalf("expected final create-new workspace item, got %#v", items[2])
	}
	if model.modalHost.Session == nil || model.modalHost.Session.Loading || model.modalHost.Session.Phase != modal.ModalPhaseReady {
		t.Fatalf("expected ready session after workspace items load, got %#v", model.modalHost.Session)
	}
}

func TestWorkspacePickerItemsOrdersRootFloatingAndDetachedPanes(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "backend",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "logs", TerminalID: "term-2"},
				"pane-3": {ID: "pane-3", Title: "float", TerminalID: "term-3"},
				"pane-4": {ID: "pane-4", Title: "orphan", TerminalID: "term-4"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
			Floating: []*workbench.FloatingState{
				{PaneID: "pane-3"},
			},
		}},
	})
	model := New(shared.Config{}, wb, runtime.New(nil))

	items := model.workspacePickerItems()
	if len(items) != 6 {
		t.Fatalf("expected workspace, tab, four panes, got %#v", items)
	}
	if items[0].Kind != modal.WorkspacePickerItemWorkspace || items[0].Name != "main" {
		t.Fatalf("expected workspace row first, got %#v", items[0])
	}
	if items[1].Kind != modal.WorkspacePickerItemTab || items[1].Name != "backend" {
		t.Fatalf("expected tab row second, got %#v", items[1])
	}
	paneIDs := []string{items[2].PaneID, items[3].PaneID, items[4].PaneID, items[5].PaneID}
	if want := []string{"pane-1", "pane-2", "pane-3", "pane-4"}; strings.Join(paneIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected pane order %v want %v", paneIDs, want)
	}
	if items[4].PaneID != "pane-3" || !items[4].Floating {
		t.Fatalf("expected floating pane metadata on pane-3 row, got %#v", items[4])
	}
	if items[5].PaneID != "pane-4" || items[5].Floating {
		t.Fatalf("expected detached pane to appear last without floating marker, got %#v", items[5])
	}
}

func TestWorkspacePickerItemsIncludeRuntimeRoleAndTerminalMetadata(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "backend",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Name = "shell"
	terminal.State = "exited"
	binding := rt.BindPane("pane-1")
	binding.Role = runtime.BindingRoleOwner

	model := New(shared.Config{}, wb, rt)
	items := model.workspacePickerItems()
	if len(items) != 3 {
		t.Fatalf("expected workspace, tab, pane rows, got %#v", items)
	}
	pane := items[2]
	if pane.Kind != modal.WorkspacePickerItemPane {
		t.Fatalf("expected pane item, got %#v", pane)
	}
	if pane.Name != "shell" {
		t.Fatalf("expected terminal name fallback for untitled pane, got %#v", pane)
	}
	if pane.State != "exited" {
		t.Fatalf("expected terminal state from runtime registry, got %#v", pane)
	}
	if pane.Role != string(runtime.BindingRoleOwner) {
		t.Fatalf("expected bound runtime role, got %#v", pane)
	}
}

func TestApplyEffectsBatchesOnlyExecutableCommands(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))

	cmd := model.applyEffects([]orchestrator.Effect{
		orchestrator.InvalidateRenderEffect{},
		orchestrator.SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeHelp, RequestID: "batch-req"}},
		orchestrator.OpenPickerEffect{RequestID: "picker-batch"},
	})
	if cmd == nil {
		t.Fatal("expected batched command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %#v", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("expected two executable commands in batch, got %d", len(batch))
	}
	first, ok := batch[0]().(EffectAppliedMsg)
	if !ok {
		t.Fatalf("expected EffectAppliedMsg from first batch command, got %#v", batch[0]())
	}
	if _, ok := first.Effect.(orchestrator.SetInputModeEffect); !ok {
		t.Fatalf("expected SetInputModeEffect payload, got %#v", first.Effect)
	}
	second, ok := batch[1]().(EffectAppliedMsg)
	if !ok {
		t.Fatalf("expected EffectAppliedMsg from second batch command, got %#v", batch[1]())
	}
	if _, ok := second.Effect.(orchestrator.OpenPickerEffect); !ok {
		t.Fatalf("expected OpenPickerEffect payload, got %#v", second.Effect)
	}
}

func TestEffectCmdSetInputModeEffectInvalidatesCachedFrame(t *testing.T) {
	model := setupModel(t, modelOpts{width: 200})
	model.input.SetMode(input.ModeState{Kind: input.ModePane})
	model.render.Invalidate()
	if view := xansi.Strip(model.View()); !strings.Contains(view, "PANE") {
		t.Fatalf("expected initial pane hints in view:\n%s", view)
	}

	cmd := model.effectCmd(orchestrator.SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeGlobal}})
	if cmd == nil {
		t.Fatal("expected set-input-mode command")
	}
	msg := cmd()
	if _, ok := msg.(EffectAppliedMsg); !ok {
		t.Fatalf("expected EffectAppliedMsg, got %#v", msg)
	}
	if _, next := model.Update(msg); next != nil {
		t.Fatalf("expected no follow-up command, got %#v", next)
	}

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "GLOBAL") {
		t.Fatalf("expected global hints after set-input-mode effect:\n%s", view)
	}
	if strings.Contains(view, "PANE") {
		t.Fatalf("expected cached pane hints to be invalidated:\n%s", view)
	}
}

func TestEffectCmdCreateTabEffectSavesState(t *testing.T) {
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
		}, {
			ID:           "tab-2",
			Name:         "tab 2",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-2": {ID: "pane-2", Title: "logs"},
			},
			Root: workbench.NewLeaf("pane-2"),
		}},
	})
	model := New(shared.Config{WorkspaceStatePath: statePath}, wb, runtime.New(nil))

	cmd := model.effectCmd(orchestrator.CreateTabEffect{})
	if cmd == nil {
		t.Fatal("expected save command batch for create tab effect")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message from create tab save command, got %#v", msg)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	file, err := persist.Load(data)
	if err != nil {
		t.Fatalf("persist.Load: %v", err)
	}
	if len(file.Data) != 1 || len(file.Data[0].Tabs) != 2 {
		t.Fatalf("expected saved workspace/tabs, got %#v", file.Data)
	}
}

func TestEffectCmdKillTerminalEffectInvokesBridgeClient(t *testing.T) {
	client := &recordingBridgeClient{}
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(client))

	cmd := model.effectCmd(orchestrator.KillTerminalEffect{TerminalID: "term-9"})
	if cmd == nil {
		t.Fatal("expected kill terminal command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message from kill command, got %#v", msg)
	}
	if len(client.killCalls) != 1 {
		t.Fatalf("expected exactly one kill call, got %d", len(client.killCalls))
	}
	if client.killCalls[0] != "term-9" {
		t.Fatalf("expected kill terminal term-9, got %#v", client.killCalls)
	}
}
