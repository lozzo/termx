package runtime

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestSessionOwnerBeginRouteAttemptInvalidatesCurrentSession(t *testing.T) {
	owner := NewSessionOwner()
	defer owner.Close()
	target := ownerEndpoint()
	dialer := &ownerDialer{}
	lease, err := owner.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, dialer)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := owner.BeginRouteAttempt(target, "cloud", ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Stamp().Generation != lease.Stamp.Generation+1 || !dialer.session.closed {
		t.Fatalf("lease=%#v attempt=%#v closed=%v", lease, attempt, dialer.session.closed)
	}
	if _, err := owner.ApplicationSession(lease); CodeOf(err) != ErrorStaleSession {
		t.Fatalf("stale application session error = %v", err)
	}
}

func TestSessionOwnerInvalidAttemptDoesNotInvalidateCurrentSession(t *testing.T) {
	owner := NewSessionOwner()
	defer owner.Close()
	target := ownerEndpoint()
	dialer := &ownerDialer{}
	lease, err := owner.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, dialer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.BeginRouteAttempt(target, "missing", ConnectIntentInteractive); err == nil {
		t.Fatal("invalid route attempt succeeded")
	}
	if dialer.session.closed {
		t.Fatal("invalid route attempt closed current session")
	}
	if _, err := owner.ApplicationSession(lease); err != nil {
		t.Fatalf("current lease became stale: %v", err)
	}
}

func TestSessionGenerationAuthoritySurvivesOwnerReplacement(t *testing.T) {
	authority := NewSessionGenerationAuthority()
	target := ownerEndpoint()
	first := NewSessionOwnerWithAuthority(authority)
	firstLease, err := first.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, &ownerDialer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := NewSessionOwnerWithAuthority(authority)
	defer second.Close()
	secondLease, err := second.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, &ownerDialer{})
	if err != nil {
		t.Fatal(err)
	}
	if secondLease.Stamp.Generation != firstLease.Stamp.Generation+1 {
		t.Fatalf("first=%#v second=%#v", firstLease, secondLease)
	}
}

func TestSessionGenerationAuthorityRevokesSessionOwnedByAnotherHost(t *testing.T) {
	authority := NewSessionGenerationAuthority()
	target := ownerEndpoint()
	first := NewSessionOwnerWithAuthority(authority)
	defer first.Close()
	firstDialer := &ownerDialer{}
	firstLease, err := first.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, firstDialer)
	if err != nil {
		t.Fatal(err)
	}
	second := NewSessionOwnerWithAuthority(authority)
	defer second.Close()
	secondDialer := &ownerDialer{}
	if _, err := second.ConnectRoute(context.Background(), target, "cloud", ConnectIntentInteractive, secondDialer); err != nil {
		t.Fatal(err)
	}
	if !firstDialer.session.closed {
		t.Fatal("new host generation did not close previous host session")
	}
	if _, err := first.ExecuteApplication(context.Background(), firstLease.Stamp, &apipb.CommandEnvelope{}); CodeOf(err) != ErrorStaleSession {
		t.Fatalf("previous host execute error = %v", err)
	}
}

func TestSessionOwnerAcquireRouteSharesConsumerLeases(t *testing.T) {
	owner := NewSessionOwner()
	defer owner.Close()
	target := ownerEndpoint()
	firstDialer := &ownerDialer{}
	first, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-a", firstDialer)
	if err != nil {
		t.Fatal(err)
	}
	reuseDialer := &ownerDialer{}
	second, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-a", reuseDialer)
	if err != nil {
		t.Fatal(err)
	}
	third, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-a", reuseDialer)
	if err != nil {
		t.Fatal(err)
	}
	if first.Stamp() != second.Stamp() || second.Stamp() != third.Stamp() || firstDialer.calls != 1 || reuseDialer.calls != 0 {
		t.Fatalf("first=%#v second=%#v third=%#v dialCalls=%d/%d", first.Stamp(), second.Stamp(), third.Stamp(), firstDialer.calls, reuseDialer.calls)
	}
	firstDialer.session.eventCancelled = make(chan struct{}, 2)
	if _, err := first.ApplicationEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := second.ApplicationEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstDialer.session.eventCancelled:
	case <-time.After(time.Second):
		t.Fatal("released lease did not cancel its event subscription")
	}
	select {
	case <-firstDialer.session.eventCancelled:
		t.Fatal("releasing one lease cancelled another event subscription")
	case <-time.After(20 * time.Millisecond):
	}
	if firstDialer.session.closed {
		t.Fatal("consumer lease closed shared ready session")
	}
	if _, err := second.ExecuteApplication(context.Background(), &apipb.CommandEnvelope{}); err != nil {
		t.Fatal(err)
	}
	provider := third.(ResourceStreamSession)
	stream, err := provider.OpenResourceStream(&apipb.ResourceHandle{})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	_ = second.Close()
	select {
	case <-firstDialer.session.eventCancelled:
	case <-time.After(time.Second):
		t.Fatal("second lease did not cancel its event subscription")
	}
	_ = third.Close()
	if firstDialer.session.closed {
		t.Fatal("final consumer release closed cached ready session")
	}
}

