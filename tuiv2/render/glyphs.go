package render

// PaneChromeGlyphs collects the pane-frame lifecycle and action glyphs in one
// place so callers can swap them as a set during startup.
type PaneChromeGlyphs struct {
	Zoom             string
	SplitVertical    string
	SplitHorizontal  string
	Close            string
	CenterFloating   string
	CollapseFloating string
	Running          string
	Waiting          string
	Exited           string
	Killed           string
}

var defaultPaneChromeGlyphs = PaneChromeGlyphs{
	Zoom:             "\uf065", // nf-fa-expand
	SplitVertical:    "\ueb71", // nf-cod-split-vertical   (left/right panes)
	SplitHorizontal:  "\ueb6f", // nf-cod-split-horizontal (top/bottom panes)
	Close:            "\uf00d", // nf-fa-times
	CenterFloating:   "\uf05b", // nf-fa-crosshairs
	CollapseFloating: "\uf068", // nf-fa-minus
	Running:          "●",
	Waiting:          "○",
	Exited:           "✕", // U+2715, 1-column multiplication x
	Killed:           "✕", // U+2715
}

var paneChromeGlyphs = defaultPaneChromeGlyphs

func DefaultPaneChromeGlyphs() PaneChromeGlyphs {
	return defaultPaneChromeGlyphs
}

func SetPaneChromeGlyphs(glyphs PaneChromeGlyphs) {
	next := defaultPaneChromeGlyphs
	if glyphs.Zoom != "" {
		next.Zoom = glyphs.Zoom
	}
	if glyphs.SplitVertical != "" {
		next.SplitVertical = glyphs.SplitVertical
	}
	if glyphs.SplitHorizontal != "" {
		next.SplitHorizontal = glyphs.SplitHorizontal
	}
	if glyphs.Close != "" {
		next.Close = glyphs.Close
	}
	if glyphs.CenterFloating != "" {
		next.CenterFloating = glyphs.CenterFloating
	}
	if glyphs.CollapseFloating != "" {
		next.CollapseFloating = glyphs.CollapseFloating
	}
	if glyphs.Running != "" {
		next.Running = glyphs.Running
	}
	if glyphs.Waiting != "" {
		next.Waiting = glyphs.Waiting
	}
	if glyphs.Exited != "" {
		next.Exited = glyphs.Exited
	}
	if glyphs.Killed != "" {
		next.Killed = glyphs.Killed
	}
	paneChromeGlyphs = next
}

func ResetPaneChromeGlyphs() {
	paneChromeGlyphs = defaultPaneChromeGlyphs
}
