package cloudcompanion

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/connection"
)

type endpointContractFixture struct {
	SchemaVersion      int                        `json:"schema_version"`
	Transport          string                     `json:"transport"`
	Phases             []string                   `json:"phases"`
	ObservedPaths      []string                   `json:"observed_paths"`
	RelayPolicies      []relayPolicyFixture       `json:"relay_policies"`
	CloudErrors        []string                   `json:"cloud_errors"`
	AuthorizationCases []authorizationCaseFixture `json:"authorization_cases"`
}

type relayPolicyFixture struct {
	RelayMode       string `json:"relay_mode"`
	RoutePreference string `json:"route_preference"`
	RelayOnly       bool   `json:"relay_only"`
}

type authorizationCaseFixture struct {
	Name              string `json:"name"`
	EndpointID        string `json:"endpoint_id"`
	TargetDeviceID    string `json:"target_device_id"`
	DeviceFingerprint string `json:"device_fingerprint"`
	GrantRef          string `json:"grant_ref"`
	RelayMode         string `json:"relay_mode"`
	Valid             bool   `json:"valid"`
}

func TestManagedEndpointContractFixtureMatchesGoDomain(t *testing.T) {
	payload, err := os.ReadFile("testdata/managed_endpoint_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture endpointContractFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != 1 || fixture.Transport != string(connection.TransportHubP2P) {
		t.Fatalf("unexpected fixture header: %#v", fixture)
	}
	wantPhases := []string{"idle", "resolving", "signaling", "connecting", "authorizing", "connected", "failed"}
	if !equalStrings(fixture.Phases, wantPhases) {
		t.Fatalf("phases = %#v, want %#v", fixture.Phases, wantPhases)
	}
	wantPaths := []string{string(PathDirect), string(PathSingleRelay), string(PathRelayMesh)}
	if !equalStrings(fixture.ObservedPaths, wantPaths) {
		t.Fatalf("paths = %#v, want %#v", fixture.ObservedPaths, wantPaths)
	}
	for _, policyFixture := range fixture.RelayPolicies {
		policy, err := DialPolicyForRelayMode(connection.RelayMode(policyFixture.RelayMode))
		if err != nil {
			t.Fatalf("relay policy %q: %v", policyFixture.RelayMode, err)
		}
		if got := routePreferenceName(policy.RoutePreference); got != policyFixture.RoutePreference || policy.RelayOnly != policyFixture.RelayOnly {
			t.Fatalf("relay policy %q = %q relayOnly=%v", policyFixture.RelayMode, got, policy.RelayOnly)
		}
	}
	for _, testCase := range fixture.AuthorizationCases {
		cfg := connection.Config{
			ID: connection.EndpointID(testCase.EndpointID), Label: testCase.EndpointID,
			Transport: connection.TransportHubP2P, ConnectMode: connection.ConnectOnDemand, Enabled: true,
			HubURL: "https://hub.example.com", HubDeviceID: testCase.TargetDeviceID,
			DeviceFingerprint: testCase.DeviceFingerprint, GrantRef: testCase.GrantRef,
			RelayMode: connection.RelayMode(testCase.RelayMode),
		}
		err := ValidateManagedConfig(cfg)
		if (err == nil) != testCase.Valid {
			t.Fatalf("case %q valid=%v err=%v", testCase.Name, testCase.Valid, err)
		}
	}
	wantErrors := make([]string, 0, int(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY))
	for code := cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING; code <= cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY; code++ {
		wantErrors = append(wantErrors, StableErrorName(code))
	}
	if !equalStrings(fixture.CloudErrors, wantErrors) {
		t.Fatalf("cloud errors = %#v, want %#v", fixture.CloudErrors, wantErrors)
	}
}

func routePreferenceName(value cloudpb.RoutePreference) string {
	return strings.ToLower(strings.TrimPrefix(value.String(), "ROUTE_PREFERENCE_"))
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
