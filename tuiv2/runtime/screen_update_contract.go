package runtime

import (
	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/protocol"
)

type ScreenUpdateContract struct {
	Update         protocol.ScreenUpdate
	Classification protocol.ScreenUpdateClassification
	Summary        VisibleScreenUpdateSummary
}

type screenUpdateOrigin string

const (
	screenUpdateOriginLive      screenUpdateOrigin = "live"
	screenUpdateOriginBootstrap screenUpdateOrigin = "bootstrap"
	screenUpdateOriginRecovery  screenUpdateOrigin = "recovery"
)

type screenUpdateLifecycle string

const (
	screenUpdateLifecycleNoop        screenUpdateLifecycle = "noop"
	screenUpdateLifecycleDelta       screenUpdateLifecycle = "delta"
	screenUpdateLifecycleFullReplace screenUpdateLifecycle = "full_replace"
	screenUpdateLifecyclePlaceholder screenUpdateLifecycle = "placeholder"
)

type ClassifiedScreenUpdate struct {
	Contract         ScreenUpdateContract
	Origin           screenUpdateOrigin
	Lifecycle        screenUpdateLifecycle
	AdvanceBootstrap bool
	ClearRecovery    bool
}

func NewScreenUpdateContract(update protocol.ScreenUpdate) ScreenUpdateContract {
	normalized := protocol.NormalizeScreenUpdate(update)
	return ScreenUpdateContract{
		Update:         normalized,
		Classification: protocol.ClassifyScreenUpdate(normalized),
		Summary:        screenUpdateSummaryFromProtocol(normalized),
	}
}

func DecodeScreenUpdateContractPayload(payload []byte) (ScreenUpdateContract, error) {
	update, err := protocol.DecodeScreenUpdatePayload(payload)
	if err != nil {
		return ScreenUpdateContract{}, err
	}
	return NewScreenUpdateContract(update), nil
}

func screenUpdateSummaryFromProtocol(update protocol.ScreenUpdate) VisibleScreenUpdateSummary {
	summary := VisibleScreenUpdateSummary{
		FullReplace:  update.FullReplace,
		ScreenScroll: update.ScreenScroll,
	}
	if summary.ScreenScroll == 0 {
		for _, op := range update.Ops {
			if op.Code == protocol.ScreenOpScrollRect && op.Dx == 0 && op.Rect.X == 0 && int(update.Size.Cols) > 0 && op.Rect.Width >= int(update.Size.Cols) {
				summary.ScreenScroll = -op.Dy
				break
			}
		}
	}
	switch {
	case len(update.ChangedSpans) > 0:
		summary.ChangedRows = make([]int, 0, len(update.ChangedSpans))
		seen := make(map[int]struct{}, len(update.ChangedSpans))
		for _, span := range update.ChangedSpans {
			if _, ok := seen[span.Row]; ok {
				continue
			}
			seen[span.Row] = struct{}{}
			summary.ChangedRows = append(summary.ChangedRows, span.Row)
		}
	case len(update.Ops) > 0:
		rows := make([]int, 0, len(update.Ops))
		seen := make(map[int]struct{}, len(update.Ops))
		addRow := func(row int) {
			if row < 0 {
				return
			}
			if _, ok := seen[row]; ok {
				return
			}
			seen[row] = struct{}{}
			rows = append(rows, row)
		}
		addRange := func(start, end int) {
			for row := start; row < end; row++ {
				addRow(row)
			}
		}
		for _, op := range update.Ops {
			switch op.Code {
			case protocol.ScreenOpWriteSpan, protocol.ScreenOpClearToEOL:
				addRow(op.Row)
			case protocol.ScreenOpScrollRect, protocol.ScreenOpClearRect:
				addRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
			case protocol.ScreenOpCopyRect:
				addRange(op.DstY, op.DstY+op.Src.Height)
			case protocol.ScreenOpResize:
				addRange(0, int(op.Size.Rows))
			}
		}
		if len(rows) > 0 {
			summary.ChangedRows = rows
		}
	}
	return summary
}

