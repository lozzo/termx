package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/proto/wire"
	remotev2client "github.com/lozzow/termx/remote/client"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/tui/services"
	"github.com/lozzow/termx/tui/state"
)

var (
	dialV3ManagedSession = remotev2client.DialSession
)

// v3ManagedCloudEndpointDialer 把共享 connection registry、文件凭据、Cloud Companion 和公开 remote-v2 串成单 endpoint bundle。
// Companion 未安装、grant 缺失、WebRTC 或端到端授权失败都只返回当前 endpoint 错误，不允许 fallback 到 local、SSH 或旧 Hub API。
func v3ManagedCloudEndpointDialer() services.EndpointDialer {
	return func(ctx context.Context, cfg connection.Config) (services.EndpointServiceBundle, error) {
		if err := cloudcompanion.ValidateManagedConfig(cfg); err != nil {
			return services.EndpointServiceBundle{}, fmt.Errorf("managed cloud endpoint %q config: %w", cfg.ID, err)
		}
		companion, err := openV3CloudCompanion(ctx)
		if err != nil {
			return services.EndpointServiceBundle{}, fmt.Errorf("managed cloud endpoint %q: %w", cfg.ID, err)
		}
		if companion == nil {
			return services.EndpointServiceBundle{}, fmt.Errorf("managed cloud endpoint %q: %w", cfg.ID,
				cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "Cloud Companion returned no client"))
		}
		var companionCloser io.Closer
		if closer, ok := companion.(io.Closer); ok {
			companionCloser = closer
			defer func() {
				if companionCloser != nil {
					_ = companionCloser.Close()
				}
			}()
		}
		policy, err := cloudcompanion.DialPolicyForRelayMode(cfg.RelayMode)
		if err != nil {
			return services.EndpointServiceBundle{}, fmt.Errorf("managed cloud endpoint %q policy: %w", cfg.ID, err)
		}
		grant, err := remoteauth.NewCredentialStore(v3RemoteCredentialDir()).Resolve(cfg.GrantRef)
		if err != nil {
			return services.EndpointServiceBundle{}, fmt.Errorf("managed cloud endpoint %q credential: %w", cfg.ID, err)
		}
		session, err := dialV3ManagedSession(ctx, remotev2client.DialOptions{
			Companion: companion, EndpointID: string(cfg.ID), TargetDeviceID: cfg.HubDeviceID,
			DeviceFingerprint: cfg.DeviceFingerprint, CapabilityGrant: grant,
			RoutePreference: policy.RoutePreference, RelayOnly: policy.RelayOnly,
			QualityObservation: remotev2client.QualityObservationOptions{Enabled: true, NetworkClass: "unknown"},
		})
		if err != nil {
			return services.EndpointServiceBundle{}, fmt.Errorf("managed cloud endpoint %q dial: %w", cfg.ID, err)
		}
		client := protocol.NewClient(session.Transport)
		if err := client.Hello(ctx, protocol.Hello{Version: wire.Version, Client: "cmd/termx-v3:managed:" + string(cfg.ID)}); err != nil {
			_ = client.Close()
			return services.EndpointServiceBundle{}, fmt.Errorf("managed cloud endpoint %q hello: %w", cfg.ID, err)
		}
		if companionCloser != nil {
			// Companion 必须活到最终质量窗口完成；释放顺序不能让 terminal transport 等待 telemetry。
			closeManagedCompanionAfterSession(session, companionCloser)
			companionCloser = nil
		}
		terminal := services.ProtocolTerminalServiceAdapter{Client: client}
		core := services.ProtocolCoreClientAdapter{Client: client}
		path := services.ProtocolPathServiceAdapter{Client: client}
		return services.EndpointServiceBundle{
			EndpointID: state.EndpointID(cfg.ID), Terminal: terminal, Core: core,
			Surface: terminal, LiveEvents: terminal, Path: path,
			ObservedPath:         string(session.ObservedPath),
			RouteSelectionReason: string(session.RouteSelectionReason),
			Lifecycle:            services.EndpointLifecycle{Done: client.Done(), Err: client.Err},
		}, nil
	}
}

func closeManagedCompanionAfterSession(session remotev2client.Session, closer io.Closer) {
	go func() {
		if session.Transport != nil {
			<-session.Transport.Done()
		}
		if session.ObservationDone != nil {
			<-session.ObservationDone
		}
		_ = closer.Close()
	}()
}

func v3RemoteCredentialDir() string {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "termx", "remote-v2", "credentials")
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "termx", "remote-v2", "credentials")
	}
	return filepath.Join(os.TempDir(), "termx-state", "remote-v2", "credentials")
}
