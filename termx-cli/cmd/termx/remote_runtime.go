package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	termx "github.com/lozzow/termx/termx-core"
	termxremote "github.com/lozzow/termx/termx-remote"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	"github.com/lozzow/termx/termx-shared/transport"
)

type remoteRuntimeHost struct {
	core    coreRemoteDaemon
	service *termxremote.Service
}

type remoteRuntimeCore interface {
	Create(context.Context, termx.CreateOptions) (*termx.TerminalInfo, error)
	Get(context.Context, string) (*termx.TerminalInfo, error)
	List(context.Context, ...termx.ListOptions) ([]*termx.TerminalInfo, error)
	SetMetadata(context.Context, string, string, map[string]string) error
	Restart(context.Context, string) error
	Remove(context.Context, string) error
	Events(context.Context, ...termx.EventsOption) <-chan termx.Event
	StorageGet(context.Context, termx.StorageGetRequest) (termx.StorageEntry, error)
	StoragePut(context.Context, termx.StoragePutRequest) (termx.StorageEntry, error)
	StorageDelete(context.Context, termx.StorageDeleteRequest) (termx.StorageDeleteResult, error)
	StorageList(context.Context, termx.StorageListRequest) ([]termx.StorageEntry, error)
	ServeTransport(context.Context, transport.Transport, string) error
	ServeScopedTransport(context.Context, transport.Transport, string, termx.TransportScope) error
}

type coreRemoteDaemon = remoteRuntimeCore

func newRemoteRuntimeHost(core coreRemoteDaemon, cfg remoteprotocol.Config) *remoteRuntimeHost {
	host := &remoteRuntimeHost{core: core}
	host.service = termxremote.NewService(cfg, host)
	return host
}

func (h *remoteRuntimeHost) Start(ctx context.Context) error {
	if h == nil || h.service == nil {
		return nil
	}
	return h.service.Start(ctx)
}

func (h *remoteRuntimeHost) Close(ctx context.Context) error {
	if h == nil || h.service == nil {
		return nil
	}
	return h.service.Close(ctx)
}

func (h *remoteRuntimeHost) TerminalInventoryChanged() {
	if h == nil || h.service == nil {
		return
	}
	h.service.TriggerSync()
}

func (h *remoteRuntimeHost) HandleProtocolMethod(ctx context.Context, method string, params []byte) ([]byte, int, bool, error) {
	if h == nil || h.service == nil || !strings.HasPrefix(method, "remote.") {
		return nil, 0, false, nil
	}
	var (
		result any
		err    error
	)
	switch method {
	case "remote.status":
		status := h.service.Status()
		result = &status
	case "remote.pair.start":
		in, err := decodeRemotePairStartParams(params)
		if err != nil {
			return nil, 400, true, err
		}
		resultValue, callErr := h.service.PairStart(in)
		result, err = &resultValue, callErr
	case "remote.local.enable":
		in, err := decodeRemoteLocalEnableParams(params)
		if err != nil {
			return nil, 400, true, err
		}
		resultValue, callErr := h.service.LocalEnable(ctx, in)
		result, err = &resultValue, callErr
	case "remote.local.status":
		status := h.service.LocalStatus()
		result = &status
	case "remote.local.disable":
		resultValue, callErr := h.service.LocalDisable(ctx)
		result, err = &resultValue, callErr
	default:
		return nil, 404, true, fmt.Errorf("unknown method: %s", method)
	}
	if err != nil {
		return nil, 500, true, err
	}
	data, err := encodeRemoteResult(method, result)
	if err != nil {
		return nil, 500, true, err
	}
	return data, 0, true, nil
}

func (h *remoteRuntimeHost) Create(ctx context.Context, params protocol.CreateParams) (*protocol.CreateResult, error) {
	if h == nil || h.core == nil {
		return nil, fmt.Errorf("core daemon is nil")
	}
	created, err := h.core.Create(ctx, termx.CreateOptions{
		Command:        append([]string(nil), params.Command...),
		ID:             params.ID,
		Name:           params.Name,
		Tags:           copyStringMap(params.Tags),
		Size:           termx.Size{Cols: params.Size.Cols, Rows: params.Size.Rows},
		Dir:            params.Dir,
		Env:            append([]string(nil), params.Env...),
		ScrollbackSize: params.ScrollbackSize,
	})
	if err != nil {
		return nil, err
	}
	return &protocol.CreateResult{TerminalID: created.ID, State: string(created.State)}, nil
}

