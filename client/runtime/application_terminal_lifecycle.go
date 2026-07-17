package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/lozzow/termx/client/endpoint"
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

// TerminalSize 是 daemon 确认的 terminal cell 尺寸。
type TerminalSize struct {
	Cols uint16
	Rows uint16
}

// TerminalResourceUsage 是 owning daemon 对 terminal process 的采样投影。
// SampledAt 为零表示本次没有可用采样，client runtime 不缓存或推断 process 状态。
type TerminalResourceUsage struct {
	PID            int
	CPUPercentX100 int
	MemoryBytes    uint64
	SampledAt      time.Time
}

// TerminalInfo 是 owning daemon terminal lifecycle/metadata 的只读 application projection。
type TerminalInfo struct {
	Ref             TerminalRef
	Name            string
	Command         []string
	Tags            map[string]string
	Size            TerminalSize
	State           string
	CWD             string
	LiveCWD         string
	CreatedAt       time.Time
	ExitCode        *int
	ExitedAt        time.Time
	AttachmentCount int
	Resources       TerminalResourceUsage
}

// TerminalDefaultsRequest 请求 owning endpoint daemon 的 terminal 创建默认值。
type TerminalDefaultsRequest struct {
	EndpointID endpoint.EndpointID
}

// TerminalDefaultsResult 返回 daemon 进程所在机器的默认命令和 cwd。
type TerminalDefaultsResult struct {
	DefaultCommand []string
	DefaultCWD     string
}

// TerminalCreateRequest 描述在 owning endpoint 创建 terminal 的 application intent。
// Command/Env/Tags 在方法返回前归调用方所有；runtime implementation 必须在异步使用前复制。
type TerminalCreateRequest struct {
	EndpointID         endpoint.EndpointID
	TerminalID         string
	Name               string
	Command            []string
	Tags               map[string]string
	Size               TerminalSize
	CWD                string
	Env                []string
	ScrollbackRows     int
	ScrollbackMaxBytes int64
	ScrollbackMaxAge   time.Duration
}

// TerminalCreateResult 返回 owning daemon 创建的 terminal identity 和初始 lifecycle state。
type TerminalCreateResult struct {
	Ref   TerminalRef
	State string
}

// TerminalListRequest 请求单个 owning endpoint 的 terminal inventory。
type TerminalListRequest struct {
	EndpointID endpoint.EndpointID
}

// TerminalListResult 返回单 endpoint terminal inventory；runtime 不混入其它 endpoint 的缓存条目。
type TerminalListResult struct {
	Items []TerminalInfo
}

// TerminalMutationRequest 指定 restart/kill/remove 操作的 terminal identity。
type TerminalMutationRequest struct {
	Ref TerminalRef
}

// TerminalMetadataRequest 请求 owning daemon 原子更新 terminal name 和 tags。
type TerminalMetadataRequest struct {
	Ref  TerminalRef
	Name string
	Tags map[string]string
}

// TerminalTagsRequest 请求 owning daemon 原子替换 terminal tags。
type TerminalTagsRequest struct {
	Ref  TerminalRef
	Tags map[string]string
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
