package render

import (
	"fmt"
	"strings"

	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
	"github.com/lozzow/termx/tui/render/compositor"
	"github.com/lozzow/termx/tui/render/projection"
	"github.com/lozzow/termx/tui/render/surface"
)

type Config struct {
	DebugVisible *bool
	Screens      projection.RuntimeTerminalStore
	Compat       btui.Renderer
}

type Renderer interface {
	btui.Renderer
}

type renderer struct {
	debugVisible *bool
	screens      projection.RuntimeTerminalStore
	compat       btui.Renderer
}

type TerminalStoreBinder interface {
	WithTerminalStore(screens projection.RuntimeTerminalStore) Renderer
}

type terminalStoreAwareRenderer interface {
	WithTerminalStore(screens projection.RuntimeTerminalStore) btui.Renderer
}

func NewRenderer(cfg Config) Renderer {
	return &renderer{
		debugVisible: cfg.DebugVisible,
		screens:      cfg.Screens,
		compat:       cfg.Compat,
	}
}

func (r *renderer) WithTerminalStore(screens projection.RuntimeTerminalStore) Renderer {
	if r == nil {
		return nil
	}
	next := *r
	next.screens = screens
	if aware, ok := next.compat.(terminalStoreAwareRenderer); ok {
		next.compat = aware.WithTerminalStore(screens)
	}
	return &next
}

// Render 默认优先走新的 tiled workbench；
// debug、overlay、floating 这些本任务未覆盖的路径继续保留 compat 作为对照兜底。
func (r *renderer) Render(state types.AppState, notices []btui.Notice) string {
	if r == nil {
		return ""
	}

	if r.debugEnabled() && r.compat != nil {
		return r.compat.Render(state, notices)
	}
	view := projection.ProjectWorkbench(state, r.screens, 0, 0)
	if state.UI.Overlay.Kind != types.OverlayNone || len(view.Floating) > 0 {
		if r.compat != nil {
			return r.compat.Render(state, notices)
		}
	}

	lines, ok := r.renderWorkbench(state, view, notices)
	if ok {
		return strings.Join(lines, "\n")
	}
	if r.compat != nil {
		return r.compat.Render(state, notices)
	}
	return "termx"
}

func (r *renderer) debugEnabled() bool {
	return r != nil && r.debugVisible != nil && *r.debugVisible
}

func (r *renderer) renderWorkbench(state types.AppState, view projection.WorkbenchView, notices []btui.Notice) ([]string, bool) {
	workspace, tab, ok := activeWorkspaceTab(state)
	if !ok {
		return nil, false
	}

	paneRects, width, height := resolvePaneRects(tab, orderedPaneIDs(view))
	if len(paneRects) == 0 {
		return nil, false
	}

	workbench := compositor.View{
		Width:  width,
		Height: height,
		Panes:  make([]compositor.Pane, 0, len(paneRects)),
	}
	for _, projected := range view.Tiled {
		pane, ok := tab.Panes[projected.PaneID]
		if !ok {
			continue
		}
		rect, ok := paneRects[pane.ID]
		if !ok {
			continue
		}
		workbench.Panes = append(workbench.Panes, compositor.Pane{
			ID:      pane.ID,
			Rect:    rect,
			Active:  pane.ID == view.ActivePaneID,
			Surface: surface.BuildPaneSurface(state, pane, r.screens, rect.W-2, rect.H-2),
		})
	}
	if len(workbench.Panes) == 0 {
		return nil, false
	}

	lines := []string{renderHeaderLine(workspace, tab)}
	lines = append(lines, compositor.ComposeWorkbench(workbench).Lines()...)
	if noticeLine := renderNoticeLine(notices); noticeLine != "" {
		lines = append(lines, noticeLine)
	}
	return lines, true
}

func activeWorkspaceTab(state types.AppState) (types.WorkspaceState, types.TabState, bool) {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return types.WorkspaceState{}, types.TabState{}, false
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return workspace, types.TabState{}, false
	}
	return workspace, tab, true
}

func orderedPaneIDs(view projection.WorkbenchView) []types.PaneID {
	ids := make([]types.PaneID, 0, len(view.Tiled))
	for _, pane := range view.Tiled {
		ids = append(ids, pane.PaneID)
	}
	return ids
}

func resolvePaneRects(tab types.TabState, paneIDs []types.PaneID) (map[types.PaneID]types.Rect, int, int) {
	if len(paneIDs) == 0 {
		return nil, 0, 0
	}

	rects := make(map[types.PaneID]types.Rect, len(paneIDs))
	minX := 0
	minY := 0
	maxRight := 0
	maxBottom := 0
	haveRealRect := false

	for _, paneID := range paneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		rect := pane.Rect
		if rect.W <= 0 || rect.H <= 0 {
			continue
		}
		if !haveRealRect || rect.X < minX {
			minX = rect.X
		}
		if !haveRealRect || rect.Y < minY {
			minY = rect.Y
		}
		if right := rect.X + rect.W; right > maxRight {
			maxRight = right
		}
		if bottom := rect.Y + rect.H; bottom > maxBottom {
			maxBottom = bottom
		}
		haveRealRect = true
	}

	if haveRealRect {
		width := max(defaultWorkbenchWidth, maxRight-minX)
		height := max(defaultWorkbenchHeight, maxBottom-minY)
		for _, paneID := range paneIDs {
			pane := tab.Panes[paneID]
			rect := pane.Rect
			if rect.W <= 0 || rect.H <= 0 {
				continue
			}
			rect.X -= minX
			rect.Y -= minY
			rects[paneID] = rect
		}
		for _, paneID := range paneIDs {
			if _, ok := rects[paneID]; ok {
				continue
			}
			rects[paneID] = types.Rect{X: 0, Y: 0, W: width, H: height}
		}
		return rects, width, height
	}

	width := defaultWorkbenchWidth
	height := defaultWorkbenchHeight
	if len(paneIDs) == 1 {
		rects[paneIDs[0]] = types.Rect{X: 0, Y: 0, W: width, H: height}
		return rects, width, height
	}

	columnWidth := max(20, width/len(paneIDs))
	x := 0
	for index, paneID := range paneIDs {
		paneWidth := columnWidth
		if index == len(paneIDs)-1 {
			paneWidth = width - x
		}
		rects[paneID] = types.Rect{X: x, Y: 0, W: paneWidth, H: height}
		x += paneWidth
	}
	return rects, width, height
}

const (
	defaultWorkbenchWidth  = 80
	defaultWorkbenchHeight = 20
)

func renderHeaderLine(workspace types.WorkspaceState, tab types.TabState) string {
	index := 1
	for i, tabID := range workspace.TabOrder {
		if tabID == tab.ID {
			index = i + 1
			break
		}
	}
	return fmt.Sprintf("termx  [%s]  [%d:%s]", safeLabel(workspace.Name, "workspace"), index, safeLabel(tab.Name, "tab"))
}

func renderNoticeLine(notices []btui.Notice) string {
	if len(notices) == 0 {
		return ""
	}
	parts := make([]string, 0, len(notices))
	for _, notice := range notices {
		if strings.TrimSpace(notice.Text) == "" {
			continue
		}
		parts = append(parts, notice.Text)
	}
	if len(parts) == 0 {
		return ""
	}
	return "notice: " + strings.Join(parts, " | ")
}

func safeLabel(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
