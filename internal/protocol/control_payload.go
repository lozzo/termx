package protocol

import (
	"fmt"
	"time"

	"github.com/lozzow/termx/termx-proto/wirepb"
	"github.com/lozzow/termx/termx-shared/plugin"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func EncodeHelloPayload(hello Hello) ([]byte, error) {
	return proto.Marshal(&wirepb.Hello{
		Version: uint32(hello.Version),
		Client:  hello.Client,
		Server:  hello.Server,
	})
}

func DecodeHelloPayload(payload []byte) (Hello, error) {
	var msg wirepb.Hello
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return Hello{}, err
	}
	return Hello{Version: int(msg.GetVersion()), Client: msg.GetClient(), Server: msg.GetServer()}, nil
}

func EncodeRequestPayload(req Request) ([]byte, error) {
	return proto.Marshal(&wirepb.RequestEnvelope{
		Id:     req.ID,
		Method: req.Method,
		Params: append([]byte(nil), req.Params...),
	})
}

func DecodeRequestPayload(payload []byte) (Request, error) {
	var msg wirepb.RequestEnvelope
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return Request{}, err
	}
	return Request{ID: msg.GetId(), Method: msg.GetMethod(), Params: append([]byte(nil), msg.GetParams()...)}, nil
}

func EncodeResponsePayload(resp Response) ([]byte, error) {
	return proto.Marshal(&wirepb.ResponseEnvelope{
		Id:     resp.ID,
		Result: append([]byte(nil), resp.Result...),
	})
}

func DecodeResponsePayload(payload []byte) (Response, error) {
	var msg wirepb.ResponseEnvelope
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return Response{}, err
	}
	return Response{ID: msg.GetId(), Result: append([]byte(nil), msg.GetResult()...)}, nil
}

func EncodeErrorPayload(msg ErrorMessage) ([]byte, error) {
	return proto.Marshal(&wirepb.ErrorEnvelope{
		Id: msg.ID,
		Error: &wirepb.ProtocolError{
			Code:    int32(msg.Error.Code),
			Message: msg.Error.Message,
		},
	})
}

func DecodeErrorPayload(payload []byte) (ErrorMessage, error) {
	var msg wirepb.ErrorEnvelope
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return ErrorMessage{}, err
	}
	return ErrorMessage{
		ID: msg.GetId(),
		Error: ProtocolError{
			Code:    int(msg.GetError().GetCode()),
			Message: msg.GetError().GetMessage(),
		},
	}, nil
}

func EncodeEventPayload(event Event) ([]byte, error) {
	return proto.Marshal(eventToWirePB(event))
}

func DecodeEventPayload(payload []byte) (Event, error) {
	var msg wirepb.Event
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return Event{}, err
	}
	return eventFromWirePB(&msg), nil
}

// EncodeClientControlInvocationPayload 编码 daemon broker 投递给 client mailbox 的 invocation。
// payload 只用于 client.control.watch 返回的 stream channel，不能混进普通 daemon events。
func EncodeClientControlInvocationPayload(invocation ClientControlInvocation) ([]byte, error) {
	return proto.Marshal(clientControlInvocationToWirePB(invocation))
}

// DecodeClientControlInvocationPayload 解码 client.control.watch mailbox item。
// 调用方必须把它继续交给 client 本地 action registry/reducer，不得在 protocol 层解释 UI state。
func DecodeClientControlInvocationPayload(payload []byte) (ClientControlInvocation, error) {
	var msg wirepb.ClientControlInvocation
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return ClientControlInvocation{}, err
	}
	return clientControlInvocationFromWirePB(&msg), nil
}

