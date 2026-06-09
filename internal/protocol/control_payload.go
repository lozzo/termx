package protocol

import (
	"fmt"
	"reflect"
	"time"

	"github.com/lozzow/termx/termx-proto/wirepb"
	"google.golang.org/protobuf/proto"
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
	case "list", "remote.status", "remote.local.status", "remote.local.disable":
		return proto.Marshal(&wirepb.Empty{})
	case "get", "kill", "restart", "remove":
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
	case "ensure_resize":
		value, ok := params.(EnsureResizeParams)
		if !ok {
			if ptr, ptrOK := params.(*EnsureResizeParams); ptrOK && ptr != nil {
				value = *ptr
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
		return proto.Marshal(&wirepb.GetParams{TerminalId: value.TerminalID})
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
	case "snapshot":
		value, ok := params.(SnapshotParams)
		if !ok {
			if ptr, ptrOK := params.(*SnapshotParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.SnapshotParams", params)
		}
		return proto.Marshal(&wirepb.SnapshotParams{
			TerminalId:       value.TerminalID,
			ScrollbackOffset: int32(value.ScrollbackOffset),
			ScrollbackLimit:  int32(value.ScrollbackLimit),
		})
	case "grid.viewport":
		value, ok := params.(GridViewportParams)
		if !ok {
			if ptr, ptrOK := params.(*GridViewportParams); ptrOK && ptr != nil {
				value = *ptr
				ok = true
			}
		}
		if !ok {
			return nil, methodParamsTypeError(method, "protocol.GridViewportParams", params)
		}
		return proto.Marshal(&wirepb.GridViewportParams{
			TerminalId:       value.TerminalID,
			ScrollbackOffset: int32(value.ScrollbackOffset),
			ScrollbackLimit:  int32(value.ScrollbackLimit),
			Cols:             int32(value.Cols),
		})
	case "history.window":
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
		return proto.Marshal(&wirepb.HistoryWindowParams{
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
		})
	case "remote.pair.start":
		switch value := params.(type) {
		case interface {
			GetLocalPairURL() string
			GetTTLSeconds() int
			GetAuthTTLSeconds() int
		}:
			return proto.Marshal(&wirepb.RemotePairStartParams{
				LocalPairUrl:   value.GetLocalPairURL(),
				TtlSeconds:     int32(value.GetTTLSeconds()),
				AuthTtlSeconds: int32(value.GetAuthTTLSeconds()),
			})
		case interface {
			GetLocalPairURL() string
			GetTTLSeconds() int
		}:
			return proto.Marshal(&wirepb.RemotePairStartParams{
				LocalPairUrl: value.GetLocalPairURL(),
				TtlSeconds:   int32(value.GetTTLSeconds()),
			})
		default:
			localPairURL, ttlSeconds, authTTLSeconds, ok := remotePairStartFields(params)
			if !ok {
				return nil, methodParamsTypeError(method, "remote pair start params", params)
			}
			return proto.Marshal(&wirepb.RemotePairStartParams{
				LocalPairUrl:   localPairURL,
				TtlSeconds:     int32(ttlSeconds),
				AuthTtlSeconds: int32(authTTLSeconds),
			})
		}
	case "remote.local.enable":
		localWebAddr, iceTCPAddr, hubURLs, controlURL, accessToken, region, ok := remoteLocalEnableFields(params)
		if !ok {
			return nil, methodParamsTypeError(method, "remote local enable params", params)
		}
		return proto.Marshal(&wirepb.RemoteLocalEnableParams{
			LocalWebAddr: localWebAddr,
			IceTcpAddr:   iceTCPAddr,
			HubUrls:      hubURLs,
			ControlUrl:   controlURL,
			AccessToken:  accessToken,
			Region:       region,
		})
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
	case "list", "remote.status", "remote.local.status", "remote.local.disable":
		return struct{}{}, decodeEmpty(payload)
	case "get", "kill", "restart", "remove":
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
	case "ensure_resize":
		var msg wirepb.EnsureResizeParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
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
		var msg wirepb.GetParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return DetachParams{TerminalID: msg.GetTerminalId()}, nil
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
	case "snapshot":
		var msg wirepb.SnapshotParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return SnapshotParams{TerminalID: msg.GetTerminalId(), ScrollbackOffset: int(msg.GetScrollbackOffset()), ScrollbackLimit: int(msg.GetScrollbackLimit())}, nil
	case "grid.viewport":
		var msg wirepb.GridViewportParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return GridViewportParams{TerminalID: msg.GetTerminalId(), ScrollbackOffset: int(msg.GetScrollbackOffset()), ScrollbackLimit: int(msg.GetScrollbackLimit()), Cols: int(msg.GetCols())}, nil
	case "history.window":
		var msg wirepb.HistoryWindowParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return HistoryWindowParams{
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
		}, nil
	case "remote.pair.start":
		var msg wirepb.RemotePairStartParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return &msg, nil
	case "remote.local.enable":
		var msg wirepb.RemoteLocalEnableParams
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return &msg, nil
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
	case "ensure_resize":
		value, ok := result.(EnsureResizeResult)
		if !ok {
			if ptr, ptrOK := result.(*EnsureResizeResult); ptrOK && ptr != nil {
				value = *ptr
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
	case "remote.status":
		msg, ok := result.(*wirepb.RemoteStatus)
		if !ok || msg == nil {
			return nil, methodResultTypeError(method, "*wirepb.RemoteStatus", result)
		}
		return proto.Marshal(msg)
	case "remote.pair.start":
		msg, ok := result.(*wirepb.RemotePairStartResult)
		if !ok || msg == nil {
			return nil, methodResultTypeError(method, "*wirepb.RemotePairStartResult", result)
		}
		return proto.Marshal(msg)
	case "remote.local.enable", "remote.local.status", "remote.local.disable":
		msg, ok := result.(*wirepb.RemoteLocalStatus)
		if !ok || msg == nil {
			return nil, methodResultTypeError(method, "*wirepb.RemoteLocalStatus", result)
		}
		return proto.Marshal(msg)
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
	case "ensure_resize":
		var msg wirepb.EnsureResizeResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
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
	case "remote.status":
		var msg wirepb.RemoteStatus
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		return assignRemoteResult(out, &msg)
	case "remote.pair.start":
		var msg wirepb.RemotePairStartResult
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		return assignRemoteResult(out, &msg)
	case "remote.local.enable", "remote.local.status", "remote.local.disable":
		var msg wirepb.RemoteLocalStatus
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return err
		}
		return assignRemoteResult(out, &msg)
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

func remotePairStartFields(params any) (string, int, int, bool) {
	value := reflect.Indirect(reflect.ValueOf(params))
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return "", 0, 0, false
	}
	localPairURL := stringField(value, "LocalPairURL")
	ttlSeconds := intField(value, "TTLSeconds")
	authTTLSeconds := intField(value, "AuthTTLSeconds")
	return localPairURL, ttlSeconds, authTTLSeconds, true
}

func remoteLocalEnableFields(params any) (string, string, []string, string, string, string, bool) {
	value := reflect.Indirect(reflect.ValueOf(params))
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return "", "", nil, "", "", "", false
	}
	return stringField(value, "LocalWebAddr"),
		stringField(value, "ICETCPAddr"),
		stringSliceField(value, "HubURLs"),
		stringField(value, "ControlURL"),
		stringField(value, "AccessToken"),
		stringField(value, "Region"),
		true
}

func assignRemoteResult(out any, msg proto.Message) error {
	target := reflect.ValueOf(out)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("protocol: remote decode target must be non-nil pointer, got %T", out)
	}
	switch source := msg.(type) {
	case *wirepb.RemoteStatus:
		elem := target.Elem()
		if elem.Kind() != reflect.Struct {
			return fmt.Errorf("protocol: remote.status decode target must point to struct, got %T", out)
		}
		setStringField(elem, "State", source.GetState())
		setStringField(elem, "Detail", source.GetDetail())
		setStringField(elem, "DeviceID", source.GetDeviceId())
		setStringField(elem, "DeviceName", source.GetDeviceName())
		setStringField(elem, "ControlURL", source.GetControlUrl())
		setStringField(elem, "HubURL", source.GetHubUrl())
		setStringSliceField(elem, "HubURLs", source.GetHubUrls())
		setStringField(elem, "DataDir", source.GetDataDir())
		setStringField(elem, "Mode", source.GetMode())
		setBoolField(elem, "AllowLAN", source.GetAllowLan())
		setIntField(elem, "TerminalCount", int(source.GetTerminalCount()))
		setTimeField(elem, "UpdatedAt", unixNanoToTime(source.GetUpdatedAtUnixNano()))
		return nil
	case *wirepb.RemotePairStartResult:
		elem := target.Elem()
		if elem.Kind() != reflect.Struct {
			return fmt.Errorf("protocol: remote.pair.start decode target must point to struct, got %T", out)
		}
		setStringField(elem, "Type", source.GetType())
		setStringField(elem, "MachineID", source.GetMachineId())
		setStringField(elem, "MachineName", source.GetMachineName())
		setStringField(elem, "LocalPairURL", source.GetLocalPairUrl())
		setStringField(elem, "PairSessionID", source.GetPairSessionId())
		setStringField(elem, "PairSecret", source.GetPairSecret())
		setStringField(elem, "AnswerProofSecret", source.GetAnswerProofSecret())
		setTimeField(elem, "ExpiresAt", unixNanoToTime(source.GetExpiresAtUnixNano()))
		return nil
	case *wirepb.RemoteLocalStatus:
		elem := target.Elem()
		if elem.Kind() != reflect.Struct {
			return fmt.Errorf("protocol: remote.local status decode target must point to struct, got %T", out)
		}
		setBoolField(elem, "Enabled", source.GetEnabled())
		setStringField(elem, "HTTPURL", source.GetHttpUrl())
		setStringField(elem, "LocalWebAddr", source.GetLocalWebAddr())
		setStringField(elem, "LocalPairURL", source.GetLocalPairUrl())
		setBoolField(elem, "ICETCPEnabled", source.GetIceTcpEnabled())
		setStringField(elem, "ICETCPAddr", source.GetIceTcpAddr())
		setIntField(elem, "ICETCPPort", int(source.GetIceTcpPort()))
		setTimeField(elem, "UpdatedAt", unixNanoToTime(source.GetUpdatedAtUnixNano()))
		return nil
	default:
		return fmt.Errorf("protocol: unsupported remote result %T", msg)
	}
}

func stringField(value reflect.Value, name string) string {
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

func intField(value reflect.Value, name string) int {
	field := value.FieldByName(name)
	if !field.IsValid() {
		return 0
	}
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(field.Int())
	default:
		return 0
	}
}

func stringSliceField(value reflect.Value, name string) []string {
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.Slice || field.Type().Elem().Kind() != reflect.String {
		return nil
	}
	out := make([]string, field.Len())
	for i := 0; i < field.Len(); i++ {
		out[i] = field.Index(i).String()
	}
	return out
}

func setStringField(value reflect.Value, name string, text string) {
	field := value.FieldByName(name)
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.String {
		field.SetString(text)
	}
}

func setStringSliceField(value reflect.Value, name string, values []string) {
	field := value.FieldByName(name)
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.String {
		field.Set(reflect.ValueOf(append([]string(nil), values...)))
	}
}

func setBoolField(value reflect.Value, name string, flag bool) {
	field := value.FieldByName(name)
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.Bool {
		field.SetBool(flag)
	}
}

func setIntField(value reflect.Value, name string, number int) {
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanSet() {
		return
	}
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		field.SetInt(int64(number))
	}
}

func setTimeField(value reflect.Value, name string, ts time.Time) {
	field := value.FieldByName(name)
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(ts))
	}
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
		ResizeOwnership:            resizeOwnershipToWirePB(info.ResizeOwnership),
		ResizeOwnerAttachmentCount: int32(info.ResizeOwnerAttachmentCount),
	}
	if info.ExitCode != nil {
		value := int32(*info.ExitCode)
		msg.ExitCode = &value
	}
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
		ResizeOwnership:            resizeOwnershipFromWirePB(msg.GetResizeOwnership()),
		ResizeOwnerAttachmentCount: int(msg.GetResizeOwnerAttachmentCount()),
	}
	if msg.ExitCode != nil {
		value := int(msg.GetExitCode())
		out.ExitCode = &value
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
