package heartbeat

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/registry"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

func TestPostReportsStaticInfoAndMachines(t *testing.T) {
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

	reg := registry.New(registry.Config{Verifier: heartbeatVerifierForTest{publicKey: publicKey}})
	now := time.Date(2026, 5, 3, 14, 35, 0, 0, time.UTC)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, hubv1.CanonicalAgentRegistrationSignatureMessage(hubv1.AgentRegistrationSignatureFields{
		MachineID: "machine_1",
		AgentID:   "agent_1",
		Nonce:     "nonce-heartbeat-test",
		Timestamp: now,
	})))
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID:          "machine_1",
		AgentID:            "agent_1",
		SignatureAlgorithm: "ed25519",
		SignatureNonce:     "nonce-heartbeat-test",
		SignatureTimestamp: now.Unix(),
		SignatureValue:     signature,
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	err = Post(context.Background(), Config{
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

type heartbeatVerifierForTest struct {
	publicKey ed25519.PublicKey
}

func (v heartbeatVerifierForTest) VerifyAgentRegistration(_ context.Context, in registry.AgentRegistration) error {
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

func (heartbeatVerifierForTest) VerifyOfferTicket(context.Context, registry.OfferTicket) error {
	return nil
}
