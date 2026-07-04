package linehist

import (
	"fmt"
	"sort"
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
type ScreenSnapshot struct {
	Cols  int
	Rows  []ScreenRow
	InAlt bool
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
	return ScreenSnapshot{Cols: info.Cols, Rows: rows[:last+1], InAlt: info.IsAlternateScreen}
}

// frozenView 是 Freeze 时刻的投影边界。冷段是 append-only 文件的不可变
// 前缀，只需记录 coldCount；热段（当时的 mutable 屏幕行）必须 materialize，
// 因为后续 repaint 会改写屏幕。
type frozenView struct {
	coldCount  int
	cols       int
	hot        []history.HistoryRow
	generation history.Generation
}

// coldRowIndex 是某个 cols 下冷段记录的 rows-per-line prefix sum。
// prefix[i] = 前 i 条记录投影出的 row 总数；记录 i 覆盖绝对行
// [prefix[i], prefix[i+1])。冷段 append-only，索引只增量延伸。
type coldRowIndex struct {
	prefix []int
}

func (idx *coldRowIndex) covered() int {
	return len(idx.prefix) - 1
}

// liveView 是一次查询的一致性视图：coldCount/热段行在 ingest gate 内采集，
// 之后冷段 [0,coldCount) 是不可变前缀，可以在 gate 外分页读文件。
type liveView struct {
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

// ApplyTransaction 消费一次 tap 事务的 EvictedRows。调用方必须持有 gate
// （Terminal 在 tapOpMu 临界区内、ApplyPTYWrite 之后调用）；本方法不能
// 再锁 gate，否则自死锁。
func (store *Store) ApplyTransaction(tx vterm.TerminalSemanticTransaction) error {
	if store == nil {
		return nil
	}
	err := store.engine.ApplyEvictedRows(tx.EvictedRows)
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
	return history.HistoryReadState{
		Generation:  generation,
		HasTimeline: store.engine.LineCount() > 0,
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

// captureLive 在 gate 内采集一致性视图：冷段计数、未闭合尾部与当前屏。
// gate 释放后冷段 [0,coldCount) 是不可变前缀，分页读文件不再阻塞 ingest。
func (store *Store) captureLive(reqCols int) liveView {
	unlock := store.lockGate()
	coldCount := store.engine.LineCount()
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
		coldCount:  coldCount,
		cols:       cols,
		hot:        hotRowsFromScreen(coldCount, openTail, snap, cols),
		generation: generation,
	}
}

// hotRowsFromScreen 把未闭合尾部与当前屏拼成热段 rows。
// primary：openTail 与屏幕行按 Wrapped 标志拼成 logical line 后按 cols
// 重新换行（ordinary、mutable）。alt：屏幕行按 fixed grid 原样投影，
// openTail 单独成行（它属于 primary 时间线，不能混进 alt frame）。
func hotRowsFromScreen(coldCount int, openTail []Run, snap ScreenSnapshot, cols int) []history.HistoryRow {
	var rows []history.HistoryRow
	nextID := history.LogicalLineID(coldCount + 1)
	if snap.InAlt {
		if len(openTail) > 0 {
			rows = appendWrappedLineRows(rows, cellsFromRuns(openTail), cols, nextID, history.HistorySegmentCommitted, false)
			nextID++
		}
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
		return rows
	}
	var lines [][]history.Cell
	current := cellsFromRuns(openTail)
	haveCurrent := len(current) > 0
	for _, screenRow := range snap.Rows {
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
		// token 存在期间冷段前缀与热段 rows 都不随 repaint 改变。
		return req, liveView{
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

// viewTotal 返回视图的绝对 row 总数（冷段 prefix sum 尾值 + 热段行数）。
func (store *Store) viewTotal(view liveView) (int, error) {
	coldRows, err := store.coldRowsTotal(view.cols, view.coldCount)
	if err != nil {
		return 0, err
	}
	return coldRows + len(view.hot), nil
}

func (store *Store) coldRowsTotal(cols int, coldCount int) (int, error) {
	idx, err := store.ensureColdIndex(cols, coldCount)
	if err != nil {
		return 0, err
	}
	return idx.prefix[coldCount], nil
}

// ensureColdIndex 惰性构建/延伸某个 cols 的冷段 row 索引。
// 冷段 append-only：已有前缀永不失效，只向后补新记录的行数。
func (store *Store) ensureColdIndex(cols int, atLeast int) (*coldRowIndex, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	idx := store.indexes[cols]
	if idx == nil {
		idx = &coldRowIndex{prefix: []int{0}}
		store.indexes[cols] = idx
	}
	const batch = 1024
	for idx.covered() < atLeast {
		end := minInt(atLeast, idx.covered()+batch)
		lines, err := store.engine.Lines(idx.covered(), end)
		if err != nil {
			return nil, err
		}
		if len(lines) == 0 {
			return nil, fmt.Errorf("linehist: cold index out of range: covered=%d want=%d", idx.covered(), atLeast)
		}
		for _, line := range lines {
			idx.prefix = append(idx.prefix, idx.prefix[len(idx.prefix)-1]+countWrappedRows(line.Runs, cols))
		}
	}
	return idx, nil
}

// rowsForRange 返回视图内绝对 row 区间 [start,end) 的投影 rows。
// 冷段用 prefix sum 二分定位记录区间，只读命中的文件记录。
func (store *Store) rowsForRange(view liveView, start int, end int) ([]history.HistoryRow, error) {
	coldRows, err := store.coldRowsTotal(view.cols, view.coldCount)
	if err != nil {
		return nil, err
	}
	total := coldRows + len(view.hot)
	start = clampInt(start, 0, total)
	end = clampInt(end, start, total)
	var rows []history.HistoryRow
	if start < coldRows {
		coldEnd := minInt(end, coldRows)
		coldPage, err := store.coldRowsForRange(view.cols, view.coldCount, start, coldEnd)
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

func (store *Store) coldRowsForRange(cols int, coldCount int, start int, end int) ([]history.HistoryRow, error) {
	if end <= start {
		return nil, nil
	}
	idx, err := store.ensureColdIndex(cols, coldCount)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	prefix := idx.prefix[:coldCount+1]
	firstLine := sort.SearchInts(prefix, start+1) - 1
	lastLine := sort.SearchInts(prefix, end) - 1
	lineStartRow := prefix[firstLine]
	store.mu.Unlock()
	lines, err := store.engine.Lines(firstLine, lastLine+1)
	if err != nil {
		return nil, err
	}
	var rows []history.HistoryRow
	rowIndex := lineStartRow
	for lineOffset, line := range lines {
		id := history.LogicalLineID(firstLine + lineOffset + 1)
		wrapped := wrapCells(cellsFromRuns(line.Runs), cols)
		for i, rowCells := range wrapped {
			if rowIndex >= end {
				break
			}
			if rowIndex >= start {
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

// firstRowOfLine 返回某 LineID 首行的绝对 row index；未命中回退 fallback
// （与旧 rowsBetweenCursors 的 head/tail 回退语义一致）。
func (store *Store) firstRowOfLine(view liveView, id history.LogicalLineID, fallback int) int {
	if id == 0 {
		return fallback
	}
	if int(id) <= view.coldCount {
		idx, err := store.ensureColdIndex(view.cols, view.coldCount)
		if err != nil {
			return fallback
		}
		store.mu.Lock()
		row := idx.prefix[int(id)-1]
		store.mu.Unlock()
		return row
	}
	coldRows, err := store.coldRowsTotal(view.cols, view.coldCount)
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
