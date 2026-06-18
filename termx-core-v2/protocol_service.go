package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	"github.com/rivo/uniseg"
)

const (
	protocolErrorBadRequest = 400
	protocolErrorNotFound   = 404
	protocolErrorInternal   = 500
)

var errProtocolAttachmentMismatch = errors.New("protocol attachment mismatch")

type protocolSession struct {
	server        *Server
	conn          transport.Transport
	sendMu        sync.Mutex
	nextCh        atomic.Uint32
	nextSnapshot  atomic.Uint64
	mu            sync.RWMutex
	attachments   map[uint16]protocolAttachment
	resizeOwners  map[string]uint16
	sizeLocks     map[string]bool
	ownerEpoch    uint64
	eventCancels  map[uint64]context.CancelFunc
	nextEventSub  uint64
	historyMu     sync.Mutex
	historyPins   map[string]frozenHistorySnapshot
	historyLatest map[string]string
}

type frozenHistorySnapshot struct {
	TerminalID string
	Snapshot   history.FrozenSnapshot
}

// protocolAttachment 是 daemon-side channel/view registry；它不保存 TUI workspace/pane truth。
type protocolAttachment struct {
	TerminalID   string
	Channel      uint16
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
	Epoch        uint64
}

func newProtocolSession(server *Server, conn transport.Transport) *protocolSession {
	session := &protocolSession{
		server:        server,
		conn:          conn,
		attachments:   make(map[uint16]protocolAttachment),
		resizeOwners:  make(map[string]uint16),
		sizeLocks:     make(map[string]bool),
		eventCancels:  make(map[uint64]context.CancelFunc),
		historyPins:   make(map[string]frozenHistorySnapshot),
		historyLatest: make(map[string]string),
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
		coreLifecycleTrace(session.server.cfg.logger, "protocol.list",
			"count", len(items),
			"items", coreTerminalListSummary(items),
		)
		out := protocol.ListResult{Terminals: make([]protocol.TerminalInfo, 0, len(items))}
		for _, item := range items {
			out.Terminals = append(out.Terminals, session.protocolInfoFromCoreV2(item))
		}
		return encodeMethodResult(req.Method, out)
	case "get":
		in := params.(protocol.GetParams)
		info, err := session.server.GetTerminal(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, session.protocolInfoFromCoreV2(info))
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
		if info, err := session.server.GetTerminal(in.TerminalID); err == nil {
			coreLifecycleTrace(session.server.cfg.logger, "protocol.restart.request", coreTerminalInfoAttrs(info)...)
		} else {
			coreLifecycleTrace(session.server.cfg.logger, "protocol.restart.request",
				"terminal_id", in.TerminalID,
				"error", err.Error(),
			)
		}
		err := session.server.RestartTerminal(ctx, in.TerminalID)
		if err != nil {
			coreLifecycleTrace(session.server.cfg.logger, "protocol.restart.result",
				"terminal_id", in.TerminalID,
				"error", err.Error(),
			)
			return nil, false, errorCode(err), err
		}
		if info, infoErr := session.server.GetTerminal(in.TerminalID); infoErr == nil {
			coreLifecycleTrace(session.server.cfg.logger, "protocol.restart.result", coreTerminalInfoAttrs(info)...)
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
	case "input":
		in := params.(protocol.InputParams)
		if err := session.input(ctx, in); err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "attach":
		in := params.(protocol.AttachParams)
		attachment, control, err := session.attach(in)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.AttachResult{
			Mode:          attachment.Mode,
			Channel:       attachment.Channel,
			ResizeControl: control,
		})
	case "detach":
		in := params.(protocol.DetachParams)
		session.detach(in)
		return encodeMethodResult(req.Method, nil)
	case "ensure_resize":
		in := params.(protocol.EnsureResizeParams)
		attachment, err := session.attachmentForChannel(in.Channel)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		if attachment.TerminalID != in.TerminalID {
			return nil, false, protocolErrorBadRequest, fmt.Errorf("resize channel %d is attached to %s, not %s", in.Channel, attachment.TerminalID, in.TerminalID)
		}
		control, canResize, err := session.resizeControlForRequest(attachment, in.ResizePolicy, in.SurfaceID, in.ViewID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		if !canResize {
			return encodeMethodResult(req.Method, protocol.EnsureResizeResult{
				Size:          control.ResizeOwnership.Size,
				Resized:       false,
				ResizeControl: control,
			})
		}
		if control.ResizeOwnership != nil && control.ResizeOwnership.Size == (protocol.Size{Cols: in.Cols, Rows: in.Rows}) {
			// 中文说明：owner 转移后仍要走 ensure_resize 同步 ownership；
			// 但 PTY 尺寸没变时不能实际 resize，避免制造多余 resize 事件和历史 invalidation。
			return encodeMethodResult(req.Method, protocol.EnsureResizeResult{
				Size:          control.ResizeOwnership.Size,
				Resized:       false,
				ResizeControl: control,
			})
		}
		err = session.server.ResizeTerminal(ctx, in.TerminalID, in.Cols, in.Rows)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		control = session.resizeControlForOwner(attachment, protocol.Size{Cols: in.Cols, Rows: in.Rows})
		return encodeMethodResult(req.Method, protocol.EnsureResizeResult{
			Size:          protocol.Size{Cols: in.Cols, Rows: in.Rows},
			Resized:       true,
			ResizeControl: control,
		})
	case "resize.lock":
		in := params.(protocol.ResizeControlParams)
		control, err := session.setResizeLock(in, true)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.ResizeControlResult{Size: control.ResizeOwnership.Size, ResizeControl: control})
	case "resize.unlock":
		in := params.(protocol.ResizeControlParams)
		control, err := session.setResizeLock(in, false)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.ResizeControlResult{Size: control.ResizeOwnership.Size, ResizeControl: control})
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
		window, err := session.historyWindow(ctx, in)
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
	attrs := coreTerminalInfoAttrs(info)
	attrs = append(attrs,
		"screen_rows", len(snapshot.Screen.Cells),
		"cursor_row", snapshot.Cursor.Row,
		"cursor_col", snapshot.Cursor.Col,
		"cursor_visible", snapshot.Cursor.Visible,
	)
	coreLifecycleTrace(session.server.cfg.logger, "protocol.snapshot", attrs...)
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
	attachment, err := session.attachmentForChannel(channel)
	if err != nil {
		return err
	}
	switch typ {
	case wire.TypeInput:
		return session.server.WriteInput(ctx, attachment.TerminalID, payload)
	case wire.TypeResize:
		cols, rows, err := wire.DecodeResizePayload(payload)
		if err != nil {
			return err
		}
		control, canResize, err := session.resizeControlForRequest(attachment, attachment.ResizePolicy, attachment.SurfaceID, attachment.ViewID)
		if err != nil {
			return err
		}
		if !canResize {
			return fmt.Errorf("resize denied for channel %d: %s", channel, control.Reason)
		}
		if err := session.server.ResizeTerminal(ctx, attachment.TerminalID, cols, rows); err != nil {
			return err
		}
		session.resizeControlForOwner(attachment, protocol.Size{Cols: cols, Rows: rows})
		return nil
	case wire.TypeStreamReady, wire.TypeBootstrapDone:
		return nil
	default:
		return fmt.Errorf("unsupported stream frame type %d", typ)
	}
}

