package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestUIInputReducerOpensTerminalPickerFromCtrlF(t *testing.T) {
	reducer := NewUIInputReducer()
	root, effects := reducer(state.Root{Shell: state.DefaultShell()}, InputMsg{Event: input.InputEvent{
		Kind: input.EventKindKey,
		Key:  input.KeyChar,
		Char: "\x06",
		Ctrl: true,
	}})

	if !root.Shell.Overlay.Open || root.Shell.Overlay.Kind != state.OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", root.Shell.Overlay)
	}
	if len(effects) != 1 {
		t.Fatalf("expected handled effect, got %#v", effects)
	}
	if _, ok := effects[0].(handledEffect); !ok {
		t.Fatalf("expected handled effect, got %#v", effects)
	}
}

func TestInteractiveRuntimeCtrlFDoesNotSendTerminalInput(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(8)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x06", Ctrl: true}); err != nil {
		t.Fatalf("send ctrl-f: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain ctrl-f: %v", err)
	}

	if !runtime.State().Shell.Overlay.Open || runtime.State().Shell.Overlay.Kind != state.OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %#v", runtime.State().Shell.Overlay)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("ctrl-f must not be sent to terminal, got %#v", terminal.Inputs)
	}
	last := lastFrame(t, host.Frames())
	if !frameContains(last, "terminal-picker") && !frameContains(last, "terminal picker pending") {
		t.Fatalf("expected terminal picker placeholder in frame, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeCtrlVEntersCopyWithoutTerminalInput(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			1,
			nil,
		)}},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: core, Rows: 20},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x16", Ctrl: true}); err != nil {
		t.Fatalf("send ctrl-v: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain ctrl-v: %v", err)
	}

	if !runtime.State().CopyMode.Active {
		t.Fatalf("expected copy mode active, got %#v", runtime.State().CopyMode)
	}
	if len(core.LatestRequests) != 1 {
		t.Fatalf("expected authoritative latest request, got %#v", core.LatestRequests)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("ctrl-v must not be sent to terminal, got %#v", terminal.Inputs)
	}
	last := lastFrame(t, host.Frames())
	if !frameContains(last, "copy history empty") {
		t.Fatalf("expected copy empty content in frame, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeShellSemanticActionsReachRenderPath(t *testing.T) {
	host := NewFakeTerminalHost(8)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)

	for _, msg := range []Msg{
		ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine},
		ShellSetHeaderVisibleMsg{Visible: false},
		ShellSetFooterVisibleMsg{Visible: false},
		ShellAddToastMsg{Toast: state.ToastSpec{ID: "toast-1", Severity: state.ToastWarning, Title: "warn"}},
		ShellCloseCurrentToastMsg{},
		ShellAddToastMsg{Toast: state.ToastSpec{ID: "toast-2", Severity: state.ToastInfo, Title: "notice"}},
		ShellClearToastsMsg{},
	} {
		if err := runtime.Post(msg); err != nil {
			t.Fatalf("post %T: %v", msg, err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if runtime.State().Shell.PanelPresentation != state.PanelPresentationSplitLine {
		t.Fatalf("expected split line presentation, got %#v", runtime.State().Shell)
	}
	if runtime.State().Shell.HeaderVisible || runtime.State().Shell.FooterVisible {
		t.Fatalf("expected hidden header/footer, got %#v", runtime.State().Shell)
	}
	if len(runtime.State().Shell.Toasts) != 0 {
		t.Fatalf("expected cleared toasts, got %#v", runtime.State().Shell.Toasts)
	}
	last := lastFrame(t, host.Frames())
	if frameContains(last, " main ") || frameContains(last, " live ") {
		t.Fatalf("hidden header/footer should not render shell bars, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimePaneAndResizeModeKeymapUsesPaneCommandPath(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "v"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x12", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyRight},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain input %#v: %v", event, err)
		}
	}

	if runtime.State().Shell.InteractionMode != state.InteractionModeResize {
		t.Fatalf("expected resize mode, got %#v", runtime.State().Shell.InteractionMode)
	}
	tab := runtime.State().Shell.Workspace.Tabs[0]
	if len(tab.Panes) != 2 || tab.RootSplit.Direction != state.SplitDirectionVertical {
		t.Fatalf("expected keyboard split through pane command path, got %#v", tab)
	}
	if tab.RootSplit.BiasCells == 0 {
		t.Fatalf("expected resize key to update split bias, got %#v", tab.RootSplit)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("pane/resize shortcuts must not leak to terminal input, got %#v", terminal.Inputs)
	}
	last := lastFrame(t, host.Frames())
	if !frameContains(last, "mode:resize") {
		t.Fatalf("footer should show resize mode, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeActivePaneVisualFeedbackFollowsKeyboardAndMouse(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(32)
	host.SetSize(96, 28)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "v"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send keyboard event %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain keyboard event %#v: %v", event, err)
		}
	}
	keyboardFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != "pane-2" {
		t.Fatalf("keyboard split should activate pane-2, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, keyboardFrame, "pane", render.StyleAccent)
	assertPaneVisualState(t, keyboardFrame, "shell", render.StyleMuted)
	if !frameContains(keyboardFrame, "active:pane:pane") {
		t.Fatalf("footer should reflect keyboard split active pane, got %#v", keyboardFrame.Lines)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "n"}); err != nil {
		t.Fatalf("send keyboard focus: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain keyboard focus: %v", err)
	}
	focusFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != state.DefaultPaneID {
		t.Fatalf("keyboard focus-next should activate default pane, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, focusFrame, "shell", render.StyleAccent)
	assertPaneVisualState(t, focusFrame, "pane", render.StyleMuted)
	if !frameContains(focusFrame, "pane.focus-next") || !frameContains(focusFrame, "active:pane:shell") {
		t.Fatalf("keyboard focus should update toast/footer immediately, got %#v", focusFrame.Lines)
	}

	paneContent := frameHitRegion(t, focusFrame, render.HitRegionPaneContent, "pane-2")
	if err := host.SendInput(mouseEventAt(paneContent.Rect)); err != nil {
		t.Fatalf("send mouse focus: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain mouse focus: %v", err)
	}
	mouseFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != "pane-2" {
		t.Fatalf("mouse focus should activate pane-2, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, mouseFrame, "pane", render.StyleAccent)
	assertPaneVisualState(t, mouseFrame, "shell", render.StyleMuted)
	if !frameContains(mouseFrame, "pane.focus") || !frameContains(mouseFrame, "active:pane:pane") {
		t.Fatalf("mouse focus should update toast/footer immediately, got %#v", mouseFrame.Lines)
	}

	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x12", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyRight},
		{Kind: input.EventKindKey, Key: input.KeyEsc},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "p"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "z"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send post-focus event %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain post-focus event %#v: %v", event, err)
		}
	}
	zoomFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.ZoomedPaneID != "pane-2" {
		t.Fatalf("zoom should keep active pane zoomed, got %#v", runtime.State().Shell)
	}
	if !frameContains(zoomFrame, "mode:pane") || !frameContains(zoomFrame, "pane.toggle-zoom") || !frameContains(zoomFrame, "active:pane:pane") {
		t.Fatalf("resize/presentation/zoom should keep visible active feedback, got %#v", zoomFrame.Lines)
	}
	assertPaneVisualState(t, zoomFrame, "pane", render.StyleAccent)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "z"}); err != nil {
		t.Fatalf("send unzoom before close: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain unzoom before close: %v", err)
	}
	unzoomFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.ZoomedPaneID != "" {
		t.Fatalf("unzoom before close should restore split layout, got %#v", runtime.State().Shell)
	}
	assertPaneVisualState(t, unzoomFrame, "pane", render.StyleAccent)

	if err := runtime.Post(ShellClearToastsMsg{}); err != nil {
		t.Fatalf("post clear toasts before close: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain clear toasts before close: %v", err)
	}
	clearFrame := lastFrame(t, host.Frames())
	action := frameHitRegion(t, clearFrame, render.HitRegionPaneAction, "pane-2")
	if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send close click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain close click: %v", err)
	}
	closeFrame := lastFrame(t, host.Frames())
	if runtime.State().Shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("mouse close should remove active pane, got %#v", runtime.State().Shell)
	}
	if runtime.State().Shell.EnsureDefaults().ActivePaneID != state.DefaultPaneID {
		t.Fatalf("close should choose stable next active pane, got %#v", runtime.State().Shell)
	}
	if !frameContains(closeFrame, "pane.close") || !frameContains(closeFrame, "active:pane:shell") {
		t.Fatalf("close should update active pane visuals/footer/toast, got %#v", closeFrame.Lines)
	}
	assertPaneVisualState(t, closeFrame, "shell", render.StyleAccent)
}

