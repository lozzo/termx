package remote

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-remote/agent/runtime"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	"github.com/lozzow/termx/termx-remote/protocol/runtimepb"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
	"github.com/lozzow/termx/termx-shared/terminalmeta"
	"github.com/lozzow/termx/termx-shared/transport"
	"google.golang.org/protobuf/proto"
)

type Daemon interface {
	Create(ctx context.Context, params protocol.CreateParams) (*protocol.CreateResult, error)
	Get(ctx context.Context, terminalID string) (*protocol.TerminalInfo, error)
	List(ctx context.Context) (*protocol.ListResult, error)
	SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error
	Restart(ctx context.Context, terminalID string) error
	Remove(ctx context.Context, terminalID string) error
	Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error)
	ServeTransport(ctx context.Context, t transport.Transport, remote string) error
}

type StorageDaemon interface {
	StorageGet(ctx context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error)
	StoragePut(ctx context.Context, params protocol.StoragePutParams) (*protocol.StorageEntry, error)
	StorageDelete(ctx context.Context, params protocol.StorageDeleteParams) (*protocol.StorageDeleteResult, error)
	StorageList(ctx context.Context, params protocol.StorageListParams) (*protocol.StorageListResult, error)
}

type ScopedDaemon interface {
	ServeScopedTransport(ctx context.Context, t transport.Transport, remote string, scope TransportScope) error
}

type TransportScope struct {
	TerminalID        string
	MachineEventsOnly bool
}

type Service struct {
	cfg    remoteprotocol.Config
	daemon Daemon

	manager *runtime.Manager

	pairing *pairingStore

	rtcMu     sync.Mutex
	rtcFiles  *fileapi.Manager
	rtcCtx    context.Context
	rtcCancel context.CancelFunc

	localMu sync.Mutex
	local   *localRuntime
}

func NewService(cfg remoteprotocol.Config, daemon Daemon) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		cfg:       cfg,
		daemon:    daemon,
		rtcCtx:    ctx,
		rtcCancel: cancel,
		pairing:   &pairingStore{},
	}
	runtimeAdapter := daemonRuntimeAdapter{daemon: daemon}
	s.manager = runtime.NewManager(runtimeConfig(cfg), runtimeAdapter, runtimeAdapter)
	s.manager.SetPairClaimer(pairClaimer{store: s.pairing})
	return s
}

func (s *Service) Start(ctx context.Context) error {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.Start(ctx)
}

func (s *Service) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.manager != nil {
		s.manager.Close()
	}
	_, err := s.LocalDisable(ctx)
	if s.rtcFiles != nil {
		s.rtcFiles.Close()
	}
	if s.rtcCancel != nil {
		s.rtcCancel()
	}
	return err
}

func (s *Service) TriggerSync() {
	if s == nil || s.manager == nil {
		return
	}
	s.manager.TriggerSync()
}

