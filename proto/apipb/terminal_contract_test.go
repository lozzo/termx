package apipb

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestTerminalInputRoundTripKeepsAttachmentAndOperationFence(t *testing.T) {
	command := &CommandEnvelope{Command: &CommandEnvelope_TerminalInput{TerminalInput: &TerminalInputCommand{
		Context: &RequestContext{
			RequestId:    "input-1",
			ApiVersion:   &ApiVersion{Major: 1},
			Capabilities: []ApiCapability{ApiCapability_API_CAPABILITY_TERMINAL_ATTACHMENT},
			Session:      &EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 9},
		},
		Attachment: &ResourceHandle{Id: "attachment-1", Kind: "terminal_attachment", Generation: 3},
		Operation: &OperationStamp{
			Session: &EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 9}, OperationId: "input-op-1",
		},
		Data: []byte("echo test\r"),
	}}}
	payload, err := proto.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CommandEnvelope
	if err := proto.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	input := decoded.GetTerminalInput()
	if input.GetAttachment().GetId() != "attachment-1" || input.GetOperation().GetOperationId() != "input-op-1" || string(input.GetData()) != "echo test\r" {
		t.Fatalf("terminal input did not preserve fences: %#v", input)
	}
}

func TestTerminalApplicationContractHidesProtocolChannel(t *testing.T) {
	descriptor := (&AttachmentHandle{}).ProtoReflect().Descriptor()
	if descriptor.Fields().ByName("channel") != nil {
		t.Fatal("public attachment handle must not expose protocol channel")
	}
	assertFieldNumber(t, descriptor, "resource", 1)
	assertFieldNumber(t, descriptor, "terminal", 2)
	assertFieldNumber(t, descriptor, "operation", 3)
	assertFieldNumber(t, (&CommandEnvelope{}).ProtoReflect().Descriptor(), "terminal_attach", 29)
	assertFieldNumber(t, (&CommandEnvelope{}).ProtoReflect().Descriptor(), "terminal_input", 31)
	assertFieldNumber(t, (&CommandEnvelope{}).ProtoReflect().Descriptor(), "path_list_directories", 34)
}
