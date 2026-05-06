package runtime

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	remoteconfig "github.com/lozzow/termx/termx-remote/config"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

func TestManagerRegistersTwoHubs(t *testing.T) {
	dataDir := t.TempDir()
	hub1 := newMultiHubTestServer(t, "hub-1", false)
	defer hub1.Close()
	hub2 := newMultiHubTestServer(t, "hub-2", false)
	defer hub2.Close()

	manager := NewManager(remoteconfig.Config{
		Enabled:    true,
		DataDir:    dataDir,
		DeviceName: "multi-hub-agent",
		HubURLs:    []string{hub1.URL, hub2.URL},
		Mode:       "local",
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
	if first.DisplayName != "multi-hub-agent" || second.DisplayName != "multi-hub-agent" {
		t.Fatalf("registrations missing display name: first=%+v second=%+v", first, second)
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