func (s *Service) Status() remoteprotocol.Status {
	if s == nil || s.manager == nil {
		return remoteprotocol.Status{
			State:     remoteprotocol.StateDisabled,
			Detail:    "remote runtime disabled",
			UpdatedAt: time.Now().UTC(),
		}
	}
	status := s.manager.Status()
	return remoteprotocol.Status{
		State:         mapRuntimeState(status.State),
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

func (s *Service) PairStart(params remoteprotocol.PairStartParams) (remoteprotocol.PairStartResult, error) {
	if s == nil {
		return remoteprotocol.PairStartResult{}, fmt.Errorf("remote service is nil")
	}
	cfg := remoteconfig.Normalize(remoteconfig.Config{
		Enabled:    s.cfg.Enabled,
		DataDir:    s.cfg.DataDir,
		DeviceName: s.cfg.DeviceName,
	})
	machineID := ""
	machineName := strings.TrimSpace(cfg.DeviceName)
	if s.manager != nil {
		status := s.manager.Status()
		machineID = strings.TrimSpace(status.DeviceID)
		if machineName == "" {
			machineName = strings.TrimSpace(status.DeviceName)
		}
	}
	if machineID == "" {
		ident, err := identity.LoadOrCreate(cfg.DataDir, cfg.DeviceName)
		if err != nil {
			return remoteprotocol.PairStartResult{}, err
		}
		machineID = ident.DeviceID
		if machineName == "" {
			machineName = ident.DisplayName
		}
	}
	machineSecret, err := identity.LoadOrCreateMachineSecret(cfg.DataDir)
	if err != nil {
		return remoteprotocol.PairStartResult{}, err
	}
	tokenTTLSeconds := s.cfg.TokenTTLSeconds
	if params.AuthTTLSeconds > 0 {
		tokenTTLSeconds = params.AuthTTLSeconds
	}
	pairCfg := pairing.Config{
		MachineID:       machineID,
		MachineName:     machineName,
		MachineSecret:   machineSecret,
		DefaultTokenTTL: time.Duration(tokenTTLSeconds) * time.Second,
		LocalPairURL:    strings.TrimSpace(params.LocalPairURL),
	}
	manager, err := s.pairing.managerForConfig(pairCfg)
	if err != nil {
		return remoteprotocol.PairStartResult{}, err
	}

	session, err := manager.CreateSession(time.Duration(params.TTLSeconds) * time.Second)
	if err != nil {
		return remoteprotocol.PairStartResult{}, err
	}
	return remoteprotocol.PairStartResult{
		Type:              session.Type,
		MachineID:         session.MachineID,
		MachineName:       session.MachineName,
		LocalPairURL:      session.LocalPairURL,
		PairSessionID:     session.PairSessionID,
		PairSecret:        session.PairSecret,
		AnswerProofSecret: session.AnswerProofSecret,
		ExpiresAt:         session.ExpiresAt,
	}, nil
}

type LocalICETCPMux = remotertc.LocalICETCPMux

func StartLocalICETCPMux(ctx context.Context, addr string) (*LocalICETCPMux, error) {
	return remotertc.StartLocalICETCPMux(ctx, addr)
}

func (s *Service) LocalEnable(ctx context.Context, params remoteprotocol.LocalEnableParams) (remoteprotocol.LocalStatus, error) {
	if s == nil {
		return remoteprotocol.LocalStatus{}, fmt.Errorf("remote service is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.localMu.Lock()
	if s.local != nil {
		local := s.local
		status := s.local.statusLocked()
		s.localMu.Unlock()
		s.attachManagerToCloud(params, local.httpURL)
		s.attachManagerToLocalHub(s.managerContext(ctx), local)
		return status, nil
	}
	s.localMu.Unlock()

	runtime, err := newEmbeddedLocalHub(ctx, params, runtimeConfig(s.cfg))
	if err != nil {
		return remoteprotocol.LocalStatus{}, err
	}
	s.localMu.Lock()
	if s.local != nil {
		existing := s.local
		status := existing.statusLocked()
		s.localMu.Unlock()
		_ = runtime.close(context.Background())
		return status, nil
	}
	s.local = runtime
	status := runtime.statusLocked()
	s.localMu.Unlock()
	s.attachManagerToCloud(params, runtime.httpURL)
	s.attachManagerToLocalHub(s.managerContext(ctx), runtime)
	return status, nil
}

func (s *Service) LocalStatus() remoteprotocol.LocalStatus {
	if s == nil {
		return remoteprotocol.LocalStatus{UpdatedAt: time.Now().UTC()}
	}
	s.localMu.Lock()
	defer s.localMu.Unlock()
	if s.local == nil {
		return remoteprotocol.LocalStatus{UpdatedAt: time.Now().UTC()}
	}
	return s.local.statusLocked()
}

func (s *Service) LocalDisable(ctx context.Context) (remoteprotocol.LocalStatus, error) {
	if s == nil {
		return remoteprotocol.LocalStatus{}, fmt.Errorf("remote service is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.localMu.Lock()
	runtime := s.local
	s.local = nil
	s.localMu.Unlock()
	if runtime != nil {
		s.detachManagerFromLocalHub(runtime)
		if err := runtime.close(ctx); err != nil {
			return remoteprotocol.LocalStatus{}, err
		}
	}
	return remoteprotocol.LocalStatus{UpdatedAt: time.Now().UTC()}, nil
}

type daemonRuntimeAdapter struct {
	daemon Daemon
}

func (p daemonRuntimeAdapter) ListRemoteTerminals(ctx context.Context) []runtime.TerminalInventoryItem {
	if p.daemon == nil {
		return nil
	}
	list, err := p.daemon.List(ctx)
	if err != nil {
		return nil
	}
	out := make([]runtime.TerminalInventoryItem, 0, len(list.Terminals))
	for _, item := range list.Terminals {
		out = append(out, terminalInventoryFromProtocol(item))
	}
	return out
}

func (p daemonRuntimeAdapter) ServeRemoteTransport(ctx context.Context, t transport.Transport, remote string) error {
	if p.daemon == nil {
		return nil
	}
	if scoped, ok := p.daemon.(ScopedDaemon); ok && scoped != nil {
		scope, scopedRemote, err := scopedTransportScopeFromRemote(remote)
		if err != nil {
			return err
		}
		if scopedRemote {
			return scoped.ServeScopedTransport(ctx, t, remote, scope)
		}
	}
	return p.daemon.ServeTransport(ctx, t, remote)
}

func scopedTransportScopeFromRemote(remote string) (TransportScope, bool, error) {
	label := strings.TrimSpace(remote)
	label = strings.TrimPrefix(label, "webrtc:")
	if !strings.HasPrefix(label, "terminal:") {
		return TransportScope{}, false, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(label, "terminal:"))
	if payload == "" {
		return TransportScope{}, true, fmt.Errorf("terminal transport label requires terminal id")
	}
	// 中文说明：App native bridge 可能使用 terminal:<machine>:<terminal>，服务端
	// 当前 session 已经绑定 machine，所以 terminal scope 只取最后一段 terminal id。
	if idx := strings.LastIndex(payload, ":"); idx >= 0 {
		payload = strings.TrimSpace(payload[idx+1:])
	}
	if payload == "" {
		return TransportScope{}, true, fmt.Errorf("terminal transport label requires terminal id")
	}
	return TransportScope{TerminalID: payload}, true, nil
}

func (p daemonRuntimeAdapter) RouteTerminalManagementRequest(ctx context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	return terminalManagementRouter{daemon: p.daemon}.RouteTerminalManagementRequest(ctx, req)
}

func (p daemonRuntimeAdapter) RouteStorageRequest(ctx context.Context, req remotertc.StorageRequest) (int32, []byte, string) {
	storage, ok := p.daemon.(StorageDaemon)
	if !ok || storage == nil {
		return http.StatusServiceUnavailable, nil, "storage api is not available"
	}
	return storageRouter{storage: storage}.RouteStorageRequest(ctx, req)
}

func (p daemonRuntimeAdapter) SubscribeRemoteEvents(ctx context.Context, filters remotertc.EventFilters) (<-chan []byte, func(), error) {
	if p.daemon == nil {
		ch := make(chan []byte)
		close(ch)
		return ch, func() {}, nil
	}
	params := protocol.EventsParams{
		TerminalID: strings.TrimSpace(filters.TerminalID),
	}
	if len(filters.Types) > 0 {
		params.Types = make([]protocol.EventType, 0, len(filters.Types))
		for _, typ := range filters.Types {
			params.Types = append(params.Types, protocol.EventType(typ))
		}
	}
	events, err := p.daemon.Events(ctx, params)
	if err != nil {
		return nil, func() {}, err
	}
	out := make(chan []byte, 64)
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
				payload, err := p.marshalRemoteEvent(ctx, evt)
				if err != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case out <- payload:
				}
			}
		}
	}()
	return out, func() {}, nil
}

func (p daemonRuntimeAdapter) marshalRemoteEvent(ctx context.Context, evt protocol.Event) ([]byte, error) {
	event := runtimeEventFromProtocol(evt)
	if p.daemon != nil && (evt.Type == protocol.EventTerminalMetadataChanged || evt.Type == protocol.EventTerminalResized) {
		terminalID := strings.TrimSpace(evt.TerminalID)
		if terminalID != "" {
			info, getErr := p.daemon.Get(ctx, terminalID)
			if getErr == nil && info != nil {
				event.Terminal = terminalInventoryToProto(terminalInventoryFromProtocol(*info))
			}
		}
	}
	return proto.Marshal(event)
}

type terminalManagementRouter struct {
	daemon Daemon
}

type storageRouter struct {
	storage StorageDaemon
}

func (r storageRouter) RouteStorageRequest(ctx context.Context, req remotertc.StorageRequest) (int32, []byte, string) {
	if r.storage == nil {
		return http.StatusServiceUnavailable, nil, "storage api is not available"
	}
	switch req.Path {
	case "/storage/get":
		var body runtimepb.StorageGetRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid storage get request"
		}
		entry, err := r.storage.StorageGet(ctx, protocol.StorageGetParams{
			AppID:   strings.TrimSpace(body.GetAppId()),
			Scope:   protocol.StorageScope(strings.TrimSpace(body.GetScope())),
			OwnerID: strings.TrimSpace(body.GetOwnerId()),
			Key:     strings.TrimSpace(body.GetKey()),
		})
		if err != nil {
			return storageErrorStatus(err), nil, err.Error()
		}
		return marshalRuntimeAPIResponse(storageEntryToProto(entry))
	case "/storage/put":
		var body runtimepb.StoragePutRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid storage put request"
		}
		entry, err := r.storage.StoragePut(ctx, protocol.StoragePutParams{
			AppID:           strings.TrimSpace(body.GetAppId()),
			Scope:           protocol.StorageScope(strings.TrimSpace(body.GetScope())),
			OwnerID:         strings.TrimSpace(body.GetOwnerId()),
			Key:             strings.TrimSpace(body.GetKey()),
			Value:           append([]byte(nil), body.GetValue()...),
			CheckVersion:    body.GetCheckVersion(),
			ExpectedVersion: body.GetExpectedVersion(),
		})
		if err != nil {
			return storageErrorStatus(err), nil, err.Error()
		}
		return marshalRuntimeAPIResponse(storageEntryToProto(entry))
	case "/storage/delete":
		var body runtimepb.StorageDeleteRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid storage delete request"
		}
		result, err := r.storage.StorageDelete(ctx, protocol.StorageDeleteParams{
			AppID:           strings.TrimSpace(body.GetAppId()),
			Scope:           protocol.StorageScope(strings.TrimSpace(body.GetScope())),
			OwnerID:         strings.TrimSpace(body.GetOwnerId()),
			Key:             strings.TrimSpace(body.GetKey()),
			CheckVersion:    body.GetCheckVersion(),
			ExpectedVersion: body.GetExpectedVersion(),
		})
		if err != nil {
			return storageErrorStatus(err), nil, err.Error()
		}
		return marshalRuntimeAPIResponse(&runtimepb.StorageDeleteResponse{
			AppId:   result.AppID,
			Scope:   string(result.Scope),
			OwnerId: result.OwnerID,
			Key:     result.Key,
			Deleted: result.Deleted,
			Version: result.Version,
		})
	case "/storage/list":
		var body runtimepb.StorageListRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid storage list request"
		}
		result, err := r.storage.StorageList(ctx, protocol.StorageListParams{
			AppID:   strings.TrimSpace(body.GetAppId()),
			Scope:   protocol.StorageScope(strings.TrimSpace(body.GetScope())),
			OwnerID: strings.TrimSpace(body.GetOwnerId()),
			Prefix:  strings.TrimSpace(body.GetPrefix()),
		})
		if err != nil {
			return storageErrorStatus(err), nil, err.Error()
		}
		entries := make([]*runtimepb.StorageEntry, 0, len(result.Entries))
		for _, entry := range result.Entries {
			item := entry
			entries = append(entries, storageEntryToProto(&item))
		}
		return marshalRuntimeAPIResponse(&runtimepb.StorageListResponse{Entries: entries})
	default:
		return http.StatusNotFound, nil, "unknown storage route"
	}
}

