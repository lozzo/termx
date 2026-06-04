package render

import (
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
	Text  string
	Width int
	Style StyleToken
	Safe  bool
}

type StyleToken string

const (
	StyleAccent  StyleToken = "accent"
	StyleMuted   StyleToken = "muted"
	StyleStatus  StyleToken = "status"
	StyleInfo    StyleToken = "info"
	StyleSuccess StyleToken = "success"
	StyleWarning StyleToken = "warning"
	StyleDanger  StyleToken = "danger"
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

func (line Line) ANSIString() string {
	if len(line.Cells) == 0 {
		return ""
	}
	var out strings.Builder
	for _, cell := range line.Cells {
		text := SafeLine(cell.Text)
		if cell.Style == "" {
			out.WriteString(text)
			continue
		}
		out.WriteString(ansiForStyleToken(cell.Style))
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
	Blink      bool
	HitRegions []HitRegion
	Metadata   RenderMetadata
	Layers     []Layer
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
	for i, line := range result.Content {
		lines[i] = ensureANSIReset(line.ANSIString())
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

func ansiForStyleToken(token StyleToken) string {
	switch token {
	case StyleAccent:
		return "\x1b[1;36m"
	case StyleMuted:
		return "\x1b[2m"
	case StyleStatus:
		return "\x1b[7m"
	case StyleInfo:
		return "\x1b[34m"
	case StyleSuccess:
		return "\x1b[32m"
	case StyleWarning:
		return "\x1b[33m"
	case StyleDanger:
		return "\x1b[31m"
	default:
		return "\x1b[1m"
	}
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
	PaneID    string
	Direction SplitDirection
	Children  []SplitVM
}

type FloatingVM struct {
	ID      string
	Title   string
	Rect    Rect
	Z       int
	Content ContentVM
}

type HeaderVM struct {
	Visible bool
	Title   string
	Notice  string
}

type FooterVM struct {
	Visible bool
	Mode    string
	Hint    string
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
