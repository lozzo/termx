package app

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type testMsg struct {
	Name string
}

func (testMsg) isMsg() {}

func TestRuntimeContractsDoNotUseBubbleTea(t *testing.T) {
	var msg Msg = NoopMsg{}
	var effect Effect = NoopEffect{}
	if msg == nil {
		t.Fatal("expected msg contract")
	}
	if effect == nil {
		t.Fatal("expected effect contract")
	}
}

func TestAppPackageDoesNotImportBubbleTea(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob app files: %v", err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imported := range parsed.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if path == "github.com/charmbracelet/bubbletea" || strings.Contains(path, "/bubbles") {
				t.Fatalf("%s imports Bubble Tea contract package %s", file, path)
			}
		}
	}
}

func TestComposeReducersStopsAtHandledEffect(t *testing.T) {
	var seen []string
	reducer := ComposeReducers(
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			seen = append(seen, "first")
			return root.Advance(), []Effect{handledEffect{}}
		},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			seen = append(seen, "second")
			return root.Advance(), []Effect{NoopEffect{}}
		},
	)

	root, effects := reducer(state.Root{}, NoopMsg{})
	if root.Generation != 1 {
		t.Fatalf("expected only first reducer to run, got generation %d", root.Generation)
	}
	if !reflect.DeepEqual(seen, []string{"first"}) {
		t.Fatalf("unexpected reducer sequence %v", seen)
	}
	if len(effects) != 0 {
		t.Fatalf("handled marker must not leak as effect %#v", effects)
	}
}

func TestAppRuntimeProcessesMessagesInOrderAndRenders(t *testing.T) {
	host := NewFakeTerminalHost(4)
	var seen []string
	runtime := NewAppRuntime(
		state.Root{},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			seen = append(seen, msg.(testMsg).Name)
			return root.Advance(), nil
		},
		func(root state.Root) render.Frame {
			return render.Frame{Lines: []string{string(rune('0' + root.Generation))}}
		},
		host,
		NewSyncEffectRunner(),
	)

	if err := runtime.Post(testMsg{Name: "first"}); err != nil {
		t.Fatalf("post first: %v", err)
	}
	if err := runtime.Post(testMsg{Name: "second"}); err != nil {
		t.Fatalf("post second: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !reflect.DeepEqual(seen, []string{"first", "second"}) {
		t.Fatalf("unexpected message order %v", seen)
	}
	if runtime.State().Generation != 2 {
		t.Fatalf("expected generation 2, got %d", runtime.State().Generation)
	}
	if got := frameLines(host.Frames()); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("unexpected rendered frames %v", got)
	}
}

func TestAppRuntimeRoutesEffectResultsThroughMessagePath(t *testing.T) {
	host := NewFakeTerminalHost(4)
	var seen []string
	runtime := NewAppRuntime(
		state.Root{},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			seen = append(seen, msg.(testMsg).Name)
			if msg.(testMsg).Name == "start" {
				return root.Advance(), []Effect{FuncEffect{
					Run: func(context.Context) Msg {
						return testMsg{Name: "done"}
					},
				}}
			}
			return root.Advance(), nil
		},
		nil,
		host,
		NewSyncEffectRunner(),
	)

	if err := runtime.Post(testMsg{Name: "start"}); err != nil {
		t.Fatalf("post start: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !reflect.DeepEqual(seen, []string{"start", "done"}) {
		t.Fatalf("effect result did not return through message path: %v", seen)
	}
}

func TestAppRuntimeSupportsBatchAndCancel(t *testing.T) {
	runner := NewSyncEffectRunner()
	var seen []string
	runtime := NewAppRuntime(
		state.Root{},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			seen = append(seen, msg.(testMsg).Name)
			if msg.(testMsg).Name == "start" {
				return root, []Effect{
					CancelEffect{Token: "drop"},
					BatchEffect{Effects: []Effect{
						FuncEffect{Token: "keep", Run: func(context.Context) Msg { return testMsg{Name: "kept"} }},
						FuncEffect{Token: "drop", Run: func(context.Context) Msg { return testMsg{Name: "dropped"} }},
					}},
				}
			}
			return root, nil
		},
		nil,
		nil,
		runner,
	)

	if err := runtime.Post(testMsg{Name: "start"}); err != nil {
		t.Fatalf("post start: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !reflect.DeepEqual(seen, []string{"start", "kept"}) {
		t.Fatalf("unexpected batch/cancel sequence %v", seen)
	}
}

func TestAppRuntimeIngestsTerminalHostInput(t *testing.T) {
	host := NewFakeTerminalHost(2)
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	var seen []input.EventKind
	runtime := NewAppRuntime(
		state.Root{},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			inputMsg, ok := msg.(InputMsg)
			if !ok {
				t.Fatalf("expected InputMsg, got %T", msg)
			}
			seen = append(seen, inputMsg.Event.Kind)
			return root.Advance(), nil
		},
		nil,
		host,
		nil,
	)

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !reflect.DeepEqual(seen, []input.EventKind{input.EventKindKey}) {
		t.Fatalf("unexpected host input events %v", seen)
	}
}

func TestAppRuntimeIngestsHostThemeWithoutTerminalInputLeak(t *testing.T) {
	host := NewFakeTerminalHost(2)
	if err := host.SendInput(input.InputEvent{
		Kind:  input.EventKindHostTheme,
		Theme: input.HostThemeEvent{DefaultFG: "#aabbcc", PaletteIndex: 5, PaletteColor: "#445566"},
	}); err != nil {
		t.Fatalf("send host theme: %v", err)
	}
	var leaked bool
	runtime := NewAppRuntime(
		state.Root{},
		ComposeReducers(NewShellReducer(), func(root state.Root, msg Msg) (state.Root, []Effect) {
			if _, ok := msg.(InputMsg); ok {
				leaked = true
			}
			return root, nil
		}),
		func(state.Root) render.Frame { return render.Frame{} },
		host,
		NewSyncEffectRunner(),
	)
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	got := runtime.State().HostTheme
	if got.DefaultFG != "#aabbcc" {
		t.Fatalf("expected host default fg update, got %#v", got)
	}
	if color, ok := got.PaletteColor(5); !ok || color != "#445566" {
		t.Fatalf("expected palette update, got %#v ok=%v", color, ok)
	}
	if leaked {
		t.Fatal("host theme event must not leak as terminal InputMsg")
	}
}

func TestAppRuntimeInitializesViewportFromHostSizeAndRenders(t *testing.T) {
	host := NewFakeTerminalHost(4)
	host.SetSize(132, 43)
	var seen []Msg
	runtime := NewAppRuntime(
		state.Root{},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			seen = append(seen, msg)
			if root.Viewport.Cols != 132 || root.Viewport.Rows != 43 {
				t.Fatalf("reducer must see updated viewport before message handling, got %#v", root.Viewport)
			}
			return root, nil
		},
		func(root state.Root) render.Frame {
			return render.Frame{Lines: []string{viewportLabel(root)}}
		},
		host,
		nil,
	)

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if got := runtime.State().Viewport; !got.Valid || got.Cols != 132 || got.Rows != 43 {
		t.Fatalf("expected viewport initialized from host size, got %#v", got)
	}
	if len(seen) != 1 {
		t.Fatalf("expected one initial HostResizeMsg through reducer chain, got %d", len(seen))
	}
	if _, ok := seen[0].(HostResizeMsg); !ok {
		t.Fatalf("expected HostResizeMsg, got %T", seen[0])
	}
	if got := frameLines(host.Frames()); !reflect.DeepEqual(got, []string{"132x43"}) {
		t.Fatalf("expected initial viewport render, got %v", got)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if got := frameLines(host.Frames()); !reflect.DeepEqual(got, []string{"132x43"}) {
		t.Fatalf("initial size must be ingested once, got %v", got)
	}
}

func TestAppRuntimeIngestsHostResizeEventsAndDeduplicates(t *testing.T) {
	host := NewFakeTerminalHost(4)
	var seen []Msg
	runtime := NewAppRuntime(
		state.Root{},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			seen = append(seen, msg)
			return root, nil
		},
		func(root state.Root) render.Frame {
			return render.Frame{Lines: []string{viewportLabel(root)}}
		},
		host,
		nil,
	)

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("initial drain: %v", err)
	}
	if len(host.Frames()) != 0 {
		t.Fatalf("invalid initial host size must not render, got %#v", host.Frames())
	}
	if err := host.SendResize(90, 30); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := host.SendResize(90, 30); err != nil {
		t.Fatalf("send duplicate resize: %v", err)
	}
	if err := host.SendResize(100, 32); err != nil {
		t.Fatalf("send second resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if got := runtime.State().Viewport; !got.Valid || got.Cols != 100 || got.Rows != 32 {
		t.Fatalf("expected latest viewport, got %#v", got)
	}
	if len(seen) != 2 {
		t.Fatalf("expected duplicate resize to be filtered before reducer, got %d messages", len(seen))
	}
	if got := frameLines(host.Frames()); !reflect.DeepEqual(got, []string{"90x30", "100x32"}) {
		t.Fatalf("expected resize frames without duplicate, got %v", got)
	}
}

func TestFakeTerminalHostReportsFullInputQueue(t *testing.T) {
	host := NewFakeTerminalHost(1)
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey}); err != nil {
		t.Fatalf("send first input: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse}); !errors.Is(err, ErrInputQueueFull) {
		t.Fatalf("expected ErrInputQueueFull, got %v", err)
	}
}

