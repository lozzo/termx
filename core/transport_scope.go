package core

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
)

// TransportScope 只约束一条 protocol session 的可见能力，不保存 terminal truth。
type TransportScope struct {
	// PrincipalID 标识当前已验证授权主体，仅用于绑定可恢复资源；不得作为 terminal 或账号 truth。
	// local listener 使用固定 local principal，remote DataChannel 使用已签名 CapabilityGrant ID。
	PrincipalID string
	// AllowDaemon 表示该 session 拥有当前 daemon 的完整 protocol 能力。
	// 该字段必须由本地 listener 或已验证的 daemon-level capability 显式设置；零值不能代表无限权限。
	AllowDaemon bool
	// TerminalID 把 session 限制到单个 daemon-local terminal。
	// terminal lifecycle 和 history truth 仍由 core-v2 持有，scope 只在 protocol method/stream 入口执行授权。
	TerminalID string
	// MachineEventsOnly 只允许订阅 daemon 的受限 terminal lifecycle 事件。
	// 它不能与 AllowDaemon 或 TerminalID 组合，也不能访问 storage、history、input 或 terminal management method。
	MachineEventsOnly bool
	// FileReadMetadata 允许读取 daemon 文件系统的目录项和 lstat metadata。
	// 权限必须由 local listener 或已验证 grant 显式赋予，不能从 AllowDaemon 推导。
	FileReadMetadata bool
	// FileReadContent 允许有界预览文件内容；它不包含上传或 mutation 权限。
	FileReadContent bool
	// FileMutate 允许 mkdir、rename、delete、copy 和 move。
	FileMutate bool
	// FileWriteContent 允许创建、恢复并完成上传 transfer；它不隐含 mutation 权限。
	FileWriteContent bool
}

func fullDaemonTransportScope() TransportScope {
	return TransportScope{PrincipalID: "local", AllowDaemon: true, FileReadMetadata: true, FileReadContent: true, FileWriteContent: true, FileMutate: true}
}

func (scope TransportScope) normalized() TransportScope {
	scope.TerminalID = strings.TrimSpace(scope.TerminalID)
	return scope
}

func (scope TransportScope) validate() error {
	capabilities := 0
	if scope.AllowDaemon {
		capabilities++
	}
	if scope.TerminalID != "" {
		capabilities++
	}
	if scope.MachineEventsOnly {
		capabilities++
	}
	if capabilities == 0 {
		return fmt.Errorf("transport scope requires explicit capability")
	}
	if capabilities != 1 {
		return fmt.Errorf("transport scope capabilities are mutually exclusive")
	}
	if (scope.FileReadMetadata || scope.FileReadContent || scope.FileWriteContent || scope.FileMutate) && !scope.AllowDaemon {
		return fmt.Errorf("file permissions require daemon scope")
	}
	if (scope.FileReadMetadata || scope.FileReadContent || scope.FileWriteContent || scope.FileMutate) && scope.PrincipalID == "" {
		return fmt.Errorf("file permissions require verified principal")
	}
	return nil
}

func (scope TransportScope) unrestricted() bool {
	return scope.AllowDaemon
}

func (scope TransportScope) constrainMethod(method string, params any) (any, error) {
	scope = scope.normalized()
	if strings.HasPrefix(method, "file.") {
		return scope.constrainFileMethod(method, params)
	}
	if scope.unrestricted() {
		return params, nil
	}
	if scope.MachineEventsOnly {
		return scope.constrainMachineEventsOnly(method, params)
	}
	return scope.constrainTerminalMethod(method, params)
}

func (scope TransportScope) constrainFileMethod(method string, params any) (any, error) {
	if !scope.AllowDaemon {
		return nil, fmt.Errorf("transport scope denies daemon file method %q", method)
	}
	required := false
	switch method {
	case "file.list", "file.stat":
		required = scope.FileReadMetadata
	case "file.preview":
		required = scope.FileReadContent
	case "file.download.open":
		required = scope.FileReadContent
	case "file.upload.open":
		required = scope.FileWriteContent
	case "file.transfer.cancel":
		required = scope.FileReadContent || scope.FileWriteContent
	case "file.mkdir", "file.rename", "file.delete", "file.move":
		required = scope.FileMutate
	case "file.copy":
		required = scope.FileMutate && scope.FileReadContent
	}
	if !required {
		return nil, fmt.Errorf("transport scope denies file permission for method %q", method)
	}
	return params, nil
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
