package protocoladapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	xansi "github.com/charmbracelet/x/ansi"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/shared/perftrace"
	"github.com/lozzow/termx/tui/port"
	"github.com/lozzow/termx/tui/state"
)

type ProtocolHistoryClient interface {
	HistoryWindow(context.Context, protocol.HistoryWindowParams) (*protocol.HistoryWindow, error)
	HistoryCopy(context.Context, protocol.HistoryWindowParams) (string, error)
	ReleaseHistory(context.Context, protocol.HistoryWindowParams) error
}

type ProtocolStorageClient interface {
	StorageGet(context.Context, protocol.StorageGetParams) (*protocol.StorageEntry, error)
	StoragePut(context.Context, protocol.StoragePutParams) (*protocol.StorageEntry, error)
	Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error)
}

// ProtocolPathServiceAdapter 把 endpoint path completion service 映射到 core-v2 protocol。
// adapter 不读取本地文件系统；Prefix 直接交给当前 protocol session 所属 daemon，
// 保证 SSH/hub endpoint 的目录候选来自远端机器。
type ProtocolPathServiceAdapter struct {
	Application *clientruntime.ApplicationSession
}

// NewProtocolPathServiceAdapter 复用 terminal application session 查询 owning endpoint 的路径和默认值。
// 该构造函数不接受裸 protocol client，防止 adapter 丢失 route/generation fence。
func NewProtocolPathServiceAdapter(application *clientruntime.ApplicationSession) (ProtocolPathServiceAdapter, error) {
	if application == nil {
		return ProtocolPathServiceAdapter{}, fmt.Errorf("missing path application session")
	}
	return ProtocolPathServiceAdapter{Application: application}, nil
}

// ListDirectories 返回当前 daemon 文件系统中的目录候选。
// EndpointID 已由 client runtime adapter 剥离；如果 Client 缺失，调用方会在 prompt 内展示失败。
func (adapter ProtocolPathServiceAdapter) ListDirectories(ctx context.Context, req port.PathListDirectoriesRequest) (port.PathListDirectoriesResult, error) {
	if adapter.Application == nil {
		return port.PathListDirectoriesResult{}, fmt.Errorf("missing path client")
	}
	result, err := adapter.Application.PathListDirectories(ctx, &apipb.PathListDirectoriesCommand{Prefix: req.Prefix, Limit: int32(req.Limit)})
	if err != nil {
		return port.PathListDirectoriesResult{}, err
	}
	out := port.PathListDirectoriesResult{
		EndpointID: req.EndpointID, BasePath: result.GetBasePath(), Missing: result.GetMissing(), Truncated: result.GetTruncated(),
		Entries: make([]port.PathDirectoryEntry, 0, len(result.GetEntries())),
	}
	for _, entry := range result.GetEntries() {
		out.Entries = append(out.Entries, port.PathDirectoryEntry{Name: entry.GetName(), Path: entry.GetPath()})
	}
	return out, nil
}

// Defaults 返回当前 daemon 进程所在机器的创建默认值。
// EndpointID 已由 client runtime adapter 剥离；adapter 只消费 protocol 投影，不读取 TUI 本地环境。
func (adapter ProtocolPathServiceAdapter) Defaults(ctx context.Context, req port.PathDefaultsRequest) (port.PathDefaultsResult, error) {
	if adapter.Application == nil {
		return port.PathDefaultsResult{}, fmt.Errorf("missing path client")
	}
	result, err := adapter.Application.TerminalDefaults(ctx, &apipb.TerminalDefaultsCommand{})
	if err != nil {
		return port.PathDefaultsResult{}, err
	}
	return port.PathDefaultsResult{
		EndpointID: req.EndpointID, DefaultCommand: append([]string(nil), result.GetDefaults().GetDefaultCommand()...), DefaultCWD: result.GetDefaults().GetDefaultCwd(),
	}, nil
}

