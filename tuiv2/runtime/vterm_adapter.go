package runtime

import (
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func vtermScreenUpdateFromProtocol(update protocol.ScreenUpdate) localvterm.ScreenUpdate {
	out := localvterm.ScreenUpdate{
		FullReplace:      update.FullReplace,
		ResetScrollback:  update.ResetScrollback,
		Size:             localvterm.Size{Cols: update.Size.Cols, Rows: update.Size.Rows},
		ScreenScroll:     update.ScreenScroll,
		Title:            update.Title,
		Screen:           protocolScreenToVTerm(update.Screen),
		ScreenTimestamps: append([]time.Time(nil), update.ScreenTimestamps...),
		ScreenRowKinds:   append([]string(nil), update.ScreenRowKinds...),
		ScreenWrapped:    append([]bool(nil), update.ScreenWrapped...),
		Ops:              make([]localvterm.DamageOp, 0, len(update.Ops)),
		ScrollbackTrim:   update.ScrollbackTrim,
		ScrollbackAppend: make([]localvterm.ScrollbackRowAppend, 0, len(update.ScrollbackAppend)),
		Cursor:           protocolCursorToVTerm(update.Cursor),
		Modes:            protocolModesToVTerm(update.Modes),
	}
	for _, op := range update.Ops {
		out.Ops = append(out.Ops, localvterm.DamageOp{
			Code:       vtermScreenOpCodeFromProtocol(op.Code),
			Rect:       localvterm.DamageRect{X: op.Rect.X, Y: op.Rect.Y, Width: op.Rect.Width, Height: op.Rect.Height},
			Src:        localvterm.DamageRect{X: op.Src.X, Y: op.Src.Y, Width: op.Src.Width, Height: op.Src.Height},
			DstX:       op.DstX,
			DstY:       op.DstY,
			Dx:         op.Dx,
			Dy:         op.Dy,
			Row:        op.Row,
			Col:        op.Col,
			Cells:      protocolCellRowToVTerm(op.Cells),
			Size:       localvterm.Size{Cols: op.Size.Cols, Rows: op.Size.Rows},
			Timestamp:  op.Timestamp,
			RowKind:    op.RowKind,
			Wrapped:    op.Wrapped,
			WrappedSet: op.WrappedSet,
		})
	}
	for _, row := range update.ScrollbackAppend {
		out.ScrollbackAppend = append(out.ScrollbackAppend, localvterm.ScrollbackRowAppend{
			Cells:      protocolCellRowToVTerm(row.Cells),
			Timestamp:  row.Timestamp,
			RowKind:    row.RowKind,
			Wrapped:    row.Wrapped,
			WrappedSet: row.WrappedSet,
		})
	}
	return out
}

func vtermScreenOpCodeFromProtocol(code protocol.ScreenOpCode) localvterm.ScreenOpCode {
	switch code {
	case protocol.ScreenOpWriteSpan:
		return localvterm.ScreenOpWriteSpan
	case protocol.ScreenOpScrollRect:
		return localvterm.ScreenOpScrollRect
	case protocol.ScreenOpCopyRect:
		return localvterm.ScreenOpCopyRect
	case protocol.ScreenOpClearRect:
		return localvterm.ScreenOpClearRect
	case protocol.ScreenOpClearToEOL:
		return localvterm.ScreenOpClearToEOL
	case protocol.ScreenOpCursor:
		return localvterm.ScreenOpCursor
	case protocol.ScreenOpModes:
		return localvterm.ScreenOpModes
	case protocol.ScreenOpResize:
		return localvterm.ScreenOpResize
	case protocol.ScreenOpTitle:
		return localvterm.ScreenOpTitle
	default:
		return 0
	}
}
