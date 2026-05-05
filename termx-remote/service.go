package remote

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
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
	"github.com/lozzow/termx/termx-remote/cert"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/hub/sessionflow"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/localweb"
	"github.com/lozzow/termx/termx-remote/pairing"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
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

	pairMu  sync.Mutex
	pairing *pairing.Manager
	pairCfg pairing.Config

	rtcMu     sync.Mutex
	rtcReplay *cert.ReplayWindow
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
	s.manager.SetPairClaimer(localWebAdapter{service: s})
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
		DataDir:       status.DataDir,
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
	machineKey, err := identity.LoadOrCreateMachineKey(cfg.DataDir)
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
		MachineID:    machineID,
		MachineName:  machineName,
		MachineKey:   machineKey,
		LocalPairURL: strings.TrimSpace(params.LocalPairURL),
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
		Type:                        session.Type,
		MachineID:                   session.MachineID,
		MachineName:                 session.MachineName,
		MachinePublicKeyFingerprint: session.MachinePublicKeyFingerprint,
		LocalPairURL:                session.LocalPairURL,
		PairSessionID:               session.PairSessionID,
		PairSecret:                  session.PairSecret,
		ExpiresAt:                   session.ExpiresAt,
	}, nil
}

func NewLocalWebStaticAssets(files map[string]string) fs.FS {
	return localweb.NewStaticAssets(files)
}

func EmbeddedLocalWebAssets() fs.FS {
	return localweb.EmbeddedAssets()
}

type LocalICETCPMux = remotertc.LocalICETCPMux

func StartLocalICETCPMux(ctx context.Context, addr string) (*LocalICETCPMux, error) {
	return remotertc.StartLocalICETCPMux(ctx, addr)
}

type LocalWebOptions struct {
	HTTPURL                string
	LocalPairURL           string
	ICETCPEnabled          bool
	ICETCPPort             int
	ICETCPMux              *LocalICETCPMux
	LocalRTCSessionContext context.Context
	Assets                 fs.FS
}

func (s *Service) LocalWebHandler(opts LocalWebOptions) http.Handler {
	assets := opts.Assets
	if assets == nil {
		assets = localweb.EmbeddedAssets()
	}
	iceTCPEnabled := opts.ICETCPEnabled
	iceTCPPort := opts.ICETCPPort
	if opts.ICETCPMux != nil {
		endpoint := opts.ICETCPMux.Endpoint()
		iceTCPEnabled = endpoint.Enabled
		iceTCPPort = endpoint.Port
	}
	rtcSessionCtx := opts.LocalRTCSessionContext
	if rtcSessionCtx == nil && s != nil {
		rtcSessionCtx = s.rtcCtx
	}
	adapter := localWebAdapter{
		service:       s,
		httpURL:       strings.TrimSpace(opts.HTTPURL),
		iceTCPEnabled: iceTCPEnabled,
		iceTCPPort:    iceTCPPort,
		iceTCPMux:     opts.ICETCPMux,
		rtcSessionCtx: rtcSessionCtx,
	}
	return localweb.NewHandler(localweb.Config{
		Assets:           assets,
		Status:           adapter,
		Terminals:        adapter,
		TerminalsManager: adapter,
		Pairing:          adapter,
		RTC:              adapter,
	})
}

