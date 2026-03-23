package bt

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/domain/types"
)

type RuntimeExecutor interface {
	Execute(effect reducer.Effect) ([]intent.Intent, error)
}

type TerminalService interface {
	ConnectTerminal(paneID types.PaneID, terminalID types.TerminalID) error
	CreateTerminal(paneID types.PaneID, command []string, name string) error
	StopTerminal(terminalID types.TerminalID) error
	UpdateTerminalMetadata(terminalID types.TerminalID, name string, tags map[string]string) error
	ConnectTerminalInNewTab(workspaceID types.WorkspaceID, terminalID types.TerminalID) error
	ConnectTerminalInFloatingPane(workspaceID types.WorkspaceID, tabID types.TabID, terminalID types.TerminalID) error
}

type RuntimeEffectHandler struct {
	Executor RuntimeExecutor
}

type DefaultRuntimeExecutor struct {
	TerminalService TerminalService
}

type effectIntentsMsg struct {
	Intents []intent.Intent
}

func (h RuntimeEffectHandler) Handle(effects []reducer.Effect) tea.Cmd {
	if len(effects) == 0 || h.Executor == nil {
		return nil
	}
	// effect 执行应留在 tea.Cmd 中，避免在 Update 同步阻塞 runtime 调用。
	return func() tea.Msg {
		intents := make([]intent.Intent, 0, len(effects))
		for _, effect := range effects {
			next, err := h.Executor.Execute(effect)
			if err != nil {
				continue
			}
			intents = append(intents, next...)
		}
		if len(intents) == 0 {
			return nil
		}
		return effectIntentsMsg{Intents: intents}
	}
}

func (e DefaultRuntimeExecutor) Execute(effect reducer.Effect) ([]intent.Intent, error) {
	switch effectValue := effect.(type) {
	case reducer.ConnectTerminalEffect:
		if e.TerminalService != nil {
			return nil, e.TerminalService.ConnectTerminal(effectValue.PaneID, effectValue.TerminalID)
		}
		return nil, nil
	case reducer.CreateTerminalEffect:
		if e.TerminalService != nil {
			return nil, e.TerminalService.CreateTerminal(effectValue.PaneID, effectValue.Command, effectValue.Name)
		}
		return nil, nil
	case reducer.StopTerminalEffect:
		if e.TerminalService != nil {
			return nil, e.TerminalService.StopTerminal(effectValue.TerminalID)
		}
		return nil, nil
	case reducer.UpdateTerminalMetadataEffect:
		if e.TerminalService != nil {
			return nil, e.TerminalService.UpdateTerminalMetadata(effectValue.TerminalID, effectValue.Name, cloneStringMap(effectValue.Tags))
		}
		return nil, nil
	case reducer.ConnectTerminalInNewTabEffect:
		if e.TerminalService != nil {
			return nil, e.TerminalService.ConnectTerminalInNewTab(effectValue.WorkspaceID, effectValue.TerminalID)
		}
		return nil, nil
	case reducer.ConnectTerminalInFloatingPaneEffect:
		if e.TerminalService != nil {
			return nil, e.TerminalService.ConnectTerminalInFloatingPane(effectValue.WorkspaceID, effectValue.TabID, effectValue.TerminalID)
		}
		return nil, nil
	case reducer.OpenPromptEffect:
		return []intent.Intent{intent.OpenPromptIntent{
			PromptKind: effectValue.PromptKind,
			TerminalID: effectValue.TerminalID,
		}}, nil
	default:
		return nil, nil
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