func EncodeMethodParams(method string, params any) ([]byte, error) {
	if params == nil {
		return proto.Marshal(&wirepb.Empty{})
	}
	switch method {
	case "create":
		value, ok := params.(CreateParams)
		if !ok {
			if ptr, ptrOK := params.(*CreateParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.CreateParams", params)
		}
		return proto.Marshal(createParamsToWirePB(value))
	case "list", "path.defaults", "remote.status", "remote.local.status", "remote.local.disable":
		return proto.Marshal(&wirepb.Empty{})
	case "get", "kill", "restart", "remove", "history.backlog.status":
		value, ok := params.(GetParams)
		if !ok {
			if ptr, ptrOK := params.(*GetParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.GetParams", params)
		}
		return proto.Marshal(&wirepb.GetParams{TerminalId: value.TerminalID})
	case "resize":
		value, ok := params.(ResizeParams)
		if !ok {
			if ptr, ptrOK := params.(*ResizeParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.ResizeParams", params)
		}
		return proto.Marshal(&wirepb.ResizeParams{TerminalId: value.TerminalID, Cols: uint32(value.Cols), Rows: uint32(value.Rows)})
	case "input":
		value, ok := params.(InputParams)
		if !ok {
			if ptr, ptrOK := params.(*InputParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.InputParams", params)
		}
		return encodeInputParamsPayload(value), nil
	case "ensure_resize", "resize.lock", "resize.unlock":
		value, ok := params.(EnsureResizeParams)
		if !ok {
			if ptr, ptrOK := params.(*EnsureResizeParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			if control, controlOK := params.(ResizeControlParams); controlOK {
				value = EnsureResizeParams{
					TerminalID:   control.TerminalID,
					Channel:      control.Channel,
					ResizePolicy: control.ResizePolicy,
					SurfaceID:    control.SurfaceID,
					ViewID:       control.ViewID,
				}
				ok = true
			} else if ptr, ptrOK := params.(*ResizeControlParams); ptrOK && ptr != nil {
				value = EnsureResizeParams{
					TerminalID:   ptr.TerminalID,
					Channel:      ptr.Channel,
					ResizePolicy: ptr.ResizePolicy,
					SurfaceID:    ptr.SurfaceID,
					ViewID:       ptr.ViewID,
				}
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.EnsureResizeParams", params)
		}
		return proto.Marshal(&wirepb.EnsureResizeParams{
			TerminalId:   value.TerminalID,
			Channel:      uint32(value.Channel),
			Cols:         uint32(value.Cols),
			Rows:         uint32(value.Rows),
			ResizePolicy: value.ResizePolicy,
			SurfaceId:    value.SurfaceID,
			ViewId:       value.ViewID,
		})
	case "set_tags":
		value, ok := params.(SetTagsParams)
		if !ok {
			if ptr, ptrOK := params.(*SetTagsParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.SetTagsParams", params)
		}
		return proto.Marshal(&wirepb.SetTagsParams{TerminalId: value.TerminalID, Tags: cloneStringMap(value.Tags)})
	case "set_metadata":
		value, ok := params.(SetMetadataParams)
		if !ok {
			if ptr, ptrOK := params.(*SetMetadataParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.SetMetadataParams", params)
		}
		return proto.Marshal(&wirepb.SetMetadataParams{TerminalId: value.TerminalID, Name: value.Name, Tags: cloneStringMap(value.Tags)})
	case "attach":
		value, ok := params.(AttachParams)
		if !ok {
			if ptr, ptrOK := params.(*AttachParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.AttachParams", params)
		}
		return proto.Marshal(&wirepb.AttachParams{
			TerminalId:   value.TerminalID,
			Mode:         value.Mode,
			ResizePolicy: value.ResizePolicy,
			SurfaceId:    value.SurfaceID,
			ViewId:       value.ViewID,
		})
	case "detach":
		value, ok := params.(DetachParams)
		if !ok {
			if ptr, ptrOK := params.(*DetachParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.DetachParams", params)
		}
		return proto.Marshal(&wirepb.EnsureResizeParams{
			TerminalId: value.TerminalID,
			Channel:    uint32(value.Channel),
			SurfaceId:  value.SurfaceID,
			ViewId:     value.ViewID,
		})
	case "events":
		value, ok := params.(EventsParams)
		if !ok {
			if ptr, ptrOK := params.(*EventsParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.EventsParams", params)
		}
		return proto.Marshal(eventsParamsToWirePB(value))
	case "storage.get":
		value, ok := params.(StorageGetParams)
		if !ok {
			if ptr, ptrOK := params.(*StorageGetParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.StorageGetParams", params)
		}
		return proto.Marshal(storageGetParamsToWirePB(value))
	case "storage.put":
		value, ok := params.(StoragePutParams)
		if !ok {
			if ptr, ptrOK := params.(*StoragePutParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.StoragePutParams", params)
		}
		return proto.Marshal(storagePutParamsToWirePB(value))
	case "storage.delete":
		value, ok := params.(StorageDeleteParams)
		if !ok {
			if ptr, ptrOK := params.(*StorageDeleteParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.StorageDeleteParams", params)
		}
		return proto.Marshal(storageDeleteParamsToWirePB(value))
	case "storage.list":
		value, ok := params.(StorageListParams)
		if !ok {
			if ptr, ptrOK := params.(*StorageListParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.StorageListParams", params)
		}
		return proto.Marshal(storageListParamsToWirePB(value))
	case "path.list_dirs":
		value, ok := params.(PathListDirsParams)
		if !ok {
			if ptr, ptrOK := params.(*PathListDirsParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.PathListDirsParams", params)
		}
		return proto.Marshal(pathListDirsParamsToWirePB(value))
	case "workbench.get":
		value, ok := params.(WorkbenchGetParams)
		if !ok {
			if ptr, ptrOK := params.(*WorkbenchGetParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.WorkbenchGetParams", params)
		}
		return proto.Marshal(workbenchGetParamsToWirePB(value))
	case "workbench.apply":
		value, ok := params.(WorkbenchMutateParams)
		if !ok {
			if ptr, ptrOK := params.(*WorkbenchMutateParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.WorkbenchMutateParams", params)
		}
		return proto.Marshal(workbenchMutateParamsToWirePB(value))
	case "live.screen.get":
		value, ok := params.(LiveScreenParams)
		if !ok {
			if ptr, ptrOK := params.(*LiveScreenParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.LiveScreenParams", params)
		}
		return proto.Marshal(&wirepb.GetParams{TerminalId: value.TerminalID})
	case "live.invalidation.next":
		value, ok := params.(LiveInvalidationNextParams)
		if !ok {
			if ptr, ptrOK := params.(*LiveInvalidationNextParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.LiveInvalidationNextParams", params)
		}
		msg := &wirepb.GetParams{TerminalId: value.TerminalID}
		setUint64ProtoFieldOrUnknown(msg, liveInvalidationObservedRevisionFieldNumber, value.ObservedRevision)
		return proto.Marshal(msg)
	case "history.window", "history.release", "history.copy":
		value, ok := params.(HistoryWindowParams)
		if !ok {
			if ptr, ptrOK := params.(*HistoryWindowParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.HistoryWindowParams", params)
		}
		msg := &wirepb.HistoryWindowParams{
			TerminalId:          value.TerminalID,
			BeforeOffset:        int32(value.BeforeOffset),
			Limit:               int32(value.Limit),
			Cols:                int32(value.Cols),
			Token:               value.Token,
			HistoryGeneration:   value.Generation,
			CursorValid:         value.CursorValid,
			BeforeLineId:        value.BeforeLineID,
			BeforeRowInLine:     int32(value.BeforeRowInLine),
			BoundaryFirstLineId: value.BoundaryFirstLineID,
			BoundaryLastLineId:  value.BoundaryLastLineID,
		}
		encodeHistoryWindowParamsUnknownFields(msg, value)
		return proto.Marshal(msg)
	case "remote.pair.start":
		value, ok := params.(RemotePairStartParams)
		if !ok {
			if ptr, ptrOK := params.(*RemotePairStartParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.RemotePairStartParams", params)
		}
		return proto.Marshal(remotePairStartParamsToWirePB(value))
	case "remote.local.enable":
		value, ok := params.(RemoteLocalEnableParams)
		if !ok {
			if ptr, ptrOK := params.(*RemoteLocalEnableParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.RemoteLocalEnableParams", params)
		}
		return proto.Marshal(remoteLocalEnableParamsToWirePB(value))
	case MethodClientSessionRegister:
		value, ok := params.(ClientSessionRegisterParams)
		if !ok {
			if ptr, ptrOK := params.(*ClientSessionRegisterParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.ClientSessionRegisterParams", params)
		}
		return proto.Marshal(clientSessionRegisterParamsToWirePB(value))
	case MethodClientSessionList:
		value, ok := params.(ClientSessionListParams)
		if !ok {
			if ptr, ptrOK := params.(*ClientSessionListParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.ClientSessionListParams", params)
		}
		return proto.Marshal(clientSessionListParamsToWirePB(value))
	case MethodClientControlCall:
		value, ok := params.(ClientControlCallParams)
		if !ok {
			if ptr, ptrOK := params.(*ClientControlCallParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.ClientControlCallParams", params)
		}
		return proto.Marshal(clientControlCallParamsToWirePB(value))
	case MethodClientControlWatch:
		value, ok := params.(ClientControlWatchParams)
		if !ok {
			if ptr, ptrOK := params.(*ClientControlWatchParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.ClientControlWatchParams", params)
		}
		return proto.Marshal(clientControlWatchParamsToWirePB(value))
	case MethodClientControlUnwatch:
		value, ok := params.(ClientControlUnwatchParams)
		if !ok {
			if ptr, ptrOK := params.(*ClientControlUnwatchParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.ClientControlUnwatchParams", params)
		}
		return proto.Marshal(clientControlUnwatchParamsToWirePB(value))
	case MethodClientControlRespond:
		value, ok := params.(ClientControlResponseParams)
		if !ok {
			if ptr, ptrOK := params.(*ClientControlResponseParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.ClientControlResponseParams", params)
		}
		return proto.Marshal(clientControlResponseParamsToWirePB(value))
	default:
		return nil, fmt.Errorf("protocol: no protobuf params codec for method %q", method)
	}
}

func DecodeMethodParams(method string, payload []byte) (any, error) {
	switch method {
	case "create":
		var msg wirepb.CreateParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return createParamsFromWirePB(&msg), nil
	case "list", "path.defaults", "remote.status", "remote.local.status", "remote.local.disable":
		return struct{}{}, decodeEmpty(payload)
	case "get", "kill", "restart", "remove", "history.backlog.status":
		var msg wirepb.GetParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return GetParams{TerminalID: msg.GetTerminalId()}, nil
	case "resize":
		var msg wirepb.ResizeParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return ResizeParams{TerminalID: msg.GetTerminalId(), Cols: uint16(msg.GetCols()), Rows: uint16(msg.GetRows())}, nil
	case "input":
		return decodeInputParamsPayload(payload)
	case "ensure_resize", "resize.lock", "resize.unlock":
		var msg wirepb.EnsureResizeParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		if method == "resize.lock" || method == "resize.unlock" {
			return ResizeControlParams{
				TerminalID:   msg.GetTerminalId(),
				Channel:      uint16(msg.GetChannel()),
				ResizePolicy: msg.GetResizePolicy(),
				SurfaceID:    msg.GetSurfaceId(),
				ViewID:       msg.GetViewId(),
			}, nil
		}
		return EnsureResizeParams{
			TerminalID:   msg.GetTerminalId(),
			Channel:      uint16(msg.GetChannel()),
			Cols:         uint16(msg.GetCols()),
			Rows:         uint16(msg.GetRows()),
			ResizePolicy: msg.GetResizePolicy(),
			SurfaceID:    msg.GetSurfaceId(),
			ViewID:       msg.GetViewId(),
		}, nil
	case "set_tags":
		var msg wirepb.SetTagsParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return SetTagsParams{TerminalID: msg.GetTerminalId(), Tags: cloneStringMap(msg.GetTags())}, nil
	case "set_metadata":
		var msg wirepb.SetMetadataParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return SetMetadataParams{TerminalID: msg.GetTerminalId(), Name: msg.GetName(), Tags: cloneStringMap(msg.GetTags())}, nil
	case "attach":
		var msg wirepb.AttachParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return AttachParams{
			TerminalID:   msg.GetTerminalId(),
			Mode:         msg.GetMode(),
			ResizePolicy: msg.GetResizePolicy(),
			SurfaceID:    msg.GetSurfaceId(),
			ViewID:       msg.GetViewId(),
		}, nil
	case "detach":
		var msg wirepb.EnsureResizeParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return DetachParams{TerminalID: msg.GetTerminalId(), Channel: uint16(msg.GetChannel()), SurfaceID: msg.GetSurfaceId(), ViewID: msg.GetViewId()}, nil
	case "events":
		var msg wirepb.EventsParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return eventsParamsFromWirePB(&msg), nil
	case "storage.get":
		var msg wirepb.StorageGetParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return storageGetParamsFromWirePB(&msg), nil
	case "storage.put":
		var msg wirepb.StoragePutParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return storagePutParamsFromWirePB(&msg), nil
	case "storage.delete":
		var msg wirepb.StorageDeleteParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return storageDeleteParamsFromWirePB(&msg), nil
	case "storage.list":
		var msg wirepb.StorageListParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return storageListParamsFromWirePB(&msg), nil
	case "path.list_dirs":
		var msg wirepb.PathListDirsParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return pathListDirsParamsFromWirePB(&msg), nil
	case "workbench.get":
		var msg wirepb.WorkbenchGetParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return workbenchGetParamsFromWirePB(&msg), nil
	case "workbench.apply":
		var msg wirepb.WorkbenchMutateParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return workbenchMutateParamsFromWirePB(&msg), nil
	case "live.screen.get":
		var msg wirepb.GetParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return LiveScreenParams{TerminalID: msg.GetTerminalId()}, nil
	case "live.invalidation.next":
		var msg wirepb.GetParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return LiveInvalidationNextParams{
			TerminalID:       msg.GetTerminalId(),
			ObservedRevision: uint64ProtoFieldOrUnknown(&msg, liveInvalidationObservedRevisionFieldNumber),
		}, nil
	case "history.window", "history.release", "history.copy":
		var msg wirepb.HistoryWindowParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		params := HistoryWindowParams{
			TerminalID:          msg.GetTerminalId(),
			BeforeOffset:        int(msg.GetBeforeOffset()),
			Limit:               int(msg.GetLimit()),
			Cols:                int(msg.GetCols()),
			Token:               msg.GetToken(),
			Generation:          msg.GetHistoryGeneration(),
			CursorValid:         msg.GetCursorValid(),
			BeforeLineID:        msg.GetBeforeLineId(),
			BeforeRowInLine:     int(msg.GetBeforeRowInLine()),
			BoundaryFirstLineID: msg.GetBoundaryFirstLineId(),
			BoundaryLastLineID:  msg.GetBoundaryLastLineId(),
		}
		decodeHistoryWindowParamsUnknownFields(&msg, &params)
		return params, nil
	case "remote.pair.start":
		var msg wirepb.RemotePairStartParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return remotePairStartParamsFromWirePB(&msg), nil
	case "remote.local.enable":
		var msg wirepb.RemoteLocalEnableParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return remoteLocalEnableParamsFromWirePB(&msg), nil
	case MethodClientSessionRegister:
		var msg wirepb.ClientSessionRegisterParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return clientSessionRegisterParamsFromWirePB(&msg), nil
	case MethodClientSessionList:
		var msg wirepb.ClientSessionListParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return clientSessionListParamsFromWirePB(&msg), nil
	case MethodClientControlCall:
		var msg wirepb.ClientControlCallParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return clientControlCallParamsFromWirePB(&msg), nil
	case MethodClientControlWatch:
		var msg wirepb.ClientControlWatchParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return clientControlWatchParamsFromWirePB(&msg), nil
	case MethodClientControlUnwatch:
		var msg wirepb.ClientControlUnwatchParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return clientControlUnwatchParamsFromWirePB(&msg)
	case MethodClientControlRespond:
		var msg wirepb.ClientControlResponseParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return clientControlResponseParamsFromWirePB(&msg), nil
	default:
		return nil, fmt.Errorf("protocol: no protobuf params codec for method %q", method)
	}
}

func EncodeMethodResult(method string, result any) ([]byte, error) {
	if result == nil {
		return proto.Marshal(&wirepb.Empty{})
	}
	switch method {
	case "create":
		value, ok := result.(CreateResult)
		if !ok {
			if ptr, ptrOK := result.(*CreateResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.CreateResult", result)
		}
		return proto.Marshal(&wirepb.CreateResult{TerminalId: value.TerminalID, State: value.State})
	case "list":
		value, ok := result.(ListResult)
		if !ok {
			if ptr, ptrOK := result.(*ListResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.ListResult", result)
		}
		return proto.Marshal(listResultToWirePB(value))
	case "get":
		value, ok := result.(TerminalInfo)
		if !ok {
			if ptr, ptrOK := result.(*TerminalInfo); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.TerminalInfo", result)
		}
		return proto.Marshal(terminalInfoToWirePB(value))
	case "ensure_resize", "resize.lock", "resize.unlock":
		value, ok := result.(EnsureResizeResult)
		if !ok {
			if ptr, ptrOK := result.(*EnsureResizeResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			if control, controlOK := result.(ResizeControlResult); controlOK {
				value = EnsureResizeResult{ResizeControl: control.ResizeControl, Size: control.Size}
				ok = true
			} else if ptr, ptrOK := result.(*ResizeControlResult); ptrOK && ptr != nil {
				value = EnsureResizeResult{ResizeControl: ptr.ResizeControl, Size: ptr.Size}
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.EnsureResizeResult", result)
		}
		return proto.Marshal(&wirepb.EnsureResizeResult{ResizeControl: resizeControlToWirePB(value.ResizeControl), Size: sizeToWirePB(value.Size), Resized: value.Resized})
	case "attach":
		value, ok := result.(AttachResult)
		if !ok {
			if ptr, ptrOK := result.(*AttachResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.AttachResult", result)
		}
		return proto.Marshal(&wirepb.AttachResult{Mode: value.Mode, Channel: uint32(value.Channel), ResizeControl: resizeControlToWirePB(value.ResizeControl)})
	case "storage.get", "storage.put":
		value, ok := result.(StorageEntry)
		if !ok {
			if ptr, ptrOK := result.(*StorageEntry); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.StorageEntry", result)
		}
		return proto.Marshal(storageEntryToWirePB(value))
	case "storage.delete":
		value, ok := result.(StorageDeleteResult)
		if !ok {
			if ptr, ptrOK := result.(*StorageDeleteResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.StorageDeleteResult", result)
		}
		return proto.Marshal(storageDeleteResultToWirePB(value))
	case "storage.list":
		value, ok := result.(StorageListResult)
		if !ok {
			if ptr, ptrOK := result.(*StorageListResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.StorageListResult", result)
		}
		return proto.Marshal(storageListResultToWirePB(value))
	case "path.list_dirs":
		value, ok := result.(PathListDirsResult)
		if !ok {
			if ptr, ptrOK := result.(*PathListDirsResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.PathListDirsResult", result)
		}
		return proto.Marshal(pathListDirsResultToWirePB(value))
	case "path.defaults":
		value, ok := result.(PathDefaultsResult)
		if !ok {
			if ptr, ptrOK := result.(*PathDefaultsResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.PathDefaultsResult", result)
		}
		return proto.Marshal(pathDefaultsResultToWirePB(value))
	case "workbench.get":
		value, ok := result.(WorkbenchSnapshot)
		if !ok {
			if ptr, ptrOK := result.(*WorkbenchSnapshot); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.WorkbenchSnapshot", result)
		}
		return proto.Marshal(workbenchSnapshotToWirePB(value))
	case "workbench.apply":
		value, ok := result.(WorkbenchMutateResult)
		if !ok {
			if ptr, ptrOK := result.(*WorkbenchMutateResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.WorkbenchMutateResult", result)
		}
		return proto.Marshal(workbenchMutateResultToWirePB(value))
	case "history.backlog.status":
		value, ok := result.(HistoryBacklogStatus)
		if !ok {
			if ptr, ptrOK := result.(*HistoryBacklogStatus); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.HistoryBacklogStatus", result)
		}
		return proto.Marshal(historyBacklogStatusToWirePB(value))
	case "remote.status":
		value, ok := result.(RemoteStatus)
		if !ok {
			if ptr, ptrOK := result.(*RemoteStatus); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.RemoteStatus", result)
		}
		return proto.Marshal(remoteStatusToWirePB(value))
	case "remote.pair.start":
		value, ok := result.(RemotePairStartResult)
		if !ok {
			if ptr, ptrOK := result.(*RemotePairStartResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.RemotePairStartResult", result)
		}
		return proto.Marshal(remotePairStartResultToWirePB(value))
	case "remote.local.enable", "remote.local.status", "remote.local.disable":
		value, ok := result.(RemoteLocalStatus)
		if !ok {
			if ptr, ptrOK := result.(*RemoteLocalStatus); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.RemoteLocalStatus", result)
		}
		return proto.Marshal(remoteLocalStatusToWirePB(value))
	case MethodClientSessionRegister:
		value, ok := result.(ClientSessionRegisterResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientSessionRegisterResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.ClientSessionRegisterResult", result)
		}
		return proto.Marshal(clientSessionRegisterResultToWirePB(value))
	case MethodClientSessionList:
		value, ok := result.(ClientSessionListResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientSessionListResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.ClientSessionListResult", result)
		}
		return proto.Marshal(clientSessionListResultToWirePB(value))
	case MethodClientControlCall:
		value, ok := result.(ClientControlCallResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientControlCallResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.ClientControlCallResult", result)
		}
		return proto.Marshal(clientControlCallResultToWirePB(value))
	case MethodClientControlWatch:
		value, ok := result.(ClientControlWatchResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientControlWatchResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.ClientControlWatchResult", result)
		}
		return proto.Marshal(clientControlWatchResultToWirePB(value))
	case MethodClientControlUnwatch:
		value, ok := result.(ClientControlUnwatchResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientControlUnwatchResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.ClientControlUnwatchResult", result)
		}
		return proto.Marshal(clientControlUnwatchResultToWirePB(value))
	case MethodClientControlRespond:
		value, ok := result.(ClientControlResponseResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientControlResponseResult); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodResultTypeError(method, "protocol.ClientControlResponseResult", result)
		}
		return proto.Marshal(clientControlResponseResultToWirePB(value))
	default:
		return nil, fmt.Errorf("protocol: no protobuf result codec for method %q", method)
	}
}

func DecodeMethodResult(method string, payload []byte, out any) error {
	if out == nil {
		return nil
	}
	switch method {
	case "create":
		var msg wirepb.CreateResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*CreateResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.CreateResult", out)
		}
		*ptr = CreateResult{TerminalID: msg.GetTerminalId(), State: msg.GetState()}
		return nil
	case "list":
		var msg wirepb.ListResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*ListResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.ListResult", out)
		}
		*ptr = listResultFromWirePB(&msg)
		return nil
	case "get":
		var msg wirepb.TerminalInfo
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*TerminalInfo)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.TerminalInfo", out)
		}
		*ptr = terminalInfoFromWirePB(&msg)
		return nil
	case "ensure_resize", "resize.lock", "resize.unlock":
		var msg wirepb.EnsureResizeResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		if method == "resize.lock" || method == "resize.unlock" {
			ptr, ok := out.(*ResizeControlResult)
			if !ok || ptr == nil {
				return methodOutTypeError(method, "*protocol.ResizeControlResult", out)
			}
			*ptr = ResizeControlResult{ResizeControl: resizeControlFromWirePB(msg.GetResizeControl()), Size: sizeFromWirePB(msg.GetSize())}
			return nil
		}
		ptr, ok := out.(*EnsureResizeResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.EnsureResizeResult", out)
		}
		*ptr = EnsureResizeResult{ResizeControl: resizeControlFromWirePB(msg.GetResizeControl()), Size: sizeFromWirePB(msg.GetSize()), Resized: msg.GetResized()}
		return nil
	case "attach":
		var msg wirepb.AttachResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*AttachResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.AttachResult", out)
		}
		*ptr = AttachResult{Mode: msg.GetMode(), Channel: uint16(msg.GetChannel()), ResizeControl: resizeControlFromWirePB(msg.GetResizeControl())}
		return nil
	case "storage.get", "storage.put":
		var msg wirepb.StorageEntry
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*StorageEntry)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.StorageEntry", out)
		}
		*ptr = storageEntryFromWirePB(&msg)
		return nil
	case "storage.delete":
		var msg wirepb.StorageDeleteResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*StorageDeleteResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.StorageDeleteResult", out)
		}
		*ptr = storageDeleteResultFromWirePB(&msg)
		return nil
	case "storage.list":
		var msg wirepb.StorageListResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*StorageListResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.StorageListResult", out)
		}
		*ptr = storageListResultFromWirePB(&msg)
		return nil
	case "path.list_dirs":
		var msg wirepb.PathListDirsResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*PathListDirsResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.PathListDirsResult", out)
		}
		*ptr = pathListDirsResultFromWirePB(&msg)
		return nil
	case "path.defaults":
		var msg wirepb.PathDefaultsResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*PathDefaultsResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.PathDefaultsResult", out)
		}
		*ptr = pathDefaultsResultFromWirePB(&msg)
		return nil
	case "workbench.get":
		var msg wirepb.WorkbenchSnapshot
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*WorkbenchSnapshot)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.WorkbenchSnapshot", out)
		}
		*ptr = workbenchSnapshotFromWirePB(&msg)
		return nil
	case "workbench.apply":
		var msg wirepb.WorkbenchMutateResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*WorkbenchMutateResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.WorkbenchMutateResult", out)
		}
		*ptr = workbenchMutateResultFromWirePB(&msg)
		return nil
	case "history.backlog.status":
		var msg wirepb.GetParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*HistoryBacklogStatus)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.HistoryBacklogStatus", out)
		}
		*ptr = historyBacklogStatusFromWirePB(&msg)
		return nil
	case "remote.status":
		var msg wirepb.RemoteStatus
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*RemoteStatus)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.RemoteStatus", out)
		}
		*ptr = remoteStatusFromWirePB(&msg)
		return nil
	case "remote.pair.start":
		var msg wirepb.RemotePairStartResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*RemotePairStartResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.RemotePairStartResult", out)
		}
		*ptr = remotePairStartResultFromWirePB(&msg)
		return nil
	case "remote.local.enable", "remote.local.status", "remote.local.disable":
		var msg wirepb.RemoteLocalStatus
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*RemoteLocalStatus)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.RemoteLocalStatus", out)
		}
		*ptr = remoteLocalStatusFromWirePB(&msg)
		return nil
	case MethodClientSessionRegister:
		var msg wirepb.ClientSessionRegisterResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*ClientSessionRegisterResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.ClientSessionRegisterResult", out)
		}
		*ptr = clientSessionRegisterResultFromWirePB(&msg)
		return nil
	case MethodClientSessionList:
		var msg wirepb.ClientSessionListResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*ClientSessionListResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.ClientSessionListResult", out)
		}
		*ptr = clientSessionListResultFromWirePB(&msg)
		return nil
	case MethodClientControlCall:
		var msg wirepb.ClientControlCallResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*ClientControlCallResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.ClientControlCallResult", out)
		}
		*ptr = clientControlCallResultFromWirePB(&msg)
		return nil
	case MethodClientControlWatch:
		var msg wirepb.ClientControlWatchResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*ClientControlWatchResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.ClientControlWatchResult", out)
		}
		value, err := clientControlWatchResultFromWirePB(&msg)
		if err != nil {
			return err
		}
		*ptr = value
		return nil
	case MethodClientControlUnwatch:
		var msg wirepb.ClientControlUnwatchResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*ClientControlUnwatchResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.ClientControlUnwatchResult", out)
		}
		value, err := clientControlUnwatchResultFromWirePB(&msg)
		if err != nil {
			return err
		}
		*ptr = value
		return nil
	case MethodClientControlRespond:
		var msg wirepb.ClientControlResponseResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		ptr, ok := out.(*ClientControlResponseResult)
		if !ok || ptr == nil {
			return methodOutTypeError(method, "*protocol.ClientControlResponseResult", out)
		}
		*ptr = clientControlResponseResultFromWirePB(&msg)
		return nil
	default:
		return fmt.Errorf("protocol: no protobuf result codec for method %q", method)
	}
}

func MustEncodeMethodParams(method string, params any) []byte {
	payload, err := EncodeMethodParams(method, params)
	if err != nil {
		panic(err)
	}
	return payload
}

func decodeEmpty(payload []byte) error {
	var msg wirepb.Empty
	return proto.Unmarshal(payload, &msg)
}

func methodParamsTypeError(method, want string, got any) error {
	return fmt.Errorf("protocol: method %q params must be %s, got %T", method, want, got)
}

func methodResultTypeError(method, want string, got any) error {
	return fmt.Errorf("protocol: method %q result must be %s, got %T", method, want, got)
}

func methodOutTypeError(method, want string, got any) error {
	return fmt.Errorf("protocol: method %q decode target must be %s, got %T", method, want, got)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func remoteStatusToWirePB(status RemoteStatus) *wirepb.RemoteStatus {
	return &wirepb.RemoteStatus{
		State:             status.State,
		Detail:            status.Detail,
		DeviceId:          status.DeviceID,
		DeviceName:        status.DeviceName,
		ControlUrl:        status.ControlURL,
		HubUrl:            status.HubURL,
		HubUrls:           append([]string(nil), status.HubURLs...),
		DataDir:           status.DataDir,
		Mode:              status.Mode,
		AllowLan:          status.AllowLAN,
		TerminalCount:     int32(status.TerminalCount),
		UpdatedAtUnixNano: timeToUnixNano(status.UpdatedAt),
	}
}

func remoteStatusFromWirePB(msg *wirepb.RemoteStatus) RemoteStatus {
	if msg == nil {
		return RemoteStatus{}
	}
	return RemoteStatus{
		State:         msg.GetState(),
		Detail:        msg.GetDetail(),
		DeviceID:      msg.GetDeviceId(),
		DeviceName:    msg.GetDeviceName(),
		ControlURL:    msg.GetControlUrl(),
		HubURL:        msg.GetHubUrl(),
		HubURLs:       append([]string(nil), msg.GetHubUrls()...),
		DataDir:       msg.GetDataDir(),
		Mode:          msg.GetMode(),
		AllowLAN:      msg.GetAllowLan(),
		TerminalCount: int(msg.GetTerminalCount()),
		UpdatedAt:     unixNanoToTime(msg.GetUpdatedAtUnixNano()),
	}
}

func remotePairStartParamsToWirePB(params RemotePairStartParams) *wirepb.RemotePairStartParams {
	return &wirepb.RemotePairStartParams{
		LocalPairUrl:   params.LocalPairURL,
		TtlSeconds:     int32(params.TTLSeconds),
		AuthTtlSeconds: int32(params.AuthTTLSeconds),
	}
}

func remotePairStartParamsFromWirePB(msg *wirepb.RemotePairStartParams) RemotePairStartParams {
	if msg == nil {
		return RemotePairStartParams{}
	}
	return RemotePairStartParams{
		LocalPairURL:   msg.GetLocalPairUrl(),
		TTLSeconds:     int(msg.GetTtlSeconds()),
		AuthTTLSeconds: int(msg.GetAuthTtlSeconds()),
	}
}

func remotePairStartResultToWirePB(result RemotePairStartResult) *wirepb.RemotePairStartResult {
	return &wirepb.RemotePairStartResult{
		Type:              result.Type,
		MachineId:         result.MachineID,
		MachineName:       result.MachineName,
		LocalPairUrl:      result.LocalPairURL,
		PairSessionId:     result.PairSessionID,
		PairSecret:        result.PairSecret,
		AnswerProofSecret: result.AnswerProofSecret,
		ExpiresAtUnixNano: timeToUnixNano(result.ExpiresAt),
	}
}

func remotePairStartResultFromWirePB(msg *wirepb.RemotePairStartResult) RemotePairStartResult {
	if msg == nil {
		return RemotePairStartResult{}
	}
	return RemotePairStartResult{
		Type:              msg.GetType(),
		MachineID:         msg.GetMachineId(),
		MachineName:       msg.GetMachineName(),
		LocalPairURL:      msg.GetLocalPairUrl(),
		PairSessionID:     msg.GetPairSessionId(),
		PairSecret:        msg.GetPairSecret(),
		AnswerProofSecret: msg.GetAnswerProofSecret(),
		ExpiresAt:         unixNanoToTime(msg.GetExpiresAtUnixNano()),
	}
}

func remoteLocalEnableParamsToWirePB(params RemoteLocalEnableParams) *wirepb.RemoteLocalEnableParams {
	return &wirepb.RemoteLocalEnableParams{
		LocalWebAddr: params.LocalWebAddr,
		IceTcpAddr:   params.ICETCPAddr,
		HubUrls:      append([]string(nil), params.HubURLs...),
		ControlUrl:   params.ControlURL,
		AccessToken:  params.AccessToken,
		Region:       params.Region,
	}
}

func remoteLocalEnableParamsFromWirePB(msg *wirepb.RemoteLocalEnableParams) RemoteLocalEnableParams {
	if msg == nil {
		return RemoteLocalEnableParams{}
	}
	return RemoteLocalEnableParams{
		LocalWebAddr: msg.GetLocalWebAddr(),
		ICETCPAddr:   msg.GetIceTcpAddr(),
		HubURLs:      append([]string(nil), msg.GetHubUrls()...),
		ControlURL:   msg.GetControlUrl(),
		AccessToken:  msg.GetAccessToken(),
		Region:       msg.GetRegion(),
	}
}

func remoteLocalStatusToWirePB(status RemoteLocalStatus) *wirepb.RemoteLocalStatus {
	return &wirepb.RemoteLocalStatus{
		Enabled:           status.Enabled,
		HttpUrl:           status.HTTPURL,
		LocalWebAddr:      status.LocalWebAddr,
		LocalPairUrl:      status.LocalPairURL,
		IceTcpEnabled:     status.ICETCPEnabled,
		IceTcpAddr:        status.ICETCPAddr,
		IceTcpPort:        int32(status.ICETCPPort),
		UpdatedAtUnixNano: timeToUnixNano(status.UpdatedAt),
	}
}

func remoteLocalStatusFromWirePB(msg *wirepb.RemoteLocalStatus) RemoteLocalStatus {
	if msg == nil {
		return RemoteLocalStatus{}
	}
	return RemoteLocalStatus{
		Enabled:       msg.GetEnabled(),
		HTTPURL:       msg.GetHttpUrl(),
		LocalWebAddr:  msg.GetLocalWebAddr(),
		LocalPairURL:  msg.GetLocalPairUrl(),
		ICETCPEnabled: msg.GetIceTcpEnabled(),
		ICETCPAddr:    msg.GetIceTcpAddr(),
		ICETCPPort:    int(msg.GetIceTcpPort()),
		UpdatedAt:     unixNanoToTime(msg.GetUpdatedAtUnixNano()),
	}
}

func clientControlActionSpecToWirePB(spec ClientControlActionSpec) *wirepb.ClientControlActionSpec {
	return &wirepb.ClientControlActionSpec{
		Id:                   string(spec.ID),
		OwnerPluginId:        string(spec.OwnerPluginID),
		Scope:                string(spec.Scope),
		SupportedClientKinds: clientKindsToStrings(spec.SupportedClientKinds),
		RequiredCaps:         capabilitiesToStrings(spec.RequiredCaps),
		ClientRequiredCaps:   capabilitiesToStrings(spec.ClientRequiredCaps),
		DaemonRequiredCaps:   capabilitiesToStrings(spec.DaemonRequiredCaps),
		Danger:               string(spec.Danger),
		ParamsSchema:         spec.ParamsSchema,
		Idempotent:           spec.Idempotent,
		BroadcastAllowed:     spec.BroadcastAllowed,
	}
}

func clientControlActionSpecFromWirePB(msg *wirepb.ClientControlActionSpec) ClientControlActionSpec {
	if msg == nil {
		return ClientControlActionSpec{}
	}
	return ClientControlActionSpec{
		ID:                   plugin.ActionID(msg.GetId()),
		OwnerPluginID:        plugin.PluginID(msg.GetOwnerPluginId()),
		Scope:                plugin.ActionScope(msg.GetScope()),
		SupportedClientKinds: clientKindsFromStrings(msg.GetSupportedClientKinds()),
		RequiredCaps:         capabilitiesFromStrings(msg.GetRequiredCaps()),
		ClientRequiredCaps:   capabilitiesFromStrings(msg.GetClientRequiredCaps()),
		DaemonRequiredCaps:   capabilitiesFromStrings(msg.GetDaemonRequiredCaps()),
		Danger:               plugin.DangerLevel(msg.GetDanger()),
		ParamsSchema:         msg.GetParamsSchema(),
		Idempotent:           msg.GetIdempotent(),
		BroadcastAllowed:     msg.GetBroadcastAllowed(),
	}
}

func clientControlActionSpecsToWirePB(specs []ClientControlActionSpec) []*wirepb.ClientControlActionSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]*wirepb.ClientControlActionSpec, 0, len(specs))
	for _, spec := range specs {
		out = append(out, clientControlActionSpecToWirePB(spec))
	}
	return out
}

func clientControlActionSpecsFromWirePB(msgs []*wirepb.ClientControlActionSpec) []ClientControlActionSpec {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]ClientControlActionSpec, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, clientControlActionSpecFromWirePB(msg))
	}
	return out
}

func clientSessionRegisterParamsToWirePB(params ClientSessionRegisterParams) *wirepb.ClientSessionRegisterParams {
	return &wirepb.ClientSessionRegisterParams{
		SessionId:    params.SessionID,
		ClientKind:   string(params.ClientKind),
		WorkspaceId:  params.WorkspaceID,
		InstanceId:   params.InstanceID,
		Pid:          int32(params.PID),
		Capabilities: capabilitiesToStrings(params.Capabilities),
		Actions:      clientControlActionSpecsToWirePB(params.Actions),
		Metadata:     cloneStringMap(params.Metadata),
	}
}

func clientSessionRegisterParamsFromWirePB(msg *wirepb.ClientSessionRegisterParams) ClientSessionRegisterParams {
	if msg == nil {
		return ClientSessionRegisterParams{}
	}
	return ClientSessionRegisterParams{
		SessionID:    msg.GetSessionId(),
		ClientKind:   plugin.ClientKind(msg.GetClientKind()),
		WorkspaceID:  msg.GetWorkspaceId(),
		InstanceID:   msg.GetInstanceId(),
		PID:          int(msg.GetPid()),
		Capabilities: capabilitiesFromStrings(msg.GetCapabilities()),
		Actions:      clientControlActionSpecsFromWirePB(msg.GetActions()),
		Metadata:     cloneStringMap(msg.GetMetadata()),
	}
}

func clientSessionRegisterResultToWirePB(result ClientSessionRegisterResult) *wirepb.ClientSessionRegisterResult {
	return &wirepb.ClientSessionRegisterResult{Session: clientSessionInfoToWirePB(result.Session)}
}

func clientSessionRegisterResultFromWirePB(msg *wirepb.ClientSessionRegisterResult) ClientSessionRegisterResult {
	if msg == nil {
		return ClientSessionRegisterResult{}
	}
	return ClientSessionRegisterResult{Session: clientSessionInfoFromWirePB(msg.GetSession())}
}

func clientSessionListParamsToWirePB(params ClientSessionListParams) *wirepb.ClientSessionListParams {
	return &wirepb.ClientSessionListParams{
		ClientKind:     string(params.ClientKind),
		WorkspaceId:    params.WorkspaceID,
		IncludeActions: params.IncludeActions,
	}
}

func clientSessionListParamsFromWirePB(msg *wirepb.ClientSessionListParams) ClientSessionListParams {
	if msg == nil {
		return ClientSessionListParams{}
	}
	return ClientSessionListParams{
		ClientKind:     plugin.ClientKind(msg.GetClientKind()),
		WorkspaceID:    msg.GetWorkspaceId(),
		IncludeActions: msg.GetIncludeActions(),
	}
}

func clientSessionListResultToWirePB(result ClientSessionListResult) *wirepb.ClientSessionListResult {
	return &wirepb.ClientSessionListResult{Sessions: clientSessionInfosToWirePB(result.Sessions)}
}

func clientSessionListResultFromWirePB(msg *wirepb.ClientSessionListResult) ClientSessionListResult {
	if msg == nil {
		return ClientSessionListResult{}
	}
	return ClientSessionListResult{Sessions: clientSessionInfosFromWirePB(msg.GetSessions())}
}

func clientSessionInfoToWirePB(info ClientSessionInfo) *wirepb.ClientSessionInfo {
	return &wirepb.ClientSessionInfo{
		SessionId:           info.SessionID,
		ClientKind:          string(info.ClientKind),
		WorkspaceId:         info.WorkspaceID,
		InstanceId:          info.InstanceID,
		Pid:                 int32(info.PID),
		Capabilities:        capabilitiesToStrings(info.Capabilities),
		Actions:             clientControlActionSpecsToWirePB(info.Actions),
		ConnectedAtUnixNano: timeToUnixNano(info.ConnectedAt),
		LastSeenAtUnixNano:  timeToUnixNano(info.LastSeenAt),
		Metadata:            cloneStringMap(info.Metadata),
	}
}

func clientSessionInfoFromWirePB(msg *wirepb.ClientSessionInfo) ClientSessionInfo {
	if msg == nil {
		return ClientSessionInfo{}
	}
	return ClientSessionInfo{
		SessionID:    msg.GetSessionId(),
		ClientKind:   plugin.ClientKind(msg.GetClientKind()),
		WorkspaceID:  msg.GetWorkspaceId(),
		InstanceID:   msg.GetInstanceId(),
		PID:          int(msg.GetPid()),
		Capabilities: capabilitiesFromStrings(msg.GetCapabilities()),
		Actions:      clientControlActionSpecsFromWirePB(msg.GetActions()),
		ConnectedAt:  unixNanoToTime(msg.GetConnectedAtUnixNano()),
		LastSeenAt:   unixNanoToTime(msg.GetLastSeenAtUnixNano()),
		Metadata:     cloneStringMap(msg.GetMetadata()),
	}
}

func clientSessionInfosToWirePB(infos []ClientSessionInfo) []*wirepb.ClientSessionInfo {
	if len(infos) == 0 {
		return nil
	}
	out := make([]*wirepb.ClientSessionInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, clientSessionInfoToWirePB(info))
	}
	return out
}

func clientSessionInfosFromWirePB(msgs []*wirepb.ClientSessionInfo) []ClientSessionInfo {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]ClientSessionInfo, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, clientSessionInfoFromWirePB(msg))
	}
	return out
}

func clientTerminalRefToWirePB(ref *ClientTerminalRef) *wirepb.ClientTerminalRef {
	if ref == nil {
		return nil
	}
	return &wirepb.ClientTerminalRef{
		EndpointId: string(ref.EndpointID),
		TerminalId: string(ref.TerminalID),
	}
}

func clientTerminalRefFromWirePB(msg *wirepb.ClientTerminalRef) *ClientTerminalRef {
	if msg == nil {
		return nil
	}
	return &ClientTerminalRef{
		EndpointID: plugin.EndpointID(msg.GetEndpointId()),
		TerminalID: plugin.TerminalID(msg.GetTerminalId()),
	}
}

func clientControlTargetToWirePB(target ClientControlTarget) *wirepb.ClientControlTarget {
	return &wirepb.ClientControlTarget{
		SessionId:   target.SessionID,
		ClientKind:  string(target.ClientKind),
		WorkspaceId: target.WorkspaceID,
		Broadcast:   target.Broadcast,
		ActivePanel: target.ActivePanel,
		TerminalRef: clientTerminalRefToWirePB(target.TerminalRef),
	}
}

func clientControlTargetFromWirePB(msg *wirepb.ClientControlTarget) ClientControlTarget {
	if msg == nil {
		return ClientControlTarget{}
	}
	return ClientControlTarget{
		SessionID:   msg.GetSessionId(),
		ClientKind:  plugin.ClientKind(msg.GetClientKind()),
		WorkspaceID: msg.GetWorkspaceId(),
		Broadcast:   msg.GetBroadcast(),
		ActivePanel: msg.GetActivePanel(),
		TerminalRef: clientTerminalRefFromWirePB(msg.GetTerminalRef()),
	}
}

func clientControlWatchParamsToWirePB(params ClientControlWatchParams) *wirepb.ClientControlWatchParams {
	return &wirepb.ClientControlWatchParams{SessionId: params.SessionID}
}

func clientControlWatchParamsFromWirePB(msg *wirepb.ClientControlWatchParams) ClientControlWatchParams {
	if msg == nil {
		return ClientControlWatchParams{}
	}
	return ClientControlWatchParams{SessionID: msg.GetSessionId()}
}

func clientControlWatchResultToWirePB(result ClientControlWatchResult) *wirepb.ClientControlWatchResult {
	return &wirepb.ClientControlWatchResult{Channel: uint32(result.Channel), SessionId: result.SessionID}
}

func clientControlWatchResultFromWirePB(msg *wirepb.ClientControlWatchResult) (ClientControlWatchResult, error) {
	if msg == nil {
		return ClientControlWatchResult{}, nil
	}
	channel, err := clientControlChannelFromWire(msg.GetChannel())
	if err != nil {
		return ClientControlWatchResult{}, err
	}
	return ClientControlWatchResult{SessionID: msg.GetSessionId(), Channel: channel}, nil
}

func clientControlUnwatchParamsToWirePB(params ClientControlUnwatchParams) *wirepb.ClientControlUnwatchParams {
	return &wirepb.ClientControlUnwatchParams{SessionId: params.SessionID, Channel: uint32(params.Channel)}
}

func clientControlUnwatchParamsFromWirePB(msg *wirepb.ClientControlUnwatchParams) (ClientControlUnwatchParams, error) {
	if msg == nil {
		return ClientControlUnwatchParams{}, nil
	}
	channel, err := clientControlChannelFromWire(msg.GetChannel())
	if err != nil {
		return ClientControlUnwatchParams{}, err
	}
	return ClientControlUnwatchParams{SessionID: msg.GetSessionId(), Channel: channel}, nil
}

func clientControlUnwatchResultToWirePB(result ClientControlUnwatchResult) *wirepb.ClientControlUnwatchResult {
	return &wirepb.ClientControlUnwatchResult{SessionId: result.SessionID, Channel: uint32(result.Channel), Stopped: result.Stopped}
}

func clientControlUnwatchResultFromWirePB(msg *wirepb.ClientControlUnwatchResult) (ClientControlUnwatchResult, error) {
	if msg == nil {
		return ClientControlUnwatchResult{}, nil
	}
	channel, err := clientControlChannelFromWire(msg.GetChannel())
	if err != nil {
		return ClientControlUnwatchResult{}, err
	}
	return ClientControlUnwatchResult{SessionID: msg.GetSessionId(), Channel: channel, Stopped: msg.GetStopped()}, nil
}

func clientControlChannelFromWire(channel uint32) (uint16, error) {
	if channel > uint32(^uint16(0)) {
		return 0, fmt.Errorf("client control stream channel %d exceeds uint16", channel)
	}
	return uint16(channel), nil
}

func clientControlSourceToWirePB(source ClientControlSource) *wirepb.ClientControlSource {
	return &wirepb.ClientControlSource{PluginId: string(source.PluginID), Kind: source.Kind}
}

func clientControlSourceFromWirePB(msg *wirepb.ClientControlSource) ClientControlSource {
	if msg == nil {
		return ClientControlSource{}
	}
	return ClientControlSource{PluginID: plugin.PluginID(msg.GetPluginId()), Kind: msg.GetKind()}
}

func clientControlInvocationToWirePB(invocation ClientControlInvocation) *wirepb.ClientControlInvocation {
	return &wirepb.ClientControlInvocation{
		RequestId:        invocation.RequestID,
		ActionId:         string(invocation.ActionID),
		Params:           append([]byte(nil), invocation.Params...),
		Source:           clientControlSourceToWirePB(invocation.Source),
		Target:           clientControlTargetToWirePB(invocation.Target),
		TraceParentId:    invocation.TraceParent.TraceID,
		TraceParentToken: invocation.TraceParent.Token,
		DeadlineUnixNano: timeToUnixNano(invocation.Deadline),
		IdempotencyKey:   invocation.IdempotencyKey,
	}
}

func clientControlInvocationFromWirePB(msg *wirepb.ClientControlInvocation) ClientControlInvocation {
	if msg == nil {
		return ClientControlInvocation{}
	}
	return ClientControlInvocation{
		RequestID:      msg.GetRequestId(),
		ActionID:       plugin.ActionID(msg.GetActionId()),
		Params:         append([]byte(nil), msg.GetParams()...),
		Source:         clientControlSourceFromWirePB(msg.GetSource()),
		Target:         clientControlTargetFromWirePB(msg.GetTarget()),
		TraceParent:    plugin.TraceParent{TraceID: msg.GetTraceParentId(), Token: msg.GetTraceParentToken()},
		Deadline:       unixNanoToTime(msg.GetDeadlineUnixNano()),
		IdempotencyKey: msg.GetIdempotencyKey(),
	}
}

func clientControlCallParamsToWirePB(params ClientControlCallParams) *wirepb.ClientControlCallParams {
	return &wirepb.ClientControlCallParams{
		RequestId:        params.RequestID,
		ActionId:         string(params.ActionID),
		Params:           append([]byte(nil), params.Params...),
		Target:           clientControlTargetToWirePB(params.Target),
		TraceParentId:    params.TraceParent.TraceID,
		TraceParentToken: params.TraceParent.Token,
		DeadlineUnixNano: timeToUnixNano(params.Deadline),
		IdempotencyKey:   params.IdempotencyKey,
	}
}

func clientControlCallParamsFromWirePB(msg *wirepb.ClientControlCallParams) ClientControlCallParams {
	if msg == nil {
		return ClientControlCallParams{}
	}
	return ClientControlCallParams{
		RequestID:      msg.GetRequestId(),
		ActionID:       plugin.ActionID(msg.GetActionId()),
		Params:         append([]byte(nil), msg.GetParams()...),
		Target:         clientControlTargetFromWirePB(msg.GetTarget()),
		TraceParent:    plugin.TraceParent{TraceID: msg.GetTraceParentId(), Token: msg.GetTraceParentToken()},
		Deadline:       unixNanoToTime(msg.GetDeadlineUnixNano()),
		IdempotencyKey: msg.GetIdempotencyKey(),
	}
}

func clientControlCallResultToWirePB(result ClientControlCallResult) *wirepb.ClientControlCallResult {
	return &wirepb.ClientControlCallResult{
		RequestId:  result.RequestID,
		Broadcast:  result.Broadcast,
		Deliveries: clientControlDeliveriesToWirePB(result.Deliveries),
	}
}

func clientControlCallResultFromWirePB(msg *wirepb.ClientControlCallResult) ClientControlCallResult {
	if msg == nil {
		return ClientControlCallResult{}
	}
	return ClientControlCallResult{
		RequestID:  msg.GetRequestId(),
		Broadcast:  msg.GetBroadcast(),
		Deliveries: clientControlDeliveriesFromWirePB(msg.GetDeliveries()),
	}
}

func clientControlDeliveryToWirePB(delivery ClientControlDelivery) *wirepb.ClientControlDelivery {
	return &wirepb.ClientControlDelivery{
		SessionId: delivery.SessionID,
		Status:    string(delivery.Status),
		Error:     clientControlErrorToWirePB(delivery.Error),
	}
}

func clientControlDeliveryFromWirePB(msg *wirepb.ClientControlDelivery) ClientControlDelivery {
	if msg == nil {
		return ClientControlDelivery{}
	}
	return ClientControlDelivery{
		SessionID: msg.GetSessionId(),
		Status:    ClientControlStatus(msg.GetStatus()),
		Error:     clientControlErrorFromWirePB(msg.GetError()),
	}
}

func clientControlDeliveriesToWirePB(deliveries []ClientControlDelivery) []*wirepb.ClientControlDelivery {
	if len(deliveries) == 0 {
		return nil
	}
	out := make([]*wirepb.ClientControlDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		out = append(out, clientControlDeliveryToWirePB(delivery))
	}
	return out
}

func clientControlDeliveriesFromWirePB(msgs []*wirepb.ClientControlDelivery) []ClientControlDelivery {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]ClientControlDelivery, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, clientControlDeliveryFromWirePB(msg))
	}
	return out
}

func clientControlResponseParamsToWirePB(params ClientControlResponseParams) *wirepb.ClientControlResponseParams {
	return &wirepb.ClientControlResponseParams{
		RequestId:        params.RequestID,
		SessionId:        params.SessionID,
		Status:           string(params.Status),
		Result:           append([]byte(nil), params.Result...),
		Error:            clientControlErrorToWirePB(params.Error),
		TraceParentId:    params.TraceParent.TraceID,
		TraceParentToken: params.TraceParent.Token,
	}
}

func clientControlResponseParamsFromWirePB(msg *wirepb.ClientControlResponseParams) ClientControlResponseParams {
	if msg == nil {
		return ClientControlResponseParams{}
	}
	return ClientControlResponseParams{
		RequestID:   msg.GetRequestId(),
		SessionID:   msg.GetSessionId(),
		Status:      ClientControlStatus(msg.GetStatus()),
		Result:      append([]byte(nil), msg.GetResult()...),
		Error:       clientControlErrorFromWirePB(msg.GetError()),
		TraceParent: plugin.TraceParent{TraceID: msg.GetTraceParentId(), Token: msg.GetTraceParentToken()},
	}
}

func clientControlResponseResultToWirePB(result ClientControlResponseResult) *wirepb.ClientControlResponseResult {
	return &wirepb.ClientControlResponseResult{RequestId: result.RequestID, Accepted: result.Accepted}
}

func clientControlResponseResultFromWirePB(msg *wirepb.ClientControlResponseResult) ClientControlResponseResult {
	if msg == nil {
		return ClientControlResponseResult{}
	}
	return ClientControlResponseResult{RequestID: msg.GetRequestId(), Accepted: msg.GetAccepted()}
}

func clientControlErrorToWirePB(err *ClientControlError) *wirepb.ClientControlError {
	if err == nil {
		return nil
	}
	return &wirepb.ClientControlError{Code: err.Code, Message: err.Message, Retryable: err.Retryable}
}

func clientControlErrorFromWirePB(msg *wirepb.ClientControlError) *ClientControlError {
	if msg == nil {
		return nil
	}
	return &ClientControlError{Code: msg.GetCode(), Message: msg.GetMessage(), Retryable: msg.GetRetryable()}
}

func capabilitiesToStrings(caps []plugin.Capability) []string {
	if len(caps) == 0 {
		return nil
	}
	out := make([]string, 0, len(caps))
	for _, cap := range caps {
		out = append(out, string(cap))
	}
	return out
}

func capabilitiesFromStrings(values []string) []plugin.Capability {
	if len(values) == 0 {
		return nil
	}
	out := make([]plugin.Capability, 0, len(values))
	for _, value := range values {
		out = append(out, plugin.Capability(value))
	}
	return out
}

func clientKindsToStrings(kinds []plugin.ClientKind) []string {
	if len(kinds) == 0 {
		return nil
	}
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, string(kind))
	}
	return out
}

