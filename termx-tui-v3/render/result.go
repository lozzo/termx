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
	Anchor  bool
	Row     int
	Col     int
	Shape   CursorShape
}

type RenderMetadata struct {
	Width  int
	Height int
}

type Cell struct {
	Text       string
	Width      int
	Style      StyleToken
	ANSIStyle  ANSICellStyle
	LinkURL    string
	LinkParams string
	// TerminalContent 标记来自 core-v2 protocol/live/history 的真实 terminal cell。
	TerminalContent bool
	Safe            bool
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
	StyleAccent              StyleToken = "accent"
	StyleForeground          StyleToken = "foreground"
	StyleStrongForeground    StyleToken = "strong-foreground"
	StyleMuted               StyleToken = "muted"
	StyleStatus              StyleToken = "status"
	StyleStatusAccent        StyleToken = "status-accent"
	StyleStatusMuted         StyleToken = "status-muted"
	StyleStatusWarning       StyleToken = "status-warning"
	StyleHeaderWorkspace     StyleToken = "header-workspace"
	StyleHeaderSpacer        StyleToken = "header-spacer"
	StyleHeaderInactiveIndex StyleToken = "header-inactive-index"
	StyleHeaderInactiveTitle StyleToken = "header-inactive-title"
	StyleHeaderInactiveClose StyleToken = "header-inactive-close"
	StyleHeaderActiveMarker  StyleToken = "header-active-marker"
	StyleHeaderActiveIndex   StyleToken = "header-active-index"
	StyleHeaderActiveTitle   StyleToken = "header-active-title"
	StyleHeaderActiveClose   StyleToken = "header-active-close"
	StyleHeaderCreate        StyleToken = "header-create"
	StyleFooterChrome        StyleToken = "footer-chrome"
	StyleFooterMuted         StyleToken = "footer-muted"
	StyleFooterAccent        StyleToken = "footer-accent"
	StyleFooterKeyPane       StyleToken = "footer-key-pane"
	StyleFooterKeyResize     StyleToken = "footer-key-resize"
	StyleFooterKeyTab        StyleToken = "footer-key-tab"
	StyleFooterKeyWorkspace  StyleToken = "footer-key-workspace"
	StyleFooterKeyFloat      StyleToken = "footer-key-float"
	StyleFooterKeyCopy       StyleToken = "footer-key-copy"
	StyleFooterKeyPicker     StyleToken = "footer-key-picker"
	StyleFooterKeyGlobal     StyleToken = "footer-key-global"
	StyleInfo                StyleToken = "info"
	StyleSuccess             StyleToken = "success"
	StyleWarning             StyleToken = "warning"
	StyleDanger              StyleToken = "danger"
	StyleDangerStrong        StyleToken = "danger-strong"
	StyleOverlay             StyleToken = "overlay"
	StylePicker              StyleToken = "picker"
	StylePickerMuted         StyleToken = "picker-muted"
	StylePickerAccent        StyleToken = "picker-accent"
	StylePickerInfo          StyleToken = "picker-info"
	StylePickerSuccess       StyleToken = "picker-success"
	StylePickerMatch         StyleToken = "picker-match"
	StylePromptSuggestion    StyleToken = "prompt-suggestion"
	StylePromptSuggestionHit StyleToken = "prompt-suggestion-hit"
	StyleToast               StyleToken = "toast"
	StyleToastAccent         StyleToken = "toast-accent"
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
	return line.ansiString(theme, 1)
}

func (line Line) ansiString(theme Theme, baseColumn int) string {
	if len(line.Cells) == 0 {
		return ""
	}
	var out strings.Builder
	if baseColumn < 1 {
		baseColumn = 1
	}
	modelCol := baseColumn
	for index, cell := range line.Cells {
		styleSeq := ""
		if !cell.ANSIStyle.IsZero() {
			styleSeq = ansiForCellStyle(cell.ANSIStyle)
		} else if cell.Style != "" {
			styleSeq = ansiForStyleToken(cell.Style, theme)
		}
		linkOpen, linkClose := ansiLinkOpenClose(cell.LinkURL, cell.LinkParams)
		if linkOpen != "" {
			out.WriteString(linkOpen)
		}
		if styleSeq != "" {
			out.WriteString(styleSeq)
		}
		writeANSIText(&out, cell, modelCol)
		modelCol += maxInt(0, cell.Width)
		if styleSeq != "" {
			out.WriteString(ANSIReset)
		}
		if linkClose != "" {
			out.WriteString(linkClose)
		}
		if index < len(line.Cells)-1 {
			// 中文说明：真实 TTY 对 emoji/FE0F 的列宽可能与模型不同；每个 cell 边界按模型列复位。
			out.WriteString(ansiColumn(modelCol))
		}
	}
	return out.String()
}

