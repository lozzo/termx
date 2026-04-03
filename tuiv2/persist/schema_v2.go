package persist

type WorkspaceStateFileV2 struct {
	Version int                `json:"version"`
	Data    []WorkspaceEntryV2 `json:"workspaces,omitempty"`
}

type WorkspaceEntryV2 struct {
	Name      string       `json:"name"`
	ActiveTab int          `json:"active_tab"`
	Tabs      []TabEntryV2 `json:"tabs"`
}

type TabEntryV2 struct {
	Name         string            `json:"name"`
	ActivePaneID string            `json:"active_pane_id,omitempty"`
	ZoomedPaneID string            `json:"zoomed_pane_id,omitempty"`
	LayoutPreset int               `json:"layout_preset,omitempty"`
	Layout       *LayoutNodeEntry  `json:"layout,omitempty"`
	Panes        []PaneEntryV2     `json:"panes"`
	Floating     []FloatingEntryV2 `json:"floating,omitempty"`
}

type LayoutNodeEntry struct {
	PaneID    string           `json:"pane_id,omitempty"`
	Direction string           `json:"direction,omitempty"`
	Ratio     float64          `json:"ratio,omitempty"`
	First     *LayoutNodeEntry `json:"first,omitempty"`
	Second    *LayoutNodeEntry `json:"second,omitempty"`
}

type PaneEntryV2 struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	TerminalID string `json:"terminal_id,omitempty"`
}

type FloatingEntryV2 struct {
	PaneID string      `json:"pane_id"`
	Rect   RectEntryV2 `json:"rect"`
	Z      int         `json:"z,omitempty"`
}

type RectEntryV2 struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}