func clientKindsFromStrings(values []string) []plugin.ClientKind {
	if len(values) == 0 {
		return nil
	}
	out := make([]plugin.ClientKind, 0, len(values))
	for _, value := range values {
		out = append(out, plugin.ClientKind(value))
	}
	return out
}

func createParamsToWirePB(params CreateParams) *wirepb.CreateParams {
	return &wirepb.CreateParams{
		Command:                 append([]string(nil), params.Command...),
		Id:                      params.ID,
		Name:                    params.Name,
		Tags:                    cloneStringMap(params.Tags),
		Size:                    sizeToWirePB(params.Size),
		Dir:                     params.Dir,
		Env:                     append([]string(nil), params.Env...),
		ScrollbackSize:          int32(params.ScrollbackSize),
		ScrollbackMaxBytes:      params.ScrollbackMaxBytes,
		ScrollbackMaxAgeSeconds: int64(params.ScrollbackMaxAge / time.Second),
	}
}

func createParamsFromWirePB(msg *wirepb.CreateParams) CreateParams {
	if msg == nil {
		return CreateParams{}
	}
	return CreateParams{
		Command:            append([]string(nil), msg.GetCommand()...),
		ID:                 msg.GetId(),
		Name:               msg.GetName(),
		Tags:               cloneStringMap(msg.GetTags()),
		Size:               sizeFromWirePB(msg.GetSize()),
		Dir:                msg.GetDir(),
		Env:                append([]string(nil), msg.GetEnv()...),
		ScrollbackSize:     int(msg.GetScrollbackSize()),
		ScrollbackMaxBytes: msg.GetScrollbackMaxBytes(),
		ScrollbackMaxAge:   time.Duration(msg.GetScrollbackMaxAgeSeconds()) * time.Second,
	}
}

