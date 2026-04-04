package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/render"
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
