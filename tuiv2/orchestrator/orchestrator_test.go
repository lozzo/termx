package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func newTestOrchestrator(t *testing.T) (*Orchestrator, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not stop in time")
		}
	})

	var transport *unixtransport.Transport
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		transport, err = unixtransport.Dial(socketPath)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := protocol.NewClient(transport)
	t.Cleanup(func() { _ = client.Close() })

	helloCtx, helloCancel := context.WithTimeout(ctx, 2*time.Second)
	defer helloCancel()
	if err := client.Hello(helloCtx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	rt := runtime.New(bridge.NewProtocolClient(client))
	wb := workbench.NewWorkbench()
	mh := modal.NewHost()
	return New(wb, rt, mh), ctx
}

func TestHandleSemanticActionOpenPicker(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:     input.ActionOpenPicker,
		TargetID: "req-1",
	})
	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}
	if orch.modalHost.Session == nil || orch.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session, got %#v", orch.modalHost.Session)
	}
}

func TestHandleSemanticActionOpenWorkspacePicker(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	orch.workbench.AddWorkspace("main", &workbench.WorkspaceState{Name: "main"})
	orch.workbench.AddWorkspace("dev", &workbench.WorkspaceState{Name: "dev"})

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:     input.ActionOpenWorkspacePicker,
		TargetID: "workspace-picker-1",
	})
	if len(effects) != 3 {
		t.Fatalf("expected 3 effects, got %d", len(effects))
	}
	if orch.modalHost.Session == nil || orch.modalHost.Session.Kind != input.ModeWorkspacePicker {
		t.Fatalf("expected workspace picker session, got %#v", orch.modalHost.Session)
	}
	if orch.modalHost.WorkspacePicker == nil {
		t.Fatal("expected workspace picker state to be initialized")
	}
}

func TestHandleSemanticActionSwitchWorkspace(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-main", "pane-main")
	seedTabWithSinglePane(orch.workbench, "dev", "tab-dev", "pane-dev")
	orch.modalHost.Open(input.ModeWorkspacePicker, "workspace-picker-1")
	orch.modalHost.WorkspacePicker = &modal.WorkspacePickerState{}

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind: input.ActionSwitchWorkspace,
		Text: "dev",
	})
	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}
	if current := orch.workbench.CurrentWorkspace(); current == nil || current.Name != "dev" {
		t.Fatalf("expected current workspace dev, got %#v", current)
	}
	if orch.modalHost.Session != nil {
		t.Fatalf("expected workspace picker modal to close, got %#v", orch.modalHost.Session)
	}
}

func TestHandleSemanticActionCreateWorkspaceClosesWorkspacePicker(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-main", "pane-main")
	orch.modalHost.Open(input.ModeWorkspacePicker, "workspace-picker-1")
	orch.modalHost.WorkspacePicker = &modal.WorkspacePickerState{}

	effects := orch.HandleSemanticAction(input.SemanticAction{Kind: input.ActionCreateWorkspace})
	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}
	current := orch.workbench.CurrentWorkspace()
	if current == nil || current.Name == "" || current.Name == "main" {
		t.Fatalf("expected newly created workspace to become current, got %#v", current)
	}
	if orch.modalHost.Session != nil {
		t.Fatalf("expected workspace picker modal to close after create, got %#v", orch.modalHost.Session)
	}
}

func TestHandleSemanticActionZoomPaneTogglesCurrentPane(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")

	effects := orch.HandleSemanticAction(input.SemanticAction{Kind: input.ActionZoomPane, PaneID: "pane-1"})
	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	tab := orch.workbench.CurrentTab()
	if tab == nil || tab.ZoomedPaneID != "pane-1" {
		t.Fatalf("expected pane-1 to be zoomed, got %#v", tab)
	}

	_ = orch.HandleSemanticAction(input.SemanticAction{Kind: input.ActionZoomPane, PaneID: "pane-1"})
	if tab.ZoomedPaneID != "" {
		t.Fatalf("expected zoom toggle off, got %q", tab.ZoomedPaneID)
	}
}

func TestAttachAndLoadSnapshot(t *testing.T) {
	orch, ctx := newTestOrchestrator(t)

	created, err := orch.runtime.Registry(), error(nil)
	_ = created
	result, err := orch.runtime.ListTerminals(ctx)
	if err != nil {
		t.Fatalf("list terminals: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty terminal list, got %d", len(result))
	}

	createdTerm, err := orch.runtimeClientCreate(ctx, []string{"sh"}, "demo")
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	msgs, err := orch.AttachAndLoadSnapshot(ctx, "pane-1", createdTerm.TerminalID, "collaborator", 0, 10)
	if err != nil {
		t.Fatalf("attach and load snapshot: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 msgs, got %d", len(msgs))
	}
}

