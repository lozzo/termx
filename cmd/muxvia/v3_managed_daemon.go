package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	corev2 "github.com/muxvia/muxvia/core"
	"github.com/muxvia/muxvia/proto/cloudpb"
	remotev2daemon "github.com/muxvia/muxvia/remote/daemon"
	remotev2webrtc "github.com/muxvia/muxvia/remote/webrtc"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/transport"
)

var openV3CloudDaemonCompanion = defaultOpenV3CloudDaemonCompanion

// Hub presence 是短期租约；间隔只用于避免旧 stream 异常结束时形成续约风暴，不改变 fresh proof 语义。
var v3ManagedPresenceRetryDelay = time.Second

// v3ManagedDaemonCore 是 cloud DataChannel 可以进入 core-v2 的最小边界。
// 只有 remote daemon 完成 DeviceIdentity 与 CapabilityGrant 握手后才能调用 ServeScopedTransport。
type v3ManagedDaemonCore interface {
	ServeScopedTransport(context.Context, transport.Transport, corev2.TransportScope) error
}

func startV3ManagedDaemon(ctx context.Context, core v3ManagedDaemonCore, clientAccess v3ClientAccessRuntime, logger *slog.Logger) error {
	if core == nil {
		return fmt.Errorf("managed cloud daemon requires core-v2 server")
	}
	identity := clientAccess.Identity
	if err := identity.Validate(); err != nil {
		return err
	}
	if clientAccess.Store == nil {
		return fmt.Errorf("managed cloud daemon requires client access store")
	}
	companion, err := openV3CloudDaemonCompanion(ctx)
	if err != nil {
		return err
	}
	controlReceipts, err := remotev2daemon.LoadControlReceiptStore(v3RemoteControlDir(), identity)
	if err != nil {
		_ = companion.Close()
		return err
	}
	if _, err := controlReceipts.Enrollment(); err != nil {
		_ = controlReceipts.Close()
		_ = companion.Close()
		return err
	}
	managedRuntime, err := remotev2daemon.NewManagedRuntime(identity.DeviceID, nil)
	if err != nil {
		_ = controlReceipts.Close()
		_ = companion.Close()
		return err
	}
	hostname, _ := os.Hostname()
	agent := remotev2daemon.Agent{
		Companion: companion,
		Identity:  identity,
		Metadata: &cloudpb.DeviceMetadata{
			DisplayName: hostname, Hostname: hostname, Platform: runtime.GOOS + "/" + runtime.GOARCH,
			MuxviaVersion: muxviaBuildVersion, SignalingVersions: []uint32{cloudcompanion.ProtocolVersionMax},
		},
		Answerer: remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
			Core: core, Identity: identity, AccessStore: clientAccess.Store, ManagedRuntime: managedRuntime,
		}},
		Runtime:         managedRuntime,
		AccessStore:     clientAccess.Store,
		ControlReceipts: controlReceipts,
	}
	go func() {
		defer controlReceipts.Close()
		defer companion.Close()
		if runErr := agent.RunContinuously(ctx, v3ManagedPresenceRetryDelay); runErr != nil && ctx.Err() == nil {
			// managed presence 是 endpoint transport；失败不能停止本地 listener 或 core terminal lifecycle。
			logger.Warn("managed cloud presence stopped", "error", runErr)
		}
	}()
	logger.Info("managed cloud presence starting", "device_id", identity.DeviceID, "device_fingerprint", identity.Fingerprint)
	return nil
}