// ProtocolCoreClientAdapter 是真实 protocol history.window 的 service adapter。
type ProtocolCoreClientAdapter struct {
	Client ProtocolHistoryClient
}

func (adapter ProtocolCoreClientAdapter) HistoryLatest(ctx context.Context, req port.HistoryLatestRequest) (port.HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID: req.TerminalID,
		Limit:      req.Rows,
		Cols:       req.Cols,
		Generation: req.GenerationBoundary,
	})
	if err != nil {
		return port.HistoryResult{RequestID: req.RequestID}, normalizeProtocolHistoryError(err)
	}
	return port.HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryOlder(ctx context.Context, req port.HistoryOlderRequest) (port.HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Mode:                "older",
		Token:               req.Token,
		Generation:          req.Generation,
		CursorValid:         req.Cursor.Valid,
		BeforeLineID:        req.Cursor.BeforeLineID,
		BeforeRowInLine:     req.Cursor.BeforeRowInLine,
		BeforeRowIndex:      req.Cursor.BeforeRowIndex,
		CursorSegment:       req.Cursor.Segment,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return port.HistoryResult{RequestID: req.RequestID}, normalizeProtocolHistoryError(err)
	}
	return port.HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryNewer(ctx context.Context, req port.HistoryNewerRequest) (port.HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Mode:                "newer",
		Token:               req.Token,
		Generation:          req.Generation,
		AfterCursorValid:    req.Cursor.Valid,
		AfterLineID:         req.Cursor.BeforeLineID,
		AfterRowInLine:      req.Cursor.BeforeRowInLine,
		AfterRowIndex:       req.Cursor.BeforeRowIndex,
		AfterCursorSegment:  req.Cursor.Segment,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return port.HistoryResult{RequestID: req.RequestID}, normalizeProtocolHistoryError(err)
	}
	return port.HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryOldest(ctx context.Context, req port.HistoryOldestRequest) (port.HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Mode:                "oldest",
		Token:               req.Token,
		Generation:          req.Generation,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return port.HistoryResult{RequestID: req.RequestID}, normalizeProtocolHistoryError(err)
	}
	return port.HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) ReleaseHistory(ctx context.Context, req port.HistoryReleaseRequest) error {
	if adapter.Client == nil || req.Token == "" {
		return nil
	}
	return adapter.Client.ReleaseHistory(ctx, protocol.HistoryWindowParams{
		TerminalID: req.TerminalID,
		Token:      req.Token,
	})
}

func (adapter ProtocolCoreClientAdapter) HistoryCopyRange(ctx context.Context, req port.HistoryCopyRangeRequest) (port.HistoryCopyRangeResult, error) {
	text, err := adapter.Client.HistoryCopy(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Cols:                req.Cols,
		Token:               req.Token,
		Generation:          req.Generation,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
		RangeValid:          req.Start.Valid && req.End.Valid,
		RangeStartLineID:    req.Start.LineID,
		RangeStartCol:       req.Start.Col,
		RangeEndLineID:      req.End.LineID,
		RangeEndCol:         req.End.Col,
	})
	if err != nil {
		return port.HistoryCopyRangeResult{}, normalizeProtocolHistoryError(err)
	}
	return port.HistoryCopyRangeResult{Text: text}, nil
}

func (adapter ProtocolCoreClientAdapter) historyWindow(ctx context.Context, params protocol.HistoryWindowParams) (state.HistoryWindow, error) {
	mode := protocolHistoryWindowPerfMode(params.Mode)
	finishRPC := perftrace.Measure("tui.protocol.history_window." + mode + ".rpc")
	window, err := adapter.Client.HistoryWindow(ctx, params)
	if window != nil {
		finishRPC(len(window.Rows))
		perftrace.Count("tui.protocol.history_window."+mode+".rows", len(window.Rows))
	} else {
		finishRPC(0)
	}
	if err != nil {
		return state.HistoryWindow{}, normalizeProtocolHistoryError(err)
	}
	finishConvert := perftrace.Measure("tui.protocol.history_window." + mode + ".convert")
	converted := historyWindowFromProtocol(window, params.Cols)
	finishConvert(len(converted.Rows))
	return converted, nil
}

func protocolHistoryWindowPerfMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", "latest":
		return "latest"
	case "older":
		return "older"
	case "newer":
		return "newer"
	case "oldest":
		return "oldest"
	default:
		return "other"
	}
}

func normalizeProtocolHistoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, port.ErrStaleHistoryWindow) {
		return err
	}
	if strings.Contains(strings.ToLower(err.Error()), port.ErrStaleHistoryWindow.Error()) {
		// 中文说明：core 用 stale history window 拒绝过期 token/cursor，这是历史会话控制信号；
		// TUI 需要按 typed sentinel 处理，不能把 protocol 400 直接显示给用户。
		return fmt.Errorf("%w: %v", port.ErrStaleHistoryWindow, err)
	}
	return err
}

func historyWindowFromProtocol(window *protocol.HistoryWindow, requestedCols int) state.HistoryWindow {
	if window == nil {
		return state.HistoryWindow{}
	}
	cols := requestedCols
	if cols <= 0 {
		cols = int(window.Size.Cols)
	}
	sourceLines := historySourceLinesFromProtocol(window)
	rows, lines := historyRowsFromProtocol(window, sourceLines, cols)
	return state.HistoryWindow{
		TerminalID:  window.TerminalID,
		Token:       window.Token,
		Op:          state.HistoryWindowOp(window.Op),
		Cols:        cols,
		SourceLines: sourceLines,
		Rows:        rows,
		Lines:       lines,
		Cursor: state.HistoryCursor{
			Valid:           window.CursorValid,
			BeforeLineID:    window.CursorLineID,
			BeforeRowInLine: window.CursorRow,
			BeforeRowIndex:  window.CursorRowIndex,
			Segment:         window.CursorSegment,
		},
		HasMore:    window.HasMore,
		Generation: window.Generation,
		Boundary: state.HistoryBoundary{
			FirstLineID: window.FirstLineID,
			LastLineID:  window.LastLineID,
		},
		LoadedLines: window.LoadedLines,
		TotalLines:  window.LogicalTotal,
	}
}

func historySourceLinesFromProtocol(window *protocol.HistoryWindow) []state.HistoryLogicalLine {
	if window == nil || len(window.Rows) == 0 {
		return nil
	}
	lines := make([]state.HistoryLogicalLine, 0, len(window.Rows))
	for i, row := range window.Rows {
		lineID := uint64At(window.RowLineIDs, i)
		text, cells := historyTextAndCellsFromCompactRow(row)
		span, hasSpan := historyProtocolSpanForRow(window.Lines, i, lineID)
		nextLine := state.HistoryLogicalLine{
			Text:               text,
			Cells:              cells,
			LineID:             lineID,
			Kind:               historyProtocolRowKind(window, i, span, hasSpan),
			Segment:            stringAt(window.RowSegments, i),
			SessionID:          uint64At(window.RowSessionIDs, i),
			FrameID:            uint64At(window.RowFrameIDs, i),
			FixedGrid:          boolAt(window.RowFixedGrid, i) || (hasSpan && span.FixedGrid),
			ScreenCols:         intAt(window.RowScreenCols, i),
			ScreenRow:          intAt(window.RowScreenRows, i),
			ScreenRowSet:       boolAt(window.RowScreenRowSet, i),
			ProjectionRowIndex: intAt(window.RowIndexes, i),
			TailFill:           historyTailFillFromProtocol(row.TailFill),
			LiveTail:           historyProtocolRowIsLiveTail(window, i),
			ClippedBefore:      hasSpan && span.ClippedBefore,
			ClippedAfter:       hasSpan && span.ClippedAfter,
		}
		if nextLine.ScreenCols == 0 && hasSpan {
			nextLine.ScreenCols = span.ScreenCols
		}
		// protocol 可能按当前 cols 把一条 logical line 切成多行；这里必须先按
		// stable source identity 合回 frozen source，再交给 TUI 本地 reflow。
		if len(lines) > 0 && sameProtocolHistorySource(lines[len(lines)-1], nextLine) {
			appendHistoryProtocolSegment(&lines[len(lines)-1], text, cells)
			if tail := historyTailFillFromProtocol(row.TailFill); tail != nil {
				lines[len(lines)-1].TailFill = tail
			}
			lines[len(lines)-1].LiveTail = lines[len(lines)-1].LiveTail || historyProtocolRowIsLiveTail(window, i)
			if nextLine.ScreenRowSet {
				lines[len(lines)-1].ScreenRow = nextLine.ScreenRow
				lines[len(lines)-1].ScreenRowSet = true
			}
			continue
		}
		lines = append(lines, nextLine)
	}
	return lines
}

