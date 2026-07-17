package core

import "context"

type protocolAdmissionLease struct{}

// Release 结束当前 request 的 connection-bound admission lease。
func (protocolAdmissionLease) Release() {}

// AcquireApplication 在当前 protocol request 生命周期内校验 immutable transport scope。
// request goroutine 本身就是 connection-bound lease；session close 会先 cancel，再等待该 goroutine退出。
func (session *protocolSession) AcquireApplication(ctx context.Context, admission ApplicationAdmission) (ApplicationAdmissionLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !applicationCapabilitySupported(admission.Capability) {
		return nil, ErrApplicationUnsupportedCapability
	}
	scope := session.scope.normalized()
	if scope.AllowDaemon {
		return protocolAdmissionLease{}, nil
	}
	if scope.MachineEventsOnly {
		return nil, ErrApplicationForbidden
	}
	target := admission.TerminalID
	if len(admission.ResourceToken) > 0 {
		attachment, err := session.attachmentForToken(admission.ResourceToken)
		if err != nil {
			return nil, ErrApplicationForbidden
		}
		target = attachment.TerminalID
	}
	if target == "" || target != scope.TerminalID {
		return nil, ErrApplicationForbidden
	}
	return protocolAdmissionLease{}, nil
}

func applicationCapabilitySupported(capability ApplicationCapability) bool {
	switch capability {
	case ApplicationCapabilityResourceLifecycle,
		ApplicationCapabilityTerminalLifecycle,
		ApplicationCapabilityTerminalAttachment,
		ApplicationCapabilityPathQuery:
		return true
	default:
		return false
	}
}

// CancelApplicationOperation 当前没有 daemon operation registry，因此始终 fail closed。
func (session *protocolSession) CancelApplicationOperation(context.Context, string) error {
	return ErrApplicationCancellationUnavailable
}

// ReleaseApplicationResource 释放当前 session registry 持有的 opaque attachment token。
func (session *protocolSession) ReleaseApplicationResource(_ context.Context, token []byte) error {
	attachment, err := session.attachmentForToken(token)
	if err != nil {
		return err
	}
	session.detach(attachmentDetachRequest{Channel: attachment.Channel})
	return nil
}

// ApplicationTerminalDefaults 返回 owning daemon 机器的默认 shell 与 cwd。
func (session *protocolSession) ApplicationTerminalDefaults(context.Context) (TerminalDefaults, error) {
	return pathDefaults(), nil
}

// ApplicationTerminalCreate 把已由 API Mapping 构造的 core record 交给 lifecycle owner。
func (session *protocolSession) ApplicationTerminalCreate(_ context.Context, record TerminalRecord) (TerminalInfo, error) {
	return session.server.RegisterTerminal(record)
}

// ApplicationTerminalList 返回当前 owning daemon 的 terminal inventory。
func (session *protocolSession) ApplicationTerminalList(context.Context) ([]TerminalInfo, error) {
	return session.server.ListTerminals(), nil
}

// ApplicationTerminalGet 返回单个 daemon-local terminal lifecycle snapshot。
func (session *protocolSession) ApplicationTerminalGet(_ context.Context, terminalID string) (TerminalInfo, error) {
	return session.server.GetTerminal(terminalID)
}

// ApplicationTerminalAttachmentCount 返回当前 daemon registry 中 terminal 的活动 attachment 数量。
func (session *protocolSession) ApplicationTerminalAttachmentCount(terminalID string) int {
	return session.server.protocolAttachmentCount(terminalID)
}

// ApplicationTerminalRestart 请求 lifecycle owner 按保存的 process specification 重启 terminal。
func (session *protocolSession) ApplicationTerminalRestart(ctx context.Context, terminalID string) error {
	return session.server.RestartTerminal(ctx, terminalID)
}

// ApplicationTerminalKill 终止 terminal process，但保留 record 与 history truth。
func (session *protocolSession) ApplicationTerminalKill(ctx context.Context, terminalID string) error {
	return session.server.KillTerminal(ctx, terminalID)
}

// ApplicationTerminalRemove 删除满足 lifecycle 条件的 terminal record。
func (session *protocolSession) ApplicationTerminalRemove(_ context.Context, terminalID string) error {
	return session.server.RemoveTerminal(terminalID)
}

// ApplicationTerminalSetMetadata 原子更新 terminal name 与 tags。
func (session *protocolSession) ApplicationTerminalSetMetadata(ctx context.Context, terminalID, name string, tags map[string]string) error {
	_, err := session.server.SetMetadata(ctx, terminalID, name, tags)
	return err
}

// ApplicationTerminalSetTags 替换 terminal tags，同时保留 daemon-owned name。
func (session *protocolSession) ApplicationTerminalSetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	info, err := session.server.GetTerminal(terminalID)
	if err != nil {
		return err
	}
	_, err = session.server.SetMetadata(ctx, terminalID, info.Name, tags)
	return err
}

type protocolAttachTransaction struct {
	session    *protocolSession
	attachment protocolAttachment
	result     TerminalAttachment
	committed  bool
}

// Result 返回 pending attachment 的 core-native 不可变投影。
func (transaction *protocolAttachTransaction) Result() TerminalAttachment {
	return transaction.result
}

// Commit 原子发布 pending attachment token。
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

// Rollback 释放尚未发布的 attachment，且可以重复调用。
func (transaction *protocolAttachTransaction) Rollback(context.Context) error {
	if transaction == nil || transaction.committed {
		return nil
	}
	transaction.session.detach(attachmentDetachRequest{Channel: transaction.attachment.Channel})
	return nil
}

