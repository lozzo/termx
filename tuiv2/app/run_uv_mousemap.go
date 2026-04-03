package app

import (
	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

func uvMouseEventToTeaMouseMsg(event uv.MouseEvent, action tea.MouseAction) (tea.MouseMsg, bool) {
	mouse := event.Mouse()
	button, ok := uvMouseButtonToTeaMouseButton(mouse.Button)
	if !ok {
		return tea.MouseMsg{}, false
	}
	return tea.MouseMsg{
		X:      mouse.X,
		Y:      mouse.Y,
		Shift:  mouse.Mod.Contains(uv.ModShift),
		Alt:    mouse.Mod.Contains(uv.ModAlt),
		Ctrl:   mouse.Mod.Contains(uv.ModCtrl),
		Action: action,
		Button: button,
	}, true
}

func uvMouseButtonToTeaMouseButton(button uv.MouseButton) (tea.MouseButton, bool) {
	switch button {
	case uv.MouseNone:
		return tea.MouseButtonNone, true
	case uv.MouseLeft:
		return tea.MouseButtonLeft, true
	case uv.MouseMiddle:
		return tea.MouseButtonMiddle, true
	case uv.MouseRight:
		return tea.MouseButtonRight, true
	case uv.MouseWheelUp:
		return tea.MouseButtonWheelUp, true
	case uv.MouseWheelDown:
		return tea.MouseButtonWheelDown, true
	case uv.MouseWheelLeft:
		return tea.MouseButtonWheelLeft, true
	case uv.MouseWheelRight:
		return tea.MouseButtonWheelRight, true
	case uv.MouseBackward:
		return tea.MouseButtonBackward, true
	case uv.MouseForward:
		return tea.MouseButtonForward, true
	case uv.MouseButton10:
		return tea.MouseButton10, true
	case uv.MouseButton11:
		return tea.MouseButton11, true
	default:
		return tea.MouseButtonNone, false
	}
}
