package live

import (
	"os"
	"strings"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

const preserveAltScreenOnExitEnv = "TERMX_PRESERVE_ALT_SCREEN_ON_EXIT"

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
	size                         SurfaceSize
	vt                           *vterm.VTerm
	onResponse                   vterm.ResponseHandler
	pending                      string
	preserveAltScreenFrameOnExit bool
}

type SurfaceTrackOptions struct {
	PreserveAltScreenFrameOnExit bool
	OnResponse                   vterm.ResponseHandler
}

type SurfaceWriteResult struct {
	Segments []SurfaceWriteSegment
}

type SurfaceWriteSegment struct {
	Raw                string
	Damages            []vterm.WriteDamage
	AltScreenExitFrame [][]vterm.Cell
}

// SurfaceSnapshot 是真实 live terminal 的 size-bound cell matrix，不是 history truth。
type SurfaceSnapshot struct {
	Size   SurfaceSize
	Screen vterm.ScreenData
	Cursor vterm.CursorState
	Modes  vterm.TerminalModes
}

func DefaultSurfaceTrackOptions() SurfaceTrackOptions {
	return SurfaceTrackOptions{
		PreserveAltScreenFrameOnExit: boolEnvDefault(preserveAltScreenOnExitEnv, true),
	}
}

func NewSurfaceTrack(size SurfaceSize) *SurfaceTrack {
	return NewSurfaceTrackWithOptions(size, DefaultSurfaceTrackOptions())
}

