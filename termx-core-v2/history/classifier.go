package history

// ScreenSessionState 是 classifier 的只读输入；它不是 store，也不保存 history truth。
type ScreenSessionState struct {
	Mode                 ScreenOutputMode
	ActiveSessionID      ScreenSessionID
	ActivePrimaryFrameID ScreenFrameID
	ActiveAltFrameID     ScreenFrameID
	Generation           Generation
	InAltScreen          bool
	HasPrimaryCurrent    bool
	HasAltCurrent        bool
	SynchronizedOutput   bool
}

// ScreenAppDecision 只表达语义决策，不能携带 renderer rows 或 live snapshot。
type ScreenAppDecision struct {
	Mode                         ScreenOutputMode
	PublishFrame                 bool
	ClosePrimarySession          bool
	ArchivePrimaryBeforeAlt      bool
	ClearPrimaryCurrentForAlt    bool
	EnterAltTransientFrame       bool
	ExitAltTransientFrame        bool
	ForceCommitPrimaryFinalFrame bool
	ForceCommitFrontier          bool
	NonHistoryBoundary           bool
}

// ScreenAppClassifier 只能根据 terminal semantic transaction 和 session state 判断。
// failure condition：不得按 Codex/Claude/htop/vim 等进程名分支。
type ScreenAppClassifier interface {
	// Classify returns the screen/history mode decision for one transaction. The
	// implementation may inspect terminal ops and current session state only.
	Classify(tx TerminalSemanticTransaction, state ScreenSessionState) ScreenAppDecision
}
