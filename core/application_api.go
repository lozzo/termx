package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	apilayer "github.com/lozzow/termx/api_layer"
	apimapping "github.com/lozzow/termx/api_mapping"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
)

type applicationExecutor interface {
	Execute(context.Context, *apipb.CommandEnvelope) *apipb.ResultEnvelope
}

func newApplicationExecutor(session *protocolSession) applicationExecutor {
	return apilayer.NewService(session, session, session, session)
}

func (session *protocolSession) dispatchApplicationPayload(ctx context.Context, payload []byte) ([]byte, bool, int, error) {
	decoded, err := protocol.DecodeMethodParams("api.execute", payload)
	if err != nil {
		return nil, false, protocolErrorBadRequest, err
	}
	command, ok := decoded.(*apipb.CommandEnvelope)
	if !ok || command == nil {
		return nil, false, protocolErrorBadRequest, fmt.Errorf("api.execute decoded unexpected payload %T", decoded)
	}
	result := session.application.Execute(ctx, command)
	payload, err = protocol.EncodeMethodResult("api.execute", result)
	if err != nil {
		return nil, false, protocolErrorInternal, err
	}
	return payload, false, 0, nil
}

type protocolAdmissionLease struct{}

func (protocolAdmissionLease) Release() {}

// Acquire 在当前 protocol request waitgroup 生命周期内校验 immutable transport scope。
// request goroutine 本身就是 connection-bound lease；session close 会先 cancel，再等待该 goroutine 退出。
func (session *protocolSession) Acquire(ctx context.Context, command *apipb.CommandEnvelope, required apipb.ApiCapability) (apilayer.AdmissionLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !applicationCapabilitySupported(required) {
		return nil, apilayer.ErrAdmissionUnsupportedCapability
	}
	if err := session.authorizeApplicationCommand(command); err != nil {
		return nil, err
	}
	return protocolAdmissionLease{}, nil
}

func applicationCapabilitySupported(capability apipb.ApiCapability) bool {
	switch capability {
	case apipb.ApiCapability_API_CAPABILITY_UNSPECIFIED,
		apipb.ApiCapability_API_CAPABILITY_RESOURCE_LIFECYCLE,
		apipb.ApiCapability_API_CAPABILITY_TERMINAL_LIFECYCLE,
		apipb.ApiCapability_API_CAPABILITY_TERMINAL_ATTACHMENT,
		apipb.ApiCapability_API_CAPABILITY_PATH_QUERY:
		return true
	default:
		return false
	}
}

func (session *protocolSession) authorizeApplicationCommand(command *apipb.CommandEnvelope) error {
	scope := session.scope.normalized()
	if scope.AllowDaemon {
		return nil
	}
	if scope.MachineEventsOnly {
		return apilayer.ErrAdmissionForbidden
	}
	target, ok := session.applicationCommandTerminal(command)
	if !ok || target == "" || target != scope.TerminalID {
		return apilayer.ErrAdmissionForbidden
	}
	return nil
}

func (session *protocolSession) applicationCommandTerminal(command *apipb.CommandEnvelope) (string, bool) {
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalGet:
		return value.TerminalGet.GetTerminal().GetTerminalId(), true
	case *apipb.CommandEnvelope_TerminalRestart:
		return value.TerminalRestart.GetTerminal().GetTerminalId(), true
	case *apipb.CommandEnvelope_TerminalKill:
		return value.TerminalKill.GetTerminal().GetTerminalId(), true
	case *apipb.CommandEnvelope_TerminalRemove:
		return value.TerminalRemove.GetTerminal().GetTerminalId(), true
	case *apipb.CommandEnvelope_TerminalSetMetadata:
		return value.TerminalSetMetadata.GetTerminal().GetTerminalId(), true
	case *apipb.CommandEnvelope_TerminalSetTags:
		return value.TerminalSetTags.GetTerminal().GetTerminalId(), true
	case *apipb.CommandEnvelope_TerminalAttach:
		return value.TerminalAttach.GetTerminal().GetTerminalId(), true
	case *apipb.CommandEnvelope_TerminalDetach:
		return session.applicationResourceTerminal(value.TerminalDetach.GetAttachment())
	case *apipb.CommandEnvelope_TerminalInput:
		return session.applicationResourceTerminal(value.TerminalInput.GetAttachment())
	case *apipb.CommandEnvelope_TerminalResize:
		return session.applicationResourceTerminal(value.TerminalResize.GetAttachment())
	case *apipb.CommandEnvelope_TerminalResizeLock:
		return session.applicationResourceTerminal(value.TerminalResizeLock.GetAttachment())
	case *apipb.CommandEnvelope_ReleaseResource:
		return session.applicationResourceTerminal(value.ReleaseResource.GetResource())
	default:
		return "", false
	}
}

