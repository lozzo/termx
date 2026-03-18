package tui

import (
	"context"
	"io"
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
	if m.terminalPicker != nil {
		return m.handleTerminalPickerEvent(event)
	}

	if m.prompt != nil {
		return m.handlePromptEvent(event)
	}

	switch event := event.(type) {
	case uv.KeyPressEvent:
		return m.handleKeyPressEvent(event)
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

	if m.prefixActive {
		m.prefixActive = false
		m.prefixSeq++
		m.invalidateRender()
		return m.handlePrefixEvent(event)
	}

	if event.MatchString("ctrl+a") {
		cmd := m.activatePrefix()
		m.invalidateRender()
		return cmd
	}

	return m.sendKeyToActive(event)
}

func (m *Model) handlePrefixEvent(event uv.KeyPressEvent) tea.Cmd {
	switch {
	case event.MatchString("ctrl+a"):
		return m.sendToActive([]byte{0x01})
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
		m.workspace.Tabs = append(m.workspace.Tabs, newTab(nextTabName(m.workspace.Tabs)))
		m.workspace.ActiveTab = len(m.workspace.Tabs) - 1
		m.invalidateRender()
		return m.createPaneCmd(m.workspace.ActiveTab, "", "")
	case event.MatchString("n"):
		if len(m.workspace.Tabs) > 0 {
			m.workspace.ActiveTab = (m.workspace.ActiveTab + 1) % len(m.workspace.Tabs)
		}
		return m.resizeVisiblePanesCmd()
	case event.MatchString("p"):
		if len(m.workspace.Tabs) > 0 {
			m.workspace.ActiveTab = (m.workspace.ActiveTab - 1 + len(m.workspace.Tabs)) % len(m.workspace.Tabs)
		}
		return m.resizeVisiblePanesCmd()
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
					m.workspace.ActiveTab = i - 1
				}
				m.invalidateRender()
				return m.resizeVisiblePanesCmd()
			}
		}
	}
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
			m.prompt = nil
		case event.MatchString("enter"):
			m.commitPrompt()
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
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := reader.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				program.Send(rawInputMsg{data: data})
			}
			if err != nil {
				return
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
