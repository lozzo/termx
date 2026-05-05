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

	"github.com/lozzow/termx/termx-remote/bridge"
	"github.com/lozzow/termx/termx-remote/cert"
	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/discovery"
	"github.com/lozzow/termx/termx-remote/fileapi"
	"github.com/lozzow/termx/termx-remote/hub/sessionflow"
	"github.com/lozzow/termx/termx-remote/identity"
	"github.com/lozzow/termx/termx-remote/pairing"
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

const hubPresenceSyncTimeout = 5 * time.Second

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
	agentID          string
	hubStates        map[string]*hubRuntimeState
	hubSessionID     string
	hubRTCServers    []hubv1.RTCIceServerConfig
	signalingStarted bool
	signalingCancel  context.CancelFunc
	explicitHubURL   bool
	explicitHubURLs  map[string]struct{}
	answerOptions    remotertc.AnswerOptions
	answerer         cloudOfferAnswerer
	pairing          PairClaimer
	replay           *cert.ReplayWindow
	discoveredHubURL string
}

type hubRuntimeState struct {
	SessionID        string
	RTCServers       []hubv1.RTCIceServerConfig
	SignalingStarted bool
	SignalingCancel  context.CancelFunc
	AnswerOptions    remotertc.AnswerOptions
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
	explicitHubURLs := make(map[string]struct{}, len(cfg.HubURLs))
	for _, hubURL := range cfg.HubURLs {
		if hubURL = strings.TrimSpace(hubURL); hubURL != "" {
			explicitHubURLs[hubURL] = struct{}{}
		}
	}
	return &Manager{
		cfg:             cfg,
		provider:        provider,
		host:            host,
		files:           fileapi.NewManager(),
		hubStates:       make(map[string]*hubRuntimeState),
		explicitHubURL:  len(explicitHubURLs) > 0,
		explicitHubURLs: explicitHubURLs,
		answerer:        defaultCloudOfferAnswerer{},
		replay:          cert.NewReplayWindow(5 * time.Minute),
		syncCh:          make(chan struct{}, 1),
		status: Status{
			State:     StateDisabled,
			Detail:    "remote runtime disabled",
			DataDir:   cfg.DataDir,
			UpdatedAt: time.Now().UTC(),
		},
	}
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
		m.explicitHubURLs = nil
	} else {
		m.cfg.HubURLs = []string{hubURL}
		m.explicitHubURLs = map[string]struct{}{hubURL: {}}
	}
	m.explicitHubURL = hubURL != ""
	m.mu.Unlock()
}

func (m *Manager) AddHubURL(hubURL string) {
	m.addHubURL(hubURL, remotertc.AnswerOptions{}, false, false)
}

func (m *Manager) AddExplicitHubURL(hubURL string) {
	m.addHubURL(hubURL, remotertc.AnswerOptions{}, false, true)
}

func (m *Manager) AddHubURLWithAnswerOptions(hubURL string, opts remotertc.AnswerOptions) {
	m.addHubURL(hubURL, opts, true, false)
}

