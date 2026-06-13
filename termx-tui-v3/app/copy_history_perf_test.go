package app

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
	"github.com/lozzow/termx/termx-tui-v3/terminalhost"
)

var copyHistoryPerfFrame render.Frame

func BenchmarkCopyHistoryRenderScrollFrame(b *testing.B) {
	root := copyHistoryPerfRoot(180, 60, 5000)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.CopyMode.ViewportTop = 2500 + (i % 200)
		copyHistoryPerfFrame = renderer.Render(builder.Build(root))
	}
}

func BenchmarkCopyHistoryRenderANSIFrame(b *testing.B) {
	root := copyHistoryPerfRoot(180, 60, 5000)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.CopyMode.ViewportTop = 2500 + (i % 200)
		copyHistoryPerfFrame = renderer.RenderANSI(builder.Build(root))
	}
}

func BenchmarkCopyHistoryFrameSinkScrollShift(b *testing.B) {
	root := copyHistoryPerfRoot(180, 60, 5000)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	sink := terminalhost.NewFrameSink(io.Discard)
	root.CopyMode.ViewportTop = 2500
	if err := sink.WriteFrame(renderer.Render(builder.Build(root))); err != nil {
		b.Fatalf("write initial frame: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.CopyMode.ViewportTop = 2501 + (i % 200)
		if err := sink.WriteFrame(renderer.Render(builder.Build(root))); err != nil {
			b.Fatalf("write scrolled frame: %v", err)
		}
	}
}

func BenchmarkCopyHistoryFrameSinkScrollShiftANSI(b *testing.B) {
	root := copyHistoryPerfRoot(180, 60, 5000)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	sink := terminalhost.NewFrameSink(io.Discard)
	root.CopyMode.ViewportTop = 2500
	if err := sink.WriteFrame(renderer.RenderANSI(builder.Build(root))); err != nil {
		b.Fatalf("write initial frame: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.CopyMode.ViewportTop = 2501 + (i % 200)
		if err := sink.WriteFrame(renderer.RenderANSI(builder.Build(root))); err != nil {
			b.Fatalf("write scrolled frame: %v", err)
		}
	}
}

func BenchmarkCopyHistoryRuntimeWheelBatch(b *testing.B) {
	host := NewFakeTerminalHost(4096)
	host.SetSize(180, 60)
	root := copyHistoryPerfRoot(180, 60, 5000)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(
		root,
		ComposeReducers(NewCopyModeReducer(CopyModeDeps{Core: &services.FakeCoreClient{}})),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
	benchmarkCopyHistoryWheelRuntime(b, runtime, host)
}

func BenchmarkCopyHistoryRuntimeWheelBatchANSI(b *testing.B) {
	host := NewFakeTerminalHost(4096)
	host.SetSize(180, 60)
	root := copyHistoryPerfRoot(180, 60, 5000)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(
		root,
		ComposeReducers(NewCopyModeReducer(CopyModeDeps{Core: &services.FakeCoreClient{}})),
		func(root state.Root) render.Frame {
			return renderer.RenderANSI(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
	benchmarkCopyHistoryWheelRuntime(b, runtime, host)
}

func benchmarkCopyHistoryWheelRuntime(b *testing.B, runtime *AppRuntime, host *FakeTerminalHost) {
	b.Helper()
	for i := 0; i < 100; i++ {
		if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseWheelUp}); err != nil {
			b.Fatalf("seed input: %v", err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		b.Fatalf("seed drain: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseWheelUp}); err != nil {
			b.Fatalf("send input: %v", err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			b.Fatalf("drain: %v", err)
		}
	}
}

func copyHistoryPerfRoot(cols int, rows int, historyRows int) state.Root {
	history := state.HistoryStore{
		PaneID:     state.DefaultPaneID,
		ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
		TerminalID: "term-1",
		Token:      "tok-perf",
		Cols:       cols - 2,
		Cursor:     state.HistoryCursor{Valid: true, BeforeLineID: 1},
		Generation: 7,
		Boundary:   state.HistoryBoundary{FirstLineID: 1, LastLineID: uint64(historyRows)},
		HasMore:    true,
	}
	history.SourceLines = make([]state.HistoryLogicalLine, historyRows)
	for i := range history.SourceLines {
		history.SourceLines[i] = state.HistoryLogicalLine{
			Text:   fmt.Sprintf("copy-history-perf row %05d segment segment segment segment segment", i),
			LineID: uint64(i + 1),
		}
	}
	history.Rows, history.Lines = state.ReflowHistoryLogicalLines(history.SourceLines, history.Cols)
	copyMode := state.CopyModeStore{
		Active:      true,
		PaneID:      state.DefaultPaneID,
		ViewID:      state.TerminalPaneViewID(state.DefaultPaneID),
		TerminalID:  "term-1",
		ViewportTop: historyRows / 2,
		ViewRows:    rows - 4,
		BoundToken:  "tok-perf",
		BoundCols:   history.Cols,
	}
	return state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: cols, Rows: rows},
		Shell:    state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1"),
		Session: state.TerminalSessionStore{
			TerminalID: "term-1",
			Attached:   true,
			Cols:       cols - 2,
			Rows:       rows - 4,
		},
		Surface:       state.TerminalSurfaceStore{TerminalID: "term-1", Cols: cols - 2, Rows: rows - 4, Lines: []string{"live"}},
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 4, cols-2, rows-4, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		History:       history,
		CopyMode:      copyMode,
	}
}
