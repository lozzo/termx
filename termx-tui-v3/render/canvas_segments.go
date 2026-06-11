package render

import xansi "github.com/charmbracelet/x/ansi"

type canvasSegment struct {
	text       string
	width      int
	style      StyleToken
	ansiStyle  ANSICellStyle
	linkURL    string
	linkParams string
	owner      string
	layer      LayerKind
	terminal   bool
	safe       bool
}

func cellSegments(text string, style StyleToken, owner string, layer LayerKind) []canvasSegment {
	text = xansi.Strip(SafeLine(text))
	segments := make([]canvasSegment, 0, DisplayWidth(text))
	for len(text) > 0 {
		cluster, width := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		if width < 0 {
			width = 0
		}
		if width > 0 {
			segments = append(segments, canvasSegment{
				text:  cluster,
				width: width,
				style: style,
				owner: owner,
				layer: layer,
				safe:  true,
			})
		}
		text = text[len(cluster):]
	}
	return segments
}

func cellsFromSegments(segments []canvasSegment) []Cell {
	if len(segments) == 0 {
		return nil
	}
	cells := make([]Cell, len(segments))
	for i, segment := range segments {
		cells[i] = Cell{
			Text:            segment.text,
			Width:           segment.width,
			Style:           segment.style,
			ANSIStyle:       segment.ansiStyle,
			LinkURL:         segment.linkURL,
			LinkParams:      segment.linkParams,
			TerminalContent: segment.terminal,
			Safe:            segment.safe,
		}
	}
	return cells
}
