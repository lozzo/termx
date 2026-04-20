package vterm

import (
	"bytes"
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	charmvt "github.com/charmbracelet/x/vt"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"golang.org/x/text/unicode/norm"
)

var safeEmulatorWrite = func(emu *charmvt.SafeEmulator, data []byte) (int, error) {
	return emu.Write(data)
}

type Cell struct {
	Content string
	Width   int
	Style   CellStyle
}

type CellStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

type CursorShape string

const (
	CursorBlock     CursorShape = "block"
	CursorUnderline CursorShape = "underline"
	CursorBar       CursorShape = "bar"
)

type CursorState struct {
	Row     int
	Col     int
	Visible bool
	Shape   CursorShape
	Blink   bool
}

type TerminalModes struct {
	AlternateScreen   bool
	AlternateScroll   bool
	MouseTracking     bool
	MouseX10          bool
	MouseNormal       bool
	MouseButtonEvent  bool
	MouseAnyEvent     bool
	MouseSGR          bool
	BracketedPaste    bool
	ApplicationCursor bool
	AutoWrap          bool
}

type ScreenData struct {
	Cells             [][]Cell
	IsAlternateScreen bool
}

// ResponseHandler is called when the emulator produces a response (e.g. DSR
// cursor position report). The data should be written to the PTY's stdin so
// the child process receives it.
type ResponseHandler func(data []byte)

// TitleHandler is called when the terminal title changes (OSC 2).
type TitleHandler func(title string)

type VTerm struct {
	emu *charmvt.SafeEmulator

	mu        sync.RWMutex
	cursor    CursorState
	modes     TerminalModes
	mouseMode mouseModeState
	resp      ResponseHandler
	onTitle   TitleHandler
	sbSize    int
	defaultFG string
	defaultBG string
	palette   map[int]string

	scrollbackTimestamps   []time.Time
	screenTimestamps       []time.Time
	scrollbackRowKinds     []string
	screenRowKinds         []string
	screenRowCache         [][]Cell
	scrollbackRowCache     [][]Cell
	screenFingerprintCache []rowFingerprint
	resizeTailStartCol     int
	resizeTailBG           []string
	resizeBottomFillBG     string
	resizeBottomFillRow    int

	done chan struct{} // closed when drain goroutine exits
}

type mouseModeState struct {
	x10         bool
	normal      bool
	highlight   bool
	buttonEvent bool
	anyEvent    bool
	sgr         bool
}

type rowFingerprint struct {
	hash  uint64
	blank bool
}

type rowCacheReconcilePlan struct {
	afterScreen               []rowFingerprint
	preservedFromBefore       int
	requiredScrollbackAppends int
	beforeScrollbackLen       int
	screenScrollShift         int
}

type DamageRow struct {
	Row       int
	Cells     []Cell
	Timestamp time.Time
	RowKind   string
}

type WriteDamage struct {
	ChangedScreenRows []DamageRow
	ScrollbackAppend  []DamageRow
	ScrollbackTrim    int
	ScreenScroll      int
	Cursor            CursorState
	Modes             TerminalModes
	SizeCols          int
	SizeRows          int
	DiffCPUNanos      int64
}

const modeAlternateScroll ansi.DECMode = 1007

const (
	rowFingerprintOffset64 = 14695981039346656037
	rowFingerprintPrime64  = 1099511628211
)

func New(cols, rows int, scrollbackSize int, onResponse ResponseHandler) *VTerm {
	v := &VTerm{
		cursor: CursorState{
			Visible: true,
			Shape:   CursorBlock,
		},
		modes:  TerminalModes{AutoWrap: true},
		resp:   onResponse,
		sbSize: scrollbackSize,
		done:   make(chan struct{}),
	}
	v.resetEmulator(cols, rows)
	return v
}

func (v *VTerm) resetEmulator(cols, rows int) {
	emu := charmvt.NewSafeEmulator(cols, rows)
	emu.SetScrollbackSize(v.sbSize)
	v.applyDefaultColorsToEmulator(emu)
	v.emu = emu
	v.scrollbackTimestamps = nil
	v.screenTimestamps = make([]time.Time, maxInt(rows, 1))
	v.scrollbackRowKinds = nil
	v.screenRowKinds = make([]string, maxInt(rows, 1))
	v.invalidateRowCachesLocked()
	v.invalidateFingerprintCacheLocked()
	emu.SetCallbacks(charmvt.Callbacks{
		AltScreen: func(on bool) {
			// Called from within Write(), which already holds v.mu.Lock()
			v.modes.AlternateScreen = on
		},
		CursorVisibility: func(visible bool) {
			v.cursor.Visible = visible
		},
		CursorStyle: func(style charmvt.CursorStyle, blink bool) {
			switch style {
			case charmvt.CursorUnderline:
				v.cursor.Shape = CursorUnderline
			case charmvt.CursorBar:
				v.cursor.Shape = CursorBar
			default:
				v.cursor.Shape = CursorBlock
			}
			v.cursor.Blink = blink
		},
		EnableMode: func(mode ansi.Mode) {
			v.setMode(mode, true)
		},
		DisableMode: func(mode ansi.Mode) {
			v.setMode(mode, false)
		},
		Title: func(title string) {
			if v.onTitle != nil {
				v.onTitle(title)
			}
		},
	})

	// Drain the emulator's response pipe. Without this, programs that send
	// DSR (Device Status Report, e.g. vi/vim) will deadlock because the
	// emulator writes the response to an io.Pipe and nobody reads it,
	// blocking the Write() call that holds the lock.
	done := make(chan struct{})
	v.done = done
	go func(emu *charmvt.SafeEmulator) {
		defer close(done)
		v.drainResponses(emu, v.resp)
	}(emu)
}

// drainResponses reads from the emulator's response pipe and forwards data
// to the handler. Exits when the emulator is closed (Read returns error).
func (v *VTerm) drainResponses(emu *charmvt.SafeEmulator, handler ResponseHandler) {
	buf := make([]byte, 256)
	for {
		n, err := emu.Read(buf)
		if n > 0 && handler != nil {
			data := make([]byte, n)
			copy(data, buf[:n])
			handler(data)
		}
		if err != nil {
			return
		}
	}
}

func (v *VTerm) Write(data []byte) (n int, err error) {
	n, err, _ = v.WriteWithDamage(data)
	return n, err
}

func (v *VTerm) WriteWithDamage(data []byte) (n int, err error, damage WriteDamage) {
	finish := perftrace.Measure("vterm.write")
	defer func() {
		finish(len(data))
	}()
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureScreenFingerprintCacheLocked()
	beforeWidth := 0
	beforeHeight := 0
	beforeAltScreen := false
	if v.emu != nil {
		beforeWidth = v.emu.Width()
		beforeHeight = v.emu.Height()
		beforeAltScreen = v.emu.IsAltScreen()
	}
	snapshotFinish := perftrace.Measure("vterm.write.before_snapshot")
	beforeScreen := append([]rowFingerprint(nil), v.screenFingerprintCache...)
	beforeScrollbackLen := v.scrollbackRowCountLocked()
	beforeScreenTimestamps := cloneTimeSlice(v.screenTimestamps)
	beforeScreenRowKinds := cloneStringSlice(v.screenRowKinds)
	snapshotFinish(0)
	defer func() {
		if r := recover(); r != nil {
			n = 0
			err = fmt.Errorf("vterm write panic: %v", r)
			damage = WriteDamage{}
		}
	}()
	normalized := normalizeRenderableUTF8(data)
	v.clearTouchedRowsLocked()
	emulatorFinish := perftrace.Measure("vterm.write.emulator")
	n, err = safeEmulatorWrite(v.emu, normalized)
	emulatorFinish(len(normalized))
	pos := v.emu.CursorPosition()
	v.cursor.Row = pos.Y
	v.cursor.Col = pos.X
	v.modes.AlternateScreen = v.emu.IsAltScreen()
	diffStart := time.Now()
	reconcileFinish := perftrace.Measure("vterm.write.reconcile")
	afterWidth := 0
	afterHeight := 0
	afterAltScreen := false
	if v.emu != nil {
		afterWidth = v.emu.Width()
		afterHeight = v.emu.Height()
		afterAltScreen = v.emu.IsAltScreen()
	}
	dirtyRows, dirtyReliable := v.consumeTouchedRowsLocked()
	now := time.Now().UTC()
	switch {
	case !dirtyReliable,
		beforeWidth != afterWidth,
		beforeHeight != afterHeight,
		beforeAltScreen != afterAltScreen:
		damage = v.writeDamageFullCompareLocked(beforeScreen, beforeScreenTimestamps, beforeScreenRowKinds, beforeScrollbackLen, now)
	default:
		damage = v.writeDamageDirtyRowsLocked(beforeScreen, beforeScreenTimestamps, beforeScreenRowKinds, beforeScrollbackLen, dirtyRows, now)
	}
	damage.DiffCPUNanos = time.Since(diffStart).Nanoseconds()
	reconcileFinish(0)
	perftrace.Count("vterm.write.changed_rows", damageChangedRowCount(damage))
	perftrace.Count("vterm.write.changed_cells", damageChangedCellCount(damage))
	perftrace.Count("vterm.write.diff_cpu_ns", int(damage.DiffCPUNanos))
	return n, err, damage
}