func terminalInfoToWirePB(info TerminalInfo) *wirepb.TerminalInfo {
	msg := &wirepb.TerminalInfo{
		Id:                         info.ID,
		Name:                       info.Name,
		Command:                    append([]string(nil), info.Command...),
		Tags:                       cloneStringMap(info.Tags),
		Size:                       sizeToWirePB(info.Size),
		State:                      info.State,
		Cwd:                        info.CWD,
		LiveCwd:                    info.LiveCWD,
		CreatedAtUnixNano:          timeToUnixNano(info.CreatedAt),
		ExitedAtUnixNano:           timeToUnixNano(info.ExitedAt),
		ResizeOwnership:            resizeOwnershipToWirePB(info.ResizeOwnership),
		ResizeOwnerAttachmentCount: int32(info.ResizeOwnerAttachmentCount),
	}
	if info.ExitCode != nil {
		value := int32(*info.ExitCode)
		msg.ExitCode = &value
	}
	setInt64UnknownField(msg, terminalInfoExitedAtFieldNumber, timeToUnixNano(info.ExitedAt))
	setInt32ProtoFieldOrUnknown(msg, terminalInfoResourcePIDFieldNumber, int32(info.Resources.PID))
	setInt32ProtoFieldOrUnknown(msg, terminalInfoResourceCPUPercentX100FieldNumber, int32(info.Resources.CPUPercentX100))
	setUint64ProtoFieldOrUnknown(msg, terminalInfoResourceMemoryBytesFieldNumber, info.Resources.MemoryBytes)
	setInt64UnknownField(msg, terminalInfoResourceSampledAtFieldNumber, timeToUnixNano(info.Resources.SampledAt))
	return msg
}

