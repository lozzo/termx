package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestUVMouseEventToTeaMouseMsg(t *testing.T) {
	msg, ok := uvMouseEventToTeaMouseMsg(uv.MouseClickEvent(uv.Mouse{
		X:      12,
		Y:      7,
		Button: uv.MouseLeft,
		Mod:    uv.ModAlt | uv.ModCtrl,
	}), tea.MouseActionPress)
	if !ok {
		t.Fatal("expected mouse conversion to succeed")
	}
	if msg.X != 12 || msg.Y != 7 {
		t.Fatalf("expected coordinates 12,7 got %d,%d", msg.X, msg.Y)
	}
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		t.Fatalf("unexpected mouse mapping %#v", msg)
	}
	if !msg.Alt || !msg.Ctrl || msg.Shift {
		t.Fatalf("unexpected modifiers %#v", msg)
	}
}

func TestUVMouseEventToTeaMouseMsgRejectsUnknownButton(t *testing.T) {
	if _, ok := uvMouseEventToTeaMouseMsg(uv.MouseMotionEvent(uv.Mouse{
		X:      1,
		Y:      2,
		Button: uv.MouseButton(99),
	}), tea.MouseActionMotion); ok {
		t.Fatal("expected unsupported mouse button to be rejected")
	}
}

func TestUVKeyToTeaKeyMsgMapsShiftTab(t *testing.T) {
	msg, ok := uvKeyToTeaKeyMsg(uv.KeyPressEvent(uv.Key{Code: uv.KeyTab, Mod: uv.ModShift}))
	if !ok {
		t.Fatal("expected shift-tab conversion")
	}
	if msg.Type != tea.KeyShiftTab {
		t.Fatalf("expected KeyShiftTab, got %v", msg.Type)
	}
}

func TestUVKeyToTeaKeyMsgMapsCtrlLeft(t *testing.T) {
	msg, ok := uvKeyToTeaKeyMsg(uv.KeyPressEvent(uv.Key{Code: uv.KeyLeft, Mod: uv.ModCtrl}))
	if !ok {
		t.Fatal("expected ctrl-left conversion")
	}
	if msg.Type != tea.KeyCtrlLeft {
		t.Fatalf("expected KeyCtrlLeft, got %v", msg.Type)
	}
}

func TestUVKeyToTeaKeyMsgMapsFunctionKey(t *testing.T) {
	msg, ok := uvKeyToTeaKeyMsg(uv.KeyPressEvent(uv.Key{Code: uv.KeyF5}))
	if !ok {
		t.Fatal("expected function-key conversion")
	}
	if msg.Type != tea.KeyF5 {
		t.Fatalf("expected KeyF5, got %v", msg.Type)
	}
}
