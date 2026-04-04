package workbench

// VisibleWorkbench 是 render 层消费的只读投影。
type VisibleWorkbench struct {
	WorkspaceName  string
	WorkspaceCount int
	Tabs           []VisibleTab
	ActiveTab      int
	FloatingPanes  []VisiblePane
}

type VisibleTab struct {
	ID           string
	Name         string
	Panes        []VisiblePane
	ActivePaneID string
	ZoomedPaneID string
	ScrollOffset int
}

type VisiblePane struct {
	ID         string
	Title      string
	TerminalID string
	Rect       Rect
	Floating   bool
}
