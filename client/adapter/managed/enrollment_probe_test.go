package managed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestEnrollmentProbeUsesBoundedWorkersAndReportsReachability(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	candidates := make([]*cloudpb.HubEnrollmentCandidate, 24)
	for index := range candidates {
		candidates[index] = enrollmentProbeCandidate(string(rune('a'+index)), server.URL)
	}
	observations, err := probeEnrollmentCandidates(context.Background(), server.Client(), candidates, 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != len(candidates) || maximum.Load() > 4 {
		t.Fatalf("probe results/workers = (%d, %d)", len(observations), maximum.Load())
	}
	for index, observation := range observations {
		if !observation.GetReachable() || observation.GetLatencyMillis() == 0 || observation.GetHubId() != candidates[index].GetHubId() {
			t.Fatalf("observation[%d] = %v", index, observation)
		}
	}
}

func TestEnrollmentProbeTimeoutAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()
	observations, err := probeEnrollmentCandidates(context.Background(), server.Client(), []*cloudpb.HubEnrollmentCandidate{enrollmentProbeCandidate("slow", server.URL)}, 1, 20*time.Millisecond)
	if err != nil || len(observations) != 1 || observations[0].GetReachable() {
		t.Fatalf("timeout observation = (%v, %v)", observations, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = probeEnrollmentCandidates(ctx, server.Client(), []*cloudpb.HubEnrollmentCandidate{enrollmentProbeCandidate("cancelled", server.URL)}, 1, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestEnrollmentProbeRejectsUntrustedOrDuplicateCandidates(t *testing.T) {
	client := &http.Client{}
	if _, err := probeEnrollmentCandidates(context.Background(), client, []*cloudpb.HubEnrollmentCandidate{enrollmentProbeCandidate("public-http", "http://example.com")}, 1, time.Second); err == nil {
		t.Fatal("public HTTP candidate was accepted")
	}
	duplicate := enrollmentProbeCandidate("same", "http://127.0.0.1:1")
	if _, err := probeEnrollmentCandidates(context.Background(), client, []*cloudpb.HubEnrollmentCandidate{duplicate, duplicate}, 1, time.Second); err == nil {
		t.Fatal("duplicate candidate was accepted")
	}
}

func TestPreferredEnrollmentHubUsesReachabilityLatencyAndStableID(t *testing.T) {
	preferred, err := PreferredEnrollmentHub([]*cloudpb.HubReachabilityObservation{
		{HubId: "hub-c"},
		{HubId: "hub-b", Reachable: true, LatencyMillis: 12},
		{HubId: "hub-a", Reachable: true, LatencyMillis: 12},
	})
	if err != nil || preferred != "hub-a" {
		t.Fatalf("preferred Hub = (%q, %v)", preferred, err)
	}
	if _, err := PreferredEnrollmentHub([]*cloudpb.HubReachabilityObservation{{HubId: "hub-a"}}); err == nil {
		t.Fatal("all-unreachable observations must fail")
	}
}

func enrollmentProbeCandidate(id, origin string) *cloudpb.HubEnrollmentCandidate {
	return &cloudpb.HubEnrollmentCandidate{HubId: "hub-" + id, HubUrl: origin, HealthUrl: origin + "/healthz", Region: "local-1"}
}
