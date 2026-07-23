package controller

import (
	"errors"
	"testing"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestEnrollmentSelectionPrefersReachabilityLatencyThenCurrentLoad(t *testing.T) {
	offered := []*cloudpb.HubEnrollmentCandidate{
		enrollmentCandidate("hub-a"),
		enrollmentCandidate("hub-b"),
		enrollmentCandidate("hub-c"),
	}
	current := enrollmentCandidatesWithLoad(offered, []uint64{8, 2, 1}, 100)
	selected, err := selectEnrollmentHub("daemon-1", offered, current, []*cloudpb.HubReachabilityObservation{
		{HubId: "hub-a", Reachable: true, LatencyMillis: 8},
		{HubId: "hub-b", Reachable: true, LatencyMillis: 20},
		{HubId: "hub-c"},
	}, "")
	if err != nil || selected.GetHubId() != "hub-a" {
		t.Fatalf("latency selection = (%v, %v)", selected, err)
	}
	selected, err = selectEnrollmentHub("daemon-1", offered, current, []*cloudpb.HubReachabilityObservation{
		{HubId: "hub-a", Reachable: true, LatencyMillis: 10},
		{HubId: "hub-b", Reachable: true, LatencyMillis: 10},
	}, "")
	if err != nil || selected.GetHubId() != "hub-b" {
		t.Fatalf("load tie-break = (%v, %v)", selected, err)
	}
}

func TestEnrollmentSelectionRejectsForgedObservationAndFullHub(t *testing.T) {
	offered := []*cloudpb.HubEnrollmentCandidate{enrollmentCandidate("hub-a")}
	current := enrollmentCandidatesWithLoad(offered, []uint64{0}, 100)
	if _, err := selectEnrollmentHub("daemon-1", offered, current, []*cloudpb.HubReachabilityObservation{{HubId: "hub-forged", Reachable: true, LatencyMillis: 1}}, ""); !errors.Is(err, errEnrollmentDenied) {
		t.Fatalf("forged observation error = %v", err)
	}
	full := enrollmentCandidatesWithLoad(offered, []uint64{100}, 100)
	if _, err := selectEnrollmentHub("daemon-1", offered, full, nil, ""); !errors.Is(err, errEnrollmentDenied) {
		t.Fatalf("full Hub error = %v", err)
	}
}

func TestEnrollmentSelectionHasDeterministicFallbackAndDoesNotMigrateExistingAssignment(t *testing.T) {
	candidates := []*cloudpb.HubEnrollmentCandidate{enrollmentCandidate("hub-a"), enrollmentCandidate("hub-b")}
	current := enrollmentCandidatesWithLoad(candidates, []uint64{4, 4}, 100)
	first, err := selectEnrollmentHub("daemon-stable", candidates, current, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		next, err := selectEnrollmentHub("daemon-stable", candidates, current, nil, "")
		if err != nil || next.GetHubId() != first.GetHubId() {
			t.Fatalf("fallback changed = (%v, %v), want %s", next, err, first.GetHubId())
		}
	}
	selected, err := selectEnrollmentHub("daemon-stable", candidates, current, []*cloudpb.HubReachabilityObservation{{HubId: "hub-a", Reachable: true, LatencyMillis: 1}}, "hub-b")
	if err != nil || selected.GetHubId() != "hub-b" {
		t.Fatalf("existing assignment selection = (%v, %v)", selected, err)
	}
}

func enrollmentCandidate(hubID string) *cloudpb.HubEnrollmentCandidate {
	return &cloudpb.HubEnrollmentCandidate{HubId: hubID, HubUrl: "https://" + hubID + ".example.test", HealthUrl: "https://" + hubID + ".example.test/healthz", Region: "test-1"}
}

func enrollmentCandidatesWithLoad(values []*cloudpb.HubEnrollmentCandidate, assignments []uint64, maximum uint64) []enrollmentHubCandidate {
	result := make([]enrollmentHubCandidate, 0, len(values))
	for index, value := range values {
		result = append(result, enrollmentHubCandidate{value: value, assignmentCount: assignments[index], maxAssignments: maximum})
	}
	return result
}
