package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type refreshingInputTerminalService struct {
	services.FakeTerminalService
	nextChannel         uint16
	staleChannels       map[uint16]bool
	staleKnownOnAttach  bool
	knownActiveChannels map[uint16]bool
}

func (service *refreshingInputTerminalService) Attach(_ context.Context, req services.TerminalAttachRequest) (services.TerminalAttachResult, error) {
	service.Attaches = append(service.Attaches, req)
	if service.AttachErr != nil {
		return services.TerminalAttachResult{}, service.AttachErr
	}
	if service.staleChannels == nil {
		service.staleChannels = make(map[uint16]bool)
	}
	if service.knownActiveChannels == nil {
		service.knownActiveChannels = make(map[uint16]bool)
	}
	if service.staleKnownOnAttach {
		for channel := range service.knownActiveChannels {
			service.staleChannels[channel] = true
		}
	}
	channel := service.nextChannel
	if channel == 0 {
		channel = 1
	}
	service.nextChannel = channel + 1
	service.knownActiveChannels[channel] = true
	delete(service.staleChannels, channel)
	result := service.AttachResult
	if result.TerminalID == "" {
		result.TerminalID = req.TerminalID
	}
	result.Channel = channel
	if result.Cols == 0 {
		result.Cols = req.Cols
	}
	if result.Rows == 0 {
		result.Rows = req.Rows
	}
	if result.ResizePolicy == "" {
		result.ResizePolicy = req.ResizePolicy
	}
	if result.SurfaceID == "" {
		result.SurfaceID = req.SurfaceID
	}
	if result.ViewID == "" {
		result.ViewID = req.ViewID
	}
	if !result.SizeLocked && result.ControlReason == "" && result.ResizePolicy == state.TerminalResizeRoleOwner {
		result.CanResize = true
	}
	return result, nil
}

func (service *refreshingInputTerminalService) SendInput(_ context.Context, req services.TerminalInputRequest) error {
	service.Inputs = append(service.Inputs, req)
	if service.InputErr != nil {
		return service.InputErr
	}
	if service.staleChannels[req.Channel] {
		return fmt.Errorf("stale channel %d", req.Channel)
	}
	return nil
}

func TestLiveInputRoutesLSSequenceAcrossTwoTiledPaneBindings(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	shell := state.DefaultShell().
		BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1").
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindPane(state.NewPaneTerminalView("pane-2", "term-2", 8, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID("pane-2"), true))
	host := NewFakeTerminalHost(16)
	host.SetSize(100, 30)
	runtime := NewInteractiveRuntime(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}

	pane2Content := frameHitRegion(t, lastFrame(t, host.Frames()), render.HitRegionPaneContent, "pane-2")
	if err := host.SendInput(mouseEventAt(pane2Content.Rect)); err != nil {
		t.Fatalf("click pane-2: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "s"},
		{Kind: input.EventKindKey, Key: input.KeyEnter},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send pane-2 input %#v: %v", event, err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane-2 input: %v", err)
	}

	pane1Content := frameHitRegion(t, lastFrame(t, host.Frames()), render.HitRegionPaneContent, state.DefaultPaneID)
	if err := host.SendInput(mouseEventAt(pane1Content.Rect)); err != nil {
		t.Fatalf("click pane-1: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "s"},
		{Kind: input.EventKindKey, Key: input.KeyEnter},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send pane-1 input %#v: %v", event, err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane-1 input: %v", err)
	}

	if got := compactInputRequests(terminal.Inputs); len(got) != 2 ||
		got[0] != "term-2#8:ls\r" ||
		got[1] != "term-1#7:ls\r" {
		t.Fatalf("input must follow clicked pane binding, got %#v raw=%#v", got, terminal.Inputs)
	}
}

func TestTerminalInputRouterLogsActiveViewRoute(t *testing.T) {
	t.Setenv(tuiInputTraceEnv, "1")
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	terminal := &services.FakeTerminalService{}
	root := state.Root{
		Shell: state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1"),
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-1", state.TerminalPaneViewID(state.DefaultPaneID), true))
	reducer := ComposeReducers(NewUIInputReducer(), NewTerminalInputRouterReducer(LiveDeps{Terminal: terminal, Logger: logger}), NewLiveReducer(LiveDeps{Terminal: terminal, Logger: logger}))

	_, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l", RawSeq: "l"}})
	if len(effects) != 1 {
		t.Fatalf("ordinary key should produce terminal input effect, got %#v", effects)
	}
	msg, ok := effects[0].(FuncEffect).Run(context.Background()).(LiveInputResultMsg)
	if !ok || msg.Err != nil {
		t.Fatalf("expected terminal input result, got %#v ok=%v", msg, ok)
	}
	if len(terminal.Inputs) != 1 || terminal.Inputs[0].ViewID != state.TerminalPaneViewID(state.DefaultPaneID) || terminal.Inputs[0].Channel != 7 || string(terminal.Inputs[0].Bytes) != "l" {
		t.Fatalf("input must route through active view binding, got %#v", terminal.Inputs)
	}
	text := logs.String()
	for _, want := range []string{"tui-v3 input route", "tui-v3 terminal input sent", "result=terminal", "target_view=" + state.TerminalPaneViewID(state.DefaultPaneID), "terminal_id=term-1", "channel=7"} {
		if !strings.Contains(text, want) {
			t.Fatalf("input route log missing %q in:\n%s", want, text)
		}
	}
}

func TestLiveInputRoutesBetweenTiledAndFloatingSharedTerminalChannels(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	shell := state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-shared")
	var result state.FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float", Kind: state.PaneTerminalLive, TerminalID: "term-shared"},
		Rect:     state.FloatingRect{X: 10, Y: 4, W: 30, H: 8},
		Source:   state.PaneCommandSourceTest,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}
	shell = shell.FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-shared", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindFloating(state.NewFloatingTerminalView("floating-1", "floating-pane-1", "term-shared", 8, 30, 8, state.TerminalResizeRoleFollower, "surface", state.TerminalFloatingViewID("floating-1"), false))
	host := NewFakeTerminalHost(16)
	host.SetSize(100, 30)
	runtime := NewInteractiveRuntime(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}

	floatingContent := frameActionHitRegion(t, lastFrame(t, host.Frames()), render.ActionFloatingRaise.String(), "floating-pane-1")
	if err := host.SendInput(mouseEventAt(floatingContent.Rect)); err != nil {
		t.Fatalf("click floating: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"}); err != nil {
		t.Fatalf("send floating key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating input: %v", err)
	}

	paneContent := frameHitRegion(t, lastFrame(t, host.Frames()), render.HitRegionPaneContent, state.DefaultPaneID)
	if err := host.SendInput(mouseEventAt(paneContent.Rect)); err != nil {
		t.Fatalf("click pane: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "p"}); err != nil {
		t.Fatalf("send pane key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane input: %v", err)
	}

	if got := compactInputRequests(terminal.Inputs); len(got) != 2 ||
		got[0] != "term-shared#8:f" ||
		got[1] != "term-shared#7:p" {
		t.Fatalf("shared terminal input must use active view channel, got %#v raw=%#v", got, terminal.Inputs)
	}
}

func TestLiveInputRoutesBetweenTiledAndFloatingDifferentTerminals(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	shell := state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-pane")
	var result state.FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float", Kind: state.PaneTerminalLive, TerminalID: "term-float"},
		Rect:     state.FloatingRect{X: 10, Y: 4, W: 30, H: 8},
		Source:   state.PaneCommandSourceTest,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-pane", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindFloating(state.NewFloatingTerminalView("floating-1", "floating-pane-1", "term-float", 8, 30, 8, state.TerminalResizeRoleOwner, "surface", state.TerminalFloatingViewID("floating-1"), true))
	host := NewFakeTerminalHost(16)
	host.SetSize(100, 30)
	runtime := NewInteractiveRuntime(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}

	floatingContent := frameActionHitRegion(t, lastFrame(t, host.Frames()), render.ActionFloatingRaise.String(), "floating-pane-1")
	if err := host.SendInput(mouseEventAt(floatingContent.Rect)); err != nil {
		t.Fatalf("click floating: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"}); err != nil {
		t.Fatalf("send floating key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating input: %v", err)
	}

	paneContent := frameHitRegion(t, lastFrame(t, host.Frames()), render.HitRegionPaneContent, state.DefaultPaneID)
	if err := host.SendInput(mouseEventAt(paneContent.Rect)); err != nil {
		t.Fatalf("click pane: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "p"}); err != nil {
		t.Fatalf("send pane key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane input: %v", err)
	}

	if got := compactInputRequests(terminal.Inputs); len(got) != 2 ||
		got[0] != "term-float#8:f" ||
		got[1] != "term-pane#7:p" {
		t.Fatalf("different terminal input must follow active view binding, got %#v raw=%#v", got, terminal.Inputs)
	}
}

func TestTerminalPoolReattachCurrentPaneDoesNotOverwriteSiblingBinding(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{
			TerminalID:   "term-1",
			Channel:      21,
			Cols:         80,
			Rows:         24,
			ResizePolicy: state.TerminalResizeRoleFollower,
			SurfaceID:    "surface",
		},
	}
	shell := state.DefaultShell().
		BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1").
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 80, 24, state.TerminalResizeRoleFollower, "surface", state.TerminalPaneViewID("pane-2"), false))
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})

	root, effects := reducer(root, TerminalPoolAttachRequestMsg{TerminalID: "term-1", TargetPaneID: state.DefaultPaneID})
	if len(effects) != 1 {
		t.Fatalf("expected attach effect, got %#v", effects)
	}
	msg, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolAttachResultMsg)
	if !ok {
		t.Fatalf("expected attach result, got %#v", msg)
	}
	root, _ = reducer(root, msg)

	pane1, ok := root.TerminalViews.PaneBinding(state.DefaultPaneID)
	if !ok || pane1.Channel != 21 || pane1.ViewID != state.TerminalPaneViewID(state.DefaultPaneID) {
		t.Fatalf("current pane should receive fresh channel, binding=%#v ok=%v", pane1, ok)
	}
	pane2, ok := root.TerminalViews.PaneBinding("pane-2")
	if !ok || pane2.Channel != 8 || pane2.ViewID != state.TerminalPaneViewID("pane-2") {
		t.Fatalf("reattach must not overwrite sibling channel, binding=%#v ok=%v", pane2, ok)
	}
}

func TestTerminalPoolAttachExitedTerminalPreservesLifecycleBeforeInput(t *testing.T) {
	exitedAt := time.Date(2026, 6, 17, 12, 45, 0, 0, time.UTC)
	exitCode := 23
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{
			TerminalID:   "term-exited",
			Channel:      21,
			Cols:         80,
			Rows:         24,
			ResizePolicy: state.TerminalResizeRoleFollower,
			SurfaceID:    "surface",
		},
	}
	root := state.Root{
		Shell: state.DefaultShell().
			BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-old"),
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{
			TerminalID: "term-exited",
			Title:      "done",
			State:      string(state.TerminalLiveExited),
			ExitCode:   &exitCode,
			ExitedAt:   exitedAt,
			Command:    []string{"bash", "-lc", "exit 23"},
		}}},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-old", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})

	next, effects := reducer(root, TerminalPoolAttachRequestMsg{TerminalID: "term-exited", TargetPaneID: state.DefaultPaneID})
	if len(effects) != 1 {
		t.Fatalf("expected attach effect, got %#v", effects)
	}
	msg, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolAttachResultMsg)
	if !ok {
		t.Fatalf("expected attach result, got %#v", msg)
	}
	next, _ = reducer(next, msg)

	surface := next.Surface.SurfaceForTerminal("term-exited")
	if surface.State != state.TerminalLiveExited || surface.ExitCode != 23 || !surface.ExitedAt.Equal(exitedAt) || strings.Join(surface.Command, " ") != "bash -lc exit 23" {
		t.Fatalf("picker attach to exited terminal should keep lifecycle immediately, surface=%#v", surface)
	}
	if next.Session.TerminalID != "term-exited" || !next.Session.Attached || next.Session.Channel != 21 {
		t.Fatalf("attach should still bind the selected terminal view/session, session=%#v", next.Session)
	}
}