func (r terminalManagementRouter) RouteTerminalManagementRequest(ctx context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	switch req.Path {
	case "list":
		terminals, err := r.listTerminals(ctx)
		if err != nil {
			return http.StatusInternalServerError, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(&runtimepb.TerminalListResponse{Terminals: terminalInventoryListToProto(terminals)})
	case "get_directory":
		var body runtimepb.TerminalDirectoryRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid get_directory request"
		}
		directory, source, err := r.getTerminalDirectory(ctx, body.GetTerminalId())
		if err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(&runtimepb.TerminalDirectoryResponse{
			TerminalId: strings.TrimSpace(body.GetTerminalId()),
			Path:       directory,
			Source:     source,
		})
	case "create":
		var body runtimepb.TerminalCreateRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid create request"
		}
		terminal, err := r.createTerminal(
			ctx,
			body.GetName(),
			body.GetCommand(),
			body.GetDir(),
			body.GetEnv(),
			body.GetTags()[terminalmeta.SizeLockTag],
			int(body.GetScrollbackSize()),
			body.GetScrollbackMaxBytes(),
			time.Duration(body.GetScrollbackMaxAgeSeconds())*time.Second,
		)
		if err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(terminalInventoryToProto(terminal))
	case "set_metadata":
		var body runtimepb.TerminalSetMetadataRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid set_metadata request"
		}
		terminalID := strings.TrimSpace(body.GetTerminalId())
		if terminalID == "" {
			return http.StatusBadRequest, nil, "terminal_id is required"
		}
		terminal, err := r.updateTerminal(ctx, terminalID, body.GetName(), body.GetTags()["cwd"], body.GetTags()["environment"], body.GetTags()[terminalmeta.SizeLockTag])
		if err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(terminalInventoryToProto(terminal))
	case "restart":
		var body runtimepb.TerminalIDRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid restart request"
		}
		terminalID := strings.TrimSpace(body.GetTerminalId())
		if terminalID == "" {
			return http.StatusBadRequest, nil, "terminal_id is required"
		}
		if err := r.restartTerminal(ctx, terminalID); err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(&runtimepb.Empty{})
	case "remove":
		var body runtimepb.TerminalIDRequest
		if err := proto.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid remove request"
		}
		terminalID := strings.TrimSpace(body.GetTerminalId())
		if terminalID == "" {
			return http.StatusBadRequest, nil, "terminal_id is required"
		}
		if err := r.removeTerminal(ctx, terminalID); err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(&runtimepb.Empty{})
	default:
		return http.StatusNotFound, nil, "unknown terminal management route"
	}
}