func NewSurfaceTrackWithOptions(size SurfaceSize, options SurfaceTrackOptions) *SurfaceTrack {
	if !size.Valid() {
		size = SurfaceSize{Cols: 80, Rows: 24}
	}
	return &SurfaceTrack{
		size:                         size,
		vt:                           vterm.New(size.Cols, size.Rows, 0, options.OnResponse),
		onResponse:                   options.OnResponse,
		preserveAltScreenFrameOnExit: options.PreserveAltScreenFrameOnExit,
	}
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

func (surface *SurfaceTrack) ResetForRestartPreservingScreen() {
	surface.ensureVTerm()
	snapshot := surface.Snapshot()
	rows := cloneVTermCellRows(snapshot.Screen.Cells)
	size := surface.size
	rows, cursor := restartPreservedScreenRows(rows, size.Rows)
	// 中文说明：重启的是外部进程，不是 terminal identity。保留可见 tail，
	// 但用全新 VTerm 丢弃旧程序的 mouse/bracketed paste/alt-screen/pending escape 状态。
	_ = surface.vt.Close()
	surface.vt = vterm.New(size.Cols, size.Rows, 0, surface.onResponse)
	surface.pending = ""
	if len(rows) == 0 {
		return
	}
	surface.vt.LoadSizedSnapshotWithExtendedMetadata(
		size.Cols,
		size.Rows,
		nil,
		nil,
		nil,
		nil,
		vterm.ScreenData{Cells: rows},
		nil,
		nil,
		nil,
		cursor,
		vterm.TerminalModes{AutoWrap: true},
	)
}

func (surface *SurfaceTrack) Write(text string) {
	_ = surface.WriteWithResult(text)
}

func (surface *SurfaceTrack) WriteWithResult(text string) SurfaceWriteResult {
	var result SurfaceWriteResult
	var raw strings.Builder
	var damages []vterm.WriteDamage
	if text == "" && surface.pending == "" {
		return result
	}
	surface.ensureVTerm()
	text = surface.pending + text
	surface.pending = ""
	for text != "" {
		idx := strings.Index(text, "\x1b[?")
		if idx < 0 {
			damages = appendSurfaceWriteDamage(damages, surface.writeRaw(text))
			raw.WriteString(text)
			appendSurfaceWriteRawSegment(&result, &raw, &damages)
			return result
		}
		if idx > 0 {
			damages = appendSurfaceWriteDamage(damages, surface.writeRaw(text[:idx]))
			raw.WriteString(text[:idx])
			text = text[idx:]
			continue
		}
		consumed, action, _, complete := consumePrivateModeCSI(text)
		if !complete {
			surface.pending = text
			appendSurfaceWriteRawSegment(&result, &raw, &damages)
			return result
		}
		if consumed <= 0 {
			damages = appendSurfaceWriteDamage(damages, surface.writeRaw(text[:1]))
			raw.WriteString(text[:1])
			text = text[1:]
			continue
		}
		if action == privateModeAltExit && surface.vt.IsAltScreen() {
			var altFrame [][]vterm.Cell
			if surface.preserveAltScreenFrameOnExit {
				altFrame = surface.altScreenFrameCells()
			}
			damages = appendSurfaceWriteDamage(damages, surface.writeRaw(text[:consumed]))
			raw.WriteString(text[:consumed])
			if surface.preserveAltScreenFrameOnExit {
				surface.appendAltScreenFrameCells(altFrame)
				if len(altFrame) > 0 {
					result.Segments = append(result.Segments, SurfaceWriteSegment{
						Raw:                raw.String(),
						Damages:            cloneSurfaceWriteDamages(damages),
						AltScreenExitFrame: cloneVTermCellRows(altFrame),
					})
					raw.Reset()
					damages = nil
				}
			}
			text = text[consumed:]
			continue
		}
		damages = appendSurfaceWriteDamage(damages, surface.writeRaw(text[:consumed]))
		raw.WriteString(text[:consumed])
		text = text[consumed:]
	}
	if raw.Len() > 0 {
		appendSurfaceWriteRawSegment(&result, &raw, &damages)
	}
	return result
}

func appendSurfaceWriteRawSegment(result *SurfaceWriteResult, raw *strings.Builder, damages *[]vterm.WriteDamage) {
	if result == nil || raw == nil || raw.Len() == 0 {
		return
	}
	result.Segments = append(result.Segments, SurfaceWriteSegment{
		Raw:     raw.String(),
		Damages: cloneSurfaceWriteDamages(*damages),
	})
	raw.Reset()
	*damages = nil
}

func appendSurfaceWriteDamage(damages []vterm.WriteDamage, damage vterm.WriteDamage) []vterm.WriteDamage {
	if damage.SizeCols == 0 && damage.SizeRows == 0 && len(damage.Ops) == 0 && len(damage.ScrollbackAppend) == 0 && len(damage.AlternateAppend) == 0 && !damage.RequiresFullReplace {
		return damages
	}
	return append(damages, damage)
}

func cloneSurfaceWriteDamages(in []vterm.WriteDamage) []vterm.WriteDamage {
	if len(in) == 0 {
		return nil
	}
	out := make([]vterm.WriteDamage, len(in))
	copy(out, in)
	return out
}

func (surface *SurfaceTrack) writeRaw(text string) vterm.WriteDamage {
	if text == "" {
		return vterm.WriteDamage{}
	}
	// 中文说明：同一批 PTY bytes 只在 core 持有的 vterm 中解码一次；返回的
	// damage 是 EventRouter 的语义输入，不是 history truth 或 live snapshot。
	_, _, damage := surface.vt.WriteWithDamage([]byte(text))
	return damage
}

func (surface *SurfaceTrack) altScreenFrameCells() [][]vterm.Cell {
	snapshot := surface.Snapshot()
	rows := make([][]vterm.Cell, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		cloned := make([]vterm.Cell, len(row))
		copy(cloned, row)
		rows = append(rows, trimTrailingDefaultBlankCells(cloned))
	}
	for len(rows) > 0 && !rowHasVisibleFootprint(rows[0]) {
		rows = rows[1:]
	}
	for len(rows) > 0 && !rowHasVisibleFootprint(rows[len(rows)-1]) {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func (surface *SurfaceTrack) appendAltScreenFrameCells(rows [][]vterm.Cell) {
	if len(rows) == 0 {
		return
	}
	var builder strings.Builder
	builder.WriteString("\r\n")
	// 中文说明：alt-screen 退出时只把最后一帧追加到 live surface，
	// 这里用 cell replay 保留 SGR、带背景空白和列布局，但不回写 history parser。
	builder.Write(vterm.EncodeHistoryRowsReplay(rows))
	surface.writeRaw(builder.String())
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
		Size: surface.size,
		// 中文说明：live snapshot 是协议/渲染高频路径，保留行数和 styled footprint，
		// 但不克隆每行尾部的纯默认空白，避免压力输出反复搬运整屏空白。
		Screen: surface.vt.TrimmedScreenContent(),
		Cursor: surface.vt.CursorState(),
		Modes:  surface.vt.Modes(),
	}
}

func (surface *SurfaceTrack) VisitTrimmedScreenRows(visit func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell)) vterm.TrimmedScreenRowsInfo {
	surface.ensureVTerm()
	return surface.vt.VisitTrimmedScreenRows(visit)
}

func (surface *SurfaceTrack) ensureVTerm() {
	if surface.vt != nil {
		return
	}
	if !surface.size.Valid() {
		surface.size = SurfaceSize{Cols: 80, Rows: 24}
	}
	surface.vt = vterm.New(surface.size.Cols, surface.size.Rows, 0, surface.onResponse)
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

func rowHasVisibleFootprint(row []vterm.Cell) bool {
	for _, cell := range row {
		if cellHasVisibleFootprint(cell) {
			return true
		}
	}
	return false
}

func cellHasVisibleFootprint(cell vterm.Cell) bool {
	if cell.Content != "" && strings.Trim(cell.Content, " ") != "" {
		return true
	}
	if cell.Style != (vterm.CellStyle{}) || cell.LinkURL != "" || cell.LinkParams != "" {
		return true
	}
	return cell.Width > 1
}

func trimTrailingDefaultBlankCells(row []vterm.Cell) []vterm.Cell {
	last := len(row) - 1
	for last >= 0 && !cellHasVisibleFootprint(row[last]) {
		last--
	}
	return row[:last+1]
}

func cloneVTermCellRows(rows [][]vterm.Cell) [][]vterm.Cell {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([][]vterm.Cell, len(rows))
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		cloned[i] = make([]vterm.Cell, len(row))
		copy(cloned[i], row)
	}
	return cloned
}

func restartPreservedScreenRows(rows [][]vterm.Cell, maxRows int) ([][]vterm.Cell, vterm.CursorState) {
	last := -1
	for i, row := range rows {
		if rowHasVisibleFootprint(row) {
			last = i
		}
	}
	if last < 0 {
		return nil, vterm.CursorState{Visible: false}
	}
	rows = rows[:last+1]
	if maxRows > 0 && len(rows) >= maxRows {
		rows = rows[len(rows)-maxRows+1:]
	}
	// 中文说明：保留旧 tail 后，新进程从下一空行继续写；这里的 cursor 是真实
	// surface 坐标种子，不能隐藏，否则新 shell 不显式 show cursor 时会一直不可见。
	return rows, vterm.CursorState{Row: len(rows), Col: 0, Visible: true}
}

func boolEnvDefault(name string, fallback bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