func TestAppRuntimeDispatchesMouseHitRegionsToPaneCommands(t *testing.T) {
	focusHost := NewFakeTerminalHost(8)
	focusRoot := state.Root{
		Shell: state.DefaultShell().
			SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical).
			FocusPane(state.PaneCommandTarget{PaneID: "pane-main"}),
	}
	focusRuntime := newShellHitRuntime(focusRoot, focusHost)
	if err := focusRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post focus initial render: %v", err)
	}
	if err := focusRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("focus initial drain: %v", err)
	}

	content := frameHitRegion(t, lastRuntimeFrame(t, focusHost), render.HitRegionPaneContent, "pane-2")
	if err := focusHost.SendInput(mouseEventAt(content.Rect)); err != nil {
		t.Fatalf("send content click: %v", err)
	}
	if err := focusRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("focus drain: %v", err)
	}
	if focusRuntime.State().Shell.EnsureDefaults().ActivePaneID != "pane-2" {
		t.Fatalf("content click should focus pane-2, got %#v", focusRuntime.State().Shell)
	}

	closeHost := NewFakeTerminalHost(8)
	closeRoot := state.Root{
		Shell: state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical),
	}
	closeRuntime := newShellHitRuntime(closeRoot, closeHost)
	if err := closeRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post close initial render: %v", err)
	}
	if err := closeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("close initial drain: %v", err)
	}

	frame := lastRuntimeFrame(t, closeHost)
	action := frameHitRegionByAction(t, frame, render.HitRegionPaneAction, "pane.close", "pane-2")
	closeMouse := mouseEventAtRenderedTokenInRect(t, frame, action.Rect, render.DefaultPaneChromeGlyphs().Close)
	if !pointInRenderRect(closeMouse, action.Rect) {
		t.Fatalf("visible close token must be inside pane.close hit region, mouse=%#v region=%#v line=%q", closeMouse, action, frame.Lines[action.Rect.Y])
	}
	if err := closeHost.SendInput(closeMouse); err != nil {
		t.Fatalf("send visible close click: %v", err)
	}
	if err := closeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("close drain: %v", err)
	}
	if closeRuntime.State().Shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("action click should close pane-2, got %#v", closeRuntime.State().Shell)
	}
}

func TestAppRuntimePaneCloseHitRegionMatchesWideGlyph(t *testing.T) {
	render.SetPaneChromeGlyphs(render.PaneChromeGlyphs{Close: "❌"})
	defer render.ResetPaneChromeGlyphs()

	host := NewFakeTerminalHost(8)
	root := state.Root{
		Shell: state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical),
	}
	runtime := newShellHitRuntime(root, host)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("initial drain: %v", err)
	}

	frame := lastRuntimeFrame(t, host)
	action := frameHitRegionByAction(t, frame, render.HitRegionPaneAction, render.ActionPaneClose.String(), "pane-2")
	closeMouse := mouseEventAtRenderedTokenInRect(t, frame, action.Rect, "❌")
	if !pointInRenderRect(closeMouse, action.Rect) {
		t.Fatalf("wide close glyph must be inside pane.close hit region, mouse=%#v region=%#v line=%q", closeMouse, action, frame.Lines[action.Rect.Y])
	}
	if err := host.SendInput(closeMouse); err != nil {
		t.Fatalf("send wide close click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if runtime.State().Shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("wide glyph close click should close pane-2, got %#v", runtime.State().Shell)
	}
}

func TestAppRuntimeLastPaneCloseClickShowsFeedback(t *testing.T) {
	host := NewFakeTerminalHost(8)
	runtime := newShellHitRuntime(state.Root{Shell: state.DefaultShell()}, host)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("initial drain: %v", err)
	}

	frame := lastRuntimeFrame(t, host)
	action := frameHitRegionByAction(t, frame, render.HitRegionPaneAction, render.ActionPaneClose.String(), state.DefaultPaneID)
	closeMouse := mouseEventAtRenderedTokenInRect(t, frame, action.Rect, render.DefaultPaneChromeGlyphs().Close)
	if err := host.SendInput(closeMouse); err != nil {
		t.Fatalf("send last pane close click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if !shell.HasPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}) {
		t.Fatalf("last pane close must not remove final pane, got %#v", shell)
	}
	if len(shell.Toasts) == 0 || shell.Toasts[len(shell.Toasts)-1].Body != "cannot close last pane" {
		t.Fatalf("last pane close click should show feedback, got %#v", shell.Toasts)
	}
}

func TestAppRuntimeDispatchesHeaderTabActionHitRegions(t *testing.T) {
	closeHost := NewFakeTerminalHost(8)
	closeShell, _ := state.DefaultShell().ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	closeRoot := state.Root{
		Shell: closeShell,
	}
	closeRuntime := newShellHitRuntime(closeRoot, closeHost)
	if err := closeRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post close initial render: %v", err)
	}
	if err := closeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("close initial drain: %v", err)
	}
	closeAction := frameActionHitRegion(t, lastRuntimeFrame(t, closeHost), render.ActionTabClose.String(), "tab-2")
	if err := closeHost.SendInput(mouseEventAt(closeAction.Rect)); err != nil {
		t.Fatalf("send tab close click: %v", err)
	}
	if err := closeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("close drain: %v", err)
	}
	if tabs := closeRuntime.State().Shell.EnsureDefaults().Workspace.Tabs; len(tabs) != 1 || tabs[0].Title != "main" {
		t.Fatalf("tab close click should remove active tab and keep main, got %#v", tabs)
	}

	targetHost := NewFakeTerminalHost(8)
	targetShell, _ := state.DefaultShell().ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	targetRoot := state.Root{Shell: targetShell}
	targetRuntime := newShellHitRuntime(targetRoot, targetHost)
	if err := targetRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post target close initial render: %v", err)
	}
	if err := targetRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("target close initial drain: %v", err)
	}
	mainCloseAction := frameActionHitRegion(t, lastRuntimeFrame(t, targetHost), render.ActionTabClose.String(), state.DefaultTabID)
	if err := targetHost.SendInput(mouseEventAt(mainCloseAction.Rect)); err != nil {
		t.Fatalf("send inactive tab close click: %v", err)
	}
	if err := targetRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("target close drain: %v", err)
	}
	if tabs := targetRuntime.State().Shell.EnsureDefaults().Workspace.Tabs; len(tabs) != 1 || tabs[0].Title != "logs" || targetRuntime.State().Shell.EnsureDefaults().Workspace.ActiveTabID != tabs[0].ID {
		t.Fatalf("inactive tab close click should close the clicked tab and keep active logs tab, got %#v shell=%#v", tabs, targetRuntime.State().Shell.EnsureDefaults())
	}

	createHost := NewFakeTerminalHost(8)
	createRuntime := newShellHitRuntime(state.Root{Shell: state.DefaultShell()}, createHost)
	if err := createRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post create initial render: %v", err)
	}
	if err := createRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("create initial drain: %v", err)
	}
	createAction := frameActionHitRegion(t, lastRuntimeFrame(t, createHost), render.ActionTabCreate.String(), "")
	if err := createHost.SendInput(mouseEventAt(createAction.Rect)); err != nil {
		t.Fatalf("send tab create click: %v", err)
	}
	if err := createRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("create drain: %v", err)
	}
	if tabs := createRuntime.State().Shell.EnsureDefaults().Workspace.Tabs; len(tabs) != 2 || createRuntime.State().Shell.EnsureDefaults().Workspace.ActiveTabID == state.DefaultTabID {
		t.Fatalf("tab create click should add and activate a tab, got %#v", createRuntime.State().Shell)
	}
}

