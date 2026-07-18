package main

import (
	pionadapter "github.com/lozzow/termx/client/adapter/managed/pion"
	"github.com/lozzow/termx/client/binding"
	"github.com/lozzow/termx/client/binding/managedhost"
	clientruntime "github.com/lozzow/termx/client/runtime"
)

var androidSessionAuthority = clientruntime.NewSessionGenerationAuthority()

// androidProductionHost 只装配 Android generation、Pion peer 与共享 managed host。
// Cloud/credential/auth/Hello/API 逻辑不得在 JNI 包内形成平台分叉。
type androidProductionHost struct {
	*managedhost.Host
	broker *binding.PlatformBroker
}

func newAndroidProductionHost() *androidProductionHost {
	broker := binding.NewPlatformBroker()
	host, err := managedhost.New(managedhost.Options{
		Broker: broker, Peers: pionadapter.Factory{}, ClientName: "termx-android", CredentialPrefix: "android-access-",
		SessionAuthority: androidSessionAuthority,
	})
	if err != nil {
		panic(err)
	}
	return &androidProductionHost{Host: host, broker: broker}
}

func (host *androidProductionHost) close() error { return host.Close() }
