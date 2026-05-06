package heartbeat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/registry"
	hubturn "github.com/lozzow/termx/termx-remote/hub/turn"
)

func TestPostReportsStaticInfoAndMachines(t *testing.T) {
	var gotPath string
	var gotSecret string
	var gotBody struct {
		HubID    string   `json:"hub_id"`
		AgentIDs []string `json:"agent_ids"`
		Static   struct {
			Name      string `json:"name"`
			Region    string `json:"region"`
			HTTPURL   string `json:"http_url"`
			MaxAgents int    `json:"max_agents"`
		} `json:"static"`
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSecret = r.Header.Get("X-TermX-Hub-Secret")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode heartbeat body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer control.Close()

	reg := registry.New(registry.Config{})
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID: "machine_1",
		AgentID:   "agent_1",
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	err := Post(context.Background(), Config{
		ControlURL: control.URL,
		Secret:     "hub-secret",
		HubID:      "hub-test",
		Name:       "Hub Test",
		Region:     "iad",
		HTTPURL:    "https://hub.example.test",
		MaxAgents:  100,
		Client:     control.Client(),
	}, reg)
	if err != nil {
		t.Fatalf("Post returned error: %v", err)
	}
	if gotPath != "/api/internal/hubs/heartbeat" || gotSecret != "hub-secret" {
		t.Fatalf("unexpected heartbeat request path=%q secret=%q", gotPath, gotSecret)
	}
	if gotBody.HubID != "hub-test" || gotBody.Static.Name != "Hub Test" || gotBody.Static.Region != "iad" ||
		gotBody.Static.HTTPURL != "https://hub.example.test" || gotBody.Static.MaxAgents != 100 {
		t.Fatalf("unexpected heartbeat body: %+v", gotBody)
	}
	if len(gotBody.AgentIDs) != 1 || gotBody.AgentIDs[0] != "machine_1" {
		t.Fatalf("expected machine ids in heartbeat, got %+v", gotBody.AgentIDs)
	}
}

func TestPostReportsRelayTrafficDeltas(t *testing.T) {
	var gotBody struct {
		Traffic []struct {
			AgentID  string `json:"agent_id"`
			BytesIn  int64  `json:"bytes_in"`
			BytesOut int64  `json:"bytes_out"`
		} `json:"traffic"`
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode heartbeat body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer control.Close()

	reader := &trafficReaderStub{deltas: []hubturn.TrafficDelta{{
		AgentID:  "machine_traffic",
		BytesIn:  1024,
		BytesOut: 2048,
	}}}
	err := Post(context.Background(), Config{
		ControlURL:    control.URL,
		Secret:        "hub-secret",
		HubID:         "hub-test",
		HTTPURL:       "https://hub.example.test",
		Client:        control.Client(),
		TrafficReader: reader,
	}, nil)
	if err != nil {
		t.Fatalf("Post returned error: %v", err)
	}
	if reader.drains != 1 {
		t.Fatalf("traffic reader drains = %d, want 1", reader.drains)
	}
	if len(gotBody.Traffic) != 1 || gotBody.Traffic[0].AgentID != "machine_traffic" ||
		gotBody.Traffic[0].BytesIn != 1024 || gotBody.Traffic[0].BytesOut != 2048 {
		t.Fatalf("unexpected traffic payload: %+v", gotBody.Traffic)
	}
}

func TestPostOmitsTrafficWhenNoRelayDeltas(t *testing.T) {
	var gotBody map[string]json.RawMessage
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode heartbeat body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer control.Close()

	reader := &trafficReaderStub{}
	err := Post(context.Background(), Config{
		ControlURL:    control.URL,
		Secret:        "hub-secret",
		HubID:         "hub-test",
		HTTPURL:       "https://hub.example.test",
		Client:        control.Client(),
		TrafficReader: reader,
	}, nil)
	if err != nil {
		t.Fatalf("Post returned error: %v", err)
	}
	if _, ok := gotBody["traffic"]; ok {
		t.Fatalf("heartbeat should omit empty traffic payload: %s", string(gotBody["traffic"]))
	}
	if reader.drains != 1 {
		t.Fatalf("traffic reader drains = %d, want 1", reader.drains)
	}
}

func TestPostAppliesKickAgentsResponseToRegistry(t *testing.T) {
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/hubs/heartbeat" {
			t.Fatalf("unexpected heartbeat path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"kick_agents":["machine_kick"]}`))
	}))
	defer control.Close()

	reg := registry.New(registry.Config{})
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID: "machine_kick",
		AgentID:   "agent_kick",
	}); err != nil {
		t.Fatalf("register kicked agent: %v", err)
	}
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID: "machine_keep",
		AgentID:   "agent_keep",
	}); err != nil {
		t.Fatalf("register kept agent: %v", err)
	}

	if err := Post(context.Background(), Config{
		ControlURL: control.URL,
		Secret:     "hub-secret",
		HubID:      "hub-test",
		HTTPURL:    "https://hub.example.test",
		Client:     control.Client(),
	}, reg); err != nil {
		t.Fatalf("Post returned error: %v", err)
	}

	for _, agent := range reg.Agents() {
		if agent.MachineID == "machine_kick" || agent.ID == "agent_kick" {
			t.Fatalf("kicked agent remained online: %+v", agent)
		}
	}
	if _, ok := reg.GetAgent("agent_keep"); !ok {
		t.Fatal("non-kicked agent disappeared")
	}
	if err := reg.Heartbeat(context.Background(), registry.HeartbeatInput{
		MachineID: "machine_kick",
		AgentID:   "agent_kick",
	}); !errors.Is(err, registry.ErrAgentForcedOffline) {
		t.Fatalf("kicked heartbeat err = %v", err)
	}
	if _, err := reg.Poll(context.Background(), registry.PollInput{
		MachineID: "machine_kick",
		AgentID:   "agent_kick",
		Timeout:   time.Millisecond,
	}); !errors.Is(err, registry.ErrAgentForcedOffline) {
		t.Fatalf("kicked poll err = %v", err)
	}
	if _, err := reg.SubmitOffer(context.Background(), registry.OfferInput{
		MachineID:  "machine_kick",
		TerminalID: "terminal_kick",
		SDP:        "v=0\r\no=- offer 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel",
	}); !errors.Is(err, registry.ErrAgentForcedOffline) {
		t.Fatalf("kicked offer err = %v", err)
	}
}

type trafficReaderStub struct {
	deltas []hubturn.TrafficDelta
	drains int
}

func (s *trafficReaderStub) DrainTraffic() []hubturn.TrafficDelta {
	s.drains++
	return append([]hubturn.TrafficDelta(nil), s.deltas...)
}
