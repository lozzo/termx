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
	return RenderVM{
		Mode:   ModeLive,
		Lines:  []string{"live surface pending"},
		Status: "live",
	}
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