func (v *VTerm) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.emu == nil {
		return nil
	}
	err := v.emu.Close()
	<-v.done
	v.emu = nil
	v.invalidateRowCachesLocked()
	v.invalidateFingerprintCacheLocked()
	return err
}

func (v *VTerm) LoadSnapshot(screen ScreenData, cursor CursorState, modes TerminalModes) {
	v.LoadSnapshotWithScrollback(nil, screen, cursor, modes)
}

func (v *VTerm) LoadSnapshotWithScrollback(scrollback [][]Cell, screen ScreenData, cursor CursorState, modes TerminalModes) {
	v.LoadSnapshotWithTimestamps(scrollback, nil, screen, nil, cursor, modes)
}

func (v *VTerm) LoadSnapshotWithTimestamps(scrollback [][]Cell, scrollbackTimestamps []time.Time, screen ScreenData, screenTimestamps []time.Time, cursor CursorState, modes TerminalModes) {
	v.LoadSnapshotWithMetadata(scrollback, scrollbackTimestamps, nil, screen, screenTimestamps, nil, cursor, modes)
}

func (v *VTerm) LoadSnapshotWithMetadata(scrollback [][]Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, screen ScreenData, screenTimestamps []time.Time, screenRowKinds []string, cursor CursorState, modes TerminalModes) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.emu != nil {
		_ = v.emu.Close()
		<-v.done
	}

	height := len(screen.Cells)
	width := 1
	for _, row := range screen.Cells {
		if len(row) > width {
			width = len(row)
		}
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if cursor.Col+1 > width {
		width = cursor.Col + 1
	}
	if cursor.Row+1 > height {
		height = cursor.Row + 1
	}

	v.cursor = cursor
	v.modes = modes
	v.resetEmulator(width, height)
	v.scrollbackTimestamps = normalizeTimeSlice(scrollbackTimestamps, len(scrollback))
	v.scrollbackRowKinds = normalizeStringSlice(scrollbackRowKinds, len(scrollback))
	v.screenTimestamps = normalizeTimeSlice(screenTimestamps, height)
	v.screenRowKinds = normalizeStringSlice(screenRowKinds, height)
	v.loadMouseModesLocked(modes)
	if len(scrollback) > 0 {
		sb := v.emu.Emulator.Scrollback()
		for _, row := range scrollback {
			sb.Push(uvLine(row))
		}
	}
	v.alignScrollbackMetadataLocked()
	v.clearResizeTailFillLocked()
	if modes.AlternateScreen {
		_, _ = v.emu.Write([]byte("\x1b[?1049h"))
	}
	if len(screen.Cells) > 0 {
		// 中文说明：不要逐格 SetCell。直接回放整屏 ANSI 可以把内容、样式和
		// 宽字符续位一次性恢复进 emulator，避免宽字符在后续刷新时被打散。
		_, _ = safeEmulatorWrite(v.emu, encodeScreenSnapshot(screen.Cells))
	}
	if cursor.Visible {
		_, _ = v.emu.Write([]byte("\x1b[?25h"))
	} else {
		_, _ = v.emu.Write([]byte("\x1b[?25l"))
	}
	if modes.ApplicationCursor {
		_, _ = v.emu.Write([]byte("\x1b[?1h"))
	} else {
		_, _ = v.emu.Write([]byte("\x1b[?1l"))
	}
	if modes.BracketedPaste {
		_, _ = v.emu.Write([]byte("\x1b[?2004h"))
	} else {
		_, _ = v.emu.Write([]byte("\x1b[?2004l"))
	}
	if !modes.AutoWrap {
		_, _ = v.emu.Write([]byte("\x1b[?7l"))
	}
	if cursor.Row >= 0 && cursor.Col >= 0 {
		_, _ = v.emu.Write([]byte(fmt.Sprintf("\x1b[%d;%dH", cursor.Row+1, cursor.Col+1)))
	}
	v.invalidateRowCachesLocked()
	v.invalidateFingerprintCacheLocked()
}

func (v *VTerm) ApplyScreenUpdate(update protocol.ScreenUpdate) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.applyScreenUpdateLocked(update)
}

func (v *VTerm) applyScreenUpdateLocked(update protocol.ScreenUpdate) bool {
	if !v.canApplyScreenUpdateLocked(update) {
		return false
	}
	targetCols, targetRows := v.screenUpdateTargetSizeLocked(update)
	if targetCols != v.emu.Width() || targetRows != v.emu.Height() {
		v.resizeLocked(targetCols, targetRows)
	}
	if !v.applyScreenUpdateScrollbackLocked(update) {
		return false
	}
	v.applyScreenScrollLocked(update.ScreenScroll)

	var b strings.Builder
	for _, row := range update.ChangedRows {
		writeAbsoluteProtocolRowSnapshot(&b, row.Row, row.Cells)
	}

	modes := terminalModesFromProtocol(update.Modes)
	cursor := cursorStateFromProtocol(update.Cursor)
	writeTerminalModesANSI(&b, modes)
	writeCursorShapeANSI(&b, cursor)
	if cursor.Row >= 0 && cursor.Col >= 0 {
		fmt.Fprintf(&b, "\x1b[%d;%dH", cursor.Row+1, cursor.Col+1)
	}
	if cursor.Visible {
		b.WriteString("\x1b[?25h")
	} else {
		b.WriteString("\x1b[?25l")
	}

	if b.Len() > 0 {
		if _, err := safeEmulatorWrite(v.emu, []byte(b.String())); err != nil {
			return false
		}
	}

	height := v.emu.Height()
	v.screenTimestamps = normalizeTimeSlice(v.screenTimestamps, height)
	v.screenRowKinds = normalizeStringSlice(v.screenRowKinds, height)
	for _, row := range update.ChangedRows {
		if row.Row < 0 || row.Row >= height {
			continue
		}
		v.screenTimestamps[row.Row] = row.Timestamp
		v.screenRowKinds[row.Row] = row.RowKind
	}
	v.cursor = cursor
	v.modes = modes
	v.loadMouseModesLocked(modes)
	v.invalidateRowCachesLocked()
	v.invalidateFingerprintCacheLocked()
	return true
}

func (v *VTerm) applyScreenScrollLocked(delta int) {
	if v == nil || v.emu == nil || delta == 0 {
		return
	}
	height := v.emu.Height()
	width := v.emu.Width()
	if height <= 0 || width <= 0 {
		return
	}
	if delta >= height || delta <= -height {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				v.emu.Emulator.SetCell(x, y, uvBlankCell())
			}
		}
		zeroTime := make([]time.Time, height)
		zeroKinds := make([]string, height)
		v.screenTimestamps = zeroTime
		v.screenRowKinds = zeroKinds
		return
	}
	screen := make([][]Cell, height)
	for y := 0; y < height; y++ {
		screen[y] = cloneCellSlice(v.screenRowViewLocked(y))
	}
	nextTimes := normalizeTimeSlice(v.screenTimestamps, height)
	nextKinds := normalizeStringSlice(v.screenRowKinds, height)
	if delta > 0 {
		for y := 0; y < height-delta; y++ {
			screen[y] = screen[y+delta]
			nextTimes[y] = nextTimes[y+delta]
			nextKinds[y] = nextKinds[y+delta]
		}
		for y := height - delta; y < height; y++ {
			screen[y] = nil
			nextTimes[y] = time.Time{}
			nextKinds[y] = ""
		}
	} else {
		shift := -delta
		for y := height - 1; y >= shift; y-- {
			screen[y] = screen[y-shift]
			nextTimes[y] = nextTimes[y-shift]
			nextKinds[y] = nextKinds[y-shift]
		}
		for y := 0; y < shift; y++ {
			screen[y] = nil
			nextTimes[y] = time.Time{}
			nextKinds[y] = ""
		}
	}
	for y := 0; y < height; y++ {
		row := screen[y]
		for x := 0; x < width; x++ {
			if x < len(row) {
				v.emu.Emulator.SetCell(x, y, uvCell(row[x]))
				continue
			}
			v.emu.Emulator.SetCell(x, y, uvBlankCell())
		}
	}
	v.screenTimestamps = nextTimes
	v.screenRowKinds = nextKinds
}

func (v *VTerm) canApplyScreenUpdateLocked(update protocol.ScreenUpdate) bool {
	if v == nil || v.emu == nil || update.FullReplace {
		return false
	}
	if update.ResetScrollback {
		return false
	}
	targetCols, targetRows := v.screenUpdateTargetSizeLocked(update)
	if targetCols <= 0 || targetRows <= 0 {
		return false
	}
	if v.emu.IsAltScreen() != update.Modes.AlternateScreen {
		return false
	}
	for _, row := range update.ChangedRows {
		if row.Row < 0 || row.Row >= targetRows {
			return false
		}
	}
	return true
}

