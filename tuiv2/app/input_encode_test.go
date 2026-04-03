package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestEncodeTerminalKeyMsgSpecialKeys(t *testing.T) {
	tests := []struct {
		name  string
		msg   tea.KeyMsg
		modes localvterm.TerminalModes
		want  string
	}{
		{name: "application-cursor-up", msg: tea.KeyMsg{Type: tea.KeyUp}, modes: localvterm.TerminalModes{ApplicationCursor: true}, want: "\x1bOA"},
		{name: "shift-tab", msg: tea.KeyMsg{Type: tea.KeyShiftTab}, want: "\x1b[Z"},
		{name: "ctrl-left", msg: tea.KeyMsg{Type: tea.KeyCtrlLeft}, want: "\x1b[1;5D"},
		{name: "alt-left", msg: tea.KeyMsg{Type: tea.KeyLeft, Alt: true}, want: "\x1b[1;3D"},
		{name: "ctrl-home", msg: tea.KeyMsg{Type: tea.KeyCtrlHome}, want: "\x1b[1;5H"},
		{name: "ctrl-shift-end", msg: tea.KeyMsg{Type: tea.KeyCtrlShiftEnd}, want: "\x1b[1;6F"},
		{name: "delete", msg: tea.KeyMsg{Type: tea.KeyDelete}, want: "\x1b[3~"},
		{name: "f5", msg: tea.KeyMsg{Type: tea.KeyF5}, want: "\x1b[15~"},
		{name: "alt-f5", msg: tea.KeyMsg{Type: tea.KeyF5, Alt: true}, want: "\x1b[15;3~"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(encodeTerminalKeyMsg(tc.msg, tc.modes)); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestEncodeTerminalKeyMsgControlAndPrintableKeys(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{name: "ctrl-c", msg: tea.KeyMsg{Type: tea.KeyCtrlC}, want: "\x03"},
		{name: "alt-ctrl-c", msg: tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, want: "\x1b\x03"},
		{name: "alt-rune", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true}, want: "\x1bx"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(encodeTerminalKeyMsg(tc.msg, localvterm.TerminalModes{})); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestEncodeTerminalPasteHonorsBracketedPaste(t *testing.T) {
	if got := string(encodeTerminalPaste("abc", localvterm.TerminalModes{})); got != "abc" {
		t.Fatalf("expected plain paste, got %q", got)
	}
	if got := string(encodeTerminalPaste("abc", localvterm.TerminalModes{BracketedPaste: true})); got != "\x1b[200~abc\x1b[201~" {
		t.Fatalf("expected bracketed paste sequence, got %q", got)
	}
}
