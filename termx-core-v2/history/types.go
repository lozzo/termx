// Package history 定义 logical-line payload model。
package history

// LogicalLineID 标识 authoritative history model 中的一条 logical line。
type LogicalLineID uint64

// Generation 标记 payload 变化；重建后的 history 实现负责决定 generation 如何分配
// 并暴露给 consumer。
type Generation uint64

// SealState 描述 logical line 是否仍可接收 semantic writes。
type SealState string

const (
	SealStateOpen   SealState = "open"
	SealStateSealed SealState = "sealed"
)

// Residency 描述 logical line payload 的驻留位置。它不是 mutability flag；
// mutability 由重建后的模型单独负责。
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

// CellRun 是 logical line 内部的 compact payload 表示。
// domain owner 仍是 history logical line；truth source 是 vterm semantic
// WriteSpan run。它只用于 ordinary sealed-line 热路径，HistoryWindow 对外仍按
// Cells 展开，避免 TUI/protocol 获得第二套 history truth。
type CellRun struct {
	Text       string
	Style      CellStyle
	LinkURL    string
	LinkParams string
}

// CellStyle 保存单个 cell 的 terminal style token。空 FG/BG 表示 terminal default
// color；viewer 按当前主题解析默认色。
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
	Runs              []CellRun
	TailFill          *RowTailFill
	ScreenCols        int
	// ScreenRow 只对 fixed-grid screen-frame payload 有意义，表示该 logical line
	// 在生成它的 terminal frame 中的物理行号。truth source 是 FrameJournal 的
	// LogicalLineDraft.Row；ordinary history 不应依赖该字段。
	ScreenRow    int
	ScreenRowSet bool
	Dirty        bool
	Residency    Residency
}