func (v *VTerm) applyScreenUpdateScrollbackLocked(update protocol.ScreenUpdate) bool {
	if v == nil || v.emu == nil {
		return false
	}
	if update.ScrollbackTrim <= 0 && len(update.ScrollbackAppend) == 0 {
		return true
	}

	sb := v.emu.Emulator.Scrollback()
	currentLines := sb.Lines()
	trim := update.ScrollbackTrim
	if trim < 0 {
		trim = 0
	}
	if trim > len(currentLines) {
		trim = len(currentLines)
	}

	nextLines := make([]uv.Line, 0, len(currentLines)-trim+len(update.ScrollbackAppend))
	for i := trim; i < len(currentLines); i++ {
		nextLines = append(nextLines, cloneUVLine(currentLines[i]))
	}
	for _, row := range update.ScrollbackAppend {
		nextLines = append(nextLines, uvLineFromProtocol(row.Cells))
	}

	nextTimestamps := append([]time.Time(nil), tailTimeSlice(v.scrollbackTimestamps, trim)...)
	nextKinds := append([]string(nil), tailStringSlice(v.scrollbackRowKinds, trim)...)
	for _, row := range update.ScrollbackAppend {
		nextTimestamps = append(nextTimestamps, row.Timestamp)
		nextKinds = append(nextKinds, row.RowKind)
	}

	sb.Clear()
	for _, line := range nextLines {
		sb.Push(line)
	}
	v.scrollbackTimestamps = normalizeTimeSlice(nextTimestamps, len(nextLines))
	v.scrollbackRowKinds = normalizeStringSlice(nextKinds, len(nextLines))
	v.alignScrollbackMetadataLocked()
	return true
}

func (v *VTerm) screenUpdateTargetSizeLocked(update protocol.ScreenUpdate) (cols, rows int) {
	if v == nil || v.emu == nil {
		return 0, 0
	}
	cols, rows = v.emu.Width(), v.emu.Height()
	if update.Size.Cols > 0 {
		cols = int(update.Size.Cols)
	}
	if update.Size.Rows > 0 {
		rows = int(update.Size.Rows)
	}
	return cols, rows
}

func (v *VTerm) Resize(cols, rows int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.resizeLocked(cols, rows)
}

func (v *VTerm) resizeLocked(cols, rows int) {
	if v == nil || v.emu == nil {
		return
	}
	oldCols, oldRows := v.emu.Width(), v.emu.Height()
	v.captureResizeTailFillLocked(oldCols, oldRows, cols, rows)
	v.ensureScreenFingerprintCacheLocked()
	beforeScreen := append([]rowFingerprint(nil), v.screenFingerprintCache...)
	beforeScrollbackLen := v.scrollbackRowCountLocked()
	beforeScreenTimestamps := cloneTimeSlice(v.screenTimestamps)
	beforeScreenRowKinds := cloneStringSlice(v.screenRowKinds)
	v.emu.Resize(cols, rows)
	pos := v.emu.CursorPosition()
	v.cursor.Row = pos.Y
	v.cursor.Col = pos.X
	afterScreen := v.screenRowFingerprintsLocked()
	v.screenFingerprintCache = afterScreen
	v.reconcileRowMetadataLocked(beforeScreen, beforeScreenTimestamps, beforeScreenRowKinds, beforeScrollbackLen, afterScreen, time.Now().UTC())
	v.invalidateRowCachesLocked()
}

func (v *VTerm) CellAt(x, y int) Cell {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.convertCell(v.emu.CellAt(x, y))
}

func (v *VTerm) ScreenContent() ScreenData {
	v.mu.RLock()
	defer v.mu.RUnlock()
	height := v.emu.Height()
	rows := make([][]Cell, height)
	for y := 0; y < height; y++ {
		rows[y] = cloneCellSlice(v.screenRowViewLocked(y))
	}
	return ScreenData{
		Cells:             rows,
		IsAlternateScreen: v.emu.IsAltScreen(),
	}
}

func (v *VTerm) ScreenRowCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.emu == nil {
		return 0
	}
	return v.emu.Height()
}

func (v *VTerm) ScreenRow(y int) []Cell {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return cloneCellSlice(v.screenRowViewLocked(y))
}

// ScreenRowView returns a read-only view of the current screen row.
// The returned slice is invalidated by the next write, resize, or snapshot load.
func (v *VTerm) ScreenRowView(y int) []Cell {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.screenRowViewLocked(y)
}

func (v *VTerm) Size() (int, int) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.emu.Width(), v.emu.Height()
}

func (v *VTerm) ScrollbackContent() [][]Cell {
	v.mu.RLock()
	defer v.mu.RUnlock()
	rows := v.scrollbackRowsLocked()
	out := make([][]Cell, len(rows))
	for i, row := range rows {
		out[i] = cloneCellSlice(row)
	}
	return out
}

func (v *VTerm) ScrollbackRowCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.emu == nil {
		return 0
	}
	return v.emu.ScrollbackLen()
}

func (v *VTerm) ScrollbackRow(y int) []Cell {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return cloneCellSlice(v.scrollbackRowViewLocked(y))
}

// ScrollbackRowView returns a read-only view of the current scrollback row.
// The returned slice is invalidated by the next write, resize, or snapshot load.
func (v *VTerm) ScrollbackRowView(y int) []Cell {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.scrollbackRowViewLocked(y)
}

func (v *VTerm) scrollbackRowsLocked() [][]Cell {
	count := v.scrollbackRowCountLocked()
	rows := make([][]Cell, 0, count)
	for y := 0; y < count; y++ {
		rows = append(rows, v.scrollbackRowViewLocked(y))
	}
	return rows
}

func (v *VTerm) ScreenTimestamps() []time.Time {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return cloneTimeSlice(v.screenTimestamps)
}

func (v *VTerm) ScrollbackTimestamps() []time.Time {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return cloneTimeSlice(v.scrollbackTimestamps)
}

func (v *VTerm) ScreenRowTimestampAt(y int) time.Time {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return timeAt(v.screenTimestamps, y)
}

func (v *VTerm) ScrollbackRowTimestampAt(y int) time.Time {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return timeAt(v.scrollbackTimestamps, y)
}

func (v *VTerm) ScreenRowKinds() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return cloneStringSlice(v.screenRowKinds)
}

func (v *VTerm) ScrollbackRowKinds() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return cloneStringSlice(v.scrollbackRowKinds)
}

func (v *VTerm) ScreenRowKindAt(y int) string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return stringAt(v.screenRowKinds, y)
}

func (v *VTerm) ScrollbackRowKindAt(y int) string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return stringAt(v.scrollbackRowKinds, y)
}

func (v *VTerm) CursorState() CursorState {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.cursor
}

func (v *VTerm) Modes() TerminalModes {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.modes
}

func (v *VTerm) IsAltScreen() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.emu.IsAltScreen()
}

func (v *VTerm) EncodeReplay(scrollbackLimit int) []byte {
	v.mu.RLock()
	defer v.mu.RUnlock()

	scrollback := v.scrollbackRowsLocked()
	if scrollbackLimit > 0 && len(scrollback) > scrollbackLimit {
		scrollback = scrollback[len(scrollback)-scrollbackLimit:]
	}
	return encodeTerminalReplay(scrollback, v.screenRowsLocked(), v.cursor, v.modes)
}

func (v *VTerm) setMode(mode ansi.Mode, enabled bool) {
	switch mode {
	case ansi.ModeCursorKeys:
		v.modes.ApplicationCursor = enabled
	case modeAlternateScroll:
		v.modes.AlternateScroll = enabled
	case ansi.ModeMouseX10:
		v.mouseMode.x10 = enabled
		v.updateMouseTrackingLocked()
	case ansi.ModeMouseNormal:
		v.mouseMode.normal = enabled
		v.updateMouseTrackingLocked()
	case ansi.ModeMouseHighlight:
		v.mouseMode.highlight = enabled
		v.updateMouseTrackingLocked()
	case ansi.ModeMouseButtonEvent:
		v.mouseMode.buttonEvent = enabled
		v.updateMouseTrackingLocked()
	case ansi.ModeMouseAnyEvent:
		v.mouseMode.anyEvent = enabled
		v.updateMouseTrackingLocked()
	case ansi.ModeMouseExtSgr:
		v.mouseMode.sgr = enabled
		v.updateMouseTrackingLocked()
	case ansi.ModeNumericKeypad:
		// x/vt uses "numeric keypad" mode for keypad application mode.
		// Keep this for future input translation support if needed.
	case ansi.ModeBracketedPaste:
		v.modes.BracketedPaste = enabled
	case ansi.ModeAutoWrap:
		v.modes.AutoWrap = enabled
	}
}

func (v *VTerm) updateMouseTrackingLocked() {
	v.syncMouseModesLocked()
}

func (v *VTerm) syncMouseModesLocked() {
	v.modes.MouseX10 = v.mouseMode.x10
	v.modes.MouseNormal = v.mouseMode.normal
	v.modes.MouseButtonEvent = v.mouseMode.buttonEvent
	v.modes.MouseAnyEvent = v.mouseMode.anyEvent
	v.modes.MouseSGR = v.mouseMode.sgr
	v.modes.MouseTracking = v.mouseMode.x10 ||
		v.mouseMode.normal ||
		v.mouseMode.highlight ||
		v.mouseMode.buttonEvent ||
		v.mouseMode.anyEvent
}

