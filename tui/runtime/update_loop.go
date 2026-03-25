package runtime

import "github.com/lozzow/termx/protocol"

type UpdateMessage struct {
	Event protocol.Event
}

type UpdateLoop struct {
	events <-chan protocol.Event
}

func NewUpdateLoop(events <-chan protocol.Event) *UpdateLoop {
	return &UpdateLoop{events: events}
}

func (l *UpdateLoop) Next() (UpdateMessage, bool) {
	select {
	case event, ok := <-l.events:
		if !ok {
			return UpdateMessage{}, false
		}
		return UpdateMessage{Event: event}, true
	default:
		return UpdateMessage{}, false
	}
}
