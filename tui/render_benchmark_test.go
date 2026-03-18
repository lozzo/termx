package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
)

func BenchmarkModelViewSinglePaneCached(b *testing.B) {
	model := benchmarkModelWithPanes(b, 1, 120, 40)
	_ = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = model.View()
	}
}

func BenchmarkModelViewFourPanesCached(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	_ = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = model.View()
	}
}

func BenchmarkModelViewSinglePaneFixedPinnedCached(b *testing.B) {
	model := benchmarkFixedViewportModel(b, 32, 14, true)
	_ = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = model.View()
	}
}

func BenchmarkModelViewFourPanesOneDirty(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	tab := model.currentTab()
	_ = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		active := tab.Panes[tab.ActivePaneID]
		if active == nil {
			b.Fatal("expected active pane")
		}
		active.renderDirty = true
		_ = model.View()
	}
}

func BenchmarkRenderTabCompositeFourPanes(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	tab := model.currentTab()
	_ = model.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = model.renderTabComposite(tab, model.width, model.height-2)
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
		_ = model.View()
	}
}

func BenchmarkHandlePaneOutputViewBatchedWithoutTick(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	model.renderBatching = true
	model.program = &tea.Program{}
	_ = model.View()

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
		_ = model.View()
	}
}

func BenchmarkHandlePaneOutputViewBatchedWithTick(b *testing.B) {
	model := benchmarkModelWithPanes(b, 4, 160, 48)
	model.renderBatching = true
	model.program = &tea.Program{}
	_ = model.View()

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
		_ = model.View()
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
		_ = model.View()
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
		_ = model.View()
	}
}

func benchmarkModelWithPanes(b *testing.B, paneCount, width, height int) *Model {
	b.Helper()
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = width
	model.height = height

	msg := mustRunCmdForBenchmark(b, model.Init())
	_, cmd := model.Update(msg)
	runCmdForBenchmark(b, model, cmd)

	splits := []rune{'%', '"', '%', '"', '%', '"'}
	for i := 1; i < paneCount; i++ {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
		_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{splits[(i-1)%len(splits)]}})
		msg = mustRunCmdForBenchmark(b, cmd)
		_, cmd = model.Update(msg)
		runCmdForBenchmark(b, model, cmd)
	}

	tab := model.currentTab()
	for i, paneID := range tab.Root.LeafIDs() {
		pane := tab.Panes[paneID]
		if pane == nil {
			b.Fatalf("missing pane %q", paneID)
		}
		writeBenchmarkContent(b, pane, i)
	}
	_ = model.View()
	return model
}

func benchmarkFixedViewportModel(b *testing.B, width, height int, pinned bool) *Model {
	b.Helper()
	model := benchmarkModelWithPanes(b, 1, width, height)
	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		b.Fatal("expected active pane")
	}

	pane.VTerm.Resize(120, 40)
	writeBenchmarkContent(b, pane, 0)
	pane.Mode = ViewportModeFixed
	pane.Pin = pinned
	pane.Offset = Point{X: 24, Y: 8}
	if !pinned {
		viewW, viewH, ok := model.paneViewportSizeInTab(tab, pane.ID)
		if !ok {
			b.Fatal("expected visible fixed viewport")
		}
		_ = model.syncViewport(pane, viewW, viewH)
	}
	tab.renderCache = nil
	model.renderDirty = true
	return model
}

func writeBenchmarkContent(b *testing.B, pane *Pane, index int) {
	b.Helper()
	if pane == nil || pane.VTerm == nil {
		b.Fatal("expected live pane")
	}
	for row := 0; row < 30; row++ {
		line := fmt.Sprintf("\x1b[3%dm[pane-%d row-%02d] benchmark text 0123456789 abcdefghijklmnopqrstuvwxyz\r\n", (index+row)%7, index, row)
		if _, err := pane.VTerm.Write([]byte(line)); err != nil {
			b.Fatalf("write benchmark content failed: %v", err)
		}
	}
	pane.live = true
	pane.renderDirty = true
}

func runCmdForBenchmark(b *testing.B, model *Model, cmd tea.Cmd) {
	b.Helper()
	for cmd != nil {
		msg := mustRunCmdForBenchmark(b, cmd)
		var next tea.Cmd
		_, next = model.Update(msg)
		cmd = next
	}
}

func mustRunCmdForBenchmark(b *testing.B, cmd tea.Cmd) tea.Msg {
	b.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}