func (r terminalManagementRouter) listTerminals(ctx context.Context) ([]runtime.TerminalInventoryItem, error) {
	if r.daemon == nil {
		return nil, nil
	}
	list, err := r.daemon.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]runtime.TerminalInventoryItem, 0, len(list.Terminals))
	for _, item := range list.Terminals {
		out = append(out, terminalInventoryFromProtocol(item))
	}
	return out, nil
}

func (r terminalManagementRouter) createTerminal(ctx context.Context, name string, command []string, dir string, env []string, sizeLockMode string, scrollbackSize int, scrollbackMaxBytes int64, scrollbackMaxAge time.Duration) (runtime.TerminalInventoryItem, error) {
	if r.daemon == nil {
		return runtime.TerminalInventoryItem{}, nil
	}
	resolvedCommand := defaultTerminalCommand(command)
	resolvedDir := defaultTerminalDir(dir)
	resolvedEnv := terminalEnvironmentFromRemote(env)
	environmentTag := strings.Join(resolvedEnv, "\n")
	created, err := r.daemon.Create(ctx, protocol.CreateParams{
		Command:            append([]string(nil), resolvedCommand...),
		Name:               strings.TrimSpace(name),
		Tags:               localTerminalTags(resolvedDir, environmentTag, sizeLockMode),
		Dir:                resolvedDir,
		Env:                resolvedEnv,
		ScrollbackSize:     scrollbackSize,
		ScrollbackMaxBytes: scrollbackMaxBytes,
		ScrollbackMaxAge:   scrollbackMaxAge,
	})
	if err != nil {
		return runtime.TerminalInventoryItem{}, err
	}
	info, err := r.daemon.Get(ctx, created.TerminalID)
	if err == nil && info != nil {
		return terminalInventoryFromProtocol(*info), nil
	}
	return runtime.TerminalInventoryItem{
		ID:      created.TerminalID,
		Name:    strings.TrimSpace(name),
		Command: append([]string(nil), resolvedCommand...),
		State:   created.State,
		CWD:     resolvedDir,
	}, nil
}

