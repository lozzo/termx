package runtime

import (
	"context"
	"fmt"
	"net/http"
	"os"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
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
}

func NewManager(cfg remoteconfig.Config, provider InventoryProvider, host bridge.TransportSink) *Manager {
	cfg = remoteconfig.Normalize(cfg)
	return &Manager{
		cfg:      cfg,
		provider: provider,
		host:     host,
		files:    fileapi.NewManager(),
		syncCh:   make(chan struct{}, 1),
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

func (m *Manager) syncControlRegistration(ctx context.Context) error {
	if m.cfg.ControlURL == "" || m.cfg.AccessToken == "" {
		return fmt.Errorf("control URL and access token are required for device registration")
	}

	hostname, _ := os.Hostname()
	var terminals []TerminalInventoryItem
	if m.provider != nil {
		terminals = m.provider.ListRemoteTerminals(ctx)
	}
	payload := discovery.DeviceRegistrationRequest{
		DeviceID:    m.identity.DeviceID,
		DisplayName: firstNonEmpty(m.identity.DisplayName, m.cfg.DeviceName),
		Hostname:    hostname,
		Platform:    fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		State:       string(StateConfigured),
		Terminals:   make([]discovery.DeviceRegistrationTerminal, 0, len(terminals)),
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
		resp, err := discovery.RegisterHub(ctx, m.cfg.HubURL, hubv1.HubRegisterRequest{
			Version:        "remote.hub.v1",
			DeviceID:       m.identity.DeviceID,
			DisplayName:    firstNonEmpty(m.identity.DisplayName, m.cfg.DeviceName),
			Hostname:       hostname,
			Platform:       fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
			RuntimeVersion: "termx-dev",
			Terminals:      terminals,
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

		terminalManagement := m.terminalManagementRouter()
		answer, err := remotertc.AnswerOfferWithOptions(ctx, *resp.Offer, iceServers, m.host, m.files, remotertc.AnswerOptions{
			ChannelPolicy: remotertc.ChannelPolicy{
				TerminalID:              strings.TrimSpace(resp.Offer.TerminalID),
				AllowTerminal:           true,
				AllowFileManager:        true,
				AllowTerminalManagement: terminalManagement != nil,
			},
			TerminalManagement: terminalManagement,
		})
		if err != nil {
			answer = hubv1.SignalingAnswer{
				SessionID: resp.Offer.SessionID,
				Error:     err.Error(),
			}
		}
		submitCtx, submitCancel := context.WithTimeout(ctx, 10*time.Second)
		_ = discovery.SubmitHubAnswer(submitCtx, m.cfg.HubURL, hubv1.SubmitSignalingAnswerRequest{
			AgentSessionID: agentSessionID,
			DeviceID:       deviceID,
			Answer:         answer,
		})
		submitCancel()
	}
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