func (session *protocolSession) historyWindow(ctx context.Context, params protocol.HistoryWindowParams) (*protocol.HistoryWindow, error) {
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
	var window history.HistoryWindow
	if params.CursorValid {
		if err := session.validateOlderWindowRequest(params, cols); err != nil {
			return nil, err
		}
		snapshot, ok := session.frozenSnapshot(params.TerminalID, params.Token)
		if !ok {
			return nil, ErrStaleHistoryWindow
		}
		window = frozenSnapshotOlderWindow(snapshot, cols, limit, protocolCursorToCore(params))
		window.Token = history.WindowToken(params.Token)
		if params.BoundaryLastLineID != 0 {
			// older response 的 payload 只包含本页更老 rows，但它要接回客户端
			// 已加载 frozen 视图；LastLineID 必须保持当前 tail boundary，不能变成本页尾。
			window.LastLineID = history.LogicalLineID(params.BoundaryLastLineID)
		}
	} else if params.Token != "" {
		if err := session.validateFrozenWindowRequest(params, cols); err != nil {
			return nil, err
		}
		snapshot, ok := session.frozenSnapshot(params.TerminalID, params.Token)
		if !ok {
			return nil, ErrStaleHistoryWindow
		}
		// 带 token 但不带 cursor 表示在当前 frozen snapshot 内跳到最老页；
		// 这是跳转语义，返回 replace，避免客户端为了到顶一次性加载中间所有页。
		window = frozenSnapshotOldestWindow(snapshot, cols, limit)
		window.Token = history.WindowToken(params.Token)
	} else {
		terminal, err := session.server.Terminal(params.TerminalID)
		if err != nil {
			return nil, err
		}
		if err := terminal.FlushHistory(ctx); err != nil {
			return nil, err
		}
		snapshot := terminal.FreezeSnapshot()
		snapshot.Token = session.sessionFrozenSnapshotToken(params.TerminalID, snapshot.Token)
		session.storeFrozenSnapshot(params.TerminalID, snapshot)
		window = frozenSnapshotLatestWindow(snapshot, cols, limit)
	}
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	size := Size{Cols: uint16(cols), Rows: info.Size.Rows}
	return protocolHistoryWindowFromCore(params.TerminalID, size, window), nil
}

func (session *protocolSession) validateOlderWindowRequest(params protocol.HistoryWindowParams, cols int) error {
	if err := session.validateFrozenWindowRequest(params, cols); err != nil {
		return err
	}
	snapshot, _ := session.frozenSnapshot(params.TerminalID, params.Token)
	if params.CursorValid {
		cursor := protocolCursorToCore(params)
		if !frozenSnapshotCursorValid(snapshot, cols, cursor) {
			return ErrStaleHistoryWindow
		}
	}
	return nil
}

func (session *protocolSession) validateFrozenWindowRequest(params protocol.HistoryWindowParams, cols int) error {
	snapshot, ok := session.frozenSnapshot(params.TerminalID, params.Token)
	if !ok {
		return ErrStaleHistoryWindow
	}
	if params.Token != "" && params.Token != snapshot.Token {
		return ErrStaleHistoryWindow
	}
	if params.Generation != 0 && params.Generation != uint64(snapshot.Generation) {
		return ErrStaleHistoryWindow
	}
	// older 请求带回的是客户端当前 frozen latest/prepend 视图的 logical
	// boundary，而不是“只看最后一行”重算出来的尾部 line id。这里要接受
	// snapshot 当前投影尾部任意合法 suffix window 的 boundary，避免多行 latest
	// 或 prepend 过的 frozen 视图被误判成 stale。
	if !frozenSnapshotBoundaryValid(snapshot, cols, params.BoundaryFirstLineID, params.BoundaryLastLineID) {
		return ErrStaleHistoryWindow
	}
	return nil
}

