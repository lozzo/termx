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
	Schema            string              `json:"schema"`
	SchemaVersion     int                 `json:"schemaVersion"`
	Workspace         WorkspaceState      `json:"workspace"`
	Workspaces        []WorkspaceState    `json:"workspaces"`
	Floatings         []FloatingPaneState `json:"floatings,omitempty"`
	ActiveFloatingID  string              `json:"activeFloatingId,omitempty"`
	PanelPresentation PanelPresentation   `json:"panelPresentation"`
	ActivePaneID      string              `json:"activePaneId"`
	ZoomedPaneID      string              `json:"zoomedPaneId,omitempty"`
	HeaderVisible     bool                `json:"headerVisible"`
	FooterVisible     bool                `json:"footerVisible"`
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

func SnapshotWorkbenchForStorage(shell ShellStore) WorkbenchStorageSnapshot {
	shell = shell.EnsureDefaults()
	return WorkbenchStorageSnapshot{
		Schema:            WorkbenchStorageSchema,
		SchemaVersion:     WorkbenchStorageSchemaV1,
		Workspace:         cloneWorkspace(shell.Workspace),
		Workspaces:        cloneWorkspaces(shell.Workspaces),
		Floatings:         cloneFloatings(shell.Floatings),
		ActiveFloatingID:  shell.ActiveFloatingID,
		PanelPresentation: shell.PanelPresentation,
		ActivePaneID:      shell.ActivePaneID,
		ZoomedPaneID:      shell.ZoomedPaneID,
		HeaderVisible:     shell.HeaderVisible,
		FooterVisible:     shell.FooterVisible,
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

func (snapshot WorkbenchStorageSnapshot) Validate() error {
	if snapshot.Schema != WorkbenchStorageSchema || snapshot.SchemaVersion != WorkbenchStorageSchemaV1 {
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
	return nil
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
