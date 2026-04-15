package runtime

import (
	"maps"
	"sort"

	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared"
)

type TerminalRegistry struct {
	terminals map[string]*TerminalRuntime
}

type TerminalRuntime struct {
	TerminalID string

	// 以下 metadata 字段是从 server event / List 结果同步的派生数据，
	// 不是独立 canonical source。
	Name     string
	Command  []string
	Tags     map[string]string
	State    string
	ExitCode *int
	Title    string // OSC 2 标题，由 VTerm 回调更新

	Channel         uint16
	AttachMode      string
	Snapshot        *bridge.SnapshotRef
	SnapshotVersion uint64
	SurfaceVersion  uint64
	VTerm           VTermLike

	ScrollbackLoadedLimit  int
	ScrollbackLoadingLimit int
	ScrollbackExhausted    bool

	OwnerPaneID   string   // 全局 owner pane，用于所有视图的共享展示
	ControlPaneID string   // 当前本地视图可实际驱动 resize/control 的 pane
	BoundPaneIDs  []string // 只读派生缓存，不是第二份可写绑定真相
	// Owner release freezes further PTY resize until a view explicitly reacquires control.
	RequiresExplicitOwner bool
	// Owner handoff should force the next resize from the new owner even if the
	// local snapshot cache still happens to match the requested geometry.
	PendingOwnerResize bool
	// Local shrink previews keep the VTerm resized for subsequent output
	// parsing, but render should stay on a clipped snapshot until the terminal
	// emits a real redraw. This avoids exposing emulator-only mid-states.
	PreferSnapshot bool

	Stream   StreamState
	Recovery RecoveryState

	BootstrapPending bool
}

func NewTerminalRegistry() *TerminalRegistry {
	return &TerminalRegistry{terminals: make(map[string]*TerminalRuntime)}
}

func (r *TerminalRegistry) Get(id string) *TerminalRuntime {
	if r == nil {
		return nil
	}
	return r.terminals[id]
}

func (r *TerminalRegistry) GetOrCreate(id string) *TerminalRuntime {
	if r == nil || id == "" {
		return nil
	}
	if terminal := r.terminals[id]; terminal != nil {
		return terminal
	}
	terminal := &TerminalRuntime{TerminalID: id}
	r.terminals[id] = terminal
	return terminal
}

func (r *TerminalRegistry) IDs() []string {
	if r == nil {
		return nil
	}
	ids := make([]string, 0, len(r.terminals))
	for id := range r.terminals {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return shared.LessNumericStrings(ids[i], ids[j])
	})
	return ids
}

func (r *TerminalRegistry) SetMetadata(terminalID string, name string, tags map[string]string) {
	if r == nil || terminalID == "" {
		return
	}
	terminal := r.Get(terminalID)
	if terminal == nil {
		return
	}
	terminal.Name = name
	terminal.Tags = cloneTags(tags)
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(tags))
	maps.Copy(cloned, tags)
	return cloned
}
