package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/companion/session"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestAdapterRejectsInvalidDynamicLoginAndRefreshHubDirectory(t *testing.T) {
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", JSONMediaType)
		_ = json.NewEncoder(writer).Encode(SessionWire{
			Kind: session.KindAccount, AccountID: "account-1", AccountLabel: "Alice", DeviceID: "client-1",
			ExpiresAt: now.Add(time.Hour).Unix(), AccessToken: []byte("private-account-access-token"),
			RefreshToken: bytes.Repeat([]byte{0x41}, 32), RefreshExpiresAt: now.Add(24 * time.Hour).Unix(),
			HubID: "hub-1", HubURL: "http://cloud.example.test", HubRegion: "other-region", HubDirectoryVersion: 1,
		})
	}))
	defer server.Close()
	adapter, err := New(Config{ControlPlaneURL: server.URL, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.CompleteLogin(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: "flow-1"}); err == nil {
		t.Fatal("login accepted an invalid dynamic Hub URL")
	}
	local, err := session.NewRefreshable(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", AccountLabel: "Alice", DeviceID: "client-1", ExpiresAt: now.Add(time.Hour), HubID: "hub-1", HubURL: server.URL, HubRegion: "local-1", HubDirectoryVersion: 1}, []byte("current-account-access-token"), bytes.Repeat([]byte{0x51}, 32), now.Add(24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Destroy()
	authorization, err := local.RefreshAuthorization(now)
	if err != nil {
		t.Fatal(err)
	}
	defer authorization.Destroy()
	if _, err := adapter.RefreshSession(context.Background(), authorization); err == nil {
		t.Fatal("refresh accepted an invalid dynamic Hub URL")
	}
}

func TestAdapterAcceptsControllerSelectedDaemonRefreshDirectory(t *testing.T) {
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", JSONMediaType)
		_ = json.NewEncoder(writer).Encode(SessionWire{
			Kind: session.KindDevice, AccountID: "account-1", AccountLabel: "Alice", DeviceID: "daemon-1",
			ExpiresAt: now.Add(time.Hour).Unix(), AccessToken: []byte("new-daemon-access-token"),
			RefreshToken: bytes.Repeat([]byte{0x41}, 32), RefreshExpiresAt: now.Add(24 * time.Hour).Unix(),
			HubID: "hub-2", HubURL: server.URL, HubRegion: "remote-1", HubDirectoryVersion: 2,
		})
	}))
	defer server.Close()
	adapter, err := New(Config{ControlPlaneURL: server.URL, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	local, err := session.NewRefreshable(session.Metadata{Kind: session.KindDevice, AccountID: "account-1", AccountLabel: "Alice", DeviceID: "daemon-1", ExpiresAt: now.Add(time.Hour), HubID: "hub-1", HubURL: server.URL, HubRegion: "local-1", HubDirectoryVersion: 1}, []byte("current-daemon-access-token"), bytes.Repeat([]byte{0x51}, 32), now.Add(24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Destroy()
	authorization, err := local.RefreshAuthorization(now)
	if err != nil {
		t.Fatal(err)
	}
	defer authorization.Destroy()
	refreshed, err := adapter.RefreshSession(context.Background(), authorization)
	if err != nil {
		t.Fatal(err)
	}
	defer refreshed.Destroy()
	if refreshed.Metadata().HubID != "hub-2" || refreshed.Metadata().HubURL != server.URL || refreshed.Metadata().HubRegion != "remote-1" {
		t.Fatalf("refreshed daemon directory = %v", refreshed.Metadata())
	}
}

func TestAdapterRoutesEdgeRequestToControllerSelectedSessionHub(t *testing.T) {
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	staticRequests := 0
	staticHub := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		staticRequests++
	}))
	defer staticHub.Close()
	dynamicRequests := 0
	dynamicHub := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		dynamicRequests++
		if request.URL.Path != "/v1/presence/begin" {
			t.Errorf("dynamic Hub path = %q", request.URL.Path)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer dynamicHub.Close()

	adapter, err := New(Config{ControlPlaneURL: staticHub.URL, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := session.New(session.Metadata{
		Kind: session.KindDevice, AccountID: "account-1", DeviceID: "daemon-1", ExpiresAt: now.Add(time.Hour),
		HubID: "hub-dynamic", HubURL: dynamicHub.URL, HubRegion: "dynamic", HubDirectoryVersion: 2,
	}, []byte("controller-selected-hub-token"), now)
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Destroy()
	authorization := stored.Authorization()
	defer authorization.Destroy()

	response, err := adapter.doEdgeHub(context.Background(), "/v1/presence/begin", authorization, &cloudpb.BeginPresenceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if dynamicRequests != 1 || staticRequests != 0 {
		t.Fatalf("Hub request counts = dynamic %d, static %d", dynamicRequests, staticRequests)
	}
}
