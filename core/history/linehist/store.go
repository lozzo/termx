package linehist

import (
	"fmt"
	"sync"
	"time"

	"github.com/muxvia/muxvia/core/history"
	"github.com/muxvia/muxvia/shared/perftrace"
	vterm "github.com/muxvia/muxvia/vterm/vterm"
)

// ScreenRow 是查询时刻 emulator 当前屏的一条物理行快照。
// Wrapped=true 表示该行软换行续到下一行（与滚出行的 Wrapped 语义一致）。
type ScreenRow struct {
	Cells   []vterm.Cell
	Wrapped bool
}

// ScreenSnapshot 是查询时刻的 emulator 当前屏（热段唯一来源）。
// 它只在持有 ingest gate 时采集，保证与冷段落盘边界一致：任一物理行
// 要么已作为 EvictedRows 落盘，要么出现在本快照里，不重不漏。
// alt 期间 PrimaryRows 携带被 alt 覆盖但仍未滚出的主屏保存行——它们
// 属于 primary 时间线热段（alt 退出后程序仍可改写），必须继续投影。
type ScreenSnapshot struct {
	Cols        int
	Rows        []ScreenRow
	InAlt       bool
	PrimaryRows []ScreenRow
}

// ScreenSnapshotFromVTerm 从 tap vterm 采集当前屏快照。
// 尾部空白行裁掉（终端底部未使用区域不是内容），中间空行保留。
// 调用方必须已经用 ingest gate 串行化了对该 vterm 的写入。
func ScreenSnapshotFromVTerm(vt *vterm.VTerm) ScreenSnapshot {
	if vt == nil {
		return ScreenSnapshot{}
	}
	var rows []ScreenRow
	info := vt.VisitTrimmedScreenRows(func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell) {
		for len(rows) < rowIndex {
			rows = append(rows, ScreenRow{})
		}
		row := ScreenRow{}
		if cellCount > 0 {
			row.Cells = make([]vterm.Cell, cellCount)
			for i := 0; i < cellCount; i++ {
				row.Cells[i] = cellAt(i)
			}
		}
		rows = append(rows, row)
	})
	wrapped := vt.ScreenWrapped()
	last := -1
	for i := range rows {
		if i < len(wrapped) {
			rows[i].Wrapped = wrapped[i]
		}
		if len(rows[i].Cells) > 0 || rows[i].Wrapped {
			last = i
		}
	}
	snap := ScreenSnapshot{Cols: info.Cols, Rows: rows[:last+1], InAlt: info.IsAlternateScreen}
	if info.IsAlternateScreen {
		primaryCells, primaryWrapped := vt.PrimarySavedScreenRows()
		primary := make([]ScreenRow, len(primaryCells))
		lastPrimary := -1
		for i := range primaryCells {
			primary[i] = ScreenRow{Cells: primaryCells[i]}
			if i < len(primaryWrapped) {
				primary[i].Wrapped = primaryWrapped[i]
			}
			if len(primary[i].Cells) > 0 || primary[i].Wrapped {
				lastPrimary = i
			}
		}
		snap.PrimaryRows = primary[:lastPrimary+1]
	}
	return snap
}

// frozenView 是 Freeze 时刻的投影边界。冷段是 append-only 文件的不可变
// 记录区间，只需记录 coldBase/coldCount（绝对域）；热段（当时的 mutable
// 屏幕行）必须 materialize，因为后续 repaint 会改写屏幕。ED3 只写软页
// 边界、不隐藏旧冷段，所以 token 与 live 视图都按同一份 logical-line truth
// 投影。
type frozenView struct {
	coldBase   int
	coldCount  int
	cols       int
	hot        []history.HistoryRow
	generation history.Generation
}

// liveView 是一次查询的一致性视图：coldBase/coldCount/热段行在 ingest gate
// 内采集，之后冷段记录 [coldBase, coldBase+coldCount) 是不可变区间，可以在
// gate 外分页读文件。
type liveView struct {
	coldBase   int
	coldCount  int
	cols       int
	hot        []history.HistoryRow
	generation history.Generation
}

