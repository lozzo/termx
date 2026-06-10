package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

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
		t.Fatalf("unexpected resize requests %#v", terminal.Resizes)
	}
	if runtime.State().Session.Cols != 100 || runtime.State().Surface.Cols != 100 {
		t.Fatalf("resize was not reflected in state %#v", runtime.State())
	}
	last := lastFrame(t, host.Frames())
	if len(last.Lines) == 0 || !frameContains(last, "$ echo hi") {
		t.Fatalf("expected live surface frame, got %#v", last.Lines)
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
	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-2", Channel: 2, Cols: 42, Rows: 20}}); err != nil {
		t.Fatalf("post term-2 attach result: %v", err)
	}
	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-3", Channel: 3, Cols: 42, Rows: 20}}); err != nil {
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
	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{TerminalID: "term-float", Channel: 9, Cols: 28, Rows: 6}}); err != nil {
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
	if !frameContains(lastFrame(t, host.Frames()), "term-float") {
		t.Fatalf("picker should render pool terminal row, got %#v", lastFrame(t, host.Frames()).Lines)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "o"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "a"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "t"},
		{Kind: input.EventKindKey, Key: input.KeyDown},
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
		Result:           services.TerminalAttachResult{TerminalID: "term-float", Channel: 5, Cols: 28, Rows: 6},
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
	if !frameContains(frame, "live surface pending") || runtime.State().Surface.Ready {
		t.Fatalf("expected pending surface after terminal switch, frame=%#v state=%#v", frame.Lines, runtime.State().Surface)
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
	runtime := NewLiveRuntime(
		state.Root{Shell: shell},
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

	pendingFrame := lastFrame(t, host.Frames())
	if !frameContains(pendingFrame, "live surface pending") {
		t.Fatalf("expected pending fallback after resize without surface, got %#v", pendingFrame.Lines)
	}
	pendingLayer, ok := firstPanelLayerForAppTest(render.NewRenderer(render.DefaultTheme()).RenderResult(render.NewRenderVMBuilder().Build(runtime.State())))
	if !ok {
		t.Fatalf("expected pending panel layer")
	}
	if panelLayerContainsPlainForAppTest(pendingLayer, "·") {
		t.Fatalf("pending fallback should not be filled by resize-boundary dots, got %#v", pendingLayer.Lines)
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
	if !ok || !panelLayer.ContentOverflow.Right || !panelLayer.ContentOverflow.Bottom {
		t.Fatalf("live resize overflow should be exposed through panel layer, layer=%#v ok=%v", panelLayer, ok)
	}
	rightMarkerRow := panelLayer.Rect.Y + panelLayer.Rect.H/2
	rightMarkerCol := panelLayer.Rect.X + panelLayer.Rect.W - 1
	if got := render.SliceCells(frame.Lines[rightMarkerRow], rightMarkerCol, rightMarkerCol+1); got != ">" {
		t.Fatalf("right overflow marker should be on pane chrome, got %q frame=%#v", got, frame.Lines)
	}
	bottomMarkerRow := panelLayer.Rect.Y + panelLayer.Rect.H - 1
	bottomMarkerCol := panelLayer.Rect.X + panelLayer.Rect.W/2
	if got := render.SliceCells(frame.Lines[bottomMarkerRow], bottomMarkerCol, bottomMarkerCol+1); got != "v" {
		t.Fatalf("bottom overflow marker should be on pane chrome, got %q frame=%#v", got, frame.Lines)
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
	if err := runtime.Post(ShellSetHeaderVisibleMsg{Visible: false}); err != nil {
		t.Fatalf("post header hide: %v", err)
	}
	if err := runtime.Post(ShellSetFooterVisibleMsg{Visible: false}); err != nil {
		t.Fatalf("post footer hide: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain chrome resize: %v", err)
	}

	if len(terminal.Resizes) != 2 {
		t.Fatalf("expected one resize per chrome layout change, got %#v", terminal.Resizes)
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
