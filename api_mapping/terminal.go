package apimapping

import (
	"fmt"
	"math"
	"time"

	"github.com/lozzow/termx/proto/apipb"
)

const (
	maxTerminalInputBytes = 1 << 20
	maxPathPrefixBytes    = 4096
	maxPathEntries        = 1000
	maxCommandArguments   = 256
	maxEnvironmentEntries = 4096
	maxTagEntries         = 256
	maxAggregateTextBytes = 1 << 20
	maxMetadataTextBytes  = 4096
)

// RequestContextForCommand 返回 envelope 顶层唯一的公共 request context。
// context 位于 oneof 外，旧服务即使不认识未来 command 也能保留 request correlation。
func RequestContextForCommand(command *apipb.CommandEnvelope) *apipb.RequestContext {
	if command == nil {
		return nil
	}
	return command.GetContext()
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
	if !terminalCommandPayloadPresent(command) {
		return validation("command", "typed command payload is required")
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
		if err := validateTerminalRefForContext(value.TerminalSetMetadata.GetTerminal(), requestContext); err != nil {
			return err
		}
		if len(value.TerminalSetMetadata.GetName()) > maxMetadataTextBytes {
			return validation("terminal_set_metadata.name", "exceeds 4096 bytes")
		}
		return validateStringMap("terminal_set_metadata.tags", value.TerminalSetMetadata.GetTags())
	case *apipb.CommandEnvelope_TerminalSetTags:
		if err := validateTerminalRefForContext(value.TerminalSetTags.GetTerminal(), requestContext); err != nil {
			return err
		}
		return validateStringMap("terminal_set_tags.tags", value.TerminalSetTags.GetTags())
	case *apipb.CommandEnvelope_TerminalAttach:
		attach := value.TerminalAttach
		if err := validateTerminalRefForContext(attach.GetTerminal(), requestContext); err != nil {
			return err
		}
		if !validAttachmentMode(attach.GetMode()) {
			return validation("terminal_attach.mode", "must be specified")
		}
		if !validResizePolicy(attach.GetResizePolicy()) {
			return validation("terminal_attach.resize_policy", "must be specified")
		}
		if attach.GetSurfaceId() == "" || attach.GetViewId() == "" {
			return validation("terminal_attach.surface_id", "surface_id and view_id are required")
		}
		if len(attach.GetSurfaceId()) > maxAPIIdentityBytes || len(attach.GetViewId()) > maxAPIIdentityBytes {
			return validation("terminal_attach.surface_id", "surface_id or view_id exceeds 256 bytes")
		}
		return ValidateOperationStamp(attach.GetOperation(), requestContext.GetSession())
	case *apipb.CommandEnvelope_TerminalDetach:
		return validateAttachmentOperation(value.TerminalDetach.GetAttachment(), value.TerminalDetach.GetOperation(), requestContext)
	case *apipb.CommandEnvelope_TerminalInput:
		if len(value.TerminalInput.GetData()) == 0 {
			return validation("terminal_input.data", "must not be empty")
		}
		if len(value.TerminalInput.GetData()) > maxTerminalInputBytes {
			return validation("terminal_input.data", "exceeds 1 MiB")
		}
		return validateAttachmentOperation(value.TerminalInput.GetAttachment(), value.TerminalInput.GetOperation(), requestContext)
	case *apipb.CommandEnvelope_TerminalResize:
		resize := value.TerminalResize
		if err := ValidateTerminalSize(resize.GetSize()); err != nil {
			return err
		}
		if !validResizePolicy(resize.GetResizePolicy()) {
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
		if value.PathListDirectories.GetLimit() > maxPathEntries {
			return validation("path_list_directories.limit", "exceeds 1000 entries")
		}
		if len(value.PathListDirectories.GetPrefix()) > maxPathPrefixBytes {
			return validation("path_list_directories.prefix", "exceeds 4096 bytes")
		}
		return nil
	default:
		return validation("command", "is not a terminal or path command")
	}
}

func terminalCommandPayloadPresent(command *apipb.CommandEnvelope) bool {
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalDefaults:
		return value.TerminalDefaults != nil
	case *apipb.CommandEnvelope_TerminalCreate:
		return value.TerminalCreate != nil
	case *apipb.CommandEnvelope_TerminalList:
		return value.TerminalList != nil
	case *apipb.CommandEnvelope_TerminalGet:
		return value.TerminalGet != nil
	case *apipb.CommandEnvelope_TerminalRestart:
		return value.TerminalRestart != nil
	case *apipb.CommandEnvelope_TerminalKill:
		return value.TerminalKill != nil
	case *apipb.CommandEnvelope_TerminalRemove:
		return value.TerminalRemove != nil
	case *apipb.CommandEnvelope_TerminalSetMetadata:
		return value.TerminalSetMetadata != nil
	case *apipb.CommandEnvelope_TerminalSetTags:
		return value.TerminalSetTags != nil
	case *apipb.CommandEnvelope_TerminalAttach:
		return value.TerminalAttach != nil
	case *apipb.CommandEnvelope_TerminalDetach:
		return value.TerminalDetach != nil
	case *apipb.CommandEnvelope_TerminalInput:
		return value.TerminalInput != nil
	case *apipb.CommandEnvelope_TerminalResize:
		return value.TerminalResize != nil
	case *apipb.CommandEnvelope_TerminalResizeLock:
		return value.TerminalResizeLock != nil
	case *apipb.CommandEnvelope_PathListDirectories:
		return value.PathListDirectories != nil
	default:
		return false
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
	if len(spec.GetTerminalId()) > 256 {
		return validation("terminal_create.terminal.terminal_id", "exceeds 256 bytes")
	}
	if len(spec.GetName()) > maxMetadataTextBytes || len(spec.GetCwd()) > maxMetadataTextBytes {
		return validation("terminal_create.terminal", "name or cwd exceeds 4096 bytes")
	}
	if err := validateStringSlice("terminal_create.terminal.command", spec.GetCommand(), maxCommandArguments); err != nil {
		return err
	}
	if err := validateStringSlice("terminal_create.terminal.env", spec.GetEnv(), maxEnvironmentEntries); err != nil {
		return err
	}
	if err := validateStringMap("terminal_create.terminal.tags", spec.GetTags()); err != nil {
		return err
	}
	if spec.GetScrollbackRows() < 0 {
		return validation("terminal_create.terminal.scrollback_rows", "must not be negative")
	}
	if spec.GetScrollbackMaxBytes() < 0 {
		return validation("terminal_create.terminal.scrollback_max_bytes", "must not be negative")
	}
	if spec.GetScrollbackMaxAgeSeconds() < 0 || spec.GetScrollbackMaxAgeSeconds() > math.MaxInt64/int64(time.Second) {
		return validation("terminal_create.terminal.scrollback_max_age_seconds", "is outside supported duration range")
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

// ValidateTerminalAttachResult 校验 controller 发布的 attachment handle 与已授权原始 attach command 完全一致。
// opaque token 真伪由 owning resource registry 保证；这里拒绝畸形、跨 session 或未知 enum 结果。
func ValidateTerminalAttachResult(command *apipb.TerminalAttachCommand, result *apipb.TerminalAttachResult, session *apipb.EndpointSessionStamp) error {
	if command == nil {
		return fmt.Errorf("terminal attach publication requires the authorized command")
	}
	if result == nil || result.GetAttachment() == nil {
		return fmt.Errorf("terminal attach controller returned no attachment handle")
	}
	handle := result.GetAttachment()
	if err := ValidateResourceHandle(handle.GetResource()); err != nil {
		return fmt.Errorf("invalid terminal attachment resource: %w", err)
	}
	if handle.GetResource().GetKind() != apipb.ResourceKind_RESOURCE_KIND_TERMINAL_ATTACHMENT {
		return fmt.Errorf("terminal attachment result has the wrong resource kind")
	}
	if !SessionStampsEqual(handle.GetResource().GetSession(), session) {
		return fmt.Errorf("terminal attachment result belongs to a different origin session")
	}
	if handle.GetTerminal().GetEndpointId() != command.GetTerminal().GetEndpointId() || handle.GetTerminal().GetTerminalId() != command.GetTerminal().GetTerminalId() {
		return fmt.Errorf("terminal attachment result identifies a different terminal")
	}
	if err := ValidateOperationStamp(handle.GetOperation(), session); err != nil {
		return fmt.Errorf("invalid terminal attachment operation: %w", err)
	}
	if handle.GetOperation().GetOperationId() != command.GetOperation().GetOperationId() {
		return fmt.Errorf("terminal attachment result identifies a different operation")
	}
	if handle.GetSurfaceId() != command.GetSurfaceId() || handle.GetViewId() != command.GetViewId() {
		return fmt.Errorf("terminal attachment result identifies a different surface or view")
	}
	if result.GetMode() != command.GetMode() || !validAttachmentMode(result.GetMode()) {
		return fmt.Errorf("terminal attachment result mode differs from the authorized command")
	}
	if result.GetResizePolicy() != command.GetResizePolicy() || !validResizePolicy(result.GetResizePolicy()) {
		return fmt.Errorf("terminal attachment result resize policy differs from the authorized command")
	}
	if err := ValidateTerminalSize(result.GetSize()); err != nil {
		return fmt.Errorf("invalid terminal attachment result size: %w", err)
	}
	return nil
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
	if len(ref.GetEndpointId()) > maxAPIIdentityBytes || len(ref.GetTerminalId()) > maxAPIIdentityBytes {
		return validation("terminal", "endpoint_id or terminal_id exceeds 256 bytes")
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
	if resource.GetKind() != apipb.ResourceKind_RESOURCE_KIND_TERMINAL_ATTACHMENT {
		return validation("attachment.kind", "must be terminal_attachment")
	}
	if !SessionStampsEqual(resource.GetSession(), contextMessage.GetSession()) {
		return validation("attachment.session", "must match context.session")
	}
	return ValidateOperationStamp(operation, contextMessage.GetSession())
}

func validAttachmentMode(mode apipb.AttachmentMode) bool {
	return mode == apipb.AttachmentMode_ATTACHMENT_MODE_COLLABORATOR || mode == apipb.AttachmentMode_ATTACHMENT_MODE_OBSERVER
}

func validResizePolicy(policy apipb.ResizePolicy) bool {
	switch policy {
	case apipb.ResizePolicy_RESIZE_POLICY_OWNER,
		apipb.ResizePolicy_RESIZE_POLICY_FOLLOWER,
		apipb.ResizePolicy_RESIZE_POLICY_OBSERVER:
		return true
	default:
		return false
	}
}

func validateStringSlice(field string, values []string, maxItems int) error {
	if len(values) > maxItems {
		return validation(field, fmt.Sprintf("exceeds %d items", maxItems))
	}
	total := 0
	for index, value := range values {
		total += len(value)
		if total > maxAggregateTextBytes {
			return validation(field, "exceeds 1 MiB total text")
		}
		if len(value) > maxMetadataTextBytes {
			return validation(fmt.Sprintf("%s[%d]", field, index), "exceeds 4096 bytes")
		}
	}
	return nil
}

func validateStringMap(field string, values map[string]string) error {
	if len(values) > maxTagEntries {
		return validation(field, "exceeds 256 entries")
	}
	total := 0
	for key, value := range values {
		if len(key) == 0 || len(key) > 256 {
			return validation(field, "contains an empty or oversized key")
		}
		if len(value) > maxMetadataTextBytes {
			return validation(field, "contains a value exceeding 4096 bytes")
		}
		total += len(key) + len(value)
		if total > maxAggregateTextBytes {
			return validation(field, "exceeds 1 MiB total text")
		}
	}
	return nil
}
