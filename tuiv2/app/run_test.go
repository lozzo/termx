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
