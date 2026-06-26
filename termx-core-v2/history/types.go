// Package history defines the logical-line payload model.
package history

// LogicalLineID identifies one logical line in the authoritative history model.
type LogicalLineID uint64

// Generation marks payload changes. The rebuilt history implementation will
// decide how generations are assigned and exposed to consumers.
type Generation uint64

// SealState describes whether a logical line may still receive semantic writes.
type SealState string

const (
	SealStateOpen   SealState = "open"
	SealStateSealed SealState = "sealed"
)

// Residency describes where the logical line payload lives. It is not a
// mutability flag; the rebuilt model owns mutability separately.
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

// CellStyle stores terminal style tokens for one cell. Empty FG/BG means
// terminal default color; the viewer resolves defaults with its current theme.
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

// RowTailFill 表达 terminal 物理行从已有内容末尾到 visual row 行尾的背景。
// 它不属于 logical text，也不增加 logical line 宽度。
type RowTailFill struct {
	Style CellStyle
}

// LogicalLine 是 history 重建后唯一允许复用的旧定义：历史 truth 的基本单位
// 仍是 logical line，不是 visual row、screen row、scrollback row 或 raw bytes。
type LogicalLine struct {
	ID                LogicalLineID
	Generation        Generation
	CreatedGeneration Generation
	ContentGeneration Generation
	Seal              SealState
	Kind              string
	Cells             []Cell
	TailFill          *RowTailFill
	Dirty             bool
	Residency         Residency
}
