package live

import (
	"strings"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// SurfaceSize describes the current host projection size.
type SurfaceSize struct {
	Cols int
	Rows int
}

// Valid reports whether the size can be used for a live surface projection.
func (s SurfaceSize) Valid() bool {
	return s.Cols > 0 && s.Rows > 0
}

type SurfaceTrack struct {
	size    SurfaceSize
	vt      *vterm.VTerm
	pending string
}

// SurfaceSnapshot 是真实 live terminal 的 size-bound cell matrix，不是 history truth。
type SurfaceSnapshot struct {
	Size   SurfaceSize
	Screen vterm.ScreenData
	Cursor vterm.CursorState
	Modes  vterm.TerminalModes
}

func NewSurfaceTrack(size SurfaceSize) *SurfaceTrack {
	if !size.Valid() {
		size = SurfaceSize{Cols: 80, Rows: 24}
	}
	return &SurfaceTrack{size: size, vt: vterm.New(size.Cols, size.Rows, 0, nil)}
}

func (surface *SurfaceTrack) Size() SurfaceSize {
	return surface.size
}

func (surface *SurfaceTrack) Resize(size SurfaceSize) {
	if !size.Valid() {
		return
	}
	surface.size = size
	surface.ensureVTerm()
	surface.vt.ResizeWithDamage(size.Cols, size.Rows)
}

func (surface *SurfaceTrack) Write(text string) {
	if text == "" && surface.pending == "" {
		return
	}
	surface.ensureVTerm()
	text = surface.pending + text
	surface.pending = ""
	for text != "" {
		idx := strings.Index(text, "\x1b[?")
		if idx < 0 {
			surface.writeRaw(text)
			return
		}
		if idx > 0 {
			surface.writeRaw(text[:idx])
			text = text[idx:]
			continue
		}
		consumed, action, filtered, complete := consumePrivateModeCSI(text)
		if !complete {
			surface.pending = text
			return
		}
		if consumed <= 0 {
			surface.writeRaw(text[:1])
			text = text[1:]
			continue
		}
		if action == privateModeAltExit && surface.vt.IsAltScreen() {
			surface.preserveAltScreenFrameAsPrimary()
			if filtered != "" {
				surface.writeRaw(filtered)
			}
			text = text[consumed:]
			continue
		}
		surface.writeRaw(text[:consumed])
		text = text[consumed:]
	}
}

func (surface *SurfaceTrack) writeRaw(text string) {
	if text == "" {
		return
	}
	// 中文说明：core-v2 live surface 只暴露当前 screen snapshot，不消费增量 damage。
	// 所以这里始终走 latest-frame 写入，避免压力输出为每个 PTY 小块构造细粒度 damage。
	_, _, _ = surface.vt.WriteForLatestFrame([]byte(text))
}

func (surface *SurfaceTrack) preserveAltScreenFrameAsPrimary() {
	snapshot := surface.Snapshot()
	snapshot.Modes.AlternateScreen = false
	snapshot.Screen.IsAlternateScreen = false
	surface.vt.LoadSnapshot(snapshot.Screen, snapshot.Cursor, snapshot.Modes)
}

func (surface *SurfaceTrack) Rows() []string {
	snapshot := surface.Snapshot()
	if len(snapshot.Screen.Cells) == 0 {
		return nil
	}
	out := make([]string, len(snapshot.Screen.Cells))
	for rowIndex, row := range snapshot.Screen.Cells {
		out[rowIndex] = strings.TrimRight(vtermRowText(row), " ")
	}
	return trimTrailingEmptyRows(out)
}

func (surface *SurfaceTrack) Snapshot() SurfaceSnapshot {
	surface.ensureVTerm()
	return SurfaceSnapshot{
		Size:   surface.size,
		Screen: surface.vt.ScreenContent(),
		Cursor: surface.vt.CursorState(),
		Modes:  surface.vt.Modes(),
	}
}

func (surface *SurfaceTrack) ensureVTerm() {
	if surface.vt != nil {
		return
	}
	if !surface.size.Valid() {
		surface.size = SurfaceSize{Cols: 80, Rows: 24}
	}
	surface.vt = vterm.New(surface.size.Cols, surface.size.Rows, 0, nil)
}

type privateModeAltAction int

const (
	privateModeNoAlt privateModeAltAction = iota
	privateModeAltEnter
	privateModeAltExit
)

func consumePrivateModeCSI(input string) (int, privateModeAltAction, string, bool) {
	if !strings.HasPrefix(input, "\x1b[?") {
		return 0, privateModeNoAlt, "", true
	}
	end := -1
	for i := 3; i < len(input); i++ {
		b := input[i]
		if b >= 0x40 && b <= 0x7e {
			end = i
			break
		}
	}
	if end < 0 {
		return 0, privateModeNoAlt, "", false
	}
	final := input[end]
	sequence := input[:end+1]
	if final != 'h' && final != 'l' {
		return end + 1, privateModeNoAlt, sequence, true
	}
	params := strings.FieldsFunc(input[3:end], func(r rune) bool {
		return r == ';' || r == ':'
	})
	hasAlt := false
	kept := make([]string, 0, len(params))
	for _, param := range params {
		if param == "" {
			continue
		}
		if isAltScreenPrivateMode(param) {
			hasAlt = true
			continue
		}
		kept = append(kept, param)
	}
	if !hasAlt {
		return end + 1, privateModeNoAlt, sequence, true
	}
	if final == 'h' {
		return end + 1, privateModeAltEnter, sequence, true
	}
	filtered := ""
	if len(kept) > 0 {
		filtered = "\x1b[?" + strings.Join(kept, ";") + string(final)
	}
	return end + 1, privateModeAltExit, filtered, true
}

func isAltScreenPrivateMode(param string) bool {
	switch param {
	case "47", "1047", "1049":
		return true
	default:
		return false
	}
}

func vtermRowText(row []vterm.Cell) string {
	var out strings.Builder
	for _, cell := range row {
		out.WriteString(cell.Content)
	}
	return out.String()
}

func trimTrailingEmptyRows(rows []string) []string {
	last := len(rows) - 1
	for last >= 0 && rows[last] == "" {
		last--
	}
	if last < 0 {
		return []string{""}
	}
	out := make([]string, last+1)
	copy(out, rows[:last+1])
	return out
}
