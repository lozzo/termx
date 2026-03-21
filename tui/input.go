package tui

import (
	"context"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	charmvt "github.com/charmbracelet/x/vt"
	localvterm "github.com/lozzow/termx/vterm"
	"github.com/muesli/cancelreader"
	xterm "golang.org/x/term"
)

type inputEventMsg struct {
	event uv.Event
}

func (m *Model) handleInputEvent(event uv.Event) tea.Cmd {
	switch event.(type) {
	case uv.KeyPressEvent, uv.MouseClickEvent, uv.MouseMotionEvent, uv.MouseReleaseEvent, uv.PasteEvent:
		m.noteInteraction()
	}

	if m.workspacePicker != nil {
		return m.handleWorkspacePickerEvent(event)
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

func (m *Model) handleKeyPressEvent(event uv.KeyPressEvent) tea.Cmd {
	if m.showHelp {
		switch {
		case event.MatchString("esc"), event.MatchString("q"), event.MatchString("?"):
			m.showHelp = false
			m.invalidateRender()
		}
		return nil
	}

	if m.prefixActive && m.directMode {
		if cmd := m.directModeCmdForEvent(event); cmd != nil {
			return cmd
		}
	}

	if m.prefixActive {
		return m.handleActivePrefixEvent(event)
	}

	if cmd := m.directModeCmdForEvent(event); cmd != nil {
		m.invalidateRender()
		return cmd
	}

	if event.MatchString("ctrl+a") {
		cmd := m.activatePrefix()
		m.invalidateRender()
		return cmd
	}

	if cmd := m.handleExitedPaneEvent(event); cmd != nil {
		return cmd
	}

	return m.sendKeyToActive(event)
}

func (m *Model) directModeCmdForEvent(event uv.KeyPressEvent) tea.Cmd {
	switch {
	case event.MatchString("ctrl+p"):
		return m.enterDirectMode(prefixModePane)
	case event.MatchString("ctrl+r"):
		return m.enterDirectMode(prefixModeResize)
	case event.MatchString("ctrl+t"):
		return m.enterDirectMode(prefixModeTab)
	case event.MatchString("ctrl+w"):
		return m.enterDirectMode(prefixModeWorkspace)
	case event.MatchString("ctrl+o"):
		return m.enterDirectMode(prefixModeFloating)
	case event.MatchString("ctrl+v"):
		return m.enterDirectMode(prefixModeViewport)
	case event.MatchString("ctrl+g"):
		return m.enterDirectMode(prefixModeGlobal)
	case event.MatchString("ctrl+f"):
		return m.openTerminalPickerCmd()
	default:
		return nil
	}
}

func (m *Model) handleExitedPaneEvent(event uv.KeyPressEvent) tea.Cmd {
	pane := activePane(m.currentTab())
	if paneTerminalState(pane) != "exited" {
		return nil
	}
	switch {
	case event.MatchString("r"):
		return m.restartActivePaneCmd()
	default:
		return nil
	}
}

func (m *Model) handlePrefixEvent(event uv.KeyPressEvent) tea.Cmd {
	switch {
	case event.MatchString("ctrl+a"):
		return m.sendToActive([]byte{0x01})
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("h") || event.MatchString("left")):
		m.moveActiveFloatingPane(-4, 0)
		return nil
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("j") || event.MatchString("down")):
		m.moveActiveFloatingPane(0, 2)
		return nil
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("k") || event.MatchString("up")):
		m.moveActiveFloatingPane(0, -2)
		return nil
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("l") || event.MatchString("right")):
		m.moveActiveFloatingPane(4, 0)
		return nil
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("H") || event.MatchString("shift+h")):
		m.resizeActiveFloatingPane(-4, 0)
		return nil
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("J") || event.MatchString("shift+j")):
		m.resizeActiveFloatingPane(0, 2)
		return nil
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("K") || event.MatchString("shift+k")):
		m.resizeActiveFloatingPane(0, -2)
		return nil
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("L") || event.MatchString("shift+l")):
		m.resizeActiveFloatingPane(4, 0)
		return nil
	case event.MatchString("\""):
		return m.splitActivePane(SplitHorizontal)
	case event.MatchString("%"):
		return m.splitActivePane(SplitVertical)
	case event.MatchString("h"), event.MatchString("left"):
		m.moveFocus(DirectionLeft)
	case event.MatchString("j"), event.MatchString("down"):
		m.moveFocus(DirectionDown)
	case event.MatchString("k"), event.MatchString("up"):
		m.moveFocus(DirectionUp)
	case event.MatchString("l"), event.MatchString("right"):
		m.moveFocus(DirectionRight)
	case event.MatchString("c"):
		return m.openNewTabTerminalPickerCmd()
	case event.MatchString("n"):
		if len(m.workspace.Tabs) > 0 {
			next := (m.workspace.ActiveTab + 1) % len(m.workspace.Tabs)
			return m.activateTab(next)
		}
		return nil
	case event.MatchString("p"):
		if len(m.workspace.Tabs) > 0 {
			next := (m.workspace.ActiveTab - 1 + len(m.workspace.Tabs)) % len(m.workspace.Tabs)
			return m.activateTab(next)
		}
		return nil
	case event.MatchString("z"):
		tab := m.currentTab()
		if tab != nil {
			if tab.ZoomedPaneID == tab.ActivePaneID {
				tab.ZoomedPaneID = ""
			} else {
				tab.ZoomedPaneID = tab.ActivePaneID
			}
		}
		return m.resizeVisiblePanesCmd()
	case event.MatchString("{"):
		m.swapActivePane(-1)
		return m.resizeVisiblePanesCmd()
	case event.MatchString("}"):
		m.swapActivePane(1)
		return m.resizeVisiblePanesCmd()
	case event.MatchString("H"), event.MatchString("shift+h"):
		m.resizeActivePane(DirectionLeft, 2)
		return m.resizeVisiblePanesCmd()
	case event.MatchString("J"), event.MatchString("shift+j"):
		m.resizeActivePane(DirectionDown, 2)
		return m.resizeVisiblePanesCmd()
	case event.MatchString("K"), event.MatchString("shift+k"):
		m.resizeActivePane(DirectionUp, 2)
		return m.resizeVisiblePanesCmd()
	case event.MatchString("L"), event.MatchString("shift+l"):
		m.resizeActivePane(DirectionRight, 2)
		return m.resizeVisiblePanesCmd()
	case event.MatchString("space"):
		m.cycleActiveLayout()
		return m.resizeVisiblePanesCmd()
	case event.MatchString(","):
		m.beginRenameTab()
		return nil
	case event.MatchString("f"):
		return m.openTerminalPickerCmd()
	case event.MatchString("s"):
		return m.openWorkspacePickerCmd()
	case event.MatchString("w"):
		return m.openFloatingTerminalPickerCmd(m.workspace.ActiveTab)
	case event.MatchString("W"), event.MatchString("shift+w"):
		m.toggleFloatingLayerVisibility()
		return nil
	case event.MatchString("tab"):
		m.cycleFloatingFocus()
		return nil
	case event.MatchString("]"):
		m.raiseActiveFloatingPane()
		return nil
	case event.MatchString("_"):
		m.lowerActiveFloatingPane()
		return nil
	case event.MatchString(":"):
		m.beginCommandPrompt()
		return nil
	case event.MatchString("x"):
		return m.closeActivePaneCmd()
	case event.MatchString("X"), event.MatchString("shift+x"):
		return m.killActiveTerminalCmd()
	case event.MatchString("M"), event.MatchString("shift+m"):
		m.toggleActiveViewportMode()
		return m.resizeVisiblePanesCmd()
	case event.MatchString("P"), event.MatchString("shift+p"):
		m.toggleActiveViewportPin()
		return nil
	case event.MatchString("R"), event.MatchString("shift+r"):
		m.toggleActiveViewportReadonly()
		return nil
	case event.MatchString("ctrl+h"), event.MatchString("ctrl+left"):
		m.panActiveViewport(-4, 0)
		return nil
	case event.MatchString("ctrl+j"), event.MatchString("ctrl+down"):
		m.panActiveViewport(0, 2)
		return nil
	case event.MatchString("ctrl+k"), event.MatchString("ctrl+up"):
		m.panActiveViewport(0, -2)
		return nil
	case event.MatchString("ctrl+l"), event.MatchString("ctrl+right"):
		m.panActiveViewport(4, 0)
		return nil
	case event.MatchString("&"):
		return m.killActiveTabCmd()
	case event.MatchString("d"):
		m.quitting = true
		m.invalidateRender()
		return tea.Quit
	case event.MatchString("?"):
		m.showHelp = true
		m.invalidateRender()
	default:
		for i := 1; i <= 9; i++ {
			if event.MatchString(string(rune('0' + i))) {
				if i-1 < len(m.workspace.Tabs) {
					return m.activateTab(i - 1)
				}
				return nil
			}
		}
	}
	return nil
}

