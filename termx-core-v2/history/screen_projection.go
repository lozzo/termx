package history

import "time"

// ScreenProjectionRow 是 screen-backed projection 中的单条 physical row 视图。
// domain owner 仍是 ScreenHistoryBuffer：RowID/Version 来自 physical row truth，
// HistoryRow 只是后续 protocol/TUI 的兼容投影，不能反向成为 history truth。
type ScreenProjectionRow struct {
	LineID             LogicalLineID
	RowID              uint64
	Version            uint64
	Cells              []Cell
	OwnerKind          RowOwnerKind
	Segment            HistorySegment
	Kind               LineKind
	RowInLine          int
	ProjectionRowIndex int
	ScreenRow          int
	ScreenCols         int
	Sealed             bool
	Wrapped            bool
	Continued          bool
}

// ScreenLogicalLine 是从一组 physical rows 投影出的 logical line。
// RowIDs 是 correctness identity；projection 可以拼接文本 cells，但不得用纯文本
// dedupe 代替 RowID，也不得把 logical line 写回 ScreenHistoryBuffer。
type ScreenLogicalLine struct {
	LineID     LogicalLineID
	RowIDs     []uint64
	Versions   []uint64
	Rows       []ScreenProjectionRow
	Cells      []Cell
	Segment    HistorySegment
	Kind       LineKind
	Sealed     bool
	Wrapped    bool
	Continued  bool
	ScreenCols int
}

// ScreenHistoryProjection 是 ScreenHistoryBuffer 的 read-only query projection。
// 它同时保留 logical lines 和 physical rows：logical line 服务 HistoryWindow/copy
// 阶段，rows 保留 RowID/Version/ScreenRow 以保证 latest/current 与 sealed 去重。
type ScreenHistoryProjection struct {
	Generation Generation
	Cols       int
	InAlt      bool
	Lines      []ScreenLogicalLine
	Rows       []ScreenProjectionRow
}

// ProjectHistory 从 committed physical rows 与当前 main/alt screen 生成查询投影。
// 调用边界：这是 R422 独立 projection layer，不接 production HistoryStore；它只读
// ScreenHistoryBuffer，并按 RowID 去重，避免同一 physical row 同时出现在 sealed
// history 和 current screen 中。
func (buffer *ScreenHistoryBuffer) ProjectHistory() ScreenHistoryProjection {
	if buffer == nil {
		return ScreenHistoryProjection{}
	}
	buffer.ensure()
	builder := screenProjectionBuilder{
		projection: ScreenHistoryProjection{
			Generation: Generation(buffer.AppliedSeq),
			Cols:       buffer.Cols,
			InAlt:      buffer.InAlt,
		},
		seenRows:         make(map[uint64]struct{}),
		currentLineIndex: -1,
	}
	builder.addPhysicalRows(buffer.Committed, HistorySegmentCommitted, true)
	if buffer.InAlt {
		builder.addPhysicalRows(buffer.Alt.Rows, HistorySegmentCurrentAltFrame, false)
	} else {
		builder.addPhysicalRows(buffer.Main.Rows, HistorySegmentCurrentPrimaryFrame, false)
	}
	return builder.projection
}

// HistoryRows 将 screen-backed projection 转成现有 HistoryWindow rows。
// RowID/Version 仍保留在 ScreenProjectionRow；转换后的 HistoryRow 只用于兼容
// window/copy 消费面，不能丢掉上层持有的 ScreenHistoryProjection identity。
func (projection ScreenHistoryProjection) HistoryRows() []HistoryRow {
	if len(projection.Rows) == 0 {
		return nil
	}
	rows := make([]HistoryRow, len(projection.Rows))
	for index, row := range projection.Rows {
		rows[index] = row.historyRow(projection.Cols)
	}
	return rows
}

// LatestWindow 生成一个基于 screen-backed projection 的 latest HistoryWindow。
// 它是 R422 harness 与后续 R425 接入点；分页只读取 projection rows，不访问旧
// HistoryStore、live surface、TUI rows 或 raw PTY。
func (projection ScreenHistoryProjection) LatestWindow(req HistoryWindowRequest) HistoryWindow {
	rows := projection.HistoryRows()
	limit := normalizedLimit(req.Limit)
	start := latestRowsWindowStart(rows, limit)
	page := cloneHistoryRows(rows[start:])
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, projection.Generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = HistoryCursor{
			Generation:     projection.Generation,
			BeforeRowIndex: start,
			Valid:          start > 0,
			Token:          req.Token,
		}
		if boundary.Cursor.Valid {
			boundary.Cursor.Segment = page[0].Segment
			boundary.Cursor.LineID = page[0].LineID
			boundary.Cursor.RowInLine = page[0].RowInLine
		}
	}
	return HistoryWindow{
		TerminalID:   req.TerminalID,
		Token:        req.Token,
		Op:           HistoryWindowReplace,
		Cols:         req.Cols,
		Rows:         page,
		Lines:        spansForRows(page),
		Generation:   projection.Generation,
		Boundary:     boundary,
		HasMore:      start > 0,
		LogicalTotal: len(rows),
		Timestamp:    time.Now().UTC(),
	}
}

