package terminal

import "github.com/lozzow/termx/tui/state/types"

type State string

const (
	StateRunning State = "running"
	StateExited  State = "exited"
)

type Metadata struct {
	ID      types.TerminalID
	Name    string
	Command []string
	Tags    map[string]string
	State   State
}

type BindingSnapshot struct {
	TerminalID   types.TerminalID
	TerminalName string
	Command      []string
	Tags         map[string]string
}

func SnapshotFromMetadata(meta Metadata) BindingSnapshot {
	return BindingSnapshot{
		TerminalID:   meta.ID,
		TerminalName: meta.Name,
		Command:      append([]string(nil), meta.Command...),
		Tags:         cloneTags(meta.Tags),
	}
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}
