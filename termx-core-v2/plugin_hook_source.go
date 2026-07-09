package termxcorev2

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-shared/plugin"
)

const defaultTerminalOutputIdleAfter = time.Minute

var errTerminalActivityHookSourceRequired = errors.New("terminal activity hook source is required")

// DaemonHookSourceConfig 描述 daemon/core 系统 hook 源的本地配置。
// DaemonID 是 daemon-local source identity；EndpointID/TerminalRef 属于 client 侧 truth，不能在这里生成。
type DaemonHookSourceConfig struct {
	DaemonID string
	Now      func() time.Time
}

// DaemonHookSource 把 core-v2 拥有的 terminal 事实封装成 plugin.HookEvent。
// 它只负责 after-event envelope 和 daemon-local identity，不执行插件、不路由 mailbox，也不解释 TUI panel/focus state。
type DaemonHookSource struct {
	mu       sync.Mutex
	daemonID string
	now      func() time.Time
	sequence uint64
}

// NewDaemonHookSource 创建 daemon/core hook source adapter。
// 调用方应按 daemon 进程生命周期持有它，以保证 Sequence 在单 source host 内单调递增。
func NewDaemonHookSource(config DaemonHookSourceConfig) *DaemonHookSource {
	daemonID := config.DaemonID
	if daemonID == "" {
		daemonID = ModuleName
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &DaemonHookSource{
		daemonID: daemonID,
		now:      now,
	}
}

// DaemonTerminalSizePayload 是 hook payload 内的 terminal 尺寸快照。
// 它来自 core-v2 terminal lifecycle/resize truth，只表达 cell 尺寸，不携带任何 screen 内容。
type DaemonTerminalSizePayload struct {
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// DaemonTerminalLifecyclePayload 是 terminal lifecycle hook 的元数据 payload。
// payload 只包含 daemon-local terminal identity、名称、状态和退出元数据，不包含 PTY 原始输出。
type DaemonTerminalLifecyclePayload struct {
	TerminalID string                     `json:"terminal_id"`
	Name       string                     `json:"name,omitempty"`
	State      string                     `json:"state,omitempty"`
	Size       *DaemonTerminalSizePayload `json:"size,omitempty"`
	ExitCode   *int                       `json:"exit_code,omitempty"`
	ExitedAt   time.Time                  `json:"exited_at,omitempty"`
	CreatedAt  time.Time                  `json:"created_at,omitempty"`
}

// DaemonTerminalResizePayload 是 terminal resize hook 的元数据 payload。
// resize 是 latest/coalesced 事实，payload 只表达 old/new size 和当前 terminal state。
type DaemonTerminalResizePayload struct {
	TerminalID string                     `json:"terminal_id"`
	Name       string                     `json:"name,omitempty"`
	State      string                     `json:"state,omitempty"`
	OldSize    *DaemonTerminalSizePayload `json:"old_size,omitempty"`
	NewSize    *DaemonTerminalSizePayload `json:"new_size,omitempty"`
}

// TerminalEvent 把 core-v2 Event 转换成系统 hook event。
// 只有 terminal lifecycle/resize after-event 会被封装；live invalidation 等显示刷新不是 PTY activity，也不会被当作 hook 发布。
func (source *DaemonHookSource) TerminalEvent(event Event) (plugin.HookEvent, bool) {
	return source.TerminalEventWithTrace(event, plugin.MessageTrace{})
}

// TerminalEventWithTrace 把 core-v2 Event 转换成系统 hook event，并继承已有因果 trace。
// plugin action 间接造成 terminal 事实变化时必须传入 action trace，避免 hook -> plugin -> action -> hook 丢失 self-caused 防线。
func (source *DaemonHookSource) TerminalEventWithTrace(event Event, cause plugin.MessageTrace) (plugin.HookEvent, bool) {
	info, ok := terminalInfoForHook(event)
	if !ok {
		return plugin.HookEvent{}, false
	}
	eventType, lossy, ok := daemonTerminalHookType(event.Type)
	if !ok {
		return plugin.HookEvent{}, false
	}
	at := event.Timestamp
	if at.IsZero() {
		at = source.nowTime()
	}
	var payload []byte
	switch event.Type {
	case EventTerminalResized:
		payload = mustHookJSON(DaemonTerminalResizePayload{
			TerminalID: info.ID,
			Name:       info.Name,
			State:      string(info.State),
			OldSize:    daemonHookSize(event.OldSize),
			NewSize:    daemonHookSize(firstValidSize(event.NewSize, info.Size)),
		})
	default:
		payload = mustHookJSON(DaemonTerminalLifecyclePayload{
			TerminalID: info.ID,
			Name:       info.Name,
			State:      string(info.State),
			Size:       daemonHookSize(info.Size),
			ExitCode:   cloneExitCode(info.ExitCode),
			ExitedAt:   info.ExitedAt,
			CreatedAt:  info.CreatedAt,
		})
	}
	return source.terminalHookEvent(eventType, plugin.TerminalID(info.ID), plugin.ObjectKindTerminal, info.ID, at, payload, lossy, cause), true
}

func (source *DaemonHookSource) terminalHookEvent(eventType plugin.EventType, terminalID plugin.TerminalID, objectKind string, objectID string, at time.Time, payload []byte, lossy bool, cause plugin.MessageTrace) plugin.HookEvent {
	seq := source.nextSequence()
	at = normalizeHookTime(at)
	eventID := fmt.Sprintf("daemon:%s:%d", source.daemonID, seq)
	return plugin.HookEvent{
		EventID:          eventID,
		Type:             eventType,
		SourceHost:       plugin.HostDaemon,
		DaemonID:         source.daemonID,
		DaemonTerminalID: terminalID,
		ObjectKind:       objectKind,
		ObjectID:         objectID,
		Sequence:         seq,
		Time:             at,
		Trace:            hookTrace(eventID, cause),
		Payload:          payload,
		Lossy:            lossy,
	}
}

func (source *DaemonHookSource) nextSequence() uint64 {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.sequence++
	return source.sequence
}

func (source *DaemonHookSource) nowTime() time.Time {
	return source.now().UTC()
}

func daemonTerminalHookType(eventType EventType) (plugin.EventType, bool, bool) {
	switch eventType {
	case EventTerminalCreated:
		return plugin.SystemEventDaemonTerminalCreated, false, true
	case EventTerminalExited:
		return plugin.SystemEventDaemonTerminalExited, false, true
	case EventTerminalRemoved:
		return plugin.SystemEventDaemonTerminalRemoved, false, true
	case EventTerminalResized:
		return plugin.SystemEventDaemonTerminalResized, true, true
	default:
		return "", false, false
	}
}

func terminalInfoForHook(event Event) (TerminalInfo, bool) {
	if event.Terminal != nil {
		info := event.Terminal.Clone()
		if info.ID == "" {
			info.ID = event.TerminalID
		}
		return info, info.ID != ""
	}
	if event.TerminalID == "" {
		return TerminalInfo{}, false
	}
	return TerminalInfo{ID: event.TerminalID}, true
}

func daemonHookSize(size Size) *DaemonTerminalSizePayload {
	if !size.Valid() {
		return nil
	}
	return &DaemonTerminalSizePayload{Cols: size.Cols, Rows: size.Rows}
}

func firstValidSize(primary Size, fallback Size) Size {
	if primary.Valid() {
		return primary
	}
	return fallback
}

func cloneExitCode(value *int) *int {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

// TerminalActivityTrackerConfig 描述单个 daemon-local terminal 的 PTY activity hook 追踪配置。
// Source 提供 daemon host envelope；TerminalID 是 daemon-local identity，不能替代 client 侧 TerminalRef。
type TerminalActivityTrackerConfig struct {
	Source     *DaemonHookSource
	TerminalID string
	IdleAfter  time.Duration
}

// TerminalActivityTracker 从真实 PTY ingest 元数据生成 activity/idle/resumed hook。
// 它只记录字节计数、时间和序号，不保存也不发布 PTY 原始内容；live invalidation 不能作为输入。
type TerminalActivityTracker struct {
	mu             sync.Mutex
	source         *DaemonHookSource
	terminalID     plugin.TerminalID
	idleAfter      time.Duration
	lastOutputAt   time.Time
	lastTrace      plugin.MessageTrace
	totalBytes     uint64
	outputSequence uint64
	idleFired      bool
}

// TerminalOutputActivityPayload 是 PTY 输出 activity hook 的元数据 payload。
// Bytes 只表示本次 PTY ingest 的字节数，不能携带或暗示原始输出内容。
type TerminalOutputActivityPayload struct {
	Bytes          int       `json:"bytes"`
	LastOutputAt   time.Time `json:"last_output_at"`
	TotalBytes     uint64    `json:"total_bytes"`
	OutputSequence uint64    `json:"output_sequence"`
}

// TerminalOutputIdlePayload 是 PTY 输出 idle hook 的元数据 payload。
// 它只表示一段时间内没有新的 PTY bytes，不能被解释成进程完成或特定程序语义。
type TerminalOutputIdlePayload struct {
	IdleFor        time.Duration `json:"idle_for"`
	LastOutputAt   time.Time     `json:"last_output_at"`
	TotalBytes     uint64        `json:"total_bytes"`
	OutputSequence uint64        `json:"output_sequence"`
	TerminalState  string        `json:"terminal_state"`
}

// TerminalOutputResumedPayload 是 PTY 输出从 idle 状态恢复时的元数据 payload。
// 它和 activity hook 一样只暴露计数和时间，不暴露本次输出的字符内容。
type TerminalOutputResumedPayload struct {
	Bytes          int           `json:"bytes"`
	IdleFor        time.Duration `json:"idle_for"`
	LastOutputAt   time.Time     `json:"last_output_at"`
	TotalBytes     uint64        `json:"total_bytes"`
	OutputSequence uint64        `json:"output_sequence"`
}

// NewTerminalActivityTracker 创建单 terminal 的 PTY activity tracker。
// Source 必须由 daemon 级 hook source 注入，避免每个 terminal 自建 source 后生成重复 EventID/Sequence。
func NewTerminalActivityTracker(config TerminalActivityTrackerConfig) (*TerminalActivityTracker, error) {
	source := config.Source
	if source == nil {
		return nil, errTerminalActivityHookSourceRequired
	}
	idleAfter := config.IdleAfter
	if idleAfter <= 0 {
		idleAfter = defaultTerminalOutputIdleAfter
	}
	return &TerminalActivityTracker{
		source:     source,
		terminalID: plugin.TerminalID(config.TerminalID),
		idleAfter:  idleAfter,
	}, nil
}

// RecordPTYOutput 记录一次真实 PTY bytes ingest，并返回应发布的 hook events。
// bytes 小于等于 0 时不产生事件；调用方必须从 PTY read/ingest 路径调用，不能从 live invalidation 调用。
func (tracker *TerminalActivityTracker) RecordPTYOutput(at time.Time, bytes int) []plugin.HookEvent {
	return tracker.RecordPTYOutputWithTrace(at, bytes, plugin.MessageTrace{})
}

// RecordPTYOutputWithTrace 记录 PTY bytes ingest 并继承已有因果 trace。
// 当输出由插件 action 间接触发时，调用方应传入 action trace；idle hook 会记住最近一次输出 trace 以维持循环防护。
func (tracker *TerminalActivityTracker) RecordPTYOutputWithTrace(at time.Time, bytes int, cause plugin.MessageTrace) []plugin.HookEvent {
	if bytes <= 0 || tracker.terminalID == "" {
		return nil
	}
	if at.IsZero() {
		at = tracker.source.nowTime()
	}
	at = normalizeHookTime(at)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	previousLastOutput := tracker.lastOutputAt
	wasIdle := tracker.idleFired
	tracker.outputSequence++
	tracker.totalBytes += uint64(bytes)
	tracker.lastOutputAt = at
	if cause.TraceID != "" {
		tracker.lastTrace = cause.Clone()
	}
	tracker.idleFired = false

	var events []plugin.HookEvent
	if wasIdle {
		events = append(events, tracker.outputResumedEventLocked(at, bytes, idleDuration(previousLastOutput, at), cause))
	}
	events = append(events, tracker.outputActivityEventLocked(at, bytes, cause))
	return events
}

// Tick 检查 terminal 是否已达到 idle threshold，并返回至多一个 output_idle hook。
// terminalState 来自 core terminal lifecycle 投影；空值会被标记为 unknown，不能从程序名或 live 刷新推断。
func (tracker *TerminalActivityTracker) Tick(at time.Time, terminalState string) []plugin.HookEvent {
	return tracker.TickWithTrace(at, terminalState, plugin.MessageTrace{})
}

// TickWithTrace 检查 idle threshold，并允许调用方显式继承触发本次检查的因果 trace。
// 若 cause 为空，idle hook 会继承最近一次 PTY 输出 trace；这保证插件造成输出后产生的 idle 仍受 self-caused 过滤。
func (tracker *TerminalActivityTracker) TickWithTrace(at time.Time, terminalState string, cause plugin.MessageTrace) []plugin.HookEvent {
	if tracker.terminalID == "" {
		return nil
	}
	if at.IsZero() {
		at = tracker.source.nowTime()
	}
	at = normalizeHookTime(at)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.lastOutputAt.IsZero() || tracker.idleFired {
		return nil
	}
	idleFor := at.Sub(tracker.lastOutputAt)
	if idleFor < tracker.idleAfter {
		return nil
	}
	if terminalState == "" {
		terminalState = "unknown"
	}
	tracker.idleFired = true
	if cause.TraceID == "" {
		cause = tracker.lastTrace.Clone()
	}
	return []plugin.HookEvent{tracker.outputIdleEventLocked(at, idleFor, terminalState, cause)}
}

func (tracker *TerminalActivityTracker) outputActivityEventLocked(at time.Time, bytes int, cause plugin.MessageTrace) plugin.HookEvent {
	return tracker.source.terminalHookEvent(
		plugin.SystemEventDaemonTerminalOutputActivity,
		tracker.terminalID,
		plugin.ObjectKindTerminal,
		string(tracker.terminalID),
		at,
		mustHookJSON(TerminalOutputActivityPayload{
			Bytes:          bytes,
			LastOutputAt:   at,
			TotalBytes:     tracker.totalBytes,
			OutputSequence: tracker.outputSequence,
		}),
		true,
		cause,
	)
}

func (tracker *TerminalActivityTracker) outputIdleEventLocked(at time.Time, idleFor time.Duration, terminalState string, cause plugin.MessageTrace) plugin.HookEvent {
	return tracker.source.terminalHookEvent(
		plugin.SystemEventDaemonTerminalOutputIdle,
		tracker.terminalID,
		plugin.ObjectKindTerminal,
		string(tracker.terminalID),
		at,
		mustHookJSON(TerminalOutputIdlePayload{
			IdleFor:        idleFor,
			LastOutputAt:   tracker.lastOutputAt,
			TotalBytes:     tracker.totalBytes,
			OutputSequence: tracker.outputSequence,
			TerminalState:  terminalState,
		}),
		true,
		cause,
	)
}

func (tracker *TerminalActivityTracker) outputResumedEventLocked(at time.Time, bytes int, idleFor time.Duration, cause plugin.MessageTrace) plugin.HookEvent {
	return tracker.source.terminalHookEvent(
		plugin.SystemEventDaemonTerminalOutputResumed,
		tracker.terminalID,
		plugin.ObjectKindTerminal,
		string(tracker.terminalID),
		at,
		mustHookJSON(TerminalOutputResumedPayload{
			Bytes:          bytes,
			IdleFor:        idleFor,
			LastOutputAt:   at,
			TotalBytes:     tracker.totalBytes,
			OutputSequence: tracker.outputSequence,
		}),
		true,
		cause,
	)
}

func idleDuration(lastOutputAt time.Time, at time.Time) time.Duration {
	if lastOutputAt.IsZero() || at.Before(lastOutputAt) {
		return 0
	}
	return at.Sub(lastOutputAt)
}

func normalizeHookTime(at time.Time) time.Time {
	return at.UTC()
}

func hookTrace(eventID string, cause plugin.MessageTrace) plugin.MessageTrace {
	if cause.TraceID == "" {
		return plugin.MessageTrace{TraceID: eventID}
	}
	return cause.Clone()
}

func mustHookJSON(value any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return payload
}
