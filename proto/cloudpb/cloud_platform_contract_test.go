package cloudpb

import (
	"bytes"
	"os"
	"sort"
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
	target.Target = &ManagementCommandTarget_AssignmentMigration{AssignmentMigration: &AssignmentMigrationTarget{DaemonDeviceId: "daemon-1", TargetHubId: "hub-2"}}
	if target.GetPeerSession() != nil || target.GetAssignmentMigration().GetTargetHubId() != "hub-2" {
		t.Fatalf("assignment migration target oneof is not exclusive: %#v", target)
	}

	envelope := &HubControlEnvelope{Payload: &HubControlEnvelope_FullProjection{FullProjection: &FullProjectionSnapshot{ProjectionRevision: 1}}}
	envelope.Payload = &HubControlEnvelope_Command{Command: &HubCommand{CommandId: "command-1"}}
	if envelope.GetFullProjection() != nil || envelope.GetCommand().GetCommandId() != "command-1" {
		t.Fatalf("hub envelope oneof is not exclusive: %#v", envelope)
	}

	presence := &PresenceEvent{Payload: &PresenceEvent_Offer{Offer: &SignalingOffer{SignalingSessionId: "signal-1"}}}
	presence.Payload = &PresenceEvent_DaemonCommand{DaemonCommand: &DaemonControlCommand{CommandId: "daemon-command-1"}}
	if presence.GetOffer() != nil || presence.GetDaemonCommand().GetCommandId() != "daemon-command-1" {
		t.Fatalf("presence command oneof is not exclusive: %#v", presence)
	}
}

