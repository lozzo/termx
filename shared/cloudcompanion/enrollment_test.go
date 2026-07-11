package cloudcompanion

import (
	"bytes"
	"testing"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestEnrollmentProofSigningBytesAreDeterministicAndContextBound(t *testing.T) {
	input := &cloudpb.DeviceEnrollmentProofInput{
		FlowId: "flow-1", ChallengeId: "challenge-1", Challenge: bytes.Repeat([]byte{1}, 32),
		DeviceId: "device-1", DevicePublicKey: bytes.Repeat([]byte{2}, 32), SignedAtUnixNano: 123456789,
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