func (m *Model) handleActivePrefixEvent(event uv.KeyPressEvent) tea.Cmd {
	result := m.dispatchPrefixEvent(event)
	if !result.keep {
		m.clearPrefixState()
		m.invalidateRender()
		return result.cmd
	}
	m.invalidateRender()
	if result.rearm {
		return tea.Batch(result.cmd, m.armPrefixTimeout())
	}
	return result.cmd
}

func (m *Model) dispatchPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	switch m.prefixMode {
	case prefixModePane:
		return m.dispatchPaneModeEvent(event)
	case prefixModeResize:
		return m.dispatchResizeModeEvent(event)
	case prefixModeTab:
		return m.dispatchTabSubPrefixEvent(event)
	case prefixModeWorkspace:
		return m.dispatchWorkspaceSubPrefixEvent(event)
	case prefixModeViewport:
		return m.dispatchViewportSubPrefixEvent(event)
	case prefixModeFloating:
		return m.dispatchFloatingModeEvent(event)
	case prefixModeOffsetPan:
		return m.dispatchOffsetPanModeEvent(event)
	case prefixModeGlobal:
		return m.dispatchGlobalModeEvent(event)
	default:
		switch {
		case event.MatchString("t"):
			return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeTab, prefixFallbackNone), keep: true}
		case event.MatchString("v"):
			return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeViewport, prefixFallbackNone), keep: true}
		case event.MatchString("o"):
			return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeFloating, prefixFallbackNone), keep: true}
		case event.MatchString("w"):
			return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeWorkspace, prefixFallbackFloatingCreate), keep: true}
		default:
			cmd := m.handlePrefixEvent(event)
			keep := shouldKeepPrefixEvent(event)
			return prefixDispatchResult{cmd: cmd, keep: keep, rearm: keep}
		}
	}
}