func (s *Service) LocalEnable(ctx context.Context, params remoteprotocol.LocalEnableParams) (remoteprotocol.LocalStatus, error) {
	if s == nil {
		return remoteprotocol.LocalStatus{}, fmt.Errorf("remote service is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	localWebAddr := strings.TrimSpace(params.LocalWebAddr)
	if localWebAddr == "" {
		localWebAddr = "127.0.0.1:18888"
	}
	iceTCPAddr := strings.TrimSpace(params.ICETCPAddr)
	if iceTCPAddr == "" {
		iceTCPAddr = "127.0.0.1:18889"
	}

	s.localMu.Lock()
	defer s.localMu.Unlock()
	if s.local != nil && s.local.localWebAddr == localWebAddr && s.local.iceTCPAddr == iceTCPAddr {
		return s.local.statusLocked(), nil
	}
	runtime, err := s.newLocalRuntime(ctx, localWebAddr, iceTCPAddr)
	if err != nil {
		return remoteprotocol.LocalStatus{}, err
	}
	old := s.local
	s.local = runtime
	status := runtime.statusLocked()
	if old != nil {
		go func() {
			_ = old.close(context.Background())
		}()
	}
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
		if err := runtime.close(ctx); err != nil {
			return remoteprotocol.LocalStatus{}, err
		}
	}
	return remoteprotocol.LocalStatus{UpdatedAt: time.Now().UTC()}, nil
}

func (s *Service) newLocalRuntime(ctx context.Context, localWebAddr string, iceTCPAddr string) (*localRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	iceMux, err := StartLocalICETCPMux(runtimeCtx, iceTCPAddr)
	if err != nil {
		cancel()
		return nil, err
	}
	httpURL, actualWebAddr, shutdown, err := s.startLocalWeb(runtimeCtx, localWebAddr, iceMux)
	if err != nil {
		_ = iceMux.Close()
		cancel()
		return nil, err
	}
	endpoint := iceMux.Endpoint()
	flow := sessionflow.LocalPlan(nil)
	if err := sessionflow.ValidateClientPath(flow.Path); err != nil {
		_ = shutdown(context.Background())
		_ = iceMux.Close()
		cancel()
		return nil, err
	}
	return &localRuntime{
		localWebAddr:  localWebAddr,
		iceTCPAddr:    iceTCPAddr,
		httpURL:       httpURL,
		actualWebAddr: actualWebAddr,
		localPairURL:  httpURL + "/api/local/pair",
		iceTCPPort:    endpoint.Port,
		iceTCPMux:     iceMux,
		shutdown:      shutdown,
		cancel:        cancel,
		updatedAt:     time.Now().UTC(),
	}, nil
}

func (s *Service) startLocalWeb(ctx context.Context, addr string, iceTCPMux *LocalICETCPMux) (string, string, func(context.Context) error, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", "", nil, fmt.Errorf("remote local web address is required")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", "", nil, err
	}

	baseURL := localWebBaseURL(listener.Addr())
	actualAddr := listener.Addr().String()
	httpServer := &http.Server{
		Handler: s.LocalWebHandler(LocalWebOptions{
			HTTPURL:                baseURL,
			LocalPairURL:           baseURL + "/api/local/pair",
			ICETCPMux:              iceTCPMux,
			LocalRTCSessionContext: ctx,
		}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			return
		}
	}()

	var once sync.Once
	var shutdownErr error
	shutdown := func(shutdownCtx context.Context) error {
		once.Do(func() {
			shutdownErr = httpServer.Shutdown(shutdownCtx)
			if shutdownErr != nil {
				_ = httpServer.Close()
			}
			<-done
		})
		return shutdownErr
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	return baseURL, actualAddr, shutdown, nil
}

type localRuntime struct {
	localWebAddr  string
	iceTCPAddr    string
	httpURL       string
	actualWebAddr string
	localPairURL  string
	iceTCPPort    int
	iceTCPMux     *LocalICETCPMux
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
		if closeErr := r.iceTCPMux.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

type localWebAdapter struct {
	service       *Service
	httpURL       string
	iceTCPEnabled bool
	iceTCPPort    int
	iceTCPMux     *LocalICETCPMux
	rtcSessionCtx context.Context
}

func (a localWebAdapter) LocalStatus(ctx context.Context) (localweb.Status, error) {
	_ = ctx
	if a.service == nil {
		return localweb.Status{}, nil
	}
	status := a.service.Status()
	cfg := a.service.normalizedConfig()
	machineID := strings.TrimSpace(status.DeviceID)
	machineName := strings.TrimSpace(status.DeviceName)
	machineKey, err := identity.LoadOrCreateMachineKey(cfg.DataDir)
	if err != nil {
		return localweb.Status{}, err
	}
	fingerprint := identity.MachinePublicKeyFingerprint(machineKey.PublicKey)
	if machineID == "" || machineName == "" {
		ident, identErr := identity.LoadOrCreate(cfg.DataDir, cfg.DeviceName)
		if identErr != nil {
			return localweb.Status{}, identErr
		}
		if machineID == "" {
			machineID = ident.DeviceID
		}
		if machineName == "" {
			machineName = ident.DisplayName
		}
	}
	return localweb.Status{
		MachineID:                   machineID,
		MachineName:                 machineName,
		MachinePublicKeyFingerprint: fingerprint,
		RemoteEnabled:               status.State != remoteprotocol.StateDisabled,
		LocalRTC: localweb.LocalRTCStatus{
			HTTPURL:       a.httpURL,
			ICETCPEnabled: a.iceTCPEnabled,
			ICETCPPort:    a.iceTCPPort,
		},
		UpdatedAt: status.UpdatedAt,
	}, nil
}

func (a localWebAdapter) ListTerminals(ctx context.Context) ([]localweb.Terminal, error) {
	if a.service == nil || a.service.daemon == nil {
		return nil, nil
	}
	list, err := a.service.daemon.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]localweb.Terminal, 0, len(list.Terminals))
	for _, item := range list.Terminals {
		out = append(out, localwebTerminalFromProtocol(item))
	}
	return out, nil
}

