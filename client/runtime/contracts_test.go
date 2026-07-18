package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/client/endpoint"
)

func TestWasAttemptedDefaultsToNoReplayForUnknownErrors(t *testing.T) {
	if WasAttempted(nil) {
		t.Fatal("nil error must not be attempted")
	}
	if !WasAttempted(errors.New("adapter failed")) {
		t.Fatal("unknown error must conservatively prevent replay")
	}
	if WasAttempted(&Error{Code: ErrorStaleSession, Message: "stale", Attempted: false}) {
		t.Fatal("generation guard failure must remain unattempted")
	}
	if !WasAttempted(&Error{Code: ErrorUnavailable, Message: "write failed", Attempted: true}) {
		t.Fatal("adapter write failure must remain attempted")
	}
}

func TestAttemptRequestBindsOneRouteAndGeneration(t *testing.T) {
	target := endpoint.NewSSHEndpoint("studio", "Studio", "studio.example", "ssh:studio", "127.0.0.1:41120", "127.0.0.1:41121", endpoint.ConnectOnDemand)
	request, err := NewAttemptRequest(target, "ssh", 7, ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if stamp := request.Stamp(); stamp.EndpointID != "studio" || stamp.RouteID != "ssh" || stamp.Generation != 7 {
		t.Fatalf("attempt stamp = %#v", stamp)
	}
	if request.Route().Kind != endpoint.RouteSSHWebRTCTCP || request.Intent() != ConnectIntentInteractive {
		t.Fatalf("attempt contract = route %#v intent %q", request.Route(), request.Intent())
	}
}

func TestAttemptRequestDoesNotExposeMutableRegistryRoute(t *testing.T) {
	target := endpoint.NewSSHEndpoint("studio", "Studio", "studio.example", "ssh:studio", "127.0.0.1:41120", "127.0.0.1:41121", endpoint.ConnectOnDemand)
	request, err := NewAttemptRequest(target, "ssh", 3, ConnectIntentBackground)
	if err != nil {
		t.Fatal(err)
	}
	route := target.Routes["ssh"]
	route.Host = "changed.example"
	target.Routes["ssh"] = route
	exposed := request.Route()
	exposed.Host = "consumer-change.example"
	if got := request.Route().Host; got != "studio.example" {
		t.Fatalf("attempt route mutated through registry or getter: %q", got)
	}
}

func TestAttemptRequestRejectsUnknownRouteAndGeneration(t *testing.T) {
	target := endpoint.NewLocalEndpoint(endpoint.DefaultEndpointID, "Local", "auto", endpoint.ConnectAuto)
	if _, err := NewAttemptRequest(target, "missing", 1, ConnectIntentInteractive); CodeOf(err) != ErrorInvalidRequest {
		t.Fatalf("missing route error = %v code=%q", err, CodeOf(err))
	}
	if _, err := NewAttemptRequest(target, "local", 0, ConnectIntentInteractive); CodeOf(err) != ErrorInvalidRequest {
		t.Fatalf("missing generation error = %v code=%q", err, CodeOf(err))
	}
}

func TestApplicationContractsRejectIncompleteStamp(t *testing.T) {
	for name, validate := range map[string]func() error{
		"lease":      func() error { return (SessionLease{}).Validate() },
		"disconnect": func() error { return (DisconnectRequest{}).Validate() },
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(); CodeOf(err) != ErrorInvalidRequest {
				t.Fatalf("error = %v code=%q", err, CodeOf(err))
			}
		})
	}
}

func TestCodeOfPreservesCancellationAndStableRuntimeCode(t *testing.T) {
	if got := CodeOf(context.Canceled); got != ErrorCanceled {
		t.Fatalf("canceled code = %q", got)
	}
	cause := errors.New("host unavailable")
	err := runtimeError(ErrorAuthorization, "authorization failed", cause)
	if got := CodeOf(err); got != ErrorAuthorization || !errors.Is(err, cause) {
		t.Fatalf("runtime error = %v code=%q", err, got)
	}
}