func (session *protocolSession) startEvents(ctx context.Context, params protocol.EventsParams) {
	eventCtx, cancel := context.WithCancel(ctx)
	session.mu.Lock()
	session.nextEventSub++
	subID := session.nextEventSub
	session.eventCancels[subID] = cancel
	session.mu.Unlock()
	events := session.server.Events(eventCtx, eventFilterFromProtocol(params))
	go func() {
		defer session.clearEventSubscription(subID)
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
	cancels := make([]context.CancelFunc, 0, len(session.eventCancels))
	for id, cancel := range session.eventCancels {
		cancels = append(cancels, cancel)
		delete(session.eventCancels, id)
	}
	session.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (session *protocolSession) clearEventSubscription(id uint64) {
	session.mu.Lock()
	delete(session.eventCancels, id)
	session.mu.Unlock()
}

func (session *protocolSession) attach(params protocol.AttachParams) (protocolAttachment, *protocol.ResizeControl, error) {
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		coreLifecycleTrace(session.server.cfg.logger, "protocol.attach.request",
			"terminal_id", params.TerminalID,
			"view_id", params.ViewID,
			"surface_id", params.SurfaceID,
			"resize_policy", params.ResizePolicy,
			"error", err.Error(),
		)
		return protocolAttachment{}, nil, err
	}
	attrs := coreTerminalInfoAttrs(info)
	attrs = append(attrs,
		"view_id", params.ViewID,
		"surface_id", params.SurfaceID,
		"resize_policy", params.ResizePolicy,
		"mode", params.Mode,
	)
	coreLifecycleTrace(session.server.cfg.logger, "protocol.attach.request", attrs...)
	channel := uint16(session.nextCh.Add(1))
	attachment := protocolAttachment{
		TerminalID:   params.TerminalID,
		Channel:      channel,
		Mode:         normalizeAttachMode(params.Mode),
		ResizePolicy: normalizeResizePolicy(params.ResizePolicy),
		SurfaceID:    params.SurfaceID,
		ViewID:       params.ViewID,
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if attachment.ResizePolicy == protocol.ResizePolicyOwner || session.resizeOwners[attachment.TerminalID] == 0 {
		session.ownerEpoch++
		attachment.ResizePolicy = protocol.ResizePolicyOwner
		attachment.Epoch = session.ownerEpoch
		session.resizeOwners[attachment.TerminalID] = channel
	}
	session.attachments[channel] = attachment
	control := session.resizeControlForAttachmentLocked(attachment, protocolSizeFromCore(info.Size))
	coreLifecycleTrace(session.server.cfg.logger, "protocol.attach.result",
		"terminal_id", attachment.TerminalID,
		"channel", attachment.Channel,
		"view_id", attachment.ViewID,
		"surface_id", attachment.SurfaceID,
		"resize_policy", attachment.ResizePolicy,
		"can_resize", control.CanResize,
		"control_reason", control.Reason,
		"owner_view_id", control.OwnerViewID,
		"owner_surface_id", control.OwnerSurfaceID,
		"state", string(info.State),
	)
	return attachment, control, nil
}

func (session *protocolSession) detach(params protocol.DetachParams) {
	session.mu.Lock()
	defer session.mu.Unlock()
	for channel, attachment := range session.attachments {
		if !detachMatches(params, channel, attachment) {
			continue
		}
		delete(session.attachments, channel)
		if session.resizeOwners[attachment.TerminalID] == channel {
			delete(session.resizeOwners, attachment.TerminalID)
			session.promoteResizeOwnerLocked(attachment.TerminalID)
		}
	}
}

func detachMatches(params protocol.DetachParams, channel uint16, attachment protocolAttachment) bool {
	if params.Channel != 0 {
		return params.Channel == channel
	}
	if params.TerminalID != "" && params.TerminalID != attachment.TerminalID {
		return false
	}
	if params.SurfaceID != "" && params.SurfaceID != attachment.SurfaceID {
		return false
	}
	if params.ViewID != "" && params.ViewID != attachment.ViewID {
		return false
	}
	return params.TerminalID != "" || params.SurfaceID != "" || params.ViewID != ""
}

func (session *protocolSession) input(ctx context.Context, params protocol.InputParams) error {
	attachment, err := session.attachmentForChannel(params.Channel)
	if err != nil {
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"surface_id", params.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	if attachment.TerminalID != params.TerminalID {
		err := fmt.Errorf("%w: input channel %d is attached to %s, not %s", errProtocolAttachmentMismatch, params.Channel, attachment.TerminalID, params.TerminalID)
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"attached_terminal", attachment.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"attached_view_id", attachment.ViewID,
			"surface_id", params.SurfaceID,
			"attached_surface_id", attachment.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	if params.SurfaceID != "" && attachment.SurfaceID != params.SurfaceID {
		err := fmt.Errorf("%w: input channel %d surface mismatch: %s != %s", errProtocolAttachmentMismatch, params.Channel, attachment.SurfaceID, params.SurfaceID)
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"attached_view_id", attachment.ViewID,
			"surface_id", params.SurfaceID,
			"attached_surface_id", attachment.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	if params.ViewID != "" && attachment.ViewID != params.ViewID {
		err := fmt.Errorf("%w: input channel %d view mismatch: %s != %s", errProtocolAttachmentMismatch, params.Channel, attachment.ViewID, params.ViewID)
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"attached_view_id", attachment.ViewID,
			"surface_id", params.SurfaceID,
			"attached_surface_id", attachment.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	err = session.server.WriteInput(ctx, attachment.TerminalID, params.Data)
	if err != nil {
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"surface_id", params.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
		"terminal_id", params.TerminalID,
		"channel", params.Channel,
		"view_id", params.ViewID,
		"surface_id", params.SurfaceID,
		"bytes", len(params.Data),
		"result", "ok",
	)
	return nil
}

func (session *protocolSession) promoteResizeOwnerLocked(terminalID string) {
	for channel, attachment := range session.attachments {
		if attachment.TerminalID != terminalID || attachment.ResizePolicy == protocol.ResizePolicyObserver {
			continue
		}
		session.ownerEpoch++
		attachment.ResizePolicy = protocol.ResizePolicyOwner
		attachment.Epoch = session.ownerEpoch
		session.attachments[channel] = attachment
		session.resizeOwners[terminalID] = channel
		return
	}
}

func (session *protocolSession) attachmentForChannel(channel uint16) (protocolAttachment, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()
	attachment, ok := session.attachments[channel]
	if !ok {
		return protocolAttachment{}, ErrTerminalNotFound
	}
	return attachment, nil
}

func (session *protocolSession) resizeControlForRequest(attachment protocolAttachment, policy string, surfaceID string, viewID string) (*protocol.ResizeControl, bool, error) {
	info, err := session.server.GetTerminal(attachment.TerminalID)
	if err != nil {
		return nil, false, err
	}
	if policy != "" {
		attachment.ResizePolicy = normalizeResizePolicy(policy)
	}
	if surfaceID != "" {
		attachment.SurfaceID = surfaceID
	}
	if viewID != "" {
		attachment.ViewID = viewID
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if current, ok := session.attachments[attachment.Channel]; ok {
		current.ResizePolicy = attachment.ResizePolicy
		current.SurfaceID = attachment.SurfaceID
		current.ViewID = attachment.ViewID
		attachment = current
		session.attachments[attachment.Channel] = current
	}
	if attachment.ResizePolicy == protocol.ResizePolicyOwner && session.resizeOwners[attachment.TerminalID] != attachment.Channel {
		session.ownerEpoch++
		attachment.Epoch = session.ownerEpoch
		session.attachments[attachment.Channel] = attachment
		session.resizeOwners[attachment.TerminalID] = attachment.Channel
	}
	control := session.resizeControlForAttachmentLocked(attachment, protocolSizeFromCore(info.Size))
	return control, control.CanResize, nil
}

func (session *protocolSession) setResizeLock(params protocol.ResizeControlParams, locked bool) (*protocol.ResizeControl, error) {
	attachment, err := session.attachmentForChannel(params.Channel)
	if err != nil {
		return nil, err
	}
	if attachment.TerminalID != params.TerminalID {
		return nil, fmt.Errorf("resize control channel %d is attached to %s, not %s", params.Channel, attachment.TerminalID, params.TerminalID)
	}
	control, _, err := session.resizeControlForRequest(attachment, params.ResizePolicy, params.SurfaceID, params.ViewID)
	if err != nil {
		return nil, err
	}
	ownerAttachmentID := ""
	if control.ResizeOwnership != nil {
		ownerAttachmentID = control.ResizeOwnership.OwnerAttachmentID
	}
	if ownerAttachmentID != strconv.FormatUint(uint64(attachment.Channel), 10) || attachment.ResizePolicy != protocol.ResizePolicyOwner {
		return control, nil
	}
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.sizeLocks[params.TerminalID] != locked {
		session.ownerEpoch++
		session.sizeLocks[params.TerminalID] = locked
		ownerChannel := session.resizeOwners[params.TerminalID]
		if owner, ok := session.attachments[ownerChannel]; ok {
			owner.Epoch = session.ownerEpoch
			session.attachments[ownerChannel] = owner
		}
	}
	if current, ok := session.attachments[attachment.Channel]; ok {
		attachment = current
	}
	return session.resizeControlForAttachmentLocked(attachment, protocolSizeFromCore(info.Size)), nil
}

func (session *protocolSession) resizeControlForOwner(attachment protocolAttachment, size protocol.Size) *protocol.ResizeControl {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.ownerEpoch++
	attachment.ResizePolicy = protocol.ResizePolicyOwner
	attachment.Epoch = session.ownerEpoch
	session.attachments[attachment.Channel] = attachment
	session.resizeOwners[attachment.TerminalID] = attachment.Channel
	return session.resizeControlForAttachmentLocked(attachment, size)
}

func (session *protocolSession) resizeControlForAttachmentLocked(attachment protocolAttachment, size protocol.Size) *protocol.ResizeControl {
	ownerChannel := session.resizeOwners[attachment.TerminalID]
	owner, hasOwner := session.attachments[ownerChannel]
	if !hasOwner || owner.TerminalID != attachment.TerminalID {
		owner = attachment
		ownerChannel = attachment.Channel
	}
	ownership := &protocol.ResizeOwnership{
		OwnerAttachmentID: strconv.FormatUint(uint64(ownerChannel), 10),
		OwnerSurfaceID:    owner.SurfaceID,
		OwnerViewID:       owner.ViewID,
		Size:              size,
		SizeLocked:        session.sizeLocks[attachment.TerminalID],
		Epoch:             owner.Epoch,
	}
	control := &protocol.ResizeControl{
		CanResize:       ownerChannel == attachment.Channel && attachment.ResizePolicy == protocol.ResizePolicyOwner && !ownership.SizeLocked,
		Reason:          protocol.ResizeControlReasonFollower,
		SizeLocked:      ownership.SizeLocked,
		SurfaceID:       attachment.SurfaceID,
		OwnerSurfaceID:  owner.SurfaceID,
		OwnerViewID:     owner.ViewID,
		ResizeOwnership: ownership,
	}
	if ownership.SizeLocked && ownerChannel == attachment.Channel && attachment.ResizePolicy == protocol.ResizePolicyOwner {
		control.Reason = protocol.ResizeControlReasonSizeLocked
	} else if attachment.ResizePolicy == protocol.ResizePolicyObserver {
		control.Reason = protocol.ResizeControlReasonObserver
	} else if control.CanResize {
		control.Reason = protocol.ResizeControlReasonOwner
	}
	return control
}

func (session *protocolSession) protocolInfoFromCoreV2(info TerminalInfo) protocol.TerminalInfo {
	out := protocolInfoFromCoreV2(info)
	session.mu.RLock()
	defer session.mu.RUnlock()
	ownerChannel := session.resizeOwners[info.ID]
	owner, hasOwner := session.attachments[ownerChannel]
	for _, attachment := range session.attachments {
		if attachment.TerminalID == info.ID {
			out.ResizeOwnerAttachmentCount++
		}
	}
	if hasOwner && owner.TerminalID == info.ID {
		out.ResizeOwnership = &protocol.ResizeOwnership{
			OwnerAttachmentID: strconv.FormatUint(uint64(ownerChannel), 10),
			OwnerSurfaceID:    owner.SurfaceID,
			OwnerViewID:       owner.ViewID,
			Size:              protocolSizeFromCore(info.Size),
			SizeLocked:        session.sizeLocks[info.ID],
			Epoch:             owner.Epoch,
		}
	}
	return out
}

func normalizeResizePolicy(policy string) string {
	switch policy {
	case protocol.ResizePolicyFollower, protocol.ResizePolicyObserver:
		return policy
	default:
		return protocol.ResizePolicyOwner
	}
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
		rows[i] = protocol.CompactRowFromCellsPreserveTrailingBlankCells(protocolCellsFromHistory(row.Cells), true)
		if row.TailFill != nil {
			style := protocolCompactRowStyleFromHistory(row.TailFill.Style)
			rows[i].TailFill = &style
		}
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

func protocolCompactRowStyleFromHistory(style history.CellStyle) protocol.CompactRowStyle {
	return protocol.CompactRowStyle{
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

func protocolCellsFromHistory(cells []history.Cell) []protocol.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(cells))
	for i, cell := range cells {
		out[i] = protocol.Cell{
			Content:    cell.Text,
			Width:      cell.Width,
			Style:      protocolCellStyleFromHistory(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		}
	}
	return out
}

func protocolCellStyleFromHistory(style history.CellStyle) protocol.CellStyle {
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
		ExitedAt:  info.ExitedAt,
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
			out.StateChanged.ExitedAt = event.Terminal.ExitedAt
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
				ExitedAt: event.Terminal.ExitedAt,
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

func (session *protocolSession) storeFrozenSnapshot(terminalID string, snapshot history.FrozenSnapshot) {
	session.historyMu.Lock()
	defer session.historyMu.Unlock()
	session.historyPins[snapshot.Token] = frozenHistorySnapshot{
		TerminalID: terminalID,
		Snapshot:   snapshot,
	}
	session.historyLatest[terminalID] = snapshot.Token
}

func (session *protocolSession) sessionFrozenSnapshotToken(terminalID string, base string) string {
	seq := session.nextSnapshot.Add(1)
	if base == "" {
		base = "snap"
	}
	return fmt.Sprintf("%s:%s:%d", base, terminalID, seq)
}

func (session *protocolSession) frozenSnapshot(terminalID string, token string) (history.FrozenSnapshot, bool) {
	session.historyMu.Lock()
	defer session.historyMu.Unlock()
	if token == "" {
		token = session.historyLatest[terminalID]
	}
	if token == "" {
		return history.FrozenSnapshot{}, false
	}
	pin, ok := session.historyPins[token]
	if !ok {
		return history.FrozenSnapshot{}, false
	}
	if pin.TerminalID != terminalID {
		return history.FrozenSnapshot{}, false
	}
	return pin.Snapshot, true
}

func protocolCursorToCore(params protocol.HistoryWindowParams) history.HistoryCursor {
	return history.HistoryCursor{
		Valid:           params.CursorValid,
		BeforeLineID:    history.LogicalLineID(params.BeforeLineID),
		BeforeRowInLine: params.BeforeRowInLine,
	}
}

func frozenSnapshotLatestWindow(snapshot history.FrozenSnapshot, cols int, rows int) history.HistoryWindow {
	return frozenSnapshotWindow(snapshot, cols, rows, history.HistoryCursor{}, history.HistoryWindowReplace)
}

func frozenSnapshotOlderWindow(snapshot history.FrozenSnapshot, cols int, rows int, cursor history.HistoryCursor) history.HistoryWindow {
	return frozenSnapshotWindow(snapshot, cols, rows, cursor, history.HistoryWindowPrepend)
}

func frozenSnapshotOldestWindow(snapshot history.FrozenSnapshot, cols int, rows int) history.HistoryWindow {
	if rows <= 0 {
		rows = 24
	}
	selected, hasMore := projectFrozenSnapshotOldestHeadRows(snapshot, cols, rows)
	return buildFrozenSnapshotWindow(snapshot, cols, history.HistoryWindowReplace, selected, history.HistoryCursor{}, hasMore)
}

func frozenSnapshotCursorValid(snapshot history.FrozenSnapshot, cols int, cursor history.HistoryCursor) bool {
	if cols <= 0 || !cursor.Valid {
		return false
	}
	_, _, ok := snapshotCursorStartPosition(snapshot, cols, cursor)
	return ok
}

func frozenSnapshotBoundaryValid(snapshot history.FrozenSnapshot, cols int, firstLineID uint64, lastLineID uint64) bool {
	if firstLineID == 0 && lastLineID == 0 {
		return true
	}
	if cols <= 0 {
		return false
	}
	lines := snapshotVisibleLines(snapshot)
	if len(lines) == 0 {
		return firstLineID == 0 && lastLineID == 0
	}
	if firstLineID == 0 {
		firstLineID = uint64(lines[0].Line.ID)
	}
	if lastLineID == 0 {
		lastLineID = uint64(lines[len(lines)-1].Line.ID)
	}
	if lastLineID != uint64(lines[len(lines)-1].Line.ID) {
		return false
	}
	_, ok := snapshotLineIndex(lines, history.LogicalLineID(firstLineID))
	return ok
}

func frozenSnapshotWindow(snapshot history.FrozenSnapshot, cols int, rows int, cursor history.HistoryCursor, op history.HistoryWindowOp) history.HistoryWindow {
	if rows <= 0 {
		rows = 24
	}
	if op == history.HistoryWindowReplace {
		selected, hasMore := projectFrozenSnapshotLatestTailRows(snapshot, cols, rows)
		return buildFrozenSnapshotWindow(snapshot, cols, op, selected, latestCursorSnapshotTail(selected, hasMore), hasMore)
	}
	selected, nextCursor, hasMore, ok := projectFrozenSnapshotOlderRowsBeforeCursor(snapshot, cols, rows, cursor)
	if !ok {
		return history.HistoryWindow{
			Token:      history.WindowToken(snapshot.Token),
			Op:         op,
			Cols:       cols,
			Generation: snapshot.Generation,
		}
	}
	return buildFrozenSnapshotWindow(snapshot, cols, op, selected, nextCursor, hasMore)
}

type snapshotProjectedRow struct {
	row          history.VisualRow
	lineRowCount int
	committed    bool
}

func buildFrozenSnapshotWindow(
	snapshot history.FrozenSnapshot,
	cols int,
	op history.HistoryWindowOp,
	selected []snapshotProjectedRow,
	cursor history.HistoryCursor,
	hasMore bool,
) history.HistoryWindow {
	spans, visualRows, firstLine, lastLine := buildSnapshotWindowRows(selected)
	return history.HistoryWindow{
		Token:       history.WindowToken(snapshot.Token),
		Op:          op,
		Cols:        cols,
		Rows:        visualRows,
		Spans:       spans,
		Cursor:      cursor,
		HasMore:     hasMore,
		Generation:  snapshot.Generation,
		FirstLineID: firstLine,
		LastLineID:  lastLine,
		LoadedLines: len(spans),
		TotalRows:   len(selected),
		TotalLines:  countCommittedSnapshotLines(snapshot),
	}
}

func buildSnapshotWindowRows(rows []snapshotProjectedRow) ([]history.LogicalLineSpan, []history.VisualRow, history.LogicalLineID, history.LogicalLineID) {
	if len(rows) == 0 {
		return nil, nil, 0, 0
	}
	visualRows := make([]history.VisualRow, len(rows))
	spans := make([]history.LogicalLineSpan, 0)
	firstLine := rows[0].row.LineID
	lastLine := rows[len(rows)-1].row.LineID
	for i := 0; i < len(rows); {
		lineID := rows[i].row.LineID
		start := i
		end := i
		for end+1 < len(rows) && rows[end+1].row.LineID == lineID {
			end++
		}
		clippedBefore := rows[start].row.RowInLine > 0
		clippedAfter := rows[end].row.RowInLine < rows[end].lineRowCount-1
		for rowIndex := start; rowIndex <= end; rowIndex++ {
			row := rows[rowIndex].row
			row.Committed = rows[rowIndex].committed
			row.ClippedBefore = clippedBefore
			row.ClippedAfter = clippedAfter
			visualRows[rowIndex] = row
		}
		spans = append(spans, history.LogicalLineSpan{
			LineID:         lineID,
			FirstRow:       start,
			LastRow:        end,
			ClippedBefore:  clippedBefore,
			ClippedAfter:   clippedAfter,
			LineGeneration: rows[start].row.LineGeneration,
		})
		i = end + 1
	}
	return spans, visualRows, firstLine, lastLine
}

func projectFrozenSnapshotLatestRows(snapshot history.FrozenSnapshot, cols int) []snapshotProjectedRow {
	return projectFrozenSnapshotRows(snapshot.Lines, cols)
}

func projectFrozenSnapshotLatestTailRows(snapshot history.FrozenSnapshot, cols int, maxRows int) ([]snapshotProjectedRow, bool) {
	lines := snapshotVisibleLines(snapshot)
	rows := make([]snapshotProjectedRow, 0, maxRows)
	hasMore := false
	for i := len(lines) - 1; i >= 0; i-- {
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{lines[i]}, cols)
		for rowIndex := len(lineRows) - 1; rowIndex >= 0; rowIndex-- {
			if len(rows) >= maxRows {
				hasMore = true
				break
			}
			rows = append(rows, lineRows[rowIndex])
		}
		if hasMore {
			break
		}
	}
	reverseSnapshotProjectedRows(rows)
	return rows, hasMore
}

func projectFrozenSnapshotOldestHeadRows(snapshot history.FrozenSnapshot, cols int, maxRows int) ([]snapshotProjectedRow, bool) {
	lines := snapshotVisibleLines(snapshot)
	rows := make([]snapshotProjectedRow, 0, maxRows)
	hasMore := false
	for _, line := range lines {
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)
		for _, row := range lineRows {
			if len(rows) >= maxRows {
				hasMore = true
				break
			}
			rows = append(rows, row)
		}
		if hasMore {
			break
		}
	}
	return rows, hasMore
}

func projectFrozenSnapshotOlderRowsBeforeCursor(snapshot history.FrozenSnapshot, cols int, maxRows int, cursor history.HistoryCursor) ([]snapshotProjectedRow, history.HistoryCursor, bool, bool) {
	startLineIndex, startRowIndex, ok := snapshotCursorStartPosition(snapshot, cols, cursor)
	if !ok {
		return nil, history.HistoryCursor{}, false, false
	}
	rows := make([]snapshotProjectedRow, 0, maxRows)
	hasMore := false
	lines := snapshot.Lines
	for lineIndex := startLineIndex; lineIndex >= 0; lineIndex-- {
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{lines[lineIndex]}, cols)
		rowIndex := len(lineRows) - 1
		if lineIndex == startLineIndex {
			rowIndex = startRowIndex
		}
		for ; rowIndex >= 0; rowIndex-- {
			if len(rows) >= maxRows {
				hasMore = true
				break
			}
			rows = append(rows, lineRows[rowIndex])
		}
		if hasMore {
			break
		}
	}
	reverseSnapshotProjectedRows(rows)
	nextCursor := history.HistoryCursor{}
	if hasMore && len(rows) > 0 {
		nextCursor = cursorFromSnapshotRow(rows[0])
	}
	return rows, nextCursor, hasMore, true
}

func latestCursorSnapshotTail(rows []snapshotProjectedRow, hasMore bool) history.HistoryCursor {
	if !hasMore || len(rows) == 0 {
		return history.HistoryCursor{}
	}
	// 中文说明：copy mode 用的是冻结快照；冻结后 live-tail 行也已经是只读
	// source。older 分页必须从当前页第一行继续往上走，不能跳过尚未 committed
	// 但已经被冻结进 copy/history 的屏幕行。
	return cursorFromSnapshotRow(rows[0])
}

func snapshotCursorStartPosition(snapshot history.FrozenSnapshot, cols int, cursor history.HistoryCursor) (int, int, bool) {
	if !cursor.Valid {
		return -1, -1, false
	}
	lines := snapshot.Lines
	if cursor.BeforeLineID == 0 {
		if len(lines) == 0 {
			return -1, -1, false
		}
		lineIndex := len(lines) - 1
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{lines[lineIndex]}, cols)
		if len(lineRows) == 0 {
			return lineIndex - 1, -1, true
		}
		return lineIndex, len(lineRows) - 1, true
	}
	lineIndex, ok := snapshotLineIndex(lines, cursor.BeforeLineID)
	if !ok {
		return -1, -1, false
	}
	lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{lines[lineIndex]}, cols)
	for rowIndex, row := range lineRows {
		if row.row.RowInLine == cursor.BeforeRowInLine {
			return lineIndex, rowIndex - 1, true
		}
	}
	return -1, -1, false
}

func snapshotVisibleLines(snapshot history.FrozenSnapshot) []history.SnapshotLine {
	return snapshot.Lines
}

func snapshotCommittedLines(snapshot history.FrozenSnapshot) []history.SnapshotLine {
	lines := make([]history.SnapshotLine, 0, snapshot.CommittedLines)
	for _, line := range snapshot.Lines {
		if line.Committed {
			lines = append(lines, line)
		}
	}
	return lines
}

func snapshotCommittedEnd(snapshot history.FrozenSnapshot) int {
	if snapshot.CommittedLines > 0 && snapshot.CommittedLines <= len(snapshot.Lines) {
		return snapshot.CommittedLines
	}
	for index, line := range snapshot.Lines {
		if !line.Committed {
			return index
		}
	}
	return len(snapshot.Lines)
}

func snapshotLineIndex(lines []history.SnapshotLine, id history.LogicalLineID) (int, bool) {
	if id == 0 || len(lines) == 0 {
		return -1, false
	}
	// 中文说明：FrozenSnapshot 的 line 序列来自 committed index + frontier，
	// line id 单调递增；older 每页只需要二分定位 cursor/boundary，不应全量扫描。
	index := sort.Search(len(lines), func(i int) bool {
		return lines[i].Line.ID >= id
	})
	if index < len(lines) && lines[index].Line.ID == id {
		return index, true
	}
	for index, line := range lines {
		if line.Line.ID == id {
			return index, true
		}
	}
	return -1, false
}

func reverseSnapshotProjectedRows(rows []snapshotProjectedRow) {
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
}

func projectFrozenSnapshotCommittedRows(snapshot history.FrozenSnapshot, cols int) []snapshotProjectedRow {
	lines := make([]history.SnapshotLine, 0, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		if line.Committed {
			lines = append(lines, line)
		}
	}
	return projectFrozenSnapshotRows(lines, cols)
}

func projectFrozenSnapshotRows(lines []history.SnapshotLine, cols int) []snapshotProjectedRow {
	var rows []snapshotProjectedRow
	for _, snapLine := range lines {
		lineRows := projectFrozenSnapshotLine(snapLine.Line, cols)
		for _, row := range lineRows {
			rows = append(rows, snapshotProjectedRow{
				row:          row,
				lineRowCount: len(lineRows),
				committed:    snapLine.Committed,
			})
		}
	}
	return rows
}

func projectFrozenSnapshotLine(line history.LogicalLine, cols int) []history.VisualRow {
	cells := normalizeProjectionCellsSnapshot(line.Cells)
	if len(cells) == 0 {
		row := history.VisualRow{LineID: line.ID, LineGeneration: line.Generation}
		if line.TailFill != nil {
			fill := *line.TailFill
			row.TailFill = &fill
		}
		return []history.VisualRow{row}
	}
	rows := make([]history.VisualRow, 0)
	rowIndex := 0
	for _, chunk := range wrapCellsSnapshot(cells, cols) {
		rows = append(rows, history.VisualRow{
			Text:           lineTextFromSnapshotCells(chunk),
			Cells:          cloneHistoryCellsSnapshot(chunk),
			LineID:         line.ID,
			RowInLine:      rowIndex,
			LineGeneration: line.Generation,
		})
		rowIndex++
	}
	if line.TailFill != nil && len(rows) > 0 {
		fill := *line.TailFill
		rows[len(rows)-1].TailFill = &fill
	}
	return rows
}

func normalizeProjectionCellsSnapshot(cells []history.Cell) []history.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]history.Cell, 0, len(cells))
	for _, cell := range cells {
		if cell.Text == "" && cell.Width <= 0 {
			continue
		}
		if cell.Width <= 0 {
			out = append(out, expandHistoryCellSnapshot(cell)...)
			continue
		}
		out = append(out, cell)
	}
	return out
}

func wrapCellsSnapshot(cells []history.Cell, cols int) [][]history.Cell {
	if cols <= 0 {
		return [][]history.Cell{cloneHistoryCellsSnapshot(cells)}
	}
	rows := make([][]history.Cell, 0)
	current := make([]history.Cell, 0)
	width := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		rows = append(rows, cloneHistoryCellsSnapshot(current))
		current = current[:0]
		width = 0
	}
	for _, cell := range cells {
		cellWidth := cell.Width
		if cellWidth <= 0 {
			cellWidth = historyCellTextWidth(cell.Text)
		}
		if cellWidth <= 0 {
			continue
		}
		if width+cellWidth <= cols {
			current = append(current, cell)
			width += cellWidth
			if width >= cols {
				flush()
			}
			continue
		}
		for _, part := range splitMeasuredHistoryCellSnapshot(cell) {
			partWidth := part.Width
			if partWidth <= 0 {
				partWidth = historyCellTextWidth(part.Text)
			}
			if partWidth <= 0 {
				continue
			}
			if width > 0 && width+partWidth > cols {
				flush()
			}
			current = append(current, part)
			width += partWidth
			if width >= cols {
				flush()
			}
		}
	}
	flush()
	if len(rows) == 0 {
		rows = append(rows, nil)
	}
	return rows
}

func cloneHistoryCellsSnapshot(cells []history.Cell) []history.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]history.Cell, len(cells))
	copy(out, cells)
	return out
}

