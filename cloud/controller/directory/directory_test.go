package directory_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/muxvia/muxvia/cloud/controller/directory"
	"github.com/muxvia/muxvia/cloud/runtimesnapshot"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDirectoryPublishesOnlyCommittedSnapshotAndFencesGeneration(t *testing.T) {
	directoryState := newDirectory(t, 15*time.Millisecond)
	attach(t, directoryState, "connection-old")
	snapshot := &cloudv1.RuntimeSnapshot{Revision: 1, Agents: []*cloudv1.AgentPresence{testAgent("daemon-1", 1)}}
	commit(t, directoryState, "connection-old", snapshot)

	attach(t, directoryState, "connection-new")
	begin := &cloudv1.SnapshotBegin{SnapshotId: "pending", Revision: 0}
	if err := directoryState.BeginSnapshot(context.Background(), "connection-new", begin); err != nil {
		t.Fatalf("begin replacement snapshot: %v", err)
	}
	projection, found, err := directoryState.Edge(context.Background(), "edge-1")
	if err != nil || !found || projection.ConnectionID != "connection-old" {
		t.Fatalf("half snapshot changed current projection: %+v found=%v err=%v", projection, found, err)
	}
	commit(t, directoryState, "connection-new", &cloudv1.RuntimeSnapshot{})
	directoryState.Detach("connection-old")
	time.Sleep(30 * time.Millisecond)
	projection, found, err = directoryState.Edge(context.Background(), "edge-1")
	if err != nil || !found || projection.ConnectionID != "connection-new" || projection.AgentCount != 0 {
		t.Fatalf("old generation cleanup removed replacement: %+v found=%v err=%v", projection, found, err)
	}
}

func TestDirectoryRejectsRevisionGapAndRemovesWholeGeneration(t *testing.T) {
	directoryState := newDirectory(t, 10*time.Millisecond)
	attach(t, directoryState, "connection-1")
	commit(t, directoryState, "connection-1", &cloudv1.RuntimeSnapshot{Revision: 7, Agents: []*cloudv1.AgentPresence{testAgent("daemon-1", 1)}})
	err := directoryState.ApplyDelta(context.Background(), "connection-1", &cloudv1.RuntimeDelta{Revision: 9, Change: &cloudv1.RuntimeDelta_AgentUpserted{AgentUpserted: testAgent("daemon-2", 1)}})
	var syncErr *directory.SyncError
	if !errors.As(err, &syncErr) || syncErr.ExpectedRevision != 8 {
		t.Fatalf("revision gap error = %v, want SyncError expected 8", err)
	}
	directoryState.Detach("connection-1")
	eventually(t, time.Second, func() bool {
		_, found, queryErr := directoryState.Edge(context.Background(), "edge-1")
		return queryErr == nil && !found
	})
	if _, found, err := directoryState.LocateDaemon(context.Background(), "daemon-1"); err != nil || found {
		t.Fatalf("detached daemon still indexed: found=%v err=%v", found, err)
	}
}

func TestControllerRestartStartsEmptyAndRebuildsFromEdgeSnapshot(t *testing.T) {
	snapshot := &cloudv1.RuntimeSnapshot{Revision: 2, Agents: []*cloudv1.AgentPresence{testAgent("daemon-rebuild", 3)}, Sessions: []*cloudv1.ClientSessionSummary{testSession("session-rebuild", "daemon-rebuild", 4)}}
	first := newDirectory(t, 0)
	attach(t, first, "connection-before-restart")
	commit(t, first, "connection-before-restart", snapshot)
	first.Close()

	restarted, err := directory.New(directory.Config{MailboxSize: 64, GracePeriod: 0})
	if err != nil {
		t.Fatalf("create restarted Directory: %v", err)
	}
	t.Cleanup(restarted.Close)
	if edges, err := restarted.ListEdges(context.Background()); err != nil || len(edges) != 0 {
		t.Fatalf("restarted Directory restored topology: edges=%v err=%v", edges, err)
	}
	attach(t, restarted, "connection-after-restart")
	commit(t, restarted, "connection-after-restart", snapshot)
	projection, found, err := restarted.Edge(context.Background(), "edge-1")
	if err != nil || !found || projection.AgentCount != 1 || projection.SessionCount != 1 {
		t.Fatalf("snapshot did not rebuild restarted Directory: %+v found=%v err=%v", projection, found, err)
	}
}