func TestAttachAndLoadSnapshotWritesWorkbenchStructuralBinding(t *testing.T) {
	orch, ctx := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")

	createdTerm, err := orch.runtimeClientCreate(ctx, []string{"sh"}, "demo")
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	if _, err := orch.AttachAndLoadSnapshot(ctx, "pane-1", createdTerm.TerminalID, "collaborator", 0, 10); err != nil {
		t.Fatalf("attach and load snapshot: %v", err)
	}

	pane := orch.workbench.ActivePane()
	if pane == nil || pane.TerminalID != createdTerm.TerminalID {
		t.Fatalf("expected orchestrator attach to write structural binding, got %#v", pane)
	}
}

func (o *Orchestrator) runtimeClientCreate(ctx context.Context, command []string, name string) (*protocol.CreateResult, error) {
	return o.runtimeClient().Create(ctx, command, name, protocol.Size{Cols: 80, Rows: 24})
}

func (o *Orchestrator) runtimeClient() bridge.Client {
	return o.runtimeClientUnsafe()
}

func (o *Orchestrator) runtimeClientUnsafe() bridge.Client {
	return o.runtimeClientField()
}

func (o *Orchestrator) runtimeClientField() bridge.Client {
	return o.runtimeClientFromRuntime()
}

func (o *Orchestrator) runtimeClientFromRuntime() bridge.Client {
	return o.runtimeClientAccessor()
}

func (o *Orchestrator) runtimeClientAccessor() bridge.Client {
	return o.runtimeBridgeClient()
}

func (o *Orchestrator) runtimeBridgeClient() bridge.Client {
	return o.runtimeClientDirect()
}

func (o *Orchestrator) runtimeClientDirect() bridge.Client {
	return o.runtimeTestClient()
}

func (o *Orchestrator) runtimeTestClient() bridge.Client {
	return o.runtimeClientValue()
}

func (o *Orchestrator) runtimeClientValue() bridge.Client {
	return o.runtimeInternalClient()
}

func (o *Orchestrator) runtimeInternalClient() bridge.Client {
	return o.runtimeExposeClient()
}

func (o *Orchestrator) runtimeExposeClient() bridge.Client {
	return o.runtimeVisibleClient()
}

func (o *Orchestrator) runtimeVisibleClient() bridge.Client {
	return o.runtime.Client()
}

// TestHandleSemanticActionOpenPickerEffects 验证 ActionOpenPicker 产出
// OpenPickerEffect 和 SetInputModeEffect，且 modalHost.Session 正确初始化。
func TestHandleSemanticActionOpenPickerEffects(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:     input.ActionOpenPicker,
		TargetID: "req-picker-1",
	})

	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}

	var hasOpenPicker, hasSetInputMode bool
	for _, e := range effects {
		switch eff := e.(type) {
		case OpenPickerEffect:
			if eff.RequestID != "req-picker-1" {
				t.Errorf("OpenPickerEffect.RequestID: got %q, want %q", eff.RequestID, "req-picker-1")
			}
			hasOpenPicker = true
		case SetInputModeEffect:
			if eff.Mode.Kind != input.ModePicker {
				t.Errorf("SetInputModeEffect.Mode.Kind: got %q, want %q", eff.Mode.Kind, input.ModePicker)
			}
			if eff.Mode.RequestID != "req-picker-1" {
				t.Errorf("SetInputModeEffect.Mode.RequestID: got %q, want %q", eff.Mode.RequestID, "req-picker-1")
			}
			hasSetInputMode = true
		}
	}

	if !hasOpenPicker {
		t.Error("expected OpenPickerEffect in effects")
	}
	if !hasSetInputMode {
		t.Error("expected SetInputModeEffect in effects")
	}

	if orch.modalHost.Session == nil {
		t.Fatal("expected non-nil Session after ActionOpenPicker")
	}
	if orch.modalHost.Session.Kind != input.ModePicker {
		t.Errorf("Session.Kind: got %q, want %q", orch.modalHost.Session.Kind, input.ModePicker)
	}
	if orch.modalHost.Session.RequestID != "req-picker-1" {
		t.Errorf("Session.RequestID: got %q, want %q", orch.modalHost.Session.RequestID, "req-picker-1")
	}
}

