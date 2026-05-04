package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/lozzow/termx/termx-hub/internal/cloud"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

func TestNewHubHandlerFromEnvRequiresControlConfiguration(t *testing.T) {
	t.Setenv("TERMX_HUB_CONTROL_URL", "")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "")

	_, _, err := newHubHandlerFromEnv()
	if err == nil {
		t.Fatal("hub handler without control verifier configuration succeeded")
	}
}

func TestNewHubHandlerFromEnvBuildsRunnableControlBackedHandler(t *testing.T) {
	t.Setenv("TERMX_HUB_CONTROL_URL", "http://127.0.0.1:12306")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "hub-secret")

	handler, _, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	if handler == nil {
		t.Fatal("handler is nil")
	}
	if _, ok := handler.(interface {
		StartCleanup(context.Context, <-chan time.Time)
	}); !ok {
		t.Fatal("handler does not expose cleanup loop for bounded hub memory")
	}
	req, err := http.NewRequest(http.MethodGet, "/api/health", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHubHandlerCleanupLoopRemovesExpiredRegistryState(t *testing.T) {
	t.Setenv("TERMX_HUB_CONTROL_URL", "http://127.0.0.1:12306")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "hub-secret")

	oldVerifier := newControlVerifier
	newControlVerifier = func(baseURL string, sharedSecret string) controlVerifier {
		return commandVerifierForTest{}
	}
	t.Cleanup(func() { newControlVerifier = oldVerifier })
	handler, _, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	cleaner, ok := handler.(interface {
		StartCleanup(context.Context, <-chan time.Time)
	})
	if !ok {
		t.Fatal("handler does not expose cleanup")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ticks := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() {
		cleaner.StartCleanup(ctx, ticks)
		close(done)
	}()
	ticks <- time.Date(2026, 5, 3, 18, 25, 0, 0, time.UTC)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup loop did not stop after context cancellation")
	}
}

