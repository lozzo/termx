package render

import "strings"

type RenderResult struct {
	Lines  []string
	Cursor string
	Blink  bool
}

func (r RenderResult) Frame() string {
	return strings.Join(r.Lines, "\n")
}

func (r RenderResult) CursorSequence() string {
	if r.Cursor == "" {
		return hideCursorANSI()
	}
	return r.Cursor
}

func cloneRenderResult(result RenderResult) RenderResult {
	result.Lines = append([]string(nil), result.Lines...)
	return result
}
