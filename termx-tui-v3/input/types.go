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
	KeyUp      Key = "up"
	KeyDown    Key = "down"
	KeyLeft    Key = "left"
	KeyRight   Key = "right"
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
	MouseLeftDrag  MouseButton = "left-drag"
	MouseLeftUp    MouseButton = "left-up"
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
	IntentSetInteractionMode IntentKind = "set-interaction-mode"
	IntentExitInteraction    IntentKind = "exit-interaction"
	IntentShellAction        IntentKind = "shell-action"
	IntentPaneCommand        IntentKind = "pane-command"
	IntentWorkbenchCommand   IntentKind = "workbench-command"
)

type InteractionMode string

const (
	InteractionModeNormal    InteractionMode = ""
	InteractionModePane      InteractionMode = "pane"
	InteractionModeResize    InteractionMode = "resize"
	InteractionModeGlobal    InteractionMode = "global"
	InteractionModeFloating  InteractionMode = "floating"
	InteractionModeTab       InteractionMode = "tab"
	InteractionModeWorkspace InteractionMode = "workspace"
)

type ShellAction string

const (
	ShellActionToggleHeader ShellAction = "shell.toggle-header"
	ShellActionToggleFooter ShellAction = "shell.toggle-footer"
	ShellActionClearToasts  ShellAction = "shell.clear-toasts"
	ShellActionCloseToast   ShellAction = "shell.close-toast"
	ShellActionFloatingCtrl ShellAction = "shell.floating-control"
	ShellActionFloatingNew  ShellAction = "shell.floating-new"
	ShellActionFloatingMove ShellAction = "shell.floating-move"
	ShellActionFloatingSize ShellAction = "shell.floating-size"
	ShellActionOpenPool     ShellAction = "shell.open-terminal-pool"
	ShellActionOpenTree     ShellAction = "shell.open-workbench-tree"
	ShellActionOpenPrompt   ShellAction = "shell.open-prompt"
	ShellActionOpenHelp     ShellAction = "shell.open-help"
)

// Intent 是 input router 输出的 semantic intent，不直接修改 state。
type Intent struct {
	Kind    IntentKind
	Event   InputEvent
	Bytes   []byte
	Reason  string
	Command string
	Mode    InteractionMode
	Action  ShellAction
}

func Route(event InputEvent, copyModeActive bool) Intent {
	return RouteWithMode(event, copyModeActive, InteractionModeNormal)
}

func RouteWithMode(event InputEvent, copyModeActive bool, mode InteractionMode) Intent {
	switch event.Kind {
	case EventKindKey:
		return routeKey(event, copyModeActive, mode)
	case EventKindMouse:
		return routeMouse(event, copyModeActive)
	default:
		return Intent{Kind: IntentNone, Event: event, Reason: "unknown input kind"}
	}
}

