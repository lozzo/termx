package app

import "github.com/lozzow/termx/internal/protocol"

func legacyProtocolSnapshotWindowFixture(snapshot *protocol.Snapshot, offset, limit int) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := cloneSnapshot(snapshot)
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	rows := len(snapshot.Scrollback)
	end := rows - offset
	if end < 0 {
		end = 0
	}
	start := 0
	if limit > 0 && end > limit {
		start = end - limit
	}
	cloned.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[start:end])
	cloned.ScrollbackOffset = offset
	cloned.ScrollbackHasMore = start > 0
	return cloned
}
