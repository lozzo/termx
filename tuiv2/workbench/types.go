package workbench

type Workbench struct {
	current string
	order   []string
	store   map[string]*WorkspaceState
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

type FloatingState struct {
	PaneID string
	Rect   Rect
	Z      int
}
