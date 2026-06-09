package protocol

func RowOwnershipIsCommitted(value string) bool {
	switch value {
	case RowOwnershipPersisted, RowOwnershipLiveTailReclaimed:
		return true
	default:
		return false
	}
}

func RowOwnershipIsLiveTailLive(value string) bool {
	return value == RowOwnershipLiveTailLive
}

func RowOwnershipIsKnown(value string) bool {
	switch value {
	case RowOwnershipPersisted, RowOwnershipLiveTailReclaimed, RowOwnershipLiveTailLive, RowOwnershipScreen:
		return true
	default:
		return false
	}
}

func HasExplicitRowOwnership(ownership []string, rows int) bool {
	if rows <= 0 || len(ownership) < rows {
		return false
	}
	for i := 0; i < rows; i++ {
		if !RowOwnershipIsKnown(ownership[i]) {
			return false
		}
	}
	return true
}

func CountCommittedRowOwnership(ownership []string, rows int) int {
	if rows <= 0 || len(ownership) == 0 {
		return 0
	}
	if rows > len(ownership) {
		rows = len(ownership)
	}
	count := 0
	for i := 0; i < rows; i++ {
		if RowOwnershipIsCommitted(ownership[i]) {
			count++
		}
	}
	return count
}

func CountCommittedRowOwnershipRange(ownership []string, start, end int) int {
	if start < 0 {
		start = 0
	}
	if end > len(ownership) {
		end = len(ownership)
	}
	if start >= end {
		return 0
	}
	count := 0
	for i := start; i < end; i++ {
		if RowOwnershipIsCommitted(ownership[i]) {
			count++
		}
	}
	return count
}

func HasOnlyLiveTailLiveOwnership(ownership []string, rows int) bool {
	if !HasExplicitRowOwnership(ownership, rows) {
		return false
	}
	for i := 0; i < rows; i++ {
		if !RowOwnershipIsLiveTailLive(ownership[i]) {
			return false
		}
	}
	return true
}

func SnapshotCommittedLoadedDepth(snapshot *Snapshot) int {
	if snapshot == nil {
		return 0
	}
	if snapshot.ScrollbackLoadedRows > 0 && snapshotHasAuthoritativeLoadedRows(snapshot) {
		return snapshot.ScrollbackLoadedRows
	}
	rows := len(snapshot.Scrollback)
	if !HasExplicitRowOwnership(snapshot.ScrollbackOwnership, rows) {
		return 0
	}
	committedRows := CountCommittedRowOwnership(snapshot.ScrollbackOwnership, rows)
	loaded := snapshot.ScrollbackOffset + committedRows
	if committedRows <= 0 {
		return loaded
	}
	if canonicalRows := SnapshotCanonicalCommittedRows(snapshot); canonicalRows > loaded {
		return canonicalRows
	}
	return loaded
}

func snapshotHasAuthoritativeLoadedRows(snapshot *Snapshot) bool {
	if snapshot == nil {
		return false
	}
	if SnapshotCanonicalCommittedRows(snapshot) > 0 {
		return true
	}
	rows := len(snapshot.Scrollback)
	if !HasExplicitRowOwnership(snapshot.ScrollbackOwnership, rows) {
		return false
	}
	return CountCommittedRowOwnership(snapshot.ScrollbackOwnership, rows) > 0
}

func SnapshotCanonicalCommittedRows(snapshot *Snapshot) int {
	if snapshot == nil || snapshot.HistoryGeneration == 0 || snapshot.ScrollbackLastRowID < snapshot.ScrollbackFirstRowID {
		return 0
	}
	rows := snapshot.ScrollbackLastRowID - snapshot.ScrollbackFirstRowID + 1
	maxInt := int(^uint(0) >> 1)
	if rows > uint64(maxInt) {
		return maxInt
	}
	return int(rows)
}

func SnapshotHasCanonicalCommittedWindow(snapshot *Snapshot) bool {
	if snapshot == nil || SnapshotCanonicalCommittedRows(snapshot) <= 0 {
		return false
	}
	return SnapshotCommittedLoadedDepth(snapshot) > 0
}