func writeANSIText(out *strings.Builder, cell Cell, startModelCol int) {
	text := SafeLine(cell.Text)
	if !cell.TerminalContent || cell.Width <= 1 || !strings.Contains(text, "\ufe0f") {
		out.WriteString(text)
		return
	}
	modelCol := startModelCol
	for len(text) > 0 {
		cluster, width := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		text = text[len(cluster):]
		out.WriteString(cluster)
		modelWidth := width
		if modelWidth < 0 {
			modelWidth = 0
		}
		eraseContinuation := terminalFE0FContinuationErase(cluster)
		if eraseContinuation && modelWidth < 2 {
			modelWidth = 2
		}
		if modelWidth == 0 {
			continue
		}
		modelCol += modelWidth
		if eraseContinuation {
			// 中文说明：只在 terminal/protocol cell 的 FE0F footprint 边界做 TTY 写出保护；
			// 先清模型 continuation 物理格，再锚回下一模型列，避免后续内容或边框漂移。
			out.WriteString(ansiEraseChars(1))
			if text != "" {
				out.WriteString(ansiColumn(modelCol))
			}
		}
	}
}

func terminalFE0FContinuationErase(text string) bool {
	return strings.Contains(text, "\ufe0f") && !strings.Contains(text, "\u200d") && !strings.Contains(text, "\u20e3")
}

func ansiEraseChars(count int) string {
	if count <= 0 {
		return ""
	}
	return "\x1b[" + strconv.Itoa(count) + "X"
}

func ansiColumn(col int) string {
	if col < 1 {
		col = 1
	}
	return "\x1b[" + strconv.Itoa(col) + "G"
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
	LayerPopup    LayerKind = "popup"
	LayerToast    LayerKind = "toast"
)

type Layer struct {
	Kind            LayerKind
	Rect            Rect
	Lines           []Line
	ContentOverflow ContentOverflow
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
	case StyleForeground:
		return sgrForeground(theme.ChromeFG, false)
	case StyleStrongForeground:
		return sgrForeground(theme.ChromeFG, true)
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
	case StyleHeaderWorkspace:
		return sgrForegroundBackground(headerWorkspaceFG(theme), headerWorkspaceBG(theme), true)
	case StyleHeaderSpacer:
		return sgrForegroundBackground(theme.StatusFG, theme.StatusBG, false)
	case StyleHeaderInactiveIndex:
		return sgrForegroundBackground(headerInactiveIndexFG(theme), theme.StatusBG, false)
	case StyleHeaderInactiveTitle:
		return sgrForegroundBackground(theme.InactivePane, theme.StatusBG, false)
	case StyleHeaderInactiveClose:
		return sgrForegroundBackground(headerInactiveCloseFG(theme), theme.StatusBG, false)
	case StyleHeaderActiveMarker:
		return sgrForegroundBackground(theme.Accent, headerActiveBG(theme), true)
	case StyleHeaderActiveIndex:
		return sgrForegroundBackground(theme.StatusFG, headerActiveBG(theme), true)
	case StyleHeaderActiveTitle:
		return sgrForegroundBackground(headerActiveFG(theme), headerActiveBG(theme), true)
	case StyleHeaderActiveClose:
		return sgrForegroundBackground(theme.Danger, headerActiveBG(theme), false)
	case StyleHeaderCreate:
		return sgrForegroundBackground(headerCreateFG(theme), headerCreateBG(theme), true)
	case StyleFooterChrome:
		return sgrForeground(theme.Accent, false)
	case StyleFooterMuted:
		return sgrForeground(theme.Muted, false)
	case StyleFooterAccent:
		return sgrForeground(theme.Accent, true)
	case StyleFooterKeyPane:
		return sgrForeground(theme.Accent, true)
	case StyleFooterKeyResize:
		return sgrForeground(theme.Warning, true)
	case StyleFooterKeyTab:
		return sgrForeground(theme.Info, true)
	case StyleFooterKeyWorkspace:
		return sgrForeground(theme.Success, true)
	case StyleFooterKeyFloat:
		return sgrForeground(mixHostColor(theme.Accent, theme.Info, 0.45), true)
	case StyleFooterKeyCopy:
		return sgrForeground(mixHostColor(theme.Success, theme.Info, 0.35), true)
	case StyleFooterKeyPicker:
		return sgrForeground(mixHostColor(theme.Danger, theme.Warning, 0.35), true)
	case StyleFooterKeyGlobal:
		return sgrForeground(mixHostColor(theme.Accent, theme.Warning, 0.35), true)
	case StyleInfo:
		return sgrForeground(theme.Info, false)
	case StyleSuccess:
		return sgrForeground(theme.Success, false)
	case StyleWarning:
		return sgrForeground(theme.Warning, false)
	case StyleDanger:
		return sgrForeground(theme.Danger, false)
	case StyleDangerStrong:
		return sgrForeground(theme.Danger, true)
	case StyleOverlay:
		return sgrForegroundBackground(theme.ChromeFG, theme.OverlayBG, false)
	case StylePicker:
		return sgrForeground(theme.ChromeFG, false)
	case StylePickerMuted:
		return sgrForeground(theme.Muted, false) + "\x1b[2m"
	case StylePickerAccent:
		return sgrForeground(theme.Accent, true)
	case StylePickerInfo:
		return sgrForeground(theme.Info, true)
	case StylePickerSuccess:
		return sgrForeground(theme.Success, true)
	case StylePickerMatch:
		return sgrForeground(theme.Warning, true)
	case StylePromptSuggestion:
		return sgrForegroundBackground(theme.ChromeFG, promptSuggestionBG(theme), false)
	case StylePromptSuggestionHit:
		return sgrForegroundBackground(theme.ChromeFG, promptSuggestionHitBG(theme), true)
	case StyleToast:
		return sgrForegroundBackground(theme.ChromeFG, theme.ToastBG, false)
	case StyleToastAccent:
		return sgrForegroundBackground(theme.Accent, theme.ToastBG, true)
	default:
		return "\x1b[1m"
	}
}

