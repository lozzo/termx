package main

import (
	"context"
	"fmt"
	"strings"

	coreprotocol "github.com/lozzow/termx/internal/protocol"
	corev2 "github.com/lozzow/termx/termx-core-v2"
	remote "github.com/lozzow/termx/termx-remote"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	"github.com/lozzow/termx/termx-shared/transport"
)

type coreV2RemoteDaemonAdapter struct {
	server *corev2.Server
}

func newCoreV2RemoteDaemonAdapter(server *corev2.Server) *coreV2RemoteDaemonAdapter {
	return &coreV2RemoteDaemonAdapter{server: server}
}

func (adapter *coreV2RemoteDaemonAdapter) requireServer() (*corev2.Server, error) {
	if adapter == nil || adapter.server == nil {
		return nil, fmt.Errorf("core-v2 remote daemon adapter has no server")
	}
	return adapter.server, nil
}

func (adapter *coreV2RemoteDaemonAdapter) Create(_ context.Context, params coreprotocol.CreateParams) (*coreprotocol.CreateResult, error) {
	server, err := adapter.requireServer()
	if err != nil {
		return nil, err
	}
	terminalID := strings.TrimSpace(params.ID)
	if terminalID == "" {
		terminalID = strings.TrimSpace(params.Name)
	}
	if terminalID == "" {
		terminalID = newV3TerminalID()
	}
	info, err := server.RegisterTerminal(corev2.TerminalRecord{
		ID:      terminalID,
		Name:    params.Name,
		Command: append([]string(nil), params.Command...),
		Tags:    cloneRemoteAdapterStringMap(params.Tags),
		Size:    corev2.SizeFromProtocol(params.Size),
		Options: corev2.TerminalCreateOptions{
			Dir:                params.Dir,
			Env:                append([]string(nil), params.Env...),
			ScrollbackSize:     params.ScrollbackSize,
			ScrollbackMaxBytes: params.ScrollbackMaxBytes,
			ScrollbackMaxAge:   params.ScrollbackMaxAge,
		},
	})
	if err != nil {
		return nil, err
	}
	return &coreprotocol.CreateResult{TerminalID: info.ID, State: string(info.State)}, nil
}

func (adapter *coreV2RemoteDaemonAdapter) Get(_ context.Context, terminalID string) (*coreprotocol.TerminalInfo, error) {
	server, err := adapter.requireServer()
	if err != nil {
		return nil, err
	}
	info, err := server.GetTerminal(terminalID)
	if err != nil {
		return nil, err
	}
	out := server.ProtocolTerminalInfo(info)
	return &out, nil
}

func (adapter *coreV2RemoteDaemonAdapter) List(context.Context) (*coreprotocol.ListResult, error) {
	server, err := adapter.requireServer()
	if err != nil {
		return nil, err
	}
	items := server.ListTerminals()
	out := coreprotocol.ListResult{Terminals: make([]coreprotocol.TerminalInfo, 0, len(items))}
	for _, item := range items {
		out.Terminals = append(out.Terminals, server.ProtocolTerminalInfo(item))
	}
	return &out, nil
}

func (adapter *coreV2RemoteDaemonAdapter) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	server, err := adapter.requireServer()
	if err != nil {
		return err
	}
	_, err = server.SetMetadata(ctx, terminalID, name, cloneRemoteAdapterStringMap(tags))
	return err
}

func (adapter *coreV2RemoteDaemonAdapter) Restart(ctx context.Context, terminalID string) error {
	server, err := adapter.requireServer()
	if err != nil {
		return err
	}
	return server.RestartTerminal(ctx, terminalID)
}

func (adapter *coreV2RemoteDaemonAdapter) Remove(_ context.Context, terminalID string) error {
	server, err := adapter.requireServer()
	if err != nil {
		return err
	}
	return server.RemoveTerminal(terminalID)
}

