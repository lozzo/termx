package termx

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/vterm"
)

func BenchmarkEventBusPublish64Subscribers(b *testing.B) {
	bus := NewEventBus(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 64; i++ {
		_ = bus.Subscribe(ctx)
	}

	evt := Event{Type: EventTerminalCreated, TerminalID: "bench"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(evt)
	}
}

func BenchmarkServerHandleRequestList(b *testing.B) {
	srv := NewServer()
	srv.terminals = make(map[string]*Terminal, 1000)
	for i := 0; i < 1000; i++ {
		id := strconv.Itoa(i)
		srv.terminals[id] = &Terminal{
			id:      id,
			name:    id,
			command: []string{"bash"},
			tags:    map[string]string{"group": "bench"},
			size:    Size{Cols: 80, Rows: 24},
			state:   StateRunning,
		}
	}

	req := protocol.Request{
		ID:     1,
		Method: "list",
		Params: json.RawMessage(`{}`),
	}
	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := srv.handleRequest(context.Background(), "bench", nil, allocator, attachments, &attachmentsMu, req, sendFrame); err != nil {
			b.Fatalf("handle request failed: %v", err)
		}
	}
}

func BenchmarkServerListParallel(b *testing.B) {
	srv := NewServer()
	srv.terminals = make(map[string]*Terminal, 1000)
	for i := 0; i < 1000; i++ {
		id := strconv.Itoa(i)
		srv.terminals[id] = &Terminal{
			id:      id,
			name:    id,
			command: []string{"bash"},
			tags:    map[string]string{"group": "bench"},
			size:    Size{Cols: 80, Rows: 24},
			state:   StateRunning,
		}
	}

	ctx := context.Background()
	opts := ListOptions{Tags: map[string]string{"group": "bench"}}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			infos, err := srv.List(ctx, opts)
			if err != nil {
				b.Fatalf("list failed: %v", err)
			}
			if len(infos) != 1000 {
				b.Fatalf("unexpected list size: %d", len(infos))
			}
		}
	})
}

func BenchmarkTerminalScreenUpdatePayloadFromDamageLocked(b *testing.B) {
	scenarios := []struct {
		name  string
		build func(*testing.B) (*Terminal, vterm.WriteDamage)
	}{
		{
			name:  "scroll_output",
			build: benchmarkTerminalDamageScrollOutput,
		},
		{
			name:  "fullscreen_alt",
			build: benchmarkTerminalDamageFullscreenAlt,
		},
		{
			name:  "nvim_alt_scroll_3_rows",
			build: benchmarkTerminalDamageNvimAltScroll,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			term, damage := scenario.build(b)
			totalBytes := 0
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				payload, ok := term.screenUpdatePayloadFromDamageLocked(damage)
				if !ok {
					b.Fatal("expected payload")
				}
				totalBytes += len(payload)
			}
			b.ReportMetric(float64(totalBytes)/float64(b.N), "payload_bytes")
		})
	}
}

func BenchmarkScreenUpdateEncodeStages(b *testing.B) {
	scenarios := []struct {
		name  string
		build func(*testing.B) (*Terminal, vterm.WriteDamage)
	}{
		{name: "scroll_output", build: benchmarkTerminalDamageScrollOutput},
		{name: "fullscreen_alt", build: benchmarkTerminalDamageFullscreenAlt},
		{name: "nvim_alt_scroll_3_rows", build: benchmarkTerminalDamageNvimAltScroll},
	}

	for _, scenario := range scenarios {
		term, damage := scenario.build(b)
		title := term.title
		deltaUpdate := screenUpdateFromDamageState(damage, title)
		state := term.currentStreamScreenStateLocked()
		if state == nil || state.snapshot == nil {
			b.Fatalf("%s: expected stream screen state", scenario.name)
		}
		fullUpdate := fullReplaceUpdateForStateDelta(nil, state, !state.snapshot.Modes.AlternateScreen)
		if state.snapshot.Modes.AlternateScreen {
			fullUpdate.ResetScrollback = false
			fullUpdate.ScrollbackTrim = deltaUpdate.ScrollbackTrim
			fullUpdate.ScrollbackAppend = append([]protocol.ScrollbackRowAppend(nil), deltaUpdate.ScrollbackAppend...)
		}

		b.Run(fmt.Sprintf("%s/from_damage_state", scenario.name), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				update := screenUpdateFromDamageState(damage, title)
				if update.Size != deltaUpdate.Size {
					b.Fatalf("unexpected update size: %#v", update.Size)
				}
			}
		})

		b.Run(fmt.Sprintf("%s/full_snapshot", scenario.name), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				next := term.currentStreamScreenStateLocked()
				if next == nil || next.snapshot == nil {
					b.Fatal("expected stream snapshot")
				}
			}
		})

		b.Run(fmt.Sprintf("%s/encode_strategy", scenario.name), func(b *testing.B) {
			totalBytes := 0
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				payload, mode, ok := encodeScreenUpdatePayloadByStrategy(deltaUpdate, fullUpdate, state.snapshot.Modes.AlternateScreen)
				if !ok {
					b.Fatal("expected encoded payload")
				}
				if mode == "" {
					b.Fatal("expected encode mode")
				}
				totalBytes += len(payload)
			}
			b.ReportMetric(float64(totalBytes)/float64(b.N), "payload_bytes")
		})
	}
}