func TestInteractiveRuntimeTabRenameFooterAction(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(140, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	shell, result := state.DefaultShell().ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("create tab: %#v", result)
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   shell.SetInteractionMode(state.InteractionModeTab),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}
	action := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionTabRename.String(), "")
	if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send tab rename footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tab rename footer click: %v", err)
	}
	shell = runtime.State().Shell.EnsureDefaults()
	if !shell.Overlay.Open || shell.Overlay.Prompt.Purpose != "tab.rename" || shell.Overlay.Prompt.Value != "logs" {
		t.Fatalf("tab rename footer action should open active tab rename prompt, got %#v", shell.Overlay)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("tab rename footer action must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeTabSwitchFooterActions(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(110, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	shell, result := state.DefaultShell().ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("create tab: %#v", result)
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   shell.SetInteractionMode(state.InteractionModeTab),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}

	prevAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionTabPrevious.String(), "")
	if err := host.SendInput(mouseEventAt(prevAction.Rect)); err != nil {
		t.Fatalf("send tab previous footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tab previous footer click: %v", err)
	}
	if got := runtime.State().Shell.EnsureDefaults().Workspace.ActiveTabID; got != state.DefaultTabID {
		t.Fatalf("tab previous footer action should activate default tab, got %q", got)
	}

	nextAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionTabNext.String(), "")
	if err := host.SendInput(mouseEventAt(nextAction.Rect)); err != nil {
		t.Fatalf("send tab next footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tab next footer click: %v", err)
	}
	if got := runtime.State().Shell.EnsureDefaults().Workspace.ActiveTabID; got == state.DefaultTabID {
		t.Fatalf("tab next footer action should return to logs tab, got %q", got)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("tab switch footer actions must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimePaneModeFooterActions(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(120, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   state.DefaultShell().SetInteractionMode(state.InteractionModePane),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}

	splitAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionPaneFooterSplit.String(), "")
	if err := host.SendInput(mouseEventAt(splitAction.Rect)); err != nil {
		t.Fatalf("send pane split footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane split footer click: %v", err)
	}
	tab := runtime.State().Shell.EnsureDefaults().Workspace.Tabs[0]
	if len(tab.Panes) != 2 || tab.RootSplit.Direction != state.SplitDirectionVertical || runtime.State().Shell.EnsureDefaults().ActivePaneID == state.DefaultPaneID {
		t.Fatalf("pane split footer action should create and activate vertical split, shell=%#v", runtime.State().Shell.EnsureDefaults())
	}

	focusAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionPaneFooterFocus.String(), "")
	if err := host.SendInput(mouseEventAt(focusAction.Rect)); err != nil {
		t.Fatalf("send pane focus footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane focus footer click: %v", err)
	}
	if got := runtime.State().Shell.EnsureDefaults().ActivePaneID; got != state.DefaultPaneID {
		t.Fatalf("pane focus footer action should focus next pane, got %q", got)
	}

	zoomAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionPaneFooterZoom.String(), "")
	if err := host.SendInput(mouseEventAt(zoomAction.Rect)); err != nil {
		t.Fatalf("send pane zoom footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane zoom footer click: %v", err)
	}
	if got := runtime.State().Shell.EnsureDefaults().ZoomedPaneID; got != state.DefaultPaneID {
		t.Fatalf("pane zoom footer action should toggle zoom on active pane, got %q", got)
	}

	closeAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionPaneFooterClose.String(), "")
	if err := host.SendInput(mouseEventAt(closeAction.Rect)); err != nil {
		t.Fatalf("send pane close footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane close footer click: %v", err)
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if shell.HasPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}) || len(shell.Workspace.Tabs[0].Panes) != 1 {
		t.Fatalf("pane close footer action should close active pane through workbench command, shell=%#v", shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("pane mode footer actions must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimePaneModeFooterCloseLastPaneShowsFeedback(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(120, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   state.DefaultShell().SetInteractionMode(state.InteractionModePane),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}
	closeAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionPaneFooterClose.String(), "")
	if err := host.SendInput(mouseEventAt(closeAction.Rect)); err != nil {
		t.Fatalf("send last pane footer close click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain last pane footer close click: %v", err)
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if !shell.HasPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}) {
		t.Fatalf("last pane footer close must not remove final pane, got %#v", shell)
	}
	if len(shell.Toasts) == 0 || shell.Toasts[len(shell.Toasts)-1].Body != "cannot close last pane" {
		t.Fatalf("last pane footer close should show feedback, got %#v", shell.Toasts)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("last pane footer close must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeResizeModeFooterActions(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(120, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		SetInteractionMode(state.InteractionModeResize)
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   shell,
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}

	rightAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionResizeRight.String(), "")
	if err := host.SendInput(mouseEventAt(rightAction.Rect)); err != nil {
		t.Fatalf("send resize right footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain resize right footer click: %v", err)
	}
	split := runtime.State().Shell.EnsureDefaults().Workspace.Tabs[0].RootSplit
	if split.BiasCells != 2 {
		t.Fatalf("resize right footer action should update split bias by keyboard step, got %#v", split)
	}

	balanceAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionResizeBalance.String(), "")
	if err := host.SendInput(mouseEventAt(balanceAction.Rect)); err != nil {
		t.Fatalf("send resize balance footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain resize balance footer click: %v", err)
	}
	split = runtime.State().Shell.EnsureDefaults().Workspace.Tabs[0].RootSplit
	if split.BiasCells != 0 {
		t.Fatalf("resize balance footer action should clear split bias, got %#v", split)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("resize mode footer actions must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeResizeModeFooterActionSinglePaneStaysStable(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(120, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   state.DefaultShell().SetInteractionMode(state.InteractionModeResize),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}
	leftAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionResizeLeft.String(), "")
	if err := host.SendInput(mouseEventAt(leftAction.Rect)); err != nil {
		t.Fatalf("send single pane resize footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain single pane resize footer click: %v", err)
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if shell.ActivePaneID != state.DefaultPaneID || len(shell.Workspace.Tabs[0].Panes) != 1 || shell.Workspace.Tabs[0].RootSplit.PaneID != state.DefaultPaneID {
		t.Fatalf("single pane resize footer action should keep pane tree stable, got %#v", shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("single pane resize footer action must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeFloatingFooterActions(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(120, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   state.DefaultShell().SetInteractionMode(state.InteractionModeFloating),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}

	newAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFloatingNew.String(), "")
	if err := host.SendInput(mouseEventAt(newAction.Rect)); err != nil {
		t.Fatalf("send floating new footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating new footer click: %v", err)
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if len(shell.Floatings) != 1 || shell.ActiveFloatingID == "" || !shell.Floatings[0].Active {
		t.Fatalf("floating new footer action should create active floating, shell=%#v", shell)
	}

	closeAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFloatingClose.String(), "")
	if err := host.SendInput(mouseEventAt(closeAction.Rect)); err != nil {
		t.Fatalf("send active floating close footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain active floating close footer click: %v", err)
	}
	shell = runtime.State().Shell.EnsureDefaults()
	if len(shell.Floatings) != 0 || shell.ActiveFloatingID != "" {
		t.Fatalf("floating close footer action should close active floating, shell=%#v", shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("floating footer actions must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeFloatingFooterCloseWithoutActiveShowsFeedback(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(120, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   state.DefaultShell().SetInteractionMode(state.InteractionModeFloating),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}
	closeAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFloatingClose.String(), "")
	if err := host.SendInput(mouseEventAt(closeAction.Rect)); err != nil {
		t.Fatalf("send inactive floating close footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain inactive floating close footer click: %v", err)
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if len(shell.Toasts) == 0 || shell.Toasts[len(shell.Toasts)-1].Body != "floating not found" {
		t.Fatalf("floating close without active target should show feedback, got %#v", shell.Toasts)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("inactive floating close footer action must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeSingleTabSwitchFooterActionsStayStable(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(110, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   state.DefaultShell().SetInteractionMode(state.InteractionModeTab),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}
	for _, actionID := range []string{render.ActionTabNext.String(), render.ActionTabPrevious.String()} {
		action := frameActionHitRegion(t, lastRuntimeFrame(t, host), actionID, "")
		if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
			t.Fatalf("send single tab switch footer click %s: %v", actionID, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain single tab switch footer click %s: %v", actionID, err)
		}
		shell := runtime.State().Shell.EnsureDefaults()
		if shell.Workspace.ActiveTabID != state.DefaultTabID || len(shell.Workspace.Tabs) != 1 {
			t.Fatalf("single tab switch footer action %s should keep state stable, got %#v", actionID, shell)
		}
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("single tab switch footer actions must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestAppRuntimeDispatchesFooterActionHitRegions(t *testing.T) {
	paneHost := NewFakeTerminalHost(8)
	paneRuntime := newShellHitRuntime(state.Root{Shell: state.DefaultShell()}, paneHost)
	if err := paneRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post pane footer render: %v", err)
	}
	if err := paneRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane footer render: %v", err)
	}
	paneAction := frameActionHitRegion(t, lastRuntimeFrame(t, paneHost), render.ActionFooterPaneMode.String(), "")
	if err := paneHost.SendInput(mouseEventAt(paneAction.Rect)); err != nil {
		t.Fatalf("send footer pane click: %v", err)
	}
	if err := paneRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain footer pane click: %v", err)
	}
	if got := paneRuntime.State().Shell.EnsureDefaults().InteractionMode; got != state.InteractionModePane {
		t.Fatalf("footer pane click should enter pane mode, got %q", got)
	}

	globalModeHost := NewFakeTerminalHost(8)
	globalModeRuntime := newShellHitRuntime(state.Root{Shell: state.DefaultShell()}, globalModeHost)
	if err := globalModeRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post global footer render: %v", err)
	}
	if err := globalModeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain global footer render: %v", err)
	}
	globalModeAction := frameActionHitRegion(t, lastRuntimeFrame(t, globalModeHost), render.ActionFooterGlobalMode.String(), "")
	if err := globalModeHost.SendInput(mouseEventAt(globalModeAction.Rect)); err != nil {
		t.Fatalf("send footer global click: %v", err)
	}
	if err := globalModeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain footer global click: %v", err)
	}
	if got := globalModeRuntime.State().Shell.EnsureDefaults().InteractionMode; got != state.InteractionModeGlobal {
		t.Fatalf("footer global click should enter global mode, got %q", got)
	}

	globalHost := NewFakeTerminalHost(8)
	globalRuntime := newShellHitRuntime(state.Root{Shell: state.DefaultShell().SetInteractionMode(state.InteractionModeGlobal).AddToast(state.ToastSpec{ID: "toast-1", Title: "notice"})}, globalHost)
	if err := globalRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post global footer render: %v", err)
	}
	if err := globalRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain global footer render: %v", err)
	}
	headerAction := frameActionHitRegion(t, lastRuntimeFrame(t, globalHost), render.ActionFooterToggleHeader.String(), "")
	if err := globalHost.SendInput(mouseEventAt(headerAction.Rect)); err != nil {
		t.Fatalf("send footer header click: %v", err)
	}
	if err := globalRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain footer header click: %v", err)
	}
	if globalRuntime.State().Shell.EnsureDefaults().HeaderVisible {
		t.Fatalf("footer header click should hide header, got %#v", globalRuntime.State().Shell.EnsureDefaults())
	}
	clearAction := frameActionHitRegion(t, lastRuntimeFrame(t, globalHost), render.ActionFooterClearToasts.String(), "")
	if err := globalHost.SendInput(mouseEventAt(clearAction.Rect)); err != nil {
		t.Fatalf("send footer clear click: %v", err)
	}
	if err := globalRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain footer clear click: %v", err)
	}
	if len(globalRuntime.State().Shell.EnsureDefaults().Toasts) != 0 {
		t.Fatalf("footer clear click should clear toasts, got %#v", globalRuntime.State().Shell.EnsureDefaults().Toasts)
	}
}

