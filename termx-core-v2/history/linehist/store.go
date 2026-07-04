package linehist

import (
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
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
// emulator 当前屏，查询时按请求 cols 投影。它不持有第二份屏幕模型；
// Apply(mutation batch) 是 no-op，写 truth 的唯一入口是 ApplyTransaction。
type Store struct {
	mu         sync.Mutex
	terminalID string
	engine     *Engine
	screen     func() ScreenSnapshot
	gate       sync.Locker
	generation history.Generation
	nextToken  uint64
	frozen     map[history.HistoryToken]*frozenView
	indexes    map[int]*coldRowIndex
}

// NewStore 创建 linehist store。screen/gate 由 Terminal 通过 Bind 注入。
func NewStore(terminalID string, engine *Engine) *Store {
	return &Store{
		terminalID: terminalID,
		engine:     engine,
		frozen:     make(map[history.HistoryToken]*frozenView),
		indexes:    make(map[int]*coldRowIndex),
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

// Close 落盘未闭合尾部并关闭文件（terminal remove/shutdown 边界）。
func (store *Store) Close() error {
	if store == nil {
		return nil
	}
	unlock := store.lockGate()
	defer unlock()
	engineErr := store.engine.Close()
	indexErr := store.closeColdIndexes()
	if engineErr != nil {
		return engineErr
	}
	return indexErr
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
	req, view, err := store.viewForRequest(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	total, err := store.viewTotal(view)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	limit := normalizedLimit(req.Limit)
	start := maxInt(0, total-limit)
	page, err := store.rowsForRange(view, start, total)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, view.generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[0], start, view.generation, req.Token, start > 0)
	}
	return buildWindow(req, page, total, history.HistoryWindowReplace, boundary, view.generation, start > 0), nil
}

// OlderWindow 按 cursor.BeforeRowIndex 向更旧方向 prepend 分页。
func (store *Store) OlderWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	req, view, err := store.viewForRequest(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	total, err := store.viewTotal(view)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	if !req.Cursor.Valid {
		boundary := history.HistoryBoundary{Cursor: history.HistoryCursor{Generation: view.generation, Token: req.Token}}
		return buildWindow(req, nil, total, history.HistoryWindowPrepend, boundary, view.generation, false), nil
	}
	limit := normalizedLimit(req.Limit)
	end := clampInt(req.Cursor.BeforeRowIndex, 0, total)
	start := maxInt(0, end-limit)
	page, err := store.rowsForRange(view, start, end)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, view.generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[0], start, view.generation, req.Token, start > 0)
	}
	return buildWindow(req, page, total, history.HistoryWindowPrepend, boundary, view.generation, start > 0), nil
}

// OldestWindow 从投影 head 返回 replace window（TUI copy mode `g` 跳转）。
func (store *Store) OldestWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	req, view, err := store.viewForRequest(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	total, err := store.viewTotal(view)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	limit := normalizedLimit(req.Limit)
	end := minInt(total, limit)
	page, err := store.rowsForRange(view, 0, end)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	annotateProjectionRowIndexes(page, 0)
	boundary := boundaryForRows(page, view.generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[len(page)-1], end, view.generation, req.Token, end < total)
	}
	return buildWindow(req, page, total, history.HistoryWindowReplace, boundary, view.generation, end < total), nil
}

// NewerWindow 按 cursor.BeforeRowIndex 向更新方向 append 分页。
func (store *Store) NewerWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	req, view, err := store.viewForRequest(req)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	total, err := store.viewTotal(view)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	if !req.Cursor.Valid {
		boundary := history.HistoryBoundary{Cursor: history.HistoryCursor{Generation: view.generation, Token: req.Token}}
		return buildWindow(req, nil, total, history.HistoryWindowAppend, boundary, view.generation, false), nil
	}
	limit := normalizedLimit(req.Limit)
	start := clampInt(req.Cursor.BeforeRowIndex, 0, total)
	end := minInt(total, start+limit)
	page, err := store.rowsForRange(view, start, end)
	if err != nil {
		return history.HistoryWindow{}, err
	}
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, view.generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[len(page)-1], end, view.generation, req.Token, end < total)
	}
	return buildWindow(req, page, total, history.HistoryWindowAppend, boundary, view.generation, end < total), nil
}

