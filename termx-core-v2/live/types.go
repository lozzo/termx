package live

import "strings"

// SurfaceSize describes the current host projection size.
type SurfaceSize struct {
	Cols int
	Rows int
}

// Valid reports whether the size can be used for a live surface projection.
func (s SurfaceSize) Valid() bool {
	return s.Cols > 0 && s.Rows > 0
}

type SurfaceTrack struct {
	size SurfaceSize
	rows []string
}

func NewSurfaceTrack(size SurfaceSize) *SurfaceTrack {
	if !size.Valid() {
		size = SurfaceSize{Cols: 80, Rows: 24}
	}
	return &SurfaceTrack{size: size, rows: []string{""}}
}

func (surface *SurfaceTrack) Size() SurfaceSize {
	return surface.size
}

func (surface *SurfaceTrack) Resize(size SurfaceSize) {
	if !size.Valid() {
		return
	}
	surface.size = size
	surface.trimRows()
}

func (surface *SurfaceTrack) Write(text string) {
	for _, part := range strings.SplitAfter(text, "\n") {
		if part == "" {
			continue
		}
		if strings.HasSuffix(part, "\n") {
			surface.appendText(strings.TrimSuffix(part, "\n"))
			surface.rows = append(surface.rows, "")
			continue
		}
		surface.appendText(part)
	}
	surface.trimRows()
}

func (surface *SurfaceTrack) Rows() []string {
	out := make([]string, len(surface.rows))
	copy(out, surface.rows)
	return out
}

func (surface *SurfaceTrack) appendText(text string) {
	if len(surface.rows) == 0 {
		surface.rows = append(surface.rows, "")
	}
	last := len(surface.rows) - 1
	surface.rows[last] += text
}

func (surface *SurfaceTrack) trimRows() {
	if surface.size.Rows <= 0 || len(surface.rows) <= surface.size.Rows {
		return
	}
	surface.rows = append([]string(nil), surface.rows[len(surface.rows)-surface.size.Rows:]...)
}