func TestInteractiveRuntimeFooterActionDoesNotLeakTerminalInput(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(100, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 100, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	action := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFooterPaneMode.String(), "")
	if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send footer pane click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain footer pane click: %v", err)
	}
	if got := runtime.State().Shell.EnsureDefaults().InteractionMode; got != state.InteractionModePane {
		t.Fatalf("footer pane click should enter pane mode, got %q", got)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("footer action click must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeWorkspaceDeleteFooterAction(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(110, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	shell, result := state.DefaultShell().ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: "remote"})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("create workspace: %#v", result)
	}
	remoteWorkspaceID := result.ID
	shell, result = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceSwitch, TargetID: remoteWorkspaceID})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("switch workspace: %#v", result)
	}
	shell = shell.SetInteractionMode(state.InteractionModeWorkspace)
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   shell,
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}
	activeBefore := runtime.State().Shell.EnsureDefaults().Workspace.ID
	if activeBefore != remoteWorkspaceID {
		t.Fatalf("workspace delete harness must start from remote workspace, active=%q remote=%q shell=%#v", activeBefore, remoteWorkspaceID, runtime.State().Shell)
	}
	action := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFooterDeleteWorkspace.String(), "")
	if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send workspace delete footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workspace delete footer click: %v", err)
	}
	shell = runtime.State().Shell.EnsureDefaults()
	if shell.Workspace.ID == activeBefore || len(shell.Workspaces) != 1 || workspaceIDExistsForTest(shell.Workspaces, activeBefore) {
		t.Fatalf("workspace delete footer action should delete active workspace, got %#v", shell)
	}
	if !toastExistsForTest(shell.Toasts, string(state.WorkbenchCommandWorkspaceDelete), activeBefore) {
		t.Fatalf("workspace delete footer action should show success feedback for deleted workspace %q, got %#v", activeBefore, shell.Toasts)
	}
	action = frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFooterDeleteWorkspace.String(), "")
	if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send last workspace delete footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain last workspace delete footer click: %v", err)
	}
	shell = runtime.State().Shell.EnsureDefaults()
	if len(shell.Workspaces) != 1 || !toastExistsForTest(shell.Toasts, string(state.WorkbenchCommandWorkspaceDelete), "cannot delete last workspace") {
		t.Fatalf("last workspace delete should be rejected with feedback, got %#v", shell)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("workspace footer delete must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeWorkspaceNewRenameFooterActions(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(110, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   state.DefaultShell().SetInteractionMode(state.InteractionModeWorkspace),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}

	newAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFooterNewWorkspace.String(), "")
	if err := host.SendInput(mouseEventAt(newAction.Rect)); err != nil {
		t.Fatalf("send workspace new footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workspace new footer click: %v", err)
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if len(shell.Workspaces) != 2 || shell.Workspace.ID == state.DefaultWorkspaceID || shell.Workspace.Name != "workspace 2" {
		t.Fatalf("workspace new footer action should create and activate next workspace, got %#v", shell)
	}
	if !toastExistsForTest(shell.Toasts, string(state.WorkbenchCommandWorkspaceCreate), shell.Workspace.ID) {
		t.Fatalf("workspace new footer action should show create feedback, got %#v", shell.Toasts)
	}

	renameAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFooterRenameWorkspace.String(), "")
	if err := host.SendInput(mouseEventAt(renameAction.Rect)); err != nil {
		t.Fatalf("send workspace rename footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workspace rename footer click: %v", err)
	}
	shell = runtime.State().Shell.EnsureDefaults()
	if !shell.Overlay.Open || shell.Overlay.Prompt.Purpose != "workspace.rename" || shell.Overlay.Prompt.Value != "workspace 2" {
		t.Fatalf("workspace rename footer action should open active workspace rename prompt, got %#v", shell.Overlay)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("workspace new/rename footer actions must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeWorkspaceSwitchFooterActions(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(110, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	shell, result := state.DefaultShell().ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: "remote"})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("create workspace: %#v", result)
	}
	remoteWorkspaceID := result.ID
	shell, result = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceSwitch, TargetID: state.DefaultWorkspaceID})
	if result.Status != state.WorkbenchCommandOK {
		t.Fatalf("switch workspace: %#v", result)
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   shell.SetInteractionMode(state.InteractionModeWorkspace),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}

	nextAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFooterNextWorkspace.String(), "")
	if err := host.SendInput(mouseEventAt(nextAction.Rect)); err != nil {
		t.Fatalf("send workspace next footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workspace next footer click: %v", err)
	}
	if got := runtime.State().Shell.EnsureDefaults().Workspace.ID; got != remoteWorkspaceID {
		t.Fatalf("workspace next footer action should activate remote workspace, got %q", got)
	}

	prevAction := frameActionHitRegion(t, lastRuntimeFrame(t, host), render.ActionFooterPreviousWorkspace.String(), "")
	if err := host.SendInput(mouseEventAt(prevAction.Rect)); err != nil {
		t.Fatalf("send workspace previous footer click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workspace previous footer click: %v", err)
	}
	if got := runtime.State().Shell.EnsureDefaults().Workspace.ID; got != state.DefaultWorkspaceID {
		t.Fatalf("workspace previous footer action should return to default workspace, got %q", got)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("workspace switch footer actions must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeSingleWorkspaceSwitchFooterActionsStayStable(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(110, 24)
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 5, Cols: 100, Rows: 24},
	}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell:   state.DefaultShell().SetInteractionMode(state.InteractionModeWorkspace),
			Session: state.TerminalSessionStore{}.Attach("term-1", 5, 100, 24),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain render: %v", err)
	}
	for _, actionID := range []string{render.ActionFooterNextWorkspace.String(), render.ActionFooterPreviousWorkspace.String()} {
		action := frameActionHitRegion(t, lastRuntimeFrame(t, host), actionID, "")
		if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
			t.Fatalf("send single workspace switch footer click %s: %v", actionID, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain single workspace switch footer click %s: %v", actionID, err)
		}
		shell := runtime.State().Shell.EnsureDefaults()
		if shell.Workspace.ID != state.DefaultWorkspaceID || len(shell.Workspaces) != 1 {
			t.Fatalf("single workspace switch footer action %s should keep state stable, got %#v", actionID, shell)
		}
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("single workspace switch footer actions must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func workspaceIDExistsForTest(workspaces []state.WorkspaceState, id string) bool {
	for _, workspace := range workspaces {
		if workspace.ID == id {
			return true
		}
	}
	return false
}

func toastExistsForTest(toasts []state.ToastState, title string, body string) bool {
	for _, toast := range toasts {
		if toast.Title == title && toast.Body == body {
			return true
		}
	}
	return false
}

func TestAppRuntimeTiledPaneClickDeactivatesFloatingFocus(t *testing.T) {
	host := NewFakeTerminalHost(8)
	root := state.Root{
		Shell: state.DefaultShell().
			SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical).
			FocusPane(state.PaneCommandTarget{PaneID: "pane-main"}),
	}
	var result state.FloatingCommandResult
	root.Shell, result = root.Shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "floating", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 30, Y: 4, W: 24, H: 8},
		BoundsW:  90,
		BoundsH:  28,
	})
	if result.Status != state.FloatingCommandOK || root.Shell.ActiveFloatingID == "" {
		t.Fatalf("expected active floating setup, result=%#v shell=%#v", result, root.Shell)
	}

	runtime := newShellHitRuntime(root, host)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain initial render: %v", err)
	}
	vmWithFloating := render.NewRenderVMBuilder().Build(runtime.State())
	if len(vmWithFloating.Shell.Layout.Panels) < 2 || vmWithFloating.Shell.Layout.Panels[1].Active || len(vmWithFloating.Shell.Layout.Floating) != 1 || !vmWithFloating.Shell.Layout.Floating[0].Active {
		t.Fatalf("active floating should dim tiled panes before click, panels=%#v floating=%#v", vmWithFloating.Shell.Layout.Panels, vmWithFloating.Shell.Layout.Floating)
	}

	content := frameHitRegion(t, lastRuntimeFrame(t, host), render.HitRegionPaneContent, "pane-2")
	if err := host.SendInput(mouseEventAt(content.Rect)); err != nil {
		t.Fatalf("send tiled pane click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tiled pane click: %v", err)
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if shell.ActiveFloatingID != "" || len(shell.Floatings) != 1 || shell.Floatings[0].Active {
		t.Fatalf("tiled pane click should deactivate floating without closing it, shell=%#v", shell)
	}
	if shell.ActivePaneID != "pane-2" {
		t.Fatalf("tiled pane click should focus pane-2, shell=%#v", shell)
	}
	vmAfterClick := render.NewRenderVMBuilder().Build(runtime.State())
	if len(vmAfterClick.Shell.Layout.Panels) < 2 || !vmAfterClick.Shell.Layout.Panels[1].Active || len(vmAfterClick.Shell.Layout.Floating) != 1 || vmAfterClick.Shell.Layout.Floating[0].Active {
		t.Fatalf("tiled pane should regain active style and floating should render inactive, panels=%#v floating=%#v", vmAfterClick.Shell.Layout.Panels, vmAfterClick.Shell.Layout.Floating)
	}
}

func TestAppRuntimeDispatchesMouseSplitActions(t *testing.T) {
	downHost := NewFakeTerminalHost(8)
	downRuntime := newShellHitRuntime(state.Root{Shell: state.DefaultShell()}, downHost)
	if err := downRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post split-down initial render: %v", err)
	}
	if err := downRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split-down initial render: %v", err)
	}
	downAction := frameHitRegionByAction(t, lastRuntimeFrame(t, downHost), render.HitRegionPaneAction, "pane.split-down", state.DefaultPaneID)
	if err := downHost.SendInput(mouseEventAt(downAction.Rect)); err != nil {
		t.Fatalf("send split-down click: %v", err)
	}
	if err := downRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split-down click: %v", err)
	}
	downSplit := downRuntime.State().Shell.Workspace.Tabs[0].RootSplit
	if len(downRuntime.State().Shell.Workspace.Tabs[0].Panes) != 2 || downSplit.Direction != state.SplitDirectionHorizontal {
		t.Fatalf("split-down action should create horizontal split, shell=%#v", downRuntime.State().Shell)
	}

	rightHost := NewFakeTerminalHost(8)
	rightRuntime := newShellHitRuntime(state.Root{Shell: state.DefaultShell()}, rightHost)
	if err := rightRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post split-right initial render: %v", err)
	}
	if err := rightRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split-right initial render: %v", err)
	}
	rightAction := frameHitRegionByAction(t, lastRuntimeFrame(t, rightHost), render.HitRegionPaneAction, "pane.split-right", state.DefaultPaneID)
	if err := rightHost.SendInput(mouseEventAt(rightAction.Rect)); err != nil {
		t.Fatalf("send split-right click: %v", err)
	}
	if err := rightRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split-right click: %v", err)
	}
	rightSplit := rightRuntime.State().Shell.Workspace.Tabs[0].RootSplit
	if len(rightRuntime.State().Shell.Workspace.Tabs[0].Panes) != 2 || rightSplit.Direction != state.SplitDirectionVertical {
		t.Fatalf("split-right action should create vertical split, shell=%#v", rightRuntime.State().Shell)
	}
	if rightRuntime.State().Shell.ActivePaneID == state.DefaultPaneID || downRuntime.State().Shell.ActivePaneID == state.DefaultPaneID {
		t.Fatalf("mouse split actions should activate new panes, down=%#v right=%#v", downRuntime.State().Shell.ActivePaneID, rightRuntime.State().Shell.ActivePaneID)
	}
}

func TestAppRuntimeSplitActionOnHorizontalDividerPaneWinsOverResize(t *testing.T) {
	host := NewFakeTerminalHost(8)
	root := state.Root{
		Shell: state.DefaultShell().
			SetPanelPresentation(state.PanelPresentationSplitLine).
			SplitActivePane(state.PaneState{ID: "pane-bottom", Title: "bottom", Kind: state.PaneTerminalLive}, state.SplitDirectionHorizontal),
	}
	runtime := newShellHitRuntime(root, host)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain initial render: %v", err)
	}

	action := frameHitRegionByAction(t, lastRuntimeFrame(t, host), render.HitRegionPaneAction, "pane.split-down", "pane-bottom")
	if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send bottom split action: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain bottom split action: %v", err)
	}

	shell := runtime.State().Shell.EnsureDefaults()
	if len(shell.Workspace.Tabs[0].Panes) != 3 || !shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("bottom pane split icon should create a new pane instead of starting divider resize, shell=%#v", shell)
	}
	if shell.ActivePaneID != "pane-2" || runtime.mouseDrag.Active {
		t.Fatalf("split action should activate new pane and not leave resize drag state, active=%q drag=%#v", shell.ActivePaneID, runtime.mouseDrag)
	}
	for _, toast := range shell.Toasts {
		if toast.Body == "missing new pane id" || toast.Body == "target pane not found" {
			t.Fatalf("split action should not produce invalid toast, got %#v", shell.Toasts)
		}
	}
}

