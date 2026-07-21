package cloudcompanion

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	endpointdomain "github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

type endpointContractFixture struct {
	SchemaVersion               int                        `json:"schema_version"`
	Transport                   string                     `json:"transport"`
	Phases                      []string                   `json:"phases"`
	ObservedPaths               []string                   `json:"observed_paths"`
	RelayPolicies               []relayPolicyFixture       `json:"relay_policies"`
	RouteSelectionReasons       []string                   `json:"route_selection_reasons"`
	RoutePlanFields             []string                   `json:"route_plan_fields"`
	ForbiddenRoutePlanFragments []string                   `json:"forbidden_route_plan_fragments"`
	CloudErrors                 []string                   `json:"cloud_errors"`
	AuthorizationCases          []authorizationCaseFixture `json:"authorization_cases"`
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
	if fixture.SchemaVersion != 2 || fixture.Transport != string(endpointdomain.RouteManagedWebRTC) {
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
		policy, err := DialPolicyForRelayMode(endpointdomain.RelayMode(policyFixture.RelayMode))
		if err != nil {
			t.Fatalf("relay policy %q: %v", policyFixture.RelayMode, err)
		}
		if got := routePreferenceName(policy.RoutePreference); got != policyFixture.RoutePreference || policy.RelayOnly != policyFixture.RelayOnly {
			t.Fatalf("relay policy %q = %q relayOnly=%v", policyFixture.RelayMode, got, policy.RelayOnly)
		}
	}
	wantReasons := []string{
		string(RouteReasonInitialBest), string(RouteReasonOnlyViable), string(RouteReasonLowerLoss),
		string(RouteReasonDirectUnstable), string(RouteReasonLowerLatency), string(RouteReasonLowerScore),
		string(RouteReasonCostGuard), string(RouteReasonMinimumHold), string(RouteReasonCooldown),
		string(RouteReasonHysteresisHold), string(RouteReasonInsufficientImprovement),
		string(RouteReasonCurrentUnavailable), string(RouteReasonCurrentBest),
	}
	if !equalStrings(fixture.RouteSelectionReasons, wantReasons) {
		t.Fatalf("route selection reasons = %#v, want %#v", fixture.RouteSelectionReasons, wantReasons)
	}
	for index, want := range fixture.RouteSelectionReasons {
		got := RouteSelectionReasonFromWire(cloudpb.RouteSelectionReason(index + 1))
		if string(got) != want || !IsKnownRouteSelectionReason(got) {
			t.Fatalf("route reason wire %d = %q, want %q", index+1, got, want)
		}
	}
	if RouteSelectionReasonFromWire(cloudpb.RouteSelectionReason(99)) != "" || IsKnownRouteSelectionReason("") {
		t.Fatal("unknown route reason must remain invalid")
	}
	descriptor := (&cloudpb.ManagedRoutePlan{}).ProtoReflect().Descriptor()
	fields := make([]string, 0, descriptor.Fields().Len())
	for index := 0; index < descriptor.Fields().Len(); index++ {
		fields = append(fields, string(descriptor.Fields().Get(index).Name()))
	}
	if !equalStrings(fixture.RoutePlanFields, fields) {
		t.Fatalf("route plan fields = %#v, want %#v", fields, fixture.RoutePlanFields)
	}
	for _, field := range fields {
		for _, fragment := range fixture.ForbiddenRoutePlanFragments {
			if strings.Contains(field, fragment) {
				t.Fatalf("route plan field %q contains forbidden fragment %q", field, fragment)
			}
		}
	}
	for _, testCase := range fixture.AuthorizationCases {
		cfg := endpointdomain.NewManagedEndpoint(
			endpointdomain.EndpointID(testCase.EndpointID), testCase.EndpointID,
			endpointdomain.DaemonIdentity{DeviceID: testCase.TargetDeviceID, DeviceFingerprint: testCase.DeviceFingerprint},
			testCase.TargetDeviceID, testCase.GrantRef, endpointdomain.RelayMode(testCase.RelayMode), endpointdomain.ConnectOnDemand,
		)
		route, _ := cfg.Route("cloud")
		err := ValidateManagedRoute(cfg, route)
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
