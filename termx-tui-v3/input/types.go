package input

// EventKind 分类宿主输入，不依赖 Bubble Tea key/mouse 类型。
type EventKind string

const (
	EventKindKey    EventKind = "key"
	EventKindMouse  EventKind = "mouse"
	EventKindResize EventKind = "resize"
)

type Key string

const (
	KeyPageUp  Key = "page-up"
	KeyPageDn  Key = "page-down"
	KeyEsc     Key = "esc"
	KeyEnter   Key = "enter"
	KeyChar    Key = "char"
	KeyUnknown Key = "unknown"
)

type MouseButton string

const (
	MouseWheelUp   MouseButton = "wheel-up"
	MouseWheelDown MouseButton = "wheel-down"
	MouseLeft      MouseButton = "left"
)

// InputEvent 是 TerminalHost 拥有的宿主输入边界。
type InputEvent struct {
	Kind   EventKind
	Key    Key
	Char   string
	Mouse  MouseButton
	Row    int
	Col    int
	Cols   int
	Rows   int
	Alt    bool
	Ctrl   bool
	Shift  bool
	RawSeq string
}

type IntentKind string

const (
	IntentNone               IntentKind = "none"
	IntentOpenTerminalPicker IntentKind = "open-terminal-picker"
	IntentEnterCopyMode      IntentKind = "enter-copy-mode"
	IntentRequestOlder       IntentKind = "request-older"
	IntentExitCopyMode       IntentKind = "exit-copy-mode"
	IntentTerminalInput      IntentKind = "terminal-input"
	IntentMouseSelect        IntentKind = "mouse-select"
	IntentPaneCommand        IntentKind = "pane-command"
)

// Intent 是 input router 输出的 semantic intent，不直接修改 state。
type Intent struct {
	Kind    IntentKind
	Event   InputEvent
	Bytes   []byte
	Reason  string
	Command string
}

func Route(event InputEvent, copyModeActive bool) Intent {
	switch event.Kind {
	case EventKindKey:
		return routeKey(event, copyModeActive)
	case EventKindMouse:
		return routeMouse(event, copyModeActive)
	default:
		return Intent{Kind: IntentNone, Event: event, Reason: "unknown input kind"}
	}
}

func routeKey(event InputEvent, copyModeActive bool) Intent {
	if event.Ctrl && event.Key == KeyChar {
		switch event.Char {
		case "f", "\x06":
			return Intent{Kind: IntentOpenTerminalPicker, Event: event}
		case "v", "\x16":
			return Intent{Kind: IntentEnterCopyMode, Event: event}
		}
	}
	switch event.Key {
	case KeyPageUp:
		if copyModeActive {
			return Intent{Kind: IntentRequestOlder, Event: event}
		}
		return Intent{Kind: IntentEnterCopyMode, Event: event}
	case KeyEsc:
		if copyModeActive {
			return Intent{Kind: IntentExitCopyMode, Event: event}
		}
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte{'\x1b'}}
	case KeyEnter:
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte{'\r'}}
	case KeyChar:
		if event.Char != "" {
			return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte(event.Char)}
		}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routeMouse(event InputEvent, copyModeActive bool) Intent {
	switch event.Mouse {
	case MouseWheelUp:
		if copyModeActive {
			return Intent{Kind: IntentRequestOlder, Event: event}
		}
		return Intent{Kind: IntentEnterCopyMode, Event: event}
	case MouseLeft:
		if copyModeActive {
			return Intent{Kind: IntentMouseSelect, Event: event}
		}
	}
	return Intent{Kind: IntentNone, Event: event}
}