func (h *remoteRuntimeHost) Get(ctx context.Context, terminalID string) (*protocol.TerminalInfo, error) {
	if h == nil || h.core == nil {
		return nil, fmt.Errorf("core daemon is nil")
	}
	info, err := h.core.Get(ctx, terminalID)
	if err != nil {
		return nil, err
	}
	return protocolInfoFromCore(info), nil
}

func (h *remoteRuntimeHost) List(ctx context.Context) (*protocol.ListResult, error) {
	if h == nil || h.core == nil {
		return nil, fmt.Errorf("core daemon is nil")
	}
	list, err := h.core.List(ctx)
	if err != nil {
		return nil, err
	}
	out := protocol.ListResult{Terminals: make([]protocol.TerminalInfo, 0, len(list))}
	for _, item := range list {
		if converted := protocolInfoFromCore(item); converted != nil {
			out.Terminals = append(out.Terminals, *converted)
		}
	}
	return &out, nil
}

func (h *remoteRuntimeHost) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	if h == nil || h.core == nil {
		return fmt.Errorf("core daemon is nil")
	}
	return h.core.SetMetadata(ctx, terminalID, name, tags)
}

func (h *remoteRuntimeHost) Restart(ctx context.Context, terminalID string) error {
	if h == nil || h.core == nil {
		return fmt.Errorf("core daemon is nil")
	}
	return h.core.Restart(ctx, terminalID)
}

func (h *remoteRuntimeHost) Remove(ctx context.Context, terminalID string) error {
	if h == nil || h.core == nil {
		return fmt.Errorf("core daemon is nil")
	}
	return h.core.Remove(ctx, terminalID)
}

func (h *remoteRuntimeHost) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	if h == nil || h.core == nil {
		return nil, fmt.Errorf("core daemon is nil")
	}
	opts := make([]termx.EventsOption, 0, 3)
	if strings.TrimSpace(params.TerminalID) != "" {
		opts = append(opts, termx.WithTerminalFilter(strings.TrimSpace(params.TerminalID)))
	}
	if len(params.Types) > 0 {
		types := make([]termx.EventType, 0, len(params.Types))
		for _, typ := range params.Types {
			types = append(types, termx.EventType(typ))
		}
		opts = append(opts, termx.WithTypeFilter(types...))
	}
	events := h.core.Events(ctx, opts...)
	out := make(chan protocol.Event, 64)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-events:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				case out <- protocolEventFromCore(evt):
				}
			}
		}
	}()
	return out, nil
}

func (h *remoteRuntimeHost) ServeTransport(ctx context.Context, t transport.Transport, remote string) error {
	if h == nil || h.core == nil {
		return nil
	}
	return h.core.ServeTransport(ctx, t, remote)
}

func (h *remoteRuntimeHost) ServeScopedTransport(ctx context.Context, t transport.Transport, remote string, scope termxremote.TransportScope) error {
	if h == nil || h.core == nil {
		return nil
	}
	return h.core.ServeScopedTransport(ctx, t, remote, termx.TransportScope{
		TerminalID:        scope.TerminalID,
		MachineEventsOnly: scope.MachineEventsOnly,
	})
}

func (h *remoteRuntimeHost) StorageGet(ctx context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error) {
	if h == nil || h.core == nil {
		return nil, fmt.Errorf("core daemon is nil")
	}
	entry, err := h.core.StorageGet(ctx, termx.StorageGetRequest{
		AppID:   params.AppID,
		Scope:   termx.StorageScope(params.Scope),
		OwnerID: params.OwnerID,
		Key:     params.Key,
	})
	if err != nil {
		return nil, err
	}
	return protocolStorageEntryFromCore(entry), nil
}

func (h *remoteRuntimeHost) StoragePut(ctx context.Context, params protocol.StoragePutParams) (*protocol.StorageEntry, error) {
	if h == nil || h.core == nil {
		return nil, fmt.Errorf("core daemon is nil")
	}
	entry, err := h.core.StoragePut(ctx, termx.StoragePutRequest{
		AppID:           params.AppID,
		Scope:           termx.StorageScope(params.Scope),
		OwnerID:         params.OwnerID,
		Key:             params.Key,
		Value:           append([]byte(nil), params.Value...),
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	})
	if err != nil {
		return nil, err
	}
	return protocolStorageEntryFromCore(entry), nil
}