func TestAppRuntimeDragsPaneResizeHitRegions(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 7, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(32)
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
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post split-line: %v", err)
	}
	if err := runtime.Post(ShellSplitActivePaneMsg{Pane: state.PaneState{ID: "pane-2", Title: "right", Kind: state.PaneTerminalLive}, Direction: state.SplitDirectionVertical}); err != nil {
		t.Fatalf("post split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain split: %v", err)
	}
	beforeInputCount := len(terminal.Inputs)
	beforeResizeCount := len(terminal.Resizes)
	beforeToastCount := len(runtime.State().Shell.Toasts)
	beforeRects := paneLayoutRects(runtime.State())
	resizeRegion := framePaneResizeRegion(t, lastRuntimeFrame(t, host), state.DefaultPaneID, state.PaneResizeRight)
	start := mouseEventAt(resizeRegion.Rect)
	start.Mouse = input.MouseLeft
	if err := host.SendInput(start); err != nil {
		t.Fatalf("send drag start: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain drag start: %v", err)
	}
	if runtime.mouseDrag.PaneID != state.DefaultPaneID || runtime.mouseDrag.Direction != state.PaneResizeRight {
		t.Fatalf("expected active pane resize drag state, got %#v", runtime.mouseDrag)
	}

	drag := start
	drag.Mouse = input.MouseLeftDrag
	drag.Col += 5
	if err := host.SendInput(drag); err != nil {
		t.Fatalf("send drag move: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain drag move: %v", err)
	}
	split := runtime.State().Shell.Workspace.Tabs[0].RootSplit
	if split.BiasCells != 5 {
		t.Fatalf("expected horizontal divider drag to resize split bias, got %#v", split)
	}
	grownRects := paneLayoutRects(runtime.State())
	if grownRects[state.DefaultPaneID].X != beforeRects[state.DefaultPaneID].X || grownRects["pane-2"].X+grownRects["pane-2"].W != beforeRects["pane-2"].X+beforeRects["pane-2"].W {
		t.Fatalf("dragging right divider should keep opposite pane edges anchored, before=%#v after=%#v", beforeRects, grownRects)
	}
	reverseDrag := start
	reverseDrag.Mouse = input.MouseLeftDrag
	reverseDrag.Col -= 2
	if err := host.SendInput(reverseDrag); err != nil {
		t.Fatalf("send reverse drag move: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain reverse drag move: %v", err)
	}
	split = runtime.State().Shell.Workspace.Tabs[0].RootSplit
	if split.BiasCells != -2 || runtime.mouseDrag.Direction != state.PaneResizeRight {
		t.Fatalf("dragging the same right edge back must keep the left edge anchored, split=%#v drag=%#v", split, runtime.mouseDrag)
	}
	shrunkRects := paneLayoutRects(runtime.State())
	if shrunkRects[state.DefaultPaneID].X != beforeRects[state.DefaultPaneID].X || shrunkRects["pane-2"].X+shrunkRects["pane-2"].W != beforeRects["pane-2"].X+beforeRects["pane-2"].W {
		t.Fatalf("dragging right divider backward should still keep opposite pane edges anchored, before=%#v after=%#v", beforeRects, shrunkRects)
	}
	if len(terminal.Resizes) <= beforeResizeCount {
		t.Fatalf("pane drag resize should schedule active terminal content resize, got %#v", terminal.Resizes)
	}
	if len(terminal.Inputs) != beforeInputCount {
		t.Fatalf("pane resize drag must not leak to terminal input, got %#v", terminal.Inputs)
	}
	if len(runtime.State().Shell.Toasts) != beforeToastCount {
		t.Fatalf("pane resize drag success should not add toast, before=%d after=%#v", beforeToastCount, runtime.State().Shell.Toasts)
	}

	release := reverseDrag
	release.Mouse = input.MouseLeftUp
	if err := host.SendInput(release); err != nil {
		t.Fatalf("send drag release: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain drag release: %v", err)
	}
	if runtime.mouseDrag.Active {
		t.Fatalf("release should clear drag state, got %#v", runtime.mouseDrag)
	}

	afterRelease := release
	afterRelease.Mouse = input.MouseLeftDrag
	afterRelease.Col += 3
	if err := host.SendInput(afterRelease); err != nil {
		t.Fatalf("send drag after release: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain drag after release: %v", err)
	}
	if got := runtime.State().Shell.Workspace.Tabs[0].RootSplit.BiasCells; got != -2 {
		t.Fatalf("drag after release must not resize, bias=%d", got)
	}
}

func TestAppRuntimeDragsHorizontalPaneDividerResize(t *testing.T) {
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := newShellHitRuntime(
		state.Root{Shell: state.DefaultShell().
			SetPanelPresentation(state.PanelPresentationSplitLine).
			SplitActivePane(state.PaneState{ID: "pane-bottom", Title: "bottom", Kind: state.PaneTerminalLive}, state.SplitDirectionHorizontal)},
		host,
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain initial render: %v", err)
	}
	resizeRegion := framePaneResizeRegion(t, lastRuntimeFrame(t, host), state.DefaultPaneID, state.PaneResizeDown)
	if resizeRegion.Direction != string(state.PaneResizeDown) {
		t.Fatalf("expected horizontal split divider direction down, got %#v", resizeRegion)
	}
	start := mouseEventAt(resizeRegion.Rect)
	start.Mouse = input.MouseLeft
	drag := start
	drag.Mouse = input.MouseLeftDrag
	drag.Row += 3
	release := drag
	release.Mouse = input.MouseLeftUp
	for _, event := range []input.InputEvent{start, drag, release} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send event %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain event %#v: %v", event, err)
		}
	}
	if got := runtime.State().Shell.Workspace.Tabs[0].RootSplit.BiasCells; got != 3 {
		t.Fatalf("expected vertical drag to update horizontal split bias, got %#v", runtime.State().Shell.Workspace.Tabs[0].RootSplit)
	}
}

func TestAppRuntimeDragsNestedPaneResizeOnlyChangesExactDivider(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 9, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(32)
	host.SetSize(90, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 90, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post split-line: %v", err)
	}
	if err := runtime.Post(ShellSplitActivePaneMsg{Pane: state.PaneState{ID: "pane-middle", Title: "middle", Kind: state.PaneTerminalLive}, Direction: state.SplitDirectionVertical}); err != nil {
		t.Fatalf("post middle split: %v", err)
	}
	if err := runtime.Post(ShellSplitActivePaneMsg{Pane: state.PaneState{ID: "pane-right", Title: "right", Kind: state.PaneTerminalLive}, Direction: state.SplitDirectionVertical}); err != nil {
		t.Fatalf("post right split: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain setup: %v", err)
	}
	beforeInputCount := len(terminal.Inputs)
	beforeToastCount := len(runtime.State().Shell.Toasts)
	beforeRects := paneLayoutRects(runtime.State())

	resizeRegion := framePaneResizeRegion(t, lastRuntimeFrame(t, host), "pane-middle", state.PaneResizeRight)
	if resizeRegion.SplitPath != "root/1" {
		t.Fatalf("expected nested divider split path, got %#v", resizeRegion)
	}
	start := mouseEventAt(resizeRegion.Rect)
	start.Mouse = input.MouseLeft
	if err := host.SendInput(start); err != nil {
		t.Fatalf("send nested drag start: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain nested drag start: %v", err)
	}
	if runtime.mouseDrag.SplitPath != "root/1" || runtime.mouseDrag.PaneID != "pane-middle" {
		t.Fatalf("drag state should keep exact nested split path, got %#v", runtime.mouseDrag)
	}

	drag := start
	drag.Mouse = input.MouseLeftDrag
	drag.Col -= 5
	if err := host.SendInput(drag); err != nil {
		t.Fatalf("send nested drag move: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain nested drag move: %v", err)
	}
	root := runtime.State().Shell.Workspace.Tabs[0].RootSplit
	if root.BiasCells != 0 || len(root.Children) < 2 || root.Children[1].BiasCells != -5 {
		t.Fatalf("nested divider drag should not mutate outer split, got %#v", root)
	}
	afterRects := paneLayoutRects(runtime.State())
	if afterRects[state.DefaultPaneID] != beforeRects[state.DefaultPaneID] {
		t.Fatalf("outer left pane must stay anchored, before=%#v after=%#v", beforeRects[state.DefaultPaneID], afterRects[state.DefaultPaneID])
	}
	if afterRects["pane-middle"].X != beforeRects["pane-middle"].X || afterRects["pane-middle"].W != beforeRects["pane-middle"].W-5 {
		t.Fatalf("left side of nested divider should shrink by drag delta, before=%#v after=%#v", beforeRects["pane-middle"], afterRects["pane-middle"])
	}
	if afterRects["pane-right"].X != beforeRects["pane-right"].X-5 || afterRects["pane-right"].X+afterRects["pane-right"].W != beforeRects["pane-right"].X+beforeRects["pane-right"].W {
		t.Fatalf("right side of nested divider should grow while outer edge stays anchored, before=%#v after=%#v", beforeRects["pane-right"], afterRects["pane-right"])
	}
	if len(terminal.Inputs) != beforeInputCount {
		t.Fatalf("nested pane resize drag must not leak to terminal input, got %#v", terminal.Inputs)
	}
	if len(runtime.State().Shell.Toasts) != beforeToastCount {
		t.Fatalf("nested pane resize drag success should not add toast, before=%d after=%#v", beforeToastCount, runtime.State().Shell.Toasts)
	}
}

