package apilayer

import (
	"context"

	apimapping "github.com/muxvia/muxvia/api_mapping"
	"github.com/muxvia/muxvia/proto/apipb"
)

// PlatformController 是 API Layer 到非 terminal core-native adapter 的 Proto 边界。
// 所有方法只接收 generated Proto，业务字段转换必须委托 API Mapping。
type PlatformController interface {
	// HistoryWindow 查询 core authoritative history window。
	HistoryWindow(context.Context, *apipb.EndpointSessionStamp, *apipb.HistoryWindowCommand) (*apipb.HistoryWindowResult, error)
	// HistoryCopy 从 frozen history token 复制文本。
	HistoryCopy(context.Context, *apipb.EndpointSessionStamp, *apipb.HistoryCopyCommand) (*apipb.HistoryCopyResult, error)
	// HistoryRelease 释放 owning core history token。
	HistoryRelease(context.Context, *apipb.EndpointSessionStamp, *apipb.HistoryReleaseCommand) (*apipb.AcknowledgeResult, error)
	// HistoryBacklogStatus 返回 history ingest 诊断投影。
	HistoryBacklogStatus(context.Context, *apipb.EndpointSessionStamp, *apipb.HistoryBacklogStatusCommand) (*apipb.HistoryBacklogStatusResult, error)
	// LiveScreen 读取 latest-only native screen。
	LiveScreen(context.Context, *apipb.EndpointSessionStamp, *apipb.LiveScreenGetCommand) (*apipb.NativeScreenResult, error)
	// LiveInvalidation 等待 observed revision 之后的唤醒边沿。
	LiveInvalidation(context.Context, *apipb.EndpointSessionStamp, *apipb.LiveInvalidationNextCommand) (*apipb.LiveInvalidationResult, error)
	// EventSubscribe 创建 session-owned filtered event resource。
	EventSubscribe(context.Context, *apipb.EndpointSessionStamp, *apipb.EventSubscribeCommand) (*apipb.EventSubscriptionResult, error)
	// FileList 查询 daemon-owned 目录窗口。
	FileList(context.Context, *apipb.EndpointSessionStamp, *apipb.FileListCommand) (*apipb.FileListResult, error)
	// FileStat 查询 daemon-owned path metadata。
	FileStat(context.Context, *apipb.EndpointSessionStamp, *apipb.FileStatCommand) (*apipb.FileStatResult, error)
	// FilePreview 有界读取 daemon-owned 文件内容。
	FilePreview(context.Context, *apipb.EndpointSessionStamp, *apipb.FilePreviewCommand) (*apipb.FilePreviewResult, error)
	// FileMkdir 创建 daemon-owned directory。
	FileMkdir(context.Context, *apipb.EndpointSessionStamp, *apipb.FileMkdirCommand) (*apipb.FileOperationResult, error)
	// FileRename 原子重命名 daemon-owned path。
	FileRename(context.Context, *apipb.EndpointSessionStamp, *apipb.FileRenameCommand) (*apipb.FileOperationResult, error)
	// FileDelete 删除 daemon-owned path。
	FileDelete(context.Context, *apipb.EndpointSessionStamp, *apipb.FileDeleteCommand) (*apipb.FileOperationResult, error)
	// FileCopy 批量复制 daemon-owned paths。
	FileCopy(context.Context, *apipb.EndpointSessionStamp, *apipb.FileCopyCommand) (*apipb.FileBatchResult, error)
	// FileMove 批量移动 daemon-owned paths。
	FileMove(context.Context, *apipb.EndpointSessionStamp, *apipb.FileMoveCommand) (*apipb.FileBatchResult, error)
	// FileDownloadOpen 创建 current-session download stream resource。
	FileDownloadOpen(context.Context, *apipb.EndpointSessionStamp, *apipb.FileDownloadOpenCommand) (*apipb.FileTransferOpenResult, error)
	// FileUploadOpen 创建或恢复 principal-bound upload。
	FileUploadOpen(context.Context, *apipb.EndpointSessionStamp, *apipb.FileUploadOpenCommand) (*apipb.FileTransferOpenResult, error)
	// FileTransferCancel 取消 current-session file transfer。
	FileTransferCancel(context.Context, *apipb.EndpointSessionStamp, *apipb.FileTransferCancelCommand) (*apipb.FileTransferCancelResult, error)
	// StorageGet 读取 opaque storage value。
	StorageGet(context.Context, *apipb.EndpointSessionStamp, *apipb.StorageGetCommand) (*apipb.StorageGetResult, error)
	// StoragePut 执行 opaque storage CAS put。
	StoragePut(context.Context, *apipb.EndpointSessionStamp, *apipb.StoragePutCommand) (*apipb.StoragePutResult, error)
	// StorageDelete 执行 opaque storage CAS delete。
	StorageDelete(context.Context, *apipb.EndpointSessionStamp, *apipb.StorageDeleteCommand) (*apipb.StorageDeleteResult, error)
	// StorageList 查询当前 storage identity prefix。
	StorageList(context.Context, *apipb.EndpointSessionStamp, *apipb.StorageListCommand) (*apipb.StorageListResult, error)
	// ClientAccessIdentity 返回 daemon public identity。
	ClientAccessIdentity(context.Context, *apipb.EndpointSessionStamp, *apipb.ClientAccessIdentityCommand) (*apipb.ClientAccessIdentityResult, error)
	// ClientAccessList 返回脱敏授权记录。
	ClientAccessList(context.Context, *apipb.EndpointSessionStamp, *apipb.ClientAccessListCommand) (*apipb.ClientAccessListResult, error)
	// ClientAccessTicketCreate 创建一次性 pairing ticket。
	ClientAccessTicketCreate(context.Context, *apipb.EndpointSessionStamp, *apipb.ClientAccessTicketCreateCommand) (*apipb.ClientAccessTicketCreateResult, error)
	// ClientAccessRevoke 撤销 owning daemon grant。
	ClientAccessRevoke(context.Context, *apipb.EndpointSessionStamp, *apipb.ClientAccessRevokeCommand) (*apipb.ClientAccessRevokeResult, error)
	// RemoteStatus 返回 daemon remote runtime 状态。
	RemoteStatus(context.Context, *apipb.EndpointSessionStamp, *apipb.RemoteStatusCommand) (*apipb.RemoteStatusResult, error)
	// RemotePairStart 创建 daemon-local remote pairing session。
	RemotePairStart(context.Context, *apipb.EndpointSessionStamp, *apipb.RemotePairStartCommand) (*apipb.RemotePairStartResult, error)
	// RemoteLocalEnable 启动 local remote runtime。
	RemoteLocalEnable(context.Context, *apipb.EndpointSessionStamp, *apipb.RemoteLocalEnableCommand) (*apipb.RemoteLocalStatusResult, error)
	// RemoteLocalStatus 查询 local remote runtime。
	RemoteLocalStatus(context.Context, *apipb.EndpointSessionStamp, *apipb.RemoteLocalStatusCommand) (*apipb.RemoteLocalStatusResult, error)
	// RemoteLocalDisable 停止 local remote runtime。
	RemoteLocalDisable(context.Context, *apipb.EndpointSessionStamp, *apipb.RemoteLocalDisableCommand) (*apipb.RemoteLocalStatusResult, error)
}

