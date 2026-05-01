package termx

import (
	"context"
	"io/fs"
	"net/http"
	"strings"

	remoteconfig "github.com/lozzow/termx/termx-core/internal/remote/config"
	"github.com/lozzow/termx/termx-core/internal/remote/identity"
	"github.com/lozzow/termx/termx-core/internal/remote/localweb"
	"github.com/lozzow/termx/termx-core/internal/remote/pairing"
)

type LocalWebOptions struct {
	HTTPURL       string
	LocalPairURL  string
	ICETCPEnabled bool
	ICETCPPort    int
	Assets        fs.FS
}

func NewLocalWebStaticAssets(files map[string]string) fs.FS {
	return localweb.NewStaticAssets(files)
}

func (s *Server) LocalWebHandler(opts LocalWebOptions) http.Handler {
	assets := opts.Assets
	if assets == nil {
		assets = localweb.EmbeddedAssets()
	}
	adapter := localWebServerAdapter{
		server:        s,
		httpURL:       strings.TrimSpace(opts.HTTPURL),
		iceTCPEnabled: opts.ICETCPEnabled,
		iceTCPPort:    opts.ICETCPPort,
	}
	return localweb.NewHandler(localweb.Config{
		Assets:    assets,
		Status:    adapter,
		Terminals: adapter,
		Pairing:   adapter,
	})
}

type localWebServerAdapter struct {
	server        *Server
	httpURL       string
	iceTCPEnabled bool
	iceTCPPort    int
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
		out = append(out, localweb.Terminal{
			TerminalID:   item.ID,
			Name:         item.Name,
			Command:      append([]string(nil), item.Command...),
			Cols:         int(item.Size.Cols),
			Rows:         int(item.Size.Rows),
			State:        string(item.State),
			LastActiveAt: item.CreatedAt,
		})
	}
	return out, nil
}

func (a localWebServerAdapter) ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	_ = ctx
	if a.server == nil {
		return pairing.ClaimResponse{}, nil
	}
	return a.server.remotePairClaim(req)
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
