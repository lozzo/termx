package transformer

import (
	"fmt"
	"time"

	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/apipb"
)

// RequestContextForCommand 返回 typed command 自身携带的公共 request context。
func RequestContextForCommand(command *apipb.CommandEnvelope) *apipb.RequestContext {
	if command == nil {
		return nil
	}
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_CancelOperation:
		return value.CancelOperation.GetContext()
	case *apipb.CommandEnvelope_ReleaseResource:
		return value.ReleaseResource.GetContext()
	case *apipb.CommandEnvelope_TerminalDefaults:
		return value.TerminalDefaults.GetContext()
	case *apipb.CommandEnvelope_TerminalCreate:
		return value.TerminalCreate.GetContext()
	case *apipb.CommandEnvelope_TerminalList:
		return value.TerminalList.GetContext()
	case *apipb.CommandEnvelope_TerminalGet:
		return value.TerminalGet.GetContext()
	case *apipb.CommandEnvelope_TerminalRestart:
		return value.TerminalRestart.GetContext()
	case *apipb.CommandEnvelope_TerminalKill:
		return value.TerminalKill.GetContext()
	case *apipb.CommandEnvelope_TerminalRemove:
		return value.TerminalRemove.GetContext()
	case *apipb.CommandEnvelope_TerminalSetMetadata:
		return value.TerminalSetMetadata.GetContext()
	case *apipb.CommandEnvelope_TerminalSetTags:
		return value.TerminalSetTags.GetContext()
	case *apipb.CommandEnvelope_TerminalAttach:
		return value.TerminalAttach.GetContext()
	case *apipb.CommandEnvelope_TerminalDetach:
		return value.TerminalDetach.GetContext()
	case *apipb.CommandEnvelope_TerminalInput:
		return value.TerminalInput.GetContext()
	case *apipb.CommandEnvelope_TerminalResize:
		return value.TerminalResize.GetContext()
	case *apipb.CommandEnvelope_TerminalResizeLock:
		return value.TerminalResizeLock.GetContext()
	case *apipb.CommandEnvelope_PathListDirectories:
		return value.PathListDirectories.GetContext()
	default:
		return nil
	}
}

// RequiredCapabilityForCommand 返回 typed command 必须显式协商的 capability。
func RequiredCapabilityForCommand(command *apipb.CommandEnvelope) apipb.ApiCapability {
	if command == nil {
		return apipb.ApiCapability_API_CAPABILITY_UNSPECIFIED
	}
	switch command.GetCommand().(type) {
	case *apipb.CommandEnvelope_CancelOperation:
		return apipb.ApiCapability_API_CAPABILITY_OPERATION_CANCELLATION
	case *apipb.CommandEnvelope_ReleaseResource:
		return apipb.ApiCapability_API_CAPABILITY_RESOURCE_LIFECYCLE
	case *apipb.CommandEnvelope_TerminalAttach,
		*apipb.CommandEnvelope_TerminalDetach,
		*apipb.CommandEnvelope_TerminalInput,
		*apipb.CommandEnvelope_TerminalResize,
		*apipb.CommandEnvelope_TerminalResizeLock:
		return apipb.ApiCapability_API_CAPABILITY_TERMINAL_ATTACHMENT
	case *apipb.CommandEnvelope_PathListDirectories:
		return apipb.ApiCapability_API_CAPABILITY_PATH_QUERY
	case *apipb.CommandEnvelope_TerminalDefaults,
		*apipb.CommandEnvelope_TerminalCreate,
		*apipb.CommandEnvelope_TerminalList,
		*apipb.CommandEnvelope_TerminalGet,
		*apipb.CommandEnvelope_TerminalRestart,
		*apipb.CommandEnvelope_TerminalKill,
		*apipb.CommandEnvelope_TerminalRemove,
		*apipb.CommandEnvelope_TerminalSetMetadata,
		*apipb.CommandEnvelope_TerminalSetTags:
		return apipb.ApiCapability_API_CAPABILITY_TERMINAL_LIFECYCLE
	default:
		return apipb.ApiCapability_API_CAPABILITY_UNSPECIFIED
	}
}

