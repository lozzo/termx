package state

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	WorkbenchStorageAppID       = "termx-tui-v3"
	WorkbenchStorageScopePublic = "public"
	WorkbenchStorageKeyRoot     = "workbench/root"
	WorkbenchStorageSchema      = "termx.tui.v3.workbench"
	WorkbenchStorageSchemaV1    = 1
	WorkbenchStorageSchemaV2    = 2
)

var ErrInvalidWorkbenchSnapshot = errors.New("invalid workbench snapshot")

type WorkbenchStorageRef struct {
	AppID   string
	Scope   string
	OwnerID string
	Key     string
	Version uint64
}

type WorkbenchStorageSnapshot struct {
	Schema            string                `json:"schema"`
	SchemaVersion     int                   `json:"schemaVersion"`
	Workspace         WorkspaceState        `json:"workspace"`
	Workspaces        []WorkspaceState      `json:"workspaces"`
	Floatings         []FloatingPaneState   `json:"floatings,omitempty"`
	ActiveFloatingID  string                `json:"activeFloatingId,omitempty"`
	PanelPresentation PanelPresentation     `json:"panelPresentation"`
	ActivePaneID      string                `json:"activePaneId"`
	ZoomedPaneID      string                `json:"zoomedPaneId,omitempty"`
	HeaderVisible     bool                  `json:"headerVisible"`
	FooterVisible     bool                  `json:"footerVisible"`
	TerminalViews     []TerminalViewBinding `json:"terminalViews,omitempty"`
}

func DefaultWorkbenchStorageRef(workspaceID string) WorkbenchStorageRef {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = DefaultWorkspaceID
	}
	return WorkbenchStorageRef{
		AppID:   WorkbenchStorageAppID,
		Scope:   WorkbenchStorageScopePublic,
		OwnerID: workspaceID,
		Key:     WorkbenchStorageKeyRoot,
	}
}

func (ref WorkbenchStorageRef) WithVersion(version uint64) WorkbenchStorageRef {
	ref.Version = version
	return ref
}

func (ref WorkbenchStorageRef) KeyPrefix() string {
	key := strings.TrimSpace(ref.Key)
	if key == "" {
		return "workbench/"
	}
	if strings.HasSuffix(key, "/") {
		return key
	}
	if slash := strings.LastIndex(key, "/"); slash >= 0 {
		return key[:slash+1]
	}
	return key
}

func SnapshotWorkbenchForStorage(shell ShellStore) WorkbenchStorageSnapshot {
	return SnapshotRootWorkbenchForStorage(Root{Shell: shell})
}

func SnapshotRootWorkbenchForStorage(root Root) WorkbenchStorageSnapshot {
	shell := root.Shell.EnsureDefaults()
	return WorkbenchStorageSnapshot{
		Schema:            WorkbenchStorageSchema,
		SchemaVersion:     WorkbenchStorageSchemaV2,
		Workspace:         cloneWorkspace(shell.Workspace),
		Workspaces:        cloneWorkspaces(shell.Workspaces),
		Floatings:         cloneFloatings(shell.Floatings),
		ActiveFloatingID:  shell.ActiveFloatingID,
		PanelPresentation: shell.PanelPresentation,
		ActivePaneID:      shell.ActivePaneID,
		ZoomedPaneID:      shell.ZoomedPaneID,
		HeaderVisible:     shell.HeaderVisible,
		FooterVisible:     shell.FooterVisible,
		TerminalViews:     root.TerminalViews.Bindings(),
	}
}

func (snapshot WorkbenchStorageSnapshot) ToShellStore() (ShellStore, error) {
	if err := snapshot.Validate(); err != nil {
		return ShellStore{}, err
	}
	shell := ShellStore{
		Workspace:         cloneWorkspace(snapshot.Workspace),
		Workspaces:        cloneWorkspaces(snapshot.Workspaces),
		Floatings:         cloneFloatings(snapshot.Floatings),
		ActiveFloatingID:  snapshot.ActiveFloatingID,
		PanelPresentation: snapshot.PanelPresentation,
		ActivePaneID:      snapshot.ActivePaneID,
		ZoomedPaneID:      snapshot.ZoomedPaneID,
		HeaderVisible:     snapshot.HeaderVisible,
		FooterVisible:     snapshot.FooterVisible,
		initialized:       true,
	}
	return shell.EnsureDefaults(), nil
}

func (snapshot WorkbenchStorageSnapshot) ToTerminalViewStore() (TerminalViewStore, error) {
	if err := snapshot.Validate(); err != nil {
		return TerminalViewStore{}, err
	}
	store := TerminalViewStore{}
	for _, binding := range snapshot.TerminalViews {
		if binding.FloatingID != "" {
			store = store.BindFloating(binding)
			continue
		}
		store = store.BindPane(binding)
	}
	return store, nil
}

func (snapshot WorkbenchStorageSnapshot) Validate() error {
	if snapshot.Schema != WorkbenchStorageSchema || snapshot.SchemaVersion != WorkbenchStorageSchemaV2 {
		return ErrInvalidWorkbenchSnapshot
	}
	if snapshot.Workspace.ID == "" {
		return ErrInvalidWorkbenchSnapshot
	}
	if len(snapshot.Workspaces) == 0 {
		return ErrInvalidWorkbenchSnapshot
	}
	if snapshot.PanelPresentation != PanelPresentationCard && snapshot.PanelPresentation != PanelPresentationSplitLine {
		return ErrInvalidWorkbenchSnapshot
	}
	if !snapshot.validTerminalViews() {
		return ErrInvalidWorkbenchSnapshot
	}
	return nil
}

func (snapshot WorkbenchStorageSnapshot) validTerminalViews() bool {
	seen := map[string]struct{}{}
	for _, binding := range snapshot.TerminalViews {
		if binding.ViewID == "" || binding.TerminalID == "" || binding.PaneID == "" && binding.FloatingID == "" {
			return false
		}
		if _, ok := seen[binding.ViewID]; ok {
			return false
		}
		seen[binding.ViewID] = struct{}{}
	}
	return true
}

func EncodeWorkbenchStorageSnapshot(shell ShellStore) ([]byte, error) {
	return json.Marshal(SnapshotWorkbenchForStorage(shell))
}

func EncodeWorkbenchStorageSnapshotValue(snapshot WorkbenchStorageSnapshot) ([]byte, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(snapshot)
}

func DecodeWorkbenchStorageSnapshot(data []byte) (WorkbenchStorageSnapshot, error) {
	var snapshot WorkbenchStorageSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return WorkbenchStorageSnapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return WorkbenchStorageSnapshot{}, err
	}
	return snapshot, nil
}