type writeDamageBenchmarkCase struct {
	vt         *vterm.VTerm
	beforeEach func(int)
	payload    func(int) []byte
}

func BenchmarkVTermWriteScenarios(b *testing.B) {
	scenarios := []struct {
		name    string
		newCase func() *writeDamageBenchmarkCase
	}{
		{
			name: "single_char_churn",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(120, 40, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(120, 40, "seed"), vterm.CursorState{Row: 20, Col: 60, Visible: true}, vterm.TerminalModes{AutoWrap: true})
				payloads := [][]byte{
					[]byte("\x1b[21;61HX"),
					[]byte("\x1b[21;61HY"),
				}
				return &writeDamageBenchmarkCase{
					vt:      vt,
					payload: func(i int) []byte { return payloads[i&1] },
				}
			},
		},
		{
			name: "scroll_output",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(80, 24, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(80, 24, "log"), vterm.CursorState{Row: 23, Col: 0, Visible: true}, vterm.TerminalModes{AutoWrap: true})
				payloads := [][]byte{
					[]byte("scroll-a\n"),
					[]byte("scroll-b\n"),
				}
				return &writeDamageBenchmarkCase{
					vt:      vt,
					payload: func(i int) []byte { return payloads[i&1] },
				}
			},
		},
		{
			name: "nvim_alt_scroll_3_rows",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(120, 40, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(120, 40, "nvim"), vterm.CursorState{Row: 39, Col: 0, Visible: true}, vterm.TerminalModes{AlternateScreen: true, AutoWrap: true})
				payloads := [][]byte{
					[]byte("nvim-137\nnvim-138\nnvim-139\n"),
					[]byte("nvim-140\nnvim-141\nnvim-142\n"),
				}
				return &writeDamageBenchmarkCase{
					vt:      vt,
					payload: func(i int) []byte { return payloads[i&1] },
				}
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			caseData := scenario.newCase()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if caseData.beforeEach != nil {
					b.StopTimer()
					caseData.beforeEach(i)
					b.StartTimer()
				}
				if _, err := caseData.vt.Write(caseData.payload(i)); err != nil {
					b.Fatalf("Write failed: %v", err)
				}
			}
		})
	}
}