func (adapter *coreV2RemoteDaemonAdapter) Events(ctx context.Context, params coreprotocol.EventsParams) (<-chan coreprotocol.Event, error) {
	server, err := adapter.requireServer()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	events := server.Events(ctx, corev2.EventFilterFromProtocol(params))
	out := make(chan coreprotocol.Event, 64)
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
				select {
				case <-ctx.Done():
					return
				case out <- corev2.ProtocolEventFromCoreV2(event):
				}
			}
		}
	}()
	return out, nil
}

func (adapter *coreV2RemoteDaemonAdapter) ServeTransport(ctx context.Context, t transport.Transport, _ string) error {
	server, err := adapter.requireServer()
	if err != nil {
		return err
	}
	return server.ServeTransport(ctx, t)
}

func (adapter *coreV2RemoteDaemonAdapter) ServeScopedTransport(ctx context.Context, t transport.Transport, _ string, scope remote.TransportScope) error {
	server, err := adapter.requireServer()
	if err != nil {
		return err
	}
	return server.ServeScopedTransport(ctx, t, corev2.TransportScope{
		TerminalID:        scope.TerminalID,
		MachineEventsOnly: scope.MachineEventsOnly,
	})
}

func (adapter *coreV2RemoteDaemonAdapter) StorageGet(ctx context.Context, params coreprotocol.StorageGetParams) (*coreprotocol.StorageEntry, error) {
	server, err := adapter.requireServer()
	if err != nil {
		return nil, err
	}
	entry, err := server.StorageGet(ctx, params.AppID, corev2.StorageScopeFromProtocol(params.Scope), params.OwnerID, params.Key)
	if err != nil {
		return nil, err
	}
	out := corev2.ProtocolStorageEntryFromCore(entry)
	return &out, nil
}

func (adapter *coreV2RemoteDaemonAdapter) StoragePut(ctx context.Context, params coreprotocol.StoragePutParams) (*coreprotocol.StorageEntry, error) {
	server, err := adapter.requireServer()
	if err != nil {
		return nil, err
	}
	entry, err := server.StoragePut(ctx, corev2.StoragePutRequest{
		AppID:           params.AppID,
		Scope:           corev2.StorageScopeFromProtocol(params.Scope),
		OwnerID:         params.OwnerID,
		Key:             params.Key,
		Value:           append([]byte(nil), params.Value...),
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	})
	if err != nil {
		return nil, err
	}
	out := corev2.ProtocolStorageEntryFromCore(entry)
	return &out, nil
}

func (adapter *coreV2RemoteDaemonAdapter) StorageDelete(ctx context.Context, params coreprotocol.StorageDeleteParams) (*coreprotocol.StorageDeleteResult, error) {
	server, err := adapter.requireServer()
	if err != nil {
		return nil, err
	}
	result, err := server.StorageDelete(ctx, corev2.StorageDeleteRequest{
		AppID:           params.AppID,
		Scope:           corev2.StorageScopeFromProtocol(params.Scope),
		OwnerID:         params.OwnerID,
		Key:             params.Key,
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	})
	if err != nil {
		return nil, err
	}
	return &coreprotocol.StorageDeleteResult{
		AppID:   result.AppID,
		Scope:   corev2.ProtocolStorageScopeFromCore(result.Scope),
		OwnerID: result.OwnerID,
		Key:     result.Key,
		Deleted: result.Deleted,
		Version: result.Version,
	}, nil
}

func (adapter *coreV2RemoteDaemonAdapter) StorageList(ctx context.Context, params coreprotocol.StorageListParams) (*coreprotocol.StorageListResult, error) {
	server, err := adapter.requireServer()
	if err != nil {
		return nil, err
	}
	entries := server.StorageList(ctx, params.AppID, corev2.StorageScopeFromProtocol(params.Scope), params.OwnerID, params.Prefix)
	out := coreprotocol.StorageListResult{Entries: make([]coreprotocol.StorageEntry, 0, len(entries))}
	for _, entry := range entries {
		out.Entries = append(out.Entries, corev2.ProtocolStorageEntryFromCore(entry))
	}
	return &out, nil
}

