package termx

import (
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-vterm/vterm"
)

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