func TestInteractionModeContentClickThenKeyUsesTerminalInputRoute(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	root := state.Root{
		Shell: state.DefaultShell().
			BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1").
			SetInteractionMode(state.InteractionModeResize),
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	host := NewFakeTerminalHost(16)
	host.SetSize(100, 30)
	runtime := NewInteractiveRuntime(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}
	content := frameHitRegion(t, lastFrame(t, host.Frames()), render.HitRegionPaneContent, state.DefaultPaneID)
	if err := host.SendInput(mouseEventAt(content.Rect)); err != nil {
		t.Fatalf("click pane content: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"}); err != nil {
		t.Fatalf("send key after content activation: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain input: %v", err)
	}

	if runtime.State().Shell.EnsureDefaults().InteractionMode != state.InteractionModeNormal {
		t.Fatalf("content click should exit interaction mode, shell=%#v", runtime.State().Shell)
	}
	if got := compactInputRequests(terminal.Inputs); len(got) != 1 || got[0] != "term-1#7:l" {
		t.Fatalf("key after content click should reach terminal service, got %#v raw=%#v", got, terminal.Inputs)
	}
}

func TestLiveAppAttachRenderInputAndResize(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{
			TerminalID: "term-1",
			Channel:    9,
			Cols:       78,
			Rows:       22,
			CanResize:  true,
		},
	}
	host := NewFakeTerminalHost(8)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       80,
		Rows:       24,
		Lines:      []string{"$ echo hi", "hi"},
	}}); err != nil {
		t.Fatalf("post surface: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := runtime.Post(LiveResizeMsg{Cols: 100, Rows: 40}); err != nil {
		t.Fatalf("post resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(terminal.Attaches) != 1 || terminal.Attaches[0].TerminalID != "term-1" {
		t.Fatalf("unexpected attach requests %#v", terminal.Attaches)
	}
	if len(terminal.Inputs) != 1 || string(terminal.Inputs[0].Bytes) != "x" || terminal.Inputs[0].Channel != 9 {
		t.Fatalf("unexpected input requests %#v", terminal.Inputs)
	}
	if len(terminal.Resizes) != 1 || terminal.Resizes[0].Cols != 100 || terminal.Resizes[0].Rows != 40 || terminal.Resizes[0].Channel != 9 {
		t.Fatalf("manual resize must win over stale attach correction, got %#v", terminal.Resizes)
	}
	if runtime.State().Session.Cols != 100 || runtime.State().Surface.Cols != 100 {
		t.Fatalf("resize was not reflected in state %#v", runtime.State())
	}
	if binding, ok := runtime.State().TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.DesiredCols != 100 || binding.DesiredRows != 40 {
		t.Fatalf("manual resize should sync active owner binding desired size, got %#v", binding)
	}
	last := lastFrame(t, host.Frames())
	if len(last.Lines) == 0 || !frameContains(last, "$ echo hi") {
		t.Fatalf("expected live surface frame, got %#v", last.Lines)
	}
}

func TestLiveAttachmentStoreSupportsSameTerminalAcrossTwoPanes(t *testing.T) {
	reducer := NewLiveReducer(LiveDeps{})
	root := state.Root{Shell: state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneEmpty}, state.SplitDirectionVertical)}

	var effects []Effect
	root, effects = reducer(root, LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8, Cols: 40, Rows: 12, ResizePolicy: state.TerminalResizeRoleFollower, SurfaceID: "surface", ViewID: state.TerminalPaneViewID("pane-2")}})
	if len(effects) != 1 {
		t.Fatalf("expected workbench persist effect without live deps, got %#v", effects)
	}
	if msg := effects[0].(FuncEffect).Run(context.Background()); msg.(WorkbenchStoragePersistRequestMsg).Reason != "terminal.attach" {
		t.Fatalf("expected terminal attach persist request, got %#v", msg)
	}
	root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	root, _ = reducer(root, LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-1", Channel: 7, Cols: 80, Rows: 24, ResizePolicy: state.TerminalResizeRoleOwner, SurfaceID: "surface", ViewID: state.TerminalPaneViewID(state.DefaultPaneID), CanResize: true}})

	bindings := root.TerminalViews.BindingsForTerminal("term-1")
	if len(bindings) != 2 {
		t.Fatalf("expected two bindings for shared terminal, got %#v", bindings)
	}
	if target, ok := liveInputTarget(root); !ok || target.Channel != 7 || target.TerminalID != "term-1" {
		t.Fatalf("active pane should use its own attachment channel, target=%#v ok=%v", target, ok)
	}
	root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{PaneID: "pane-2"})
	if target, ok := liveInputTarget(root); !ok || target.Channel != 8 || target.TerminalID != "term-1" {
		t.Fatalf("sibling pane should use its own attachment channel, target=%#v ok=%v", target, ok)
	}

	root, _ = reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandClose, Target: state.PaneCommandTarget{PaneID: state.DefaultPaneID}, Source: state.PaneCommandSourceTest})
	if _, ok := root.TerminalViews.PaneBinding(state.DefaultPaneID); ok {
		t.Fatal("close pane should detach only that view")
	}
	if _, ok := root.TerminalViews.PaneBinding("pane-2"); !ok {
		t.Fatal("close pane should not detach sibling view for same terminal")
	}
	root, _ = reduceTerminalPoolKillResult(root, TerminalPoolKillResultMsg{TerminalID: "term-1"})
	if bindings := root.TerminalViews.BindingsForTerminal("term-1"); len(bindings) != 0 {
		t.Fatalf("kill terminal should clear all view bindings, got %#v", bindings)
	}
	if pane, ok := root.Shell.Pane(state.PaneCommandTarget{PaneID: "pane-2"}); !ok || pane.TerminalID != "" || pane.Kind != state.PaneEmpty {
		t.Fatalf("kill terminal should clear pane terminal binding, pane=%#v ok=%v", pane, ok)
	}
	if root.Session.TerminalID == "term-1" || root.Surface.TerminalID == "term-1" {
		t.Fatalf("kill terminal should clear active session and surface, session=%#v surface=%#v", root.Session, root.Surface)
	}
}

func TestLiveInputAttachesActiveViewWhenChannelMissing(t *testing.T) {
	terminal := &refreshingInputTerminalService{nextChannel: 21}
	host := NewFakeTerminalHost(8)
	host.SetSize(100, 30)
	root := state.Root{
		Shell: state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1"),
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 0, 80, 24, state.TerminalResizeRoleFollower, "surface", state.TerminalPaneViewID(state.DefaultPaneID), false))
	runtime := NewInteractiveRuntime(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"}); err != nil {
		t.Fatalf("send key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(terminal.Attaches) != 1 {
		t.Fatalf("missing channel should attach target view once, got %#v", terminal.Attaches)
	}
	if got := terminal.Attaches[0]; got.TerminalID != "term-1" || got.ViewID != state.TerminalPaneViewID(state.DefaultPaneID) || got.ResizePolicy != state.TerminalResizeRoleFollower {
		t.Fatalf("attach should target active pane view, got %#v", got)
	}
	if len(terminal.Inputs) != 1 || terminal.Inputs[0].Channel != 21 || string(terminal.Inputs[0].Bytes) != "l" {
		t.Fatalf("input should replay through fresh channel, got %#v", terminal.Inputs)
	}
	if binding, ok := runtime.State().TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.Channel != 21 {
		t.Fatalf("fresh channel should be stored on active view, binding=%#v ok=%v", binding, ok)
	}
}

func TestLiveInputRefreshesStaleViewChannelWithoutStealingSibling(t *testing.T) {
	terminal := &refreshingInputTerminalService{
		nextChannel:         21,
		staleChannels:       map[uint16]bool{7: true},
		staleKnownOnAttach:  true,
		knownActiveChannels: map[uint16]bool{7: true, 8: true},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(120, 36)
	shell := state.DefaultShell().
		BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1").
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 80, 24, state.TerminalResizeRoleFollower, "surface", state.TerminalPaneViewID("pane-2"), false))
	runtime := NewInteractiveRuntime(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"}); err != nil {
		t.Fatalf("send first pane key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain first pane: %v", err)
	}

	if len(terminal.Inputs) != 2 || terminal.Inputs[0].Channel != 7 || terminal.Inputs[1].Channel != 21 {
		t.Fatalf("stale active pane should retry on fresh channel, got %#v", terminal.Inputs)
	}
	if len(terminal.Attaches) != 1 || terminal.Attaches[0].ViewID != state.TerminalPaneViewID(state.DefaultPaneID) {
		t.Fatalf("retry should reattach default pane only, got %#v", terminal.Attaches)
	}
	if binding, ok := runtime.State().TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.Channel != 21 {
		t.Fatalf("default pane channel should refresh, binding=%#v ok=%v", binding, ok)
	}

	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandFocus, Target: state.PaneCommandTarget{PaneID: "pane-2"}, Source: state.PaneCommandSourceTest}}); err != nil {
		t.Fatalf("focus pane-2: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "s"}); err != nil {
		t.Fatalf("send second pane key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain second pane: %v", err)
	}

	if len(terminal.Inputs) != 4 || terminal.Inputs[2].Channel != 8 || terminal.Inputs[3].Channel != 22 {
		t.Fatalf("sibling pane should recover its own channel after focus, got %#v", terminal.Inputs)
	}
	if len(terminal.Attaches) != 2 || terminal.Attaches[1].ViewID != state.TerminalPaneViewID("pane-2") {
		t.Fatalf("second retry should reattach pane-2 view, got %#v", terminal.Attaches)
	}
	if binding, ok := runtime.State().TerminalViews.PaneBinding("pane-2"); !ok || binding.Channel != 22 {
		t.Fatalf("pane-2 channel should refresh independently, binding=%#v ok=%v", binding, ok)
	}
}

func TestLiveInputReattachesOnlyActiveViewWhenProtocolChannelCheckFails(t *testing.T) {
	terminal := &refreshingInputTerminalService{
		nextChannel:         21,
		staleChannels:       map[uint16]bool{7: true},
		knownActiveChannels: map[uint16]bool{7: true, 8: true},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(120, 36)
	shell := state.DefaultShell().
		BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1").
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 80, 24, state.TerminalResizeRoleFollower, "surface", state.TerminalPaneViewID("pane-2"), false))
	runtime := NewInteractiveRuntime(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"}); err != nil {
		t.Fatalf("send first pane key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain first pane: %v", err)
	}

	if len(terminal.Attaches) != 1 || terminal.Attaches[0].ViewID != state.TerminalPaneViewID(state.DefaultPaneID) {
		t.Fatalf("failed active view input should reattach only active pane, got %#v", terminal.Attaches)
	}
	if got := compactInputRequests(terminal.Inputs); len(got) != 2 ||
		got[0] != "term-1#7:l" ||
		got[1] != "term-1#21:l" {
		t.Fatalf("input should retry on active pane fresh channel, got %#v raw=%#v", got, terminal.Inputs)
	}
	if binding, ok := runtime.State().TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.Channel != 21 {
		t.Fatalf("active pane channel should refresh, binding=%#v ok=%v", binding, ok)
	}
	if binding, ok := runtime.State().TerminalViews.PaneBinding("pane-2"); !ok || binding.Channel != 8 {
		t.Fatalf("sibling channel must not be overwritten, binding=%#v ok=%v", binding, ok)
	}
}

func TestLiveAttachResultAcceptsPrefilledSessionBeforeFirstBinding(t *testing.T) {
	root := state.Root{
		Session: state.TerminalSessionStore{TerminalID: "term-1", Cols: 100, Rows: 30},
		Surface: state.TerminalSurfaceStore{TerminalID: "term-1", Cols: 100, Rows: 30},
	}
	root, effects := reduceLiveAttachResult(root, LiveAttachResultMsg{Result: services.TerminalAttachResult{
		TerminalID:   "term-1",
		Channel:      7,
		Cols:         100,
		Rows:         30,
		ResizePolicy: state.TerminalResizeRoleOwner,
		SurfaceID:    "termx-cli-v3",
		ViewID:       "termx-cli-v3-main",
		CanResize:    true,
	}}, LiveDeps{})

	if len(effects) != 1 {
		t.Fatalf("expected workbench persist effect without live deps, got %#v", effects)
	}
	if msg := effects[0].(FuncEffect).Run(context.Background()); msg.(WorkbenchStoragePersistRequestMsg).Reason != "terminal.attach" {
		t.Fatalf("expected terminal attach persist request, got %#v", msg)
	}
	if !root.Session.Attached || root.Session.Channel != 7 || root.Session.ViewID != "termx-cli-v3-main" {
		t.Fatalf("prefilled CLI session should accept first attach result, session=%#v", root.Session)
	}
	if binding, ok := root.TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.ViewID != "termx-cli-v3-main" {
		t.Fatalf("expected first attach result to create active pane binding, binding=%#v ok=%v", binding, ok)
	}
}

func TestLiveAttachAndInitialSurfaceEffectsAreAsync(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	root := state.Root{Shell: state.DefaultShell()}
	_, effects := reduceLiveAttach(root, LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}, LiveDeps{Terminal: terminal})
	if len(effects) != 1 {
		t.Fatalf("expected one attach effect, got %#v", effects)
	}
	attach, ok := effects[0].(FuncEffect)
	if !ok || !attach.Async || !attach.ForceSyncInTests {
		t.Fatalf("attach must be async in real runtime and sync-capable in harness, got %#v", effects[0])
	}

	effects = liveSurfaceEffect("term-1", 80, 24, LiveDeps{Terminal: terminal})
	if len(effects) != 1 {
		t.Fatalf("expected one live surface effect, got %#v", effects)
	}
	surface, ok := effects[0].(FuncEffect)
	if !ok || !surface.Async || !surface.ForceSyncInTests {
		t.Fatalf("initial live surface fetch must be async in real runtime and sync-capable in harness, got %#v", effects[0])
	}
}