func TestAppRuntimeDragsFourColumnPaneResizeOnlyAdjacentColumns(t *testing.T) {
	leftRuntime, leftHost, leftTerminal := newFourColumnPaneRuntime(t)
	beforeLeftRects := paneLayoutRects(leftRuntime.State())
	beforeLeftInputCount := len(leftTerminal.Inputs)
	beforeLeftToastCount := len(leftRuntime.State().Shell.Toasts)

	leftDivider := framePaneResizeRegion(t, lastRuntimeFrame(t, leftHost), state.DefaultPaneID, state.PaneResizeRight)
	if leftDivider.ResizeBeforePaneID != state.DefaultPaneID || leftDivider.ResizeAfterPaneID != "pane-2" {
		t.Fatalf("expected pane-2 left divider to target pane-main/pane-2, got %#v", leftDivider)
	}
	leftStart := mouseEventAt(leftDivider.Rect)
	leftStart.Mouse = input.MouseLeft
	if err := leftHost.SendInput(leftStart); err != nil {
		t.Fatalf("send left divider start: %v", err)
	}
	if err := leftRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain left divider start: %v", err)
	}
	leftDrag := leftStart
	leftDrag.Mouse = input.MouseLeftDrag
	leftDrag.Col -= 4
	if err := leftHost.SendInput(leftDrag); err != nil {
		t.Fatalf("send left divider drag: %v", err)
	}
	if err := leftRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain left divider drag: %v", err)
	}
	afterLeftRects := paneLayoutRects(leftRuntime.State())
	if afterLeftRects[state.DefaultPaneID].W != beforeLeftRects[state.DefaultPaneID].W-4 || afterLeftRects["pane-2"].X != beforeLeftRects["pane-2"].X-4 || afterLeftRects["pane-2"].W != beforeLeftRects["pane-2"].W+4 {
		t.Fatalf("dragging pane-2 left edge left should only trade width with pane-main, before=%#v after=%#v", beforeLeftRects, afterLeftRects)
	}
	if afterLeftRects["pane-3"] != beforeLeftRects["pane-3"] || afterLeftRects["pane-4"] != beforeLeftRects["pane-4"] {
		t.Fatalf("dragging pane-2 left edge must not grow later panes, before=%#v after=%#v", beforeLeftRects, afterLeftRects)
	}
	if len(leftTerminal.Inputs) != beforeLeftInputCount || len(leftRuntime.State().Shell.Toasts) != beforeLeftToastCount {
		t.Fatalf("four-column left drag should not leak input or add toast, inputs=%#v toasts=%#v", leftTerminal.Inputs, leftRuntime.State().Shell.Toasts)
	}

	rightRuntime, rightHost, rightTerminal := newFourColumnPaneRuntime(t)
	beforeRightRects := paneLayoutRects(rightRuntime.State())
	beforeRightInputCount := len(rightTerminal.Inputs)
	beforeRightToastCount := len(rightRuntime.State().Shell.Toasts)

	rightDivider := framePaneResizeRegion(t, lastRuntimeFrame(t, rightHost), "pane-2", state.PaneResizeRight)
	if rightDivider.ResizeBeforePaneID != "pane-2" || rightDivider.ResizeAfterPaneID != "pane-3" {
		t.Fatalf("expected pane-2 right divider to target pane-2/pane-3, got %#v", rightDivider)
	}
	rightStart := mouseEventAt(rightDivider.Rect)
	rightStart.Mouse = input.MouseLeft
	if err := rightHost.SendInput(rightStart); err != nil {
		t.Fatalf("send right divider start: %v", err)
	}
	if err := rightRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain right divider start: %v", err)
	}
	rightDrag := rightStart
	rightDrag.Mouse = input.MouseLeftDrag
	rightDrag.Col += 5
	if err := rightHost.SendInput(rightDrag); err != nil {
		t.Fatalf("send right divider drag: %v", err)
	}
	if err := rightRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain right divider drag: %v", err)
	}
	afterRightRects := paneLayoutRects(rightRuntime.State())
	if afterRightRects["pane-2"].W != beforeRightRects["pane-2"].W+5 || afterRightRects["pane-3"].X != beforeRightRects["pane-3"].X+5 || afterRightRects["pane-3"].W != beforeRightRects["pane-3"].W-5 {
		t.Fatalf("dragging pane-2 right edge right should only trade width with pane-3, before=%#v after=%#v", beforeRightRects, afterRightRects)
	}
	if afterRightRects["pane-4"] != beforeRightRects["pane-4"] {
		t.Fatalf("dragging pane-2 right edge must not shrink pane-4, before=%#v after=%#v", beforeRightRects["pane-4"], afterRightRects["pane-4"])
	}
	if len(rightTerminal.Inputs) != beforeRightInputCount || len(rightRuntime.State().Shell.Toasts) != beforeRightToastCount {
		t.Fatalf("four-column right drag should not leak input or add toast, inputs=%#v toasts=%#v", rightTerminal.Inputs, rightRuntime.State().Shell.Toasts)
	}
}

func TestAppRuntimeDragsStackedRightColumnResizeAsSharedWidthGroup(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 12, Cols: 80, Rows: 20},
	}
	host := NewFakeTerminalHost(64)
	root := state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: 80, Rows: 20},
		Shell:    stackedRightColumnShellForTest(),
	}
	runtime := newShellHitRuntimeWithTerminal(root, host, terminal)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post stacked initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain stacked initial render: %v", err)
	}
	beforeRects := paneLayoutRects(runtime.State())
	beforeInputCount := len(terminal.Inputs)
	beforeToastCount := len(runtime.State().Shell.Toasts)

	divider := framePaneResizeRegion(t, lastRuntimeFrame(t, host), "left", state.PaneResizeRight)
	if divider.ResizeBeforePaneID != "left" || divider.ResizeAfterPaneID != "top" {
		t.Fatalf("expected root divider to target left/top boundary panes, got %#v", divider)
	}
	start := mouseEventAt(divider.Rect)
	start.Mouse = input.MouseLeft
	if err := host.SendInput(start); err != nil {
		t.Fatalf("send stacked drag start: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain stacked drag start: %v", err)
	}
	drag := start
	drag.Mouse = input.MouseLeftDrag
	drag.Col -= 6
	if err := host.SendInput(drag); err != nil {
		t.Fatalf("send stacked drag move: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain stacked drag move: %v", err)
	}

	afterRects := paneLayoutRects(runtime.State())
	if afterRects["left"].W != beforeRects["left"].W-6 {
		t.Fatalf("left column should shrink by drag delta, before=%#v after=%#v", beforeRects["left"], afterRects["left"])
	}
	for _, paneID := range []string{"top", "middle-left", "bottom"} {
		if afterRects[paneID].X != beforeRects[paneID].X-6 || afterRects[paneID].W != beforeRects[paneID].W+6 {
			t.Fatalf("%s should move with the shared right-column boundary, before=%#v after=%#v", paneID, beforeRects[paneID], afterRects[paneID])
		}
	}
	if afterRects["middle-right"].X != beforeRects["middle-right"].X || afterRects["middle-right"].W != beforeRects["middle-right"].W {
		t.Fatalf("nested right child should keep its outer anchor and width, before=%#v after=%#v", beforeRects["middle-right"], afterRects["middle-right"])
	}
	if len(terminal.Inputs) != beforeInputCount || len(runtime.State().Shell.Toasts) != beforeToastCount {
		t.Fatalf("stacked column drag should not leak input or add toast, inputs=%#v toasts=%#v", terminal.Inputs, runtime.State().Shell.Toasts)
	}
}

