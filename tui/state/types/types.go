package types

// 这一层只放跨状态包共享的基础类型，避免 workspace/layout/terminal 相互绕依赖。

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

func (r Rect) Empty() bool {
	return r.W <= 0 || r.H <= 0
}

type SplitDirection string

const (
	SplitDirectionVertical   SplitDirection = "vertical"
	SplitDirectionHorizontal SplitDirection = "horizontal"
)

func (d SplitDirection) Normalize() SplitDirection {
	switch d {
	case SplitDirectionHorizontal:
		return SplitDirectionHorizontal
	case SplitDirectionVertical:
		fallthrough
	default:
		return SplitDirectionVertical
	}
}

type PaneKind string

const (
	PaneKindTiled    PaneKind = "tiled"
	PaneKindFloating PaneKind = "floating"
)

type PaneSlotState string

const (
	PaneSlotLive        PaneSlotState = "live"
	PaneSlotExited      PaneSlotState = "exited"
	PaneSlotUnconnected PaneSlotState = "unconnected"
)
