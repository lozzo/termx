package hub

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubcontrol"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestControlClientBootstrapsMemoryProjectionAndReportsReconciliation(t *testing.T) {
	now := time.Now().UTC()
	hubPublicKey, hubPrivateKey, _ := ed25519.GenerateKey(rand.Reader)
	relayPublicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	controllerPublicKey, controllerPrivateKey, _ := ed25519.GenerateKey(rand.Reader)
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _ := hubregistry.New(store)
	topologyService, _ := cloudtopology.New(registry, store)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublicKey), RelayId: "relay-1", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublicKey)}
	if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: hubPublicKey, RelayControlPublicKey: relayPublicKey, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	publisher := hubcontrol.NewPublisher()
	full, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{
		HubID: "hub-1", Revision: 1, GeneratedAt: now, TTL: time.Hour, SigningKeyID: "controller-key", SigningKey: controllerPrivateKey,
		Accounts:    []*cloudpb.HubAccountPolicy{{AccountId: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}}},
		Devices:     []*cloudpb.CloudDevicePolicy{{AccountId: "account-1", DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1}},
		Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(10 * time.Minute).UnixMilli()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishFull(full); err != nil {
		t.Fatal(err)
	}
	results := &acceptingRuntimeResults{hub: make(chan *cloudpb.HubCommandResult, 1)}
	controlServer, _ := hubcontrol.NewServer(hubcontrol.ServerConfig{Registry: registry, CursorStore: store, Publisher: publisher, Topology: topologyService, Results: results, Clock: time.Now, Random: rand.Reader, ChallengeTTL: time.Minute, EnvelopeTTL: time.Minute})
	httpServer := httptest.NewServer(controlServer.Handler())
	defer httpServer.Close()
	projection, err := NewProjection(ProjectionConfig{HubID: "hub-1", ControllerKeyID: "controller-key", ControllerPublicKey: controllerPublicKey, MaxStaleness: time.Hour, PolicySink: &projectionSink{}})
	if err != nil {
		t.Fatal(err)
	}
	topology := newStaticTopology("hub-1")
	client, err := NewControlClient(ControlClientConfig{ControllerURL: httpServer.URL, Metadata: metadata, PrivateKey: hubPrivateKey, SoftwareVersion: "test", Projection: projection, Topology: topology, MinBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	waitProjectionRevision(t, projection, 1)
	waitCursor(t, store, 2)
	deltaNow := time.Now().UTC()
	deltaAssignment := &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: 2, NotBeforeUnixMillis: deltaNow.UnixMilli(), ExpiresAtUnixMillis: deltaNow.Add(time.Minute).UnixMilli()}
	delta, err := hubcontrol.BuildSignedDelta(hubcontrol.DeltaProjectionInput{HubID: "hub-1", Revision: 2, PreviousRevision: 1, GeneratedAt: deltaNow, TTL: 30 * time.Minute, SigningKeyID: "controller-key", SigningKey: controllerPrivateKey, AssignmentOperations: []*cloudpb.HubAssignmentDelta{{Operation: cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_UPSERT, DaemonDeviceId: "daemon-1", Assignment: deltaAssignment}}, ResultingAccounts: full.GetAccounts(), ResultingDevices: full.GetDevices(), ResultingAssignments: []*cloudpb.HubAssignment{deltaAssignment}})
	if err != nil {
		t.Fatal(err)
	}
	resultingFull, err := hubcontrol.BuildSignedFullProjection(hubcontrol.FullProjectionInput{HubID: "hub-1", Revision: 2, GeneratedAt: deltaNow, TTL: 30 * time.Minute, SigningKeyID: "controller-key", SigningKey: controllerPrivateKey, Accounts: full.GetAccounts(), Devices: full.GetDevices(), Assignments: []*cloudpb.HubAssignment{deltaAssignment}})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishDelta(delta, resultingFull); err != nil {
		t.Fatal(err)
	}
	waitProjectionRevision(t, projection, 2)
	waitCursor(t, store, 3)
	state := client.State()
	if !state.Attached || state.ControlGeneration != 1 || state.LastSequence != 3 {
		t.Fatalf("control client state = %#v", state)
	}
	commandNow := time.Now().UTC()
	command := &cloudpb.HubCommand{CommandId: "command-1", CommandKind: cloudpb.HubCommandKind_HUB_COMMAND_KIND_KICK_PRESENCE, IssuedAtUnixMillis: commandNow.UnixMilli(), ExpiresAtUnixMillis: commandNow.Add(time.Minute).UnixMilli(), Target: &cloudpb.HubCommand_KickPresence{KickPresence: &cloudpb.KickPresenceTarget{DaemonDeviceId: "daemon-1", AssignmentEpoch: 2, PresenceSessionId: "presence-1"}}}
	if err := publisher.PublishCommand("hub-1", command); err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-topology.commands:
		if received.GetCommandId() != command.GetCommandId() {
			t.Fatalf("received command = %v", received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Hub command was not delivered")
	}
	waitCursor(t, store, 4)
	select {
	case result := <-results.hub:
		if result.GetCommandId() != command.GetCommandId() {
			t.Fatalf("persisted result = %v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("Hub command result was not persisted")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v", err)
	}
}

type staticTopology struct {
	hubID    string
	changes  chan struct{}
	events   chan *cloudpb.HubRuntimeEnvelope
	commands chan *cloudpb.HubCommand
}

type acceptingRuntimeResults struct {
	hub chan *cloudpb.HubCommandResult
}

func (results *acceptingRuntimeResults) IngestHubResult(_ context.Context, result *cloudpb.HubCommandResult, _ time.Time) error {
	results.hub <- proto.Clone(result).(*cloudpb.HubCommandResult)
	return nil
}

func (*acceptingRuntimeResults) IngestDaemonResult(context.Context, *cloudpb.DaemonCommandResult, time.Time) error {
	return nil
}

func newStaticTopology(hubID string) *staticTopology {
	return &staticTopology{hubID: hubID, changes: make(chan struct{}), events: make(chan *cloudpb.HubRuntimeEnvelope), commands: make(chan *cloudpb.HubCommand, 1)}
}

func (topology *staticTopology) TopologyChanges() <-chan struct{} { return topology.changes }

func (topology *staticTopology) RuntimeEvents() <-chan *cloudpb.HubRuntimeEnvelope {
	return topology.events
}

func (topology *staticTopology) ExecuteHubCommand(command *cloudpb.HubCommand, generation uint64, now time.Time) *cloudpb.HubCommandResult {
	topology.commands <- proto.Clone(command).(*cloudpb.HubCommand)
	return &cloudpb.HubCommandResult{CommandId: command.GetCommandId(), HubId: topology.hubID, ControlGeneration: generation, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.UnixMilli()}
}

func (topology *staticTopology) TopologySnapshot(generation uint64, observedAt time.Time) *cloudpb.HubTopologySnapshot {
	snapshot := &cloudpb.HubTopologySnapshot{HubId: topology.hubID, ControlGeneration: generation, ObservedAtUnixMillis: observedAt.UnixMilli()}
	payload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	digest := sha256.Sum256(payload)
	snapshot.TopologyDigest = digest[:]
	return snapshot
}

func waitProjectionRevision(t *testing.T, projection *Projection, revision uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if projection.Snapshot().Revision == revision {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("projection revision = %d, want %d", projection.Snapshot().Revision, revision)
}

func waitCursor(t *testing.T, store *cloudsqlite.Store, sequence uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		accepted, _, err := store.ControlCursor(context.Background(), "hub-1", 1, cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB)
		if err == nil && accepted == sequence {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	accepted, _, _ := store.ControlCursor(context.Background(), "hub-1", 1, cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB)
	t.Fatalf("accepted cursor = %d, want %d", accepted, sequence)
}
