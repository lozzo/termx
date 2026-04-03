package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	view := model.View()
	// tab bar contains workspace name and tab name
	for _, want := range []string{"main", "tab 1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
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
	if tab == nil || tab.ActivePaneID != "pane-dev" {
		t.Fatalf("expected restored active pane pane-dev, got %#v", tab)
	}
	if pane := model.workbench.ActivePane(); pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected failed auto-reattach to clear restored binding, got %#v", pane)
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
	model := New(shared.Config{}, wb, runtime.New(nil))
	cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("expected terminal input command")
	}
	msg := cmd()
	inputMsg, ok := msg.(input.TerminalInput)
	if !ok {
		t.Fatalf("expected TerminalInput, got %#v", msg)
	}
	if inputMsg.PaneID != "pane-1" {
		t.Fatalf("expected pane-1 injected, got %q", inputMsg.PaneID)
	}
	if string(inputMsg.Data) != "a" {
		t.Fatalf("expected input data 'a', got %q", inputMsg.Data)
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
		t.Fatal("expected runtime input command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message from terminal input command, got %#v", msg)
	}
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
	model := New(shared.Config{}, wb, rt)

	cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyUp})
	if cmd == nil {
		t.Fatal("expected terminal input command")
	}
	msg := cmd()
	inputMsg, ok := msg.(input.TerminalInput)
	if !ok {
		t.Fatalf("expected TerminalInput, got %#v", msg)
	}
	if string(inputMsg.Data) != "\x1bOA" {
		t.Fatalf("expected application cursor up sequence, got %q", string(inputMsg.Data))
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
	model := New(shared.Config{}, wb, runtime.New(nil))

	cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd == nil {
		t.Fatal("expected terminal input command")
	}
	msg := cmd()
	inputMsg, ok := msg.(input.TerminalInput)
	if !ok {
		t.Fatalf("expected TerminalInput, got %#v", msg)
	}
	if string(inputMsg.Data) != "\x1b[Z" {
		t.Fatalf("expected shift-tab sequence, got %q", string(inputMsg.Data))
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
	view := model.View()
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
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-name" {
		t.Fatalf("expected create-terminal-name prompt, got %#v", model.modalHost.Prompt)
	}
	if model.input.Mode().Kind != input.ModePrompt {
		t.Fatalf("expected input mode prompt, got %q", model.input.Mode().Kind)
	}
}

func TestModelPromptSubmitAdvancesCreateTerminalToTags(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:        "create-terminal-name",
		Title:       "Create Terminal",
		Value:       "demo",
		Original:    "shell",
		DefaultName: "shell",
		PaneID:      "pane-1",
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: "pane-1"})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected local prompt advance without async msg, got %#v", msg)
		}
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-tags" {
		t.Fatalf("expected tags step prompt, got %#v", model.modalHost.Prompt)
	}
	if model.modalHost.Prompt.Name != "demo" {
		t.Fatalf("expected prompt to retain submitted name, got %#v", model.modalHost.Prompt)
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
		Kind:        "create-terminal-tags",
		Title:       "Create Terminal",
		Value:       "role=dev env=test",
		Name:        "demo",
		DefaultName: "shell",
		PaneID:      "pane-1",
		Command:     []string{"/bin/sh"},
		AllowEmpty:  true,
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
	if len(client.setTagsCalls) != 1 || client.setTagsCalls[0].tags["role"] != "dev" || client.setTagsCalls[0].tags["env"] != "test" {
		t.Fatalf("unexpected tag calls: %#v", client.setTagsCalls)
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
		Kind:   "create-terminal-name",
		Title:  "Create Terminal",
		Value:  "de",
		PaneID: "pane-1",
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if got := model.modalHost.Prompt.Value; got != "dem" {
		t.Fatalf("expected prompt value dem after rune input, got %q", got)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := model.modalHost.Prompt.Value; got != "de" {
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
	if got.channel != 7 || got.cols != 118 || got.rows != 36 {
		t.Fatalf("unexpected resize call: %+v", got)
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

	view := model.View()
	for _, want := range []string{"PANE", "h/j/k/l FOCUS", "% VSPLIT", "d DETACH", "r RECONNECT", "X CLOSE+KILL", "w CLOSE", "Esc BACK"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
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
	if pane == nil || pane.TerminalID != "term-restore" {
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
	if pane.TerminalID != "" {
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
	command []string
	name    string
	size    protocol.Size
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

func (c *recordingBridgeClient) Create(_ context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	c.createCalls = append(c.createCalls, createCall{command: append([]string(nil), command...), name: name, size: size})
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
	c.inputCalls = append(c.inputCalls, inputCall{channel: channel, data: append([]byte(nil), data...)})
	return nil
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
