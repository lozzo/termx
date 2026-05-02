package termx

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/cert"
	remoteconfig "github.com/lozzow/termx/termx-core/internal/remote/config"
	"github.com/lozzow/termx/termx-core/internal/remote/fileapi"
	"github.com/lozzow/termx/termx-core/internal/remote/identity"
	"github.com/lozzow/termx/termx-core/internal/remote/localweb"
	"github.com/lozzow/termx/termx-core/internal/remote/pairing"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/lozzow/termx/termx-core/terminalmeta"
	"github.com/lozzow/termx/termx-core/transport"
)

type LocalICETCPMux = remotertc.LocalICETCPMux

type LocalWebOptions struct {
	HTTPURL                string
	LocalPairURL           string
	ICETCPEnabled          bool
	ICETCPPort             int
	ICETCPMux              *LocalICETCPMux
	LocalRTCSessionContext context.Context
	Assets                 fs.FS
}

func NewLocalWebStaticAssets(files map[string]string) fs.FS {
	return localweb.NewStaticAssets(files)
}

func StartLocalICETCPMux(ctx context.Context, addr string) (*LocalICETCPMux, error) {
	return remotertc.StartLocalICETCPMux(ctx, addr)
}

func (s *Server) LocalWebHandler(opts LocalWebOptions) http.Handler {
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
		rtcSessionCtx = s.remoteRTCCtx
	}
	adapter := localWebServerAdapter{
		server:        s,
		httpURL:       strings.TrimSpace(opts.HTTPURL),
		iceTCPEnabled: iceTCPEnabled,
		iceTCPPort:    iceTCPPort,
		iceTCPMux:     opts.ICETCPMux,
		rtcSessionCtx: rtcSessionCtx,
	}
	return localweb.NewHandler(localweb.Config{
		Assets:          assets,
		Status:          adapter,
		Terminals:       adapter,
		TerminalsManager: adapter,
		Pairing:         adapter,
		RTC:             adapter,
	})
}

type localWebServerAdapter struct {
	server        *Server
	httpURL       string
	iceTCPEnabled bool
	iceTCPPort    int
	iceTCPMux     *LocalICETCPMux
	rtcSessionCtx context.Context
}

func (a localWebServerAdapter) LocalStatus(ctx context.Context) (localweb.Status, error) {
	_ = ctx
	if a.server == nil {
		return localweb.Status{}, nil
	}
	status := a.server.RemoteStatus()
	cfg := a.server.remoteNormalizedConfig()
	machineID := strings.TrimSpace(status.DeviceID)
	machineName := strings.TrimSpace(status.DeviceName)
	fingerprint := ""
	machineKey, err := identity.LoadOrCreateMachineKey(cfg.DataDir)
	if err != nil {
		return localweb.Status{}, err
	}
	fingerprint = identity.MachinePublicKeyFingerprint(machineKey.PublicKey)
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
		RemoteEnabled:               status.State != RemoteStateDisabled,
		LocalRTC: localweb.LocalRTCStatus{
			HTTPURL:       a.httpURL,
			ICETCPEnabled: a.iceTCPEnabled,
			ICETCPPort:    a.iceTCPPort,
		},
		UpdatedAt: status.UpdatedAt,
	}, nil
}

func (a localWebServerAdapter) ListTerminals(ctx context.Context) ([]localweb.Terminal, error) {
	if a.server == nil {
		return nil, nil
	}
	list, err := a.server.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]localweb.Terminal, 0, len(list))
	for _, item := range list {
		sizeLockMode := terminalmeta.SizeLockMode(item.Tags)
		out = append(out, localweb.Terminal{
			TerminalID:   item.ID,
			Name:         item.Name,
			Command:      append([]string(nil), item.Command...),
			Cols:         int(item.Size.Cols),
			Rows:         int(item.Size.Rows),
			State:        string(item.State),
			LastActiveAt: item.CreatedAt,
			SizeLocked:   sizeLockMode == terminalmeta.SizeLockLock,
			SizeLockMode: sizeLockMode,
			CWD:          item.Tags["termx.cwd"],
			Environment:  item.Tags["termx.environment"],
		})
	}
	return out, nil
}

