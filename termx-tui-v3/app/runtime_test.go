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

	action := frameHitRegionByAction(t, lastRuntimeFrame(t, closeHost), render.HitRegionPaneAction, "pane.close", "pane-2")
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
	if len(newTerminal.Creates) != 1 {
		t.Fatalf("picker new should call terminal create, got %#v", newTerminal.Creates)
	}
	toasts := newRuntime.State().Shell.Toasts
	if len(toasts) == 0 || toasts[len(toasts)-1].Title != "picker.new" || toasts[len(toasts)-1].Body != "term-created" {
		t.Fatalf("picker new should show feedback toast, got %#v", toasts)
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
