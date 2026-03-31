package runtime

import localvterm "github.com/lozzow/termx/vterm"

type BindingRole string

const (
	BindingRoleOwner    BindingRole = "owner"
	BindingRoleFollower BindingRole = "follower"
)

type StreamState struct {
	Active     bool
	Stop       func()
	RetryCount int
}

type RecoveryState struct {
	SyncLost     bool
	DroppedBytes uint64
}

type VTermLike interface {
	Write(data []byte) (int, error)
	LoadSnapshot(screen localvterm.ScreenData, cursor localvterm.CursorState, modes localvterm.TerminalModes)
	Resize(cols, rows int)
	Size() (int, int)
	ScreenContent() localvterm.ScreenData
	ScrollbackContent() [][]localvterm.Cell
	CursorState() localvterm.CursorState
	Modes() localvterm.TerminalModes
	SetDefaultColors(fg, bg string)
	SetIndexedColor(index int, value string)
}

type Option func(*Runtime)

type VTermFactory func(channel uint16) VTermLike
