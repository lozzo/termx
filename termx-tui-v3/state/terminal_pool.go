package state

type TerminalPoolStatus string

const (
	TerminalPoolIdle    TerminalPoolStatus = ""
	TerminalPoolLoading TerminalPoolStatus = "loading"
	TerminalPoolReady   TerminalPoolStatus = "ready"
	TerminalPoolError   TerminalPoolStatus = "error"
)

type TerminalPoolStore struct {
	Status         TerminalPoolStatus
	Items          []TerminalPoolItem
	RequestSeq     uint64
	AppliedSeq     uint64
	LastError      string
	LastCreatedID  string
	LastAttachedID string
	LastKilledID   string
	LastRemovedID  string
	LastEditedID   string
}

type TerminalPoolItem struct {
	TerminalID string
	Title      string
	State      string
	CWD        string
	Tags       map[string]string
	Cols       int
	Rows       int
	Attached   bool
}

func (store TerminalPoolStore) RequestList() TerminalPoolStore {
	store.RequestSeq++
	store.Status = TerminalPoolLoading
	store.LastError = ""
	return store
}

func (store TerminalPoolStore) ApplyList(seq uint64, items []TerminalPoolItem, err string) (TerminalPoolStore, bool) {
	if store.IsStale(seq) {
		return store, false
	}
	store.AppliedSeq = seq
	if err != "" {
		store.Status = TerminalPoolError
		store.LastError = err
		return store, true
	}
	store.Status = TerminalPoolReady
	store.Items = cloneTerminalPoolItems(items)
	store.LastError = ""
	return store, true
}

func (store TerminalPoolStore) ApplyCreated(terminalID string, err string) TerminalPoolStore {
	if err != "" {
		store.LastError = err
		store.Status = TerminalPoolError
		return store
	}
	store.LastCreatedID = terminalID
	store.LastError = ""
	return store
}

func (store TerminalPoolStore) ApplyAttached(terminalID string, err string) TerminalPoolStore {
	if err != "" {
		store.LastError = err
		store.Status = TerminalPoolError
		return store
	}
	store.LastAttachedID = terminalID
	store.LastError = ""
	store.Items = markTerminalPoolAttached(store.Items, terminalID)
	return store
}

func (store TerminalPoolStore) ApplyKilled(terminalID string, err string) TerminalPoolStore {
	if err != "" {
		store.LastError = err
		store.Status = TerminalPoolError
		return store
	}
	store.LastKilledID = terminalID
	store.LastError = ""
	return store
}

func (store TerminalPoolStore) ApplyRemoved(terminalID string, err string) TerminalPoolStore {
	if err != "" {
		store.LastError = err
		store.Status = TerminalPoolError
		return store
	}
	store.LastRemovedID = terminalID
	store.LastError = ""
	store.Items = removeTerminalPoolItem(store.Items, terminalID)
	return store
}

func (store TerminalPoolStore) ApplyEdited(terminalID string, err string) TerminalPoolStore {
	if err != "" {
		store.LastError = err
		store.Status = TerminalPoolError
		return store
	}
	store.LastEditedID = terminalID
	store.LastError = ""
	return store
}

func (store TerminalPoolStore) IsStale(seq uint64) bool {
	return seq != 0 && seq < store.RequestSeq
}

func cloneTerminalPoolItems(items []TerminalPoolItem) []TerminalPoolItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]TerminalPoolItem, len(items))
	for i, item := range items {
		cloned[i] = item
		cloned[i].Tags = cloneStringMap(item.Tags)
	}
	return cloned
}

func markTerminalPoolAttached(items []TerminalPoolItem, terminalID string) []TerminalPoolItem {
	cloned := cloneTerminalPoolItems(items)
	for index := range cloned {
		cloned[index].Attached = cloned[index].TerminalID == terminalID
	}
	return cloned
}

func removeTerminalPoolItem(items []TerminalPoolItem, terminalID string) []TerminalPoolItem {
	if terminalID == "" {
		return cloneTerminalPoolItems(items)
	}
	out := make([]TerminalPoolItem, 0, len(items))
	for _, item := range items {
		if item.TerminalID == terminalID {
			continue
		}
		out = append(out, item)
	}
	return cloneTerminalPoolItems(out)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
