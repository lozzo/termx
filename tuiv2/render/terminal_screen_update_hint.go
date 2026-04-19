package render

import runtimestate "github.com/lozzow/termx/tuiv2/runtime"

type terminalScreenUpdateHint struct {
	SurfaceVersion uint64
	FullReplace    bool
	ScreenScroll   int
	ChangedRows    []int
}

func terminalScreenUpdateHintFromVisible(summary runtimestate.VisibleScreenUpdateSummary) terminalScreenUpdateHint {
	return terminalScreenUpdateHint{
		SurfaceVersion: summary.SurfaceVersion,
		FullReplace:    summary.FullReplace,
		ScreenScroll:   summary.ScreenScroll,
		ChangedRows:    append([]int(nil), summary.ChangedRows...),
	}
}

func effectiveTerminalScreenUpdateHint(hint terminalScreenUpdateHint, hasSurface bool, surfaceVersion uint64) terminalScreenUpdateHint {
	if !hasSurface || hint.SurfaceVersion != surfaceVersion {
		return terminalScreenUpdateHint{}
	}
	return hint
}
