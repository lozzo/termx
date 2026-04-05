package workbench

type Workbench struct {
	current string
	order   []string
	store   map[string]*WorkspaceState

	version        uint64
	visibleRect    Rect
	visibleVersion uint64
	visibleCache   *VisibleWorkbench
}

type WorkspaceState struct {
	Name      string
	Tabs      []*TabState
	ActiveTab int
}

type TabState struct {
	ID              string
	Name            string
	Root            *LayoutNode
	Panes           map[string]*PaneState
	Floating        []*FloatingState
	FloatingVisible bool
	ActivePaneID    string
	ZoomedPaneID    string
	ScrollOffset    int
	LayoutPreset    int
}

type PaneState struct {
	ID         string
	Title      string
	TerminalID string // 唯一可写绑定真相
}

type FloatingDisplayState string

const (
	FloatingDisplayExpanded  FloatingDisplayState = "expanded"
	FloatingDisplayCollapsed FloatingDisplayState = "collapsed"
	FloatingDisplayHidden    FloatingDisplayState = "hidden"
)

type FloatingFitMode string

const (
	FloatingFitManual FloatingFitMode = "manual"
	FloatingFitAuto   FloatingFitMode = "auto"
)

type FloatingState struct {
	PaneID      string
	Rect        Rect
	Z           int
	Display     FloatingDisplayState
	FitMode     FloatingFitMode
	RestoreRect Rect
	AutoFitCols int
	AutoFitRows int
}