func TestDaemonCommandPresenceAndResultReportAreProtoFirst(t *testing.T) {
	command := &DaemonControlCommand{CommandId: "command-1", CommandKind: DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, AccountId: "account-1", TargetDeviceId: "daemon-1", HubId: "hub-1", AssignmentEpoch: 7, AuthEpoch: 3, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", Target: &DaemonControlCommand_ManagedPeerSession{ManagedPeerSession: &ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 2}}}
	payload, err := proto.Marshal(&PresenceEvent{Payload: &PresenceEvent_DaemonCommand{DaemonCommand: command}})
	if err != nil {
		t.Fatal(err)
	}
	decoded := &PresenceEvent{}
	if err := proto.Unmarshal(payload, decoded); err != nil || decoded.GetDaemonCommand().GetManagedPeerSession().GetSessionIncarnation() != 2 {
		t.Fatalf("daemon command presence round trip = (%#v, %v)", decoded, err)
	}
	request := &IPCRequest{RequestId: 9, Operation: &IPCRequest_ReportDaemonCommandResult{ReportDaemonCommandResult: &ReportDaemonCommandResultRequest{Result: &DaemonCommandResult{CommandId: "command-1", ResultCode: RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED}}}}
	payload, err = proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	decodedRequest := &IPCRequest{}
	if err := proto.Unmarshal(payload, decodedRequest); err != nil || decodedRequest.GetReportDaemonCommandResult().GetResult().GetCommandId() != "command-1" {
		t.Fatalf("daemon result report round trip = (%#v, %v)", decodedRequest, err)
	}
	for _, name := range []protoreflect.Name{"ReportDaemonCommandResultRequest", "ReportDaemonCommandResultResponse"} {
		if File_cloudpb_cloud_companion_proto.Messages().ByName(name) == nil {
			t.Fatalf("daemon command companion contract missing %s", name)
		}
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

func TestRelayQuotaAndReservationAreProtoFirst(t *testing.T) {
	periodFields := descriptorFieldNames((&RelayQuotaPeriod{}).ProtoReflect().Descriptor())
	for _, required := range []string{"account_id", "period_start_unix_millis", "period_end_unix_millis", "limit_bytes", "used_bytes", "reserved_bytes", "remaining_bytes", "active_lease_count", "revision"} {
		if !periodFields[required] {
			t.Fatalf("RelayQuotaPeriod missing %q: %v", required, periodFields)
		}
	}
	reservationFields := descriptorFieldNames((&RelayLeaseReservation{}).ProtoReflect().Descriptor())
	for _, required := range []string{"lease_id", "account_id", "managed_session_id", "client_device_id", "target_device_id", "region", "hub_id", "relay_id", "route_id", "reserved_bytes", "used_bytes", "state", "issued_at_unix_millis", "expires_at_unix_millis", "revision"} {
		if !reservationFields[required] {
			t.Fatalf("RelayLeaseReservation missing %q: %v", required, reservationFields)
		}
	}
	assertFieldsExcludeFragments(t, (&RelayLeaseReservation{}).ProtoReflect().Descriptor(), []string{"terminal", "grant", "capability", "credential", "private_key", "payload"})
	aggregateFields := descriptorFieldNames((&RelayUsageAggregate{}).ProtoReflect().Descriptor())
	for _, required := range []string{"account_id", "managed_session_id", "route_id", "period_start_unix_millis", "period_end_unix_millis", "bytes_up", "bytes_down", "active_seconds", "revision"} {
		if !aggregateFields[required] {
			t.Fatalf("RelayUsageAggregate missing %q: %v", required, aggregateFields)
		}
	}
	reserveFields := descriptorFieldNames((&ReserveRelayLeaseRequest{}).ProtoReflect().Descriptor())
	for _, required := range []string{"account_id", "managed_session_id", "client_device_id", "target_device_id", "hub_id", "relay_id", "region", "lease_id"} {
		if !reserveFields[required] {
			t.Fatalf("ReserveRelayLeaseRequest missing %q: %v", required, reserveFields)
		}
	}
	usageFields := descriptorFieldNames((&RelayUsageEvent{}).ProtoReflect().Descriptor())
	for _, required := range []string{"event_id", "lease_id", "managed_session_id", "relay_id", "path_kind", "sequence", "interval_start_unix", "interval_end_unix", "bytes_up", "bytes_down", "active_seconds", "key_id", "signature"} {
		if !usageFields[required] {
			t.Fatalf("RelayUsageEvent missing %q: %v", required, usageFields)
		}
	}
	assertFieldsExcludeFragments(t, (&RelayUsageEvent{}).ProtoReflect().Descriptor(), []string{"terminal", "grant", "credential", "private_key", "payload"})
	for _, name := range []protoreflect.Name{"RelayControlChallengeRequest", "RelayControlChallengeResponse", "RelayControlChallengeProofInput", "ReportRelayRuntimeRequest", "ReportRelayRuntimeResponse"} {
		if File_cloudpb_cloud_hub_control_proto.Messages().ByName(name) == nil {
			t.Fatalf("Relay control contract missing %s", name)
		}
	}
	resultFields := descriptorFieldNames((&RelayCommandResult{}).ProtoReflect().Descriptor())
	for _, required := range []string{"allocations", "final_usage_sequence", "usage_drain_complete", "usage_settlement_complete", "settled_usage"} {
		if !resultFields[required] {
			t.Fatalf("RelayCommandResult missing %q: %v", required, resultFields)
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

func TestAssignmentMigrationContractCarriesExactSourceAndTargetFences(t *testing.T) {
	fields := descriptorFieldNames((&AssignmentMigrationTarget{}).ProtoReflect().Descriptor())
	for _, required := range []string{"migration_id", "daemon_device_id", "source_hub_id", "source_assignment_epoch", "source_control_generation", "target_hub_id", "target_assignment_epoch", "target_not_before_unix_millis", "target_expires_at_unix_millis"} {
		if !fields[required] {
			t.Fatalf("AssignmentMigrationTarget missing %q: %v", required, fields)
		}
	}
	if ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT == ManagementCommandKind_MANAGEMENT_COMMAND_KIND_UNSPECIFIED {
		t.Fatal("assignment migration command kind is unspecified")
	}
	resultFields := descriptorFieldNames((&HubCommandResult{}).ProtoReflect().Descriptor())
	if !resultFields["control_generation"] || !resultFields["execution_control_generation"] {
		t.Fatalf("HubCommandResult does not separate transport and execution generation: %v", resultFields)
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
		"RecentAuthenticationRequest", "RecentAuthenticationResponse", "OperatorLoginRequest", "OperatorLoginResponse",
		"ListOperatorAccountsRequest", "ListOperatorAccountsResponse", "GetOperatorAccountRequest", "GetOperatorAccountResponse",
		"OperatorTransitionSubscriptionRequest", "OperatorTransitionSubscriptionResponse",
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
		"RegisterAccountRequest", "RegisterAccountResponse", "PasswordLoginRequest", "PasswordLoginResponse", "RefreshAccountSessionRequest", "RefreshAccountSessionResponse",
		"LogoutAccountSessionRequest", "LogoutAccountSessionResponse", "ChangeAccountPasswordRequest", "ChangeAccountPasswordResponse",
		"PlanPriceDefinition", "PlanPresentation", "CreateCheckoutRequest", "CreateCheckoutResponse", "PaymentAttemptProjection", "CreatePaymentAttemptRequest", "CreatePaymentAttemptResponse",
		"ApplyPaymentEventRequest", "ApplyPaymentEventResponse", "ConfirmTestPaymentRequest", "ConfirmTestPaymentResponse",
		"TransitionSubscriptionRequest", "TransitionSubscriptionResponse", "GetAccountCommerceRequest", "GetAccountCommerceResponse", "CloudProductError",
		"PaymentEventProjection",
	} {
		if File_cloudpb_cloud_product_proto.Messages().ByName(name) == nil {
			t.Fatalf("cloud product proto missing %s", name)
		}
	}
	orderFields := descriptorFieldNames((&OrderProjection{}).ProtoReflect().Descriptor())
	for _, required := range []string{"requested_transition", "source_subscription_revision", "source_plan_id", "source_plan_version", "price"} {
		if !orderFields[required] {
			t.Fatalf("OrderProjection missing %q: %v", required, orderFields)
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

func TestDaemonControlEnrollmentAndTerminalRevokeResultRoundTrip(t *testing.T) {
	response := &CompleteDeviceEnrollmentResponse{Session: &CloudSessionSummary{AccountId: "account-1", DeviceId: "daemon-1"}, ControlEnrollment: &DaemonControlEnrollment{AccountId: "account-1", DaemonDeviceId: "daemon-1", AuthEpoch: 7, VerificationKeys: []*DaemonControlVerificationKey{{KeyId: "control-1", PublicKey: bytes.Repeat([]byte{0x41}, 32), NotBeforeUnixMillis: 1, NotAfterUnixMillis: 2}}, EnrolledAtUnixMillis: 1}}
	payload, err := proto.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	decodedResponse := &CompleteDeviceEnrollmentResponse{}
	if err := proto.Unmarshal(payload, decodedResponse); err != nil || !proto.Equal(response, decodedResponse) {
		t.Fatalf("enrollment round-trip = (%v, %v)", decodedResponse, err)
	}
	serviceSession := &DeviceEnrollmentServiceSession{Session: response.GetSession(), AccessToken: []byte("private-edge-token"), HubId: "hub-1", HubDirectoryVersion: 3, ControlEnrollment: response.GetControlEnrollment()}
	payload, err = proto.Marshal(serviceSession)
	if err != nil {
		t.Fatal(err)
	}
	decodedServiceSession := &DeviceEnrollmentServiceSession{}
	if err := proto.Unmarshal(payload, decodedServiceSession); err != nil || !proto.Equal(serviceSession, decodedServiceSession) {
		t.Fatalf("private enrollment service session round-trip = (%v, %v)", decodedServiceSession, err)
	}
	result := &DaemonCommandResult{CommandId: "command-1", DaemonDeviceId: "daemon-1", AssignmentEpoch: 3, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", ResultCode: RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, OpaqueAccessReference: "opaque-1", AccessProjectionRevision: 9, ClosedSessionCount: 2, CompletedAtUnixMillis: 4}
	payload, err = proto.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	decodedResult := &DaemonCommandResult{}
	if err := proto.Unmarshal(payload, decodedResult); err != nil || !proto.Equal(result, decodedResult) {
		t.Fatalf("terminal revoke result round-trip = (%v, %v)", decodedResult, err)
	}
}

func TestDaemonEnrollmentHubProposalIsProtoFirstAndControllerValidated(t *testing.T) {
	challengeFields := descriptorFieldNames((&DeviceEnrollmentChallenge{}).ProtoReflect().Descriptor())
	completeFields := descriptorFieldNames((&CompleteDeviceEnrollmentRequest{}).ProtoReflect().Descriptor())
	for _, required := range []string{"flow_id", "challenge_id", "challenge", "expires_at_unix", "hub_candidates", "candidate_set_digest", "flow_revision"} {
		if !challengeFields[required] {
			t.Fatalf("DeviceEnrollmentChallenge missing %q: %v", required, challengeFields)
		}
	}
	for _, required := range []string{"hub_observations", "preferred_hub_id", "candidate_set_digest", "flow_revision"} {
		if !completeFields[required] {
			t.Fatalf("CompleteDeviceEnrollmentRequest missing %q: %v", required, completeFields)
		}
	}
	proofFields := descriptorFieldNames((&DeviceEnrollmentProofInput{}).ProtoReflect().Descriptor())
	for _, required := range []string{"candidate_set_digest", "preferred_hub_id", "hub_observations_digest", "flow_revision"} {
		if !proofFields[required] {
			t.Fatalf("DeviceEnrollmentProofInput missing %q: %v", required, proofFields)
		}
	}
	candidateFields := descriptorFieldNames((&HubEnrollmentCandidate{}).ProtoReflect().Descriptor())
	for _, required := range []string{"hub_id", "hub_url", "health_url", "region"} {
		if !candidateFields[required] {
			t.Fatalf("HubEnrollmentCandidate missing %q: %v", required, candidateFields)
		}
	}
	for _, forbidden := range []string{"assignment", "capacity", "maximum", "score", "weight"} {
		for field := range candidateFields {
			if strings.Contains(field, forbidden) {
				t.Fatalf("Hub candidate leaked Controller fleet field %q", field)
			}
		}
	}
	observationFields := descriptorFieldNames((&HubReachabilityObservation{}).ProtoReflect().Descriptor())
	for _, required := range []string{"hub_id", "reachable", "latency_millis"} {
		if !observationFields[required] {
			t.Fatalf("HubReachabilityObservation missing %q: %v", required, observationFields)
		}
	}
	for _, forbidden := range []string{"assignment", "epoch", "token", "audience", "selected", "score", "weight"} {
		for field := range observationFields {
			if strings.Contains(field, forbidden) {
				t.Fatalf("client Hub observation owns forbidden field %q", field)
			}
		}
	}
}

func TestMobileActivationCarriesStableClientDeviceIdentity(t *testing.T) {
	fields := descriptorFieldNames((&ClaimMobileActivationRequest{}).ProtoReflect().Descriptor())
	for _, required := range []string{"user_code", "client_metadata", "client_device_id"} {
		if !fields[required] {
			t.Fatalf("ClaimMobileActivationRequest missing %q: %v", required, fields)
		}
	}
	request := &ClaimMobileActivationRequest{UserCode: "MXA-TEST", ClientDeviceId: "client-12345678-1234-1234-1234-123456789abc", ClientMetadata: &DeviceMetadata{DisplayName: "Phone", Platform: "android", MuxviaVersion: "test"}}
	payload, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &ClaimMobileActivationRequest{}
	if err := proto.Unmarshal(payload, decoded); err != nil || !proto.Equal(request, decoded) {
		t.Fatalf("mobile activation identity round-trip = (%v, %v)", decoded, err)
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
	// protoc 按 import 拓扑输出 descriptor；schema ownership 调整后文件顺序可能变化，但契约内容不变。
	sort.Slice(baseline.File, func(left, right int) bool { return baseline.File[left].GetName() < baseline.File[right].GetName() })
	sort.Slice(current.File, func(left, right int) bool { return current.File[left].GetName() < current.File[right].GetName() })
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