func TestTerminalPoolRemoveDeletesInventoryAndBindings(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Shell:        state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1"),
		Session:      state.TerminalSessionStore{TerminalID: "term-1", Channel: 7, Attached: true, InputChannels: map[string]uint16{"term-1": 7}},
		Surface:      state.TerminalSurfaceStore{TerminalID: "term-1", Ready: true, Lines: []string{"live"}, Surfaces: map[string]state.LiveSurfaceSnapshot{"term-1": {TerminalID: "term-1"}}},
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{TerminalID: "term-1", Title: "one"}, {TerminalID: "term-2", Title: "two"}}},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))

	next, effects := reducer(root, TerminalPoolRemoveRequestMsg{TerminalID: "term-1"})
	if len(effects) != 1 {
		t.Fatalf("expected remove effect, got %#v", effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background())
	if len(terminal.Removes) != 1 || terminal.Removes[0].TerminalID != "term-1" {
		t.Fatalf("remove request should call terminal service remove, got %#v", terminal.Removes)
	}
	next, _ = reducer(next, msg)
	if next.TerminalPool.LastRemovedID != "term-1" || len(next.TerminalPool.Items) != 1 || next.TerminalPool.Items[0].TerminalID != "term-2" {
		t.Fatalf("remove result should update pool inventory, pool=%#v", next.TerminalPool)
	}
	if _, ok := next.TerminalViews.PaneBinding(state.DefaultPaneID); ok {
		t.Fatal("remove result should clear terminal view binding")
	}
	if pane, ok := next.Shell.Pane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}); !ok || pane.TerminalID != "" || pane.Kind != state.PaneEmpty {
		t.Fatalf("remove result should clear shell pane binding, pane=%#v ok=%v", pane, ok)
	}
	if next.Session.TerminalID != "" || next.Surface.TerminalID != "" {
		t.Fatalf("remove result should clear active session and surface, session=%#v surface=%#v", next.Session, next.Surface)
	}
}

func TestLiveStreamTokenIsTerminalScoped(t *testing.T) {
	terminal := &services.FakeTerminalService{LiveEventsCh: make(chan services.TerminalLiveEvent)}
	first := liveStreamEffect("term-1", 80, 24, LiveDeps{Terminal: terminal})
	second := liveStreamEffect("term-2", 80, 24, LiveDeps{Terminal: terminal})
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected cancel+stream effects, first=%#v second=%#v", first, second)
	}
	firstCancel := first[0].(CancelEffect)
	firstStream := first[1].(StreamEffect)
	secondCancel := second[0].(CancelEffect)
	secondStream := second[1].(StreamEffect)
	if firstCancel.Token != firstStream.Token || secondCancel.Token != secondStream.Token || firstStream.Token == secondStream.Token {
		t.Fatalf("live stream tokens must be scoped per terminal, first=%q/%q second=%q/%q", firstCancel.Token, firstStream.Token, secondCancel.Token, secondStream.Token)
	}
}

func TestLiveStreamContextCanceledDoesNotPostPanelError(t *testing.T) {
	terminal := &services.FakeTerminalService{LiveEventsErr: context.Canceled}
	effects := liveStreamEffect("term-1", 80, 24, LiveDeps{Terminal: terminal})
	if len(effects) != 2 {
		t.Fatalf("expected cancel+stream effects, got %#v", effects)
	}
	stream := effects[1].(StreamEffect)
	var posted []Msg
	stream.Run(context.Background(), func(msg Msg) {
		posted = append(posted, msg)
	})
	if len(posted) != 0 {
		t.Fatalf("context canceled live stream should not post UI error, got %#v", posted)
	}

	root := state.Root{Shell: state.DefaultShell()}
	root.Surface = state.TerminalSurfaceStore{TerminalID: "term-1"}
	root.Session = state.TerminalSessionStore{TerminalID: "term-1"}
	next, effects := reduceLiveEvent(root, LiveEventMsg{Event: services.TerminalLiveEvent{TerminalID: "term-1", Err: context.Canceled}})
	if len(effects) != 0 || next.Surface.Err != "" || next.Session.LastError != "" {
		t.Fatalf("context canceled live event must stay silent, root=%#v effects=%#v", next, effects)
	}
}

func TestTerminalPoolReconnectUsesActiveViewIdentity(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 11, Cols: 80, Rows: 24, CanResize: true}}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{Shell: state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-logs", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical)}

	root, effects := reducer(root, TerminalPoolReconnectRequestMsg{TerminalID: "term-1"})
	if len(effects) != 1 {
		t.Fatalf("expected reconnect effect, got %#v", effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background())
	if len(terminal.Reconnects) != 1 || terminal.Reconnects[0].ViewID != state.TerminalPaneViewID("pane-logs") || terminal.Reconnects[0].SurfaceID != "termx-tui-v3" {
		t.Fatalf("reconnect should use active pane view identity, requests=%#v", terminal.Reconnects)
	}
	root, _ = reducer(root, msg)
	if binding, ok := root.TerminalViews.PaneBinding("pane-logs"); !ok || binding.Channel != 11 || binding.TerminalID != "term-1" {
		t.Fatalf("reconnect result should bind active pane view, binding=%#v ok=%v", binding, ok)
	}
}

func TestLiveAppLayoutResizePreservesAttachResizeOwner(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{
			TerminalID:   "term-1",
			Channel:      9,
			Cols:         80,
			Rows:         24,
			CanResize:    true,
			ResizePolicy: "owner",
			SurfaceID:    "surface-1",
			ViewID:       "view-1",
		},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{
		TerminalID:   "term-1",
		Cols:         80,
		Rows:         24,
		Mode:         "collaborator",
		ResizePolicy: "owner",
		SurfaceID:    "surface-1",
		ViewID:       "view-1",
	}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if len(terminal.Resizes) != 1 {
		t.Fatalf("expected attach correction resize, got %#v", terminal.Resizes)
	}
	got := terminal.Resizes[0]
	if got.Cols != 78 || got.Rows != 20 || got.ResizePolicy != "owner" || got.SurfaceID != "surface-1" || got.ViewID != "view-1" {
		t.Fatalf("layout resize must preserve attach owner metadata, got %#v", got)
	}
}

func TestTerminalLayoutResizeUsesSharedOwnerWhenActiveViewIsFollower(t *testing.T) {
	reducer := NewTerminalLayoutResizeReducer()
	shell := state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical)
	root := state.Root{
		Shell:    shell.FocusPane(state.PaneCommandTarget{PaneID: "pane-2"}),
		Viewport: state.ViewportStore{Valid: true, Cols: 100, Rows: 30},
		Session:  state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24, DesiredCols: 80, DesiredRows: 24},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	next, effects := reducer(root, HostResizeMsg{Cols: 120, Rows: 40})
	if len(effects) != 1 {
		t.Fatalf("visible shared owner should resize PTY even when active view is follower, got %#v", effects)
	}
	ownerMsg := effects[0].(FuncEffect).Run(context.Background()).(LiveResizeMsg)
	if ownerMsg.ViewID != "view-1" || ownerMsg.Seq != 1 || ownerMsg.Cols <= 0 || ownerMsg.Rows <= 0 {
		t.Fatalf("resize effect should carry shared owner view identity, got %#v", ownerMsg)
	}
	if got, _ := next.TerminalViews.PaneBinding("pane-2"); got.DesiredCols != 40 || got.DesiredRows != 12 || got.RequestSeq != 0 {
		t.Fatalf("follower layout resize must not mutate desired size, got %#v", got)
	}
	if got, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID); got.RequestSeq != 1 || got.DesiredCols != ownerMsg.Cols || got.DesiredRows != ownerMsg.Rows {
		t.Fatalf("owner desired size should track request, got %#v msg=%#v", got, ownerMsg)
	}

	root.TerminalViews = root.TerminalViews.TransferPaneResizeOwner("pane-2")
	next, effects = reducer(root, HostResizeMsg{Cols: 120, Rows: 40})
	if len(effects) != 1 {
		t.Fatalf("active owner should schedule one resize effect, got %#v", effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background()).(LiveResizeMsg)
	if msg.ViewID != "view-2" || msg.Seq != 1 || msg.Cols <= 0 || msg.Rows <= 0 {
		t.Fatalf("resize effect should carry active owner view identity, got %#v", msg)
	}
	if got, _ := next.TerminalViews.PaneBinding("pane-2"); got.RequestSeq != 1 || got.DesiredCols != msg.Cols || got.DesiredRows != msg.Rows {
		t.Fatalf("owner view desired size should track request, got %#v msg=%#v", got, msg)
	}
}

func TestTerminalLayoutResizeOwnerPaneResizeChangesPTYSize(t *testing.T) {
	reducer := ComposeReducers(NewShellReducer(), NewTerminalLayoutResizeReducer())
	shell := state.DefaultShell().SetPanelPresentation(state.PanelPresentationSplitLine)
	shell = shell.SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical)
	root := state.Root{
		Shell:    shell.FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}),
		Viewport: state.ViewportStore{Valid: true, Cols: 100, Rows: 30},
		Session:  state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24, DesiredCols: 80, DesiredRows: 24},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	next, effects := reducer(root, ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandResize, Target: state.PaneCommandTarget{PaneID: state.DefaultPaneID}, ResizeDirection: state.PaneResizeRight, Delta: 6, Source: state.PaneCommandSourceTest}})
	msg, ok := liveResizeMsgFromEffects(effects)
	if !ok {
		t.Fatalf("owner pane resize should schedule one PTY resize effect, got %#v", effects)
	}
	if msg.ViewID != "view-1" || msg.Seq != 1 || msg.Cols == 80 || msg.Rows == 24 {
		t.Fatalf("resize effect should carry changed owner view size, got %#v", msg)
	}
	owner, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if owner.RequestSeq != 1 || owner.DesiredCols != msg.Cols || owner.DesiredRows != msg.Rows {
		t.Fatalf("owner desired size should track pane resize request, got %#v msg=%#v", owner, msg)
	}
}

func TestTerminalSizeLockBlocksSplitLayoutResize(t *testing.T) {
	reducer := ComposeReducers(NewShellReducer(), NewTerminalLayoutResizeReducer())
	root := state.Root{
		Shell:    state.DefaultShell().SetPanelPresentation(state.PanelPresentationSplitLine).FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}),
		Viewport: state.ViewportStore{Valid: true, Cols: 100, Rows: 30},
		Session:  state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24, DesiredCols: 80, DesiredRows: 24},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.ApplyTerminalSizeLock("term-1", true)

	next, effects := reducer(root, ShellSplitActivePaneMsg{
		Pane:      state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive},
		Direction: state.SplitDirectionHorizontal,
	})
	if _, ok := liveResizeMsgFromEffects(effects); ok {
		t.Fatalf("splitting a locked terminal must not emit PTY resize, effects=%#v", effects)
	}
	owner, _ := next.TerminalViews.PaneBinding("pane-2")
	if owner.ResizeRole != state.TerminalResizeRoleOwner || owner.CanResize || !owner.SizeLocked || owner.ControlReason != "size_locked" {
		t.Fatalf("new split owner should inherit terminal lock without resize authority, got %#v", owner)
	}
	previous, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if previous.CanResize || !previous.SizeLocked {
		t.Fatalf("previous pane should remain locked after split, got %#v", previous)
	}

	next.TerminalViews = next.TerminalViews.ApplyTerminalSizeLock("term-1", false)
	owner, _ = next.TerminalViews.PaneBinding("pane-2")
	if owner.SizeLocked || !owner.CanResize {
		t.Fatalf("unlock should restore resize authority to current owner, got %#v", owner)
	}
}

func TestTerminalSizeUnlockResizesOwnerWhenPanelSizeDiverged(t *testing.T) {
	reducer := ComposeReducers(NewTerminalPoolReducer(LiveDeps{}), NewTerminalLayoutResizeReducer())
	root := state.Root{
		Shell: state.DefaultShell().
			SetPanelPresentation(state.PanelPresentationSplitLine).
			FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}),
		Viewport: state.ViewportStore{Valid: true, Cols: 120, Rows: 40},
		Session:  state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24, DesiredCols: 80, DesiredRows: 24},
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "main",
			Tags:       map[string]string{"termx.size_lock": "lock"},
		}}},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.ApplyTerminalSizeLock("term-1", true)

	next, effects := reducer(root, TerminalSizeLockToggleResultMsg{TerminalID: "term-1", Tags: map[string]string{}, Locked: false})
	msg, ok := liveResizeMsgFromEffects(effects)
	if !ok {
		t.Fatalf("unlock should trigger owner resize when panel and terminal size diverged, effects=%#v", effects)
	}
	if msg.ViewID != "view-1" || msg.Cols == 80 || msg.Rows == 24 {
		t.Fatalf("unlock resize should use owner view content rect, got %#v", msg)
	}
	owner, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if owner.SizeLocked || !owner.CanResize || owner.RequestSeq != msg.Seq || owner.DesiredCols != msg.Cols || owner.DesiredRows != msg.Rows {
		t.Fatalf("owner should be unlocked and track requested resize, owner=%#v msg=%#v", owner, msg)
	}
}

