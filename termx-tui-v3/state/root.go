package state

// Root is the reducer-owned TUI-v3 state root.
type Root struct {
	Generation uint64
}

// Advance returns a copy with an incremented generation.
func (r Root) Advance() Root {
	r.Generation++
	return r
}
