package commandoutbox_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/commandoutbox"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestDispatcherRetriesIdenticalSignedDaemonCommandUntilExecutionReceipt(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outbox, _ := commandoutbox.New(store)
	now := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)
	projection := closeSessionCommand(now, "child-1")
	if _, _, err := outbox.Create(context.Background(), projection, "idem-dispatch", now); err != nil {
		t.Fatal(err)
	}
	publisher := &recordingCommandPublisher{}
	source := &plannerSource{device: cloudtopology.DeviceOwnership{DeviceID: "daemon-1", AccountID: "account-1", Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 9}}
	dispatcher, err := commandoutbox.NewDispatcher(outbox, publisher, source, "control-1", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.DispatchOnce(context.Background(), now.Add(time.Second), 32); err != nil {
		t.Fatal(err)
	}
	if _, _, err := outbox.ApplyHubResult(context.Background(), &cloudpb.HubCommandResult{CommandId: "child-1", HubId: "hub-1", ControlGeneration: 2, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(2 * time.Second).UnixMilli()}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.DispatchOnce(context.Background(), now.Add(3*time.Second), 32); err != nil {
		t.Fatal(err)
	}
	if len(publisher.commands) != 2 || !proto.Equal(publisher.commands[0], publisher.commands[1]) {
		t.Fatalf("dispatcher retries = %v", publisher.commands)
	}
	verifier, _ := cloudpb.NewDaemonControlVerifier(map[string]ed25519.PublicKey{"control-1": publicKey})
	if err := verifier.Verify(publisher.commands[0].GetDaemonCommand(), now.Add(time.Second)); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

type recordingCommandPublisher struct {
	hubIDs   []string
	commands []*cloudpb.HubCommand
}

func (publisher *recordingCommandPublisher) PublishCommand(hubID string, command *cloudpb.HubCommand) error {
	publisher.hubIDs = append(publisher.hubIDs, hubID)
	publisher.commands = append(publisher.commands, proto.Clone(command).(*cloudpb.HubCommand))
	return nil
}