func BenchmarkVTermWriteWithDamageScenarios(b *testing.B) {
	scenarios := []struct {
		name    string
		newCase func() *writeDamageBenchmarkCase
	}{
		{
			name: "single_char_churn",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(120, 40, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(120, 40, "seed"), vterm.CursorState{Row: 20, Col: 60, Visible: true}, vterm.TerminalModes{AutoWrap: true})
				payloads := [][]byte{
					[]byte("\x1b[21;61HX"),
					[]byte("\x1b[21;61HY"),
				}
				return &writeDamageBenchmarkCase{
					vt:      vt,
					payload: func(i int) []byte { return payloads[i&1] },
				}
			},
		},
		{
			name: "scroll_output",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(80, 24, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(80, 24, "log"), vterm.CursorState{Row: 23, Col: 0, Visible: true}, vterm.TerminalModes{AutoWrap: true})
				payloads := [][]byte{
					[]byte("scroll-a\n"),
					[]byte("scroll-b\n"),
				}
				return &writeDamageBenchmarkCase{
					vt:      vt,
					payload: func(i int) []byte { return payloads[i&1] },
				}
			},
		},
		{
			name: "fullscreen_tui_update",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(100, 30, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(100, 30, "tui"), vterm.CursorState{Row: 0, Col: 0, Visible: true}, vterm.TerminalModes{AlternateScreen: true, AutoWrap: true})
				payloads := [][]byte{
					benchmarkFullScreenPayload(100, 30, 'X'),
					benchmarkFullScreenPayload(100, 30, 'Y'),
				}
				return &writeDamageBenchmarkCase{
					vt:      vt,
					payload: func(i int) []byte { return payloads[i&1] },
				}
			},
		},
		{
			name: "resize",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(100, 30, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(100, 30, "resize"), vterm.CursorState{Row: 0, Col: 0, Visible: true}, vterm.TerminalModes{AlternateScreen: true, AutoWrap: true})
				payloads := [][]byte{
					benchmarkFullScreenPayload(120, 40, 'R'),
					benchmarkFullScreenPayload(100, 30, 'S'),
				}
				return &writeDamageBenchmarkCase{
					vt: vt,
					beforeEach: func(i int) {
						if i&1 == 0 {
							vt.Resize(120, 40)
							return
						}
						vt.Resize(100, 30)
					},
					payload: func(i int) []byte { return payloads[i&1] },
				}
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			caseData := scenario.newCase()
			var (
				totalChangedRows  int64
				totalChangedCells int64
				totalEncodedBytes int64
				totalDiffCPUNs    int64
			)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if caseData.beforeEach != nil {
					b.StopTimer()
					caseData.beforeEach(i)
					b.StartTimer()
				}
				b.StopTimer()
				before := caseData.vt.ScreenContent()
				b.StartTimer()
				_, err, damage := caseData.vt.WriteWithDamage(caseData.payload(i))
				if err != nil {
					b.Fatalf("WriteWithDamage failed: %v", err)
				}
				b.StopTimer()
				after := caseData.vt.ScreenContent()
				changedRows, changedCells := benchmarkScreenDiff(before, after)
				totalChangedRows += int64(changedRows)
				totalChangedCells += int64(changedCells)
				totalEncodedBytes += int64(benchmarkEncodedDamageBytes(b, damage))
				totalDiffCPUNs += damage.DiffCPUNanos
				b.StartTimer()
			}
			b.ReportMetric(float64(totalChangedRows)/float64(b.N), "changed_rows")
			b.ReportMetric(float64(totalChangedCells)/float64(b.N), "changed_cells")
			b.ReportMetric(float64(totalEncodedBytes)/float64(b.N), "encoded_bytes")
			b.ReportMetric(float64(totalDiffCPUNs)/float64(b.N), "diff_cpu_ns")
		})
	}
}

func TestPerfVTermWriteWithDamageScenarios(t *testing.T) {
	if os.Getenv("TERMX_RUN_VTERM_WRITE_PERF") != "1" {
		t.Skip("set TERMX_RUN_VTERM_WRITE_PERF=1 to run vterm write perf scenarios")
	}

	scenarios := []struct {
		name    string
		newCase func() *writeDamageBenchmarkCase
	}{
		{
			name: "single_char_churn",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(120, 40, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(120, 40, "seed"), vterm.CursorState{Row: 20, Col: 60, Visible: true}, vterm.TerminalModes{AutoWrap: true})
				payloads := [][]byte{
					[]byte("\x1b[21;61HX"),
					[]byte("\x1b[21;61HY"),
				}
				return &writeDamageBenchmarkCase{vt: vt, payload: func(i int) []byte { return payloads[i&1] }}
			},
		},
		{
			name: "scroll_output",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(80, 24, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(80, 24, "log"), vterm.CursorState{Row: 23, Col: 0, Visible: true}, vterm.TerminalModes{AutoWrap: true})
				payloads := [][]byte{
					[]byte("scroll-a\n"),
					[]byte("scroll-b\n"),
				}
				return &writeDamageBenchmarkCase{vt: vt, payload: func(i int) []byte { return payloads[i&1] }}
			},
		},
		{
			name: "fullscreen_tui_update",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(100, 30, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(100, 30, "tui"), vterm.CursorState{Row: 0, Col: 0, Visible: true}, vterm.TerminalModes{AlternateScreen: true, AutoWrap: true})
				payloads := [][]byte{
					benchmarkFullScreenPayload(100, 30, 'X'),
					benchmarkFullScreenPayload(100, 30, 'Y'),
				}
				return &writeDamageBenchmarkCase{vt: vt, payload: func(i int) []byte { return payloads[i&1] }}
			},
		},
		{
			name: "resize",
			newCase: func() *writeDamageBenchmarkCase {
				vt := vterm.New(100, 30, 4096, nil)
				vt.LoadSnapshot(benchmarkFilledScreen(100, 30, "resize"), vterm.CursorState{Row: 0, Col: 0, Visible: true}, vterm.TerminalModes{AlternateScreen: true, AutoWrap: true})
				payloads := [][]byte{
					benchmarkFullScreenPayload(120, 40, 'R'),
					benchmarkFullScreenPayload(100, 30, 'S'),
				}
				return &writeDamageBenchmarkCase{
					vt: vt,
					beforeEach: func(i int) {
						if i&1 == 0 {
							vt.Resize(120, 40)
							return
						}
						vt.Resize(100, 30)
					},
					payload: func(i int) []byte { return payloads[i&1] },
				}
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			caseData := scenario.newCase()
			const iterations = 2000
			var (
				totalChangedRows  int64
				totalChangedCells int64
				totalEncodedBytes int64
				totalDiffCPUNs    int64
			)
			for i := 0; i < iterations; i++ {
				if caseData.beforeEach != nil {
					caseData.beforeEach(i)
				}
				before := caseData.vt.ScreenContent()
				_, err, damage := caseData.vt.WriteWithDamage(caseData.payload(i))
				if err != nil {
					t.Fatalf("WriteWithDamage failed: %v", err)
				}
				after := caseData.vt.ScreenContent()
				changedRows, changedCells := benchmarkScreenDiff(before, after)
				totalChangedRows += int64(changedRows)
				totalChangedCells += int64(changedCells)
				totalEncodedBytes += int64(benchmarkEncodedDamageBytesT(t, damage))
				totalDiffCPUNs += damage.DiffCPUNanos
			}
			t.Logf("%s changed_rows=%.2f changed_cells=%.2f encoded_bytes=%.2f diff_cpu_ns=%.0f",
				scenario.name,
				float64(totalChangedRows)/iterations,
				float64(totalChangedCells)/iterations,
				float64(totalEncodedBytes)/iterations,
				float64(totalDiffCPUNs)/iterations,
			)
		})
	}
}