func isTerminalCommand(command *apipb.CommandEnvelope) bool {
	switch command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalDefaults, *apipb.CommandEnvelope_TerminalCreate,
		*apipb.CommandEnvelope_TerminalList, *apipb.CommandEnvelope_TerminalGet,
		*apipb.CommandEnvelope_TerminalRestart, *apipb.CommandEnvelope_TerminalKill,
		*apipb.CommandEnvelope_TerminalRemove, *apipb.CommandEnvelope_TerminalSetMetadata,
		*apipb.CommandEnvelope_TerminalSetTags, *apipb.CommandEnvelope_TerminalAttach,
		*apipb.CommandEnvelope_TerminalDetach, *apipb.CommandEnvelope_TerminalInput,
		*apipb.CommandEnvelope_TerminalResize, *apipb.CommandEnvelope_TerminalResizeLock,
		*apipb.CommandEnvelope_PathListDirectories:
		return true
	default:
		return false
	}
}

func (service *Service) executePlatform(ctx context.Context, session *apipb.EndpointSessionStamp, command *apipb.CommandEnvelope, requestContext *apipb.RequestContext) *apipb.ResultEnvelope {
	requestID := requestContext.GetRequestId()
	if service == nil || service.platform == nil {
		return unavailable(requestID, session, "platform controller is unavailable")
	}
	var validationErr error
	switch command.GetCommand().(type) {
	case *apipb.CommandEnvelope_HistoryWindow, *apipb.CommandEnvelope_HistoryCopy,
		*apipb.CommandEnvelope_HistoryRelease, *apipb.CommandEnvelope_HistoryBacklogStatus,
		*apipb.CommandEnvelope_LiveScreenGet, *apipb.CommandEnvelope_LiveInvalidationNext:
		validationErr = apimapping.ValidateHistoryLiveCommand(command)
	case *apipb.CommandEnvelope_EventSubscribe:
		validationErr = apimapping.ValidateEventSubscribeCommand(command)
	case *apipb.CommandEnvelope_ClientAccessIdentity, *apipb.CommandEnvelope_ClientAccessList,
		*apipb.CommandEnvelope_ClientAccessTicketCreate, *apipb.CommandEnvelope_ClientAccessRevoke,
		*apipb.CommandEnvelope_RemoteStatus, *apipb.CommandEnvelope_RemotePairStart,
		*apipb.CommandEnvelope_RemoteLocalEnable, *apipb.CommandEnvelope_RemoteLocalStatus,
		*apipb.CommandEnvelope_RemoteLocalDisable:
		validationErr = apimapping.ValidateAccessRemoteCommand(command)
	default:
		validationErr = apimapping.ValidateFileStorageCommand(command)
	}
	if validationErr != nil {
		return errorResult(requestID, session, apimapping.ErrorToProto(validationErr, false))
	}
	var result any
	var err error
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_HistoryWindow:
		result, err = service.platform.HistoryWindow(ctx, cloneSession(session), cloneMessage(value.HistoryWindow))
	case *apipb.CommandEnvelope_HistoryCopy:
		result, err = service.platform.HistoryCopy(ctx, cloneSession(session), cloneMessage(value.HistoryCopy))
	case *apipb.CommandEnvelope_HistoryRelease:
		result, err = service.platform.HistoryRelease(ctx, cloneSession(session), cloneMessage(value.HistoryRelease))
	case *apipb.CommandEnvelope_HistoryBacklogStatus:
		result, err = service.platform.HistoryBacklogStatus(ctx, cloneSession(session), cloneMessage(value.HistoryBacklogStatus))
	case *apipb.CommandEnvelope_LiveScreenGet:
		result, err = service.platform.LiveScreen(ctx, cloneSession(session), cloneMessage(value.LiveScreenGet))
	case *apipb.CommandEnvelope_LiveInvalidationNext:
		result, err = service.platform.LiveInvalidation(ctx, cloneSession(session), cloneMessage(value.LiveInvalidationNext))
	case *apipb.CommandEnvelope_EventSubscribe:
		result, err = service.platform.EventSubscribe(ctx, cloneSession(session), cloneMessage(value.EventSubscribe))
	case *apipb.CommandEnvelope_FileList:
		result, err = service.platform.FileList(ctx, cloneSession(session), cloneMessage(value.FileList))
	case *apipb.CommandEnvelope_FileStat:
		result, err = service.platform.FileStat(ctx, cloneSession(session), cloneMessage(value.FileStat))
	case *apipb.CommandEnvelope_FilePreview:
		result, err = service.platform.FilePreview(ctx, cloneSession(session), cloneMessage(value.FilePreview))
	case *apipb.CommandEnvelope_FileMkdir:
		result, err = service.platform.FileMkdir(ctx, cloneSession(session), cloneMessage(value.FileMkdir))
	case *apipb.CommandEnvelope_FileRename:
		result, err = service.platform.FileRename(ctx, cloneSession(session), cloneMessage(value.FileRename))
	case *apipb.CommandEnvelope_FileDelete:
		result, err = service.platform.FileDelete(ctx, cloneSession(session), cloneMessage(value.FileDelete))
	case *apipb.CommandEnvelope_FileCopy:
		result, err = service.platform.FileCopy(ctx, cloneSession(session), cloneMessage(value.FileCopy))
	case *apipb.CommandEnvelope_FileMove:
		result, err = service.platform.FileMove(ctx, cloneSession(session), cloneMessage(value.FileMove))
	case *apipb.CommandEnvelope_FileDownloadOpen:
		result, err = service.platform.FileDownloadOpen(ctx, cloneSession(session), cloneMessage(value.FileDownloadOpen))
	case *apipb.CommandEnvelope_FileUploadOpen:
		result, err = service.platform.FileUploadOpen(ctx, cloneSession(session), cloneMessage(value.FileUploadOpen))
	case *apipb.CommandEnvelope_FileTransferCancel:
		result, err = service.platform.FileTransferCancel(ctx, cloneSession(session), cloneMessage(value.FileTransferCancel))
	case *apipb.CommandEnvelope_StorageGet:
		result, err = service.platform.StorageGet(ctx, cloneSession(session), cloneMessage(value.StorageGet))
	case *apipb.CommandEnvelope_StoragePut:
		result, err = service.platform.StoragePut(ctx, cloneSession(session), cloneMessage(value.StoragePut))
	case *apipb.CommandEnvelope_StorageDelete:
		result, err = service.platform.StorageDelete(ctx, cloneSession(session), cloneMessage(value.StorageDelete))
	case *apipb.CommandEnvelope_StorageList:
		result, err = service.platform.StorageList(ctx, cloneSession(session), cloneMessage(value.StorageList))
	case *apipb.CommandEnvelope_ClientAccessIdentity:
		result, err = service.platform.ClientAccessIdentity(ctx, cloneSession(session), cloneMessage(value.ClientAccessIdentity))
	case *apipb.CommandEnvelope_ClientAccessList:
		result, err = service.platform.ClientAccessList(ctx, cloneSession(session), cloneMessage(value.ClientAccessList))
	case *apipb.CommandEnvelope_ClientAccessTicketCreate:
		result, err = service.platform.ClientAccessTicketCreate(ctx, cloneSession(session), cloneMessage(value.ClientAccessTicketCreate))
	case *apipb.CommandEnvelope_ClientAccessRevoke:
		result, err = service.platform.ClientAccessRevoke(ctx, cloneSession(session), cloneMessage(value.ClientAccessRevoke))
	case *apipb.CommandEnvelope_RemoteStatus:
		result, err = service.platform.RemoteStatus(ctx, cloneSession(session), cloneMessage(value.RemoteStatus))
	case *apipb.CommandEnvelope_RemotePairStart:
		result, err = service.platform.RemotePairStart(ctx, cloneSession(session), cloneMessage(value.RemotePairStart))
	case *apipb.CommandEnvelope_RemoteLocalEnable:
		result, err = service.platform.RemoteLocalEnable(ctx, cloneSession(session), cloneMessage(value.RemoteLocalEnable))
	case *apipb.CommandEnvelope_RemoteLocalStatus:
		result, err = service.platform.RemoteLocalStatus(ctx, cloneSession(session), cloneMessage(value.RemoteLocalStatus))
	case *apipb.CommandEnvelope_RemoteLocalDisable:
		result, err = service.platform.RemoteLocalDisable(ctx, cloneSession(session), cloneMessage(value.RemoteLocalDisable))
	default:
		return errorResult(requestID, session, apimapping.ErrorToProto(&apimapping.ValidationError{Field: "command", Reason: "unsupported platform command"}, false))
	}
	if err != nil || result == nil {
		return errorResult(requestID, session, apimapping.ErrorToProto(err, true))
	}
	out := &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session)}
	switch value := result.(type) {
	case *apipb.AcknowledgeResult:
		out.Result = &apipb.ResultEnvelope_Acknowledge{Acknowledge: value}
	case *apipb.HistoryWindowResult:
		out.Result = &apipb.ResultEnvelope_HistoryWindow{HistoryWindow: value}
	case *apipb.HistoryCopyResult:
		out.Result = &apipb.ResultEnvelope_HistoryCopy{HistoryCopy: value}
	case *apipb.HistoryBacklogStatusResult:
		out.Result = &apipb.ResultEnvelope_HistoryBacklogStatus{HistoryBacklogStatus: value}
	case *apipb.NativeScreenResult:
		out.Result = &apipb.ResultEnvelope_LiveScreen{LiveScreen: value}
	case *apipb.LiveInvalidationResult:
		out.Result = &apipb.ResultEnvelope_LiveInvalidation{LiveInvalidation: value}
	case *apipb.EventSubscriptionResult:
		out.Result = &apipb.ResultEnvelope_EventSubscription{EventSubscription: value}
	case *apipb.FileListResult:
		out.Result = &apipb.ResultEnvelope_FileList{FileList: value}
	case *apipb.FileStatResult:
		out.Result = &apipb.ResultEnvelope_FileStat{FileStat: value}
	case *apipb.FilePreviewResult:
		out.Result = &apipb.ResultEnvelope_FilePreview{FilePreview: value}
	case *apipb.FileOperationResult:
		out.Result = &apipb.ResultEnvelope_FileOperation{FileOperation: value}
	case *apipb.FileBatchResult:
		out.Result = &apipb.ResultEnvelope_FileBatch{FileBatch: value}
	case *apipb.FileTransferOpenResult:
		out.Result = &apipb.ResultEnvelope_FileTransferOpen{FileTransferOpen: value}
	case *apipb.FileTransferCancelResult:
		out.Result = &apipb.ResultEnvelope_FileTransferCancel{FileTransferCancel: value}
	case *apipb.StorageGetResult:
		out.Result = &apipb.ResultEnvelope_StorageGet{StorageGet: value}
	case *apipb.StoragePutResult:
		out.Result = &apipb.ResultEnvelope_StoragePut{StoragePut: value}
	case *apipb.StorageDeleteResult:
		out.Result = &apipb.ResultEnvelope_StorageDelete{StorageDelete: value}
	case *apipb.StorageListResult:
		out.Result = &apipb.ResultEnvelope_StorageList{StorageList: value}
	case *apipb.ClientAccessIdentityResult:
		out.Result = &apipb.ResultEnvelope_ClientAccessIdentity{ClientAccessIdentity: value}
	case *apipb.ClientAccessListResult:
		out.Result = &apipb.ResultEnvelope_ClientAccessList{ClientAccessList: value}
	case *apipb.ClientAccessTicketCreateResult:
		out.Result = &apipb.ResultEnvelope_ClientAccessTicketCreate{ClientAccessTicketCreate: value}
	case *apipb.ClientAccessRevokeResult:
		out.Result = &apipb.ResultEnvelope_ClientAccessRevoke{ClientAccessRevoke: value}
	case *apipb.RemoteStatusResult:
		out.Result = &apipb.ResultEnvelope_RemoteStatus{RemoteStatus: value}
	case *apipb.RemotePairStartResult:
		out.Result = &apipb.ResultEnvelope_RemotePairStart{RemotePairStart: value}
	case *apipb.RemoteLocalStatusResult:
		out.Result = &apipb.ResultEnvelope_RemoteLocalStatus{RemoteLocalStatus: value}
	default:
		return unavailable(requestID, session, "platform controller returned an unsupported result")
	}
	return out
}
