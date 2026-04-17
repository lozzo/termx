package app

type verticalScrollMode uint8

const (
	verticalScrollModeNone verticalScrollMode = iota
	verticalScrollModeRowsOnly
	verticalScrollModeRectsOnly
	verticalScrollModeRowsAndRects
)

func (m verticalScrollMode) Enabled() bool {
	return m != verticalScrollModeNone
}

func (m verticalScrollMode) RowsAllowed() bool {
	return m == verticalScrollModeRowsOnly || m == verticalScrollModeRowsAndRects
}

func (m verticalScrollMode) RectsAllowed() bool {
	return m == verticalScrollModeRectsOnly || m == verticalScrollModeRowsAndRects
}

func (m verticalScrollMode) String() string {
	switch m {
	case verticalScrollModeRowsOnly:
		return "rows_only"
	case verticalScrollModeRectsOnly:
		return "rects_only"
	case verticalScrollModeRowsAndRects:
		return "rows_and_rects"
	default:
		return "none"
	}
}
