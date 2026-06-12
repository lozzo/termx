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
	attachments  map[uint16]protocolAttachment
	resizeOwners map[string]uint16
	sizeLocks    map[string]bool
	ownerEpoch   uint64
	cancelEvents context.CancelFunc
	historyMu    sync.Mutex
	historyPins  map[string]frozenHistorySnapshot
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
		server:       server,
		conn:         conn,
		attachments:  make(map[uint16]protocolAttachment),
		resizeOwners: make(map[string]uint16),
		sizeLocks:    make(map[string]bool),
		historyPins:  make(map[string]frozenHistorySnapshot),
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
	} else {
		terminal, err := session.server.Terminal(params.TerminalID)
		if err != nil {
			return nil, err
		}
		snapshot := terminal.FreezeSnapshot()
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
	if params.CursorValid {
		cursor := protocolCursorToCore(params)
		if !frozenSnapshotCursorValid(snapshot, cols, cursor) {
			return ErrStaleHistoryWindow
		}
	}
	latest := frozenSnapshotLatestWindow(snapshot, cols, 1)
	if params.BoundaryFirstLineID != 0 && params.BoundaryFirstLineID != uint64(latest.FirstLineID) {
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

func (session *protocolSession) attach(params protocol.AttachParams) (protocolAttachment, *protocol.ResizeControl, error) {
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return protocolAttachment{}, nil, err
	}
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

func (session *protocolSession) storeFrozenSnapshot(terminalID string, snapshot history.FrozenSnapshot) {
	session.historyMu.Lock()
	defer session.historyMu.Unlock()
	session.historyPins[terminalID] = frozenHistorySnapshot{
		TerminalID: terminalID,
		Snapshot:   snapshot,
	}
}

func (session *protocolSession) frozenSnapshot(terminalID string, token string) (history.FrozenSnapshot, bool) {
	session.historyMu.Lock()
	defer session.historyMu.Unlock()
	pin, ok := session.historyPins[terminalID]
	if !ok {
		return history.FrozenSnapshot{}, false
	}
	if token != "" && pin.Snapshot.Token != token {
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

func frozenSnapshotCursorValid(snapshot history.FrozenSnapshot, cols int, cursor history.HistoryCursor) bool {
	if cols <= 0 || !cursor.Valid {
		return false
	}
	rows := projectFrozenSnapshotCommittedRows(snapshot, cols)
	if len(rows) == 0 {
		return false
	}
	return historyCursorBoundaryIndex(rows, cursor) >= 0
}

func frozenSnapshotWindow(snapshot history.FrozenSnapshot, cols int, rows int, cursor history.HistoryCursor, op history.HistoryWindowOp) history.HistoryWindow {
	if rows <= 0 {
		rows = 24
	}
	var projected []snapshotProjectedRow
	if op == history.HistoryWindowReplace {
		projected = projectFrozenSnapshotLatestRows(snapshot, cols)
		start := tailStartSnapshot(len(projected), rows)
		selected := projected[start:]
		return buildFrozenSnapshotWindow(snapshot, cols, op, projected, selected, latestCursorSnapshot(projected, start))
	}
	projected = projectFrozenSnapshotCommittedRows(snapshot, cols)
	boundary := historyCursorBoundaryIndex(projected, cursor)
	if boundary < 0 {
		return history.HistoryWindow{
			Token:      history.WindowToken(snapshot.Token),
			Op:         op,
			Cols:       cols,
			Generation: snapshot.Generation,
		}
	}
	candidates := projected[:boundary]
	start := tailStartSnapshot(len(candidates), rows)
	selected := candidates[start:]
	return buildFrozenSnapshotWindow(snapshot, cols, op, projected, selected, cursorBeforeSelectedSnapshot(candidates, start))
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
	allRows []snapshotProjectedRow,
	selected []snapshotProjectedRow,
	cursor history.HistoryCursor,
) history.HistoryWindow {
	spans, visualRows, firstLine, lastLine := buildSnapshotWindowRows(selected)
	return history.HistoryWindow{
		Token:       history.WindowToken(snapshot.Token),
		Op:          op,
		Cols:        cols,
		Rows:        visualRows,
		Spans:       spans,
		Cursor:      cursor,
		HasMore:     cursor.Valid,
		Generation:  snapshot.Generation,
		FirstLineID: firstLine,
		LastLineID:  lastLine,
		LoadedLines: len(spans),
		TotalRows:   len(allRows),
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
		return []history.VisualRow{{LineID: line.ID, LineGeneration: line.Generation}}
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
		width := cell.Width
		if width <= 0 {
			width = historyCellTextWidth(cell.Text)
		}
		if width <= 0 {
			continue
		}
		next := cell
		next.Width = width
		out = append(out, next)
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
		if width > 0 && width+cell.Width > cols {
			flush()
		}
		current = append(current, cell)
		width += cell.Width
		if width >= cols {
			flush()
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
	for _, r := range text {
		if r == '\n' {
			continue
		}
		width++
	}
	return width
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
