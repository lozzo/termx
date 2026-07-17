package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
	unixtransport "github.com/lozzow/termx/shared/transport/unix"
)

// v3ClientAccessRuntime 是当前 daemon 进程唯一装配的 DeviceIdentity、AccessStore 与 local pairing socket。
// local protocol、SSH stdio、future direct TLS 和 managed WebRTC 都必须引用这里的同一 Identity/Store，不能各自加载第二份授权真值。
type v3ClientAccessRuntime struct {
	Identity      remoteauth.Identity
	Store         *remoteauth.AccessStore
	PairingSocket string
}

// Close 释放 daemon-local AccessStore 的唯一进程 owner lock。
// 调用方必须先停止 pairing listener、managed ingress 和 core session，再结束该 runtime。
func (runtime v3ClientAccessRuntime) Close() error {
	if runtime.Store == nil {
		return nil
	}
	return runtime.Store.Close()
}

func loadV3ClientAccessRuntime(socketPath string) (v3ClientAccessRuntime, error) {
	identity, err := remoteauth.LoadOrCreateLocalIdentity(v3RemoteIdentityDir())
	if err != nil {
		return v3ClientAccessRuntime{}, err
	}
	store, err := remoteauth.LoadAccessStore(v3RemoteAccessDir(), identity, remoteauth.AccessStoreOptions{})
	if err != nil {
		return v3ClientAccessRuntime{}, err
	}
	return v3ClientAccessRuntime{Identity: identity, Store: store, PairingSocket: v3PairingSocketPath(socketPath)}, nil
}

func v3RemoteAccessDir() string {
	return filepath.Join(filepath.Dir(v3RemoteIdentityDir()), "access")
}

func v3PairingSocketPath(socketPath string) string {
	return strings.TrimSpace(socketPath) + ".pair"
}

// startV3PairingListener 启动 owner-only Unix PairingExchange listener。
// listener 只运行 remoteauth v2，不接受 termx Hello 或 capability session；context 取消会关闭 socket，且不会影响 core local listener。
func startV3PairingListener(ctx context.Context, runtime v3ClientAccessRuntime, logger *slog.Logger) (func(), error) {
	binding, err := remoteauth.LocalUnixChannelBinding(runtime.PairingSocket)
	if err != nil {
		return nil, err
	}
	listener, err := unixtransport.NewListener(runtime.PairingSocket)
	if err != nil {
		return nil, fmt.Errorf("listen for local PairingExchange: %w", err)
	}
	acceptor := remotev2daemon.PairingAcceptor{Identity: runtime.Identity, AccessStore: runtime.Store}
	serveCtx, cancel := context.WithCancel(ctx)
	acceptDone := make(chan struct{})
	shutdownDone := make(chan struct{})
	var handlers sync.WaitGroup
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			cancel()
			_ = listener.Close()
			<-acceptDone
			handlers.Wait()
			close(shutdownDone)
		})
	}
	go func() {
		defer close(acceptDone)
		for {
			connection, acceptErr := listener.Accept(serveCtx)
			if acceptErr != nil {
				if serveCtx.Err() == nil && logger != nil {
					logger.Warn("local PairingExchange listener stopped", "error", acceptErr)
				}
				return
			}
			handlers.Add(1)
			go func(connection transport.Transport) {
				defer handlers.Done()
				if serveErr := acceptor.ServeBoundTransport(serveCtx, connection, binding); serveErr != nil && serveCtx.Err() == nil && logger != nil {
					logger.Debug("local PairingExchange rejected", "error", serveErr)
				}
			}(connection)
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			shutdown()
		case <-shutdownDone:
		}
	}()
	return shutdown, nil
}

type v3ClientAccessService struct {
	identity remoteauth.Identity
	store    *remoteauth.AccessStore
}

// Identity 返回当前 daemon 进程统一 DeviceIdentity 的公开投影；私钥永远不进入 protocol response。
func (service v3ClientAccessService) Identity(context.Context) (protocol.ClientAccessIdentityResult, error) {
	if err := service.identity.Validate(); err != nil {
		return protocol.ClientAccessIdentityResult{}, err
	}
	return protocol.ClientAccessIdentityResult{
		DeviceID: service.identity.DeviceID, DeviceFingerprint: service.identity.Fingerprint,
		DevicePublicKey: append([]byte(nil), service.identity.PublicKey...),
	}, nil
}

// CreateTicket 把已通过 transport scope 校验的 owner 请求交给唯一 AccessStore 原子签发并登记。
func (service v3ClientAccessService) CreateTicket(_ context.Context, params protocol.ClientAccessTicketCreateParams) (protocol.ClientAccessTicketCreateResult, error) {
	if service.store == nil {
		return protocol.ClientAccessTicketCreateResult{}, fmt.Errorf("client access store is unavailable")
	}
	ticketTTL, err := checkedSecondsDuration(params.TicketTTLSeconds, "ticket ttl")
	if err != nil {
		return protocol.ClientAccessTicketCreateResult{}, err
	}
	grantLifetime, err := checkedSecondsDuration(params.GrantLifetimeSeconds, "grant lifetime")
	if err != nil {
		return protocol.ClientAccessTicketCreateResult{}, err
	}
	bundle, claims, err := service.store.IssuePairingBundle(remoteauth.PairingIssueOptions{
		Label: params.Label, Scope: params.Scope, TicketTTL: ticketTTL, GrantLifetime: grantLifetime,
	})
	if err != nil {
		return protocol.ClientAccessTicketCreateResult{}, err
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		return protocol.ClientAccessTicketCreateResult{}, err
	}
	return protocol.ClientAccessTicketCreateResult{Bundle: payload, TicketID: claims.TicketID, ExpiresAt: claims.ExpiresAt}, nil
}

// List 返回 AccessStore 的脱敏授权记录；该调用不刷新时间戳或写入本地状态。
func (service v3ClientAccessService) List(context.Context) (protocol.ClientAccessListResult, error) {
	if service.store == nil {
		return protocol.ClientAccessListResult{}, fmt.Errorf("client access store is unavailable")
	}
	return protocol.ClientAccessListResult{Records: service.store.ListClientAccess()}, nil
}

// Revoke 由 owning daemon 原子持久化撤销状态；删除客户端本地 credential 不能替代该操作。
func (service v3ClientAccessService) Revoke(_ context.Context, params protocol.ClientAccessRevokeParams) (protocol.ClientAccessRecord, error) {
	if service.store == nil {
		return protocol.ClientAccessRecord{}, fmt.Errorf("client access store is unavailable")
	}
	record, err := service.store.RevokeGrant(params.GrantID)
	if err != nil {
		return protocol.ClientAccessRecord{}, err
	}
	return record, nil
}

func checkedSecondsDuration(seconds int64, field string) (time.Duration, error) {
	if seconds <= 0 || seconds > int64((365*24*time.Hour)/time.Second) {
		return 0, fmt.Errorf("%s must be between one second and one year", field)
	}
	return time.Duration(seconds) * time.Second, nil
}
