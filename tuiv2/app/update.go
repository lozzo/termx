package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case SemanticActionMsg:
		return m, m.applyEffects(m.orchestrator.HandleSemanticAction(typed.Action))
	case input.SemanticAction:
		return m, m.applyEffects(m.orchestrator.HandleSemanticAction(typed))
	case TerminalInputMsg:
		return m, m.handleTerminalInput(typed.Input)
	case input.TerminalInput:
		return m, m.handleTerminalInput(typed)
	case sequenceMsg:
		return m, m.nextSequenceCmd(typed)
	case orchestrator.TerminalAttachedMsg:
		m.render.Invalidate()
		return m, nil
	case orchestrator.SnapshotLoadedMsg:
		m.render.Invalidate()
		return m, nil
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		m.render.Invalidate()
		return m, nil
	case error:
		m.err = typed
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) applyEffects(effects []orchestrator.Effect) tea.Cmd {
	if len(effects) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(effects))
	for _, effect := range effects {
		if cmd := m.effectCmd(effect); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) effectCmd(effect orchestrator.Effect) tea.Cmd {
	switch typed := effect.(type) {
	case orchestrator.SetInputModeEffect:
		return func() tea.Msg {
			m.input.SetMode(typed.Mode)
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.OpenPickerEffect:
		return func() tea.Msg {
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.AttachTerminalEffect:
		return func() tea.Msg {
			msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), typed.PaneID, typed.TerminalID, typed.Mode, 0, 200)
			if err != nil {
				return err
			}
			if len(msgs) == 0 {
				return nil
			}
			return sequenceMsg(msgs)
		}
	case orchestrator.LoadSnapshotEffect:
		return func() tea.Msg {
			snapshot, err := m.runtime.LoadSnapshot(context.Background(), typed.TerminalID, typed.Offset, typed.Limit)
			if err != nil {
				return err
			}
			return orchestrator.SnapshotLoadedMsg{TerminalID: typed.TerminalID, Snapshot: snapshot}
		}
	default:
		return nil
	}
}

func (m *Model) handleTerminalInput(in input.TerminalInput) tea.Cmd {
	if len(in.Data) == 0 {
		return nil
	}
	return func() tea.Msg {
		if err := m.runtime.SendInput(context.Background(), in.PaneID, in.Data); err != nil {
			return err
		}
		return nil
	}
}

func (m *Model) nextSequenceCmd(seq sequenceMsg) tea.Cmd {
	if len(seq) == 0 {
		return nil
	}
	return func() tea.Msg {
		return seq[0]
	}
}

type sequenceMsg []any
