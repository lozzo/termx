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
	CursorRect  Rect
	Blink       bool
	HitRegions  []HitRegion
	Metadata    RenderMetadata
	Theme       Theme
}

// Clone 返回 detached frame，防止测试或 host 共享修改 frame lines。
func (frame Frame) Clone() Frame {
	cloned := Frame{
		Cursor:     frame.Cursor,
		CursorRect: frame.CursorRect,
		Blink:      frame.Blink,
		Metadata:   frame.Metadata,
		Theme:      frame.Theme,
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
	if len(frame.HitRegions) > 0 {
		cloned.HitRegions = cloneHitRegions(frame.HitRegions)
	}
	return cloned
}

func FrameFromRenderResult(result RenderResult) Frame {
	return Frame{
		Lines:       result.Lines(),
		StyledLines: result.Content,
		ANSILines:   result.ANSILines(),
		Cursor:      result.Cursor,
		CursorRect:  result.CursorRect,
		Blink:       result.Blink,
		HitRegions:  cloneHitRegions(result.HitRegions),
		Metadata:    result.Metadata,
		Theme:       result.Theme.WithFallback(),
	}
}

func ANSIFrameFromRenderResult(result RenderResult) Frame {
	return Frame{
		ANSILines:  result.ANSILines(),
		Cursor:     result.Cursor,
		CursorRect: result.CursorRect,
		Blink:      result.Blink,
		HitRegions: cloneHitRegions(result.HitRegions),
		Metadata:   result.Metadata,
		Theme:      result.Theme.WithFallback(),
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

// FrameSinkPreference 允许真实 host 声明自己只需要 ANSI 输出。
// 测试 recorder 不实现该接口时保留完整 Frame，方便继续断言 plain/styled 内容。
type FrameSinkPreference interface {
	NeedsCompleteFrame() bool
}
