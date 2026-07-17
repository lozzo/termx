package runtime

import (
	"context"
	"strings"

	"github.com/lozzow/termx/client/endpoint"
	coreapi "github.com/lozzow/termx/core/api"
)

// TerminalRef 是 client application 跨 endpoint 引用 daemon-local terminal 的稳定身份。
// TerminalID 不能脱离 EndpointID 作为 runtime map key、CLI target 或 TUI binding truth。
type TerminalRef struct {
	EndpointID endpoint.EndpointID
	TerminalID string
}

// Validate 校验 terminal ref 是否同时绑定 owning endpoint 与 daemon-local terminal ID。
func (ref TerminalRef) Validate() error {
	if strings.TrimSpace(string(ref.EndpointID)) == "" || strings.TrimSpace(ref.TerminalID) == "" {
		return runtimeError(ErrorInvalidRequest, "terminal endpoint_id and terminal_id are required", nil)
	}
	return nil
}

// TerminalDefaultsRequest 请求 owning endpoint daemon 的 terminal 创建默认值。
type TerminalDefaultsRequest struct {
	EndpointID endpoint.EndpointID
}

// TerminalDefaultsResult 只给 daemon-local defaults 增加 owning endpoint 上下文。
type TerminalDefaultsResult struct {
	EndpointID endpoint.EndpointID
	Defaults   coreapi.PathDefaults
}

// TerminalCreateRequest 只给 daemon-local create spec 增加 owning endpoint 上下文。
// Spec 中的 slice/map 在方法返回前归调用方所有；runtime implementation 异步使用前必须复制。
type TerminalCreateRequest struct {
	EndpointID endpoint.EndpointID
	Spec       coreapi.TerminalCreateSpec
}

// TerminalCreateResult 只给 daemon create result 增加 owning endpoint 上下文。
type TerminalCreateResult struct {
	EndpointID endpoint.EndpointID
	Terminal   coreapi.TerminalCreateResult
}

// TerminalListRequest 请求单个 owning endpoint 的 terminal inventory。
type TerminalListRequest struct {
	EndpointID endpoint.EndpointID
}

// TerminalListResult 只给 daemon-local inventory 增加 owning endpoint 上下文；runtime 不混入其它 endpoint 的缓存条目。
type TerminalListResult struct {
	EndpointID endpoint.EndpointID
	Items      []coreapi.TerminalInfo
}

// TerminalMutationRequest 指定 restart/kill/remove 操作的 terminal identity。
type TerminalMutationRequest struct {
	Ref TerminalRef
}

// TerminalMetadataRequest 只给 daemon-local metadata update 增加 owning endpoint 上下文。
type TerminalMetadataRequest struct {
	EndpointID endpoint.EndpointID
	Update     coreapi.TerminalMetadataUpdate
}

// TerminalTagsRequest 只给 daemon-local tags update 增加 owning endpoint 上下文。
type TerminalTagsRequest struct {
	EndpointID endpoint.EndpointID
	Update     coreapi.TerminalTagsUpdate
}

// TerminalLifecycleApplication 是 CLI/TUI 管理 daemon terminal lifecycle 与 metadata 的窄接口。
// 实现必须通过当前 endpoint session stamp 路由，不能按裸 TerminalID 搜索其它 endpoint。
type TerminalLifecycleApplication interface {
	TerminalDefaults(context.Context, TerminalDefaultsRequest) (TerminalDefaultsResult, error)
	CreateTerminal(context.Context, TerminalCreateRequest) (TerminalCreateResult, error)
	ListTerminals(context.Context, TerminalListRequest) (TerminalListResult, error)
	RestartTerminal(context.Context, TerminalMutationRequest) error
	KillTerminal(context.Context, TerminalMutationRequest) error
	RemoveTerminal(context.Context, TerminalMutationRequest) error
	SetTerminalMetadata(context.Context, TerminalMetadataRequest) error
	SetTerminalTags(context.Context, TerminalTagsRequest) error
}
