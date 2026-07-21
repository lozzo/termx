// Package daemon owns the authorized remote DataChannel-to-core-v2 boundary.
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"

	core "github.com/muxvia/muxvia/core"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/proto/remoteauthpb"
	remotewebrtc "github.com/muxvia/muxvia/remote/webrtc"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"github.com/muxvia/muxvia/shared/transport"
)

// ScopedTransportServer 是 remote-v2 允许调用的 core-v2 transport 边界。
// 实现方只能按已经验证的 capability scope 建立 protocol session，不能把远程 DataChannel 当作本地无限权限 listener。
type ScopedTransportServer interface {
	// ServeScopedTransport 按 daemon 已验证的 scope 服务同一条 DataChannel 上的 muxvia protocol。
	ServeScopedTransport(context.Context, transport.Transport, core.TransportScope) error
}

type observedScopedTransportServer interface {
	ScopedTransportServer
	ServeScopedTransportObserved(context.Context, transport.Transport, core.TransportScope, core.TransportLifecycleObserver) error
}

type outboundDrainer interface {
	Drain(context.Context) error
}

// SessionAcceptor 是远程 DataChannel 进入 core-v2 的唯一授权入口。
// Identity、grant expiry/revoke 和 scope mapping 都属于 daemon；Cloud Companion、Hub admission 或 signaling 结果不能预授权 session。
type SessionAcceptor struct {
	Core        ScopedTransportServer
	Identity    remoteauth.Identity
	AccessStore *remoteauth.AccessStore
	Random      io.Reader
	Now         func() time.Time
	// ManagedRuntime 只用于 Cloud managed offer；Direct/SSH 调用 ServeDataChannel 时不进入 registry。
	ManagedRuntime *ManagedRuntime
}

// ServeDataChannel 在同一 transport 上完成 DeviceHello/CapabilityOpen，成功后才调用 core-v2。
// daemonDTLSFingerprint 必须来自当前 Pion 本端 DTLSTransport；失败会关闭当前 channel，且不创建 protocol session 或 fallback。
func (acceptor SessionAcceptor) ServeDataChannel(ctx context.Context, connection transport.Transport, daemonDTLSFingerprint string) error {
	binding, err := remoteauth.DTLSChannelBinding(daemonDTLSFingerprint)
	if err != nil {
		if connection != nil {
			_ = connection.Close()
		}
		return fmt.Errorf("bind remote data channel: %w", err)
	}
	return acceptor.ServeBoundTransport(ctx, connection, binding)
}

// ServeManagedDataChannel 使用 Hub 已验证 SessionContext 服务 Cloud managed DataChannel。
// Hub metadata 只用于 registry correlation；CapabilityGrant 仍必须在当前 DataChannel 内重新验证。
func (acceptor SessionAcceptor) ServeManagedDataChannel(ctx context.Context, connection transport.Transport, daemonDTLSFingerprint string, sessionContext remotewebrtc.ManagedSessionContext, owner remotewebrtc.ManagedSessionOwner) error {
	binding, err := remoteauth.DTLSChannelBinding(daemonDTLSFingerprint)
	if err != nil {
		if connection != nil {
			_ = connection.Close()
		}
		return fmt.Errorf("bind managed remote data channel: %w", err)
	}
	return acceptor.serveBoundTransport(ctx, connection, binding, &sessionContext, owner)
}

// ServeBoundTransport 在任意已提供可信 TLS/DTLS channel binding 的 transport 上执行同一 v2 auth 状态机。
// PairingExchange 成功后只关闭 transport；只有 client-bound capability 成功才把 scope 交给 core-v2，二者不能复用同一 channel。
func (acceptor SessionAcceptor) ServeBoundTransport(ctx context.Context, connection transport.Transport, binding remoteauth.ChannelBinding) error {
	return acceptor.serveBoundTransport(ctx, connection, binding, nil, nil)
}

