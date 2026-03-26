package runtime

import (
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/core/types"
)

type EventAdapter struct{}

func (EventAdapter) Normalize(evt protocol.Event) app.Message {
	switch evt.Type {
	case protocol.EventTerminalStateChanged:
		if evt.StateChanged != nil && evt.StateChanged.NewState == "exited" {
			return app.MessageTerminalExited{TerminalID: types.TerminalID(evt.TerminalID)}
		}
	case protocol.EventTerminalRemoved:
		return app.MessageTerminalRemoved{TerminalID: types.TerminalID(evt.TerminalID)}
	}
	return nil
}
