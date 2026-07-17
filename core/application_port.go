package core

import (
	"context"
	"errors"

	"github.com/lozzow/termx/proto/apipb"
)

// ApplicationCapability 表示 connection admission 需要的 core-native 能力类别。
// 它只参与 daemon 内部授权，不是公共 capability schema 的第二份真值。
type ApplicationCapability uint8

const (
	// ApplicationCapabilityResourceLifecycle 表示释放 session-owned resource。
	ApplicationCapabilityResourceLifecycle ApplicationCapability = iota + 1
	// ApplicationCapabilityTerminalLifecycle 表示 terminal lifecycle 与 metadata 操作。
	ApplicationCapabilityTerminalLifecycle
	// ApplicationCapabilityTerminalAttachment 表示 attachment、input 与 resize 操作。
	ApplicationCapabilityTerminalAttachment
	// ApplicationCapabilityPathQuery 表示 daemon 文件系统 path 查询。
	ApplicationCapabilityPathQuery
)

var (
	// ErrApplicationForbidden 表示连接身份有效，但请求不在 immutable transport scope 内。
	ErrApplicationForbidden = errors.New("application request is forbidden")
	// ErrApplicationUnsupportedCapability 表示连接没有协商请求所需能力。
	ErrApplicationUnsupportedCapability = errors.New("application capability is unsupported")
	// ErrApplicationCancellationUnavailable 表示 daemon 尚未发布 operation cancellation registry。
	ErrApplicationCancellationUnavailable = errors.New("application operation cancellation is unavailable")
)

// ApplicationAdmission 是 API Layer 映射后的 connection-bound 授权输入。
// TerminalID 与 ResourceToken 只能二选一表达目标；core 不读取 Proto command 推断权限。
type ApplicationAdmission struct {
	Capability    ApplicationCapability
	TerminalID    string
	ResourceToken []byte
}

// ApplicationAdmissionLease 覆盖一次 controller 执行期，保证连接 scope 不在校验后失效。
type ApplicationAdmissionLease interface {
	Release()
}

// TerminalDefaults 是 daemon 默认 shell 与 cwd 的 core-native 查询结果。
type TerminalDefaults struct {
	DefaultCommand []string
	DefaultCWD     string
}

// PathDirectoryEntry 是 path completion 查询返回的 core-native 目录项。
type PathDirectoryEntry struct {
	Name string
	Path string
}

// PathDirectories 是一次 path completion 窗口，不拥有文件系统 truth。
type PathDirectories struct {
	BasePath  string
	Entries   []PathDirectoryEntry
	Missing   bool
	Truncated bool
}

// TerminalAttachmentMode 是 core attachment registry 的交互权限模式。
type TerminalAttachmentMode string

const (
	// TerminalAttachmentModeCollaborator 允许 input，并参与 resize arbitration。
	TerminalAttachmentModeCollaborator TerminalAttachmentMode = "collaborator"
	// TerminalAttachmentModeObserver 只观察 terminal 输出。
	TerminalAttachmentModeObserver TerminalAttachmentMode = "observer"
)

// TerminalResizePolicy 是 core attachment registry 的 resize 角色。
type TerminalResizePolicy string

const (
	// TerminalResizePolicyOwner 请求成为 resize owner。
	TerminalResizePolicyOwner TerminalResizePolicy = "owner"
	// TerminalResizePolicyFollower 跟随当前 owner size。
	TerminalResizePolicyFollower TerminalResizePolicy = "follower"
	// TerminalResizePolicyObserver 不参与 resize ownership。
	TerminalResizePolicyObserver TerminalResizePolicy = "observer"
)

// TerminalResizeReason 解释 daemon 返回的 resize control 决策。
type TerminalResizeReason string

const (
	// TerminalResizeReasonOwner 表示当前 attachment 是 owner。
	TerminalResizeReasonOwner TerminalResizeReason = "owner"
	// TerminalResizeReasonFollower 表示当前 attachment 跟随 owner。
	TerminalResizeReasonFollower TerminalResizeReason = "follower"
	// TerminalResizeReasonObserver 表示 observer 不允许 resize。
	TerminalResizeReasonObserver TerminalResizeReason = "observer"
	// TerminalResizeReasonSizeLocked 表示 owner size 被显式锁定。
	TerminalResizeReasonSizeLocked TerminalResizeReason = "size_locked"
)

// TerminalAttachmentRequest 是建立 daemon attachment 的 core-native 输入。
type TerminalAttachmentRequest struct {
	TerminalID   string
	Mode         TerminalAttachmentMode
	ResizePolicy TerminalResizePolicy
	SurfaceID    string
	ViewID       string
}

// TerminalResizeOwnership 是 daemon attachment registry 的 resize owner 投影。
type TerminalResizeOwnership struct {
	OwnerAttachmentID string
	OwnerSurfaceID    string
	OwnerViewID       string
	Size              Size
	SizeLocked        bool
	Epoch             uint64
}

// TerminalResizeControl 是 resize arbitration 的 core-native 结果。
type TerminalResizeControl struct {
	CanResize       bool
	Reason          TerminalResizeReason
	SizeLocked      bool
	SurfaceID       string
	OwnerSurfaceID  string
	OwnerViewID     string
	ResizeOwnership *TerminalResizeOwnership
}

