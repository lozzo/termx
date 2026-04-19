package runtime

import "slices"

type DetachedTerminalBinding struct {
	Name     string
	Command  []string
	Tags     map[string]string
	State    string
	ExitCode *int
}

func (r *Runtime) BindDetachedTerminal(paneID, terminalID string, binding DetachedTerminalBinding) *TerminalRuntime {
	if r == nil || r.registry == nil || paneID == "" || terminalID == "" {
		return nil
	}
	terminal := r.registry.GetOrCreate(terminalID)
	if terminal == nil {
		return nil
	}
	terminal.Name = binding.Name
	if len(binding.Command) > 0 {
		terminal.Command = slices.Clone(binding.Command)
	}
	terminal.Tags = cloneTags(binding.Tags)
	terminal.State = binding.State
	terminal.ExitCode = cloneExitCode(binding.ExitCode)
	terminal.Channel = 0
	terminal.AttachMode = ""
	terminal.BoundPaneIDs = appendBoundPaneID(terminal.BoundPaneIDs, paneID)
	r.clearTerminalLocalControl(terminal, paneID, false)
	r.syncTerminalOwnership(terminal)
	r.touch()
	return terminal
}
