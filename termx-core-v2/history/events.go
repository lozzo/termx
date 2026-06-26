package history

import "time"

// HistorySemanticEventKind 枚举 R319 后 history renderer 内部使用的有序语义事件。
// 这些事件来自同一份 TerminalSemanticTransaction，不允许从 raw PTY 或 live
// snapshot 重新合成。
type HistorySemanticEventKind string

const (
	HistorySemanticEventOp               HistorySemanticEventKind = "op"
	HistorySemanticEventPrimaryScrollOut HistorySemanticEventKind = "primary-scroll-out"
	HistorySemanticEventPrimaryFrame     HistorySemanticEventKind = "primary-frame"
	HistorySemanticEventAltFrame         HistorySemanticEventKind = "alt-frame"
	HistorySemanticEventAltEnter         HistorySemanticEventKind = "alt-enter"
	HistorySemanticEventAltExit          HistorySemanticEventKind = "alt-exit"
	HistorySemanticEventResize           HistorySemanticEventKind = "resize"
	HistorySemanticEventFullReplace      HistorySemanticEventKind = "full-replace"
	HistorySemanticEventClose            HistorySemanticEventKind = "close"
)

// HistorySemanticEvent 是 renderer 内部的 ordered event 形状。它把 ops、scroll-out
// proof 和 frame payload 放到同一条顺序链上，便于 harness 检查是否漏处理某类
// terminal semantic。
type HistorySemanticEvent struct {
	Seq       uint64
	Order     int
	Kind      HistorySemanticEventKind
	Time      time.Time
	Op        *TerminalSemanticOp
	ScrollOut *TerminalSemanticScrollOut
	Frame     *TerminalSemanticFrame
	Size      TerminalSemanticSize
	Reason    string
}