func defaultTerminalCommand(command []string) []string {
	out := make([]string, 0, len(command))
	for _, part := range command {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) > 0 {
		return out
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell}
}

func defaultTerminalDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir != "" {
		return dir
	}
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return ""
	}
	return wd
}

func (r terminalManagementRouter) updateTerminal(ctx context.Context, terminalID string, name string, cwd string, environment string, sizeLockMode string) (runtime.TerminalInventoryItem, error) {
	if r.daemon == nil {
		return runtime.TerminalInventoryItem{}, nil
	}
	info, err := r.daemon.Get(ctx, terminalID)
	if err != nil {
		return runtime.TerminalInventoryItem{}, err
	}
	tags := copyTags(info.Tags)
	if tags == nil {
		tags = map[string]string{}
	}
	mergeLocalTerminalTags(tags, cwd, environment, sizeLockMode)
	if err := r.daemon.SetMetadata(ctx, terminalID, strings.TrimSpace(name), tags); err != nil {
		return runtime.TerminalInventoryItem{}, err
	}
	updated, err := r.daemon.Get(ctx, terminalID)
	if err != nil {
		return runtime.TerminalInventoryItem{}, err
	}
	return terminalInventoryFromProtocol(*updated), nil
}

func (r terminalManagementRouter) removeTerminal(ctx context.Context, terminalID string) error {
	if r.daemon == nil {
		return nil
	}
	return r.daemon.Remove(ctx, terminalID)
}