func benchmarkFilledScreen(cols, rows int, label string) vterm.ScreenData {
	screen := make([][]vterm.Cell, rows)
	for y := 0; y < rows; y++ {
		rowText := fmt.Sprintf("%s-%02d", label, y)
		row := make([]vterm.Cell, cols)
		for x := 0; x < cols; x++ {
			content := " "
			if x < len(rowText) {
				content = string(rowText[x])
			}
			row[x] = vterm.Cell{Content: content, Width: 1}
		}
		screen[y] = row
	}
	return vterm.ScreenData{Cells: screen}
}

func benchmarkFullScreenPayload(cols, rows int, fill byte) []byte {
	var b strings.Builder
	for row := 0; row < rows; row++ {
		fmt.Fprintf(&b, "\x1b[%d;1H", row+1)
		for col := 0; col < cols; col++ {
			b.WriteByte(fill)
		}
	}
	return []byte(b.String())
}

func benchmarkEncodedDamageBytes(b *testing.B, damage vterm.WriteDamage) int {
	b.Helper()
	payload, err := benchmarkEncodeDamagePayload(damage)
	if err != nil {
		b.Fatalf("encode damage payload: %v", err)
	}
	return len(payload)
}

func benchmarkEncodedDamageBytesT(t *testing.T, damage vterm.WriteDamage) int {
	t.Helper()
	payload, err := benchmarkEncodeDamagePayload(damage)
	if err != nil {
		t.Fatalf("encode damage payload: %v", err)
	}
	return len(payload)
}

func benchmarkEncodeDamagePayload(damage vterm.WriteDamage) ([]byte, error) {
	update := protocol.ScreenUpdate{
		Size:             protocol.Size{Cols: uint16(damage.SizeCols), Rows: uint16(damage.SizeRows)},
		ScreenScroll:     damage.ScreenScroll,
		ChangedSpans:     make([]protocol.ScreenSpanUpdate, 0, len(damage.ChangedScreenSpans)),
		Ops:              make([]protocol.ScreenOp, 0, len(damage.Ops)+2),
		ScrollbackTrim:   damage.ScrollbackTrim,
		ScrollbackAppend: make([]protocol.ScrollbackRowAppend, 0, len(damage.ScrollbackAppend)),
		Cursor:           protocolCursorStateFromVTerm(damage.Cursor),
		Modes:            protocolModesFromVTerm(damage.Modes),
	}
	for _, span := range damage.ChangedScreenSpans {
		update.ChangedSpans = append(update.ChangedSpans, protocol.ScreenSpanUpdate{
			Row:       span.Row,
			ColStart:  span.ColStart,
			Cells:     protocolCellsFromVTermRow(span.Cells),
			Op:        span.Op,
			Timestamp: span.Timestamp,
			RowKind:   span.RowKind,
		})
	}
	for _, op := range damage.Ops {
		update.Ops = append(update.Ops, protocol.ScreenOp{
			Code:      op.Code,
			Rect:      protocol.ScreenRect{X: op.Rect.X, Y: op.Rect.Y, Width: op.Rect.Width, Height: op.Rect.Height},
			Src:       protocol.ScreenRect{X: op.Src.X, Y: op.Src.Y, Width: op.Src.Width, Height: op.Src.Height},
			DstX:      op.DstX,
			DstY:      op.DstY,
			Dx:        op.Dx,
			Dy:        op.Dy,
			Row:       op.Row,
			Col:       op.Col,
			Cells:     protocolCellsFromVTermRow(op.Cells),
			Timestamp: op.Timestamp,
			RowKind:   op.RowKind,
		})
	}
	for _, row := range damage.ScrollbackAppend {
		update.ScrollbackAppend = append(update.ScrollbackAppend, protocol.ScrollbackRowAppend{
			Cells:     protocolCellsFromVTermRow(row.Cells),
			Timestamp: row.Timestamp,
			RowKind:   row.RowKind,
		})
	}
	return protocol.EncodeScreenUpdatePayload(update)
}

