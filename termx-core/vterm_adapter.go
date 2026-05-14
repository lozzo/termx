package termx

import (
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-vterm/vterm"
)

func vtermScreenUpdateFromProtocol(update protocol.ScreenUpdate) vterm.ScreenUpdate {
	out := vterm.ScreenUpdate{
		FullReplace:      update.FullReplace,
		ResetScrollback:  update.ResetScrollback,
		Size:             vterm.Size{Cols: update.Size.Cols, Rows: update.Size.Rows},
		ScreenScroll:     update.ScreenScroll,
		Title:            update.Title,
		Screen:           protocolScreenToVTerm(update.Screen),
		ScreenTimestamps: append([]time.Time(nil), update.ScreenTimestamps...),
		ScreenRowKinds:   append([]string(nil), update.ScreenRowKinds...),
		ScreenWrapped:    append([]bool(nil), update.ScreenWrapped...),
		Ops:              make([]vterm.DamageOp, 0, len(update.Ops)),
		ScrollbackTrim:   update.ScrollbackTrim,
		ScrollbackAppend: make([]vterm.ScrollbackRowAppend, 0, len(update.ScrollbackAppend)),
		Cursor:           protocolCursorToVTerm(update.Cursor),
		Modes:            protocolModesToVTerm(update.Modes),
	}
	for _, op := range update.Ops {
		out.Ops = append(out.Ops, vterm.DamageOp{
			Code:       vtermScreenOpCodeFromProtocol(op.Code),
			Rect:       vterm.DamageRect{X: op.Rect.X, Y: op.Rect.Y, Width: op.Rect.Width, Height: op.Rect.Height},
			Src:        vterm.DamageRect{X: op.Src.X, Y: op.Src.Y, Width: op.Src.Width, Height: op.Src.Height},
			DstX:       op.DstX,
			DstY:       op.DstY,
			Dx:         op.Dx,
			Dy:         op.Dy,
			Row:        op.Row,
			Col:        op.Col,
			Cells:      protocolCellRowToVTermRow(op.Cells),
			Size:       vterm.Size{Cols: op.Size.Cols, Rows: op.Size.Rows},
			Timestamp:  op.Timestamp,
			RowKind:    op.RowKind,
			Wrapped:    op.Wrapped,
			WrappedSet: op.WrappedSet,
		})
	}
	for _, row := range update.ScrollbackAppend {
		out.ScrollbackAppend = append(out.ScrollbackAppend, vterm.ScrollbackRowAppend{
			Cells:      protocolCellRowToVTermRow(row.Cells),
			Timestamp:  row.Timestamp,
			RowKind:    row.RowKind,
			Wrapped:    row.Wrapped,
			WrappedSet: row.WrappedSet,
		})
	}
	return out
}

func vtermScreenOpCodeFromProtocol(code protocol.ScreenOpCode) vterm.ScreenOpCode {
	switch code {
	case protocol.ScreenOpWriteSpan:
		return vterm.ScreenOpWriteSpan
	case protocol.ScreenOpScrollRect:
		return vterm.ScreenOpScrollRect
	case protocol.ScreenOpCopyRect:
		return vterm.ScreenOpCopyRect
	case protocol.ScreenOpClearRect:
		return vterm.ScreenOpClearRect
	case protocol.ScreenOpClearToEOL:
		return vterm.ScreenOpClearToEOL
	case protocol.ScreenOpCursor:
		return vterm.ScreenOpCursor
	case protocol.ScreenOpModes:
		return vterm.ScreenOpModes
	case protocol.ScreenOpResize:
		return vterm.ScreenOpResize
	case protocol.ScreenOpTitle:
		return vterm.ScreenOpTitle
	default:
		return 0
	}
}

func protocolScreenOpCodeFromVTerm(code vterm.ScreenOpCode) protocol.ScreenOpCode {
	switch code {
	case vterm.ScreenOpWriteSpan:
		return protocol.ScreenOpWriteSpan
	case vterm.ScreenOpScrollRect:
		return protocol.ScreenOpScrollRect
	case vterm.ScreenOpCopyRect:
		return protocol.ScreenOpCopyRect
	case vterm.ScreenOpClearRect:
		return protocol.ScreenOpClearRect
	case vterm.ScreenOpClearToEOL:
		return protocol.ScreenOpClearToEOL
	case vterm.ScreenOpCursor:
		return protocol.ScreenOpCursor
	case vterm.ScreenOpModes:
		return protocol.ScreenOpModes
	case vterm.ScreenOpResize:
		return protocol.ScreenOpResize
	case vterm.ScreenOpTitle:
		return protocol.ScreenOpTitle
	default:
		return 0
	}
}
