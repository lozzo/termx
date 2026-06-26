package history

// LogicalLineStore owns logical-line payload truth. Indexes, journal records
// and storage backends may reference ids from it, but must not duplicate truth.
type LogicalLineStore interface {
	// Put inserts a new logical line payload into the authoritative store.
	Put(line LogicalLine) error
	// Get returns the current payload for a logical line id if it is retained.
	Get(id LogicalLineID) (LogicalLine, bool)
	// Update replaces payload for an existing logical line and must bump the
	// appropriate generation before callers expose a new history window.
	Update(line LogicalLine) error
	// Delete removes payload only after no committed index, frontier or frame
	// journal entry still references it.
	Delete(id LogicalLineID) error
}

// CommittedHistoryIndex owns ordinary/final-frame ordering. It stores ids and
// cursor boundaries only; payload remains in LogicalLineStore.
type CommittedHistoryIndex interface {
	// Append adds an already-owned logical line id to committed history order.
	Append(id LogicalLineID) error
	// RemoveRange drops complete logical lines from committed order for truncate
	// or retention without mutating active frontier payload.
	RemoveRange(first LogicalLineID, last LogicalLineID) error
	// Boundary returns the current segment-aware committed cursor boundary.
	Boundary() HistoryBoundary
}

// MutableFrontier tracks logical lines still owned by current primary history
// semantics. It is an index/state boundary, not a second payload store.
type MutableFrontier interface {
	// OpenLine returns the active append line if terminal semantics still own one.
	OpenLine() (LogicalLineID, bool)
	// Reclaim moves complete committed suffix lines back under mutable ownership.
	Reclaim(ids []LogicalLineID) error
	// Hide moves still-mutable visible lines into hidden frontier ownership.
	Hide(ids []LogicalLineID) error
	// Seal marks a line as no longer accepting append writes while it may still
	// remain under primary screen ownership.
	Seal(id LogicalLineID) error
}

// ScreenFrameJournal indexes current and archived fixed-grid frames. Frame
// payload lines still live in LogicalLineStore.
type ScreenFrameJournal interface {
	// PublishCurrent replaces the current frame for its session or transient alt
	// segment without ordinary-committing the prior repaint.
	PublishCurrent(frame ScreenFrame) error
	// Archive records an explicit boundary frame such as alt enter.
	Archive(record FrameRecord) error
	// Current returns the latest current frame for a session if one is active.
	Current(sessionID ScreenSessionID) (ScreenFrame, bool)
	// Older walks archived frame records before returning to ordinary committed
	// history; cursor segment must remain explicit.
	Older(cursor HistoryCursor, limit int) ([]FrameRecord, HistoryCursor, error)
}

// StorageBackend 只负责 residency/recovery；mutability policy 属于 store/projector。
type StorageBackend interface {
	// Apply persists or updates store/index/journal residency records atomically
	// for one history generation.
	Apply(tx StorageTransaction) error
	// Recover rebuilds logical store, committed index, frontier and frame journal
	// metadata without replaying raw PTY bytes or reading live snapshots.
	Recover() (RecoveredHistoryState, error)
	// Compact reclaims storage records that are no longer referenced by any
	// authoritative history structure.
	Compact(policy StorageCompactionPolicy) error
}

// StorageTransaction is the storage-layer view of a history generation update.
// It cannot decide whether a persisted line is mutable or immutable.
type StorageTransaction struct {
	Generation Generation
	Lines      []LogicalLine
	Tombstones []LogicalLineID
	Committed  []LogicalLineID
	Frames     []FrameRecord
}

// RecoveredHistoryState is the complete state needed to restore history truth.
// Recovering only row payload is not enough for infinite history semantics.
type RecoveredHistoryState struct {
	Generation Generation
	Lines      []LogicalLine
	Committed  []LogicalLineID
	Frontier   []LogicalLineID
	Frames     []FrameRecord
}

// StorageCompactionPolicy bounds backend cleanup. It is a storage concern, not
// the policy that decides which history content is visible.
type StorageCompactionPolicy struct {
	MaxFrames int
	MaxBytes  int64
}

// InfiniteHistoryStore 是 authoritative history truth 的外部 contract。
type InfiniteHistoryStore interface {
	// ApplyMutation applies a projector transaction as the single write path into
	// authoritative history truth.
	ApplyMutation(mutation HistoryMutation) error
	// ApplyOrdinaryEvent applies a low-level ordinary event for focused harnesses;
	// production should prefer projector mutations once R303 is complete.
	ApplyOrdinaryEvent(event HistoryEvent) error

	// OpenScreenSession creates primary screen app session state owned by history.
	OpenScreenSession(params ScreenSessionParams) (ScreenSessionID, error)
	// PublishPrimaryFrame publishes current primary fixed-grid content without
	// growing ordinary history depth.
	PublishPrimaryFrame(session ScreenSessionID, frame ScreenFrame) error
	// ArchivePrimaryFrame records an explicit primary frame boundary such as alt
	// enter or retention policy.
	ArchivePrimaryFrame(session ScreenSessionID, frame ScreenFrame, reason ArchiveReason) error
	// PublishAltFrame publishes current alt-screen content as selectable transient
	// state without ordinary commit.
	PublishAltFrame(frame ScreenFrame) error
	// CloseScreenSession resolves active session state according to close policy.
	CloseScreenSession(session ScreenSessionID, policy ClosePolicy) error

	// LatestWindow returns the authoritative latest projection for a terminal.
	LatestWindow(req HistoryWindowRequest) (HistoryWindow, error)
	// OlderWindow pages across current/archive/committed segments using cursor
	// truth from a previous response.
	OlderWindow(req HistoryWindowRequest) (HistoryWindow, error)
	// Freeze creates a tokenized copy/history boundary immune to later repaint.
	Freeze(req FreezeHistoryRequest) (FrozenHistorySnapshot, error)
	// Copy returns selected text from authoritative history payload.
	Copy(req HistoryCopyRequest) (string, error)
	// Release drops a frozen token or other history window resource.
	Release(token HistoryToken) error
}
