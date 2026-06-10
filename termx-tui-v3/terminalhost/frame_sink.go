package terminalhost

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/lozzow/termx/termx-tui-v3/render"
)

const (
	cursorHome              = "\x1b[H"
	clearScreen             = "\x1b[2J"
	clearLine               = "\x1b[2K"
	cursorShapeBlock        = "\x1b[2 q"
	cursorShapeBar          = "\x1b[6 q"
	synchronizedOutputBegin = "\x1b[?2026h"
	synchronizedOutputEnd   = "\x1b[?2026l"
)

// FrameSink 把 Renderer 生成的 frame 直接写到宿主输出。
type FrameSink struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewFrameSink(writer io.Writer) *FrameSink {
	return &FrameSink{writer: writer}
}

func (sink *FrameSink) WriteFrame(frame render.Frame) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var builder strings.Builder
	builder.WriteString(synchronizedOutputBegin)
	builder.WriteString(hideCursor)
	builder.WriteString(cursorHome)
	builder.WriteString(clearScreen)
	lines := frame.ANSILines
	if len(lines) == 0 {
		lines = frame.Lines
	}
	for i, line := range lines {
		// 满宽 frame 写到最后一列后继续输出换行，会在部分终端触发额外自动换行；
		// 每行绝对定位可以保持 pane 竖向边框连续。
		builder.WriteString(cursorPosition(i+1, 1))
		builder.WriteString(clearLine)
		builder.WriteString(line)
		builder.WriteString(render.ANSIReset)
	}
	builder.WriteString(render.ANSIReset)
	builder.WriteString(frameCursorSequence(frame))
	builder.WriteString(synchronizedOutputEnd)
	_, err := io.WriteString(sink.writer, builder.String())
	return err
}

func frameCursorSequence(frame render.Frame) string {
	if (!frame.Cursor.Visible && !frame.Cursor.Anchor) || frame.CursorRect.W <= 0 || frame.CursorRect.H <= 0 {
		return hideCursor
	}
	// 中文说明：参考 tuiv2 的最终光标复投经验。v3 已经有全局 CursorRect，
	// 因此只在真实 virtual cursor 可见时显示宿主光标；anchor-only 仍隐藏但停靠。
	sequence := cursorShapeSequence(frame.Cursor.Shape) + cursorPosition(frame.CursorRect.Y+1, frame.CursorRect.X+1)
	if frame.Cursor.Visible {
		return sequence + showCursor
	}
	return hideCursor + sequence
}

func cursorShapeSequence(shape render.CursorShape) string {
	switch shape {
	case render.CursorShapeBar:
		return cursorShapeBar
	default:
		return cursorShapeBlock
	}
}

func cursorPosition(row int, col int) string {
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	return fmt.Sprintf("\x1b[%d;%dH", row, col)
}