func (v *VTerm) loadMouseModesLocked(modes TerminalModes) {
	v.mouseMode = mouseModeState{
		x10:         modes.MouseX10,
		normal:      modes.MouseNormal,
		buttonEvent: modes.MouseButtonEvent,
		anyEvent:    modes.MouseAnyEvent,
		sgr:         modes.MouseSGR,
	}
	if !v.mouseMode.x10 && !v.mouseMode.normal && !v.mouseMode.buttonEvent && !v.mouseMode.anyEvent && modes.MouseTracking {
		// Older snapshots only persisted the aggregate tracking bit. Preserve the
		// previous compatibility behavior by treating that as button-event mode
		// until explicit protocol fields are available.
		v.mouseMode.buttonEvent = true
	}
	v.syncMouseModesLocked()
}

func (v *VTerm) RenderLines() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	rendered := v.emu.Render()
	if rendered == "" {
		return nil
	}
	return strings.Split(rendered, "\n")
}

func (v *VTerm) SendKey(key uv.KeyEvent) {
	v.emu.SendKey(key)
}

func (v *VTerm) SendText(text string) {
	v.emu.SendText(text)
}

func uvCell(cell Cell) *uv.Cell {
	if cell.Content == "" && cell.Width == 0 {
		// 中文说明：这是宽字符的续位占位符，不是普通空格。恢复快照时必须原样保留，
		// 否则后续增量写入会把已经正确的中日韩宽字符行重新串成“字符 + 一堆空格”。
		return &uv.Cell{}
	}
	c := &uv.Cell{
		Content: cell.Content,
		Width:   cell.Width,
	}
	if c.Content == "" {
		c.Content = " "
	}
	if c.Width <= 0 {
		c.Width = 1
	}
	if cell.Style.FG != "" {
		c.Style.Fg = decodeTerminalColor(cell.Style.FG)
	}
	if cell.Style.BG != "" {
		c.Style.Bg = decodeTerminalColor(cell.Style.BG)
	}
	if cell.Style.Bold {
		c.Style.Attrs |= uv.AttrBold
	}
	if cell.Style.Italic {
		c.Style.Attrs |= uv.AttrItalic
	}
	if cell.Style.Blink {
		c.Style.Attrs |= uv.AttrBlink
	}
	if cell.Style.Reverse {
		c.Style.Attrs |= uv.AttrReverse
	}
	if cell.Style.Strikethrough {
		c.Style.Attrs |= uv.AttrStrikethrough
	}
	if cell.Style.Underline {
		c.Style.Underline = uv.UnderlineSingle
	}
	return c
}

func uvLine(row []Cell) uv.Line {
	line := make(uv.Line, 0, len(row))
	for _, cell := range row {
		line = append(line, *uvCell(cell))
	}
	return line
}

func uvLineFromProtocol(row []protocol.Cell) uv.Line {
	line := make(uv.Line, 0, len(row))
	for _, cell := range row {
		line = append(line, *uvCellFromProtocol(cell))
	}
	return line
}

func uvCellFromProtocol(cell protocol.Cell) *uv.Cell {
	return uvCell(Cell{
		Content: cell.Content,
		Width:   cell.Width,
		Style:   cellStyleFromProtocol(cell.Style),
	})
}

func uvBlankCell() *uv.Cell {
	return &uv.Cell{Content: " ", Width: 1}
}

func encodeScreenSnapshot(rows [][]Cell) []byte {
	var b strings.Builder
	for y, row := range rows {
		for x, cell := range row {
			if cell.Content == "" && cell.Width == 0 {
				// 中文说明：续位列本身不再重复写字符，由前一个宽字符占满即可。
				continue
			}
			content := cell.Content
			if content == "" {
				content = " "
			}
			b.WriteString(fmt.Sprintf("\x1b[%d;%dH", y+1, x+1))
			b.WriteString(cellStyleANSI(cell.Style))
			b.WriteString(content)
		}
	}
	if b.Len() == 0 {
		return nil
	}
	b.WriteString("\x1b[0m")
	return []byte(b.String())
}

func encodeTerminalReplay(scrollback, screen [][]Cell, cursor CursorState, modes TerminalModes) []byte {
	var b strings.Builder

	if !modes.AlternateScreen && len(scrollback) > 0 {
		writeSequentialRows(&b, scrollback)
		b.WriteString("\r\n")
		visibleRows := len(screen)
		if visibleRows < 1 {
			visibleRows = 1
		}
		for i := 0; i < visibleRows-1; i++ {
			b.WriteByte('\n')
		}
		b.WriteString("\x1b[0m")
	}

	if modes.AlternateScreen {
		b.WriteString("\x1b[?1049h")
	}
	b.WriteString("\x1b[H\x1b[2J\x1b[H")
	b.Write(encodeScreenSnapshot(screen))
	writeTerminalModesANSI(&b, modes)
	writeCursorShapeANSI(&b, cursor)
	if cursor.Row >= 0 && cursor.Col >= 0 {
		fmt.Fprintf(&b, "\x1b[%d;%dH", cursor.Row+1, cursor.Col+1)
	}
	if cursor.Visible {
		b.WriteString("\x1b[?25h")
	} else {
		b.WriteString("\x1b[?25l")
	}

	return []byte(b.String())
}

func writeSequentialRows(b *strings.Builder, rows [][]Cell) {
	if b == nil || len(rows) == 0 {
		return
	}
	for i, row := range rows {
		writeSequentialRow(b, row)
		if i < len(rows)-1 {
			b.WriteString("\r\n")
		}
	}
}

func writeSequentialRow(b *strings.Builder, row []Cell) {
	if b == nil {
		return
	}
	last := len(row) - 1
	for last >= 0 {
		cell := row[last]
		if cell.Content == "" || strings.TrimSpace(cell.Content) == "" {
			last--
			continue
		}
		break
	}
	for i := 0; i <= last; i++ {
		cell := row[i]
		if cell.Content == "" && cell.Width == 0 {
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		b.WriteString(cellStyleANSI(cell.Style))
		b.WriteString(content)
	}
	b.WriteString("\x1b[0m")
}

func writeAbsoluteProtocolRowSnapshot(b *strings.Builder, y int, row []protocol.Cell) {
	if b == nil || y < 0 {
		return
	}
	fmt.Fprintf(b, "\x1b[%d;1H\x1b[2K", y+1)
	last := lastSignificantProtocolCell(row)
	if last < 0 {
		return
	}
	currentStyle := protocol.CellStyle{}
	for x, cell := range row[:last+1] {
		if cell.Content == "" && cell.Width == 0 {
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		if x == 0 {
			fmt.Fprintf(b, "\x1b[%d;1H", y+1)
		}
		if cell.Style != currentStyle {
			b.WriteString(cellStyleANSI(cellStyleFromProtocol(cell.Style)))
			currentStyle = cell.Style
		}
		b.WriteString(content)
	}
	b.WriteString("\x1b[0m")
}

func lastSignificantProtocolCell(row []protocol.Cell) int {
	last := len(row) - 1
	for last >= 0 {
		if protocolCellNeedsWrite(row[last]) {
			return last
		}
		last--
	}
	return -1
}

func protocolCellNeedsWrite(cell protocol.Cell) bool {
	if cell.Style != (protocol.CellStyle{}) {
		return true
	}
	if cell.Width > 1 {
		return true
	}
	if cell.Content == "" {
		return false
	}
	return strings.TrimSpace(cell.Content) != ""
}

func writeTerminalModesANSI(b *strings.Builder, modes TerminalModes) {
	if b == nil {
		return
	}
	writePrivateModeANSI(b, 1, modes.ApplicationCursor)
	writePrivateModeANSI(b, 7, modes.AutoWrap)
	writePrivateModeANSI(b, 1007, modes.AlternateScroll)
	writePrivateModeANSI(b, 2004, modes.BracketedPaste)

	mouseX10 := modes.MouseX10
	mouseNormal := modes.MouseNormal
	mouseButton := modes.MouseButtonEvent
	mouseAny := modes.MouseAnyEvent
	if modes.MouseTracking && !mouseX10 && !mouseNormal && !mouseButton && !mouseAny {
		mouseNormal = true
	}
	writePrivateModeANSI(b, 9, mouseX10)
	writePrivateModeANSI(b, 1000, mouseNormal)
	writePrivateModeANSI(b, 1002, mouseButton)
	writePrivateModeANSI(b, 1003, mouseAny)
	writePrivateModeANSI(b, 1005, false)
	writePrivateModeANSI(b, 1006, modes.MouseSGR)
}

func writeCursorShapeANSI(b *strings.Builder, cursor CursorState) {
	if b == nil {
		return
	}
	code := 0
	switch cursor.Shape {
	case CursorUnderline:
		if cursor.Blink {
			code = 3
		} else {
			code = 4
		}
	case CursorBar:
		if cursor.Blink {
			code = 5
		} else {
			code = 6
		}
	case CursorBlock:
		if cursor.Blink {
			code = 1
		} else {
			code = 2
		}
	}
	if code > 0 {
		fmt.Fprintf(b, "\x1b[%d q", code)
	}
}

func writePrivateModeANSI(b *strings.Builder, mode int, enabled bool) {
	if enabled {
		fmt.Fprintf(b, "\x1b[?%dh", mode)
		return
	}
	fmt.Fprintf(b, "\x1b[?%dl", mode)
}

func cellStyleANSI(style CellStyle) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	if style.Bold {
		b.WriteString(";1")
	}
	if style.Italic {
		b.WriteString(";3")
	}
	if style.Underline {
		b.WriteString(";4")
	}
	if style.Blink {
		b.WriteString(";5")
	}
	if style.Reverse {
		b.WriteString(";7")
	}
	if style.Strikethrough {
		b.WriteString(";9")
	}
	writeCellStyleColor(&b, style.FG, true)
	writeCellStyleColor(&b, style.BG, false)
	b.WriteByte('m')
	return b.String()
}

func writeCellStyleColor(b *strings.Builder, value string, foreground bool) {
	if b == nil || strings.TrimSpace(value) == "" {
		return
	}
	switch c := decodeTerminalColor(value).(type) {
	case ansi.BasicColor:
		code := int(c)
		if code < 8 {
			if foreground {
				b.WriteString(fmt.Sprintf(";3%d", code))
			} else {
				b.WriteString(fmt.Sprintf(";4%d", code))
			}
			return
		}
		if foreground {
			b.WriteString(fmt.Sprintf(";9%d", code-8))
		} else {
			b.WriteString(fmt.Sprintf(";10%d", code-8))
		}
	case ansi.IndexedColor:
		if foreground {
			b.WriteString(fmt.Sprintf(";38;5;%d", int(c)))
		} else {
			b.WriteString(fmt.Sprintf(";48;5;%d", int(c)))
		}
	case ansi.RGBColor:
		if foreground {
			b.WriteString(fmt.Sprintf(";38;2;%d;%d;%d", c.R, c.G, c.B))
		} else {
			b.WriteString(fmt.Sprintf(";48;2;%d;%d;%d", c.R, c.G, c.B))
		}
	default:
		if rgb := ansi.XParseColor(value); rgb != nil {
			r, g, bl, _ := rgb.RGBA()
			if foreground {
				b.WriteString(fmt.Sprintf(";38;2;%d;%d;%d", uint8(r>>8), uint8(g>>8), uint8(bl>>8)))
			} else {
				b.WriteString(fmt.Sprintf(";48;2;%d;%d;%d", uint8(r>>8), uint8(g>>8), uint8(bl>>8)))
			}
		}
	}
}

func (v *VTerm) Paste(text string) {
	v.emu.Paste(text)
}

func (v *VTerm) SetTitleHandler(handler TitleHandler) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.onTitle = handler
}

