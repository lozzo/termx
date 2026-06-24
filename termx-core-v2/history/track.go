package history

// HistoryTrack 持有一个 primary terminal history stream 的 authoritative
// logical-line 状态。
type HistoryTrack struct {
	store     LogicalLineStore
	committed *CommittedHistoryIndex
	frontier  *MutableFrontier

	activeLine LogicalLineID
	activeCol  int
	overwrite  bool
	altScreen  bool
	// 中文说明：有些前台 TUI 不进入 alt-screen，而是在 primary screen 上隐藏光标、
	// 开启鼠标追踪后反复 home-clear repaint。intent 只表示已看到这类控制序列；
	// 第一次 home-clear 仍是 page-break，之后才进入可替换 fullscreen frame。
	primaryFullscreenIntent bool
	primaryFullscreenFrame  bool
	primaryFullscreenModes  map[int]struct{}
	generation              Generation
	screenRows              int
	screenRow               int
	screen                  primaryScreenLineMap
}

func NewHistoryTrack() *HistoryTrack {
	return NewHistoryTrackWith(NewMemoryLogicalLineStore(nil), NewCommittedHistoryIndex(), NewMutableFrontier())
}

func NewHistoryTrackWith(
	store LogicalLineStore,
	committed *CommittedHistoryIndex,
	frontier *MutableFrontier,
) *HistoryTrack {
	if store == nil {
		store = NewMemoryLogicalLineStore(nil)
	}
	if committed == nil {
		committed = NewCommittedHistoryIndex()
	}
	if frontier == nil {
		frontier = NewMutableFrontier()
	}
	return &HistoryTrack{
		store:     store,
		committed: committed,
		frontier:  frontier,
	}
}

// SetPrimaryScreenRows 只更新当前 primary screen 的可见行数，用于决定
// sealed line 何时真正脱离 screen ownership 变成 committable。
func (track *HistoryTrack) SetPrimaryScreenRows(rows int) {
	if rows < 0 {
		rows = 0
	}
	track.screenRows = rows
	track.screen.resize(rows)
	if rows == 0 {
		track.screenRow = 0
		return
	}
	if track.screenRow >= rows {
		track.screenRow = rows - 1
	}
}

func (track *HistoryTrack) Apply(event HistoryEvent) error {
	event.ownedCells = false
	return track.apply(event)
}

func (track *HistoryTrack) ApplyOwned(event HistoryEvent) error {
	event.ownedCells = true
	return track.apply(event)
}

func (track *HistoryTrack) apply(event HistoryEvent) error {
	switch event.Kind {
	case EventWritePrimaryCells:
		return track.writePrimaryCells(event.Cells, event.ownedCells)
	case EventCarriageReturn:
		return track.carriageReturn()
	case EventCursorForward:
		return track.cursorForward(event.Count)
	case EventCursorBackward:
		return track.cursorBackward(event.Count)
	case EventCursorHorizontalAbsolute:
		return track.cursorHorizontalAbsolute(event.Count)
	case EventCursorUp:
		return track.cursorUp(event.Count)
	case EventCursorDown:
		return track.cursorDown(event.Count)
	case EventCursorPosition:
		return track.cursorPosition(event.Row, event.Column)
	case EventEraseCharacters:
		return track.eraseCharacters(event.Count, eraseBlankStyle(event.Style))
	case EventEraseInLine:
		return track.eraseInLine(event.EraseMode, event.EraseCols, eraseBlankStyle(event.Style))
	case EventEraseInDisplay:
		return track.eraseInDisplay(event.EraseMode)
	case EventSetActiveLineTailFill:
		return track.setActiveLineTailFill(eraseBlankStyle(event.Style))
	case EventEnterPrimaryFullscreen:
		return track.enterPrimaryFullscreen(event.PrimaryMode)
	case EventExitPrimaryFullscreen:
		return track.exitPrimaryFullscreen(event.PrimaryMode)
	case EventPrimaryScrollOut:
		return track.primaryScrollOut(event.Count)
	case EventAppendAltScreenFrame:
		return track.appendAltScreenFrame(event.Rows)
	case EventSealLogicalLine:
		return track.sealActiveLine()
	case EventSoftWrapLine:
		return track.softWrapActiveLine()
	case EventMutateFrontier:
		return track.mutateFrontierLine(event)
	case EventResetFrontier:
		return track.resetFrontier()
	case EventCommitFrontier:
		return track.commitFrontier(false)
	case EventForceCommitFrontier:
		return track.commitFrontier(true)
	case EventReclaimCommittedSuffix:
		return track.reclaimCommittedSuffix(event)
	case EventHideFrontier:
		return track.hideFrontier(event)
	case EventTruncateCommittedHistory:
		return track.truncateCommittedHistory(event)
	case EventSwitchAltScreen:
		return track.switchAltScreen(event.EnterAltScreen)
	case EventNonHistoryBoundary:
		track.bumpGeneration()
		return nil
	case EventResize:
		return track.resize(event)
	default:
		return ErrInvalidEventKind
	}
}

func (track *HistoryTrack) primaryScrollOut(count int) error {
	if track.altScreen {
		return nil
	}
	if count <= 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		// 中文说明：vterm scrollback row 不能成为 history truth；这里只用
		// primary screen ownership 判断哪条 logical line 离开屏幕并可提交。
		owner, ok := track.screen.owner(0)
		track.screen.scrollUp()
		if ok && owner.LineID != 0 {
			if err := track.ensureScrolledOutLineSealed(owner.LineID); err != nil {
				return err
			}
		}
		if track.screenRow > 0 {
			track.screenRow--
		}
	}
	return track.commitFrontier(false)
}

func (track *HistoryTrack) ensureScrolledOutLineSealed(id LogicalLineID) error {
	if id == 0 || !track.frontier.Contains(id) {
		return nil
	}
	seal, _, ok := track.lineCommitState(id)
	if !ok {
		return ErrUnknownLine
	}
	if seal == SealStateSealed {
		return nil
	}
	return track.sealLineDirty(id)
}

func (track *HistoryTrack) Line(id LogicalLineID) (LogicalLine, bool) {
	return track.store.Line(id)
}

func (track *HistoryTrack) LineIDs() []LogicalLineID {
	return track.store.LineIDs()
}

func (track *HistoryTrack) RetainedLineCount() int {
	if store, ok := track.store.(interface{ RetainedLineCount() int }); ok {
		return store.RetainedLineCount()
	}
	return 0
}

func (track *HistoryTrack) CommittedIDs() []LogicalLineID {
	return track.committed.IDs()
}

