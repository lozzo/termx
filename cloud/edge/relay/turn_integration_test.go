package relay_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/edge/relay"
	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	"github.com/anytty/anytty/cloud/relayquota"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	turn "github.com/pion/turn/v4"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRealTURNUDPAndBlockedUDPFallsBackToTCP(t *testing.T) {
	server, state, material := startRealTURN(t)
	defer state.Close()
	defer func() {
		if err := server.Close(context.Background()); err != nil {
			t.Errorf("close TURN server: %v", err)
		}
	}()

	udpClient, udpControl := newTURNClient(t, "udp", server.Address(), material)
	udpRelay, err := udpClient.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	assertRelayRoundTrip(t, udpRelay)
	_ = udpRelay.Close()
	udpClient.Close()
	_ = udpControl.Close()

	blockedSocket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	blocked := &blockedPacketConn{PacketConn: blockedSocket}
	blockedClient, err := turn.NewClient(&turn.ClientConfig{STUNServerAddr: server.Address(), TURNServerAddr: server.Address(), Conn: blocked, Username: material.GetUsername(), Password: material.GetCredential(), Realm: "relay.test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := blockedClient.Listen(); err != nil {
		t.Fatal(err)
	}
	if _, err := blockedClient.Allocate(); err == nil {
		t.Fatal("blocked UDP unexpectedly allocated Relay transport")
	}
	blockedClient.Close()
	_ = blocked.Close()

	tcpClient, tcpControl := newTURNClient(t, "tcp", server.Address(), material)
	tcpRelay, err := tcpClient.Allocate()
	if err != nil {
		t.Fatalf("TCP fallback allocation: %v", err)
	}
	assertRelayWrite(t, tcpRelay)
	_ = tcpRelay.Close()
	tcpClient.Close()
	_ = tcpControl.Close()
}

func TestRealTURNSameCredentialAllowsFourAllocationsAndRejectsFifth(t *testing.T) {
	server, state, material := startRealTURN(t)
	defer state.Close()
	defer func() { _ = server.Close(context.Background()) }()

	clients := make([]*turn.Client, 0, edgeruntime.MaxPhysicalAllocationsPerReservation+1)
	controls := make([]net.PacketConn, 0, edgeruntime.MaxPhysicalAllocationsPerReservation+1)
	allocations := make([]net.PacketConn, 0, edgeruntime.MaxPhysicalAllocationsPerReservation)
	defer func() {
		for _, allocation := range allocations {
			_ = allocation.Close()
		}
		for index, client := range clients {
			client.Close()
			_ = controls[index].Close()
		}
	}()
	for index := 0; index < edgeruntime.MaxPhysicalAllocationsPerReservation; index++ {
		client, control := newTURNClient(t, "udp", server.Address(), material)
		clients, controls = append(clients, client), append(controls, control)
		allocation, err := client.Allocate()
		if err != nil {
			t.Fatalf("allocation %d: %v", index+1, err)
		}
		allocations = append(allocations, allocation)
	}
	fifth, fifthControl := newTURNClient(t, "udp", server.Address(), material)
	clients, controls = append(clients, fifth), append(controls, fifthControl)
	if allocation, err := fifth.Allocate(); err == nil {
		_ = allocation.Close()
		t.Fatal("fifth physical TURN allocation was authorized")
	}
}

func startRealTURN(t *testing.T) (*relay.Server, *edgeruntime.State, *cloudv1.RelayICEConfig) {
	t.Helper()
	now := time.Now().UTC()
	state, err := edgeruntime.NewState(edgeruntime.StateConfig{MailboxSize: 64, DeltaBuffer: 64, MaxSessions: 64, MaxPendingSignals: 64, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	policySnapshot := &cloudv1.RelayPolicySnapshot{AccountId: "account", SubscriptionId: "subscription", PlanId: "plan", RelayEnabled: true, EdgeId: "edge", DaemonId: "daemon"}
	digest, err := relayquota.PolicyDigest(policySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	grant := &cloudv1.RelayGrant{ReservationId: "00000000-0000-4000-8000-000000000010", SessionId: "session", ReservedBytes: 1 << 20, MaxRateBytesPerSecond: 1 << 20, AuthorizedUntil: timestamppb.New(now.Add(time.Minute)), PolicyDigest: digest, Policy: policySnapshot}
	deriver, err := policy.NewCredentialDeriver(make([]byte, 32), []string{"turn:placeholder?transport=udp"})
	if err != nil {
		t.Fatal(err)
	}
	material, err := deriver.Material(grant)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterRelayGrant(context.Background(), grant, material); err != nil {
		t.Fatal(err)
	}
	address := freeTURNAddress(t)
	server, err := relay.Start(relay.Config{ListenAddress: address, PublicEndpoint: address, Realm: "relay.test", Runtime: state})
	if err != nil {
		state.Close()
		t.Fatal(err)
	}
	return server, state, material
}

func newTURNClient(t *testing.T, transport, address string, material *cloudv1.RelayICEConfig) (*turn.Client, net.PacketConn) {
	t.Helper()
	var control net.PacketConn
	if transport == "udp" {
		connection, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		control = connection
	} else {
		connection, err := net.Dial("tcp4", address)
		if err != nil {
			t.Fatal(err)
		}
		control = turn.NewSTUNConn(connection)
	}
	client, err := turn.NewClient(&turn.ClientConfig{STUNServerAddr: address, TURNServerAddr: address, Conn: control, Username: material.GetUsername(), Password: material.GetCredential(), Realm: "relay.test"})
	if err != nil {
		_ = control.Close()
		t.Fatal(err)
	}
	if err := client.Listen(); err != nil {
		client.Close()
		_ = control.Close()
		t.Fatal(err)
	}
	return client, control
}

func assertRelayRoundTrip(t *testing.T, allocation net.PacketConn) {
	t.Helper()
	peer, source := assertRelayWrite(t, allocation)
	defer peer.Close()
	buffer := make([]byte, 64)
	response := []byte("ack")
	if _, err := peer.WriteTo(response, source); err != nil {
		t.Fatal(err)
	}
	count, _, err := allocation.ReadFrom(buffer)
	if err != nil || string(buffer[:count]) != string(response) {
		t.Fatalf("Relay read %q: %v", buffer[:count], err)
	}
}

func assertRelayWrite(t *testing.T, allocation net.PacketConn) (net.PacketConn, net.Addr) {
	t.Helper()
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	deadline := time.Now().Add(5 * time.Second)
	_ = peer.SetDeadline(deadline)
	_ = allocation.SetDeadline(deadline)
	payload := []byte("relay-reservation")
	if _, err := allocation.WriteTo(payload, peer.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64)
	count, source, err := peer.ReadFrom(buffer)
	if err != nil || string(buffer[:count]) != string(payload) {
		t.Fatalf("peer read %q from %v: %v", buffer[:count], source, err)
	}
	return peer, source
}

func freeTURNAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	packet, err := net.ListenPacket("udp4", address)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	_ = packet.Close()
	_ = listener.Close()
	return address
}

type blockedPacketConn struct{ net.PacketConn }

func (*blockedPacketConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, errors.New("injected UDP block")
}