func (v *VTerm) SetDefaultColors(fg, bg string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.defaultFG = normalizeColorString(fg)
	v.defaultBG = normalizeColorString(bg)
	v.applyDefaultColorsToEmulator(v.emu)
}

func (v *VTerm) DefaultColors() (fg, bg string) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.defaultFG, v.defaultBG
}

func (v *VTerm) SetIndexedColor(index int, value string) {
	if index < 0 || index > 255 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	value = normalizeColorString(value)
	if value == "" {
		if v.palette != nil {
			delete(v.palette, index)
		}
	} else {
		if v.palette == nil {
			v.palette = make(map[int]string)
		}
		v.palette[index] = value
	}
	if v.emu != nil {
		v.emu.SetIndexedColor(index, ansi.XParseColor(value))
	}
}

func (v *VTerm) IndexedColor(index int) string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.palette == nil {
		return ""
	}
	return v.palette[index]
}

func (v *VTerm) convertCell(cell *uv.Cell) Cell {
	if cell == nil {
		return Cell{}
	}
	style := CellStyle{}
	if cell.Style.Fg != nil {
		style.FG = v.resolveColorString(cell.Style.Fg)
	}
	if cell.Style.Bg != nil {
		style.BG = v.resolveColorString(cell.Style.Bg)
	}
	style.Bold = cell.Style.Attrs&uv.AttrBold != 0
	style.Italic = cell.Style.Attrs&uv.AttrItalic != 0
	style.Underline = cell.Style.Underline != 0
	style.Blink = cell.Style.Attrs&uv.AttrBlink != 0
	style.Reverse = cell.Style.Attrs&uv.AttrReverse != 0
	style.Strikethrough = cell.Style.Attrs&uv.AttrStrikethrough != 0
	return Cell{
		Content: cell.Content,
		Width:   cell.Width,
		Style:   style,
	}
}

func (v *VTerm) resolveColorString(c color.Color) string {
	if c == nil {
		return ""
	}
	switch value := c.(type) {
	case ansi.BasicColor:
		return encodeBasicColor(value)
	case ansi.IndexedColor:
		return encodeIndexedColor(value)
	}
	return colorToString(c)
}

func encodeBasicColor(c ansi.BasicColor) string {
	return "ansi:" + strconv.Itoa(int(c))
}

func encodeIndexedColor(c ansi.IndexedColor) string {
	return "idx:" + strconv.Itoa(int(c))
}

func decodeTerminalColor(value string) color.Color {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, "ansi:"):
		index, err := strconv.Atoi(strings.TrimPrefix(value, "ansi:"))
		if err == nil && index >= 0 && index <= 15 {
			return ansi.BasicColor(index)
		}
	case strings.HasPrefix(value, "idx:"):
		index, err := strconv.Atoi(strings.TrimPrefix(value, "idx:"))
		if err == nil && index >= 0 && index <= 255 {
			return ansi.IndexedColor(index)
		}
	}
	return ansi.XParseColor(value)
}

