package termx

import (
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
)

func trimSnapshotBinaryResultToFrameBudget(snapshot *Snapshot, encoded []byte, budget int) (*Snapshot, []byte) {
	if snapshot == nil || budget <= 0 || len(encoded) <= budget || len(snapshot.Scrollback) == 0 {
		return snapshot, encoded
	}
	low, high := 0, len(snapshot.Scrollback)
	var best *Snapshot
	var bestEncoded []byte
	for low <= high {
		keep := (low + high) / 2
		candidate := snapshotWithScrollbackTail(snapshot, keep)
		data, err := protocol.EncodeSnapshotPayload(protocolSnapshotFromCore(candidate))
		if err != nil {
			break
		}
		if len(data) <= budget {
			best = candidate
			bestEncoded = data
			low = keep + 1
			continue
		}
		high = keep - 1
	}
	if best != nil {
		return best, bestEncoded
	}
	trimmed := snapshotWithScrollbackTail(snapshot, 0)
	data, err := protocol.EncodeSnapshotPayload(protocolSnapshotFromCore(trimmed))
	if err != nil || len(data) > len(encoded) {
		return snapshot, encoded
	}
	return trimmed, data
}

func snapshotWithScrollbackTail(snapshot *Snapshot, keep int) *Snapshot {
	if snapshot == nil {
		return nil
	}
	out := *snapshot
	rowCount := len(snapshot.Scrollback)
	if keep < 0 {
		keep = 0
	}
	if keep > rowCount {
		keep = rowCount
	}
	trim := rowCount - keep
	if trim <= 0 {
		return &out
	}
	out.ScrollbackHasMore = out.ScrollbackHasMore || rowCount > 0
	if trim >= rowCount {
		out.Scrollback = nil
		out.ScrollbackTimestamps = nil
		out.ScrollbackRowKinds = nil
		out.ScrollbackWrapped = nil
		return &out
	}
	out.Scrollback = snapshot.Scrollback[trim:]
	out.ScrollbackTimestamps = trimTimeMetadataHead(snapshot.ScrollbackTimestamps, trim)
	out.ScrollbackRowKinds = trimStringMetadataHead(snapshot.ScrollbackRowKinds, trim)
	out.ScrollbackWrapped = trimBoolMetadataHead(snapshot.ScrollbackWrapped, trim)
	return &out
}

func trimTimeMetadataHead(values []time.Time, trim int) []time.Time {
	if len(values) == 0 {
		return nil
	}
	if trim >= len(values) {
		return nil
	}
	return values[trim:]
}

func trimStringMetadataHead(values []string, trim int) []string {
	if len(values) == 0 {
		return nil
	}
	if trim >= len(values) {
		return nil
	}
	return values[trim:]
}

func trimBoolMetadataHead(values []bool, trim int) []bool {
	if len(values) == 0 {
		return nil
	}
	if trim >= len(values) {
		return nil
	}
	return values[trim:]
}

func trimGridViewportBinaryResultToFrameBudget(viewport *protocol.GridViewport, encoded []byte, budget int) (*protocol.GridViewport, []byte) {
	if viewport == nil || budget <= 0 || len(encoded) <= budget || len(viewport.Rows) == 0 {
		return viewport, encoded
	}
	low, high := 0, len(viewport.Rows)
	var best *protocol.GridViewport
	var bestEncoded []byte
	for low <= high {
		keep := (low + high) / 2
		candidate := protocolGridViewportWithRowTail(viewport, keep)
		data, err := protocol.EncodeGridViewportPayload(candidate)
		if err != nil {
			break
		}
		if len(data) <= budget {
			best = candidate
			bestEncoded = data
			low = keep + 1
			continue
		}
		high = keep - 1
	}
	if best != nil {
		return best, bestEncoded
	}
	trimmed := protocolGridViewportWithRowTail(viewport, 0)
	data, err := protocol.EncodeGridViewportPayload(trimmed)
	if err != nil || len(data) > len(encoded) {
		return viewport, encoded
	}
	return trimmed, data
}

func protocolGridViewportWithRowTail(viewport *protocol.GridViewport, keep int) *protocol.GridViewport {
	if viewport == nil {
		return nil
	}
	out := *viewport
	rowCount := len(viewport.Rows)
	if keep < 0 {
		keep = 0
	}
	if keep > rowCount {
		keep = rowCount
	}
	trim := rowCount - keep
	if trim <= 0 {
		return &out
	}
	out.ScrollbackHasMore = out.ScrollbackHasMore || rowCount > 0
	if trim >= rowCount {
		out.Rows = nil
		out.ScrollbackTimestamps = nil
		out.ScrollbackRowKinds = nil
		out.ScrollbackWrapped = nil
		return &out
	}
	out.Rows = viewport.Rows[trim:]
	out.ScrollbackTimestamps = trimTimeMetadataHead(viewport.ScrollbackTimestamps, trim)
	out.ScrollbackRowKinds = trimStringMetadataHead(viewport.ScrollbackRowKinds, trim)
	out.ScrollbackWrapped = trimBoolMetadataHead(viewport.ScrollbackWrapped, trim)
	return &out
}