func TestLiveMetadataEventProjectsTerminalSizeLockAndUnlockResize(t *testing.T) {
	reducer := ComposeReducers(NewLiveReducer(LiveDeps{}), NewTerminalLayoutResizeReducer())
	root := state.Root{
		Shell: state.DefaultShell().
			SetPanelPresentation(state.PanelPresentationSplitLine).
			FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}),
		Viewport: state.ViewportStore{Valid: true, Cols: 120, Rows: 40},
		Session:  state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24, DesiredCols: 80, DesiredRows: 24},
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "main",
		}}},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 80, 24, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	locked, effects := reducer(root, LiveEventMsg{Event: services.TerminalLiveEvent{TerminalID: "term-1", Metadata: true, Tags: map[string]string{"termx.size_lock": "lock"}}})
	if _, ok := liveResizeMsgFromEffects(effects); ok {
		t.Fatalf("metadata lock must not resize PTY, effects=%#v", effects)
	}
	owner, _ := locked.TerminalViews.PaneBinding(state.DefaultPaneID)
	follower, _ := locked.TerminalViews.PaneBinding("pane-2")
	if !owner.SizeLocked || owner.CanResize || !follower.SizeLocked || follower.CanResize {
		t.Fatalf("metadata lock should broadcast to all terminal views, owner=%#v follower=%#v", owner, follower)
	}
	if locked.TerminalPool.Items[0].Tags["termx.size_lock"] != "lock" {
		t.Fatalf("metadata lock should update terminal pool tags, pool=%#v", locked.TerminalPool)
	}

	unlocked, effects := reducer(locked, LiveEventMsg{Event: services.TerminalLiveEvent{TerminalID: "term-1", Metadata: true, Tags: map[string]string{}}})
	msg, ok := liveResizeMsgFromEffects(effects)
	if !ok {
		t.Fatalf("metadata unlock should trigger owner resize when panel and terminal size diverged, effects=%#v", effects)
	}
	owner, _ = unlocked.TerminalViews.PaneBinding(state.DefaultPaneID)
	follower, _ = unlocked.TerminalViews.PaneBinding("pane-2")
	if owner.SizeLocked || !owner.CanResize || follower.SizeLocked || follower.CanResize {
		t.Fatalf("metadata unlock should broadcast unlocked projection, owner=%#v follower=%#v", owner, follower)
	}
	if msg.ViewID != "view-1" {
		t.Fatalf("metadata unlock resize should target owner view, got %#v", msg)
	}
}

func TestTerminalSizeLockBlocksAttachResultResizeToSplitPane(t *testing.T) {
	reducer := ComposeReducers(NewShellReducer(), NewTerminalPoolReducer(LiveDeps{}), NewTerminalLayoutResizeReducer())
	root := state.Root{
		Shell: state.DefaultShell().
			SetPanelPresentation(state.PanelPresentationSplitLine).
			FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}),
		Viewport: state.ViewportStore{Valid: true, Cols: 120, Rows: 40},
		Session:  state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 100, Rows: 30, DesiredCols: 100, DesiredRows: 30},
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "main",
			Tags:       map[string]string{"termx.size_lock": "lock"},
			Cols:       100,
			Rows:       30,
		}}},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 100, 30, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.ApplyTerminalSizeLock("term-1", true)

	root, effects := reducer(root, ShellSplitActivePaneMsg{
		Pane:      state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneEmpty},
		Direction: state.SplitDirectionHorizontal,
	})
	if _, ok := liveResizeMsgFromEffects(effects); ok {
		t.Fatalf("splitting an empty sibling for locked terminal must not emit PTY resize, effects=%#v", effects)
	}
	if root.Shell.EnsureDefaults().ActivePaneID != "pane-2" {
		t.Fatalf("new split pane should be active before attach, shell=%#v", root.Shell.EnsureDefaults())
	}

	next, effects := reducer(root, TerminalPoolAttachResultMsg{
		TerminalID: "term-1",
		Result: services.TerminalAttachResult{
			TerminalID:   "term-1",
			Channel:      8,
			Cols:         60,
			Rows:         14,
			ResizePolicy: state.TerminalResizeRoleOwner,
			SurfaceID:    "surface",
			ViewID:       state.TerminalPaneViewID("pane-2"),
			CanResize:    true,
		},
	})
	if _, ok := liveResizeMsgFromEffects(effects); ok {
		t.Fatalf("attaching split pane to locked terminal must not emit PTY resize, effects=%#v", effects)
	}
	binding, ok := next.TerminalViews.PaneBinding("pane-2")
	if !ok || binding.CanResize || !binding.SizeLocked || binding.ControlReason != "size_locked" {
		t.Fatalf("attached split pane should inherit terminal lock without resize authority, binding=%#v ok=%v", binding, ok)
	}
	previous, ok := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if !ok || previous.CanResize || !previous.SizeLocked {
		t.Fatalf("previous binding should remain locked, binding=%#v ok=%v", previous, ok)
	}
}

func TestTerminalPoolAttachRequestToSameLockedTerminalDoesNotResize(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{
			TerminalID:   "term-1",
			Channel:      8,
			Cols:         60,
			Rows:         14,
			ResizePolicy: state.TerminalResizeRoleOwner,
			CanResize:    true,
			SurfaceID:    "surface",
			ViewID:       state.TerminalPaneViewID("pane-2"),
		},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(120, 40)
	root := state.Root{
		Shell: state.DefaultShell().
			SetPanelPresentation(state.PanelPresentationSplitLine).
			FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}),
		Viewport: state.ViewportStore{Valid: true, Cols: 120, Rows: 40},
		Session:  state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 100, Rows: 30, DesiredCols: 100, DesiredRows: 30},
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "main",
			Tags:       map[string]string{"termx.size_lock": "lock"},
			Cols:       100,
			Rows:       30,
		}}},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 100, 30, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	root.TerminalViews = root.TerminalViews.ApplyTerminalSizeLock("term-1", true)
	runtime := NewInteractiveRuntime(root, host, NewSyncEffectRunner(), LiveDeps{Terminal: terminal}, CopyModeDeps{Core: &services.FakeCoreClient{}})

	if err := runtime.Post(ShellSplitActivePaneMsg{
		Pane:      state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneEmpty},
		Direction: state.SplitDirectionHorizontal,
	}); err != nil {
		t.Fatalf("post split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split: %v", err)
	}
	if err := runtime.Post(TerminalPoolAttachRequestMsg{TerminalID: "term-1"}); err != nil {
		t.Fatalf("post attach request: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if len(terminal.Attaches) != 1 {
		t.Fatalf("expected one attach request, got %#v", terminal.Attaches)
	}
	if got := terminal.Attaches[0]; got.ResizePolicy != state.TerminalResizeRoleFollower || got.ViewID != state.TerminalPaneViewID("pane-2") {
		t.Fatalf("same-terminal pool attach should be a follower request for the new pane, got %#v", got)
	}
	if len(terminal.Resizes) != 0 {
		t.Fatalf("locked terminal attach must not resize PTY, got %#v", terminal.Resizes)
	}
	binding, ok := runtime.State().TerminalViews.PaneBinding("pane-2")
	if !ok || binding.CanResize || !binding.SizeLocked || binding.ControlReason != "size_locked" {
		t.Fatalf("attached pane should stay size-locked without resize authority, binding=%#v ok=%v", binding, ok)
	}
}

func TestTakeResizeOwnerAttachResultTriggersOwnerViewResize(t *testing.T) {
	reducer := ComposeReducers(NewLiveReducer(LiveDeps{}), NewTerminalLayoutResizeReducer())
	shell := state.DefaultShell().SetPanelPresentation(state.PanelPresentationSplitLine)
	shell = shell.SplitActivePane(state.PaneState{ID: "pane-2", Title: "two", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical)
	root := state.Root{
		Shell:    shell.FocusPane(state.PaneCommandTarget{PaneID: "pane-2"}),
		Viewport: state.ViewportStore{Valid: true, Cols: 120, Rows: 40},
		Session:  state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24, DesiredCols: 80, DesiredRows: 24},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", "view-2", false))

	next, effects := reducer(root, LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8, Cols: 40, Rows: 12, ResizePolicy: state.TerminalResizeRoleOwner, SurfaceID: "surface", ViewID: "view-2", CanResize: true, OwnerSurfaceID: "surface", OwnerViewID: "view-2", ResizeEpoch: 2}})
	owner, _ := next.TerminalViews.PaneBinding("pane-2")
	if !owner.HasAuthoritativeResizeOwner() {
		t.Fatalf("attach result should project clicked view as authoritative owner, got %#v", owner)
	}
	previous, _ := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if previous.ResizeRole != state.TerminalResizeRoleFollower || previous.CanResize {
		t.Fatalf("previous owner should be demoted by authoritative result, got %#v", previous)
	}
	msg, ok := liveResizeMsgFromEffects(effects)
	if !ok || msg.ViewID != "view-2" || msg.Seq != 1 || msg.Cols <= 40 || msg.Rows <= 12 {
		t.Fatalf("owner attach result should immediately resize to active content rect, got msg=%#v effects=%#v", msg, effects)
	}
}

func TestViewScopedOwnerResizeUsesBindingResizePolicy(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	reducer := NewLiveReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Session: state.TerminalSessionStore{
			TerminalID:   "term-1",
			Channel:      8,
			Attached:     true,
			ResizePolicy: state.TerminalResizeRoleFollower,
			SurfaceID:    "surface-follower",
			ViewID:       "view-follower",
		},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-owner", "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-owner", "view-owner", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-follower", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface-follower", "view-follower", false))
	var decision state.TerminalViewResizeDecision
	root.TerminalViews, decision = root.TerminalViews.RequestViewResize("view-owner", 100, 30)
	if !decision.Allowed || !decision.Changed {
		t.Fatalf("expected owner view resize decision, got %#v", decision)
	}

	next, effects := reducer(root, LiveResizeMsg{TerminalID: "term-1", Cols: 100, Rows: 30, Seq: decision.Seq, ViewID: "view-owner"})
	if len(effects) != 1 {
		t.Fatalf("view-scoped owner resize should emit terminal resize effect, got %#v", effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background())
	next, _ = reducer(next, msg)
	if len(terminal.Resizes) != 1 {
		t.Fatalf("expected one terminal resize request, got %#v", terminal.Resizes)
	}
	got := terminal.Resizes[0]
	if got.Channel != 7 || got.ResizePolicy != state.TerminalResizeRoleOwner || got.SurfaceID != "surface-owner" || got.ViewID != "view-owner" {
		t.Fatalf("view-scoped resize must use owner binding identity, got %#v", got)
	}
	if binding, ok := next.TerminalViews.PaneBinding("pane-owner"); !ok || binding.DesiredCols != 100 || binding.DesiredRows != 30 {
		t.Fatalf("owner binding should track requested desired size, binding=%#v ok=%v", binding, ok)
	}
}

func liveResizeMsgFromEffects(effects []Effect) (LiveResizeMsg, bool) {
	for _, effect := range effects {
		funcEffect, ok := effect.(FuncEffect)
		if !ok || funcEffect.Run == nil {
			continue
		}
		msg, ok := funcEffect.Run(context.Background()).(LiveResizeMsg)
		if ok {
			return msg, true
		}
	}
	return LiveResizeMsg{}, false
}

func TestLiveResizeResultUsesViewScopedStaleGuard(t *testing.T) {
	reducer := NewLiveReducer(LiveDeps{})
	root := state.Root{
		Session: state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24, DesiredCols: 80, DesiredRows: 24, ResizeRequestSeq: 9},
		Surface: state.TerminalSurfaceStore{TerminalID: "term-1", Cols: 80, Rows: 24},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 100, 30, state.TerminalResizeRoleOwner, "surface", "view-1", true))

	next, effects := reducer(root, LiveResizeResultMsg{ViewID: "view-1", Seq: 1, Cols: 100, Rows: 30})
	if len(effects) != 0 {
		t.Fatalf("resize result should not emit effects, got %#v", effects)
	}
	if next.Session.Cols != 100 || next.Surface.Cols != 100 {
		t.Fatalf("view-scoped result should not be rejected by global seq, got session=%#v surface=%#v", next.Session, next.Surface)
	}
}