func (track *HistoryTrack) FrontierIDs() []LogicalLineID {
	return track.frontier.IDs()
}

func (track *HistoryTrack) HiddenFrontierIDs() []LogicalLineID {
	return track.frontier.HiddenIDs()
}

func (track *HistoryTrack) CommittableIDs() []LogicalLineID {
	ids := track.frontier.IDs()
	if len(ids) == 0 {
		return nil
	}
	committable := make([]LogicalLineID, 0, len(ids))
	for _, id := range ids {
		if track.lineCommittable(id) {
			committable = append(committable, id)
		}
	}
	return committable
}

func (track *HistoryTrack) Generation() Generation {
	return track.generation
}

func (track *HistoryTrack) InAltScreen() bool {
	return track.altScreen
}

func (track *HistoryTrack) ActiveLineID() LogicalLineID {
	return track.activeLine
}

func (track *HistoryTrack) writePrimaryCells(cells []Cell, ownedCells bool) error {
	if track.altScreen || len(cells) == 0 {
		return nil
	}
	nextGeneration := track.nextGeneration()
	line, created, err := track.ensureWritableActiveLineForCells(cells, ownedCells, nextGeneration)
	if err != nil {
		return err
	}
	incomingWidth := logicalLineWidth(cells)
	if created {
		track.activeCol = incomingWidth
		track.activeLine = line.ID
		track.overwrite = false
		track.setGeneration(nextGeneration)
		return nil
	}
	activeCol := track.activeCol
	overwrite := track.overwrite
	line, lineWidth, err := track.writePrimaryCellsOwned(line.ID, primaryCellsWriteRequest{
		Cells:             cells,
		OwnedCells:        ownedCells,
		ActiveCol:         activeCol,
		Overwrite:         overwrite,
		ContentGeneration: nextGeneration,
	})
	if err != nil {
		return err
	}
	track.activeLine = line.ID
	track.activeCol += incomingWidth
	track.overwrite = track.activeCol < lineWidth
	track.setGeneration(nextGeneration)
	return nil
}

func (track *HistoryTrack) writePrimaryCellsOwned(id LogicalLineID, req primaryCellsWriteRequest) (LogicalLine, int, error) {
	type primaryWriter interface {
		writePrimaryCellsOwned(LogicalLineID, primaryCellsWriteRequest) (LogicalLine, int, error)
	}
	if store, ok := track.store.(primaryWriter); ok {
		return store.writePrimaryCellsOwned(id, req)
	}
	line, ok := track.store.Line(id)
	if !ok {
		return LogicalLine{}, 0, ErrUnknownLine
	}
	lineWidth := logicalLineWidth(line.Cells)
	if !req.Overwrite && req.ActiveCol == lineWidth {
		if req.OwnedCells {
			line.Cells = append(line.Cells, req.Cells...)
		} else {
			line.Cells = append(line.Cells, cloneCells(req.Cells)...)
		}
		line.Cells = mergeAppendableCellRuns(line.Cells)
		lineWidth = logicalLineWidth(line.Cells)
	} else {
		line.Cells = overwriteLineCellsAtColumn(line.Cells, req.ActiveCol, req.Cells)
		lineWidth = logicalLineWidth(line.Cells)
	}
	// 中文说明：TailFill 只描述“当前内容末尾到行尾”的背景；后续真实写入会改变末尾位置，
	// 必须丢弃旧 metadata，避免 resize/copy 时把背景延伸到错误位置。
	line.TailFill = nil
	line.Dirty = true
	if line.CreatedGeneration == 0 {
		line.CreatedGeneration = req.ContentGeneration
	}
	line.ContentGeneration = req.ContentGeneration
	line, err := track.replaceOwnedLine(line)
	return line, lineWidth, err
}

func (track *HistoryTrack) replaceOwnedLine(line LogicalLine) (LogicalLine, error) {
	type ownedReplacer interface {
		replaceOwnedLine(LogicalLine) (LogicalLine, error)
	}
	if store, ok := track.store.(ownedReplacer); ok {
		return store.replaceOwnedLine(line)
	}
	return track.store.ReplaceLine(line)
}

func (track *HistoryTrack) mutateOwnedLine(id LogicalLineID, mutate func(*LogicalLine)) (LogicalLine, error) {
	type ownedMutator interface {
		mutateOwnedLine(LogicalLineID, func(*LogicalLine)) (LogicalLine, error)
	}
	if store, ok := track.store.(ownedMutator); ok {
		return store.mutateOwnedLine(id, mutate)
	}
	line, ok := track.store.Line(id)
	if !ok {
		return LogicalLine{}, ErrUnknownLine
	}
	mutate(&line)
	return track.replaceOwnedLine(line)
}

func (track *HistoryTrack) lineExists(id LogicalLineID) bool {
	type lineChecker interface {
		HasLine(LogicalLineID) bool
	}
	if store, ok := track.store.(lineChecker); ok {
		return store.HasLine(id)
	}
	_, ok := track.store.Line(id)
	return ok
}

func (track *HistoryTrack) inspectLine(id LogicalLineID, inspect func(LogicalLine)) bool {
	line, ok := track.store.Line(id)
	if !ok {
		return false
	}
	inspect(line)
	return true
}

func (track *HistoryTrack) lineCommitState(id LogicalLineID) (SealState, bool, bool) {
	type commitStateReader interface {
		lineCommitState(LogicalLineID) (SealState, bool, bool)
	}
	if store, ok := track.store.(commitStateReader); ok {
		return store.lineCommitState(id)
	}
	line, ok := track.store.Line(id)
	if !ok {
		return "", false, false
	}
	return line.Seal, line.Dirty, true
}

func (track *HistoryTrack) sealLineDirty(id LogicalLineID) error {
	type dirtySealer interface {
		sealLineDirty(LogicalLineID) error
	}
	if store, ok := track.store.(dirtySealer); ok {
		return store.sealLineDirty(id)
	}
	line, ok := track.store.Line(id)
	if !ok {
		return ErrUnknownLine
	}
	line.Seal = SealStateSealed
	line.Dirty = true
	_, err := track.replaceOwnedLine(line)
	return err
}

func (track *HistoryTrack) markLineClean(id LogicalLineID) error {
	type cleaner interface {
		markLineClean(LogicalLineID) error
	}
	if store, ok := track.store.(cleaner); ok {
		return store.markLineClean(id)
	}
	line, ok := track.store.Line(id)
	if !ok {
		return ErrUnknownLine
	}
	line.Dirty = false
	_, err := track.replaceOwnedLine(line)
	return err
}

