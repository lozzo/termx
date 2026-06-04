package render

import "github.com/lozzow/termx/termx-tui-v3/state"

type Mode string

const (
	ModeLive Mode = "live"
	ModeCopy Mode = "copy"
)

type RenderVM struct {
	Mode       Mode
	Lines      []string
	Status     string
	HitRegions []HitRegion
}

type HitRegionKind string

const (
	HitRegionHistoryRow HitRegionKind = "history-row"
	HitRegionStatus     HitRegionKind = "status"
)

type Rect struct {
	X int
	Y int
	W int
	H int
}

type HitRegion struct {
	Kind   HitRegionKind
	Rect   Rect
	LineID uint64
	Row    int
}

type RenderVMBuilder struct{}

func NewRenderVMBuilder() RenderVMBuilder {
	return RenderVMBuilder{}
}

func (RenderVMBuilder) Build(root state.Root) RenderVM {
	if canRenderCopyMode(root.History, root.CopyMode) {
		return buildCopyModeVM(root.History, root.CopyMode)
	}
	return buildLiveVM(root.Surface, root.Session)
}

func canRenderCopyMode(history state.HistoryStore, copyMode state.CopyModeStore) bool {
	return copyMode.Active &&
		copyMode.BoundToken != "" &&
		copyMode.BoundToken == history.Token &&
		copyMode.BoundCols == history.Cols &&
		copyMode.TerminalID == history.TerminalID &&
		len(history.Rows) > 0
}

func buildCopyModeVM(history state.HistoryStore, copyMode state.CopyModeStore) RenderVM {
	lines := make([]string, len(history.Rows))
	regions := make([]HitRegion, len(history.Rows))
	for i, row := range history.Rows {
		lines[i] = row.Text
		regions[i] = HitRegion{
			Kind:   HitRegionHistoryRow,
			Rect:   Rect{Y: i, W: history.Cols, H: 1},
			LineID: row.LineID,
			Row:    i,
		}
	}
	return RenderVM{
		Mode:       ModeCopy,
		Lines:      lines,
		Status:     copyModeStatus(copyMode),
		HitRegions: regions,
	}
}

func buildLiveVM(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) RenderVM {
	lines := append([]string(nil), surface.Lines...)
	if len(lines) == 0 {
		lines = []string{"live surface pending"}
	}
	status := "live"
	if surface.TerminalID != "" {
		status = "live: " + surface.TerminalID
	}
	if session.LastError != "" {
		status = "error: " + session.LastError
	} else if surface.Err != "" {
		status = "error: " + surface.Err
	}
	return RenderVM{
		Mode:   ModeLive,
		Lines:  lines,
		Status: status,
	}
}

func copyModeStatus(copyMode state.CopyModeStore) string {
	if copyMode.Empty {
		return "copy: empty"
	}
	return "copy"
}

type Renderer struct {
	Theme Theme
}

func NewRenderer(theme Theme) Renderer {
	if theme == (Theme{}) {
		theme = DefaultTheme()
	}
	return Renderer{Theme: theme}
}

func (renderer Renderer) Render(vm RenderVM) Frame {
	lines := make([]string, 0, len(vm.Lines)+1)
	for _, line := range vm.Lines {
		lines = append(lines, SafeLine(line))
	}
	if vm.Status != "" {
		lines = append(lines, StatusStyle(renderer.Theme).Render(SafeLine(vm.Status)))
	}
	return Frame{Lines: lines}
}
