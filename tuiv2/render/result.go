package render

import "strings"

type PresentMetadata struct {
	OwnerMap [][]uint32
}

type RenderResult struct {
	Lines  []string
	Cursor string
	Blink  bool
	Meta   *PresentMetadata
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
	result.Meta = clonePresentMetadata(result.Meta)
	return result
}

func clonePresentMetadata(meta *PresentMetadata) *PresentMetadata {
	if meta == nil {
		return nil
	}
	clone := &PresentMetadata{
		OwnerMap: make([][]uint32, len(meta.OwnerMap)),
	}
	for y := range meta.OwnerMap {
		if len(meta.OwnerMap[y]) == 0 {
			continue
		}
		clone.OwnerMap[y] = append([]uint32(nil), meta.OwnerMap[y]...)
	}
	return clone
}