func (session *protocolSession) applicationResourceTerminal(resource *apipb.ResourceHandle) (string, bool) {
	attachment, err := session.attachmentForToken(resource.GetOpaqueToken())
	if err != nil {
		return "", false
	}
	return attachment.TerminalID, true
}

// CancelOperation 当前没有 daemon operation registry；admission 不发布该 capability，因此该方法 fail closed。
func (session *protocolSession) CancelOperation(context.Context, *apipb.OperationStamp) error {
	return &apimapping.ClassifiedError{Err: errors.New("operation cancellation is unavailable"), Code: apipb.ApiErrorCode_API_ERROR_CODE_UNAVAILABLE, Retryable: true}
}

// ReleaseResource 释放 session-owned opaque resource；当前 terminal 切片只接受 attachment handle。
func (session *protocolSession) ReleaseResource(_ context.Context, resource *apipb.ResourceHandle) error {
	attachment, err := session.attachmentForToken(resource.GetOpaqueToken())
	if err != nil {
		return classifyCoreApplicationError(err)
	}
	session.detach(attachmentDetachRequest{Channel: attachment.Channel})
	return nil
}

func (session *protocolSession) TerminalDefaults(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalDefaultsCommand) (*apipb.TerminalDefaultsResult, error) {
	defaults := pathDefaults()
	return &apipb.TerminalDefaultsResult{Defaults: defaults}, nil
}

func (session *protocolSession) TerminalCreate(_ context.Context, origin *apipb.EndpointSessionStamp, command *apipb.TerminalCreateCommand) (*apipb.TerminalCreateResult, error) {
	spec := command.GetTerminal()
	info, err := session.server.RegisterTerminal(TerminalRecord{
		ID: spec.GetTerminalId(), Name: spec.GetName(), Command: append([]string(nil), spec.GetCommand()...), Tags: cloneStringMap(spec.GetTags()),
		Size:    Size{Cols: uint16(spec.GetSize().GetCols()), Rows: uint16(spec.GetSize().GetRows())},
		Options: TerminalCreateOptions{Dir: spec.GetCwd(), Env: append([]string(nil), spec.GetEnv()...), ScrollbackSize: int(spec.GetScrollbackRows()), ScrollbackMaxBytes: spec.GetScrollbackMaxBytes(), ScrollbackMaxAge: time.Duration(spec.GetScrollbackMaxAgeSeconds()) * time.Second},
	})
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	projection, err := session.terminalInfoToAPI(origin.GetEndpointId(), info)
	if err != nil {
		return nil, err
	}
	return &apipb.TerminalCreateResult{Terminal: projection}, nil
}

func (session *protocolSession) TerminalList(_ context.Context, origin *apipb.EndpointSessionStamp, _ *apipb.TerminalListCommand) (*apipb.TerminalListResult, error) {
	items := session.server.ListTerminals()
	result := &apipb.TerminalListResult{Terminals: make([]*apipb.TerminalInfo, 0, len(items))}
	for _, item := range items {
		projection, err := session.terminalInfoToAPI(origin.GetEndpointId(), item)
		if err != nil {
			return nil, err
		}
		result.Terminals = append(result.Terminals, projection)
	}
	return result, nil
}

func (session *protocolSession) TerminalGet(_ context.Context, origin *apipb.EndpointSessionStamp, command *apipb.TerminalGetCommand) (*apipb.TerminalGetResult, error) {
	info, err := session.server.GetTerminal(command.GetTerminal().GetTerminalId())
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	projection, err := session.terminalInfoToAPI(origin.GetEndpointId(), info)
	if err != nil {
		return nil, err
	}
	return &apipb.TerminalGetResult{Terminal: projection}, nil
}

func (session *protocolSession) TerminalRestart(ctx context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalRestartCommand) error {
	return classifyCoreApplicationError(session.server.RestartTerminal(ctx, command.GetTerminal().GetTerminalId()))
}