func (track *HistoryTrack) ensureWritableActiveLineForCells(cells []Cell, ownedCells bool, generation Generation) (LogicalLine, bool, error) {
	if track.activeLine != 0 && (track.frontier.Contains(track.activeLine) || track.committed.Contains(track.activeLine)) {
		if track.lineExists(track.activeLine) {
			return LogicalLine{ID: track.activeLine}, false, nil
		}
	}
	// 中文说明：只有 pipeline 内部明确交出所有权的首批 cells 才走 owned-create；
	// 普通 Apply 仍由 store 返回 detached line，避免外部 mutation 污染 history truth。
	line, err := track.createLine(CreateLineRequest{
		Seal:              SealStateOpen,
		CreatedGeneration: generation,
		ContentGeneration: generation,
		Cells:             cells,
		ownedCells:        ownedCells,
		Dirty:             true,
		Residency:         ResidencyMemory,
	})
	if err != nil {
		return LogicalLine{}, false, err
	}
	if err := track.frontier.Add(line.ID); err != nil {
		return LogicalLine{}, false, err
	}
	track.screen.set(track.screenRow, primaryScreenLineOwner{LineID: line.ID})
	return line, true, nil
}

func (track *HistoryTrack) createLine(req CreateLineRequest) (LogicalLine, error) {
	if req.ownedCells {
		type ownedCreator interface {
			createLineOwned(CreateLineRequest) (LogicalLine, error)
		}
		if store, ok := track.store.(ownedCreator); ok {
			return store.createLineOwned(req)
		}
	}
	return track.store.CreateLine(req)
}

func (track *HistoryTrack) ensureWritableActiveLine() (LogicalLine, error) {
	if track.activeLine != 0 && (track.frontier.Contains(track.activeLine) || track.committed.Contains(track.activeLine)) {
		line, ok := track.store.Line(track.activeLine)
		if ok {
			return line, nil
		}
	}
	line, err := track.store.CreateLine(CreateLineRequest{
		Seal:              SealStateOpen,
		CreatedGeneration: track.generation,
		ContentGeneration: track.generation,
		Residency:         ResidencyMemory,
	})
	if err != nil {
		return LogicalLine{}, err
	}
	if err := track.frontier.Add(line.ID); err != nil {
		return LogicalLine{}, err
	}
	track.activeLine = line.ID
	track.activeCol = 0
	track.overwrite = false
	track.screen.set(track.screenRow, primaryScreenLineOwner{LineID: line.ID})
	return line, nil
}

func (track *HistoryTrack) carriageReturn() error {
	if track.altScreen {
		return nil
	}
	if track.activeLine == 0 {
		return nil
	}
	if !track.frontier.Contains(track.activeLine) && !track.committed.Contains(track.activeLine) {
		track.activeLine = 0
		track.activeCol = 0
		return nil
	}
	if _, ok := track.store.Line(track.activeLine); !ok {
		track.activeLine = 0
		track.activeCol = 0
		return nil
	}
	track.activeCol = 0
	track.overwrite = true
	track.bumpGeneration()
	return nil
}

func (track *HistoryTrack) cursorForward(count int) error {
	if count <= 0 {
		count = 1
	}
	return track.moveCursorBy(count)
}

func (track *HistoryTrack) cursorBackward(count int) error {
	if count <= 0 {
		count = 1
	}
	return track.moveCursorBy(-count)
}

func (track *HistoryTrack) cursorHorizontalAbsolute(column int) error {
	if column <= 0 {
		column = 1
	}
	return track.setActiveColumn(column - 1)
}

func (track *HistoryTrack) cursorUp(count int) error {
	if count <= 0 {
		count = 1
	}
	return track.moveCursorRowBy(-count)
}

func (track *HistoryTrack) cursorDown(count int) error {
	if count <= 0 {
		count = 1
	}
	return track.moveCursorRowBy(count)
}

func (track *HistoryTrack) cursorPosition(row int, column int) error {
	if row <= 0 {
		row = 1
	}
	if column <= 0 {
		column = 1
	}
	track.setCursorScreenRow(row - 1)
	if err := track.bindActiveLineFromScreenRow(); err != nil {
		return err
	}
	return track.setActiveColumn(column - 1)
}

func (track *HistoryTrack) moveCursorRowBy(delta int) error {
	if track.altScreen {
		return nil
	}
	track.setCursorScreenRow(track.screenRow + delta)
	return track.bindActiveLineFromScreenRow()
}

func (track *HistoryTrack) setCursorScreenRow(row int) {
	if track.screenRows <= 0 {
		track.screenRow = 0
		return
	}
	if row < 0 {
		row = 0
	}
	if row >= track.screenRows {
		row = track.screenRows - 1
	}
	track.screenRow = row
}

func (track *HistoryTrack) bindActiveLineFromScreenRow() error {
	owner, ok := track.screen.owner(track.screenRow)
	if !ok || owner.LineID == 0 {
		track.activeLine = 0
		track.activeCol = 0
		track.overwrite = false
		return nil
	}
	if _, ok := track.store.Line(owner.LineID); !ok {
		track.screen.clear(track.screenRow)
		track.activeLine = 0
		track.activeCol = 0
		track.overwrite = false
		return nil
	}
	track.activeLine = owner.LineID
	track.overwrite = true
	return nil
}

func (track *HistoryTrack) moveCursorBy(delta int) error {
	if track.altScreen || track.activeLine == 0 {
		return nil
	}
	if !track.activeCursorLineValid() {
		return nil
	}
	nextCol := track.activeCol + delta
	if nextCol < 0 {
		nextCol = 0
	}
	track.activeCol = nextCol
	if delta < 0 {
		track.overwrite = true
	}
	return nil
}

func (track *HistoryTrack) setActiveColumn(column int) error {
	if track.altScreen || track.activeLine == 0 {
		return nil
	}
	if !track.activeCursorLineValid() {
		return nil
	}
	if column < 0 {
		column = 0
	}
	track.activeCol = column
	// 中文说明：终端光标移回已存在内容中间后，后续输出是覆盖写；
	// 这类 shell 补全/行编辑临时字符不能被当成 append-only history。
	track.overwrite = true
	return nil
}

func (track *HistoryTrack) activeCursorLineValid() bool {
	if track.activeLine == 0 || (!track.frontier.Contains(track.activeLine) && !track.committed.Contains(track.activeLine)) {
		track.activeLine = 0
		track.activeCol = 0
		track.overwrite = false
		return false
	}
	return true
}