type coreV2RemoteServiceHook struct {
	service *remote.Service
}

func newCoreV2RemoteServiceHook(service *remote.Service) corev2.RemoteService {
	return coreV2RemoteServiceHook{service: service}
}

func (hook coreV2RemoteServiceHook) requireService() (*remote.Service, error) {
	if hook.service == nil {
		return nil, fmt.Errorf("remote service is nil")
	}
	return hook.service, nil
}

func (hook coreV2RemoteServiceHook) Status(context.Context) (coreprotocol.RemoteStatus, error) {
	service, err := hook.requireService()
	if err != nil {
		return coreprotocol.RemoteStatus{}, err
	}
	return coreRemoteStatusFromRemote(service.Status()), nil
}

func (hook coreV2RemoteServiceHook) PairStart(_ context.Context, params coreprotocol.RemotePairStartParams) (coreprotocol.RemotePairStartResult, error) {
	service, err := hook.requireService()
	if err != nil {
		return coreprotocol.RemotePairStartResult{}, err
	}
	result, err := service.PairStart(remotePairStartParamsFromCore(params))
	if err != nil {
		return coreprotocol.RemotePairStartResult{}, err
	}
	return corePairStartResultFromRemote(result), nil
}

func (hook coreV2RemoteServiceHook) LocalEnable(ctx context.Context, params coreprotocol.RemoteLocalEnableParams) (coreprotocol.RemoteLocalStatus, error) {
	service, err := hook.requireService()
	if err != nil {
		return coreprotocol.RemoteLocalStatus{}, err
	}
	status, err := service.LocalEnable(ctx, remoteLocalEnableParamsFromCore(params))
	if err != nil {
		return coreprotocol.RemoteLocalStatus{}, err
	}
	return coreLocalStatusFromRemote(status), nil
}

func (hook coreV2RemoteServiceHook) LocalStatus(context.Context) (coreprotocol.RemoteLocalStatus, error) {
	service, err := hook.requireService()
	if err != nil {
		return coreprotocol.RemoteLocalStatus{}, err
	}
	return coreLocalStatusFromRemote(service.LocalStatus()), nil
}

func (hook coreV2RemoteServiceHook) LocalDisable(ctx context.Context) (coreprotocol.RemoteLocalStatus, error) {
	service, err := hook.requireService()
	if err != nil {
		return coreprotocol.RemoteLocalStatus{}, err
	}
	status, err := service.LocalDisable(ctx)
	if err != nil {
		return coreprotocol.RemoteLocalStatus{}, err
	}
	return coreLocalStatusFromRemote(status), nil
}

func coreRemoteStatusFromRemote(status remoteprotocol.Status) coreprotocol.RemoteStatus {
	return coreprotocol.RemoteStatus{
		State:         string(status.State),
		Detail:        status.Detail,
		DeviceID:      status.DeviceID,
		DeviceName:    status.DeviceName,
		ControlURL:    status.ControlURL,
		HubURL:        status.HubURL,
		HubURLs:       append([]string(nil), status.HubURLs...),
		DataDir:       status.DataDir,
		Mode:          status.Mode,
		AllowLAN:      status.AllowLAN,
		TerminalCount: status.TerminalCount,
		UpdatedAt:     status.UpdatedAt,
	}
}

func remoteStatusFromCore(status coreprotocol.RemoteStatus) remoteprotocol.Status {
	return remoteprotocol.Status{
		State:         remoteprotocol.RuntimeState(status.State),
		Detail:        status.Detail,
		DeviceID:      status.DeviceID,
		DeviceName:    status.DeviceName,
		ControlURL:    status.ControlURL,
		HubURL:        status.HubURL,
		HubURLs:       append([]string(nil), status.HubURLs...),
		DataDir:       status.DataDir,
		Mode:          status.Mode,
		AllowLAN:      status.AllowLAN,
		TerminalCount: status.TerminalCount,
		UpdatedAt:     status.UpdatedAt,
	}
}

