package render

import (
	"strconv"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// ANSIReset 是 FrameSink 写完 styled frame 后必须输出的 SGR reset。
const ANSIReset = "\x1b[0m"

type CursorShape string

const (
	CursorShapeBlock CursorShape = "block"
	CursorShapeBar   CursorShape = "bar"
)

type Cursor struct {
	Visible bool
	Row     int
	Col     int
	Shape   CursorShape
}

type RenderMetadata struct {
	Width  int
	Height int
}

type Cell struct {
	Text      string
	Width     int
	Style     StyleToken
	ANSIStyle ANSICellStyle
	Safe      bool
}

// ANSICellStyle 保留真实 terminal 内容的 SGR 语义；不要映射到 TermX chrome theme token。
type ANSICellStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

func (style ANSICellStyle) IsZero() bool {
	return style == ANSICellStyle{}
}

type StyleToken string

const (
	StyleAccent        StyleToken = "accent"
	StyleMuted         StyleToken = "muted"
	StyleStatus        StyleToken = "status"
	StyleStatusAccent  StyleToken = "status-accent"
	StyleStatusMuted   StyleToken = "status-muted"
	StyleStatusWarning StyleToken = "status-warning"
	StyleInfo          StyleToken = "info"
	StyleSuccess       StyleToken = "success"
	StyleWarning       StyleToken = "warning"
	StyleDanger        StyleToken = "danger"
	StyleOverlay       StyleToken = "overlay"
	StyleToast         StyleToken = "toast"
	StyleToastAccent   StyleToken = "toast-accent"
)

type Line struct {
	Cells []Cell
}

func NewLine(text string) Line {
	return Line{Cells: []Cell{NewCell(text)}}
}

func NewCell(text string) Cell {
	return Cell{
		Text:  SafeLine(text),
		Width: DisplayWidth(text),
		Safe:  true,
	}
}

func (line Line) String() string {
	if len(line.Cells) == 0 {
		return ""
	}
	var out strings.Builder
	for _, cell := range line.Cells {
		out.WriteString(cell.Text)
	}
	return out.String()
}

func (line Line) PlainString() string {
	return xansi.Strip(line.String())
}

func (line Line) ANSIString(theme Theme) string {
	if len(line.Cells) == 0 {
		return ""
	}
	var out strings.Builder
	for _, cell := range line.Cells {
		text := SafeLine(cell.Text)
		styleSeq := ""
		if !cell.ANSIStyle.IsZero() {
			styleSeq = ansiForCellStyle(cell.ANSIStyle)
		} else if cell.Style != "" {
			styleSeq = ansiForStyleToken(cell.Style, theme)
		}
		if styleSeq == "" {
			out.WriteString(text)
			continue
		}
		out.WriteString(styleSeq)
		out.WriteString(text)
		out.WriteString(ANSIReset)
	}
	return out.String()
}

func (line Line) Clone() Line {
	if len(line.Cells) == 0 {
		return Line{}
	}
	cells := make([]Cell, len(line.Cells))
	copy(cells, line.Cells)
	return Line{Cells: cells}
}

func (line Line) Width() int {
	width := 0
	for _, cell := range line.Cells {
		width += maxInt(0, cell.Width)
	}
	return width
}

type LayerKind string

const (
	LayerBase     LayerKind = "base"
	LayerPanel    LayerKind = "panel"
	LayerFloating LayerKind = "floating"
	LayerChrome   LayerKind = "chrome"
	LayerOverlay  LayerKind = "overlay"
	LayerToast    LayerKind = "toast"
)

type Layer struct {
	Kind  LayerKind
	Rect  Rect
	Lines []Line
}

type RenderResult struct {
	Content    []Line
	Cursor     Cursor
	CursorRect Rect
	Blink      bool
	HitRegions []HitRegion
	Metadata   RenderMetadata
	Layers     []Layer
	Theme      Theme
}

func (result RenderResult) Lines() []string {
	if len(result.Content) == 0 {
		return nil
	}
	lines := make([]string, len(result.Content))
	for i, line := range result.Content {
		lines[i] = line.PlainString()
	}
	return lines
}

func (result RenderResult) StyledLines() []Line {
	if len(result.Content) == 0 {
		return nil
	}
	lines := make([]Line, len(result.Content))
	for i, line := range result.Content {
		lines[i] = line.Clone()
	}
	return lines
}

func (result RenderResult) ANSILines() []string {
	if len(result.Content) == 0 {
		return nil
	}
	lines := make([]string, len(result.Content))
	theme := result.Theme.WithFallback()
	for i, line := range result.Content {
		lines[i] = ensureANSIReset(line.ANSIString(theme))
	}
	return lines
}

func (result RenderResult) Frame() Frame {
	return FrameFromRenderResult(result)
}

func ensureANSIReset(value string) string {
	if strings.HasSuffix(value, ANSIReset) || strings.HasSuffix(value, "\x1b[m") {
		return value
	}
	return value + ANSIReset
}

func ansiForStyleToken(token StyleToken, theme Theme) string {
	theme = theme.WithFallback()
	switch token {
	case StyleAccent:
		return sgrForeground(theme.Accent, true)
	case StyleMuted:
		return sgrForeground(theme.Muted, false) + "\x1b[2m"
	case StyleStatus:
		return sgrForegroundBackground(theme.StatusFG, theme.StatusBG, false)
	case StyleStatusAccent:
		return sgrForegroundBackground(theme.Accent, theme.StatusBG, true)
	case StyleStatusMuted:
		return sgrForegroundBackground(theme.Muted, theme.StatusBG, false) + "\x1b[2m"
	case StyleStatusWarning:
		return sgrForegroundBackground(theme.Warning, theme.StatusBG, false)
	case StyleInfo:
		return sgrForeground(theme.Info, false)
	case StyleSuccess:
		return sgrForeground(theme.Success, false)
	case StyleWarning:
		return sgrForeground(theme.Warning, false)
	case StyleDanger:
		return sgrForeground(theme.Danger, false)
	case StyleOverlay:
		return sgrForegroundBackground(theme.ChromeFG, theme.OverlayBG, false)
	case StyleToast:
		return sgrForegroundBackground(theme.ChromeFG, theme.ToastBG, false)
	case StyleToastAccent:
		return sgrForegroundBackground(theme.Accent, theme.ToastBG, true)
	default:
		return "\x1b[1m"
	}
}

func ansiForCellStyle(style ANSICellStyle) string {
	var params []string
	if style.Bold {
		params = append(params, "1")
	}
	if style.Italic {
		params = append(params, "3")
	}
	if style.Underline {
		params = append(params, "4")
	}
	if style.Blink {
		params = append(params, "5")
	}
	if style.Reverse {
		params = append(params, "7")
	}
	if style.Strikethrough {
		params = append(params, "9")
	}
	params = appendANSIColorParams(params, style.FG, true)
	params = appendANSIColorParams(params, style.BG, false)
	if len(params) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(params, ";") + "m"
}

func appendANSIColorParams(params []string, value string, foreground bool) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return params
	}
	if code, ok := ansiPaletteColorCode(value, foreground); ok {
		return append(params, strconv.Itoa(code))
	}
	if index, ok := ansiIndexedColor(value); ok {
		prefix := "38"
		if !foreground {
			prefix = "48"
		}
		return append(params, prefix, "5", strconv.Itoa(index))
	}
	r, g, b, ok := parseHexColor(value)
	if !ok {
		return params
	}
	prefix := "38"
	if !foreground {
		prefix = "48"
	}
	return append(params, prefix, "2", r, g, b)
}

