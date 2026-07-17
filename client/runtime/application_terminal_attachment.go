package runtime

import (
	"context"
	"strings"

	"github.com/lozzow/termx/client/endpoint"
	coreapi "github.com/lozzow/termx/core/api"
)

// AttachmentStamp 把 daemon attachment identity 固定到创建它的 endpoint session generation。
// detach/input/resize 必须携带原始 stamp，不能从当前 endpoint session 或 active pane 重建。
type AttachmentStamp struct {
	EndpointSessionStamp
	TerminalID  string
	Channel     uint16
	SurfaceID   string
	ViewID      string
	OperationID string
}

// Validate 校验 attachment 的 session fence 和 daemon/view identity 是否完整。
func (stamp AttachmentStamp) Validate() error {
	if err := stamp.EndpointSessionStamp.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(stamp.TerminalID) == "" || stamp.Channel == 0 ||
		strings.TrimSpace(stamp.SurfaceID) == "" || strings.TrimSpace(stamp.ViewID) == "" || strings.TrimSpace(stamp.OperationID) == "" {
		return runtimeError(ErrorInvalidRequest, "attachment terminal/channel/surface/view/operation identity is required", nil)
	}
	return nil
}

// TerminalRef 返回 attachment 绑定的 owning endpoint 与 daemon-local terminal identity。
func (stamp AttachmentStamp) TerminalRef() TerminalRef {
	return TerminalRef{EndpointID: stamp.EndpointID, TerminalID: stamp.TerminalID}
}

// TerminalAttachRequest 只给 daemon-local attach spec 增加 owning endpoint 上下文。
type TerminalAttachRequest struct {
	EndpointID endpoint.EndpointID
	Spec       coreapi.TerminalAttachSpec
}

// TerminalAttachResult 把 daemon attach result 固定到创建它的 client session stamp。
type TerminalAttachResult struct {
	Stamp      AttachmentStamp
	Attachment coreapi.TerminalAttachResult
}

// TerminalDetachRequest 精确释放一个既有 attachment；stale stamp 不得触发 lazy dial。
type TerminalDetachRequest struct {
	Stamp AttachmentStamp
}

// TerminalInputRequest 携带非幂等 terminal input 与原始 attachment stamp。
// Data 在方法返回前归调用方所有；implementation 异步使用前必须复制，失败后不得隐式重放。
type TerminalInputRequest struct {
	Stamp AttachmentStamp
	Data  []byte
}

// TerminalResizeRequest 携带 owner view 的 resize intent 和原始 attachment stamp。
type TerminalResizeRequest struct {
	Stamp        AttachmentStamp
	Size         coreapi.TerminalSize
	ResizePolicy string
}

// TerminalResizeResult 把 daemon resize result 固定到发起操作的 attachment stamp。
type TerminalResizeResult struct {
	Stamp  AttachmentStamp
	Resize coreapi.TerminalResizeResult
}

// TerminalAttachmentApplication 是 attach/detach/input/resize 的窄 application interface。
// 实现必须先校验 attachment generation，再决定是否调用 adapter；stale cleanup 不能建立新 session。
type TerminalAttachmentApplication interface {
	AttachTerminal(context.Context, TerminalAttachRequest) (TerminalAttachResult, error)
	DetachTerminal(context.Context, TerminalDetachRequest) error
	SendTerminalInput(context.Context, TerminalInputRequest) error
	ResizeTerminal(context.Context, TerminalResizeRequest) (TerminalResizeResult, error)
}