// Store 实现 history.HistoryStore：冷段 = logical-line 文件，热段 =
// emulator 当前屏，查询时按 logical-line 窗口返回 source rows。visual
// reflow 由 TUI 根据当前 cols 本地完成；本层不持有第二份屏幕模型，也不
// 构建全局 visual row 坐标。Apply(mutation batch) 是 no-op，写 truth 的
// 唯一入口是 ApplyTransaction。
type Store struct {
	mu         sync.Mutex
	terminalID string
	engine     *Engine
	screen     func() ScreenSnapshot
	gate       sync.Locker
	generation history.Generation
	nextToken  uint64
	frozen     map[history.HistoryToken]*frozenView
}

// NewStore 创建 linehist store。screen/gate 由 Terminal 通过 Bind 注入。
func NewStore(terminalID string, engine *Engine) *Store {
	return &Store{
		terminalID: terminalID,
		engine:     engine,
		frozen:     make(map[history.HistoryToken]*frozenView),
	}
}

// Bind 注入热段快照来源与 ingest gate。gate 必须是串行化
// ApplyTransaction 调用的同一把锁（Terminal 的 tapOpMu）：查询在 gate 内
// 采集 coldCount+屏幕快照，保证滚出行不重不漏。
func (store *Store) Bind(screen func() ScreenSnapshot, gate sync.Locker) {
	if store == nil {
		return
	}
	store.mu.Lock()
	store.screen = screen
	store.gate = gate
	store.mu.Unlock()
}

// ApplyTransaction 消费一次 tap 事务的 EvictedRows 与 ClearScrollback 边界。
// 调用方必须持有 gate（Terminal 在 tapOpMu 临界区内、ApplyPTYWrite 之后
// 调用）；本方法不能再锁 gate，否则自死锁。
// 顺序：先落盘本事务滚出的行，再处理 clear-scrollback 软页边界——`clear`
// 命令常见形态是 ED2（把整屏挤进 scrollback）+ ED3（清 terminal
// scrollback），先消费 EvictedRows 才能保住 clear 前内容。ED3 不删除也
// 不隐藏 authoritative history；RIS 刻意不清历史（与 xterm 默认一致）。
func (store *Store) ApplyTransaction(tx vterm.TerminalSemanticTransaction) error {
	if store == nil {
		return nil
	}
	err := store.engine.ApplyEvictedRows(tx.EvictedRows)
	if err == nil && tx.ClearScrollback {
		err = store.engine.ApplyClearScrollbackBoundary()
	}
	store.mu.Lock()
	store.generation++
	store.mu.Unlock()
	return err
}

// SealOpenTail 把未闭合尾部强制闭合落盘（进程退出等 lifecycle 边界）。
// 它在 gate 外调用，自己获取 gate 保持与 ingest 的互斥。
func (store *Store) SealOpenTail() error {
	if store == nil {
		return nil
	}
	unlock := store.lockGate()
	defer unlock()
	err := store.engine.SealOpenTail()
	store.mu.Lock()
	store.generation++
	store.mu.Unlock()
	return err
}

// SealLifecycleTail 在进程退出、remove 或 restart 前封存当前 primary 时间线热段。
// truth source 仍是同一把 gate 下的 vterm 当前屏：已经离开屏幕的内容来自
// Engine open tail，仍在 primary 当前屏的行来自 ScreenSnapshot；alt 当前屏
// 是 transient，不传入封存，只保留被 alt 覆盖的 primary saved rows。
func (store *Store) SealLifecycleTail() error {
	if store == nil {
		return nil
	}
	unlock := store.lockGate()
	defer unlock()
	store.mu.Lock()
	screen := store.screen
	store.mu.Unlock()
	var snap ScreenSnapshot
	if screen != nil {
		snap = screen()
	}
	rows := snap.Rows
	if snap.InAlt {
		rows = snap.PrimaryRows
	}
	err := store.engine.SealPrimaryScreenRows(rows)
	store.mu.Lock()
	store.generation++
	store.mu.Unlock()
	return err
}

// AppendLifecycleLines 把 core terminal lifecycle marker 写入 authoritative
// history。它只服务 core-owned start/exit 边界，不能被用于从 live screen、
// TUI rows 或 raw PTY fallback 生成程序正文。
func (store *Store) AppendLifecycleLines(lines []string) error {
	if store == nil || len(lines) == 0 {
		return nil
	}
	unlock := store.lockGate()
	defer unlock()
	err := store.engine.AppendLifecycleLines(lines)
	store.mu.Lock()
	store.generation++
	store.mu.Unlock()
	return err
}