func terminalInfoFromWirePB(msg *wirepb.TerminalInfo) TerminalInfo {
	if msg == nil {
		return TerminalInfo{}
	}
	out := TerminalInfo{
		ID:                         msg.GetId(),
		Name:                       msg.GetName(),
		Command:                    append([]string(nil), msg.GetCommand()...),
		Tags:                       cloneStringMap(msg.GetTags()),
		Size:                       sizeFromWirePB(msg.GetSize()),
		State:                      msg.GetState(),
		CWD:                        msg.GetCwd(),
		LiveCWD:                    msg.GetLiveCwd(),
		CreatedAt:                  unixNanoToTime(msg.GetCreatedAtUnixNano()),
		ExitedAt:                   unixNanoToTime(msg.GetExitedAtUnixNano()),
		ResizeOwnership:            resizeOwnershipFromWirePB(msg.GetResizeOwnership()),
		ResizeOwnerAttachmentCount: int(msg.GetResizeOwnerAttachmentCount()),
		Resources: TerminalResourceUsage{
			PID:            int(int32ProtoFieldOrUnknown(msg, terminalInfoResourcePIDFieldNumber)),
			CPUPercentX100: int(int32ProtoFieldOrUnknown(msg, terminalInfoResourceCPUPercentX100FieldNumber)),
			MemoryBytes:    uint64ProtoFieldOrUnknown(msg, terminalInfoResourceMemoryBytesFieldNumber),
			SampledAt:      unixNanoToTime(int64UnknownField(msg, terminalInfoResourceSampledAtFieldNumber)),
		},
	}
	if msg.ExitCode != nil {
		value := int(msg.GetExitCode())
		out.ExitCode = &value
	}
	if out.ExitedAt.IsZero() {
		out.ExitedAt = unixNanoToTime(int64UnknownField(msg, terminalInfoExitedAtFieldNumber))
	}
	return out
}

func listResultToWirePB(result ListResult) *wirepb.ListResult {
	out := &wirepb.ListResult{Terminals: make([]*wirepb.TerminalInfo, 0, len(result.Terminals))}
	for _, item := range result.Terminals {
		out.Terminals = append(out.Terminals, terminalInfoToWirePB(item))
	}
	return out
}

func listResultFromWirePB(msg *wirepb.ListResult) ListResult {
	if msg == nil {
		return ListResult{}
	}
	out := ListResult{Terminals: make([]TerminalInfo, 0, len(msg.GetTerminals()))}
	for _, item := range msg.GetTerminals() {
		out.Terminals = append(out.Terminals, terminalInfoFromWirePB(item))
	}
	return out
}

func resizeOwnershipToWirePB(value *ResizeOwnership) *wirepb.ResizeOwnership {
	if value == nil {
		return nil
	}
	return &wirepb.ResizeOwnership{
		OwnerAttachmentId: value.OwnerAttachmentID,
		OwnerSurfaceId:    value.OwnerSurfaceID,
		OwnerViewId:       value.OwnerViewID,
		OwnerRemoteAddr:   value.OwnerRemoteAddr,
		Size:              sizeToWirePB(value.Size),
		SizeLocked:        value.SizeLocked,
		Epoch:             value.Epoch,
	}
}

func resizeOwnershipFromWirePB(msg *wirepb.ResizeOwnership) *ResizeOwnership {
	if msg == nil {
		return nil
	}
	return &ResizeOwnership{
		OwnerAttachmentID: msg.GetOwnerAttachmentId(),
		OwnerSurfaceID:    msg.GetOwnerSurfaceId(),
		OwnerViewID:       msg.GetOwnerViewId(),
		OwnerRemoteAddr:   msg.GetOwnerRemoteAddr(),
		Size:              sizeFromWirePB(msg.GetSize()),
		SizeLocked:        msg.GetSizeLocked(),
		Epoch:             msg.GetEpoch(),
	}
}

func resizeControlToWirePB(value *ResizeControl) *wirepb.ResizeControl {
	if value == nil {
		return nil
	}
	return &wirepb.ResizeControl{
		CanResize:       value.CanResize,
		Reason:          value.Reason,
		SizeLocked:      value.SizeLocked,
		SurfaceId:       value.SurfaceID,
		OwnerSurfaceId:  value.OwnerSurfaceID,
		OwnerViewId:     value.OwnerViewID,
		ResizeOwnership: resizeOwnershipToWirePB(value.ResizeOwnership),
	}
}

func resizeControlFromWirePB(msg *wirepb.ResizeControl) *ResizeControl {
	if msg == nil {
		return nil
	}
	return &ResizeControl{
		CanResize:       msg.GetCanResize(),
		Reason:          msg.GetReason(),
		SizeLocked:      msg.GetSizeLocked(),
		SurfaceID:       msg.GetSurfaceId(),
		OwnerSurfaceID:  msg.GetOwnerSurfaceId(),
		OwnerViewID:     msg.GetOwnerViewId(),
		ResizeOwnership: resizeOwnershipFromWirePB(msg.GetResizeOwnership()),
	}
}

func eventsParamsToWirePB(params EventsParams) *wirepb.EventsParams {
	out := &wirepb.EventsParams{
		TerminalId:       params.TerminalID,
		Types:            make([]uint32, 0, len(params.Types)),
		StorageAppId:     params.StorageAppID,
		StorageScope:     storageScopeToWirePB(params.StorageScope),
		StorageOwnerId:   params.StorageOwnerID,
		StorageKeyPrefix: params.StorageKeyPrefix,
		WorkbenchId:      params.WorkbenchID,
	}
	for _, typ := range params.Types {
		out.Types = append(out.Types, uint32(typ))
	}
	return out
}

