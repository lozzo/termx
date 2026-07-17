package runtime

import (
	"context"
	"strings"
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

// TerminalAttachRequest 描述 consumer 希望为 terminal view 建立 attachment 的意图。
type TerminalAttachRequest struct {
	Ref          TerminalRef
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

// TerminalAttachResult 返回 daemon 确认后的 attachment stamp、尺寸和 resize ownership。
type TerminalAttachResult struct {
	Stamp          AttachmentStamp
	Size           TerminalSize
	CanResize      bool
	ResizeReason   string
	SizeLocked     bool
	OwnerSurfaceID string
	OwnerViewID    string
	ResizeEpoch    uint64
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
	Size         TerminalSize
	ResizePolicy string
}

// TerminalResizeResult 返回 daemon 对 resize ownership、epoch 和最终尺寸的确认。
type TerminalResizeResult struct {
	Size           TerminalSize
	Resized        bool
	CanResize      bool
	ResizeReason   string
	SizeLocked     bool
	OwnerSurfaceID string
	OwnerViewID    string
	ResizeEpoch    uint64
}

// TerminalAttachmentApplication 是 attach/detach/input/resize 的窄 application interface。
// 实现必须先校验 attachment generation，再决定是否调用 adapter；stale cleanup 不能建立新 session。
type TerminalAttachmentApplication interface {
	AttachTerminal(context.Context, TerminalAttachRequest) (TerminalAttachResult, error)
	DetachTerminal(context.Context, TerminalDetachRequest) error
	SendTerminalInput(context.Context, TerminalInputRequest) error
	ResizeTerminal(context.Context, TerminalResizeRequest) (TerminalResizeResult, error)
}
