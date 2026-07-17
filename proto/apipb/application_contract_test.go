package apipb

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestApplicationEnvelopeRoundTripPreservesTypedCommandAndUnknownFields(t *testing.T) {
	command := &CommandEnvelope{Command: &CommandEnvelope_CancelOperation{CancelOperation: &CancelOperationCommand{
		Context: &RequestContext{
			RequestId:    "request-1",
			ApiVersion:   &ApiVersion{Major: 1},
			Capabilities: []ApiCapability{ApiCapability_API_CAPABILITY_TYPED_ERRORS, ApiCapability_API_CAPABILITY_OPERATION_CANCELLATION},
			Session:      &EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7},
		},
		Operation: &OperationStamp{
			Session:     &EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7},
			OperationId: "operation-1",
		},
	}}}
	payload, err := proto.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	payload = protowire.AppendTag(payload, 99, protowire.VarintType)
	payload = protowire.AppendVarint(payload, 42)

	var decoded CommandEnvelope
	if err := proto.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetCancelOperation().GetContext().GetRequestId() != "request-1" || decoded.GetCancelOperation().GetOperation().GetOperationId() != "operation-1" {
		t.Fatalf("typed command did not round trip: %#v", &decoded)
	}
	reencoded, err := proto.Marshal(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	unknown := protowire.AppendTag(nil, 99, protowire.VarintType)
	unknown = protowire.AppendVarint(unknown, 42)
	if !bytes.Contains(reencoded, unknown) {
		t.Fatalf("unknown field was not preserved: %x", reencoded)
	}
}

func TestApplicationContractFieldNumbersRemainStable(t *testing.T) {
	assertFieldNumber(t, (&RequestContext{}).ProtoReflect().Descriptor(), "request_id", 1)
	assertFieldNumber(t, (&RequestContext{}).ProtoReflect().Descriptor(), "api_version", 2)
	assertFieldNumber(t, (&RequestContext{}).ProtoReflect().Descriptor(), "capabilities", 3)
	assertFieldNumber(t, (&RequestContext{}).ProtoReflect().Descriptor(), "session", 4)
	assertFieldNumber(t, (&CommandEnvelope{}).ProtoReflect().Descriptor(), "cancel_operation", 10)
	assertFieldNumber(t, (&CommandEnvelope{}).ProtoReflect().Descriptor(), "release_resource", 11)
	assertFieldNumber(t, (&ResultEnvelope{}).ProtoReflect().Descriptor(), "acknowledge", 10)
	assertFieldNumber(t, (&ResultEnvelope{}).ProtoReflect().Descriptor(), "error", 11)
	assertFieldNumber(t, (&EventEnvelope{}).ProtoReflect().Descriptor(), "api_version", 3)
}

func assertFieldNumber(t *testing.T, descriptor protoreflect.MessageDescriptor, name protoreflect.Name, want protoreflect.FieldNumber) {
	t.Helper()
	field := descriptor.Fields().ByName(name)
	if field == nil {
		t.Fatalf("%s is missing field %s", descriptor.FullName(), name)
	}
	if field.Number() != want {
		t.Fatalf("%s.%s field number=%d want=%d", descriptor.FullName(), name, field.Number(), want)
	}
}