func colorToString(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

func cursorStateFromProtocol(cursor protocol.CursorState) CursorState {
	return CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   CursorShape(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func terminalModesFromProtocol(modes protocol.TerminalModes) TerminalModes {
	return TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		AlternateScroll:   modes.AlternateScroll,
		MouseTracking:     modes.MouseTracking,
		MouseX10:          modes.MouseX10,
		MouseNormal:       modes.MouseNormal,
		MouseButtonEvent:  modes.MouseButtonEvent,
		MouseAnyEvent:     modes.MouseAnyEvent,
		MouseSGR:          modes.MouseSGR,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func cellStyleFromProtocol(style protocol.CellStyle) CellStyle {
	return CellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func normalizeColorString(value string) string {
	if rgb := ansi.XParseColor(strings.TrimSpace(value)); rgb != nil {
		return colorToString(rgb)
	}
	return ""
}

func (v *VTerm) applyDefaultColorsToEmulator(emu *charmvt.SafeEmulator) {
	if emu == nil {
		return
	}
	emu.SetDefaultForegroundColor(ansi.XParseColor(v.defaultFG))
	emu.SetDefaultBackgroundColor(ansi.XParseColor(v.defaultBG))
	for index, value := range v.palette {
		emu.SetIndexedColor(index, ansi.XParseColor(value))
	}
}

func (v *VTerm) screenRowsLocked() [][]Cell {
	height := v.screenRowCountLocked()
	rows := make([][]Cell, height)
	for y := 0; y < height; y++ {
		rows[y] = v.screenRowViewLocked(y)
	}
	return rows
}

func (v *VTerm) screenRowCountLocked() int {
	if v.emu == nil {
		return 0
	}
	return v.emu.Height()
}

func (v *VTerm) scrollbackRowCountLocked() int {
	if v.emu == nil {
		return 0
	}
	return v.emu.ScrollbackLen()
}

func (v *VTerm) screenRowViewLocked(y int) []Cell {
	if v.emu == nil || y < 0 || y >= v.emu.Height() {
		return nil
	}
	if cached := v.screenRowCache[y]; cached != nil {
		return cached
	}
	width := v.emu.Width()
	row := make([]Cell, width)
	for x := 0; x < width; x++ {
		row[x] = v.convertCell(v.emu.CellAt(x, y))
	}
	v.applyResizeTailFillLocked(y, row)
	v.applyResizeBottomFillLocked(y, row)
	v.screenRowCache[y] = row
	return row
}

func (v *VTerm) scrollbackRowViewLocked(y int) []Cell {
	if v.emu == nil || y < 0 || y >= v.emu.ScrollbackLen() {
		return nil
	}
	v.ensureScrollbackRowCacheLocked(v.emu.ScrollbackLen())
	if cached := v.scrollbackRowCache[y]; cached != nil {
		return cached
	}
	width := v.emu.Width()
	row := make([]Cell, 0, width)
	for x := 0; x < width; x++ {
		cell := v.emu.ScrollbackCellAt(x, y)
		if cell == nil && x >= len(row) {
			row = append(row, Cell{})
			continue
		}
		row = append(row, v.convertCell(cell))
	}
	v.scrollbackRowCache[y] = row
	return row
}

func (v *VTerm) invalidateRowCachesLocked() {
	if v.emu == nil {
		v.screenRowCache = nil
		v.scrollbackRowCache = nil
		return
	}
	height := maxInt(v.emu.Height(), 0)
	if cap(v.screenRowCache) >= height {
		v.screenRowCache = v.screenRowCache[:height]
		clear(v.screenRowCache)
	} else {
		v.screenRowCache = make([][]Cell, height)
	}
	v.scrollbackRowCache = nil
}

func (v *VTerm) invalidateFingerprintCacheLocked() {
	v.screenFingerprintCache = nil
}

func (v *VTerm) ensureScreenFingerprintCacheLocked() {
	if v == nil || v.emu == nil {
		v.screenFingerprintCache = nil
		return
	}
	if len(v.screenFingerprintCache) == v.emu.Height() {
		return
	}
	v.screenFingerprintCache = v.screenRowFingerprintsLocked()
}

func (v *VTerm) clearTouchedRowsLocked() {
	if v == nil || v.emu == nil {
		return
	}
	touched := v.emu.Touched()
	for row := range touched {
		touched[row] = nil
	}
}

func (v *VTerm) consumeTouchedRowsLocked() ([]int, bool) {
	if v == nil || v.emu == nil {
		return nil, false
	}
	touched := v.emu.Touched()
	if touched == nil {
		return nil, false
	}
	rows := make([]int, 0, len(touched))
	for row, line := range touched {
		if line == nil {
			continue
		}
		if line.FirstCell == -1 && line.LastCell == -1 {
			touched[row] = nil
			continue
		}
		rows = append(rows, row)
		touched[row] = nil
	}
	return rows, true
}

func (v *VTerm) clearResizeTailFillLocked() {
	v.resizeTailStartCol = 0
	v.resizeTailBG = nil
	v.resizeBottomFillBG = ""
	v.resizeBottomFillRow = 0
}

func (v *VTerm) pruneResizeTailFillLocked() {
	if v == nil || v.emu == nil || v.resizeTailStartCol <= 0 || len(v.resizeTailBG) == 0 {
		v.resizeTailStartCol = 0
		v.resizeTailBG = nil
	} else {
		if v.resizeTailStartCol >= v.emu.Width() {
			v.resizeTailStartCol = 0
			v.resizeTailBG = nil
		} else {
			height := minInt(len(v.resizeTailBG), v.emu.Height())
			if height <= 0 {
				v.resizeTailStartCol = 0
				v.resizeTailBG = nil
			} else {
				v.resizeTailBG = v.resizeTailBG[:height]
			}
		}
	}
	if v.resizeBottomFillBG != "" && (v.resizeBottomFillRow < 0 || v.resizeBottomFillRow >= v.emu.Height()) {
		v.resizeBottomFillBG = ""
		v.resizeBottomFillRow = 0
	}
}

func (v *VTerm) captureResizeTailFillLocked(oldCols, oldRows, newCols, newRows int) {
	v.clearResizeTailFillLocked()
	if v.emu == nil || oldCols <= 0 || oldRows <= 0 || !v.emu.IsAltScreen() {
		return
	}
	if newCols > oldCols {
		count := minInt(oldRows, maxInt(newRows, 0))
		if count > 0 {
			bg := make([]string, count)
			hasAny := false
			for y := 0; y < count; y++ {
				fill := v.screenRowTailBackgroundLocked(y, oldCols)
				if fill == "" {
					continue
				}
				bg[y] = fill
				hasAny = true
			}
			if hasAny {
				v.resizeTailStartCol = maxInt(0, oldCols-1)
				v.resizeTailBG = bg
			}
		}
	}
	if newRows > oldRows {
		scanCols := maxInt(1, minInt(oldCols, newCols))
		for scanY := oldRows - 1; scanY >= 0; scanY-- {
			fill := v.screenRowTailBackgroundLocked(scanY, scanCols)
			if fill == "" {
				continue
			}
			v.resizeBottomFillBG = fill
			v.resizeBottomFillRow = oldRows
			break
		}
	}
}

func (v *VTerm) screenRowTailBackgroundLocked(y, width int) string {
	if v == nil || v.emu == nil || y < 0 || y >= v.emu.Height() || width <= 0 {
		return ""
	}
	if width > v.emu.Width() {
		width = v.emu.Width()
	}
	for x := width - 1; x >= 0; x-- {
		cell := v.convertCell(v.emu.CellAt(x, y))
		if cell.Style.BG == "" {
			continue
		}
		return cell.Style.BG
	}
	return ""
}

func (v *VTerm) applyResizeTailFillLocked(y int, row []Cell) {
	if v == nil || y < 0 || y >= len(v.resizeTailBG) || v.resizeTailStartCol <= 0 || v.resizeTailStartCol >= len(row) {
		return
	}
	bg := v.resizeTailBG[y]
	if bg == "" {
		return
	}
	for x := v.resizeTailStartCol; x < len(row); x++ {
		if !cellNeedsResizeFill(row[x]) {
			continue
		}
		row[x].Style.BG = bg
	}
}

func (v *VTerm) applyResizeBottomFillLocked(y int, row []Cell) {
	if v == nil || v.resizeBottomFillBG == "" || y < v.resizeBottomFillRow {
		return
	}
	bg := v.resizeBottomFillBG
	for x := 0; x < len(row); x++ {
		if !cellNeedsResizeFill(row[x]) {
			continue
		}
		row[x].Style.BG = bg
	}
}

func cellNeedsResizeFill(cell Cell) bool {
	if cell.Style.BG != "" {
		return false
	}
	if cell.Width > 1 {
		return false
	}
	return strings.TrimSpace(cell.Content) == ""
}

func (v *VTerm) SeedResizeFillState(tailStartCol int, tailBG []CellStyle, bottomBG string, bottomStartRow int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if tailStartCol > 0 && len(tailBG) > 0 {
		if v.resizeTailStartCol == 0 || tailStartCol < v.resizeTailStartCol {
			v.resizeTailStartCol = tailStartCol
		}
		if len(v.resizeTailBG) < len(tailBG) {
			next := make([]string, len(tailBG))
			copy(next, v.resizeTailBG)
			v.resizeTailBG = next
		}
		for i := range tailBG {
			if tailBG[i].BG == "" {
				continue
			}
			v.resizeTailBG[i] = tailBG[i].BG
		}
	}
	if bottomBG != "" && bottomStartRow >= 0 {
		v.resizeBottomFillBG = bottomBG
		v.resizeBottomFillRow = bottomStartRow
	}
}

func (v *VTerm) ensureScrollbackRowCacheLocked(count int) {
	switch {
	case count <= 0:
		v.scrollbackRowCache = nil
	case cap(v.scrollbackRowCache) >= count:
		prevLen := len(v.scrollbackRowCache)
		v.scrollbackRowCache = v.scrollbackRowCache[:count]
		if count > prevLen {
			clear(v.scrollbackRowCache[prevLen:])
		}
	default:
		v.scrollbackRowCache = make([][]Cell, count)
	}
}

func (v *VTerm) screenRowFingerprintsLocked() []rowFingerprint {
	height := v.screenRowCountLocked()
	rows := make([]rowFingerprint, height)
	for y := 0; y < height; y++ {
		rows[y] = v.screenRowFingerprintLocked(y)
	}
	return rows
}

func (v *VTerm) screenRowFingerprintLocked(y int) rowFingerprint {
	if v.emu == nil || y < 0 || y >= v.emu.Height() {
		return rowFingerprint{}
	}
	return v.rowFingerprintLocked(v.emu.Width(), func(x int) *uv.Cell {
		return v.emu.CellAt(x, y)
	})
}

func (v *VTerm) scrollbackTailRowFingerprintsLocked(count int) []rowFingerprint {
	if count <= 0 {
		return nil
	}
	total := v.scrollbackRowCountLocked()
	if total <= 0 {
		return nil
	}
	start := maxInt(0, total-count)
	rows := make([]rowFingerprint, 0, total-start)
	for y := start; y < total; y++ {
		rows = append(rows, v.scrollbackRowFingerprintLocked(y))
	}
	return rows
}

func (v *VTerm) scrollbackRowFingerprintLocked(y int) rowFingerprint {
	if v.emu == nil || y < 0 || y >= v.emu.ScrollbackLen() {
		return rowFingerprint{}
	}
	return v.rowFingerprintLocked(v.emu.Width(), func(x int) *uv.Cell {
		return v.emu.ScrollbackCellAt(x, y)
	})
}

func (v *VTerm) rowFingerprintLocked(width int, cellAt func(int) *uv.Cell) rowFingerprint {
	fingerprint := rowFingerprint{
		hash:  rowFingerprintOffset64,
		blank: true,
	}
	hashUint64(&fingerprint.hash, uint64(width))
	for x := 0; x < width; x++ {
		if !hashCellFingerprint(&fingerprint.hash, cellAt(x)) {
			fingerprint.blank = false
		}
	}
	return fingerprint
}

func (v *VTerm) writeDamageDirtyRowsLocked(beforeScreen []rowFingerprint, beforeScreenTimestamps []time.Time, beforeScreenRowKinds []string, beforeScrollbackLen int, dirtyRows []int, now time.Time) WriteDamage {
	if v == nil || v.emu == nil {
		return WriteDamage{}
	}
	v.ensureScreenFingerprintCacheLocked()
	for _, row := range dirtyRows {
		if row < 0 || row >= len(v.screenFingerprintCache) {
			continue
		}
		v.screenFingerprintCache[row] = v.screenRowFingerprintLocked(row)
	}
	return v.writeDamageFromFingerprintsLocked(beforeScreen, beforeScreenTimestamps, beforeScreenRowKinds, beforeScrollbackLen, v.screenFingerprintCache, now)
}

func (v *VTerm) writeDamageFullCompareLocked(beforeScreen []rowFingerprint, beforeScreenTimestamps []time.Time, beforeScreenRowKinds []string, beforeScrollbackLen int, now time.Time) WriteDamage {
	afterScreen := v.screenRowFingerprintsLocked()
	v.screenFingerprintCache = afterScreen
	return v.writeDamageFromFingerprintsLocked(beforeScreen, beforeScreenTimestamps, beforeScreenRowKinds, beforeScrollbackLen, afterScreen, now)
}

func (v *VTerm) writeDamageFromFingerprintsLocked(beforeScreen []rowFingerprint, beforeScreenTimestamps []time.Time, beforeScreenRowKinds []string, beforeScrollbackLen int, afterScreen []rowFingerprint, now time.Time) WriteDamage {
	cachePlan := v.reconcileRowMetadataLocked(beforeScreen, beforeScreenTimestamps, beforeScreenRowKinds, beforeScrollbackLen, afterScreen, now)
	v.pruneResizeTailFillLocked()
	v.reconcileRowCachesLocked(beforeScreen, cachePlan)
	return v.writeDamageLocked(beforeScreen, cachePlan)
}

func (v *VTerm) reconcileRowMetadataLocked(beforeScreen []rowFingerprint, beforeScreenTimestamps []time.Time, beforeScreenRowKinds []string, beforeScrollbackLen int, afterScreen []rowFingerprint, now time.Time) rowCacheReconcilePlan {
	if v.emu == nil {
		v.screenTimestamps = nil
		v.scrollbackTimestamps = nil
		v.screenRowKinds = nil
		v.scrollbackRowKinds = nil
		return rowCacheReconcilePlan{}
	}
	scrollShift := detectScreenScrollShift(beforeScreen, afterScreen)
	afterScrollbackLen := v.scrollbackRowCountLocked()
	requiredAppends := scrollShift
	if minAppend := afterScrollbackLen - beforeScrollbackLen; minAppend > requiredAppends {
		requiredAppends = minAppend
	}
	appendedRows := v.scrollbackTailRowFingerprintsLocked(requiredAppends)
	preservedFromBefore := 0
	for preservedFromBefore < len(appendedRows) && preservedFromBefore < len(beforeScreen) && preservedFromBefore < len(beforeScreenTimestamps) {
		if beforeScreenTimestamps[preservedFromBefore].IsZero() && rowFingerprintIsBlank(beforeScreen[preservedFromBefore]) {
			break
		}
		if !rowFingerprintsEqual(beforeScreen[preservedFromBefore], appendedRows[preservedFromBefore]) {
			break
		}
		preservedFromBefore++
	}
	for i := 0; i < preservedFromBefore; i++ {
		ts := beforeScreenTimestamps[i]
		if ts.IsZero() && shouldAssignTimestampToRowFingerprint(beforeScreen[i], i, v.cursor.Row) {
			ts = now
		}
		v.scrollbackTimestamps = append(v.scrollbackTimestamps, ts)
		v.scrollbackRowKinds = append(v.scrollbackRowKinds, stringAt(beforeScreenRowKinds, i))
	}
	for i := preservedFromBefore; i < requiredAppends; i++ {
		v.scrollbackTimestamps = append(v.scrollbackTimestamps, now)
		v.scrollbackRowKinds = append(v.scrollbackRowKinds, "")
	}
	v.alignScrollbackMetadataLocked()
	screenScrollShift := 0
	if preservedFromBefore == 0 && afterScrollbackLen == beforeScrollbackLen {
		screenScrollShift = scrollShift
	}

	nextScreenTimestamps := make([]time.Time, len(afterScreen))
	nextScreenRowKinds := make([]string, len(afterScreen))
	for row := range afterScreen {
		mappedRow := row + preservedFromBefore
		if screenScrollShift > 0 {
			mappedRow = row + screenScrollShift
		}
		if mappedRow < len(beforeScreen) && mappedRow < len(beforeScreenTimestamps) && rowFingerprintsEqual(beforeScreen[mappedRow], afterScreen[row]) {
			nextScreenTimestamps[row] = beforeScreenTimestamps[mappedRow]
			nextScreenRowKinds[row] = stringAt(beforeScreenRowKinds, mappedRow)
		}
		if nextScreenTimestamps[row].IsZero() && shouldAssignTimestampToRowFingerprint(afterScreen[row], row, v.cursor.Row) {
			nextScreenTimestamps[row] = now
		}
	}
	v.screenTimestamps = nextScreenTimestamps
	v.screenRowKinds = nextScreenRowKinds
	return rowCacheReconcilePlan{
		afterScreen:               afterScreen,
		preservedFromBefore:       preservedFromBefore,
		requiredScrollbackAppends: requiredAppends,
		beforeScrollbackLen:       beforeScrollbackLen,
		screenScrollShift:         screenScrollShift,
	}
}

func (v *VTerm) reconcileRowCachesLocked(beforeScreen []rowFingerprint, plan rowCacheReconcilePlan) {
	if v.emu == nil {
		v.screenRowCache = nil
		v.scrollbackRowCache = nil
		return
	}
	if v.hasResizeFillCacheHazardLocked() {
		v.invalidateRowCachesLocked()
		return
	}

	oldScreenCache := v.screenRowCache
	oldScrollbackCache := v.scrollbackRowCache
	nextScreenCache := make([][]Cell, len(plan.afterScreen))
	for row := range plan.afterScreen {
		mappedRow := row + plan.preservedFromBefore
		if plan.screenScrollShift > 0 {
			mappedRow = row + plan.screenScrollShift
		}
		if mappedRow >= len(beforeScreen) || mappedRow >= len(oldScreenCache) {
			continue
		}
		if !rowFingerprintsEqual(beforeScreen[mappedRow], plan.afterScreen[row]) {
			continue
		}
		nextScreenCache[row] = oldScreenCache[mappedRow]
	}
	v.screenRowCache = nextScreenCache

	afterScrollbackLen := v.scrollbackRowCountLocked()
	if afterScrollbackLen <= 0 {
		v.scrollbackRowCache = nil
		return
	}
	nextScrollbackCache := make([][]Cell, afterScrollbackLen)
	rowsDroppedFromFront := maxInt(0, plan.beforeScrollbackLen+plan.requiredScrollbackAppends-afterScrollbackLen)
	retainedFromOldScrollback := plan.beforeScrollbackLen - rowsDroppedFromFront
	if retainedFromOldScrollback < 0 {
		retainedFromOldScrollback = 0
	}
	if rowsDroppedFromFront < len(oldScrollbackCache) && retainedFromOldScrollback > 0 {
		available := len(oldScrollbackCache) - rowsDroppedFromFront
		if retainedFromOldScrollback > available {
			retainedFromOldScrollback = available
		}
		copy(nextScrollbackCache[:retainedFromOldScrollback], oldScrollbackCache[rowsDroppedFromFront:rowsDroppedFromFront+retainedFromOldScrollback])
	} else {
		retainedFromOldScrollback = 0
	}
	for i := 0; i < plan.preservedFromBefore && retainedFromOldScrollback+i < afterScrollbackLen; i++ {
		if i >= len(oldScreenCache) {
			break
		}
		nextScrollbackCache[retainedFromOldScrollback+i] = oldScreenCache[i]
	}
	v.scrollbackRowCache = nextScrollbackCache
}

func (v *VTerm) writeDamageLocked(beforeScreen []rowFingerprint, plan rowCacheReconcilePlan) WriteDamage {
	damage := WriteDamage{
		Cursor:       v.cursor,
		Modes:        v.modes,
		SizeCols:     0,
		SizeRows:     len(plan.afterScreen),
		ScreenScroll: plan.screenScrollShift,
	}
	if v == nil || v.emu == nil {
		return damage
	}
	damage.SizeCols = v.emu.Width()
	afterScrollbackLen := v.scrollbackRowCountLocked()
	rowsDroppedFromFront := maxInt(0, plan.beforeScrollbackLen+plan.requiredScrollbackAppends-afterScrollbackLen)
	retainedFromOldScrollback := plan.beforeScrollbackLen - rowsDroppedFromFront
	if retainedFromOldScrollback < 0 {
		retainedFromOldScrollback = 0
	}
	damage.ScrollbackTrim = rowsDroppedFromFront
	if retainedFromOldScrollback > afterScrollbackLen {
		retainedFromOldScrollback = afterScrollbackLen
	}
	for row := range plan.afterScreen {
		mappedRow := row + plan.preservedFromBefore
		if plan.screenScrollShift > 0 {
			mappedRow = row + plan.screenScrollShift
		}
		if mappedRow < len(beforeScreen) && rowFingerprintsEqual(beforeScreen[mappedRow], plan.afterScreen[row]) {
			continue
		}
		damage.ChangedScreenRows = append(damage.ChangedScreenRows, DamageRow{
			Row:       row,
			Cells:     cloneCellSlice(v.screenRowViewLocked(row)),
			Timestamp: timeAt(v.screenTimestamps, row),
			RowKind:   stringAt(v.screenRowKinds, row),
		})
	}
	for row := retainedFromOldScrollback; row < afterScrollbackLen; row++ {
		damage.ScrollbackAppend = append(damage.ScrollbackAppend, DamageRow{
			Row:       row,
			Cells:     cloneCellSlice(v.scrollbackRowViewLocked(row)),
			Timestamp: timeAt(v.scrollbackTimestamps, row),
			RowKind:   stringAt(v.scrollbackRowKinds, row),
		})
	}
	return damage
}

func (v *VTerm) hasResizeFillCacheHazardLocked() bool {
	if v == nil {
		return false
	}
	return (v.resizeTailStartCol > 0 && len(v.resizeTailBG) > 0) || v.resizeBottomFillBG != ""
}

func detectScreenScrollShift(before, after []rowFingerprint) int {
	limit := minInt(len(before), len(after))
	if limit <= 1 {
		return 0
	}
	bestShift := 0
	bestScore := rowAlignmentScore(before, after, 0)
	for shift := 1; shift < limit; shift++ {
		score := rowAlignmentScore(before, after, shift)
		if score > bestScore {
			bestScore = score
			bestShift = shift
		}
	}
	if bestShift == 0 {
		return 0
	}
	if bestScore <= rowAlignmentScore(before, after, 0) {
		return 0
	}
	return bestShift
}

func rowAlignmentScore(before, after []rowFingerprint, shift int) int {
	score := 0
	for row := 0; row+shift < len(before) && row < len(after); row++ {
		if rowFingerprintsEqual(before[row+shift], after[row]) {
			score++
		}
	}
	return score
}

func rowFingerprintsEqual(left, right rowFingerprint) bool {
	return left.hash == right.hash && left.blank == right.blank
}

func rowFingerprintIsBlank(row rowFingerprint) bool {
	return row.blank
}

func damageChangedRowCount(damage WriteDamage) int {
	return len(damage.ChangedScreenRows) + len(damage.ScrollbackAppend)
}

func damageChangedCellCount(damage WriteDamage) int {
	count := 0
	for _, row := range damage.ChangedScreenRows {
		count += trimmedDamageCellCount(row.Cells)
	}
	for _, row := range damage.ScrollbackAppend {
		count += trimmedDamageCellCount(row.Cells)
	}
	return count
}

func trimmedDamageCellCount(row []Cell) int {
	last := len(row) - 1
	for last >= 0 {
		if vtermCellNeedsWire(row[last]) {
			return last + 1
		}
		last--
	}
	return 0
}

func vtermCellNeedsWire(cell Cell) bool {
	if cell.Style != (CellStyle{}) {
		return true
	}
	if cell.Width > 1 {
		return true
	}
	if cell.Content == "" {
		return false
	}
	return strings.TrimSpace(cell.Content) != ""
}

func hashCellFingerprint(hash *uint64, cell *uv.Cell) bool {
	content := ""
	width := 0
	var fg color.Color
	var bg color.Color
	var attrs uint8
	var underline uv.UnderlineStyle
	if cell != nil {
		content = cell.Content
		width = cell.Width
		fg = cell.Style.Fg
		bg = cell.Style.Bg
		attrs = cell.Style.Attrs
		underline = cell.Style.Underline
	}

	bold := attrs&uv.AttrBold != 0
	italic := attrs&uv.AttrItalic != 0
	underlined := underline != 0
	blink := attrs&uv.AttrBlink != 0
	reverse := attrs&uv.AttrReverse != 0
	strikethrough := attrs&uv.AttrStrikethrough != 0

	hashString(hash, content)
	hashUint64(hash, uint64(width))
	hashBool(hash, bold)
	hashBool(hash, italic)
	hashBool(hash, underlined)
	hashBool(hash, blink)
	hashBool(hash, reverse)
	hashBool(hash, strikethrough)
	hashColorFingerprint(hash, fg)
	hashColorFingerprint(hash, bg)

	return strings.TrimSpace(content) == "" &&
		fg == nil &&
		bg == nil &&
		!bold &&
		!italic &&
		!underlined &&
		!blink &&
		!reverse &&
		!strikethrough
}

func hashColorFingerprint(hash *uint64, value color.Color) {
	if value == nil {
		hashUint64(hash, 0)
		return
	}
	switch colorValue := value.(type) {
	case ansi.BasicColor:
		hashUint64(hash, 1)
		hashUint64(hash, uint64(colorValue))
	case ansi.IndexedColor:
		hashUint64(hash, 2)
		hashUint64(hash, uint64(colorValue))
	default:
		r, g, b, _ := value.RGBA()
		hashUint64(hash, 3)
		hashUint64(hash, uint64(uint8(r>>8)))
		hashUint64(hash, uint64(uint8(g>>8)))
		hashUint64(hash, uint64(uint8(b>>8)))
	}
}

func hashString(hash *uint64, value string) {
	hashUint64(hash, uint64(len(value)))
	for i := 0; i < len(value); i++ {
		*hash ^= uint64(value[i])
		*hash *= rowFingerprintPrime64
	}
}

func hashBool(hash *uint64, value bool) {
	if value {
		hashUint64(hash, 1)
		return
	}
	hashUint64(hash, 0)
}

func hashUint64(hash *uint64, value uint64) {
	*hash ^= value
	*hash *= rowFingerprintPrime64
}

func cloneTimeSlice(values []time.Time) []time.Time {
	if len(values) == 0 {
		return nil
	}
	return append([]time.Time(nil), values...)
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneCellSlice(values []Cell) []Cell {
	if len(values) == 0 {
		return nil
	}
	return append([]Cell(nil), values...)
}

func cloneUVLine(values uv.Line) uv.Line {
	if len(values) == 0 {
		return nil
	}
	return append(uv.Line(nil), values...)
}

func normalizeTimeSlice(values []time.Time, count int) []time.Time {
	if count <= 0 {
		return nil
	}
	out := make([]time.Time, count)
	copy(out, values)
	return out
}

func normalizeStringSlice(values []string, count int) []string {
	if count <= 0 {
		return nil
	}
	out := make([]string, count)
	copy(out, values)
	return out
}

func stringAt(values []string, idx int) string {
	if idx < 0 || idx >= len(values) {
		return ""
	}
	return values[idx]
}

func timeAt(values []time.Time, idx int) time.Time {
	if idx < 0 || idx >= len(values) {
		return time.Time{}
	}
	return values[idx]
}

func tailTimeSlice(values []time.Time, trim int) []time.Time {
	if trim <= 0 {
		return values
	}
	if trim >= len(values) {
		return nil
	}
	return values[trim:]
}

func tailStringSlice(values []string, trim int) []string {
	if trim <= 0 {
		return values
	}
	if trim >= len(values) {
		return nil
	}
	return values[trim:]
}

func shouldAssignTimestampToRowFingerprint(row rowFingerprint, rowIndex, cursorRow int) bool {
	if !rowFingerprintIsBlank(row) {
		return true
	}
	return rowIndex >= 0 && rowIndex <= cursorRow
}

func (v *VTerm) alignScrollbackMetadataLocked() {
	if v.emu == nil {
		v.scrollbackTimestamps = nil
		v.scrollbackRowKinds = nil
		return
	}
	alignLen := v.emu.ScrollbackLen()
	switch {
	case alignLen <= 0:
		v.scrollbackTimestamps = nil
		v.scrollbackRowKinds = nil
	case alignLen < len(v.scrollbackTimestamps):
		v.scrollbackTimestamps = append([]time.Time(nil), v.scrollbackTimestamps[len(v.scrollbackTimestamps)-alignLen:]...)
	case alignLen > len(v.scrollbackTimestamps):
		v.scrollbackTimestamps = append(v.scrollbackTimestamps, make([]time.Time, alignLen-len(v.scrollbackTimestamps))...)
	}
	switch {
	case alignLen <= 0:
		v.scrollbackRowKinds = nil
	case alignLen < len(v.scrollbackRowKinds):
		v.scrollbackRowKinds = append([]string(nil), v.scrollbackRowKinds[len(v.scrollbackRowKinds)-alignLen:]...)
	case alignLen > len(v.scrollbackRowKinds):
		v.scrollbackRowKinds = append(v.scrollbackRowKinds, make([]string, alignLen-len(v.scrollbackRowKinds))...)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizeRenderableUTF8(data []byte) []byte {
	if len(data) == 0 || bytes.IndexByte(data, 0x1b) >= 0 || !utf8.Valid(data) {
		return data
	}

	normalized := norm.NFC.Bytes(data)
	if bytes.Equal(normalized, data) {
		return data
	}
	return normalized
}
