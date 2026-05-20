package app

type copyModeLogicalLines struct {
	wrapped func(int) bool
	total   int
}

func newCopyModeLogicalLines(buffer copyModeBuffer) copyModeLogicalLines {
	return copyModeLogicalLines{
		wrapped: buffer.rowWrapped,
		total:   buffer.totalRows(),
	}
}

func (l copyModeLogicalLines) rowContinues(row int) bool {
	return row >= 0 && row < l.total-1 && l.wrapped != nil && l.wrapped(row)
}

func (l copyModeLogicalLines) lineStart(row int) int {
	if row < 0 {
		return 0
	}
	if row >= l.total {
		row = l.total - 1
	}
	for row > 0 && l.wrapped != nil && l.wrapped(row-1) {
		row--
	}
	return row
}

func (l copyModeLogicalLines) lineEnd(row int) int {
	if row < 0 {
		return 0
	}
	if row >= l.total {
		row = l.total - 1
	}
	for row < l.total-1 && l.wrapped != nil && l.wrapped(row) {
		row++
	}
	return row
}
