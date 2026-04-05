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
		{name: "enter", msg: tea.KeyMsg{Type: tea.KeyEnter}, want: "\r"},
		{name: "alt-enter", msg: tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, want: "\x1b\r"},
		{name: "ctrl-m", msg: tea.KeyMsg{Type: tea.KeyCtrlM}, want: "\r"},
		{name: "ctrl-j", msg: tea.KeyMsg{Type: tea.KeyCtrlJ}, want: "\n"},
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

func TestEncodeSGR1006Mouse(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.MouseMsg
		col  int
		row  int
		want string
	}{
		{
			name: "left-press",
			msg: tea.MouseMsg{
				Action: tea.MouseActionPress,
				Button: tea.MouseButtonLeft,
			},
			col:  4,
			row:  2,
			want: "\x1b[<0;4;2M",
		},
		{
			name: "left-motion",
			msg: tea.MouseMsg{
				Action: tea.MouseActionMotion,
				Button: tea.MouseButtonLeft,
			},
			col:  9,
			row:  3,
			want: "\x1b[<32;9;3M",
		},
		{
			name: "left-release",
			msg: tea.MouseMsg{
				Action: tea.MouseActionRelease,
				Button: tea.MouseButtonLeft,
			},
			col:  9,
			row:  3,
			want: "\x1b[<3;9;3m",
		},
		{
			name: "middle-press",
			msg: tea.MouseMsg{
				Action: tea.MouseActionPress,
				Button: tea.MouseButtonMiddle,
			},
			col:  2,
			row:  5,
			want: "\x1b[<1;2;5M",
		},
		{
			name: "right-motion",
			msg: tea.MouseMsg{
				Action: tea.MouseActionMotion,
				Button: tea.MouseButtonRight,
			},
			col:  3,
			row:  4,
			want: "\x1b[<34;3;4M",
		},
		{
			name: "middle-release",
			msg: tea.MouseMsg{
				Action: tea.MouseActionRelease,
				Button: tea.MouseButtonMiddle,
			},
			col:  2,
			row:  5,
			want: "\x1b[<3;2;5m",
		},
		{
			name: "wheel-up-with-mods",
			msg: tea.MouseMsg{
				Action: tea.MouseActionPress,
				Button: tea.MouseButtonWheelUp,
				Shift:  true,
				Alt:    true,
			},
			col:  1,
			row:  1,
			want: "\x1b[<76;1;1M",
		},
		{
			name: "wheel-down",
			msg: tea.MouseMsg{
				Action: tea.MouseActionPress,
				Button: tea.MouseButtonWheelDown,
			},
			col:  8,
			row:  7,
			want: "\x1b[<65;8;7M",
		},
		{
			name: "wheel-right",
			msg: tea.MouseMsg{
				Action: tea.MouseActionPress,
				Button: tea.MouseButtonWheelRight,
			},
			col:  6,
			row:  2,
			want: "\x1b[<67;6;2M",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(encodeSGR1006Mouse(tc.msg, tc.col, tc.row)); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
