package tui

import (
	"context"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
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

func (m *Model) handlePrefixEvent(event uv.KeyPressEvent) tea.Cmd {
	return m.handlePrefixInput(prefixInputFromEvent(event))
}

func panePrefixActionForEvent(event uv.KeyPressEvent) panePrefixAction {
	return panePrefixActionForInput(prefixInputFromEvent(event))
}

func (m *Model) handleActivePrefixEvent(event uv.KeyPressEvent) tea.Cmd {
	return m.applyActivePrefixResult(m.dispatchPrefixEvent(event))
}

func (m *Model) dispatchPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchPrefixInput(prefixInputFromEvent(event))
}

func (m *Model) modeEventResult(cmd tea.Cmd, keep bool) prefixDispatchResult {
	if m.directMode {
		return prefixDispatchResult{cmd: cmd, keep: true}
	}
	return prefixDispatchResult{cmd: cmd, keep: keep, rearm: keep}
}

func (m *Model) dispatchPaneModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchPaneModeInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchResizeModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchResizeModeInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchTabSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchTabSubPrefixInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchWorkspaceSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchWorkspaceSubPrefixInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchViewportSubPrefixEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchViewportSubPrefixInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchFloatingModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchFloatingModeInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchOffsetPanModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchOffsetPanModeInput(prefixInputFromEvent(event))
}

func (m *Model) dispatchGlobalModeEvent(event uv.KeyPressEvent) prefixDispatchResult {
	return m.dispatchGlobalModeInput(prefixInputFromEvent(event))
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