func TestValidateReadySessionRejectsWrongGenerationAndMissingLifecycle(t *testing.T) {
	target := endpoint.NewLocalEndpoint(endpoint.DefaultEndpointID, "Local", "auto", endpoint.ConnectAuto)
	request, err := NewAttemptRequest(target, "local", 4, ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	var nilSession *contractReadySession
	if err := ValidateReadySession(request, nilSession); CodeOf(err) != ErrorUnavailable {
		t.Fatalf("nil session error = %v code=%q", err, CodeOf(err))
	}
	readyEvidence := ReadySessionEvidence{Identity: endpoint.DaemonIdentity{DeviceID: "device-ready", DeviceFingerprint: "SHA256:device-ready"}, IdentityVerified: true, AuthorizationVerified: true, ProtocolVersion: 1}
	wrong := &contractReadySession{stamp: EndpointSessionStamp{EndpointID: endpoint.DefaultEndpointID, RouteID: "local", Generation: 5}, evidence: readyEvidence, done: make(chan struct{})}
	if err := ValidateReadySession(request, wrong); CodeOf(err) != ErrorStaleSession {
		t.Fatalf("wrong generation error = %v code=%q", err, CodeOf(err))
	}
	missingLifecycle := &contractReadySession{stamp: request.Stamp(), evidence: readyEvidence}
	if err := ValidateReadySession(request, missingLifecycle); CodeOf(err) != ErrorUnavailable {
		t.Fatalf("missing lifecycle error = %v code=%q", err, CodeOf(err))
	}
	ready := &contractReadySession{stamp: request.Stamp(), evidence: readyEvidence, done: make(chan struct{})}
	if err := ValidateReadySession(request, ready); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	close(closed)
	if err := ValidateReadySession(request, &contractReadySession{stamp: request.Stamp(), evidence: readyEvidence, done: closed}); CodeOf(err) != ErrorUnavailable {
		t.Fatalf("ended ready session error = %v", err)
	}
}

func TestReadySessionEvidenceRequiresProofAuthorizationHelloAndPin(t *testing.T) {
	expected := endpoint.DaemonIdentity{DeviceID: "device-1", DeviceFingerprint: "SHA256:device-1"}
	tests := []struct {
		name     string
		evidence ReadySessionEvidence
		code     ErrorCode
	}{
		{name: "identity", evidence: ReadySessionEvidence{AuthorizationVerified: true, ProtocolVersion: 1}, code: ErrorIdentity},
		{name: "authorization", evidence: ReadySessionEvidence{Identity: expected, IdentityVerified: true, ProtocolVersion: 1}, code: ErrorAuthorization},
		{name: "hello", evidence: ReadySessionEvidence{Identity: expected, IdentityVerified: true, AuthorizationVerified: true}, code: ErrorUnavailable},
		{name: "pin", evidence: ReadySessionEvidence{Identity: endpoint.DaemonIdentity{DeviceID: "other", DeviceFingerprint: "SHA256:other"}, IdentityVerified: true, AuthorizationVerified: true, ProtocolVersion: 1}, code: ErrorIdentity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.evidence.Validate(expected); CodeOf(err) != test.code {
				t.Fatalf("evidence error = %v code=%q, want %q", err, CodeOf(err), test.code)
			}
		})
	}
}

type contractReadySession struct {
	stamp    EndpointSessionStamp
	evidence ReadySessionEvidence
	done     chan struct{}
}

func (session *contractReadySession) Stamp() EndpointSessionStamp { return session.stamp }
func (session *contractReadySession) ObservedPath() string        { return "" }
func (session *contractReadySession) Readiness() ReadySessionEvidence {
	return session.evidence
}
func (session *contractReadySession) Done() <-chan struct{} { return session.done }
func (session *contractReadySession) Err() error            { return nil }
func (session *contractReadySession) Close() error          { return nil }

var _ ReadySession = (*contractReadySession)(nil)

type contractRuntime struct{}

func (contractRuntime) EnsureSession(context.Context, ConnectRequest) (SessionLease, error) {
	return SessionLease{}, nil
}

func (contractRuntime) Disconnect(context.Context, DisconnectRequest) error {
	return nil
}

func (contractRuntime) WatchEndpoint(context.Context, endpoint.EndpointID) (<-chan EndpointEvent, error) {
	return make(chan EndpointEvent), nil
}

var _ Runtime = contractRuntime{}
