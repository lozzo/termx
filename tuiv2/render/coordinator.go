package render

import (
	"fmt"
	"strings"
)

// Coordinator 负责 render invalidation / schedule / flush / ticker。
// 它通过 VisibleStateFn 拉取 workbench + runtime 的当前可见状态。
type Coordinator struct {
	visibleFn VisibleStateFn
}

type VisibleStateFn func() VisibleRenderState

func NewCoordinator(fn VisibleStateFn) *Coordinator {
	return &Coordinator{visibleFn: fn}
}

func (c *Coordinator) Invalidate()    {}
func (c *Coordinator) Schedule()      {}
func (c *Coordinator) FlushPending()  {}
func (c *Coordinator) StartTicker()   {}

// RenderFrame pulls the current VisibleRenderState and serialises it
// into a plain-text frame. The format is intentionally minimal: one
// field per line, indented children. Upper layers (app.Model.View) may
// append modal / error decorations on top of this output.
func (c *Coordinator) RenderFrame() string {
	if c == nil || c.visibleFn == nil {
		return ""
	}
	state := c.visibleFn()
	var lines []string

	if state.Workbench == nil {
		lines = append(lines, "tuiv2")
	} else {
		wb := state.Workbench
		lines = append(lines, fmt.Sprintf("workspace: %s", wb.WorkspaceName))
		lines = append(lines, fmt.Sprintf("active_tab: %d", wb.ActiveTab))
		for _, tab := range wb.Tabs {
			lines = append(lines, fmt.Sprintf("tab %s (%s)", tab.ID, tab.Name))
			for _, pane := range tab.Panes {
				lines = append(lines, fmt.Sprintf("  pane %s title=%s terminal=%s", pane.ID, pane.Title, pane.TerminalID))
			}
		}
	}

	if state.Runtime != nil {
		lines = append(lines, fmt.Sprintf("terminals: %d", len(state.Runtime.Terminals)))
		for _, term := range state.Runtime.Terminals {
			lines = append(lines, fmt.Sprintf("  terminal %s name=%s state=%s", term.TerminalID, term.Name, term.State))
		}
	}

	return strings.Join(lines, "\n")
}
