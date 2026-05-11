package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/terminalmeta"
	"github.com/lozzow/termx/termx-core/transport"
	"github.com/lozzow/termx/termx-remote/agent/runtime"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
)

type Daemon interface {
	Create(ctx context.Context, params protocol.CreateParams) (*protocol.CreateResult, error)
	Get(ctx context.Context, terminalID string) (*protocol.TerminalInfo, error)
	List(ctx context.Context) (*protocol.ListResult, error)
	SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error
	Remove(ctx context.Context, terminalID string) error
	Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error)
	ServeTransport(ctx context.Context, t transport.Transport, remote string) error
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
	pairCfg := pairing.Config{
		MachineID:       machineID,
		MachineName:     machineName,
		MachineSecret:   machineSecret,
		DefaultTokenTTL: time.Duration(s.cfg.TokenTTLSeconds) * time.Second,
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
	return p.daemon.ServeTransport(ctx, t, remote)
}

func (p daemonRuntimeAdapter) RouteTerminalManagementRequest(ctx context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	return terminalManagementRouter{daemon: p.daemon}.RouteTerminalManagementRequest(ctx, req)
}

func (p daemonRuntimeAdapter) SubscribeRemoteEvents(ctx context.Context, filters remotertc.EventFilters) (<-chan []byte, func(), error) {
	if p.daemon == nil {
		ch := make(chan []byte)
		close(ch)
		return ch, func() {}, nil
	}
	params := protocol.EventsParams{
		TerminalID: strings.TrimSpace(filters.TerminalID),
		SessionID:  strings.TrimSpace(filters.SessionID),
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
	if p.daemon == nil {
		return json.Marshal(evt)
	}
	record, err := eventRecord(evt)
	if err != nil {
		return json.Marshal(evt)
	}
	if evt.Type == protocol.EventTerminalMetadataChanged || evt.Type == protocol.EventTerminalResized {
		terminalID := strings.TrimSpace(evt.TerminalID)
		if terminalID != "" {
			info, getErr := p.daemon.Get(ctx, terminalID)
			if getErr == nil && info != nil {
				record["terminal"] = terminalInventoryFromProtocol(*info)
			}
		}
	}
	return json.Marshal(record)
}

func eventRecord(evt protocol.Event) (map[string]any, error) {
	payload, err := json.Marshal(evt)
	if err != nil {
		return nil, err
	}
	var record map[string]any
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, err
	}
	return record, nil
}

type terminalManagementRouter struct {
	daemon Daemon
}

func (r terminalManagementRouter) RouteTerminalManagementRequest(ctx context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	switch req.Path {
	case "list":
		terminals, err := r.listTerminals(ctx)
		if err != nil {
			return http.StatusInternalServerError, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(map[string]any{"terminals": terminals})
	case "get_directory":
		var body struct {
			TerminalID string `json:"terminal_id"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid get_directory request"
		}
		directory, source, err := r.getTerminalDirectory(ctx, body.TerminalID)
		if err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(map[string]string{
			"terminal_id": strings.TrimSpace(body.TerminalID),
			"path":        directory,
			"source":      source,
		})
	case "create":
		var body struct {
			Name    string            `json:"name"`
			Command []string          `json:"command"`
			Dir     string            `json:"dir"`
			Env     []string          `json:"env"`
			Tags    map[string]string `json:"tags"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid create request"
		}
		terminal, err := r.createTerminal(ctx, body.Name, body.Command, body.Dir, firstNonEmpty(body.Env), body.Tags[terminalmeta.SizeLockTag])
		if err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(terminal)
	case "set_metadata":
		var body struct {
			TerminalID string            `json:"terminal_id"`
			Name       string            `json:"name"`
			Tags       map[string]string `json:"tags"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid set_metadata request"
		}
		terminalID := strings.TrimSpace(body.TerminalID)
		if terminalID == "" {
			return http.StatusBadRequest, nil, "terminal_id is required"
		}
		terminal, err := r.updateTerminal(ctx, terminalID, body.Name, body.Tags["cwd"], body.Tags["environment"], body.Tags[terminalmeta.SizeLockTag])
		if err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(terminal)
	case "remove":
		var body struct {
			TerminalID string `json:"terminal_id"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return http.StatusBadRequest, nil, "invalid remove request"
		}
		terminalID := strings.TrimSpace(body.TerminalID)
		if terminalID == "" {
			return http.StatusBadRequest, nil, "terminal_id is required"
		}
		if err := r.removeTerminal(ctx, terminalID); err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return http.StatusOK, []byte(`{}`), ""
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

func (r terminalManagementRouter) createTerminal(ctx context.Context, name string, command []string, dir string, environment string, sizeLockMode string) (runtime.TerminalInventoryItem, error) {
	if r.daemon == nil {
		return runtime.TerminalInventoryItem{}, nil
	}
	resolvedCommand := defaultTerminalCommand(command)
	resolvedDir := defaultTerminalDir(dir)
	created, err := r.daemon.Create(ctx, protocol.CreateParams{
		Command: append([]string(nil), resolvedCommand...),
		Name:    strings.TrimSpace(name),
		Tags:    localTerminalTags(resolvedDir, environment, sizeLockMode),
		Dir:     resolvedDir,
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
		ID:           item.ID,
		Name:         item.Name,
		State:        item.State,
		Command:      append([]string(nil), item.Command...),
		Cols:         int(item.Size.Cols),
		Rows:         int(item.Size.Rows),
		CWD:          cwd,
		Environment:  item.Tags["termx.environment"],
		SizeLocked:   terminalmeta.SizeLocked(item.Tags),
		SizeLockMode: sizeLockMode,
		ResizeOwnership:            cloneProtocolResizeOwnership(item.ResizeOwnership),
		ResizeOwnerAttachmentCount: item.ResizeOwnerAttachmentCount,
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

func marshalRuntimeAPIResponse(value any) (int32, []byte, string) {
	data, err := json.Marshal(value)
	if err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}
	return http.StatusOK, data, ""
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
