package cloudv1

import (
	"os"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestCloudV1DescriptorBaseline 锁定当前 Cloud v1 跨进程契约，后续只能做可审查的兼容扩展。
func TestCloudV1DescriptorBaseline(t *testing.T) {
	payload, err := os.ReadFile("testdata/cloud-v1.pb")
	if err != nil {
		t.Fatal(err)
	}
	baseline := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(payload, baseline); err != nil {
		t.Fatalf("decode Cloud descriptor baseline: %v", err)
	}
	current := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(File_cloud_v1_common_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_edge_config_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_usage_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_runtime_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_edge_control_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_ticket_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_enrollment_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_directory_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_client_gateway_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_agent_gateway_proto),
	}}
	if !proto.Equal(baseline, current) {
		t.Fatal("Cloud v1 descriptor differs from testdata/cloud-v1.pb")
	}
}

// TestR5GatewayContracts 锁定客户端与 daemon 都通过双向流完成信令，且 envelope 不能退化成无 generation 的 unary API。
func TestR5GatewayContracts(t *testing.T) {
	for _, service := range []protoreflect.ServiceDescriptor{
		File_cloud_v1_agent_gateway_proto.Services().ByName("AgentGateway"),
		File_cloud_v1_client_gateway_proto.Services().ByName("ClientGateway"),
	} {
		if service == nil {
			t.Fatal("R5 gateway service is missing")
		}
		method := service.Methods().ByName("Connect")
		if method == nil || !method.IsStreamingClient() || !method.IsStreamingServer() {
			t.Fatalf("%s.Connect must be bidirectional streaming", service.FullName())
		}
	}
	assertEnvelopeFields(t, (&ClientSignal{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{"hello": 20, "offer": 21})
	assertEnvelopeFields(t, (&EdgeSignal{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{"ready": 20, "answer": 21, "rejected": 22})
	assertEnvelopeFields(t, (&AgentEvent{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{"hello": 20, "heartbeat": 21, "answer": 22, "rejected": 23, "authorization": 24})
	assertEnvelopeFields(t, (&EdgeCommand{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{"ready": 20, "offer": 21, "authorize": 22})
}

// TestEdgeControlIsBidirectionalStreaming 保证 Edge 连接数不会回退为轮询或一元 RPC。
func TestEdgeControlIsBidirectionalStreaming(t *testing.T) {
	service := File_cloud_v1_edge_control_proto.Services().ByName("EdgeControl")
	if service == nil {
		t.Fatal("EdgeControl service is missing")
	}
	method := service.Methods().ByName("Connect")
	if method == nil || !method.IsStreamingClient() || !method.IsStreamingServer() {
		t.Fatalf("EdgeControl.Connect must be bidirectional streaming: %#v", method)
	}
	assertEnvelopeFields(t, (&EdgeEvent{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"hello": 20, "snapshot_begin": 21, "snapshot_chunk": 22, "snapshot_end": 23, "runtime_delta": 24, "heartbeat": 25, "config_applied": 26, "relay_lease_request": 27, "usage_batch": 28,
	})
	assertEnvelopeFields(t, (&ControllerCommand{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"welcome": 20, "snapshot_accepted": 21, "resync_required": 22, "desired_config": 23, "relay_lease_decision": 24, "usage_ack": 25,
	})
}

// TestR6RelayContracts 锁定 daemon 预检、短租约和幂等 usage 的跨进程边界。
func TestR6RelayContracts(t *testing.T) {
	lease := (&RelayLeaseClaims{}).ProtoReflect().Descriptor()
	for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{
		"lease_id": 1, "account_id": 2, "edge_id": 3, "daemon_id": 4, "client_id": 5, "session_id": 6,
		"max_bytes": 7, "max_rate_bytes_per_second": 8, "max_concurrent_allocations": 9, "issued_at": 10, "expires_at": 11,
	} {
		field := lease.Fields().ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("RelayLeaseClaims.%s field number=%v want=%d", name, field, number)
		}
	}
	if (&RelayLeaseDecision{}).ProtoReflect().Descriptor().Oneofs().ByName("result") == nil {
		t.Fatal("RelayLeaseDecision result oneof is missing")
	}
	if (&UsageEvent{}).ProtoReflect().Descriptor().Fields().ByName("event_id").Number() != 2 {
		t.Fatal("UsageEvent.event_id must remain the idempotency key at field 2")
	}
}

func assertEnvelopeFields(t *testing.T, descriptor protoreflect.MessageDescriptor, wantPayload map[protoreflect.Name]protoreflect.FieldNumber) {
	t.Helper()
	want := map[protoreflect.Name]protoreflect.FieldNumber{
		"protocol_version": 1,
		"message_id":       2,
		"sender_id":        3,
		"boot_id":          4,
		"connection_id":    5,
		"stream_seq":       6,
		"sent_at":          7,
	}
	for name, number := range want {
		field := descriptor.Fields().ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("%s.%s field number=%v want=%d", descriptor.FullName(), name, field, number)
		}
	}
	payload := descriptor.Oneofs().ByName("payload")
	if payload == nil || payload.Fields().Len() != len(wantPayload) {
		t.Fatalf("%s payload field count=%d want=%d", descriptor.FullName(), payload.Fields().Len(), len(wantPayload))
	}
	for name, number := range wantPayload {
		field := payload.Fields().ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("%s payload %s field number=%v want=%d", descriptor.FullName(), name, field, number)
		}
	}
}
