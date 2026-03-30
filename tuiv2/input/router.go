package input

import tea "github.com/charmbracelet/bubbletea"

// RouteResult is the output of the router for a single input event.
// Exactly one of Action or TerminalInput is non-nil for a meaningful result;
// both nil means the event was consumed / ignored.
type RouteResult struct {
	// Action is set when the key triggered a semantic TUI action.
	Action *SemanticAction
	// TerminalInput is set when the key should be forwarded to the active terminal.
	TerminalInput *TerminalInput
}

// Router translates raw input events into semantic actions or terminal
// passthrough payloads. It owns the current input mode and a keymap.
type Router struct {
	mode   ModeState
	keymap Keymap
}

// NewRouter creates a Router in ModeNormal with the default keymap.
func NewRouter() *Router {
	return &Router{
		mode:   ModeState{Kind: ModeNormal},
		keymap: DefaultKeymap(),
	}
}

// NewRouterWithKeymap creates a Router with a custom keymap (useful for tests).
func NewRouterWithKeymap(km Keymap) *Router {
	return &Router{
		mode:   ModeState{Kind: ModeNormal},
		keymap: km,
	}
}

// Mode returns the current mode state (read-only snapshot).
func (r *Router) Mode() ModeState {
	return r.mode
}

// SetMode updates the current mode.
func (r *Router) SetMode(mode ModeState) {
	r.mode = mode
}

// RouteKeyMsg is the primary entry point: it routes a bubbletea KeyMsg
// according to the current mode and keymap.
func (r *Router) RouteKeyMsg(msg tea.KeyMsg) RouteResult {
	return TranslateKeyMsg(msg, r.mode.Kind, &r.keymap)
}

// RouteKey is the legacy stub kept for interface stability.
func (r *Router) RouteKey(any) RouteResult {
	return RouteResult{}
}

// RouteRaw is a stub for raw-byte routing (not yet implemented).
func (r *Router) RouteRaw([]byte) RouteResult {
	return RouteResult{}
}

// RouteEvent is a stub for generic event routing (not yet implemented).
func (r *Router) RouteEvent(any) RouteResult {
	return RouteResult{}
}