func TestLiveResizeResultProjectsTerminalSizeLockWithoutResizingSurface(t *testing.T) {
	reducer := NewLiveReducer(LiveDeps{})
	root := state.Root{
		Session: state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24, DesiredCols: 100, DesiredRows: 30},
		Surface: state.TerminalSurfaceStore{TerminalID: "term-1", Cols: 80, Rows: 24},
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 100, 30, state.TerminalResizeRoleOwner, "surface", "view-1", true))

	next, effects := reducer(root, LiveResizeResultMsg{
		ViewID: "view-1",
		Seq:    1,
		Cols:   100,
		Rows:   30,
		Result: services.TerminalResizeResult{
			TerminalID:     "term-1",
			Cols:           80,
			Rows:           24,
			Resized:        false,
			CanResize:      false,
			SizeLocked:     true,
			ControlReason:  "size_locked",
			OwnerSurfaceID: "surface",
			OwnerViewID:    "view-1",
			ResizeEpoch:    3,
			ResizePolicy:   state.TerminalResizeRoleOwner,
			SurfaceID:      "surface",
			ViewID:         "view-1",
		},
	})
	if len(effects) != 0 {
		t.Fatalf("locked resize result should not emit effects, got %#v", effects)
	}
	if next.Session.Cols != 80 || next.Surface.Cols != 80 {
		t.Fatalf("locked resize result must not resize session/surface, got session=%#v surface=%#v", next.Session, next.Surface)
	}
	binding, ok := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if !ok || !binding.SizeLocked || binding.CanResize || binding.ControlReason != "size_locked" || binding.ResizeEpoch != 3 {
		t.Fatalf("expected terminal size lock projection on binding, got %#v", binding)
	}
	if binding.Layout.SizeLocked {
		t.Fatalf("terminal size lock must not mutate view-local layout lock, got %#v", binding.Layout)
	}
}

func TestLiveAppInputDisplaysOnlyAfterSurfaceEventAndExitState(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 78, Rows: 20},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain input: %v", err)
	}
	if len(terminal.Inputs) != 1 || string(terminal.Inputs[0].Bytes) != "x" || terminal.Inputs[0].Channel != 4 {
		t.Fatalf("expected terminal service input, got %#v", terminal.Inputs)
	}
	beforeSurface := lastFrame(t, host.Frames())
	if frameContains(beforeSurface, "typed x") {
		t.Fatalf("runtime must not fake local echo before live surface event, got %#v", beforeSurface.Lines)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       78,
		Rows:       20,
		Lines:      []string{"$ typed x", "echo x"},
		Cursor:     state.LiveCursor{Visible: true, Row: 1, Col: 6, Shape: "bar"},
	}}); err != nil {
		t.Fatalf("post surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain surface: %v", err)
	}
	afterSurface := lastFrame(t, host.Frames())
	if !frameContains(afterSurface, "$ typed x") || !afterSurface.Cursor.Visible || afterSurface.Cursor.Shape != render.CursorShapeBar {
		t.Fatalf("expected service-returned live content and cursor, got lines=%#v cursor=%#v", afterSurface.Lines, afterSurface.Cursor)
	}
	if err := runtime.Post(LiveExitMsg{TerminalID: "term-1", ExitCode: 0, Reason: "shell exited"}); err != nil {
		t.Fatalf("post exit: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain exit: %v", err)
	}
	if runtime.State().Session.Attached || runtime.State().Session.State != state.TerminalLiveExited {
		t.Fatalf("expected detached exited session, got %#v", runtime.State().Session)
	}
	exitFrame := lastFrame(t, host.Frames())
	if !frameContains(exitFrame, "exited: term-1 code:0 shell exited") || !frameContains(exitFrame, "$ typed x") {
		t.Fatalf("expected exit status with preserved last surface, got %#v", exitFrame.Lines)
	}
}

func TestLiveInputRoutesToFocusedPaneTerminal(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 1, Cols: 78, Rows: 20},
	}
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		SplitActivePane(state.PaneState{ID: "pane-3", Title: "build", Kind: state.PaneTerminalLive, TerminalID: "term-3"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	host := NewFakeTerminalHost(16)
	host.SetSize(90, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: shell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 90, Rows: 24}}); err != nil {
		t.Fatalf("post main attach: %v", err)
	}
	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-2", Channel: 2, Cols: 42, Rows: 20, ViewID: state.TerminalPaneViewID("pane-2")}}); err != nil {
		t.Fatalf("post term-2 attach result: %v", err)
	}
	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-3", Channel: 3, Cols: 42, Rows: 20, ViewID: state.TerminalPaneViewID("pane-3")}}); err != nil {
		t.Fatalf("post term-3 attach result: %v", err)
	}
	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{Action: state.PaneCommandFocus, Target: state.PaneCommandTarget{PaneID: state.DefaultPaneID}, Source: state.PaneCommandSourceTest}}); err != nil {
		t.Fatalf("restore main focus: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}

	frame := lastFrame(t, host.Frames())
	pane2Content := frameHitRegion(t, frame, render.HitRegionPaneContent, "pane-2")
	if err := host.SendInput(mouseEventAt(pane2Content.Rect)); err != nil {
		t.Fatalf("click pane-2: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "b"}); err != nil {
		t.Fatalf("send pane-2 key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane-2 key: %v", err)
	}
	if got := runtime.State().Shell.EnsureDefaults().ActivePaneID; got != "pane-2" {
		t.Fatalf("click should focus pane-2, got %q", got)
	}

	frame = lastFrame(t, host.Frames())
	pane3Content := frameHitRegion(t, frame, render.HitRegionPaneContent, "pane-3")
	if err := host.SendInput(mouseEventAt(pane3Content.Rect)); err != nil {
		t.Fatalf("click pane-3: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "c"}); err != nil {
		t.Fatalf("send pane-3 key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane-3 key: %v", err)
	}

	if len(terminal.Inputs) != 2 {
		t.Fatalf("expected two routed inputs, got %#v", terminal.Inputs)
	}
	if terminal.Inputs[0].TerminalID != "term-2" || terminal.Inputs[0].Channel != 2 || string(terminal.Inputs[0].Bytes) != "b" {
		t.Fatalf("pane-2 input should route to term-2, got %#v", terminal.Inputs[0])
	}
	if terminal.Inputs[1].TerminalID != "term-3" || terminal.Inputs[1].Channel != 3 || string(terminal.Inputs[1].Bytes) != "c" {
		t.Fatalf("pane-3 input should route to term-3, got %#v", terminal.Inputs[1])
	}
}

func TestMousePaneContentActivationExitsInteractionModeBeforeLiveInput(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		SetInteractionMode(state.InteractionModePane)
	host := NewFakeTerminalHost(16)
	host.SetSize(90, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: shell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-main", Channel: 1, Cols: 42, Rows: 20, ViewID: state.TerminalPaneViewID(state.DefaultPaneID)}}); err != nil {
		t.Fatalf("post main attach result: %v", err)
	}
	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-2", Channel: 2, Cols: 42, Rows: 20, ViewID: state.TerminalPaneViewID("pane-2")}}); err != nil {
		t.Fatalf("post term-2 attach result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}

	frame := lastFrame(t, host.Frames())
	pane2Content := frameHitRegion(t, frame, render.HitRegionPaneContent, "pane-2")
	if err := host.SendInput(mouseEventAt(pane2Content.Rect)); err != nil {
		t.Fatalf("click pane-2: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"}); err != nil {
		t.Fatalf("send pane-2 key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane-2 key: %v", err)
	}

	shell = runtime.State().Shell.EnsureDefaults()
	if shell.ActivePaneID != "pane-2" || shell.ActiveFloatingID != "" || shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("content click should activate pane input mode, shell=%#v", shell)
	}
	if len(terminal.Inputs) != 1 || terminal.Inputs[0].TerminalID != "term-2" || terminal.Inputs[0].Channel != 2 || string(terminal.Inputs[0].Bytes) != "l" {
		t.Fatalf("pane input should route to clicked pane terminal after mode exit, inputs=%#v", terminal.Inputs)
	}
}

func TestLiveInputTargetsActiveFloatingBeforeTiledPane(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 1, Cols: 78, Rows: 20}}
	shell := state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-main")
	var result state.FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float", Kind: state.PaneTerminalLive, TerminalID: "term-float"},
		Rect:     state.FloatingRect{X: 10, Y: 4, W: 30, H: 8},
		Source:   state.PaneCommandSourceTest,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: shell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post main attach: %v", err)
	}
	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-float", Channel: 9, Cols: 28, Rows: 6, ViewID: state.TerminalFloatingViewID("floating-1")}}); err != nil {
		t.Fatalf("post floating attach result: %v", err)
	}
	if err := runtime.Post(ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandFocusRaise, TargetID: "floating-1", Source: state.PaneCommandSourceTest}}); err != nil {
		t.Fatalf("focus floating: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"}); err != nil {
		t.Fatalf("send floating key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating key: %v", err)
	}

	if len(terminal.Inputs) != 1 || terminal.Inputs[0].TerminalID != "term-float" || terminal.Inputs[0].Channel != 9 || string(terminal.Inputs[0].Bytes) != "f" {
		t.Fatalf("active floating input should route to floating terminal, got %#v", terminal.Inputs)
	}
}

func TestMouseFloatingContentActivationExitsInteractionModeBeforeLiveInput(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	shell := state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-main")
	var result state.FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float", Kind: state.PaneTerminalLive, TerminalID: "term-float"},
		Rect:     state.FloatingRect{X: 10, Y: 4, W: 30, H: 8},
		Source:   state.PaneCommandSourceTest,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}
	shell = shell.SetInteractionMode(state.InteractionModeFloating)
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: shell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-main", Channel: 1, Cols: 78, Rows: 20, ViewID: state.TerminalPaneViewID(state.DefaultPaneID)}}); err != nil {
		t.Fatalf("post main attach result: %v", err)
	}
	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-float", Channel: 9, Cols: 28, Rows: 6, ViewID: state.TerminalFloatingViewID("floating-1")}}); err != nil {
		t.Fatalf("post floating attach result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}

	frame := lastFrame(t, host.Frames())
	floatingContent := frameActionHitRegion(t, frame, render.ActionFloatingRaise.String(), "floating-pane-1")
	if err := host.SendInput(mouseEventAt(floatingContent.Rect)); err != nil {
		t.Fatalf("click floating: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"}); err != nil {
		t.Fatalf("send floating key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating key: %v", err)
	}

	shell = runtime.State().Shell.EnsureDefaults()
	if shell.ActiveFloatingID != "floating-1" || shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("content click should activate floating input mode, shell=%#v", shell)
	}
	if len(terminal.Inputs) != 1 || terminal.Inputs[0].TerminalID != "term-float" || terminal.Inputs[0].Channel != 9 || string(terminal.Inputs[0].Bytes) != "f" {
		t.Fatalf("floating input should route to clicked floating terminal after mode exit, inputs=%#v", terminal.Inputs)
	}
}

func TestTerminalPoolPaneReattachDoesNotStealSharedFloatingBinding(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{
			TerminalID:   "term-b",
			Channel:      9,
			Cols:         80,
			Rows:         24,
			ResizePolicy: state.TerminalResizeRoleFollower,
			SurfaceID:    "surface",
		},
	}
	shell := state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-a")
	var result state.FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float", Kind: state.PaneTerminalLive, TerminalID: "term-a"},
		Rect:     state.FloatingRect{X: 8, Y: 4, W: 40, H: 10},
		Source:   state.PaneCommandSourceTest,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-a", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindFloating(state.NewFloatingTerminalView("floating-1", "floating-pane-1", "term-a", 8, 80, 24, state.TerminalResizeRoleFollower, "surface", state.TerminalFloatingViewID("floating-1"), false))
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})

	root, effects := reducer(root, TerminalPoolAttachRequestMsg{TerminalID: "term-b", TargetPaneID: state.DefaultPaneID})
	if len(effects) != 1 {
		t.Fatalf("expected attach effect, got %#v", effects)
	}
	msg, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolAttachResultMsg)
	if !ok {
		t.Fatalf("expected attach result, got %#v", msg)
	}
	root, _ = reducer(root, msg)

	paneBinding, ok := root.TerminalViews.PaneBinding(state.DefaultPaneID)
	if !ok || paneBinding.TerminalID != "term-b" || paneBinding.Channel != 9 {
		t.Fatalf("pane should rebind to term-b, binding=%#v ok=%v", paneBinding, ok)
	}
	floatingBinding, ok := root.TerminalViews.FloatingBinding("floating-1")
	if !ok || floatingBinding.TerminalID != "term-a" || floatingBinding.Channel != 8 {
		t.Fatalf("floating binding must remain on term-a, binding=%#v ok=%v", floatingBinding, ok)
	}
	root.Shell, _ = root.Shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandFocusRaise, TargetID: "floating-1", Source: state.PaneCommandSourceTest})
	target, ok := liveInputTarget(root)
	if !ok || target.TerminalID != "term-a" || target.Channel != 8 || !target.Floating {
		t.Fatalf("focused floating input must keep its own binding, target=%#v ok=%v", target, ok)
	}
}