func (a localWebServerAdapter) CreateLocalTerminal(ctx context.Context, req localweb.CreateTerminalRequest) (localweb.Terminal, error) {
	if a.server == nil {
		return localweb.Terminal{}, nil
	}
	size := Size{}
	if req.Cols > 0 {
		size.Cols = uint16(req.Cols)
	}
	if req.Rows > 0 {
		size.Rows = uint16(req.Rows)
	}
	tags := localTerminalTags(req.CWD, req.Environment, req.SizeLockMode)
	created, err := a.server.Create(ctx, CreateOptions{
		Command: req.Command,
		Name:    strings.TrimSpace(req.Name),
		Tags:    tags,
		Size:    size,
		Dir:     strings.TrimSpace(req.CWD),
	})
	if err != nil {
		return localweb.Terminal{}, err
	}
	return a.localTerminalFromInfo(created), nil
}

func (a localWebServerAdapter) UpdateLocalTerminal(ctx context.Context, terminalID string, req localweb.UpdateTerminalRequest) (localweb.Terminal, error) {
	if a.server == nil {
		return localweb.Terminal{}, nil
	}
	info, err := a.server.Get(ctx, terminalID)
	if err != nil {
		return localweb.Terminal{}, err
	}
	tags := copyTags(info.Tags)
	mergeLocalTerminalTags(tags, req.CWD, req.Environment, req.SizeLockMode)
	if err := a.server.SetMetadata(ctx, terminalID, strings.TrimSpace(req.Name), tags); err != nil {
		return localweb.Terminal{}, err
	}
	updated, err := a.server.Get(ctx, terminalID)
	if err != nil {
		return localweb.Terminal{}, err
	}
	return a.localTerminalFromInfo(updated), nil
}

func (a localWebServerAdapter) DeleteLocalTerminal(ctx context.Context, terminalID string) error {
	if a.server == nil {
		return nil
	}
	term, err := a.server.getTerminal(terminalID)
	if err != nil {
		return err
	}
	term.MarkRemoved()
	if err := term.Close(); err != nil {
		return err
	}
	a.server.removeTerminal(terminalID, "removed")
	return nil
}

func (a localWebServerAdapter) ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	_ = ctx
	if a.server == nil {
		return pairing.ClaimResponse{}, nil
	}
	return a.server.remotePairClaim(req)
}

func (a localWebServerAdapter) AnswerLocalRTCOffer(ctx context.Context, req localweb.RTCOfferRequest) (localweb.RTCOfferResponse, error) {
	if a.server == nil {
		return localweb.RTCOfferResponse{}, nil
	}
	return a.server.remoteLocalRTCAnswer(ctx, req, a.iceTCPMux, a.iceTCPEnabled, a.rtcSessionCtx)
}

func (s *Server) remoteLocalRTCAnswer(
	ctx context.Context,
	req localweb.RTCOfferRequest,
	iceTCPMux *LocalICETCPMux,
	iceTCPEnabled bool,
	sessionCtx context.Context,
) (localweb.RTCOfferResponse, error) {
	terminalID := strings.TrimSpace(req.Offer.TerminalID)
	if terminalID == "" {
		if !hasCapability(req.AppCertificate.Payload.Capabilities, "terminal") {
			return localweb.RTCOfferResponse{}, pairingErr("app certificate lacks machine inventory capability")
		}
		return s.remoteLocalRTCAnswerWithScope(
			ctx, req, iceTCPMux, iceTCPEnabled, sessionCtx,
			"", false, false, true,
			[]string{"events"},
		)
	}
	if !hasCapability(req.AppCertificate.Payload.Capabilities, "terminal") || !hasCapability(req.AppCertificate.Payload.Capabilities, "file_manager") {
		return localweb.RTCOfferResponse{}, pairingErr("app certificate lacks remote RTC capabilities")
	}
	return s.remoteLocalRTCAnswerWithScope(
		ctx, req, iceTCPMux, iceTCPEnabled, sessionCtx,
		terminalID, true, true, false,
		[]string{"api", "terminal:{terminal_id}", "file:{transfer_id}"},
	)
}

type localRTCTransportSink struct {
	server           *Server
	terminalID       string
	machineEventsOnly bool
}

func (s localRTCTransportSink) ServeRemoteTransport(ctx context.Context, t transport.Transport, remote string) error {
	if s.server == nil {
		return nil
	}
	return s.server.handleTransportScoped(ctx, t, remote, transportScope{
		TerminalID:        s.terminalID,
		MachineEventsOnly: s.machineEventsOnly,
	})
}