func sameProtocolHistorySource(left state.HistoryLogicalLine, right state.HistoryLogicalLine) bool {
	return left.LineID != 0 &&
		left.LineID == right.LineID &&
		left.Kind == right.Kind &&
		left.Segment == right.Segment &&
		left.SessionID == right.SessionID &&
		left.FrameID == right.FrameID &&
		left.FixedGrid == right.FixedGrid &&
		(!left.FixedGrid || left.ScreenCols == right.ScreenCols)
}

func historyProtocolSpanForRow(spans []protocol.HistoryLineSpan, rowIndex int, lineID uint64) (protocol.HistoryLineSpan, bool) {
	for _, span := range spans {
		if span.LogicalLineID != 0 && lineID != 0 && span.LogicalLineID != lineID {
			continue
		}
		if rowIndex >= span.StartRow && rowIndex <= span.EndRow {
			return span, true
		}
	}
	return protocol.HistoryLineSpan{}, false
}

func appendHistoryProtocolSegment(line *state.HistoryLogicalLine, text string, cells []state.HistoryCell) {
	if line == nil {
		return
	}
	if len(line.Cells) > 0 && len(cells) == 0 && text != "" {
		cells = []state.HistoryCell{{Text: text, Width: displayWidthForProtocolHistoryText(text)}}
	}
	if len(line.Cells) == 0 && len(cells) > 0 && line.Text != "" {
		// 中文说明：同一 logical line 中只要任一 protocol row 需要 cell 元数据，
		// source line 的 Cells 就必须覆盖完整 Text，否则后续本地 reflow 会忽略纯文本前缀。
		line.Cells = []state.HistoryCell{{Text: line.Text, Width: displayWidthForProtocolHistoryText(line.Text)}}
	}
	line.Text += text
	line.Cells = append(line.Cells, cells...)
}

