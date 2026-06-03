package history

// LogicalLineID identifies one logical line in the history track.
type LogicalLineID uint64

// SealState describes whether a logical line can still receive appended cells.
type SealState string

const (
	SealStateOpen   SealState = "open"
	SealStateSealed SealState = "sealed"
)

// LogicalLine is the minimum domain object required before storage,
// projection, and mutation semantics are added in later slices.
type LogicalLine struct {
	ID    LogicalLineID
	Seal  SealState
	Dirty bool
}

// LogicalLineStore is the single history truth. The first slice only defines
// the contract shape; later slices will add the in-memory implementation.
type LogicalLineStore interface {
	Line(LogicalLineID) (LogicalLine, bool)
}