func TestLiveInputDoesNotFallbackToSessionForEmptyActivePane(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-main", Channel: 1, Cols: 78, Rows: 20}}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(ShellSplitActivePaneMsg{
		Pane:      state.PaneState{ID: "pane-empty", Title: "empty", Kind: state.PaneEmpty},
		Direction: state.SplitDirectionVertical,
	}); err != nil {
		t.Fatalf("split empty pane: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain empty split: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}); err != nil {
		t.Fatalf("send empty pane key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain empty pane key: %v", err)
	}

	if len(terminal.Inputs) != 0 {
		t.Fatalf("empty active pane must not receive old session terminal input, got %#v", terminal.Inputs)
	}
	if toasts := runtime.State().Shell.Toasts; len(toasts) == 0 || toasts[len(toasts)-1].Body != "no terminal bound" {
		t.Fatalf("empty active pane should show explicit input state, got %#v", runtime.State().Shell.Toasts)
	}
}

func TestFloatingEmptyPaneAttachesExistingTerminalFromPicker(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{Channel: 9},
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-float",
			Title:      "floating shell",
			State:      "running",
		}}},
	}
	shell := state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-main")
	var result state.FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float slot", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 10, Y: 5, W: 30, H: 8},
		Source:   state.PaneCommandSourceTest,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(90, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: shell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-main", Cols: 90, Rows: 24}}); err != nil {
		t.Fatalf("post main attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain main attach: %v", err)
	}
	terminal.Resizes = nil

	emptyAttach := frameActionHitRegion(t, lastFrame(t, host.Frames()), "empty.attach", "floating-pane-1")
	if err := host.SendInput(mouseEventAt(emptyAttach.Rect)); err != nil {
		t.Fatalf("send floating empty attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating empty attach: %v", err)
	}
	if len(terminal.Lists) != 1 || runtime.State().Shell.Overlay.Kind != state.OverlayTerminalPicker || runtime.State().Shell.Overlay.TargetID != "floating-1" {
		t.Fatalf("empty attach should open picker for floating, lists=%#v overlay=%#v", terminal.Lists, runtime.State().Shell.Overlay)
	}
	if !frameContains(lastFrame(t, host.Frames()), "floating shell") || frameContains(lastFrame(t, host.Frames()), "@pool") {
		t.Fatalf("picker should render pool terminal row, got %#v", lastFrame(t, host.Frames()).Lines)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "o"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "a"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "t"},
		{Kind: input.EventKindKey, Key: input.KeyEnter},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send picker input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain picker input %#v: %v", event, err)
		}
	}

	if len(terminal.Attaches) < 2 {
		t.Fatalf("expected main attach and floating attach, got %#v", terminal.Attaches)
	}
	floatAttach := terminal.Attaches[len(terminal.Attaches)-1]
	if floatAttach.TerminalID != "term-float" || floatAttach.Cols != 28 || floatAttach.Rows != 6 {
		t.Fatalf("floating attach should use floating content rect, got %#v", floatAttach)
	}
	floating := runtime.State().Shell.EnsureDefaults().Floatings[0]
	if !floating.Active || floating.Pane.Kind != state.PaneTerminalLive || floating.Pane.TerminalID != "term-float" || runtime.State().Shell.Overlay.Open {
		t.Fatalf("floating should bind selected terminal and close picker, floating=%#v overlay=%#v", floating, runtime.State().Shell.Overlay)
	}
	if len(terminal.Resizes) != 0 {
		t.Fatalf("attach result already matches floating content rect, resize should dedupe, got %#v", terminal.Resizes)
	}

	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-float",
		Revision:   1,
		Cols:       28,
		Rows:       6,
		Lines:      []string{"floating ready"},
	}}); err != nil {
		t.Fatalf("post floating surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating surface: %v", err)
	}
	if !frameContains(lastFrame(t, host.Frames()), "floating ready") {
		t.Fatalf("floating should render terminal live content, got %#v", lastFrame(t, host.Frames()).Lines)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"}); err != nil {
		t.Fatalf("send floating key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating key: %v", err)
	}
	if got := terminal.Inputs[len(terminal.Inputs)-1]; got.TerminalID != "term-float" || got.Channel != 9 || string(got.Bytes) != "f" {
		t.Fatalf("floating input should route to attached terminal, got %#v all=%#v", got, terminal.Inputs)
	}

	if err := runtime.Post(ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandClose, TargetID: "floating-1", Source: state.PaneCommandSourceTest}}); err != nil {
		t.Fatalf("post floating close: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating close: %v", err)
	}
	if len(runtime.State().Shell.Floatings) != 0 || len(terminal.Kills) != 0 {
		t.Fatalf("closing floating should remove window without killing terminal, floatings=%#v kills=%#v", runtime.State().Shell.Floatings, terminal.Kills)
	}
}

func TestActiveFloatingResizeCommandResizesAttachedTerminalContentRect(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-float", Channel: 5, Cols: 28, Rows: 6}}
	shell := state.DefaultShell()
	var result state.FloatingCommandResult
	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float", Kind: state.PaneTerminalLive, TerminalID: "term-float"},
		Rect:     state.FloatingRect{X: 8, Y: 4, W: 30, H: 8},
		Source:   state.PaneCommandSourceTest,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(90, 24)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: shell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(TerminalPoolAttachResultMsg{
		TerminalID:       "term-float",
		TargetFloatingID: "floating-1",
		Result:           services.TerminalAttachResult{TerminalID: "term-float", Channel: 5, Cols: 28, Rows: 6, ResizePolicy: state.TerminalResizeRoleOwner, CanResize: true},
	}); err != nil {
		t.Fatalf("post floating attach result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating attach result: %v", err)
	}
	terminal.Resizes = nil

	if err := runtime.Post(ShellFloatingCommandMsg{Command: state.FloatingCommand{
		Action:   state.FloatingCommandResize,
		TargetID: "floating-1",
		DeltaW:   4,
		DeltaH:   2,
		Source:   state.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post floating resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating resize: %v", err)
	}
	if len(terminal.Resizes) != 1 {
		t.Fatalf("active floating resize should emit one terminal resize, got %#v", terminal.Resizes)
	}
	if got := terminal.Resizes[0]; got.TerminalID != "term-float" || got.Channel != 5 || got.Cols != 32 || got.Rows != 8 {
		t.Fatalf("floating resize should use updated content rect, got %#v", got)
	}
}

func TestLiveAppAttachSwitchClearsStaleSurfaceRows(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-new", Channel: 5, Cols: 78, Rows: 20},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{
			Surface: state.TerminalSurfaceStore{
				TerminalID: "term-old",
				Ready:      true,
				Lines:      []string{"old terminal output"},
			},
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-new", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	frame := lastFrame(t, host.Frames())
	if frameContains(frame, "old terminal output") {
		t.Fatalf("new attach must not render stale live rows, got %#v", frame.Lines)
	}
	if frameContains(frame, "old terminal output") || !frameContains(frame, "live surface empty") || !runtime.State().Surface.Ready {
		t.Fatalf("expected empty ready surface after terminal switch, frame=%#v state=%#v", frame.Lines, runtime.State().Surface)
	}
}

func TestLiveAppAttachHydratesReadySurfaceFromService(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8, Cols: 78, Rows: 20},
		SurfaceResult: services.TerminalSurfaceResult{
			Ready: true,
			Snapshot: state.LiveSurfaceSnapshot{
				TerminalID: "term-1",
				Cols:       78,
				Rows:       20,
				Lines:      []string{"alpha", "beta 你好🚀"},
				Cursor:     state.LiveCursor{Visible: true, Row: 1, Col: 8, Shape: "bar"},
			},
		},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	if len(terminal.Surfaces) != 1 || terminal.Surfaces[0].TerminalID != "term-1" || terminal.Surfaces[0].Rows != 20 {
		t.Fatalf("expected live surface request after attach, got %#v", terminal.Surfaces)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "alpha") || !frameContains(frame, "beta 你好🚀") || !frame.Cursor.Visible || frame.Cursor.Shape != render.CursorShapeBar {
		t.Fatalf("expected hydrated live surface in frame, lines=%#v cursor=%#v", frame.Lines, frame.Cursor)
	}
}

func TestLiveAppAttachClearsPendingForEmptySurfaceSnapshot(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-blank", Channel: 8, Cols: 78, Rows: 20},
		SurfaceResult: services.TerminalSurfaceResult{
			Snapshot: state.LiveSurfaceSnapshot{
				TerminalID: "term-blank",
				Cols:       78,
				Rows:       20,
			},
		},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-blank", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	if !runtime.State().Surface.Ready {
		t.Fatalf("empty snapshot should still mark live surface ready: %#v", runtime.State().Surface)
	}
	frame := lastFrame(t, host.Frames())
	if frameContains(frame, "live surface pending") {
		t.Fatalf("empty ready snapshot must clear pending fallback, frame=%#v", frame.Lines)
	}
}

func TestLiveRuntimeConsumesBackendLiveEventsAndRedraws(t *testing.T) {
	liveEvents := make(chan services.TerminalLiveEvent, 2)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8, Cols: 78, Rows: 20},
		LiveEventsCh: liveEvents,
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewAsyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	liveEvents <- services.TerminalLiveEvent{
		TerminalID: "term-1",
		Ready:      true,
		Snapshot: state.LiveSurfaceSnapshot{
			TerminalID: "term-1",
			Cols:       78,
			Rows:       20,
			Lines:      []string{"backend live update"},
		},
	}
	if err := drainUntilFrameContains(context.Background(), runtime, host, "backend live update"); err != nil {
		t.Fatal(err)
	}
	if len(terminal.LiveEventRequests) != 1 || terminal.LiveEventRequests[0].TerminalID != "term-1" {
		t.Fatalf("expected live event subscription after attach, got %#v", terminal.LiveEventRequests)
	}
}

func TestLiveEventUsesEventTerminalIDWhenSnapshotTerminalIDMissing(t *testing.T) {
	root := state.Root{
		Surface: (state.TerminalSurfaceStore{}).ApplySnapshot(state.LiveSurfaceSnapshot{
			TerminalID: "term-main",
			Revision:   2,
			Cols:       80,
			Rows:       24,
			Lines:      []string{"main"},
		}),
	}
	reducer := NewLiveReducer(LiveDeps{})
	next, _ := reducer(root, LiveEventMsg{Event: services.TerminalLiveEvent{
		TerminalID: "term-logs",
		Ready:      true,
		Snapshot: state.LiveSurfaceSnapshot{
			Revision: 1,
			Cols:     40,
			Rows:     12,
			Lines:    []string{"logs"},
		},
	}})

	if got := next.Surface.SurfaceForTerminal("term-main").Lines[0]; got != "main" {
		t.Fatalf("event for another terminal must not overwrite current projection, got %q", got)
	}
	logs := next.Surface.SurfaceForTerminal("term-logs")
	if logs.TerminalID != "term-logs" || logs.Lines[0] != "logs" || logs.Revision != 1 {
		t.Fatalf("expected event terminal id to be applied to snapshot, got %#v", logs)
	}
}

func TestLiveSurfaceStoreKeepsPaneTerminalBindingsIsolated(t *testing.T) {
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-main"
	shell = shell.SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-logs"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID})
	host := NewFakeTerminalHost(8)
	host.SetSize(100, 24)
	root := state.Root{Shell: shell}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-main", 7, 48, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindPane(state.NewPaneTerminalView("pane-2", "term-logs", 8, 48, 20, state.TerminalResizeRoleFollower, "surface", state.TerminalPaneViewID("pane-2"), false))
	runtime := NewLiveRuntime(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
	)

	for _, msg := range []Msg{
		LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{TerminalID: "term-main", Cols: 48, Rows: 20, Lines: []string{"main-only"}}},
		LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{TerminalID: "term-logs", Cols: 48, Rows: 20, Lines: []string{"logs-only"}}},
		NoopMsg{},
	} {
		if err := runtime.Post(msg); err != nil {
			t.Fatalf("post %T: %v", msg, err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "main-only") || !frameContains(frame, "logs-only") {
		t.Fatalf("expected both pane-bound live surfaces, got %#v", frame.Lines)
	}

	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{TerminalID: "term-old", Cols: 48, Rows: 20, Lines: []string{"old-stale"}}}); err != nil {
		t.Fatalf("post stale surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain stale surface: %v", err)
	}
	frame = lastFrame(t, host.Frames())
	if frameContains(frame, "old-stale") {
		t.Fatalf("unbound old terminal update must not render into active panes, got %#v", frame.Lines)
	}
	if got := runtime.State().Surface.SurfaceForTerminal("term-main").Lines[0]; got != "main-only" {
		t.Fatalf("old terminal update polluted main binding, got %q", got)
	}
}