func remotePairStartParamsFromCore(params coreprotocol.RemotePairStartParams) remoteprotocol.PairStartParams {
	return remoteprotocol.PairStartParams{
		LocalPairURL:   params.LocalPairURL,
		TTLSeconds:     params.TTLSeconds,
		AuthTTLSeconds: params.AuthTTLSeconds,
	}
}

func corePairStartParamsFromRemote(params remoteprotocol.PairStartParams) coreprotocol.RemotePairStartParams {
	return coreprotocol.RemotePairStartParams{
		LocalPairURL:   params.LocalPairURL,
		TTLSeconds:     params.TTLSeconds,
		AuthTTLSeconds: params.AuthTTLSeconds,
	}
}

func corePairStartResultFromRemote(result remoteprotocol.PairStartResult) coreprotocol.RemotePairStartResult {
	return coreprotocol.RemotePairStartResult{
		Type:              result.Type,
		MachineID:         result.MachineID,
		MachineName:       result.MachineName,
		LocalPairURL:      result.LocalPairURL,
		PairSessionID:     result.PairSessionID,
		PairSecret:        result.PairSecret,
		AnswerProofSecret: result.AnswerProofSecret,
		ExpiresAt:         result.ExpiresAt,
	}
}

func remotePairStartResultFromCore(result coreprotocol.RemotePairStartResult) remoteprotocol.PairStartResult {
	return remoteprotocol.PairStartResult{
		Type:              result.Type,
		MachineID:         result.MachineID,
		MachineName:       result.MachineName,
		LocalPairURL:      result.LocalPairURL,
		PairSessionID:     result.PairSessionID,
		PairSecret:        result.PairSecret,
		AnswerProofSecret: result.AnswerProofSecret,
		ExpiresAt:         result.ExpiresAt,
	}
}

func remoteLocalEnableParamsFromCore(params coreprotocol.RemoteLocalEnableParams) remoteprotocol.LocalEnableParams {
	return remoteprotocol.LocalEnableParams{
		LocalWebAddr: params.LocalWebAddr,
		ICETCPAddr:   params.ICETCPAddr,
		HubURLs:      append([]string(nil), params.HubURLs...),
		ControlURL:   params.ControlURL,
		AccessToken:  params.AccessToken,
		Region:       params.Region,
	}
}

func coreLocalEnableParamsFromRemote(params remoteprotocol.LocalEnableParams) coreprotocol.RemoteLocalEnableParams {
	return coreprotocol.RemoteLocalEnableParams{
		LocalWebAddr: params.LocalWebAddr,
		ICETCPAddr:   params.ICETCPAddr,
		HubURLs:      append([]string(nil), params.HubURLs...),
		ControlURL:   params.ControlURL,
		AccessToken:  params.AccessToken,
		Region:       params.Region,
	}
}

func coreLocalStatusFromRemote(status remoteprotocol.LocalStatus) coreprotocol.RemoteLocalStatus {
	return coreprotocol.RemoteLocalStatus{
		Enabled:       status.Enabled,
		HTTPURL:       status.HTTPURL,
		LocalWebAddr:  status.LocalWebAddr,
		LocalPairURL:  status.LocalPairURL,
		ICETCPEnabled: status.ICETCPEnabled,
		ICETCPAddr:    status.ICETCPAddr,
		ICETCPPort:    status.ICETCPPort,
		UpdatedAt:     status.UpdatedAt,
	}
}

func remoteLocalStatusFromCore(status coreprotocol.RemoteLocalStatus) remoteprotocol.LocalStatus {
	return remoteprotocol.LocalStatus{
		Enabled:       status.Enabled,
		HTTPURL:       status.HTTPURL,
		LocalWebAddr:  status.LocalWebAddr,
		LocalPairURL:  status.LocalPairURL,
		ICETCPEnabled: status.ICETCPEnabled,
		ICETCPAddr:    status.ICETCPAddr,
		ICETCPPort:    status.ICETCPPort,
		UpdatedAt:     status.UpdatedAt,
	}
}

func cloneRemoteAdapterStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