// Close 落盘未闭合尾部并关闭文件（terminal remove/shutdown 边界）。
func (store *Store) Close() error {
	if store == nil {
		return nil
	}
	unlock := store.lockGate()
	defer unlock()
	return store.engine.Close()
}

// Apply 是 HistoryStore 兼容入口。linehist 的写 truth 只来自
// ApplyTransaction 的 EvictedRows，renderer mutation batch 一律忽略。
func (store *Store) Apply(batch history.HistoryMutationBatch) error {
	return nil
}

// ReadState 返回只读边界诊断。linehist 路径没有 classifier；这里只暴露
// generation 与是否已有 sealed timeline，不能从中派生 payload。
func (store *Store) ReadState() history.HistoryReadState {
	if store == nil {
		return history.HistoryReadState{}
	}
	store.mu.Lock()
	generation := store.generation
	store.mu.Unlock()
	_, visible := store.engine.VisibleLineRange()
	return history.HistoryReadState{
		Generation:  generation,
		HasTimeline: visible > 0,
	}
}

// LatestWindow 返回投影尾部 replace window。
func (store *Store) LatestWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	finish := perftrace.Measure("core.linehist.window.latest")
	responseRows := 0
	defer func() { finish(responseRows) }()
	req, view, err := store.viewForRequest(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	total := viewLogicalTotal(view)
	limit := normalizedLimit(req.Limit)
	start := maxInt(0, total-limit)
	page, err := store.logicalRowsForLineRange(view, start, total)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	responseRows = len(page)
	boundary := boundaryForRows(page, view.generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeLine(page[0], view.generation, req.Token, start > 0)
	}
	return buildWindow(req, page, total, history.HistoryWindowReplace, boundary, view.generation, start > 0), nil
}

// OlderWindow 按 cursor.LineID 向更旧方向 prepend logical-line 分页。
func (store *Store) OlderWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	finish := perftrace.Measure("core.linehist.window.older")
	responseRows := 0
	defer func() { finish(responseRows) }()
	req, view, err := store.viewForRequest(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	total := viewLogicalTotal(view)
	if !req.Cursor.Valid {
		boundary := history.HistoryBoundary{Cursor: history.HistoryCursor{Generation: view.generation, Token: req.Token}}
		return buildWindow(req, nil, total, history.HistoryWindowPrepend, boundary, view.generation, false), nil
	}
	limit := normalizedLimit(req.Limit)
	end := clampInt(int(req.Cursor.LineID)-1, 0, total)
	start := maxInt(0, end-limit)
	page, err := store.logicalRowsForLineRange(view, start, end)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	responseRows = len(page)
	boundary := boundaryForRows(page, view.generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeLine(page[0], view.generation, req.Token, start > 0)
	}
	return buildWindow(req, page, total, history.HistoryWindowPrepend, boundary, view.generation, start > 0), nil
}

// OldestWindow 从投影 head 返回 replace window（TUI copy mode `g` 跳转）。
func (store *Store) OldestWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	finish := perftrace.Measure("core.linehist.window.oldest")
	responseRows := 0
	defer func() { finish(responseRows) }()
	req, view, err := store.viewForRequest(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	total := viewLogicalTotal(view)
	limit := normalizedLimit(req.Limit)
	end := minInt(total, limit)
	page, err := store.logicalRowsForLineRange(view, 0, end)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	responseRows = len(page)
	boundary := boundaryForRows(page, view.generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeLine(page[0], view.generation, req.Token, false)
		boundary.LastLineID = history.LogicalLineID(total)
	}
	return buildWindow(req, page, total, history.HistoryWindowReplace, boundary, view.generation, end < total), nil
}