func lineTextFromSnapshotCells(cells []history.Cell) string {
	var builder strings.Builder
	for _, cell := range cells {
		builder.WriteString(cell.Text)
	}
	return builder.String()
}

func historyCellTextWidth(text string) int {
	width := 0
	for _, cluster := range historyTextClustersSnapshot(text) {
		width += uniseg.StringWidth(cluster)
	}
	return width
}

func splitMeasuredHistoryCellSnapshot(cell history.Cell) []history.Cell {
	clusters := historyTextClustersSnapshot(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	out := make([]history.Cell, 0, len(clusters))
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = uniseg.StringWidth(cluster)
		out = append(out, next)
	}
	return out
}

func expandHistoryCellSnapshot(cell history.Cell) []history.Cell {
	clusters := historyTextClustersSnapshot(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	out := make([]history.Cell, 0, len(clusters))
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = uniseg.StringWidth(cluster)
		out = append(out, next)
	}
	return out
}

func historyTextClustersSnapshot(text string) []string {
	if text == "" {
		return nil
	}
	graphemes := uniseg.NewGraphemes(text)
	clusters := make([]string, 0)
	for graphemes.Next() {
		cluster := graphemes.Str()
		if cluster == "\n" {
			continue
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func latestCursorSnapshot(rows []snapshotProjectedRow, selectionStart int) history.HistoryCursor {
	if len(rows) == 0 {
		return history.HistoryCursor{}
	}
	for i := selectionStart; i < len(rows); i++ {
		if !rows[i].committed {
			continue
		}
		if hasCommittedRowBeforeSnapshot(rows, i) {
			return cursorFromSnapshotRow(rows[i])
		}
		return history.HistoryCursor{}
	}
	if hasAnyCommittedRowSnapshot(rows) {
		return history.HistoryCursor{Valid: true}
	}
	return history.HistoryCursor{}
}

func cursorBeforeSelectedSnapshot(rows []snapshotProjectedRow, selectionStart int) history.HistoryCursor {
	if len(rows) == 0 || selectionStart <= 0 {
		return history.HistoryCursor{}
	}
	return cursorFromSnapshotRow(rows[selectionStart])
}

func cursorFromSnapshotRow(row snapshotProjectedRow) history.HistoryCursor {
	return history.HistoryCursor{
		Valid:           true,
		BeforeLineID:    row.row.LineID,
		BeforeRowInLine: row.row.RowInLine,
	}
}

func historyCursorBoundaryIndex(rows []snapshotProjectedRow, cursor history.HistoryCursor) int {
	if !cursor.Valid {
		return -1
	}
	if cursor.BeforeLineID == 0 {
		return len(rows)
	}
	for i, row := range rows {
		if row.row.LineID == cursor.BeforeLineID && row.row.RowInLine == cursor.BeforeRowInLine {
			return i
		}
	}
	return -1
}

func hasCommittedRowBeforeSnapshot(rows []snapshotProjectedRow, index int) bool {
	for i := 0; i < index; i++ {
		if rows[i].committed {
			return true
		}
	}
	return false
}

func hasAnyCommittedRowSnapshot(rows []snapshotProjectedRow) bool {
	for _, row := range rows {
		if row.committed {
			return true
		}
	}
	return false
}

func tailStartSnapshot(totalRows int, maxRows int) int {
	if totalRows <= maxRows {
		return 0
	}
	return totalRows - maxRows
}

func countCommittedSnapshotLines(snapshot history.FrozenSnapshot) int {
	if snapshot.CommittedLines > 0 && snapshot.CommittedLines <= len(snapshot.Lines) {
		return snapshot.CommittedLines
	}
	count := 0
	for _, line := range snapshot.Lines {
		if line.Committed {
			count++
		}
	}
	return count
}

func tokenPartHasNumericPrefix(part string, prefix string) bool {
	return strings.HasPrefix(part, prefix) && len(part) > len(prefix) && part[len(prefix)] >= '0' && part[len(prefix)] <= '9'
}

func errorCode(err error) int {
	switch {
	case errors.Is(err, ErrTerminalNotFound):
		return protocolErrorNotFound
	case errors.Is(err, ErrInvalidTerminalID), errors.Is(err, ErrInvalidCommand), errors.Is(err, ErrInvalidServerSize), errors.Is(err, ErrTerminalExited), errors.Is(err, ErrStaleHistoryWindow), errors.Is(err, ErrInvalidStorageKey), errors.Is(err, ErrStorageVersionConflict), errors.Is(err, ErrInvalidWorkbenchMutation), errors.Is(err, ErrDuplicateWorkbenchResource), errors.Is(err, ErrWorkbenchVersionConflict), errors.Is(err, errProtocolAttachmentMismatch):
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
