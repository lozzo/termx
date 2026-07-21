package cloudcompanion

import (
	"strings"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestValidateSingleRelayLeaseRejectsUnsafeMaterial(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	request := &cloudpb.AcquireRelayLeaseRequest{
		ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1",
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
	}
	valid := &cloudpb.RelayLease{
		LeaseId: "lease-1", SignedLease: []byte("signed-lease"), ExpiresAtUnix: uint64(now.Add(time.Minute).Unix()),
		PathKind:   cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
		IceServers: []*cloudpb.IceServer{{Urls: []string{"turn:relay.example.test:3478?transport=udp"}, Username: "client-user", Credential: "client-password"}},
	}
	if err := ValidateSingleRelayLease(request, valid, now); err != nil {
		t.Fatalf("valid single Relay lease = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*cloudpb.AcquireRelayLeaseRequest, *cloudpb.RelayLease)
	}{
		{name: "direct preference", mutate: func(request *cloudpb.AcquireRelayLeaseRequest, _ *cloudpb.RelayLease) {
			request.RoutePreference = cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY
		}},
		{name: "expired", mutate: func(_ *cloudpb.AcquireRelayLeaseRequest, lease *cloudpb.RelayLease) {
			lease.ExpiresAtUnix = uint64(now.Unix())
		}},
		{name: "overlong", mutate: func(_ *cloudpb.AcquireRelayLeaseRequest, lease *cloudpb.RelayLease) {
			lease.ExpiresAtUnix = uint64(now.Add(11 * time.Minute).Unix())
		}},
		{name: "mesh field", mutate: func(_ *cloudpb.AcquireRelayLeaseRequest, lease *cloudpb.RelayLease) {
			lease.RouteId = "route-1"
		}},
		{name: "stun URL", mutate: func(_ *cloudpb.AcquireRelayLeaseRequest, lease *cloudpb.RelayLease) {
			lease.IceServers[0].Urls[0] = "stun:stun.example.test"
		}},
		{name: "missing principal credential", mutate: func(_ *cloudpb.AcquireRelayLeaseRequest, lease *cloudpb.RelayLease) {
			lease.IceServers[0].Credential = ""
		}},
		{name: "noncanonical lease ID", mutate: func(_ *cloudpb.AcquireRelayLeaseRequest, lease *cloudpb.RelayLease) {
			lease.LeaseId = " lease-1"
		}},
		{name: "oversized signed lease", mutate: func(_ *cloudpb.AcquireRelayLeaseRequest, lease *cloudpb.RelayLease) {
			lease.SignedLease = []byte(strings.Repeat("x", maxSignedRelayLeaseBytes+1))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentRequest := proto.Clone(request).(*cloudpb.AcquireRelayLeaseRequest)
			currentLease := proto.Clone(valid).(*cloudpb.RelayLease)
			test.mutate(currentRequest, currentLease)
			err := ValidateSingleRelayLease(currentRequest, currentLease, now)
			if !IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
				t.Fatalf("unsafe single Relay material error = %v", err)
			}
		})
	}
}
