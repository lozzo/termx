package main

import (
	"context"
	"testing"

	clientendpoint "github.com/lozzow/termx/client/endpoint"
)

func TestCLIRoutePlanEnvironmentRequiresCapabilityAndSSHCredentials(t *testing.T) {
	target := clientendpoint.Endpoint{
		ID: "studio", Label: "Studio", LabelSource: clientendpoint.SourceUser, ConnectMode: clientendpoint.ConnectOnDemand, Enabled: true,
		DaemonIdentity: clientendpoint.DaemonIdentity{DeviceID: "device-studio", DeviceFingerprint: "SHA256:studio"},
		Routes: map[clientendpoint.RouteID]clientendpoint.AccessRoute{
			"local": {ID: "local", Kind: clientendpoint.RouteLocalUnix, Enabled: true, Source: clientendpoint.SourceLocal, PolicySource: clientendpoint.SourceUser, Socket: "auto"},
			"ssh": {ID: "ssh", Kind: clientendpoint.RouteSSHWebRTCTCP, Enabled: true, Source: clientendpoint.SourceManual, PolicySource: clientendpoint.SourceUser,
				Host: "studio", RemoteSignalingAddress: "127.0.0.1:41120", RemoteICETCPAddress: "127.0.0.1:41121", CredentialRef: "credential:ssh-studio", SSHCredentialRef: "ssh:studio"},
			"cloud": {ID: "cloud", Kind: clientendpoint.RouteManagedWebRTC, Enabled: true, Source: clientendpoint.SourceCloud, PolicySource: clientendpoint.SourceUser,
				TargetDeviceID: "device-studio", CredentialRef: "credential:missing", RelayMode: clientendpoint.RelayAuto},
		},
	}
	environment := cliRoutePlanEnvironment(context.Background(), target,
		fakeCLICapabilityAvailability{"credential:ssh-studio": true}, fakeCLISSHAvailability{"ssh:studio": true})
	if len(environment.SupportedRouteKinds) != 3 {
		t.Fatalf("supported route kinds = %#v", environment.SupportedRouteKinds)
	}
	if len(environment.AvailableCredentialRefs) != 2 || environment.AvailableCredentialRefs[0] != "credential:ssh-studio" || environment.AvailableCredentialRefs[1] != "ssh:studio" {
		t.Fatalf("available credentials = %#v", environment.AvailableCredentialRefs)
	}
}

type fakeCLICapabilityAvailability map[string]bool

func (values fakeCLICapabilityAvailability) Available(_ context.Context, _ string, reference string) bool {
	return values[reference]
}

type fakeCLISSHAvailability map[string]bool

func (values fakeCLISSHAvailability) Available(reference string) bool { return values[reference] }
