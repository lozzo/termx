package tui

import (
	"image/color"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
)

type inputEventMsg struct {
	event uv.Event
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.workspacePicker != nil {
		return m, m.handleWorkspacePickerKey(msg)
	}

	if m.terminalManager != nil {
		return m, m.handleTerminalManagerKey(msg)
	}

	if m.terminalPicker != nil {
		return m, m.handleTerminalPickerKey(msg)
	}

	if m.prompt != nil {
		return m, m.handlePromptKey(msg)
	}

	return m, m.handleCommonKeyboardFlow(keyboardFlowHooks{
		dismissHelp: func() {
			switch msg.Type {
			case tea.KeyEsc:
				m.showHelp = false
				m.invalidateRender()
			case tea.KeyRunes:
				if len(msg.Runes) == 1 && (msg.Runes[0] == 'q' || msg.Runes[0] == '?') {
					m.showHelp = false
					m.invalidateRender()
				}
			}
		},
		directCmd: func() tea.Cmd {
			return m.directModeCmdForKey(msg)
		},
		activePrefix: func() tea.Cmd {
			return m.handleActivePrefixKey(msg)
		},
		ctrlACmd: func() (tea.Cmd, bool) {
			if msg.Type == tea.KeyCtrlA {
				return m.sendToActive([]byte{0x01}), true
			}
			return nil, false
		},
		preExited: func() (tea.Cmd, bool) {
			if msg.Type == tea.KeyEsc && m.focusTiledPane() {
				return nil, true
			}
			return nil, false
		},
		exitedCmd: func() tea.Cmd {
			return m.handleExitedPaneKey(msg)
		},
		fallbackSend: func() tea.Cmd {
			if tab := m.currentTab(); tab != nil {
				if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
					data := encodeKey(msg)
					if len(data) > 0 {
						return m.sendToActive(data)
					}
				}
			}
			return nil
		},
	})
}

func (m *Model) handleCommonKeyboardFlow(hooks keyboardFlowHooks) tea.Cmd {
	if m.inputBlocked {
		return nil
	}
	if m.showHelp {
		if hooks.dismissHelp != nil {
			hooks.dismissHelp()
		}
		return nil
	}
	if m.prefixActive && m.directMode && hooks.directCmd != nil {
		if cmd := hooks.directCmd(); cmd != nil {
			return cmd
		}
	}
	if m.prefixActive && hooks.activePrefix != nil {
		return hooks.activePrefix()
	}
	if hooks.directCmd != nil {
		if cmd := hooks.directCmd(); cmd != nil {
			return cmd
		}
	}
	if hooks.ctrlACmd != nil {
		if cmd, ok := hooks.ctrlACmd(); ok {
			return cmd
		}
	}
	if hooks.preExited != nil {
		if cmd, ok := hooks.preExited(); ok {
			return cmd
		}
	}
	if hooks.exitedCmd != nil {
		if cmd := hooks.exitedCmd(); cmd != nil {
			return cmd
		}
	}
	if hooks.fallbackSend != nil {
		return hooks.fallbackSend()
	}
	return nil
}

func (m *Model) handleInputEvent(event uv.Event) tea.Cmd {
	switch event.(type) {
	case uv.KeyPressEvent, uv.MouseClickEvent, uv.MouseMotionEvent, uv.MouseReleaseEvent, uv.PasteEvent:
		m.noteInteraction()
	}

	if m.workspacePicker != nil {
		return m.handleWorkspacePickerEvent(event)
	}

	if m.terminalManager != nil {
		return m.handleTerminalManagerEvent(event)
	}

	if m.terminalPicker != nil {
		return m.handleTerminalPickerEvent(event)
	}

	if m.prompt != nil {
		return m.handlePromptEvent(event)
	}
	if m.inputBlocked {
		return nil
	}

	switch event := event.(type) {
	case uv.ForegroundColorEvent:
		m.applyHostTerminalColors(event.Color, nil)
		return nil
	case uv.BackgroundColorEvent:
		m.applyHostTerminalColors(nil, event.Color)
		return nil
	case uv.UnknownOscEvent:
		if index, c, ok := parsePaletteColorEvent(string(event)); ok {
			m.applyHostTerminalPaletteColor(index, c)
		}
		return nil
	case uv.KeyPressEvent:
		return m.handleKeyPressEvent(event)
	case uv.MouseClickEvent:
		return m.handleMouseClickEvent(event)
	case uv.MouseMotionEvent:
		return m.handleMouseMotionEvent(event)
	case uv.MouseReleaseEvent:
		return m.handleMouseReleaseEvent(event)
	case uv.PasteEvent:
		return m.pasteToActive(event.Content)
	case uv.UnknownEvent:
		if m.showHelp || m.prefixActive {
			return nil
		}
		return m.sendToActive([]byte(string(event)))
	default:
		return nil
	}
}

func parsePaletteColorEvent(raw string) (int, color.Color, bool) {
	if raw == "" {
		return 0, nil, false
	}
	raw = strings.TrimPrefix(raw, "\x1b]")
	raw = strings.TrimSuffix(raw, "\x07")
	raw = strings.TrimSuffix(raw, "\x1b\\")
	parts := strings.Split(raw, ";")
	if len(parts) != 3 || parts[0] != "4" {
		return 0, nil, false
	}
	index, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || index < 0 || index > 255 {
		return 0, nil, false
	}
	c := xansi.XParseColor(strings.TrimSpace(parts[2]))
	if c == nil {
		return 0, nil, false
	}
	return index, c, true
}

