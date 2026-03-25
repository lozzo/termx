package runtime

import (
	"context"
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
)

type UpdateMessage struct {
	Event protocol.Event
}

type UpdateLoop struct {
	events    <-chan protocol.Event
	scheduler WorkspaceSaveScheduler
}

type WorkspaceSaveScheduler interface {
	Schedule(app.Model) tea.Cmd
	Flush(context.Context, app.Model) error
}

func NewUpdateLoop(events <-chan protocol.Event, schedulers ...WorkspaceSaveScheduler) *UpdateLoop {
	loop := &UpdateLoop{events: events}
	if len(schedulers) > 0 {
		loop.scheduler = schedulers[0]
	}
	return loop
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

func (l *UpdateLoop) NextCmd() tea.Cmd {
	if l == nil || l.events == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-l.events
		if !ok {
			return nil
		}
		return UpdateMessage{Event: event}
	}
}

// ObserveModelTransition 通过“可持久化投影是否变化”判断这次更新是否影响 workspace。
// 这样不用在 reducer/runtime 两边维护一份脆弱的 mutation 白名单。
func (l *UpdateLoop) ObserveModelTransition(previous, next tea.Model) tea.Cmd {
	if l == nil || l.scheduler == nil {
		return nil
	}

	previousModel, previousOK := extractAppModel(previous)
	nextModel, nextOK := extractAppModel(next)
	if !nextOK {
		return nil
	}
	if previousOK && persistedModelFingerprint(previousModel) == persistedModelFingerprint(nextModel) {
		return nil
	}
	return l.scheduler.Schedule(nextModel)
}

func (l *UpdateLoop) Flush(ctx context.Context, model tea.Model) error {
	if l == nil || l.scheduler == nil {
		return nil
	}
	appModel, ok := extractAppModel(model)
	if !ok {
		return nil
	}
	return l.scheduler.Flush(ctx, appModel)
}

func extractAppModel(model tea.Model) (app.Model, bool) {
	switch typed := model.(type) {
	case app.Model:
		return typed, true
	case interface{ AppModel() app.Model }:
		return typed.AppModel(), true
	case interface{ UnderlyingModel() tea.Model }:
		return extractAppModel(typed.UnderlyingModel())
	default:
		return app.Model{}, false
	}
}

func persistedModelFingerprint(model app.Model) string {
	payload, err := json.Marshal(persistedWorkbenchStateFromModel(model))
	if err != nil {
		return ""
	}
	return string(payload)
}
