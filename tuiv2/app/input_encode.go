package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/vterm"
)

type terminalModifiers struct {
	shift bool
	alt   bool
	ctrl  bool
}

func (m *Model) encodeActiveTerminalInput(msg tea.KeyMsg, paneID string) []byte {
	if m == nil {
		return nil
	}
	pane := m.activePaneForInput(paneID)
	if pane == nil {
		return nil
	}
	modes := m.terminalModesForPane(pane)
	if msg.Paste {
		return encodeTerminalPaste(string(msg.Runes), modes)
	}
	return encodeTerminalKeyMsg(msg, modes)
}

func (m *Model) activePaneForInput(paneID string) *workbench.PaneState {
	if m == nil || m.workbench == nil {
		return nil
	}
	pane := m.workbench.ActivePane()
	if pane == nil {
		return nil
	}
	if paneID == "" || pane.ID == paneID {
		return pane
	}
	return nil
}

func (m *Model) terminalModesForPane(pane *workbench.PaneState) localvterm.TerminalModes {
	if m == nil || m.runtime == nil || pane == nil || pane.TerminalID == "" {
		return localvterm.TerminalModes{}
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return localvterm.TerminalModes{}
	}
	if terminal.VTerm != nil {
		return terminal.VTerm.Modes()
	}
	if terminal.Snapshot != nil {
		return localvterm.TerminalModes{
			AlternateScreen:   terminal.Snapshot.Modes.AlternateScreen,
			MouseTracking:     terminal.Snapshot.Modes.MouseTracking,
			BracketedPaste:    terminal.Snapshot.Modes.BracketedPaste,
			ApplicationCursor: terminal.Snapshot.Modes.ApplicationCursor,
			AutoWrap:          terminal.Snapshot.Modes.AutoWrap,
		}
	}
	return localvterm.TerminalModes{}
}

func encodeTerminalKeyMsg(msg tea.KeyMsg, modes localvterm.TerminalModes) []byte {
	if data, ok := encodePrintableTeaKey(msg); ok {
		return data
	}
	if data, ok := encodeControlTeaKey(msg); ok {
		return data
	}
	return encodeSpecialTeaKey(msg, modes)
}

func encodePrintableTeaKey(msg tea.KeyMsg) ([]byte, bool) {
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return nil, false
		}
		return prependEscape(msg.Alt, []byte(string(msg.Runes))), true
	case tea.KeySpace:
		return prependEscape(msg.Alt, []byte{' '}), true
	default:
		return nil, false
	}
}

func encodeControlTeaKey(msg tea.KeyMsg) ([]byte, bool) {
	if (msg.Type >= 0 && msg.Type <= 31) || msg.Type == 127 {
		return prependEscape(msg.Alt, []byte{byte(msg.Type)}), true
	}
	return nil, false
}

func encodeTerminalPaste(text string, modes localvterm.TerminalModes) []byte {
	if text == "" {
		return nil
	}
	data := []byte(text)
	if !modes.BracketedPaste {
		return data
	}
	out := make([]byte, 0, len(data)+12)
	out = append(out, []byte("\x1b[200~")...)
	out = append(out, data...)
	out = append(out, []byte("\x1b[201~")...)
	return out
}

func encodeSpecialTeaKey(msg tea.KeyMsg, modes localvterm.TerminalModes) []byte {
	if final, mods, ok := arrowKeySpec(msg); ok {
		return encodeCursorKey(final, mods, modes.ApplicationCursor)
	}
	if final, mods, ok := homeEndKeySpec(msg); ok {
		return encodeCursorKey(final, mods, modes.ApplicationCursor)
	}
	if code, mods, ok := tildeKeySpec(msg); ok {
		return encodeTildeKey(code, mods)
	}
	if number, mods, ok := functionKeySpec(msg); ok {
		return encodeFunctionKey(number, mods)
	}
	return nil
}

