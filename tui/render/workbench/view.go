package workbench

import (
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/render/chrome"
	overlayview "github.com/lozzow/termx/tui/render/overlay"
	poolview "github.com/lozzow/termx/tui/render/pool"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

func init() {
	app.SetViewRenderer(Render)
}

func Render(model app.Model, width, height int) string {
	if model.Screen == app.ScreenTerminalPool {
		return poolview.Render(model, width, height)
	}
	lines := make([]string, 0, height)
	lines = append(lines, renderTopbar(model))
	lines = append(lines, renderPrimaryPane(model, width))
	lines = append(lines, renderActionBar(model))
	if model.Overlay.HasActive() {
		lines = append(lines, overlayview.Render(model, width, height))
	}
	return strings.Join(lines, "\n")
}

func renderTopbar(model app.Model) string {
	workspaceName := "main"
	if model.Workspace != nil && model.Workspace.ID != "" {
		workspaceName = string(model.Workspace.ID)
	}
	return "termx  [" + workspaceName + "]  [1:shell]"
}

func renderPrimaryPane(model app.Model, width int) string {
	tab := activeTab(model)
	if tab == nil {
		return chrome.Frame("unconnected", "unconnected", width, []string{"No terminal connected"})
	}
	pane, ok := tab.ActivePane()
	if !ok {
		return chrome.Frame("unconnected", "unconnected", width, []string{"No terminal connected"})
	}

	switch pane.SlotState {
	case types.PaneSlotUnconnected:
		lines := []string{}
		if model.Notice != nil {
			lines = append(lines, "notice: "+model.Notice.Message, "")
		}
		lines = append(lines,
			"",
			"connect existing terminal",
			"create new terminal",
			"open terminal pool",
		)
		return chrome.Frame("unconnected", "unconnected", width, lines)
	case types.PaneSlotLive:
		title := terminalName(model, pane.TerminalID)
		return chrome.Frame(title, liveMeta(model, pane), width, livePaneLines(model, pane))
	case types.PaneSlotExited:
		title := terminalName(model, pane.TerminalID)
		return chrome.Frame(title, "exited", width, []string{"terminal exited", "restart not wired yet"})
	default:
		return chrome.Frame("unconnected", "unconnected", width, []string{"No terminal connected"})
	}
}

func renderActionBar(model app.Model) string {
	title := "unconnected"
	tab := activeTab(model)
	if tab != nil {
		if pane, ok := tab.ActivePane(); ok && pane.TerminalID != "" {
			title = terminalName(model, pane.TerminalID)
		}
	}
	return " <c-p> pane  <c-t> tab  <c-w> workspace  <c-o> float  <c-f> connect  <?> help          " + title + "  ▣ tiled"
}

func activeTab(model app.Model) *workspace.TabState {
	if model.Workspace == nil {
		return nil
	}
	return model.Workspace.ActiveTab()
}

func terminalName(model app.Model, terminalID types.TerminalID) string {
	meta, ok := model.Terminals[terminalID]
	if !ok || meta.Name == "" {
		return "shell"
	}
	return meta.Name
}

func snapshotLines(model app.Model, terminalID types.TerminalID) []string {
	session, ok := model.Sessions[terminalID]
	if !ok || session.Snapshot == nil {
		return []string{"$"}
	}
	return flattenScreen(session.Snapshot)
}

func flattenScreen(snapshot *protocol.Snapshot) []string {
	if snapshot == nil || len(snapshot.Screen.Cells) == 0 {
		return []string{"$"}
	}
	lines := make([]string, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		var b strings.Builder
		for _, cell := range row {
			if cell.Content == "" {
				continue
			}
			b.WriteString(cell.Content)
		}
		lines = append(lines, b.String())
	}
	if len(lines) == 0 {
		return []string{"$"}
	}
	return lines
}

func liveMeta(model app.Model, pane workspace.PaneState) string {
	meta, ok := model.Terminals[pane.TerminalID]
	if !ok {
		return "running  owner"
	}
	if meta.OwnerPaneID != "" && meta.OwnerPaneID != pane.ID {
		return "running  follower"
	}
	return "running  owner"
}

func livePaneLines(model app.Model, pane workspace.PaneState) []string {
	lines := snapshotLines(model, pane.TerminalID)
	meta, ok := model.Terminals[pane.TerminalID]
	if !ok {
		return lines
	}
	if meta.OwnerPaneID != "" && meta.OwnerPaneID != pane.ID {
		return append([]string{"become owner"}, lines...)
	}
	return lines
}