// Freeze 记录当前投影边界。冷段是不可变文件前缀（记 coldCount 即可），
// 热段 materialize 成 rows；后续 repaint/eviction 不影响 token 视图。
func (store *Store) Freeze(req history.FreezeHistoryRequest) (history.FrozenHistorySnapshot, error) {
	if store == nil {
		return history.FrozenHistorySnapshot{}, nil
	}
	view := store.captureLive(req.Cols)
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
	_, view, err := store.viewForRequest(history.HistoryWindowRequest{
		TerminalID: req.TerminalID,
		Cols:       req.Cols,
		Token:      req.Token,
	})
	if err != nil {
		return "", err
	}
	total, err := store.viewTotal(view)
	if err != nil {
		return "", err
	}
	if total == 0 {
		return "", nil
	}
	startRow := 0
	endRow := total - 1
	if req.Start.Valid || req.End.Valid {
		startRow = store.firstRowOfLine(view, req.Start.LineID, 0)
		endRow = store.firstRowOfLine(view, req.End.LineID, total-1)
		if startRow > endRow {
			startRow, endRow = endRow, startRow
		}
	}
	rows, err := store.rowsForRange(view, startRow, endRow+1)
	if err != nil {
		return "", err
	}
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, rowText(row.Cells))
	}
	return joinLines(texts), nil
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
	gate.Lock()
	return gate.Unlock
}

// captureLive 在 gate 内采集一致性视图：可见冷段区间、未闭合尾部与当前屏。
// gate 释放后冷段记录 [coldBase, coldBase+coldCount) 是不可变区间，
// 分页读文件不再阻塞 ingest。
func (store *Store) captureLive(reqCols int) liveView {
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
	return liveView{
		coldBase:   coldBase,
		coldCount:  coldCount,
		cols:       cols,
		hot:        hotRowsFromScreen(coldCount, openTail, snap, cols),
		generation: generation,
	}
}

