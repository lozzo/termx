package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lozzow/termx/web-control/internal/httpapi"
)

func TestHealthHandlerReturnsControlPlaneStatus(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(httpapi.Config{
		ServiceName: "termx-web-control",
		Version:     "test-version",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body struct {
		Service   string `json:"service"`
		Status    string `json:"status"`
		Version   string `json:"version"`
		Runtime   string `json:"runtime"`
		Transport string `json:"transport"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health body: %v", err)
	}

	if body.Service != "termx-web-control" {
		t.Fatalf("service = %q", body.Service)
	}
	if body.Status != "ok" {
		t.Fatalf("status = %q", body.Status)
	}
	if body.Version != "test-version" {
		t.Fatalf("version = %q", body.Version)
	}
	if body.Runtime != "control-plane" {
		t.Fatalf("runtime = %q", body.Runtime)
	}
	if body.Transport != "signaling-control-only" {
		t.Fatalf("transport = %q", body.Transport)
	}
}

func TestHealthRouterDoesNotExposeRuntimeProxyRoutes(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(httpapi.Config{})

	for _, path := range []string{
		"/api/v1/terminal",
		"/api/v1/files/proxy",
		"/api/v1/events/stream",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, rec.Code)
		}
	}
}
