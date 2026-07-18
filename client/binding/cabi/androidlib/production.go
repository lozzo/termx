package main

import (
	"sync/atomic"

	pionadapter "github.com/lozzow/termx/client/adapter/managed/pion"
	"github.com/lozzow/termx/client/binding"
	"github.com/lozzow/termx/client/binding/managedhost"
	clientruntime "github.com/lozzow/termx/client/runtime"
)

// androidProductionHost 只装配 Android generation、Pion peer 与共享 managed host。
// Cloud/credential/auth/Hello/API 逻辑不得在 JNI 包内形成平台分叉。
type androidProductionHost struct {
	*managedhost.Host
	broker *binding.PlatformBroker
}

var androidProcessGeneration atomic.Uint64

func nextAndroidSessionGeneration() clientruntime.SessionGeneration {
	return clientruntime.SessionGeneration(androidProcessGeneration.Add(1))
}

func newAndroidProductionHost() *androidProductionHost {
	broker := binding.NewPlatformBroker()
	host, err := managedhost.New(managedhost.Options{
		Broker: broker, Peers: pionadapter.Factory{}, ClientName: "termx-android", CredentialPrefix: "android-access-",
		NextGeneration: nextAndroidSessionGeneration,
	})
	if err != nil {
		panic(err)
	}
	return &androidProductionHost{Host: host, broker: broker}
}

func (host *androidProductionHost) close() error { return host.Close() }