func (r terminalManagementRouter) restartTerminal(ctx context.Context, terminalID string) error {
	if r.daemon == nil {
		return nil
	}
	return r.daemon.Restart(ctx, terminalID)
}

func (r terminalManagementRouter) getTerminalDirectory(ctx context.Context, terminalID string) (string, string, error) {
	if r.daemon == nil {
		return "", "", nil
	}
	terminalID = strings.TrimSpace(terminalID)
	if terminalID == "" {
		return "", "", fmt.Errorf("terminal_id is required")
	}
	info, err := r.daemon.Get(ctx, terminalID)
	if err != nil {
		return "", "", err
	}
	if info == nil {
		return "", "", fmt.Errorf("terminal not found")
	}
	if cwd := strings.TrimSpace(info.LiveCWD); cwd != "" {
		return cwd, "process", nil
	}
	if cwd := strings.TrimSpace(info.CWD); cwd != "" {
		return cwd, "reported", nil
	}
	if cwd := strings.TrimSpace(info.Tags["termx.cwd"]); cwd != "" {
		return cwd, "metadata", nil
	}
	return "", "", nil
}

type pairingStore struct {
	mu      sync.Mutex
	manager *pairing.Manager
	cfg     pairing.Config
}

func (s *pairingStore) managerForConfig(cfg pairing.Config) (*pairing.Manager, error) {
	if s == nil {
		return nil, pairingErr("pairing store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manager == nil {
		s.manager = pairing.NewManager(cfg)
		s.cfg = cfg
		return s.manager, nil
	}
	if pairManagerConfigChanged(s.cfg, cfg) {
		if err := s.manager.UpdateConfig(cfg); err != nil {
			return nil, err
		}
		s.cfg = cfg
	}
	return s.manager, nil
}

func (s *pairingStore) claim(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	if s == nil {
		return pairing.ClaimResponse{}, pairingErr("pairing store is nil")
	}
	if err := ctx.Err(); err != nil {
		return pairing.ClaimResponse{}, err
	}
	if strings.TrimSpace(req.PairSessionID) == "" {
		return pairing.ClaimResponse{}, pairingErr("pair_session_id is required")
	}
	s.mu.Lock()
	manager := s.manager
	s.mu.Unlock()
	if manager == nil {
		return pairing.ClaimResponse{}, pairingErr("pair session not found")
	}
	return manager.ClaimSession(req)
}

func (s *Service) pairClaim(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	if s == nil || s.pairing == nil {
		return pairing.ClaimResponse{}, pairingErr("pairing store is nil")
	}
	return s.pairing.claim(ctx, req)
}

type pairClaimer struct {
	store *pairingStore
}

func (p pairClaimer) ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	if p.store == nil {
		return pairing.ClaimResponse{}, pairingErr("pairing store is nil")
	}
	return p.store.claim(ctx, req)
}