func (m *Model) encodeTerminalMouseInput(msg tea.MouseMsg, paneID string, contentRect workbench.Rect) []byte {
	if m == nil {
		return nil
	}
	pane := m.activePaneForInput(paneID)
	if pane == nil || pane.TerminalID == "" {
		return nil
	}
	modes := m.terminalModesForPane(pane)
	if !modes.MouseTracking {
		return nil
	}
	col := msg.X - contentRect.X + 1
	row := msg.Y - contentRect.Y + 1
	if col < 1 || row < 1 || col > contentRect.W || row > contentRect.H {
		return nil
	}
	return encodeSGR1006Mouse(msg, col, row)
}

func encodeSGR1006Mouse(msg tea.MouseMsg, col, row int) []byte {
	if col < 1 || row < 1 {
		return nil
	}
	mods := sgrMouseModifierBits(msg)
	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonLeft:
			return encodeSGRMouseSequence(mods, col, row, false)
		case tea.MouseButtonWheelUp:
			return encodeSGRMouseSequence(64+mods, col, row, false)
		case tea.MouseButtonWheelDown:
			return encodeSGRMouseSequence(65+mods, col, row, false)
		default:
			return nil
		}
	case tea.MouseActionMotion:
		if msg.Button != tea.MouseButtonLeft {
			return nil
		}
		return encodeSGRMouseSequence(32+mods, col, row, false)
	case tea.MouseActionRelease:
		if msg.Button != tea.MouseButtonLeft {
			return nil
		}
		return encodeSGRMouseSequence(3+mods, col, row, true)
	default:
		return nil
	}
}

func encodeSGRMouseSequence(code, col, row int, release bool) []byte {
	final := "M"
	if release {
		final = "m"
	}
	return []byte("\x1b[<" + itoa(code) + ";" + itoa(col) + ";" + itoa(row) + final)
}

func sgrMouseModifierBits(msg tea.MouseMsg) int {
	mods := 0
	if msg.Shift {
		mods += 4
	}
	if msg.Alt {
		mods += 8
	}
	if msg.Ctrl {
		mods += 16
	}
	return mods
}