func historyRowsFromProtocol(window *protocol.HistoryWindow, sourceLines []state.HistoryLogicalLine, cols int) ([]state.HistoryRow, []state.HistoryLineSpan) {
	if window == nil || len(window.Rows) == 0 {
		return nil, nil
	}
	if cols != int(window.Size.Cols) {
		return state.ReflowHistoryLogicalLines(sourceLines, cols)
	}
	rows := make([]state.HistoryRow, 0, len(window.Rows))
	for i, row := range window.Rows {
		text, cells := historyTextAndCellsFromCompactRow(row)
		rows = append(rows, state.HistoryRow{
			Text:               text,
			Cells:              cells,
			TailFill:           historyTailFillFromProtocol(row.TailFill),
			LineID:             uint64At(window.RowLineIDs, i),
			RowInLine:          intAt(window.RowInLine, i),
			Kind:               stringAt(window.RowKinds, i),
			Segment:            stringAt(window.RowSegments, i),
			SessionID:          uint64At(window.RowSessionIDs, i),
			FrameID:            uint64At(window.RowFrameIDs, i),
			FixedGrid:          boolAt(window.RowFixedGrid, i),
			ScreenCols:         intAt(window.RowScreenCols, i),
			ScreenRow:          intAt(window.RowScreenRows, i),
			ScreenRowSet:       boolAt(window.RowScreenRowSet, i),
			ProjectionRowIndex: intAt(window.RowIndexes, i),
			LiveTail:           historyProtocolRowIsLiveTail(window, i),
		})
	}
	lines := make([]state.HistoryLineSpan, 0, len(window.Lines))
	for _, span := range window.Lines {
		lines = append(lines, state.HistoryLineSpan{
			LineID:             span.LogicalLineID,
			StartRow:           span.StartRow,
			EndRow:             span.EndRow,
			Kind:               span.RowKind,
			Segment:            stringAt(window.RowSegments, span.StartRow),
			SessionID:          firstNonZeroUint64(span.SessionID, uint64At(window.RowSessionIDs, span.StartRow)),
			FrameID:            firstNonZeroUint64(span.FrameID, uint64At(window.RowFrameIDs, span.StartRow)),
			FixedGrid:          span.FixedGrid || boolAt(window.RowFixedGrid, span.StartRow),
			ScreenCols:         firstNonZeroInt(span.ScreenCols, intAt(window.RowScreenCols, span.StartRow)),
			ScreenRow:          intAt(window.RowScreenRows, span.StartRow),
			ScreenRowSet:       boolAt(window.RowScreenRowSet, span.StartRow),
			ProjectionRowIndex: intAt(window.RowIndexes, span.StartRow),
			ClippedBefore:      span.ClippedBefore,
			ClippedAfter:       span.ClippedAfter,
		})
	}
	if len(lines) == 0 {
		lines = historyLineSpansFromRows(rows, sourceLines)
	}
	return rows, lines
}

func historyProtocolRowIsLiveTail(window *protocol.HistoryWindow, index int) bool {
	if window == nil || index < 0 || index >= len(window.RowOwnership) {
		return false
	}
	return protocol.RowOwnershipIsLiveTailLive(window.RowOwnership[index])
}

func historyProtocolRowKind(window *protocol.HistoryWindow, index int, span protocol.HistoryLineSpan, hasSpan bool) string {
	if kind := stringAt(window.RowKinds, index); kind != "" {
		return kind
	}
	if hasSpan {
		return span.RowKind
	}
	return ""
}

func historyLineSpansFromRows(rows []state.HistoryRow, sourceLines []state.HistoryLogicalLine) []state.HistoryLineSpan {
	if len(rows) == 0 {
		return nil
	}
	spans := make([]state.HistoryLineSpan, 0, len(rows))
	start := 0
	current := rows[0].LineID
	for row := 1; row < len(rows); row++ {
		if rows[row].LineID == current &&
			rows[row].Kind == rows[start].Kind &&
			rows[row].Segment == rows[start].Segment &&
			rows[row].SessionID == rows[start].SessionID &&
			rows[row].FrameID == rows[start].FrameID &&
			rows[row].FixedGrid == rows[start].FixedGrid &&
			(!rows[row].FixedGrid || rows[row].ScreenCols == rows[start].ScreenCols) {
			continue
		}
		spans = append(spans, historyLineSpanFromRowGroup(rows, current, start, row-1, sourceLines))
		start = row
		current = rows[row].LineID
	}
	spans = append(spans, historyLineSpanFromRowGroup(rows, current, start, len(rows)-1, sourceLines))
	return spans
}

