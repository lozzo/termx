package input

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
)

type Context struct {
	Screen app.Screen
}

type Router struct{}

func NewRouter() Router {
	return Router{}
}

func (Router) Translate(ctx Context, msg tea.KeyMsg) any {
	switch ctx.Screen {
	case app.ScreenWorkbench:
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'p' {
			return app.IntentOpenTerminalPool
		}
	case app.ScreenTerminalPool:
		if msg.Type == tea.KeyEsc {
			return app.IntentCloseScreen
		}
	}
	return nil
}
