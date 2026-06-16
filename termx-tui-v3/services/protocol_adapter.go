package services

import (
	"context"
	"fmt"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type ProtocolHistoryClient interface {
	HistoryWindow(context.Context, protocol.HistoryWindowParams) (*protocol.HistoryWindow, error)
}

type ProtocolStorageClient interface {
	StorageGet(context.Context, protocol.StorageGetParams) (*protocol.StorageEntry, error)
	StoragePut(context.Context, protocol.StoragePutParams) (*protocol.StorageEntry, error)
	Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error)
}

// ProtocolCoreClientAdapter 是真实 protocol history.window 的 service adapter。
type ProtocolCoreClientAdapter struct {
	Client ProtocolHistoryClient
}

func (adapter ProtocolCoreClientAdapter) HistoryLatest(ctx context.Context, req HistoryLatestRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID: req.TerminalID,
		Limit:      req.Rows,
		Cols:       req.Cols,
	})
	if err != nil {
		return HistoryResult{}, err
	}
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryOlder(ctx context.Context, req HistoryOlderRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Token:               req.Token,
		Generation:          req.Generation,
		CursorValid:         req.Cursor.Valid,
		BeforeLineID:        req.Cursor.BeforeLineID,
		BeforeRowInLine:     req.Cursor.BeforeRowInLine,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return HistoryResult{}, err
	}
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) HistoryOldest(ctx context.Context, req HistoryOldestRequest) (HistoryResult, error) {
	window, err := adapter.historyWindow(ctx, protocol.HistoryWindowParams{
		TerminalID:          req.TerminalID,
		Limit:               req.Rows,
		Cols:                req.Cols,
		Token:               req.Token,
		Generation:          req.Generation,
		BoundaryFirstLineID: req.Boundary.FirstLineID,
		BoundaryLastLineID:  req.Boundary.LastLineID,
	})
	if err != nil {
		return HistoryResult{}, err
	}
	return HistoryResult{RequestID: req.RequestID, Window: window}, nil
}

func (adapter ProtocolCoreClientAdapter) historyWindow(ctx context.Context, params protocol.HistoryWindowParams) (state.HistoryWindow, error) {
	window, err := adapter.Client.HistoryWindow(ctx, params)
	if err != nil {
		return state.HistoryWindow{}, err
	}
	return historyWindowFromProtocol(window, params.Cols), nil
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
	rows, lines := state.ReflowHistoryLogicalLines(sourceLines, cols)
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
	spansByLineID := make(map[uint64]protocol.HistoryLineSpan, len(window.Lines))
	for _, span := range window.Lines {
		if span.LogicalLineID == 0 {
			continue
		}
		spansByLineID[span.LogicalLineID] = span
	}
	lines := make([]state.HistoryLogicalLine, 0, len(window.Rows))
	for i, row := range window.Rows {
		lineID := uint64At(window.RowLineIDs, i)
		cells := historyCellsFromProtocol(row.DecodeCells())
		text := historyCellsPlainText(cells)
		span, hasSpan := spansByLineID[lineID]
		// protocol 可能按当前 cols 把一条 logical line 切成多行；这里必须先按
		// stable line id 合回 frozen source，再交给 TUI 本地 reflow。
		if len(lines) > 0 && lineID != 0 && lines[len(lines)-1].LineID == lineID {
			lines[len(lines)-1].Text += text
			lines[len(lines)-1].Cells = append(lines[len(lines)-1].Cells, cloneHistoryCells(cells)...)
			if tail := historyTailFillFromProtocol(row.TailFill); tail != nil {
				lines[len(lines)-1].TailFill = tail
			}
			continue
		}
		lines = append(lines, state.HistoryLogicalLine{
			Text:          text,
			Cells:         cells,
			LineID:        lineID,
			TailFill:      historyTailFillFromProtocol(row.TailFill),
			ClippedBefore: hasSpan && span.ClippedBefore,
			ClippedAfter:  hasSpan && span.ClippedAfter,
		})
	}
	return lines
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

func historyCellsFromProtocol(cells []protocol.Cell) []state.HistoryCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]state.HistoryCell, len(cells))
	for i, cell := range cells {
		out[i] = state.HistoryCell{
			Text:       cell.Content,
			Width:      cell.Width,
			Style:      historyCellStyleFromProtocol(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		}
	}
	return out
}

