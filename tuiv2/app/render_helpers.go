package app

import (
	"strconv"
	"strings"

	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) revealCursorAndInvalidate() {
	if m == nil || m.render == nil {
		return
	}
	m.render.RevealCursorBlink()
}

func (m *Model) forceFullRedraw() {
	if m == nil {
		return
	}
	if m.render != nil {
		m.render.ResetCaches()
		m.render.Invalidate()
	}
	if resetter, ok := m.frameOut.(frameResetWriter); ok {
		resetter.ResetFrameState()
	}
}

func (m *Model) visibleAltScreenGeometryChanged() bool {
	if m == nil || m.workbench == nil {
		return false
	}
	signature, hasAltScreen := m.visibleLayoutSignature()
	changed := m.visibleLayoutSigSet && signature != m.lastVisibleLayoutSig
	m.lastVisibleLayoutSig = signature
	m.visibleLayoutSigSet = true
	return changed && hasAltScreen
}

func (m *Model) visibleLayoutSignature() (string, bool) {
	if m == nil || m.workbench == nil {
		return "", false
	}
	body := m.bodyRect()
	visible := m.workbench.VisibleWithSize(body)
	if visible == nil {
		return "", false
	}
	var builder strings.Builder
	builder.Grow(128)
	builder.WriteString("body:")
	builder.WriteString(strconv.Itoa(body.W))
	builder.WriteByte('x')
	builder.WriteString(strconv.Itoa(body.H))
	builder.WriteByte('|')
	builder.WriteString(strconv.Itoa(visible.ActiveTab))
	builder.WriteByte('|')

	hasAltScreen := false
	if visible.ActiveTab >= 0 && visible.ActiveTab < len(visible.Tabs) {
		tab := visible.Tabs[visible.ActiveTab]
		builder.WriteString(tab.ID)
		builder.WriteByte('|')
		builder.WriteString(tab.ZoomedPaneID)
		builder.WriteByte('|')
		for _, pane := range tab.Panes {
			appendVisiblePaneGeometry(&builder, pane)
			hasAltScreen = hasAltScreen || m.visiblePaneUsesAltScreen(pane.TerminalID)
		}
	}
	builder.WriteString("floating|")
	for _, pane := range visible.FloatingPanes {
		appendVisiblePaneGeometry(&builder, pane)
		hasAltScreen = hasAltScreen || m.visiblePaneUsesAltScreen(pane.TerminalID)
	}
	return builder.String(), hasAltScreen
}

func appendVisiblePaneGeometry(builder *strings.Builder, pane workbench.VisiblePane) {
	if builder == nil {
		return
	}
	builder.WriteString(pane.ID)
	builder.WriteByte('@')
	builder.WriteString(strconv.Itoa(pane.Rect.X))
	builder.WriteByte(',')
	builder.WriteString(strconv.Itoa(pane.Rect.Y))
	builder.WriteByte(',')
	builder.WriteString(strconv.Itoa(pane.Rect.W))
	builder.WriteByte(',')
	builder.WriteString(strconv.Itoa(pane.Rect.H))
	builder.WriteByte(',')
	if pane.Floating {
		builder.WriteByte('f')
	} else {
		builder.WriteByte('t')
	}
	if pane.Frameless {
		builder.WriteByte('n')
	} else {
		builder.WriteByte('b')
	}
	if pane.SharedLeft {
		builder.WriteByte('l')
	}
	if pane.SharedTop {
		builder.WriteByte('u')
	}
	builder.WriteByte('|')
}

func (m *Model) visiblePaneUsesAltScreen(terminalID string) bool {
	if m == nil || m.runtime == nil || terminalID == "" {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil {
		return false
	}
	if terminal.VTerm != nil {
		return terminal.VTerm.Modes().AlternateScreen
	}
	if terminal.Snapshot != nil {
		return terminal.Snapshot.Modes.AlternateScreen
	}
	return false
}

func (m *Model) beginHostThemeBootstrap(expectedPalette int) {
	if m == nil {
		return
	}
	m.hostThemeBootstrapPending = true
	m.hostThemeBootstrapPaletteN = maxInt(0, expectedPalette)
	m.hostThemeBootstrapSeenFG = false
	m.hostThemeBootstrapSeenBG = false
	if m.hostThemeBootstrapPalette == nil {
		m.hostThemeBootstrapPalette = make(map[int]struct{})
	} else {
		clear(m.hostThemeBootstrapPalette)
	}
}
