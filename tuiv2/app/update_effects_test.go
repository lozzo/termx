package app

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestModelUpdateOpenPickerSetsModeAndInitializesPickerState(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
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

func TestEffectCmdSwitchTabEffectResizesVisiblePanes(t *testing.T) {
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
	if cmd == nil {
		t.Fatal("expected resize command for switch tab effect")
	}
	if msg := cmd(); msg != nil {
		if err, ok := msg.(error); ok {
			t.Fatalf("unexpected resize error: %v", err)
		}
	}
	if len(client.resizes) != 1 {
		t.Fatalf("expected exactly one resize call, got %d", len(client.resizes))
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

func TestEffectCmdLoadPickerItemsSkipsExitedTerminals(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", State: "running"},
				{ID: "term-2", Name: "done", State: "exited"},
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
		t.Fatalf("expected running terminal plus create row, got %#v", loaded.Items)
	}
	if loaded.Items[0].TerminalID != "term-1" {
		t.Fatalf("expected only running terminal to remain attachable, got %#v", loaded.Items)
	}
	if !loaded.Items[1].CreateNew {
		t.Fatalf("expected final picker row to remain create-new, got %#v", loaded.Items[1])
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
	if !items[2].CreateNew || items[2].Name != "new workspace" {
		t.Fatalf("expected final create-new workspace item, got %#v", items[2])
	}
	if model.modalHost.Session == nil || model.modalHost.Session.Loading || model.modalHost.Session.Phase != modal.ModalPhaseReady {
		t.Fatalf("expected ready session after workspace items load, got %#v", model.modalHost.Session)
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