func historyCellStyleFromProtocol(style protocol.CellStyle) state.HistoryCellStyle {
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
		builder.WriteString(cell.Text)
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

type ProtocolWorkbenchStorageAdapter struct {
	Client ProtocolStorageClient
}

func (adapter ProtocolWorkbenchStorageAdapter) LoadWorkbench(ctx context.Context, ref state.WorkbenchStorageRef) (WorkbenchStorageLoadResult, error) {
	entry, err := adapter.Client.StorageGet(ctx, protocol.StorageGetParams{
		AppID:   ref.AppID,
		Scope:   protocol.StorageScope(ref.Scope),
		OwnerID: ref.OwnerID,
		Key:     ref.Key,
	})
	if err != nil {
		return WorkbenchStorageLoadResult{}, err
	}
	snapshot, err := state.DecodeWorkbenchStorageSnapshot(entry.Value)
	if err != nil {
		return WorkbenchStorageLoadResult{}, err
	}
	return WorkbenchStorageLoadResult{
		Snapshot: snapshot,
		Version:  entry.Version,
		Found:    true,
	}, nil
}

type ProtocolClipboardStorageAdapter struct {
	Client ProtocolStorageClient
}

func (adapter ProtocolClipboardStorageAdapter) LoadClipboard(ctx context.Context, ref state.ClipboardStorageRef) (ClipboardStorageLoadResult, error) {
	entry, err := adapter.Client.StorageGet(ctx, protocol.StorageGetParams{
		AppID:   ref.AppID,
		Scope:   protocol.StorageScope(ref.Scope),
		OwnerID: ref.OwnerID,
		Key:     ref.Key,
	})
	if err != nil {
		if isStorageNotFound(err) {
			return ClipboardStorageLoadResult{Found: false}, nil
		}
		return ClipboardStorageLoadResult{}, err
	}
	if entry == nil || len(entry.Value) == 0 {
		return ClipboardStorageLoadResult{Found: false}, nil
	}
	snapshot, err := state.DecodeClipboardStorageSnapshot(entry.Value)
	if err != nil {
		return ClipboardStorageLoadResult{}, err
	}
	return ClipboardStorageLoadResult{
		Snapshot: snapshot,
		Version:  entry.Version,
		Found:    true,
	}, nil
}

func (adapter ProtocolWorkbenchStorageAdapter) SaveWorkbench(ctx context.Context, req WorkbenchStorageSaveRequest) (WorkbenchStorageSaveResult, error) {
	value, err := state.EncodeWorkbenchStorageSnapshotValue(req.Snapshot)
	if err != nil {
		return WorkbenchStorageSaveResult{}, err
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
			return WorkbenchStorageSaveResult{}, fmt.Errorf("%w: %v", ErrWorkbenchStorageConflict, err)
		}
		return WorkbenchStorageSaveResult{}, err
	}
	return WorkbenchStorageSaveResult{
		Ref:     req.Ref.WithVersion(entry.Version),
		Version: entry.Version,
	}, nil
}

func (adapter ProtocolClipboardStorageAdapter) SaveClipboard(ctx context.Context, req ClipboardStorageSaveRequest) (ClipboardStorageSaveResult, error) {
	value, err := state.EncodeClipboardStorageSnapshotValue(req.Snapshot)
	if err != nil {
		return ClipboardStorageSaveResult{}, err
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
			return ClipboardStorageSaveResult{}, fmt.Errorf("%w: %v", ErrClipboardStorageConflict, err)
		}
		return ClipboardStorageSaveResult{}, err
	}
	return ClipboardStorageSaveResult{
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

func (adapter ProtocolWorkbenchStorageAdapter) WatchWorkbench(ctx context.Context, ref state.WorkbenchStorageRef) (<-chan WorkbenchStorageEvent, error) {
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
	out := make(chan WorkbenchStorageEvent, 16)
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
				changed := WorkbenchStorageEvent{
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

func (adapter ProtocolClipboardStorageAdapter) WatchClipboard(ctx context.Context, ref state.ClipboardStorageRef) (<-chan ClipboardStorageEvent, error) {
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
	out := make(chan ClipboardStorageEvent, 16)
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
				changed := ClipboardStorageEvent{
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