func (session *protocolSession) TerminalKill(ctx context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalKillCommand) error {
	return classifyCoreApplicationError(session.server.KillTerminal(ctx, command.GetTerminal().GetTerminalId()))
}

func (session *protocolSession) TerminalRemove(_ context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalRemoveCommand) error {
	return classifyCoreApplicationError(session.server.RemoveTerminal(command.GetTerminal().GetTerminalId()))
}

func (session *protocolSession) TerminalSetMetadata(ctx context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalSetMetadataCommand) error {
	_, err := session.server.SetMetadata(ctx, command.GetTerminal().GetTerminalId(), command.GetName(), command.GetTags())
	return classifyCoreApplicationError(err)
}

func (session *protocolSession) TerminalSetTags(ctx context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalSetTagsCommand) error {
	id := command.GetTerminal().GetTerminalId()
	info, err := session.server.GetTerminal(id)
	if err != nil {
		return classifyCoreApplicationError(err)
	}
	_, err = session.server.SetMetadata(ctx, id, info.Name, command.GetTags())
	return classifyCoreApplicationError(err)
}

type protocolAttachTransaction struct {
	session    *protocolSession
	attachment protocolAttachment
	result     *apipb.TerminalAttachResult
	committed  bool
}

func (transaction *protocolAttachTransaction) Result() *apipb.TerminalAttachResult {
	return transaction.result
}
func (transaction *protocolAttachTransaction) Commit(context.Context) error {
	if transaction == nil || transaction.committed {
		return nil
	}
	if err := transaction.session.publishAttachmentToken(transaction.attachment); err != nil {
		return err
	}
	transaction.committed = true
	return nil
}
func (transaction *protocolAttachTransaction) Rollback(context.Context) error {
	if transaction == nil || transaction.committed {
		return nil
	}
	transaction.session.detach(attachmentDetachRequest{Channel: transaction.attachment.Channel})
	return nil
}

func (session *protocolSession) TerminalAttach(_ context.Context, origin *apipb.EndpointSessionStamp, command *apipb.TerminalAttachCommand) (apilayer.TerminalAttachTransaction, error) {
	attachment, control, err := session.attach(attachmentRequest{
		TerminalID: command.GetTerminal().GetTerminalId(), Mode: attachmentModeFromAPI(command.GetMode()), ResizePolicy: resizePolicyFromAPI(command.GetResizePolicy()), SurfaceID: command.GetSurfaceId(), ViewID: command.GetViewId(),
	}, false)
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	info, err := session.server.GetTerminal(attachment.TerminalID)
	if err != nil {
		session.detach(attachmentDetachRequest{Channel: attachment.Channel})
		return nil, classifyCoreApplicationError(err)
	}
	result := &apipb.TerminalAttachResult{
		Attachment: &apipb.AttachmentHandle{
			Resource: &apipb.ResourceHandle{OpaqueToken: append([]byte(nil), attachment.Token...), Kind: apipb.ResourceKind_RESOURCE_KIND_TERMINAL_ATTACHMENT, Session: origin, Generation: 1},
			Terminal: command.GetTerminal(), Operation: command.GetOperation(), SurfaceId: command.GetSurfaceId(), ViewId: command.GetViewId(),
		},
		Mode: command.GetMode(), ResizePolicy: command.GetResizePolicy(), Size: terminalSizeToAPI(info.Size), ResizeControl: resizeControlToAPI(control),
	}
	return &protocolAttachTransaction{session: session, attachment: attachment, result: result}, nil
}

func (session *protocolSession) TerminalDetach(_ context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalDetachCommand) error {
	attachment, err := session.attachmentForToken(command.GetAttachment().GetOpaqueToken())
	if err != nil {
		return classifyCoreApplicationError(err)
	}
	session.detach(attachmentDetachRequest{Channel: attachment.Channel})
	return nil
}

func (session *protocolSession) TerminalInput(ctx context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalInputCommand) error {
	attachment, err := session.attachmentForToken(command.GetAttachment().GetOpaqueToken())
	if err != nil {
		return classifyCoreApplicationError(err)
	}
	err = session.input(ctx, attachmentInputRequest{TerminalID: attachment.TerminalID, Channel: attachment.Channel, SurfaceID: attachment.SurfaceID, ViewID: attachment.ViewID, Data: append([]byte(nil), command.GetData()...)})
	return classifyCoreApplicationError(err)
}

