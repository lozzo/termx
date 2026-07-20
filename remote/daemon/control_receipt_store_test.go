package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

func TestTerminalGrantRevokePersistsReceiptAndClosesBoundSessions(t *testing.T) {
	identity, _, accessStore, now := sessionFixture(t, remoteauth.Scope{TerminalID: "terminal-1"})
	controlPublic, controlPrivate, _ := ed25519.GenerateKey(rand.Reader)
	controlDir := t.TempDir()
	receipts, err := LoadControlReceiptStore(controlDir, identity)
	if err != nil {
		t.Fatal(err)
	}
	enrollment := &cloudpb.DaemonControlEnrollment{AccountId: "account-1", DaemonDeviceId: identity.DeviceID, AuthEpoch: 5, EnrolledAtUnixMillis: now.Add(-time.Hour).UnixMilli(), VerificationKeys: []*cloudpb.DaemonControlVerificationKey{{KeyId: "control-1", PublicKey: controlPublic, NotBeforeUnixMillis: now.Add(-time.Hour).UnixMilli(), NotAfterUnixMillis: now.Add(time.Hour).UnixMilli()}}}
	if err := receipts.InstallEnrollment(enrollment); err != nil {
		t.Fatal(err)
	}
	runtime, _ := NewManagedRuntime(identity.DeviceID, nil)
	if err := runtime.BindPresence("hub-1", 3, "presence-1", now); err != nil {
		t.Fatal(err)
	}
	record := accessStore.ListClientAccess()[0]
	reference := OpaqueAccessReference(identity.DeviceID, record.GrantID)
	owner := &agentCloseOwner{done: make(chan struct{})}
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: identity.DeviceID, ManagedSessionId: "managed-1", SessionIncarnation: 1, AssignmentEpoch: 3, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: runtime.RuntimeGeneration()}
	handle, _, err := runtime.Registry().Begin(&cloudpb.ManagedPeerSessionProjection{Target: target, ClientDeviceId: "client-1", EstablishedPresenceSessionId: "presence-1", AuthenticatedClientFingerprint: record.SubjectKeyFingerprint, OpaqueAccessReference: reference, ControlOwnerHubId: "hub-1", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}, owner, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.MarkReady(now); err != nil {
		t.Fatal(err)
	}
	owner.request = func() {
		_, _ = handle.MarkClosed("access_revoked", now)
		close(owner.done)
	}
	unsigned := &cloudpb.DaemonControlCommand{CommandId: "revoke-1", CommandKind: cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_REVOKE_TERMINAL_ACCESS, AccountId: "account-1", TargetDeviceId: identity.DeviceID, HubId: "hub-1", AssignmentEpoch: 3, AuthEpoch: 5, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: runtime.RuntimeGeneration(), IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Target: &cloudpb.DaemonControlCommand_TerminalAccess{TerminalAccess: &cloudpb.RevokeTerminalAccessTarget{DaemonDeviceId: identity.DeviceID, OpaqueAccessReference: reference, AssignmentEpoch: 3, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: runtime.RuntimeGeneration(), AccessProjectionRevision: accessStore.AccessProjectionRevision()}}}
	command, err := cloudpb.SignDaemonControlCommand(unsigned, "control-1", controlPrivate)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.ExecuteControlCommand(context.Background(), command, receipts, accessStore, now)
	if err != nil || result.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED || result.GetClosedSessionCount() != 1 || result.GetOpaqueAccessReference() != reference || result.GetAccessProjectionRevision() != accessStore.AccessProjectionRevision() || !accessStore.Revoked(record.GrantID) {
		t.Fatalf("ExecuteControlCommand() = (%v, %v)", result, err)
	}
	if err := receipts.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadControlReceiptStore(controlDir, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	replayed, err := runtime.ExecuteControlCommand(context.Background(), proto.Clone(command).(*cloudpb.DaemonControlCommand), reloaded, accessStore, now.Add(10*time.Second))
	if err != nil || !proto.Equal(result, replayed) {
		t.Fatalf("persisted replay = (%v, %v), want %v", replayed, err, result)
	}
	conflict := proto.Clone(command).(*cloudpb.DaemonControlCommand)
	conflict.GetTerminalAccess().OpaqueAccessReference = "different-reference"
	if _, err := runtime.ExecuteControlCommand(context.Background(), conflict, reloaded, accessStore, now.Add(10*time.Second)); !errors.Is(err, cloudpb.ErrInvalidDaemonControlSignature) && !errors.Is(err, ErrControlReceiptConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
}
