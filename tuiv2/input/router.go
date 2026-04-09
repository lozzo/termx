package input

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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
	now    func() time.Time
	repeat repeatedRootShortcutState
}

type repeatedRootShortcutState struct {
	active   bool
	keyType  tea.KeyType
	alt      bool
	deadline time.Time
}

const repeatedRootShortcutPassthroughWindow = 250 * time.Millisecond

// NewRouter creates a Router in ModeNormal with the default keymap.
func NewRouter() *Router {
	return newRouterWithClock(DefaultKeymap(), time.Now)
}

// NewRouterWithKeymap creates a Router with a custom keymap (useful for tests).
func NewRouterWithKeymap(km Keymap) *Router {
	return newRouterWithClock(km, time.Now)
}

func newRouterWithClock(km Keymap, now func() time.Time) *Router {
	if now == nil {
		now = time.Now
	}
	return &Router{
		mode:   ModeState{Kind: ModeNormal},
		keymap: km,
		now:    now,
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

// TryRepeatedPassthrough converts a quickly repeated root shortcut into
// terminal input so applications can still receive their native Ctrl bindings.
// It also clears stale repeat state when another key breaks the sequence.
func (r *Router) TryRepeatedPassthrough(msg tea.KeyMsg) RouteResult {
	if r == nil {
		return RouteResult{}
	}
	if !r.isRepeatableRootShortcut(msg) {
		r.clearRepeatedRootShortcut()
		return RouteResult{}
	}
	if !r.repeat.active {
		return RouteResult{}
	}
	if !r.sameRepeatedRootShortcut(msg) {
		r.clearRepeatedRootShortcut()
		return RouteResult{}
	}
	now := r.nowTime()
	if now.After(r.repeat.deadline) {
		r.clearRepeatedRootShortcut()
		return RouteResult{}
	}
	inputMsg := terminalInputForKeyMsg(msg)
	r.armRepeatedRootShortcut(msg, now)
	return RouteResult{TerminalInput: inputMsg}
}

// RouteKeyMsg is the primary entry point: it routes a bubbletea KeyMsg
// according to the current mode and keymap.
func (r *Router) RouteKeyMsg(msg tea.KeyMsg) RouteResult {
	if result := r.TryRepeatedPassthrough(msg); result.Action != nil || result.TerminalInput != nil {
		return result
	}
	result := TranslateKeyMsg(msg, r.mode.Kind, &r.keymap)
	r.observeRouteResult(msg, result)
	return result
}

func (r *Router) observeRouteResult(msg tea.KeyMsg, result RouteResult) {
	if r == nil || result.Action == nil {
		return
	}
	action := r.keymap.LookupNormal(msg)
	if action == nil || action.Kind != result.Action.Kind || msg.Type == tea.KeyRunes {
		return
	}
	r.armRepeatedRootShortcut(msg, r.nowTime())
}

func (r *Router) isRepeatableRootShortcut(msg tea.KeyMsg) bool {
	return msg.Type != tea.KeyRunes && r.keymap.LookupNormal(msg) != nil
}

func (r *Router) sameRepeatedRootShortcut(msg tea.KeyMsg) bool {
	return r.repeat.keyType == msg.Type && r.repeat.alt == msg.Alt
}

func (r *Router) armRepeatedRootShortcut(msg tea.KeyMsg, now time.Time) {
	r.repeat = repeatedRootShortcutState{
		active:   true,
		keyType:  msg.Type,
		alt:      msg.Alt,
		deadline: now.Add(repeatedRootShortcutPassthroughWindow),
	}
}

func (r *Router) clearRepeatedRootShortcut() {
	r.repeat = repeatedRootShortcutState{}
}

func (r *Router) nowTime() time.Time {
	if r == nil || r.now == nil {
		return time.Now()
	}
	return r.now()
}
