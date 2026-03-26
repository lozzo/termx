package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

type prefixInput struct {
	token string
	ctrl  bool
	alt   bool
	shift bool
}

func prefixInputFromKey(msg tea.KeyMsg) prefixInput {
	switch msg.Type {
	case tea.KeyEsc:
		return prefixInput{token: "esc"}
	case tea.KeyTab:
		return prefixInput{token: "tab"}
	case tea.KeySpace:
		return prefixInput{token: "space"}
	case tea.KeyLeft:
		return prefixInput{token: "left"}
	case tea.KeyRight:
		return prefixInput{token: "right"}
	case tea.KeyUp:
		return prefixInput{token: "up"}
	case tea.KeyDown:
		return prefixInput{token: "down"}
	case tea.KeyCtrlLeft:
		return prefixInput{token: "ctrl+left", ctrl: true}
	case tea.KeyCtrlRight:
		return prefixInput{token: "ctrl+right", ctrl: true}
	case tea.KeyCtrlUp:
		return prefixInput{token: "ctrl+up", ctrl: true}
	case tea.KeyCtrlDown:
		return prefixInput{token: "ctrl+down", ctrl: true}
	case tea.KeyCtrlH:
		return prefixInput{token: "ctrl+h", ctrl: true}
	case tea.KeyCtrlJ:
		return prefixInput{token: "ctrl+j", ctrl: true}
	case tea.KeyCtrlK:
		return prefixInput{token: "ctrl+k", ctrl: true}
	case tea.KeyCtrlL:
		return prefixInput{token: "ctrl+l", ctrl: true}
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			return prefixInput{token: string(msg.Runes[0])}
		}
	}
	return prefixInput{}
}

func prefixInputFromEvent(event uv.KeyPressEvent) prefixInput {
	base := prefixInput{
		ctrl:  event.Mod&uv.ModCtrl != 0,
		alt:   event.Mod&uv.ModAlt != 0,
		shift: event.Mod&uv.ModShift != 0,
	}
	switch {
	case event.MatchString("esc"):
		base.token = "esc"
		return base
	case event.MatchString("tab"):
		base.token = "tab"
		return base
	case event.MatchString("space"):
		base.token = "space"
		return base
	case event.MatchString("left"):
		base.token = "left"
		return base
	case event.MatchString("right"):
		base.token = "right"
		return base
	case event.MatchString("up"):
		base.token = "up"
		return base
	case event.MatchString("down"):
		base.token = "down"
		return base
	case event.MatchString("ctrl+left"):
		base.token = "ctrl+left"
		return base
	case event.MatchString("ctrl+right"):
		base.token = "ctrl+right"
		return base
	case event.MatchString("ctrl+up"):
		base.token = "ctrl+up"
		return base
	case event.MatchString("ctrl+down"):
		base.token = "ctrl+down"
		return base
	case event.MatchString("ctrl+h"):
		base.token = "ctrl+h"
		return base
	case event.MatchString("ctrl+j"):
		base.token = "ctrl+j"
		return base
	case event.MatchString("ctrl+k"):
		base.token = "ctrl+k"
		return base
	case event.MatchString("ctrl+l"):
		base.token = "ctrl+l"
		return base
	case event.Text != "":
		base.token = event.Text
		return base
	default:
		return base
	}
}
