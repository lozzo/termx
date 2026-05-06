package runtime

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-remote/bridge"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/discovery"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/hub/sessionflow"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	pb "github.com/lozzow/termx/termx-remote/protocol/hubgrpc"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
	"github.com/lozzow/termx/termx-remote/session/token"
)

type State string

const (
	StateDisabled    State = "disabled"
	StateConfigured  State = "configured"
	StateRegistering State = "registering"
	StateOnline      State = "online"
	StateDegraded    State = "degraded"
)

var errHubForcedOffline = errors.New("hub forced offline")

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
	State         State       `json:"state"`
	Detail        string      `json:"detail,omitempty"`
	DeviceID      string      `json:"device_id,omitempty"`
	DeviceName    string      `json:"device_name,omitempty"`
	ControlURL    string      `json:"control_url,omitempty"`
	HubURL        string      `json:"hub_url,omitempty"`
	HubURLs       []string    `json:"hub_urls,omitempty"`
	Hubs          []HubStatus `json:"hubs,omitempty"`
	DataDir       string      `json:"data_dir,omitempty"`
	Mode          string      `json:"mode,omitempty"`
	AllowLAN      bool        `json:"allow_lan"`
	TerminalCount int         `json:"terminal_count"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

type HubKind string

const (
	HubKindLocal  HubKind = "local"
	HubKindOnline HubKind = "online"
)

type HubSource string

const (
	HubSourceConfig     HubSource = "config"
	HubSourceEmbedded   HubSource = "embedded"
	HubSourceExplicit   HubSource = "explicit"
	HubSourceWebControl HubSource = "web_control"
)

type HubTransport string

const (
	HubTransportGRPC HubTransport = "grpc"
)

type HubConnectionState string

const (
	HubConnectionDisconnected  HubConnectionState = "disconnected"
	HubConnectionConnecting    HubConnectionState = "connecting"
	HubConnectionConnected     HubConnectionState = "connected"
	HubConnectionReconnecting  HubConnectionState = "reconnecting"
	HubConnectionForcedOffline HubConnectionState = "forced_offline"
)

type HubStatus struct {
	URL                string             `json:"url"`
	Kind               HubKind            `json:"kind"`
	Source             HubSource          `json:"source"`
	Transport          HubTransport       `json:"transport"`
	State              HubConnectionState `json:"state"`
	ConnectedAt        time.Time          `json:"connected_at,omitempty"`
	LastAckAt          time.Time          `json:"last_ack_at,omitempty"`
	LastError          string             `json:"last_error,omitempty"`
	ForcedReason       string             `json:"forced_reason,omitempty"`
	AllowRelay         bool               `json:"allow_relay,omitempty"`
	AllowRelayTransfer bool               `json:"allow_relay_transfer,omitempty"`
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
	agentID          string
	hubStates        map[string]*hubRuntimeState
	answerOptions    remotertc.AnswerOptions
	answerer         cloudOfferAnswerer
	pairing          PairClaimer
	discoveredHubURL string
}

type hubRuntimeState struct {
	URL             string
	Kind            HubKind
	Source          HubSource
	Transport       HubTransport
	ConnectionState HubConnectionState
	SessionID       string
	RTCServers      []hubv1.RTCIceServerConfig
	RelayPolicy     hubv1.RelayPolicy
	SessionContext  context.Context
	SessionCancel   context.CancelFunc
	ConnectedAt     time.Time
	LastAckAt       time.Time
	LastError       string
	ForcedOffline   bool
	ForcedOfflineAt time.Time
	ForcedReason    string
	AnswerOptions   remotertc.AnswerOptions
}

type PairClaimer interface {
	ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error)
}

type cloudOfferAnswerer interface {
	AnswerOffer(
		ctx context.Context,
		offer hubv1.SignalingOffer,
		iceServers []hubv1.RTCIceServerConfig,
		sink bridge.TransportSink,
		fileManager *fileapi.Manager,
		opts any,
	) (hubv1.SignalingAnswer, error)
}

type defaultCloudOfferAnswerer struct{}

func (defaultCloudOfferAnswerer) AnswerOffer(
	ctx context.Context,
	offer hubv1.SignalingOffer,
	iceServers []hubv1.RTCIceServerConfig,
	sink bridge.TransportSink,
	fileManager *fileapi.Manager,
	opts any,
) (hubv1.SignalingAnswer, error) {
	answerOptions, _ := opts.(remotertc.AnswerOptions)
	return remotertc.AnswerOfferWithOptions(ctx, offer, iceServers, sink, fileManager, answerOptions)
}

func NewManager(cfg remoteconfig.Config, provider InventoryProvider, host bridge.TransportSink) *Manager {
	cfg = remoteconfig.Normalize(cfg)
	manager := &Manager{
		cfg:       cfg,
		provider:  provider,
		host:      host,
		files:     fileapi.NewManager(),
		hubStates: make(map[string]*hubRuntimeState),
		answerer:  defaultCloudOfferAnswerer{},
		syncCh:    make(chan struct{}, 1),
		status: Status{
			State:     StateDisabled,
			Detail:    "remote runtime disabled",
			DataDir:   cfg.DataDir,
			Mode:      cfg.Mode,
			AllowLAN:  cfg.AllowLAN,
			UpdatedAt: time.Now().UTC(),
		},
	}
	manager.mu.Lock()
	for _, hubURL := range cfg.HubURLs {
		manager.configureHubEndpointLocked(hubURL, hubKindForMode(cfg.Mode, true), HubSourceConfig, remotertc.AnswerOptions{}, false)
	}
	manager.mu.Unlock()
	return manager
}

func (m *Manager) SetPairClaimer(pairing PairClaimer) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.pairing = pairing
	m.mu.Unlock()
}

func (m *Manager) SetHubURL(hubURL string) {
	if m == nil {
		return
	}
	hubURL = strings.TrimSpace(hubURL)
	m.mu.Lock()
	if !sameHubURLsLocked(m.cfg.HubURLs, []string{hubURL}) {
		m.stopRemovedHubSignalingLocked([]string{hubURL})
	}
	m.cfg.HubURL = hubURL
	if hubURL == "" {
		m.cfg.HubURLs = nil
	} else {
		m.cfg.Enabled = true
		m.cfg.HubURLs = []string{hubURL}
		m.configureHubEndpointLocked(hubURL, hubKindForMode(m.cfg.Mode, true), HubSourceExplicit, remotertc.AnswerOptions{}, false)
	}
	m.mu.Unlock()
}

func (m *Manager) AddHubURL(hubURL string) {
	m.addHubURL(hubURL, remotertc.AnswerOptions{}, false, HubSourceConfig, hubKindForMode(m.cfg.Mode, false))
}

func (m *Manager) AddExplicitHubURL(hubURL string) {
	m.addHubURL(hubURL, remotertc.AnswerOptions{}, false, HubSourceExplicit, HubKindOnline)
}

func (m *Manager) AddHubURLWithAnswerOptions(hubURL string, opts remotertc.AnswerOptions) {
	m.addHubURL(hubURL, opts, true, HubSourceEmbedded, HubKindLocal)
}

func (m *Manager) addHubURL(hubURL string, opts remotertc.AnswerOptions, hasOptions bool, source HubSource, kind HubKind) {
	if m == nil {
		return
	}
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return
	}
	m.mu.Lock()
	m.cfg.Enabled = true
	if !containsString(m.cfg.HubURLs, hubURL) {
		m.cfg.HubURLs = append(m.cfg.HubURLs, hubURL)
	}
	if strings.TrimSpace(m.cfg.HubURL) == "" {
		m.cfg.HubURL = hubURL
	}
	m.configureHubEndpointLocked(hubURL, kind, source, opts, hasOptions)
	m.mu.Unlock()
	m.TriggerSync()
}

func (m *Manager) ConfigureHubAnswerOptions(hubURL string, opts remotertc.AnswerOptions) {
	if m == nil {
		return
	}
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return
	}
	m.mu.Lock()
	state := m.hubStateLocked(hubURL)
	m.configureHubEndpointLocked(hubURL, state.Kind, state.Source, opts, true)
	state.AnswerOptions = opts
	state.ForcedOffline = false
	state.ForcedReason = ""
	state.ForcedOfflineAt = time.Time{}
	m.mu.Unlock()
	m.TriggerSync()
}

func (m *Manager) ConfigureCloud(controlURL string, accessToken string, region string) {
	if m == nil {
		return
	}
	controlURL = strings.TrimSpace(controlURL)
	accessToken = strings.TrimSpace(accessToken)
	region = strings.TrimSpace(region)
	if controlURL == "" && accessToken == "" && region == "" {
		return
	}
	m.mu.Lock()
	if controlURL != "" || accessToken != "" {
		m.cfg.Enabled = true
	}
	if controlURL != "" {
		m.cfg.ControlURL = controlURL
	}
	if accessToken != "" {
		m.cfg.AccessToken = accessToken
	}
	if region != "" {
		m.cfg.Region = region
	}
	m.mu.Unlock()
	m.TriggerSync()
}

func (m *Manager) ConfigureAnswerOptions(opts remotertc.AnswerOptions) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.answerOptions = opts
	m.mu.Unlock()
}

func (m *Manager) DetachHub(hubURL string) {
	if m == nil {
		return
	}
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return
	}
	m.mu.Lock()
	if containsString(m.cfg.HubURLs, hubURL) || strings.TrimSpace(m.cfg.HubURL) == hubURL {
		remaining := removeString(m.cfg.HubURLs, hubURL)
		m.stopRemovedHubSignalingLocked(remaining)
		m.cfg.HubURLs = remaining
		if len(remaining) > 0 {
			m.cfg.HubURL = remaining[0]
		} else {
			m.cfg.HubURL = ""
			m.answerOptions = remotertc.AnswerOptions{}
		}
	}
	m.mu.Unlock()
	m.TriggerSync()
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.RLock()
	if m.started {
		m.mu.RUnlock()
		return nil
	}
	cfg := m.cfg
	m.mu.RUnlock()

	if !cfg.Enabled {
		m.setStatus(StateDisabled, "remote runtime disabled")
		return nil
	}
	if err := cfg.Validate(); err != nil {
		m.setStatus(StateDegraded, err.Error())
		return nil
	}

	ident, err := identity.LoadOrCreate(cfg.DataDir, cfg.DeviceName)
	if err != nil {
		m.setStatus(StateDegraded, err.Error())
		return nil
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.identity = ident
	m.started = true
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()

	defer func() {
		go m.reconcileLoop(runCtx)
	}()
	m.mu.RLock()
	controlURL := m.cfg.ControlURL
	hubURLs := append([]string(nil), m.cfg.HubURLs...)
	m.mu.RUnlock()
	switch {
	case controlURL == "" && len(hubURLs) == 0:
		m.setStatus(StateConfigured, "remote identity ready; waiting for control or hub configuration")
	default:
		state, detail, err := m.reconcile(runCtx)
		if err != nil {
			m.setStatus(StateDegraded, err.Error())
		} else {
			m.setStatus(state, detail)
		}
	}
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	cancel := m.cancel
	var hubCancels []context.CancelFunc
	for _, state := range m.hubStates {
		if state != nil && state.SessionCancel != nil {
			hubCancels = append(hubCancels, state.SessionCancel)
		}
	}
	m.cancel = nil
	m.started = false
	m.hubStates = make(map[string]*hubRuntimeState)
	m.status.State = StateDisabled
	m.status.Detail = "remote runtime closed"
	m.status.HubURL = ""
	m.status.HubURLs = nil
	m.status.Hubs = nil
	m.status.UpdatedAt = time.Now().UTC()
	m.mu.Unlock()
	for _, hubCancel := range hubCancels {
		if hubCancel != nil {
			hubCancel()
		}
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
	status.HubURLs = append([]string(nil), status.HubURLs...)
	status.Hubs = append([]HubStatus(nil), status.Hubs...)
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
		HubURLs:       append([]string(nil), m.cfg.HubURLs...),
		Hubs:          m.hubStatusesLocked(),
		DataDir:       m.cfg.DataDir,
		Mode:          m.cfg.Mode,
		AllowLAN:      m.cfg.AllowLAN,
		TerminalCount: m.status.TerminalCount,
		UpdatedAt:     time.Now().UTC(),
	}
}

func (m *Manager) hubStatusesLocked() []HubStatus {
	out := make([]HubStatus, 0, len(m.cfg.HubURLs))
	seen := make(map[string]struct{}, len(m.cfg.HubURLs))
	for _, hubURL := range m.cfg.HubURLs {
		hubURL = strings.TrimSpace(hubURL)
		if hubURL == "" {
			continue
		}
		seen[hubURL] = struct{}{}
		out = append(out, m.hubStatusLocked(hubURL))
	}
	for hubURL := range m.hubStates {
		hubURL = strings.TrimSpace(hubURL)
		if hubURL == "" {
			continue
		}
		if _, ok := seen[hubURL]; ok {
			continue
		}
		out = append(out, m.hubStatusLocked(hubURL))
	}
	return out
}

func (m *Manager) hubStatusLocked(hubURL string) HubStatus {
	state := m.hubStates[strings.TrimSpace(hubURL)]
	if state == nil {
		kind := hubKindForMode(m.cfg.Mode, true)
		return HubStatus{
			URL:       strings.TrimSpace(hubURL),
			Kind:      kind,
			Source:    HubSourceConfig,
			Transport: HubTransportGRPC,
			State:     HubConnectionDisconnected,
		}
	}
	connectionState := state.ConnectionState
	if connectionState == "" {
		connectionState = HubConnectionDisconnected
	}
	if state.ForcedOffline {
		connectionState = HubConnectionForcedOffline
	}
	return HubStatus{
		URL:                firstNonEmpty(state.URL, hubURL),
		Kind:               state.Kind,
		Source:             state.Source,
		Transport:          state.Transport,
		State:              connectionState,
		ConnectedAt:        state.ConnectedAt,
		LastAckAt:          state.LastAckAt,
		LastError:          state.LastError,
		ForcedReason:       state.ForcedReason,
		AllowRelay:         state.RelayPolicy.AllowRelay,
		AllowRelayTransfer: state.RelayPolicy.AllowRelayTransfer,
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

func sameHubURLsLocked(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func removeString(values []string, target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && value != target {
			out = append(out, value)
		}
	}
	return out
}

func hubKindForMode(mode string, explicit bool) HubKind {
	if remoteconfig.ModeIncludesOnline(mode) && (explicit || !remoteconfig.ModeIncludesLocal(mode)) {
		return HubKindOnline
	}
	if remoteconfig.ModeIncludesLocal(mode) {
		return HubKindLocal
	}
	if remoteconfig.ModeIncludesOnline(mode) {
		return HubKindOnline
	}
	return HubKindLocal
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
	m.mu.RLock()
	controlURL := m.cfg.ControlURL
	hubURLs := append([]string(nil), m.cfg.HubURLs...)
	m.mu.RUnlock()
	if controlURL != "" {
		if err := m.syncControlRegistration(ctx); err != nil {
			return StateDegraded, "", err
		}
		if err := m.discoverHub(ctx); err != nil {
			return StateDegraded, "", err
		}
		m.mu.RLock()
		controlURL = m.cfg.ControlURL
		hubURLs = append([]string(nil), m.cfg.HubURLs...)
		m.mu.RUnlock()
	}
	if len(hubURLs) > 0 {
		registered, started, lastErr := m.syncHubPresences(ctx, hubURLs)
		if registered == 0 && started > 0 {
			if controlURL != "" {
				return StateRegistering, "device registered in control; hub signaling registering", nil
			}
			return StateRegistering, "hub signaling registering; control registration not configured", nil
		}
		if registered == 0 && lastErr != nil {
			return StateDegraded, "", lastErr
		}
		if controlURL != "" {
			return StateOnline, "device registered in control and hub; terminal signaling active", nil
		}
		return StateOnline, "device registered in hub; control registration not configured", nil
	}
	if controlURL != "" {
		return StateConfigured, "device registered in control; waiting for hub configuration", nil
	}
	return StateConfigured, "remote identity ready; waiting for control or hub configuration", nil
}

type hubSyncResult struct {
	started bool
	err     error
}

func (m *Manager) syncHubPresences(ctx context.Context, hubURLs []string) (int, int, error) {
	resultCh := make(chan hubSyncResult, len(hubURLs))
	pending := 0
	for _, hubURL := range hubURLs {
		hubURL = strings.TrimSpace(hubURL)
		if hubURL == "" {
			continue
		}
		pending++
		go func(hubURL string) {
			var err error
			started := false
			switch kind := m.hubKind(hubURL); kind {
			case HubKindOnline, HubKindLocal:
				err = m.ensureGRPCHubLoop(ctx, hubURL)
				started = err == nil
			default:
				err = nil
			}
			resultCh <- hubSyncResult{started: started, err: err}
		}(hubURL)
	}
	if pending == 0 {
		return 0, 0, nil
	}

	registered := m.registeredHubCount(hubURLs)
	started := 0
	var lastErr error
	for pending > 0 {
		select {
		case <-ctx.Done():
			if registered > 0 {
				return registered, started, lastErr
			}
			return 0, started, ctx.Err()
		case result := <-resultCh:
			pending--
			if result.started {
				started++
			}
			if result.err != nil {
				lastErr = result.err
			}
			registered = m.registeredHubCount(hubURLs)
			if registered > 0 {
				for pending > 0 {
					select {
					case result := <-resultCh:
						pending--
						if result.started {
							started++
						}
						if result.err != nil {
							lastErr = result.err
						}
						registered = m.registeredHubCount(hubURLs)
					default:
						return registered, started, lastErr
					}
				}
				return registered, started, lastErr
			}
		}
	}
	registered = m.registeredHubCount(hubURLs)
	return registered, started, lastErr
}

func (m *Manager) hubKind(hubURL string) HubKind {
	m.mu.RLock()
	state := m.hubStates[strings.TrimSpace(hubURL)]
	mode := m.cfg.Mode
	m.mu.RUnlock()
	if state != nil && state.Kind != "" {
		return state.Kind
	}
	return hubKindForMode(mode, true)
}

func (m *Manager) registeredHubCount(hubURLs []string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, hubURL := range hubURLs {
		state := m.hubStates[strings.TrimSpace(hubURL)]
		if state == nil || state.ForcedOffline {
			continue
		}
		if state.ConnectionState == HubConnectionConnected && strings.TrimSpace(state.SessionID) != "" {
			count++
		}
	}
	return count
}

func (m *Manager) discoverHub(ctx context.Context) error {
	m.mu.RLock()
	controlURL := m.cfg.ControlURL
	accessToken := m.cfg.AccessToken
	preferredRegion := m.cfg.Region
	m.mu.RUnlock()
	if controlURL == "" || accessToken == "" {
		return nil
	}
	hubs, err := discovery.DiscoverHubs(ctx, controlURL, accessToken)
	if err != nil {
		return err
	}
	if hub, ok := selectDiscoveredHub(hubs, hubSelectionOptions{
		PreferredRegion: preferredRegion,
		Now:             time.Now().UTC(),
	}); ok {
		selectedURL := m.selectLowLatencyHubURL(ctx, hubs, hub)
		m.mu.Lock()
		hubURLs := append([]string(nil), m.cfg.HubURLs...)
		previousDiscovered := strings.TrimSpace(m.discoveredHubURL)
		if previousDiscovered != "" && previousDiscovered != selectedURL {
			hubURLs = removeString(hubURLs, previousDiscovered)
		}
		if !containsString(hubURLs, selectedURL) {
			hubURLs = append(hubURLs, selectedURL)
		}
		if previousDiscovered != "" && previousDiscovered != selectedURL {
			m.stopRemovedHubSignalingLocked(hubURLs)
		}
		if strings.TrimSpace(m.cfg.HubURL) == "" || strings.TrimSpace(m.cfg.HubURL) == previousDiscovered {
			m.cfg.HubURL = selectedURL
		}
		m.cfg.HubURLs = hubURLs
		m.discoveredHubURL = selectedURL
		m.configureHubEndpointLocked(selectedURL, HubKindOnline, HubSourceWebControl, remotertc.AnswerOptions{}, false)
		m.mu.Unlock()
	}
	return nil
}

func (m *Manager) selectLowLatencyHubURL(ctx context.Context, hubs []discovery.Hub, fallback discovery.Hub) string {
	urls := make([]string, 0, len(hubs))
	now := time.Now().UTC()
	for _, hub := range hubs {
		if !hubUsable(hub, now) {
			continue
		}
		if url := strings.TrimSpace(hub.HTTPURL); url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) > 0 {
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		for _, result := range discovery.ProbeHubs(probeCtx, urls, 3*time.Second, 3) {
			if result.Available && strings.TrimSpace(result.URL) != "" {
				return strings.TrimSpace(result.URL)
			}
		}
	}
	return strings.TrimSpace(fallback.HTTPURL)
}

func (m *Manager) stopHubSignalingLoopLocked() {
	for _, state := range m.hubStates {
		if state == nil {
			continue
		}
		if state.SessionCancel != nil {
			state.SessionCancel()
		}
		state.SessionID = ""
		state.RTCServers = nil
		state.RelayPolicy = hubv1.RelayPolicy{}
		state.SessionContext = nil
		state.SessionCancel = nil
		state.ConnectionState = HubConnectionDisconnected
		state.ConnectedAt = time.Time{}
	}
}

func (m *Manager) stopRemovedHubSignalingLocked(keep []string) {
	keepSet := make(map[string]struct{}, len(keep))
	for _, hubURL := range keep {
		if hubURL = strings.TrimSpace(hubURL); hubURL != "" {
			keepSet[hubURL] = struct{}{}
		}
	}
	for hubURL, state := range m.hubStates {
		if _, ok := keepSet[hubURL]; ok {
			continue
		}
		if state != nil && state.SessionCancel != nil {
			state.SessionCancel()
		}
		delete(m.hubStates, hubURL)
	}
	if len(keepSet) == 0 {
		m.stopHubSignalingLoopLocked()
		return
	}
	if _, ok := keepSet[strings.TrimSpace(m.cfg.HubURL)]; !ok {
		m.stopHubSignalingLoopLocked()
	}
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
	m.mu.RLock()
	controlURL := m.cfg.ControlURL
	accessToken := m.cfg.AccessToken
	deviceName := m.cfg.DeviceName
	m.mu.RUnlock()
	if controlURL == "" || accessToken == "" {
		return fmt.Errorf("control URL and access token are required for device registration")
	}

	hostname, _ := os.Hostname()
	payload := discovery.DeviceRegistrationRequest{
		DeviceID:    m.identity.DeviceID,
		DisplayName: firstNonEmpty(m.identity.DisplayName, deviceName),
		Hostname:    hostname,
		Platform:    fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		State:       string(StateConfigured),
	}
	return discovery.RegisterDevice(ctx, controlURL, accessToken, payload)
}

func (m *Manager) hubStateLocked(hubURL string) *hubRuntimeState {
	hubURL = strings.TrimSpace(hubURL)
	if m.hubStates == nil {
		m.hubStates = make(map[string]*hubRuntimeState)
	}
	state := m.hubStates[hubURL]
	if state == nil {
		state = &hubRuntimeState{
			URL:             hubURL,
			Kind:            hubKindForMode(m.cfg.Mode, true),
			Source:          HubSourceConfig,
			Transport:       HubTransportGRPC,
			ConnectionState: HubConnectionDisconnected,
		}
		m.hubStates[hubURL] = state
	}
	if state.URL == "" {
		state.URL = hubURL
	}
	if state.Kind == "" {
		state.Kind = hubKindForMode(m.cfg.Mode, true)
	}
	if state.Source == "" {
		state.Source = HubSourceConfig
	}
	if state.Transport == "" {
		state.Transport = HubTransportGRPC
	}
	if state.ConnectionState == "" {
		state.ConnectionState = HubConnectionDisconnected
	}
	return state
}

func (m *Manager) configureHubEndpointLocked(hubURL string, kind HubKind, source HubSource, opts remotertc.AnswerOptions, hasOptions bool) *hubRuntimeState {
	state := m.hubStateLocked(hubURL)
	if kind != "" {
		state.Kind = kind
	}
	if source != "" {
		state.Source = source
	}
	state.Transport = HubTransportGRPC
	state.ForcedOffline = false
	state.ForcedReason = ""
	state.ForcedOfflineAt = time.Time{}
	if hasOptions {
		state.AnswerOptions = opts
	}
	return state
}

func randomAgentID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("agent-%d", time.Now().UnixNano())
	}
	return "agent-" + hex.EncodeToString(raw[:])
}

func (m *Manager) buildGRPCTerminals() []*pb.Terminal {
	var terminals []TerminalInventoryItem
	if m.provider != nil {
		terminals = m.provider.ListRemoteTerminals(context.Background())
	}
	out := make([]*pb.Terminal, 0, len(terminals))
	for _, terminal := range terminals {
		out = append(out, &pb.Terminal{
			TerminalId:    terminal.ID,
			Name:          terminal.Name,
			RemoteEnabled: true,
		})
	}
	return out
}

func (m *Manager) buildGRPCRegisterRequest() *pb.RegisterRequest {
	m.mu.Lock()
	if strings.TrimSpace(m.agentID) == "" {
		m.agentID = randomAgentID()
	}
	agentID := m.agentID
	deviceID := m.identity.DeviceID
	displayName := firstNonEmpty(m.identity.DisplayName, m.cfg.DeviceName)
	m.mu.Unlock()
	hostname, _ := os.Hostname()
	return &pb.RegisterRequest{
		AgentId:     agentID,
		DeviceId:    deviceID,
		MachineId:   deviceID,
		DisplayName: displayName,
		Hostname:    hostname,
		Platform:    fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		Version:     "termx-dev",
		Terminals:   m.buildGRPCTerminals(),
	}
}

func (m *Manager) ensureGRPCHubLoop(ctx context.Context, hubURL string) error {
	m.mu.Lock()
	state := m.hubStateLocked(hubURL)
	if state.ForcedOffline {
		reason := state.ForcedReason
		m.mu.Unlock()
		return forcedOfflineError(reason)
	}
	if state.SessionCancel != nil {
		m.mu.Unlock()
		return nil
	}
	loopCtx, cancel := context.WithCancel(ctx)
	state.SessionContext = loopCtx
	state.SessionCancel = cancel
	state.ConnectionState = HubConnectionConnecting
	state.LastError = ""
	m.mu.Unlock()
	go m.runGRPCHubLoop(loopCtx, hubURL)
	return nil
}

func (m *Manager) runGRPCHubLoop(ctx context.Context, hubURL string) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := m.connectAndServeGRPC(ctx, hubURL); err != nil && ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}
		backoff = time.Second
	}
}

func (m *Manager) connectAndServeGRPC(ctx context.Context, hubURL string) error {
	client, err := discovery.NewGRPCHubClient(hubURL, m.grpcAccessTokenForHub(hubURL))
	if err != nil {
		m.markHubConnectionError(hubURL, err)
		return fmt.Errorf("create grpc client: %w", err)
	}
	defer client.Close()

	stream, err := client.Connect(ctx)
	if err != nil {
		m.markHubConnectionError(hubURL, err)
		return fmt.Errorf("connect: %w", err)
	}
	sender := &lockedGRPCClientSender{stream: stream}
	if err := sender.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Register{
		Register: m.buildGRPCRegisterRequest(),
	}}); err != nil {
		m.markHubConnectionError(hubURL, err)
		return err
	}
	msg, err := stream.Recv()
	if err != nil {
		m.markHubConnectionError(hubURL, err)
		return err
	}
	ack := msg.GetRegisterAck()
	if ack == nil {
		m.markHubConnectionError(hubURL, fmt.Errorf("expected register_ack"))
		return fmt.Errorf("expected register_ack")
	}
	m.updateFromGRPCRegisterAck(hubURL, ack)
	m.TriggerSync()

	interval := time.Duration(ack.GetHeartbeatIntervalSeconds()) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go m.grpcHeartbeatLoop(heartbeatCtx, sender, ack.GetAgentSessionId(), interval)

	for {
		msg, err := stream.Recv()
		if err != nil {
			m.markHubConnectionError(hubURL, err)
			m.TriggerSync()
			return err
		}
		switch p := msg.GetPayload().(type) {
		case *pb.HubToAgent_SignalingOffer:
			go m.handleGRPCOffer(ctx, hubURL, p.SignalingOffer, sender)
		case *pb.HubToAgent_PairingClaim:
			go m.handleGRPCPairingClaim(ctx, p.PairingClaim, sender)
		case *pb.HubToAgent_Kick:
			return fmt.Errorf("kicked: %s", p.Kick.GetReason())
		}
	}
}

func (m *Manager) grpcAccessTokenForHub(hubURL string) string {
	m.mu.RLock()
	accessToken := strings.TrimSpace(m.cfg.AccessToken)
	kind := HubKind("")
	if state := m.hubStates[strings.TrimSpace(hubURL)]; state != nil {
		kind = state.Kind
	}
	m.mu.RUnlock()
	if accessToken != "" {
		return accessToken
	}
	if kind == HubKindLocal {
		return "local"
	}
	return ""
}

type lockedGRPCClientSender struct {
	mu     sync.Mutex
	stream pb.AgentHub_ConnectClient
}

func (s *lockedGRPCClientSender) Send(msg *pb.AgentToHub) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stream.Send(msg)
}

func (m *Manager) grpcHeartbeatLoop(ctx context.Context, sender interface{ Send(*pb.AgentToHub) error }, sessionID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = sender.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_Heartbeat{
				Heartbeat: &pb.HeartbeatRequest{
					AgentSessionId: sessionID,
					Terminals:      m.buildGRPCTerminals(),
				},
			}})
		}
	}
}

func (m *Manager) updateFromGRPCRegisterAck(hubURL string, ack *pb.RegisterResponse) {
	m.mu.Lock()
	state := m.hubStateLocked(hubURL)
	state.SessionID = ack.GetAgentSessionId()
	state.RTCServers = grpcIceServersToHub(ack.GetIceServers())
	if policy := ack.GetRelayPolicy(); policy != nil {
		state.RelayPolicy = hubv1.RelayPolicy{
			AllowRelay:         policy.GetAllowRelay(),
			AllowRelayTransfer: policy.GetAllowRelayTransfer(),
		}
	} else {
		state.RelayPolicy = hubv1.RelayPolicy{}
	}
	now := time.Now().UTC()
	state.ConnectionState = HubConnectionConnected
	if state.ConnectedAt.IsZero() {
		state.ConnectedAt = now
	}
	state.LastAckAt = now
	state.LastError = ""
	m.mu.Unlock()
}

func (m *Manager) markHubConnectionError(hubURL string, err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	state := m.hubStateLocked(hubURL)
	if state.ConnectionState == HubConnectionConnected {
		state.ConnectionState = HubConnectionReconnecting
	} else if state.ConnectionState != HubConnectionForcedOffline {
		state.ConnectionState = HubConnectionConnecting
	}
	state.LastError = err.Error()
	m.mu.Unlock()
}

func grpcIceServersToHub(in []*pb.RTCIceServer) []hubv1.RTCIceServerConfig {
	out := make([]hubv1.RTCIceServerConfig, 0, len(in))
	for _, server := range in {
		if server == nil {
			continue
		}
		out = append(out, hubv1.RTCIceServerConfig{
			URLs:       append([]string(nil), server.GetUrls()...),
			Username:   server.GetUsername(),
			Credential: server.GetCredential(),
		})
	}
	return out
}

func (m *Manager) handleGRPCOffer(ctx context.Context, hubURL string, pbOffer *pb.SignalingOffer, sender interface{ Send(*pb.AgentToHub) error }) {
	if pbOffer == nil {
		return
	}
	offer := hubv1.SignalingOffer{
		SessionID:            pbOffer.GetSessionId(),
		MachineID:            pbOffer.GetMachineId(),
		TerminalID:           pbOffer.GetTerminalId(),
		SDP:                  pbOffer.GetSdp(),
		Candidates:           append([]string(nil), pbOffer.GetIceCandidates()...),
		SessionToken:         pbOffer.GetSessionToken(),
		AnswerProofChallenge: pbOffer.GetAnswerProofChallenge(),
	}
	answer, err := m.answerOffer(ctx, hubURL, offer)
	if err != nil {
		answer = hubv1.SignalingAnswer{SessionID: offer.SessionID, Error: err.Error()}
	}
	_ = sender.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_SignalingAnswer{
		SignalingAnswer: &pb.SignalingAnswer{
			SessionId:     answer.SessionID,
			Sdp:           answer.SDP,
			IceCandidates: append([]string(nil), answer.ICECandidates...),
			Error:         answer.Error,
			AnswerProof:   answer.AnswerProof,
		},
	}})
}

func (m *Manager) handleGRPCPairingClaim(ctx context.Context, claim *pb.PairingClaim, sender interface{ Send(*pb.AgentToHub) error }) {
	if claim == nil {
		return
	}
	result := m.claimHubPairing(ctx, hubv1.PairingClaim{
		ClaimID:               claim.GetClaimId(),
		MachineID:             m.identity.DeviceID,
		PairSessionID:         claim.GetPairSessionId(),
		PairSecret:            claim.GetPairSecret(),
		AppDeviceID:           claim.GetAppDeviceId(),
		AppName:               claim.GetAppName(),
		RequestedCapabilities: append([]string(nil), claim.GetRequestedCapabilities()...),
	})
	_ = sender.Send(&pb.AgentToHub{Payload: &pb.AgentToHub_PairingResult{
		PairingResult: &pb.PairingResult{
			ClaimId:      result.ClaimID,
			SessionToken: result.SessionToken,
			ExpiresAt:    result.ExpiresAt,
			MachineId:    result.MachineID,
			MachineName:  result.MachineName,
			Error:        result.Error,
		},
	}})
}

func (m *Manager) answerOffer(ctx context.Context, hubURL string, offer hubv1.SignalingOffer) (hubv1.SignalingAnswer, error) {
	m.mu.RLock()
	state := m.hubStateLocked(hubURL)
	iceServers := append([]hubv1.RTCIceServerConfig(nil), state.RTCServers...)
	answerOptions := state.AnswerOptions
	sessionCtx := state.SessionContext
	m.mu.RUnlock()
	answerOptions.SessionContext = combineSessionContext(sessionCtx, answerOptions.SessionContext)
	return m.answerCloudOfferWithOptions(ctx, offer, iceServers, answerOptions), nil
}

func (m *Manager) claimHubPairing(ctx context.Context, claim hubv1.PairingClaim) hubv1.PairingResult {
	result := hubv1.PairingResult{
		ClaimID:   claim.ClaimID,
		MachineID: claim.MachineID,
	}
	m.mu.RLock()
	pairing := m.pairing
	m.mu.RUnlock()
	if pairing == nil {
		result.Error = "remote pairing is not available"
		return result
	}
	out, err := pairing.ClaimPairSession(ctx, pairingpkgClaimRequest(claim))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if strings.TrimSpace(out.MachineID) != strings.TrimSpace(claim.MachineID) {
		result.Error = fmt.Sprintf("pairing response machine mismatch: %s != %s", out.MachineID, claim.MachineID)
		return result
	}
	result.MachineName = out.MachineName
	result.SessionToken = out.SessionToken
	result.ExpiresAt = out.ExpiresAt.Format(time.RFC3339)
	return result
}

func pairingpkgClaimRequest(claim hubv1.PairingClaim) pairing.ClaimRequest {
	return pairing.ClaimRequest{
		PairSessionID:         claim.PairSessionID,
		PairSecret:            claim.PairSecret,
		AppDeviceID:           claim.AppDeviceID,
		AppName:               claim.AppName,
		RequestedCapabilities: append([]string(nil), claim.RequestedCapabilities...),
	}
}

func (m *Manager) answerCloudOffer(ctx context.Context, offer hubv1.SignalingOffer, iceServers []hubv1.RTCIceServerConfig) hubv1.SignalingAnswer {
	m.mu.RLock()
	answerOptions := m.answerOptions
	m.mu.RUnlock()
	return m.answerCloudOfferWithOptions(ctx, offer, iceServers, answerOptions)
}

func (m *Manager) answerCloudOfferWithOptions(ctx context.Context, offer hubv1.SignalingOffer, iceServers []hubv1.RTCIceServerConfig, answerOptions remotertc.AnswerOptions) hubv1.SignalingAnswer {
	claims, err := m.verifyOfferSession(ctx, offer)
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
	policy := cloudOfferChannelPolicy(offer, claims.Capabilities, terminalManagement)
	offerICEServers := append([]hubv1.RTCIceServerConfig(nil), iceServers...)
	answerOptions.ChannelPolicy = policy
	answerOptions.TerminalManagement = terminalManagement
	answerOptions.Events = m.eventRouter()
	flow := sessionflow.ManagedPlan(offerICEServers, sessionflow.RelayPolicy{
		AllowRelay:         len(offerICEServers) > 0,
		AllowRelayTransfer: policy.AllowRelayTransfer,
	})
	answer, err := sessionflow.AnswerManaged(ctx, answerer, sessionflow.AnswerInput{
		Plan:    flow,
		Offer:   offer,
		Sink:    m.host,
		Files:   m.files,
		Options: answerOptions,
	})
	if err != nil {
		return hubv1.SignalingAnswer{
			SessionID: offer.SessionID,
			Error:     err.Error(),
		}
	}
	answer.AnswerProof = m.answerProof(offer, claims)
	return answer
}

func (m *Manager) verifyOfferSession(ctx context.Context, offer hubv1.SignalingOffer) (token.Claims, error) {
	if m == nil {
		return token.Claims{}, fmt.Errorf("remote manager is nil")
	}
	claims, err := m.verifySessionToken(m.identity.DeviceID, offer.SessionToken)
	if err != nil {
		return token.Claims{}, err
	}
	terminalID := strings.TrimSpace(offer.TerminalID)
	if terminalID != "" {
		if !m.hasTerminal(ctx, terminalID) {
			return token.Claims{}, fmt.Errorf("terminal %q is not available for remote access", terminalID)
		}
		if !hasAppCapability(claims.Capabilities, "terminal") {
			return token.Claims{}, fmt.Errorf("session token terminal capability is required")
		}
	} else if !hasAppCapability(claims.Capabilities, "terminal") && !hasAppCapability(claims.Capabilities, "terminal_management") {
		return token.Claims{}, fmt.Errorf("session token terminal or terminal_management capability is required for machine-scoped remote runtime")
	}
	return claims, nil
}

func (m *Manager) answerProof(offer hubv1.SignalingOffer, claims token.Claims) string {
	challenge := strings.TrimSpace(offer.AnswerProofChallenge)
	if challenge == "" {
		return ""
	}
	secret, err := identity.LoadOrCreateMachineSecret(m.cfg.DataDir)
	if err != nil {
		return ""
	}
	proofKey, err := token.OpenAnswerProofKey(secret, claims)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(proofKey))
	mac.Write([]byte("termx-answer-proof-v1:"))
	mac.Write([]byte(strings.TrimSpace(claims.SessionID)))
	mac.Write([]byte(":"))
	mac.Write([]byte(strings.TrimSpace(offer.SessionID)))
	mac.Write([]byte(":"))
	mac.Write([]byte(challenge))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (m *Manager) verifySessionToken(machineID, sessionToken string) (token.Claims, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return token.Claims{}, fmt.Errorf("session_token is required")
	}
	secret, err := identity.LoadOrCreateMachineSecret(m.cfg.DataDir)
	if err != nil {
		return token.Claims{}, fmt.Errorf("load machine secret: %w", err)
	}
	claims, err := token.Verify(sessionToken, secret, time.Now().UTC())
	if err != nil {
		return token.Claims{}, fmt.Errorf("invalid session token: %w", err)
	}
	if claims.MachineID != machineID {
		return token.Claims{}, fmt.Errorf("session token machine_id mismatch: got %q want %q", claims.MachineID, machineID)
	}
	return claims, nil
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
	terminalID := strings.TrimSpace(offer.TerminalID)
	allowTerminal := hasAppCapability(capabilities, "terminal")
	return remotertc.ChannelPolicy{
		TerminalID:              terminalID,
		AllowTerminal:           allowTerminal,
		AllowAPI:                true,
		AllowFileManager:        hasAppCapability(capabilities, "file_manager"),
		AllowTerminalManagement: hasAppCapability(capabilities, "terminal_management") && terminalManagement != nil,
		AllowEvents:             allowTerminal,
		AllowRelayTransfer:      hasAppCapability(capabilities, "file_manager"),
	}
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

func (m *Manager) eventRouter() remotertc.EventRouter {
	if m == nil {
		return nil
	}
	if router, ok := m.host.(remotertc.EventRouter); ok {
		return router
	}
	if router, ok := m.provider.(remotertc.EventRouter); ok {
		return router
	}
	return nil
}

func (m *Manager) resetHubSession(hubURL string) {
	m.mu.Lock()
	var cancels []context.CancelFunc
	if hubURL = strings.TrimSpace(hubURL); hubURL != "" {
		state := m.hubStateLocked(hubURL)
		if state.SessionCancel != nil {
			cancels = append(cancels, state.SessionCancel)
		}
		state.SessionID = ""
		state.RTCServers = nil
		state.RelayPolicy = hubv1.RelayPolicy{}
		state.SessionContext = nil
		state.SessionCancel = nil
		state.ConnectionState = HubConnectionDisconnected
		state.ConnectedAt = time.Time{}
	} else {
		for _, state := range m.hubStates {
			if state == nil {
				continue
			}
			if state.SessionCancel != nil {
				cancels = append(cancels, state.SessionCancel)
			}
			state.SessionID = ""
			state.RTCServers = nil
			state.RelayPolicy = hubv1.RelayPolicy{}
			state.SessionContext = nil
			state.SessionCancel = nil
			state.ConnectionState = HubConnectionDisconnected
			state.ConnectedAt = time.Time{}
		}
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (m *Manager) forceHubOffline(hubURL string, cause error) {
	reason := "hub forced offline"
	if cause != nil {
		reason = cause.Error()
	}
	m.mu.Lock()
	var cancels []context.CancelFunc
	if hubURL = strings.TrimSpace(hubURL); hubURL != "" {
		state := m.hubStateLocked(hubURL)
		if state.SessionCancel != nil {
			cancels = append(cancels, state.SessionCancel)
		}
		state.SessionID = ""
		state.RTCServers = nil
		state.RelayPolicy = hubv1.RelayPolicy{}
		state.SessionContext = nil
		state.SessionCancel = nil
		state.ConnectionState = HubConnectionForcedOffline
		state.ConnectedAt = time.Time{}
		state.ForcedOffline = true
		state.ForcedOfflineAt = time.Now().UTC()
		state.ForcedReason = reason
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	m.setStatus(StateDegraded, reason)
}

func forcedOfflineError(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errHubForcedOffline
	}
	return fmt.Errorf("%w: %s", errHubForcedOffline, reason)
}

func combineSessionContext(hubCtx, optionCtx context.Context) context.Context {
	switch {
	case hubCtx == nil:
		return optionCtx
	case optionCtx == nil:
		return hubCtx
	}
	combined, cancel := context.WithCancel(hubCtx)
	go func() {
		select {
		case <-optionCtx.Done():
			cancel()
		case <-combined.Done():
		}
	}()
	return combined
}
