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
	HasPrimaryCurrent    bool
	HasAltCurrent        bool
	SynchronizedOutput   bool
}

// HistoryDecision 只表达 semantic transaction 该走哪条 history renderer 路径。
// 它不能携带 renderer rows、live snapshot、进程名或协议/TUI 状态。
type HistoryDecision struct {
	Mode                    HistoryOutputMode
	PublishPrimaryFrame     bool
	ArchivePrimaryBeforeAlt bool
	ClearPrimaryCurrent     bool
	PublishAltFrame         bool
	ClearAltFrame           bool
	ClosePrimaryFrame       bool
	SealOpenLine            bool
	NonHistoryBoundary      bool
}

// HistorySemanticClassifier 只能根据 terminal semantic transaction 和
// HistoryReadState 判断 renderer 路径。失败条件：不得按 Codex、Claude Code、vim、
// htop 等进程名分支。
type HistorySemanticClassifier interface {
	// Classify 返回一个 transaction 的 history renderer 决策。实现只能检查
	// terminal semantics 和 history read state。
	Classify(tx TerminalSemanticTransaction, state HistoryReadState) HistoryDecision
}
