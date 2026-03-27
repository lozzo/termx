package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
)

var (
	benchmarkStringSink string
	benchmarkGridSink   [][]drawCell
)

func BenchmarkModelViewSinglePaneCached(b *testing.B) {
	model := benchmarkModelWithPanes(b, 1, 120, 40)
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkStringSink = model.View()
	}
}

func BenchmarkModelViewFourPanesCached(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkStringSink = model.View()
	}
}

func BenchmarkModelViewSinglePaneFixedPinnedCached(b *testing.B) {
	model := benchmarkFixedViewportModel(b, 32, 14, true)
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkStringSink = model.View()
	}
}

func BenchmarkModelViewFourPanesOneDirty(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	tab := model.currentTab()
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		active := tab.Panes[tab.ActivePaneID]
		if active == nil {
			b.Fatal("expected active pane")
		}
		active.renderDirty = true
		benchmarkStringSink = model.View()
	}
}

func BenchmarkRenderTabCompositeFourPanes(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	tab := model.currentTab()
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFourPanesActiveSwitch(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	tab := model.currentTab()
	ids := tab.Root.LeafIDs()
	if len(ids) < 2 {
		b.Fatal("expected multiple panes")
	}
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tab.ActivePaneID = ids[i%len(ids)]
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFourPanesContentOnlyDirty(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	tab := model.currentTab()
	benchmarkStringSink = model.View()

	active := tab.Panes[tab.ActivePaneID]
	if active == nil {
		b.Fatal("expected active pane")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		active.renderDirty = true
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFourPanesCursorOnly(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	tab := model.currentTab()
	ids := tab.Root.LeafIDs()
	if len(ids) < 2 {
		b.Fatal("expected multiple panes")
	}
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prev := tab.Panes[tab.ActivePaneID]
		if prev != nil {
			prev.renderDirty = false
		}
		tab.ActivePaneID = ids[i%len(ids)]
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFloatingOverlayTiledDirty(b *testing.B) {
	model := benchmarkModelWithFloatingOverlay(b, 160, 48)
	tab := model.currentTab()
	basePane := tab.Panes[firstTiledPaneID(tab)]
	if basePane == nil {
		b.Fatal("expected tiled pane")
	}
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		basePane.renderDirty = true
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFloatingOverlayTiledDirtyRows(b *testing.B) {
	model := benchmarkModelWithFloatingOverlay(b, 160, 48)
	tab := model.currentTab()
	basePane := tab.Panes[firstTiledPaneID(tab)]
	if basePane == nil {
		b.Fatal("expected tiled pane")
	}
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		basePane.renderDirty = true
		basePane.SetDirtyRows(0, 0, true)
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFloatingOverlayTiledDirtySpan(b *testing.B) {
	model := benchmarkModelWithFloatingOverlay(b, 160, 48)
	tab := model.currentTab()
	basePane := tab.Panes[firstTiledPaneID(tab)]
	if basePane == nil {
		b.Fatal("expected tiled pane")
	}
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		basePane.renderDirty = true
		basePane.SetDirtyRows(0, 0, true)
		basePane.dirtyColsKnown = true
		basePane.dirtyColStart = 0
		basePane.dirtyColEnd = 7
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFloatingOverlayActiveTiledDirtySpan(b *testing.B) {
	model := benchmarkModelWithFloatingOverlay(b, 160, 48)
	tab := model.currentTab()
	baseID := firstTiledPaneID(tab)
	basePane := tab.Panes[baseID]
	if basePane == nil {
		b.Fatal("expected tiled pane")
	}
	tab.ActivePaneID = baseID
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		basePane.renderDirty = true
		basePane.SetDirtyRows(0, 0, true)
		basePane.dirtyColsKnown = true
		basePane.dirtyColStart = 0
		basePane.dirtyColEnd = 8
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFloatingOverlayFloatingDirty(b *testing.B) {
	model := benchmarkModelWithFloatingOverlay(b, 160, 48)
	tab := model.currentTab()
	if len(tab.Floating) == 0 {
		b.Fatal("expected floating pane")
	}
	floatPane := tab.Panes[tab.Floating[0].PaneID]
	if floatPane == nil {
		b.Fatal("expected floating pane")
	}
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		floatPane.renderDirty = true
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkRenderTabCompositeFloatingOverlayFloatingMove(b *testing.B) {
	model := benchmarkModelWithFloatingOverlay(b, 160, 48)
	tab := model.currentTab()
	if len(tab.Floating) == 0 {
		b.Fatal("expected floating pane")
	}
	entry := tab.Floating[0]
	base := entry.Rect
	benchmarkStringSink = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.Rect = Rect{X: base.X + (i % 6), Y: base.Y + (i % 4), W: base.W, H: base.H}
		benchmarkStringSink = model.renderTabComposite(tab, model.width, model.height-2)
	}
}

func BenchmarkHandlePaneOutputAndViewFourPanes(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	tab := model.currentTab()
	active := tab.Panes[tab.ActivePaneID]
	if active == nil {
		b.Fatal("expected active pane")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.handlePaneOutput(paneOutputMsg{
			paneID: active.ID,
			frame: protocol.StreamFrame{
				Type:    protocol.TypeOutput,
				Payload: []byte(fmt.Sprintf("tick-%d\r\n", i)),
			},
		})
		benchmarkStringSink = model.View()
	}
}

func BenchmarkHandlePaneOutputViewBatchedWithoutTick(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	model.renderBatching = true
	model.program = &tea.Program{}
	benchmarkStringSink = model.View()

	tab := model.currentTab()
	active := tab.Panes[tab.ActivePaneID]
	if active == nil {
		b.Fatal("expected active pane")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.handlePaneOutput(paneOutputMsg{
			paneID: active.ID,
			frame: protocol.StreamFrame{
				Type:    protocol.TypeOutput,
				Payload: []byte("batched\r\n"),
			},
		})
		benchmarkStringSink = model.View()
	}
}

func BenchmarkHandlePaneOutputViewBatchedWithTick(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	model.renderBatching = true
	model.program = &tea.Program{}
	benchmarkStringSink = model.View()

	tab := model.currentTab()
	active := tab.Panes[tab.ActivePaneID]
	if active == nil {
		b.Fatal("expected active pane")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.handlePaneOutput(paneOutputMsg{
			paneID: active.ID,
			frame: protocol.StreamFrame{
				Type:    protocol.TypeOutput,
				Payload: []byte("batched\r\n"),
			},
		})
		_, _ = model.Update(renderTickMsg{})
		benchmarkStringSink = model.View()
	}
}

func BenchmarkHandlePaneOutputViewFixedFollow(b *testing.B) {
	model := benchmarkFixedViewportModel(b, 32, 14, false)
	tab := model.currentTab()
	active := tab.Panes[tab.ActivePaneID]
	if active == nil {
		b.Fatal("expected active pane")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.handlePaneOutput(paneOutputMsg{
			paneID: active.ID,
			frame: protocol.StreamFrame{
				Type:    protocol.TypeOutput,
				Payload: []byte(fmt.Sprintf("fixed-%04d 0123456789abcdefghijklmnop\r\n", i)),
			},
		})
		benchmarkStringSink = model.View()
	}
}

func BenchmarkPaneCellsForViewportFixedCached(b *testing.B) {
	model := benchmarkFixedViewportModel(b, 32, 14, true)
	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		b.Fatal("expected active pane")
	}

	benchmarkGridSink = paneCellsForViewport(pane, 30, 12)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkGridSink = paneCellsForViewport(pane, 30, 12)
	}
}

func BenchmarkPaneCellsForViewportFixedRecrop(b *testing.B) {
	model := benchmarkFixedViewportModel(b, 32, 14, true)
	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		b.Fatal("expected active pane")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pane.Offset = Point{X: 24 + (i % 2), Y: 8}
		benchmarkGridSink = paneCellsForViewport(pane, 30, 12)
	}
}

func BenchmarkModelViewTerminalPicker100Items(b *testing.B) {
	model := benchmarkModelWithPanes(b, 1, 160, 48)
	items := make([]terminalPickerItem, 0, 100)
	for i := 0; i < 100; i++ {
		items = append(items, terminalPickerItem{
			Info: protocol.TerminalInfo{
				ID:      fmt.Sprintf("bench-%03d", i),
				Name:    fmt.Sprintf("worker-%03d", i),
				Command: []string{"tail", "-f", fmt.Sprintf("worker-%03d.log", i)},
				State:   "running",
			},
			Observed: i%3 != 0,
			Orphan:   i%3 == 0,
			Location: "ws:main / tab:1",
		})
	}
	model.terminalPicker = &terminalPicker{Items: items}
	model.terminalPicker.applyFilter()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkStringSink = model.View()
	}
}

func BenchmarkModelViewTerminalPicker100ItemsDirtyFilter(b *testing.B) {
	model := benchmarkModelWithPanes(b, 1, 160, 48)
	items := make([]terminalPickerItem, 0, 100)
	for i := 0; i < 100; i++ {
		items = append(items, terminalPickerItem{
			Info: protocol.TerminalInfo{
				ID:      fmt.Sprintf("bench-%03d", i),
				Name:    fmt.Sprintf("worker-%03d", i),
				Command: []string{"tail", "-f", fmt.Sprintf("worker-%03d.log", i)},
				State:   "running",
			},
			Observed: i%3 != 0,
			Orphan:   i%3 == 0,
			Location: "ws:main / tab:1",
		})
	}
	model.terminalPicker = &terminalPicker{Items: items}
	model.terminalPicker.applyFilter()
	benchmarkStringSink = model.View()

	queries := []string{"w", "wo", "wor", "worker-0", "log", "bench-0", "tail"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.terminalPicker.Query = queries[i%len(queries)]
		model.terminalPicker.applyFilter()
		model.invalidateRender()
		benchmarkStringSink = model.View()
	}
}

func TestBenchmarkModelWithPanesBuildsRequestedLayout(t *testing.T) {
	model := benchmarkModelWithPanes(t, 4, 160, 48)
	tab := model.currentTab()
	if tab == nil {
		t.Fatal("expected active tab")
	}
	if len(tab.Root.LeafIDs()) != 4 {
		t.Fatalf("expected 4 leaves, got %d", len(tab.Root.LeafIDs()))
	}
	if len(tab.Panes) != 4 {
		t.Fatalf("expected 4 panes, got %d", len(tab.Panes))
	}
}

func TestBenchmarkModelWithFloatingOverlayBuildsFloatingPane(t *testing.T) {
	model := benchmarkModelWithFloatingOverlay(t, 160, 48)
	tab := model.currentTab()
	if tab == nil {
		t.Fatal("expected active tab")
	}
	if len(tab.Floating) != 1 {
		t.Fatalf("expected 1 floating pane, got %d", len(tab.Floating))
	}
}

func benchmarkModelWithPanes(tb testing.TB, paneCount, width, height int) *Model {
	tb.Helper()
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = width
	model.height = height

	msg := mustRunCmdForBenchmark(tb, model.Init())
	_, cmd := model.Update(msg)
	runCmdForBenchmark(tb, model, cmd)

	splits := []rune{'%', '"', '%', '"', '%', '"'}
	for i := 1; i < paneCount; i++ {
		_ = activatePrefixForTest(model)
		_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{splits[(i-1)%len(splits)]}})
		msg = mustRunCmdForBenchmark(tb, cmd)
		_, _ = model.Update(msg)
		_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			cmd = commitDefaultTerminalCreatePrompt(tb, model)
		}
		msg = mustRunCmdForBenchmark(tb, cmd)
		_, cmd = model.Update(msg)
		runCmdForBenchmark(tb, model, cmd)
	}

	tab := model.currentTab()
	for i, paneID := range tab.Root.LeafIDs() {
		pane := tab.Panes[paneID]
		if pane == nil {
			tb.Fatalf("missing pane %q", paneID)
		}
		writeBenchmarkContent(tb, pane, i)
	}
	benchmarkStringSink = model.View()
	return model
}

func benchmarkFixedViewportModel(tb testing.TB, width, height int, pinned bool) *Model {
	tb.Helper()
	model := benchmarkModelWithPanes(tb, 1, width, height)
	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		tb.Fatal("expected active pane")
	}

	pane.VTerm.Resize(120, 40)
	writeBenchmarkContent(tb, pane, 0)
	pane.Mode = ViewportModeFixed
	pane.Pin = pinned
	pane.Offset = Point{X: 24, Y: 8}
	if !pinned {
		viewW, viewH, ok := model.paneViewportSizeInTab(tab, pane.ID)
		if !ok {
			tb.Fatal("expected visible fixed viewport")
		}
		_ = model.syncViewport(pane, viewW, viewH)
	}
	tab.renderCache = nil
	model.renderDirty = true
	return model
}

func benchmarkModelWithFloatingOverlay(tb testing.TB, width, height int) *Model {
	tb.Helper()
	model := benchmarkModelWithPanes(tb, 1, width, height)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	msg := mustRunCmdForBenchmark(tb, cmd)
	_, _ = model.Update(msg)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		cmd = commitDefaultTerminalCreatePrompt(tb, model)
	}
	msg = mustRunCmdForBenchmark(tb, cmd)
	_, cmd = model.Update(msg)
	runCmdForBenchmark(tb, model, cmd)
	model.clearPrefixState()

	tab := model.currentTab()
	if len(tab.Floating) == 0 {
		tb.Fatal("expected floating pane")
	}
	floating := tab.Floating[0]
	floating.Rect = Rect{X: 20, Y: 6, W: 48, H: 16}
	floatPane := tab.Panes[floating.PaneID]
	if floatPane == nil {
		tb.Fatal("expected floating pane")
	}
	floatPane.Title = "bench-float"
	writeBenchmarkContent(tb, floatPane, 9)
	tab.renderCache = nil
	model.renderDirty = true
	benchmarkStringSink = model.View()
	return model
}

func writeBenchmarkContent(tb testing.TB, pane *Pane, index int) {
	tb.Helper()
	if pane == nil || pane.VTerm == nil {
		tb.Fatal("expected live pane")
	}
	for row := 0; row < 30; row++ {
		line := fmt.Sprintf("\x1b[3%dm[pane-%d row-%02d] benchmark text 0123456789 abcdefghijklmnopqrstuvwxyz\r\n", (index+row)%7, index, row)
		if _, err := pane.VTerm.Write([]byte(line)); err != nil {
			tb.Fatalf("write benchmark content failed: %v", err)
		}
	}
	pane.live = true
	pane.renderDirty = true
}

func runCmdForBenchmark(tb testing.TB, model *Model, cmd tea.Cmd) {
	tb.Helper()
	for cmd != nil {
		msg := mustRunCmdForBenchmark(tb, cmd)
		var next tea.Cmd
		_, next = model.Update(msg)
		cmd = next
	}
}

func mustRunCmdForBenchmark(tb testing.TB, cmd tea.Cmd) tea.Msg {
	tb.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}
