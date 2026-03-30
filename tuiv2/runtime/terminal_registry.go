package runtime

import (
	"maps"
	"slices"
	"sort"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
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

	Channel    uint16
	AttachMode string
	Snapshot   *bridge.SnapshotRef
	VTerm      VTermLike

	OwnerPaneID  string   // 只读派生缓存，不是第二份可写绑定真相
	BoundPaneIDs []string // 只读派生缓存，不是第二份可写绑定真相

	Stream   StreamState
	Recovery RecoveryState
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
	sort.Strings(ids)
	return ids
}

func (r *TerminalRegistry) UpsertTerminalInfo(info protocol.TerminalInfo) *TerminalRuntime {
	terminal := r.GetOrCreate(info.ID)
	if terminal == nil {
		return nil
	}
	terminal.Name = info.Name
	terminal.Command = slices.Clone(info.Command)
	terminal.Tags = cloneTags(info.Tags)
	terminal.State = info.State
	if info.ExitCode != nil {
		exitCode := *info.ExitCode
		terminal.ExitCode = &exitCode
	} else {
		terminal.ExitCode = nil
	}
	return terminal
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(tags))
	maps.Copy(cloned, tags)
	return cloned
}
