package runtime

func (r *Runtime) SetTerminalMetadata(terminalID, name string, tags map[string]string) {
	if r == nil || r.registry == nil || terminalID == "" {
		return
	}
	r.registry.SetMetadata(terminalID, name, tags)
	r.invalidate()
}
