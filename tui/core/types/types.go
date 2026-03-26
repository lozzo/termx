package types

type WorkspaceID string
type TabID string
type PaneID string
type TerminalID string

type Rect struct {
	X int
	Y int
	W int
	H int
}

func (r Rect) Right() int {
	return r.X + r.W
}

func (r Rect) Bottom() int {
	return r.Y + r.H
}

type SplitDirection string

const (
	SplitHorizontal SplitDirection = "horizontal"
	SplitVertical   SplitDirection = "vertical"
)

type PaneSlotState string

const (
	PaneSlotUnconnected PaneSlotState = "unconnected"
	PaneSlotLive        PaneSlotState = "live"
	PaneSlotExited      PaneSlotState = "exited"
)

type PaneKind string

const (
	PaneKindTiled    PaneKind = "tiled"
	PaneKindFloating PaneKind = "floating"
)

type ConnectionRole string

const (
	ConnectionRoleOwner    ConnectionRole = "owner"
	ConnectionRoleFollower ConnectionRole = "follower"
)