func (track *HistoryTrack) eraseInLine(mode int, screenCols int, style CellStyle) error {
	if track.altScreen {
		return nil
	}
	if track.activeLine == 0 {
		if !styleCreatesVisibleBlank(style) || screenCols <= 0 {
			return nil
		}
		if _, err := track.ensureWritableActiveLine(); err != nil {
			return err
		}
	}
	if !track.frontier.Contains(track.activeLine) && !track.committed.Contains(track.activeLine) {
		track.activeLine = 0
		track.activeCol = 0
		track.overwrite = false
		return nil
	}
	line, ok := track.store.Line(track.activeLine)
	if !ok {
		return nil
	}
	line.Cells = eraseLineCellsAtColumn(line.Cells, track.activeCol, mode, screenCols, style)
	line.TailFill = nil
	line.Dirty = true
	nextGeneration := track.nextGeneration()
	line.ContentGeneration = nextGeneration
	line, err := track.replaceOwnedLine(line)
	if err != nil {
		return err
	}
	track.activeLine = line.ID
	track.activeCol = minInt(track.activeCol, logicalLineWidth(line.Cells))
	track.overwrite = false
	track.setGeneration(nextGeneration)
	return nil
}

func (track *HistoryTrack) eraseCharacters(count int, style CellStyle) error {
	if track.altScreen || count <= 0 || track.activeLine == 0 {
		return nil
	}
	if !track.frontier.Contains(track.activeLine) && !track.committed.Contains(track.activeLine) {
		track.activeLine = 0
		track.activeCol = 0
		track.overwrite = false
		return nil
	}
	line, ok := track.store.Line(track.activeLine)
	if !ok {
		return nil
	}
	line.Cells = eraseCharacterCellsAtColumn(line.Cells, track.activeCol, count, style)
	line.TailFill = nil
	line.Dirty = true
	nextGeneration := track.nextGeneration()
	line.ContentGeneration = nextGeneration
	line, err := track.replaceOwnedLine(line)
	if err != nil {
		return err
	}
	track.activeLine = line.ID
	track.activeCol = minInt(track.activeCol, logicalLineWidth(line.Cells))
	track.overwrite = true
	track.setGeneration(nextGeneration)
	return nil
}

func (track *HistoryTrack) setActiveLineTailFill(style CellStyle) error {
	if track.altScreen || !styleCreatesVisibleBlank(style) {
		return nil
	}
	line, err := track.ensureWritableActiveLine()
	if err != nil {
		return err
	}
	line.TailFill = &RowTailFill{Style: style}
	line.Dirty = true
	nextGeneration := track.nextGeneration()
	line.ContentGeneration = nextGeneration
	line, err = track.replaceOwnedLine(line)
	if err != nil {
		return err
	}
	track.activeLine = line.ID
	track.setGeneration(nextGeneration)
	return nil
}

func (track *HistoryTrack) eraseInDisplay(mode int) error {
	if track.altScreen {
		return nil
	}
	switch mode {
	case 0:
		if track.activeCol == 0 && track.primaryFullscreenFrame {
			return track.replacePrimaryFullscreenFrame()
		}
		if track.activeCol == 0 && track.primaryFullscreenIntent {
			return track.startPrimaryFullscreenFrame()
		}
		if track.screenRow == 0 && track.activeCol == 0 {
			if track.primaryFullscreenFrame {
				return track.replacePrimaryFullscreenFrame()
			}
			if track.primaryFullscreenIntent {
				return track.startPrimaryFullscreenFrame()
			}
			// 中文说明：全屏程序常见入口是先把 cursor 放到左上角再 ED0 清屏；
			// 这不是行编辑删除，必须保留进入前的 primary logical line 页面。
			return track.clearPrimaryScreenPageBreak()
		}
		return track.eraseDisplayFromCursor()
	case 1:
		return track.eraseDisplayToCursor()
	case 2:
		if track.primaryFullscreenFrame {
			return track.replacePrimaryFullscreenFrame()
		}
		if track.primaryFullscreenIntent {
			return track.startPrimaryFullscreenFrame()
		}
		return track.clearPrimaryScreenPageBreak()
	case 3:
		return track.truncateCommittedHistory(HistoryEvent{
			Kind:    EventTruncateCommittedHistory,
			LineIDs: track.committed.IDs(),
		})
	default:
		return nil
	}
}

func (track *HistoryTrack) enterPrimaryFullscreen(mode int) error {
	if track.altScreen {
		return nil
	}
	if mode != 0 {
		if track.primaryFullscreenModes == nil {
			track.primaryFullscreenModes = make(map[int]struct{})
		}
		track.primaryFullscreenModes[mode] = struct{}{}
	}
	if track.primaryFullscreenIntent {
		return nil
	}
	track.primaryFullscreenIntent = true
	track.bumpGeneration()
	return nil
}

func (track *HistoryTrack) exitPrimaryFullscreen(mode int) error {
	if mode != 0 && track.primaryFullscreenModes != nil {
		delete(track.primaryFullscreenModes, mode)
		if len(track.primaryFullscreenModes) > 0 {
			return nil
		}
	}
	if !track.primaryFullscreenIntent && !track.primaryFullscreenFrame {
		return nil
	}
	if track.primaryFullscreenFrame {
		// 中文说明：primary-screen TUI 会在运行中切换 cursor/mouse mode；
		// 这些 mode 退出不能等价于程序退出，否则正在刷新的输入框会暴露进 history。
		track.primaryFullscreenIntent = false
		track.primaryFullscreenModes = nil
		return nil
	}
	track.clearPrimaryFullscreenState()
	track.bumpGeneration()
	return nil
}

func (track *HistoryTrack) startPrimaryFullscreenFrame() error {
	if err := track.clearPrimaryScreenPageBreak(); err != nil {
		return err
	}
	track.primaryFullscreenFrame = true
	return nil
}

// replacePrimaryFullscreenFrame 清掉当前未提交 fullscreen frame，让下一帧在同一位置重建。
// 它不能碰 committed history；进入 fullscreen 前的页面已经由第一次 page-break 保留。
func (track *HistoryTrack) replacePrimaryFullscreenFrame() error {
	modes := track.primaryFullscreenModes
	if err := track.resetFrontier(); err != nil {
		return err
	}
	track.primaryFullscreenModes = modes
	track.primaryFullscreenIntent = true
	track.primaryFullscreenFrame = true
	return nil
}

func (track *HistoryTrack) clearPrimaryFullscreenState() {
	track.primaryFullscreenIntent = false
	track.primaryFullscreenFrame = false
	track.primaryFullscreenModes = nil
}

