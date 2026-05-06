package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/terminalmeta"
	"github.com/lozzow/termx/termx-core/transport"
	"github.com/lozzow/termx/termx-remote/agent/runtime"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/grpcadapter"
	"github.com/lozzow/termx/termx-remote/hub/httpapi"
	"github.com/lozzow/termx/termx-remote/hub/registry"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
	"github.com/soheilhy/cmux"
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

	pairMu  sync.Mutex
	pairing *pairing.Manager
	pairCfg pairing.Config

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
	}
	s.manager = runtime.NewManager(runtimeConfig(cfg), inventoryProvider{service: s}, inventoryProvider{service: s})
	s.manager.SetPairClaimer(pairClaimer{service: s})
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
	machineSecret, err := identity.LoadOrCreateMachineSecret(cfg.DataDir)
	if err != nil {
		return remoteprotocol.PairStartResult{}, err
	}
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
	pairCfg := pairing.Config{
		MachineID:       machineID,
		MachineName:     machineName,
		MachineSecret:   machineSecret,
		DefaultTokenTTL: time.Duration(s.cfg.TokenTTLSeconds) * time.Second,
		LocalPairURL:    strings.TrimSpace(params.LocalPairURL),
	}
	s.pairMu.Lock()
	if s.pairing == nil {
		s.pairing = pairing.NewManager(pairCfg)
		s.pairCfg = pairCfg
	} else if pairManagerConfigChanged(s.pairCfg, pairCfg) {
		if err := s.pairing.UpdateConfig(pairCfg); err != nil {
			s.pairMu.Unlock()
			return remoteprotocol.PairStartResult{}, err
		}
		s.pairCfg = pairCfg
	}
	manager := s.pairing
	s.pairMu.Unlock()

	session, err := manager.CreateSession(time.Duration(params.TTLSeconds) * time.Second)
	if err != nil {
		return remoteprotocol.PairStartResult{}, err
	}
	return remoteprotocol.PairStartResult{
		Type:          session.Type,
		MachineID:     session.MachineID,
		MachineName:   session.MachineName,
		LocalPairURL:  session.LocalPairURL,
		PairSessionID: session.PairSessionID,
		PairSecret:    session.PairSecret,
		ExpiresAt:     session.ExpiresAt,
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

func (s *Service) managerContext(fallback context.Context) context.Context {
	if s != nil && s.rtcCtx != nil {
		return s.rtcCtx
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
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

type localRuntime struct {
	localWebAddr  string
	iceTCPAddr    string
	httpURL       string
	actualWebAddr string
	localPairURL  string
	iceTCPPort    int
	iceTCPMux     *LocalICETCPMux
	listener      net.Listener
	httpServer    *http.Server
	shutdown      func(context.Context) error
	cancel        context.CancelFunc
	updatedAt     time.Time
}

func (r *localRuntime) statusLocked() remoteprotocol.LocalStatus {
	if r == nil {
		return remoteprotocol.LocalStatus{UpdatedAt: time.Now().UTC()}
	}
	return remoteprotocol.LocalStatus{
		Enabled:       true,
		HTTPURL:       r.httpURL,
		LocalWebAddr:  nonEmpty(r.actualWebAddr, r.localWebAddr),
		LocalPairURL:  r.localPairURL,
		ICETCPEnabled: r.iceTCPMux != nil,
		ICETCPAddr:    r.iceTCPAddr,
		ICETCPPort:    r.iceTCPPort,
		UpdatedAt:     r.updatedAt,
	}
}

func (r *localRuntime) close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.cancel != nil {
		r.cancel()
	}
	var err error
	if r.shutdown != nil {
		err = r.shutdown(ctx)
	}
	if r.iceTCPMux != nil {
		if closeErr := r.iceTCPMux.Close(); err == nil && closeErr != nil && !isClosedNetworkError(closeErr) {
			err = closeErr
		}
	}
	return err
}

func newEmbeddedLocalHub(ctx context.Context, params remoteprotocol.LocalEnableParams, cfg remoteconfig.Config) (*localRuntime, error) {
	cfg = remoteconfig.Normalize(cfg)
	listenAddr := strings.TrimSpace(params.LocalWebAddr)
	if listenAddr == "" {
		listenAddr = "0.0.0.0:18888"
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	actualAddr := listener.Addr().String()
	advertisedHost := localHubAdvertisedHost(listener.Addr())
	port, err := portFromAddr(listener.Addr())
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	reg := registry.New(registry.Config{AgentTTL: 2 * time.Minute})
	cloudSvc := cloud.NewService(cloud.Config{Registry: reg})
	hubHandler := httpapi.NewHandler(httpapi.Config{
		Cloud:          cloudSvc,
		Registry:       reg,
		AnswerTimeout:  250 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		LocalDiscovery: true,
	})
	allowedNets, err := httpapi.ParseLANIPs(cfg.LANIPs)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("parse lan_ips: %w", err)
	}
	lanFilter := httpapi.NewLANFilter(cfg.AllowLAN, allowedNets)
	httpServer := &http.Server{Handler: lanFilter(hubHandler)}
	grpcServer := grpcadapter.NewServer(reg, cloudSvc, nil)
	mux := cmux.New(listener)
	grpcListener := mux.Match(cmux.HTTP2())
	httpListener := mux.Match(cmux.HTTP1Fast())
	iceListener := mux.Match(cmux.Any())
	iceMux, err := remotertc.NewLocalICETCPMuxFromListener(iceListener)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 3)
	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, net.ErrClosed) && !isClosedNetworkError(err) {
			done <- err
			cancel()
			return
		}
		done <- nil
	}()
	go func() {
		if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			done <- err
			cancel()
			return
		}
		done <- nil
	}()
	go func() {
		if err := mux.Serve(); err != nil && !errors.Is(err, net.ErrClosed) && !strings.Contains(strings.ToLower(err.Error()), "use of closed network connection") {
			done <- err
			cancel()
			return
		}
		done <- nil
	}()

	rt := &localRuntime{
		localWebAddr:  listenAddr,
		iceTCPAddr:    net.JoinHostPort(advertisedHost, strconv.Itoa(port)),
		httpURL:       "http://" + net.JoinHostPort(advertisedHost, strconv.Itoa(port)),
		actualWebAddr: actualAddr,
		localPairURL:  "http://" + net.JoinHostPort(advertisedHost, strconv.Itoa(port)) + "/api/v1/pairing/claims",
		iceTCPPort:    port,
		iceTCPMux:     iceMux,
		listener:      listener,
		httpServer:    httpServer,
		cancel:        cancel,
		updatedAt:     time.Now().UTC(),
	}
	rt.shutdown = func(shutdownCtx context.Context) error {
		if shutdownCtx == nil {
			shutdownCtx = context.Background()
		}
		cancel()
		var err error
		if grpcServer != nil {
			grpcServer.GracefulStop()
		}
		if httpServer != nil {
			err = httpServer.Shutdown(shutdownCtx)
			if isClosedNetworkError(err) {
				err = nil
			}
		}
		if listener != nil {
			if closeErr := listener.Close(); err == nil && closeErr != nil && !isClosedNetworkError(closeErr) {
				err = closeErr
			}
		}
		select {
		case <-runCtx.Done():
		default:
		}
		return err
	}
	return rt, nil
}

