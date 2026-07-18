package main

import (
	"context"
	"testing"

	clientendpoint "github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/shared/remoteauth"
)

func TestCLIRoutePlanEnvironmentKeepsCloudLazyAndRecognizesOpenSSHAlias(t *testing.T) {
	target := clientendpoint.Endpoint{
		ID: "studio", Label: "Studio", LabelSource: clientendpoint.SourceUser, ConnectMode: clientendpoint.ConnectOnDemand, Enabled: true,
		DaemonIdentity: clientendpoint.DaemonIdentity{DeviceID: "device-studio", DeviceFingerprint: "SHA256:studio"},
		Routes: map[clientendpoint.RouteID]clientendpoint.AccessRoute{
			"local": {ID: "local", Kind: clientendpoint.RouteLocalUnix, Enabled: true, Source: clientendpoint.SourceLocal, PolicySource: clientendpoint.SourceUser, Socket: "auto"},
			"ssh": {ID: "ssh", Kind: clientendpoint.RouteSSHWebRTCTCP, Enabled: true, Source: clientendpoint.SourceManual, PolicySource: clientendpoint.SourceUser,
				Host: "studio", RemoteSignalingAddress: "127.0.0.1:41120", RemoteICETCPAddress: "127.0.0.1:41121", CredentialRef: "ssh:studio"},
			"cloud": {ID: "cloud", Kind: clientendpoint.RouteManagedWebRTC, Enabled: true, Source: clientendpoint.SourceCloud, PolicySource: clientendpoint.SourceUser,
				TargetDeviceID: "device-studio", CredentialRef: "credential:missing", RelayMode: clientendpoint.RelayAuto},
		},
	}
	credentials := cliCredentialSource{store: remoteauth.NewCredentialStore(t.TempDir())}
	environment := cliRoutePlanEnvironment(context.Background(), target, credentials)
	if len(environment.SupportedRouteKinds) != 3 {
		t.Fatalf("supported route kinds = %#v", environment.SupportedRouteKinds)
	}
	if len(environment.AvailableCredentialRefs) != 1 || environment.AvailableCredentialRefs[0] != "ssh:studio" {
		t.Fatalf("available credentials = %#v", environment.AvailableCredentialRefs)
	}
}