func (session *protocolSession) TerminalResize(ctx context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalResizeCommand) (*apipb.TerminalResizeResult, error) {
	attachment, err := session.attachmentForToken(command.GetAttachment().GetOpaqueToken())
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	control, canResize, err := session.resizeControlForRequest(attachment, resizePolicyFromAPI(command.GetResizePolicy()), attachment.SurfaceID, attachment.ViewID)
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	requested := Size{Cols: uint16(command.GetSize().GetCols()), Rows: uint16(command.GetSize().GetRows())}
	resized := false
	if canResize && requested != control.ResizeOwnership.Size {
		if err := session.server.ResizeTerminal(ctx, attachment.TerminalID, requested.Cols, requested.Rows); err != nil {
			return nil, classifyCoreApplicationError(err)
		}
		resized = true
		if command.GetResizePolicy() == apipb.ResizePolicy_RESIZE_POLICY_OWNER {
			control = session.resizeControlForOwner(attachment, requested)
		}
	}
	info, err := session.server.GetTerminal(attachment.TerminalID)
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	return &apipb.TerminalResizeResult{Size: terminalSizeToAPI(info.Size), Resized: resized, ResizeControl: resizeControlToAPI(control)}, nil
}

func (session *protocolSession) TerminalResizeLock(_ context.Context, _ *apipb.EndpointSessionStamp, command *apipb.TerminalResizeLockCommand) (*apipb.TerminalResizeResult, error) {
	attachment, err := session.attachmentForToken(command.GetAttachment().GetOpaqueToken())
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	control, err := session.setResizeLock(attachmentResizeControlRequest{TerminalID: attachment.TerminalID, Channel: attachment.Channel, ResizePolicy: attachment.ResizePolicy, SurfaceID: attachment.SurfaceID, ViewID: attachment.ViewID}, command.GetLocked())
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	info, err := session.server.GetTerminal(attachment.TerminalID)
	if err != nil {
		return nil, classifyCoreApplicationError(err)
	}
	return &apipb.TerminalResizeResult{Size: terminalSizeToAPI(info.Size), ResizeControl: resizeControlToAPI(control)}, nil
}

func (session *protocolSession) PathListDirectories(_ context.Context, _ *apipb.EndpointSessionStamp, command *apipb.PathListDirectoriesCommand) (*apipb.PathListDirectoriesResult, error) {
	result, err := listPathDirectories(command)
	if err != nil {
		return nil, &apimapping.ClassifiedError{Err: err, Code: apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL}
	}
	return result, nil
}

func (session *protocolSession) terminalInfoToAPI(endpointID string, info TerminalInfo) (*apipb.TerminalInfo, error) {
	state, err := terminalStateToAPI(info.State)
	if err != nil {
		return nil, err
	}
	if info.Resources.PID < math.MinInt32 || info.Resources.PID > math.MaxInt32 || info.Resources.CPUPercentX100 < math.MinInt32 || info.Resources.CPUPercentX100 > math.MaxInt32 {
		return nil, fmt.Errorf("terminal resource usage exceeds public API integer range")
	}
	out := &apipb.TerminalInfo{
		Ref: &apipb.TerminalRef{EndpointId: endpointID, TerminalId: info.ID}, Name: info.Name, Command: append([]string(nil), info.Command...), Tags: cloneStringMap(info.Tags), Size: terminalSizeToAPI(info.Size), State: state,
		Cwd: info.CWD, LiveCwd: info.LiveCWD, CreatedAtUnixNano: unixNanoOrZero(info.CreatedAt), ExitedAtUnixNano: unixNanoOrZero(info.ExitedAt), AttachmentCount: int32(session.server.protocolAttachmentCount(info.ID)),
		Resources: &apipb.TerminalResourceUsage{Pid: int32(info.Resources.PID), CpuPercentX100: int32(info.Resources.CPUPercentX100), MemoryBytes: info.Resources.MemoryBytes, SampledAtUnixNano: unixNanoOrZero(info.Resources.SampledAt)},
	}
	if info.ExitCode != nil {
		if *info.ExitCode < math.MinInt32 || *info.ExitCode > math.MaxInt32 {
			return nil, fmt.Errorf("terminal exit code exceeds public API integer range")
		}
		value := int32(*info.ExitCode)
		out.ExitCode = &value
	}
	return out, nil
}