// clearPrimaryScreenPageBreak 处理真实终端里的 CSI 2J：它不是从 live
// surface 截屏造历史，而是把 core 已经持有的 primary mutable frontier
// 封口进 committed history，再清空 screen ownership，让清屏后的 UI 从新页开始。
func (track *HistoryTrack) clearPrimaryScreenPageBreak() error {
	ids := track.frontier.IDs()
	changed := false
	for _, id := range ids {
		line, ok := track.store.Line(id)
		if !ok {
			return ErrUnknownLine
		}
		if line.Seal != SealStateSealed {
			line.Seal = SealStateSealed
			line.Dirty = true
		}
		if line.Dirty {
			line.Dirty = false
			if _, err := track.replaceOwnedLine(line); err != nil {
				return err
			}
			changed = true
		}
		if !track.committed.Contains(id) {
			if err := track.committed.Append(id); err != nil {
				return err
			}
			changed = true
		}
		if track.frontier.Remove(id) {
			changed = true
		}
	}
	if track.activeLine != 0 || track.activeCol != 0 || track.overwrite {
		changed = true
	}
	track.activeLine = 0
	track.activeCol = 0
	track.overwrite = false
	track.screen.clearAll()
	if changed {
		track.bumpGeneration()
	}
	return nil
}

// eraseDisplayFromCursor 只作用于当前 primary mutable frontier：它会擦掉
// active open line 从 cursor 到尾部的内容，并清掉 cursor 之下仍可变的行；
// 它不能借机创建或截断 committed history。
func (track *HistoryTrack) eraseDisplayFromCursor() error {
	visible := track.visibleFrontierIDs()
	if len(visible) == 0 {
		return nil
	}
	cursorIndex, hasActiveLine := track.activeVisibleFrontierIndex(visible)
	if !hasActiveLine {
		// 当前 cursor 已经在空白新行上时，ED 0 对历史侧没有额外效果。
		return nil
	}
	changed, err := track.eraseActiveLineWithoutBump(0)
	if err != nil {
		return err
	}
	deleted, err := track.deleteFrontierLinesWithoutBump(visible[cursorIndex+1:])
	if err != nil {
		return err
	}
	if changed || deleted {
		track.bumpGeneration()
	}
	return nil
}

// eraseDisplayToCursor 同样只作用于 mutable frontier：它会擦掉 active line
// 从开头到 cursor 的内容，并删除 cursor 之上仍可变但尚未 committed 的行。
func (track *HistoryTrack) eraseDisplayToCursor() error {
	visible := track.visibleFrontierIDs()
	if len(visible) == 0 {
		return nil
	}
	cursorIndex, hasActiveLine := track.activeVisibleFrontierIndex(visible)
	if !hasActiveLine {
		// 当前 cursor 位于换行后的空白行时，ED 1 等价于清空上方全部 visible
		// mutable frontier。
		deleted, err := track.deleteFrontierLinesWithoutBump(visible)
		if err != nil {
			return err
		}
		if deleted {
			track.bumpGeneration()
		}
		return nil
	}
	changed, err := track.eraseActiveLineWithoutBump(1)
	if err != nil {
		return err
	}
	deleted, err := track.deleteFrontierLinesWithoutBump(visible[:cursorIndex])
	if err != nil {
		return err
	}
	if changed || deleted {
		track.bumpGeneration()
	}
	return nil
}

func (track *HistoryTrack) sealActiveLine() error {
	if track.altScreen {
		return nil
	}
	if track.activeLine == 0 {
		return track.sealBlankLine()
	}
	seal, _, ok := track.lineCommitState(track.activeLine)
	if !ok {
		track.activeLine = 0
		return nil
	}
	if seal == SealStateSealed {
		track.activeLine = 0
		track.activeCol = 0
		track.overwrite = false
		track.advanceScreenCursorLine()
		track.bumpGeneration()
		return nil
	}
	if err := track.sealLineDirty(track.activeLine); err != nil {
		return err
	}
	track.activeLine = 0
	track.activeCol = 0
	track.overwrite = false
	track.advanceScreenCursorLine()
	track.bumpGeneration()
	return nil
}

func (track *HistoryTrack) sealBlankLine() error {
	nextGeneration := track.nextGeneration()
	line, err := track.store.CreateLine(CreateLineRequest{
		Seal:              SealStateSealed,
		CreatedGeneration: nextGeneration,
		ContentGeneration: nextGeneration,
		Dirty:             true,
		Residency:         ResidencyMemory,
	})
	if err != nil {
		return err
	}
	if err := track.frontier.Add(line.ID); err != nil {
		return err
	}
	track.screen.set(track.screenRow, primaryScreenLineOwner{LineID: line.ID})
	track.advanceScreenCursorLine()
	track.setGeneration(nextGeneration)
	return nil
}

func (track *HistoryTrack) softWrapActiveLine() error {
	if track.altScreen {
		return nil
	}
	if track.activeLine == 0 {
		track.advanceScreenCursorLine()
		track.bumpGeneration()
		return nil
	}
	if !track.activeCursorLineValid() {
		return nil
	}
	// 中文说明：自动换行只改变当前 logical line 在 primary screen 上的
	// visual-row ownership；logical line 仍保持 open，不能被 seal 成两条历史。
	track.advanceScreenCursorLine()
	track.screen.set(track.screenRow, primaryScreenLineOwner{LineID: track.activeLine})
	track.overwrite = false
	track.bumpGeneration()
	return nil
}

func (track *HistoryTrack) mutateFrontierLine(event HistoryEvent) error {
	if track.altScreen {
		return nil
	}
	lineID := event.LineID
	if lineID == 0 {
		lineID = track.activeLine
	}
	if lineID == 0 {
		return ErrInvalidLineID
	}
	if !track.frontier.Contains(lineID) {
		return ErrLineNotMutable
	}
	line, ok := track.store.Line(lineID)
	if !ok {
		return ErrUnknownLine
	}
	line.Cells = cloneCells(event.Cells)
	line.TailFill = nil
	line.Dirty = true
	nextGeneration := track.nextGeneration()
	line.ContentGeneration = nextGeneration
	line, err := track.replaceOwnedLine(line)
	if err != nil {
		return err
	}
	track.activeLine = line.ID
	track.activeCol = logicalLineWidth(line.Cells)
	track.overwrite = false
	track.screen.set(track.screenRow, primaryScreenLineOwner{LineID: line.ID})
	track.setGeneration(nextGeneration)
	return nil
}

