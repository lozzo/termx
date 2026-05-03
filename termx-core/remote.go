package termx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	remoteconfig "github.com/lozzow/termx/termx-core/internal/remote/config"
	"github.com/lozzow/termx/termx-core/internal/remote/identity"
	"github.com/lozzow/termx/termx-core/internal/remote/pairing"
	remoteruntime "github.com/lozzow/termx/termx-core/internal/remote/runtime"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/transport"
)

type RemoteRuntimeState string

const (
	RemoteStateDisabled    RemoteRuntimeState = "disabled"
	RemoteStateConfigured  RemoteRuntimeState = "configured"
	RemoteStateRegistering RemoteRuntimeState = "registering"
	RemoteStateOnline      RemoteRuntimeState = "online"
	RemoteStateDegraded    RemoteRuntimeState = "degraded"
)

type RemoteConfig struct {
	Enabled     bool
	ControlURL  string
	HubURL      string
	AccessToken string
	DataDir     string
	DeviceName  string
	Region      string
}

type RemoteStatus struct {
	State         RemoteRuntimeState `json:"state"`
	Detail        string             `json:"detail,omitempty"`
	DeviceID      string             `json:"device_id,omitempty"`
	DeviceName    string             `json:"device_name,omitempty"`
	ControlURL    string             `json:"control_url,omitempty"`
	HubURL        string             `json:"hub_url,omitempty"`
	DataDir       string             `json:"data_dir,omitempty"`
	TerminalCount int                `json:"terminal_count"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

type PairStartOptions struct {
	LocalPairURL string
	TTL          time.Duration
}

func WithRemoteConfig(cfg RemoteConfig) ServerOption {
	return func(sc *serverConfig) {
		sc.remoteConfig = cfg
	}
}

func (s *Server) RemotePairStart(opts PairStartOptions) (protocol.PairStartResult, error) {
	if s == nil {
		return protocol.PairStartResult{}, fmt.Errorf("server is nil")
	}
	cfg := remoteconfig.Normalize(remoteconfig.Config{
		Enabled:    s.cfg.remoteConfig.Enabled,
		DataDir:    s.cfg.remoteConfig.DataDir,
		DeviceName: s.cfg.remoteConfig.DeviceName,
	})
	machineKey, err := identity.LoadOrCreateMachineKey(cfg.DataDir)
	if err != nil {
		return protocol.PairStartResult{}, err
	}
	machineID := ""
	machineName := strings.TrimSpace(cfg.DeviceName)
	if s.remoteManager != nil {
		status := s.remoteManager.Status()
		machineID = strings.TrimSpace(status.DeviceID)
		if machineName == "" {
			machineName = strings.TrimSpace(status.DeviceName)
		}
	}
	if machineID == "" {
		ident, err := identity.LoadOrCreate(cfg.DataDir, cfg.DeviceName)
		if err != nil {
			return protocol.PairStartResult{}, err
		}
		machineID = ident.DeviceID
		if machineName == "" {
			machineName = ident.DisplayName
		}
	}
	if strings.TrimSpace(opts.LocalPairURL) == "" {
		return protocol.PairStartResult{}, fmt.Errorf("local_pair_url is required")
	}
	pairCfg := pairing.Config{
		MachineID:    machineID,
		MachineName:  machineName,
		MachineKey:   machineKey,
		LocalPairURL: opts.LocalPairURL,
	}
	s.remotePairMu.Lock()
	if s.remotePairing == nil {
		s.remotePairing = pairing.NewManager(pairCfg)
		s.remotePairCfg = pairCfg
	} else if pairManagerConfigChanged(s.remotePairCfg, pairCfg) {
		if err := s.remotePairing.UpdateConfig(pairCfg); err != nil {
			s.remotePairMu.Unlock()
			return protocol.PairStartResult{}, err
		}
		s.remotePairCfg = pairCfg
	}
	manager := s.remotePairing
	s.remotePairMu.Unlock()

	session, err := manager.CreateSession(opts.TTL)
	if err != nil {
		return protocol.PairStartResult{}, err
	}
	return protocol.PairStartResult{
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

func pairManagerConfigChanged(a, b pairing.Config) bool {
	if a.MachineID != b.MachineID || a.MachineName != b.MachineName || a.LocalPairURL != b.LocalPairURL {
		return true
	}
	return !a.MachineKey.PublicKey.Equal(b.MachineKey.PublicKey)
}

func (s *Server) RemoteStatus() RemoteStatus {
	if s == nil || s.remoteManager == nil {
		return RemoteStatus{
			State:     RemoteStateDisabled,
			Detail:    "remote runtime disabled",
			UpdatedAt: time.Now().UTC(),
		}
	}
	status := s.remoteManager.Status()
	return RemoteStatus{
		State:         mapRemoteState(status.State),
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

func newRemoteManager(cfg RemoteConfig, provider remoteruntime.InventoryProvider, host bridge.TransportSink) *remoteruntime.Manager {
	return remoteruntime.NewManager(remoteconfig.Config{
		Enabled:     cfg.Enabled,
		ControlURL:  cfg.ControlURL,
		HubURL:      cfg.HubURL,
		AccessToken: cfg.AccessToken,
		DataDir:     cfg.DataDir,
		DeviceName:  cfg.DeviceName,
		Region:      cfg.Region,
	}, provider, host)
}

type remoteInventoryProvider struct {
	server *Server
}

func (p remoteInventoryProvider) ListRemoteTerminals(ctx context.Context) []remoteruntime.TerminalInventoryItem {
	if p.server == nil {
		return nil
	}
	list, err := p.server.List(ctx)
	if err != nil {
		return nil
	}
	out := make([]remoteruntime.TerminalInventoryItem, 0, len(list))
	for _, item := range list {
		out = append(out, remoteruntime.TerminalInventoryItem{
			ID:      item.ID,
			Name:    item.Name,
			State:   string(item.State),
			Command: append([]string(nil), item.Command...),
			Cols:    int(item.Size.Cols),
			Rows:    int(item.Size.Rows),
		})
	}
	return out
}

func (p remoteInventoryProvider) ServeRemoteTransport(ctx context.Context, t transport.Transport, remote string) error {
	if p.server == nil {
		return nil
	}
	return p.server.ServeTransport(ctx, t, remote)
}

func mapRemoteState(state remoteruntime.State) RemoteRuntimeState {
	switch state {
	case remoteruntime.StateConfigured:
		return RemoteStateConfigured
	case remoteruntime.StateRegistering:
		return RemoteStateRegistering
	case remoteruntime.StateOnline:
		return RemoteStateOnline
	case remoteruntime.StateDegraded:
		return RemoteStateDegraded
	default:
		return RemoteStateDisabled
	}
}
