package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/remoteauthpb"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
)

var v3DirectRuntimeAddresses struct {
	sync.RWMutex
	signaling string
	ice       string
}

const (
	defaultDirectSignalingAddress = "127.0.0.1:41120"
	defaultDirectICETCPAddress    = "127.0.0.1:41121"
)

// startV3DirectDaemon 启动当前 daemon 唯一的 embedded signaling 与共享 ICE-TCP mux。
// listener 使用同一 DeviceIdentity/AccessStore/Core；Direct 失败必须阻止 daemon 发布不可兑换的 Route hint。
func startV3DirectDaemon(ctx context.Context, core v3ManagedDaemonCore, clientAccess v3ClientAccessRuntime, logger *slog.Logger) (func(), error) {
	if core == nil || clientAccess.Store == nil {
		return nil, fmt.Errorf("Direct daemon requires core-v2 and client access store")
	}
	signalingAddress, iceAddress := v3DirectAddresses()
	signalingListener, err := net.Listen("tcp", signalingAddress)
	if err != nil {
		return nil, fmt.Errorf("listen Direct signaling %q: %w", signalingAddress, err)
	}
	iceListener, err := net.Listen("tcp", iceAddress)
	if err != nil {
		_ = signalingListener.Close()
		return nil, fmt.Errorf("listen Direct ICE-TCP %q: %w", iceAddress, err)
	}
	server, err := remotev2webrtc.NewDirectServer(clientAccess.Identity, remotev2daemon.SessionAcceptor{
		Core: core, Identity: clientAccess.Identity, AccessStore: clientAccess.Store,
	}, signalingListener, iceListener, nil)
	if err != nil {
		_ = signalingListener.Close()
		_ = iceListener.Close()
		return nil, err
	}
	actualSignaling := signalingListener.Addr().String()
	actualICE := iceListener.Addr().String()
	v3DirectRuntimeAddresses.Lock()
	v3DirectRuntimeAddresses.signaling = actualSignaling
	v3DirectRuntimeAddresses.ice = actualICE
	v3DirectRuntimeAddresses.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if serveErr := server.Serve(ctx); serveErr != nil && ctx.Err() == nil {
			logger.Error("Direct WebRTC server stopped", "error", serveErr)
		}
	}()
	logger.Info("Direct WebRTC server listening", "signaling", actualSignaling, "ice_tcp", actualICE)
	return func() {
		_ = server.Close()
		<-done
		v3DirectRuntimeAddresses.Lock()
		if v3DirectRuntimeAddresses.signaling == actualSignaling && v3DirectRuntimeAddresses.ice == actualICE {
			v3DirectRuntimeAddresses.signaling = ""
			v3DirectRuntimeAddresses.ice = ""
		}
		v3DirectRuntimeAddresses.Unlock()
	}, nil
}

// v3DirectPairingRoute 返回 pair create 签名进 bootstrap 的唯一 Direct Route Proto。
// RTC007 之前只发布 daemon 当前 listener locator；地址覆盖与 LAN seed 不在本切片提前实现。
func v3DirectPairingRoute() *remoteauthpb.EndpointRouteConfigV1 {
	signalingAddress, iceAddress := v3DirectAddresses()
	return &remoteauthpb.EndpointRouteConfigV1{
		SchemaVersion: endpoint.RouteConfigVersion, RouteId: "direct", Enabled: true,
		Route: &remoteauthpb.EndpointRouteConfigV1_DirectWebrtcTcp{DirectWebrtcTcp: &remoteauthpb.DirectWebRTCTCPRouteConfig{
			SignalingAddresses: []string{signalingAddress}, IceTcpAddresses: []string{iceAddress},
		}},
	}
}

func v3DirectAddresses() (string, string) {
	v3DirectRuntimeAddresses.RLock()
	activeSignaling := v3DirectRuntimeAddresses.signaling
	activeICE := v3DirectRuntimeAddresses.ice
	v3DirectRuntimeAddresses.RUnlock()
	if activeSignaling != "" && activeICE != "" {
		return activeSignaling, activeICE
	}
	signaling := strings.TrimSpace(os.Getenv("TERMX_DIRECT_SIGNALING_LISTEN"))
	if signaling == "" {
		signaling = defaultDirectSignalingAddress
	}
	ice := strings.TrimSpace(os.Getenv("TERMX_DIRECT_ICE_TCP_LISTEN"))
	if ice == "" {
		ice = defaultDirectICETCPAddress
	}
	return signaling, ice
}
