package history

// HistoryReadState 是 classifier 的只读输入。它从 HistoryState 派生，不允许
// 暴露 store 写接口，也不允许读取 live renderer snapshot。
type HistoryReadState struct {
	Mode                 HistoryOutputMode
	ActiveSessionID      ScreenSessionID
	ActivePrimaryFrameID ScreenFrameID
	ActiveAltFrameID     ScreenFrameID
	Generation           Generation
	InAltScreen          bool
	HasOpenLine          bool
	// HasTimeline 表示 history store 已经存在 sealed timeline 记录。它只用于
	// classifier 判断 full-replace side proof 是否会重复接管旧 primary 屏幕；
	// 不暴露具体内容，避免 classifier 变成第二份 history truth。
	HasTimeline        bool
	HasPrimaryCurrent  bool
	HasAltCurrent      bool
	SynchronizedOutput bool
}

// HistoryDecision 只表达 semantic transaction 该走哪条 history renderer 路径。
// 它不能携带 renderer rows、live snapshot、进程名或协议/TUI 状态。
type HistoryDecision struct {
	Mode                HistoryOutputMode
	PublishPrimaryFrame bool
	// PublishPrimaryFrameTouchedRowsOnly 表示本次 primary frame 只能接管当前
	// transaction 明确触达的 rows。它用于 synchronized 输出或 full-replace
	// direct damage 刚启动但尚未 clear 全屏的场景，防止已 sealed 的普通 shell
	// tail 被整屏 side proof 再发布为 current frame；真值来源只能是 ordered
	// semantic ops 或 vterm direct damage 的 row/rect proof。
	PublishPrimaryFrameTouchedRowsOnly bool
	ArchivePrimaryBeforeAlt            bool
	ClearPrimaryCurrent                bool
	PublishAltFrame                    bool
	ClearAltFrame                      bool
	ClosePrimaryFrame                  bool
	// ClosePrimaryFrameBeforePrimaryReplace 表示 transaction 本身携带明确的
	// lifecycle/session boundary，旧 current frame 必须先 seal，再接收本次
	// primary frame。当前 terminal classifier 不在 synchronized repaint 路径设置
	// 该标记；普通输出恢复、alt archive 和 process close 由独立边界处理，避免
	// screen app 的每次原地刷新被错误写进 sealed timeline。
	ClosePrimaryFrameBeforePrimaryReplace bool
	// ArchivePrimaryAfterPrimaryFrame 表示同一 transaction 同时携带 primary frame
	// side proof 和 alt-enter 边界；renderer 必须先发布 primary frame，再按
	// alt-enter 把它归档，不能等后续普通输出把它 close 成普通 committed frame。
	ArchivePrimaryAfterPrimaryFrame bool
	// ClosePrimaryFrameBeforeStream 表示普通输出恢复前必须先关闭 primary
	// current frame，防止后续 prompt/open line 被投影到旧 screen-frame 前面。
	ClosePrimaryFrameBeforeStream bool
	SealOpenLine                  bool
	ConsumeStreamOps              bool
	ConsumeScrollOutProof         bool
	// ConsumeClearTimeScrollOutProof 表示 ED2 clear-time scroll-out proof 来自
	// 当前 primary frame ownership 离开 viewport，必须进入 sealed timeline；
	// 普通 shell 已 sealed 的可见行不能打开这个开关，否则会重复历史。
	ConsumeClearTimeScrollOutProof bool
	ConsumeClearBoundary           bool
	NonHistoryBoundary             bool
}

// HistorySemanticClassifier 只能根据 terminal semantic transaction 和
// HistoryReadState 判断 renderer 路径。失败条件：不得按 Codex、Claude Code、vim、
// htop 等进程名分支。
type HistorySemanticClassifier interface {
	// Classify 返回一个 transaction 的 history renderer 决策。实现只能检查
	// terminal semantics 和 history read state。
	Classify(tx TerminalSemanticTransaction, state HistoryReadState) HistoryDecision
}
