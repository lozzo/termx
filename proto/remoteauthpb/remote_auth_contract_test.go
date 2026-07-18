package remoteauthpb

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestRawGrantOnlyCrossesEndToEndCapabilityOrPairingFrames(t *testing.T) {
	messages := File_remoteauthpb_remote_auth_proto.Messages()
	grantFields := make([]string, 0, 1)
	for messageIndex := 0; messageIndex < messages.Len(); messageIndex++ {
		message := messages.Get(messageIndex)
		for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
			field := message.Fields().Get(fieldIndex)
			if strings.EqualFold(string(field.Name()), "grant") {
				grantFields = append(grantFields, string(message.FullName())+"."+string(field.Name()))
			}
		}
	}
	want := []string{"termx.remote.auth.v1.CapabilityOpen.grant", "termx.remote.auth.v1.PairingAccepted.grant"}
	if len(grantFields) != len(want) || grantFields[0] != want[0] || grantFields[1] != want[1] {
		t.Fatalf("raw grant fields = %v, want %v", grantFields, want)
	}
}

func TestRemoteAuthContractExcludesCloudAndPrivateCredentials(t *testing.T) {
	forbidden := []string{"private_key", "session_token", "account_token", "admission_ticket", "relay_lease"}
	messages := File_remoteauthpb_remote_auth_proto.Messages()
	for messageIndex := 0; messageIndex < messages.Len(); messageIndex++ {
		assertRemoteAuthMessageAllowed(t, messages.Get(messageIndex), forbidden)
	}
}

func TestAuthEnvelopeHasSinglePayloadOneof(t *testing.T) {
	descriptor := (&AuthEnvelope{}).ProtoReflect().Descriptor()
	if descriptor.Oneofs().Len() != 1 || descriptor.Oneofs().Get(0).Name() != "payload" {
		t.Fatalf("AuthEnvelope oneofs = %v, want one payload", descriptor.Oneofs())
	}
	if descriptor.Oneofs().Get(0).Fields().Len() != 6 {
		t.Fatalf("AuthEnvelope payload variants = %d, want 6", descriptor.Oneofs().Get(0).Fields().Len())
	}
}

func TestEndpointRouteConfigV1OwnsVersionedRouteOneof(t *testing.T) {
	descriptor := (&EndpointRouteConfigV1{}).ProtoReflect().Descriptor()
	if field := descriptor.Fields().ByName("schema_version"); field == nil || field.Number() != 1 {
		t.Fatal("EndpointRouteConfigV1.schema_version must remain field 1")
	}
	if descriptor.Oneofs().Len() != 2 {
		// proto3 optional priority is represented as a synthetic oneof in reflection.
		t.Fatalf("EndpointRouteConfigV1 oneofs = %d, want route plus optional priority", descriptor.Oneofs().Len())
	}
	routeOneof := descriptor.Oneofs().ByName("route")
	if routeOneof == nil || routeOneof.Fields().Len() != 4 {
		t.Fatalf("EndpointRouteConfigV1.route variants = %v", routeOneof)
	}
	want := map[protoreflect.Name]protoreflect.FieldNumber{
		"local_unix": 20, "direct_webrtc_tcp": 21, "ssh_webrtc_tcp": 22, "managed_webrtc": 23,
	}
	for name, number := range want {
		field := routeOneof.Fields().ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("EndpointRouteConfigV1.%s field = %v, want number %d", name, field, number)
		}
	}
	if descriptor.Fields().ByName("kind") != nil || descriptor.Fields().ByName("addresses") != nil || descriptor.Fields().ByName("remote_socket") != nil {
		t.Fatal("EndpointRouteConfigV1 must not restore the old flat route schema")
	}
}

func TestEndpointConfigAndRegistryAreVersionedProtoContracts(t *testing.T) {
	for _, message := range []protoreflect.MessageDescriptor{
		(&EndpointConfigV1{}).ProtoReflect().Descriptor(),
		(&EndpointRegistryV1{}).ProtoReflect().Descriptor(),
	} {
		field := message.Fields().ByName("schema_version")
		if field == nil || field.Number() != 1 {
			t.Fatalf("%s schema_version must remain field 1", message.FullName())
		}
	}
}

func TestDirectSignalingUsesVersionedProtoAndSignedAnswer(t *testing.T) {
	request := (&DirectSignalingRequestV1{}).ProtoReflect().Descriptor()
	answer := (&DirectSignalingAnswerV1{}).ProtoReflect().Descriptor()
	response := (&DirectSignalingResponseV1{}).ProtoReflect().Descriptor()
	if request.Fields().ByName("schema_version").Number() != 1 || answer.Fields().ByName("schema_version").Number() != 1 {
		t.Fatal("Direct signaling request/answer must retain schema_version field 1")
	}
	if answer.Fields().ByName("identity") == nil || answer.Fields().ByName("signature") == nil {
		t.Fatal("Direct signaling answer must carry daemon identity and signature")
	}
	payload := response.Oneofs().ByName("payload")
	if payload == nil || payload.Fields().Len() != 2 || payload.Fields().ByName("answer") == nil || payload.Fields().ByName("error") == nil {
		t.Fatal("Direct signaling response must be an answer/error oneof")
	}
}

func assertRemoteAuthMessageAllowed(t *testing.T, message protoreflect.MessageDescriptor, forbidden []string) {
	t.Helper()
	for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
		field := message.Fields().Get(fieldIndex)
		name := strings.ToLower(string(field.Name()))
		for _, fragment := range forbidden {
			if strings.Contains(name, fragment) {
				t.Fatalf("remote auth field %s.%s contains forbidden fragment %q", message.FullName(), field.Name(), fragment)
			}
		}
	}
}
