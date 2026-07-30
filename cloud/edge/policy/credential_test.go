package policy_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCredentialMaterialIsReservationSpecificAndRestartBound(t *testing.T) {
	now := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	grant := &cloudv1.RelayGrant{ReservationId: "reservation-r6", SessionId: "session-r6", AuthorizedUntil: timestamppb.New(now.Add(time.Minute))}
	first, err := policy.NewCredentialDeriver(bytes.Repeat([]byte{0x11}, 32), []string{"turn:edge.example:3478?transport=udp"})
	if err != nil {
		t.Fatal(err)
	}
	material, err := first.Material(grant)
	if err != nil {
		t.Fatal(err)
	}
	if material.GetReservationId() != grant.GetReservationId() || material.GetUsername() != "v2:reservation-r6:session-r6" || material.GetCredential() == "" || !material.GetExpiresAt().AsTime().Equal(grant.GetAuthorizedUntil().AsTime()) {
		t.Fatalf("incomplete credential material: %v", material)
	}
	renewedGrant := proto.Clone(grant).(*cloudv1.RelayGrant)
	renewedGrant.AuthorizedUntil = timestamppb.New(now.Add(2 * time.Minute))
	renewed, err := first.Material(renewedGrant)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.GetUsername() != material.GetUsername() || renewed.GetCredential() != material.GetCredential() || !renewed.GetExpiresAt().AsTime().After(material.GetExpiresAt().AsTime()) {
		t.Fatalf("renewal changed stable credential: initial=%v renewed=%v", material, renewed)
	}
	second, _ := policy.NewCredentialDeriver(bytes.Repeat([]byte{0x22}, 32), material.GetUrls())
	if second.Password(material.GetUsername()) == material.GetCredential() {
		t.Fatal("credential survived an Edge process secret replacement")
	}
}