func arrowKeySpec(msg tea.KeyMsg) (byte, terminalModifiers, bool) {
	switch msg.Type {
	case tea.KeyUp:
		return 'A', terminalModifiers{alt: msg.Alt}, true
	case tea.KeyDown:
		return 'B', terminalModifiers{alt: msg.Alt}, true
	case tea.KeyRight:
		return 'C', terminalModifiers{alt: msg.Alt}, true
	case tea.KeyLeft:
		return 'D', terminalModifiers{alt: msg.Alt}, true
	case tea.KeyShiftUp:
		return 'A', terminalModifiers{shift: true, alt: msg.Alt}, true
	case tea.KeyShiftDown:
		return 'B', terminalModifiers{shift: true, alt: msg.Alt}, true
	case tea.KeyShiftRight:
		return 'C', terminalModifiers{shift: true, alt: msg.Alt}, true
	case tea.KeyShiftLeft:
		return 'D', terminalModifiers{shift: true, alt: msg.Alt}, true
	case tea.KeyCtrlUp:
		return 'A', terminalModifiers{ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlDown:
		return 'B', terminalModifiers{ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlRight:
		return 'C', terminalModifiers{ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlLeft:
		return 'D', terminalModifiers{ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlShiftUp:
		return 'A', terminalModifiers{shift: true, ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlShiftDown:
		return 'B', terminalModifiers{shift: true, ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlShiftRight:
		return 'C', terminalModifiers{shift: true, ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlShiftLeft:
		return 'D', terminalModifiers{shift: true, ctrl: true, alt: msg.Alt}, true
	default:
		return 0, terminalModifiers{}, false
	}
}

func homeEndKeySpec(msg tea.KeyMsg) (byte, terminalModifiers, bool) {
	switch msg.Type {
	case tea.KeyHome:
		return 'H', terminalModifiers{alt: msg.Alt}, true
	case tea.KeyEnd:
		return 'F', terminalModifiers{alt: msg.Alt}, true
	case tea.KeyShiftHome:
		return 'H', terminalModifiers{shift: true, alt: msg.Alt}, true
	case tea.KeyShiftEnd:
		return 'F', terminalModifiers{shift: true, alt: msg.Alt}, true
	case tea.KeyCtrlHome:
		return 'H', terminalModifiers{ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlEnd:
		return 'F', terminalModifiers{ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlShiftHome:
		return 'H', terminalModifiers{shift: true, ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlShiftEnd:
		return 'F', terminalModifiers{shift: true, ctrl: true, alt: msg.Alt}, true
	default:
		return 0, terminalModifiers{}, false
	}
}

func tildeKeySpec(msg tea.KeyMsg) (int, terminalModifiers, bool) {
	switch msg.Type {
	case tea.KeyShiftTab:
		return -1, terminalModifiers{shift: true, alt: msg.Alt}, true
	case tea.KeyInsert:
		return 2, terminalModifiers{alt: msg.Alt}, true
	case tea.KeyDelete:
		return 3, terminalModifiers{alt: msg.Alt}, true
	case tea.KeyPgUp:
		return 5, terminalModifiers{alt: msg.Alt}, true
	case tea.KeyPgDown:
		return 6, terminalModifiers{alt: msg.Alt}, true
	case tea.KeyCtrlPgUp:
		return 5, terminalModifiers{ctrl: true, alt: msg.Alt}, true
	case tea.KeyCtrlPgDown:
		return 6, terminalModifiers{ctrl: true, alt: msg.Alt}, true
	default:
		return 0, terminalModifiers{}, false
	}
}

func functionKeySpec(msg tea.KeyMsg) (int, terminalModifiers, bool) {
	switch {
	case msg.Type <= tea.KeyF1 && msg.Type >= tea.KeyF20:
		return int(tea.KeyF1-msg.Type) + 1, terminalModifiers{alt: msg.Alt}, true
	default:
		return 0, terminalModifiers{}, false
	}
}

func encodeCursorKey(final byte, mods terminalModifiers, applicationCursor bool) []byte {
	if mods.parameter() == 1 {
		if applicationCursor {
			return []byte{0x1b, 'O', final}
		}
		return []byte{0x1b, '[', final}
	}
	return encodeCSIWithModifier(1, mods.parameter(), final)
}

func encodeTildeKey(code int, mods terminalModifiers) []byte {
	if code == -1 {
		if mods.parameter() == 2 {
			return []byte("\x1b[Z")
		}
		return encodeCSIWithModifier(1, mods.parameter(), 'Z')
	}
	if mods.parameter() == 1 {
		return []byte("\x1b[" + itoa(code) + "~")
	}
	return []byte("\x1b[" + itoa(code) + ";" + itoa(mods.parameter()) + "~")
}

func encodeFunctionKey(number int, mods terminalModifiers) []byte {
	if number >= 1 && number <= 4 {
		final := byte('P' + number - 1)
		if mods.parameter() == 1 {
			return []byte{0x1b, 'O', final}
		}
		return encodeCSIWithModifier(1, mods.parameter(), final)
	}
	code, ok := functionTildeCode(number)
	if !ok {
		return nil
	}
	return encodeTildeKey(code, mods)
}

func functionTildeCode(number int) (int, bool) {
	switch number {
	case 5:
		return 15, true
	case 6:
		return 17, true
	case 7:
		return 18, true
	case 8:
		return 19, true
	case 9:
		return 20, true
	case 10:
		return 21, true
	case 11:
		return 23, true
	case 12:
		return 24, true
	case 13:
		return 25, true
	case 14:
		return 26, true
	case 15:
		return 28, true
	case 16:
		return 29, true
	case 17:
		return 31, true
	case 18:
		return 32, true
	case 19:
		return 33, true
	case 20:
		return 34, true
	default:
		return 0, false
	}
}

func encodeCSIWithModifier(prefix, modifier int, final byte) []byte {
	return []byte("\x1b[" + itoa(prefix) + ";" + itoa(modifier) + string(final))
}

func prependEscape(enabled bool, data []byte) []byte {
	if !enabled || len(data) == 0 {
		return data
	}
	out := make([]byte, 0, len(data)+1)
	out = append(out, 0x1b)
	out = append(out, data...)
	return out
}

func (m terminalModifiers) parameter() int {
	param := 1
	if m.shift {
		param++
	}
	if m.alt {
		param += 2
	}
	if m.ctrl {
		param += 4
	}
	return param
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	if v < 0 {
		return "-" + itoa(-v)
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