func benchmarkScreenDiff(before, after vterm.ScreenData) (int, int) {
	maxRows := len(before.Cells)
	if len(after.Cells) > maxRows {
		maxRows = len(after.Cells)
	}
	changedRows := 0
	changedCells := 0
	for row := 0; row < maxRows; row++ {
		beforeRow := benchmarkScreenRow(before, row)
		afterRow := benchmarkScreenRow(after, row)
		maxCols := len(beforeRow)
		if len(afterRow) > maxCols {
			maxCols = len(afterRow)
		}
		rowChanged := false
		for col := 0; col < maxCols; col++ {
			if benchmarkScreenCell(beforeRow, col) != benchmarkScreenCell(afterRow, col) {
				rowChanged = true
				changedCells++
			}
		}
		if rowChanged {
			changedRows++
		}
	}
	return changedRows, changedCells
}

func benchmarkScreenRow(screen vterm.ScreenData, row int) []vterm.Cell {
	if row < 0 || row >= len(screen.Cells) {
		return nil
	}
	return screen.Cells[row]
}

func benchmarkScreenCell(row []vterm.Cell, col int) vterm.Cell {
	if col < 0 || col >= len(row) {
		return vterm.Cell{}
	}
	return row[col]
}

func benchmarkTerminalDamageScrollOutput(b *testing.B) (*Terminal, vterm.WriteDamage) {
	b.Helper()
	vt := vterm.New(80, 24, 4096, nil)
	vt.LoadSnapshot(
		benchmarkFilledScreen(80, 24, "log"),
		vterm.CursorState{Row: 23, Col: 0, Visible: true},
		vterm.TerminalModes{AutoWrap: true},
	)
	_, err, damage := vt.WriteWithDamage([]byte("scroll-a\n"))
	if err != nil {
		b.Fatalf("WriteWithDamage failed: %v", err)
	}
	return &Terminal{vterm: vt, title: "bench"}, damage
}

func benchmarkTerminalDamageFullscreenAlt(b *testing.B) (*Terminal, vterm.WriteDamage) {
	b.Helper()
	vt := vterm.New(100, 30, 4096, nil)
	vt.LoadSnapshot(
		benchmarkFilledScreen(100, 30, "tui"),
		vterm.CursorState{Row: 0, Col: 0, Visible: true},
		vterm.TerminalModes{AlternateScreen: true, AutoWrap: true},
	)
	_, err, damage := vt.WriteWithDamage(benchmarkFullScreenPayload(100, 30, 'X'))
	if err != nil {
		b.Fatalf("WriteWithDamage failed: %v", err)
	}
	return &Terminal{vterm: vt, title: "bench"}, damage
}

func benchmarkTerminalDamageNvimAltScroll(b *testing.B) (*Terminal, vterm.WriteDamage) {
	b.Helper()
	vt := vterm.New(120, 40, 4096, nil)
	vt.LoadSnapshot(
		benchmarkFilledScreen(120, 40, "nvim"),
		vterm.CursorState{Row: 39, Col: 0, Visible: true},
		vterm.TerminalModes{AlternateScreen: true, AutoWrap: true},
	)
	_, err, damage := vt.WriteWithDamage([]byte("nvim-137\nnvim-138\nnvim-139\n"))
	if err != nil {
		b.Fatalf("WriteWithDamage failed: %v", err)
	}
	return &Terminal{vterm: vt, title: "bench"}, damage
}