func (a localWebAdapter) CreateLocalTerminal(ctx context.Context, req localweb.CreateTerminalRequest) (localweb.Terminal, error) {
	if a.service == nil || a.service.daemon == nil {
		return localweb.Terminal{}, nil
	}
	size := protocol.Size{}
	if req.Cols > 0 {
		size.Cols = uint16(req.Cols)
	}
	if req.Rows > 0 {
		size.Rows = uint16(req.Rows)
	}
	created, err := a.service.daemon.Create(ctx, protocol.CreateParams{
		Command: req.Command,
		Name:    strings.TrimSpace(req.Name),
		Tags:    localTerminalTags(req.CWD, req.Environment, req.SizeLockMode),
		Size:    size,
		Dir:     strings.TrimSpace(req.CWD),
	})
	if err != nil {
		return localweb.Terminal{}, err
	}
	info, err := a.service.daemon.Get(ctx, created.TerminalID)
	if err == nil && info != nil {
		return localwebTerminalFromProtocol(*info), nil
	}
	return localweb.Terminal{
		TerminalID: created.TerminalID,
		Name:       strings.TrimSpace(req.Name),
		Command:    append([]string(nil), req.Command...),
		Cols:       int(size.Cols),
		Rows:       int(size.Rows),
		State:      created.State,
	}, nil
}

func (a localWebAdapter) UpdateLocalTerminal(ctx context.Context, terminalID string, req localweb.UpdateTerminalRequest) (localweb.Terminal, error) {
	if a.service == nil || a.service.daemon == nil {
		return localweb.Terminal{}, nil
	}
	info, err := a.service.daemon.Get(ctx, terminalID)
	if err != nil {
		return localweb.Terminal{}, err
	}
	tags := copyTags(info.Tags)
	if tags == nil {
		tags = map[string]string{}
	}
	mergeLocalTerminalTags(tags, req.CWD, req.Environment, req.SizeLockMode)
	if err := a.service.daemon.SetMetadata(ctx, terminalID, strings.TrimSpace(req.Name), tags); err != nil {
		return localweb.Terminal{}, err
	}
	updated, err := a.service.daemon.Get(ctx, terminalID)
	if err != nil {
		return localweb.Terminal{}, err
	}
	return localwebTerminalFromProtocol(*updated), nil
}