func routeKey(event InputEvent, copyModeActive bool, mode InteractionMode) Intent {
	if event.Key == KeyEsc {
		if mode != InteractionModeNormal {
			return Intent{Kind: IntentExitInteraction, Event: event}
		}
		if copyModeActive {
			return Intent{Kind: IntentExitCopyMode, Event: event}
		}
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte{'\x1b'}}
	}
	if event.Ctrl && event.Key == KeyChar {
		switch event.Char {
		case "p", "\x10":
			return Intent{Kind: IntentSetInteractionMode, Event: event, Mode: InteractionModePane}
		case "r", "\x12":
			return Intent{Kind: IntentSetInteractionMode, Event: event, Mode: InteractionModeResize}
		case "g", "\x07":
			return Intent{Kind: IntentSetInteractionMode, Event: event, Mode: InteractionModeGlobal}
		case "o", "\x0f":
			return Intent{Kind: IntentSetInteractionMode, Event: event, Mode: InteractionModeFloating}
		case "t", "\x14":
			return Intent{Kind: IntentSetInteractionMode, Event: event, Mode: InteractionModeTab}
		case "w", "\x17":
			return Intent{Kind: IntentSetInteractionMode, Event: event, Mode: InteractionModeWorkspace}
		case "f", "\x06":
			return Intent{Kind: IntentOpenTerminalPicker, Event: event}
		case "v", "\x16":
			return Intent{Kind: IntentEnterCopyMode, Event: event}
		}
	}
	switch mode {
	case InteractionModePane:
		return routePaneModeKey(event)
	case InteractionModeResize:
		return routeResizeModeKey(event)
	case InteractionModeGlobal:
		return routeGlobalModeKey(event)
	case InteractionModeFloating:
		return routeFloatingModeKey(event)
	case InteractionModeTab:
		return routeTabModeKey(event)
	case InteractionModeWorkspace:
		return routeWorkspaceModeKey(event)
	}
	switch event.Key {
	case KeyPageUp:
		if copyModeActive {
			return Intent{Kind: IntentRequestOlder, Event: event}
		}
		return Intent{Kind: IntentEnterCopyMode, Event: event}
	case KeyEnter:
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte{'\r'}}
	case KeyChar:
		if event.Char != "" {
			return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte(event.Char)}
		}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routePaneModeKey(event InputEvent) Intent {
	if event.Key != KeyChar {
		return Intent{Kind: IntentNone, Event: event}
	}
	switch event.Char {
	case "v":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane split-right"}
	case "s":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane split-down"}
	case "x":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane close"}
	case "z":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane toggle-zoom"}
	case "b":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane balance"}
	case "p":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane presentation split-line"}
	case "c":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane presentation card"}
	case "n":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane focus-next"}
	case "N":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane focus-prev"}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routeResizeModeKey(event InputEvent) Intent {
	switch event.Key {
	case KeyLeft:
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane resize left delta=2"}
	case KeyRight:
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane resize right delta=2"}
	case KeyUp:
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane resize up delta=2"}
	case KeyDown:
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane resize down delta=2"}
	}
	if event.Key != KeyChar {
		return Intent{Kind: IntentNone, Event: event}
	}
	delta := "2"
	switch event.Char {
	case "h":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane resize left delta=" + delta}
	case "l":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane resize right delta=" + delta}
	case "k":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane resize up delta=" + delta}
	case "j":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane resize down delta=" + delta}
	case "b":
		return Intent{Kind: IntentPaneCommand, Event: event, Command: "pane balance"}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routeGlobalModeKey(event InputEvent) Intent {
	if event.Key != KeyChar {
		return Intent{Kind: IntentNone, Event: event}
	}
	switch event.Char {
	case "h":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionToggleHeader}
	case "f":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionToggleFooter}
	case "t":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionClearToasts}
	case "T":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionCloseToast}
	case "p":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionOpenPool}
	case "w":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionOpenTree}
	case ":":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionOpenPrompt}
	case "?":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionOpenHelp}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routeFloatingModeKey(event InputEvent) Intent {
	switch event.Key {
	case KeyLeft:
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingMove, Reason: "left"}
	case KeyRight:
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingMove, Reason: "right"}
	case KeyUp:
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingMove, Reason: "up"}
	case KeyDown:
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingMove, Reason: "down"}
	}
	if event.Key != KeyChar {
		return Intent{Kind: IntentNone, Event: event}
	}
	switch event.Char {
	case "n":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingNew}
	case "x":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingCtrl, Reason: "close"}
	case "z":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingCtrl, Reason: "collapse"}
	case "c":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingCtrl, Reason: "center"}
	case "h":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingMove, Reason: "left"}
	case "l":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingMove, Reason: "right"}
	case "k":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingMove, Reason: "up"}
	case "j":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingMove, Reason: "down"}
	case "H":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingSize, Reason: "narrow"}
	case "L":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingSize, Reason: "wide"}
	case "K":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingSize, Reason: "short"}
	case "J":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionFloatingSize, Reason: "tall"}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routeTabModeKey(event InputEvent) Intent {
	if event.Key != KeyChar {
		return Intent{Kind: IntentNone, Event: event}
	}
	switch event.Char {
	case "n":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "tab create"}
	case "l", "]":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "tab next"}
	case "h", "[":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "tab previous"}
	case "r":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "tab rename"}
	case "x":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "tab close"}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routeWorkspaceModeKey(event InputEvent) Intent {
	if event.Key != KeyChar {
		return Intent{Kind: IntentNone, Event: event}
	}
	switch event.Char {
	case "n":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "workspace create"}
	case "l", "]":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "workspace next"}
	case "h", "[":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "workspace previous"}
	case "r":
		return Intent{Kind: IntentWorkbenchCommand, Event: event, Command: "workspace rename"}
	case "t":
		return Intent{Kind: IntentShellAction, Event: event, Action: ShellActionOpenTree}
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
	case MouseLeft, MouseLeftDrag:
		if copyModeActive {
			return Intent{Kind: IntentMouseSelect, Event: event}
		}
	}
	return Intent{Kind: IntentNone, Event: event}
}