func (acceptor SessionAcceptor) serveBoundTransport(ctx context.Context, connection transport.Transport, binding remoteauth.ChannelBinding, managed *remotewebrtc.ManagedSessionContext, owner remotewebrtc.ManagedSessionOwner) error {
	if acceptor.Core == nil {
		return fmt.Errorf("remote daemon core transport server is not configured")
	}
	if connection == nil {
		return fmt.Errorf("remote daemon data channel transport is not configured")
	}
	defer connection.Close()
	if acceptor.AccessStore == nil || !acceptor.AccessStore.Available() {
		return fmt.Errorf("remote daemon client access store is not configured")
	}
	if binding.Kind == remoteauthpb.ChannelBindingKind_CHANNEL_BINDING_KIND_LOCAL_UNIX {
		return fmt.Errorf("remote daemon session ingress rejects local Unix binding")
	}
	result, err := (remoteauth.ServerHandshake{
		Identity: acceptor.Identity, AccessStore: acceptor.AccessStore,
		Random: acceptor.Random, Now: acceptor.Now,
	}).Accept(ctx, connection, binding)
	if err != nil {
		return fmt.Errorf("authorize remote transport: %w", err)
	}
	if result.Mode == remoteauth.ServerHandshakeModePairing {
		// PairingAccepted 是该 DataChannel 的最后一帧。Send 成功只表示进入底层队列，
		// 必须在关闭 pairing-only transport 前 drain，避免 peer close 丢弃已签发 grant。
		if drainer, ok := connection.(outboundDrainer); ok {
			if err := drainer.Drain(ctx); err != nil {
				return fmt.Errorf("drain PairingExchange response: %w", err)
			}
		}
		return nil
	}
	if result.Mode != remoteauth.ServerHandshakeModeCapability {
		return fmt.Errorf("authorize remote transport: unsupported handshake mode %d", result.Mode)
	}
	claims := result.Claims
	scope := core.TransportScope{
		PrincipalID:        claims.SubjectKeyFingerprint,
		AllowDaemon:        claims.Scope.AllowDaemon,
		TerminalID:         claims.Scope.TerminalID,
		MachineEventsOnly:  claims.Scope.MachineEventsOnly,
		FileReadMetadata:   claims.Scope.FileReadMetadata,
		FileReadContent:    claims.Scope.FileReadContent,
		FileWriteContent:   claims.Scope.FileWriteContent,
		FileMutate:         claims.Scope.FileMutate,
		ManageClientAccess: claims.Scope.ManageClientAccess,
	}
	if managed == nil {
		return acceptor.Core.ServeScopedTransport(ctx, connection, scope)
	}
	if acceptor.ManagedRuntime == nil || owner == nil || owner.Done() == nil {
		return fmt.Errorf("remote daemon managed session runtime is not configured")
	}
	observedCore, ok := acceptor.Core.(observedScopedTransportServer)
	if !ok {
		return fmt.Errorf("remote daemon core transport lifecycle observer is not configured")
	}
	registry := acceptor.ManagedRuntime.Registry()
	if registry == nil || managed.ManagedSessionID == "" || managed.SessionIncarnation == 0 || managed.ClientDeviceID == "" || managed.PresenceSessionID == "" || managed.AssignmentEpoch == 0 || managed.ObservedPath == 0 {
		return fmt.Errorf("remote daemon managed session context is not bound")
	}
	now := time.Now().UTC()
	if acceptor.Now != nil {
		now = acceptor.Now().UTC()
	}
	closer := newManagedRegistryCloser(owner)
	projection := &cloudpb.ManagedPeerSessionProjection{
		Target:         &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: acceptor.Identity.DeviceID, ManagedSessionId: managed.ManagedSessionID, SessionIncarnation: managed.SessionIncarnation, AssignmentEpoch: managed.AssignmentEpoch, ControlPresenceSessionId: managed.PresenceSessionID, DaemonRuntimeGeneration: acceptor.ManagedRuntime.RuntimeGeneration()},
		ClientDeviceId: managed.ClientDeviceID, EstablishedPresenceSessionId: managed.PresenceSessionID,
		AuthenticatedClientFingerprint: claims.SubjectKeyFingerprint, OpaqueAccessReference: OpaqueAccessReference(acceptor.Identity.DeviceID, claims.GrantID),
		ControlOwnerHubId: registry.controlOwnerHubID, ObservedDataPath: managed.ObservedPath,
		State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED, Freshness: cloudpb.Freshness_FRESHNESS_FRESH,
		ConnectedAtUnixMillis: now.UnixMilli(), ObservedAtUnixMillis: now.UnixMilli(), FreshUntilUnixMillis: now.Add(5 * time.Minute).UnixMilli(),
	}
	handle, _, err := registry.Begin(projection, closer, now)
	if err != nil {
		owner.RequestClose()
		return err
	}
	observer := &managedHelloObserver{handle: handle, closer: closer, now: acceptor.Now}
	serveErr := observedCore.ServeScopedTransportObserved(ctx, connection, scope, observer)
	_ = connection.Close()
	owner.RequestClose()
	<-owner.Done()
	closedAt := time.Now().UTC()
	if acceptor.Now != nil {
		closedAt = acceptor.Now().UTC()
	}
	reason := "peer_closed"
	if serveErr != nil {
		reason = "protocol_closed"
	}
	_, closeErr := handle.MarkClosed(reason, closedAt)
	closer.markClosed()
	if serveErr != nil {
		return serveErr
	}
	return closeErr
}