func (r *Runtime) applyScreenUpdateContract(terminal *TerminalRuntime, terminalID string, classified ClassifiedScreenUpdate) {
	if r == nil || terminal == nil {
		return
	}
	update := classified.Contract.Update
	summary := classified.Contract.Summary

	snapshotApplyFinish := perftrace.Measure("runtime.stream.screen_update.snapshot_apply")
	terminal.Snapshot = applyScreenUpdateSnapshot(terminal.Snapshot, terminalID, update)
	snapshotApplyFinish(0)

	vt := r.ensureVTerm(terminal)
	if vt != nil && terminal.Snapshot != nil {
		appliedPartial := false
		if !update.FullReplace {
			if applier, ok := vt.(screenUpdateApplier); ok {
				loadFinish := perftrace.Measure("runtime.stream.screen_update.load_vterm_partial")
				appliedPartial = applier.ApplyScreenUpdate(update)
				loadFinish(0)
			}
		}
		if !appliedPartial {
			loadFinish := perftrace.Measure("runtime.stream.screen_update.load_vterm_full")
			loadSnapshotIntoVTerm(vt, terminal.Snapshot)
			loadFinish(0)
		}
		terminal.PreferSnapshot = false
		invalidateFinish := perftrace.Measure("runtime.stream.screen_update.invalidate")
		r.bumpSurfaceVersion(terminal)
		summary.SurfaceVersion = terminal.SurfaceVersion
		terminal.ScreenUpdate = summary
		terminal.SnapshotVersion = terminal.SurfaceVersion
		classified.applyStateTransitions(terminal)
		r.invalidate()
		invalidateFinish(0)
		return
	}

	invalidateFinish := perftrace.Measure("runtime.stream.screen_update.invalidate")
	terminal.PreferSnapshot = true
	terminal.SnapshotVersion++
	summary.SurfaceVersion = terminal.SurfaceVersion
	terminal.ScreenUpdate = summary
	classified.applyStateTransitions(terminal)
	r.invalidate()
	invalidateFinish(0)
}

func (r *Runtime) applyDecodedScreenUpdateContract(terminal *TerminalRuntime, terminalID string, contract ScreenUpdateContract) {
	if r == nil || terminal == nil {
		return
	}
	update := contract.Update
	recordScreenUpdateMetrics(update)
	if update.Title != "" && update.Title != terminal.Title {
		terminal.Title = update.Title
		r.touch()
		if r.onTitleChange != nil {
			r.onTitleChange(terminal.TerminalID, update.Title)
		}
	}
	classified := classifyDecodedScreenUpdate(terminal, contract)
	if classified.Lifecycle == screenUpdateLifecyclePlaceholder {
		invalidateFinish := perftrace.Measure("runtime.stream.screen_update.invalidate")
		r.invalidate()
		invalidateFinish(0)
		return
	}
	r.applyScreenUpdateContract(terminal, terminalID, classified)
}

func classifyDecodedScreenUpdate(terminal *TerminalRuntime, contract ScreenUpdateContract) ClassifiedScreenUpdate {
	classified := ClassifiedScreenUpdate{
		Contract:  contract,
		Origin:    screenUpdateOriginLive,
		Lifecycle: screenUpdateLifecycleFromClassification(contract.Classification),
	}
	if terminal == nil {
		return classified
	}
	switch {
	case terminal.BootstrapPending:
		classified.Origin = screenUpdateOriginBootstrap
	case hasScreenUpdateRecovery(terminal.Recovery):
		classified.Origin = screenUpdateOriginRecovery
	}
	if classified.Origin == screenUpdateOriginBootstrap &&
		terminal.Snapshot != nil &&
		contract.Classification.BlankFullReplace {
		classified.Lifecycle = screenUpdateLifecyclePlaceholder
		return classified
	}
	advancesBoundary := classified.Lifecycle == screenUpdateLifecycleDelta ||
		classified.Lifecycle == screenUpdateLifecycleFullReplace
	classified.AdvanceBootstrap = terminal.BootstrapPending && advancesBoundary
	classified.ClearRecovery = hasScreenUpdateRecovery(terminal.Recovery) && advancesBoundary
	return classified
}

func (classified ClassifiedScreenUpdate) applyStateTransitions(terminal *TerminalRuntime) {
	if terminal == nil {
		return
	}
	if classified.AdvanceBootstrap {
		terminal.BootstrapPending = false
	}
	if classified.ClearRecovery {
		terminal.Recovery = RecoveryState{}
	}
}

func screenUpdateLifecycleFromClassification(classification protocol.ScreenUpdateClassification) screenUpdateLifecycle {
	switch {
	case !classification.HasContentChange:
		return screenUpdateLifecycleNoop
	case classification.FullReplace:
		return screenUpdateLifecycleFullReplace
	default:
		return screenUpdateLifecycleDelta
	}
}

func hasScreenUpdateRecovery(recovery RecoveryState) bool {
	return recovery.SyncLost || recovery.DroppedBytes > 0
}