func (m *Manager) addHubURL(hubURL string, opts remotertc.AnswerOptions, hasOptions bool, explicit bool) {
	if m == nil {
		return
	}
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return
	}
	m.mu.Lock()
	if !containsString(m.cfg.HubURLs, hubURL) {
		m.cfg.HubURLs = append(m.cfg.HubURLs, hubURL)
	}
	if strings.TrimSpace(m.cfg.HubURL) == "" {
		m.cfg.HubURL = hubURL
	}
	if hasOptions {
		m.hubStateLocked(hubURL).AnswerOptions = opts
	}
	if explicit {
		m.explicitHubURL = true
		if m.explicitHubURLs == nil {
			m.explicitHubURLs = make(map[string]struct{})
		}
		m.explicitHubURLs[hubURL] = struct{}{}
	}
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
	m.hubStateLocked(hubURL).AnswerOptions = opts
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
	if controlURL != "" {
		m.cfg.ControlURL = controlURL
	}
	if accessToken != "" {
		m.cfg.AccessToken = accessToken
	}
	if region != "" {
		m.cfg.Region = region
	}
	if controlURL != "" || accessToken != "" {
		m.explicitHubURL = len(m.explicitHubURLs) > 0
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
	m.mu.Lock()
	if hubURL == "" || containsString(m.cfg.HubURLs, hubURL) || strings.TrimSpace(m.cfg.HubURL) == hubURL {
		remaining := removeString(m.cfg.HubURLs, hubURL)
		m.stopRemovedHubSignalingLocked(remaining)
		m.cfg.HubURLs = remaining
		delete(m.explicitHubURLs, hubURL)
		if len(remaining) > 0 {
			m.cfg.HubURL = remaining[0]
		} else {
			m.cfg.HubURL = ""
			m.explicitHubURL = false
			m.explicitHubURLs = nil
			m.answerOptions = remotertc.AnswerOptions{}
		}
	}
	m.mu.Unlock()
	m.TriggerSync()
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
	signalingCancel := m.signalingCancel
	var hubCancels []context.CancelFunc
	for _, state := range m.hubStates {
		if state != nil && state.SignalingCancel != nil {
			hubCancels = append(hubCancels, state.SignalingCancel)
		}
	}
	m.cancel = nil
	m.signalingCancel = nil
	m.hubStates = make(map[string]*hubRuntimeState)
	m.mu.Unlock()
	if signalingCancel != nil {
		signalingCancel()
	}
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
		online, lastErr := m.syncHubPresences(ctx, hubURLs)
		if online == 0 && lastErr != nil {
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
	err error
}

func (m *Manager) syncHubPresences(ctx context.Context, hubURLs []string) (int, error) {
	resultCh := make(chan hubSyncResult, len(hubURLs))
	pending := 0
	for _, hubURL := range hubURLs {
		hubURL = strings.TrimSpace(hubURL)
		if hubURL == "" {
			continue
		}
		pending++
		go func(hubURL string) {
			syncCtx, cancel := context.WithTimeout(ctx, hubPresenceSyncTimeout)
			defer cancel()
			err := m.syncHubPresence(syncCtx, hubURL)
			if err == nil {
				m.ensureHubSignalingLoop(ctx, hubURL)
			}
			resultCh <- hubSyncResult{err: err}
		}(hubURL)
	}
	if pending == 0 {
		return 0, nil
	}

	online := 0
	var lastErr error
	for pending > 0 {
		select {
		case <-ctx.Done():
			if online > 0 {
				return online, lastErr
			}
			return 0, ctx.Err()
		case result := <-resultCh:
			pending--
			if result.err != nil {
				lastErr = result.err
			} else {
				online++
			}
			if online > 0 {
				for pending > 0 {
					select {
					case result := <-resultCh:
						pending--
						if result.err != nil {
							lastErr = result.err
						} else {
							online++
						}
					default:
						return online, lastErr
					}
				}
				return online, lastErr
			}
		}
	}
	return online, lastErr
}

func (m *Manager) discoverHub(ctx context.Context) error {
	m.mu.RLock()
	controlURL := m.cfg.ControlURL
	accessToken := m.cfg.AccessToken
	hubURL := m.cfg.HubURL
	preferredRegion := m.cfg.Region
	explicitHubURL := m.explicitHubURL
	discoveredHubURL := m.discoveredHubURL
	m.mu.RUnlock()
	if controlURL == "" || accessToken == "" {
		return nil
	}
	if strings.TrimSpace(hubURL) != "" && explicitHubURL && strings.TrimSpace(discoveredHubURL) == "" {
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
		selectedURL := strings.TrimSpace(hub.HTTPURL)
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
		if state != nil && state.SignalingCancel != nil {
			state.SignalingCancel()
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
	dataDir := m.cfg.DataDir
	deviceName := m.cfg.DeviceName
	m.mu.RUnlock()
	if controlURL == "" || accessToken == "" {
		return fmt.Errorf("control URL and access token are required for device registration")
	}

	hostname, _ := os.Hostname()
	machineKey, err := identity.LoadOrCreateMachineKey(dataDir)
	if err != nil {
		return err
	}
	payload := discovery.DeviceRegistrationRequest{
		DeviceID:         m.identity.DeviceID,
		MachinePublicKey: identity.PublicKeyString(machineKey.PublicKey),
		DisplayName:      firstNonEmpty(m.identity.DisplayName, deviceName),
		Hostname:         hostname,
		Platform:         fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		State:            string(StateConfigured),
	}
	return discovery.RegisterDevice(ctx, controlURL, accessToken, payload)
}

func (m *Manager) syncHubPresence(ctx context.Context, hubURL string) error {
	hostname, _ := os.Hostname()

	m.mu.Lock()
	hubState := m.hubStateLocked(hubURL)
	hubSessionID := hubState.SessionID
	agentID := m.agentID
	dataDir := m.cfg.DataDir
	deviceName := m.cfg.DeviceName
	m.mu.Unlock()

	terminals := m.remoteHubTerminals(ctx)
	if hubSessionID == "" {
		machineKey, err := identity.LoadOrCreateMachineKey(dataDir)
		if err != nil {
			return err
		}
		if strings.TrimSpace(agentID) == "" {
			agentID = randomAgentID()
			m.mu.Lock()
			if strings.TrimSpace(m.agentID) == "" {
				m.agentID = agentID
			} else {
				agentID = m.agentID
			}
			m.mu.Unlock()
		}
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
		resp, err := discovery.RegisterHub(ctx, hubURL, hubv1.HubRegisterRequest{
			Version:        "remote.hub.v1",
			DeviceID:       m.identity.DeviceID,
			AgentID:        agentID,
			DisplayName:    firstNonEmpty(m.identity.DisplayName, deviceName),
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
		state := m.hubStateLocked(hubURL)
		state.SessionID = resp.AgentSessionID
		state.RTCServers = append([]hubv1.RTCIceServerConfig(nil), resp.RTCConfig.IceServers...)
		if hubURL == strings.TrimSpace(m.cfg.HubURL) {
			m.hubSessionID = resp.AgentSessionID
			m.hubRTCServers = append([]hubv1.RTCIceServerConfig(nil), resp.RTCConfig.IceServers...)
		}
		m.mu.Unlock()
		return nil
	}

	_, err := discovery.HeartbeatHub(ctx, hubURL, hubv1.HubHeartbeatRequest{
		AgentSessionID: hubSessionID,
		DeviceID:       m.identity.DeviceID,
		LastSeenAt:     time.Now().UTC().Format(time.RFC3339),
		Terminals:      terminals,
	})
	if err == nil {
		return nil
	}
	if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
		m.resetHubSession(hubURL)
		return m.syncHubPresence(ctx, hubURL)
	}
	return err
}

func (m *Manager) hubStateLocked(hubURL string) *hubRuntimeState {
	hubURL = strings.TrimSpace(hubURL)
	if m.hubStates == nil {
		m.hubStates = make(map[string]*hubRuntimeState)
	}
	state := m.hubStates[hubURL]
	if state == nil {
		state = &hubRuntimeState{}
		m.hubStates[hubURL] = state
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

func (m *Manager) ensureHubSignalingLoop(ctx context.Context, hubURL string) {
	m.mu.Lock()
	state := m.hubStateLocked(hubURL)
	if state.SignalingStarted || state.SessionID == "" || m.host == nil {
		m.mu.Unlock()
		return
	}
	state.SignalingStarted = true
	deviceID := m.identity.DeviceID
	agentSessionID := state.SessionID
	iceServers := append([]hubv1.RTCIceServerConfig(nil), state.RTCServers...)
	answerOptions := state.AnswerOptions
	loopCtx, cancel := context.WithCancel(ctx)
	state.SignalingCancel = cancel
	if hubURL == strings.TrimSpace(m.cfg.HubURL) {
		m.signalingStarted = true
		m.signalingCancel = cancel
	}
	m.mu.Unlock()

	go m.hubSignalingLoop(loopCtx, hubURL, deviceID, agentSessionID, iceServers, answerOptions)
	go m.hubPairingLoop(loopCtx, hubURL, deviceID, agentSessionID)
}

func (m *Manager) hubSignalingLoop(ctx context.Context, hubURL, deviceID, agentSessionID string, iceServers []hubv1.RTCIceServerConfig, hubAnswerOptions ...remotertc.AnswerOptions) {
	answerOptions := m.answerOptionsForHubLoop(hubAnswerOptions...)
	pendingAnswers := make(map[string]hubv1.SignalingAnswer)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pollCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		resp, ok, err := discovery.PollHubOffer(pollCtx, hubURL, hubv1.SignalingPollRequest{
			AgentSessionID: agentSessionID,
			DeviceID:       deviceID,
			TimeoutSeconds: 15,
		})
		cancel()
		if err != nil {
			if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
				m.resetHubSession(hubURL)
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
			answer = m.answerCloudOfferWithOptions(ctx, *resp.Offer, iceServers, answerOptions)
			pendingAnswers[offerKey] = answer
		}
		submitCtx, submitCancel := context.WithTimeout(ctx, 10*time.Second)
		err = discovery.SubmitHubAnswer(submitCtx, hubURL, hubv1.SubmitSignalingAnswerRequest{
			AgentSessionID: agentSessionID,
			DeviceID:       deviceID,
			Answer:         answer,
		})
		submitCancel()
		if err != nil {
			if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
				m.resetHubSession(hubURL)
				m.TriggerSync()
				return
			}
			m.setStatus(StateDegraded, err.Error())
			continue
		}
		delete(pendingAnswers, offerKey)
	}
}

func (m *Manager) answerOptionsForHubLoop(hubAnswerOptions ...remotertc.AnswerOptions) remotertc.AnswerOptions {
	if len(hubAnswerOptions) > 0 {
		return hubAnswerOptions[0]
	}
	m.mu.RLock()
	answerOptions := m.answerOptions
	m.mu.RUnlock()
	return answerOptions
}

func (m *Manager) hubPairingLoop(ctx context.Context, hubURL, deviceID, agentSessionID string) {
	pendingResults := make(map[string]hubv1.PairingResult)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if handled, stop := retrySubmitPendingPairingResult(ctx, m, hubURL, deviceID, agentSessionID, pendingResults); stop {
			return
		} else if handled {
			continue
		}

		pollCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		resp, ok, err := discovery.PollHubPairingClaim(pollCtx, hubURL, hubv1.PairingPollRequest{
			AgentSessionID: agentSessionID,
			DeviceID:       deviceID,
			TimeoutSeconds: 15,
		})
		cancel()
		if err != nil {
			if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
				m.resetHubSession(hubURL)
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
		if !ok || resp.Claim == nil {
			continue
		}

		claimKey := pairingClaimKey(*resp.Claim)
		result, hasPendingResult := pendingResults[claimKey]
		if !hasPendingResult {
			result = m.claimHubPairing(ctx, *resp.Claim)
			pendingResults[claimKey] = result
		}
		submitCtx, submitCancel := context.WithTimeout(ctx, 10*time.Second)
		err = discovery.SubmitHubPairingResult(submitCtx, hubURL, hubv1.SubmitPairingResultRequest{
			AgentSessionID: agentSessionID,
			DeviceID:       deviceID,
			Result:         result,
		})
		submitCancel()
		if err != nil {
			if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
				m.resetHubSession(hubURL)
				m.TriggerSync()
				return
			}
			m.setStatus(StateDegraded, err.Error())
			continue
		}
		delete(pendingResults, claimKey)
	}
}

func retrySubmitPendingPairingResult(ctx context.Context, m *Manager, hubURL, deviceID, agentSessionID string, pendingResults map[string]hubv1.PairingResult) (bool, bool) {
	for claimKey, result := range pendingResults {
		submitCtx, submitCancel := context.WithTimeout(ctx, 10*time.Second)
		err := discovery.SubmitHubPairingResult(submitCtx, hubURL, hubv1.SubmitPairingResultRequest{
			AgentSessionID: agentSessionID,
			DeviceID:       deviceID,
			Result:         result,
		})
		submitCancel()
		if err != nil {
			if discovery.IsHTTPStatus(err, http.StatusUnauthorized) {
				m.resetHubSession(hubURL)
				m.TriggerSync()
				return true, true
			}
			m.setStatus(StateDegraded, err.Error())
			select {
			case <-ctx.Done():
				return true, true
			case <-time.After(2 * time.Second):
				return true, false
			}
		}
		delete(pendingResults, claimKey)
		return true, false
	}
	return false, false
}

func pairingClaimKey(claim hubv1.PairingClaim) string {
	var b strings.Builder
	appendKeyPart := func(value string) {
		fmt.Fprintf(&b, "%d:", len(value))
		b.WriteString(value)
		b.WriteByte('|')
	}
	appendKeyPart(claim.ClaimID)
	appendKeyPart(claim.MachineID)
	appendKeyPart(claim.PairSessionID)
	appendKeyPart(claim.PairSecret)
	appendKeyPart(claim.AppDeviceID)
	appendKeyPart(claim.AppName)
	appendKeyPart(claim.AppPublicKey)
	for _, capability := range claim.RequestedCapabilities {
		appendKeyPart(capability)
	}
	return b.String()
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
	certificate, err := json.Marshal(out.AppCertificate)
	if err != nil {
		result.Error = fmt.Sprintf("encode app certificate: %v", err)
		return result
	}
	result.MachineName = out.MachineName
	result.MachinePublicKey = out.MachinePublicKey
	result.AppCertificate = certificate
	result.ExpiresAt = out.AppCertificate.Payload.ExpiresAt.Format(time.RFC3339)
	return result
}

func pairingpkgClaimRequest(claim hubv1.PairingClaim) pairing.ClaimRequest {
	return pairing.ClaimRequest{
		PairSessionID:         claim.PairSessionID,
		PairSecret:            claim.PairSecret,
		AppDeviceID:           claim.AppDeviceID,
		AppName:               claim.AppName,
		AppPublicKey:          claim.AppPublicKey,
		RequestedCapabilities: append([]string(nil), claim.RequestedCapabilities...),
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
	m.mu.RLock()
	answerOptions := m.answerOptions
	m.mu.RUnlock()
	return m.answerCloudOfferWithOptions(ctx, offer, iceServers, answerOptions)
}

func (m *Manager) answerCloudOfferWithOptions(ctx context.Context, offer hubv1.SignalingOffer, iceServers []hubv1.RTCIceServerConfig, answerOptions remotertc.AnswerOptions) hubv1.SignalingAnswer {
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
	answerOptions.ChannelPolicy = policy
	answerOptions.TerminalManagement = terminalManagement
	answerOptions.Events = m.eventRouter()
	flow := sessionflow.ManagedPlan(offerICEServers, sessionflow.RelayPolicy{
		AllowRelay:         offer.AllowRelay,
		AllowRelayTransfer: offer.AllowRelayTransfer,
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
	return answer
}

func (m *Manager) authorizeCloudOffer(ctx context.Context, offer hubv1.SignalingOffer) (cert.AppCertificateEnvelope, error) {
	if m == nil {
		return cert.AppCertificateEnvelope{}, fmt.Errorf("remote manager is nil")
	}
	certificate, appPublicKey, err := m.verifyOfferCertificate(offer)
	if err != nil {
		return cert.AppCertificateEnvelope{}, err
	}
	if strings.TrimSpace(certificate.Payload.MachineID) != strings.TrimSpace(offer.DeviceID) {
		return cert.AppCertificateEnvelope{}, fmt.Errorf("offer machine_id does not match app certificate")
	}
	terminalID := strings.TrimSpace(offer.TerminalID)
	if terminalID != "" {
		if !m.hasTerminal(ctx, terminalID) {
			return cert.AppCertificateEnvelope{}, fmt.Errorf("terminal %q is not available for remote access", terminalID)
		}
		if !hasAppCapability(certificate.Payload.Capabilities, "terminal") {
			return cert.AppCertificateEnvelope{}, fmt.Errorf("app certificate terminal capability is required")
		}
	} else if !hasAppCapability(certificate.Payload.Capabilities, "terminal") && !hasAppCapability(certificate.Payload.Capabilities, "terminal_management") {
		return cert.AppCertificateEnvelope{}, fmt.Errorf("app certificate terminal or terminal_management capability is required for machine-scoped cloud runtime")
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
	terminalID := strings.TrimSpace(offer.TerminalID)
	allowTerminal := hasAppCapability(capabilities, "terminal")
	return remotertc.ChannelPolicy{
		TerminalID:              terminalID,
		AllowTerminal:           allowTerminal,
		AllowAPI:                true,
		AllowFileManager:        hasAppCapability(capabilities, "file_manager"),
		AllowTerminalManagement: hasAppCapability(capabilities, "terminal_management") && terminalManagement != nil,
		AllowEvents:             allowTerminal,
		AllowRelayTransfer:      offer.AllowRelayTransfer,
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
	var cancel context.CancelFunc
	if hubURL = strings.TrimSpace(hubURL); hubURL != "" {
		state := m.hubStateLocked(hubURL)
		cancel = state.SignalingCancel
		state.SessionID = ""
		state.RTCServers = nil
		state.SignalingStarted = false
		state.SignalingCancel = nil
		if hubURL == strings.TrimSpace(m.cfg.HubURL) {
			m.hubSessionID = ""
			m.hubRTCServers = nil
			m.signalingStarted = false
			m.signalingCancel = nil
		}
	} else {
		cancel = m.signalingCancel
		m.hubSessionID = ""
		m.hubRTCServers = nil
		m.signalingStarted = false
		m.signalingCancel = nil
	}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
