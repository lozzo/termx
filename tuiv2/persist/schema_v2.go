package persist

type WorkspaceStateFileV2 struct {
	Version  int                         `json:"version"`
	Metadata []PersistedTerminalMetadata `json:"terminal_metadata,omitempty"`
	Data     []WorkspaceEntryV2          `json:"workspaces,omitempty"`
}

type WorkspaceEntryV2 struct {
	Name      string       `json:"name"`
	ActiveTab int          `json:"active_tab"`
	Tabs      []TabEntryV2 `json:"tabs"`
}

type TabEntryV2 struct {
	Name         string           `json:"name"`
	ActivePaneID string           `json:"active_pane_id,omitempty"`
	ZoomedPaneID string           `json:"zoomed_pane_id,omitempty"`
	LayoutPreset int              `json:"layout_preset,omitempty"`
	Layout       *LayoutNodeEntry `json:"layout,omitempty"`
	Panes        []PaneEntryV2    `json:"panes"`
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

type PersistedTerminalMetadata struct {
	TerminalID string            `json:"terminal_id"`
	Name       string            `json:"name,omitempty"`
	Command    []string          `json:"command,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
}
