package cloudcompanion

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestRelayLeaseUnavailableForAutoOnlyAcceptsProductDenial(t *testing.T) {
	for _, code := range []cloudpb.CloudErrorCode{
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED,
	} {
		if !RelayLeaseUnavailableForAuto(&Error{Code: code}) {
			t.Fatalf("AUTO did not accept product denial %s", code)
		}
	}
	for _, err := range []error{
		errors.New("network failure"),
		&Error{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY},
		&Error{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED},
		&Error{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL},
	} {
		if RelayLeaseUnavailableForAuto(err) {
			t.Fatalf("AUTO accepted unsafe Relay fallback for %v", err)
		}
	}
}

func TestFilterRelayTransportKeepsP2PAndOnlyRequestedTURN(t *testing.T) {
	servers := []*cloudpb.IceServer{{
		Urls:     []string{"stun:stun.example.test", "turn:relay.example.test:3478?transport=udp", "turn:relay.example.test:3478?transport=tcp"},
		Username: "user", Credential: "secret",
	}}
	filtered, hasTURN, err := FilterRelayTransport(servers, cloudpb.RelayTransport_RELAY_TRANSPORT_TCP)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTURN || len(filtered) != 1 || len(filtered[0].GetUrls()) != 2 || filtered[0].GetUrls()[1] != "turn:relay.example.test:3478?transport=tcp" {
		t.Fatalf("TCP filtered ICE servers = %#v", filtered)
	}
	filtered[0].Urls[0] = "changed"
	if servers[0].GetUrls()[0] != "stun:stun.example.test" {
		t.Fatal("filter mutated source ICE material")
	}

	filtered, hasTURN, err = FilterRelayTransport(servers, cloudpb.RelayTransport_RELAY_TRANSPORT_UDP)
	if err != nil || !hasTURN || len(filtered[0].GetUrls()) != 2 || filtered[0].GetUrls()[1] != "turn:relay.example.test:3478?transport=udp" {
		t.Fatalf("UDP filtered ICE servers = %#v hasTURN=%v err=%v", filtered, hasTURN, err)
	}
}

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
