package core

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGrantRevokePersistenceFailureLeavesActiveTransportsAndAdmissionOpen(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	injected := errors.New("persist failed")
	service := newGrantAccessTestService(map[string]time.Time{"grant-a": expiresAt})
	service.revokeErr = injected
	server := newGrantTransportTestServer(service, &now, nil)

	firstRaw, first := admitGrantTestTransport(t, server, "grant-a", expiresAt)
	if _, err := server.revokeClientAccess(context.Background(), "grant-a"); !errors.Is(err, injected) {
		t.Fatalf("revoke error = %v", err)
	}
	if firstRaw.closeCount.Load() != 0 {
		t.Fatalf("persist failure closed active transport %d times", firstRaw.closeCount.Load())
	}
	server.grantMu.Lock()
	_, tombstoned := server.grantTombstones["grant-a"]
	server.grantMu.Unlock()
	if tombstoned {
		t.Fatal("persist failure installed revoke tombstone")
	}
	secondRaw, second := admitGrantTestTransport(t, server, "grant-a", expiresAt)

	server.untrackTransport(first)
	server.untrackTransport(second)
	_ = first.Close()
	_ = second.Close()
	assertGrantCloseCount(t, firstRaw, 1)
	assertGrantCloseCount(t, secondRaw, 1)
}

func TestGrantRevokeClosesAllMatchingTransportsExactlyOnceAndIsolatesOtherGrant(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	service := newGrantAccessTestService(map[string]time.Time{"grant-a": expiresAt, "grant-b": expiresAt})
	server := newGrantTransportTestServer(service, &now, nil)

	raws := make([]*grantCountingTransport, 0, 3)
	tracked := make([]*trackedTransport, 0, 3)
	for range 3 {
		raw, connection := admitGrantTestTransport(t, server, "grant-a", expiresAt)
		raws, tracked = append(raws, raw), append(tracked, connection)
	}
	isolatedRaw, isolated := admitGrantTestTransport(t, server, "grant-b", expiresAt)

	if record, err := server.revokeClientAccess(context.Background(), "grant-a"); err != nil || record.GrantID != "grant-a" {
		t.Fatalf("revoke record = %#v, error = %v", record, err)
	}
	for _, raw := range raws {
		assertGrantCloseCount(t, raw, 1)
	}
	if isolatedRaw.closeCount.Load() != 0 {
		t.Fatalf("other grant transport closed %d times", isolatedRaw.closeCount.Load())
	}

	rejected := newGrantCountingTransport()
	if _, err := server.admitTransport(context.Background(), rejected, grantTestScope("grant-a", expiresAt)); !errors.Is(err, ErrGrantTransportInactive) {
		t.Fatalf("post-revoke admission error = %v", err)
	}
	assertGrantCloseCount(t, rejected, 1)
	if _, err := server.revokeClientAccess(context.Background(), "grant-a"); err != nil {
		t.Fatalf("idempotent revoke: %v", err)
	}
	for index, connection := range tracked {
		server.untrackTransport(connection)
		_ = connection.Close()
		assertGrantCloseCount(t, raws[index], 1)
	}
	server.untrackTransport(isolated)
	_ = isolated.Close()
	assertGrantCloseCount(t, isolatedRaw, 1)
}

func TestGrantAdmissionDuringRevokeLinearizesAfterPersistence(t *testing.T) {
	for _, test := range []struct {
		name          string
		persistErr    error
		wantAdmitted  bool
		wantFirstOpen bool
	}{
		{name: "success", wantAdmitted: false, wantFirstOpen: false},
		{name: "failure", persistErr: errors.New("persist failed"), wantAdmitted: true, wantFirstOpen: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
			expiresAt := now.Add(time.Hour)
			service := newGrantAccessTestService(map[string]time.Time{"grant-race": expiresAt})
			service.revokeErr = test.persistErr
			service.revokeEntered = make(chan struct{})
			service.releaseRevoke = make(chan struct{})
			server := newGrantTransportTestServer(service, &now, nil)
			firstRaw, first := admitGrantTestTransport(t, server, "grant-race", expiresAt)

			revokeDone := make(chan error, 1)
			go func() {
				_, err := server.revokeClientAccess(context.Background(), "grant-race")
				revokeDone <- err
			}()
			<-service.revokeEntered
			secondRaw := newGrantCountingTransport()
			type admissionResult struct {
				tracked *trackedTransport
				err     error
			}
			admitStarted := make(chan struct{})
			admitDone := make(chan admissionResult, 1)
			go func() {
				close(admitStarted)
				tracked, err := server.admitTransport(context.Background(), secondRaw, grantTestScope("grant-race", expiresAt))
				admitDone <- admissionResult{tracked: tracked, err: err}
			}()
			<-admitStarted
			close(service.releaseRevoke)
			revokeErr := <-revokeDone
			admission := <-admitDone
			if !errors.Is(revokeErr, test.persistErr) {
				t.Fatalf("revoke error = %v", revokeErr)
			}
			if got := admission.err == nil; got != test.wantAdmitted {
				t.Fatalf("admitted = %v, error = %v", got, admission.err)
			}
			if got := firstRaw.closeCount.Load() == 0; got != test.wantFirstOpen {
				t.Fatalf("first transport open = %v, closes = %d", got, firstRaw.closeCount.Load())
			}
			if admission.tracked != nil {
				server.untrackTransport(admission.tracked)
				_ = admission.tracked.Close()
			}
			server.untrackTransport(first)
			_ = first.Close()
			assertGrantCloseCount(t, firstRaw, 1)
			assertGrantCloseCount(t, secondRaw, 1)
		})
	}
}

