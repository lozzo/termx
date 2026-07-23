package endpoint

import "testing"

func TestSetAutomaticRoutePrioritiesIsAtomicAndPreservesFullRace(t *testing.T) {
	registry := Registry{Version: RegistryVersion, Default: "studio", Endpoints: map[EndpointID]Endpoint{"studio": plannerEndpoint()}}
	fullRace, err := SetAutomaticRoutePriorities(registry, "studio", map[RouteID]*int{"cloud": nil, "local": nil, "ssh": nil})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range fullRace.Endpoints["studio"].RouteList() {
		if route.Enabled && !route.ManualOnly && (route.Priority != nil || route.PolicySource != SourceUser) {
			t.Fatalf("full race route was not normalized through user policy: %#v", route)
		}
	}
	zero, ten := 0, 10
	grouped, err := SetAutomaticRoutePriorities(fullRace, "studio", map[RouteID]*int{"cloud": &ten, "local": &zero, "ssh": &ten})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (RouteSelectionPlanner{}).Plan(RouteSelectionRequest{
		Endpoint: grouped.Endpoints["studio"], Intent: ConnectIntent{Kind: "interactive"}, Generation: 1,
		SupportedRouteKinds:     []RouteKind{RouteLocalUnix, RouteSSHWebRTCTCP, RouteManagedWebRTC},
		AvailableCredentialRefs: []string{"credential:ssh-studio", "ssh:studio", "credential:studio"},
	})
	if err != nil {
		t.Fatal(err)
	}
	groups := plan.Groups()
	if len(groups) != 2 || groups[0].Priority() == nil || *groups[0].Priority() != 0 || len(groups[1].Attempts()) != 2 {
		t.Fatalf("priority groups do not preserve smaller-first/equal-concurrent semantics: %#v", groups)
	}
}

func TestSetAutomaticRoutePrioritiesRejectsPartialOrUnknownRoute(t *testing.T) {
	registry := Registry{Version: RegistryVersion, Default: "studio", Endpoints: map[EndpointID]Endpoint{"studio": plannerEndpoint()}}
	one := 1
	for name, priorities := range map[string]map[RouteID]*int{
		"partial": {"local": &one},
		"unknown": {"cloud": &one, "local": &one, "missing": &one},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := SetAutomaticRoutePriorities(registry, "studio", priorities); err == nil {
				t.Fatal("invalid priority transaction succeeded")
			}
			if registry.Endpoints["studio"].Routes["local"].Priority != nil {
				t.Fatal("failed transaction mutated input registry")
			}
		})
	}
}

func TestEvaluateRouteAvailabilityUsesSharedPlannerPrerequisites(t *testing.T) {
	target := plannerEndpoint()
	planning := cloneEndpoint(target)
	cloud := planning.Routes["cloud"]
	cloud.Enabled = false
	planning.Routes["cloud"] = cloud
	availability, err := EvaluateRouteAvailability(RouteAvailabilityRequest{
		Endpoint: target, PlanningEndpoint: planning,
		SupportedRouteKinds:     []RouteKind{RouteLocalUnix, RouteSSHWebRTCTCP, RouteManagedWebRTC},
		AvailableCredentialRefs: []string{"credential:ssh-studio", "ssh:studio"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[RouteID]RouteAvailability{}
	for _, item := range availability {
		got[item.RouteID] = item
	}
	if !got["local"].Available || !got["ssh"].Available {
		t.Fatalf("eligible routes unavailable: %#v", got)
	}
	if got["cloud"].Reason != RouteAvailabilityCloudUnavailable {
		t.Fatalf("managed eligibility loss not projected: %#v", got["cloud"])
	}
}
