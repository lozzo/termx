package history

import "errors"

// LogicalLineID identifies one logical line in the history track.
type LogicalLineID uint64

// Generation changes when a line, index, frontier, or future history track
// changes in a way that should invalidate stale projections or cursors.
type Generation uint64

// SealState describes whether a logical line can still receive appended cells.
type SealState string

const (
	SealStateOpen   SealState = "open"
	SealStateSealed SealState = "sealed"
)

// Residency describes where the logical line payload currently lives.
// It is not a mutability state: persisted lines can still be replaced or
// deleted by history semantics such as truncate or clear scrollback.
type Residency string

const (
	ResidencyMemory  Residency = "memory"
	ResidencyFile    Residency = "file"
	ResidencyMmap    Residency = "mmap"
	ResidencyEvicted Residency = "evicted"
)

// Cell 是 logical line 的历史 payload 单元。Text 是内容，Width/Style/Link 是
// 终端格式语义；plain text 只能由这些 cells 派生，不能反过来成为渲染 truth。
type Cell struct {
	Text       string
	Width      int
	Style      CellStyle
	LinkURL    string
	LinkParams string
}

type CellStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

// LogicalLine is the single payload model for committed history and mutable
// frontier membership.
type LogicalLine struct {
	ID         LogicalLineID
	Generation Generation
	Seal       SealState
	Cells      []Cell
	Dirty      bool
	Residency  Residency
}

// Clone returns a detached copy so callers cannot mutate store state through
// shared slices.
func (line LogicalLine) Clone() LogicalLine {
	line.Cells = cloneCells(line.Cells)
	return line
}

// CreateLineRequest describes the initial payload and orthogonal state for a
// new logical line.
type CreateLineRequest struct {
	Seal      SealState
	Cells     []Cell
	Dirty     bool
	Residency Residency
}

var (
	ErrInvalidLineID    = errors.New("invalid logical line id")
	ErrInvalidSeal      = errors.New("invalid logical line seal state")
	ErrInvalidResidency = errors.New("invalid logical line residency")
	ErrUnknownLine      = errors.New("unknown logical line")
	ErrDuplicateLineID  = errors.New("duplicate logical line id")
)

// LogicalLineStore is the single history truth.
type LogicalLineStore interface {
	CreateLine(CreateLineRequest) (LogicalLine, error)
	Line(LogicalLineID) (LogicalLine, bool)
	ReplaceLine(LogicalLine) (LogicalLine, error)
	DeleteLine(LogicalLineID) bool
	LineIDs() []LogicalLineID
}

func normalizeSeal(seal SealState) (SealState, error) {
	if seal == "" {
		return SealStateOpen, nil
	}
	switch seal {
	case SealStateOpen, SealStateSealed:
		return seal, nil
	default:
		return "", ErrInvalidSeal
	}
}

func normalizeResidency(residency Residency) (Residency, error) {
	if residency == "" {
		return ResidencyMemory, nil
	}
	switch residency {
	case ResidencyMemory, ResidencyFile, ResidencyMmap, ResidencyEvicted:
		return residency, nil
	default:
		return "", ErrInvalidResidency
	}
}

func validateLine(line LogicalLine) error {
	if line.ID == 0 {
		return ErrInvalidLineID
	}
	if _, err := normalizeSeal(line.Seal); err != nil {
		return err
	}
	if _, err := normalizeResidency(line.Residency); err != nil {
		return err
	}
	return nil
}

func normalizeLine(line LogicalLine) (LogicalLine, error) {
	if line.ID == 0 {
		return LogicalLine{}, ErrInvalidLineID
	}
	seal, err := normalizeSeal(line.Seal)
	if err != nil {
		return LogicalLine{}, err
	}
	residency, err := normalizeResidency(line.Residency)
	if err != nil {
		return LogicalLine{}, err
	}
	line.Seal = seal
	line.Residency = residency
	line.Cells = cloneCells(line.Cells)
	return line, nil
}

func cloneCells(cells []Cell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	cloned := make([]Cell, len(cells))
	copy(cloned, cells)
	return cloned
}