func promptSuggestionBG(theme Theme) string {
	return mixHostColor(theme.HostBG, theme.Accent, 0.16)
}

func promptSuggestionHitBG(theme Theme) string {
	return mixHostColor(theme.HostBG, theme.Accent, 0.34)
}

func headerWorkspaceBG(theme Theme) string {
	return mixHostColor(headerChromeAltBG(theme), theme.Accent, 0.24)
}

func headerWorkspaceFG(theme Theme) string {
	return mixHostColor(theme.StatusFG, theme.Accent, 0.30)
}

func headerActiveBG(theme Theme) string {
	return mixHostColor(mixHostColor(theme.StatusBG, theme.StatusFG, 0.06), theme.Accent, 0.14)
}

func headerActiveFG(theme Theme) string {
	return theme.StatusFG
}

func headerInactiveIndexFG(theme Theme) string {
	return mixHostColor(theme.InactivePane, theme.Accent, 0.22)
}

func headerInactiveCloseFG(theme Theme) string {
	return mixHostColor(theme.Danger, theme.InactivePane, 0.62)
}

func headerCreateBG(theme Theme) string {
	return mixHostColor(headerChromeAltBG(theme), theme.Info, 0.18)
}

func headerCreateFG(theme Theme) string {
	return theme.StatusFG
}

