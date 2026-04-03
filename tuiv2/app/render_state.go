package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var errorClearDelay = 5 * time.Second

func renderErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func clearErrorCmd(seq uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(errorClearDelay)
		return clearErrorMsg{seq: seq}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