func eventsParamsFromWirePB(msg *wirepb.EventsParams) EventsParams {
	if msg == nil {
		return EventsParams{}
	}
	out := EventsParams{
		TerminalID:       msg.GetTerminalId(),
		Types:            make([]EventType, 0, len(msg.GetTypes())),
		StorageAppID:     msg.GetStorageAppId(),
		StorageScope:     storageScopeFromWirePB(msg.GetStorageScope()),
		StorageOwnerID:   msg.GetStorageOwnerId(),
		StorageKeyPrefix: msg.GetStorageKeyPrefix(),
		WorkbenchID:      msg.GetWorkbenchId(),
	}
	for _, typ := range msg.GetTypes() {
		out.Types = append(out.Types, EventType(typ))
	}
	return out
}

func storageScopeToWirePB(scope StorageScope) wirepb.StorageScope {
	switch scope {
	case StorageScopePublic:
		return wirepb.StorageScope_STORAGE_SCOPE_PUBLIC
	case StorageScopePrivate:
		return wirepb.StorageScope_STORAGE_SCOPE_PRIVATE
	default:
		return wirepb.StorageScope_STORAGE_SCOPE_UNSPECIFIED
	}
}

func storageScopeFromWirePB(scope wirepb.StorageScope) StorageScope {
	switch scope {
	case wirepb.StorageScope_STORAGE_SCOPE_PUBLIC:
		return StorageScopePublic
	case wirepb.StorageScope_STORAGE_SCOPE_PRIVATE:
		return StorageScopePrivate
	default:
		return ""
	}
}

func storageEntryToWirePB(entry StorageEntry) *wirepb.StorageEntry {
	return &wirepb.StorageEntry{
		AppId:             entry.AppID,
		Scope:             storageScopeToWirePB(entry.Scope),
		OwnerId:           entry.OwnerID,
		Key:               entry.Key,
		Value:             append([]byte(nil), entry.Value...),
		Version:           entry.Version,
		UpdatedAtUnixNano: timeToUnixNano(entry.UpdatedAt),
	}
}

func storageEntryFromWirePB(msg *wirepb.StorageEntry) StorageEntry {
	if msg == nil {
		return StorageEntry{}
	}
	return StorageEntry{
		AppID:     msg.GetAppId(),
		Scope:     storageScopeFromWirePB(msg.GetScope()),
		OwnerID:   msg.GetOwnerId(),
		Key:       msg.GetKey(),
		Value:     append([]byte(nil), msg.GetValue()...),
		Version:   msg.GetVersion(),
		UpdatedAt: unixNanoToTime(msg.GetUpdatedAtUnixNano()),
	}
}

func storageGetParamsToWirePB(params StorageGetParams) *wirepb.StorageGetParams {
	return &wirepb.StorageGetParams{AppId: params.AppID, Scope: storageScopeToWirePB(params.Scope), OwnerId: params.OwnerID, Key: params.Key}
}

func storageGetParamsFromWirePB(msg *wirepb.StorageGetParams) StorageGetParams {
	if msg == nil {
		return StorageGetParams{}
	}
	return StorageGetParams{AppID: msg.GetAppId(), Scope: storageScopeFromWirePB(msg.GetScope()), OwnerID: msg.GetOwnerId(), Key: msg.GetKey()}
}

func storagePutParamsToWirePB(params StoragePutParams) *wirepb.StoragePutParams {
	return &wirepb.StoragePutParams{
		AppId:           params.AppID,
		Scope:           storageScopeToWirePB(params.Scope),
		OwnerId:         params.OwnerID,
		Key:             params.Key,
		Value:           append([]byte(nil), params.Value...),
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	}
}

func storagePutParamsFromWirePB(msg *wirepb.StoragePutParams) StoragePutParams {
	if msg == nil {
		return StoragePutParams{}
	}
	return StoragePutParams{
		AppID:           msg.GetAppId(),
		Scope:           storageScopeFromWirePB(msg.GetScope()),
		OwnerID:         msg.GetOwnerId(),
		Key:             msg.GetKey(),
		Value:           append([]byte(nil), msg.GetValue()...),
		CheckVersion:    msg.GetCheckVersion(),
		ExpectedVersion: msg.GetExpectedVersion(),
	}
}

func storageDeleteParamsToWirePB(params StorageDeleteParams) *wirepb.StorageDeleteParams {
	return &wirepb.StorageDeleteParams{
		AppId:           params.AppID,
		Scope:           storageScopeToWirePB(params.Scope),
		OwnerId:         params.OwnerID,
		Key:             params.Key,
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	}
}

func storageDeleteParamsFromWirePB(msg *wirepb.StorageDeleteParams) StorageDeleteParams {
	if msg == nil {
		return StorageDeleteParams{}
	}
	return StorageDeleteParams{
		AppID:           msg.GetAppId(),
		Scope:           storageScopeFromWirePB(msg.GetScope()),
		OwnerID:         msg.GetOwnerId(),
		Key:             msg.GetKey(),
		CheckVersion:    msg.GetCheckVersion(),
		ExpectedVersion: msg.GetExpectedVersion(),
	}
}

func storageDeleteResultToWirePB(result StorageDeleteResult) *wirepb.StorageDeleteResult {
	return &wirepb.StorageDeleteResult{
		AppId:   result.AppID,
		Scope:   storageScopeToWirePB(result.Scope),
		OwnerId: result.OwnerID,
		Key:     result.Key,
		Deleted: result.Deleted,
		Version: result.Version,
	}
}

func storageDeleteResultFromWirePB(msg *wirepb.StorageDeleteResult) StorageDeleteResult {
	if msg == nil {
		return StorageDeleteResult{}
	}
	return StorageDeleteResult{
		AppID:   msg.GetAppId(),
		Scope:   storageScopeFromWirePB(msg.GetScope()),
		OwnerID: msg.GetOwnerId(),
		Key:     msg.GetKey(),
		Deleted: msg.GetDeleted(),
		Version: msg.GetVersion(),
	}
}

func storageListParamsToWirePB(params StorageListParams) *wirepb.StorageListParams {
	return &wirepb.StorageListParams{AppId: params.AppID, Scope: storageScopeToWirePB(params.Scope), OwnerId: params.OwnerID, Prefix: params.Prefix}
}

func storageListParamsFromWirePB(msg *wirepb.StorageListParams) StorageListParams {
	if msg == nil {
		return StorageListParams{}
	}
	return StorageListParams{AppID: msg.GetAppId(), Scope: storageScopeFromWirePB(msg.GetScope()), OwnerID: msg.GetOwnerId(), Prefix: msg.GetPrefix()}
}

func storageListResultToWirePB(result StorageListResult) *wirepb.StorageListResult {
	out := &wirepb.StorageListResult{Entries: make([]*wirepb.StorageEntry, 0, len(result.Entries))}
	for _, entry := range result.Entries {
		out.Entries = append(out.Entries, storageEntryToWirePB(entry))
	}
	return out
}

func storageListResultFromWirePB(msg *wirepb.StorageListResult) StorageListResult {
	if msg == nil {
		return StorageListResult{}
	}
	out := StorageListResult{Entries: make([]StorageEntry, 0, len(msg.GetEntries()))}
	for _, entry := range msg.GetEntries() {
		out.Entries = append(out.Entries, storageEntryFromWirePB(entry))
	}
	return out
}

func pathListDirsParamsToWirePB(params PathListDirsParams) *wirepb.PathListDirsParams {
	return &wirepb.PathListDirsParams{Prefix: params.Prefix, Limit: int32(params.Limit)}
}

func pathListDirsParamsFromWirePB(msg *wirepb.PathListDirsParams) PathListDirsParams {
	if msg == nil {
		return PathListDirsParams{}
	}
	return PathListDirsParams{Prefix: msg.GetPrefix(), Limit: int(msg.GetLimit())}
}

func pathListDirsResultToWirePB(result PathListDirsResult) *wirepb.PathListDirsResult {
	out := &wirepb.PathListDirsResult{
		BasePath:  result.BasePath,
		Missing:   result.Missing,
		Truncated: result.Truncated,
		Entries:   make([]*wirepb.PathDirEntry, 0, len(result.Entries)),
	}
	for _, entry := range result.Entries {
		out.Entries = append(out.Entries, &wirepb.PathDirEntry{Name: entry.Name, Path: entry.Path})
	}
	return out
}

func pathListDirsResultFromWirePB(msg *wirepb.PathListDirsResult) PathListDirsResult {
	if msg == nil {
		return PathListDirsResult{}
	}
	out := PathListDirsResult{
		BasePath:  msg.GetBasePath(),
		Missing:   msg.GetMissing(),
		Truncated: msg.GetTruncated(),
		Entries:   make([]PathDirEntry, 0, len(msg.GetEntries())),
	}
	for _, entry := range msg.GetEntries() {
		out.Entries = append(out.Entries, PathDirEntry{Name: entry.GetName(), Path: entry.GetPath()})
	}
	return out
}

func pathDefaultsResultToWirePB(result PathDefaultsResult) *wirepb.PathDefaultsResult {
	return &wirepb.PathDefaultsResult{
		DefaultCommand: append([]string(nil), result.DefaultCommand...),
		DefaultCwd:     result.DefaultCWD,
	}
}

func pathDefaultsResultFromWirePB(msg *wirepb.PathDefaultsResult) PathDefaultsResult {
	if msg == nil {
		return PathDefaultsResult{}
	}
	return PathDefaultsResult{
		DefaultCommand: append([]string(nil), msg.GetDefaultCommand()...),
		DefaultCWD:     msg.GetDefaultCwd(),
	}
}

func workbenchSnapshotToWirePB(snapshot WorkbenchSnapshot) *wirepb.WorkbenchSnapshot {
	out := &wirepb.WorkbenchSnapshot{
		Version:           snapshot.Version,
		ActiveWorkspaceId: snapshot.ActiveWorkspaceID,
		Workspaces:        make([]*wirepb.WorkbenchWorkspace, 0, len(snapshot.Workspaces)),
	}
	for _, workspace := range snapshot.Workspaces {
		out.Workspaces = append(out.Workspaces, workbenchWorkspaceToWirePB(workspace))
	}
	return out
}

func workbenchSnapshotFromWirePB(msg *wirepb.WorkbenchSnapshot) WorkbenchSnapshot {
	if msg == nil {
		return WorkbenchSnapshot{}
	}
	out := WorkbenchSnapshot{
		Version:           msg.GetVersion(),
		ActiveWorkspaceID: msg.GetActiveWorkspaceId(),
		Workspaces:        make([]WorkbenchWorkspace, 0, len(msg.GetWorkspaces())),
	}
	for _, workspace := range msg.GetWorkspaces() {
		out.Workspaces = append(out.Workspaces, workbenchWorkspaceFromWirePB(workspace))
	}
	return out
}

func workbenchWorkspaceToWirePB(workspace WorkbenchWorkspace) *wirepb.WorkbenchWorkspace {
	out := &wirepb.WorkbenchWorkspace{
		Id:          workspace.ID,
		Name:        workspace.Name,
		ActiveTabId: workspace.ActiveTabID,
		Tabs:        make([]*wirepb.WorkbenchTab, 0, len(workspace.Tabs)),
	}
	for _, tab := range workspace.Tabs {
		out.Tabs = append(out.Tabs, workbenchTabToWirePB(tab))
	}
	return out
}

func workbenchWorkspaceFromWirePB(msg *wirepb.WorkbenchWorkspace) WorkbenchWorkspace {
	if msg == nil {
		return WorkbenchWorkspace{}
	}
	out := WorkbenchWorkspace{
		ID:          msg.GetId(),
		Name:        msg.GetName(),
		ActiveTabID: msg.GetActiveTabId(),
		Tabs:        make([]WorkbenchTab, 0, len(msg.GetTabs())),
	}
	for _, tab := range msg.GetTabs() {
		out.Tabs = append(out.Tabs, workbenchTabFromWirePB(tab))
	}
	return out
}

func workbenchTabToWirePB(tab WorkbenchTab) *wirepb.WorkbenchTab {
	out := &wirepb.WorkbenchTab{
		Id:           tab.ID,
		Title:        tab.Title,
		ActivePaneId: tab.ActivePaneID,
		Panes:        make([]*wirepb.WorkbenchPane, 0, len(tab.Panes)),
		RootSplit:    workbenchSplitNodeToWirePB(tab.RootSplit),
	}
	for _, pane := range tab.Panes {
		out.Panes = append(out.Panes, workbenchPaneToWirePB(pane))
	}
	return out
}

func workbenchTabFromWirePB(msg *wirepb.WorkbenchTab) WorkbenchTab {
	if msg == nil {
		return WorkbenchTab{}
	}
	out := WorkbenchTab{
		ID:           msg.GetId(),
		Title:        msg.GetTitle(),
		ActivePaneID: msg.GetActivePaneId(),
		Panes:        make([]WorkbenchPane, 0, len(msg.GetPanes())),
		RootSplit:    workbenchSplitNodeFromWirePB(msg.GetRootSplit()),
	}
	for _, pane := range msg.GetPanes() {
		out.Panes = append(out.Panes, workbenchPaneFromWirePB(pane))
	}
	return out
}

func workbenchPaneToWirePB(pane WorkbenchPane) *wirepb.WorkbenchPane {
	return &wirepb.WorkbenchPane{
		Id:         pane.ID,
		Title:      pane.Title,
		Kind:       string(pane.Kind),
		TerminalId: pane.TerminalID,
	}
}

func workbenchPaneFromWirePB(msg *wirepb.WorkbenchPane) WorkbenchPane {
	if msg == nil {
		return WorkbenchPane{}
	}
	return WorkbenchPane{
		ID:         msg.GetId(),
		Title:      msg.GetTitle(),
		Kind:       WorkbenchPaneKind(msg.GetKind()),
		TerminalID: msg.GetTerminalId(),
	}
}

