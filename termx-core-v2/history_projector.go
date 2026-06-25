package termxcorev2

import (
	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

type ScreenOutputMode string

const (
	ScreenOutputModeOrdinary      ScreenOutputMode = "ordinary"
	ScreenOutputModePrimaryScreen ScreenOutputMode = "primary-screen"
	ScreenOutputModeAltTransient  ScreenOutputMode = "alt-transient"
)

type CloseReason string

const (
	CloseReasonProcessExit  CloseReason = "process-exit"
	CloseReasonTerminalKill CloseReason = "terminal-kill"
)

type ScreenSessionState struct {
	InAltScreen             bool
	HasActivePrimarySession bool
}

type ScreenAppDecision struct {
	Mode                         ScreenOutputMode
	PublishFrame                 bool
	ClosePrimarySession          bool
	ArchivePrimaryBeforeAlt      bool
	ClearPrimaryCurrentForAlt    bool
	EnterAltTransientFrame       bool
	ExitAltTransientFrame        bool
	ForceCommitPrimaryFinalFrame bool
}

type ScreenAppClassifier interface {
	Classify(tx TerminalSemanticTransaction, state ScreenSessionState) ScreenAppDecision
}

type HistoryProjector interface {
	Apply(tx TerminalSemanticTransaction, decision ScreenAppDecision) (HistoryMutation, error)
	ForceClose(reason CloseReason) (HistoryMutation, error)
}

type HistoryMutation struct {
	Events []history.EventKind
}

type SimpleScreenAppClassifier struct{}

func (SimpleScreenAppClassifier) Classify(tx TerminalSemanticTransaction, state ScreenSessionState) ScreenAppDecision {
	altEntered := tx.AltEntered || transactionHasAltMode(tx, true)
	altExited := tx.AltExited || transactionHasAltMode(tx, false)
	synchronizedEnd := tx.SynchronizedEnd || transactionHasSyncMode(tx, false)
	switch {
	case altEntered:
		return ScreenAppDecision{
			Mode:                      ScreenOutputModeAltTransient,
			ArchivePrimaryBeforeAlt:   true,
			ClearPrimaryCurrentForAlt: true,
			EnterAltTransientFrame:    true,
		}
	case altExited:
		return ScreenAppDecision{
			Mode:                  ScreenOutputModeOrdinary,
			ExitAltTransientFrame: true,
		}
	case tx.AltFrame != nil || state.InAltScreen:
		return ScreenAppDecision{Mode: ScreenOutputModeAltTransient, PublishFrame: tx.AltFrame != nil}
	case tx.PrimaryFrame != nil && (synchronizedEnd || state.HasActivePrimarySession):
		return ScreenAppDecision{Mode: ScreenOutputModePrimaryScreen, PublishFrame: true}
	default:
		return ScreenAppDecision{Mode: ScreenOutputModeOrdinary}
	}
}

func transactionHasAltMode(tx TerminalSemanticTransaction, enabled bool) bool {
	for _, op := range tx.Ops {
		if op.Code == vterm.ScreenOpModes && op.Private && (op.Mode == 47 || op.Mode == 1047 || op.Mode == 1049) && op.Enabled == enabled {
			return true
		}
	}
	return false
}

func transactionHasSyncMode(tx TerminalSemanticTransaction, enabled bool) bool {
	for _, op := range tx.Ops {
		if op.Code == vterm.ScreenOpModes && op.Private && op.Mode == 2026 && op.Enabled == enabled {
			return true
		}
	}
	return false
}

type HistoryTrackProjector struct {
	track *history.HistoryTrack
}

func NewHistoryTrackProjector(track *history.HistoryTrack) *HistoryTrackProjector {
	if track == nil {
		track = history.NewHistoryTrack()
	}
	return &HistoryTrackProjector{track: track}
}

func (projector *HistoryTrackProjector) Apply(tx TerminalSemanticTransaction, decision ScreenAppDecision) (HistoryMutation, error) {
	if projector == nil || projector.track == nil {
		return HistoryMutation{}, nil
	}
	mutation := HistoryMutation{}
	emit := func(event history.HistoryEvent) error {
		if err := projector.track.Apply(event); err != nil {
			return err
		}
		mutation.Events = append(mutation.Events, event.Kind)
		return nil
	}
	emitOwned := func(event history.HistoryEvent) error {
		if err := projector.track.ApplyOwned(event); err != nil {
			return err
		}
		mutation.Events = append(mutation.Events, event.Kind)
		return nil
	}

	if tx.Size.Rows > 0 {
		projector.track.SetPrimaryScreenRows(tx.Size.Rows)
	}
	hasAltEnterMode := transactionHasAltMode(tx, true)
	hasAltExitMode := transactionHasAltMode(tx, false)
	if (decision.ArchivePrimaryBeforeAlt || decision.ClearPrimaryCurrentForAlt || tx.AltEntered) && !hasAltEnterMode {
		if err := emit(history.HistoryEvent{Kind: history.EventSwitchAltScreen, EnterAltScreen: true}); err != nil {
			return mutation, err
		}
	}
	if (decision.ExitAltTransientFrame || tx.AltExited) && !hasAltExitMode {
		if err := emit(history.HistoryEvent{Kind: history.EventSwitchAltScreen, EnterAltScreen: false}); err != nil {
			return mutation, err
		}
	}

	ordinaryDirectCommit := decision.Mode == ScreenOutputModeOrdinary
	for _, op := range tx.Ops {
		if err := projector.applyOp(op, ordinaryDirectCommit, emit, emitOwned); err != nil {
			return mutation, err
		}
	}
	if !ordinaryDirectCommit {
		for range tx.PrimaryScrollOut {
			if err := emit(history.HistoryEvent{Kind: history.EventPrimaryScrollOut, Count: 1}); err != nil {
				return mutation, err
			}
		}
	}
	if decision.PublishFrame && tx.PrimaryFrame != nil && decision.Mode == ScreenOutputModePrimaryScreen {
		if err := emit(history.HistoryEvent{Kind: history.EventReplacePrimaryFrame, Rows: historyRowsFromVTermRows(tx.PrimaryFrame.Rows)}); err != nil {
			return mutation, err
		}
	}
	if (decision.PublishFrame || decision.EnterAltTransientFrame) && tx.AltFrame != nil {
		if err := emit(history.HistoryEvent{Kind: history.EventAppendAltScreenFrame, Rows: historyRowsFromVTermRows(tx.AltFrame.Rows)}); err != nil {
			return mutation, err
		}
	}
	if tx.AltExitFrame != nil {
		if err := emit(history.HistoryEvent{Kind: history.EventAppendAltScreenFrame, Rows: historyRowsFromVTermRows(tx.AltExitFrame.Rows)}); err != nil {
			return mutation, err
		}
	}
	return mutation, nil
}

func (projector *HistoryTrackProjector) ForceClose(reason CloseReason) (HistoryMutation, error) {
	if projector == nil || projector.track == nil {
		return HistoryMutation{}, nil
	}
	mutation := HistoryMutation{}
	if err := projector.track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		return mutation, err
	}
	mutation.Events = append(mutation.Events, history.EventForceCommitFrontier)
	return mutation, nil
}