// TestHandleSemanticActionSubmitPromptProducesAttachEffect 验证 ActionSubmitPrompt
// 产出 AttachTerminalEffect，携带正确的 PaneID、TerminalID 和 Mode。
func TestHandleSemanticActionSubmitPromptProducesAttachEffect(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:     input.ActionSubmitPrompt,
		PaneID:   "pane-42",
		TargetID: "term-99",
	})

	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}

	eff, ok := effects[0].(AttachTerminalEffect)
	if !ok {
		t.Fatalf("expected AttachTerminalEffect, got %T", effects[0])
	}
	if eff.PaneID != "pane-42" {
		t.Errorf("AttachTerminalEffect.PaneID: got %q, want %q", eff.PaneID, "pane-42")
	}
	if eff.TerminalID != "term-99" {
		t.Errorf("AttachTerminalEffect.TerminalID: got %q, want %q", eff.TerminalID, "term-99")
	}
	if eff.Mode != "collaborator" {
		t.Errorf("AttachTerminalEffect.Mode: got %q, want %q", eff.Mode, "collaborator")
	}
}

// TestHandleSemanticActionSubmitPromptEmptyTargetID 验证空 TargetID 也能产出 effect
// （上层负责校验 ID 合法性，orchestrator 不做截断）。
func TestHandleSemanticActionSubmitPromptEmptyTargetID(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:   input.ActionSubmitPrompt,
		PaneID: "pane-1",
		// TargetID 故意留空
	})

	if len(effects) != 1 {
		t.Fatalf("expected 1 effect even with empty TargetID, got %d", len(effects))
	}
	eff, ok := effects[0].(AttachTerminalEffect)
	if !ok {
		t.Fatalf("expected AttachTerminalEffect, got %T", effects[0])
	}
	if eff.TerminalID != "" {
		t.Errorf("expected empty TerminalID, got %q", eff.TerminalID)
	}
}

// TestHandleSemanticActionUnknown 验证未知 ActionKind 返回 nil。
func TestHandleSemanticActionUnknown(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind: "totally-unknown-action",
	})

	if effects != nil {
		t.Errorf("expected nil effects for unknown action, got %v", effects)
	}
}

func TestHandleSemanticActionFocusPaneMovesToNeighbor(t *testing.T) {
	t.Run("vertical split left and right", func(t *testing.T) {
		orch, _ := newTestOrchestrator(t)
		seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")
		if err := orch.workbench.SplitPane("tab-1", "pane-1", "pane-2", workbench.SplitVertical); err != nil {
			t.Fatalf("SplitPane: %v", err)
		}

		effects := orch.HandleSemanticAction(input.SemanticAction{
			Kind:   input.ActionFocusPaneLeft,
			PaneID: "pane-2",
		})
		if len(effects) != 1 {
			t.Fatalf("expected 1 effect, got %d", len(effects))
		}
		if _, ok := effects[0].(InvalidateRenderEffect); !ok {
			t.Fatalf("expected InvalidateRenderEffect, got %T", effects[0])
		}
		if got := orch.workbench.CurrentTab().ActivePaneID; got != "pane-1" {
			t.Fatalf("expected active pane pane-1 after moving left, got %q", got)
		}

		effects = orch.HandleSemanticAction(input.SemanticAction{
			Kind:   input.ActionFocusPaneRight,
			PaneID: "pane-1",
		})
		if len(effects) != 1 {
			t.Fatalf("expected 1 effect, got %d", len(effects))
		}
		if got := orch.workbench.CurrentTab().ActivePaneID; got != "pane-2" {
			t.Fatalf("expected active pane pane-2 after moving right, got %q", got)
		}
	})

	t.Run("horizontal split up and down", func(t *testing.T) {
		orch, _ := newTestOrchestrator(t)
		seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")
		if err := orch.workbench.SplitPane("tab-1", "pane-1", "pane-2", workbench.SplitHorizontal); err != nil {
			t.Fatalf("SplitPane: %v", err)
		}

		effects := orch.HandleSemanticAction(input.SemanticAction{
			Kind:   input.ActionFocusPaneUp,
			PaneID: "pane-2",
		})
		if len(effects) != 1 {
			t.Fatalf("expected 1 effect, got %d", len(effects))
		}
		if _, ok := effects[0].(InvalidateRenderEffect); !ok {
			t.Fatalf("expected InvalidateRenderEffect, got %T", effects[0])
		}
		if got := orch.workbench.CurrentTab().ActivePaneID; got != "pane-1" {
			t.Fatalf("expected active pane pane-1 after moving up, got %q", got)
		}

		effects = orch.HandleSemanticAction(input.SemanticAction{
			Kind:   input.ActionFocusPaneDown,
			PaneID: "pane-1",
		})
		if len(effects) != 1 {
			t.Fatalf("expected 1 effect, got %d", len(effects))
		}
		if got := orch.workbench.CurrentTab().ActivePaneID; got != "pane-2" {
			t.Fatalf("expected active pane pane-2 after moving down, got %q", got)
		}
	})
}

