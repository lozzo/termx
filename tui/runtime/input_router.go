package runtime

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state/types"
)

type terminalIOService interface {
	Input(context.Context, uint16, []byte) error
	Resize(context.Context, uint16, uint16, uint16) error
}

type InputRouter struct {
	service terminalIOService
}

func NewInputRouter(service terminalIOService) InputRouter {
	return InputRouter{service: service}
}

// HandleKey 只把键盘输入发给当前 workbench 焦点下的 live pane，避免 overlay/管理页误抢输入。
func (r InputRouter) HandleKey(ctx context.Context, model app.Model, msg tea.KeyMsg) error {
	channel, ok := activeChannel(model)
	if !ok {
		return nil
	}
	data := keyInputBytes(msg)
	if len(data) == 0 {
		return nil
	}
	return r.service.Input(ctx, channel, data)
}

func (r InputRouter) HandleResize(ctx context.Context, model app.Model, cols, rows int) error {
	channel, ok := activeResizeChannel(model)
	if !ok {
		return nil
	}
	return r.service.Resize(ctx, channel, uint16(cols), uint16(rows))
}

func activeChannel(model app.Model) (uint16, bool) {
	if model.FocusTarget != app.FocusWorkbench || model.Overlay.HasActive() || model.Workspace == nil {
		return 0, false
	}
	tab := model.Workspace.ActiveTab()
	if tab == nil {
		return 0, false
	}
	pane, ok := tab.ActivePane()
	if !ok || pane.SlotState != types.PaneSlotLive || pane.TerminalID == "" {
		return 0, false
	}
	session, ok := model.Sessions[pane.TerminalID]
	if !ok || !session.Attached {
		return 0, false
	}
	return session.Channel, true
}

func activeResizeChannel(model app.Model) (uint16, bool) {
	channel, ok := activeChannel(model)
	if !ok || model.Workspace == nil {
		return 0, false
	}
	tab := model.Workspace.ActiveTab()
	if tab == nil {
		return 0, false
	}
	pane, ok := tab.ActivePane()
	if !ok {
		return 0, false
	}
	meta, ok := model.Terminals[pane.TerminalID]
	if !ok {
		return 0, false
	}
	if meta.OwnerPaneID != "" && meta.OwnerPaneID != pane.ID {
		return 0, false
	}
	return channel, true
}

func keyInputBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	default:
		return nil
	}
}