func ansiPaletteColorCode(value string, foreground bool) (int, bool) {
	if !strings.HasPrefix(value, "ansi:") {
		return 0, false
	}
	index, err := strconv.Atoi(strings.TrimPrefix(value, "ansi:"))
	if err != nil || index < 0 || index > 15 {
		return 0, false
	}
	if index < 8 {
		if foreground {
			return 30 + index, true
		}
		return 40 + index, true
	}
	if foreground {
		return 90 + (index - 8), true
	}
	return 100 + (index - 8), true
}

func ansiIndexedColor(value string) (int, bool) {
	if !strings.HasPrefix(value, "idx:") {
		return 0, false
	}
	index, err := strconv.Atoi(strings.TrimPrefix(value, "idx:"))
	if err != nil || index < 0 || index > 255 {
		return 0, false
	}
	return index, true
}

func sgrForeground(hex string, bold bool) string {
	prefix := "\x1b["
	if bold {
		prefix += "1;"
	}
	r, g, b, ok := parseHexColor(hex)
	if !ok {
		if bold {
			return "\x1b[1m"
		}
		return ""
	}
	return prefix + "38;2;" + r + ";" + g + ";" + b + "m"
}

func sgrForegroundBackground(fg string, bg string, bold bool) string {
	fgSeq := sgrForeground(fg, bold)
	r, g, b, ok := parseHexColor(bg)
	if !ok {
		return fgSeq
	}
	return fgSeq + "\x1b[48;2;" + r + ";" + g + ";" + b + "m"
}