// ValidateTerminalCommand 校验 terminal/path command 的 identity、枚举、resource 和 operation fence。
func ValidateTerminalCommand(command *apipb.CommandEnvelope) error {
	requestContext := RequestContextForCommand(command)
	if err := ValidateRequestContext(requestContext); err != nil {
		return err
	}
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalDefaults, *apipb.CommandEnvelope_TerminalList:
		return requireSession(requestContext)
	case *apipb.CommandEnvelope_TerminalCreate:
		if err := requireSession(requestContext); err != nil {
			return err
		}
		return ValidateTerminalCreateSpec(value.TerminalCreate.GetTerminal())
	case *apipb.CommandEnvelope_TerminalGet:
		return validateTerminalRefForContext(value.TerminalGet.GetTerminal(), requestContext)
	case *apipb.CommandEnvelope_TerminalRestart:
		return validateTerminalRefForContext(value.TerminalRestart.GetTerminal(), requestContext)
	case *apipb.CommandEnvelope_TerminalKill:
		return validateTerminalRefForContext(value.TerminalKill.GetTerminal(), requestContext)
	case *apipb.CommandEnvelope_TerminalRemove:
		return validateTerminalRefForContext(value.TerminalRemove.GetTerminal(), requestContext)
	case *apipb.CommandEnvelope_TerminalSetMetadata:
		return validateTerminalRefForContext(value.TerminalSetMetadata.GetTerminal(), requestContext)
	case *apipb.CommandEnvelope_TerminalSetTags:
		return validateTerminalRefForContext(value.TerminalSetTags.GetTerminal(), requestContext)
	case *apipb.CommandEnvelope_TerminalAttach:
		attach := value.TerminalAttach
		if err := validateTerminalRefForContext(attach.GetTerminal(), requestContext); err != nil {
			return err
		}
		if attach.GetMode() == apipb.AttachmentMode_ATTACHMENT_MODE_UNSPECIFIED {
			return validation("terminal_attach.mode", "must be specified")
		}
		if attach.GetResizePolicy() == apipb.ResizePolicy_RESIZE_POLICY_UNSPECIFIED {
			return validation("terminal_attach.resize_policy", "must be specified")
		}
		if attach.GetSurfaceId() == "" || attach.GetViewId() == "" {
			return validation("terminal_attach.surface_id", "surface_id and view_id are required")
		}
		return ValidateOperationStamp(attach.GetOperation(), requestContext.GetSession())
	case *apipb.CommandEnvelope_TerminalDetach:
		return validateAttachmentOperation(value.TerminalDetach.GetAttachment(), value.TerminalDetach.GetOperation(), requestContext)
	case *apipb.CommandEnvelope_TerminalInput:
		if len(value.TerminalInput.GetData()) == 0 {
			return validation("terminal_input.data", "must not be empty")
		}
		return validateAttachmentOperation(value.TerminalInput.GetAttachment(), value.TerminalInput.GetOperation(), requestContext)
	case *apipb.CommandEnvelope_TerminalResize:
		resize := value.TerminalResize
		if err := ValidateTerminalSize(resize.GetSize()); err != nil {
			return err
		}
		if resize.GetResizePolicy() == apipb.ResizePolicy_RESIZE_POLICY_UNSPECIFIED {
			return validation("terminal_resize.resize_policy", "must be specified")
		}
		return validateAttachmentOperation(resize.GetAttachment(), resize.GetOperation(), requestContext)
	case *apipb.CommandEnvelope_TerminalResizeLock:
		return validateAttachmentOperation(value.TerminalResizeLock.GetAttachment(), value.TerminalResizeLock.GetOperation(), requestContext)
	case *apipb.CommandEnvelope_PathListDirectories:
		if err := requireSession(requestContext); err != nil {
			return err
		}
		if value.PathListDirectories.GetLimit() < 0 {
			return validation("path_list_directories.limit", "must not be negative")
		}
		return nil
	default:
		return validation("command", "is not a terminal or path command")
	}
}

// ValidateTerminalCreateSpec 校验 daemon-local terminal 创建规格。
func ValidateTerminalCreateSpec(spec *apipb.TerminalCreateSpec) error {
	if spec == nil {
		return validation("terminal_create.terminal", "is required")
	}
	if spec.GetTerminalId() == "" {
		return validation("terminal_create.terminal.terminal_id", "is required")
	}
	if len(spec.GetCommand()) == 0 {
		return validation("terminal_create.terminal.command", "must not be empty")
	}
	return ValidateTerminalSize(spec.GetSize())
}

// ValidateTerminalSize 校验 terminal cell 尺寸。
func ValidateTerminalSize(size *apipb.TerminalSize) error {
	if size == nil || size.GetCols() == 0 || size.GetRows() == 0 {
		return validation("terminal.size", "cols and rows must be greater than zero")
	}
	if size.GetCols() > 65535 || size.GetRows() > 65535 {
		return validation("terminal.size", "cols and rows exceed daemon limits")
	}
	return nil
}

