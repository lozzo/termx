package platform

import (
	"context"
	"testing"
	"time"

	"github.com/muxvia/muxvia/client/binding"
	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	"github.com/muxvia/muxvia/proto/bindingpb"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestFactoryRoutesOpaquePeerAndChannelProtoWithoutOwningAuth(t *testing.T) {
	broker := binding.NewPlatformBroker()
	factory, err := NewFactory(broker)
	if err != nil {
		t.Fatal(err)
	}
	peerCh := make(chan port.ManagedPeer, 1)
	errCh := make(chan error, 1)
	go func() {
		value, err := factory.OpenManagedPeer(context.Background(), []*cloudpb.IceServer{{Urls: []string{"stun:example.test"}}}, cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, false)
		peerCh <- value
		errCh <- err
	}()
	completePlatformRequest(t, broker, func(request *bindingpb.PlatformRequest) *bindingpb.PlatformResponse {
		if request.GetWebrtcOpenPeer().GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY {
			t.Fatalf("open peer request = %#v", request.GetWebrtcOpenPeer())
		}
		return &bindingpb.PlatformResponse{Response: &bindingpb.PlatformResponse_WebrtcPeerOpened{
			WebrtcPeerOpened: &bindingpb.WebRTCPeerOpened{PeerHandle: 41, ChannelHandle: 42},
		}}
	})
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	managedPeer := <-peerCh

	offerCh := make(chan string, 1)
	go func() {
		offer, err := managedPeer.CreateOffer(context.Background())
		offerCh <- offer
		errCh <- err
	}()
	completePlatformRequest(t, broker, func(request *bindingpb.PlatformRequest) *bindingpb.PlatformResponse {
		if request.GetWebrtcCreateOffer().GetPeerHandle() != 41 {
			t.Fatalf("create offer request = %#v", request.GetWebrtcCreateOffer())
		}
		return &bindingpb.PlatformResponse{Response: &bindingpb.PlatformResponse_WebrtcOffer{
			WebrtcOffer: &bindingpb.WebRTCCreateOfferResult{OfferSdp: "v=0\r\n"},
		}}
	})
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if offer := <-offerCh; offer != "v=0\r\n" {
		t.Fatalf("offer was rewritten: %q", offer)
	}

	readyDone := make(chan error, 1)
	go func() { readyDone <- managedPeer.WaitReady(context.Background()) }()
	completePlatformRequest(t, broker, func(request *bindingpb.PlatformRequest) *bindingpb.PlatformResponse {
		return &bindingpb.PlatformResponse{Response: &bindingpb.PlatformResponse_WebrtcPeerReady{
			WebrtcPeerReady: &bindingpb.WebRTCPeerReady{RemoteCertificateFingerprint: "sha-256:aa", ObservedPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT},
		}}
	})
	if err := <-readyDone; err != nil {
		t.Fatal(err)
	}
	if fingerprint, err := managedPeer.RemoteCertificateFingerprint(); err != nil || fingerprint != "sha-256:aa" || managedPeer.ObservedPath() != endpoint.PathDirect {
		t.Fatalf("ready fingerprint=%q path=%q err=%v", fingerprint, managedPeer.ObservedPath(), err)
	}

	messages := make(chan []byte, 1)
	managedPeer.Channel().SetMessageHandler(func(payload []byte) { messages <- payload })
	eventPayload, _ := proto.Marshal(&bindingpb.PlatformEvent{Event: &bindingpb.PlatformEvent_WebrtcChannelMessage{
		WebrtcChannelMessage: &bindingpb.WebRTCChannelMessageEvent{ChannelHandle: 42, Payload: []byte("proof")},
	}})
	if err := factory.HandleEvent(eventPayload); err != nil {
		t.Fatal(err)
	}
	message := <-messages
	eventPayload[len(eventPayload)-1] ^= 0xff
	if string(message) != "proof" {
		t.Fatalf("message alias = %q", message)
	}

	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	if err := factory.Close(); err != nil {
		t.Fatal(err)
	}
	if err := factory.HandleEvent(eventPayload); err == nil {
		t.Fatal("closed generation accepted a stale browser event")
	}
}

func completePlatformRequest(t *testing.T, broker *binding.PlatformBroker, response func(*bindingpb.PlatformRequest) *bindingpb.PlatformResponse) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	payload, err := broker.NextRequest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request := &bindingpb.PlatformRequest{}
	if err := proto.Unmarshal(payload, request); err != nil {
		t.Fatal(err)
	}
	result := response(request)
	result.RequestId = request.GetRequestId()
	encoded, err := proto.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Complete(encoded); err != nil {
		t.Fatal(err)
	}
}
