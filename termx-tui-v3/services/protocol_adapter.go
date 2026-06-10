package services

import (
	"context"
	"fmt"
	"strings"

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

func (adapter ProtocolCoreClientAdapter) historyWindow(ctx context.Context, params protocol.HistoryWindowParams) (state.HistoryWindow, error) {
	window, err := adapter.Client.HistoryWindow(ctx, params)
	if err != nil {
		return state.HistoryWindow{}, err
	}
	return historyWindowFromProtocol(window), nil
}

func historyWindowFromProtocol(window *protocol.HistoryWindow) state.HistoryWindow {
	if window == nil {
		return state.HistoryWindow{}
	}
	rows := make([]state.HistoryRow, len(window.Rows))
	for i, row := range window.Rows {
		cells := historyCellsFromProtocol(row.DecodeCells())
		rows[i] = state.HistoryRow{
			Text:      historyCellsPlainText(cells),
			Cells:     cells,
			LineID:    uint64At(window.RowLineIDs, i),
			RowInLine: intAt(window.RowInLine, i),
		}
	}
	lines := make([]state.HistoryLineSpan, len(window.Lines))
	for i, span := range window.Lines {
		lines[i] = state.HistoryLineSpan{
			LineID:        span.LogicalLineID,
			StartRow:      span.StartRow,
			EndRow:        span.EndRow,
			ClippedBefore: span.ClippedBefore,
			ClippedAfter:  span.ClippedAfter,
		}
	}
	return state.HistoryWindow{
		TerminalID: window.TerminalID,
		Token:      window.Token,
		Op:         state.HistoryWindowOp(window.Op),
		Cols:       int(window.Size.Cols),
		Rows:       rows,
		Lines:      lines,
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
	}
	return builder.String()
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

func isStorageVersionConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "storage version conflict")
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