func (row ScreenProjectionRow) historyRow(cols int) HistoryRow {
	screenCols := cols
	if row.Kind != LineKindOrdinary && row.ScreenCols > 0 {
		screenCols = row.ScreenCols
	}
	return HistoryRow{
		Cells:              cloneHistoryCells(row.Cells),
		Kind:               row.Kind,
		Segment:            row.Segment,
		LineID:             row.LineID,
		RowInLine:          row.RowInLine,
		ProjectionRowIndex: row.ProjectionRowIndex,
		FixedGrid:          row.Kind != LineKindOrdinary,
		ScreenCols:         screenCols,
		ScreenRow:          row.ScreenRow,
		ScreenRowSet:       true,
		Committed:          row.Sealed,
		Wrapped:            row.Wrapped,
	}
}

type screenProjectionBuilder struct {
	projection       ScreenHistoryProjection
	seenRows         map[uint64]struct{}
	currentLineIndex int
	lastSegment      HistorySegment
	lastWrapped      bool
}

func (builder *screenProjectionBuilder) addPhysicalRows(rows []PhysicalRow, segment HistorySegment, sealed bool) {
	builder.currentLineIndex = -1
	builder.lastWrapped = false
	for screenRow, row := range rows {
		if !screenProjectionShouldIncludeRow(row, sealed) {
			builder.lastWrapped = false
			continue
		}
		if row.ID == 0 {
			continue
		}
		if _, exists := builder.seenRows[row.ID]; exists {
			// 中文说明：latest projection 拼接 sealed/current 时以 RowID 去重；
			// 文本相同与否不参与 correctness 判断。
			builder.lastWrapped = false
			continue
		}
		builder.seenRows[row.ID] = struct{}{}
		effectiveSegment := screenProjectionSegment(row, segment)
		kind := screenProjectionLineKind(row, effectiveSegment)
		continues := builder.currentLineIndex >= 0 &&
			builder.lastSegment == effectiveSegment &&
			(row.Continued || builder.lastWrapped)
		if !continues {
			builder.startLine(row, effectiveSegment, kind, sealed)
		}
		builder.appendRow(row, effectiveSegment, kind, sealed, screenRow)
		builder.lastSegment = effectiveSegment
		builder.lastWrapped = row.Wrapped
	}
}

func (builder *screenProjectionBuilder) startLine(row PhysicalRow, segment HistorySegment, kind LineKind, sealed bool) {
	line := ScreenLogicalLine{
		LineID:     LogicalLineID(row.ID),
		Segment:    segment,
		Kind:       kind,
		Sealed:     sealed,
		ScreenCols: builder.projection.Cols,
	}
	builder.projection.Lines = append(builder.projection.Lines, line)
	builder.currentLineIndex = len(builder.projection.Lines) - 1
}

func (builder *screenProjectionBuilder) appendRow(row PhysicalRow, segment HistorySegment, kind LineKind, sealed bool, screenRow int) {
	line := &builder.projection.Lines[builder.currentLineIndex]
	projected := ScreenProjectionRow{
		LineID:             line.LineID,
		RowID:              row.ID,
		Version:            row.Version,
		Cells:              cloneHistoryCells(row.Cells),
		OwnerKind:          row.OwnerKind,
		Segment:            segment,
		Kind:               kind,
		RowInLine:          len(line.Rows),
		ProjectionRowIndex: len(builder.projection.Rows),
		ScreenRow:          screenRow,
		ScreenCols:         row.ScreenCols,
		Sealed:             sealed || row.Sealed,
		Wrapped:            row.Wrapped,
		Continued:          row.Continued,
	}
	line.RowIDs = append(line.RowIDs, row.ID)
	line.Versions = append(line.Versions, row.Version)
	line.Rows = append(line.Rows, projected)
	line.Cells = append(line.Cells, cloneHistoryCells(row.Cells)...)
	line.Wrapped = line.Wrapped || row.Wrapped
	line.Continued = line.Continued || row.Continued
	line.Sealed = line.Sealed && projected.Sealed
	builder.projection.Rows = append(builder.projection.Rows, projected)
}

func screenProjectionShouldIncludeRow(row PhysicalRow, sealed bool) bool {
	if sealed || row.Sealed {
		return true
	}
	return len(row.Cells) > 0 || row.Wrapped || row.Continued
}

func screenProjectionLineKind(row PhysicalRow, segment HistorySegment) LineKind {
	switch segment {
	case HistorySegmentCurrentAltFrame:
		return LineKindAltScreenFrame
	case HistorySegmentCurrentPrimaryFrame:
		if row.OwnerKind == RowOwnerShellStream || row.OwnerKind == RowOwnerUnknown {
			return LineKindOrdinary
		}
		return LineKindScreenFrame
	default:
		if row.OwnerKind == RowOwnerPrimaryFrame {
			return LineKindScreenFrame
		}
		return LineKindOrdinary
	}
}

func screenProjectionSegment(row PhysicalRow, segment HistorySegment) HistorySegment {
	if row.SealSegment != "" {
		return row.SealSegment
	}
	if segment == HistorySegmentCommitted && row.OwnerKind == RowOwnerPrimaryFrame {
		// 中文说明：screen buffer 的 sealed row payload 以 RowOwnerKind 保存
		// 来源；投影给 HistoryWindow 时，primary frame 离开 current ownership
		// 的 rows 继续暴露为 archived-primary-frame segment，避免 TUI/CLI 把
		// screen app session 历史误判成普通 committed stream。
		return HistorySegmentArchivedPrimaryFrame
	}
	return segment
}
