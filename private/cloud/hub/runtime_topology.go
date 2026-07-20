package hub

import (
	"sort"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func cloneManagedPeerSessions(source []*cloudpb.ManagedPeerSessionProjection) []*cloudpb.ManagedPeerSessionProjection {
	result := make([]*cloudpb.ManagedPeerSessionProjection, 0, len(source))
	for _, session := range source {
		if session != nil {
			result = append(result, proto.Clone(session).(*cloudpb.ManagedPeerSessionProjection))
		}
	}
	return result
}

func cloneTerminalAccessInventory(source *cloudpb.TerminalAccessInventorySnapshot) *cloudpb.TerminalAccessInventorySnapshot {
	if source == nil {
		return nil
	}
	return proto.Clone(source).(*cloudpb.TerminalAccessInventorySnapshot)
}

func sortTopologySnapshot(snapshot *cloudpb.HubTopologySnapshot) {
	sort.Slice(snapshot.Presences, func(left, right int) bool {
		return snapshot.Presences[left].GetDaemonDeviceId() < snapshot.Presences[right].GetDaemonDeviceId()
	})
	sort.Slice(snapshot.PeerSessions, func(left, right int) bool {
		leftTarget := snapshot.PeerSessions[left].GetTarget()
		rightTarget := snapshot.PeerSessions[right].GetTarget()
		if leftTarget.GetDaemonDeviceId() != rightTarget.GetDaemonDeviceId() {
			return leftTarget.GetDaemonDeviceId() < rightTarget.GetDaemonDeviceId()
		}
		if leftTarget.GetManagedSessionId() != rightTarget.GetManagedSessionId() {
			return leftTarget.GetManagedSessionId() < rightTarget.GetManagedSessionId()
		}
		return leftTarget.GetSessionIncarnation() < rightTarget.GetSessionIncarnation()
	})
	sort.Slice(snapshot.TerminalAccessInventories, func(left, right int) bool {
		return snapshot.TerminalAccessInventories[left].GetDaemonDeviceId() < snapshot.TerminalAccessInventories[right].GetDaemonDeviceId()
	})
}
