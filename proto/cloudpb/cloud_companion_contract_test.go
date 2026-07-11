package cloudpb

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCloudCompanionContractExcludesEndToEndSecretsAndTerminalData(t *testing.T) {
	forbidden := []string{
		"capability_grant",
		"grant",
		"private_key",
		"session_token",
		"terminal",
		"data_channel",
		"datachannel",
		"protocol_frame",
	}
	messages := File_cloudpb_cloud_companion_proto.Messages()
	for index := 0; index < messages.Len(); index++ {
		assertMessageFieldsAllowed(t, messages.Get(index), forbidden)
	}
}

func TestSignalingOfferHasOnlyRoutingAndWebRTCFields(t *testing.T) {
	descriptor := (&SignalingOffer{}).ProtoReflect().Descriptor()
	want := map[string]bool{
		"signaling_session_id": true,
		"managed_session_id":   true,
		"source_device_id":     true,
		"target_device_id":     true,
		"sdp":                  true,
		"candidates":           true,
		"route_preference":     true,
		"relay_only":           true,
	}
	if descriptor.Fields().Len() != len(want) {
		t.Fatalf("SignalingOffer field count = %d, want %d", descriptor.Fields().Len(), len(want))
	}
	for index := 0; index < descriptor.Fields().Len(); index++ {
		name := string(descriptor.Fields().Get(index).Name())
		if !want[name] {
			t.Fatalf("SignalingOffer contains unexpected field %q", name)
		}
	}
}

func TestPathQualitySummaryContainsOnlyRedactedWindowMetrics(t *testing.T) {
	descriptor := (&PathQualitySummary{}).ProtoReflect().Descriptor()
	want := map[string]bool{
		"managed_session_id":            true,
		"observed_path":                 true,
		"rtt_p50_millis":                true,
		"jitter_millis":                 true,
		"loss_basis_points":             true,
		"throughput_bps":                true,
		"connected_millis":              true,
		"network_class":                 true,
		"region":                        true,
		"rtt_p95_millis":                true,
		"sample_count":                  true,
		"disconnect_count":              true,
		"window_started_at_unix_millis": true,
		"window_ended_at_unix_millis":   true,
		"packet_count":                  true,
		"loss_event_count":              true,
		"carrier_tag":                   true,
		"provider_tag":                  true,
	}
	if descriptor.Fields().Len() != len(want) {
		t.Fatalf("PathQualitySummary field count = %d, want %d", descriptor.Fields().Len(), len(want))
	}
	for index := 0; index < descriptor.Fields().Len(); index++ {
		name := string(descriptor.Fields().Get(index).Name())
		if !want[name] {
			t.Fatalf("PathQualitySummary contains unexpected field %q", name)
		}
	}
}

func TestManagedRoutePlanContainsOnlyExecutableDiagnostics(t *testing.T) {
	descriptor := (&ManagedRoutePlan{}).ProtoReflect().Descriptor()
	want := map[string]bool{
		"plan_id":            true,
		"managed_session_id": true,
		"target_device_id":   true,
		"selected_path":      true,
		"selection_reason":   true,
		"valid_until_unix":   true,
		"ice_servers":        true,
		"relay_only":         true,
		"relay_region":       true,
	}
	if descriptor.Fields().Len() != len(want) {
		t.Fatalf("ManagedRoutePlan field count = %d, want %d", descriptor.Fields().Len(), len(want))
	}
	for index := 0; index < descriptor.Fields().Len(); index++ {
		name := string(descriptor.Fields().Get(index).Name())
		if !want[name] {
			t.Fatalf("ManagedRoutePlan contains unexpected field %q", name)
		}
	}
	for _, fragment := range []string{"cost", "score", "weight", "terminal", "grant", "payload"} {
		for field := range want {
			if strings.Contains(field, fragment) {
				t.Fatalf("ManagedRoutePlan field %q contains forbidden fragment %q", field, fragment)
			}
		}
	}
}

func TestPlanManagedRouteRequestContainsOnlyRouteBinding(t *testing.T) {
	descriptor := (&PlanManagedRouteRequest{}).ProtoReflect().Descriptor()
	want := map[string]bool{
		"endpoint_id":        true,
		"managed_session_id": true,
		"target_device_id":   true,
		"route_preference":   true,
	}
	if descriptor.Fields().Len() != len(want) {
		t.Fatalf("PlanManagedRouteRequest field count = %d, want %d", descriptor.Fields().Len(), len(want))
	}
	for index := 0; index < descriptor.Fields().Len(); index++ {
		name := string(descriptor.Fields().Get(index).Name())
		if !want[name] {
			t.Fatalf("PlanManagedRouteRequest contains unexpected field %q", name)
		}
	}
	for _, fragment := range []string{"cost", "score", "weight", "terminal", "grant", "payload", "candidate"} {
		for field := range want {
			if strings.Contains(field, fragment) {
				t.Fatalf("PlanManagedRouteRequest field %q contains forbidden fragment %q", field, fragment)
			}
		}
	}
}

func assertMessageFieldsAllowed(t *testing.T, message protoreflect.MessageDescriptor, forbidden []string) {
	t.Helper()
	messageName := strings.ToLower(string(message.Name()))
	for _, fragment := range forbidden {
		if strings.Contains(messageName, fragment) {
			t.Fatalf("cloud companion message %s contains forbidden fragment %q", message.FullName(), fragment)
		}
	}
	fields := message.Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		name := strings.ToLower(string(field.Name()))
		for _, fragment := range forbidden {
			if strings.Contains(name, fragment) {
				t.Fatalf("cloud companion field %s.%s contains forbidden fragment %q", message.FullName(), field.Name(), fragment)
			}
		}
	}
	nested := message.Messages()
	for index := 0; index < nested.Len(); index++ {
		assertMessageFieldsAllowed(t, nested.Get(index), forbidden)
	}
}
