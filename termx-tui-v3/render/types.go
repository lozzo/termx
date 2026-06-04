package render

// Frame 是 FrameSink 使用的线性输出适配结果。
//
// Render framework 的主输出是 RenderResult；Frame 只保留给现有
// TerminalHost、测试和 CLI smoke 适配层使用。
type Frame struct {
	Lines []string
}

// Clone 返回 detached frame，防止测试或 host 共享修改 frame lines。
func (frame Frame) Clone() Frame {
	if len(frame.Lines) == 0 {
		return Frame{}
	}
	lines := make([]string, len(frame.Lines))
	copy(lines, frame.Lines)
	return Frame{Lines: lines}
}

func FrameFromRenderResult(result RenderResult) Frame {
	return Frame{Lines: result.Lines()}
}

// FrameSink 把渲染帧写入 host、recorder 或 test sink。
type FrameSink interface {
	WriteFrame(Frame) error
}
