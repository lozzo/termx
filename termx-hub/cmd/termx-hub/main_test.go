package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/lozzow/termx/termx-hub/internal/managed"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

func TestNewHubHandlerFromEnvRequiresControlConfiguration(t *testing.T) {
	t.Setenv("TERMX_HUB_CONTROL_URL", "")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "")

	_, err := newHubHandlerFromEnv()
	if err == nil {
		t.Fatal("hub handler without control verifier configuration succeeded")
	}
}

func TestNewHubHandlerFromEnvBuildsRunnableControlBackedHandler(t *testing.T) {
	t.Setenv("TERMX_HUB_CONTROL_URL", "http://127.0.0.1:12306")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "hub-secret")

	handler, err := newHubHandlerFromEnv()
	if err != nil {
		t.Fatalf("new hub handler: %v", err)
	}
	if handler == nil {
		t.Fatal("handler is nil")
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

func TestNewHubHandlerFromEnvPassesSTUNServersToAgentRegister(t *testing.T) {
	t.Setenv("TERMX_HUB_CONTROL_URL", "http://127.0.0.1:12306")
	t.Setenv("TERMX_HUB_CONTROL_SECRET", "hub-secret")
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
	handler, err := newHubHandlerFromEnv()
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
	if resp.RelayPolicy.AllowRelay {
		t.Fatalf("STUN-only env unexpectedly enabled relay policy: %+v", resp.RelayPolicy)
	}
}

type commandVerifierForTest struct {
	publicKey ed25519.PublicKey
}

func (v commandVerifierForTest) VerifyAgentRegistration(_ context.Context, in registry.AgentRegistration) error {
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

func (commandVerifierForTest) CheckManagedTicket(context.Context, managed.VerifyTicketInput) (managed.Ticket, error) {
	return managed.Ticket{}, nil
}

func (commandVerifierForTest) ConsumeManagedTicket(context.Context, managed.VerifyTicketInput) (managed.Ticket, error) {
	return managed.Ticket{}, nil
}