func (projector *HistoryTrackProjector) applyOp(
	op TerminalSemanticOp,
	ordinaryDirectCommit bool,
	emit func(history.HistoryEvent) error,
	emitOwned func(history.HistoryEvent) error,
) error {
	switch op.Code {
	case vterm.ScreenOpWriteSpan:
		if len(op.Cells) == 0 {
			return nil
		}
		return emitOwned(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: historyCellsFromVTermCells(op.Cells)})
	case vterm.ScreenOpControl:
		return projector.applyControl(op, ordinaryDirectCommit, emit)
	case vterm.ScreenOpModes:
		return projector.applyMode(op, emit)
	}
	return nil
}

func (projector *HistoryTrackProjector) applyControl(op TerminalSemanticOp, ordinaryDirectCommit bool, emit func(history.HistoryEvent) error) error {
	switch op.Control {
	case "cr":
		return emit(history.HistoryEvent{Kind: history.EventCarriageReturn})
	case "lf", "ind":
		if err := emit(history.HistoryEvent{Kind: history.EventSealLogicalLine}); err != nil {
			return err
		}
		if ordinaryDirectCommit {
			// 中文说明：ordinary stdout 的完整 logical line 以 core history 为 truth，
			// 不等待 primary screen scroll-out 才进入 committed history。
			return emit(history.HistoryEvent{Kind: history.EventForceCommitFrontier})
		}
		return emit(history.HistoryEvent{Kind: history.EventCommitFrontier})
	case "soft-wrap":
		return emit(history.HistoryEvent{Kind: history.EventSoftWrapLine})
	case "cup", "vpa":
		return emit(history.HistoryEvent{Kind: history.EventCursorPosition, Row: op.Row + 1, Column: op.Col + 1})
	case "cha", "ht", "cbt":
		return emit(history.HistoryEvent{Kind: history.EventCursorHorizontalAbsolute, Count: op.Col + 1})
	case "cuu":
		return emit(history.HistoryEvent{Kind: history.EventCursorUp, Count: op.Mode})
	case "cud":
		return emit(history.HistoryEvent{Kind: history.EventCursorDown, Count: op.Mode})
	case "cuf":
		return emit(history.HistoryEvent{Kind: history.EventCursorForward, Count: op.Mode})
	case "cub", "bs":
		count := op.Mode
		if op.Control == "bs" || count <= 0 {
			count = 1
		}
		return emit(history.HistoryEvent{Kind: history.EventCursorBackward, Count: count})
	case "el":
		return emit(history.HistoryEvent{
			Kind:      history.EventEraseInLine,
			EraseMode: op.Mode,
			Style:     historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
		})
	case "ed":
		return emit(history.HistoryEvent{Kind: history.EventEraseInDisplay, EraseMode: op.Mode})
	}
	return nil
}

func (projector *HistoryTrackProjector) applyMode(op TerminalSemanticOp, emit func(history.HistoryEvent) error) error {
	if !op.Private {
		return nil
	}
	switch op.Mode {
	case 47, 1047, 1049:
		return emit(history.HistoryEvent{Kind: history.EventSwitchAltScreen, EnterAltScreen: op.Enabled})
	case 2026:
		if op.Enabled {
			return emit(history.HistoryEvent{Kind: history.EventBeginSynchronizedFrame})
		}
		return emit(history.HistoryEvent{Kind: history.EventEndSynchronizedFrame})
	}
	return nil
}