func (track *HistoryTrack) resetFrontier() error {
	if track.altScreen {
		return nil
	}
	ids := track.frontier.IDs()
	if len(ids) == 0 {
		track.activeLine = 0
		track.activeCol = 0
		track.overwrite = false
		track.screen.clearAll()
		track.clearPrimaryFullscreenState()
		return nil
	}
	for _, id := range ids {
		track.frontier.Remove(id)
		if !track.committed.Contains(id) {
			track.store.DeleteLine(id)
		}
	}
	track.activeLine = 0
	track.activeCol = 0
	track.overwrite = false
	track.screen.clearAll()
	track.clearPrimaryFullscreenState()
	track.bumpGeneration()
	return nil
}

func (track *HistoryTrack) activeVisibleFrontierIndex(visible []LogicalLineID) (int, bool) {
	if track.activeLine == 0 || !track.frontier.Contains(track.activeLine) || track.frontier.IsHidden(track.activeLine) {
		return -1, false
	}
	for idx, id := range visible {
		if id == track.activeLine {
			return idx, true
		}
	}
	return -1, false
}

func (track *HistoryTrack) eraseActiveLineWithoutBump(mode int) (bool, error) {
	if track.activeLine == 0 || !track.frontier.Contains(track.activeLine) {
		return false, nil
	}
	line, ok := track.store.Line(track.activeLine)
	if !ok || line.Seal != SealStateOpen {
		return false, nil
	}
	next := line.Clone()
	next.Cells = eraseLineCellsAtColumn(next.Cells, track.activeCol, mode, 0, CellStyle{})
	next.TailFill = nil
	next.Dirty = true
	next.ContentGeneration = track.nextGeneration()
	replaced, err := track.replaceOwnedLine(next)
	if err != nil {
		return false, err
	}
	track.activeLine = replaced.ID
	track.activeCol = minInt(track.activeCol, logicalLineWidth(replaced.Cells))
	track.overwrite = false
	return true, nil
}

func (track *HistoryTrack) deleteFrontierLinesWithoutBump(ids []LogicalLineID) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	changed := false
	for _, id := range ids {
		if !track.frontier.Remove(id) {
			continue
		}
		if !track.committed.Contains(id) {
			track.store.DeleteLine(id)
		}
		if track.activeLine == id {
			track.activeLine = 0
			track.activeCol = 0
			track.overwrite = false
		}
		track.screen.removeLine(id)
		changed = true
	}
	return changed, nil
}

func (track *HistoryTrack) commitFrontier(force bool) error {
	if track.altScreen && !force {
		return nil
	}
	if track.primaryFullscreenFrame && !force {
		return nil
	}
	if force {
		track.clearPrimaryFullscreenState()
	}
	ids := track.frontier.IDs()
	if len(ids) == 0 {
		return nil
	}

	changed := false
	for _, id := range ids {
		seal, dirty, ok := track.lineCommitState(id)
		if !ok {
			return ErrUnknownLine
		}
		if force && seal != SealStateSealed {
			if err := track.sealLineDirty(id); err != nil {
				return err
			}
			seal = SealStateSealed
			dirty = true
		}
		if !force && seal != SealStateSealed {
			continue
		}
		// 普通 commit 只允许提交已经 sealed 且不再被 primary screen 持有的 line。
		if !force && !track.lineCommittableID(id) {
			continue
		}
		if dirty {
			if err := track.markLineClean(id); err != nil {
				return err
			}
			changed = true
		}
		if !track.committed.Contains(id) {
			if err := track.committed.Append(id); err != nil {
				return err
			}
			changed = true
		}
		if track.frontier.Remove(id) {
			changed = true
		}
		if track.activeLine == id {
			track.activeLine = 0
			track.activeCol = 0
			track.overwrite = false
		}
	}
	if changed {
		track.bumpGeneration()
	}
	return nil
}

func (track *HistoryTrack) reclaimCommittedSuffix(event HistoryEvent) error {
	ids := track.reclaimIDs(event)
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if !track.committed.Contains(id) {
			return ErrLineNotCommitted
		}
		if _, ok := track.store.Line(id); !ok {
			return ErrUnknownLine
		}
	}
	insertIDs := make([]LogicalLineID, 0, len(ids))
	for _, id := range ids {
		track.committed.Remove(id)
		if !track.frontier.Contains(id) {
			insertIDs = append(insertIDs, id)
		}
	}
	if err := track.frontier.PrependMany(insertIDs); err != nil {
		return err
	}
	for _, id := range ids {
		track.frontier.Reveal(id)
		track.activeLine = id
		line, _ := track.store.Line(id)
		track.activeCol = logicalLineWidth(line.Cells)
		track.overwrite = false
		track.screen.set(track.screenRow, primaryScreenLineOwner{LineID: id})
	}
	track.bumpGeneration()
	return nil
}

func (track *HistoryTrack) reclaimIDs(event HistoryEvent) []LogicalLineID {
	if len(event.LineIDs) > 0 {
		return cloneLineIDs(event.LineIDs)
	}
	ids := track.committed.IDs()
	if event.Count <= 0 || event.Count > len(ids) {
		return nil
	}
	return cloneLineIDs(ids[len(ids)-event.Count:])
}

func (track *HistoryTrack) hideFrontier(event HistoryEvent) error {
	ids := track.frontierTargetIDs(event)
	if len(ids) == 0 {
		return nil
	}
	changed := false
	for _, id := range ids {
		wasHidden := track.frontier.IsHidden(id)
		if err := track.frontier.Hide(id); err != nil {
			return err
		}
		if !wasHidden {
			changed = true
		}
	}
	if changed {
		track.bumpGeneration()
	}
	return nil
}

func (track *HistoryTrack) frontierTargetIDs(event HistoryEvent) []LogicalLineID {
	if len(event.LineIDs) > 0 {
		return cloneLineIDs(event.LineIDs)
	}
	ids := track.frontier.IDs()
	if event.Count <= 0 || event.Count > len(ids) {
		return ids
	}
	return cloneLineIDs(ids[len(ids)-event.Count:])
}

func (track *HistoryTrack) truncateCommittedHistory(event HistoryEvent) error {
	ids := track.truncateIDs(event)
	if len(ids) == 0 {
		return nil
	}
	changed := false
	for _, id := range ids {
		if !track.committed.Remove(id) {
			continue
		}
		if !track.frontier.Contains(id) {
			track.store.DeleteLine(id)
		}
		track.screen.removeLine(id)
		changed = true
	}
	if changed {
		track.bumpGeneration()
	}
	return nil
}