func (s *Server) remoteLocalRTCAnswerWithScope(
	ctx context.Context,
	req localweb.RTCOfferRequest,
	iceTCPMux *LocalICETCPMux,
	iceTCPEnabled bool,
	sessionCtx context.Context,
	terminalID string,
	allowTerminal bool,
	allowFileManager bool,
	allowEvents bool,
	dataChannels []string,
) (localweb.RTCOfferResponse, error) {
	if s == nil {
		return localweb.RTCOfferResponse{}, nil
	}
	status := s.RemoteStatus()
	machineID := strings.TrimSpace(status.DeviceID)
	cfg := s.remoteNormalizedConfig()
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
	if terminalID != "" {
		if _, err := s.Get(ctx, terminalID); err != nil {
			return localweb.RTCOfferResponse{}, err
		}
	}
	appPublicKey, err := base64.StdEncoding.DecodeString(req.AppCertificate.Payload.AppPublicKey)
	if err != nil {
		return localweb.RTCOfferResponse{}, err
	}
	s.remoteRTCMu.Lock()
	if s.remoteRTCReplay == nil {
		s.remoteRTCReplay = cert.NewReplayWindow(5 * time.Minute)
	}
	replay := s.remoteRTCReplay
	if s.remoteRTCFiles == nil {
		s.remoteRTCFiles = fileapi.NewManager()
	}
	files := s.remoteRTCFiles
	s.remoteRTCMu.Unlock()
	if err := remotertc.VerifyOfferSignature(remotertc.OfferSignature{
		Algorithm: req.Signature.Algorithm,
		Nonce:     req.Signature.Nonce,
		Timestamp: req.Signature.Timestamp,
		Value:     req.Signature.Value,
	}, remotertc.OfferSignatureFields{
		MachineID:  req.Offer.MachineID,
		TerminalID: req.Offer.TerminalID,
		SDP:        req.Offer.SDP,
	}, ed25519.PublicKey(appPublicKey), replay, time.Now().UTC()); err != nil {
		return localweb.RTCOfferResponse{}, err
	}
	answer, err := remotertc.AnswerOfferWithOptions(ctx, hubv1.SignalingOffer{
		SessionID:     strings.TrimSpace(req.Offer.SessionID),
		DeviceID:      strings.TrimSpace(req.Offer.MachineID),
		TerminalID:    terminalID,
		SDP:           req.Offer.SDP,
		ICECandidates: append([]string(nil), req.Offer.ICECandidates...),
	}, nil, localRTCTransportSink{server: s, terminalID: terminalID, machineEventsOnly: allowEvents}, files, remotertc.AnswerOptions{
		SettingEngine: iceTCPMux,
		ChannelPolicy: remotertc.ChannelPolicy{
			TerminalID:       terminalID,
			AllowTerminal:    allowTerminal,
			AllowFileManager: allowFileManager,
			AllowEvents:      allowEvents,
		},
		SessionContext: sessionCtx,
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
	}, nil
}

func (s *Server) remotePairClaim(req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	if s == nil {
		return pairing.ClaimResponse{}, nil
	}
	if strings.TrimSpace(req.PairSessionID) == "" {
		return pairing.ClaimResponse{}, pairingErr("pair_session_id is required")
	}
	s.remotePairMu.Lock()
	manager := s.remotePairing
	s.remotePairMu.Unlock()
	if manager == nil {
		return pairing.ClaimResponse{}, pairingErr("pair session not found")
	}
	return manager.ClaimSession(req)
}

func (s *Server) remoteNormalizedConfig() remoteLocalConfig {
	if s == nil {
		return remoteLocalConfig{}
	}
	cfg := remoteconfig.Normalize(remoteconfig.Config{
		Enabled:    s.cfg.remoteConfig.Enabled,
		DataDir:    s.cfg.remoteConfig.DataDir,
		DeviceName: s.cfg.remoteConfig.DeviceName,
	})
	normalized := remoteLocalConfig{
		DataDir:    cfg.DataDir,
		DeviceName: cfg.DeviceName,
	}
	return normalized
}

type remoteLocalConfig struct {
	DataDir    string
	DeviceName string
}

type pairingErr string

func (e pairingErr) Error() string { return string(e) }

func hasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == want {
			return true
		}
	}
	return false
}

func (a localWebServerAdapter) localTerminalFromInfo(info *TerminalInfo) localweb.Terminal {
	if info == nil {
		return localweb.Terminal{}
	}
	sizeLockMode := terminalmeta.SizeLockMode(info.Tags)
	return localweb.Terminal{
		TerminalID:   info.ID,
		Name:         info.Name,
		Command:      append([]string(nil), info.Command...),
		Cols:         int(info.Size.Cols),
		Rows:         int(info.Size.Rows),
		State:        string(info.State),
		LastActiveAt: info.CreatedAt,
		SizeLocked:   sizeLockMode == terminalmeta.SizeLockLock,
		SizeLockMode: sizeLockMode,
		CWD:          info.Tags["termx.cwd"],
		Environment:  info.Tags["termx.environment"],
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