func (m *Model) modeEventResult(cmd tea.Cmd, keep bool) prefixDispatchResult {
	if m.directMode {
		return prefixDispatchResult{cmd: cmd, keep: true}
	}
	return prefixDispatchResult{cmd: cmd, keep: keep, rearm: keep}
}

func (m *Model) dispatchPaneModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	if event.MatchString("esc") {
		return prefixDispatchResult{}
	}
	cmd := m.handlePrefixEvent(event)
	return m.modeEventResult(cmd, shouldKeepPrefixEvent(event))
}

func (m *Model) dispatchResizeModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	if event.MatchString("esc") {
		return prefixDispatchResult{}
	}
	switch {
	case event.MatchString("h"), event.MatchString("left"):
		m.resizeActivePane(DirectionLeft, 2)
	case event.MatchString("j"), event.MatchString("down"):
		m.resizeActivePane(DirectionDown, 2)
	case event.MatchString("k"), event.MatchString("up"):
		m.resizeActivePane(DirectionUp, 2)
	case event.MatchString("l"), event.MatchString("right"):
		m.resizeActivePane(DirectionRight, 2)
	case event.MatchString("H"), event.MatchString("shift+h"):
		m.resizeActivePane(DirectionLeft, 4)
	case event.MatchString("J"), event.MatchString("shift+j"):
		m.resizeActivePane(DirectionDown, 4)
	case event.MatchString("K"), event.MatchString("shift+k"):
		m.resizeActivePane(DirectionUp, 4)
	case event.MatchString("L"), event.MatchString("shift+l"):
		m.resizeActivePane(DirectionRight, 4)
	case event.MatchString("a"):
		return prefixDispatchResult{cmd: m.acquireActivePaneResizeCmd(), keep: true}
	case event.MatchString("="):
		if tab := m.currentTab(); tab != nil && tab.Root != nil {
			resetLayoutRatios(tab.Root)
		}
	case event.MatchString("space"):
		m.cycleActiveLayout()
	default:
		return prefixDispatchResult{keep: true}
	}
	return prefixDispatchResult{cmd: m.resizeVisiblePanesCmd(), keep: true}
}

