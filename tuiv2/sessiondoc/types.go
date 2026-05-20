package sessiondoc

type SplitDirection string

const (
	SplitHorizontal SplitDirection = "horizontal"
	SplitVertical   SplitDirection = "vertical"
)

type Rect struct {
	X int
	Y int
	W int
	H int
}

type LayoutNode struct {
	PaneID    string
	Direction SplitDirection
	Ratio     float64
	First     *LayoutNode
	Second    *LayoutNode
}

type Doc struct {
	CurrentWorkspace string
	WorkspaceOrder   []string
	Workspaces       map[string]*Workspace
}

type Workspace struct {
	Name      string
	Tabs      []*Tab
	ActiveTab int
}

type Tab struct {
	ID              string
	Name            string
	Root            *LayoutNode
	Panes           map[string]*Pane
	Floating        []*FloatingPane
	FloatingVisible bool
	ActivePaneID    string
	ZoomedPaneID    string
	ScrollOffset    int
	LayoutPreset    int
}

type Pane struct {
	ID         string
	Title      string
	TerminalID string
}

type FloatingPane struct {
	PaneID      string
	Rect        Rect
	Z           int
	Display     string
	FitMode     string
	RestoreRect Rect
	AutoFitCols int
	AutoFitRows int
}

func New() *Doc {
	return &Doc{Workspaces: make(map[string]*Workspace)}
}

func NewLeaf(paneID string) *LayoutNode {
	return &LayoutNode{PaneID: paneID}
}

func (n *LayoutNode) IsLeaf() bool {
	return n != nil && n.First == nil && n.Second == nil
}

func (n *LayoutNode) LeafIDs() []string {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return []string{n.PaneID}
	}
	out := n.First.LeafIDs()
	out = append(out, n.Second.LeafIDs()...)
	return out
}

func (n *LayoutNode) Remove(paneID string) *LayoutNode {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		if n.PaneID == paneID {
			return nil
		}
		return n
	}
	n.First = n.First.Remove(paneID)
	n.Second = n.Second.Remove(paneID)
	switch {
	case n.First == nil:
		return n.Second
	case n.Second == nil:
		return n.First
	default:
		return n
	}
}
