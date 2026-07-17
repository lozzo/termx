package runtime

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/apipb"
)

func TestSessionOwnerReplacesGenerationAndRejectsStaleOperations(t *testing.T) {
	owner := NewSessionOwner()
	defer owner.Close()
	target := ownerEndpoint()
	firstDialer := &ownerDialer{}
	firstLease, err := owner.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, firstDialer)
	if err != nil {
		t.Fatal(err)
	}
	secondDialer := &ownerDialer{}
	secondLease, err := owner.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, secondDialer)
	if err != nil {
		t.Fatal(err)
	}
	if secondLease.Stamp.Generation != firstLease.Stamp.Generation+1 || !firstDialer.session.closed {
		t.Fatalf("generation first=%#v second=%#v firstClosed=%v", firstLease, secondLease, firstDialer.session.closed)
	}
	if _, err := owner.ExecuteApplication(context.Background(), firstLease.Stamp, &apipb.CommandEnvelope{}); CodeOf(err) != ErrorStaleSession {
		t.Fatalf("stale execute error = %v", err)
	}
	result, err := owner.ExecuteApplication(context.Background(), secondLease.Stamp, &apipb.CommandEnvelope{
		Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}},
	})
	if err != nil || result.GetTerminalList() == nil || secondDialer.session.calls != 1 {
		t.Fatalf("current execute result=%#v err=%v calls=%d", result, err, secondDialer.session.calls)
	}
	if err := owner.Disconnect(context.Background(), DisconnectRequest{Stamp: firstLease.Stamp}); CodeOf(err) != ErrorStaleSession {
		t.Fatalf("stale disconnect error = %v", err)
	}
	if secondDialer.session.closed {
		t.Fatal("stale disconnect closed current session")
	}
}

func TestSessionOwnerClosesLateAttemptAfterNewGeneration(t *testing.T) {
	owner := NewSessionOwner()
	defer owner.Close()
	target := ownerEndpoint()
	started := make(chan struct{})
	release := make(chan struct{})
	lateDialer := &ownerDialer{started: started, release: release}
	errCh := make(chan error, 1)
	go func() {
		_, err := owner.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, lateDialer)
		errCh <- err
	}()
	<-started
	winner := &ownerDialer{}
	if _, err := owner.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, winner); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-errCh; CodeOf(err) != ErrorStaleSession {
		t.Fatalf("late attempt error = %v", err)
	}
	if lateDialer.session == nil || !lateDialer.session.closed || winner.session.closed {
		t.Fatalf("lateClosed=%v winnerClosed=%v", lateDialer.session != nil && lateDialer.session.closed, winner.session.closed)
	}
}

func ownerEndpoint() endpoint.Endpoint {
	identity := endpoint.DaemonIdentity{DeviceID: "device-1", DeviceFingerprint: "fingerprint-1"}
	return endpoint.Endpoint{
		ID: "studio", DaemonIdentity: identity,
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			"cloud": {
				ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true, Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser,
				TargetDeviceID: identity.DeviceID, AccountProfile: "default", RelayMode: endpoint.RelayDirect,
			},
		},
	}
}

type ownerDialer struct {
	started chan struct{}
	release chan struct{}
	session *ownerSession
}

func (dialer *ownerDialer) Dial(_ context.Context, request AttemptRequest) (ReadySession, error) {
	if dialer.started != nil {
		close(dialer.started)
	}
	if dialer.release != nil {
		<-dialer.release
	}
	dialer.session = newOwnerSession(request.Stamp())
	return dialer.session, nil
}

type ownerSession struct {
	stamp     EndpointSessionStamp
	done      chan struct{}
	closeOnce sync.Once
	closed    bool
	calls     int
}

func newOwnerSession(stamp EndpointSessionStamp) *ownerSession {
	return &ownerSession{stamp: stamp, done: make(chan struct{})}
}

func (session *ownerSession) Stamp() EndpointSessionStamp { return session.stamp }
func (session *ownerSession) ObservedPath() string        { return "direct" }
func (session *ownerSession) Done() <-chan struct{}       { return session.done }
func (session *ownerSession) Err() error {
	if session.closed {
		return io.EOF
	}
	return nil
}
func (session *ownerSession) Close() error {
	session.closeOnce.Do(func() {
		session.closed = true
		close(session.done)
	})
	return nil
}
func (session *ownerSession) ExecuteApplication(_ context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	session.calls++
	return &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_TerminalList{TerminalList: &apipb.TerminalListResult{}}}, nil
}
func (session *ownerSession) ApplicationEvents(context.Context) (<-chan *apipb.EventEnvelope, error) {
	return make(chan *apipb.EventEnvelope), nil
}

var _ ApplicationReadySession = (*ownerSession)(nil)
