package termxcorev2

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
)

// TransportScope 只约束一条 protocol session 的可见能力，不保存 terminal truth。
type TransportScope struct {
	TerminalID        string
	MachineEventsOnly bool
}

func (scope TransportScope) normalized() TransportScope {
	scope.TerminalID = strings.TrimSpace(scope.TerminalID)
	return scope
}

func (scope TransportScope) validate() error {
	if scope.TerminalID != "" && scope.MachineEventsOnly {
		return fmt.Errorf("transport scope cannot combine terminal_id with machine_events_only")
	}
	return nil
}

func (scope TransportScope) unrestricted() bool {
	return scope.TerminalID == "" && !scope.MachineEventsOnly
}

func (scope TransportScope) constrainMethod(method string, params any) (any, error) {
	scope = scope.normalized()
	if scope.unrestricted() {
		return params, nil
	}
	if scope.MachineEventsOnly {
		return scope.constrainMachineEventsOnly(method, params)
	}
	return scope.constrainTerminalMethod(method, params)
}

func (scope TransportScope) constrainMachineEventsOnly(method string, params any) (any, error) {
	if method != "events" {
		return nil, fmt.Errorf("transport scope machine-events-only denies method %q", method)
	}
	events, ok := params.(protocol.EventsParams)
	if !ok {
		return nil, fmt.Errorf("events params have unexpected type %T", params)
	}
	return machineEventsOnlyParams(events)
}

func (scope TransportScope) constrainTerminalMethod(method string, params any) (any, error) {
	switch method {
	case "get", "kill", "restart", "remove":
		in, ok := params.(protocol.GetParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "set_tags":
		in, ok := params.(protocol.SetTagsParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "set_metadata":
		in, ok := params.(protocol.SetMetadataParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "resize":
		in, ok := params.(protocol.ResizeParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "input":
		in, ok := params.(protocol.InputParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "attach":
		in, ok := params.(protocol.AttachParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "detach":
		in, ok := params.(protocol.DetachParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		if in.TerminalID == "" {
			return params, nil
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "ensure_resize":
		in, ok := params.(protocol.EnsureResizeParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "resize.lock", "resize.unlock":
		in, ok := params.(protocol.ResizeControlParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "live.screen.get":
		in, ok := params.(protocol.LiveScreenParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "history.window", "history.copy", "history.release":
		in, ok := params.(protocol.HistoryWindowParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return params, scope.requireTerminal(method, in.TerminalID)
	case "events":
		in, ok := params.(protocol.EventsParams)
		if !ok {
			return nil, fmt.Errorf("%s params have unexpected type %T", method, params)
		}
		return scope.constrainTerminalEvents(in)
	default:
		return nil, fmt.Errorf("transport scope terminal %q denies method %q", scope.TerminalID, method)
	}
}

func (scope TransportScope) requireTerminal(method string, terminalID string) error {
	if terminalID == "" {
		return fmt.Errorf("transport scope terminal %q denies %s without terminal_id", scope.TerminalID, method)
	}
	if terminalID != scope.TerminalID {
		return fmt.Errorf("transport scope terminal %q denies %s for terminal %q", scope.TerminalID, method, terminalID)
	}
	return nil
}

func (scope TransportScope) constrainTerminalEvents(params protocol.EventsParams) (protocol.EventsParams, error) {
	if params.TerminalID != "" && params.TerminalID != scope.TerminalID {
		return protocol.EventsParams{}, fmt.Errorf("transport scope terminal %q denies events for terminal %q", scope.TerminalID, params.TerminalID)
	}
	params.TerminalID = scope.TerminalID
	if hasNonTerminalEventParams(params) {
		return protocol.EventsParams{}, fmt.Errorf("transport scope terminal %q denies non-terminal event filters", scope.TerminalID)
	}
	types, err := terminalEventTypesOnly(params.Types)
	if err != nil {
		return protocol.EventsParams{}, err
	}
	if len(types) == 0 {
		types = allTerminalEventTypes()
	}
	params.Types = types
	return params, nil
}

func (scope TransportScope) allowsAttachment(attachment protocolAttachment) error {
	if scope.unrestricted() {
		return nil
	}
	if scope.MachineEventsOnly {
		return fmt.Errorf("transport scope machine-events-only denies stream channel %d", attachment.Channel)
	}
	return scope.requireTerminal("stream", attachment.TerminalID)
}

func machineEventsOnlyParams(params protocol.EventsParams) (protocol.EventsParams, error) {
	if params.TerminalID != "" {
		return protocol.EventsParams{}, fmt.Errorf("transport scope machine-events-only denies terminal-specific events for %q", params.TerminalID)
	}
	if hasNonTerminalEventParams(params) {
		return protocol.EventsParams{}, fmt.Errorf("transport scope machine-events-only denies storage/workbench event filters")
	}
	types, err := terminalEventTypesOnly(params.Types)
	if err != nil {
		return protocol.EventsParams{}, err
	}
	if len(types) == 0 {
		types = allTerminalEventTypes()
	}
	params.Types = types
	return params, nil
}

func terminalEventTypesOnly(types []protocol.EventType) ([]protocol.EventType, error) {
	if len(types) == 0 {
		return nil, nil
	}
	out := make([]protocol.EventType, 0, len(types))
	for _, typ := range types {
		if !isTerminalEventType(typ) {
			return nil, fmt.Errorf("transport scope denies non-terminal event type %d", typ)
		}
		out = append(out, typ)
	}
	return out, nil
}

func allTerminalEventTypes() []protocol.EventType {
	return []protocol.EventType{
		protocol.EventTerminalCreated,
		protocol.EventTerminalStateChanged,
		protocol.EventTerminalResized,
		protocol.EventTerminalRemoved,
		protocol.EventCollaboratorsRevoked,
		protocol.EventTerminalReadError,
		protocol.EventTerminalLiveInvalidated,
		protocol.EventTerminalMetadataChanged,
	}
}

func isTerminalEventType(typ protocol.EventType) bool {
	switch typ {
	case protocol.EventTerminalCreated,
		protocol.EventTerminalStateChanged,
		protocol.EventTerminalResized,
		protocol.EventTerminalRemoved,
		protocol.EventCollaboratorsRevoked,
		protocol.EventTerminalReadError,
		protocol.EventTerminalLiveInvalidated,
		protocol.EventTerminalMetadataChanged:
		return true
	default:
		return false
	}
}

func hasNonTerminalEventParams(params protocol.EventsParams) bool {
	return params.StorageAppID != "" ||
		params.StorageScope != "" ||
		params.StorageOwnerID != "" ||
		params.StorageKeyPrefix != "" ||
		params.WorkbenchID != ""
}
