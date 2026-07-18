package binding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/bindingpb"
	"google.golang.org/protobuf/proto"
)

func TestPlatformBrokerCorrelatesProtoResponsesAndRejectsLateCompletion(t *testing.T) {
	broker := NewPlatformBroker()
	defer broker.Close()
	result := make(chan *bindingpb.PlatformResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		response, err := broker.Exchange(context.Background(), &bindingpb.PlatformRequest{
			Request: &bindingpb.PlatformRequest_CredentialSign{CredentialSign: &bindingpb.CredentialSignRequest{
				CredentialRef: "credential:studio", Payload: []byte("proof"),
			}},
		})
		result <- response
		errCh <- err
	}()
	payload, err := broker.NextRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	request := &bindingpb.PlatformRequest{}
	if err := proto.Unmarshal(payload, request); err != nil {
		t.Fatal(err)
	}
	if request.GetRequestId() == 0 || string(request.GetCredentialSign().GetPayload()) != "proof" {
		t.Fatalf("platform request = %#v", request)
	}
	responsePayload, _ := proto.Marshal(&bindingpb.PlatformResponse{
		RequestId: request.GetRequestId(),
		Response:  &bindingpb.PlatformResponse_CredentialSign{CredentialSign: &bindingpb.CredentialSignResponse{Signature: []byte("signature")}},
	})
	if err := broker.Complete(responsePayload); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if response := <-result; string(response.GetCredentialSign().GetSignature()) != "signature" {
		t.Fatalf("platform response = %#v", response)
	}
	if err := broker.Complete(responsePayload); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("duplicate completion error = %v", err)
	}
}

func TestPlatformBrokerCancellationAndCloseUnblockBothSides(t *testing.T) {
	broker := NewPlatformBroker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := broker.Exchange(ctx, &bindingpb.PlatformRequest{
		Request: &bindingpb.PlatformRequest_CredentialDelete{CredentialDelete: &bindingpb.CredentialDeleteRequest{CredentialRef: "credential:studio"}},
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled exchange error = %v", err)
	}
	nextErr := make(chan error, 1)
	go func() {
		_, err := broker.NextRequest(context.Background())
		nextErr <- err
	}()
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-nextErr:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("next request close error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("broker close did not unblock NextRequest")
	}
}

func TestEnginePairingAndCredentialOperationsUseGenericEvents(t *testing.T) {
	host := &extendedBindingHost{bindingHost: bindingHost{session: newBindingSession()}}
	engine, err := NewEngine(host)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	importPayload, _ := proto.Marshal(&bindingpb.ImportPairingRequest{RequestId: "pair-1", PortablePayload: "termx://bootstrap?payload=proof"})
	importOperation, err := engine.ImportPairing(importPayload)
	if err != nil {
		t.Fatal(err)
	}
	importEvent := nextBindingEvent(t, engine)
	if importEvent.GetAbiVersion() != ABIVersion || importEvent.GetImportPairing().GetOperationHandle() != importOperation || importEvent.GetImportPairing().GetEndpointId() != "studio" {
		t.Fatalf("pairing event = %#v", importEvent)
	}
	if err := engine.Release(importOperation); err != nil {
		t.Fatal(err)
	}
	deletePayload, _ := proto.Marshal(&bindingpb.DeleteCredentialRequest{RequestId: "delete-1", CredentialRef: "credential:studio"})
	deleteOperation, err := engine.DeleteCredential(deletePayload)
	if err != nil {
		t.Fatal(err)
	}
	deleteEvent := nextBindingEvent(t, engine)
	if deleteEvent.GetDeleteCredential().GetOperationHandle() != deleteOperation || deleteEvent.GetDeleteCredential().GetError() != nil || host.deleted != "credential:studio" {
		t.Fatalf("delete event = %#v deleted=%q", deleteEvent, host.deleted)
	}
}

type extendedBindingHost struct {
	bindingHost
	deleted string
}

func (host *extendedBindingHost) ImportPairing(_ context.Context, request *bindingpb.ImportPairingRequest) (*bindingpb.ImportPairingResult, error) {
	return &bindingpb.ImportPairingResult{EndpointId: "studio", CredentialRef: "credential:studio", AuthorizationRequired: true}, nil
}

func (host *extendedBindingHost) DeleteCredential(_ context.Context, request *bindingpb.DeleteCredentialRequest) error {
	host.deleted = request.GetCredentialRef()
	return nil
}

var _ PairingHost = (*extendedBindingHost)(nil)
var _ CredentialHost = (*extendedBindingHost)(nil)
