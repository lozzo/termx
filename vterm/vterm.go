package vterm

import (
	"bytes"
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	charmvt "github.com/charmbracelet/x/vt"
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

	done chan struct{} // closed when drain goroutine exits
}

type mouseModeState struct {
	x10         bool
	normal      bool
	highlight   bool
	buttonEvent bool
	anyEvent    bool
}

const modeAlternateScroll ansi.DECMode = 1007

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
	v.mu.Lock()
	defer v.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			n = 0
			err = fmt.Errorf("vterm write panic: %v", r)
		}
	}()
	normalized := normalizeRenderableUTF8(data)
	n, err = safeEmulatorWrite(v.emu, normalized)
	pos := v.emu.CursorPosition()
	v.cursor.Row = pos.Y
	v.cursor.Col = pos.X
	v.modes.AlternateScreen = v.emu.IsAltScreen()
	return n, err
}

func (v *VTerm) LoadSnapshot(screen ScreenData, cursor CursorState, modes TerminalModes) {
	v.LoadSnapshotWithScrollback(nil, screen, cursor, modes)
}

func (v *VTerm) LoadSnapshotWithScrollback(scrollback [][]Cell, screen ScreenData, cursor CursorState, modes TerminalModes) {
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
	v.setMouseTrackingAggregateLocked(modes.MouseTracking)
	if len(scrollback) > 0 {
		sb := v.emu.Emulator.Scrollback()
		for _, row := range scrollback {
			sb.Push(uvLine(row))
		}
	}
	if modes.AlternateScreen {
		_, _ = v.emu.Write([]byte("\x1b[?1049h"))
	}
	for y, row := range screen.Cells {
		for x, cell := range row {
			v.emu.SetCell(x, y, uvCell(cell))
		}
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
}

func (v *VTerm) Resize(cols, rows int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.emu.Resize(cols, rows)
	pos := v.emu.CursorPosition()
	v.cursor.Row = pos.Y
	v.cursor.Col = pos.X
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
	width := v.emu.Width()
	rows := make([][]Cell, height)
	for y := 0; y < height; y++ {
		row := make([]Cell, width)
		for x := 0; x < width; x++ {
			row[x] = v.convertCell(v.emu.CellAt(x, y))
		}
		rows[y] = row
	}
	return ScreenData{
		Cells:             rows,
		IsAlternateScreen: v.emu.IsAltScreen(),
	}
}

func (v *VTerm) Size() (int, int) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.emu.Width(), v.emu.Height()
}

func (v *VTerm) ScrollbackContent() [][]Cell {
	v.mu.RLock()
	defer v.mu.RUnlock()
	count := v.emu.ScrollbackLen()
	width := v.emu.Width()
	rows := make([][]Cell, 0, count)
	for y := 0; y < count; y++ {
		row := make([]Cell, 0, width)
		for x := 0; x < width; x++ {
			cell := v.emu.ScrollbackCellAt(x, y)
			if cell == nil && x >= len(row) {
				row = append(row, Cell{})
				continue
			}
			row = append(row, v.convertCell(cell))
		}
		rows = append(rows, row)
	}
	return rows
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
		// SGR 1006 only changes encoding. It should not on its own imply that
		// applications are actively asking for mouse events.
	case ansi.ModeNumericKeypad:
		// x/vt uses "numeric keypad" mode for keypad application mode.
		// Keep this for future input translation support if needed.
	case ansi.ModeBracketedPaste:
		v.modes.BracketedPaste = enabled
	case ansi.ModeAutoWrap:
		v.modes.AutoWrap = enabled
	}
}

func (v *VTerm) setMouseTrackingAggregateLocked(enabled bool) {
	v.mouseMode = mouseModeState{buttonEvent: enabled}
	v.modes.MouseTracking = enabled
}

func (v *VTerm) updateMouseTrackingLocked() {
	v.modes.MouseTracking = v.mouseMode.x10 ||
		v.mouseMode.normal ||
		v.mouseMode.highlight ||
		v.mouseMode.buttonEvent ||
		v.mouseMode.anyEvent
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
	emu.Emulator.SetDefaultForegroundColor(ansi.XParseColor(v.defaultFG))
	emu.Emulator.SetDefaultBackgroundColor(ansi.XParseColor(v.defaultBG))
	for index, value := range v.palette {
		emu.SetIndexedColor(index, ansi.XParseColor(value))
	}
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
