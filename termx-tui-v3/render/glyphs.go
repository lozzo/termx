package render

// PaneChromeGlyphs 集中管理 pane/floating chrome 的动作和状态字形。
// 默认 chrome 对齐 tuiv2 的 Nerd Font 字形；测试仍可通过 SetPaneChromeGlyphs 覆盖。
type PaneChromeGlyphs struct {
	ActionLeft          string
	ActionLeftSet       bool
	ActionRight         string
	ActionRightSet      bool
	ActionSeparator     string
	ActionSeparatorSet  bool
	ActionGroupLeft     string
	ActionGroupLeftSet  bool
	ActionGroupRight    string
	ActionGroupRightSet bool
	OwnerLeft           string
	OwnerLeftSet        bool
	OwnerRight          string
	OwnerRightSet       bool
	Owner               string
	OwnerSet            bool
	OwnerPending        string
	OwnerPendingSet     bool
	TakeOwner           string
	TakeOwnerSet        bool
	Zoom                string
	SplitVertical       string
	SplitHorizontal     string
	Close               string
	SizeLock            string
	SizeUnlock          string
	CenterFloating      string
	CollapseFloating    string
	Running             string
	Waiting             string
	Exited              string
	Killed              string
}

var defaultPaneChromeGlyphs = PaneChromeGlyphs{
	ActionLeft:       "[",
	ActionRight:      "]",
	ActionSeparator:  "─",
	ActionGroupLeft:  "",
	ActionGroupRight: "",
	OwnerLeft:        "",
	OwnerRight:       "",
	Owner:            "◆ owner",
	OwnerPending:     "◆ owner?",
	TakeOwner:        "◇ follow",
	Zoom:             "",
	SplitVertical:    "",
	SplitHorizontal:  "",
	Close:            "",
	SizeLock:         "󰌾",
	SizeUnlock:       "󰍀",
	CenterFloating:   "",
	CollapseFloating: "",
	Running:          "",
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
	if glyphs.ActionLeftSet {
		next.ActionLeft = glyphs.ActionLeft
	}
	if glyphs.ActionRightSet {
		next.ActionRight = glyphs.ActionRight
	}
	if glyphs.ActionSeparatorSet {
		next.ActionSeparator = glyphs.ActionSeparator
	}
	if glyphs.ActionGroupLeftSet {
		next.ActionGroupLeft = glyphs.ActionGroupLeft
	}
	if glyphs.ActionGroupRightSet {
		next.ActionGroupRight = glyphs.ActionGroupRight
	}
	if glyphs.OwnerLeftSet {
		next.OwnerLeft = glyphs.OwnerLeft
	}
	if glyphs.OwnerRightSet {
		next.OwnerRight = glyphs.OwnerRight
	}
	if glyphs.OwnerSet {
		next.Owner = glyphs.Owner
	}
	if glyphs.OwnerPendingSet {
		next.OwnerPending = glyphs.OwnerPending
	}
	if glyphs.TakeOwnerSet {
		next.TakeOwner = glyphs.TakeOwner
	}
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
	if glyphs.SizeLock != "" {
		next.SizeLock = glyphs.SizeLock
	}
	if glyphs.SizeUnlock != "" {
		next.SizeUnlock = glyphs.SizeUnlock
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

func paneChromeSizeLockGlyph() string {
	return paneChromeGlyphs.SizeLock
}

func paneChromeSizeUnlockGlyph() string {
	return paneChromeGlyphs.SizeUnlock
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

func paneChromeActionLeft() string {
	return paneChromeGlyphs.ActionLeft
}

func paneChromeActionRight() string {
	return paneChromeGlyphs.ActionRight
}

func paneChromeActionSeparator() string {
	return paneChromeGlyphs.ActionSeparator
}

func paneChromeActionGroupLeft() string {
	return paneChromeGlyphs.ActionGroupLeft
}

func paneChromeActionGroupRight() string {
	return paneChromeGlyphs.ActionGroupRight
}

func paneChromeOwnerLeft() string {
	return paneChromeGlyphs.OwnerLeft
}

func paneChromeOwnerRight() string {
	return paneChromeGlyphs.OwnerRight
}

func paneChromeOwnerText() string {
	return paneChromeGlyphs.Owner
}

func paneChromeOwnerPendingText() string {
	return paneChromeGlyphs.OwnerPending
}

func paneChromeTakeOwnerText() string {
	return paneChromeGlyphs.TakeOwner
}