// TerminalAttachment 是尚未或已经发布的 daemon attachment 投影。
// Token 由 owning protocol session registry 验证，调用方只能原样传回。
type TerminalAttachment struct {
	Token         []byte
	TerminalID    string
	Mode          TerminalAttachmentMode
	ResizePolicy  TerminalResizePolicy
	SurfaceID     string
	ViewID        string
	Size          Size
	ResizeControl *TerminalResizeControl
}

// TerminalAttachmentTransaction 持有尚未发布的 attachment。
// API Layer 完成公开 handle 校验后调用 Commit，失败路径调用 Rollback。
type TerminalAttachmentTransaction interface {
	Result() TerminalAttachment
	Commit(context.Context) error
	Rollback(context.Context) error
}

// TerminalResizeResult 是 resize/lock 操作后的 daemon authoritative 状态。
type TerminalResizeResult struct {
	Size          Size
	Resized       bool
	ResizeControl *TerminalResizeControl
}

// ApplicationSessionPort 是单条 protocol connection 暴露给 API Layer 的 core-native 窄边界。
// 实现绑定 immutable transport scope 和 session-owned resource registry，不包含 Proto 字段转换。
type ApplicationSessionPort interface {
	// AcquireApplication 原子校验 connection scope，并返回覆盖 controller 执行期的 lease。
	AcquireApplication(context.Context, ApplicationAdmission) (ApplicationAdmissionLease, error)
	// CancelApplicationOperation 取消当前 session owning operation；未发布能力时必须 fail closed。
	CancelApplicationOperation(context.Context, string) error
	// ReleaseApplicationResource 按 opaque token 释放当前 session owning resource。
	ReleaseApplicationResource(context.Context, []byte) error
	// ApplicationTerminalDefaults 返回 owning daemon 机器的 shell/cwd 默认值。
	ApplicationTerminalDefaults(context.Context) (TerminalDefaults, error)
	// ApplicationTerminalCreate 把 core record 交给 terminal lifecycle owner。
	ApplicationTerminalCreate(context.Context, TerminalRecord) (TerminalInfo, error)
	// ApplicationTerminalList 返回当前 daemon 的 terminal inventory。
	ApplicationTerminalList(context.Context) ([]TerminalInfo, error)
	// ApplicationTerminalGet 返回单个 daemon-local terminal snapshot。
	ApplicationTerminalGet(context.Context, string) (TerminalInfo, error)
	// ApplicationTerminalAttachmentCount 返回 daemon registry 中的活动 attachment 数量。
	ApplicationTerminalAttachmentCount(string) int
	// ApplicationTerminalRestart 按保存的 process specification 重启 terminal。
	ApplicationTerminalRestart(context.Context, string) error
	// ApplicationTerminalKill 终止 process，但保留 record/history。
	ApplicationTerminalKill(context.Context, string) error
	// ApplicationTerminalRemove 删除满足 lifecycle 条件的 terminal record。
	ApplicationTerminalRemove(context.Context, string) error
	// ApplicationTerminalSetMetadata 原子更新 terminal name/tags。
	ApplicationTerminalSetMetadata(context.Context, string, string, map[string]string) error
	// ApplicationTerminalSetTags 替换 tags，同时保留 daemon-owned name。
	ApplicationTerminalSetTags(context.Context, string, map[string]string) error
	// ApplicationTerminalAttach 创建 pending attachment transaction。
	ApplicationTerminalAttach(context.Context, TerminalAttachmentRequest) (TerminalAttachmentTransaction, error)
	// ApplicationTerminalDetach 释放 opaque token owning attachment。
	ApplicationTerminalDetach(context.Context, []byte) error
	// ApplicationTerminalInput 向 token owning attachment 写入 bytes，失败不得隐式重放。
	ApplicationTerminalInput(context.Context, []byte, []byte) error
	// ApplicationTerminalResize 协调 resize ownership 并返回 authoritative control。
	ApplicationTerminalResize(context.Context, []byte, Size, TerminalResizePolicy) (TerminalResizeResult, error)
	// ApplicationTerminalResizeLock 修改 owner size lock 并返回最终 control。
	ApplicationTerminalResizeLock(context.Context, []byte, bool) (TerminalResizeResult, error)
	// ApplicationPathListDirectories 查询 owning daemon 文件系统目录候选。
	ApplicationPathListDirectories(context.Context, string, int) (PathDirectories, error)
}

// ApplicationExecutor 执行 framing 已解码的公共 Proto command。
// 实现由 API Layer 注入；core protocol session 不持有具体 controller 或 mapping。
type ApplicationExecutor interface {
	// Execute 执行单个完整 Proto envelope，并始终返回带 correlation 的 result。
	Execute(context.Context, *apipb.CommandEnvelope) *apipb.ResultEnvelope
}

// ApplicationExecutorFactory 为每条 ready protocol connection 建立同寿命 API executor。
type ApplicationExecutorFactory func(ApplicationSessionPort) ApplicationExecutor
