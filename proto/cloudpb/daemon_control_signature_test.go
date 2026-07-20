package cloudpb

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
)

func TestDaemonControlSignatureBindsExactManagedSession(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	target := &ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 3, AssignmentEpoch: 7, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1"}
	command := &DaemonControlCommand{CommandId: "command-1", CommandKind: DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, AccountId: "account-1", TargetDeviceId: "daemon-1", HubId: "hub-1", AssignmentEpoch: 7, AuthEpoch: 4, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Target: &DaemonControlCommand_ManagedPeerSession{ManagedPeerSession: target}}
	signed, err := SignDaemonControlCommand(command, "daemon-control-1", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, _ := NewDaemonControlVerifier(map[string]ed25519.PublicKey{"daemon-control-1": publicKey})
	if err := verifier.Verify(signed, now); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	tampered := proto.Clone(signed).(*DaemonControlCommand)
	tampered.GetManagedPeerSession().SessionIncarnation++
	if err := verifier.Verify(tampered, now); !errors.Is(err, ErrInvalidDaemonControlCommand) && !errors.Is(err, ErrInvalidDaemonControlSignature) {
		t.Fatalf("tampered Verify() error = %v", err)
	}
}
