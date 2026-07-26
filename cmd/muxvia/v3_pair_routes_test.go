package main

import "testing"

func TestV3PairingRoutesAcceptOrdinaryDirectParameters(t *testing.T) {
	routes, err := v3PairingRoutes(v3PairRouteFlags{
		Routes: []string{"direct"}, DirectID: "frp", DirectName: "FRP Public",
		SignalingAddresses: []string{"frp.example.com:443"}, ICETCPAddresses: []string{"frp.example.com:444"}, ServerName: "mac.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].GetRouteId() != "direct-frp" || routes[0].GetDisplayName() != "FRP Public" || routes[0].GetDirectWebrtcTcp().GetServerName() != "mac.example.com" {
		t.Fatalf("routes = %#v", routes)
	}
}

func TestV3PairingRoutesAcceptStrictURIAndOrdinarySSH(t *testing.T) {
	routes, err := v3PairingRoutes(v3PairRouteFlags{Routes: []string{
		"direct://frp?name=FRP%20Public&signaling=frp.example.com:443&ice=frp.example.com:444",
		"ssh",
	}, SSHID: "office", SSHName: "Office SSH", SSHHost: "mac.example.com", SSHPort: 2222, SSHUser: "lozzow", SSHHostKeys: []string{"SHA256:abc"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].GetRouteId() != "direct-frp" || routes[1].GetRouteId() != "ssh-office" || routes[1].GetSshWebrtcTcp().GetPort() != 2222 {
		t.Fatalf("routes = %#v", routes)
	}
}

func TestV3PairingRoutesRejectFieldsWithoutExplicitRoute(t *testing.T) {
	if _, err := v3PairingRoutes(v3PairRouteFlags{SignalingAddresses: []string{"frp.example.com:443"}, ICETCPAddresses: []string{"frp.example.com:444"}}); err == nil {
		t.Fatal("implicit parameterized Direct Route was accepted")
	}
	if _, err := v3PairingRoutes(v3PairRouteFlags{Routes: []string{"direct://lan?unknown=value"}}); err == nil {
		t.Fatal("unknown URI query was accepted")
	}
	if _, err := v3PairingRoutes(v3PairRouteFlags{Routes: []string{"cloud"}}); err == nil {
		t.Fatal("unavailable Cloud Route was accepted")
	}
	if _, err := v3PairingRoutes(v3PairRouteFlags{Routes: []string{"direct"}, SSHHost: "ignored"}); err == nil {
		t.Fatal("out-of-scope SSH fields were silently ignored")
	}
}
