package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	corev2 "github.com/lozzow/termx/termx-core-v2"
	"github.com/lozzow/termx/termx-shared/connection"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport"
)

func TestStartV3RemoteAgentIsDisabledWithoutHubURL(t *testing.T) {
	t.Setenv("TERMX_HUB_URL", "")
	stop, err := startV3RemoteAgent(context.Background(), remoteAgentTestServer{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("disabled remote agent: %v", err)
	}
	stop()
}

func TestV3HubEndpointDialerFailsLocallyWhenGrantRefIsMissing(t *testing.T) {
	dialer := v3HubEndpointDialer(context.Background(), remoteauth.NewCredentialStore(t.TempDir()))
	_, err := dialer(context.Background(), connection.Config{
		ID: "lab", Transport: connection.TransportHubP2P, HubURL: "http://127.0.0.1:8447",
		HubDeviceID: "device-1", DeviceFingerprint: "ed25519-sha256:test", GrantRef: "missing", RelayMode: connection.RelayAuto,
	})
	if err == nil || !strings.Contains(err.Error(), "resolve grant_ref") {
		t.Fatalf("expected endpoint-scoped credential error, got %v", err)
	}
}

func TestStartV3RemoteAgentRequiresExplicitDeviceAndHubCredential(t *testing.T) {
	t.Setenv("TERMX_HUB_URL", "http://127.0.0.1:8447")
	t.Setenv("TERMX_REMOTE_DEVICE_ID", "")
	t.Setenv("TERMX_HUB_AGENT_TOKEN", "")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := startV3RemoteAgent(context.Background(), remoteAgentTestServer{}, logger); err == nil || !strings.Contains(err.Error(), "TERMX_REMOTE_DEVICE_ID") {
		t.Fatalf("expected device id requirement, got %v", err)
	}
	t.Setenv("TERMX_REMOTE_DEVICE_ID", "device-1")
	if _, err := startV3RemoteAgent(context.Background(), remoteAgentTestServer{}, logger); err == nil || !strings.Contains(err.Error(), "TERMX_HUB_AGENT_TOKEN") {
		t.Fatalf("expected hub credential requirement, got %v", err)
	}
}

type remoteAgentTestServer struct{}

func (remoteAgentTestServer) ListenAndServe(context.Context) error { return nil }
func (remoteAgentTestServer) Shutdown(context.Context) error       { return nil }
func (remoteAgentTestServer) ListTerminals() []corev2.TerminalInfo { return nil }
func (remoteAgentTestServer) ServeScopedTransport(context.Context, transport.Transport, corev2.TransportScope) error {
	return nil
}
