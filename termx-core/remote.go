package termx

import (
	"context"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	remoteconfig "github.com/lozzow/termx/termx-core/internal/remote/config"
	remoteruntime "github.com/lozzow/termx/termx-core/internal/remote/runtime"
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

func WithRemoteConfig(cfg RemoteConfig) ServerOption {
	return func(sc *serverConfig) {
		sc.remoteConfig = cfg
	}
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
