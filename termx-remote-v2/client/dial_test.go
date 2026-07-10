package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	remotev2webrtc "github.com/lozzow/termx/termx-remote-v2/webrtc"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport"
	"github.com/lozzow/termx/termx-shared/transport/datachannel"
)

func TestDialUsesCompanionSignalingWithoutSendingCapabilityGrant(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	now := time.Now().UTC()
	grant, err := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", DeviceID: "device-1", Scope: remoteauth.Scope{AllowDaemon: true},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	handler := &blockingAuthorizedHandler{called: make(chan struct{})}
	answerer := remotev2webrtc.Answerer{Handler: handler}
	companion := &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			return &cloudpb.ResolvedEndpoint{EndpointId: "lab", TargetDeviceId: "device-1", ManagedSessionId: "managed-1"}, nil
		},
		CreateSignalingSessionFunc: func(ctx context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
			answer, answerErr := answerer.Answer(ctx, &cloudpb.SignalingOffer{
				SignalingSessionId: "signal-1", ManagedSessionId: request.GetManagedSessionId(), Sdp: request.GetOfferSdp(),
			}, nil)
			if answerErr != nil {
				return nil, answerErr
			}
			stream := cloudcompanion.NewFakeSignalingStream(1)
			if err := stream.Push(&cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: answer}}); err != nil {
				return nil, err
			}
			return stream, nil
		},
	}
	authenticator := &recordingAuthenticator{}
	connection, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: remoteauth.Fingerprint(publicKey), CapabilityGrant: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		Authenticator:   authenticator,
		Now:             now,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer connection.Close()
	select {
	case <-handler.called:
	case <-time.After(10 * time.Second):
		t.Fatal("daemon authorized channel handler was not invoked")
	}
	if authenticator.calls != 1 || authenticator.authentication.CapabilityGrant != grant {
		t.Fatalf("authenticator = %+v", authenticator)
	}
	recorded := companion.Requests()
	if len(recorded.ResolveEndpoint) != 1 || len(recorded.CreateSignalingSession) != 1 {
		t.Fatalf("recorded companion requests = %+v", recorded)
	}
	signaling := recorded.CreateSignalingSession[0]
	if signaling.GetEndpointId() != "lab" || signaling.GetTargetDeviceId() != "device-1" || signaling.GetOfferSdp() == "" {
		t.Fatalf("signaling request = %+v", signaling)
	}
}

func TestDialRejectsGrantDeviceMismatchBeforeCompanionRequest(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	grant, _ := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", DeviceID: "device-1", Scope: remoteauth.Scope{AllowDaemon: true},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	companion := &cloudcompanion.FakeClient{}
	if _, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-2",
		DeviceFingerprint: remoteauth.Fingerprint(publicKey), CapabilityGrant: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		Authenticator:   &recordingAuthenticator{},
		Now:             now,
	}); err == nil {
		t.Fatal("grant device mismatch must fail before Companion request")
	}
	if requests := companion.Requests(); len(requests.ResolveEndpoint) != 0 {
		t.Fatalf("Companion saw request before local grant validation: %+v", requests)
	}
}

func TestDialFailsClosedWithoutDataChannelAuthenticator(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	grant, _ := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-1", DeviceID: "device-1", Scope: remoteauth.Scope{AllowDaemon: true},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	companion := &cloudcompanion.FakeClient{}
	_, err := Dial(context.Background(), DialOptions{
		Companion: companion, EndpointID: "lab", TargetDeviceID: "device-1",
		DeviceFingerprint: remoteauth.Fingerprint(publicKey), CapabilityGrant: grant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, Now: now,
	})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("Dial error = %v, want fail-closed PROTOCOL error", err)
	}
	if requests := companion.Requests(); len(requests.ResolveEndpoint) != 0 {
		t.Fatalf("Companion saw request without authenticator: %+v", requests)
	}
}

type recordingAuthenticator struct {
	calls          int
	authentication SessionAuthentication
}

func (authenticator *recordingAuthenticator) Authenticate(_ context.Context, _ transport.Transport, authentication SessionAuthentication) error {
	authenticator.calls++
	authenticator.authentication = authentication
	return nil
}

type blockingAuthorizedHandler struct {
	called chan struct{}
}

func (handler *blockingAuthorizedHandler) ServeDataChannel(ctx context.Context, _ datachannel.Channel) error {
	close(handler.called)
	<-ctx.Done()
	return ctx.Err()
}