func parseHexColor(value string) (string, string, string, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return "", "", "", false
	}
	for _, ch := range value {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return "", "", "", false
		}
	}
	return decimalByte(value[0:2]), decimalByte(value[2:4]), decimalByte(value[4:6]), true
}

func decimalByte(hex string) string {
	value := 0
	for _, ch := range hex {
		value *= 16
		switch {
		case ch >= '0' && ch <= '9':
			value += int(ch - '0')
		case ch >= 'a' && ch <= 'f':
			value += int(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			value += int(ch-'A') + 10
		}
	}
	return strconv.Itoa(value)
}

type ContentKind string

const (
	ContentTerminalLive     ContentKind = "terminal-live"
	ContentCopyHistory      ContentKind = "copy-history"
	ContentEmptyPane        ContentKind = "empty-pane"
	ContentExitedPane       ContentKind = "exited-pane"
	ContentTerminalPicker   ContentKind = "terminal-picker"
	ContentTerminalPool     ContentKind = "terminal-pool"
	ContentWorkbenchTree    ContentKind = "workbench-tree"
	ContentFloatingOverview ContentKind = "floating-overview"
	ContentPrompt           ContentKind = "prompt"
	ContentHelp             ContentKind = "help"
	ContentPlaceholder      ContentKind = "placeholder"
)

type ContentVM struct {
	Kind       ContentKind
	Lines      []Line
	Status     string
	Pending    bool
	Empty      bool
	Error      string
	Cursor     Cursor
	HitRegions []HitRegion
}

type ContentRenderRequest struct {
	Rect    Rect
	Content ContentVM
}

type ContentRenderResult struct {
	Lines      []Line
	Cursor     Cursor
	HitRegions []HitRegion
	Metadata   RenderMetadata
}

type ContentRenderer interface {
	RenderContent(ContentRenderRequest) ContentRenderResult
}

type PanelPresentation string

const (
	PanelPresentationCard      PanelPresentation = "card"
	PanelPresentationSplitLine PanelPresentation = "split-line"
)

type PanelVM struct {
	ID           string
	Title        string
	Rect         Rect
	Presentation PanelPresentation
	Active       bool
	Content      ContentVM
}

type LayoutVM struct {
	Viewport Rect
	Body     Rect
	Panels   []PanelVM
	Floating []FloatingVM
	Split    SplitVM
}

type SplitDirection string

const (
	SplitHorizontal SplitDirection = "horizontal"
	SplitVertical   SplitDirection = "vertical"
)

type SplitVM struct {
	PaneID      string
	Direction   SplitDirection
	Children    []SplitVM
	Ratio       float64
	BiasCells   int
	FixedPaneID string
	FixedCols   int
	FixedRows   int
}

type FloatingVM struct {
	ID        string
	Title     string
	Rect      Rect
	Z         int
	Active    bool
	Collapsed bool
	Content   ContentVM
}

type HeaderVM struct {
	Visible         bool
	Workspace       string
	Tab             string
	ActivePane      string
	TerminalSummary string
	FloatingSummary string
	Notice          string
	Title           string
}

type FooterVM struct {
	Visible       bool
	Mode          string
	Hint          string
	Actions       []string
	ActiveTarget  string
	GlobalSummary string
}

type ToastSeverity string

const (
	ToastInfo    ToastSeverity = "info"
	ToastSuccess ToastSeverity = "success"
	ToastWarning ToastSeverity = "warning"
	ToastError   ToastSeverity = "error"
)

type ToastVM struct {
	ID       string
	Severity ToastSeverity
	Title    string
	Body     string
	Pending  bool
}

type OverlayKind string

const (
	OverlayNone           OverlayKind = ""
	OverlayTerminalPicker OverlayKind = "terminal-picker"
	OverlayTerminalPool   OverlayKind = "terminal-pool"
	OverlayWorkbenchTree  OverlayKind = "workbench-tree"
	OverlayPrompt         OverlayKind = "prompt"
	OverlayHelp           OverlayKind = "help"
)

type OverlayVM struct {
	Kind    OverlayKind
	Opaque  bool
	Content ContentVM
}

type ShellVM struct {
	Header  HeaderVM
	Footer  FooterVM
	Layout  LayoutVM
	Overlay OverlayVM
	Toasts  []ToastVM
	Cursor  Cursor
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
