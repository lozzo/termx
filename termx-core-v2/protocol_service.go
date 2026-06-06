package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

const (
	protocolErrorBadRequest = 400
	protocolErrorNotFound   = 404
	protocolErrorInternal   = 500
)

type protocolSession struct {
	server       *Server
	conn         transport.Transport
	sendMu       sync.Mutex
	nextCh       atomic.Uint32
	mu           sync.RWMutex
	attached     map[uint16]string
	cancelEvents context.CancelFunc
}

func newProtocolSession(server *Server, conn transport.Transport) *protocolSession {
	session := &protocolSession{
		server:   server,
		conn:     conn,
		attached: make(map[uint16]string),
	}
	session.nextCh.Store(6)
	return session
}

func (session *protocolSession) run(ctx context.Context) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer session.stopEvents()
	for {
		frame, err := session.conn.Recv()
		if err != nil {
			return err
		}
		channel, typ, payload, err := wire.DecodeFrame(frame)
		if err != nil {
			return err
		}
		if channel == 0 {
			if err := session.handleControlFrame(sessionCtx, typ, payload); err != nil {
				return err
			}
			continue
		}
		if err := session.handleStreamFrame(sessionCtx, channel, typ, payload); err != nil {
			if sendErr := session.sendError(0, protocolErrorBadRequest, err.Error()); sendErr != nil {
				return sendErr
			}
		}
	}
}

func (session *protocolSession) handleControlFrame(ctx context.Context, typ uint8, payload []byte) error {
	switch typ {
	case wire.TypeHello:
		hello, err := protocol.DecodeHelloPayload(payload)
		if err != nil {
			return session.sendError(0, protocolErrorBadRequest, err.Error())
		}
		if hello.Version != 0 && hello.Version != wire.Version {
			return session.sendError(0, protocolErrorBadRequest, fmt.Sprintf("unsupported wire version %d", hello.Version))
		}
		response, err := protocol.EncodeHelloPayload(protocol.Hello{Version: wire.Version, Server: ModuleName})
		if err != nil {
			return err
		}
		return session.sendFrame(0, wire.TypeHello, response)
	case wire.TypeRequest:
		req, err := protocol.DecodeRequestPayload(payload)
		if err != nil {
			return session.sendError(0, protocolErrorBadRequest, err.Error())
		}
		return session.handleRequest(ctx, req)
	default:
		return session.sendError(0, protocolErrorBadRequest, fmt.Sprintf("unsupported control frame type %d", typ))
	}
}

func (session *protocolSession) handleRequest(ctx context.Context, req protocol.Request) error {
	result, binary, code, err := session.dispatchRequest(ctx, req)
	if err != nil {
		return session.sendError(req.ID, code, err.Error())
	}
	if binary {
		payload, err := protocol.EncodeBinaryResponsePayload(req.ID, result)
		if err != nil {
			return err
		}
		return session.sendFrame(0, wire.TypeResponseBinary, payload)
	}
	payload, err := protocol.EncodeResponsePayload(protocol.Response{ID: req.ID, Result: result})
	if err != nil {
		return err
	}
	return session.sendFrame(0, wire.TypeResponse, payload)
}