func BenchmarkDirectory100kSnapshot(b *testing.B) {
	snapshot := &cloudv1.RuntimeSnapshot{Revision: 1, Agents: make([]*cloudv1.AgentPresence, 0, 100_000)}
	for index := 0; index < 100_000; index++ {
		snapshot.Agents = append(snapshot.Agents, testAgent(fmt.Sprintf("daemon-%06d", index), 1))
	}
	for iteration := 0; iteration < b.N; iteration++ {
		directoryState, err := directory.New(directory.Config{MailboxSize: 64, GracePeriod: 0})
		if err != nil {
			b.Fatal(err)
		}
		if err := directoryState.Attach(context.Background(), directory.Attachment{EdgeID: "edge-1", BootID: "boot-1", ConnectionID: fmt.Sprintf("connection-%d", iteration), SoftwareVersion: "test"}); err != nil {
			b.Fatal(err)
		}
		commitBenchmark(b, directoryState, fmt.Sprintf("connection-%d", iteration), snapshot)
		if _, found, err := directoryState.LocateDaemon(context.Background(), "daemon-099999"); err != nil || !found {
			b.Fatalf("query 100k directory found=%v err=%v", found, err)
		}
		directoryState.Close()
	}
}

func newDirectory(t *testing.T, grace time.Duration) *directory.Directory {
	t.Helper()
	value, err := directory.New(directory.Config{MailboxSize: 128, GracePeriod: grace})
	if err != nil {
		t.Fatalf("create Directory: %v", err)
	}
	t.Cleanup(value.Close)
	return value
}

func attach(t *testing.T, value *directory.Directory, connectionID string) {
	t.Helper()
	if err := value.Attach(context.Background(), directory.Attachment{EdgeID: "edge-1", BootID: "edge-boot-1", ConnectionID: connectionID, SoftwareVersion: "test"}); err != nil {
		t.Fatalf("attach %s: %v", connectionID, err)
	}
}

func commit(t *testing.T, value *directory.Directory, connectionID string, snapshot *cloudv1.RuntimeSnapshot) {
	t.Helper()
	digest, err := runtimesnapshot.Digest(snapshot)
	if err != nil {
		t.Fatalf("digest snapshot: %v", err)
	}
	const snapshotID = "snapshot-test"
	if err := value.BeginSnapshot(context.Background(), connectionID, &cloudv1.SnapshotBegin{SnapshotId: snapshotID, Revision: snapshot.GetRevision()}); err != nil {
		t.Fatalf("begin snapshot: %v", err)
	}
	chunkCount := uint32(0)
	if len(snapshot.GetAgents()) > 0 || len(snapshot.GetSessions()) > 0 {
		if err := value.AppendSnapshot(context.Background(), connectionID, &cloudv1.SnapshotChunk{SnapshotId: snapshotID, Agents: snapshot.GetAgents(), Sessions: snapshot.GetSessions()}); err != nil {
			t.Fatalf("append snapshot: %v", err)
		}
		chunkCount = 1
	}
	if err := value.CommitSnapshot(context.Background(), connectionID, &cloudv1.SnapshotEnd{SnapshotId: snapshotID, Revision: snapshot.GetRevision(), ChunkCount: chunkCount, Digest: digest}); err != nil {
		t.Fatalf("commit snapshot: %v", err)
	}
}

func commitBenchmark(b *testing.B, value *directory.Directory, connectionID string, snapshot *cloudv1.RuntimeSnapshot) {
	b.Helper()
	digest, err := runtimesnapshot.Digest(snapshot)
	if err != nil {
		b.Fatal(err)
	}
	if err := value.BeginSnapshot(context.Background(), connectionID, &cloudv1.SnapshotBegin{SnapshotId: "benchmark", Revision: snapshot.GetRevision()}); err != nil {
		b.Fatal(err)
	}
	if err := value.AppendSnapshot(context.Background(), connectionID, &cloudv1.SnapshotChunk{SnapshotId: "benchmark", Agents: snapshot.GetAgents()}); err != nil {
		b.Fatal(err)
	}
	if err := value.CommitSnapshot(context.Background(), connectionID, &cloudv1.SnapshotEnd{SnapshotId: "benchmark", Revision: snapshot.GetRevision(), ChunkCount: 1, Digest: digest}); err != nil {
		b.Fatal(err)
	}
}

func testAgent(id string, generation uint64) *cloudv1.AgentPresence {
	return &cloudv1.AgentPresence{DaemonId: id, AccountId: "account-1", BootId: "daemon-boot", ConnectionId: "agent-connection", Generation: generation, TicketId: "ticket", TicketIssuedAt: timestamppb.New(time.Unix(1, 0))}
}

func testSession(id, daemonID string, generation uint64) *cloudv1.ClientSessionSummary {
	return &cloudv1.ClientSessionSummary{SessionId: id, AccountId: "account-1", DaemonId: daemonID, ClientId: "client-1", Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, Generation: generation}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
