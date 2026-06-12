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
	size SurfaceSize
	vt   *vterm.VTerm
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
	if text == "" {
		return
	}
	surface.ensureVTerm()
	// 中文说明：core-v2 live surface 只暴露当前 screen snapshot，不消费增量 damage。
	// 所以这里始终走 latest-frame 写入，避免压力输出为每个 PTY 小块构造细粒度 damage。
	_, _, _ = surface.vt.WriteForLatestFrame([]byte(text))
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
