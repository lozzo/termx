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
	Zoom:             "\ueb01", // nf-cod-screen_full
	SplitVertical:    "\ueb56", // nf-cod-split_vertical
	SplitHorizontal:  "\ueb57", // nf-cod-split_horizontal
	Close:            "\uea76", // nf-cod-close
	CenterFloating:   "\uebb4", // nf-cod-target
	CollapseFloating: "\ueab6", // nf-cod-chevron_up
	Running:          "\uea71", // nf-cod-debug_start
	Waiting:          "\ueb32", // nf-cod-circle_large_outline
	Exited:           "\uea87", // nf-cod-error
	Killed:           "\uea87", // nf-cod-error
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
