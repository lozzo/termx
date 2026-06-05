package render

// PaneChromeGlyphs 集中管理 pane/floating chrome 的动作和状态字形。
//
// 默认值使用 Nerd Font codepoint；终端没有对应字体时，调用方可以在启动时
// 替换成 emoji 或纯 Unicode 符号，但 render 主线仍按 cell width 做裁切和命中区计算。
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