func workbenchSplitNodeToWirePB(node WorkbenchSplitNode) *wirepb.WorkbenchSplitNode {
	out := &wirepb.WorkbenchSplitNode{
		PaneId:      node.PaneID,
		Direction:   string(node.Direction),
		Ratio:       node.Ratio,
		BiasCells:   int32(node.BiasCells),
		FixedPaneId: node.FixedPaneID,
		FixedCols:   int32(node.FixedCols),
		FixedRows:   int32(node.FixedRows),
		Children:    make([]*wirepb.WorkbenchSplitNode, 0, len(node.Children)),
	}
	for _, child := range node.Children {
		out.Children = append(out.Children, workbenchSplitNodeToWirePB(child))
	}
	return out
}

func workbenchSplitNodeFromWirePB(msg *wirepb.WorkbenchSplitNode) WorkbenchSplitNode {
	if msg == nil {
		return WorkbenchSplitNode{}
	}
	out := WorkbenchSplitNode{
		PaneID:      msg.GetPaneId(),
		Direction:   WorkbenchSplitDirection(msg.GetDirection()),
		Ratio:       msg.GetRatio(),
		BiasCells:   int(msg.GetBiasCells()),
		FixedPaneID: msg.GetFixedPaneId(),
		FixedCols:   int(msg.GetFixedCols()),
		FixedRows:   int(msg.GetFixedRows()),
		Children:    make([]WorkbenchSplitNode, 0, len(msg.GetChildren())),
	}
	for _, child := range msg.GetChildren() {
		out.Children = append(out.Children, workbenchSplitNodeFromWirePB(child))
	}
	return out
}

func workbenchGetParamsToWirePB(params WorkbenchGetParams) *wirepb.WorkbenchGetParams {
	return &wirepb.WorkbenchGetParams{WorkspaceId: params.WorkspaceID}
}

func workbenchGetParamsFromWirePB(msg *wirepb.WorkbenchGetParams) WorkbenchGetParams {
	if msg == nil {
		return WorkbenchGetParams{}
	}
	return WorkbenchGetParams{WorkspaceID: msg.GetWorkspaceId()}
}

func workbenchMutateParamsToWirePB(params WorkbenchMutateParams) *wirepb.WorkbenchMutateParams {
	return &wirepb.WorkbenchMutateParams{
		Action:          string(params.Action),
		WorkspaceId:     params.WorkspaceID,
		TabId:           params.TabID,
		PaneId:          params.PaneID,
		TargetId:        params.TargetID,
		Name:            params.Name,
		Kind:            string(params.Kind),
		TerminalId:      params.TerminalID,
		SplitDirection:  string(params.SplitDirection),
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	}
}

func workbenchMutateParamsFromWirePB(msg *wirepb.WorkbenchMutateParams) WorkbenchMutateParams {
	if msg == nil {
		return WorkbenchMutateParams{}
	}
	return WorkbenchMutateParams{
		Action:          WorkbenchMutationAction(msg.GetAction()),
		WorkspaceID:     msg.GetWorkspaceId(),
		TabID:           msg.GetTabId(),
		PaneID:          msg.GetPaneId(),
		TargetID:        msg.GetTargetId(),
		Name:            msg.GetName(),
		Kind:            WorkbenchPaneKind(msg.GetKind()),
		TerminalID:      msg.GetTerminalId(),
		SplitDirection:  WorkbenchSplitDirection(msg.GetSplitDirection()),
		CheckVersion:    msg.GetCheckVersion(),
		ExpectedVersion: msg.GetExpectedVersion(),
	}
}

func workbenchMutateResultToWirePB(result WorkbenchMutateResult) *wirepb.WorkbenchMutateResult {
	return &wirepb.WorkbenchMutateResult{
		Snapshot:   workbenchSnapshotToWirePB(result.Snapshot),
		Action:     string(result.Action),
		ResourceId: result.ResourceID,
	}
}

func workbenchMutateResultFromWirePB(msg *wirepb.WorkbenchMutateResult) WorkbenchMutateResult {
	if msg == nil {
		return WorkbenchMutateResult{}
	}
	return WorkbenchMutateResult{
		Snapshot:   workbenchSnapshotFromWirePB(msg.GetSnapshot()),
		Action:     WorkbenchMutationAction(msg.GetAction()),
		ResourceID: msg.GetResourceId(),
	}
}

func eventToWirePB(event Event) *wirepb.Event {
	out := &wirepb.Event{
		Type:              uint32(event.Type),
		TerminalId:        event.TerminalID,
		TimestampUnixNano: timeToUnixNano(event.Timestamp),
	}
	if event.Created != nil {
		out.Created = &wirepb.TerminalCreatedData{Name: event.Created.Name, Command: append([]string(nil), event.Created.Command...), Size: sizeToWirePB(event.Created.Size)}
	}
	if event.StateChanged != nil {
		out.StateChanged = &wirepb.TerminalStateChangedData{OldState: event.StateChanged.OldState, NewState: event.StateChanged.NewState}
		if event.StateChanged.ExitCode != nil {
			value := int32(*event.StateChanged.ExitCode)
			out.StateChanged.ExitCode = &value
		}
		out.StateChanged.ExitedAtUnixNano = timeToUnixNano(event.StateChanged.ExitedAt)
		setInt64UnknownField(out.StateChanged, terminalStateChangedExitedAtFieldNumber, timeToUnixNano(event.StateChanged.ExitedAt))
	}
	if event.Resized != nil {
		out.Resized = &wirepb.TerminalResizedData{OldSize: sizeToWirePB(event.Resized.OldSize), NewSize: sizeToWirePB(event.Resized.NewSize)}
	}
	if event.Removed != nil {
		out.Removed = &wirepb.TerminalRemovedData{Reason: event.Removed.Reason}
	}
	if event.CollaboratorsRevoked != nil {
		out.CollaboratorsRevoked = &wirepb.CollaboratorsRevokedData{}
	}
	if event.ReadError != nil {
		out.ReadError = &wirepb.TerminalReadErrorData{Error: event.ReadError.Error}
	}
	if event.LiveInvalidated != nil {
		setUint64UnknownField(out, eventLiveRevisionFieldNumber, event.LiveInvalidated.Revision)
	}
	if event.Storage != nil {
		out.Storage = &wirepb.StorageChangedData{
			AppId:   event.Storage.AppID,
			Scope:   storageScopeToWirePB(event.Storage.Scope),
			OwnerId: event.Storage.OwnerID,
			Key:     event.Storage.Key,
			Version: event.Storage.Version,
			Op:      event.Storage.Op,
		}
	}
	if event.Workbench != nil {
		out.Workbench = &wirepb.WorkbenchChangedData{
			WorkspaceId: event.Workbench.WorkspaceID,
			Version:     event.Workbench.Version,
			Action:      event.Workbench.Action,
			ResourceId:  event.Workbench.ResourceID,
		}
	}
	return out
}

func eventFromWirePB(msg *wirepb.Event) Event {
	if msg == nil {
		return Event{}
	}
	out := Event{
		Type:       EventType(msg.GetType()),
		TerminalID: msg.GetTerminalId(),
		Timestamp:  unixNanoToTime(msg.GetTimestampUnixNano()),
	}
	if msg.Created != nil {
		out.Created = &TerminalCreatedData{Name: msg.Created.GetName(), Command: append([]string(nil), msg.Created.GetCommand()...), Size: sizeFromWirePB(msg.Created.GetSize())}
	}
	if msg.StateChanged != nil {
		out.StateChanged = &TerminalStateChangedData{OldState: msg.StateChanged.GetOldState(), NewState: msg.StateChanged.GetNewState()}
		if msg.StateChanged.ExitCode != nil {
			value := int(msg.StateChanged.GetExitCode())
			out.StateChanged.ExitCode = &value
		}
		out.StateChanged.ExitedAt = unixNanoToTime(msg.StateChanged.GetExitedAtUnixNano())
		if out.StateChanged.ExitedAt.IsZero() {
			out.StateChanged.ExitedAt = unixNanoToTime(int64UnknownField(msg.StateChanged, terminalStateChangedExitedAtFieldNumber))
		}
	}
	if msg.Resized != nil {
		out.Resized = &TerminalResizedData{OldSize: sizeFromWirePB(msg.Resized.GetOldSize()), NewSize: sizeFromWirePB(msg.Resized.GetNewSize())}
	}
	if msg.Removed != nil {
		out.Removed = &TerminalRemovedData{Reason: msg.Removed.GetReason()}
	}
	if msg.CollaboratorsRevoked != nil {
		out.CollaboratorsRevoked = &CollaboratorsRevokedData{}
	}
	if msg.ReadError != nil {
		out.ReadError = &TerminalReadErrorData{Error: msg.ReadError.GetError()}
	}
	if out.Type == EventTerminalLiveInvalidated {
		out.LiveInvalidated = &LiveScreenInvalidatedData{Revision: uint64UnknownField(msg, eventLiveRevisionFieldNumber)}
	}
	if msg.Storage != nil {
		out.Storage = &StorageChangedData{
			AppID:   msg.Storage.GetAppId(),
			Scope:   storageScopeFromWirePB(msg.Storage.GetScope()),
			OwnerID: msg.Storage.GetOwnerId(),
			Key:     msg.Storage.GetKey(),
			Version: msg.Storage.GetVersion(),
			Op:      msg.Storage.GetOp(),
		}
	}
	if msg.Workbench != nil {
		out.Workbench = &WorkbenchChangedData{
			WorkspaceID: msg.Workbench.GetWorkspaceId(),
			Version:     msg.Workbench.GetVersion(),
			Action:      msg.Workbench.GetAction(),
			ResourceID:  msg.Workbench.GetResourceId(),
		}
	}
	return out
}

func timePtrToUnixNano(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return timeToUnixNano(*value)
}

const (
	terminalInfoExitedAtFieldNumber                 protowire.Number = 13
	terminalInfoResourcePIDFieldNumber              protowire.Number = 14
	terminalInfoResourceCPUPercentX100FieldNumber   protowire.Number = 15
	terminalInfoResourceMemoryBytesFieldNumber      protowire.Number = 16
	terminalInfoResourceSampledAtFieldNumber        protowire.Number = 17
	terminalStateChangedExitedAtFieldNumber         protowire.Number = 4
	eventLiveRevisionFieldNumber                    protowire.Number = 14
	liveInvalidationObservedRevisionFieldNumber     protowire.Number = 17
	nativeScreenLiveRevisionFieldNumber             protowire.Number = 17
	historyWindowModeFieldNumber                    protowire.Number = 12
	historyWindowAfterCursorValidFieldNumber        protowire.Number = 13
	historyWindowAfterLineIDFieldNumber             protowire.Number = 14
	historyWindowAfterRowInLineFieldNumber          protowire.Number = 15
	historyWindowRangeValidFieldNumber              protowire.Number = 16
	historyWindowRangeStartLineIDFieldNumber        protowire.Number = 17
	historyWindowRangeStartColFieldNumber           protowire.Number = 18
	historyWindowRangeEndLineIDFieldNumber          protowire.Number = 19
	historyWindowRangeEndColFieldNumber             protowire.Number = 20
	historyWindowCursorSegmentFieldNumber           protowire.Number = 21
	historyWindowAfterCursorSegmentFieldNumber      protowire.Number = 22
	historyWindowBeforeRowIndexFieldNumber          protowire.Number = 23
	historyWindowAfterRowIndexFieldNumber           protowire.Number = 24
	historyWindowResponseCursorSegmentFieldNumber   protowire.Number = 31
	historyWindowResponseRowSegmentsFieldNumber     protowire.Number = 32
	historyWindowResponseCursorRowIndexFieldNumber  protowire.Number = 33
	historyWindowResponseRowSessionIDsFieldNumber   protowire.Number = 34
	historyWindowResponseRowFrameIDsFieldNumber     protowire.Number = 35
	historyWindowResponseRowFixedGridFieldNumber    protowire.Number = 36
	historyWindowResponseRowScreenColsFieldNumber   protowire.Number = 37
	historyWindowLineSessionIDsFieldNumber          protowire.Number = 38
	historyWindowLineFrameIDsFieldNumber            protowire.Number = 39
	historyWindowLineFixedGridFieldNumber           protowire.Number = 40
	historyWindowLineScreenColsFieldNumber          protowire.Number = 41
	historyWindowResponseRowIndexesFieldNumber      protowire.Number = 42
	historyWindowResponseRowScreenRowsFieldNumber   protowire.Number = 43
	historyWindowResponseRowScreenRowSetFieldNumber protowire.Number = 44
	historyBacklogStatusHistoryEnabledFieldNumber   protowire.Number = 2
	historyBacklogStatusAppliedSeqFieldNumber       protowire.Number = 3
	historyBacklogStatusTargetSeqFieldNumber        protowire.Number = 4
	historyBacklogStatusCatchupPendingFieldNumber   protowire.Number = 5
	historyBacklogStatusPendingTxFieldNumber        protowire.Number = 6
	historyBacklogStatusPendingBytesFieldNumber     protowire.Number = 7
	historyBacklogStatusModeFieldNumber             protowire.Number = 8
	historyBacklogStatusBufferLimitFieldNumber      protowire.Number = 9
	historyBacklogStatusEventsFieldNumber           protowire.Number = 10
	historyBacklogStatusWaitNanosFieldNumber        protowire.Number = 11
	historyBacklogStatusInFlightFieldNumber         protowire.Number = 12
	historyBacklogStatusClosedFieldNumber           protowire.Number = 13
)

