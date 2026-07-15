package core

import (
	"context"
	"errors"

	"github.com/lozzow/termx/internal/protocol"
)

// ErrClientAccessServiceUnavailable 表示当前 daemon 未装配 DeviceIdentity/AccessStore 管理边界。
// local owner 与 ManageClientAccess session 都必须 fail closed，不能回退到旧 bearer pair 命令或直接写 credential 文件。
var ErrClientAccessServiceUnavailable = errors.New("client access service is not configured")

// ClientAccessService 是 core protocol session 调用 daemon-owned identity/pair/access runtime 的 typed hook。
// core 只负责认证后 method scope；ticket、grant、key binding、receipt、撤销和持久化 truth 全部由实现方的 remoteauth.AccessStore 持有。
type ClientAccessService interface {
	// Identity 返回 daemon DeviceIdentity 的公开投影；实现不得返回或记录私钥。
	Identity(ctx context.Context) (protocol.ClientAccessIdentityResult, error)
	// CreateTicket 由 owning daemon 原子登记并签发一次性 PairingTicket bundle。
	CreateTicket(ctx context.Context, params protocol.ClientAccessTicketCreateParams) (protocol.ClientAccessTicketCreateResult, error)
	// List 返回不含 ticket、grant body 或 client public key bytes 的脱敏授权投影。
	List(ctx context.Context) (protocol.ClientAccessListResult, error)
	// Revoke 按 GrantID 持久化撤销；实现失败时不得只修改 core session 内存。
	Revoke(ctx context.Context, params protocol.ClientAccessRevokeParams) (protocol.ClientAccessRecord, error)
}