func TestAppRuntimeDragsFloatingMoveAndResizeHitRegions(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 3, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	shell, result := state.DefaultShell().ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "floating", Kind: state.PaneEmpty},
		Title:    "floating",
		Rect:     state.FloatingRect{X: 10, Y: 4, W: 30, H: 8},
		BoundsW:  80,
		BoundsH:  24,
		Source:   state.PaneCommandSourceTest,
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating for test: %#v", result)
	}
	root := state.Root{
		Shell: shell,
	}
	runtime := newShellHitRuntimeWithTerminal(root, host, terminal)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post floating initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating initial render: %v", err)
	}
	beforeInputCount := len(terminal.Inputs)
	beforeToastCount := len(runtime.State().Shell.Toasts)

	moveRegion := frameActionHitRegion(t, lastRuntimeFrame(t, host), "floating.move-drag", "floating-1")
	moveStart := mouseEventAt(moveRegion.Rect)
	if err := host.SendInput(moveStart); err != nil {
		t.Fatalf("send floating move start: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating move start: %v", err)
	}
	if runtime.mouseDrag.Kind != mouseDragFloatingMove || runtime.mouseDrag.FloatingID != "floating-1" {
		t.Fatalf("expected floating move drag state, got %#v", runtime.mouseDrag)
	}
	moveDrag := moveStart
	moveDrag.Mouse = input.MouseLeftDrag
	moveDrag.Col += 4
	moveDrag.Row += 3
	if err := host.SendInput(moveDrag); err != nil {
		t.Fatalf("send floating move drag: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating move drag: %v", err)
	}
	moved := runtime.State().Shell.Floatings[0].Rect
	if moved.X != 14 || moved.Y != 7 {
		t.Fatalf("floating title drag should move rect, got %#v", moved)
	}
	if len(runtime.State().Shell.Toasts) != beforeToastCount {
		t.Fatalf("floating move drag success should not add toast, before=%d after=%#v", beforeToastCount, runtime.State().Shell.Toasts)
	}
	moveRelease := moveDrag
	moveRelease.Mouse = input.MouseLeftUp
	if err := host.SendInput(moveRelease); err != nil {
		t.Fatalf("send floating move release: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating move release: %v", err)
	}
	if runtime.mouseDrag.Active {
		t.Fatalf("floating move release should clear drag state, got %#v", runtime.mouseDrag)
	}

	resizeRegion := frameActionHitRegion(t, lastRuntimeFrame(t, host), "floating.resize-drag", "floating-1")
	resizeStart := mouseEventAt(resizeRegion.Rect)
	if err := host.SendInput(resizeStart); err != nil {
		t.Fatalf("send floating resize start: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating resize start: %v", err)
	}
	if runtime.mouseDrag.Kind != mouseDragFloatingResize || runtime.mouseDrag.FloatingID != "floating-1" {
		t.Fatalf("expected floating resize drag state, got %#v", runtime.mouseDrag)
	}
	beforeResize := runtime.State().Shell.Floatings[0].Rect
	resizeDrag := resizeStart
	resizeDrag.Mouse = input.MouseLeftDrag
	resizeDrag.Col += 6
	resizeDrag.Row += 2
	if err := host.SendInput(resizeDrag); err != nil {
		t.Fatalf("send floating resize drag: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating resize drag: %v", err)
	}
	resized := runtime.State().Shell.Floatings[0].Rect
	if resized.W != beforeResize.W+6 || resized.H != beforeResize.H+2 {
		t.Fatalf("floating resize drag should resize rect, before=%#v after=%#v", beforeResize, resized)
	}
	if len(runtime.State().Shell.Toasts) != beforeToastCount {
		t.Fatalf("floating resize drag success should not add toast, before=%d after=%#v", beforeToastCount, runtime.State().Shell.Toasts)
	}
	resizeRelease := resizeDrag
	resizeRelease.Mouse = input.MouseLeftUp
	if err := host.SendInput(resizeRelease); err != nil {
		t.Fatalf("send floating resize release: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating resize release: %v", err)
	}
	afterRelease := resizeRelease
	afterRelease.Mouse = input.MouseLeftDrag
	afterRelease.Col += 3
	afterRelease.Row += 3
	if err := host.SendInput(afterRelease); err != nil {
		t.Fatalf("send floating drag after release: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain floating drag after release: %v", err)
	}
	if got := runtime.State().Shell.Floatings[0].Rect; got != resized {
		t.Fatalf("drag after release must not resize floating, before=%#v after=%#v", resized, got)
	}
	if len(terminal.Inputs) != beforeInputCount {
		t.Fatalf("floating drag must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestAppRuntimeMouseHitPriorityAndMissFallback(t *testing.T) {
	host := NewFakeTerminalHost(8)
	root := state.Root{
		Shell: state.DefaultShell().
			OpenTerminalPicker().
			AddToast(state.ToastSpec{ID: "toast-1", Title: "notice"}),
	}
	var inputSeen int
	runtime := NewAppRuntime(
		root,
		ComposeReducers(NewShellReducer(), func(root state.Root, msg Msg) (state.Root, []Effect) {
			if _, ok := msg.(InputMsg); ok {
				inputSeen++
			}
			return root, nil
		}),
		func(root state.Root) render.Frame {
			return render.NewRenderer(render.DefaultTheme()).Render(render.NewRenderVMBuilder().Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("initial drain: %v", err)
	}

	toastRegion := frameHitRegion(t, lastRuntimeFrame(t, host), render.HitRegionToast, "")
	if err := host.SendInput(mouseEventAt(toastRegion.Rect)); err != nil {
		t.Fatalf("send toast hit: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("toast drain: %v", err)
	}
	if len(runtime.State().Shell.Toasts) != 1 {
		t.Fatalf("toast body hit should not close toast, got %#v", runtime.State().Shell.Toasts)
	}
	if !runtime.State().Shell.Overlay.Open {
		t.Fatalf("toast hit should take priority over overlay, got %#v", runtime.State().Shell.Overlay)
	}
	if inputSeen != 0 {
		t.Fatalf("toast hit should be consumed, inputSeen=%d", inputSeen)
	}

	runtime.state.Shell = runtime.State().Shell.ClearToasts()
	runtime.renderFrame()

	overlay := frameHitRegion(t, lastRuntimeFrame(t, host), render.HitRegionOverlay, "")
	if err := host.SendInput(mouseEventAt(overlay.Rect)); err != nil {
		t.Fatalf("send overlay click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("overlay drain: %v", err)
	}
	if runtime.State().Shell.Overlay.Open {
		t.Fatalf("overlay hit should close overlay, got %#v", runtime.State().Shell.Overlay)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseLeft, Row: 999, Col: 999}); err != nil {
		t.Fatalf("send miss click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("miss drain: %v", err)
	}
	if inputSeen == 0 {
		t.Fatal("missed mouse hit should continue through InputMsg fallback")
	}
}

func TestInteractiveRuntimeTerminalMouseTrackingPassthroughOnlyFromContent(t *testing.T) {
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
	frame := lastRuntimeFrame(t, host)
	content := frameHitRegion(t, frame, render.HitRegionPaneContent, state.DefaultPaneID)
	mouse := mouseEventAt(content.Rect)
	mouse.Mouse = input.MouseRight
	mouse.RawSeq = "\x1b[<2;10;4M"
	if err := host.SendInput(mouse); err != nil {
		t.Fatalf("send mouse without tracking: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain mouse without tracking: %v", err)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("mouse without terminal tracking must not passthrough, got %#v", terminal.Inputs)
	}

	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{
		TerminalID: "term-1",
		Cols:       78,
		Rows:       22,
		Lines:      []string{"tracking"},
		Modes:      state.LiveTerminalModes{MouseTracking: true, MouseSGR: true},
	}}); err != nil {
		t.Fatalf("post live surface: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain live surface: %v", err)
	}
	if err := host.SendInput(mouse); err != nil {
		t.Fatalf("send tracked content mouse: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain tracked content mouse: %v", err)
	}
	if len(terminal.Inputs) != 1 || string(terminal.Inputs[0].Bytes) != mouse.RawSeq {
		t.Fatalf("tracked content mouse should passthrough raw SGR, got %#v", terminal.Inputs)
	}

	chrome := frameHitRegion(t, lastRuntimeFrame(t, host), render.HitRegionPaneChrome, state.DefaultPaneID)
	chromeMouse := mouseEventAt(chrome.Rect)
	chromeMouse.Mouse = input.MouseRight
	chromeMouse.RawSeq = "\x1b[<2;1;1M"
	if err := host.SendInput(chromeMouse); err != nil {
		t.Fatalf("send chrome mouse: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain chrome mouse: %v", err)
	}
	if len(terminal.Inputs) != 1 {
		t.Fatalf("chrome mouse must not passthrough to terminal, got %#v", terminal.Inputs)
	}
}

func TestAppRuntimeAutoDismissesToastsOnRuntimeTick(t *testing.T) {
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 20)
	root := state.Root{
		Shell: state.DefaultShell().
			AddToast(state.ToastSpec{ID: "short", Severity: state.ToastInfo, Title: "short notice"}).
			AddToast(state.ToastSpec{ID: "pending", Severity: state.ToastError, Title: "pending notice", Pending: true}),
	}
	var inputSeen int
	runtime := NewAppRuntime(
		root,
		ComposeReducers(NewShellReducer(), func(root state.Root, msg Msg) (state.Root, []Effect) {
			if _, ok := msg.(InputMsg); ok {
				inputSeen++
			}
			return root, nil
		}),
		func(root state.Root) render.Frame {
			return render.NewRenderer(render.DefaultTheme()).Render(render.NewRenderVMBuilder().Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	runtime.now = func() time.Time { return now }
	runtime.toastTickInterval = time.Second

	if err := runtime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post initial render: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("initial drain: %v", err)
	}
	initialFrameCount := len(host.Frames())
	if !frameContains(lastRuntimeFrame(t, host), "pending notice") {
		t.Fatalf("expected pending toast in initial frame, got %#v", lastRuntimeFrame(t, host).Lines)
	}

	now = now.Add(3 * time.Second)
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("first timer drain: %v", err)
	}
	if len(host.Frames()) <= initialFrameCount {
		t.Fatalf("runtime timer should trigger redraw, before=%d after=%d", initialFrameCount, len(host.Frames()))
	}
	if len(runtime.State().Shell.Toasts) != 1 || runtime.State().Shell.Toasts[0].ID != "pending" {
		t.Fatalf("short toast should auto-dismiss while pending remains, got %#v", runtime.State().Shell.Toasts)
	}
	if frameContains(lastRuntimeFrame(t, host), "short notice") || !frameContains(lastRuntimeFrame(t, host), "pending notice") {
		t.Fatalf("expected only pending toast after first timer, got %#v", lastRuntimeFrame(t, host).Lines)
	}

	now = now.Add(5 * time.Second)
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("second timer drain: %v", err)
	}
	if len(runtime.State().Shell.Toasts) != 0 || frameContains(lastRuntimeFrame(t, host), "pending notice") {
		t.Fatalf("pending toast should eventually auto-dismiss, state=%#v frame=%#v", runtime.State().Shell.Toasts, lastRuntimeFrame(t, host).Lines)
	}
	if inputSeen != 0 {
		t.Fatalf("runtime toast timer must not leak through terminal input path, inputSeen=%d", inputSeen)
	}
}

func TestAppRuntimeDispatchesProductContentActions(t *testing.T) {
	pickerHost := NewFakeTerminalHost(8)
	pickerRoot := state.Root{
		Shell: state.DefaultShell().
			SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
			FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
			OpenTerminalPicker(),
	}
	pickerRuntime := newShellHitRuntime(pickerRoot, pickerHost)
	if err := pickerRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post picker render: %v", err)
	}
	if err := pickerRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain picker render: %v", err)
	}
	pickerAction := frameActionHitRegion(t, lastRuntimeFrame(t, pickerHost), "picker.attach", "pane-2")
	if err := pickerHost.SendInput(mouseEventAt(pickerAction.Rect)); err != nil {
		t.Fatalf("send picker attach click: %v", err)
	}
	if err := pickerRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain picker attach: %v", err)
	}
	if pickerRuntime.State().Shell.EnsureDefaults().ActivePaneID != "pane-2" || pickerRuntime.State().Shell.Overlay.Open {
		t.Fatalf("picker attach should focus pane-2 and close overlay, got %#v", pickerRuntime.State().Shell)
	}

	newHost := NewFakeTerminalHost(8)
	newTerminal := &services.FakeTerminalService{CreateResult: services.TerminalCreateResult{TerminalID: "term-created", State: "running"}}
	newRuntime := newShellHitRuntimeWithTerminal(pickerRoot, newHost, newTerminal)
	if err := newRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post picker new render: %v", err)
	}
	if err := newRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain picker new render: %v", err)
	}
	newAction := frameActionHitRegion(t, lastRuntimeFrame(t, newHost), "picker.new", "")
	if err := newHost.SendInput(mouseEventAt(newAction.Rect)); err != nil {
		t.Fatalf("send picker new click: %v", err)
	}
	if err := newRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain picker new: %v", err)
	}
	if len(newTerminal.Creates) != 0 {
		t.Fatalf("picker new should open form before create, got %#v", newTerminal.Creates)
	}
	if overlay := newRuntime.State().Shell.EnsureDefaults().Overlay; overlay.Kind != state.OverlayPrompt || overlay.Prompt.Purpose != "terminal.create" {
		t.Fatalf("picker new should open create terminal form, got %#v", overlay)
	}

	emptyHost := NewFakeTerminalHost(8)
	emptyShell := state.DefaultShell()
	emptyShell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "slot", Kind: state.PaneEmpty, Active: true}
	emptyRuntime := newShellHitRuntime(state.Root{Shell: emptyShell}, emptyHost)
	if err := emptyRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post empty render: %v", err)
	}
	if err := emptyRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain empty render: %v", err)
	}
	emptyAttach := frameActionHitRegion(t, lastRuntimeFrame(t, emptyHost), "empty.attach", state.DefaultPaneID)
	if err := emptyHost.SendInput(mouseEventAt(emptyAttach.Rect)); err != nil {
		t.Fatalf("send empty attach click: %v", err)
	}
	if err := emptyRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain empty attach: %v", err)
	}
	if len(emptyRuntime.State().Shell.Toasts) == 0 || emptyRuntime.State().Shell.Toasts[len(emptyRuntime.State().Shell.Toasts)-1].Title != "empty.attach" {
		t.Fatalf("empty attach should show feedback toast, got %#v", emptyRuntime.State().Shell.Toasts)
	}

	closeHost := NewFakeTerminalHost(8)
	closeShell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "empty", Kind: state.PaneEmpty}, state.SplitDirectionVertical)
	closeRuntime := newShellHitRuntime(state.Root{Shell: closeShell}, closeHost)
	if err := closeRuntime.Post(NoopMsg{}); err != nil {
		t.Fatalf("post close render: %v", err)
	}
	if err := closeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain close render: %v", err)
	}
	closeAction := frameActionHitRegion(t, lastRuntimeFrame(t, closeHost), "empty.close", "pane-2")
	if err := closeHost.SendInput(mouseEventAt(closeAction.Rect)); err != nil {
		t.Fatalf("send empty close click: %v", err)
	}
	if err := closeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("drain empty close: %v", err)
	}
	if closeRuntime.State().Shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("empty close action should close pane-2, got %#v", closeRuntime.State().Shell)
	}
}

