package app

import (
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
