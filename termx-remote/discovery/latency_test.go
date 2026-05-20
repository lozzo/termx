package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.Path != "/api/health" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got := ProbeHub(context.Background(), srv.URL, time.Second)
	if !got.Available {
		t.Fatalf("expected hub available: %+v", got)
	}
	if got.URL != srv.URL {
		t.Fatalf("URL = %q, want %q", got.URL, srv.URL)
	}
	if got.Latency < 0 {
		t.Fatalf("expected non-negative latency, got %s", got.Latency)
	}
}

func TestProbeHubUnavailable(t *testing.T) {
	got := ProbeHub(context.Background(), "http://127.0.0.1:1", 10*time.Millisecond)
	if got.Available {
		t.Fatalf("expected unavailable result: %+v", got)
	}
	if got.Latency != -1 {
		t.Fatalf("expected -1 latency, got %s", got.Latency)
	}
}

func TestProbeHubsSortsAvailableBeforeUnavailable(t *testing.T) {
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fast.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	results := ProbeHubs(context.Background(), []string{"http://127.0.0.1:1", slow.URL, fast.URL}, time.Second, 1)
	if len(results) != 3 {
		t.Fatalf("results length = %d, want 3", len(results))
	}
	if !results[0].Available || !results[1].Available || results[2].Available {
		t.Fatalf("unexpected availability order: %+v", results)
	}
	if results[0].Latency > results[1].Latency {
		t.Fatalf("expected latency sort, got %+v", results)
	}
}
