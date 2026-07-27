//go:build cgo

package main

import (
	"context"
	"errors"
	"log"
	"strings"

	pionadapter "github.com/anytty/anytty/client/adapter/webrtc/pion"
	"github.com/anytty/anytty/client/binding"
	"github.com/anytty/anytty/client/binding/enginehost"
	clientruntime "github.com/anytty/anytty/client/runtime"
	"github.com/anytty/anytty/proto/bindingpb"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

var androidSessionAuthority = clientruntime.NewSessionGenerationAuthority()

// androidProductionHost 只装配 Android generation、Pion peer 与共享 engine host。
// credential/auth/Hello/API 逻辑不得在 JNI 包内形成平台分叉。
type androidProductionHost struct {
	*enginehost.Host
	broker *binding.PlatformBroker
}

func newAndroidProductionHost() (*androidProductionHost, error) {
	configureAndroidLogging()
	broker := binding.NewPlatformBroker()
	network, err := pionadapter.NewDefaultRouteNet()
	if err != nil {
		_ = broker.Close()
		return nil, err
	}
	peers := pionadapter.Factory{Network: network}
	host, err := enginehost.New(enginehost.Options{
		Broker: broker, DirectPeers: peers, ClientName: "anytty-android", CredentialPrefix: "android-access-",
		SessionAuthority: androidSessionAuthority, CloudProduct: cloudv1.ClientProduct_CLIENT_PRODUCT_ANDROID,
	})
	if err != nil {
		_ = broker.Close()
		return nil, err
	}
	return &androidProductionHost{Host: host, broker: broker}, nil
}

func (host *androidProductionHost) close() error { return host.Close() }

// OpenSession 只为 Android 本地诊断记录稳定错误链；公开事件仍由 binding 映射为脱敏 ApiError。
// adapter 错误禁止包含 grant、ticket、credential、SDP 或 bridge token。
func (host *androidProductionHost) OpenSession(ctx context.Context, request *bindingpb.OpenSessionRequest) (clientruntime.ApplicationReadyPeerSession, error) {
	session, err := host.Host.OpenSession(ctx, request)
	if err != nil {
		log.Printf("anytty binding open session failed: %s", androidErrorChain(err))
	}
	return session, err
}

func androidErrorChain(err error) string {
	parts := make([]string, 0, 4)
	for current := err; current != nil && len(parts) < 8; current = errors.Unwrap(current) {
		message := strings.TrimSpace(current.Error())
		if message != "" && (len(parts) == 0 || parts[len(parts)-1] != message) {
			parts = append(parts, message)
		}
	}
	return strings.Join(parts, ": ")
}
