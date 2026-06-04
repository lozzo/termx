package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
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
