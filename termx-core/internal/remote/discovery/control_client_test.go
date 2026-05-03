package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverHubsUsesBearerTokenAndReturnsHubList(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotAuth string
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hubs": []map[string]any{{
				"id":         "hub_1",
				"region":     "iad",
				"http_url":   "https://hub-1.termx.test",
				"status":     "online",
				"capacity":   42,
				"health":     `{"ok":true}`,
				"expires_at": "2026-05-03T18:00:00Z",
			}},
		})
	}))
	defer control.Close()

	hubs, err := DiscoverHubs(context.Background(), control.URL, "access-secret")
	if err != nil {
		t.Fatalf("DiscoverHubs returned error: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/hubs" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer access-secret" {
		t.Fatalf("expected bearer token auth, got %q", gotAuth)
	}
	if len(hubs) != 1 {
		t.Fatalf("expected one hub, got %+v", hubs)
	}
	if hubs[0].ID != "hub_1" || hubs[0].Region != "iad" || hubs[0].HTTPURL != "https://hub-1.termx.test" ||
		hubs[0].Status != "online" || hubs[0].Capacity != 42 {
		t.Fatalf("unexpected hub payload: %+v", hubs[0])
	}
}
