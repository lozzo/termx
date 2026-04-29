package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	rtdebug "runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core"
	"github.com/lozzow/termx/termx-core/protocol"
	unixtransport "github.com/lozzow/termx/termx-core/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type perfResidencySnapshot struct {
	Label       string
	RSSKB       uint64
	HeapAlloc   uint64
	HeapObjects uint64
	NumGC       uint32
	Goroutines  int
}

func TestPerfResidencyTUI(t *testing.T) {
	if os.Getenv("TERMX_RUN_TUI_RESIDENCY") != "1" {
		t.Skip("set TERMX_RUN_TUI_RESIDENCY=1 to run tui residency harness")
	}

	scenarios := []struct {
		name       string
		workspaces map[string]*workbench.WorkspaceState
		configure  func(*Model)
	}{
		{
			name: "single_pane_idle",
			configure: func(model *Model) {
				registerPerfModelTerminal(model, "pane-1", "term-1", perfSnapshot("single", 96, 24), 1)
			},
		},
		{
			name: "side_by_side",
			workspaces: map[string]*workbench.WorkspaceState{
				"main": {
					Name:      "main",
					ActiveTab: 0,
					Tabs: []*workbench.TabState{{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
							"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
						},
						Root: &workbench.LayoutNode{
							Direction: workbench.SplitVertical,
							Ratio:     0.5,
							First:     workbench.NewLeaf("pane-1"),
							Second:    workbench.NewLeaf("pane-2"),
						},
					}},
				},
			},
			configure: func(model *Model) {
				registerPerfModelTerminal(model, "pane-1", "term-1", perfSnapshot("left", 48, 24), 1)
				registerPerfModelTerminal(model, "pane-2", "term-2", perfSnapshot("right", 48, 24), 2)
			},
		},
		{
			name: "floating_overlay",
			workspaces: map[string]*workbench.WorkspaceState{
				"main": {
					Name:      "main",
					ActiveTab: 0,
					Tabs: []*workbench.TabState{{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
							"pane-2": {ID: "pane-2", Title: "float", TerminalID: "term-2"},
						},
						Root: workbench.NewLeaf("pane-1"),
						Floating: []*workbench.FloatingState{{
							PaneID:  "pane-2",
							Rect:    workbench.Rect{X: 16, Y: 5, W: 40, H: 12},
							Z:       1,
							Display: workbench.FloatingDisplayExpanded,
							FitMode: workbench.FloatingFitManual,
						}},
						FloatingVisible: true,
					}},
				},
			},
			configure: func(model *Model) {
				registerPerfModelTerminal(model, "pane-1", "term-1", perfSnapshot("base", 96, 24), 1)
				registerPerfModelTerminal(model, "pane-2", "term-2", perfSnapshot("float", 40, 12), 2)
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			model := setupModel(t, modelOpts{
				workspaces: scenario.workspaces,
				width:      120,
				height:     40,
			})
			scenario.configure(model)
			_, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			runPerfRenderLoop(model, 320)
			sample := takePerfResidencySnapshot(t, scenario.name)
			t.Logf("%s rss_kb=%d heap_alloc=%d heap_objects=%d num_gc=%d goroutines=%d",
				sample.Label, sample.RSSKB, sample.HeapAlloc, sample.HeapObjects, sample.NumGC, sample.Goroutines)
		})
	}
}

