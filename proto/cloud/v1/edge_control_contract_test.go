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
		protodesc.ToFileDescriptorProto(File_cloud_v1_runtime_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_edge_control_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_ticket_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_enrollment_proto),
		protodesc.ToFileDescriptorProto(File_cloud_v1_agent_gateway_proto),
	}}
	if !proto.Equal(baseline, current) {
		t.Fatal("Cloud v1 descriptor differs from testdata/cloud-v1.pb")
	}
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
		"hello": 20, "snapshot_begin": 21, "snapshot_chunk": 22, "snapshot_end": 23, "runtime_delta": 24, "heartbeat": 25, "config_applied": 26,
	})
	assertEnvelopeFields(t, (&ControllerCommand{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"welcome": 20, "snapshot_accepted": 21, "resync_required": 22, "desired_config": 23,
	})
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
