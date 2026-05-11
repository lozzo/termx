package runtime

import (
	"context"
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
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
	"github.com/lozzow/termx/termx-core/protocol"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
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
	ID           string   `json:"terminal_id"`
	Name         string   `json:"name,omitempty"`
	State        string   `json:"state,omitempty"`
	Command      []string `json:"command,omitempty"`
	Cols         int      `json:"cols,omitempty"`
	Rows         int      `json:"rows,omitempty"`
	CWD          string   `json:"cwd,omitempty"`
	Environment  string   `json:"environment,omitempty"`
	SizeLocked   bool     `json:"size_locked,omitempty"`
	SizeLockMode string   `json:"size_lock_mode,omitempty"`
	ResizeOwnership            *protocol.ResizeOwnership `json:"resize_ownership,omitempty"`
	ResizeOwnerAttachmentCount int                       `json:"resize_owner_attachment_count,omitempty"`
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
	machineSecret    []byte
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
	seenNonces      map[string]time.Time
	offerSem        chan struct{}
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
	kind := HubKind("")
	if state != nil {
		kind = state.Kind
	}
	mode := m.cfg.Mode
	m.mu.RUnlock()
	if kind != "" {
		return kind
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
			offerSem:        make(chan struct{}, 8),
		}
		m.hubStates[hubURL] = state
	}
	if state.offerSem == nil {
		state.offerSem = make(chan struct{}, 8)
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