func TestLiveContentRendererKeepsStyleCursorAndChromeSafe(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 9, Cols: 28, Rows: 10},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(30, 12)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 30, Rows: 12}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       28,
		Rows:       8,
		Lines: []string{
			"\x1b[31mERR\x1b[0m 你好🚀 output that must clip before chrome",
			"\x1b[32mOK\x1b[0m done",
		},
		Cursor: state.LiveCursor{Visible: true, Row: 1, Col: 7, Shape: "bar"},
	}}); err != nil {
		t.Fatalf("post surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain surface: %v", err)
	}

	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "ERR 你好🚀") || !frameContains(frame, "OK done") {
		t.Fatalf("expected live content in pane content rect, got %#v", frame.Lines)
	}
	if frameContains(frame, "\x1b[") {
		t.Fatalf("plain frame must not leak raw ANSI, got %#v", frame.Lines)
	}
	assertPaneVisualState(t, frame, "ERR", render.StyleDanger)
	assertPaneVisualState(t, frame, "OK", render.StyleSuccess)
	if !ansiFrameContains(frame, "\x1b[38;2;") {
		t.Fatalf("ANSI frame must contain SGR styled live content, got %#v", frame.ANSILines)
	}
	if !frame.Cursor.Visible || frame.Cursor.Shape != render.CursorShapeBar {
		t.Fatalf("expected live content cursor metadata, got %#v", frame.Cursor)
	}
	for i, line := range frame.Lines {
		if width := render.DisplayWidth(line); width != 30 {
			t.Fatalf("row %d width=%d want=30 line=%q", i, width, line)
		}
	}
	if right := render.SliceCells(frame.Lines[2], 29, 30); right != "│" {
		t.Fatalf("right pane border must survive live ANSI/wide clipping, got %q in %#v", right, frame.Lines)
	}
}

func TestLiveAttachUsesCardContentRectForInitialTerminalSize(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 7}}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(terminal.Attaches) != 1 {
		t.Fatalf("expected one attach, got %#v", terminal.Attaches)
	}
	if got := terminal.Attaches[0]; got.Cols != 78 || got.Rows != 20 {
		t.Fatalf("attach must use card content rect, got %#v", got)
	}
}

func TestAttachResultWithExistingSizeIsCorrectedToContentRect(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 7, Cols: 80, Rows: 24}}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if got := terminal.Attaches[0]; got.Cols != 78 || got.Rows != 20 {
		t.Fatalf("attach request must use content rect, got %#v", got)
	}
	if len(terminal.Resizes) != 1 {
		t.Fatalf("expected attach result correction resize, got %#v", terminal.Resizes)
	}
	if got := terminal.Resizes[0]; got.Cols != 78 || got.Rows != 20 {
		t.Fatalf("resize correction must use content rect, got %#v", got)
	}
}

func TestHostResizeUsesActiveContentRectAndDeduplicates(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5}}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send duplicate resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain resize: %v", err)
	}

	if len(terminal.Resizes) != 1 {
		t.Fatalf("expected one deduplicated content resize, got %#v", terminal.Resizes)
	}
	if got := terminal.Resizes[0]; got.Cols != 98 || got.Rows != 36 {
		t.Fatalf("host resize must use card content rect, got %#v", got)
	}
}

func TestLiveResizeKeepsLatestContentRectAndIgnoresOldSizeSurface(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5}}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   1,
		Cols:       78,
		Rows:       20,
		Lines:      []string{"before resize"},
	}}); err != nil {
		t.Fatalf("post initial surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain initial surface: %v", err)
	}
	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send host resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain host resize: %v", err)
	}
	if got := terminal.Resizes[len(terminal.Resizes)-1]; got.Cols != 98 || got.Rows != 36 {
		t.Fatalf("host resize must use latest content rect, got %#v", got)
	}
	if runtime.State().Surface.Cols != 98 || runtime.State().Surface.Rows != 36 {
		t.Fatalf("surface resize boundary should project latest content rect, got %#v", runtime.State().Surface)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       78,
		Rows:       20,
		Lines:      []string{"late old size"},
	}}); err != nil {
		t.Fatalf("post old-size surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain old-size surface: %v", err)
	}
	oldSizeFrame := lastFrame(t, host.Frames())
	if frameContains(oldSizeFrame, "late old size") || runtime.State().Surface.Cols != 98 || runtime.State().Surface.Rows != 36 {
		t.Fatalf("late old-size surface must not roll back resized frame/state, frame=%#v state=%#v", oldSizeFrame.Lines, runtime.State().Surface)
	}

	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   3,
		Cols:       98,
		Rows:       36,
		Lines:      []string{"after resize"},
		Cursor:     state.LiveCursor{Visible: true, Row: 1, Col: 5, Shape: "bar"},
		Modes:      state.LiveTerminalModes{MouseTracking: true, MouseSGR: true},
	}}); err != nil {
		t.Fatalf("post resized surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain resized surface: %v", err)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "after resize") || frameContains(frame, "late old size") {
		t.Fatalf("expected resized live surface only, got %#v", frame.Lines)
	}
	if runtime.State().Surface.Cols != 98 || runtime.State().Surface.Rows != 36 || runtime.State().Surface.ResizeBoundary.Active {
		t.Fatalf("matching resized surface should clear resize boundary, got %#v", runtime.State().Surface)
	}
	if !frame.Cursor.Visible || frame.Cursor.Shape != render.CursorShapeBar {
		t.Fatalf("resized live surface should preserve cursor, got %#v", frame.Cursor)
	}
	vm := render.NewRenderVMBuilder().Build(runtime.State())
	panel, ok := activePanelVMForAppTest(vm.Shell)
	if !ok {
		t.Fatalf("expected active panel VM, got %#v", vm.Shell.Layout.Panels)
	}
	if panel.Content.Extent != (render.ContentExtent{Known: true, Cols: 98, Rows: 36}) {
		t.Fatalf("active content should expose resized live extent, got %#v", panel.Content.Extent)
	}
	layout := render.MeasureLayout(vm.Shell, vm.Shell.Layout.Viewport)
	contentRect, ok := activeContentRectForAppTest(layout)
	if !ok || contentRect.W != 98 || contentRect.H != 36 {
		t.Fatalf("layout should allocate resized active content rect, rect=%#v ok=%v layout=%#v", contentRect, ok, layout)
	}
	wantCursorRect := render.Rect{X: contentRect.X + 5, Y: contentRect.Y + 1, W: 1, H: 1}
	if frame.CursorRect != wantCursorRect || layout.CursorRect != wantCursorRect {
		t.Fatalf("cursor should stay content-local after resize, frame=%#v layout=%#v want=%#v", frame.CursorRect, layout.CursorRect, wantCursorRect)
	}
	if !runtime.State().Surface.Modes.MousePassthroughEnabled() {
		t.Fatalf("resized live surface should preserve mouse modes, got %#v", runtime.State().Surface.Modes)
	}
	for i, line := range frame.Lines {
		if width := render.DisplayWidth(line); width != 100 {
			t.Fatalf("resized frame row %d width=%d want=100 line=%q", i, width, line)
		}
	}
}

func TestLiveResizeFallbacksDoNotUseResizeBoundaryDots(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5}}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send host resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain host resize: %v", err)
	}

	emptyFrame := lastFrame(t, host.Frames())
	if !frameContains(emptyFrame, "live surface empty") {
		t.Fatalf("expected empty fallback after resize without surface, got %#v", emptyFrame.Lines)
	}
	emptyLayer, ok := firstPanelLayerForAppTest(render.NewRenderer(render.DefaultTheme()).RenderResult(render.NewRenderVMBuilder().Build(runtime.State())))
	if !ok {
		t.Fatalf("expected empty panel layer")
	}
	if panelLayerContainsPlainForAppTest(emptyLayer, "·") {
		t.Fatalf("empty fallback should not be filled by resize-boundary dots, got %#v", emptyLayer.Lines)
	}

	if err := runtime.Post(LiveExitMsg{TerminalID: "term-1", ExitCode: 0}); err != nil {
		t.Fatalf("post exit: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain exit: %v", err)
	}
	exitFrame := lastFrame(t, host.Frames())
	if !frameContains(exitFrame, "terminal exited: term-1 code:0") {
		t.Fatalf("expected exited fallback after resize, got %#v", exitFrame.Lines)
	}
	exitLayer, ok := firstPanelLayerForAppTest(render.NewRenderer(render.DefaultTheme()).RenderResult(render.NewRenderVMBuilder().Build(runtime.State())))
	if !ok {
		t.Fatalf("expected exited panel layer")
	}
	if panelLayerContainsPlainForAppTest(exitLayer, "·") {
		t.Fatalf("exited fallback should not be filled by resize-boundary dots, got %#v", exitLayer.Lines)
	}
}

func TestLiveSurfaceProtocolTerminalExitedIsNotErrorUI(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5},
		SurfaceErr:   errors.New("protocol error 400: terminal exited"),
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(100, 40)
	runtime := NewLiveRuntime(
		state.Root{Surface: state.TerminalSurfaceStore{TerminalID: "term-1", Lines: []string{"last output"}}},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 100, Rows: 40}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	root := runtime.State()
	if root.Session.LastError != "" || root.Surface.Err != "" {
		t.Fatalf("terminal exited should not be stored as live error, session=%q surface=%q", root.Session.LastError, root.Surface.Err)
	}
	if root.Session.State != state.TerminalLiveExited || root.Surface.State != state.TerminalLiveExited {
		t.Fatalf("expected exited state, session=%s surface=%s", root.Session.State, root.Surface.State)
	}
	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "last output") ||
		!frameContains(frame, "► R restart current terminal ◄") ||
		!frameContains(frame, "[ Ctrl-F choose another terminal ]") ||
		frameContains(frame, "protocol error 400") {
		t.Fatalf("expected exited content with restart hints and no protocol error, got %#v", frame.Lines)
	}
}

func TestLiveResizeOverflowMarkersStayOnChrome(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5}}
	host := NewFakeTerminalHost(16)
	host.SetSize(100, 40)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 100, Rows: 40}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendResize(80, 24); err != nil {
		t.Fatalf("send shrink resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain shrink resize: %v", err)
	}
	if got := terminal.Resizes[len(terminal.Resizes)-1]; got.Cols != 78 || got.Rows != 20 {
		t.Fatalf("shrunk viewport should resize terminal to content rect, got %#v", got)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       120,
		Rows:       30,
		Lines:      []string{"terminal output should clip along right edge after resize"},
	}}); err != nil {
		t.Fatalf("post oversized surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain oversized surface: %v", err)
	}

	frame := lastFrame(t, host.Frames())
	if !frameContains(frame, "terminal output should clip") {
		t.Fatalf("expected oversized live surface content, got %#v", frame.Lines)
	}
	result := render.NewRenderer(render.DefaultTheme()).RenderResult(render.NewRenderVMBuilder().Build(runtime.State()))
	panelLayer, ok := firstPanelLayerForAppTest(result)
	if !ok || panelLayer.ContentOverflow != (render.ContentOverflow{Right: true, Bottom: true}) {
		t.Fatalf("live resize mismatch should expose chrome overflow, layer=%#v ok=%v", panelLayer, ok)
	}
	rightMarkerRow := panelLayer.Rect.Y + panelLayer.Rect.H/2
	rightMarkerCol := panelLayer.Rect.X + panelLayer.Rect.W - 1
	if got := render.SliceCells(frame.Lines[rightMarkerRow], rightMarkerCol, rightMarkerCol+1); got != ">" {
		t.Fatalf("right overflow marker should be shown for live resize mismatch, got %q frame=%#v", got, frame.Lines)
	}
	bottomMarkerRow := panelLayer.Rect.Y + panelLayer.Rect.H - 1
	bottomMarkerCol := panelLayer.Rect.X + panelLayer.Rect.W/2
	if got := render.SliceCells(frame.Lines[bottomMarkerRow], bottomMarkerCol, bottomMarkerCol+1); got != "v" {
		t.Fatalf("bottom overflow marker should be shown for live resize mismatch, got %q frame=%#v", got, frame.Lines)
	}
	for _, line := range panelLayer.Lines {
		if strings.Contains(line.PlainString(), ">") || strings.Contains(line.PlainString(), "v") {
			t.Fatalf("overflow markers must stay out of panel content layer, got %#v", panelLayer.Lines)
		}
	}
	for i, line := range frame.Lines {
		if width := render.DisplayWidth(line); width != 80 {
			t.Fatalf("shrunk frame row %d width=%d want=80 line=%q", i, width, line)
		}
	}
}

func TestHostResizeUsesBusinessActivePaneWhenFloatingOwnsVisualFocus(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5}}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(ShellFloatingCommandMsg{Command: state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "float", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 10, Y: 4, W: 30, H: 8},
		Source:   state.PaneCommandSourceTest,
	}}); err != nil {
		t.Fatalf("post floating: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating: %v", err)
	}
	if !runtime.State().Shell.Floatings[0].Active {
		t.Fatalf("test expects active floating, got %#v", runtime.State().Shell.Floatings)
	}
	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain resize: %v", err)
	}

	if len(terminal.Resizes) == 0 {
		t.Fatalf("floating visual focus must not block business active pane resize")
	}
	if got := terminal.Resizes[len(terminal.Resizes)-1]; got.Cols != 98 || got.Rows != 36 {
		t.Fatalf("resize should still use tiled business active pane content rect, got %#v all=%#v", got, terminal.Resizes)
	}
}

