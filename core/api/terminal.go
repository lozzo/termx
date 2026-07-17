package api

import "time"

// TerminalSize 是 owning daemon 确认的 terminal cell 尺寸。
type TerminalSize struct {
	Cols uint16
	Rows uint16
}

// TerminalResourceUsage 是 owning daemon 对 terminal process 的采样投影。
// SampledAt 为零表示本次没有可用采样，consumer 不得缓存或推断 process state。
type TerminalResourceUsage struct {
	PID            int
	CPUPercentX100 int
	MemoryBytes    uint64
	SampledAt      time.Time
}

// TerminalInfo 是 daemon-local terminal lifecycle 与 metadata 的只读 application projection。
// 它不包含 EndpointID、client session generation、pane/view 或 transport 状态。
type TerminalInfo struct {
	TerminalID      string
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

// TerminalCreateSpec 是 daemon-local terminal 创建规格。
// Command、Env 和 Tags 的 ownership 在 application method 返回前属于调用方，异步实现必须复制。
type TerminalCreateSpec struct {
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

// TerminalCreateResult 返回 daemon 创建的 terminal identity 和初始 lifecycle state。
type TerminalCreateResult struct {
	TerminalID string
	State      string
}

// TerminalMetadataUpdate 请求 daemon 原子更新 terminal name 和 tags。
type TerminalMetadataUpdate struct {
	TerminalID string
	Name       string
	Tags       map[string]string
}

// TerminalTagsUpdate 请求 daemon 原子替换 terminal tags。
type TerminalTagsUpdate struct {
	TerminalID string
	Tags       map[string]string
}
