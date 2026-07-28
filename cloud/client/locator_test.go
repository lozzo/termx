package client

import (
	"errors"
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestCachedCapabilityRouteRoundTripsWithoutControllerTicket(t *testing.T) {
	edge := &cloudv1.CandidateEdge{EdgeId: "edge-a", PublicEndpoint: "edge.example:443", ServerName: "edge.example", CaCertificatePem: []byte("ca")}
	locator, err := EncodeEdgeLocator(edge)
	if err != nil {
		t.Fatal(err)
	}
	grantPayload, err := proto.Marshal(&cloudv1.SignedEnvelope{KeyId: "daemon-key", Payload: []byte("grant"), Signature: []byte("signature")})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := NewCachedCapabilityRoute(locator, grantPayload)
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolution.Edge()
	if resolved.GetEdgeId() != edge.GetEdgeId() || resolved.GetPublicEndpoint() != edge.GetPublicEndpoint() || resolved.GetServerName() != edge.GetServerName() {
		t.Fatalf("cached Edge = %v", resolved)
	}
	resolved.PublicEndpoint = "mutated"
	if resolution.Edge().GetPublicEndpoint() != edge.GetPublicEndpoint() {
		t.Fatal("cached route exposed mutable Edge state")
	}
}

func TestShouldRefreshEdgeLocatorOnlyForStaleOrUnreachableEdge(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "migrated", err: status.Error(codes.NotFound, "daemon moved"), want: true},
		{name: "unreachable", err: status.Error(codes.Unavailable, "edge unavailable"), want: true},
		{name: "wrapped unreachable", err: errors.Join(errors.New("exchange"), status.Error(codes.Unavailable, "edge unavailable")), want: true},
		{name: "unauthorized", err: status.Error(codes.Unauthenticated, "grant rejected")},
		{name: "daemon denied", err: status.Error(codes.PermissionDenied, "revoked")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ShouldRefreshEdgeLocator(test.err); got != test.want {
				t.Fatalf("ShouldRefreshEdgeLocator() = %v, want %v", got, test.want)
			}
		})
	}
}