// ApplicationTerminalAttach 创建 pending attachment；token 只在 transaction commit 后可查。
func (session *protocolSession) ApplicationTerminalAttach(_ context.Context, request TerminalAttachmentRequest) (TerminalAttachmentTransaction, error) {
	attachment, control, err := session.attach(attachmentRequest{
		TerminalID: request.TerminalID, Mode: string(request.Mode), ResizePolicy: string(request.ResizePolicy),
		SurfaceID: request.SurfaceID, ViewID: request.ViewID,
	}, false)
	if err != nil {
		return nil, err
	}
	info, err := session.server.GetTerminal(attachment.TerminalID)
	if err != nil {
		session.detach(attachmentDetachRequest{Channel: attachment.Channel})
		return nil, err
	}
	result := TerminalAttachment{
		Token: append([]byte(nil), attachment.Token...), TerminalID: attachment.TerminalID,
		Mode: TerminalAttachmentMode(attachment.Mode), ResizePolicy: TerminalResizePolicy(attachment.ResizePolicy), SurfaceID: attachment.SurfaceID,
		ViewID: attachment.ViewID, Size: info.Size, ResizeControl: applicationResizeControl(control),
	}
	return &protocolAttachTransaction{session: session, attachment: attachment, result: result}, nil
}

// ApplicationTerminalDetach 释放 opaque token 对应的 attachment。
func (session *protocolSession) ApplicationTerminalDetach(ctx context.Context, token []byte) error {
	return session.ReleaseApplicationResource(ctx, token)
}

// ApplicationTerminalInput 向 token owning attachment 写入 bytes，不做失败重放。
func (session *protocolSession) ApplicationTerminalInput(ctx context.Context, token, data []byte) error {
	attachment, err := session.attachmentForToken(token)
	if err != nil {
		return err
	}
	return session.input(ctx, attachmentInputRequest{
		TerminalID: attachment.TerminalID, Channel: attachment.Channel, SurfaceID: attachment.SurfaceID,
		ViewID: attachment.ViewID, Data: append([]byte(nil), data...),
	})
}

// ApplicationTerminalResize 协调 attachment resize owner 并返回 daemon 确认后的 size/control。
func (session *protocolSession) ApplicationTerminalResize(ctx context.Context, token []byte, requested Size, policy TerminalResizePolicy) (TerminalResizeResult, error) {
	attachment, err := session.attachmentForToken(token)
	if err != nil {
		return TerminalResizeResult{}, err
	}
	control, canResize, err := session.resizeControlForRequest(attachment, string(policy), attachment.SurfaceID, attachment.ViewID)
	if err != nil {
		return TerminalResizeResult{}, err
	}
	resized := false
	if canResize && requested != control.ResizeOwnership.Size {
		if err := session.server.ResizeTerminal(ctx, attachment.TerminalID, requested.Cols, requested.Rows); err != nil {
			return TerminalResizeResult{}, err
		}
		resized = true
		if policy == TerminalResizePolicyOwner {
			control = session.resizeControlForOwner(attachment, requested)
		}
	}
	info, err := session.server.GetTerminal(attachment.TerminalID)
	if err != nil {
		return TerminalResizeResult{}, err
	}
	return TerminalResizeResult{Size: info.Size, Resized: resized, ResizeControl: applicationResizeControl(control)}, nil
}

// ApplicationTerminalResizeLock 修改 attachment owner 的显式 size lock。
func (session *protocolSession) ApplicationTerminalResizeLock(_ context.Context, token []byte, locked bool) (TerminalResizeResult, error) {
	attachment, err := session.attachmentForToken(token)
	if err != nil {
		return TerminalResizeResult{}, err
	}
	control, err := session.setResizeLock(attachmentResizeControlRequest{
		TerminalID: attachment.TerminalID, Channel: attachment.Channel, ResizePolicy: attachment.ResizePolicy,
		SurfaceID: attachment.SurfaceID, ViewID: attachment.ViewID,
	}, locked)
	if err != nil {
		return TerminalResizeResult{}, err
	}
	info, err := session.server.GetTerminal(attachment.TerminalID)
	if err != nil {
		return TerminalResizeResult{}, err
	}
	return TerminalResizeResult{Size: info.Size, ResizeControl: applicationResizeControl(control)}, nil
}

// ApplicationPathListDirectories 查询 owning daemon 文件系统的目录候选。
func (session *protocolSession) ApplicationPathListDirectories(_ context.Context, prefix string, limit int) (PathDirectories, error) {
	return listPathDirectories(prefix, limit)
}

func applicationResizeControl(control *attachmentResizeControl) *TerminalResizeControl {
	if control == nil {
		return nil
	}
	out := &TerminalResizeControl{
		CanResize: control.CanResize, Reason: TerminalResizeReason(control.Reason), SizeLocked: control.SizeLocked,
		SurfaceID: control.SurfaceID, OwnerSurfaceID: control.OwnerSurfaceID, OwnerViewID: control.OwnerViewID,
	}
	if ownership := control.ResizeOwnership; ownership != nil {
		out.ResizeOwnership = &TerminalResizeOwnership{
			OwnerAttachmentID: ownership.OwnerAttachmentID, OwnerSurfaceID: ownership.OwnerSurfaceID,
			OwnerViewID: ownership.OwnerViewID, Size: ownership.Size, SizeLocked: ownership.SizeLocked, Epoch: ownership.Epoch,
		}
	}
	return out
}