func TestHandleSemanticActionClosePaneRemovesPaneAndInvalidates(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")
	if err := orch.workbench.SplitPane("tab-1", "pane-1", "pane-2", workbench.SplitVertical); err != nil {
		t.Fatalf("SplitPane: %v", err)
	}

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:   input.ActionClosePane,
		PaneID: "pane-2",
	})

	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	if _, ok := effects[0].(InvalidateRenderEffect); !ok {
		t.Fatalf("expected InvalidateRenderEffect, got %T", effects[0])
	}
	tab := orch.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab to remain")
	}
	if len(tab.Panes) != 1 {
		t.Fatalf("expected 1 pane after close, got %d", len(tab.Panes))
	}
	if got := tab.ActivePaneID; got != "pane-1" {
		t.Fatalf("expected active pane pane-1 after close, got %q", got)
	}
}

func TestHandleSemanticActionSplitPaneCreatesNewPaneAndOpensPicker(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:   input.ActionSplitPane,
		PaneID: "pane-1",
	})

	tab := orch.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if len(tab.Panes) != 2 {
		t.Fatalf("expected 2 panes after split, got %d", len(tab.Panes))
	}
	if tab.Root == nil || tab.Root.Direction != workbench.SplitVertical {
		t.Fatalf("expected vertical split root, got %#v", tab.Root)
	}
	if tab.ActivePaneID == "" || tab.ActivePaneID == "pane-1" {
		t.Fatalf("expected new active pane after split, got %q", tab.ActivePaneID)
	}
	if !strings.HasPrefix(tab.ActivePaneID, "pane-") {
		t.Fatalf("expected generated pane ID prefix pane-, got %q", tab.ActivePaneID)
	}
	if orch.modalHost.Session == nil || orch.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session, got %#v", orch.modalHost.Session)
	}

	if len(effects) != 3 {
		t.Fatalf("expected 3 effects, got %d", len(effects))
	}
	var hasInvalidate bool
	var openPicker OpenPickerEffect
	var setMode SetInputModeEffect
	for _, effect := range effects {
		switch typed := effect.(type) {
		case InvalidateRenderEffect:
			hasInvalidate = true
		case OpenPickerEffect:
			openPicker = typed
		case SetInputModeEffect:
			setMode = typed
		}
	}
	if !hasInvalidate {
		t.Fatal("expected InvalidateRenderEffect")
	}
	if openPicker.RequestID != tab.ActivePaneID {
		t.Fatalf("expected OpenPickerEffect.RequestID=%q, got %q", tab.ActivePaneID, openPicker.RequestID)
	}
	if setMode.Mode.Kind != input.ModePicker || setMode.Mode.RequestID != tab.ActivePaneID {
		t.Fatalf("unexpected SetInputModeEffect: %#v", setMode)
	}
}

func TestHandleSemanticActionCreateTabCreatesPaneAndOpensPicker(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")

	effects := orch.HandleSemanticAction(input.SemanticAction{Kind: input.ActionCreateTab})

	ws := orch.workbench.CurrentWorkspace()
	if ws == nil {
		t.Fatal("expected current workspace")
	}
	if len(ws.Tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(ws.Tabs))
	}
	tab := ws.Tabs[1]
	if tab == nil {
		t.Fatal("expected new tab at index 1")
	}
	if !strings.HasPrefix(tab.ID, "tab-") {
		t.Fatalf("expected generated tab ID to start with tab-, got %q", tab.ID)
	}
	if tab.Name != "2" {
		t.Fatalf("expected generated tab name 2, got %q", tab.Name)
	}
	if tab.ActivePaneID == "" {
		t.Fatal("expected new tab to have an active pane")
	}
	if len(tab.Panes) != 1 {
		t.Fatalf("expected new tab to start with 1 pane, got %d", len(tab.Panes))
	}
	if _, ok := tab.Panes[tab.ActivePaneID]; !ok {
		t.Fatalf("expected active pane %q in pane map", tab.ActivePaneID)
	}
	if !strings.HasPrefix(tab.ActivePaneID, "pane-") {
		t.Fatalf("expected generated pane ID to start with pane-, got %q", tab.ActivePaneID)
	}
	if ws.ActiveTab != 1 {
		t.Fatalf("expected workspace active tab index 1, got %d", ws.ActiveTab)
	}
	if orch.modalHost.Session == nil || orch.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session, got %#v", orch.modalHost.Session)
	}

	if len(effects) != 3 {
		t.Fatalf("expected 3 effects, got %d", len(effects))
	}
	var hasInvalidate bool
	var openPicker OpenPickerEffect
	var setMode SetInputModeEffect
	for _, effect := range effects {
		switch typed := effect.(type) {
		case InvalidateRenderEffect:
			hasInvalidate = true
		case OpenPickerEffect:
			openPicker = typed
		case SetInputModeEffect:
			setMode = typed
		}
	}
	if !hasInvalidate {
		t.Fatal("expected InvalidateRenderEffect")
	}
	if openPicker.RequestID != tab.ActivePaneID {
		t.Fatalf("expected OpenPickerEffect.RequestID=%q, got %q", tab.ActivePaneID, openPicker.RequestID)
	}
	if setMode.Mode.Kind != input.ModePicker || setMode.Mode.RequestID != tab.ActivePaneID {
		t.Fatalf("unexpected SetInputModeEffect: %#v", setMode)
	}
}