func assertPaneVisualState(t *testing.T, frame render.Frame, text string, style render.StyleToken) {
	t.Helper()
	for _, line := range frame.StyledLines {
		var span strings.Builder
		flush := func() bool {
			if strings.Contains(span.String(), text) {
				return true
			}
			span.Reset()
			return false
		}
		for _, cell := range line.Cells {
			if cell.Style == style {
				span.WriteString(cell.Text)
				continue
			}
			if flush() {
				return
			}
		}
		if flush() {
			return
		}
	}
	t.Fatalf("expected styled pane text %q with style %s, got %#v", text, style, frame.StyledLines)
}

func TestInteractiveRuntimeGlobalModeTogglesChromeAndEscExitsMode(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x07", Ctrl: true},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "h"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "f"},
		{Kind: input.EventKindKey, Key: input.KeyEsc},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send input %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain input %#v: %v", event, err)
		}
	}

	if runtime.State().Shell.InteractionMode != state.InteractionModeNormal {
		t.Fatalf("expected normal mode after esc, got %#v", runtime.State().Shell.InteractionMode)
	}
	if runtime.State().Shell.HeaderVisible || runtime.State().Shell.FooterVisible {
		t.Fatalf("expected global mode toggles to hide header/footer, got %#v", runtime.State().Shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("global shortcuts must not leak to terminal input, got %#v", terminal.Inputs)
	}
}
