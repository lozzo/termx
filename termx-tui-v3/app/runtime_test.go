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

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
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

	action := frameHitRegion(t, lastRuntimeFrame(t, closeHost), render.HitRegionPaneAction, "pane-2")
	if err := closeHost.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send action click: %v", err)
	}
	if err := closeRuntime.Drain(context.Background()); err != nil {
		t.Fatalf("close drain: %v", err)
	}
	if closeRuntime.State().Shell.HasPane(state.PaneCommandTarget{PaneID: "pane-2"}) {
		t.Fatalf("action click should close pane-2, got %#v", closeRuntime.State().Shell)
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

	toastClose := frameHitRegion(t, lastRuntimeFrame(t, host), render.HitRegionToastClose, "")
	if err := host.SendInput(mouseEventAt(toastClose.Rect)); err != nil {
		t.Fatalf("send toast close: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("toast drain: %v", err)
	}
	if len(runtime.State().Shell.Toasts) != 0 {
		t.Fatalf("toast close hit should clear current toast, got %#v", runtime.State().Shell.Toasts)
	}
	if !runtime.State().Shell.Overlay.Open {
		t.Fatalf("toast hit should take priority over overlay, got %#v", runtime.State().Shell.Overlay)
	}

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

func mouseEventAt(rect render.Rect) input.InputEvent {
	return input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseLeft, Row: rect.Y + 1, Col: rect.X + 1}
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
