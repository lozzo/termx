package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/cloudpb"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
)

var openV3CloudDaemonCompanion = defaultOpenV3CloudDaemonCompanion

// v3ManagedDaemonCore 是 cloud DataChannel 可以进入 core-v2 的最小边界。
// 只有 remote daemon 完成 DeviceIdentity 与 CapabilityGrant 握手后才能调用 ServeScopedTransport。
type v3ManagedDaemonCore interface {
	ServeScopedTransport(context.Context, transport.Transport, corev2.TransportScope) error
}

func startV3ManagedDaemon(ctx context.Context, core v3ManagedDaemonCore, logger *slog.Logger) error {
	if core == nil {
		return fmt.Errorf("managed cloud daemon requires core-v2 server")
	}
	identity, err := remoteauth.LoadOrCreateLocalIdentity(v3RemoteIdentityDir())
	if err != nil {
		return err
	}
	revocations, err := remoteauth.LoadRevocationStore(filepath.Join(filepath.Dir(v3RemoteIdentityDir()), "revocations"))
	if err != nil {
		return err
	}
	companion, err := openV3CloudDaemonCompanion(ctx)
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	agent := remotev2daemon.Agent{
		Companion: companion,
		Identity:  identity,
		Metadata: &cloudpb.DeviceMetadata{
			DisplayName: hostname, Hostname: hostname, Platform: runtime.GOOS + "/" + runtime.GOARCH,
			TermxVersion: termxBuildVersion, SignalingVersions: []uint32{cloudcompanion.ProtocolVersionMax},
		},
		Answerer: remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
			Core: core, Identity: identity, Revocations: revocations,
		}},
	}
	go func() {
		defer companion.Close()
		if runErr := agent.Run(ctx); runErr != nil && ctx.Err() == nil {
			// managed presence 是 endpoint transport；失败不能停止本地 listener 或 core terminal lifecycle。
			logger.Warn("managed cloud presence stopped", "error", runErr)
		}
	}()
	logger.Info("managed cloud presence starting", "device_id", identity.DeviceID, "device_fingerprint", identity.Fingerprint)
	return nil
}
