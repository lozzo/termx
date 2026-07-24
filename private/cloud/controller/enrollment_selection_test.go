package controller

import (
	"errors"
	"testing"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestEnrollmentProposalAcceptsDaemonPreferredReachableHub(t *testing.T) {
	offered := []*cloudpb.HubEnrollmentCandidate{enrollmentCandidate("hub-a"), enrollmentCandidate("hub-b")}
	current := enrollmentCandidatesWithLoad(offered, []uint64{8, 2}, 100)
	selected, err := validateEnrollmentHubProposal(offered, current, []*cloudpb.HubReachabilityObservation{
		{HubId: "hub-a", Reachable: true, LatencyMillis: 8},
		{HubId: "hub-b", Reachable: true, LatencyMillis: 20},
	}, "hub-b", "")
	if err != nil || selected.GetHubId() != "hub-b" {
		t.Fatalf("daemon proposal = (%v, %v)", selected, err)
	}
}

func TestEnrollmentProposalRejectsForgedUnreachableAndStaleHub(t *testing.T) {
	offered := []*cloudpb.HubEnrollmentCandidate{enrollmentCandidate("hub-a")}
	current := enrollmentCandidatesWithLoad(offered, []uint64{0}, 100)
	if _, err := validateEnrollmentHubProposal(offered, current, []*cloudpb.HubReachabilityObservation{{HubId: "hub-forged", Reachable: true, LatencyMillis: 1}}, "hub-forged", ""); !errors.Is(err, errEnrollmentDenied) {
		t.Fatalf("forged observation error = %v", err)
	}
	if _, err := validateEnrollmentHubProposal(offered, current, []*cloudpb.HubReachabilityObservation{{HubId: "hub-a"}}, "hub-a", ""); !errors.Is(err, errEnrollmentNoReachableHub) {
		t.Fatalf("unreachable Hub error = %v", err)
	}
	full := enrollmentCandidatesWithLoad(offered, []uint64{100}, 100)
	if _, err := validateEnrollmentHubProposal(offered, full, []*cloudpb.HubReachabilityObservation{{HubId: "hub-a", Reachable: true, LatencyMillis: 5}}, "hub-a", ""); !errors.Is(err, errEnrollmentCandidateStale) {
		t.Fatalf("full Hub error = %v", err)
	}
}

func TestEnrollmentProposalKeepsExistingAssignment(t *testing.T) {
	candidates := []*cloudpb.HubEnrollmentCandidate{enrollmentCandidate("hub-a"), enrollmentCandidate("hub-b")}
	current := enrollmentCandidatesWithLoad(candidates, []uint64{4, 4}, 100)
	observations := []*cloudpb.HubReachabilityObservation{{HubId: "hub-a", Reachable: true, LatencyMillis: 1}, {HubId: "hub-b", Reachable: true, LatencyMillis: 2}}
	if _, err := validateEnrollmentHubProposal(candidates, current, observations, "hub-a", "hub-b"); !errors.Is(err, errEnrollmentCandidateStale) {
		t.Fatalf("existing assignment migration error = %v", err)
	}
	selected, err := validateEnrollmentHubProposal(candidates, current, observations, "hub-b", "hub-b")
	if err != nil || selected.GetHubId() != "hub-b" {
		t.Fatalf("existing assignment proposal = (%v, %v)", selected, err)
	}
}

func TestEnrollmentProposalDoesNotDoubleCountExistingAssignmentCapacity(t *testing.T) {
	candidates := []*cloudpb.HubEnrollmentCandidate{enrollmentCandidate("hub-full")}
	current := enrollmentCandidatesWithLoad(candidates, []uint64{1}, 1)
	observations := []*cloudpb.HubReachabilityObservation{{HubId: "hub-full", Reachable: true, LatencyMillis: 5}}
	selected, err := validateEnrollmentHubProposal(candidates, current, observations, "hub-full", "hub-full")
	if err != nil || selected.GetHubId() != "hub-full" {
		t.Fatalf("existing assignment at capacity = (%v, %v)", selected, err)
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
