package cloudcompanion

import (
	"bytes"
	"testing"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestEnrollmentProofSigningBytesAreDeterministicAndContextBound(t *testing.T) {
	input := &cloudpb.DeviceEnrollmentProofInput{
		FlowId: "flow-1", ChallengeId: "challenge-1", Challenge: bytes.Repeat([]byte{1}, 32),
		DeviceId: "device-1", DevicePublicKey: bytes.Repeat([]byte{2}, 32), SignedAtUnixNano: 123456789,
		CandidateSetDigest: bytes.Repeat([]byte{3}, 32), PreferredHubId: "hub-1",
		HubObservationsDigest: bytes.Repeat([]byte{4}, 32), FlowRevision: 2,
	}
	first, err := EnrollmentProofSigningBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnrollmentProofSigningBytes(input)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("deterministic signing bytes mismatch: %v", err)
	}
	changed := proto.Clone(input).(*cloudpb.DeviceEnrollmentProofInput)
	changed.DeviceId = "device-2"
	other, err := EnrollmentProofSigningBytes(changed)
	if err != nil || bytes.Equal(first, other) {
		t.Fatal("device identity must change enrollment signing bytes")
	}
	for name, mutate := range map[string]func(*cloudpb.DeviceEnrollmentProofInput){
		"candidate set": func(value *cloudpb.DeviceEnrollmentProofInput) { value.CandidateSetDigest[0]++ },
		"preferred Hub": func(value *cloudpb.DeviceEnrollmentProofInput) { value.PreferredHubId = "hub-2" },
		"observations":  func(value *cloudpb.DeviceEnrollmentProofInput) { value.HubObservationsDigest[0]++ },
		"revision":      func(value *cloudpb.DeviceEnrollmentProofInput) { value.FlowRevision++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(input).(*cloudpb.DeviceEnrollmentProofInput)
			mutate(changed)
			other, err := EnrollmentProofSigningBytes(changed)
			if err != nil || bytes.Equal(first, other) {
				t.Fatalf("%s must change enrollment signing bytes: %v", name, err)
			}
		})
	}
}

func TestEnrollmentDirectoryDigestsAreOrderIndependentAndRejectDuplicates(t *testing.T) {
	candidates := []*cloudpb.HubEnrollmentCandidate{
		{HubId: "hub-b", HubUrl: "https://b.example", HealthUrl: "https://b.example/healthz", Region: "b"},
		{HubId: "hub-a", HubUrl: "https://a.example", HealthUrl: "https://a.example/healthz", Region: "a"},
	}
	first, err := EnrollmentCandidateSetDigest(candidates)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnrollmentCandidateSetDigest([]*cloudpb.HubEnrollmentCandidate{candidates[1], candidates[0]})
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("candidate digest depends on wire order: %v", err)
	}
	if _, err := EnrollmentCandidateSetDigest([]*cloudpb.HubEnrollmentCandidate{candidates[0], candidates[0]}); err == nil {
		t.Fatal("duplicate candidate must fail closed")
	}
	observations := []*cloudpb.HubReachabilityObservation{{HubId: "hub-b", Reachable: false}, {HubId: "hub-a", Reachable: true, LatencyMillis: 12}}
	first, err = EnrollmentObservationsDigest(observations)
	if err != nil {
		t.Fatal(err)
	}
	second, err = EnrollmentObservationsDigest([]*cloudpb.HubReachabilityObservation{observations[1], observations[0]})
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("observation digest depends on wire order: %v", err)
	}
	if _, err := EnrollmentObservationsDigest([]*cloudpb.HubReachabilityObservation{observations[0], observations[0]}); err == nil {
		t.Fatal("duplicate observation must fail closed")
	}
}

func TestPresenceProofSigningBytesSeparatePresenceAndManagedIdentity(t *testing.T) {
	input := &cloudpb.PresenceProofInput{
		PresenceSessionId: "presence-1", ChallengeId: "challenge-1", Challenge: bytes.Repeat([]byte{3}, 32),
		DeviceId: "device-1", DevicePublicKey: bytes.Repeat([]byte{4}, 32), SignedAtUnixNano: 987654321,
	}
	first, err := PresenceProofSigningBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	changed := proto.Clone(input).(*cloudpb.PresenceProofInput)
	changed.PresenceSessionId = "managed-1"
	other, err := PresenceProofSigningBytes(changed)
	if err != nil || bytes.Equal(first, other) {
		t.Fatal("presence session identity must change presence signing bytes")
	}
	if _, err := PresenceProofSigningBytes(&cloudpb.PresenceProofInput{}); err == nil {
		t.Fatal("empty presence proof input must fail closed")
	}
}
