package render

// PaneChromeGlyphs 集中管理 pane/floating chrome 的动作和状态字形。
// 默认值只使用基础 Unicode；Nerd Font 状态体系必须等语义和 fallback 拍板后再启用。
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
	Zoom:             "□",
	SplitVertical:    "↔",
	SplitHorizontal:  "↕",
	Close:            "×",
	CenterFloating:   "◎",
	CollapseFloating: "⌃",
	Running:          "●",
	Waiting:          "○",
	Exited:           "×",
	Killed:           "×",
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

func paneChromeCloseGlyph() string {
	return paneChromeGlyphs.Close
}

func paneChromeZoomGlyph() string {
	return paneChromeGlyphs.Zoom
}

func paneChromeSplitVerticalGlyph() string {
	return paneChromeGlyphs.SplitVertical
}

func paneChromeSplitHorizontalGlyph() string {
	return paneChromeGlyphs.SplitHorizontal
}

func paneChromeFloatingCenterGlyph() string {
	return paneChromeGlyphs.CenterFloating
}

func paneChromeFloatingCollapseGlyph() string {
	return paneChromeGlyphs.CollapseFloating
}

func paneChromeRunningGlyph() string {
	return paneChromeGlyphs.Running
}

func paneChromeWaitingGlyph() string {
	return paneChromeGlyphs.Waiting
}

func paneChromeExitedGlyph() string {
	return paneChromeGlyphs.Exited
}