func historyBacklogStatusToWirePB(status HistoryBacklogStatus) *wirepb.GetParams {
	msg := &wirepb.GetParams{TerminalId: status.TerminalID}
	// 中文说明：R448 诊断消息暂挂在 GetParams + unknown fields 上，避免在
	// 没有 protoc 的环境手写生成文件；字段只承载只读 backlog 状态，不承载
	// history payload truth。
	setBoolProtoFieldOrUnknown(msg, historyBacklogStatusHistoryEnabledFieldNumber, status.HistoryEnabled)
	setUint64ProtoFieldOrUnknown(msg, historyBacklogStatusAppliedSeqFieldNumber, status.AppliedSeq)
	setUint64ProtoFieldOrUnknown(msg, historyBacklogStatusTargetSeqFieldNumber, status.TargetSeq)
	setBoolProtoFieldOrUnknown(msg, historyBacklogStatusCatchupPendingFieldNumber, status.CatchupPending)
	setInt32ProtoFieldOrUnknown(msg, historyBacklogStatusPendingTxFieldNumber, int32(status.PendingTransactions))
	setUint64ProtoFieldOrUnknown(msg, historyBacklogStatusPendingBytesFieldNumber, uint64(status.PendingBytes))
	setStringProtoFieldOrUnknown(msg, historyBacklogStatusModeFieldNumber, status.BackpressureMode)
	setUint64ProtoFieldOrUnknown(msg, historyBacklogStatusBufferLimitFieldNumber, uint64(status.BufferLimitBytes))
	setUint64ProtoFieldOrUnknown(msg, historyBacklogStatusEventsFieldNumber, status.BackpressureEvents)
	setUint64ProtoFieldOrUnknown(msg, historyBacklogStatusWaitNanosFieldNumber, uint64(status.BackpressureWaitNanos))
	setBoolProtoFieldOrUnknown(msg, historyBacklogStatusInFlightFieldNumber, status.InFlight)
	setBoolProtoFieldOrUnknown(msg, historyBacklogStatusClosedFieldNumber, status.Closed)
	return msg
}

func historyBacklogStatusFromWirePB(msg *wirepb.GetParams) HistoryBacklogStatus {
	if msg == nil {
		return HistoryBacklogStatus{}
	}
	return HistoryBacklogStatus{
		TerminalID:            msg.GetTerminalId(),
		HistoryEnabled:        boolProtoFieldOrUnknown(msg, historyBacklogStatusHistoryEnabledFieldNumber),
		AppliedSeq:            uint64ProtoFieldOrUnknown(msg, historyBacklogStatusAppliedSeqFieldNumber),
		TargetSeq:             uint64ProtoFieldOrUnknown(msg, historyBacklogStatusTargetSeqFieldNumber),
		CatchupPending:        boolProtoFieldOrUnknown(msg, historyBacklogStatusCatchupPendingFieldNumber),
		PendingTransactions:   int(int32ProtoFieldOrUnknown(msg, historyBacklogStatusPendingTxFieldNumber)),
		PendingBytes:          int64(uint64ProtoFieldOrUnknown(msg, historyBacklogStatusPendingBytesFieldNumber)),
		BackpressureMode:      stringProtoFieldOrUnknown(msg, historyBacklogStatusModeFieldNumber),
		BufferLimitBytes:      int64(uint64ProtoFieldOrUnknown(msg, historyBacklogStatusBufferLimitFieldNumber)),
		BackpressureEvents:    uint64ProtoFieldOrUnknown(msg, historyBacklogStatusEventsFieldNumber),
		BackpressureWaitNanos: int64(uint64ProtoFieldOrUnknown(msg, historyBacklogStatusWaitNanosFieldNumber)),
		InFlight:              boolProtoFieldOrUnknown(msg, historyBacklogStatusInFlightFieldNumber),
		Closed:                boolProtoFieldOrUnknown(msg, historyBacklogStatusClosedFieldNumber),
	}
}

func encodeHistoryWindowParamsUnknownFields(msg *wirepb.HistoryWindowParams, params HistoryWindowParams) {
	// 中文说明：terminal.pb.go 当前存在历史生成差异；新增 field 先按正式
	// proto field number 写入 unknown，wire contract 不借用旧字段。
	setStringProtoFieldOrUnknown(msg, historyWindowModeFieldNumber, params.Mode)
	setBoolProtoFieldOrUnknown(msg, historyWindowAfterCursorValidFieldNumber, params.AfterCursorValid)
	setUint64ProtoFieldOrUnknown(msg, historyWindowAfterLineIDFieldNumber, params.AfterLineID)
	setInt32ProtoFieldOrUnknown(msg, historyWindowAfterRowInLineFieldNumber, int32(params.AfterRowInLine))
	setInt32ProtoFieldOrUnknown(msg, historyWindowBeforeRowIndexFieldNumber, int32(params.BeforeRowIndex))
	setInt32ProtoFieldOrUnknown(msg, historyWindowAfterRowIndexFieldNumber, int32(params.AfterRowIndex))
	setBoolProtoFieldOrUnknown(msg, historyWindowRangeValidFieldNumber, params.RangeValid)
	setUint64ProtoFieldOrUnknown(msg, historyWindowRangeStartLineIDFieldNumber, params.RangeStartLineID)
	setInt32ProtoFieldOrUnknown(msg, historyWindowRangeStartColFieldNumber, int32(params.RangeStartCol))
	setUint64ProtoFieldOrUnknown(msg, historyWindowRangeEndLineIDFieldNumber, params.RangeEndLineID)
	setInt32ProtoFieldOrUnknown(msg, historyWindowRangeEndColFieldNumber, int32(params.RangeEndCol))
	setStringProtoFieldOrUnknown(msg, historyWindowCursorSegmentFieldNumber, params.CursorSegment)
	setStringProtoFieldOrUnknown(msg, historyWindowAfterCursorSegmentFieldNumber, params.AfterCursorSegment)
}

func decodeHistoryWindowParamsUnknownFields(msg *wirepb.HistoryWindowParams, params *HistoryWindowParams) {
	if msg == nil || params == nil {
		return
	}
	params.Mode = stringProtoFieldOrUnknown(msg, historyWindowModeFieldNumber)
	params.AfterCursorValid = boolProtoFieldOrUnknown(msg, historyWindowAfterCursorValidFieldNumber)
	params.AfterLineID = uint64ProtoFieldOrUnknown(msg, historyWindowAfterLineIDFieldNumber)
	params.AfterRowInLine = int(int32ProtoFieldOrUnknown(msg, historyWindowAfterRowInLineFieldNumber))
	params.BeforeRowIndex = int(int32ProtoFieldOrUnknown(msg, historyWindowBeforeRowIndexFieldNumber))
	params.AfterRowIndex = int(int32ProtoFieldOrUnknown(msg, historyWindowAfterRowIndexFieldNumber))
	if params.BeforeRowIndex == 0 && params.BeforeRowInLine > 0 {
		// 中文说明：旧客户端曾把 projection absolute offset 放进
		// before_row_in_line；新路径只写 before_row_index。
		params.BeforeRowIndex = params.BeforeRowInLine
	}
	if params.AfterRowIndex == 0 && params.AfterRowInLine > 0 {
		params.AfterRowIndex = params.AfterRowInLine
	}
	params.RangeValid = boolProtoFieldOrUnknown(msg, historyWindowRangeValidFieldNumber)
	params.RangeStartLineID = uint64ProtoFieldOrUnknown(msg, historyWindowRangeStartLineIDFieldNumber)
	params.RangeStartCol = int(int32ProtoFieldOrUnknown(msg, historyWindowRangeStartColFieldNumber))
	params.RangeEndLineID = uint64ProtoFieldOrUnknown(msg, historyWindowRangeEndLineIDFieldNumber)
	params.RangeEndCol = int(int32ProtoFieldOrUnknown(msg, historyWindowRangeEndColFieldNumber))
	params.CursorSegment = stringProtoFieldOrUnknown(msg, historyWindowCursorSegmentFieldNumber)
	params.AfterCursorSegment = stringProtoFieldOrUnknown(msg, historyWindowAfterCursorSegmentFieldNumber)
}

func setStringProtoFieldOrUnknown(msg proto.Message, field protowire.Number, value string) {
	if msg == nil || value == "" {
		return
	}
	if fd := protoFieldDescriptor(msg, field); fd != nil {
		msg.ProtoReflect().Set(fd, protoreflect.ValueOfString(value))
		return
	}
	setStringUnknownField(msg, field, value)
}

func setBoolProtoFieldOrUnknown(msg proto.Message, field protowire.Number, value bool) {
	if msg == nil || !value {
		return
	}
	if fd := protoFieldDescriptor(msg, field); fd != nil {
		msg.ProtoReflect().Set(fd, protoreflect.ValueOfBool(value))
		return
	}
	setBoolUnknownField(msg, field, value)
}

func setUint64ProtoFieldOrUnknown(msg proto.Message, field protowire.Number, value uint64) {
	if msg == nil || value == 0 {
		return
	}
	if fd := protoFieldDescriptor(msg, field); fd != nil {
		msg.ProtoReflect().Set(fd, protoreflect.ValueOfUint64(value))
		return
	}
	setUint64UnknownField(msg, field, value)
}

func setInt32ProtoFieldOrUnknown(msg proto.Message, field protowire.Number, value int32) {
	if msg == nil || value == 0 {
		return
	}
	if fd := protoFieldDescriptor(msg, field); fd != nil {
		msg.ProtoReflect().Set(fd, protoreflect.ValueOfInt32(value))
		return
	}
	setInt32UnknownField(msg, field, value)
}

func stringProtoFieldOrUnknown(msg proto.Message, field protowire.Number) string {
	if fd := protoFieldDescriptor(msg, field); fd != nil {
		if value := msg.ProtoReflect().Get(fd).String(); value != "" {
			return value
		}
	}
	return stringUnknownField(msg, field)
}

func boolProtoFieldOrUnknown(msg proto.Message, field protowire.Number) bool {
	if fd := protoFieldDescriptor(msg, field); fd != nil {
		if value := msg.ProtoReflect().Get(fd).Bool(); value {
			return true
		}
	}
	return boolUnknownField(msg, field)
}

func uint64ProtoFieldOrUnknown(msg proto.Message, field protowire.Number) uint64 {
	if fd := protoFieldDescriptor(msg, field); fd != nil {
		if value := msg.ProtoReflect().Get(fd).Uint(); value != 0 {
			return value
		}
	}
	return uint64UnknownField(msg, field)
}

func int32ProtoFieldOrUnknown(msg proto.Message, field protowire.Number) int32 {
	if fd := protoFieldDescriptor(msg, field); fd != nil {
		if value := msg.ProtoReflect().Get(fd).Int(); value != 0 {
			return int32(value)
		}
	}
	return int32UnknownField(msg, field)
}

func protoFieldDescriptor(msg proto.Message, field protowire.Number) protoreflect.FieldDescriptor {
	if msg == nil {
		return nil
	}
	return msg.ProtoReflect().Descriptor().Fields().ByNumber(protoreflect.FieldNumber(field))
}

func setInt64UnknownField(msg proto.Message, field protowire.Number, value int64) {
	if msg == nil || value == 0 {
		return
	}
	setUint64UnknownField(msg, field, uint64(value))
}

func setUint64UnknownField(msg proto.Message, field protowire.Number, value uint64) {
	if msg == nil || value == 0 {
		return
	}
	appendUnknownField(msg, field, protowire.VarintType, func(out []byte) []byte {
		return protowire.AppendVarint(out, value)
	})
}

func setInt32UnknownField(msg proto.Message, field protowire.Number, value int32) {
	if msg == nil || value == 0 {
		return
	}
	setUint64UnknownField(msg, field, uint64(value))
}

func setBoolUnknownField(msg proto.Message, field protowire.Number, value bool) {
	if !value {
		return
	}
	setUint64UnknownField(msg, field, 1)
}

func setStringUnknownField(msg proto.Message, field protowire.Number, value string) {
	if msg == nil || value == "" {
		return
	}
	appendUnknownField(msg, field, protowire.BytesType, func(out []byte) []byte {
		out = protowire.AppendVarint(out, uint64(len(value)))
		return append(out, value...)
	})
}

func appendUnknownField(msg proto.Message, field protowire.Number, typ protowire.Type, appendValue func([]byte) []byte) {
	unknown := msg.ProtoReflect().GetUnknown()
	unknown = protowire.AppendTag(unknown, field, typ)
	unknown = appendValue(unknown)
	msg.ProtoReflect().SetUnknown(unknown)
}

func int64UnknownField(msg proto.Message, field protowire.Number) int64 {
	return int64(uint64UnknownField(msg, field))
}

func uint64UnknownField(msg proto.Message, field protowire.Number) uint64 {
	if msg == nil {
		return 0
	}
	unknown := msg.ProtoReflect().GetUnknown()
	for len(unknown) > 0 {
		num, typ, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return 0
		}
		unknown = unknown[n:]
		valueStart := unknown
		n = protowire.ConsumeFieldValue(num, typ, unknown)
		if n < 0 {
			return 0
		}
		if num == field && typ == protowire.VarintType {
			value, consumed := protowire.ConsumeVarint(valueStart)
			if consumed >= 0 {
				return value
			}
			return 0
		}
		unknown = unknown[n:]
	}
	return 0
}

func int32UnknownField(msg proto.Message, field protowire.Number) int32 {
	return int32(uint64UnknownField(msg, field))
}

func boolUnknownField(msg proto.Message, field protowire.Number) bool {
	return uint64UnknownField(msg, field) != 0
}

func stringUnknownField(msg proto.Message, field protowire.Number) string {
	if msg == nil {
		return ""
	}
	unknown := msg.ProtoReflect().GetUnknown()
	for len(unknown) > 0 {
		num, typ, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return ""
		}
		unknown = unknown[n:]
		valueStart := unknown
		n = protowire.ConsumeFieldValue(num, typ, unknown)
		if n < 0 {
			return ""
		}
		if num == field && typ == protowire.BytesType {
			value, consumed := protowire.ConsumeBytes(valueStart)
			if consumed >= 0 {
				return string(value)
			}
			return ""
		}
		unknown = unknown[n:]
	}
	return ""
}
