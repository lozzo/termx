package endpoint

import (
	"testing"
	"time"
)

func TestRouteSelectionPlannerFullRaceAndImmutableOutput(t *testing.T) {
	target := plannerEndpoint()
	plan, err := (RouteSelectionPlanner{}).Plan(RouteSelectionRequest{
		Endpoint: target, Intent: ConnectIntent{Kind: "interactive"}, Generation: 7,
		SupportedRouteKinds:     []RouteKind{RouteLocalUnix, RouteSSHWebRTCTCP, RouteManagedWebRTC},
		AvailableCredentialRefs: []string{"ssh:studio", "credential:studio"},
	})
	if err != nil {
		t.Fatal(err)
	}
	groups := plan.Groups()
	if len(groups) != 1 || groups[0].StartDelay() != 0 || groups[0].Priority() != nil {
		t.Fatalf("full-race groups = %#v", groups)
	}
	diagnostics := plan.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].RouteID != "cloud" || diagnostics[0].Reason != RoutePlanAutomaticRaceUnsupported {
		t.Fatalf("full-race diagnostics = %#v", diagnostics)
	}
	diagnostics[0].RouteID = "changed"
	if plan.Diagnostics()[0].RouteID != "cloud" {
		t.Fatal("plan diagnostics were mutated through getter")
	}
	attempts := groups[0].Attempts()
	if len(attempts) != 2 || attempts[0].Route.ID != "local" || attempts[1].Route.ID != "ssh" {
		t.Fatalf("full-race attempts = %#v", attempts)
	}
	if attempts[0].Generation != 7 || attempts[0].AttemptID != "studio:7:local" {
		t.Fatalf("attempt identity = %#v", attempts[0])
	}
	attempts[0].Route.Socket = "changed"
	attempts[0].Intent.RequiredScopes = append(attempts[0].Intent.RequiredScopes, "changed")
	if current := plan.Groups()[0].Attempts()[0]; current.Route.Socket != "auto" || len(current.Intent.RequiredScopes) != 0 {
		t.Fatalf("plan output was mutated through getter: %#v", current)
	}
	local := target.Routes["local"]
	local.Socket = "registry-change"
	target.Routes["local"] = local
	if current := plan.Groups()[0].Attempts()[0]; current.Route.Socket != "auto" {
		t.Fatalf("plan output followed registry mutation: %#v", current)
	}
}

func TestRouteSelectionPlannerGroupsPriorityWithCumulativeHedge(t *testing.T) {
	target := plannerEndpoint()
	priority10, priority20 := 10, 20
	local := target.Routes["local"]
	local.Priority = &priority10
	target.Routes["local"] = local
	ssh := target.Routes["ssh"]
	ssh.Priority = &priority20
	target.Routes["ssh"] = ssh
	cloud := target.Routes["cloud"]
	cloud.Priority = &priority20
	target.Routes["cloud"] = cloud
	target.SelectionPolicy = SelectionPolicy{HedgeDelay: 250 * time.Millisecond, HedgeDelayConfigured: true}

	plan, err := (RouteSelectionPlanner{}).Plan(RouteSelectionRequest{
		Endpoint: target, Intent: ConnectIntent{Kind: "background"}, Generation: 3,
		SupportedRouteKinds:     []RouteKind{RouteLocalUnix, RouteSSHWebRTCTCP, RouteManagedWebRTC},
		AvailableCredentialRefs: []string{"ssh:studio", "credential:studio"},
	})
	if err != nil {
		t.Fatal(err)
	}
	groups := plan.Groups()
	if len(groups) != 2 || *groups[0].Priority() != 10 || groups[0].StartDelay() != 0 || *groups[1].Priority() != 20 || groups[1].StartDelay() != 250*time.Millisecond {
		t.Fatalf("priority groups = %#v", groups)
	}
	if attempts := groups[1].Attempts(); len(attempts) != 1 || attempts[0].Route.ID != "ssh" {
		t.Fatalf("managed route entered common race: %#v", attempts)
	}
}

