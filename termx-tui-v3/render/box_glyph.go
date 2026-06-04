package render

const (
	boxConnUp = 1 << iota
	boxConnDown
	boxConnLeft
	boxConnRight
)

type boxStyle struct {
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
	Horizontal  string
	Vertical    string
}

var (
	roundedBoxStyle = boxStyle{
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "╰",
		BottomRight: "╯",
		Horizontal:  "─",
		Vertical:    "│",
	}
	squareBoxStyle = boxStyle{
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "└",
		BottomRight: "┘",
		Horizontal:  "─",
		Vertical:    "│",
	}
)

var boxGlyphConnections = map[string]uint8{
	"│": boxConnUp | boxConnDown,
	"─": boxConnLeft | boxConnRight,
	"┌": boxConnDown | boxConnRight,
	"┐": boxConnDown | boxConnLeft,
	"└": boxConnUp | boxConnRight,
	"┘": boxConnUp | boxConnLeft,
	"├": boxConnUp | boxConnDown | boxConnRight,
	"┤": boxConnUp | boxConnDown | boxConnLeft,
	"┬": boxConnDown | boxConnLeft | boxConnRight,
	"┴": boxConnUp | boxConnLeft | boxConnRight,
	"┼": boxConnUp | boxConnDown | boxConnLeft | boxConnRight,
}

var boxConnectionGlyph = map[uint8]string{
	boxConnUp:                                            "│",
	boxConnDown:                                          "│",
	boxConnLeft:                                          "─",
	boxConnRight:                                         "─",
	boxConnUp | boxConnDown:                              "│",
	boxConnLeft | boxConnRight:                           "─",
	boxConnDown | boxConnRight:                           "┌",
	boxConnDown | boxConnLeft:                            "┐",
	boxConnUp | boxConnRight:                             "└",
	boxConnUp | boxConnLeft:                              "┘",
	boxConnUp | boxConnDown | boxConnRight:               "├",
	boxConnUp | boxConnDown | boxConnLeft:                "┤",
	boxConnDown | boxConnLeft | boxConnRight:             "┬",
	boxConnUp | boxConnLeft | boxConnRight:               "┴",
	boxConnUp | boxConnDown | boxConnLeft | boxConnRight: "┼",
}

func boxConnectionsForGlyph(glyph string) (uint8, bool) {
	connections, ok := boxGlyphConnections[glyph]
	return connections, ok
}

func boxGlyphForConnections(connections uint8) (string, bool) {
	glyph, ok := boxConnectionGlyph[connections]
	return glyph, ok
}
