package policy_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCredentialMaterialIsSessionSpecificAndRestartBound(t *testing.T) {
	now := time.Now().UTC()
	claims := &cloudv1.RelayLeaseClaims{LeaseId: "lease-r6", SessionId: "session-r6", ExpiresAt: timestamppb.New(now.Add(time.Minute))}
	first, err := policy.NewCredentialDeriver(bytes.Repeat([]byte{0x11}, 32), []string{"turn:edge.example:3478?transport=udp"})
	if err != nil {
		t.Fatal(err)
	}
	material, err := first.Material(claims)
	if err != nil {
		t.Fatal(err)
	}
	if material.GetUsername() == "" || material.GetCredential() == "" || material.GetExpiresAt().AsTime() != claims.GetExpiresAt().AsTime() {
		t.Fatalf("incomplete material: %v", material)
	}
	second, _ := policy.NewCredentialDeriver(bytes.Repeat([]byte{0x22}, 32), material.GetUrls())
	if second.Password(material.GetUsername()) == material.GetCredential() {
		t.Fatal("credential survived an Edge process secret replacement")
	}
}
