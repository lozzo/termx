package control

import (
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
)

func TestRelayLeaseIDPreservesExplicitRenewalIdentity(t *testing.T) {
	existing := uuid.NewString()
	got, err := relayLeaseID(&cloudv1.RelayLeaseRequest{RenewLeaseId: existing})
	if err != nil || got != existing {
		t.Fatalf("renewal lease ID = %q, err=%v", got, err)
	}
	created, err := relayLeaseID(&cloudv1.RelayLeaseRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(created); err != nil || created == existing {
		t.Fatalf("initial lease ID = %q, err=%v", created, err)
	}
	if _, err := relayLeaseID(&cloudv1.RelayLeaseRequest{RenewLeaseId: "invalid"}); err == nil {
		t.Fatal("invalid renewal lease ID was accepted")
	}
}

func TestRelayRenewalRequiresControlProtocolV2(t *testing.T) {
	if ProtocolVersion != 2 {
		t.Fatalf("EdgeControl protocol version = %d, want=2", ProtocolVersion)
	}
}