func TestHandleSemanticActionSwitchesTabsAndWraps(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")
	if err := orch.workbench.CreateTab("main", "tab-2", "Tab Two"); err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	if err := orch.workbench.CreateFirstPane("tab-2", "pane-2"); err != nil {
		t.Fatalf("CreateFirstPane: %v", err)
	}
	if err := orch.workbench.SwitchTab("main", 0); err != nil {
		t.Fatalf("SwitchTab: %v", err)
	}

	effects := orch.HandleSemanticAction(input.SemanticAction{Kind: input.ActionPrevTab})
	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	if effect, ok := effects[0].(SwitchTabEffect); !ok {
		t.Fatalf("expected SwitchTabEffect, got %T", effects[0])
	} else if effect.Delta != -1 {
		t.Fatalf("expected SwitchTabEffect delta -1, got %#v", effect)
	}
	if got := orch.workbench.CurrentTab().ID; got != "tab-2" {
		t.Fatalf("expected prev tab to wrap to tab-2, got %q", got)
	}

	effects = orch.HandleSemanticAction(input.SemanticAction{Kind: input.ActionNextTab})
	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	if effect, ok := effects[0].(SwitchTabEffect); !ok {
		t.Fatalf("expected SwitchTabEffect, got %T", effects[0])
	} else if effect.Delta != 1 {
		t.Fatalf("expected SwitchTabEffect delta 1, got %#v", effect)
	}
	if got := orch.workbench.CurrentTab().ID; got != "tab-1" {
		t.Fatalf("expected next tab to wrap back to tab-1, got %q", got)
	}
}

func TestHandleSemanticActionCloseTabInvalidates(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	seedTabWithSinglePane(orch.workbench, "main", "tab-1", "pane-1")
	if err := orch.workbench.CreateTab("main", "tab-2", "Tab Two"); err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	if err := orch.workbench.CreateFirstPane("tab-2", "pane-2"); err != nil {
		t.Fatalf("CreateFirstPane: %v", err)
	}

	effects := orch.HandleSemanticAction(input.SemanticAction{Kind: input.ActionCloseTab, TabID: "tab-2"})

	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	if _, ok := effects[0].(InvalidateRenderEffect); !ok {
		t.Fatalf("expected InvalidateRenderEffect, got %T", effects[0])
	}
	ws := orch.workbench.CurrentWorkspace()
	if ws == nil || len(ws.Tabs) != 1 {
		t.Fatalf("expected 1 tab remaining, got %#v", ws)
	}
	if got := ws.Tabs[0].ID; got != "tab-1" {
		t.Fatalf("expected remaining tab tab-1, got %q", got)
	}
}

func TestHandleSemanticActionKillTerminalProducesEffect(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:     input.ActionKillTerminal,
		TargetID: "term-42",
	})

	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	effect, ok := effects[0].(KillTerminalEffect)
	if !ok {
		t.Fatalf("expected KillTerminalEffect, got %T", effects[0])
	}
	if effect.TerminalID != "term-42" {
		t.Fatalf("expected terminal ID term-42, got %q", effect.TerminalID)
	}
}

func seedTabWithSinglePane(wb *workbench.Workbench, wsName, tabID, paneID string) {
	wb.AddWorkspace(wsName, &workbench.WorkspaceState{
		Name:      wsName,
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           tabID,
			Name:         "Tab One",
			ActivePaneID: paneID,
			Panes: map[string]*workbench.PaneState{
				paneID: {ID: paneID},
			},
			Root: workbench.NewLeaf(paneID),
		}},
	})
}
