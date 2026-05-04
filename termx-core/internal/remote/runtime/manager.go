package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	"github.com/lozzow/termx/termx-core/internal/remote/cert"
	remoteconfig "github.com/lozzow/termx/termx-core/internal/remote/config"
	"github.com/lozzow/termx/termx-core/internal/remote/discovery"
	"github.com/lozzow/termx/termx-core/internal/remote/fileapi"
	"github.com/lozzow/termx/termx-core/internal/remote/identity"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
)

type State string

const (
	StateDisabled    State = "disabled"
	StateConfigured  State = "configured"
	StateRegistering State = "registering"
	StateOnline      State = "online"
	StateDegraded    State = "degraded"
)

type TerminalInventoryItem struct {
	ID      string
	Name    string
	State   string
	Command []string
	Cols    int
	Rows    int
}

type InventoryProvider interface {
	ListRemoteTerminals(ctx context.Context) []TerminalInventoryItem
}

type Status struct {
	State         State     `json:"state"`
	Detail        string    `json:"detail,omitempty"`
	DeviceID      string    `json:"device_id,omitempty"`
	DeviceName    string    `json:"device_name,omitempty"`
	ControlURL    string    `json:"control_url,omitempty"`
	HubURL        string    `json:"hub_url,omitempty"`
	DataDir       string    `json:"data_dir,omitempty"`
	TerminalCount int       `json:"terminal_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Manager struct {
	cfg      remoteconfig.Config
	provider InventoryProvider
	host     bridge.TransportSink
	files    *fileapi.Manager

	mu               sync.RWMutex
	status           Status
	started          bool
	identity         identity.DeviceIdentity
	cancel           context.CancelFunc
	syncCh           chan struct{}
	hubSessionID     string
	hubRTCServers    []hubv1.RTCIceServerConfig
	signalingStarted bool
	signalingCancel  context.CancelFunc
	explicitHubURL   bool
	answerer         cloudOfferAnswerer
	replay           *cert.ReplayWindow
}

type cloudOfferAnswerer interface {
	AnswerOffer(
		ctx context.Context,
		offer hubv1.SignalingOffer,
		iceServers []hubv1.RTCIceServerConfig,
		sink bridge.TransportSink,
		fileManager *fileapi.Manager,
		opts remotertc.AnswerOptions,
	) (hubv1.SignalingAnswer, error)
}

type defaultCloudOfferAnswerer struct{}

func (defaultCloudOfferAnswerer) AnswerOffer(
	ctx context.Context,
	offer hubv1.SignalingOffer,
	iceServers []hubv1.RTCIceServerConfig,
	sink bridge.TransportSink,
	fileManager *fileapi.Manager,
	opts remotertc.AnswerOptions,
) (hubv1.SignalingAnswer, error) {
	return remotertc.AnswerOfferWithOptions(ctx, offer, iceServers, sink, fileManager, opts)
}

func NewManager(cfg remoteconfig.Config, provider InventoryProvider, host bridge.TransportSink) *Manager {
	cfg = remoteconfig.Normalize(cfg)
	return &Manager{
		cfg:            cfg,
		provider:       provider,
		host:           host,
		files:          fileapi.NewManager(),
		explicitHubURL: strings.TrimSpace(cfg.HubURL) != "",
		answerer:       defaultCloudOfferAnswerer{},
		replay:         cert.NewReplayWindow(5 * time.Minute),
		syncCh:         make(chan struct{}, 1),
		status: Status{
			State:     StateDisabled,
			Detail:    "remote runtime disabled",
			DataDir:   cfg.DataDir,
			UpdatedAt: time.Now().UTC(),
		},
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()

	if !m.cfg.Enabled {
		m.setStatus(StateDisabled, "remote runtime disabled")
		return nil
	}
	if err := m.cfg.Validate(); err != nil {
		m.setStatus(StateDegraded, err.Error())
		return nil
	}

	ident, err := identity.LoadOrCreate(m.cfg.DataDir, m.cfg.DeviceName)
	if err != nil {
		m.setStatus(StateDegraded, err.Error())
		return nil
	}

	m.mu.Lock()
	m.identity = ident
	m.mu.Unlock()

	switch {
	case m.cfg.ControlURL == "" && m.cfg.HubURL == "":
		m.setStatus(StateConfigured, "remote identity ready; waiting for control or hub configuration")
	default:
		state, detail, err := m.reconcile(runCtx)
		if err != nil {
			m.setStatus(StateDegraded, err.Error())
		} else {
			m.setStatus(state, detail)
		}
		go m.reconcileLoop(runCtx)
	}
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	cancel := m.cancel
	signalingCancel := m.signalingCancel
	m.cancel = nil
	m.signalingCancel = nil
	m.mu.Unlock()
	if signalingCancel != nil {
		signalingCancel()
	}
	if cancel != nil {
		cancel()
	}
	if m.files != nil {
		m.files.Close()
	}
}

func (m *Manager) MarkOnline(detail string) {
	m.setStatus(StateOnline, detail)
}

func (m *Manager) MarkDegraded(detail string) {
	m.setStatus(StateDegraded, detail)
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	status := m.status
	m.mu.RUnlock()

	if m.provider != nil {
		status.TerminalCount = len(m.provider.ListRemoteTerminals(context.Background()))
	}
	return status
}

func (m *Manager) TriggerSync() {
	if m == nil {
		return
	}
	select {
	case m.syncCh <- struct{}{}:
	default:
	}
}

func (m *Manager) setStatus(state State, detail string) {
	detail = strings.TrimSpace(detail)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = Status{
		State:         state,
		Detail:        detail,
		DeviceID:      m.identity.DeviceID,
		DeviceName:    firstNonEmpty(m.identity.DisplayName, m.cfg.DeviceName),
		ControlURL:    m.cfg.ControlURL,
		HubURL:        m.cfg.HubURL,
		DataDir:       m.cfg.DataDir,
		TerminalCount: m.status.TerminalCount,
		UpdatedAt:     time.Now().UTC(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (m *Manager) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state, detail, err := m.reconcile(ctx)
			if err != nil {
				m.setStatus(StateDegraded, err.Error())
				continue
			}
			m.setStatus(state, detail)
		case <-m.syncCh:
			state, detail, err := m.reconcile(ctx)
			if err != nil {
				m.setStatus(StateDegraded, err.Error())
				continue
			}
			m.setStatus(state, detail)
		}
	}
}

func (m *Manager) reconcile(ctx context.Context) (State, string, error) {
	if m.cfg.ControlURL != "" {
		if err := m.syncControlRegistration(ctx); err != nil {
			return StateDegraded, "", err
		}
		if err := m.discoverHub(ctx); err != nil {
			return StateDegraded, "", err
		}
	}
	if m.cfg.HubURL != "" {
		if err := m.syncHubPresence(ctx); err != nil {
			return StateDegraded, "", err
		}
		m.ensureHubSignalingLoop(ctx)
		if m.cfg.ControlURL != "" {
			return StateOnline, "device registered in control and hub; terminal signaling active", nil
		}
		return StateOnline, "device registered in hub; control registration not configured", nil
	}
	if m.cfg.ControlURL != "" {
		return StateConfigured, "device registered in control; waiting for hub configuration", nil
	}
	return StateConfigured, "remote identity ready; waiting for control or hub configuration", nil
}

func (m *Manager) discoverHub(ctx context.Context) error {
	if m.cfg.ControlURL == "" || m.cfg.AccessToken == "" {
		return nil
	}
	m.mu.RLock()
	hubURL := m.cfg.HubURL
	preferredRegion := m.cfg.Region
	explicitHubURL := m.explicitHubURL
	m.mu.RUnlock()
	if strings.TrimSpace(hubURL) != "" && explicitHubURL {
		return nil
	}
	hubs, err := discovery.DiscoverHubs(ctx, m.cfg.ControlURL, m.cfg.AccessToken)
	if err != nil {
		return err
	}
	if hub, ok := selectDiscoveredHub(hubs, hubSelectionOptions{
		PreferredRegion: preferredRegion,
		Now:             time.Now().UTC(),
	}); ok {
		selectedURL := strings.TrimSpace(hub.HTTPURL)
		m.mu.Lock()
		if strings.TrimSpace(m.cfg.HubURL) != selectedURL {
			m.stopHubSignalingLoopLocked()
		}
		m.cfg.HubURL = selectedURL
		m.mu.Unlock()
	}
	return nil
}

func (m *Manager) stopHubSignalingLoopLocked() {
	if m.signalingCancel != nil {
		m.signalingCancel()
		m.signalingCancel = nil
	}
	m.signalingStarted = false
	m.hubSessionID = ""
	m.hubRTCServers = nil
}

type hubSelectionOptions struct {
	PreferredRegion string
	Now             time.Time
}

func selectDiscoveredHub(hubs []discovery.Hub, opts hubSelectionOptions) (discovery.Hub, bool) {
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	preferredRegion := strings.TrimSpace(opts.PreferredRegion)
	var selected discovery.Hub
	selectedSet := false
	for _, hub := range hubs {
		if !hubUsable(hub, now) {
			continue
		}
		if !selectedSet || compareHubRank(hub, selected, preferredRegion, now) > 0 {
			selected = hub
			selectedSet = true
		}
	}
	return selected, selectedSet
}

func hubUsable(hub discovery.Hub, now time.Time) bool {
	if strings.TrimSpace(hub.HTTPURL) == "" || strings.TrimSpace(hub.Status) != "online" {
		return false
	}
	if hub.Capacity <= 0 {
		return false
	}
	if expiresAt, ok := parseHubExpiry(hub.ExpiresAt); ok && !expiresAt.After(now) {
		return false
	}
	return hubHealthOK(hub.Health)
}

func compareHubRank(a discovery.Hub, b discovery.Hub, preferredRegion string, now time.Time) int {
	if preferredRegion != "" {
		aRegion := strings.TrimSpace(a.Region) == preferredRegion
		bRegion := strings.TrimSpace(b.Region) == preferredRegion
		if aRegion != bRegion {
			if aRegion {
				return 1
			}
			return -1
		}
	}
	if a.Weight != b.Weight {
		if a.Weight > b.Weight {
			return 1
		}
		return -1
	}
	if a.Capacity != b.Capacity {
		if a.Capacity > b.Capacity {
			return 1
		}
		return -1
	}
	aExpiry, aHasExpiry := parseHubExpiry(a.ExpiresAt)
	bExpiry, bHasExpiry := parseHubExpiry(b.ExpiresAt)
	if aHasExpiry != bHasExpiry {
		if aHasExpiry {
			return 1
		}
		return -1
	}
	if aHasExpiry && !aExpiry.Equal(bExpiry) {
		if aExpiry.After(bExpiry) && aExpiry.After(now) {
			return 1
		}
		return -1
	}
	aID := strings.TrimSpace(a.ID)
	bID := strings.TrimSpace(b.ID)
	if aID < bID {
		return 1
	}
	if aID > bID {
		return -1
	}
	return 0
}

func parseHubExpiry(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, true
}

func hubHealthOK(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return true
	}
	var health map[string]any
	if err := json.Unmarshal([]byte(raw), &health); err != nil {
		return false
	}
	if ok, exists := health["ok"].(bool); exists {
		return ok
	}
	if healthy, exists := health["healthy"].(bool); exists {
		return healthy
	}
	if status, exists := health["status"].(string); exists {
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "", "ok", "online", "healthy":
			return true
		default:
			return false
		}
	}
	return true
}

func (m *Manager) syncControlRegistration(ctx context.Context) error {
	if m.cfg.ControlURL == "" || m.cfg.AccessToken == "" {
		return fmt.Errorf("control URL and access token are required for device registration")
	}

	hostname, _ := os.Hostname()
	machineKey, err := identity.LoadOrCreateMachineKey(m.cfg.DataDir)
	if err != nil {
		return err
	}
	var terminals []TerminalInventoryItem
	if m.provider != nil {
		terminals = m.provider.ListRemoteTerminals(ctx)
	}
	payload := discovery.DeviceRegistrationRequest{
		DeviceID:         m.identity.DeviceID,
		MachinePublicKey: identity.PublicKeyString(machineKey.PublicKey),
		DisplayName:      firstNonEmpty(m.identity.DisplayName, m.cfg.DeviceName),
		Hostname:         hostname,
		Platform:         fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		State:            string(StateConfigured),
		Terminals:        make([]discovery.DeviceRegistrationTerminal, 0, len(terminals)),
	}
	for _, terminal := range terminals {
		payload.Terminals = append(payload.Terminals, discovery.DeviceRegistrationTerminal{
			ID:      terminal.ID,
			Name:    terminal.Name,
			Command: append([]string(nil), terminal.Command...),
			Cols:    terminal.Cols,
			Rows:    terminal.Rows,
			State:   terminal.State,
		})
	}
	return discovery.RegisterDevice(ctx, m.cfg.ControlURL, m.cfg.AccessToken, payload)
}

func (m *Manager) syncHubPresence(ctx context.Context) error {
	hostname, _ := os.Hostname()

	m.mu.RLock()
	hubSessionID := m.hubSessionID
	m.mu.RUnlock()

	terminals := m.remoteHubTerminals(ctx)
	if hubSessionID == "" {
		machineKey, err := identity.LoadOrCreateMachineKey(m.cfg.DataDir)
		if err != nil {
			return err
		}
		agentID := randomAgentID()
		nonce := randomAgentID()
		now := time.Now().UTC()
		signature := machineKey.Sign(hubv1.CanonicalAgentRegistrationSignatureMessage(hubv1.AgentRegistrationSignatureFields{
			MachineID: m.identity.DeviceID,
			AgentID:   agentID,
			Nonce:     nonce,
			Timestamp: now,
		}))
		if len(signature) != ed25519.SignatureSize {
			return fmt.Errorf("machine key cannot sign hub registration")
		}
		resp, err := discovery.RegisterHub(ctx, m.cfg.HubURL, hubv1.HubRegisterRequest{
			Version:        "remote.hub.v1",
			DeviceID:       m.identity.DeviceID,
			AgentID:        agentID,
			DisplayName:    firstNonEmpty(m.identity.DisplayName, m.cfg.DeviceName),
			Hostname:       hostname,
			Platform:       fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
			RuntimeVersion: "termx-dev",
			Terminals:      terminals,
			Signature: hubv1.AgentRegistrationSignature{
				Algorithm: "ed25519",
				Nonce:     nonce,
				Timestamp: now.Unix(),
				Value:     base64.StdEncoding.EncodeToString(signature),
			},
		})
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.hubSessionID = resp.AgentSessionID
		m.hubRTCServers = append([]hubv1.RTCIceServerConfig(nil), resp.RTCConfig.IceServers...)
		m.mu.Unlock()
		return nil
	}

	_, err := discovery.HeartbeatHub(ctx, m.cfg.HubURL, hubv1.HubHeartbeatRequest{
		AgentSessionID: hubSessionID,
		DeviceID:       m.identity.DeviceID,
		LastSeenAt:     time.Now().UTC().Format(time.RFC3339),
		Terminals:      terminals,
	})
	if err == nil {
		return nil
	}
	if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
		m.resetHubSession()
		return m.syncHubPresence(ctx)
	}
	return err
}

func randomAgentID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("agent-%d", time.Now().UnixNano())
	}
	return "agent-" + hex.EncodeToString(raw[:])
}

func (m *Manager) remoteHubTerminals(ctx context.Context) []hubv1.HubTerminalInventoryItem {
	var terminals []TerminalInventoryItem
	if m.provider != nil {
		terminals = m.provider.ListRemoteTerminals(ctx)
	}
	out := make([]hubv1.HubTerminalInventoryItem, 0, len(terminals))
	for _, terminal := range terminals {
		out = append(out, hubv1.HubTerminalInventoryItem{
			ID:      terminal.ID,
			Name:    terminal.Name,
			Command: append([]string(nil), terminal.Command...),
			Cols:    terminal.Cols,
			Rows:    terminal.Rows,
			State:   terminal.State,
		})
	}
	return out
}

func (m *Manager) ensureHubSignalingLoop(ctx context.Context) {
	m.mu.Lock()
	if m.signalingStarted || m.hubSessionID == "" || m.host == nil {
		m.mu.Unlock()
		return
	}
	m.signalingStarted = true
	deviceID := m.identity.DeviceID
	agentSessionID := m.hubSessionID
	iceServers := append([]hubv1.RTCIceServerConfig(nil), m.hubRTCServers...)
	loopCtx, cancel := context.WithCancel(ctx)
	m.signalingCancel = cancel
	m.mu.Unlock()

	go m.hubSignalingLoop(loopCtx, deviceID, agentSessionID, iceServers)
}

func (m *Manager) hubSignalingLoop(ctx context.Context, deviceID, agentSessionID string, iceServers []hubv1.RTCIceServerConfig) {
	pendingAnswers := make(map[string]hubv1.SignalingAnswer)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pollCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		resp, ok, err := discovery.PollHubOffer(pollCtx, m.cfg.HubURL, hubv1.SignalingPollRequest{
			AgentSessionID: agentSessionID,
			DeviceID:       deviceID,
			TimeoutSeconds: 15,
		})
		cancel()
		if err != nil {
			if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
				m.resetHubSession()
				m.TriggerSync()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		if !ok || resp.Offer == nil {
			continue
		}

		offerKey := pendingAnswerKey(*resp.Offer)
		answer, hasPendingAnswer := pendingAnswers[offerKey]
		if !hasPendingAnswer {
			answer = m.answerCloudOffer(ctx, *resp.Offer, iceServers)
			pendingAnswers[offerKey] = answer
		}
		submitCtx, submitCancel := context.WithTimeout(ctx, 10*time.Second)
		err = discovery.SubmitHubAnswer(submitCtx, m.cfg.HubURL, hubv1.SubmitSignalingAnswerRequest{
			AgentSessionID: agentSessionID,
			DeviceID:       deviceID,
			Answer:         answer,
		})
		submitCancel()
		if err != nil {
			if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
				m.resetHubSession()
				m.TriggerSync()
				return
			}
			m.setStatus(StateDegraded, err.Error())
			continue
		}
		delete(pendingAnswers, offerKey)
	}
}

func pendingAnswerKey(offer hubv1.SignalingOffer) string {
	var b strings.Builder
	appendKeyPart := func(value string) {
		fmt.Fprintf(&b, "%d:", len(value))
		b.WriteString(value)
		b.WriteByte('|')
	}
	appendKeyPart(offer.SessionID)
	appendKeyPart(offer.TicketID)
	appendKeyPart(offer.DeviceID)
	appendKeyPart(offer.TerminalID)
	appendKeyPart(offer.SDP)
	for _, candidate := range offer.ICECandidates {
		appendKeyPart(candidate)
	}
	appendKeyPart(fmt.Sprintf("%t", offer.AllowRelay))
	appendKeyPart(fmt.Sprintf("%t", offer.AllowRelayTransfer))
	for _, server := range offer.RTCConfig.IceServers {
		for _, url := range server.URLs {
			appendKeyPart(url)
		}
		appendKeyPart(server.Username)
		appendKeyPart(server.Credential)
	}
	appendKeyPart(string(offer.AppCertificate))
	appendKeyPart(offer.Signature.Algorithm)
	appendKeyPart(offer.Signature.Nonce)
	appendKeyPart(fmt.Sprintf("%d", offer.Signature.Timestamp))
	appendKeyPart(offer.Signature.Value)
	return b.String()
}

func (m *Manager) answerCloudOffer(ctx context.Context, offer hubv1.SignalingOffer, iceServers []hubv1.RTCIceServerConfig) hubv1.SignalingAnswer {
	certificate, err := m.authorizeCloudOffer(ctx, offer)
	if err != nil {
		return hubv1.SignalingAnswer{
			SessionID: offer.SessionID,
			Error:     err.Error(),
		}
	}
	terminalManagement := m.terminalManagementRouter()
	answerer := m.answerer
	if answerer == nil {
		answerer = defaultCloudOfferAnswerer{}
	}
	policy := cloudOfferChannelPolicy(offer, certificate.Payload.Capabilities, terminalManagement)
	offerICEServers := append([]hubv1.RTCIceServerConfig(nil), iceServers...)
	if len(offer.RTCConfig.IceServers) > 0 {
		offerICEServers = append([]hubv1.RTCIceServerConfig(nil), offer.RTCConfig.IceServers...)
	}
	answer, err := answerer.AnswerOffer(ctx, offer, offerICEServers, m.host, m.files, remotertc.AnswerOptions{
		ChannelPolicy:      policy,
		TerminalManagement: terminalManagement,
	})
	if err != nil {
		return hubv1.SignalingAnswer{
			SessionID: offer.SessionID,
			Error:     err.Error(),
		}
	}
	return answer
}

func (m *Manager) authorizeCloudOffer(ctx context.Context, offer hubv1.SignalingOffer) (cert.AppCertificateEnvelope, error) {
	if m == nil {
		return cert.AppCertificateEnvelope{}, fmt.Errorf("remote manager is nil")
	}
	terminalID := strings.TrimSpace(offer.TerminalID)
	if terminalID == "" {
		return cert.AppCertificateEnvelope{}, fmt.Errorf("terminal_id is required")
	}
	if !m.hasTerminal(ctx, terminalID) {
		return cert.AppCertificateEnvelope{}, fmt.Errorf("terminal %q is not available for remote access", terminalID)
	}
	certificate, appPublicKey, err := m.verifyOfferCertificate(offer)
	if err != nil {
		return cert.AppCertificateEnvelope{}, err
	}
	if strings.TrimSpace(certificate.Payload.MachineID) != strings.TrimSpace(offer.DeviceID) {
		return cert.AppCertificateEnvelope{}, fmt.Errorf("offer machine_id does not match app certificate")
	}
	if !hasAppCapability(certificate.Payload.Capabilities, "terminal") {
		return cert.AppCertificateEnvelope{}, fmt.Errorf("app certificate terminal capability is required")
	}
	replay := m.offerReplayWindow()
	if err := remotertc.VerifyOfferSignature(remotertc.OfferSignature{
		Algorithm: offer.Signature.Algorithm,
		Nonce:     offer.Signature.Nonce,
		Timestamp: offer.Signature.Timestamp,
		Value:     offer.Signature.Value,
	}, remotertc.OfferSignatureFields{
		TicketID:   offer.TicketID,
		MachineID:  offer.DeviceID,
		TerminalID: offer.TerminalID,
		SDP:        offer.SDP,
		Candidates: offer.ICECandidates,
	}, appPublicKey, replay, time.Now().UTC()); err != nil {
		return cert.AppCertificateEnvelope{}, err
	}
	return certificate, nil
}

func (m *Manager) verifyOfferCertificate(offer hubv1.SignalingOffer) (cert.AppCertificateEnvelope, ed25519.PublicKey, error) {
	if len(offer.AppCertificate) == 0 {
		return cert.AppCertificateEnvelope{}, nil, fmt.Errorf("app certificate is required")
	}
	var envelope cert.AppCertificateEnvelope
	if err := json.Unmarshal(offer.AppCertificate, &envelope); err != nil {
		return cert.AppCertificateEnvelope{}, nil, fmt.Errorf("decode app certificate: %w", err)
	}
	machineKey, err := identity.LoadOrCreateMachineKey(m.cfg.DataDir)
	if err != nil {
		return cert.AppCertificateEnvelope{}, nil, err
	}
	if err := cert.VerifyAppCertificate(envelope, machineKey.PublicKey, time.Now().UTC()); err != nil {
		return cert.AppCertificateEnvelope{}, nil, err
	}
	machineID := strings.TrimSpace(m.identity.DeviceID)
	if machineID == "" {
		ident, err := identity.LoadOrCreate(m.cfg.DataDir, m.cfg.DeviceName)
		if err != nil {
			return cert.AppCertificateEnvelope{}, nil, err
		}
		machineID = strings.TrimSpace(ident.DeviceID)
	}
	if strings.TrimSpace(envelope.Payload.MachineID) != machineID {
		return cert.AppCertificateEnvelope{}, nil, fmt.Errorf("app certificate machine_id does not match local machine")
	}
	appPublicKey, err := base64.StdEncoding.DecodeString(envelope.Payload.AppPublicKey)
	if err != nil {
		return cert.AppCertificateEnvelope{}, nil, fmt.Errorf("decode app public key: %w", err)
	}
	return envelope, ed25519.PublicKey(appPublicKey), nil
}

func (m *Manager) hasTerminal(ctx context.Context, terminalID string) bool {
	if m == nil || m.provider == nil {
		return false
	}
	for _, terminal := range m.provider.ListRemoteTerminals(ctx) {
		if strings.TrimSpace(terminal.ID) == terminalID {
			return true
		}
	}
	return false
}

func cloudOfferChannelPolicy(offer hubv1.SignalingOffer, capabilities []string, terminalManagement remotertc.TerminalManagementRouter) remotertc.ChannelPolicy {
	allowTerminal := hasAppCapability(capabilities, "terminal")
	return remotertc.ChannelPolicy{
		TerminalID:              strings.TrimSpace(offer.TerminalID),
		AllowTerminal:           allowTerminal,
		AllowFileManager:        hasAppCapability(capabilities, "file_manager"),
		AllowTerminalManagement: hasAppCapability(capabilities, "terminal_management") && terminalManagement != nil,
		AllowEvents:             allowTerminal,
	}
}

func (m *Manager) offerReplayWindow() *cert.ReplayWindow {
	if m == nil {
		return cert.NewReplayWindow(5 * time.Minute)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.replay == nil {
		m.replay = cert.NewReplayWindow(5 * time.Minute)
	}
	return m.replay
}

func hasAppCapability(capabilities []string, capability string) bool {
	capability = strings.TrimSpace(capability)
	for _, candidate := range capabilities {
		if strings.TrimSpace(candidate) == capability {
			return true
		}
	}
	return false
}

func (m *Manager) terminalManagementRouter() remotertc.TerminalManagementRouter {
	if m == nil {
		return nil
	}
	if router, ok := m.host.(remotertc.TerminalManagementRouter); ok {
		return router
	}
	if router, ok := m.provider.(remotertc.TerminalManagementRouter); ok {
		return router
	}
	return nil
}

func (m *Manager) resetHubSession() {
	m.mu.Lock()
	cancel := m.signalingCancel
	m.hubSessionID = ""
	m.hubRTCServers = nil
	m.signalingStarted = false
	m.signalingCancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