func (track *HistoryTrack) truncateIDs(event HistoryEvent) []LogicalLineID {
	if len(event.LineIDs) > 0 {
		return cloneLineIDs(event.LineIDs)
	}
	ids := track.committed.IDs()
	if event.Count <= 0 || event.Count >= len(ids) {
		return ids
	}
	return cloneLineIDs(ids[:event.Count])
}

func (track *HistoryTrack) switchAltScreen(enter bool) error {
	if track.altScreen == enter {
		return nil
	}
	if enter {
		if err := track.clearPrimaryScreenPageBreak(); err != nil {
			return err
		}
		track.clearPrimaryFullscreenState()
	}
	track.altScreen = enter
	track.bumpGeneration()
	return nil
}

func (track *HistoryTrack) appendAltScreenFrame(rows [][]Cell) error {
	if len(rows) == 0 {
		return nil
	}
	wasAlt := track.altScreen
	track.altScreen = false
	defer func() {
		track.altScreen = wasAlt
	}()
	// 中文说明：这是 alt-screen 退出边界的显式保留策略，不从普通 live
	// snapshot 反推历史；每一行作为新的 logical line 追加并立即提交。
	for _, row := range rows {
		if len(row) > 0 {
			if err := track.writePrimaryCells(row, false); err != nil {
				return err
			}
		}
		if err := track.sealActiveLine(); err != nil {
			return err
		}
		if err := track.commitFrontier(true); err != nil {
			return err
		}
	}
	return nil
}

func (track *HistoryTrack) resize(event HistoryEvent) error {
	before := track.generation
	switch event.ResizeDirection {
	case ResizeGrow:
		if err := track.growResize(event.Count); err != nil {
			return err
		}
	case ResizeShrink:
		if err := track.shrinkResize(event.Count); err != nil {
			return err
		}
	case ResizeSame, "":
		// Resize 本身不是历史创建事件，但 projection 依赖 cols，所以必须让
		// active history window 失效。
	default:
		return ErrInvalidResizeDirection
	}
	if track.generation == before {
		track.bumpGeneration()
	}
	return nil
}

func (track *HistoryTrack) bumpGeneration() {
	track.generation++
}

func (track *HistoryTrack) nextGeneration() Generation {
	return track.generation + 1
}

func (track *HistoryTrack) setGeneration(generation Generation) {
	if generation > track.generation {
		track.generation = generation
		return
	}
	track.bumpGeneration()
}

func (track *HistoryTrack) lineCommittable(id LogicalLineID) bool {
	if !track.frontier.Contains(id) || track.frontier.IsHidden(id) {
		return false
	}
	committable := false
	if !track.inspectLine(id, func(line LogicalLine) {
		committable = line.Seal == SealStateSealed && track.lineCommittableLoaded(id, line)
	}) {
		return false
	}
	return committable
}

func (track *HistoryTrack) lineCommittableLoaded(id LogicalLineID, line LogicalLine) bool {
	return track.frontier.Contains(id) &&
		!track.frontier.IsHidden(id) &&
		line.Seal == SealStateSealed &&
		!track.screen.containsLine(id)
}

func (track *HistoryTrack) lineCommittableID(id LogicalLineID) bool {
	return track.frontier.Contains(id) &&
		!track.frontier.IsHidden(id) &&
		!track.screen.containsLine(id)
}

func (track *HistoryTrack) visibleFrontierIDs() []LogicalLineID {
	ids := track.frontier.IDs()
	visible := make([]LogicalLineID, 0, len(ids))
	for _, id := range ids {
		if track.frontier.IsHidden(id) {
			continue
		}
		visible = append(visible, id)
	}
	return visible
}

// growResize 先把 shrink 时藏起来的 frontier 恢复成 visible ownership，
// 只有恢复不够时才按完整 logical line reclaim committed suffix。
func (track *HistoryTrack) growResize(count int) error {
	remaining := count
	hidden := track.frontier.HiddenIDs()
	for i := len(hidden) - 1; i >= 0 && remaining > 0; i-- {
		if track.frontier.Reveal(hidden[i]) {
			remaining--
		}
	}
	if remaining > 0 {
		if err := track.reclaimCommittedSuffix(HistoryEvent{
			Kind:    EventReclaimCommittedSuffix,
			Count:   remaining,
			LineIDs: nil,
		}); err != nil {
			return err
		}
	}
	return nil
}

// shrinkResize 只把最老的 visible frontier 转成 hidden ownership，不得借机提交。
func (track *HistoryTrack) shrinkResize(count int) error {
	if count <= 0 {
		return nil
	}
	visible := track.visibleFrontierIDs()
	if len(visible) == 0 {
		return nil
	}
	if count > len(visible) {
		count = len(visible)
	}
	return track.hideFrontier(HistoryEvent{
		Kind:    EventHideFrontier,
		LineIDs: cloneLineIDs(visible[:count]),
	})
}

func (track *HistoryTrack) advanceScreenCursorLine() {
	if track.screenRows <= 0 {
		track.screenRow = 0
		return
	}
	if track.screenRow >= track.screenRows-1 {
		track.screen.scrollUp()
		track.screenRow = track.screenRows - 1
		return
	}
	track.screenRow++
}

type primaryScreenLineOwner struct {
	LineID LogicalLineID
}

// primaryScreenLineMap 是 history 侧自己的“当前屏幕行 ownership”。
// 它只记录 logical line id，不读取 live surface，避免从实时快照反推历史 truth。
type primaryScreenLineMap struct {
	rows []primaryScreenLineOwner
}

func (screen *primaryScreenLineMap) resize(rows int) {
	if rows <= 0 {
		screen.rows = nil
		return
	}
	if len(screen.rows) == rows {
		return
	}
	next := make([]primaryScreenLineOwner, rows)
	if len(screen.rows) > 0 {
		if len(screen.rows) <= rows {
			copy(next, screen.rows)
		} else {
			copy(next, screen.rows[len(screen.rows)-rows:])
		}
	}
	screen.rows = next
}

func (screen *primaryScreenLineMap) set(row int, owner primaryScreenLineOwner) {
	if row < 0 || row >= len(screen.rows) {
		return
	}
	screen.rows[row] = owner
}

func (screen *primaryScreenLineMap) owner(row int) (primaryScreenLineOwner, bool) {
	if row < 0 || row >= len(screen.rows) {
		return primaryScreenLineOwner{}, false
	}
	owner := screen.rows[row]
	return owner, owner.LineID != 0
}

