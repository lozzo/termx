package render

// Frame 是 FrameSink 使用的线性输出适配结果。
//
// Render framework 的主输出是 RenderResult；Frame 只保留给现有
// TerminalHost、测试和 CLI smoke 适配层使用。
type Frame struct {
	Lines       []string
	StyledLines []Line
	ANSILines   []string
	Cursor      Cursor
	Blink       bool
	Metadata    RenderMetadata
}

// Clone 返回 detached frame，防止测试或 host 共享修改 frame lines。
func (frame Frame) Clone() Frame {
	cloned := Frame{
		Cursor:   frame.Cursor,
		Blink:    frame.Blink,
		Metadata: frame.Metadata,
	}
	if len(frame.Lines) > 0 {
		cloned.Lines = cloneStrings(frame.Lines)
	}
	if len(frame.ANSILines) > 0 {
		cloned.ANSILines = cloneStrings(frame.ANSILines)
	}
	if len(frame.StyledLines) > 0 {
		cloned.StyledLines = make([]Line, len(frame.StyledLines))
		for i, line := range frame.StyledLines {
			cloned.StyledLines[i] = line.Clone()
		}
	}
	return cloned
}

func FrameFromRenderResult(result RenderResult) Frame {
	return Frame{
		Lines:       result.Lines(),
		StyledLines: result.StyledLines(),
		ANSILines:   result.ANSILines(),
		Cursor:      result.Cursor,
		Blink:       result.Blink,
		Metadata:    result.Metadata,
	}
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

// FrameSink 把渲染帧写入 host、recorder 或 test sink。
type FrameSink interface {
	WriteFrame(Frame) error
}