func newShellHitRuntime(root state.Root, host *FakeTerminalHost) *AppRuntime {
	host.SetSize(80, 20)
	return NewAppRuntime(
		root,
		NewShellReducer(),
		func(root state.Root) render.Frame {
			return render.NewRenderer(render.DefaultTheme()).Render(render.NewRenderVMBuilder().Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
}

func paneLayoutRects(root state.Root) map[string]render.Rect {
	vm := render.NewRenderVMBuilder().Build(root)
	plan := render.MeasureLayout(vm.Shell, vm.Shell.Layout.Viewport)
	rects := make(map[string]render.Rect, len(plan.Panels))
	for _, panel := range plan.Panels {
		rects[panel.Panel.ID] = panel.Rect
	}
	return rects
}

func newFourColumnPaneRuntime(t *testing.T) (*AppRuntime, *FakeTerminalHost, *services.FakeTerminalService) {
	t.Helper()
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 12, Cols: 100, Rows: 24},
	}
	host := NewFakeTerminalHost(64)
	host.SetSize(100, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 100, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Post(ShellSetPanelPresentationMsg{Presentation: state.PanelPresentationSplitLine}); err != nil {
		t.Fatalf("post split-line: %v", err)
	}
	for _, paneID := range []string{"pane-2", "pane-3", "pane-4"} {
		if err := runtime.Post(ShellSplitActivePaneMsg{Pane: state.PaneState{ID: paneID, Title: paneID, Kind: state.PaneTerminalLive}, Direction: state.SplitDirectionVertical}); err != nil {
			t.Fatalf("post split %s: %v", paneID, err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain four-column setup: %v", err)
	}
	return runtime, host, terminal
}

func stackedRightColumnShellForTest() state.ShellStore {
	shell := state.DefaultShell().SetPanelPresentation(state.PanelPresentationSplitLine)
	tab := &shell.Workspace.Tabs[0]
	tab.Panes = []state.PaneState{
		{ID: "left", Title: "left", Kind: state.PaneTerminalLive},
		{ID: "top", Title: "top", Kind: state.PaneTerminalLive},
		{ID: "middle-left", Title: "middle-left", Kind: state.PaneTerminalLive},
		{ID: "middle-right", Title: "middle-right", Kind: state.PaneTerminalLive},
		{ID: "bottom", Title: "bottom", Kind: state.PaneTerminalLive},
	}
	tab.ActivePaneID = "middle-left"
	tab.RootSplit = state.SplitNode{
		Direction: state.SplitDirectionVertical,
		Children: []state.SplitNode{
			{PaneID: "left"},
			{
				Direction: state.SplitDirectionHorizontal,
				Children: []state.SplitNode{
					{PaneID: "top"},
					{
						Direction: state.SplitDirectionHorizontal,
						Children: []state.SplitNode{
							{
								Direction: state.SplitDirectionVertical,
								Children:  []state.SplitNode{{PaneID: "middle-left"}, {PaneID: "middle-right"}},
							},
							{PaneID: "bottom"},
						},
					},
				},
			},
		},
	}
	shell.ActivePaneID = "middle-left"
	return shell.EnsureDefaults()
}

func newShellHitRuntimeWithTerminal(root state.Root, host *FakeTerminalHost, terminal services.TerminalService) *AppRuntime {
	host.SetSize(80, 20)
	return NewAppRuntime(
		root,
		ComposeReducers(NewShellReducer(), NewTerminalPoolReducer(LiveDeps{Terminal: terminal})),
		func(root state.Root) render.Frame {
			return render.NewRenderer(render.DefaultTheme()).Render(render.NewRenderVMBuilder().Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
}

func mouseEventAt(rect render.Rect) input.InputEvent {
	return input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseLeft, Row: rect.Y + 1, Col: rect.X + 1}
}

func mouseEventAtRenderedTokenInRect(t *testing.T, frame render.Frame, rect render.Rect, token string) input.InputEvent {
	t.Helper()
	row := rect.Y
	if row < 0 || row >= len(frame.Lines) {
		t.Fatalf("row %d out of frame %#v", row, frame.Lines)
	}
	col := renderedTokenCellIndexInRange(frame.Lines[row], token, rect.X, rect.X+rect.W)
	if col < 0 {
		t.Fatalf("missing token %q inside rect=%#v row=%q", token, rect, frame.Lines[row])
	}
	return input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseLeft, Row: row + 1, Col: col + 1}
}

func renderedTokenCellIndexInRange(line string, token string, left int, right int) int {
	width := render.DisplayWidth(token)
	if width <= 0 {
		return -1
	}
	maxCol := minIntForTest(right-width, render.DisplayWidth(line)-width)
	for col := maxIntForTest(0, left); col <= maxCol; col++ {
		if render.SliceCells(line, col, col+width) == token {
			return col
		}
	}
	return -1
}

func minIntForTest(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxIntForTest(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func pointInRenderRect(event input.InputEvent, rect render.Rect) bool {
	col := event.Col - 1
	row := event.Row - 1
	return col >= rect.X && col < rect.X+rect.W && row >= rect.Y && row < rect.Y+rect.H
}

func lastRuntimeFrame(t *testing.T, host *FakeTerminalHost) render.Frame {
	t.Helper()
	frames := host.Frames()
	if len(frames) == 0 {
		t.Fatal("expected at least one rendered frame")
	}
	return frames[len(frames)-1]
}

func frameHitRegion(t *testing.T, frame render.Frame, kind render.HitRegionKind, paneID string) render.HitRegion {
	t.Helper()
	for _, region := range frame.HitRegions {
		if region.Kind == kind && (paneID == "" || region.PaneID == paneID) {
			return region
		}
	}
	t.Fatalf("missing hit region kind=%s pane=%s in %#v", kind, paneID, frame.HitRegions)
	return render.HitRegion{}
}

func frameActionHitRegion(t *testing.T, frame render.Frame, actionID string, paneID string) render.HitRegion {
	t.Helper()
	for _, region := range frame.HitRegions {
		if region.Kind == render.HitRegionContentAction && region.ActionID == actionID && (paneID == "" || region.PaneID == paneID) {
			return region
		}
	}
	t.Fatalf("missing content action=%s pane=%s in %#v", actionID, paneID, frame.HitRegions)
	return render.HitRegion{}
}

func frameHitRegionByAction(t *testing.T, frame render.Frame, kind render.HitRegionKind, actionID string, paneID string) render.HitRegion {
	t.Helper()
	for _, region := range frame.HitRegions {
		if region.Kind == kind && region.ActionID == actionID && (paneID == "" || region.PaneID == paneID) {
			return region
		}
	}
	t.Fatalf("missing hit region kind=%s action=%s pane=%s in %#v", kind, actionID, paneID, frame.HitRegions)
	return render.HitRegion{}
}

func framePaneResizeRegion(t *testing.T, frame render.Frame, paneID string, direction state.PaneResizeDirection) render.HitRegion {
	t.Helper()
	for _, region := range frame.HitRegions {
		if region.Kind == render.HitRegionPaneResize && region.ActionID == "pane.resize" && region.PaneID == paneID && region.Direction == string(direction) {
			return region
		}
	}
	t.Fatalf("missing pane resize region pane=%s direction=%s in %#v", paneID, direction, frame.HitRegions)
	return render.HitRegion{}
}

func viewportLabel(root state.Root) string {
	if !root.Viewport.Valid {
		return "unset"
	}
	return fmt.Sprintf("%dx%d", root.Viewport.Cols, root.Viewport.Rows)
}

func TestAppRuntimeQuitStopsQueue(t *testing.T) {
	var seen []string
	runtime := NewAppRuntime(
		state.Root{},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			seen = append(seen, msg.(testMsg).Name)
			return root.Advance(), nil
		},
		nil,
		nil,
		nil,
	)

	if err := runtime.Post(testMsg{Name: "before"}); err != nil {
		t.Fatalf("post before: %v", err)
	}
	if err := runtime.Post(QuitMsg{}); err != nil {
		t.Fatalf("post quit: %v", err)
	}
	if err := runtime.Post(testMsg{Name: "after"}); err != nil {
		t.Fatalf("post after: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !runtime.Quit() {
		t.Fatal("expected runtime quit")
	}
	if !reflect.DeepEqual(seen, []string{"before"}) {
		t.Fatalf("queue should stop at quit, got %v", seen)
	}
	if err := runtime.Post(testMsg{Name: "late"}); !errors.Is(err, ErrRuntimeStopped) {
		t.Fatalf("expected ErrRuntimeStopped, got %v", err)
	}
}

func TestFakeFrameSinkReturnsDetachedFrames(t *testing.T) {
	sink := &FakeFrameSink{}
	if err := sink.WriteFrame(render.Frame{Lines: []string{"one"}}); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	frames := sink.Frames()
	frames[0].Lines[0] = "mutated"

	got := sink.Frames()
	if got[0].Lines[0] != "one" {
		t.Fatalf("expected detached frames, got %v", got)
	}
}

func frameLines(frames []render.Frame) []string {
	lines := make([]string, len(frames))
	for i, frame := range frames {
		if len(frame.Lines) > 0 {
			lines[i] = frame.Lines[0]
		}
	}
	return lines
}
