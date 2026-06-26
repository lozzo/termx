package history

// historyIDAllocator 是单个 HistoryLogicalRenderer 内的 id truth source。
// domain owner：history；StreamLineReducer 与 FrameReducer 必须共享它，避免
// scroll-out/open line 和 frame rows 使用相同 logical line id 互相覆盖。
type historyIDAllocator struct {
	nextLineID   LogicalLineID
	nextRecordID HistoryRecordID
	nextSeq      uint64
}

// newHistoryIDAllocator 创建 terminal history renderer 的本地 allocator。它不是
// storage id source；recovery/store 只消费 mutation 中已有 id。
func newHistoryIDAllocator() *historyIDAllocator {
	return &historyIDAllocator{nextLineID: 1, nextRecordID: 1}
}

// nextLogicalLineID 分配新的 logical line id。调用边界：只能在 renderer/reducer
// 创建新 history-owned line payload 时使用，不能被 protocol/TUI 调用。
func (allocator *historyIDAllocator) nextLogicalLineID() LogicalLineID {
	allocator.ensure()
	id := allocator.nextLineID
	allocator.nextLineID++
	return id
}

func (allocator *historyIDAllocator) reserveLogicalLineID(id LogicalLineID) {
	allocator.ensure()
	if allocator.nextLineID <= id {
		allocator.nextLineID = id + 1
	}
}

// nextHistoryRecordID 分配 sealed timeline record id。record id 和 line id 都由同
// 一个 renderer allocator 管理，避免跨 reducer timeline record 冲突。
func (allocator *historyIDAllocator) nextHistoryRecordID() HistoryRecordID {
	allocator.ensure()
	id := allocator.nextRecordID
	allocator.nextRecordID++
	return id
}

// nextTimelineSeq 分配 sealed timeline 的全局顺序号。StreamLineReducer 与
// FrameReducer 必须共享该计数，避免普通 prompt 被排到刚关闭的 screen-frame 前面。
func (allocator *historyIDAllocator) nextTimelineSeq() uint64 {
	allocator.ensure()
	allocator.nextSeq++
	return allocator.nextSeq
}

func (allocator *historyIDAllocator) ensure() {
	if allocator.nextLineID == 0 {
		allocator.nextLineID = 1
	}
	if allocator.nextRecordID == 0 {
		allocator.nextRecordID = 1
	}
}