func (m *Model) handleKeyPressEvent(event uv.KeyPressEvent) tea.Cmd {
	return m.handleCommonKeyboardFlow(keyboardFlowHooks{
		dismissHelp: func() {
			if event.MatchString("esc") || event.MatchString("q") || event.MatchString("?") {
				m.showHelp = false
				m.invalidateRender()
			}
		},
		directCmd: func() tea.Cmd {
			return m.directModeCmdForEvent(event)
		},
		activePrefix: func() tea.Cmd {
			return m.handleActivePrefixEvent(event)
		},
		ctrlACmd: func() (tea.Cmd, bool) {
			if event.MatchString("ctrl+a") {
				return m.sendToActive([]byte{0x01}), true
			}
			return nil, false
		},
		preExited: func() (tea.Cmd, bool) {
			return nil, false
		},
		exitedCmd: func() tea.Cmd {
			return m.handleExitedPaneEvent(event)
		},
		fallbackSend: func() tea.Cmd {
			return m.sendKeyToActive(event)
		},
	})
}

func (m *Model) directModeCmdForEvent(event uv.KeyPressEvent) tea.Cmd {
	for _, shortcut := range directModeShortcutSpecs {
		if event.MatchString(shortcut.eventMatch) {
			return m.directModeCmdForShortcut(shortcut)
		}
	}
	return nil
}

func (m *Model) handleExitedPaneEvent(event uv.KeyPressEvent) tea.Cmd {
	if event.MatchString("r") {
		return m.exitedPaneShortcutCmd("r")
	}
	return nil
}

func shouldKeepPrefixEvent(event uv.KeyPressEvent) bool {
	switch {
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("h") || event.MatchString("left") || event.MatchString("j") || event.MatchString("down") || event.MatchString("k") || event.MatchString("up") || event.MatchString("l") || event.MatchString("right")):
		return true
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("H") || event.MatchString("shift+h") || event.MatchString("J") || event.MatchString("shift+j") || event.MatchString("K") || event.MatchString("shift+k") || event.MatchString("L") || event.MatchString("shift+l")):
		return true
	default:
		return shouldKeepPrefixInput(prefixInputFromEvent(event))
	}
}

func (m *Model) handleMouseClickEvent(event uv.MouseClickEvent) tea.Cmd {
	mouse := event.Mouse()
	if mouse.Button != uv.MouseLeft {
		return nil
	}
	tab := m.currentTab()
	if tab == nil {
		return nil
	}
	x, y, ok := m.mouseContentPoint(mouse.X, mouse.Y)
	if !ok {
		return nil
	}
	if paneID, rect, floating := m.paneAtPoint(tab, x, y); paneID != "" {
		tab.ActivePaneID = paneID
		if floating {
			reorderFloatingPanes(tab, paneID, true)
			m.mouseDragPaneID = paneID
			if floatingResizeHandleContains(rect, x, y) {
				m.mouseDragMode = mouseDragResize
				m.mouseDragOffset = Point{}
			} else {
				m.mouseDragMode = mouseDragMove
				m.mouseDragOffset = Point{X: x - rect.X, Y: y - rect.Y}
			}
		} else {
			m.mouseDragPaneID = ""
			m.mouseDragOffset = Point{}
			m.mouseDragMode = mouseDragNone
		}
		tab.renderCache = nil
		m.invalidateRender()
	}
	return nil
}

func (m *Model) handleMouseMotionEvent(event uv.MouseMotionEvent) tea.Cmd {
	mouse := event.Mouse()
	if mouse.Button != uv.MouseLeft || m.mouseDragPaneID == "" {
		return nil
	}
	tab := m.currentTab()
	if tab == nil {
		return nil
	}
	x, y, ok := m.mouseContentPoint(mouse.X, mouse.Y)
	if !ok {
		return nil
	}
	switch m.mouseDragMode {
	case mouseDragResize:
		entry := floatingPaneByID(tab, m.mouseDragPaneID)
		if entry == nil {
			return nil
		}
		if m.resizeFloatingPaneTo(tab, m.mouseDragPaneID, x-entry.Rect.X+1, y-entry.Rect.Y+1) {
			return m.resizeVisiblePanesCmd()
		}
	default:
		m.dragFloatingPaneTo(tab, m.mouseDragPaneID, x-m.mouseDragOffset.X, y-m.mouseDragOffset.Y)
	}
	return nil
}

func (m *Model) handleMouseReleaseEvent(event uv.MouseReleaseEvent) tea.Cmd {
	_ = event
	m.mouseDragPaneID = ""
	m.mouseDragOffset = Point{}
	m.mouseDragMode = mouseDragNone
	return nil
}

func (m *Model) sendKeyToActive(event uv.KeyPressEvent) tea.Cmd {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil || pane.VTerm == nil {
		return nil
	}
	return m.sendToActive(encodeKeyForPane(pane, event))
}

func (m *Model) pasteToActive(text string) tea.Cmd {
	tab := m.currentTab()
	pane := activePane(tab)
	if pane == nil || pane.VTerm == nil || text == "" {
		return nil
	}
	return m.sendToActive(encodePasteForPane(pane, text))
}

func (m *Model) handlePromptEvent(event uv.Event) tea.Cmd {
	switch event := event.(type) {
	case uv.KeyPressEvent:
		switch {
		case event.MatchString("esc"):
			m.cancelPrompt()
		case event.MatchString("enter"):
			return m.commitPrompt()
		case event.MatchString("backspace"):
			if m.promptAcceptsText() {
				m.deletePromptRune()
			}
		case event.Text != "" && m.promptAcceptsText():
			m.appendPrompt(event.Text)
		}
	case uv.PasteEvent:
		if m.promptAcceptsText() {
			m.appendPrompt(event.Content)
		}
	}
	return nil
}
