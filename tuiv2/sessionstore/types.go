package sessionstore

import (
	"time"

	"github.com/lozzow/termx/tuiv2/sessiondoc"
)

type SessionInfo struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  uint64
}

type ViewInfo struct {
	ViewID              string
	SessionID           string
	ClientID            string
	ActiveWorkspaceName string
	ActiveTabID         string
	FocusedPaneID       string
	WindowCols          uint16
	WindowRows          uint16
	AttachedAt          time.Time
	UpdatedAt           time.Time
}

type LeaseInfo struct {
	TerminalID string
	SessionID  string
	ViewID     string
	PaneID     string
	AcquiredAt time.Time
}

type Snapshot struct {
	Session   SessionInfo
	Workbench *sessiondoc.Doc
	View      *ViewInfo
	Leases    []LeaseInfo
}

type CreateParams struct {
	SessionID string
	Name      string
}

type AttachParams struct {
	SessionID  string
	ClientID   string
	WindowCols uint16
	WindowRows uint16
}

type ReplaceParams struct {
	SessionID    string
	ViewID       string
	BaseRevision uint64
	Workbench    *sessiondoc.Doc
}

type UpdateViewPatch struct {
	ActiveWorkspaceName string
	ActiveTabID         string
	FocusedPaneID       string
	WindowCols          uint16
	WindowRows          uint16
}

type UpdateViewParams struct {
	SessionID string
	ViewID    string
	View      UpdateViewPatch
}

type AcquireLeaseParams struct {
	SessionID  string
	ViewID     string
	PaneID     string
	TerminalID string
}

type ReleaseLeaseParams struct {
	SessionID  string
	ViewID     string
	TerminalID string
}

type EventData struct {
	SessionID string
	Revision  uint64
	ViewID    string
	Deleted   bool
}
