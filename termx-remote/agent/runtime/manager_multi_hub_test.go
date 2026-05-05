package runtime

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	"github.com/lozzow/termx/termx-remote/identity"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

func TestManagerRegistersTwoHubs(t *testing.T) {
	dataDir := t.TempDir()
	machineKey, err := identity.LoadOrCreateMachineKey(dataDir)
	if err != nil {
		t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
	}
	hub1 := newMultiHubTestServer(t, "hub-1", false)
	defer hub1.Close()
	hub2 := newMultiHubTestServer(t, "hub-2", false)
	defer hub2.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    dataDir,
		DeviceName: "multi-hub-agent",
		HubURLs:    []string{hub1.URL, hub2.URL},
	}, inventoryProviderStub{}, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer manager.Close()

	first := hub1.waitForRegister(t)
	second := hub2.waitForRegister(t)
	if first.DeviceID == "" || first.AgentID == "" {
		t.Fatalf("first registration missing identity: %+v", first)
	}
	if first.DeviceID != second.DeviceID {
		t.Fatalf("registrations used different device IDs: %q != %q", first.DeviceID, second.DeviceID)
	}
	if first.AgentID != second.AgentID {
		t.Fatalf("registrations used different agent IDs: %q != %q", first.AgentID, second.AgentID)
	}
	assertHubRegistrationSignature(t, machineKey.PublicKey, first)
	assertHubRegistrationSignature(t, machineKey.PublicKey, second)
}

func TestManagerReconnectsOnHubFailure(t *testing.T) {
	failing := newMultiHubTestServer(t, "hub-failing", true)
	defer failing.Close()
	healthy := newMultiHubTestServer(t, "hub-healthy", false)
	defer healthy.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "multi-hub-agent",
		HubURLs:    []string{failing.URL, healthy.URL},
	}, inventoryProviderStub{}, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer manager.Close()

	if got := healthy.waitForRegister(t); got.DeviceID == "" {
		t.Fatalf("healthy hub did not receive a real registration: %+v", got)
	}
	if status := manager.Status(); status.State == StateDegraded {
		t.Fatalf("one failed hub degraded the whole manager despite healthy registration: %+v", status)
	}
}

func TestManagerRegistersHealthyHubWhenAnotherHubStalls(t *testing.T) {
	stalled := newStalledHubServer(t)
	defer stalled.Close()
	healthy := newMultiHubTestServer(t, "hub-healthy", false)
	defer healthy.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "multi-hub-agent",
		HubURLs:    []string{stalled.URL(), healthy.URL},
	}, inventoryProviderStub{}, nil)
	startErr := make(chan error, 1)
	go func() {
		startErr <- manager.Start(context.Background())
	}()
	defer manager.Close()

	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stalled first hub blocked manager startup")
	}
	if got := healthy.waitForRegister(t); got.DeviceID == "" {
		t.Fatalf("healthy hub did not receive a registration while first hub stalled: %+v", got)
	}
}

func assertHubRegistrationSignature(t *testing.T, machinePublicKey ed25519.PublicKey, req hubv1.HubRegisterRequest) {
	t.Helper()
	rawSignature, err := base64.StdEncoding.DecodeString(req.Signature.Value)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	message := hubv1.CanonicalAgentRegistrationSignatureMessage(hubv1.AgentRegistrationSignatureFields{
		MachineID: req.DeviceID,
		AgentID:   req.AgentID,
		Nonce:     req.Signature.Nonce,
		Timestamp: time.Unix(req.Signature.Timestamp, 0).UTC(),
	})
	if !ed25519.Verify(machinePublicKey, message, rawSignature) {
		t.Fatalf("registration signature for hub agent %q does not verify with persisted machine key", req.AgentID)
	}
}

type multiHubTestServer struct {
	*httptest.Server
	mu        sync.Mutex
	registers []hubv1.HubRegisterRequest
	notify    chan struct{}
	fail      bool
	hubID     string
}

func newMultiHubTestServer(t *testing.T, hubID string, fail bool) *multiHubTestServer {
	t.Helper()
	s := &multiHubTestServer{
		notify: make(chan struct{}, 16),
		fail:   fail,
		hubID:  hubID,
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *multiHubTestServer) handle(w http.ResponseWriter, r *http.Request) {
	if s.fail {
		http.Error(w, "hub unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/api/v1/agents/register":
		var req hubv1.HubRegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.registers = append(s.registers, req)
		s.mu.Unlock()
		select {
		case s.notify <- struct{}{}:
		default:
		}
		_ = json.NewEncoder(w).Encode(hubv1.HubRegisterResponse{
			Version:                  "remote.hub.v1",
			HubID:                    s.hubID,
			AgentSessionID:           "agent-session-" + s.hubID,
			HeartbeatIntervalSeconds: 15,
		})
	case "/api/v1/agents/heartbeat":
		_ = json.NewEncoder(w).Encode(hubv1.HubHeartbeatResponse{
			Accepted:             true,
			NextHeartbeatSeconds: 15,
		})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *multiHubTestServer) waitForRegister(t *testing.T) hubv1.HubRegisterRequest {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		if len(s.registers) > 0 {
			req := s.registers[0]
			s.mu.Unlock()
			return req
		}
		s.mu.Unlock()
		select {
		case <-s.notify:
		case <-deadline.C:
			t.Fatal("timed out waiting for hub registration")
		}
	}
}

type stalledHubServer struct {
	listener net.Listener
	done     chan struct{}
}

func newStalledHubServer(t *testing.T) *stalledHubServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen stalled hub: %v", err)
	}
	s := &stalledHubServer{
		listener: listener,
		done:     make(chan struct{}),
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				close(s.done)
				return
			}
			go func() {
				<-s.done
				_ = conn.Close()
			}()
		}
	}()
	return s
}

func (s *stalledHubServer) URL() string {
	return "http://" + s.listener.Addr().String()
}

func (s *stalledHubServer) Close() {
	_ = s.listener.Close()
	<-s.done
}