// NewerWindow 按 cursor.LineID 向更新方向 append logical-line 分页。
func (store *Store) NewerWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	finish := perftrace.Measure("core.linehist.window.newer")
	responseRows := 0
	defer func() { finish(responseRows) }()
	req, view, err := store.viewForRequest(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	total := viewLogicalTotal(view)
	if !req.Cursor.Valid {
		boundary := history.HistoryBoundary{Cursor: history.HistoryCursor{Generation: view.generation, Token: req.Token}}
		return buildWindow(req, nil, total, history.HistoryWindowAppend, boundary, view.generation, false), nil
	}
	limit := normalizedLimit(req.Limit)
	start := clampInt(int(req.Cursor.LineID), 0, total)
	end := minInt(total, start+limit)
	page, err := store.logicalRowsForLineRange(view, start, end)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	responseRows = len(page)
	boundary := boundaryForRows(page, view.generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeLine(page[len(page)-1], view.generation, req.Token, end < total)
		boundary.LastLineID = history.LogicalLineID(total)
	}
	return buildWindow(req, page, total, history.HistoryWindowAppend, boundary, view.generation, end < total), nil
}

// Freeze 记录当前投影边界。冷段是不可变文件前缀（记 coldCount 即可），
// 热段 materialize 成 rows；后续 repaint/eviction 不影响 token 视图。
func (store *Store) Freeze(req history.FreezeHistoryRequest) (history.FrozenHistorySnapshot, error) {
	finish := perftrace.Measure("core.linehist.freeze")
	frozenRows := 0
	defer func() { finish(frozenRows) }()
	if store == nil {
		return history.FrozenHistorySnapshot{}, nil
	}
	view := store.captureLive(req.Cols)
	frozenRows = viewLogicalTotal(view)
	store.mu.Lock()
	store.nextToken++
	token := history.HistoryToken(fmt.Sprintf("linehist-%d", store.nextToken))
	store.frozen[token] = &frozenView{
		coldBase:   view.coldBase,
		coldCount:  view.coldCount,
		cols:       view.cols,
		hot:        cloneRows(view.hot),
		generation: view.generation,
	}
	store.mu.Unlock()
	windowReq := history.HistoryWindowRequest{
		TerminalID: store.terminalIDFor(req.TerminalID),
		Cols:       req.Cols,
		Limit:      req.Limit,
		Token:      token,
	}
	latest, err := store.LatestWindow(windowReq)
	if err != nil {
		store.mu.Lock()
		delete(store.frozen, token)
		store.mu.Unlock()
		return history.FrozenHistorySnapshot{}, err
	}
	return history.FrozenHistorySnapshot{
		Token:                 token,
		TerminalID:            store.terminalIDFor(req.TerminalID),
		Cols:                  req.Cols,
		CommittedUpperBound:   history.LogicalLineID(view.coldCount),
		FrozenFrontierLineIDs: hotLineIDs(view.hot),
		Boundary:              latest.Boundary,
		Generation:            view.generation,
		CreatedAt:             time.Now().UTC(),
	}, nil
}

// Copy 按 cursor LineID 选择行区间并返回 plain text。选择语义与旧 store
// 一致：start line 未命中回退到 head，end line 未命中回退到 tail，行间
// 以 "\n" 连接。冷段 LineID 直接映射记录序号，只 materialize 命中区间。
func (store *Store) Copy(req history.HistoryCopyRequest) (string, error) {
	finish := perftrace.Measure("core.linehist.copy")
	copiedRows := 0
	defer func() { finish(copiedRows) }()
	_, view, err := store.viewForRequest(history.HistoryWindowRequest{
		TerminalID: req.TerminalID,
		Cols:       req.Cols,
		Token:      req.Token,
	})
	if err != nil {
		return "", err
	}
	total := viewLogicalTotal(view)
	if total == 0 {
		return "", nil
	}
	startLine := 0
	endLine := total
	if req.Start.Valid || req.End.Valid {
		startOffset := lineOffsetForCopyCursor(req.Start.LineID, total, 0)
		endOffset := lineOffsetForCopyCursor(req.End.LineID, total, total-1)
		if startOffset > endOffset {
			startOffset, endOffset = endOffset, startOffset
		}
		startLine = startOffset
		endLine = endOffset + 1
	}
	rows, err := store.logicalRowsForLineRange(view, startLine, endLine)
	if err != nil {
		return "", err
	}
	copiedRows = len(rows)
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, rowText(row.Cells))
	}
	return joinLines(texts), nil
}

func lineOffsetForCopyCursor(id history.LogicalLineID, total int, fallback int) int {
	if id == 0 {
		return clampInt(fallback, 0, maxInt(0, total-1))
	}
	return clampInt(int(id)-1, 0, maxInt(0, total-1))
}