func (s *Service) normalizedConfig() remoteLocalConfig {
	if s == nil {
		return remoteLocalConfig{}
	}
	cfg := remoteconfig.Normalize(remoteconfig.Config{
		Enabled:    s.cfg.Enabled,
		DataDir:    s.cfg.DataDir,
		DeviceName: s.cfg.DeviceName,
	})
	return remoteLocalConfig{
		DataDir:    cfg.DataDir,
		DeviceName: cfg.DeviceName,
	}
}

type remoteLocalConfig struct {
	DataDir    string
	DeviceName string
}

type pairingErr string

func (e pairingErr) Error() string { return string(e) }

func runtimeConfig(cfg remoteprotocol.Config) remoteconfig.Config {
	return remoteconfig.Normalize(remoteconfig.Config{
		Enabled:     cfg.Enabled,
		ControlURL:  cfg.ControlURL,
		HubURL:      cfg.HubURL,
		HubURLs:     append([]string(nil), cfg.HubURLs...),
		AccessToken: cfg.AccessToken,
		DataDir:     cfg.DataDir,
		DeviceName:  cfg.DeviceName,
		Region:      cfg.Region,
		Mode:        cfg.Mode,
		AllowLAN:    cfg.AllowLAN,
		LANIPs:      append([]string(nil), cfg.LANIPs...),
		TokenTTL:    time.Duration(cfg.TokenTTLSeconds) * time.Second,
	})
}

func mapRuntimeState(state runtime.State) remoteprotocol.RuntimeState {
	switch state {
	case runtime.StateConfigured:
		return remoteprotocol.StateConfigured
	case runtime.StateRegistering:
		return remoteprotocol.StateRegistering
	case runtime.StateOnline:
		return remoteprotocol.StateOnline
	case runtime.StateDegraded:
		return remoteprotocol.StateDegraded
	default:
		return remoteprotocol.StateDisabled
	}
}

func pairManagerConfigChanged(a, b pairing.Config) bool {
	if a.MachineID != b.MachineID || a.MachineName != b.MachineName || a.LocalPairURL != b.LocalPairURL || a.DefaultTokenTTL != b.DefaultTokenTTL {
		return true
	}
	return !bytes.Equal(a.MachineSecret, b.MachineSecret)
}

func terminalInventoryFromProtocol(item protocol.TerminalInfo) runtime.TerminalInventoryItem {
	sizeLockMode := terminalmeta.SizeLockMode(item.Tags)
	cwd := strings.TrimSpace(item.LiveCWD)
	if cwd == "" {
		cwd = strings.TrimSpace(item.CWD)
	}
	if cwd == "" {
		cwd = item.Tags["termx.cwd"]
	}
	return runtime.TerminalInventoryItem{
		ID:                         item.ID,
		Name:                       item.Name,
		State:                      item.State,
		Command:                    append([]string(nil), item.Command...),
		Cols:                       int(item.Size.Cols),
		Rows:                       int(item.Size.Rows),
		CWD:                        cwd,
		Environment:                item.Tags["termx.environment"],
		SizeLocked:                 terminalmeta.SizeLocked(item.Tags),
		SizeLockMode:               sizeLockMode,
		ResizeOwnership:            cloneProtocolResizeOwnership(item.ResizeOwnership),
		ResizeOwnerAttachmentCount: item.ResizeOwnerAttachmentCount,
	}
}

func terminalInventoryListToProto(items []runtime.TerminalInventoryItem) []*runtimepb.TerminalInventoryItem {
	out := make([]*runtimepb.TerminalInventoryItem, 0, len(items))
	for _, item := range items {
		out = append(out, terminalInventoryToProto(item))
	}
	return out
}

func terminalInventoryToProto(item runtime.TerminalInventoryItem) *runtimepb.TerminalInventoryItem {
	return &runtimepb.TerminalInventoryItem{
		TerminalId:                 item.ID,
		Name:                       item.Name,
		State:                      item.State,
		Command:                    append([]string(nil), item.Command...),
		Cols:                       int32(item.Cols),
		Rows:                       int32(item.Rows),
		Cwd:                        item.CWD,
		Environment:                item.Environment,
		SizeLocked:                 item.SizeLocked,
		SizeLockMode:               item.SizeLockMode,
		ResizeOwnership:            resizeOwnershipToRuntimeProto(item.ResizeOwnership),
		ResizeOwnerAttachmentCount: int32(item.ResizeOwnerAttachmentCount),
	}
}