func headerChromeAltBG(theme Theme) string {
	return mixHostColor(theme.StatusBG, theme.StatusFG, 0.08)
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

func ansiLinkOpenClose(linkURL string, linkParams string) (string, string) {
	linkURL = sanitizeOSC8Field(linkURL)
	if linkURL == "" {
		return "", ""
	}
	linkParams = sanitizeOSC8Field(linkParams)
	return "\x1b]8;" + linkParams + ";" + linkURL + "\x1b\\", "\x1b]8;;\x1b\\"
}

func sanitizeOSC8Field(value string) string {
	value = strings.ReplaceAll(value, "\x1b", "")
	value = strings.ReplaceAll(value, "\x07", "")
	value = strings.ReplaceAll(value, "\x9c", "")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\r", "")
	return value
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
	ContentClipboardHistory ContentKind = "clipboard-history"
	ContentFloatingOverview ContentKind = "floating-overview"
	ContentPrompt           ContentKind = "prompt"
	ContentHelp             ContentKind = "help"
	ContentPlaceholder      ContentKind = "placeholder"
)

type ContentVM struct {
	Kind       ContentKind
	Lines      []Line
	Extent     ContentExtent
	Layout     ContentLayoutVM
	Status     string
	Pending    bool
	Empty      bool
	Error      string
	Cursor     Cursor
	HitRegions []HitRegion
}

// ContentExtent 表达 terminal 内容在 content rect 内实际占用的 cell 区域。
// Known=false 时，renderer 以当前 content rect 作为 extent，避免把普通旧内容误判为 extent 外占位。
type ContentExtent struct {
	Known bool
	X     int
	Y     int
	Cols  int
	Rows  int
}

// ContentLayoutVM 是 view-local terminal 内容布局投影，只影响当前 pane/floating 的裁切与对齐。
type ContentLayoutVM struct {
	Known  bool
	Mode   string
	PanX   int
	PanY   int
	AlignX string
	AlignY string
}

type ContentOverflow struct {
	Left   bool
	Right  bool
	Top    bool
	Bottom bool
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
	Overflow   ContentOverflow
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
	Chrome       PanelChromeVM
	Content      ContentVM
}

type PanelChromeVM struct {
	Title    ChromeSlotVM
	State    ChromeSlotVM
	Meta     []ChromeSlotVM
	Terminal TerminalChromeVM
	Actions  []ChromeActionVM
}

type TerminalChromeVM struct {
	Locked       bool
	LayoutMode   string
	PanX         int
	PanY         int
	AlignX       string
	AlignY       string
	Title        ChromeSlotVM
	State        ChromeSlotVM
	AttachCount  int
	Owner        ChromeSlotVM
	TakeOwner    bool
	ResizeRole   string
	CanResize    bool
	TerminalID   string
	TerminalView string
}

type ChromeSlotVM struct {
	Text  string
	Style StyleToken
}

type ChromeActionVM struct {
	Text     string
	ActionID string
	Style    StyleToken
}

type LayoutVM struct {
	Viewport           Rect
	Body               Rect
	ShellFrame         Rect
	HeaderTopFrame     Rect
	HeaderDividerFrame Rect
	FooterFrame        Rect
	Panels             []PanelVM
	BodyContent        ContentVM
	Floating           []FloatingVM
	Split              SplitVM
	ChromePatches      []ChromePatchVM
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
	Chrome    FloatingChromeVM
	Content   ContentVM
}

type FloatingChromeVM struct {
	FillOverlay      bool
	ShowResizeHandle bool
	Terminal         TerminalChromeVM
	Actions          []ChromeActionVM
}

type ChromePatchVM struct {
	Anchor ChromePatchAnchor
	X      int
	Y      int
	W      int
	Text   string
	Style  StyleToken
	Layer  LayerKind
	Owner  string
}

type ChromePatchAnchor string

const (
	ChromePatchAnchorViewport ChromePatchAnchor = "viewport"
	ChromePatchAnchorBody     ChromePatchAnchor = "body"
)

type HeaderTabVM struct {
	ID            string
	Title         string
	Index         int
	Active        bool
	CloseActionID string
	CloseTargetID string
}

type HeaderVM struct {
	Visible         bool
	Workspace       string
	Tab             string
	Tabs            []HeaderTabVM
	ActivePane      string
	TerminalSummary string
	FloatingSummary string
	Notice          string
	Title           string
}

type FooterActionVM struct {
	Key      string
	Label    string
	ActionID string
	Style    StyleToken
}

type FooterVM struct {
	Visible       bool
	Mode          string
	Hint          string
	Actions       []string
	ActionTokens  []FooterActionVM
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
	OverlayNone             OverlayKind = ""
	OverlayTerminalPicker   OverlayKind = "terminal-picker"
	OverlayTerminalPool     OverlayKind = "terminal-pool"
	OverlayWorkbenchTree    OverlayKind = "workbench-tree"
	OverlayClipboardHistory OverlayKind = "clipboard-history"
	OverlayFloatingOverview OverlayKind = "floating-overview"
	OverlayPrompt           OverlayKind = "prompt"
	OverlayHelp             OverlayKind = "help"
)

type OverlayVM struct {
	Kind    OverlayKind
	Opaque  bool
	Content ContentVM
	Popup   OverlayPopupVM
}

type ShellVM struct {
	Header  HeaderVM
	Footer  FooterVM
	Layout  LayoutVM
	Overlay OverlayVM
	Toasts  []ToastVM
	Cursor  Cursor
}

type OverlayPopupKind string

const (
	OverlayPopupNone             OverlayPopupKind = ""
	OverlayPopupPromptSuggestion OverlayPopupKind = "prompt-suggestion"
)

type OverlayPopupVM struct {
	Kind      OverlayPopupKind
	AnchorRow int
	AnchorCol int
	Lines     []Line
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