// Release 释放 frozen token。
func (store *Store) Release(token history.HistoryToken) error {
	if store == nil || token == "" {
		return nil
	}
	store.mu.Lock()
	delete(store.frozen, token)
	store.mu.Unlock()
	return nil
}

// lockGate 获取 ingest gate（若已 Bind），返回解锁函数。
func (store *Store) lockGate() func() {
	store.mu.Lock()
	gate := store.gate
	store.mu.Unlock()
	if gate == nil {
		return func() {}
	}
	finish := perftrace.Measure("core.linehist.gate_wait")
	gate.Lock()
	finish(0)
	return gate.Unlock
}

// captureLive 在 gate 内采集一致性视图：可见冷段区间、未闭合尾部与当前屏。
// gate 释放后冷段记录 [coldBase, coldBase+coldCount) 是不可变区间，
// 分页读文件不再阻塞 ingest。
func (store *Store) captureLive(reqCols int) liveView {
	finish := perftrace.Measure("core.linehist.capture_live")
	projectedRows := 0
	defer func() { finish(projectedRows) }()
	unlock := store.lockGate()
	coldBase, coldCount := store.engine.VisibleLineRange()
	openTail := store.engine.OpenTail()
	var snap ScreenSnapshot
	store.mu.Lock()
	screen := store.screen
	generation := store.generation
	store.mu.Unlock()
	if screen != nil {
		snap = screen()
	}
	unlock()
	cols := reqCols
	if cols <= 0 {
		cols = snap.Cols
	}
	if cols <= 0 {
		cols = 80
	}
	hot := hotRowsFromScreen(coldCount, openTail, snap)
	projectedRows = coldCount + len(hot)
	perftrace.Count("core.linehist.capture_live.cold_lines", coldCount)
	perftrace.Count("core.linehist.capture_live.hot_rows", len(hot))
	return liveView{
		coldBase:   coldBase,
		coldCount:  coldCount,
		cols:       cols,
		hot:        hot,
		generation: generation,
	}
}

// hotRowsFromScreen 把未闭合尾部与当前屏拼成热段 logical-line source rows。
// primary：openTail 与屏幕行按 Wrapped 标志拼成 logical line。alt：先投影
// primary 时间线尾部——
// openTail 与被 alt 覆盖但仍未滚出的主屏保存行（snap.PrimaryRows）拼成
// mutable logical line（alt 退出后程序仍可改写它们，不能 seal）——
// 再把 alt 屏幕行按 fixed grid 原样投影。
func hotRowsFromScreen(coldCount int, openTail []Run, snap ScreenSnapshot) []history.HistoryRow {
	var rows []history.HistoryRow
	nextID := history.LogicalLineID(coldCount + 1)
	primaryRows := snap.Rows
	if snap.InAlt {
		primaryRows = snap.PrimaryRows
	}
	var lines [][]history.Cell
	current := cellsFromRuns(openTail)
	haveCurrent := len(current) > 0
	for _, screenRow := range primaryRows {
		current = append(current, cellsFromVTermCells(screenRow.Cells)...)
		haveCurrent = true
		if !screenRow.Wrapped {
			lines = append(lines, current)
			current = nil
			haveCurrent = false
		}
	}
	if haveCurrent {
		lines = append(lines, current)
	}
	for _, lineCells := range lines {
		rows = appendLogicalLineRow(rows, lineCells, nextID, history.HistorySegmentCurrentPrimaryFrame, false)
		nextID++
	}
	if snap.InAlt {
		for r, screenRow := range snap.Rows {
			rows = append(rows, history.HistoryRow{
				Cells:        cellsFromVTermCells(screenRow.Cells),
				Kind:         history.LineKindAltScreenFrame,
				Segment:      history.HistorySegmentCurrentAltFrame,
				LineID:       nextID,
				FixedGrid:    true,
				ScreenCols:   snap.Cols,
				ScreenRow:    r,
				ScreenRowSet: true,
			})
			nextID++
		}
	}
	return rows
}

func appendLogicalLineRow(rows []history.HistoryRow, cells []history.Cell, id history.LogicalLineID, segment history.HistorySegment, committed bool) []history.HistoryRow {
	return append(rows, history.HistoryRow{
		Cells:     cells,
		Kind:      history.LineKindOrdinary,
		Segment:   segment,
		LineID:    id,
		Committed: committed,
	})
}

