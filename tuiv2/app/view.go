package app

import (
	"fmt"
	"strings"
)

func (m *Model) View() string {
	frame := m.render.RenderFrame()
	var extra []string
	if m.modalHost != nil && m.modalHost.Session != nil {
		extra = append(extra, fmt.Sprintf("modal: %s phase=%s", m.modalHost.Session.Kind, m.modalHost.Session.Phase))
	}
	if m.err != nil {
		extra = append(extra, fmt.Sprintf("error: %v", m.err))
	}
	if len(extra) == 0 {
		return frame
	}
	return strings.Join(append([]string{frame}, extra...), "\n")
}
