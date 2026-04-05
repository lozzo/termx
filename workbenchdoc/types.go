package workbenchdoc

type SplitDirection string

const (
	SplitHorizontal SplitDirection = "horizontal"
	SplitVertical   SplitDirection = "vertical"
)

type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type LayoutNode struct {
	PaneID    string         `json:"pane_id,omitempty"`
	Direction SplitDirection `json:"direction,omitempty"`
	Ratio     float64        `json:"ratio,omitempty"`
	First     *LayoutNode    `json:"first,omitempty"`
	Second    *LayoutNode    `json:"second,omitempty"`
}

type Doc struct {
	CurrentWorkspace string                `json:"current_workspace,omitempty"`
	WorkspaceOrder   []string              `json:"workspace_order,omitempty"`
	Workspaces       map[string]*Workspace `json:"workspaces,omitempty"`
}

type Workspace struct {
	Name      string `json:"name"`
	Tabs      []*Tab `json:"tabs,omitempty"`
	ActiveTab int    `json:"active_tab"`
}

type Tab struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Root            *LayoutNode      `json:"root,omitempty"`
	Panes           map[string]*Pane `json:"panes,omitempty"`
	Floating        []*FloatingPane  `json:"floating,omitempty"`
	FloatingVisible bool             `json:"floating_visible,omitempty"`
	ActivePaneID    string           `json:"active_pane_id,omitempty"`
	ZoomedPaneID    string           `json:"zoomed_pane_id,omitempty"`
	ScrollOffset    int              `json:"scroll_offset,omitempty"`
	LayoutPreset    int              `json:"layout_preset,omitempty"`
}

type Pane struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	TerminalID string `json:"terminal_id,omitempty"`
}

type FloatingPane struct {
	PaneID string `json:"pane_id"`
	Rect   Rect   `json:"rect"`
	Z      int    `json:"z,omitempty"`
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