func (s *Service) attachManagerToLocalHub(ctx context.Context, local *localRuntime) {
	if s == nil || s.manager == nil || local == nil {
		return
	}
	s.manager.AddHubURLWithAnswerOptions(local.httpURL, remotertc.AnswerOptions{SettingEngine: local.iceTCPMux})
	if status := s.manager.Status(); status.State == runtime.StateDisabled {
		_ = s.manager.Start(ctx)
	}
}

func (s *Service) attachManagerToCloud(params remoteprotocol.LocalEnableParams, localHubURL string) {
	if s == nil || s.manager == nil {
		return
	}
	localHubURL = strings.TrimSpace(localHubURL)
	for _, hubURL := range params.HubURLs {
		hubURL = strings.TrimSpace(hubURL)
		if hubURL == "" || hubURL == localHubURL {
			continue
		}
		s.manager.AddExplicitHubURL(hubURL)
	}
	s.manager.ConfigureCloud(params.ControlURL, params.AccessToken, params.Region)
}

func (s *Service) detachManagerFromLocalHub(local *localRuntime) {
	if s == nil || s.manager == nil || local == nil {
		return
	}
	s.manager.DetachHub(local.httpURL)
}

func localHubAdvertisedHost(addr net.Addr) string {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil || tcpAddr.IP.IsUnspecified() {
		return localHubLANIP()
	}
	return tcpAddr.IP.String()
}

func localHubLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			return ip.String()
		}
	}
	return "127.0.0.1"
}

func portFromAddr(addr net.Addr) (int, error) {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr.Port <= 0 {
		return 0, fmt.Errorf("local hub listener has invalid address %q", addr.String())
	}
	return tcpAddr.Port, nil
}

func isClosedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, net.ErrClosed) || strings.Contains(strings.ToLower(err.Error()), "use of closed network connection")
}

type localRTCTransportSink struct {
	service    *Service
	terminalID string
}

func (s localRTCTransportSink) ServeRemoteTransport(ctx context.Context, t transport.Transport, remote string) error {
	if s.service == nil || s.service.daemon == nil {
		return nil
	}
	if scoped, ok := s.service.daemon.(ScopedDaemon); ok {
		return scoped.ServeScopedTransport(ctx, t, remote, TransportScope{TerminalID: s.terminalID})
	}
	return s.service.daemon.ServeTransport(ctx, t, remote)
}

type inventoryProvider struct {
	service *Service
}