// TerminalRecordFromProto 把已校验的 public create spec 转换为 core domain record。
func TerminalRecordFromProto(spec *apipb.TerminalCreateSpec) (corev2.TerminalRecord, error) {
	if err := ValidateTerminalCreateSpec(spec); err != nil {
		return corev2.TerminalRecord{}, err
	}
	return corev2.TerminalRecord{
		ID:      spec.GetTerminalId(),
		Name:    spec.GetName(),
		Command: append([]string(nil), spec.GetCommand()...),
		Tags:    cloneStringMap(spec.GetTags()),
		Size:    corev2.Size{Cols: uint16(spec.GetSize().GetCols()), Rows: uint16(spec.GetSize().GetRows())},
		Options: corev2.TerminalCreateOptions{
			Dir:                spec.GetCwd(),
			Env:                append([]string(nil), spec.GetEnv()...),
			ScrollbackSize:     int(spec.GetScrollbackRows()),
			ScrollbackMaxBytes: spec.GetScrollbackMaxBytes(),
			ScrollbackMaxAge:   time.Duration(spec.GetScrollbackMaxAgeSeconds()) * time.Second,
		},
	}, nil
}

// TerminalInfoToProto 把 core lifecycle snapshot 转换为 endpoint-aware public projection。
func TerminalInfoToProto(endpointID string, info corev2.TerminalInfo) (*apipb.TerminalInfo, error) {
	if endpointID == "" || info.ID == "" {
		return nil, fmt.Errorf("terminal projection requires endpoint and terminal identity")
	}
	out := &apipb.TerminalInfo{
		Ref:               &apipb.TerminalRef{EndpointId: endpointID, TerminalId: info.ID},
		Name:              info.Name,
		Command:           append([]string(nil), info.Command...),
		Tags:              cloneStringMap(info.Tags),
		Size:              &apipb.TerminalSize{Cols: uint32(info.Size.Cols), Rows: uint32(info.Size.Rows)},
		State:             terminalStateToProto(info.State),
		Cwd:               info.CWD,
		LiveCwd:           info.LiveCWD,
		CreatedAtUnixNano: info.CreatedAt.UnixNano(),
		ExitedAtUnixNano:  info.ExitedAt.UnixNano(),
		Resources: &apipb.TerminalResourceUsage{
			Pid: int32(info.Resources.PID), CpuPercentX100: int32(info.Resources.CPUPercentX100),
			MemoryBytes: info.Resources.MemoryBytes, SampledAtUnixNano: info.Resources.SampledAt.UnixNano(),
		},
	}
	if info.CreatedAt.IsZero() {
		out.CreatedAtUnixNano = 0
	}
	if info.ExitedAt.IsZero() {
		out.ExitedAtUnixNano = 0
	}
	if info.Resources.SampledAt.IsZero() {
		out.Resources.SampledAtUnixNano = 0
	}
	if info.ExitCode != nil {
		value := int32(*info.ExitCode)
		out.ExitCode = &value
	}
	return out, nil
}

func requireSession(contextMessage *apipb.RequestContext) error {
	return ValidateSessionStamp(contextMessage.GetSession())
}

func validateTerminalRefForContext(ref *apipb.TerminalRef, contextMessage *apipb.RequestContext) error {
	if err := requireSession(contextMessage); err != nil {
		return err
	}
	if ref == nil || ref.GetEndpointId() == "" || ref.GetTerminalId() == "" {
		return validation("terminal", "endpoint_id and terminal_id are required")
	}
	if ref.GetEndpointId() != contextMessage.GetSession().GetEndpointId() {
		return validation("terminal.endpoint_id", "must match context.session.endpoint_id")
	}
	return nil
}

func validateAttachmentOperation(resource *apipb.ResourceHandle, operation *apipb.OperationStamp, contextMessage *apipb.RequestContext) error {
	if err := ValidateResourceHandle(resource); err != nil {
		return err
	}
	if resource.GetKind() != "terminal_attachment" {
		return validation("attachment.kind", "must be terminal_attachment")
	}
	return ValidateOperationStamp(operation, contextMessage.GetSession())
}

func terminalStateToProto(state corev2.TerminalState) apipb.TerminalState {
	switch state {
	case corev2.TerminalStateCreated:
		return apipb.TerminalState_TERMINAL_STATE_CREATED
	case corev2.TerminalStateRunning:
		return apipb.TerminalState_TERMINAL_STATE_RUNNING
	case corev2.TerminalStateExited:
		return apipb.TerminalState_TERMINAL_STATE_EXITED
	case corev2.TerminalStateRemoved:
		return apipb.TerminalState_TERMINAL_STATE_REMOVED
	default:
		return apipb.TerminalState_TERMINAL_STATE_UNSPECIFIED
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