// viewForRequest 解析 live 或 frozen 视图，并把 request cols 归一化。
func (store *Store) viewForRequest(req history.HistoryWindowRequest) (history.HistoryWindowRequest, liveView, error) {
	req.TerminalID = store.terminalIDFor(req.TerminalID)
	if req.Token != "" {
		store.mu.Lock()
		frozen, ok := store.frozen[req.Token]
		store.mu.Unlock()
		if !ok {
			return req, liveView{}, history.ErrHistoryInvalidMutation
		}
		if req.Cols <= 0 {
			req.Cols = frozen.cols
		}
		// 中文说明：frozen token 的视图必须保留 freeze 时刻的 source rows；
		// token 存在期间冷段记录区间与热段 rows 都不随 repaint/clear 改变。
		return req, liveView{
			coldBase:   frozen.coldBase,
			coldCount:  frozen.coldCount,
			cols:       frozen.cols,
			hot:        cloneRows(frozen.hot),
			generation: frozen.generation,
		}, nil
	}
	view := store.captureLive(req.Cols)
	if req.Cols <= 0 {
		req.Cols = view.cols
	}
	return req, view, nil
}

func viewLogicalTotal(view liveView) int {
	return view.coldCount + len(view.hot)
}

// logicalRowsForLineRange 按 logical-line 坐标返回窗口 source rows。
// 中文说明：copy/history 的权威分页坐标是 logical line，不是当前 cols 下的
// 全局 visual row；这里最多读取请求命中的 cold line 区间，不能为了 latest
// 或 older 构建全量 projection prefix。
func (store *Store) logicalRowsForLineRange(view liveView, start int, end int) ([]history.HistoryRow, error) {
	total := viewLogicalTotal(view)
	start = clampInt(start, 0, total)
	end = clampInt(end, start, total)
	var rows []history.HistoryRow
	if start < view.coldCount {
		coldEnd := minInt(end, view.coldCount)
		coldPage, err := store.coldLogicalRowsForLineRange(view.coldBase, view.coldCount, start, coldEnd)
		if err != nil {
			return nil, err
		}
		rows = append(rows, coldPage...)
	}
	if end > view.coldCount {
		hotStart := maxInt(0, start-view.coldCount)
		hotEnd := minInt(len(view.hot), end-view.coldCount)
		rows = append(rows, cloneRows(view.hot[hotStart:hotEnd])...)
	}
	return rows, nil
}

func (store *Store) coldLogicalRowsForLineRange(coldBase int, coldCount int, start int, end int) ([]history.HistoryRow, error) {
	if end <= start {
		return nil, nil
	}
	start = clampInt(start, 0, coldCount)
	end = clampInt(end, start, coldCount)
	firstLine := coldBase + start
	lines, err := store.engine.Lines(firstLine, coldBase+end)
	if err != nil {
		return nil, err
	}
	rows := make([]history.HistoryRow, 0, len(lines))
	for lineOffset, line := range lines {
		id := history.LogicalLineID(start + lineOffset + 1)
		rows = append(rows, history.HistoryRow{
			Cells:     cellsFromRuns(line.Runs),
			Kind:      history.LineKindOrdinary,
			Segment:   history.HistorySegmentCommitted,
			LineID:    id,
			Committed: true,
			Wrapped:   !line.HardEnd,
		})
	}
	return rows, nil
}

func (store *Store) terminalIDFor(terminalID string) string {
	if terminalID != "" {
		return terminalID
	}
	if store != nil {
		return store.terminalID
	}
	return ""
}

// hotLineIDs 返回热段 mutable 行的 LineID（每条 logical line 一次）。
func hotLineIDs(rows []history.HistoryRow) []history.LogicalLineID {
	var ids []history.LogicalLineID
	for _, row := range rows {
		if row.Committed {
			continue
		}
		if len(ids) > 0 && ids[len(ids)-1] == row.LineID {
			continue
		}
		ids = append(ids, row.LineID)
	}
	return ids
}

func joinLines(texts []string) string {
	out := ""
	for i, text := range texts {
		if i > 0 {
			out += "\n"
		}
		out += text
	}
	return out
}