func TestPerfResidencyCombined(t *testing.T) {
	if os.Getenv("TERMX_RUN_COMBINED_RESIDENCY") != "1" {
		t.Skip("set TERMX_RUN_COMBINED_RESIDENCY=1 to run combined residency harness")
	}

	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.ListenAndServe(ctx)
	}()
	defer func() {
		_ = srv.Shutdown(context.Background())
		cancel()
		select {
		case <-serverDone:
		case <-time.After(2 * time.Second):
		}
	}()

	client := dialPerfProtocolClient(t, ctx, socketPath)
	defer client.Close()

	created, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"/bin/sh", "-lc", "cat"},
		Name:    "perf-combined-cat",
		Size:    protocol.Size{Cols: 96, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	defer func() {
		_ = client.Kill(context.Background(), created.TerminalID)
	}()

	attach, err := client.Attach(ctx, created.TerminalID, string(termx.ModeCollaborator))
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}

	rt := runtime.New(bridge.NewProtocolClient(client))
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "combined", TerminalID: created.TerminalID},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	model := New(shared.Config{}, wb, rt)
	rtTerminal := rt.Registry().GetOrCreate(created.TerminalID)
	rtTerminal.Name = "perf-combined-cat"
	rtTerminal.State = "running"
	rtTerminal.Channel = attach.Channel
	rtTerminal.AttachMode = attach.Mode
	binding := rt.BindPane("pane-1")
	binding.Channel = attach.Channel
	binding.Connected = true
	_, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if err := rt.StartStream(ctx, created.TerminalID); err != nil {
		t.Fatalf("start stream: %v", err)
	}
	waitForPerfCondition(t, 5*time.Second, func() bool {
		return rt.Registry().Get(created.TerminalID) != nil && rt.Registry().Get(created.TerminalID).VTerm != nil
	})
	idleSample := takePerfResidencySnapshot(t, "combined_idle_attached")
	t.Logf("%s rss_kb=%d heap_alloc=%d heap_objects=%d num_gc=%d goroutines=%d",
		idleSample.Label, idleSample.RSSKB, idleSample.HeapAlloc, idleSample.HeapObjects, idleSample.NumGC, idleSample.Goroutines)

	payload := []byte(strings.Repeat("combined-perf-line\n", 8))
	for i := 0; i < 24; i++ {
		if err := client.Input(ctx, attach.Channel, payload); err != nil {
			t.Fatalf("send input burst %d: %v", i, err)
		}
	}
	waitForPerfViewContains(t, model, "combined-perf-line", 5*time.Second)
	runPerfRenderLoop(model, 160)

	sample := takePerfResidencySnapshot(t, "combined_after_burst")
	t.Logf("%s rss_kb=%d heap_alloc=%d heap_objects=%d num_gc=%d goroutines=%d",
		sample.Label, sample.RSSKB, sample.HeapAlloc, sample.HeapObjects, sample.NumGC, sample.Goroutines)
}

func registerPerfModelTerminal(model *Model, paneID, terminalID string, snap *protocol.Snapshot, channel uint16) {
	if model == nil || model.runtime == nil {
		return
	}
	terminal := model.runtime.Registry().GetOrCreate(terminalID)
	terminal.Name = terminalID
	terminal.State = "running"
	terminal.Channel = channel
	terminal.Snapshot = snap
	terminal.SnapshotVersion = 1
	terminal.SurfaceVersion = 1
	binding := model.runtime.BindPane(paneID)
	binding.Channel = channel
	binding.Connected = true
}

func perfSnapshot(label string, cols, rows int) *protocol.Snapshot {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	screen := make([][]protocol.Cell, rows)
	for y := 0; y < rows; y++ {
		rowText := fmt.Sprintf("%s-%03d %s", label, y, strings.Repeat("x", maxInt(1, cols/2)))
		screen[y] = perfProtocolRow(rowText, cols)
	}
	return &protocol.Snapshot{
		TerminalID: label,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen: protocol.ScreenData{
			Cells: screen,
		},
		Timestamp: time.Now().UTC(),
	}
}

func perfProtocolRow(text string, width int) []protocol.Cell {
	if width <= 0 {
		width = len(text)
	}
	out := make([]protocol.Cell, 0, width)
	runes := []rune(text)
	for i := 0; i < width; i++ {
		content := " "
		if i < len(runes) {
			content = string(runes[i])
		}
		out = append(out, protocol.Cell{Content: content, Width: 1})
	}
	return out
}

func runPerfRenderLoop(model *Model, iterations int) {
	if model == nil {
		return
	}
	for i := 0; i < iterations; i++ {
		model.render.Invalidate()
		_ = model.View()
	}
}

func takePerfResidencySnapshot(t *testing.T, label string) perfResidencySnapshot {
	t.Helper()
	goruntime.GC()
	rtdebug.FreeOSMemory()
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)
	return perfResidencySnapshot{
		Label:       label,
		RSSKB:       currentPerfRSSKB(t),
		HeapAlloc:   mem.HeapAlloc,
		HeapObjects: mem.HeapObjects,
		NumGC:       mem.NumGC,
		Goroutines:  goruntime.NumGoroutine(),
	}
}

func currentPerfRSSKB(t *testing.T) uint64 {
	t.Helper()
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Logf("rss lookup failed: %v", err)
		return 0
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return 0
	}
	rss, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		t.Logf("rss parse failed: %v", err)
		return 0
	}
	return rss
}

func dialPerfProtocolClient(t *testing.T, ctx context.Context, socketPath string) *protocol.Client {
	t.Helper()
	var lastErr error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		transport, err := unixtransport.Dial(socketPath)
		if err == nil {
			client := protocol.NewClient(transport)
			if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
				lastErr = err
				_ = client.Close()
			} else {
				return client
			}
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("dial protocol client: %v", lastErr)
	return nil
}

func waitForPerfCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func waitForPerfViewContains(t *testing.T, model *Model, needle string, timeout time.Duration) {
	t.Helper()
	waitForPerfCondition(t, timeout, func() bool {
		return strings.Contains(model.View(), needle)
	})
}
