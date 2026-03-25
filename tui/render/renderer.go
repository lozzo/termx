package render

import (
	"strings"

	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
	"github.com/lozzow/termx/tui/render/projection"
)

type Config struct {
	DebugVisible *bool
}

type Renderer interface {
	btui.Renderer
}

type renderer struct {
	debugVisible *bool
}

func NewRenderer(cfg Config) Renderer {
	return renderer{
		debugVisible: cfg.DebugVisible,
	}
}

// Render 先走新的 projection 入口，输出保持极简文本壳，
// 目的是先把运行时接线从 tui/runtime_renderer 迁到 tui/render。
func (r renderer) Render(state types.AppState, notices []btui.Notice) string {
	_ = r.debugVisible
	_ = notices

	view := projection.ProjectWorkbench(state, nil, 0, 0)
	lines := []string{"termx"}
	if view.ActivePaneID != "" {
		lines = append(lines, "active_pane: "+string(view.ActivePaneID))
	}
	if len(view.Floating) > 0 {
		floating := make([]string, 0, len(view.Floating))
		for _, pane := range view.Floating {
			floating = append(floating, string(pane.PaneID))
		}
		lines = append(lines, "floating: "+strings.Join(floating, ","))
	}
	return strings.Join(lines, "\n")
}