func (screen *primaryScreenLineMap) clear(row int) {
	if row < 0 || row >= len(screen.rows) {
		return
	}
	screen.rows[row] = primaryScreenLineOwner{}
}

func (screen *primaryScreenLineMap) clearAll() {
	for i := range screen.rows {
		screen.rows[i] = primaryScreenLineOwner{}
	}
}

func (screen *primaryScreenLineMap) scrollUp() {
	if len(screen.rows) == 0 {
		return
	}
	copy(screen.rows, screen.rows[1:])
	screen.rows[len(screen.rows)-1] = primaryScreenLineOwner{}
}

func (screen *primaryScreenLineMap) removeLine(id LogicalLineID) {
	if id == 0 {
		return
	}
	for i := range screen.rows {
		if screen.rows[i].LineID == id {
			screen.rows[i] = primaryScreenLineOwner{}
		}
	}
}

func (screen *primaryScreenLineMap) containsLine(id LogicalLineID) bool {
	if id == 0 {
		return false
	}
	for _, owner := range screen.rows {
		if owner.LineID == id {
			return true
		}
	}
	return false
}

func overwriteLineCellsAtColumn(existing []Cell, column int, incoming []Cell) []Cell {
	if len(incoming) == 0 {
		return cloneCells(existing)
	}
	base := expandUnmeasuredCellsForMutation(existing)
	write := expandUnmeasuredCellsForMutation(incoming)
	if column < 0 {
		column = 0
	}
	if column > len(base) {
		padding := make([]Cell, column-len(base))
		for i := range padding {
			padding[i] = Cell{Text: " ", Width: 1}
		}
		base = append(base, padding...)
	}
	end := column + len(write)
	if end > len(base) {
		padding := make([]Cell, end-len(base))
		for i := range padding {
			padding[i] = Cell{Text: " ", Width: 1}
		}
		base = append(base, padding...)
	}
	copy(base[column:end], cloneCells(write))
	return compactMutationCells(base)
}

func logicalLineWidth(cells []Cell) int {
	width := 0
	for _, cell := range cells {
		width += cellWidth(cell)
	}
	return width
}

func expandUnmeasuredCellsForMutation(cells []Cell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]Cell, 0, len(cells))
	for _, cell := range cells {
		width := cellWidth(cell)
		if width <= 0 {
			continue
		}
		if cell.Text == "" {
			out = append(out, blankFootprintCells(cell, width)...)
			continue
		}
		if width == 1 && len(textClusters(cell.Text)) == 1 {
			next := cell
			next.Width = 1
			out = append(out, next)
			continue
		}
		out = append(out, splitMeasuredCell(cell)...)
	}
	return out
}

func compactMutationCells(cells []Cell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]Cell, 0, len(cells))
	for _, cell := range cells {
		width := cellWidth(cell)
		if width <= 0 {
			continue
		}
		if cell.Text == "" {
			out = append(out, blankFootprintCells(cell, width)...)
			continue
		}
		out = append(out, cell)
	}
	return out
}

func mergeAppendableCellRuns(cells []Cell) []Cell {
	if len(cells) < 2 {
		return cells
	}
	out := make([]Cell, 0, len(cells))
	for _, cell := range cells {
		if len(out) > 0 && canMergeAppendableCell(out[len(out)-1], cell) {
			out[len(out)-1].Text += cell.Text
			out[len(out)-1].Width += cell.Width
			continue
		}
		out = append(out, cell)
	}
	return out
}

func canMergeAppendableCell(left Cell, right Cell) bool {
	return left.Style == right.Style &&
		left.LinkURL == right.LinkURL &&
		left.LinkParams == right.LinkParams &&
		left.Width > 0 &&
		right.Width > 0 &&
		asciiSingleWidthText(left.Text) &&
		asciiSingleWidthText(right.Text)
}

func asciiSingleWidthText(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < 0x20 || r >= 0x7f {
			return false
		}
	}
	return true
}

func eraseLineCellsAtColumn(existing []Cell, column int, mode int, screenCols int, style CellStyle) []Cell {
	base := expandUnmeasuredCellsForMutation(existing)
	if len(base) == 0 && (!styleCreatesVisibleBlank(style) || screenCols <= 0) {
		return nil
	}
	if column < 0 {
		column = 0
	}
	rowStart := 0
	rowEnd := len(base)
	if screenCols > 0 {
		rowStart = (column / screenCols) * screenCols
		rowEnd = rowStart + screenCols
	}
	eraseFrom := column
	eraseTo := rowEnd
	switch mode {
	case 1:
		eraseFrom = rowStart
		eraseTo = column + 1
	case 2:
		eraseFrom = rowStart
		eraseTo = rowEnd
	default:
		eraseFrom = column
		eraseTo = rowEnd
	}
	if eraseFrom < 0 {
		eraseFrom = 0
	}
	targetLen := len(base)
	if styleCreatesVisibleBlank(style) && eraseTo > targetLen {
		targetLen = eraseTo
	}
	base = ensureMutationCellLen(base, targetLen)
	if eraseTo > len(base) {
		eraseTo = len(base)
	}
	if eraseFrom > eraseTo {
		eraseFrom = eraseTo
	}
	for i := eraseFrom; i < eraseTo; i++ {
		base[i] = Cell{Text: " ", Width: 1, Style: style}
	}
	return compactMutationCells(base)
}

func eraseCharacterCellsAtColumn(existing []Cell, column int, count int, style CellStyle) []Cell {
	base := expandUnmeasuredCellsForMutation(existing)
	if len(base) == 0 || count <= 0 {
		return base
	}
	if column < 0 {
		column = 0
	}
	if column >= len(base) {
		return compactMutationCells(base)
	}
	eraseTo := column + count
	if eraseTo > len(base) {
		eraseTo = len(base)
	}
	for i := column; i < eraseTo; i++ {
		base[i] = Cell{Text: " ", Width: 1, Style: style}
	}
	return compactMutationCells(base)
}

func eraseBlankStyle(style CellStyle) CellStyle {
	return CellStyle{BG: style.BG}
}

func styleCreatesVisibleBlank(style CellStyle) bool {
	return style.BG != ""
}

func ensureMutationCellLen(cells []Cell, target int) []Cell {
	if target <= len(cells) {
		return cells
	}
	padding := make([]Cell, target-len(cells))
	for i := range padding {
		padding[i] = Cell{Text: " ", Width: 1}
	}
	return append(cells, padding...)
}

func minInt(a, b int) int {
	if a <= b {
		return a
	}
	return b
}