func (a localWebAdapter) DeleteLocalTerminal(ctx context.Context, terminalID string) error {
	if a.service == nil || a.service.daemon == nil {
		return nil
	}
	return a.service.daemon.Remove(ctx, terminalID)
}

func (a localWebAdapter) ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	if a.service == nil {
		return pairing.ClaimResponse{}, nil
	}
	return a.service.pairClaim(ctx, req)
}

func (a localWebAdapter) AnswerLocalRTCOffer(ctx context.Context, req localweb.RTCOfferRequest) (localweb.RTCOfferResponse, error) {
	if a.service == nil {
		return localweb.RTCOfferResponse{}, nil
	}
	return a.service.localRTCAnswer(ctx, req, a.iceTCPMux, a.iceTCPEnabled, a.rtcSessionCtx)
}

func (s *Service) localRTCAnswer(
	ctx context.Context,
	req localweb.RTCOfferRequest,
	iceTCPMux *LocalICETCPMux,
	iceTCPEnabled bool,
	sessionCtx context.Context,
) (localweb.RTCOfferResponse, error) {
	terminalID := strings.TrimSpace(req.Offer.TerminalID)
	if terminalID == "" {
		if strings.TrimSpace(req.Client.Purpose) != "inventory_events" {
			allowTerminal := hasCapability(req.AppCertificate.Payload.Capabilities, "terminal")
			allowFileManager := hasCapability(req.AppCertificate.Payload.Capabilities, "file_manager")
			allowTerminalManagement := hasCapability(req.AppCertificate.Payload.Capabilities, "terminal_management")
			if !allowTerminal && !allowTerminalManagement {
				return localweb.RTCOfferResponse{}, pairingErr("app certificate lacks machine remote RTC capability")
			}
			dataChannels := []string{"api"}
			if allowTerminal {
				dataChannels = append(dataChannels, "terminal:{terminal_id}", "events")
			}
			if allowFileManager {
				dataChannels = append(dataChannels, "file:{transfer_id}")
			}
			return s.localRTCAnswerWithScope(
				ctx, req, iceTCPMux, iceTCPEnabled, sessionCtx,
				"", allowTerminal, allowFileManager, allowTerminalManagement, allowTerminal,
				dataChannels,
			)
		}
		if !hasCapability(req.AppCertificate.Payload.Capabilities, "terminal") {
			return localweb.RTCOfferResponse{}, pairingErr("app certificate lacks machine inventory capability")
		}
		return s.localRTCAnswerWithScope(
			ctx, req, iceTCPMux, iceTCPEnabled, sessionCtx,
			"", false, false, false, true,
			[]string{"events"},
		)
	}
	if !hasCapability(req.AppCertificate.Payload.Capabilities, "terminal") {
		return localweb.RTCOfferResponse{}, pairingErr("app certificate lacks remote RTC capabilities")
	}
	allowFileManager := hasCapability(req.AppCertificate.Payload.Capabilities, "file_manager")
	allowTerminalManagement := hasCapability(req.AppCertificate.Payload.Capabilities, "terminal_management")
	dataChannels := []string{"api", "terminal:{terminal_id}"}
	if allowFileManager {
		dataChannels = append(dataChannels, "file:{transfer_id}")
	}
	return s.localRTCAnswerWithScope(
		ctx, req, iceTCPMux, iceTCPEnabled, sessionCtx,
		terminalID, true, allowFileManager, allowTerminalManagement, false,
		dataChannels,
	)
}