func TestHeaderFooterHideResizesTerminalWithReclaimedContentRows(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 6}}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	terminal.Resizes = nil
	if err := runtime.Post(ShellSetHeaderVisibleMsg{Visible: false}); err != nil {
		t.Fatalf("post header hide: %v", err)
	}
	if err := runtime.Post(ShellSetFooterVisibleMsg{Visible: false}); err != nil {
		t.Fatalf("post footer hide: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain chrome resize: %v", err)
	}

	if len(terminal.Resizes) < 2 {
		t.Fatalf("header/footer chrome changes must drive at least the intermediate and final PTY resize, got %#v", terminal.Resizes)
	}
	if got := terminal.Resizes[len(terminal.Resizes)-1]; got.Cols != 78 || got.Rows != 22 {
		t.Fatalf("hidden header/footer must reclaim content rows, got %#v", got)
	}
}

func TestSplitPresentationUsesSplitContentRectForResize(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8}}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post split presentation: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(terminal.Resizes) != 0 {
		t.Fatalf("single split-line pane now shares the same card content rect as card, got %#v", terminal.Resizes)
	}
}

func TestVerticalSplitActivePaneReservesDividerCellForResize(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8}}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post split presentation: %v", err)
	}
	if err := runtime.Post(ShellSplitActivePaneMsg{
		Pane:      state.PaneState{ID: "pane-2", Title: "right", Kind: state.PaneTerminalLive},
		Direction: state.SplitDirectionVertical,
	}); err != nil {
		t.Fatalf("post split active pane: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split: %v", err)
	}

	if got := terminal.Resizes[len(terminal.Resizes)-1]; got.Cols != 38 || got.Rows != 20 {
		t.Fatalf("active pane right of divider must reserve split divider without shell frame inset, got %#v", got)
	}
}

func TestNestedEmptySplitResizesOwnerTerminalViewContentRect(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8}}
	host := NewFakeTerminalHost(16)
	host.SetSize(140, 36)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 140, Rows: 36}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post split presentation: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	terminal.Resizes = nil

	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:         state.PaneCommandSplit,
		SplitDirection: state.SplitDirectionVertical,
		NewPane:        state.PaneState{ID: "pane-right", Title: "pane", Kind: state.PaneEmpty},
	}}); err != nil {
		t.Fatalf("post right split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain right split: %v", err)
	}
	rightSplitResize := terminal.Resizes[len(terminal.Resizes)-1]

	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:         state.PaneCommandSplit,
		Target:         state.PaneCommandTarget{PaneID: "pane-right"},
		SplitDirection: state.SplitDirectionHorizontal,
		NewPane:        state.PaneState{ID: "pane-right-bottom", Title: "pane", Kind: state.PaneEmpty},
	}}); err != nil {
		t.Fatalf("post lower right split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain lower right split: %v", err)
	}

	if len(terminal.Resizes) != 1 {
		t.Fatalf("splitting empty right pane must not resize unchanged left owner again, before=%#v all=%#v", rightSplitResize, terminal.Resizes)
	}
	if got := terminal.Resizes[0]; got.TerminalID != "term-1" || got.ViewID != state.TerminalPaneViewID(state.DefaultPaneID) || got.Cols <= 0 || got.Rows <= 0 {
		t.Fatalf("right split should resize original owner terminal view, got %#v", got)
	}
	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Revision:   2,
		Cols:       terminal.Resizes[0].Cols,
		Rows:       terminal.Resizes[0].Rows,
		Lines:      []string{"owner terminal content"},
	}}); err != nil {
		t.Fatalf("post owner-sized surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain owner-sized surface: %v", err)
	}
	if binding, ok := runtime.State().TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.DesiredCols != terminal.Resizes[0].Cols || binding.DesiredRows != terminal.Resizes[0].Rows {
		t.Fatalf("owner view desired size should track left content rect, binding=%#v resize=%#v", binding, terminal.Resizes[0])
	}
	if _, ok := runtime.State().TerminalViews.PaneBinding("pane-right"); ok {
		t.Fatalf("empty right pane must not create terminal binding")
	}
	vm := render.NewRenderVMBuilder().Build(runtime.State())
	plan := render.MeasureLayout(vm.Shell, vm.Shell.Layout.Viewport)
	result := render.NewRenderer(render.DefaultTheme()).RenderResult(vm)
	ownerLayer, ok := panelLayerByPaneIDForAppTest(result, plan, state.DefaultPaneID)
	if !ok {
		t.Fatalf("expected owner pane layer")
	}
	if ownerLayer.ContentOverflow != (render.ContentOverflow{}) {
		t.Fatalf("owner pane should not overflow after 211 empty split, got %#v", ownerLayer.ContentOverflow)
	}
	if panelLayerContainsPlainForAppTest(ownerLayer, "·") {
		t.Fatalf("owner pane should not show resize-boundary dots after 211 empty split, got %#v", ownerLayer.Lines)
	}
}

func TestPaneSizeCommandResizesActiveTerminalContentRect(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8}}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post split presentation: %v", err)
	}
	if err := runtime.Post(ShellSplitActivePaneMsg{
		Pane:      state.PaneState{ID: "pane-2", Title: "right", Kind: state.PaneTerminalLive},
		Direction: state.SplitDirectionVertical,
	}); err != nil {
		t.Fatalf("post split active pane: %v", err)
	}
	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:   state.PaneCommandSetSize,
		Target:   state.PaneCommandTarget{PaneID: "pane-2"},
		SizeMode: state.PaneSizeCells,
		Cols:     24,
	}}); err != nil {
		t.Fatalf("post fixed size command: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain size command: %v", err)
	}

	if got := terminal.Resizes[len(terminal.Resizes)-1]; got.Cols != 22 || got.Rows != 20 {
		t.Fatalf("fixed right pane size must drive active content resize, got %#v all=%#v", got, terminal.Resizes)
	}
}

func TestBatchedPaneCommandsResizeTerminalToLatestContentRect(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8}}
	host := NewFakeTerminalHost(16)
	host.SetSize(100, 40)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 100, Rows: 40}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	terminal.Resizes = nil
	for _, command := range []state.PaneCommand{
		{
			Action:         state.PaneCommandSplit,
			SplitDirection: state.SplitDirectionVertical,
			NewPane:        state.PaneState{ID: "pane-2", Title: "right", Kind: state.PaneTerminalLive},
		},
		{Action: state.PaneCommandResize, Target: state.PaneCommandTarget{PaneID: "pane-2"}, ResizeDirection: state.PaneResizeLeft, Delta: 6},
		{Action: state.PaneCommandZoom, Target: state.PaneCommandTarget{PaneID: "pane-2"}},
	} {
		if err := runtime.Post(ShellPaneCommandMsg{Command: command}); err != nil {
			t.Fatalf("post pane command: %v", err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane commands: %v", err)
	}

	if len(terminal.Resizes) < 2 {
		t.Fatalf("expected split and zoom resize requests, got %#v", terminal.Resizes)
	}
	if got := terminal.Resizes[len(terminal.Resizes)-1]; got.Cols != 98 || got.Rows != 36 {
		t.Fatalf("latest zoomed pane content rect must win, got %#v all=%#v", got, terminal.Resizes)
	}
	if runtime.State().Session.Cols != 98 || runtime.State().Session.Rows != 36 {
		t.Fatalf("stale split resize result must not override latest session size, state=%#v", runtime.State().Session)
	}
}

func TestClosePaneTransfersResizeOwnerAndRestoresFullContentRect(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 8}}
	host := NewFakeTerminalHost(16)
	host.SetSize(120, 40)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 120, Rows: 40}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:         state.PaneCommandSplit,
		SplitDirection: state.SplitDirectionVertical,
		NewPane:        state.PaneState{ID: "pane-2", Title: "right", Kind: state.PaneTerminalLive},
	}}); err != nil {
		t.Fatalf("post split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split: %v", err)
	}
	terminal.Resizes = nil
	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action: state.PaneCommandClose,
		Target: state.PaneCommandTarget{PaneID: "pane-2"},
	}}); err != nil {
		t.Fatalf("post close: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain close: %v", err)
	}

	if len(terminal.Resizes) == 0 {
		t.Fatalf("close pane should trigger owner resize to restored content rect")
	}
	if got := terminal.Resizes[len(terminal.Resizes)-1]; got.Cols != 118 || got.Rows != 36 {
		t.Fatalf("close pane should restore full content rect, got %#v all=%#v", got, terminal.Resizes)
	}
	if binding, ok := runtime.State().TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.ResizeRole != state.TerminalResizeRoleOwner || !binding.CanResize {
		t.Fatalf("remaining pane should own resize after sibling close, binding=%#v ok=%v", binding, ok)
	}
}

func TestLiveAppShowsTerminalServiceError(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachErr: errors.New("attach failed"),
	}
	host := NewFakeTerminalHost(4)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if runtime.State().Session.LastError != "attach failed" {
		t.Fatalf("expected attach error in state, got %#v", runtime.State())
	}
	frames := host.Frames()
	last := lastFrame(t, frames)
	if len(last.Lines) < 2 || !strings.Contains(last.Lines[len(last.Lines)-1], "attach failed") {
		t.Fatalf("expected rendered error status, got %#v", last.Lines)
	}
}

func TestLiveRuntimeIncludesShellReducer(t *testing.T) {
	host := NewFakeTerminalHost(4)
	runtime := NewLiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
	)

	if err := runtime.Post(ShellSetHeaderVisibleMsg{Visible: false}); err != nil {
		t.Fatalf("post shell action: %v", err)
	}
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post panel action: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if runtime.State().Shell.HeaderVisible {
		t.Fatalf("expected hidden header, got %#v", runtime.State().Shell)
	}
	if runtime.State().Shell.PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("expected split line presentation, got %#v", runtime.State().Shell)
	}
}

func TestInteractiveRuntimeIncludesShellReducer(t *testing.T) {
	host := NewFakeTerminalHost(4)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	if err := runtime.Post(ShellOpenTerminalPickerMsg{}); err != nil {
		t.Fatalf("post terminal picker action: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !runtime.State().Shell.Overlay.Open || runtime.State().Shell.Overlay.Kind != state.OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", runtime.State().Shell.Overlay)
	}
}

func compactInputRequests(inputs []services.TerminalInputRequest) []string {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]string, 0, len(inputs))
	var currentTerminal string
	var currentChannel uint16
	var current strings.Builder
	flush := func() {
		if currentTerminal == "" {
			return
		}
		out = append(out, fmt.Sprintf("%s#%d:%s", currentTerminal, currentChannel, current.String()))
		current.Reset()
	}
	for _, input := range inputs {
		if input.TerminalID != currentTerminal || input.Channel != currentChannel {
			flush()
			currentTerminal = input.TerminalID
			currentChannel = input.Channel
		}
		current.Write(input.Bytes)
	}
	flush()
	return out
}

func lastFrame(t *testing.T, frames []render.Frame) render.Frame {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("expected rendered frames")
	}
	return frames[len(frames)-1]
}

func frameContains(frame render.Frame, value string) bool {
	for _, line := range frame.Lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}

func ansiFrameContains(frame render.Frame, value string) bool {
	for _, line := range frame.ANSILines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}

func activePanelVMForAppTest(shell render.ShellVM) (render.PanelVM, bool) {
	for _, panel := range shell.Layout.Panels {
		if panel.Active {
			return panel, true
		}
	}
	return render.PanelVM{}, false
}

func activeContentRectForAppTest(layout render.LayoutPlan) (render.Rect, bool) {
	for _, panel := range layout.Panels {
		if panel.Panel.Active {
			return panel.ContentRect, true
		}
	}
	return render.Rect{}, false
}

func firstPanelLayerForAppTest(result render.RenderResult) (render.Layer, bool) {
	for _, layer := range result.Layers {
		if layer.Kind == render.LayerPanel {
			return layer, true
		}
	}
	return render.Layer{}, false
}

func panelLayerByPaneIDForAppTest(result render.RenderResult, plan render.LayoutPlan, paneID string) (render.Layer, bool) {
	for _, panel := range plan.Panels {
		if panel.Panel.ID != paneID {
			continue
		}
		for _, layer := range result.Layers {
			if layer.Kind == render.LayerPanel && layer.Rect == panel.Rect {
				return layer, true
			}
		}
	}
	return render.Layer{}, false
}

func panelLayerContainsPlainForAppTest(layer render.Layer, value string) bool {
	for _, line := range layer.Lines {
		if strings.Contains(line.PlainString(), value) {
			return true
		}
	}
	return false
}

func drainUntilFrameContains(ctx context.Context, runtime *AppRuntime, host *FakeTerminalHost, value string) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := runtime.Drain(deadlineCtx); err != nil {
			return err
		}
		for _, frame := range host.Frames() {
			if frameContains(frame, value) {
				return nil
			}
		}
		select {
		case <-deadlineCtx.Done():
			return deadlineCtx.Err()
		case <-ticker.C:
		}
	}
}