func historyLineSpanFromRowGroup(rows []state.HistoryRow, lineID uint64, start int, end int, sourceLines []state.HistoryLogicalLine) state.HistoryLineSpan {
	span := state.HistoryLineSpan{LineID: lineID, StartRow: start, EndRow: end}
	if start >= 0 && start < len(rows) {
		span.Kind = rows[start].Kind
		span.Segment = rows[start].Segment
		span.SessionID = rows[start].SessionID
		span.FrameID = rows[start].FrameID
		span.FixedGrid = rows[start].FixedGrid
		span.ScreenCols = rows[start].ScreenCols
		span.ProjectionRowIndex = rows[start].ProjectionRowIndex
	}
	if lineID == 0 {
		return span
	}
	if line, ok := historySourceLineForRow(sourceLines, rows[start]); ok && (line.ClippedBefore || line.ClippedAfter) {
		span.Kind = line.Kind
		span.Segment = line.Segment
		span.SessionID = line.SessionID
		span.FrameID = line.FrameID
		span.FixedGrid = line.FixedGrid
		span.ScreenCols = line.ScreenCols
		span.ProjectionRowIndex = line.ProjectionRowIndex
		span.ClippedBefore = line.ClippedBefore
		span.ClippedAfter = line.ClippedAfter
	}
	return span
}

func historySourceLineForRow(lines []state.HistoryLogicalLine, row state.HistoryRow) (state.HistoryLogicalLine, bool) {
	for _, line := range lines {
		if line.LineID != 0 &&
			line.LineID == row.LineID &&
			line.Kind == row.Kind &&
			line.Segment == row.Segment &&
			line.SessionID == row.SessionID &&
			line.FrameID == row.FrameID &&
			line.FixedGrid == row.FixedGrid &&
			(!line.FixedGrid || line.ScreenCols == row.ScreenCols) {
			return line, true
		}
	}
	return state.HistoryLogicalLine{}, false
}

func historyTailFillFromProtocol(style *protocol.CompactRowStyle) *state.HistoryCellStyle {
	if style == nil {
		return nil
	}
	out := state.HistoryCellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
	if out == (state.HistoryCellStyle{}) {
		return nil
	}
	return &out
}

func historyTextAndCellsFromCompactRow(row protocol.CompactRow) (string, []state.HistoryCell) {
	if row.Text != "" {
		// 中文说明：plain compact row 已经是无样式单宽文本，Text 本身足够支撑
		// search/copy/reflow；不物化 per-cell payload，避免大历史窗口常驻 cells。
		return row.Text, nil
	}
	if len(row.Runs) > 0 {
		cells := historyCellsFromCompactRuns(row.Runs)
		return historyCellsPlainText(cells), cells
	}
	if len(row.Cells) == 0 {
		return "", nil
	}
	out := make([]state.HistoryCell, len(row.Cells))
	for i, cell := range row.Cells {
		out[i] = state.HistoryCell{
			Text:       cell.Content,
			Width:      cell.Width,
			Style:      historyCellStyleFromCompact(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		}
	}
	return historyCellsPlainText(out), out
}

func historyCellsFromCompactRuns(runs []protocol.CompactRowRun) []state.HistoryCell {
	out := make([]state.HistoryCell, 0, compactRunCellCount(runs))
	for _, run := range runs {
		style := historyCellStyleFromCompact(run.Style)
		for len(run.Text) > 0 {
			cluster, width := xansi.FirstGraphemeCluster(run.Text, xansi.GraphemeWidth)
			if cluster == "" {
				break
			}
			if width < 0 {
				width = 0
			}
			out = append(out, state.HistoryCell{
				Text:       cluster,
				Width:      width,
				Style:      style,
				LinkURL:    run.LinkURL,
				LinkParams: run.LinkParams,
			})
			run.Text = run.Text[len(cluster):]
		}
	}
	return out
}

func compactRunCellCount(runs []protocol.CompactRowRun) int {
	total := 0
	for _, run := range runs {
		total += utf8.RuneCountInString(run.Text)
	}
	return total
}

func historyCellStyleFromCompact(style *protocol.CompactRowStyle) state.HistoryCellStyle {
	if style == nil {
		return state.HistoryCellStyle{}
	}
	return state.HistoryCellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func historyCellsPlainText(cells []state.HistoryCell) string {
	var builder strings.Builder
	for _, cell := range cells {
		if cell.Text == "" {
			continue
		}
		builder.WriteString(cell.Text)
		if cell.Text == " " {
			continue
		}
		if pad := state.HistoryCellDisplayWidth(cell) - displayWidthForProtocolHistoryText(cell.Text); pad > 0 {
			builder.WriteString(strings.Repeat(" ", pad))
		}
	}
	return builder.String()
}

func displayWidthForProtocolHistoryText(text string) int {
	return xansi.StringWidth(strings.ReplaceAll(text, "\n", " "))
}

func cloneHistoryCells(cells []state.HistoryCell) []state.HistoryCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]state.HistoryCell, len(cells))
	copy(out, cells)
	return out
}

