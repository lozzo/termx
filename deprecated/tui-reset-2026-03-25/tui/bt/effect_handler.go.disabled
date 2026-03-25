package bt

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/domain/types"
)

type RuntimeExecutor interface {
	Execute(effect reducer.Effect) (ExecutionResult, error)
}

type CreateTerminalResult struct {
	TerminalID types.TerminalID
	State      types.TerminalRunState
}

type TerminalService interface {
	ConnectTerminal(paneID types.PaneID, terminalID types.TerminalID) error
	CreateTerminal(paneID types.PaneID, command []string, name string) (CreateTerminalResult, error)
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

type ExecutionResult struct {
	Intents []intent.Intent
	Notices []Notice
}

type FeedbackMsg struct {
	Intents []intent.Intent
	Notices []Notice
}

type effectResultMsg = FeedbackMsg

func (h RuntimeEffectHandler) Handle(effects []reducer.Effect) tea.Cmd {
	if len(effects) == 0 || h.Executor == nil {
		return nil
	}
	// effect 执行应留在 tea.Cmd 中，避免在 Update 同步阻塞 runtime 调用。
	return func() tea.Msg {
		result := ExecutionResult{
			Intents: make([]intent.Intent, 0, len(effects)),
		}
		for _, effect := range effects {
			next, err := h.Executor.Execute(effect)
			if err != nil {
				result.Notices = append(result.Notices, Notice{
					Level: NoticeLevelError,
					Text:  err.Error(),
				})
				continue
			}
			result.Intents = append(result.Intents, next.Intents...)
			result.Notices = append(result.Notices, next.Notices...)
		}
		return feedbackMsg(result)
	}
}

func FeedbackCmd(result ExecutionResult) tea.Cmd {
	msg := feedbackMsg(result)
	if msg == nil {
		return nil
	}
	return func() tea.Msg {
		return msg
	}
}

func (e DefaultRuntimeExecutor) Execute(effect reducer.Effect) (ExecutionResult, error) {
	switch effectValue := effect.(type) {
	case reducer.ConnectTerminalEffect:
		if e.TerminalService != nil {
			return ExecutionResult{}, e.TerminalService.ConnectTerminal(effectValue.PaneID, effectValue.TerminalID)
		}
		return ExecutionResult{}, nil
	case reducer.CreateTerminalEffect:
		created := CreateTerminalResult{State: types.TerminalRunStateRunning}
		if e.TerminalService != nil {
			var err error
			created, err = e.TerminalService.CreateTerminal(effectValue.PaneID, effectValue.Command, effectValue.Name)
			if err != nil {
				return ExecutionResult{}, err
			}
			if created.State == "" {
				created.State = types.TerminalRunStateRunning
			}
		}
		return ExecutionResult{
			Intents: []intent.Intent{intent.CreateTerminalSucceededIntent{
				PaneID:     effectValue.PaneID,
				TerminalID: created.TerminalID,
				Name:       effectValue.Name,
				Command:    append([]string(nil), effectValue.Command...),
				State:      created.State,
			}},
		}, nil
	case reducer.StopTerminalEffect:
		if e.TerminalService != nil {
			if err := e.TerminalService.StopTerminal(effectValue.TerminalID); err != nil {
				return ExecutionResult{}, err
			}
		}
		return ExecutionResult{
			Intents: []intent.Intent{intent.StopTerminalSucceededIntent{
				TerminalID: effectValue.TerminalID,
			}},
		}, nil
	case reducer.UpdateTerminalMetadataEffect:
		if e.TerminalService != nil {
			if err := e.TerminalService.UpdateTerminalMetadata(effectValue.TerminalID, effectValue.Name, cloneStringMap(effectValue.Tags)); err != nil {
				return ExecutionResult{}, err
			}
		}
		return ExecutionResult{
			Intents: []intent.Intent{intent.UpdateTerminalMetadataSucceededIntent{
				TerminalID: effectValue.TerminalID,
				Name:       effectValue.Name,
				Tags:       cloneStringMap(effectValue.Tags),
			}},
		}, nil
	case reducer.ConnectTerminalInNewTabEffect:
		if e.TerminalService != nil {
			if err := e.TerminalService.ConnectTerminalInNewTab(effectValue.WorkspaceID, effectValue.TerminalID); err != nil {
				return ExecutionResult{}, err
			}
		}
		return ExecutionResult{
			Intents: []intent.Intent{intent.ConnectTerminalInNewTabSucceededIntent{
				WorkspaceID: effectValue.WorkspaceID,
				TerminalID:  effectValue.TerminalID,
			}},
		}, nil
	case reducer.ConnectTerminalInFloatingPaneEffect:
		if e.TerminalService != nil {
			if err := e.TerminalService.ConnectTerminalInFloatingPane(effectValue.WorkspaceID, effectValue.TabID, effectValue.TerminalID); err != nil {
				return ExecutionResult{}, err
			}
		}
		return ExecutionResult{
			Intents: []intent.Intent{intent.ConnectTerminalInFloatingPaneSucceededIntent{
				WorkspaceID: effectValue.WorkspaceID,
				TabID:       effectValue.TabID,
				TerminalID:  effectValue.TerminalID,
			}},
		}, nil
	case reducer.OpenPromptEffect:
		return ExecutionResult{
			Intents: []intent.Intent{intent.OpenPromptIntent{
				PromptKind: effectValue.PromptKind,
				TerminalID: effectValue.TerminalID,
			}},
		}, nil
	case reducer.NoticeEffect:
		return ExecutionResult{
			Notices: []Notice{{
				Level: noticeLevelFromReducer(effectValue.Level),
				Text:  effectValue.Text,
			}},
		}, nil
	default:
		return ExecutionResult{}, nil
	}
}

func feedbackMsg(result ExecutionResult) tea.Msg {
	if len(result.Intents) == 0 && len(result.Notices) == 0 {
		return nil
	}
	return FeedbackMsg{
		Intents: result.Intents,
		Notices: result.Notices,
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

func noticeLevelFromReducer(level string) NoticeLevel {
	switch level {
	case reducer.NoticeLevelInfo:
		return NoticeLevelInfo
	case reducer.NoticeLevelError:
		return NoticeLevelError
	default:
		return NoticeLevelError
	}
}