// hotRowsFromScreen 把未闭合尾部与当前屏拼成热段 rows。
// primary：openTail 与屏幕行按 Wrapped 标志拼成 logical line 后按 cols
// 重新换行（ordinary、mutable）。alt：先投影 primary 时间线尾部——
// openTail 与被 alt 覆盖但仍未滚出的主屏保存行（snap.PrimaryRows）拼成
// mutable logical line（alt 退出后程序仍可改写它们，不能 seal）——
// 再把 alt 屏幕行按 fixed grid 原样投影。
func hotRowsFromScreen(coldCount int, openTail []Run, snap ScreenSnapshot, cols int) []history.HistoryRow {
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
		rows = appendWrappedLineRows(rows, lineCells, cols, nextID, history.HistorySegmentCurrentPrimaryFrame, false)
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

func appendWrappedLineRows(rows []history.HistoryRow, cells []history.Cell, cols int, id history.LogicalLineID, segment history.HistorySegment, committed bool) []history.HistoryRow {
	wrapped := wrapCells(cells, cols)
	for i, rowCells := range wrapped {
		rows = append(rows, history.HistoryRow{
			Cells:     rowCells,
			Kind:      history.LineKindOrdinary,
			Segment:   segment,
			LineID:    id,
			RowInLine: i,
			Committed: committed,
			Wrapped:   i < len(wrapped)-1,
		})
	}
	return rows
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
		// 中文说明：frozen token 的视图必须按 freeze 时刻的 cols 投影；
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

// viewTotal 返回视图的 row 总数（视图冷段持久 row-count 索引区间 + 热段行数）。
func (store *Store) viewTotal(view liveView) (int, error) {
	coldRows, err := store.coldRowsTotal(view.cols, view.coldBase, view.coldCount)
	if err != nil {
		return 0, err
	}
	return coldRows + len(view.hot), nil
}

func (store *Store) coldRowsTotal(cols int, coldBase int, coldCount int) (int, error) {
	idx, err := store.ensureColdIndex(cols, coldBase+coldCount)
	if err != nil {
		return 0, err
	}
	return idx.rowsBetween(coldBase, coldBase+coldCount)
}

// ensureColdIndex 惰性构建/延伸某个 cols 的冷段 row 索引。
// 冷段 append-only：已有 row-count sidecar 永不改写正文，只向后补新记录
// 的派生行数。Store 只保护 map 生命周期；具体文件与 block prefix 由
// coldRowIndex 自己串行化，避免建索引时形成 Store.mu -> Engine.mu 的
// 反向锁顺序。
func (store *Store) ensureColdIndex(cols int, atLeast int) (*coldRowIndex, error) {
	if store == nil || store.engine == nil {
		return nil, fmt.Errorf("linehist: cold index store unavailable")
	}
	store.mu.Lock()
	idx := store.indexes[cols]
	store.mu.Unlock()
	if idx == nil {
		path := store.engine.RowIndexPath(cols)
		if path == "" {
			return nil, fmt.Errorf("linehist: row index path unavailable")
		}
		lineCount := store.engine.LineCount()
		if lineCount < atLeast {
			lineCount = atLeast
		}
		created, err := openColdRowIndex(path, cols, lineCount)
		if err != nil {
			return nil, err
		}
		store.mu.Lock()
		if existing := store.indexes[cols]; existing != nil {
			idx = existing
			store.mu.Unlock()
			_ = created.close()
		} else {
			idx = created
			store.indexes[cols] = idx
			store.mu.Unlock()
		}
	}
	if err := idx.ensure(store.engine, atLeast); err != nil {
		return nil, err
	}
	return idx, nil
}

func (store *Store) closeColdIndexes() error {
	store.mu.Lock()
	indexes := store.indexes
	store.indexes = make(map[int]*coldRowIndex)
	store.mu.Unlock()
	var firstErr error
	for _, idx := range indexes {
		if err := idx.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// rowsForRange 返回视图内 row 区间 [start,end) 的投影 rows（视图域：
// 0 = 视图冷段第一行）。冷段用持久 row-count sidecar 定位记录区间，
// 只读命中的正文 payload。
func (store *Store) rowsForRange(view liveView, start int, end int) ([]history.HistoryRow, error) {
	coldRows, err := store.coldRowsTotal(view.cols, view.coldBase, view.coldCount)
	if err != nil {
		return nil, err
	}
	total := coldRows + len(view.hot)
	start = clampInt(start, 0, total)
	end = clampInt(end, start, total)
	var rows []history.HistoryRow
	if start < coldRows {
		coldEnd := minInt(end, coldRows)
		coldPage, err := store.coldRowsForRange(view.cols, view.coldBase, view.coldCount, start, coldEnd)
		if err != nil {
			return nil, err
		}
		rows = append(rows, coldPage...)
	}
	if end > coldRows {
		hotStart := maxInt(0, start-coldRows)
		hotEnd := minInt(len(view.hot), end-coldRows)
		rows = append(rows, cloneRows(view.hot[hotStart:hotEnd])...)
	}
	return rows, nil
}

func (store *Store) coldRowsForRange(cols int, coldBase int, coldCount int, start int, end int) ([]history.HistoryRow, error) {
	if end <= start {
		return nil, nil
	}
	idx, err := store.ensureColdIndex(cols, coldBase+coldCount)
	if err != nil {
		return nil, err
	}
	// 中文说明：row sidecar/文件都在绝对域；视图域 row 加上 coldBase
	// 之前记录的 row 数即得绝对 row，再定位正文记录区间。LineID 仍按
	// authoritative logical-line 绝对顺序从 1 开始，ED3 不重置历史域。
	baseRows, err := idx.rowOffsetForLine(coldBase)
	if err != nil {
		return nil, err
	}
	absStart := start + baseRows
	absEnd := end + baseRows
	firstLine, err := idx.lineForRow(absStart)
	if err != nil {
		return nil, err
	}
	lastLine, err := idx.lineForRow(absEnd - 1)
	if err != nil {
		return nil, err
	}
	lineStartRow, err := idx.rowOffsetForLine(firstLine)
	if err != nil {
		return nil, err
	}
	lines, err := store.engine.Lines(firstLine, lastLine+1)
	if err != nil {
		return nil, err
	}
	var rows []history.HistoryRow
	rowIndex := lineStartRow
	for lineOffset, line := range lines {
		id := history.LogicalLineID(firstLine - coldBase + lineOffset + 1)
		wrapped := wrapCells(cellsFromRuns(line.Runs), cols)
		for i, rowCells := range wrapped {
			if rowIndex >= absEnd {
				break
			}
			if rowIndex >= absStart {
				rows = append(rows, history.HistoryRow{
					Cells:     rowCells,
					Kind:      history.LineKindOrdinary,
					Segment:   history.HistorySegmentCommitted,
					LineID:    id,
					RowInLine: i,
					Committed: true,
					Wrapped:   i < len(wrapped)-1 || !line.HardEnd,
				})
			}
			rowIndex++
		}
	}
	return rows, nil
}

// firstRowOfLine 返回某 LineID 首行的视图域 row index；未命中回退 fallback
// （与旧 rowsBetweenCursors 的 head/tail 回退语义一致）。
func (store *Store) firstRowOfLine(view liveView, id history.LogicalLineID, fallback int) int {
	if id == 0 {
		return fallback
	}
	if int(id) <= view.coldCount {
		idx, err := store.ensureColdIndex(view.cols, view.coldBase+view.coldCount)
		if err != nil {
			return fallback
		}
		absoluteRow, err := idx.rowOffsetForLine(view.coldBase + int(id) - 1)
		if err != nil {
			return fallback
		}
		baseRow, err := idx.rowOffsetForLine(view.coldBase)
		if err != nil {
			return fallback
		}
		return absoluteRow - baseRow
	}
	coldRows, err := store.coldRowsTotal(view.cols, view.coldBase, view.coldCount)
	if err != nil {
		return fallback
	}
	for i, row := range view.hot {
		if row.LineID == id {
			return coldRows + i
		}
	}
	return fallback
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