func uint64At(values []uint64, index int) uint64 {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func intAt(values []int, index int) int {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func boolAt(values []bool, index int) bool {
	if index < 0 || index >= len(values) {
		return false
	}
	return values[index]
}

func firstNonZeroUint64(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func stringAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

type ProtocolWorkbenchStorageAdapter struct {
	Client ProtocolStorageClient
}

func (adapter ProtocolWorkbenchStorageAdapter) LoadWorkbench(ctx context.Context, ref state.WorkbenchStorageRef) (port.WorkbenchStorageLoadResult, error) {
	entry, err := adapter.Client.StorageGet(ctx, protocol.StorageGetParams{
		AppID:   ref.AppID,
		Scope:   protocol.StorageScope(ref.Scope),
		OwnerID: ref.OwnerID,
		Key:     ref.Key,
	})
	if err != nil {
		if isStorageNotFound(err) {
			return port.WorkbenchStorageLoadResult{Found: false}, nil
		}
		return port.WorkbenchStorageLoadResult{}, err
	}
	if entry == nil || len(entry.Value) == 0 {
		return port.WorkbenchStorageLoadResult{Found: false}, nil
	}
	snapshot, err := state.DecodeWorkbenchStorageSnapshot(entry.Value)
	if err != nil {
		return port.WorkbenchStorageLoadResult{}, err
	}
	return port.WorkbenchStorageLoadResult{
		Snapshot: snapshot,
		Version:  entry.Version,
		Found:    true,
	}, nil
}

type ProtocolClipboardStorageAdapter struct {
	Client ProtocolStorageClient
}

func (adapter ProtocolClipboardStorageAdapter) LoadClipboard(ctx context.Context, ref state.ClipboardStorageRef) (port.ClipboardStorageLoadResult, error) {
	entry, err := adapter.Client.StorageGet(ctx, protocol.StorageGetParams{
		AppID:   ref.AppID,
		Scope:   protocol.StorageScope(ref.Scope),
		OwnerID: ref.OwnerID,
		Key:     ref.Key,
	})
	if err != nil {
		if isStorageNotFound(err) {
			return port.ClipboardStorageLoadResult{Found: false}, nil
		}
		return port.ClipboardStorageLoadResult{}, err
	}
	if entry == nil || len(entry.Value) == 0 {
		return port.ClipboardStorageLoadResult{Found: false}, nil
	}
	snapshot, err := state.DecodeClipboardStorageSnapshot(entry.Value)
	if err != nil {
		return port.ClipboardStorageLoadResult{}, err
	}
	return port.ClipboardStorageLoadResult{
		Snapshot: snapshot,
		Version:  entry.Version,
		Found:    true,
	}, nil
}

func (adapter ProtocolWorkbenchStorageAdapter) SaveWorkbench(ctx context.Context, req port.WorkbenchStorageSaveRequest) (port.WorkbenchStorageSaveResult, error) {
	value, err := state.EncodeWorkbenchStorageSnapshotValue(req.Snapshot)
	if err != nil {
		return port.WorkbenchStorageSaveResult{}, err
	}
	entry, err := adapter.Client.StoragePut(ctx, protocol.StoragePutParams{
		AppID:           req.Ref.AppID,
		Scope:           protocol.StorageScope(req.Ref.Scope),
		OwnerID:         req.Ref.OwnerID,
		Key:             req.Ref.Key,
		Value:           value,
		CheckVersion:    req.CheckVersion,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		if isStorageVersionConflict(err) {
			return port.WorkbenchStorageSaveResult{}, fmt.Errorf("%w: %v", port.ErrWorkbenchStorageConflict, err)
		}
		return port.WorkbenchStorageSaveResult{}, err
	}
	return port.WorkbenchStorageSaveResult{
		Ref:     req.Ref.WithVersion(entry.Version),
		Version: entry.Version,
	}, nil
}

func (adapter ProtocolClipboardStorageAdapter) SaveClipboard(ctx context.Context, req port.ClipboardStorageSaveRequest) (port.ClipboardStorageSaveResult, error) {
	value, err := state.EncodeClipboardStorageSnapshotValue(req.Snapshot)
	if err != nil {
		return port.ClipboardStorageSaveResult{}, err
	}
	entry, err := adapter.Client.StoragePut(ctx, protocol.StoragePutParams{
		AppID:           req.Ref.AppID,
		Scope:           protocol.StorageScope(req.Ref.Scope),
		OwnerID:         req.Ref.OwnerID,
		Key:             req.Ref.Key,
		Value:           value,
		CheckVersion:    req.CheckVersion,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		if isStorageVersionConflict(err) {
			return port.ClipboardStorageSaveResult{}, fmt.Errorf("%w: %v", port.ErrClipboardStorageConflict, err)
		}
		return port.ClipboardStorageSaveResult{}, err
	}
	return port.ClipboardStorageSaveResult{
		Ref:     req.Ref.WithVersion(entry.Version),
		Version: entry.Version,
	}, nil
}

func isStorageVersionConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "storage version conflict")
}

func isStorageNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "storage entry not found")
}

func (adapter ProtocolWorkbenchStorageAdapter) WatchWorkbench(ctx context.Context, ref state.WorkbenchStorageRef) (<-chan port.WorkbenchStorageEvent, error) {
	events, err := adapter.Client.Events(ctx, protocol.EventsParams{
		Types:            []protocol.EventType{protocol.EventStorageChanged},
		StorageAppID:     ref.AppID,
		StorageScope:     protocol.StorageScope(ref.Scope),
		StorageOwnerID:   ref.OwnerID,
		StorageKeyPrefix: ref.KeyPrefix(),
	})
	if err != nil {
		return nil, err
	}
	out := make(chan port.WorkbenchStorageEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Storage == nil {
					continue
				}
				changed := port.WorkbenchStorageEvent{
					Ref: state.WorkbenchStorageRef{
						AppID:   event.Storage.AppID,
						Scope:   string(event.Storage.Scope),
						OwnerID: event.Storage.OwnerID,
						Key:     event.Storage.Key,
						Version: event.Storage.Version,
					},
					Version: event.Storage.Version,
					Op:      event.Storage.Op,
				}
				select {
				case out <- changed:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (adapter ProtocolClipboardStorageAdapter) WatchClipboard(ctx context.Context, ref state.ClipboardStorageRef) (<-chan port.ClipboardStorageEvent, error) {
	events, err := adapter.Client.Events(ctx, protocol.EventsParams{
		Types:            []protocol.EventType{protocol.EventStorageChanged},
		StorageAppID:     ref.AppID,
		StorageScope:     protocol.StorageScope(ref.Scope),
		StorageOwnerID:   ref.OwnerID,
		StorageKeyPrefix: ref.KeyPrefix(),
	})
	if err != nil {
		return nil, err
	}
	out := make(chan port.ClipboardStorageEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Storage == nil {
					continue
				}
				changed := port.ClipboardStorageEvent{
					Ref: state.ClipboardStorageRef{
						AppID:   event.Storage.AppID,
						Scope:   string(event.Storage.Scope),
						OwnerID: event.Storage.OwnerID,
						Key:     event.Storage.Key,
						Version: event.Storage.Version,
					},
					Version: event.Storage.Version,
					Op:      event.Storage.Op,
				}
				select {
				case out <- changed:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