func TestGrantAbsoluteExpirySharesOneCancelableTimerAndClosesAtBoundary(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	service := newGrantAccessTestService(map[string]time.Time{"grant-expiry": expiresAt})
	timers := &grantManualTimerFactory{}
	server := newGrantTransportTestServer(service, &now, timers.afterFunc)

	firstRaw, first := admitGrantTestTransport(t, server, "grant-expiry", expiresAt)
	secondRaw, second := admitGrantTestTransport(t, server, "grant-expiry", expiresAt)
	if timers.count() != 1 {
		t.Fatalf("timers = %d, want one per active grant", timers.count())
	}
	now = expiresAt
	timers.fire(t, 0)
	assertGrantCloseCount(t, firstRaw, 1)
	assertGrantCloseCount(t, secondRaw, 1)

	rejected := newGrantCountingTransport()
	if _, err := server.admitTransport(context.Background(), rejected, grantTestScope("grant-expiry", expiresAt)); !errors.Is(err, ErrGrantTransportInactive) {
		t.Fatalf("expiry-boundary admission error = %v", err)
	}
	assertGrantCloseCount(t, rejected, 1)
	if timers.count() != 1 {
		t.Fatalf("expiry created replacement timer, count = %d", timers.count())
	}
	server.untrackTransport(first)
	server.untrackTransport(second)
	_ = first.Close()
	_ = second.Close()
}

func TestGrantDisconnectCancelsLastTimer(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	service := newGrantAccessTestService(map[string]time.Time{"grant-disconnect": expiresAt})
	timers := &grantManualTimerFactory{}
	server := newGrantTransportTestServer(service, &now, timers.afterFunc)
	raw, tracked := admitGrantTestTransport(t, server, "grant-disconnect", expiresAt)

	server.untrackTransport(tracked)
	_ = tracked.Close()
	assertGrantCloseCount(t, raw, 1)
	if timer := timers.timer(0); timer.stopCount.Load() != 1 {
		t.Fatalf("timer stop count = %d, want 1", timer.stopCount.Load())
	}
	server.grantMu.Lock()
	timerCount := len(server.grantTimers)
	server.grantMu.Unlock()
	if timerCount != 0 {
		t.Fatalf("timer index retained %d entries", timerCount)
	}
}

func TestGrantTransportCloseRacesExactOnceCount40(t *testing.T) {
	for iteration := range 40 {
		now := time.Date(2026, 7, 30, 12, 0, 0, iteration, time.UTC)
		expiresAt := now.Add(time.Hour)
		grantID := "grant-race"
		service := newGrantAccessTestService(map[string]time.Time{grantID: expiresAt})
		timers := &grantManualTimerFactory{}
		server := newGrantTransportTestServer(service, &now, timers.afterFunc)
		raw, tracked := admitGrantTestTransport(t, server, grantID, expiresAt)
		now = expiresAt

		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(4)
		go func() {
			defer wait.Done()
			<-start
			server.untrackTransport(tracked)
			_ = tracked.Close()
		}()
		go func() {
			defer wait.Done()
			<-start
			_, _ = server.revokeClientAccess(context.Background(), grantID)
		}()
		go func() {
			defer wait.Done()
			<-start
			server.expireGrant(grantID, expiresAt)
		}()
		go func() {
			defer wait.Done()
			<-start
			_ = server.Shutdown(context.Background())
		}()
		close(start)
		wait.Wait()
		assertGrantCloseCount(t, raw, 1)
		server.grantMu.Lock()
		grantCount, reverseCount, timerCount := len(server.grantTransports), len(server.transportGrants), len(server.grantTimers)
		server.grantMu.Unlock()
		if grantCount != 0 || reverseCount != 0 || timerCount != 0 {
			t.Fatalf("iteration %d leaked grant indexes: %d/%d/%d", iteration, grantCount, reverseCount, timerCount)
		}
	}
}