func (h *remoteRuntimeHost) StorageDelete(ctx context.Context, params protocol.StorageDeleteParams) (*protocol.StorageDeleteResult, error) {
	if h == nil || h.core == nil {
		return nil, fmt.Errorf("core daemon is nil")
	}
	result, err := h.core.StorageDelete(ctx, termx.StorageDeleteRequest{
		AppID:           params.AppID,
		Scope:           termx.StorageScope(params.Scope),
		OwnerID:         params.OwnerID,
		Key:             params.Key,
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	})
	if err != nil {
		return nil, err
	}
	return &protocol.StorageDeleteResult{
		AppID:   result.AppID,
		Scope:   protocol.StorageScope(result.Scope),
		OwnerID: result.OwnerID,
		Key:     result.Key,
		Deleted: result.Deleted,
		Version: result.Version,
	}, nil
}

func (h *remoteRuntimeHost) StorageList(ctx context.Context, params protocol.StorageListParams) (*protocol.StorageListResult, error) {
	if h == nil || h.core == nil {
		return nil, fmt.Errorf("core daemon is nil")
	}
	entries, err := h.core.StorageList(ctx, termx.StorageListRequest{
		AppID:   params.AppID,
		Scope:   termx.StorageScope(params.Scope),
		OwnerID: params.OwnerID,
		Prefix:  params.Prefix,
	})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.StorageEntry, 0, len(entries))
	for _, entry := range entries {
		converted := protocolStorageEntryFromCore(entry)
		if converted != nil {
			out = append(out, *converted)
		}
	}
	return &protocol.StorageListResult{Entries: out}, nil
}

func protocolInfoFromCore(info *termx.TerminalInfo) *protocol.TerminalInfo {
	if info == nil {
		return nil
	}
	return &protocol.TerminalInfo{
		ID:        info.ID,
		Name:      info.Name,
		Command:   append([]string(nil), info.Command...),
		Tags:      copyStringMap(info.Tags),
		Size:      protocol.Size{Cols: info.Size.Cols, Rows: info.Size.Rows},
		State:     string(info.State),
		CWD:       info.CWD,
		LiveCWD:   info.LiveCWD,
		CreatedAt: info.CreatedAt,
		ExitCode:  copyIntPtr(info.ExitCode),
	}
}

func protocolStorageEntryFromCore(entry termx.StorageEntry) *protocol.StorageEntry {
	return &protocol.StorageEntry{
		AppID:     entry.AppID,
		Scope:     protocol.StorageScope(entry.Scope),
		OwnerID:   entry.OwnerID,
		Key:       entry.Key,
		Value:     append([]byte(nil), entry.Value...),
		Version:   entry.Version,
		UpdatedAt: entry.UpdatedAt,
	}
}

func protocolEventFromCore(evt termx.Event) protocol.Event {
	out := protocol.Event{
		Type:       protocol.EventType(evt.Type),
		TerminalID: evt.TerminalID,
		Timestamp:  evt.Timestamp,
	}
	if evt.Created != nil {
		out.Created = &protocol.TerminalCreatedData{
			Name:    evt.Created.Name,
			Command: append([]string(nil), evt.Created.Command...),
			Size:    protocol.Size{Cols: evt.Created.Size.Cols, Rows: evt.Created.Size.Rows},
		}
	}
	if evt.StateChanged != nil {
		out.StateChanged = &protocol.TerminalStateChangedData{
			OldState: string(evt.StateChanged.OldState),
			NewState: string(evt.StateChanged.NewState),
			ExitCode: copyIntPtr(evt.StateChanged.ExitCode),
		}
	}
	if evt.Resized != nil {
		out.Resized = &protocol.TerminalResizedData{
			OldSize: protocol.Size{Cols: evt.Resized.OldSize.Cols, Rows: evt.Resized.OldSize.Rows},
			NewSize: protocol.Size{Cols: evt.Resized.NewSize.Cols, Rows: evt.Resized.NewSize.Rows},
		}
	}
	if evt.Removed != nil {
		out.Removed = &protocol.TerminalRemovedData{Reason: evt.Removed.Reason}
	}
	if evt.CollaboratorsRevoked != nil {
		out.CollaboratorsRevoked = &protocol.CollaboratorsRevokedData{}
	}
	if evt.ReadError != nil {
		out.ReadError = &protocol.TerminalReadErrorData{Error: evt.ReadError.Error}
	}
	if evt.Storage != nil {
		out.Storage = &protocol.StorageChangedData{
			AppID:   evt.Storage.AppID,
			Scope:   protocol.StorageScope(evt.Storage.Scope),
			OwnerID: evt.Storage.OwnerID,
			Key:     evt.Storage.Key,
			Version: evt.Storage.Version,
			Op:      evt.Storage.Op,
		}
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