func (m *Model) dispatchTabSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	switch {
	case m.directMode && event.MatchString("esc"):
		return prefixDispatchResult{}
	case event.MatchString("c"):
		return m.modeEventResult(m.openNewTabTerminalPickerCmd(), false)
	case event.MatchString(","):
		m.beginRenameTab()
		return m.modeEventResult(nil, false)
	case event.MatchString("r"):
		m.beginRenameTab()
		return m.modeEventResult(nil, false)
	case event.MatchString("n"):
		if len(m.workspace.Tabs) > 0 {
			next := (m.workspace.ActiveTab + 1) % len(m.workspace.Tabs)
			return m.modeEventResult(m.activateTab(next), false)
		}
		return m.modeEventResult(nil, false)
	case event.MatchString("p"):
		if len(m.workspace.Tabs) > 0 {
			next := (m.workspace.ActiveTab - 1 + len(m.workspace.Tabs)) % len(m.workspace.Tabs)
			return m.modeEventResult(m.activateTab(next), false)
		}
		return m.modeEventResult(nil, false)
	case event.MatchString("f"):
		return m.modeEventResult(m.openTerminalPickerCmd(), false)
	case event.MatchString("x"):
		return m.modeEventResult(m.killActiveTabCmd(), false)
	default:
		if m.directMode {
			for i := 1; i <= 9; i++ {
				if event.MatchString(string(rune('0' + i))) {
					if i-1 < len(m.workspace.Tabs) {
						return m.modeEventResult(m.activateTab(i-1), false)
					}
					return m.modeEventResult(nil, false)
				}
			}
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchWorkspaceSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	switch {
	case m.directMode && event.MatchString("esc"):
		return prefixDispatchResult{}
	case event.MatchString("s"):
		return m.modeEventResult(m.openWorkspacePickerCmd(), false)
	case event.MatchString("c"):
		return m.modeEventResult(m.createWorkspaceCmd(nextWorkspaceName(m.workspaceOrder)), false)
	case event.MatchString("r"):
		m.beginRenameWorkspace()
		return m.modeEventResult(nil, false)
	case event.MatchString("x"):
		return m.modeEventResult(m.deleteCurrentWorkspaceCmd(), false)
	case event.MatchString("n"):
		return m.modeEventResult(m.activateWorkspaceByOffset(1), false)
	case event.MatchString("p"):
		return m.modeEventResult(m.activateWorkspaceByOffset(-1), false)
	case event.MatchString("f"):
		return m.modeEventResult(m.openWorkspacePickerCmd(), false)
	default:
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchViewportSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	switch {
	case m.directMode && event.MatchString("esc"):
		return prefixDispatchResult{}
	case event.MatchString("m"):
		m.toggleActiveViewportMode()
		return m.modeEventResult(m.resizeVisiblePanesCmd(), false)
	case event.MatchString("r"):
		m.toggleActiveViewportReadonly()
		return m.modeEventResult(nil, false)
	case event.MatchString("p"):
		m.toggleActiveViewportPin()
		return m.modeEventResult(nil, false)
	case event.MatchString("h"), event.MatchString("left"):
		m.panActiveViewport(-4, 0)
		return m.modeEventResult(nil, false)
	case event.MatchString("j"), event.MatchString("down"):
		m.panActiveViewport(0, 2)
		return m.modeEventResult(nil, false)
	case event.MatchString("k"), event.MatchString("up"):
		m.panActiveViewport(0, -2)
		return m.modeEventResult(nil, false)
	case event.MatchString("l"), event.MatchString("right"):
		m.panActiveViewport(4, 0)
		return m.modeEventResult(nil, false)
	case event.MatchString("0"), event.MatchString("g"):
		m.setActiveViewportOffset(0, 0)
		return m.modeEventResult(nil, false)
	case event.MatchString("$"):
		m.setActiveViewportOffset(int(^uint(0)>>1), 0)
		return m.modeEventResult(nil, false)
	case event.MatchString("G"), event.MatchString("shift+g"):
		m.setActiveViewportOffset(0, int(^uint(0)>>1))
		return m.modeEventResult(nil, false)
	case event.MatchString("z"):
		pane := activePane(m.currentTab())
		if pane != nil {
			pane.Offset = Point{}
			pane.Pin = false
			pane.Mode = ViewportModeFit
		}
		return m.modeEventResult(m.resizeVisiblePanesCmd(), false)
	case event.MatchString("o"):
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{cmd: m.enterPrefixMode(prefixModeOffsetPan, prefixFallbackNone), keep: true}
	default:
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchFloatingModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	switch {
	case event.MatchString("esc"):
		_ = m.focusTiledPane()
		return prefixDispatchResult{}
	case event.MatchString("tab"):
		m.cycleFloatingFocus()
		return m.modeEventResult(nil, true)
	case event.MatchString("n"):
		return m.modeEventResult(m.openFloatingTerminalPickerCmd(m.workspace.ActiveTab), false)
	case event.MatchString("x"):
		return m.modeEventResult(m.closeActivePaneCmd(), true)
	case event.MatchString("v"):
		m.toggleFloatingLayerVisibility()
		return m.modeEventResult(nil, true)
	case event.MatchString("]"):
		m.raiseActiveFloatingPane()
		return m.modeEventResult(nil, true)
	case event.MatchString("["):
		m.lowerActiveFloatingPane()
		return m.modeEventResult(nil, true)
	case event.MatchString("h"), event.MatchString("left"):
		m.moveActiveFloatingPane(-4, 0)
		return m.modeEventResult(nil, true)
	case event.MatchString("j"), event.MatchString("down"):
		m.moveActiveFloatingPane(0, 2)
		return m.modeEventResult(nil, true)
	case event.MatchString("k"), event.MatchString("up"):
		m.moveActiveFloatingPane(0, -2)
		return m.modeEventResult(nil, true)
	case event.MatchString("l"), event.MatchString("right"):
		m.moveActiveFloatingPane(4, 0)
		return m.modeEventResult(nil, true)
	case event.MatchString("H"), event.MatchString("shift+h"):
		m.resizeActiveFloatingPane(-4, 0)
		return m.modeEventResult(nil, true)
	case event.MatchString("J"), event.MatchString("shift+j"):
		m.resizeActiveFloatingPane(0, 2)
		return m.modeEventResult(nil, true)
	case event.MatchString("K"), event.MatchString("shift+k"):
		m.resizeActiveFloatingPane(0, -2)
		return m.modeEventResult(nil, true)
	case event.MatchString("L"), event.MatchString("shift+l"):
		m.resizeActiveFloatingPane(4, 0)
		return m.modeEventResult(nil, true)
	case event.MatchString("f"):
		return m.modeEventResult(m.openTerminalPickerCmd(), false)
	default:
		if m.directMode {
			return prefixDispatchResult{keep: true}
		}
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchOffsetPanModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	switch {
	case event.MatchString("esc"):
		return prefixDispatchResult{}
	case event.MatchString("h"), event.MatchString("left"):
		m.panActiveViewport(-4, 0)
		return prefixDispatchResult{keep: true}
	case event.MatchString("j"), event.MatchString("down"):
		m.panActiveViewport(0, 2)
		return prefixDispatchResult{keep: true}
	case event.MatchString("k"), event.MatchString("up"):
		m.panActiveViewport(0, -2)
		return prefixDispatchResult{keep: true}
	case event.MatchString("l"), event.MatchString("right"):
		m.panActiveViewport(4, 0)
		return prefixDispatchResult{keep: true}
	case event.MatchString("0"), event.MatchString("g"):
		m.setActiveViewportOffset(0, 0)
		return prefixDispatchResult{keep: true}
	case event.MatchString("$"):
		m.setActiveViewportOffset(int(^uint(0)>>1), 0)
		return prefixDispatchResult{keep: true}
	case event.MatchString("G"), event.MatchString("shift+g"):
		m.setActiveViewportOffset(0, int(^uint(0)>>1))
		return prefixDispatchResult{keep: true}
	default:
		return prefixDispatchResult{}
	}
}

func (m *Model) dispatchGlobalModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	switch {
	case event.MatchString("esc"):
		return prefixDispatchResult{}
	case event.MatchString("?"):
		m.showHelp = true
		m.invalidateRender()
		return prefixDispatchResult{}
	case event.MatchString(":"):
		m.beginCommandPrompt()
		return prefixDispatchResult{}
	case event.MatchString("d"), event.MatchString("q"):
		m.quitting = true
		m.invalidateRender()
		return prefixDispatchResult{cmd: tea.Quit}
	default:
		return prefixDispatchResult{keep: true}
	}
}

func shouldKeepPrefixEvent(event uv.KeyPressEvent) bool {
	switch {
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("h") || event.MatchString("left") || event.MatchString("j") || event.MatchString("down") || event.MatchString("k") || event.MatchString("up") || event.MatchString("l") || event.MatchString("right")):
		return true
	case event.Mod&uv.ModAlt != 0 && (event.MatchString("H") || event.MatchString("shift+h") || event.MatchString("J") || event.MatchString("shift+j") || event.MatchString("K") || event.MatchString("shift+k") || event.MatchString("L") || event.MatchString("shift+l")):
		return true
	case event.MatchString("h"), event.MatchString("left"),
		event.MatchString("j"), event.MatchString("down"),
		event.MatchString("k"), event.MatchString("up"),
		event.MatchString("l"), event.MatchString("right"),
		event.MatchString("H"), event.MatchString("shift+h"),
		event.MatchString("J"), event.MatchString("shift+j"),
		event.MatchString("K"), event.MatchString("shift+k"),
		event.MatchString("L"), event.MatchString("shift+l"),
		event.MatchString("ctrl+h"), event.MatchString("ctrl+left"),
		event.MatchString("ctrl+j"), event.MatchString("ctrl+down"),
		event.MatchString("ctrl+k"), event.MatchString("ctrl+up"),
		event.MatchString("ctrl+l"), event.MatchString("ctrl+right"):
		return true
	default:
		return false
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
		m.resizeFloatingPaneTo(tab, m.mouseDragPaneID, x-entry.Rect.X+1, y-entry.Rect.Y+1)
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

func nextTabName(tabs []*Tab) string {
	return itoa(len(tabs) + 1)
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
			m.deletePromptRune()
		case event.Text != "":
			m.appendPrompt(event.Text)
		}
	case uv.PasteEvent:
		m.appendPrompt(event.Content)
	}
	return nil
}

func startInputForwarder(program *tea.Program, input io.Reader) (func(), func() error, error) {
	if input == nil {
		return func() {}, func() error { return nil }, nil
	}

	restore := func() error { return nil }
	if file, ok := input.(interface{ Fd() uintptr }); ok && xterm.IsTerminal(int(file.Fd())) {
		state, err := xterm.MakeRaw(int(file.Fd()))
		if err != nil {
			return nil, nil, err
		}
		restore = func() error {
			return xterm.Restore(int(file.Fd()), state)
		}
	}

	reader, err := cancelreader.NewReader(input)
	if err != nil {
		_ = restore()
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	eventc := make(chan uv.Event, 128)
	terminalReader := uv.NewTerminalReader(reader, os.Getenv("TERM"))
	go func() {
		defer close(done)
		streamDone := make(chan struct{})
		go func() {
			defer close(streamDone)
			_ = terminalReader.StreamEvents(ctx, eventc)
			close(eventc)
		}()
		for {
			select {
			case <-ctx.Done():
				<-streamDone
				return
			case event, ok := <-eventc:
				if !ok {
					<-streamDone
					return
				}
				program.Send(inputEventMsg{event: event})
			}
		}
	}()

	stop := func() {
		cancel()
		if reader.Cancel() {
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
			}
		}
		_ = reader.Close()
	}

	return stop, restore, nil
}

func encodeKeyForPane(pane *Pane, event uv.KeyPressEvent) []byte {
	if pane == nil {
		return nil
	}
	if data, ok := encodePrintableKey(event); ok {
		return data
	}
	emu := charmvt.NewSafeEmulator(1, 1)
	applyPaneModesToEncoder(emu, pane)
	return withEncoderBytes(emu, func() {
		emu.SendKey(event)
	})
}

func encodePrintableKey(event uv.KeyPressEvent) ([]byte, bool) {
	if event.Text == "" {
		return nil, false
	}
	if event.Mod&(uv.ModCtrl|uv.ModMeta|uv.ModHyper|uv.ModSuper) != 0 {
		return nil, false
	}

	size := len(event.Text)
	if event.Mod&uv.ModAlt != 0 {
		size++
	}
	data := make([]byte, 0, size)
	if event.Mod&uv.ModAlt != 0 {
		data = append(data, 0x1b)
	}
	data = append(data, event.Text...)
	return data, true
}

func encodePasteForPane(pane *Pane, text string) []byte {
	if pane == nil || text == "" {
		return nil
	}
	emu := charmvt.NewSafeEmulator(1, 1)
	applyPaneModesToEncoder(emu, pane)
	return withEncoderBytes(emu, func() {
		emu.Paste(text)
	})
}

func applyPaneModesToEncoder(emu *charmvt.SafeEmulator, pane *Pane) {
	modes := localModes(pane)
	if modes.ApplicationCursor {
		_, _ = emu.Write([]byte("\x1b[?1h"))
	}
	if modes.BracketedPaste {
		_, _ = emu.Write([]byte("\x1b[?2004h"))
	}
}

func localModes(pane *Pane) localvterm.TerminalModes {
	if pane == nil {
		return localvterm.TerminalModes{}
	}
	if pane.live && pane.VTerm != nil {
		return pane.VTerm.Modes()
	}
	if pane.Snapshot != nil {
		return localvterm.TerminalModes{
			AlternateScreen:   pane.Snapshot.Modes.AlternateScreen,
			MouseTracking:     pane.Snapshot.Modes.MouseTracking,
			BracketedPaste:    pane.Snapshot.Modes.BracketedPaste,
			ApplicationCursor: pane.Snapshot.Modes.ApplicationCursor,
			AutoWrap:          pane.Snapshot.Modes.AutoWrap,
		}
	}
	return localvterm.TerminalModes{}
}

func withEncoderBytes(emu *charmvt.SafeEmulator, fn func()) []byte {
	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(emu)
		done <- data
	}()
	fn()
	_ = emu.Close()
	data := <-done
	if len(data) == 0 {
		return nil
	}
	return copyBytes(data)
}