func (p inventoryProvider) ListRemoteTerminals(ctx context.Context) []runtime.TerminalInventoryItem {
	if p.service == nil || p.service.daemon == nil {
		return nil
	}
	list, err := p.service.daemon.List(ctx)
	if err != nil {
		return nil
	}
	out := make([]runtime.TerminalInventoryItem, 0, len(list.Terminals))
	for _, item := range list.Terminals {
		out = append(out, terminalInventoryFromProtocol(item))
	}
	return out
}

func (p inventoryProvider) ServeRemoteTransport(ctx context.Context, t transport.Transport, remote string) error {
	if p.service == nil || p.service.daemon == nil {
		return nil
	}
	return p.service.daemon.ServeTransport(ctx, t, remote)
}

func (p inventoryProvider) RouteTerminalManagementRequest(ctx context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	return terminalManagementRouter{service: p.service}.RouteTerminalManagementRequest(ctx, req)
}

func (p inventoryProvider) SubscribeRemoteEvents(ctx context.Context, filters remotertc.EventFilters) (<-chan []byte, func(), error) {
	if p.service == nil || p.service.daemon == nil {
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
	events, err := p.service.daemon.Events(ctx, params)
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
				payload, err := json.Marshal(evt)
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

type terminalManagementRouter struct {
	service *Service
}

func (r terminalManagementRouter) RouteTerminalManagementRequest(ctx context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	switch req.Path {
	case "list":
		terminals, err := r.listTerminals(ctx)
		if err != nil {
			return http.StatusInternalServerError, nil, err.Error()
		}
		return marshalRuntimeAPIResponse(map[string]any{"terminals": terminals})
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
	if r.service == nil || r.service.daemon == nil {
		return nil, nil
	}
	list, err := r.service.daemon.List(ctx)
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
	if r.service == nil || r.service.daemon == nil {
		return runtime.TerminalInventoryItem{}, nil
	}
	created, err := r.service.daemon.Create(ctx, protocol.CreateParams{
		Command: append([]string(nil), command...),
		Name:    strings.TrimSpace(name),
		Tags:    localTerminalTags(dir, environment, sizeLockMode),
		Dir:     strings.TrimSpace(dir),
	})
	if err != nil {
		return runtime.TerminalInventoryItem{}, err
	}
	info, err := r.service.daemon.Get(ctx, created.TerminalID)
	if err == nil && info != nil {
		return terminalInventoryFromProtocol(*info), nil
	}
	return runtime.TerminalInventoryItem{
		ID:      created.TerminalID,
		Name:    strings.TrimSpace(name),
		Command: append([]string(nil), command...),
		State:   created.State,
	}, nil
}

func (r terminalManagementRouter) updateTerminal(ctx context.Context, terminalID string, name string, cwd string, environment string, sizeLockMode string) (runtime.TerminalInventoryItem, error) {
	if r.service == nil || r.service.daemon == nil {
		return runtime.TerminalInventoryItem{}, nil
	}
	info, err := r.service.daemon.Get(ctx, terminalID)
	if err != nil {
		return runtime.TerminalInventoryItem{}, err
	}
	tags := copyTags(info.Tags)
	if tags == nil {
		tags = map[string]string{}
	}
	mergeLocalTerminalTags(tags, cwd, environment, sizeLockMode)
	if err := r.service.daemon.SetMetadata(ctx, terminalID, strings.TrimSpace(name), tags); err != nil {
		return runtime.TerminalInventoryItem{}, err
	}
	updated, err := r.service.daemon.Get(ctx, terminalID)
	if err != nil {
		return runtime.TerminalInventoryItem{}, err
	}
	return terminalInventoryFromProtocol(*updated), nil
}

func (r terminalManagementRouter) removeTerminal(ctx context.Context, terminalID string) error {
	if r.service == nil || r.service.daemon == nil {
		return nil
	}
	return r.service.daemon.Remove(ctx, terminalID)
}

func (s *Service) pairClaim(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	if s == nil {
		return pairing.ClaimResponse{}, nil
	}
	if err := ctx.Err(); err != nil {
		return pairing.ClaimResponse{}, err
	}
	if strings.TrimSpace(req.PairSessionID) == "" {
		return pairing.ClaimResponse{}, pairingErr("pair_session_id is required")
	}
	s.pairMu.Lock()
	manager := s.pairing
	s.pairMu.Unlock()
	if manager == nil {
		return pairing.ClaimResponse{}, pairingErr("pair session not found")
	}
	return manager.ClaimSession(req)
}

type pairClaimer struct {
	service *Service
}

func (p pairClaimer) ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	if p.service == nil {
		return pairing.ClaimResponse{}, pairingErr("remote service is nil")
	}
	return p.service.pairClaim(ctx, req)
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
	return string(a.MachineSecret) != string(b.MachineSecret)
}

func terminalInventoryFromProtocol(item protocol.TerminalInfo) runtime.TerminalInventoryItem {
	return runtime.TerminalInventoryItem{
		ID:      item.ID,
		Name:    item.Name,
		State:   item.State,
		Command: append([]string(nil), item.Command...),
		Cols:    int(item.Size.Cols),
		Rows:    int(item.Size.Rows),
	}
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