func TestNewHubHandlerFromEnvPassesSTUNServersToAgentRegister(t *testing.T) {
	t.Setenv("TERMX_HUB_CONTROL_URL", "http://127.0.0.1:12306")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "hub-secret")
	t.Setenv("TERMX_HUB_ID", "hub-env-test")
	t.Setenv("TERMX_HUB_STUN_SERVERS", "stun:stun.example.com:3478, turn:turn.example.com:3478")

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	oldVerifier := newControlVerifier
	newControlVerifier = func(baseURL string, sharedSecret string) controlVerifier {
		return commandVerifierForTest{publicKey: publicKey}
	}
	t.Cleanup(func() { newControlVerifier = oldVerifier })
	handler, _, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	agentID := "agent-main-test"
	now := time.Date(2026, 5, 3, 14, 35, 0, 0, time.UTC)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, hubv1.CanonicalAgentRegistrationSignatureMessage(hubv1.AgentRegistrationSignatureFields{
		MachineID: "machine_1",
		AgentID:   agentID,
		Nonce:     "nonce-main-test",
		Timestamp: now,
	})))
	reqBody := `{
		"version":"remote.hub.v1",
		"device_id":"machine_1",
		"agent_id":"` + agentID + `",
		"display_name":"Machine 1",
		"terminals":[{"id":"terminal_1","state":"running"}],
		"signature":{
			"algorithm":"ed25519",
			"nonce":"nonce-main-test",
			"timestamp":` + "1777818900" + `,
			"value":"` + signature + `"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent register status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp hubv1.HubRegisterResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if len(resp.RTCConfig.IceServers) != 1 || resp.RTCConfig.IceServers[0].URLs[0] != "stun:stun.example.com:3478" {
		t.Fatalf("unexpected ICE servers: %+v", resp.RTCConfig.IceServers)
	}
	if resp.HubID != "hub-env-test" {
		t.Fatalf("register response hub id = %q", resp.HubID)
	}
	if resp.RelayPolicy.AllowRelay {
		t.Fatalf("STUN-only env unexpectedly enabled relay policy: %+v", resp.RelayPolicy)
	}
}

func TestNewHubHandlerFromEnvWiresCloudTurnWithoutRegistrationRelayPolicy(t *testing.T) {
	t.Setenv("TERMX_HUB_CONTROL_URL", "http://127.0.0.1:12306")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "hub-secret")
	t.Setenv("TERMX_HUB_STUN_SERVERS", "stun:stun.example.com:3478")
	t.Setenv("TERMX_HUB_TURN_SERVERS", "turn:turn.example.com:3478?transport=udp")
	t.Setenv("TERMX_HUB_TURN_SHARED_SECRET", "turn-secret")

	oldVerifier := newControlVerifier
	newControlVerifier = func(baseURL string, sharedSecret string) controlVerifier {
		return commandVerifierForTest{}
	}
	t.Cleanup(func() { newControlVerifier = oldVerifier })
	handler, _, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	register := httptest.NewRecorder()
	handler.ServeHTTP(register, httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(`{"device_id":"machine_1","agent_id":"agent_1"}`)))
	if register.Code != http.StatusOK {
		t.Fatalf("agent register status = %d body=%s", register.Code, register.Body.String())
	}
	var resp hubv1.HubRegisterResponse
	if err := json.NewDecoder(register.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if resp.RelayPolicy.AllowRelay || len(resp.RTCConfig.IceServers) != 1 ||
		strings.HasPrefix(resp.RTCConfig.IceServers[0].URLs[0], "turn:") {
		t.Fatalf("registration exposed cloud relay policy: %+v", resp)
	}
}

func TestPostHubHeartbeatReportsStaticInfoAndMachines(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
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

	reg := registry.New(registry.Config{Verifier: commandVerifierForTest{publicKey: publicKey}})
	now := time.Date(2026, 5, 3, 14, 35, 0, 0, time.UTC)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, hubv1.CanonicalAgentRegistrationSignatureMessage(hubv1.AgentRegistrationSignatureFields{
		MachineID: "machine_1",
		AgentID:   "agent_1",
		Nonce:     "nonce-main-test",
		Timestamp: now,
	})))
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID:          "machine_1",
		AgentID:            "agent_1",
		SignatureAlgorithm: "ed25519",
		SignatureNonce:     "nonce-main-test",
		SignatureTimestamp: now.Unix(),
		SignatureValue:     signature,
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	err = postHubHeartbeat(context.Background(), hubHeartbeatConfig{
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
		t.Fatalf("postHubHeartbeat returned error: %v", err)
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

type commandVerifierForTest struct {
	publicKey ed25519.PublicKey
}

func (v commandVerifierForTest) VerifyAgentRegistration(_ context.Context, in registry.AgentRegistration) error {
	if v.publicKey == nil {
		return nil
	}
	rawSignature, err := base64.StdEncoding.DecodeString(in.SignatureValue)
	if err != nil {
		return err
	}
	message := hubv1.CanonicalAgentRegistrationSignatureMessage(hubv1.AgentRegistrationSignatureFields{
		MachineID: in.MachineID,
		AgentID:   in.AgentID,
		Nonce:     in.SignatureNonce,
		Timestamp: time.Unix(in.SignatureTimestamp, 0).UTC(),
	})
	if !ed25519.Verify(v.publicKey, message, rawSignature) {
		return registry.ErrUnauthorizedAgent
	}
	return nil
}

func (commandVerifierForTest) VerifyOfferTicket(context.Context, registry.OfferTicket) error {
	return nil
}

func (commandVerifierForTest) CheckConnectionTicket(context.Context, cloud.VerifyTicketInput) (cloud.Ticket, error) {
	return cloud.Ticket{}, nil
}

func (commandVerifierForTest) ConsumeConnectionTicket(context.Context, cloud.VerifyTicketInput) (cloud.Ticket, error) {
	return cloud.Ticket{}, nil
}

func (commandVerifierForTest) GetAgentPolicy(_ context.Context, in registry.AgentPolicyRequest) (registry.AgentPolicy, error) {
	if strings.TrimSpace(in.MachineID) == "" || strings.TrimSpace(in.AgentID) == "" {
		return registry.AgentPolicy{}, errors.New("machine and agent are required")
	}
	return registry.AgentPolicy{MachineID: in.MachineID, AgentID: in.AgentID}, nil
}
