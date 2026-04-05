package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var errorClearDelay = 3 * time.Second
var ownerConfirmDelay = 400 * time.Millisecond

const (
	defaultTerminalSnapshotScrollbackLimit = 500
	maxTerminalSnapshotScrollbackLimit     = 10000
	terminalScrollbackPrefetchMargin       = 8
)

func clearErrorCmd(seq uint64) tea.Cmd {
	return tea.Tick(errorClearDelay, func(time.Time) tea.Msg {
		return clearErrorMsg{seq: seq}
	})
}

func clearOwnerConfirmCmd(seq uint64) tea.Cmd {
	return tea.Tick(ownerConfirmDelay, func(time.Time) tea.Msg {
		return clearOwnerConfirmMsg{seq: seq}
	})
}

func renderErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m *Model) bodyHeight() int {
	if m == nil {
		return render.FrameBodyHeight(0)
	}
	return render.FrameBodyHeight(m.height)
}

func (m *Model) contentOriginY() int {
	return render.TopChromeRows
}

func (m *Model) bodyRect() workbench.Rect {
	if m == nil {
		return workbench.Rect{W: 1, H: render.FrameBodyHeight(0)}
	}
	return workbench.Rect{W: maxInt(1, m.width), H: m.bodyHeight()}
}

func (m *Model) activePaneContentRect() (workbench.Rect, bool) {
	if m == nil || m.workbench == nil {
		return workbench.Rect{}, false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || tab.ActivePaneID == "" {
		return workbench.Rect{}, false
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil {
		return workbench.Rect{}, false
	}
	for _, pane := range visible.FloatingPanes {
		if pane.ID != tab.ActivePaneID {
			continue
		}
		return paneContentRect(pane.Rect)
	}
	if visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return workbench.Rect{}, false
	}
	for _, pane := range visible.Tabs[visible.ActiveTab].Panes {
		if pane.ID != tab.ActivePaneID {
			continue
		}
		return paneContentRect(pane.Rect)
	}
	return workbench.Rect{}, false
}

func (m *Model) ensureActivePaneScrollbackCmd() tea.Cmd {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	pane := m.workbench.ActivePane()
	if tab == nil || pane == nil || pane.TerminalID == "" || tab.ScrollOffset <= 0 {
		return nil
	}
	contentRect, ok := m.activePaneContentRect()
	if !ok {
		return nil
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil || terminal.Snapshot == nil || terminal.Snapshot.Modes.AlternateScreen || terminal.ScrollbackExhausted {
		return nil
	}
	loaded := len(terminal.Snapshot.Scrollback)
	want := tab.ScrollOffset + contentRect.H + terminalScrollbackPrefetchMargin
	if want <= loaded {
		return nil
	}
	nextLimit := maxInt(defaultTerminalSnapshotScrollbackLimit, loaded)
	for nextLimit < want && nextLimit < maxTerminalSnapshotScrollbackLimit {
		nextLimit *= 2
	}
	if nextLimit > maxTerminalSnapshotScrollbackLimit {
		nextLimit = maxTerminalSnapshotScrollbackLimit
	}
	if nextLimit <= loaded || terminal.ScrollbackLoadingLimit >= nextLimit {
		return nil
	}
	terminal.ScrollbackLoadingLimit = nextLimit
	terminalID := pane.TerminalID
	return func() tea.Msg {
		snapshot, err := m.runtime.LoadSnapshot(context.Background(), terminalID, 0, nextLimit)
		if err != nil {
			return err
		}
		return orchestrator.SnapshotLoadedMsg{TerminalID: terminalID, Snapshot: snapshot}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Model) ensureRecoverablePane() (string, error) {
	if m == nil || m.workbench == nil {
		return "", fmt.Errorf("workbench unavailable")
	}
	if pane := m.workbench.ActivePane(); pane != nil && pane.ID != "" {
		return pane.ID, nil
	}

	ws := m.workbench.CurrentWorkspace()
	if ws == nil {
		return "", fmt.Errorf("no workspace available")
	}
	if tab := m.workbench.CurrentTab(); tab != nil {
		if len(tab.Panes) > 0 {
			if tab.ActivePaneID != "" {
				return tab.ActivePaneID, nil
			}
			return "", fmt.Errorf("current tab has no active pane")
		}
		paneID := shared.NextPaneID()
		if err := m.workbench.CreateFirstPane(tab.ID, paneID); err != nil {
			return "", err
		}
		m.render.Invalidate()
		return paneID, nil
	}

	tabID := shared.NextTabID()
	paneID := shared.NextPaneID()
	name := strconv.Itoa(len(ws.Tabs) + 1)
	if err := m.workbench.CreateTab(ws.Name, tabID, name); err != nil {
		return "", err
	}
	if err := m.workbench.CreateFirstPane(tabID, paneID); err != nil {
		return "", err
	}
	_ = m.workbench.SwitchTab(ws.Name, len(ws.Tabs)-1)
	m.render.Invalidate()
	return paneID, nil
}

func (m *Model) openPickerForPaneCmd(paneID string) tea.Cmd {
	if m == nil || m.modalHost == nil || paneID == "" {
		return nil
	}
	m.resetPickerState()
	m.modalHost.StartLoading(input.ModePicker, paneID)
	m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: paneID})
	m.render.Invalidate()
	return m.effectCmd(orchestrator.LoadPickerItemsEffect{})
}

func (m *Model) openTerminalManagerCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	m.terminalPage = &modal.TerminalManagerState{
		Title: "Terminal Pool",
	}
	m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})
	m.render.Invalidate()
	return m.loadTerminalManagerItemsCmd()
}
