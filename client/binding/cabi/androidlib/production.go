//go:build cgo

package main

import (
	pionadapter "github.com/muxvia/muxvia/client/adapter/webrtc/pion"
	"github.com/muxvia/muxvia/client/binding"
	"github.com/muxvia/muxvia/client/binding/enginehost"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
)

var androidSessionAuthority = clientruntime.NewSessionGenerationAuthority()

// androidProductionHost 只装配 Android generation、Pion peer 与共享 engine host。
// credential/auth/Hello/API 逻辑不得在 JNI 包内形成平台分叉。
type androidProductionHost struct {
	*enginehost.Host
	broker *binding.PlatformBroker
}

func newAndroidProductionHost() *androidProductionHost {
	broker := binding.NewPlatformBroker()
	peers := pionadapter.Factory{}
	host, err := enginehost.New(enginehost.Options{
		Broker: broker, DirectPeers: peers, ClientName: "muxvia-android", CredentialPrefix: "android-access-",
		SessionAuthority: androidSessionAuthority,
	})
	if err != nil {
		panic(err)
	}
	return &androidProductionHost{Host: host, broker: broker}
}

func (host *androidProductionHost) close() error { return host.Close() }
