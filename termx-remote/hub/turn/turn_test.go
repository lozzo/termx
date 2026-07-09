package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pion/stun/v3"
	pionturn "github.com/pion/turn/v4"
)

func TestTURNServerStartsAndAcceptsUDP(t *testing.T) {
	server, err := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		PublicIP:   "127.0.0.1",
		Secret:     "turn-secret",
		Realm:      "termx",
	})
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	defer server.Close()
	if server.UDPAddr().String() != server.TCPAddr().String() {
		t.Fatalf("UDP and TCP TURN listeners should share one advertised address, got udp=%s tcp=%s", server.UDPAddr(), server.TCPAddr())
	}

	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket client: %v", err)
	}
	defer conn.Close()

	msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if _, err := conn.WriteTo(msg.Raw, server.UDPAddr()); err != nil {
		t.Fatalf("send STUN binding request: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1500)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read STUN binding response: %v", err)
	}
	var resp stun.Message
	resp.Raw = buf[:n]
	if err := resp.Decode(); err != nil {
		t.Fatalf("decode STUN response: %v", err)
	}
	if resp.Type.Method != stun.MethodBinding || resp.Type.Class != stun.ClassSuccessResponse {
		t.Fatalf("unexpected STUN response type: %s", resp.Type)
	}
}

func TestTURNServerAcceptsTCPBindingRequest(t *testing.T) {
	server, err := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		PublicIP:   "127.0.0.1",
		Secret:     "turn-secret",
		Realm:      "termx",
	})
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	defer server.Close()

	conn, err := net.Dial("tcp4", server.TCPAddr().String())
	if err != nil {
		t.Fatalf("dial TURN TCP listener: %v", err)
	}
	defer conn.Close()
	msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if _, err := conn.Write(msg.Raw); err != nil {
		t.Fatalf("send TCP STUN binding request: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read TCP STUN binding response: %v", err)
	}
	var resp stun.Message
	resp.Raw = buf[:n]
	if err := resp.Decode(); err != nil {
		t.Fatalf("decode TCP STUN response: %v", err)
	}
	if resp.Type.Method != stun.MethodBinding || resp.Type.Class != stun.ClassSuccessResponse {
		t.Fatalf("unexpected TCP STUN response type: %s", resp.Type)
	}
}

func TestTURNServerRequiresPublicIPWhenListeningOnUnspecifiedAddress(t *testing.T) {
	server, err := NewServer(Config{
		ListenAddr: "0.0.0.0:0",
		Secret:     "turn-secret",
		Realm:      "termx",
	})
	if err == nil {
		_ = server.Close()
		t.Fatal("expected public IP requirement for unspecified TURN listen address")
	}
}

func TestGenerateCredentialsHMACFormat(t *testing.T) {
	clock := fixedClock(time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC))
	server, err := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		PublicIP:   "127.0.0.1",
		Secret:     "turn-secret",
		Realm:      "termx",
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	defer server.Close()

	username, credential := server.GenerateCredentials()
	expiry, err := strconv.ParseInt(username, 10, 64)
	if err != nil {
		t.Fatalf("username is not an expiry timestamp: %q", username)
	}
	wantExpiry := clock.Now().Add(24 * time.Hour).Unix()
	if expiry != wantExpiry {
		t.Fatalf("expiry = %d, want %d", expiry, wantExpiry)
	}
	mac := hmac.New(sha1.New, []byte("turn-secret"))
	mac.Write([]byte(username))
	wantCredential := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if credential != wantCredential {
		t.Fatalf("credential = %q, want %q", credential, wantCredential)
	}
	key, ok := server.AuthHandler()(username, "termx", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
	if !ok {
		t.Fatal("generated credential username did not pass auth handler")
	}
	wantKey := pionturn.GenerateAuthKey(username, "termx", credential)
	if !hmac.Equal(key, wantKey) {
		t.Fatal("auth handler returned unexpected TURN auth key")
	}
}

func TestTURNServerGracefulShutdown(t *testing.T) {
	server, err := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		PublicIP:   "127.0.0.1",
		Secret:     "turn-secret",
		Realm:      "termx",
	})
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	udpAddr := server.UDPAddr().String()
	tcpAddr := server.TCPAddr().String()

	if err := server.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if conn, err := net.ListenPacket("udp4", udpAddr); err != nil {
		t.Fatalf("UDP port was not released after Close: %v", err)
	} else {
		_ = conn.Close()
	}
	if ln, err := net.Listen("tcp4", tcpAddr); err != nil {
		t.Fatalf("TCP port was not released after Close: %v", err)
	} else {
		_ = ln.Close()
	}
}

func TestTrafficMeterDrainReturnsDeltasAndClears(t *testing.T) {
	meter := NewTrafficMeter()
	meter.Add("machine_1", 120, 45)
	meter.Add("machine_2", 10, 0)
	meter.Add("machine_1", 30, 5)

	first := meter.DrainTraffic()
	if len(first) != 2 {
		t.Fatalf("first drain = %+v", first)
	}
	assertTrafficDelta(t, first, "machine_1", 150, 50)
	assertTrafficDelta(t, first, "machine_2", 10, 0)

	if second := meter.DrainTraffic(); len(second) != 0 {
		t.Fatalf("second drain should be empty after reset: %+v", second)
	}
}

func TestRelayTrafficEventHandlerRemovesAllocationRelay(t *testing.T) {
	generator := newMeteredRelayAddressGenerator(nil, NewTrafficMeter())
	handler := relayTrafficEventHandler(generator)
	srcAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 41000}
	dstAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3478}
	relayAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 51000}

	generator.remember(relayAddr, &trafficAgentSetterStub{})
	handler.OnAllocationCreated(srcAddr, dstAddr, "udp", "machine_1:1778052600", "termx", relayAddr, 0)
	if handler.OnAllocationDeleted == nil {
		t.Fatal("relay traffic event handler must clean up deleted TURN allocations")
	}
	handler.OnAllocationDeleted(srcAddr, dstAddr, "udp", "machine_1:1778052600", "termx")

	generator.mu.Lock()
	relayCount := len(generator.relays)
	generator.mu.Unlock()
	if relayCount != 0 {
		t.Fatalf("deleted allocation left %d relay wrapper(s) registered", relayCount)
	}
}

func assertTrafficDelta(t *testing.T, deltas []TrafficDelta, agentID string, bytesIn, bytesOut int64) {
	t.Helper()
	for _, delta := range deltas {
		if delta.AgentID != agentID {
			continue
		}
		if delta.BytesIn != bytesIn || delta.BytesOut != bytesOut {
			t.Fatalf("traffic delta for %s = %+v, want in=%d out=%d", agentID, delta, bytesIn, bytesOut)
		}
		return
	}
	t.Fatalf("missing traffic delta for %s in %+v", agentID, deltas)
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

type trafficAgentSetterStub struct{}

func (trafficAgentSetterStub) setAgent(string) {}
