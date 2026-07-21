package hub_test

import (
	"errors"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/hub"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestHubRuntimeInventoryIsFullReplacementAndFencesSupersededGeneration(t *testing.T) {
	fixture := newFixture(t, 4, 4)
	presence, _ := fixture.openEdgePresence(t)
	defer presence.Close()
	opened := fixture.service.TopologySnapshot(1, fixture.clock.Now())
	if len(opened.GetPresences()) != 1 {
		t.Fatalf("opened topology = %#v", opened)
	}
	presenceID := opened.GetPresences()[0].GetPresenceSessionId()
	request := runtimeRequest("runtime-a", 0, presenceID, nil, fixture.clock.Now().UnixMilli())
	response, err := fixture.service.ReportDaemonRuntime("daemon-1", request)
	if err != nil || response.GetAcceptedRegistryRevision() != 0 {
		t.Fatalf("empty runtime report = (%#v, %v)", response, err)
	}
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", proto.Clone(request).(*cloudpb.ReportDaemonRuntimeRequest)); err != nil {
		t.Fatalf("idempotent runtime report = %v", err)
	}
	session := &cloudpb.ManagedPeerSessionProjection{Target: &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "session-1", SessionIncarnation: 1, AssignmentEpoch: 1, ControlPresenceSessionId: presenceID, DaemonRuntimeGeneration: "runtime-a"}, ClientDeviceId: "client-1", EstablishedPresenceSessionId: presenceID, ControlOwnerHubId: "hub-eu", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}
	changedRequest := runtimeRequest("runtime-a", 1, presenceID, []*cloudpb.ManagedPeerSessionProjection{session}, fixture.clock.Now().UnixMilli())
	changedRequest.TerminalAccesses = &cloudpb.TerminalAccessInventorySnapshot{ReportId: changedRequest.GetReportId(), DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-eu", AssignmentEpoch: 1, ControlPresenceSessionId: presenceID, DaemonRuntimeGeneration: "runtime-a", RegistryRevision: 1, AccessProjectionRevision: 4, ObservedAtUnixMillis: fixture.clock.Now().UnixMilli(), Accesses: []*cloudpb.TerminalAccessProjection{{DaemonDeviceId: "daemon-1", OpaqueAccessReference: "opaque-1", SubjectFingerprintSummary: "subject-1234", State: cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_ACTIVE, IssuedAtUnixMillis: fixture.clock.Now().Add(-time.Hour).UnixMilli(), ExpiresAtUnixMillis: fixture.clock.Now().Add(time.Hour).UnixMilli(), AccessProjectionRevision: 4}}}
	response, err = fixture.service.ReportDaemonRuntime("daemon-1", changedRequest)
	if err != nil || response.GetAcceptedAccessProjectionRevision() != 4 {
		t.Fatal(err)
	}
	snapshot := fixture.service.TopologySnapshot(1, fixture.clock.Now())
	if len(snapshot.GetPeerSessions()) != 1 || snapshot.GetPeerSessions()[0].GetTarget().GetManagedSessionId() != "session-1" || len(snapshot.GetTerminalAccessInventories()) != 1 || len(snapshot.GetTerminalAccessInventories()[0].GetAccesses()) != 1 || snapshot.GetTerminalAccessInventories()[0].GetAccesses()[0].GetOpaqueAccessReference() != "opaque-1" {
		t.Fatalf("session topology = %#v", snapshot)
	}
	accessOnly := proto.Clone(changedRequest).(*cloudpb.ReportDaemonRuntimeRequest)
	accessOnly.TerminalAccesses.AccessProjectionRevision = 5
	accessOnly.TerminalAccesses.Accesses[0].AccessProjectionRevision = 5
	accessOnly.TerminalAccesses.Accesses[0].State = cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_REVOKED
	response, err = fixture.service.ReportDaemonRuntime("daemon-1", accessOnly)
	if err != nil || response.GetAcceptedRegistryRevision() != 1 || response.GetAcceptedAccessProjectionRevision() != 5 {
		t.Fatalf("access-only runtime replacement = (%v, %v)", response, err)
	}
	if got := fixture.service.TopologySnapshot(1, fixture.clock.Now()); got.GetTerminalAccessInventories()[0].GetAccesses()[0].GetState() != cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_REVOKED {
		t.Fatalf("access-only replacement was not published: %v", got)
	}
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", changedRequest); !errors.Is(err, hub.ErrRuntimeReport) {
		t.Fatalf("access revision rollback error = %v", err)
	}
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", runtimeRequest("runtime-b", 0, presenceID, nil, fixture.clock.Now().UnixMilli())); err != nil {
		t.Fatal(err)
	}
	if got := fixture.service.TopologySnapshot(1, fixture.clock.Now()); len(got.GetPeerSessions()) != 0 || len(got.GetTerminalAccessInventories()) != 0 {
		t.Fatalf("new runtime empty inventory did not replace sessions: %#v", got)
	}
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", runtimeRequest("runtime-a", 2, presenceID, nil, fixture.clock.Now().UnixMilli())); !errors.Is(err, hub.ErrRuntimeReport) {
		t.Fatalf("superseded runtime generation error = %v", err)
	}
}

func runtimeRequest(generation string, revision uint64, presenceID string, sessions []*cloudpb.ManagedPeerSessionProjection, observedAt int64) *cloudpb.ReportDaemonRuntimeRequest {
	reportID := generation + ":report"
	peer := &cloudpb.PeerSessionInventorySnapshot{ReportId: reportID, DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-eu", AssignmentEpoch: 1, ControlPresenceSessionId: presenceID, DaemonRuntimeGeneration: generation, RegistryRevision: revision, Sessions: sessions, ObservedAtUnixMillis: observedAt}
	return &cloudpb.ReportDaemonRuntimeRequest{ReportId: reportID, HubId: "hub-eu", AssignmentEpoch: 1, PresenceSessionId: presenceID, DaemonRuntimeGeneration: generation, RegistryRevision: revision, PeerSessions: peer}
}