func TestRouteSelectionPlannerExplicitManualRouteAndFailures(t *testing.T) {
	target := plannerEndpoint()
	ssh := target.Routes["ssh"]
	ssh.ManualOnly = true
	target.Routes["ssh"] = ssh
	planner := RouteSelectionPlanner{}

	plan, err := planner.Plan(RouteSelectionRequest{
		Endpoint: target, Intent: ConnectIntent{Kind: "probe"}, RouteOverride: "ssh", Generation: 2,
		SupportedRouteKinds: []RouteKind{RouteSSHWebRTCTCP}, AvailableCredentialRefs: []string{"ssh:studio"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts := plan.Groups()[0].Attempts(); len(attempts) != 1 || attempts[0].Route.ID != "ssh" {
		t.Fatalf("override attempts = %#v", attempts)
	}

	request := RouteSelectionRequest{
		Endpoint: target, Intent: ConnectIntent{Kind: "interactive"}, RouteOverride: "ssh", Generation: 2,
		SupportedRouteKinds: []RouteKind{RouteSSHWebRTCTCP},
	}
	if _, err := planner.Plan(request); !IsCode(err, ErrorCredentialRequired) {
		t.Fatalf("missing credential error = %v", err)
	}
	request.AvailableCredentialRefs = []string{"ssh:studio"}
	request.SupportedRouteKinds = []RouteKind{RouteLocalUnix}
	if _, err := planner.Plan(request); !IsCode(err, ErrorRouteUnavailable) {
		t.Fatalf("unsupported platform error = %v", err)
	}
	request.SupportedRouteKinds = []RouteKind{RouteSSHWebRTCTCP}
	request.RouteOverride = "missing"
	if _, err := planner.Plan(request); !IsCode(err, ErrorRouteUnavailable) {
		t.Fatalf("missing override error = %v", err)
	}
}

func TestRouteSelectionPlannerKeepsSingleManagedRouteOutOfCommonRace(t *testing.T) {
	target := plannerEndpoint()
	for _, routeID := range []RouteID{"local", "ssh"} {
		route := target.Routes[routeID]
		route.Enabled = false
		target.Routes[routeID] = route
	}
	plan, err := (RouteSelectionPlanner{}).Plan(RouteSelectionRequest{
		Endpoint: target, Intent: ConnectIntent{Kind: "interactive"}, Generation: 4,
		SupportedRouteKinds: []RouteKind{RouteManagedWebRTC}, AvailableCredentialRefs: []string{"credential:studio"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts := plan.Groups()[0].Attempts(); len(attempts) != 1 || attempts[0].Route.Kind != RouteManagedWebRTC {
		t.Fatalf("single managed plan = %#v", attempts)
	}
}

func plannerEndpoint() Endpoint {
	identity := DaemonIdentity{DeviceID: "device-studio", DeviceFingerprint: "SHA256:studio"}
	return Endpoint{
		ID: "studio", Label: "Studio", LabelSource: SourceUser, DaemonIdentity: identity, ConnectMode: ConnectOnDemand, Enabled: true,
		Routes: map[RouteID]AccessRoute{
			"local": {ID: "local", Kind: RouteLocalUnix, Enabled: true, Source: SourceLocal, PolicySource: SourceUser, Socket: "auto"},
			"ssh":   {ID: "ssh", Kind: RouteSSHWebRTCTCP, Enabled: true, Source: SourceManual, PolicySource: SourceUser, Host: "studio", RemoteSignalingAddress: "127.0.0.1:41120", RemoteICETCPAddress: "127.0.0.1:41121", CredentialRef: "ssh:studio"},
			"cloud": {ID: "cloud", Kind: RouteManagedWebRTC, Enabled: true, Source: SourceCloud, PolicySource: SourceUser, TargetDeviceID: identity.DeviceID, RelayMode: RelayAuto, CredentialRef: "credential:studio"},
		},
	}
}
