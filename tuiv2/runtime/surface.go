package runtime

import (
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

type TerminalSurface interface {
	Size() protocol.Size
	Cursor() protocol.CursorState
	Modes() protocol.TerminalModes
	IsAlternateScreen() bool
	ScreenRows() int
	ScrollbackRows() int
	TotalRows() int
	Row(rowIndex int) []protocol.Cell
	RowTimestamp(rowIndex int) time.Time
	RowKind(rowIndex int) string
}

type materializedSurface struct {
	size               protocol.Size
	cursor             protocol.CursorState
	modes              protocol.TerminalModes
	alternateScreen    bool
	scrollback         [][]protocol.Cell
	screen             [][]protocol.Cell
	scrollbackTimes    []time.Time
	screenTimes        []time.Time
	scrollbackRowKinds []string
	screenRowKinds     []string
}

var runtimeRowsTracePath = strings.TrimSpace(os.Getenv("TERMX_DEBUG_TRACE_RUNTIME_ROWS"))
var runtimeRowsTraceMu sync.Mutex

func visibleSurface(terminal *TerminalRuntime) TerminalSurface {
	if terminal == nil || terminal.SurfaceVersion == 0 {
		return nil
	}
	return terminal.Surface
}

func materializeSurfaceFromVTerm(vt VTermLike) TerminalSurface {
	if vt == nil {
		return nil
	}
	state := vt.SnapshotRenderState()
	return &materializedSurface{
		size:               protocol.Size{Cols: uint16(state.Cols), Rows: uint16(state.Rows)},
		cursor:             protocolCursorFromVTerm(state.Cursor),
		modes:              protocolModesFromVTerm(state.Modes),
		alternateScreen:    state.Screen.IsAlternateScreen,
		scrollback:         protocolRowsFromVTerm(state.Scrollback),
		screen:             protocolRowsFromVTerm(state.Screen.Cells),
		scrollbackTimes:    normalizeSurfaceTimeSlice(state.ScrollbackTimestamps, len(state.Scrollback)),
		screenTimes:        normalizeSurfaceTimeSlice(state.ScreenTimestamps, len(state.Screen.Cells)),
		scrollbackRowKinds: normalizeSurfaceStringSlice(state.ScrollbackRowKinds, len(state.Scrollback)),
		screenRowKinds:     normalizeSurfaceStringSlice(state.ScreenRowKinds, len(state.Screen.Cells)),
	}
}

func (s *materializedSurface) Size() protocol.Size {
	if s == nil {
		return protocol.Size{}
	}
	return s.size
}

func (s *materializedSurface) Cursor() protocol.CursorState {
	if s == nil {
		return protocol.CursorState{}
	}
	return s.cursor
}

func (s *materializedSurface) Modes() protocol.TerminalModes {
	if s == nil {
		return protocol.TerminalModes{}
	}
	return s.modes
}

func (s *materializedSurface) IsAlternateScreen() bool {
	return s != nil && s.alternateScreen
}

func (s *materializedSurface) ScreenRows() int {
	if s == nil {
		return 0
	}
	return len(s.screen)
}

func (s *materializedSurface) ScrollbackRows() int {
	if s == nil {
		return 0
	}
	return len(s.scrollback)
}

func (s *materializedSurface) TotalRows() int {
	return s.ScrollbackRows() + s.ScreenRows()
}

func (s *materializedSurface) Row(rowIndex int) []protocol.Cell {
	if s == nil || rowIndex < 0 {
		return nil
	}
	if rowIndex < len(s.scrollback) {
		return s.scrollback[rowIndex]
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screen) {
		return nil
	}
	return s.screen[rowIndex]
}

func (s *materializedSurface) RowTimestamp(rowIndex int) time.Time {
	if s == nil || rowIndex < 0 {
		return time.Time{}
	}
	if rowIndex < len(s.scrollbackTimes) {
		return s.scrollbackTimes[rowIndex]
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screenTimes) {
		return time.Time{}
	}
	return s.screenTimes[rowIndex]
}

func (s *materializedSurface) RowKind(rowIndex int) string {
	if s == nil || rowIndex < 0 {
		return ""
	}
	if rowIndex < len(s.scrollbackRowKinds) {
		return s.scrollbackRowKinds[rowIndex]
	}
	rowIndex -= len(s.scrollback)
	if rowIndex < 0 || rowIndex >= len(s.screenRowKinds) {
		return ""
	}
	return s.screenRowKinds[rowIndex]
}

func protocolRowsFromVTerm(rows [][]localvterm.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for y, row := range rows {
		out[y] = protocolCellsFromVTermRow(row)
	}
	return out
}

func protocolCellsFromVTermRow(row []localvterm.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(row))
	for i, cell := range row {
		out[i] = protocolCellFromVTermCell(cell)
	}
	return out
}