type managedRegistryCloser struct {
	owner remotewebrtc.ManagedSessionOwner
	done  chan struct{}
	once  sync.Once
}

func newManagedRegistryCloser(owner remotewebrtc.ManagedSessionOwner) *managedRegistryCloser {
	return &managedRegistryCloser{owner: owner, done: make(chan struct{})}
}

func (closer *managedRegistryCloser) RequestClose()         { closer.owner.RequestClose() }
func (closer *managedRegistryCloser) Done() <-chan struct{} { return closer.done }
func (closer *managedRegistryCloser) markClosed() {
	closer.once.Do(func() { close(closer.done) })
}

type managedHelloObserver struct {
	handle *ManagedSessionHandle
	closer *managedRegistryCloser
	now    func() time.Time
}

func (observer *managedHelloObserver) HelloAccepted() {
	observedAt := time.Now().UTC()
	if observer.now != nil {
		observedAt = observer.now().UTC()
	}
	if _, err := observer.handle.MarkReady(observedAt); err != nil {
		observer.closer.RequestClose()
	}
}

// OpaqueAccessReference 返回 Cloud 管理投影使用的不可逆 grant reference。
// 它不包含 grant body、terminal ID、scope 或 client public key。
func OpaqueAccessReference(daemonDeviceID, grantID string) string {
	digest := sha256.Sum256([]byte("muxvia-access-ref-v1\x00" + daemonDeviceID + "\x00" + grantID))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// PairingAcceptor 是 owner-only local Unix pairing socket 的受限 daemon 入口。
// 它复用同一 DeviceIdentity/AccessStore 与 channel-bound PairingExchange，但永远不持有 core server，因此 capability open 不能借此进入 terminal protocol。
type PairingAcceptor struct {
	Identity    remoteauth.Identity
	AccessStore *remoteauth.AccessStore
	Random      io.Reader
	Now         func() time.Time
}

// ServeBoundTransport 完成一次 local pairing exchange 并关闭当前 transport。
// capability open、缺失 AccessStore 或非 pairing 成功模式都返回错误，不提供 local-owner terminal fallback。
func (acceptor PairingAcceptor) ServeBoundTransport(ctx context.Context, connection transport.Transport, binding remoteauth.ChannelBinding) error {
	if connection == nil {
		return fmt.Errorf("remote daemon pairing transport is not configured")
	}
	if acceptor.AccessStore == nil || !acceptor.AccessStore.Available() {
		return fmt.Errorf("remote daemon pairing store is not configured")
	}
	if binding.Kind != remoteauthpb.ChannelBindingKind_CHANNEL_BINDING_KIND_LOCAL_UNIX {
		return fmt.Errorf("owner-only pairing acceptor requires local Unix binding")
	}
	defer connection.Close()
	result, err := (remoteauth.ServerHandshake{
		Identity: acceptor.Identity, AccessStore: acceptor.AccessStore, PairingOnly: true,
		Random: acceptor.Random, Now: acceptor.Now,
	}).Accept(ctx, connection, binding)
	if err != nil {
		return fmt.Errorf("serve PairingExchange: %w", err)
	}
	if result.Mode != remoteauth.ServerHandshakeModePairing {
		return fmt.Errorf("pairing transport rejected non-pairing auth mode %d", result.Mode)
	}
	return nil
}
