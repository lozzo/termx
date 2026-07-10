package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	termxcorev2 "github.com/lozzow/termx/termx-core-v2"
	hubclient "github.com/lozzow/termx/termx-hub/client"
	"github.com/lozzow/termx/termx-proto/wire"
	remotev2daemon "github.com/lozzow/termx/termx-remote-v2/daemon"
	remotev2webrtc "github.com/lozzow/termx/termx-remote-v2/webrtc"
	"github.com/lozzow/termx/termx-shared/remoteauth"
)

func TestDialCarriesProtocolThroughHubOfferAnswer(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	now := time.Now().UTC()
	grant, err := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", DeviceID: "device-1", Scope: remoteauth.Scope{AllowDaemon: true},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	core := termxcorev2.NewServer()
	answerer := remotev2webrtc.Answerer{Acceptor: remotev2daemon.SessionAcceptor{
		Core: core, DeviceFingerprint: remoteauth.Fingerprint(publicKey), Revocations: remoteauth.NewRevocations(),
	}}
	hub := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/sessions/ice":
			_ = json.NewEncoder(writer).Encode(map[string]any{"ice_servers": []any{}, "relay_policy": map[string]any{"allow_relay": false}})
		case "/api/v1/sessions":
			var input struct {
				SessionToken string `json:"session_token"`
				Offer        struct {
					SDP string `json:"sdp"`
				} `json:"offer"`
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatalf("decode session request: %v", err)
			}
			answer, err := answerer.Answer(request.Context(), hubclient.Offer{SessionID: "session-1", SDP: input.Offer.SDP, CapabilityGrant: input.SessionToken}, hubclient.RegistrationAck{})
			if err != nil {
				t.Fatalf("answer offer: %v", err)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"answer": map[string]any{"sdp": answer.SDP}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer hub.Close()

	transport, err := Dial(context.Background(), DialOptions{
		HubURL: hub.URL, HubDeviceID: "device-1", DeviceFingerprint: remoteauth.Fingerprint(publicKey),
		CapabilityGrant: grant, RelayMode: RelayAuto,
	})
	if err != nil {
		t.Fatalf("dial hub endpoint: %v", err)
	}
	client := protocol.NewClient(transport)
	defer client.Close()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "hub-client-test"}); err != nil {
		t.Fatalf("hello over hub endpoint: %v", err)
	}
	if _, err := client.List(context.Background()); err != nil {
		t.Fatalf("list over hub endpoint: %v", err)
	}
}

func TestDialRejectsGrantDeviceMismatchBeforeHubRequest(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	grant, _ := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", DeviceID: "device-1", Scope: remoteauth.Scope{AllowDaemon: true},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if _, err := Dial(context.Background(), DialOptions{
		HubURL: "http://127.0.0.1:1", HubDeviceID: "device-2", DeviceFingerprint: remoteauth.Fingerprint(publicKey), CapabilityGrant: grant,
	}); err == nil {
		t.Fatal("grant device mismatch must fail before Hub request")
	}
}
