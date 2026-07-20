package cloudpb

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestCloudPlatformRoundTripPreservesUnknownFieldsAndEnums(t *testing.T) {
	command := &ManagementCommandProjection{
		CommandId:      "command-1",
		AccountId:      "account-1",
		CommandKind:    ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION,
		ExecutionState: CommandExecutionState(999),
		Target: &ManagementCommandTarget{Target: &ManagementCommandTarget_PeerSession{PeerSession: &ManagedPeerSessionTarget{
			DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 4,
			AssignmentEpoch: 9, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1",
		}}},
	}
	payload, err := proto.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	unknown := protowire.AppendVarint(protowire.AppendTag(nil, 199, protowire.VarintType), 73)
	payload = append(payload, unknown...)
	decoded := &ManagementCommandProjection{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetExecutionState() != CommandExecutionState(999) || decoded.GetTarget().GetPeerSession().GetSessionIncarnation() != 4 {
		t.Fatalf("cloud command did not round trip unknown enum/oneof: %#v", decoded)
	}
	reencoded, err := proto.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(reencoded, unknown) {
		t.Fatalf("cloud command lost unknown field: %x", reencoded)
	}
}

func TestCloudPlatformOneofTargetsAreExclusive(t *testing.T) {
	target := &ManagementCommandTarget{Target: &ManagementCommandTarget_Presence{Presence: &KickPresenceTarget{PresenceSessionId: "presence-1"}}}
	target.Target = &ManagementCommandTarget_PeerSession{PeerSession: &ManagedPeerSessionTarget{ManagedSessionId: "managed-1", SessionIncarnation: 2}}
	if target.GetPresence() != nil || target.GetPeerSession().GetManagedSessionId() != "managed-1" {
		t.Fatalf("management target oneof is not exclusive: %#v", target)
	}

	envelope := &HubControlEnvelope{Payload: &HubControlEnvelope_FullProjection{FullProjection: &FullProjectionSnapshot{ProjectionRevision: 1}}}
	envelope.Payload = &HubControlEnvelope_Command{Command: &HubCommand{CommandId: "command-1"}}
	if envelope.GetFullProjection() != nil || envelope.GetCommand().GetCommandId() != "command-1" {
		t.Fatalf("hub envelope oneof is not exclusive: %#v", envelope)
	}
}

func TestEdgeDeploymentKeepsHubAndRelayControlIdentitySeparate(t *testing.T) {
	fields := descriptorFieldNames((&EdgeDeploymentMetadata{}).ProtoReflect().Descriptor())
	for _, required := range []string{
		"edge_deployment_id", "hub_id", "hub_control_identity_fingerprint", "relay_id", "relay_control_identity_fingerprint",
	} {
		if !fields[required] {
			t.Fatalf("EdgeDeploymentMetadata missing %q: %v", required, fields)
		}
	}
	hubFields := descriptorFieldNames((&HubControlEnvelope{}).ProtoReflect().Descriptor())
	relayFields := descriptorFieldNames((&RelayControlEnvelope{}).ProtoReflect().Descriptor())
	if !hubFields["control_generation"] || hubFields["relay_control_generation"] || !relayFields["relay_control_generation"] || relayFields["control_generation"] {
		t.Fatalf("Hub/Relay control generations are not separated: hub=%v relay=%v", hubFields, relayFields)
	}
}

func TestPresenceProjectionSeparatesAvailabilityAndFreshness(t *testing.T) {
	fields := descriptorFieldNames((&PresenceProjection{}).ProtoReflect().Descriptor())
	for _, required := range []string{"availability", "freshness", "observation_source", "observed_at_unix_millis", "fresh_until_unix_millis"} {
		if !fields[required] {
			t.Fatalf("PresenceProjection missing %q: %v", required, fields)
		}
	}
}

func TestManagementCommandSeparatesAuthorityDeliveryExecutionAndEffect(t *testing.T) {
	fields := descriptorFieldNames((&ManagementCommandProjection{}).ProtoReflect().Descriptor())
	for _, required := range []string{"authority_result", "delivery_state", "execution_state", "observed_effect", "children"} {
		if !fields[required] {
			t.Fatalf("ManagementCommandProjection missing %q: %v", required, fields)
		}
	}
}

func TestTopologyContractExcludesTerminalAndTransportSecrets(t *testing.T) {
	assertFieldsExcludeFragments(t, (&ManagedPeerSessionProjection{}).ProtoReflect().Descriptor(), []string{
		"terminal_id", "terminal_inventory", "grant", "scope", "sdp", "candidate", "credential", "private_key", "file", "payload", "ip_address",
	})
	assertFieldsExcludeFragments(t, (&TerminalAccessProjection{}).ProtoReflect().Descriptor(), []string{
		"terminal_id", "grant", "scope", "public_key", "private_key", "payload",
	})
}

func TestCloudManagementAPIMessagesAreProtoFirst(t *testing.T) {
	file := File_cloudpb_cloud_management_proto
	messages := file.Messages()
	for _, name := range []protoreflect.Name{
		"ListAccountDevicesRequest", "ListAccountDevicesResponse",
		"ListAccountTopologyRequest", "ListAccountTopologyResponse",
		"GetManagedSessionRequest", "GetManagedSessionResponse",
		"ListDaemonTerminalAccessRequest", "ListDaemonTerminalAccessResponse",
		"CreateManagementCommandRequest", "CreateManagementCommandResponse",
		"GetManagementCommandRequest", "GetManagementCommandResponse",
		"ListManagementCommandsRequest", "ListManagementCommandsResponse",
		"ListHubFleetRequest", "ListHubFleetResponse", "GetHubStatusRequest", "GetHubStatusResponse",
	} {
		if messages.ByName(name) == nil {
			t.Fatalf("cloud management proto missing %s", name)
		}
	}
}

func TestCloudProductUsesOnePlanCapabilityAcrossCatalogEntitlementAndHubPolicy(t *testing.T) {
	capabilityFields := descriptorFieldNames((&PlanCapability{}).ProtoReflect().Descriptor())
	for _, required := range []string{"managed_p2p_enabled", "managed_p2p_max_concurrency", "standard_relay_enabled", "relay", "cloud_device_limit"} {
		if !capabilityFields[required] {
			t.Fatalf("PlanCapability missing %q: %v", required, capabilityFields)
		}
	}
	relayFields := descriptorFieldNames((&RelayServiceCapability{}).ProtoReflect().Descriptor())
	if !relayFields["max_bytes_per_period"] {
		t.Fatalf("RelayServiceCapability missing period quota: %v", relayFields)
	}
	if (&PlanDefinition{}).ProtoReflect().Descriptor().Fields().ByName("capability").Message().FullName() != (&PlanCapability{}).ProtoReflect().Descriptor().FullName() {
		t.Fatal("PlanDefinition does not use PlanCapability")
	}
	if (&EntitlementProjection{}).ProtoReflect().Descriptor().Fields().ByName("capability").Message().FullName() != (&PlanCapability{}).ProtoReflect().Descriptor().FullName() {
		t.Fatal("EntitlementProjection does not use PlanCapability")
	}
	if (&HubAccountPolicy{}).ProtoReflect().Descriptor().Fields().ByName("capability").Message().FullName() != (&PlanCapability{}).ProtoReflect().Descriptor().FullName() {
		t.Fatal("HubAccountPolicy does not use PlanCapability")
	}
	for _, name := range []protoreflect.Name{
		"GetPlanCatalogRequest", "GetPlanCatalogResponse", "GetAccountSubscriptionRequest", "GetAccountSubscriptionResponse", "GetAccountEntitlementRequest", "GetAccountEntitlementResponse",
	} {
		if File_cloudpb_cloud_product_proto.Messages().ByName(name) == nil {
			t.Fatalf("cloud product proto missing %s", name)
		}
	}
}

func TestHubControlHTTPBoundaryIsProtoFirst(t *testing.T) {
	messages := File_cloudpb_cloud_hub_control_proto.Messages()
	for _, name := range []protoreflect.Name{
		"HubControlChallengeRequest", "HubControlChallengeResponse", "HubHello",
		"ReportHubRuntimeRequest", "ReportHubRuntimeResponse",
	} {
		if messages.ByName(name) == nil {
			t.Fatalf("Hub control HTTP contract missing %s", name)
		}
	}
}

func TestCloudPlatformDescriptorBaseline(t *testing.T) {
	payload, err := os.ReadFile("testdata/cloud-platform-v1.pb")
	if err != nil {
		t.Fatal(err)
	}
	baseline := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(payload, baseline); err != nil {
		t.Fatal(err)
	}
	current := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(File_cloudpb_cloud_companion_proto),
		protodesc.ToFileDescriptorProto(File_cloudpb_cloud_product_proto),
		protodesc.ToFileDescriptorProto(File_cloudpb_cloud_topology_proto),
		protodesc.ToFileDescriptorProto(File_cloudpb_cloud_hub_control_proto),
		protodesc.ToFileDescriptorProto(File_cloudpb_cloud_management_proto),
	}}
	if !proto.Equal(baseline, current) {
		t.Fatal("cloud platform descriptor differs from testdata/cloud-platform-v1.pb")
	}
}

func descriptorFieldNames(message protoreflect.MessageDescriptor) map[string]bool {
	fields := make(map[string]bool, message.Fields().Len())
	for index := 0; index < message.Fields().Len(); index++ {
		fields[string(message.Fields().Get(index).Name())] = true
	}
	return fields
}

func assertFieldsExcludeFragments(t *testing.T, message protoreflect.MessageDescriptor, forbidden []string) {
	t.Helper()
	for field := range descriptorFieldNames(message) {
		for _, fragment := range forbidden {
			if strings.Contains(field, fragment) {
				t.Fatalf("%s field %q contains forbidden fragment %q", message.FullName(), field, fragment)
			}
		}
	}
}