func (session *protocolSession) dispatchRequest(ctx context.Context, req protocol.Request) ([]byte, bool, int, error) {
	params, err := protocol.DecodeMethodParams(req.Method, req.Params)
	if err != nil {
		return nil, false, protocolErrorBadRequest, err
	}
	switch req.Method {
	case "create":
		in := params.(protocol.CreateParams)
		info, err := session.server.RegisterTerminal(TerminalRecord{
			ID:      in.ID,
			Name:    in.Name,
			Command: append([]string(nil), in.Command...),
			Tags:    cloneStringMap(in.Tags),
			Size:    coreSizeFromProtocol(in.Size),
		})
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.CreateResult{TerminalID: info.ID, State: string(info.State)})
	case "list":
		items := session.server.ListTerminals()
		out := protocol.ListResult{Terminals: make([]protocol.TerminalInfo, 0, len(items))}
		for _, item := range items {
			out.Terminals = append(out.Terminals, protocolInfoFromCoreV2(item))
		}
		return encodeMethodResult(req.Method, out)
	case "get":
		in := params.(protocol.GetParams)
		info, err := session.server.GetTerminal(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocolInfoFromCoreV2(info))
	case "set_tags":
		in := params.(protocol.SetTagsParams)
		info, err := session.server.GetTerminal(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		_, err = session.server.SetMetadata(ctx, in.TerminalID, info.Name, in.Tags)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "set_metadata":
		in := params.(protocol.SetMetadataParams)
		_, err := session.server.SetMetadata(ctx, in.TerminalID, in.Name, in.Tags)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "kill":
		in := params.(protocol.GetParams)
		err := session.server.KillTerminal(ctx, in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "restart":
		in := params.(protocol.GetParams)
		err := session.server.RestartTerminal(ctx, in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "remove":
		in := params.(protocol.GetParams)
		err := session.server.RemoveTerminal(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "resize":
		in := params.(protocol.ResizeParams)
		err := session.server.ResizeTerminal(ctx, in.TerminalID, in.Cols, in.Rows)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "attach":
		in := params.(protocol.AttachParams)
		channel, err := session.attach(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		info, err := session.server.GetTerminal(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.AttachResult{
			Mode:    normalizeAttachMode(in.Mode),
			Channel: channel,
			ResizeControl: &protocol.ResizeControl{
				CanResize: true,
				Reason:    protocol.ResizeControlReasonOwner,
				SurfaceID: in.SurfaceID,
				ResizeOwnership: &protocol.ResizeOwnership{
					OwnerAttachmentID: fmt.Sprintf("core-v2:%d", channel),
					OwnerSurfaceID:    in.SurfaceID,
					OwnerViewID:       in.ViewID,
					Size:              protocolSizeFromCore(info.Size),
					SizeLocked:        false,
					Epoch:             uint64(channel),
				},
			},
		})
	case "detach":
		in := params.(protocol.DetachParams)
		session.detachTerminal(in.TerminalID)
		return encodeMethodResult(req.Method, nil)
	case "ensure_resize":
		in := params.(protocol.EnsureResizeParams)
		attachedTerminalID, err := session.terminalIDForChannel(in.Channel)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		if attachedTerminalID != in.TerminalID {
			return nil, false, protocolErrorBadRequest, fmt.Errorf("resize channel %d is attached to %s, not %s", in.Channel, attachedTerminalID, in.TerminalID)
		}
		err = session.server.ResizeTerminal(ctx, in.TerminalID, in.Cols, in.Rows)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.EnsureResizeResult{
			Size:    protocol.Size{Cols: in.Cols, Rows: in.Rows},
			Resized: true,
			ResizeControl: &protocol.ResizeControl{
				CanResize: true,
				Reason:    protocol.ResizeControlReasonOwner,
				SurfaceID: in.SurfaceID,
			},
		})
	case "snapshot":
		in := params.(protocol.SnapshotParams)
		snapshot, err := session.liveSnapshot(in)
		if err != nil {
			return nil, true, errorCode(err), err
		}
		payload, err := protocol.EncodeSnapshotPayload(snapshot)
		if err != nil {
			return nil, true, protocolErrorInternal, err
		}
		return payload, true, 0, nil
	case "events":
		in := params.(protocol.EventsParams)
		session.startEvents(ctx, in)
		return encodeMethodResult(req.Method, nil)
	case "storage.get":
		in := params.(protocol.StorageGetParams)
		entry, err := session.server.StorageGet(ctx, in.AppID, storageScopeFromProtocol(in.Scope), in.OwnerID, in.Key)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocolStorageEntryFromCore(entry))
	case "storage.put":
		in := params.(protocol.StoragePutParams)
		entry, err := session.server.StoragePut(ctx, StoragePutRequest{
			AppID:           in.AppID,
			Scope:           storageScopeFromProtocol(in.Scope),
			OwnerID:         in.OwnerID,
			Key:             in.Key,
			Value:           append([]byte(nil), in.Value...),
			CheckVersion:    in.CheckVersion,
			ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocolStorageEntryFromCore(entry))
	case "storage.delete":
		in := params.(protocol.StorageDeleteParams)
		result, err := session.server.StorageDelete(ctx, StorageDeleteRequest{
			AppID:           in.AppID,
			Scope:           storageScopeFromProtocol(in.Scope),
			OwnerID:         in.OwnerID,
			Key:             in.Key,
			CheckVersion:    in.CheckVersion,
			ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.StorageDeleteResult{
			AppID:   result.AppID,
			Scope:   protocolStorageScopeFromCore(result.Scope),
			OwnerID: result.OwnerID,
			Key:     result.Key,
			Deleted: result.Deleted,
			Version: result.Version,
		})
	case "storage.list":
		in := params.(protocol.StorageListParams)
		entries := session.server.StorageList(ctx, in.AppID, storageScopeFromProtocol(in.Scope), in.OwnerID, in.Prefix)
		out := protocol.StorageListResult{Entries: make([]protocol.StorageEntry, 0, len(entries))}
		for _, entry := range entries {
			out.Entries = append(out.Entries, protocolStorageEntryFromCore(entry))
		}
		return encodeMethodResult(req.Method, out)
	case "workbench.get":
		in := params.(protocol.WorkbenchGetParams)
		snapshot, err := session.server.WorkbenchSnapshot(ctx, in.WorkspaceID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, snapshot)
	case "workbench.apply":
		in := params.(protocol.WorkbenchMutateParams)
		result, err := session.server.ApplyWorkbenchMutation(ctx, in)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, result)
	case "history.window":
		in := params.(protocol.HistoryWindowParams)
		window, err := session.historyWindow(in)
		if err != nil {
			return nil, true, errorCode(err), err
		}
		payload, err := protocol.EncodeHistoryWindowPayload(window)
		if err != nil {
			return nil, true, protocolErrorInternal, err
		}
		return payload, true, 0, nil
	default:
		return nil, false, protocolErrorNotFound, fmt.Errorf("unknown method: %s", req.Method)
	}
}

func (session *protocolSession) liveSnapshot(params protocol.SnapshotParams) (*protocol.Snapshot, error) {
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	snapshot, err := session.server.LiveSnapshot(params.TerminalID)
	if err != nil {
		return nil, err
	}
	return &protocol.Snapshot{
		TerminalID: params.TerminalID,
		Size:       protocolSizeFromCore(info.Size),
		Screen: protocol.ScreenData{
			Cells:             vtermRowsToProtocolCells(snapshot.Screen.Cells),
			IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
		},
		Cursor:          vtermCursorToProtocol(snapshot.Cursor),
		Modes:           vtermModesToProtocol(snapshot.Modes),
		ScreenOwnership: repeatString(protocol.RowOwnershipScreen, len(snapshot.Screen.Cells)),
		Timestamp:       time.Now().UTC(),
	}, nil
}

func vtermRowsToProtocolCells(rows [][]vterm.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for rowIndex, row := range rows {
		out[rowIndex] = vtermCellsToProtocol(row)
	}
	return out
}

func vtermCellsToProtocol(cells []vterm.Cell) []protocol.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(cells))
	for i, cell := range cells {
		out[i] = protocol.Cell{
			Content:    cell.Content,
			Width:      cell.Width,
			Style:      vtermStyleToProtocol(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		}
	}
	return out
}

func vtermStyleToProtocol(style vterm.CellStyle) protocol.CellStyle {
	return protocol.CellStyle{
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

func vtermCursorToProtocol(cursor vterm.CursorState) protocol.CursorState {
	return protocol.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   string(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func vtermModesToProtocol(modes vterm.TerminalModes) protocol.TerminalModes {
	return protocol.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		AlternateScroll:   modes.AlternateScroll,
		MouseTracking:     modes.MouseTracking,
		MouseX10:          modes.MouseX10,
		MouseNormal:       modes.MouseNormal,
		MouseButtonEvent:  modes.MouseButtonEvent,
		MouseAnyEvent:     modes.MouseAnyEvent,
		MouseSGR:          modes.MouseSGR,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func repeatString(value string, count int) []string {
	if count <= 0 {
		return nil
	}
	out := make([]string, count)
	for i := range out {
		out[i] = value
	}
	return out
}

func encodeMethodResult(method string, result any) ([]byte, bool, int, error) {
	payload, err := protocol.EncodeMethodResult(method, result)
	if err != nil {
		return nil, false, protocolErrorInternal, err
	}
	return payload, false, 0, nil
}

func (session *protocolSession) handleStreamFrame(ctx context.Context, channel uint16, typ uint8, payload []byte) error {
	terminalID, err := session.terminalIDForChannel(channel)
	if err != nil {
		return err
	}
	switch typ {
	case wire.TypeInput:
		return session.server.WriteInput(ctx, terminalID, payload)
	case wire.TypeResize:
		cols, rows, err := wire.DecodeResizePayload(payload)
		if err != nil {
			return err
		}
		return session.server.ResizeTerminal(ctx, terminalID, cols, rows)
	case wire.TypeStreamReady, wire.TypeBootstrapDone:
		return nil
	default:
		return fmt.Errorf("unsupported stream frame type %d", typ)
	}
}

func (session *protocolSession) historyWindow(params protocol.HistoryWindowParams) (*protocol.HistoryWindow, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 24
	}
	cols := params.Cols
	if cols <= 0 {
		info, err := session.server.GetTerminal(params.TerminalID)
		if err != nil {
			return nil, err
		}
		cols = int(info.Size.Cols)
	}
	cursor := history.HistoryCursor{
		Valid:           params.CursorValid,
		BeforeLineID:    history.LogicalLineID(params.BeforeLineID),
		BeforeRowInLine: params.BeforeRowInLine,
	}
	var (
		window history.HistoryWindow
		err    error
	)
	if params.CursorValid {
		if err := session.validateOlderWindowRequest(params, cols); err != nil {
			return nil, err
		}
		window, err = session.server.OlderWindow(params.TerminalID, cols, limit, cursor)
		if params.Token != "" {
			window.Token = history.WindowToken(params.Token)
		}
	} else {
		window, err = session.server.LatestWindow(params.TerminalID, cols, limit)
	}
	if err != nil {
		return nil, err
	}
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	size := Size{Cols: uint16(cols), Rows: info.Size.Rows}
	return protocolHistoryWindowFromCore(params.TerminalID, size, window), nil
}

func (session *protocolSession) validateOlderWindowRequest(params protocol.HistoryWindowParams, cols int) error {
	latest, err := session.server.LatestWindow(params.TerminalID, cols, 1)
	if err != nil {
		return err
	}
	if params.Token != "" && !historyTokenMatchesRequest(params.Token, cols, params.Generation) {
		return ErrStaleHistoryWindow
	}
	if params.Generation != 0 && params.Generation != uint64(latest.Generation) {
		return ErrStaleHistoryWindow
	}
	if params.BoundaryLastLineID != 0 && params.BoundaryLastLineID != uint64(latest.LastLineID) {
		return ErrStaleHistoryWindow
	}
	return nil
}

func (session *protocolSession) startEvents(ctx context.Context, params protocol.EventsParams) {
	session.stopEvents()
	eventCtx, cancel := context.WithCancel(ctx)
	session.mu.Lock()
	session.cancelEvents = cancel
	session.mu.Unlock()
	events := session.server.Events(eventCtx, eventFilterFromProtocol(params))
	go func() {
		for {
			select {
			case <-eventCtx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				payload, err := protocol.EncodeEventPayload(protocolEventFromCoreV2(event))
				if err != nil {
					continue
				}
				_ = session.sendFrame(0, wire.TypeEvent, payload)
			}
		}
	}()
}

func (session *protocolSession) stopEvents() {
	session.mu.Lock()
	cancel := session.cancelEvents
	session.cancelEvents = nil
	session.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (session *protocolSession) attach(terminalID string) (uint16, error) {
	if _, err := session.server.GetTerminal(terminalID); err != nil {
		return 0, err
	}
	channel := uint16(session.nextCh.Add(1))
	session.mu.Lock()
	session.attached[channel] = terminalID
	session.mu.Unlock()
	return channel, nil
}

func (session *protocolSession) detachTerminal(terminalID string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	for channel, attachedTerminalID := range session.attached {
		if attachedTerminalID == terminalID {
			delete(session.attached, channel)
		}
	}
}

func (session *protocolSession) terminalIDForChannel(channel uint16) (string, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()
	terminalID, ok := session.attached[channel]
	if !ok {
		return "", ErrTerminalNotFound
	}
	return terminalID, nil
}

func (session *protocolSession) sendError(id uint64, code int, message string) error {
	payload, err := protocol.EncodeErrorPayload(protocol.ErrorMessage{
		ID: id,
		Error: protocol.ProtocolError{
			Code:    code,
			Message: message,
		},
	})
	if err != nil {
		return err
	}
	return session.sendFrame(0, wire.TypeError, payload)
}

func (session *protocolSession) sendFrame(channel uint16, typ uint8, payload []byte) error {
	frame, err := wire.EncodeFrame(channel, typ, payload)
	if err != nil {
		return err
	}
	session.sendMu.Lock()
	defer session.sendMu.Unlock()
	return session.conn.Send(frame)
}

func protocolHistoryWindowFromCore(terminalID string, size Size, window history.HistoryWindow) *protocol.HistoryWindow {
	rows := make([]protocol.CompactRow, len(window.Rows))
	rowOwnership := make([]string, len(window.Rows))
	rowLineIDs := make([]uint64, len(window.Rows))
	rowInLine := make([]int, len(window.Rows))
	for i, row := range window.Rows {
		rows[i] = protocol.CompactRow{Text: row.Text}
		rowLineIDs[i] = uint64(row.LineID)
		rowInLine[i] = row.RowInLine
		if row.Committed {
			rowOwnership[i] = protocol.RowOwnershipPersisted
		} else if row.LineID != 0 {
			rowOwnership[i] = protocol.RowOwnershipLiveTailLive
		}
	}
	lines := make([]protocol.HistoryLineSpan, len(window.Spans))
	for i, span := range window.Spans {
		lines[i] = protocol.HistoryLineSpan{
			StartRow:      span.FirstRow,
			EndRow:        span.LastRow,
			LogicalLineID: uint64(span.LineID),
			ClippedBefore: span.ClippedBefore,
			ClippedAfter:  span.ClippedAfter,
		}
	}
	return &protocol.HistoryWindow{
		TerminalID:   terminalID,
		Token:        string(window.Token),
		Op:           protocol.HistoryWindowOp(window.Op),
		Size:         protocolSizeFromCore(size),
		Rows:         rows,
		RowOwnership: rowOwnership,
		Lines:        lines,
		LoadedRows:   len(rows),
		TotalRows:    window.TotalRows,
		LoadedLines:  window.LoadedLines,
		LogicalTotal: window.TotalLines,
		HasMore:      window.HasMore,
		Generation:   uint64(window.Generation),
		FirstLineID:  uint64(window.FirstLineID),
		LastLineID:   uint64(window.LastLineID),
		CursorValid:  window.Cursor.Valid,
		CursorLineID: uint64(window.Cursor.BeforeLineID),
		CursorRow:    window.Cursor.BeforeRowInLine,
		RowLineIDs:   rowLineIDs,
		RowInLine:    rowInLine,
		Timestamp:    time.Now().UTC(),
	}
}

func protocolInfoFromCoreV2(info TerminalInfo) protocol.TerminalInfo {
	return protocol.TerminalInfo{
		ID:        info.ID,
		Name:      info.Name,
		Command:   append([]string(nil), info.Command...),
		Tags:      cloneStringMap(info.Tags),
		Size:      protocolSizeFromCore(info.Size),
		State:     string(info.State),
		CreatedAt: info.CreatedAt,
		ExitCode:  copyIntPtr(info.ExitCode),
	}
}

func protocolEventFromCoreV2(event Event) protocol.Event {
	out := protocol.Event{
		TerminalID: event.TerminalID,
		Timestamp:  event.Timestamp,
	}
	switch event.Type {
	case EventTerminalCreated:
		out.Type = protocol.EventTerminalCreated
		if event.Terminal != nil {
			out.Created = &protocol.TerminalCreatedData{
				Name:    event.Terminal.Name,
				Command: append([]string(nil), event.Terminal.Command...),
				Size:    protocolSizeFromCore(event.Terminal.Size),
			}
		}
	case EventTerminalExited:
		out.Type = protocol.EventTerminalStateChanged
		out.StateChanged = &protocol.TerminalStateChangedData{
			NewState: string(TerminalStateExited),
		}
		if event.Terminal != nil {
			out.StateChanged.ExitCode = copyIntPtr(event.Terminal.ExitCode)
		}
	case EventTerminalResized:
		out.Type = protocol.EventTerminalResized
		out.Resized = &protocol.TerminalResizedData{
			OldSize: protocolSizeFromCore(event.OldSize),
			NewSize: protocolSizeFromCore(event.NewSize),
		}
	case EventTerminalMetadataChanged:
		out.Type = protocol.EventTerminalMetadataChanged
	case EventTerminalChanged:
		out.Type = protocol.EventTerminalStateChanged
		if event.Terminal != nil && event.Terminal.State == TerminalStateExited {
			out.StateChanged = &protocol.TerminalStateChangedData{
				NewState: string(event.Terminal.State),
				ExitCode: copyIntPtr(event.Terminal.ExitCode),
			}
		}
	case EventTerminalRemoved:
		out.Type = protocol.EventTerminalRemoved
		out.Removed = &protocol.TerminalRemovedData{Reason: "removed"}
	case EventStorageChanged:
		out.Type = protocol.EventStorageChanged
		if event.Storage != nil {
			out.Storage = &protocol.StorageChangedData{
				AppID:   event.Storage.AppID,
				Scope:   protocolStorageScopeFromCore(event.Storage.Scope),
				OwnerID: event.Storage.OwnerID,
				Key:     event.Storage.Key,
				Version: event.Storage.Version,
				Op:      event.Storage.Op,
			}
		}
	case EventWorkbenchChanged:
		out.Type = protocol.EventWorkbenchChanged
		if event.Workbench != nil {
			out.Workbench = &protocol.WorkbenchChangedData{
				WorkspaceID: event.Workbench.WorkspaceID,
				Version:     event.Workbench.Version,
				Action:      event.Workbench.Action,
				ResourceID:  event.Workbench.ResourceID,
			}
		}
	default:
		out.Type = protocol.EventTerminalStateChanged
	}
	return out
}

func eventFilterFromProtocol(params protocol.EventsParams) EventFilter {
	out := EventFilter{TerminalID: params.TerminalID}
	for _, typ := range params.Types {
		switch typ {
		case protocol.EventTerminalCreated:
			out.Types = append(out.Types, EventTerminalCreated)
		case protocol.EventTerminalRemoved:
			out.Types = append(out.Types, EventTerminalRemoved)
		case protocol.EventTerminalStateChanged:
			out.Types = append(out.Types, EventTerminalChanged, EventTerminalExited)
		case protocol.EventTerminalMetadataChanged, protocol.EventTerminalResized:
			if typ == protocol.EventTerminalMetadataChanged {
				out.Types = append(out.Types, EventTerminalMetadataChanged)
			} else {
				out.Types = append(out.Types, EventTerminalResized)
			}
		case protocol.EventStorageChanged:
			out.Types = append(out.Types, EventStorageChanged)
		case protocol.EventWorkbenchChanged:
			out.Types = append(out.Types, EventWorkbenchChanged)
		}
	}
	out.StorageAppID = params.StorageAppID
	out.StorageScope = storageScopeFromProtocol(params.StorageScope)
	out.StorageOwnerID = params.StorageOwnerID
	out.StorageKeyPrefix = params.StorageKeyPrefix
	out.WorkbenchID = params.WorkbenchID
	return out
}

func protocolStorageEntryFromCore(entry StorageEntry) protocol.StorageEntry {
	return protocol.StorageEntry{
		AppID:     entry.AppID,
		Scope:     protocolStorageScopeFromCore(entry.Scope),
		OwnerID:   entry.OwnerID,
		Key:       entry.Key,
		Value:     append([]byte(nil), entry.Value...),
		Version:   entry.Version,
		UpdatedAt: entry.UpdatedAt,
	}
}

func protocolStorageScopeFromCore(scope StorageScope) protocol.StorageScope {
	switch scope {
	case StorageScopePrivate:
		return protocol.StorageScopePrivate
	default:
		return protocol.StorageScopePublic
	}
}

func storageScopeFromProtocol(scope protocol.StorageScope) StorageScope {
	switch scope {
	case protocol.StorageScopePrivate:
		return StorageScopePrivate
	default:
		return StorageScopePublic
	}
}

func protocolSizeFromCore(size Size) protocol.Size {
	return protocol.Size{Cols: size.Cols, Rows: size.Rows}
}

func coreSizeFromProtocol(size protocol.Size) Size {
	return Size{Cols: size.Cols, Rows: size.Rows}
}

func normalizeAttachMode(mode string) string {
	if mode == "" {
		return "collaborator"
	}
	return mode
}

func historyTokenMatchesRequest(token string, cols int, generation uint64) bool {
	if token == "" {
		return true
	}
	parts := strings.Split(token, ":")
	seenGeneration := generation == 0
	seenCols := false
	for _, part := range parts {
		if strings.HasPrefix(part, "g") {
			value, err := strconv.ParseUint(strings.TrimPrefix(part, "g"), 10, 64)
			if err != nil {
				return false
			}
			seenGeneration = generation == 0 || value == generation
		}
		if tokenPartHasNumericPrefix(part, "c") {
			value, err := strconv.Atoi(strings.TrimPrefix(part, "c"))
			if err != nil {
				return false
			}
			seenCols = value == cols
		}
	}
	return seenGeneration && seenCols
}

func tokenPartHasNumericPrefix(part string, prefix string) bool {
	return strings.HasPrefix(part, prefix) && len(part) > len(prefix) && part[len(prefix)] >= '0' && part[len(prefix)] <= '9'
}

func errorCode(err error) int {
	switch {
	case errors.Is(err, ErrTerminalNotFound):
		return protocolErrorNotFound
	case errors.Is(err, ErrInvalidTerminalID), errors.Is(err, ErrInvalidCommand), errors.Is(err, ErrInvalidServerSize), errors.Is(err, ErrTerminalExited), errors.Is(err, ErrStaleHistoryWindow), errors.Is(err, ErrInvalidStorageKey), errors.Is(err, ErrStorageVersionConflict), errors.Is(err, ErrInvalidWorkbenchMutation), errors.Is(err, ErrDuplicateWorkbenchResource), errors.Is(err, ErrWorkbenchVersionConflict):
		return protocolErrorBadRequest
	case errors.Is(err, ErrStorageEntryNotFound), errors.Is(err, ErrWorkbenchNotFound):
		return protocolErrorNotFound
	default:
		return protocolErrorInternal
	}
}

func copyIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
