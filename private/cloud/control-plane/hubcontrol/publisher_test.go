package hubcontrol

import (
	"errors"
	"testing"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestPublisherBackpressureDoesNotPartiallyPublish(t *testing.T) {
	publisher := NewPublisher()
	fast, cancelFast := publisher.Subscribe("hub-1")
	defer cancelFast()
	slow, cancelSlow := publisher.Subscribe("hub-1")
	defer cancelSlow()

	for revision := uint64(1); revision <= 16; revision++ {
		if err := publisher.PublishFull(publisherFullFixture(revision)); err != nil {
			t.Fatal(err)
		}
		<-fast
	}
	if err := publisher.PublishFull(publisherFullFixture(17)); !errors.Is(err, ErrPublisherBackpressure) {
		t.Fatalf("full queue publish error = %v", err)
	}
	select {
	case message := <-fast:
		t.Fatalf("fast subscriber received partial publish: %v", message)
	default:
	}
	if head, _ := publisher.Head("hub-1"); head.Revision != 16 {
		t.Fatalf("head advanced after partial publish: %#v", head)
	}
	<-slow
	if err := publisher.PublishFull(publisherFullFixture(17)); err != nil {
		t.Fatalf("retry publish = %v", err)
	}
	if message := <-fast; message.(*cloudpb.FullProjectionSnapshot).GetProjectionRevision() != 17 {
		t.Fatalf("retry revision = %v", message)
	}
}

func TestPublisherCommandDoesNotAdvanceProjectionHead(t *testing.T) {
	publisher := NewPublisher()
	updates, cancel := publisher.Subscribe("hub-1")
	defer cancel()
	if err := publisher.PublishFull(publisherFullFixture(1)); err != nil {
		t.Fatal(err)
	}
	<-updates
	command := &cloudpb.HubCommand{CommandId: "command-1", CommandKind: cloudpb.HubCommandKind_HUB_COMMAND_KIND_KICK_PRESENCE, IssuedAtUnixMillis: 10, ExpiresAtUnixMillis: 20, Target: &cloudpb.HubCommand_KickPresence{KickPresence: &cloudpb.KickPresenceTarget{DaemonDeviceId: "daemon-1", AssignmentEpoch: 1, PresenceSessionId: "presence-1"}}}
	if err := publisher.PublishCommand("hub-1", command); err != nil {
		t.Fatal(err)
	}
	message := <-updates
	if message.(*cloudpb.HubCommand).GetCommandId() != "command-1" {
		t.Fatalf("command update = %v", message)
	}
	if head, _ := publisher.Head("hub-1"); head.Revision != 1 {
		t.Fatalf("command changed projection head: %#v", head)
	}
}

func publisherFullFixture(revision uint64) *cloudpb.FullProjectionSnapshot {
	return &cloudpb.FullProjectionSnapshot{HubId: "hub-1", ProjectionRevision: revision, SnapshotDigest: []byte{byte(revision)}}
}
