package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

type fakeManagedSessionCloser struct {
	requestOnce sync.Once
	requested   chan struct{}
	done        chan struct{}
}

func newFakeManagedSessionCloser() *fakeManagedSessionCloser {
	return &fakeManagedSessionCloser{requested: make(chan struct{}), done: make(chan struct{})}
}

func (closer *fakeManagedSessionCloser) RequestClose() {
	closer.requestOnce.Do(func() { close(closer.requested) })
}

func (closer *fakeManagedSessionCloser) Done() <-chan struct{} { return closer.done }

func TestManagedSessionRegistryLifecycleAndInventoryShareRevision(t *testing.T) {
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	registry := newTestManagedSessionRegistry(t)
	closer := newFakeManagedSessionCloser()
	handle, authenticated, err := registry.Begin(testManagedSessionProjection(1), closer, now)
	if err != nil {
		t.Fatal(err)
	}
	if authenticated.GetRegistryRevision() != 1 || authenticated.GetSession().GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED {
		t.Fatalf("authenticated event = %#v", authenticated)
	}
	inventory, err := registry.Inventory("report-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.GetRegistryRevision() != authenticated.GetRegistryRevision() || len(inventory.GetSessions()) != 1 {
		t.Fatalf("authenticated inventory = %#v", inventory)
	}
	ready, err := handle.MarkReady(now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if ready.GetRegistryRevision() != 2 || ready.GetSession().GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY {
		t.Fatalf("ready event = %#v", ready)
	}
	if _, err := handle.MarkReady(now.Add(2 * time.Second)); !errors.Is(err, ErrManagedSessionRegistryTransition) {
		t.Fatalf("duplicate ready error = %v", err)
	}
	closed, err := handle.MarkClosed("peer_closed", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if closed.GetRegistryRevision() != 3 || closed.GetSession().GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
		t.Fatalf("closed event = %#v", closed)
	}
	inventory, err = registry.Inventory("report-2", now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if inventory.GetRegistryRevision() != closed.GetRegistryRevision() || len(inventory.GetSessions()) != 0 {
		t.Fatalf("closed inventory = %#v", inventory)
	}
	if _, _, err := registry.Begin(testManagedSessionProjection(1), newFakeManagedSessionCloser(), now.Add(5*time.Second)); !errors.Is(err, ErrManagedSessionRegistryTransition) {
		t.Fatalf("reused incarnation error = %v", err)
	}
}

func TestManagedSessionRegistryCloseExactWaitsForOwnerClosed(t *testing.T) {
	now := time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)
	registry := newTestManagedSessionRegistry(t)
	closer := newFakeManagedSessionCloser()
	handle, _, err := registry.Begin(testManagedSessionProjection(1), closer, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.MarkReady(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	resultCh := make(chan *cloudpb.ExactSessionCloseResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, closeErr := registry.CloseExact(context.Background(), testManagedSessionProjection(1).GetTarget(), now.Add(2*time.Second))
		resultCh <- result
		errCh <- closeErr
	}()
	select {
	case <-closer.requested:
	case <-time.After(time.Second):
		t.Fatal("registry did not request exact close")
	}
	select {
	case result := <-resultCh:
		t.Fatalf("close returned before owner teardown: %#v", result)
	default:
	}
	if _, err := handle.MarkClosed("command_close", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	close(closer.done)
	result := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if result.GetDisposition() != cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_REQUESTED || result.GetRegistryRevision() != 4 {
		t.Fatalf("exact close result = %#v", result)
	}

	stale := testManagedSessionProjection(1).GetTarget()
	stale.ControlPresenceSessionId = "presence-old"
	staleResult, err := registry.CloseExact(context.Background(), stale, now.Add(4*time.Second))
	if err != nil || staleResult.GetDisposition() != cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_STALE_TARGET {
		t.Fatalf("stale close result=%#v err=%v", staleResult, err)
	}
}

func TestManagedSessionRegistryPresenceReplacementKeepsEstablishedPresence(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	registry := newTestManagedSessionRegistry(t)
	handle, _, err := registry.Begin(testManagedSessionProjection(1), newFakeManagedSessionCloser(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.MarkReady(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.ReplaceControlPresence("replacement-1", "hub-1", 7, "presence-2", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GetRegistryRevision() != 3 || snapshot.GetControlPresenceSessionId() != "presence-2" || len(snapshot.GetSessions()) != 1 {
		t.Fatalf("replacement snapshot = %#v", snapshot)
	}
	session := snapshot.GetSessions()[0]
	if session.GetEstablishedPresenceSessionId() != "presence-1" || session.GetTarget().GetControlPresenceSessionId() != "presence-2" {
		t.Fatalf("replacement session = %#v", session)
	}
	if _, err := registry.ReplaceControlPresence("replacement-2", "hub-2", 8, "presence-3", now.Add(3*time.Second)); !errors.Is(err, ErrManagedSessionRegistryTarget) {
		t.Fatalf("cross-assignment replacement error = %v", err)
	}
}

func TestManagedSessionRegistryConcurrentBeginLinearizesRevisions(t *testing.T) {
	now := time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)
	registry := newTestManagedSessionRegistry(t)
	const count = 24
	revisions := make(chan uint64, count)
	errorsCh := make(chan error, count)
	var wait sync.WaitGroup
	for index := 1; index <= count; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			projection := testManagedSessionProjection(uint64(index))
			projection.Target.ManagedSessionId = fmt.Sprintf("managed-%02d", index)
			_, event, err := registry.Begin(projection, newFakeManagedSessionCloser(), now.Add(time.Duration(index)*time.Millisecond))
			if err != nil {
				errorsCh <- err
				return
			}
			revisions <- event.GetRegistryRevision()
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	close(revisions)
	seen := make(map[uint64]bool, count)
	for revision := range revisions {
		if revision == 0 || revision > count || seen[revision] {
			t.Fatalf("non-linear revision %d, seen=%v", revision, seen)
		}
		seen[revision] = true
	}
	inventory, err := registry.Inventory("concurrent", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if inventory.GetRegistryRevision() != count || len(inventory.GetSessions()) != count {
		t.Fatalf("concurrent inventory revision=%d sessions=%d", inventory.GetRegistryRevision(), len(inventory.GetSessions()))
	}
}

func newTestManagedSessionRegistry(t *testing.T) *ManagedSessionRegistry {
	t.Helper()
	registry, err := NewManagedSessionRegistry("daemon-1", "runtime-1", "hub-1", 7, "presence-1")
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func testManagedSessionProjection(incarnation uint64) *cloudpb.ManagedPeerSessionProjection {
	return &cloudpb.ManagedPeerSessionProjection{
		Target: &cloudpb.ManagedPeerSessionTarget{
			DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: incarnation,
			AssignmentEpoch: 7, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1",
		},
		ClientDeviceId:                 "client-1",
		EstablishedPresenceSessionId:   "presence-1",
		AuthenticatedClientFingerprint: "client-fingerprint",
		OpaqueAccessReference:          "opaque-access",
		ControlOwnerHubId:              "hub-1",
		ObservedDataPath:               cloudpb.ObservedPath_OBSERVED_PATH_DIRECT,
		State:                          cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED,
		Freshness:                      cloudpb.Freshness_FRESHNESS_FRESH,
	}
}
