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