func resizeOwnershipToRuntimeProto(in *protocol.ResizeOwnership) *runtimepb.ResizeOwnership {
	if in == nil {
		return nil
	}
	return &runtimepb.ResizeOwnership{
		OwnerAttachmentId: in.OwnerAttachmentID,
		OwnerSurfaceId:    in.OwnerSurfaceID,
		OwnerViewId:       in.OwnerViewID,
		OwnerRemoteAddr:   in.OwnerRemoteAddr,
		Size:              &runtimepb.Size{Cols: uint32(in.Size.Cols), Rows: uint32(in.Size.Rows)},
		SizeLocked:        in.SizeLocked,
		Epoch:             in.Epoch,
	}
}

func runtimeEventFromProtocol(evt protocol.Event) *runtimepb.EventEnvelope {
	protocolType := int32(evt.Type)
	return &runtimepb.EventEnvelope{
		Type:              runtimeEventTypeName(protocolType),
		ProtocolType:      protocolType,
		TerminalId:        strings.TrimSpace(evt.TerminalID),
		TimestampUnixNano: evt.Timestamp.UnixNano(),
	}
}

func runtimeEventTypeName(protocolType int32) string {
	switch protocol.EventType(protocolType) {
	case protocol.EventTerminalCreated:
		return "terminal_created"
	case protocol.EventTerminalStateChanged:
		return "terminal_state_changed"
	case protocol.EventTerminalResized:
		return "terminal_resized"
	case protocol.EventTerminalRemoved:
		return "terminal_removed"
	case protocol.EventTerminalMetadataChanged:
		return "terminal_metadata_changed"
	default:
		return "event"
	}
}

func cloneProtocolResizeOwnership(in *protocol.ResizeOwnership) *protocol.ResizeOwnership {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func localTerminalTags(cwd string, environment string, sizeLockMode string) map[string]string {
	tags := map[string]string{}
	mergeLocalTerminalTags(tags, cwd, environment, sizeLockMode)
	return tags
}

func terminalEnvironmentFromRemote(environment []string) []string {
	out := make([]string, 0, len(environment))
	for _, line := range environment {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func mergeLocalTerminalTags(tags map[string]string, cwd string, environment string, sizeLockMode string) {
	if strings.TrimSpace(cwd) != "" {
		tags["termx.cwd"] = strings.TrimSpace(cwd)
	}
	if strings.TrimSpace(environment) != "" {
		tags["termx.environment"] = strings.TrimSpace(environment)
	}
	switch strings.TrimSpace(sizeLockMode) {
	case terminalmeta.SizeLockOff:
		delete(tags, terminalmeta.SizeLockTag)
	case terminalmeta.SizeLockWarn, terminalmeta.SizeLockLock:
		tags[terminalmeta.SizeLockTag] = strings.TrimSpace(sizeLockMode)
	}
}

func hasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == want {
			return true
		}
	}
	return false
}

func marshalRuntimeAPIResponse(value proto.Message) (int32, []byte, string) {
	data, err := proto.Marshal(value)
	if err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}
	return http.StatusOK, data, ""
}

func storageEntryToProto(entry *protocol.StorageEntry) *runtimepb.StorageEntry {
	if entry == nil {
		return &runtimepb.StorageEntry{}
	}
	updatedAt := ""
	if !entry.UpdatedAt.IsZero() {
		updatedAt = entry.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return &runtimepb.StorageEntry{
		AppId:     entry.AppID,
		Scope:     string(entry.Scope),
		OwnerId:   entry.OwnerID,
		Key:       entry.Key,
		Value:     append([]byte(nil), entry.Value...),
		Version:   entry.Version,
		UpdatedAt: updatedAt,
	}
}

func storageErrorStatus(err error) int32 {
	if err == nil {
		return http.StatusOK
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "not found"):
		return http.StatusNotFound
	case strings.Contains(message, "does not exist"):
		return http.StatusNotFound
	case strings.Contains(message, "permission"):
		return http.StatusForbidden
	case strings.Contains(message, "conflict"):
		return http.StatusConflict
	case strings.Contains(message, "invalid"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func nonEmpty(first, fallback string) string {
	if strings.TrimSpace(first) != "" {
		return first
	}
	return fallback
}

func copyTags(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
