package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

func TestRuntimeReporterRetriesExactRevisionAndThenPublishesNewestInventory(t *testing.T) {
	runtime, err := NewManagedRuntime("daemon-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := runtime.BindPresence("hub-1", 1, "presence-1", now); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan *cloudpb.ReportDaemonRuntimeRequest, 2)
	var first *cloudpb.ReportDaemonRuntimeRequest
	calls := 0
	fake := &cloudcompanion.FakeClient{ReportDaemonRuntimeFunc: func(_ context.Context, request *cloudpb.ReportDaemonRuntimeRequest) (*cloudpb.ReportDaemonRuntimeResponse, error) {
		calls++
		if calls == 1 {
			first = proto.Clone(request).(*cloudpb.ReportDaemonRuntimeRequest)
			return nil, errors.New("response lost")
		}
		if calls == 2 && !proto.Equal(first, request) {
			t.Errorf("same revision retry changed: first=%v retry=%v", first, request)
		}
		accepted <- proto.Clone(request).(*cloudpb.ReportDaemonRuntimeRequest)
		return &cloudpb.ReportDaemonRuntimeResponse{ReportId: request.GetReportId(), DaemonRuntimeGeneration: request.GetDaemonRuntimeGeneration(), AcceptedRegistryRevision: request.GetRegistryRevision()}, nil
	}}
	agent := Agent{Companion: fake, Runtime: runtime, RuntimeReportRetryDelay: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		agent.runRuntimeReporter(ctx, runtime.Registry(), "hub-1", 1, "presence-1")
	}()
	initial := waitRuntimeReport(t, accepted)
	if initial.GetRegistryRevision() != 0 || len(initial.GetPeerSessions().GetSessions()) != 0 {
		t.Fatalf("initial runtime report = %#v", initial)
	}
	projection := testManagedSessionProjection(1)
	projection.Target.AssignmentEpoch = 1
	projection.Target.DaemonRuntimeGeneration = runtime.RuntimeGeneration()
	if _, _, err := runtime.Registry().Begin(projection, newFakeManagedSessionCloser(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	changed := waitRuntimeReport(t, accepted)
	if changed.GetRegistryRevision() != 1 || len(changed.GetPeerSessions().GetSessions()) != 1 {
		t.Fatalf("changed runtime report = %#v", changed)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime reporter did not stop with Presence context")
	}
}

func TestRuntimeReporterPublishesAccessRevisionAndRevokeReplacement(t *testing.T) {
	identity, _, accessStore, now := sessionFixture(t, remoteauth.Scope{AllowDaemon: true})
	runtime, err := NewManagedRuntime(identity.DeviceID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.BindPresence("hub-1", 3, "presence-1", now); err != nil {
		t.Fatal(err)
	}
	reports := make(chan *cloudpb.ReportDaemonRuntimeRequest, 2)
	fake := &cloudcompanion.FakeClient{ReportDaemonRuntimeFunc: func(_ context.Context, request *cloudpb.ReportDaemonRuntimeRequest) (*cloudpb.ReportDaemonRuntimeResponse, error) {
		reports <- proto.Clone(request).(*cloudpb.ReportDaemonRuntimeRequest)
		return &cloudpb.ReportDaemonRuntimeResponse{ReportId: request.GetReportId(), DaemonRuntimeGeneration: request.GetDaemonRuntimeGeneration(), AcceptedRegistryRevision: request.GetRegistryRevision(), AcceptedAccessProjectionRevision: request.GetTerminalAccesses().GetAccessProjectionRevision()}, nil
	}}
	agent := Agent{Companion: fake, Identity: identity, Runtime: runtime, AccessStore: accessStore, Now: func() time.Time { return now }, RuntimeReportRetryDelay: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		agent.runRuntimeReporter(ctx, runtime.Registry(), "hub-1", 3, "presence-1")
	}()
	initial := waitRuntimeReport(t, reports)
	if initial.GetTerminalAccesses().GetAccessProjectionRevision() != accessStore.AccessProjectionRevision() || len(initial.GetTerminalAccesses().GetAccesses()) != 1 || initial.GetTerminalAccesses().GetAccesses()[0].GetState() != cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_ACTIVE {
		t.Fatalf("initial access inventory = %v", initial.GetTerminalAccesses())
	}
	records := accessStore.ListClientAccess()
	if _, err := accessStore.RevokeGrant(records[0].GrantID); err != nil {
		t.Fatal(err)
	}
	changed := waitRuntimeReport(t, reports)
	if changed.GetTerminalAccesses().GetAccessProjectionRevision() <= initial.GetTerminalAccesses().GetAccessProjectionRevision() || changed.GetTerminalAccesses().GetAccesses()[0].GetState() != cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_REVOKED {
		t.Fatalf("revoked access inventory = %v", changed.GetTerminalAccesses())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("access reporter did not stop")
	}
}

func waitRuntimeReport(t *testing.T, reports <-chan *cloudpb.ReportDaemonRuntimeRequest) *cloudpb.ReportDaemonRuntimeRequest {
	t.Helper()
	select {
	case report := <-reports:
		return report
	case <-time.After(time.Second):
		t.Fatal("runtime report was not published")
		return nil
	}
}