func TestSessionOwnerAcquireRouteDoesNotReuseEndedOrFailedReplacement(t *testing.T) {
	owner := NewSessionOwner()
	defer owner.Close()
	target := ownerEndpoint()
	firstDialer := &ownerDialer{}
	first, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-a", firstDialer)
	if err != nil {
		t.Fatal(err)
	}
	_ = firstDialer.session.Close()
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("ended ready session did not close consumer lease")
	}
	secondDialer := &ownerDialer{}
	second, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-a", secondDialer)
	if err != nil {
		t.Fatal(err)
	}
	if second.Stamp().Generation <= first.Stamp().Generation {
		t.Fatalf("ended session generation was reused: first=%#v second=%#v", first.Stamp(), second.Stamp())
	}
	if _, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-b", &ownerDialer{err: io.ErrUnexpectedEOF}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("replacement error = %v", err)
	}
	thirdDialer := &ownerDialer{}
	third, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-a", thirdDialer)
	if err != nil {
		t.Fatal(err)
	}
	if thirdDialer.calls != 1 || third.Stamp().Generation <= second.Stamp().Generation {
		t.Fatalf("failed replacement reused stale session: second=%#v third=%#v calls=%d", second.Stamp(), third.Stamp(), thirdDialer.calls)
	}
	_ = second.Close()
	_ = third.Close()
}

func TestSessionOwnerDelayedOldDoneDoesNotCloseNewGenerationLease(t *testing.T) {
	owner := NewSessionOwner()
	defer owner.Close()
	target := ownerEndpoint()
	firstDialer := &ownerDialer{delayDone: true}
	first, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-a", firstDialer)
	if err != nil {
		t.Fatal(err)
	}
	secondDialer := &ownerDialer{}
	second, err := owner.AcquireRoute(context.Background(), target, "cloud", ConnectIntentInteractive, "config-b", secondDialer)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.(*sharedApplicationLease).active(); err == nil {
		t.Fatal("replaced old consumer lease remained active")
	}
	firstDialer.session.finishDone()
	select {
	case <-firstDialer.session.removeObserved:
	case <-time.After(time.Second):
		t.Fatal("old session Done cleanup was not observed")
	}
	if _, err := second.ExecuteApplication(context.Background(), &apipb.CommandEnvelope{
		Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}},
	}); err != nil {
		t.Fatalf("old Done closed new generation lease: %v", err)
	}
	select {
	case <-second.Done():
		t.Fatalf("new generation lease finished after old Done: %v", second.Err())
	default:
	}
	_ = second.Close()
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
	started   chan struct{}
	release   chan struct{}
	session   *ownerSession
	calls     int
	err       error
	delayDone bool
}

func (dialer *ownerDialer) Dial(_ context.Context, request AttemptRequest) (ReadySession, error) {
	dialer.calls++
	if dialer.err != nil {
		return nil, dialer.err
	}
	if dialer.started != nil {
		close(dialer.started)
	}
	if dialer.release != nil {
		<-dialer.release
	}
	dialer.session = newOwnerSession(request.Stamp())
	identity := request.DaemonIdentity()
	if identity.Empty() {
		identity = endpoint.DaemonIdentity{DeviceID: "device-owner-test", DeviceFingerprint: "SHA256:device-owner-test"}
	}
	dialer.session.evidence = ReadySessionEvidence{
		Identity: identity, IdentityVerified: true, AuthorizationVerified: true, ProtocolVersion: 1,
	}
	dialer.session.delayDone = dialer.delayDone
	return dialer.session, nil
}

type ownerSession struct {
	stamp          EndpointSessionStamp
	evidence       ReadySessionEvidence
	done           chan struct{}
	closeOnce      sync.Once
	closed         bool
	calls          int
	eventCancelled chan struct{}
	delayDone      bool
	removeObserved chan struct{}
	removeOnce     sync.Once
	closeCalls     atomic.Int32
}

func newOwnerSession(stamp EndpointSessionStamp) *ownerSession {
	return &ownerSession{stamp: stamp, done: make(chan struct{}), removeObserved: make(chan struct{})}
}

func (session *ownerSession) Stamp() EndpointSessionStamp     { return session.stamp }
func (session *ownerSession) ObservedPath() string            { return "direct" }
func (session *ownerSession) Readiness() ReadySessionEvidence { return session.evidence }
func (session *ownerSession) Done() <-chan struct{}           { return session.done }
func (session *ownerSession) Err() error {
	if session.closed {
		return io.EOF
	}
	return nil
}
func (session *ownerSession) Close() error {
	if session.closeCalls.Add(1) > 1 {
		session.removeOnce.Do(func() { close(session.removeObserved) })
	}
	session.closeOnce.Do(func() {
		session.closed = true
		if !session.delayDone {
			close(session.done)
		}
	})
	return nil
}
func (session *ownerSession) finishDone() {
	session.delayDone = false
	select {
	case <-session.done:
	default:
		close(session.done)
	}
}
func (session *ownerSession) ExecuteApplication(_ context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	session.calls++
	return &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_TerminalList{TerminalList: &apipb.TerminalListResult{}}}, nil
}
func (session *ownerSession) ApplicationEvents(ctx context.Context) (<-chan *apipb.EventEnvelope, error) {
	out := make(chan *apipb.EventEnvelope)
	if session.eventCancelled != nil {
		go func() {
			<-ctx.Done()
			session.eventCancelled <- struct{}{}
			close(out)
		}()
	}
	return out, nil
}
func (session *ownerSession) OpenResourceStream(*apipb.ResourceHandle) (ResourceStream, error) {
	return ownerResourceStream{}, nil
}

type ownerResourceStream struct{}

func (ownerResourceStream) Receive(context.Context) (uint8, []byte, error) { return 0, nil, io.EOF }
func (ownerResourceStream) Send(context.Context, uint8, []byte) error      { return nil }
func (ownerResourceStream) Close() error                                   { return nil }

var _ ApplicationReadySession = (*ownerSession)(nil)
var _ ResourceStreamSession = (*ownerSession)(nil)
