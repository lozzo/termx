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
