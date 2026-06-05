package app

import (
	"context"
	"errors"
	"strings"
	"testing"

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
		t.Fatalf("single split-line pane now shares the same outer-frame content rect as card, got %#v", terminal.Resizes)
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
		t.Fatalf("active pane right of divider must reserve shared outer frame and split divider, got %#v", got)
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
