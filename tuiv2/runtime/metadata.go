package runtime

import "github.com/lozzow/termx/termx-core/protocol"

func (r *Runtime) SetTerminalMetadata(terminalID, name string, tags map[string]string) {
	if r == nil || r.registry == nil || terminalID == "" {
		return
	}
	r.registry.SetMetadata(terminalID, name, tags)
	r.invalidate()
}

func (r *Runtime) ApplyTerminalList(terminals []protocol.TerminalInfo) {
	if r == nil || r.registry == nil {
		return
	}
	for _, info := range terminals {
		r.applyTerminalInfo(info)
	}
	r.invalidate()
}

func (r *Runtime) applyTerminalInfo(info protocol.TerminalInfo) {
	if r == nil || r.registry == nil || info.ID == "" {
		return
	}
	terminal := r.registry.GetOrCreate(info.ID)
	if terminal == nil {
		return
	}
	terminal.Name = info.Name
	terminal.Command = append([]string(nil), info.Command...)
	terminal.Tags = cloneTags(info.Tags)
	terminal.State = info.State
	terminal.ExitCode = cloneExitCode(info.ExitCode)
}
