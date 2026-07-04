package history

// HistoryReadState 返回 history store 的只读诊断边界。linehist 默认路径不再使用
// classifier；保留该类型只为了 HistoryStore 兼容和测试诊断。
type HistoryReadState struct {
	Generation  Generation
	HasTimeline bool
}

// HistoryMutationBatch 是旧 renderer mutation 入口的兼容壳。linehist 的正文写入
// truth 来自 TerminalSemanticTransaction.EvictedRows，HistoryStore.Apply 不再承载正文
// mutation。
type HistoryMutationBatch struct {
	Seq        uint64
	Generation Generation
}

// HistoryStore 是权威 history query/copy 的外部契约。R436 后默认实现是 linehist：
// 写入通过 ApplyTransaction 消费 vterm eviction，Apply 仅保留为旧接口兼容 no-op。
type HistoryStore interface {
	Apply(batch HistoryMutationBatch) error
	ReadState() HistoryReadState
	LatestWindow(req HistoryWindowRequest) (HistoryWindow, error)
	OlderWindow(req HistoryWindowRequest) (HistoryWindow, error)
	OldestWindow(req HistoryWindowRequest) (HistoryWindow, error)
	NewerWindow(req HistoryWindowRequest) (HistoryWindow, error)
	Freeze(req FreezeHistoryRequest) (FrozenHistorySnapshot, error)
	Copy(req HistoryCopyRequest) (string, error)
	Release(token HistoryToken) error
}