func (s *Service) localRTCAnswerWithScope(
	ctx context.Context,
	req localweb.RTCOfferRequest,
	iceTCPMux *LocalICETCPMux,
	iceTCPEnabled bool,
	sessionCtx context.Context,
	terminalID string,
	allowTerminal bool,
	allowFileManager bool,
	allowTerminalManagement bool,
	allowEvents bool,
	dataChannels []string,
) (localweb.RTCOfferResponse, error) {
	if s == nil {
		return localweb.RTCOfferResponse{}, nil
	}
	status := s.Status()
	machineID := strings.TrimSpace(status.DeviceID)
	cfg := s.normalizedConfig()
	machineKey, err := identity.LoadOrCreateMachineKey(cfg.DataDir)
	if err != nil {
		return localweb.RTCOfferResponse{}, err
	}
	if machineID == "" {
		ident, identErr := identity.LoadOrCreate(cfg.DataDir, cfg.DeviceName)
		if identErr != nil {
			return localweb.RTCOfferResponse{}, identErr
		}
		machineID = strings.TrimSpace(ident.DeviceID)
	}
	if err := cert.VerifyAppCertificate(req.AppCertificate, machineKey.PublicKey, time.Now().UTC()); err != nil {
		return localweb.RTCOfferResponse{}, err
	}
	if strings.TrimSpace(req.AppCertificate.Payload.MachineID) != machineID {
		return localweb.RTCOfferResponse{}, pairingErr("app certificate machine_id does not match local machine")
	}
	if strings.TrimSpace(req.Offer.MachineID) != strings.TrimSpace(req.AppCertificate.Payload.MachineID) {
		return localweb.RTCOfferResponse{}, pairingErr("offer machine_id does not match app certificate")
	}
	if terminalID != "" && s.daemon != nil {
		if _, err := s.daemon.Get(ctx, terminalID); err != nil {
			return localweb.RTCOfferResponse{}, err
		}
	}
	appPublicKey, err := base64.StdEncoding.DecodeString(req.AppCertificate.Payload.AppPublicKey)
	if err != nil {
		return localweb.RTCOfferResponse{}, err
	}
	s.rtcMu.Lock()
	if s.rtcReplay == nil {
		s.rtcReplay = cert.NewReplayWindow(5 * time.Minute)
	}
	replay := s.rtcReplay
	if s.rtcFiles == nil {
		s.rtcFiles = fileapi.NewManager()
	}
	files := s.rtcFiles
	s.rtcMu.Unlock()
	if err := remotertc.VerifyOfferSignature(remotertc.OfferSignature{
		Algorithm: req.Signature.Algorithm,
		Nonce:     req.Signature.Nonce,
		Timestamp: req.Signature.Timestamp,
		Value:     req.Signature.Value,
	}, remotertc.OfferSignatureFields{
		MachineID:  req.Offer.MachineID,
		TerminalID: req.Offer.TerminalID,
		SDP:        req.Offer.SDP,
		Candidates: req.Offer.ICECandidates,
	}, ed25519.PublicKey(appPublicKey), replay, time.Now().UTC()); err != nil {
		return localweb.RTCOfferResponse{}, err
	}
	answer, err := sessionflow.AnswerLocal(ctx, nil, sessionflow.AnswerInput{
		Plan: sessionflow.LocalPlan(nil),
		Offer: hubv1.SignalingOffer{
			SessionID:     strings.TrimSpace(req.Offer.SessionID),
			DeviceID:      strings.TrimSpace(req.Offer.MachineID),
			TerminalID:    terminalID,
			SDP:           req.Offer.SDP,
			ICECandidates: append([]string(nil), req.Offer.ICECandidates...),
		},
		Sink:  localRTCTransportSink{service: s, terminalID: terminalID},
		Files: files,
		Options: remotertc.AnswerOptions{
			SettingEngine: iceTCPMux,
			ChannelPolicy: remotertc.ChannelPolicy{
				TerminalID:              terminalID,
				AllowTerminal:           allowTerminal,
				AllowAPI:                true,
				AllowFileManager:        allowFileManager,
				AllowTerminalManagement: allowTerminalManagement,
				AllowEvents:             allowEvents,
			},
			TerminalManagement: localRTCManagementRouter{adapter: localWebAdapter{service: s}},
			Events:             inventoryProvider{service: s},
			SessionContext:     sessionCtx,
		},
	})
	if err != nil {
		return localweb.RTCOfferResponse{}, err
	}
	return localweb.RTCOfferResponse{
		Answer: localweb.RTCSessionAnswer{
			SessionID:     answer.SessionID,
			SDP:           answer.SDP,
			ICECandidates: append([]string(nil), answer.ICECandidates...),
		},
		ICETCPEnabled: iceTCPEnabled,
		DataChannels:  dataChannels,
		Capabilities: localweb.RTCCapabilities{
			TerminalAllowed:           allowTerminal,
			APIAllowed:                allowFileManager || allowTerminalManagement,
			EventsAllowed:             allowEvents,
			FileTransferAllowed:       allowFileManager,
			TerminalManagementAllowed: allowTerminalManagement,
			RelayInUse:                false,
		},
	}, nil
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
		out = append(out, runtime.TerminalInventoryItem{
			ID:      item.ID,
			Name:    item.Name,
			State:   item.State,
			Command: append([]string(nil), item.Command...),
			Cols:    int(item.Size.Cols),
			Rows:    int(item.Size.Rows),
		})
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
	return localRTCManagementRouter{adapter: localWebAdapter{service: p.service}}.RouteTerminalManagementRequest(ctx, req)
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

type localRTCManagementRouter struct {
	adapter localWebAdapter
}

func (r localRTCManagementRouter) RouteTerminalManagementRequest(ctx context.Context, req remotertc.TerminalManagementRequest) (int32, []byte, string) {
	switch req.Path {
	case "list":
		terminals, err := r.adapter.ListTerminals(ctx)
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
		terminal, err := r.adapter.CreateLocalTerminal(ctx, localweb.CreateTerminalRequest{
			Name:         body.Name,
			Command:      append([]string(nil), body.Command...),
			CWD:          body.Dir,
			Environment:  firstNonEmpty(body.Env),
			SizeLockMode: body.Tags[terminalmeta.SizeLockTag],
		})
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
		terminal, err := r.adapter.UpdateLocalTerminal(ctx, terminalID, localweb.UpdateTerminalRequest{
			Name:         body.Name,
			CWD:          body.Tags["cwd"],
			Environment:  body.Tags["environment"],
			SizeLockMode: body.Tags[terminalmeta.SizeLockTag],
		})
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
		if err := r.adapter.DeleteLocalTerminal(ctx, terminalID); err != nil {
			return http.StatusBadRequest, nil, err.Error()
		}
		return http.StatusOK, []byte(`{}`), ""
	default:
		return http.StatusNotFound, nil, "unknown terminal management route"
	}
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
		AccessToken: cfg.AccessToken,
		DataDir:     cfg.DataDir,
		DeviceName:  cfg.DeviceName,
		Region:      cfg.Region,
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
	if a.MachineID != b.MachineID || a.MachineName != b.MachineName || a.LocalPairURL != b.LocalPairURL {
		return true
	}
	return !a.MachineKey.PublicKey.Equal(b.MachineKey.PublicKey)
}

func localwebTerminalFromProtocol(item protocol.TerminalInfo) localweb.Terminal {
	sizeLockMode := terminalmeta.SizeLockMode(item.Tags)
	return localweb.Terminal{
		TerminalID:   item.ID,
		Name:         item.Name,
		Command:      append([]string(nil), item.Command...),
		Cols:         int(item.Size.Cols),
		Rows:         int(item.Size.Rows),
		State:        item.State,
		LastActiveAt: item.CreatedAt,
		SizeLocked:   sizeLockMode == terminalmeta.SizeLockLock,
		SizeLockMode: sizeLockMode,
		CWD:          item.Tags["termx.cwd"],
		Environment:  item.Tags["termx.environment"],
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

func localWebBaseURL(addr net.Addr) string {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return "http://" + addr.String()
	}
	host := tcpAddr.IP.String()
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(tcpAddr.Port))
}