func classifyCoreApplicationError(err error) error {
	if err == nil {
		return nil
	}
	classified := &apimapping.ClassifiedError{Err: err, Code: apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL}
	switch {
	case errors.Is(err, ErrInvalidTerminalID), errors.Is(err, ErrInvalidCommand), errors.Is(err, ErrInvalidServerSize):
		classified.Code = apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST
	case errors.Is(err, ErrTerminalNotFound):
		classified.Code = apipb.ApiErrorCode_API_ERROR_CODE_NOT_FOUND
	case errors.Is(err, ErrDuplicateTerminal), errors.Is(err, ErrTerminalExited), errors.Is(err, errProtocolAttachmentMismatch):
		classified.Code = apipb.ApiErrorCode_API_ERROR_CODE_CONFLICT
	case errors.Is(err, ErrServerClosed), errors.Is(err, ErrHistoryNotRebuilt):
		classified.Code = apipb.ApiErrorCode_API_ERROR_CODE_UNAVAILABLE
		classified.Retryable = true
	}
	return classified
}

func terminalStateToAPI(state TerminalState) (apipb.TerminalState, error) {
	switch state {
	case TerminalStateCreated:
		return apipb.TerminalState_TERMINAL_STATE_CREATED, nil
	case TerminalStateRunning:
		return apipb.TerminalState_TERMINAL_STATE_RUNNING, nil
	case TerminalStateExited:
		return apipb.TerminalState_TERMINAL_STATE_EXITED, nil
	case TerminalStateRemoved:
		return apipb.TerminalState_TERMINAL_STATE_REMOVED, nil
	default:
		return apipb.TerminalState_TERMINAL_STATE_UNSPECIFIED, fmt.Errorf("unsupported core terminal state %q", state)
	}
}

func terminalSizeToAPI(size Size) *apipb.TerminalSize {
	return &apipb.TerminalSize{Cols: uint32(size.Cols), Rows: uint32(size.Rows)}
}

func attachmentModeFromAPI(mode apipb.AttachmentMode) string {
	if mode == apipb.AttachmentMode_ATTACHMENT_MODE_OBSERVER {
		return "observer"
	}
	return "collaborator"
}

func resizePolicyFromAPI(policy apipb.ResizePolicy) string {
	switch policy {
	case apipb.ResizePolicy_RESIZE_POLICY_FOLLOWER:
		return attachmentResizePolicyFollower
	case apipb.ResizePolicy_RESIZE_POLICY_OBSERVER:
		return attachmentResizePolicyObserver
	default:
		return attachmentResizePolicyOwner
	}
}

func resizeControlToAPI(control *attachmentResizeControl) *apipb.ResizeControl {
	if control == nil {
		return nil
	}
	out := &apipb.ResizeControl{CanResize: control.CanResize, Reason: resizeControlReasonToAPI(control.Reason), SizeLocked: control.SizeLocked, SurfaceId: control.SurfaceID, OwnerSurfaceId: control.OwnerSurfaceID, OwnerViewId: control.OwnerViewID}
	if ownership := control.ResizeOwnership; ownership != nil {
		out.Ownership = &apipb.ResizeOwnership{OwnerAttachmentId: ownership.OwnerAttachmentID, OwnerSurfaceId: ownership.OwnerSurfaceID, OwnerViewId: ownership.OwnerViewID, Size: &apipb.TerminalSize{Cols: uint32(ownership.Size.Cols), Rows: uint32(ownership.Size.Rows)}, SizeLocked: ownership.SizeLocked, Epoch: ownership.Epoch}
	}
	return out
}

func resizeControlReasonToAPI(reason string) apipb.ResizeControlReason {
	switch reason {
	case attachmentResizeReasonOwner:
		return apipb.ResizeControlReason_RESIZE_CONTROL_REASON_OWNER
	case attachmentResizeReasonObserver:
		return apipb.ResizeControlReason_RESIZE_CONTROL_REASON_OBSERVER
	case attachmentResizeReasonSizeLocked:
		return apipb.ResizeControlReason_RESIZE_CONTROL_REASON_SIZE_LOCKED
	default:
		return apipb.ResizeControlReason_RESIZE_CONTROL_REASON_FOLLOWER
	}
}

func unixNanoOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}