type grantAccessTestService struct {
	mu              sync.Mutex
	grants          map[string]time.Time
	revoked         map[string]bool
	revokeErr       error
	revokeEntered   chan struct{}
	releaseRevoke   chan struct{}
	revokeEnterOnce sync.Once
}

func newGrantAccessTestService(grants map[string]time.Time) *grantAccessTestService {
	return &grantAccessTestService{grants: grants, revoked: make(map[string]bool)}
}

func (service *grantAccessTestService) Identity(context.Context, []byte) (ClientAccessIdentity, error) {
	return ClientAccessIdentity{}, nil
}

func (service *grantAccessTestService) CreateTicket(context.Context, ClientAccessTicketRequest) (ClientAccessTicket, error) {
	return ClientAccessTicket{}, nil
}

func (service *grantAccessTestService) List(context.Context) ([]ClientAccessRecord, error) {
	return nil, nil
}

func (service *grantAccessTestService) GrantActive(_ context.Context, grantID string, expiresAt, now time.Time) bool {
	service.mu.Lock()
	stored, ok := service.grants[grantID]
	active := ok && !service.revoked[grantID] && stored.Equal(expiresAt) && now.Before(stored)
	service.mu.Unlock()
	return active
}

func (service *grantAccessTestService) Revoke(_ context.Context, grantID string) (ClientAccessRecord, error) {
	if service.revokeEntered != nil {
		service.revokeEnterOnce.Do(func() { close(service.revokeEntered) })
		<-service.releaseRevoke
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.revokeErr != nil {
		return ClientAccessRecord{}, service.revokeErr
	}
	expiresAt, ok := service.grants[grantID]
	if !ok {
		return ClientAccessRecord{}, errors.New("client access grant not found")
	}
	service.revoked[grantID] = true
	return ClientAccessRecord{GrantID: grantID, ExpiresAt: expiresAt, RevokedAt: expiresAt.Add(-time.Minute)}, nil
}

type grantCountingTransport struct {
	done       chan struct{}
	doneOnce   sync.Once
	closeCount atomic.Int32
}

func newGrantCountingTransport() *grantCountingTransport {
	return &grantCountingTransport{done: make(chan struct{})}
}

func (*grantCountingTransport) Send([]byte) error     { return io.EOF }
func (*grantCountingTransport) Recv() ([]byte, error) { return nil, io.EOF }

func (transport *grantCountingTransport) Close() error {
	transport.closeCount.Add(1)
	transport.doneOnce.Do(func() { close(transport.done) })
	return nil
}

func (transport *grantCountingTransport) Done() <-chan struct{} { return transport.done }

type grantManualTimer struct {
	callback  func()
	stopCount atomic.Int32
}

func (timer *grantManualTimer) Stop() bool {
	return timer.stopCount.Add(1) == 1
}

type grantManualTimerFactory struct {
	mu     sync.Mutex
	timers []*grantManualTimer
}

func (factory *grantManualTimerFactory) afterFunc(_ time.Duration, callback func()) grantTimer {
	timer := &grantManualTimer{callback: callback}
	factory.mu.Lock()
	factory.timers = append(factory.timers, timer)
	factory.mu.Unlock()
	return timer
}

func (factory *grantManualTimerFactory) count() int {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return len(factory.timers)
}

func (factory *grantManualTimerFactory) timer(index int) *grantManualTimer {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return factory.timers[index]
}

func (factory *grantManualTimerFactory) fire(t *testing.T, index int) {
	t.Helper()
	timer := factory.timer(index)
	if timer.callback == nil {
		t.Fatal("manual grant timer has no callback")
	}
	timer.callback()
}

func newGrantTransportTestServer(service ClientAccessService, now *time.Time, afterFunc func(time.Duration, func()) grantTimer) *Server {
	server := NewServer(WithClientAccessService(service))
	server.grantNow = func() time.Time { return *now }
	if afterFunc != nil {
		server.grantAfterFunc = afterFunc
	}
	return server
}

func grantTestScope(grantID string, expiresAt time.Time) TransportScope {
	return TransportScope{GrantID: grantID, GrantExpiresAt: expiresAt, PrincipalID: "subject", AllowDaemon: true}
}

func admitGrantTestTransport(t *testing.T, server *Server, grantID string, expiresAt time.Time) (*grantCountingTransport, *trackedTransport) {
	t.Helper()
	raw := newGrantCountingTransport()
	tracked, err := server.admitTransport(context.Background(), raw, grantTestScope(grantID, expiresAt))
	if err != nil {
		t.Fatalf("admit %s: %v", grantID, err)
	}
	return raw, tracked
}

func assertGrantCloseCount(t *testing.T, transport *grantCountingTransport, want int32) {
	t.Helper()
	if got := transport.closeCount.Load(); got != want {
		t.Fatalf("transport close count = %d, want %d", got, want)
	}
}
