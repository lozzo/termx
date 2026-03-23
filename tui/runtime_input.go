package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
)

type RuntimeTerminalInputClient interface {
	Input(ctx context.Context, channel uint16, data []byte) error
}

type runtimeTerminalInputHandler struct {
	client RuntimeTerminalInputClient
	store  RuntimeTerminalStore
}

func NewRuntimeTerminalInputHandler(client RuntimeTerminalInputClient, store RuntimeTerminalStore) btui.UnmappedKeyHandler {
	if client == nil || store == nil {
		return nil
	}
	return runtimeTerminalInputHandler{
		client: client,
		store:  store,
	}
}

// HandleKey 只接管“没有命中任何 intent”的按键。
// 这样 overlay / mode 语义仍然优先走 reducer，terminal 输入只是底层兜底转发。
func (h runtimeTerminalInputHandler) HandleKey(state types.AppState, msg tea.KeyMsg) tea.Cmd {
	if state.UI.Overlay.Kind != types.OverlayNone || state.UI.Mode.Active != types.ModeNone {
		return nil
	}
	session, ok := activeTerminalSession(state, h.store)
	if !ok || session.Channel == 0 {
		return nil
	}
	data, ok := encodeTerminalInput(msg)
	if !ok {
		return nil
	}
	payload := append([]byte(nil), data...)
	return func() tea.Msg {
		if err := h.client.Input(context.Background(), session.Channel, payload); err != nil {
			return btui.FeedbackMsg{
				Notices: []btui.Notice{{
					Level: btui.NoticeLevelError,
					Text:  err.Error(),
				}},
			}
		}
		return nil
	}
}

func encodeTerminalInput(msg tea.KeyMsg) ([]byte, bool) {
	switch {
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0:
		return []byte(string(msg.Runes)), true
	case msg.String() == " ":
		return []byte(" "), true
	}
	switch msg.Type {
	case tea.KeyEnter:
		return []byte("\r"), true
	case tea.KeyTab:
		return []byte("\t"), true
	case tea.KeyBackspace, tea.KeyDelete:
		return []byte{0x7f}, true
	case tea.KeyUp:
		return []byte("\x1b[A"), true
	case tea.KeyDown:
		return []byte("\x1b[B"), true
	case tea.KeyRight:
		return []byte("\x1b[C"), true
	case tea.KeyLeft:
		return []byte("\x1b[D"), true
	case tea.KeyCtrlC:
		return []byte{0x03}, true
	case tea.KeyCtrlD:
		return []byte{0x04}, true
	default:
		return nil, false
	}
}
