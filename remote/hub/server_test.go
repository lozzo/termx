package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeAgent struct {
	lastOffer Offer
	answer    Answer
	err       error
}

func (f *fakeAgent) HandleOffer(_ context.Context, offer Offer) (*Answer, error) {
	f.lastOffer = offer
	if f.err != nil {
		return nil, f.err
	}
	return &f.answer, nil
}

type fakeTurnProvider struct {
	username   string
	credential string
}

func (f fakeTurnProvider) GenerateCredentials(_ string) (string, string) {
	return f.username, f.credential
}

func TestServerRTCConfigIncludesTURNCredentials(t *testing.T) {
	auth := NewAuth("jwt-secret")
	registry := NewRegistry()
	server := NewServer(Config{
		PublicHost: "hub.termx.test",
		TURNPort:   3478,
	}, auth, registry, fakeTurnProvider{username: "u1", credential: "c1"})

	token, err := auth.GenerateUserToken("user-1", "alice", "user")
	if err != nil {
		t.Fatalf("GenerateUserToken returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rtc/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload struct {
		ICEServers []struct {
			URLs       []string `json:"urls"`
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
		} `json:"iceServers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.ICEServers) < 2 {
		t.Fatalf("expected stun and turn entries, got %#v", payload.ICEServers)
	}
	if payload.ICEServers[0].URLs[0] != "stun:hub.termx.test:3478" {
		t.Fatalf("unexpected stun server: %#v", payload.ICEServers[0])
	}
	if payload.ICEServers[1].Username != "u1" || payload.ICEServers[1].Credential != "c1" {
		t.Fatalf("unexpected turn credentials: %#v", payload.ICEServers[1])
	}
}

func TestServerRTCOfferForwardsToRegisteredAgent(t *testing.T) {
	auth := NewAuth("jwt-secret")
	registry := NewRegistry()
	agent := &fakeAgent{answer: Answer{SDP: "answer-sdp"}}
	registry.Register("device-1", "user-1", agent)
	server := NewServer(Config{
		PublicHost: "hub.termx.test",
		TURNPort:   3478,
	}, auth, registry, fakeTurnProvider{username: "u2", credential: "c2"})

	token, err := auth.GenerateUserToken("user-1", "alice", "user")
	if err != nil {
		t.Fatalf("GenerateUserToken returned error: %v", err)
	}

	body, _ := json.Marshal(OfferRequest{SDP: "offer-sdp"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/device-1/rtc/offer", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if agent.lastOffer.UserID != "user-1" || agent.lastOffer.DeviceID != "device-1" {
		t.Fatalf("unexpected forwarded offer context: %#v", agent.lastOffer)
	}
	if len(agent.lastOffer.ICEServers) == 0 {
		t.Fatalf("expected generated ice servers, got %#v", agent.lastOffer)
	}

	var resp Answer
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SDP != "answer-sdp" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestServerRTCOfferRejectsWrongUser(t *testing.T) {
	auth := NewAuth("jwt-secret")
	registry := NewRegistry()
	registry.Register("device-1", "user-1", &fakeAgent{})
	server := NewServer(Config{}, auth, registry, fakeTurnProvider{})

	token, err := auth.GenerateUserToken("user-2", "bob", "user")
	if err != nil {
		t.Fatalf("GenerateUserToken returned error: %v", err)
	}

	body, _ := json.Marshal(OfferRequest{SDP: "offer-sdp"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/device-1/rtc/offer", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for foreign device, got %d", rec.Code)
	}
}

func TestServerRTCOfferRejectsMissingAuth(t *testing.T) {
	server := NewServer(Config{}, NewAuth("jwt-secret"), NewRegistry(), fakeTurnProvider{})
	body, _ := json.Marshal(OfferRequest{SDP: "offer-sdp"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/device-1/rtc/offer", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing auth, got %d", rec.Code)
	}
}

func TestServerRTCConfigWithoutTurnProviderReturnsOnlySTUN(t *testing.T) {
	auth := NewAuth("jwt-secret")
	server := NewServer(Config{
		PublicHost: "hub.termx.test",
		TURNPort:   3478,
	}, auth, NewRegistry(), nil)
	token, err := auth.GenerateUserToken("user-1", "alice", "user")
	if err != nil {
		t.Fatalf("GenerateUserToken returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rtc/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var payload struct {
		ICEServers []struct {
			URLs []string `json:"urls"`
		} `json:"iceServers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.ICEServers) != 1 || payload.ICEServers[0].URLs[0] != "stun:hub.termx.test:3478" {
		t.Fatalf("unexpected ICE servers without turn provider: %#v", payload.ICEServers)
	}
}

func TestServerRTCOfferReturnsBadGatewayOnAgentError(t *testing.T) {
	auth := NewAuth("jwt-secret")
	registry := NewRegistry()
	registry.Register("device-1", "user-1", &fakeAgent{err: errors.New("offline")})
	server := NewServer(Config{}, auth, registry, fakeTurnProvider{})
	token, err := auth.GenerateUserToken("user-1", "alice", "user")
	if err != nil {
		t.Fatalf("GenerateUserToken returned error: %v", err)
	}

	body, _ := json.Marshal(OfferRequest{SDP: "offer-sdp"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/device-1/rtc/offer", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}
