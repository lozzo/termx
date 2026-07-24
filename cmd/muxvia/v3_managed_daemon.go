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
	controlReceipts, err := remotev2daemon.LoadControlReceiptStore(v3RemoteControlDir(), identity)
	if err != nil {
		return err
	}
	if _, err := controlReceipts.Enrollment(); err != nil {
		_ = controlReceipts.Close()
		return err
	}
	managedRuntime, err := remotev2daemon.NewManagedRuntime(identity.DeviceID, nil)
	if err != nil {
		_ = controlReceipts.Close()
		return err
	}
	hostname, _ := os.Hostname()
	metadata := &cloudpb.DeviceMetadata{
		DisplayName: hostname, Hostname: hostname, Platform: runtime.GOOS + "/" + runtime.GOARCH,
		MuxviaVersion: muxviaBuildVersion, SignalingVersions: []uint32{cloudcompanion.ProtocolVersionMax},
	}
	answerer := remotev2webrtc.Answerer{
		Handler: remotev2daemon.SessionAcceptor{
			Core: core, Identity: identity, AccessStore: clientAccess.Store, ManagedRuntime: managedRuntime,
		},
		OnSessionError: func(sessionErr error) {
			// DataChannel handler 已完成凭证脱敏；composition 只记录失败链，不接管 session 或重试。
			logger.Warn("managed data channel session stopped", "error", sessionErr)
		},
	}
	go func() {
		defer controlReceipts.Close()
		for {
			companion, openErr := openV3CloudDaemonCompanion(ctx)
			if openErr != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Warn("managed cloud companion unavailable; retrying", "error", openErr)
				if !waitV3ManagedPresenceRetry(ctx, v3ManagedPresenceRetryDelay) {
					return
				}
				continue
			}
			agent := remotev2daemon.Agent{
				Companion: companion, Identity: identity, Metadata: metadata, Answerer: answerer,
				Runtime: managedRuntime, AccessStore: clientAccess.Store, ControlReceipts: controlReceipts,
			}
			runErr := agent.RunContinuously(ctx, v3ManagedPresenceRetryDelay)
			_ = companion.Close()
			if runErr != nil && ctx.Err() == nil {
				// 明确 revoke 等非续约错误终止 Cloud；本地 listener 和 terminal lifecycle 继续运行。
				logger.Warn("managed cloud presence stopped", "error", runErr)
			}
			return
		}
	}()
	logger.Info("managed cloud presence starting", "device_id", identity.DeviceID, "device_fingerprint", identity.Fingerprint)
	return nil
}

func waitV3ManagedPresenceRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		delay = time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