func normalizeSurfaceTimeSlice(values []time.Time, count int) []time.Time {
	if count <= 0 {
		return nil
	}
	out := make([]time.Time, count)
	copy(out, values)
	return out
}

func normalizeSurfaceStringSlice(values []string, count int) []string {
	if count <= 0 {
		return nil
	}
	out := make([]string, count)
	copy(out, values)
	return out
}

func syncSurfaceScrollbackState(terminal *TerminalRuntime) {
	if terminal == nil || terminal.Surface == nil {
		return
	}
	if loaded := terminal.Surface.ScrollbackRows(); loaded > terminal.ScrollbackLoadedLimit {
		terminal.ScrollbackLoadedLimit = loaded
	}
	if terminal.ScrollbackLoadingLimit > 0 && terminal.Surface.ScrollbackRows() >= terminal.ScrollbackLoadingLimit {
		terminal.ScrollbackLoadingLimit = 0
	}
}

func (r *Runtime) publishSurface(terminal *TerminalRuntime) {
	if r == nil || terminal == nil {
		return
	}
	terminal.Surface = materializeSurfaceFromVTerm(terminal.VTerm)
	if terminal.Surface == nil {
		return
	}
	terminal.SurfaceVersion++
	terminal.PublishedGeneration = terminal.PublishGeneration
	terminal.PublishScheduled = false
	syncSurfaceScrollbackState(terminal)
	appendPublishedSurfaceRows(terminal)
	r.invalidate()
}

func appendPublishedSurfaceRows(terminal *TerminalRuntime) {
	if runtimeRowsTracePath == "" || terminal == nil || terminal.Surface == nil {
		return
	}
	runtimeRowsTraceMu.Lock()
	defer runtimeRowsTraceMu.Unlock()
	f, err := os.OpenFile(runtimeRowsTracePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	surface := terminal.Surface
	size := surface.Size()
	cursor := surface.Cursor()
	_, _ = fmt.Fprintf(f, "%s runtime.surface term=%s surfVer=%d snapVer=%d size=%dx%d cursor=%d,%d hash=%s\n", time.Now().Format(time.RFC3339Nano), terminal.TerminalID, terminal.SurfaceVersion, terminal.SnapshotVersion, size.Cols, size.Rows, cursor.Row, cursor.Col, runtimeSurfaceDigest(surface))
	start := surface.ScrollbackRows()
	maxRows := runtimeMinInt(6, surface.ScreenRows())
	for i := 0; i < maxRows; i++ {
		rowIndex := start + i
		text := runtimeProtocolRowPlainText(surface.Row(rowIndex), int(size.Cols))
		_, _ = fmt.Fprintf(f, "  row[%d] hash=%016x text=%q\n", rowIndex, runtimeHashString(text), text)
	}
}

func runtimeProtocolRowPlainText(row []protocol.Cell, width int) string {
	if width <= 0 {
		width = len(row)
	}
	var b strings.Builder
	col := 0
	for i := 0; i < len(row) && col < width; i++ {
		cell := row[i]
		if cell.Content == "" && cell.Width == 0 {
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		w := cell.Width
		if w <= 0 {
			w = runtimeMaxInt(1, len(content))
		}
		if col+w > width {
			break
		}
		b.WriteString(content)
		col += w
	}
	return b.String()
}

func runtimeHashString(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func runtimeSurfaceDigest(surface TerminalSurface) string {
	if surface == nil {
		return "nil"
	}
	h := fnv.New64a()
	size := surface.Size()
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%d|", size.Cols, size.Rows)))
	for i := 0; i < surface.TotalRows(); i++ {
		row := surface.Row(i)
		for _, cell := range row {
			_, _ = h.Write([]byte(cell.Content))
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte{'\n'})
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

func runtimeMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runtimeMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (r *Runtime) scheduleSurfacePublish(terminal *TerminalRuntime) {
	if r == nil || terminal == nil {
		return
	}
	terminal.PublishGeneration++
	generation := terminal.PublishGeneration
	if terminal.PublishScheduled || surfacePublishDebounce <= 0 {
		if surfacePublishDebounce <= 0 {
			r.publishSurface(terminal)
		}
		return
	}
	terminal.PublishScheduled = true
	time.AfterFunc(surfacePublishDebounce, func() {
		if r == nil || terminal == nil {
			return
		}
		if terminal.PublishGeneration != generation {
			terminal.PublishScheduled = false
			r.scheduleSurfacePublish(terminal)
			return
		}
		r.publishSurface(terminal)
	})
}

func (r *Runtime) bumpSurfaceVersion(terminal *TerminalRuntime) {
	if terminal == nil {
		return
	}
	terminal.SurfaceVersion++
	syncSurfaceScrollbackState(terminal)
}

func (r *Runtime) PublishSurfaceForTesting(terminalID string) bool {
	if r == nil || r.registry == nil || terminalID == "" {
		return false
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil || terminal.VTerm == nil {
		return false
	}
	r.publishSurface(terminal)
	return true
}
