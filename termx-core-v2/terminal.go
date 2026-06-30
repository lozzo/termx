package termxcorev2

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	"github.com/lozzow/termx/termx-shared/perftrace"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

type Terminal struct {
	mu      sync.Mutex
	info    TerminalInfo
	options TerminalCreateOptions
	process TerminalProcess
	tap     *SemanticTap
	// 中文说明：tapOpMu 串行化生产 PTY 输出与 direct resize/restart tap 操作。
	// 它定义 single SemanticTap 的输入顺序边界，避免 resize 后的新输出抢先按旧尺寸进 tap。
	tapOpMu         sync.Mutex
	historyMu       sync.Mutex
	historyRenderer history.HistoryLogicalRenderer
	historyStore    history.HistoryStore
	historyEnabled  bool
	queueMu         sync.Mutex
	tapQ            *terminalLiveIngestQueue
	historyQ        *terminalHistoryIngestQueue
	events          *eventBroker
	update          func(TerminalInfo)
}

func newTerminal(info TerminalInfo, options TerminalCreateOptions, process TerminalProcess, events *eventBroker, update func(TerminalInfo), historyStore history.HistoryStore, historyEnabled bool) *Terminal {
	terminal := &Terminal{
		info:           info.Clone(),
		options:        cloneTerminalCreateOptions(options),
		process:        process,
		events:         events,
		update:         update,
		historyEnabled: historyEnabled,
	}
	terminal.tap = NewSemanticTap(info.ID, info.Size, terminal.handleLiveSurfaceResponse)
	if historyEnabled {
		terminal.historyRenderer = history.NewHistoryLogicalRenderer(nil, nil)
		if historyStore == nil {
			historyStore = history.NewInMemoryHistoryStore(info.ID)
		}
		terminal.historyStore = historyStore
	}
	terminal.watchProcess(process)
	return terminal
}

func (terminal *Terminal) Info() TerminalInfo {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.info.Clone()
}

func (terminal *Terminal) SetMetadata(name string, tags map[string]string) TerminalInfo {
	terminal.mu.Lock()
	terminal.info.Name = name
	terminal.info.Tags = cloneStringMap(tags)
	info := terminal.info.Clone()
	terminal.mu.Unlock()
	terminal.syncInfo(info)
	terminal.publish(EventTerminalMetadataChanged, info)
	return info
}

func (terminal *Terminal) Input(data []byte) error {
	terminal.mu.Lock()
	process := terminal.process
	if terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	terminal.mu.Unlock()
	return process.Input(data)
}

func (terminal *Terminal) handleLiveSurfaceResponse(data []byte) {
	if len(data) == 0 {
		return
	}
	terminal.mu.Lock()
	process := terminal.process
	state := terminal.info.State
	terminal.mu.Unlock()
	if process == nil || state == TerminalStateExited || state == TerminalStateRemoved {
		return
	}
	// 中文说明：OSC/DSR/DA 等终端查询的响应必须回写到当前 PTY，
	// 否则 Codex 这类 TUI 会误判颜色/能力并降级渲染。
	_ = process.Input(data)
}

// IngestOutput 是测试和诊断入口，用一段 PTY 输出同步推进 terminal。
// 它走同一个 SemanticTap owner 更新 native live screen 和 authoritative history，
// 不能再把 raw PTY 分别交给 live/history 两套 vterm 解释。
func (terminal *Terminal) IngestOutput(output string) error {
	terminal.mu.Lock()
	if terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	info := terminal.info.Clone()
	terminal.mu.Unlock()

	terminal.tapOpMu.Lock()
	result, err := terminal.tap.ApplyPTYWrite([]byte(output))
	if err == nil && terminal.historyEnabled {
		terminal.ingestHistoryTransactions([]history.TerminalSemanticTransaction{result.Transaction()})
	}
	terminal.tapOpMu.Unlock()
	if err != nil {
		return err
	}

	terminal.publishLiveInvalidated(info.ID, uint64(result.Revision()))
	return nil
}

// Resize 调整 terminal PTY 和 core native live screen 的尺寸。
// resize 和 PTY bytes 共享同一个 SemanticTap owner；history 只消费 tap resize
// transaction 作为 non-history boundary，不能由 resized snapshot 生成 sealed history。
func (terminal *Terminal) Resize(size Size) error {
	if !size.Valid() {
		return ErrInvalidServerSize
	}
	terminal.mu.Lock()
	process := terminal.process
	if terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	terminal.mu.Unlock()
	if err := terminal.flushTapQueue(context.Background()); err != nil {
		return err
	}

	terminal.tapOpMu.Lock()
	defer terminal.tapOpMu.Unlock()

	terminal.mu.Lock()
	if terminal.process != process || terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	terminal.mu.Unlock()
	if err := process.Resize(size); err != nil {
		return err
	}
	terminal.mu.Lock()
	if terminal.process != process || terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	oldSize := terminal.info.Size
	terminal.info.Size = size
	info := terminal.info.Clone()
	terminal.mu.Unlock()

	result, err := terminal.tap.Resize(size)
	if err != nil {
		return err
	}
	if terminal.historyEnabled {
		terminal.enqueueOrApplyHistoryResizeTransaction(result.Transaction())
	}

	terminal.syncInfo(info)
	terminal.publishResize(info, oldSize, size)
	terminal.publishLiveInvalidated(info.ID, uint64(result.Revision()))
	return nil
}

func (terminal *Terminal) Kill() error {
	terminal.mu.Lock()
	process := terminal.process
	terminal.mu.Unlock()
	return process.Kill()
}

func (terminal *Terminal) Close() error {
	return terminal.closeWithReason(history.CloseReasonTerminalRemove)
}

func (terminal *Terminal) closeWithReason(reason history.CloseReason) error {
	_ = terminal.FlushHistory(context.Background())
	terminal.mu.Lock()
	process := terminal.process
	shouldCloseHistory := terminal.info.State == TerminalStateRunning
	terminal.info.State = TerminalStateRemoved
	info := terminal.info.Clone()
	terminal.mu.Unlock()
	if shouldCloseHistory && terminal.historyEnabled {
		// 中文说明：remove/shutdown 不一定会经过 process-exit watcher；running terminal
		// 的最后 open line/current frame 必须用 lifecycle close reason 交给 history renderer。
		terminal.forceCloseHistory(reason)
	}
	terminal.syncInfo(info)
	if process == nil {
		return nil
	}
	return process.Close()
}

func (terminal *Terminal) Restart(ctx context.Context, factory ProcessFactory) error {
	terminal.mu.Lock()
	info := terminal.info.Clone()
	options := terminal.options
	terminal.mu.Unlock()
	if err := terminal.FlushHistory(ctx); err != nil {
		return err
	}
	// 中文说明：restart 生成的是 core 持有的新 terminal process，不能绑定到本次
	// protocol request/session 的 ctx；否则 TUI 退出关闭 socket 会把刚重启的 PTY 杀掉。
	process, err := factory.Spawn(context.Background(), processSpecFromTerminal(info, options))
	if err != nil {
		return err
	}
	terminal.mu.Lock()
	old := terminal.process
	oldInfo := terminal.info.Clone()
	terminal.process = process
	terminal.info.State = TerminalStateRunning
	terminal.info.ExitCode = nil
	terminal.info.ExitedAt = time.Time{}
	info = terminal.info.Clone()
	terminal.mu.Unlock()
	_ = oldInfo
	terminal.queueMu.Lock()
	terminal.tapQ = nil
	terminal.historyQ = nil
	terminal.queueMu.Unlock()
	terminal.tapOpMu.Lock()
	snapshot := terminal.tap.ResetForRestartPreservingScreen(info.Size)
	terminal.tapOpMu.Unlock()
	terminal.syncInfo(info)
	terminal.watchProcess(process)
	_ = old.Close()
	terminal.publishLifecycle(EventTerminalChanged, info)
	terminal.publishLiveInvalidated(info.ID, uint64(snapshot.Revision))
	return nil
}

func (terminal *Terminal) Wait() <-chan ProcessExit {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.process.Wait()
}

func (terminal *Terminal) LiveRows() []string {
	return terminalLiveRowsFromNativeSnapshot(terminal.tap.NativeScreenSnapshot())
}

func (terminal *Terminal) LiveSnapshot() live.SurfaceSnapshot {
	return terminalLiveSurfaceSnapshotFromNative(terminal.tap.NativeScreenSnapshot())
}

func (terminal *Terminal) VisitLiveTrimmedScreenRows(visit func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell)) vterm.TrimmedScreenRowsInfo {
	return terminalVisitNativeScreenSnapshot(terminal.tap.NativeScreenSnapshot(), visit)
}

// VisitLiveTrimmedScreenRowsWithRevision 在同一 live 锁内读取当前 native screen
// 与 live projection revision。revision 只表示当前屏投影版本，不是 history truth
// generation；protocol/TUI 用它拒绝旧 snapshot，不能把它解释成 logical history 版本。
func (terminal *Terminal) VisitLiveTrimmedScreenRowsWithRevision(visit func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell)) (vterm.TrimmedScreenRowsInfo, uint64) {
	snapshot := terminal.tap.NativeScreenSnapshot()
	info := terminalVisitNativeScreenSnapshot(snapshot, visit)
	return info, uint64(snapshot.Revision)
}

// NativeScreenSnapshot 返回 core 当前 latest native screen。
// 调用方只能把它用于实时显示 projection；history/window/copy truth 必须继续走 HistoryWindow/Copy。
func (terminal *Terminal) NativeScreenSnapshot(terminalID string) NativeScreenSnapshot {
	snapshot := terminal.tap.NativeScreenSnapshot()
	snapshot.TerminalID = terminalID
	return snapshot
}

// LiveRevision 返回当前 terminal native screen 的 latest-only revision。
// 它只服务 live invalidation one-shot arm 的“是否已经有新屏幕”判断；
// history/window/copy 不能把它当成 logical history generation。
func (terminal *Terminal) LiveRevision() LiveRevision {
	return terminal.tap.LiveRevision()
}

// FlushHistory 等待当前 terminal 的 tap queue 与 history transaction worker 追平。
// 第一段 fence 保证已进入 PTY 输出队列的 bytes 先推进 single SemanticTap；第二段
// fence 再等待 tap 产出的 semantic transaction 落到 authoritative history store。
// 它不等待客户端 render；调用边界是 history.window/freeze/copy 和 lifecycle close。
func (terminal *Terminal) FlushHistory(ctx context.Context) error {
	if err := terminal.flushTapQueue(ctx); err != nil {
		return err
	}
	terminal.queueMu.Lock()
	queue := terminal.historyQ
	terminal.queueMu.Unlock()
	if queue == nil {
		return nil
	}
	// 中文说明：history/copy/freeze 只等待 core 内部 tap/history 队列追平；
	// 不等待 TUI render 或 protocol snapshot ack，避免把客户端速度变成 history truth。
	return queue.Flush(ctx)
}

func (terminal *Terminal) publish(typ EventType, info TerminalInfo) {
	terminal.publishEvent(typ, info, false)
}

func (terminal *Terminal) publishLifecycle(typ EventType, info TerminalInfo) {
	terminal.publishEvent(typ, info, true)
}

func (terminal *Terminal) publishEvent(typ EventType, info TerminalInfo, lifecycleKnown bool) {
	if terminal.events == nil {
		return
	}
	terminalCopy := info.Clone()
	terminal.events.publish(Event{
		Type:           typ,
		TerminalID:     info.ID,
		Terminal:       &terminalCopy,
		LifecycleKnown: lifecycleKnown,
	})
}

func (terminal *Terminal) publishLiveInvalidated(terminalID string, revision uint64) {
	if terminal.events == nil {
		return
	}
	terminal.events.publish(Event{
		Type:       EventTerminalLiveInvalidated,
		TerminalID: terminalID,
		Live: &LiveScreenInvalidated{
			TerminalID: terminalID,
			Revision:   LiveRevision(revision),
		},
	})
}

func (terminal *Terminal) publishResize(info TerminalInfo, oldSize Size, newSize Size) {
	if terminal.events == nil {
		return
	}
	terminalCopy := info.Clone()
	terminal.events.publish(Event{
		Type:       EventTerminalResized,
		TerminalID: info.ID,
		Terminal:   &terminalCopy,
		OldSize:    oldSize,
		NewSize:    newSize,
	})
}

func (terminal *Terminal) watchProcess(process TerminalProcess) {
	outputDone := terminal.watchOutput(process)
	terminal.watchExit(process, outputDone)
}

func (terminal *Terminal) watchOutput(process TerminalProcess) <-chan struct{} {
	done := make(chan struct{})
	output := process.Output()
	if output == nil {
		close(done)
		return done
	}
	tapQueue := newTerminalLiveIngestQueue()
	terminal.setTapQueue(process, tapQueue)
	var historyWorker *terminalHistoryIngestQueue
	if terminal.historyEnabled {
		historyWorker = newTerminalHistoryIngestQueue()
		terminal.setHistoryQueue(process, historyWorker)
		go historyWorker.Run(func(txs []history.TerminalSemanticTransaction) error {
			return terminal.ingestProcessHistoryTransactions(process, txs)
		})
	}
	go tapQueue.Run(func(output string) error {
		return terminal.ingestProcessTapOutput(process, output, historyWorker)
	})
	go func() {
		defer close(done)
		defer func() {
			tapQueue.Close()
			tapQueue.Wait()
			terminal.clearTapQueue(process, tapQueue)
			if historyWorker != nil {
				historyWorker.Close()
				historyWorker.Wait()
				terminal.clearHistoryQueue(process, historyWorker)
			}
		}()
		for chunk := range output {
			if len(chunk) == 0 {
				continue
			}
			text := string(chunk)
			tapQueue.Enqueue(text)
		}
	}()
	return done
}

func (terminal *Terminal) setTapQueue(process TerminalProcess, queue *terminalLiveIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	terminal.tapQ = queue
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) clearTapQueue(process TerminalProcess, queue *terminalLiveIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	if terminal.tapQ == queue {
		terminal.tapQ = nil
	}
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) flushTapQueue(ctx context.Context) error {
	terminal.queueMu.Lock()
	queue := terminal.tapQ
	terminal.queueMu.Unlock()
	if queue == nil {
		return nil
	}
	return queue.Flush(ctx)
}

func (terminal *Terminal) setHistoryQueue(process TerminalProcess, queue *terminalHistoryIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	terminal.historyQ = queue
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) clearHistoryQueue(process TerminalProcess, queue *terminalHistoryIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	if terminal.historyQ == queue {
		terminal.historyQ = nil
	}
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) enqueueOrApplyHistoryResizeTransaction(tx history.TerminalSemanticTransaction) {
	terminal.queueMu.Lock()
	queue := terminal.historyQ
	terminal.queueMu.Unlock()
	if queue != nil && queue.Enqueue(tx) {
		return
	}
	// 中文说明：没有异步 history worker 时，resize 仍只按 boundary-only 进入 renderer；
	// 不能从 resized live snapshot 回填 committed history，也不能等待未来输出兜底。
	terminal.applyHistoryResizeTransaction(tx)
}

func (terminal *Terminal) watchExit(process TerminalProcess, outputDone <-chan struct{}) {
	go func() {
		exit, ok := <-process.Wait()
		if !ok {
			return
		}
		if outputDone != nil {
			<-outputDone
		}
		terminal.markExited(process, exit)
	}()
}

func (terminal *Terminal) ingestProcessTapOutput(process TerminalProcess, output string, historyWorker *terminalHistoryIngestQueue) error {
	terminal.mu.Lock()
	if terminal.process != process {
		terminal.mu.Unlock()
		return nil
	}
	if terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	info := terminal.info.Clone()
	terminal.mu.Unlock()

	terminal.tapOpMu.Lock()
	finishLiveWrite := perftrace.Measure("core.live.write_screen")
	result, err := terminal.tap.ApplyPTYWrite([]byte(output))
	finishLiveWrite(len(output))
	if err == nil && historyWorker != nil {
		// 中文说明：history backlog 的单位是 tap 产出的 semantic transaction；
		// 这里不能把 raw PTY bytes 交给第二个 vterm replay。
		historyWorker.Enqueue(result.Transaction())
	}
	terminal.tapOpMu.Unlock()
	if err != nil {
		return err
	}

	terminal.mu.Lock()
	stillCurrent := terminal.process == process && terminal.info.State != TerminalStateExited && terminal.info.State != TerminalStateRemoved
	terminal.mu.Unlock()
	if !stillCurrent {
		return nil
	}
	perftrace.Count("core.terminal.changed", len(output))
	perftrace.Count("core.live.invalidation_publish", len(output))
	terminal.publishLiveInvalidated(info.ID, uint64(result.Revision()))
	return nil
}

func (terminal *Terminal) markExited(process TerminalProcess, exit ProcessExit) {
	terminal.mu.Lock()
	if terminal.process != process || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return
	}
	terminal.info.State = TerminalStateExited
	code := exit.Code
	terminal.info.ExitCode = &code
	// 退出时间以 core-v2 完成输出收口并标记生命周期的 UTC 时刻为准。
	terminal.info.ExitedAt = time.Now().UTC()
	info := terminal.info.Clone()
	terminal.mu.Unlock()
	if terminal.historyEnabled {
		terminal.forceCloseHistory(history.CloseReasonProcessExit)
	}
	terminal.appendExitMarker(info)
	terminal.syncInfo(info)
	terminal.publish(EventTerminalExited, info)
}

func (terminal *Terminal) appendExitMarker(info TerminalInfo) {
	lines := terminalExitMarkerLines(info)
	if len(lines) == 0 {
		return
	}
	text := "\r\n" + strings.Join(lines, "\r\n") + "\r\n"
	terminal.tapOpMu.Lock()
	result, err := terminal.tap.ApplyPTYWrite([]byte(text))
	terminal.tapOpMu.Unlock()
	if err != nil {
		return
	}
	terminal.publishLiveInvalidated(info.ID, uint64(result.Revision()))
}

func terminalExitMarkerLines(info TerminalInfo) []string {
	if info.ID == "" {
		return nil
	}
	head := "terminal exited: " + info.ID
	if info.ExitCode != nil {
		head += fmt.Sprintf(" code:%d", *info.ExitCode)
	}
	head += " exited"
	lines := []string{head}
	if !info.ExitedAt.IsZero() {
		lines = append(lines, "exited at: "+info.ExitedAt.UTC().Format(time.RFC3339))
	}
	if len(info.Command) > 0 {
		lines = append(lines, "command: "+strings.Join(info.Command, " "))
	}
	return lines
}

func terminalLiveRowsFromNativeSnapshot(snapshot NativeScreenSnapshot) []string {
	if len(snapshot.Rows) == 0 {
		return nil
	}
	out := make([]string, len(snapshot.Rows))
	for index, row := range snapshot.Rows {
		out[index] = strings.TrimRight(terminalVTermRowText(row.Cells), " ")
	}
	return terminalTrimTrailingEmptyRows(out)
}

func terminalLiveSurfaceSnapshotFromNative(snapshot NativeScreenSnapshot) live.SurfaceSnapshot {
	rows := make([][]vterm.Cell, len(snapshot.Rows))
	for index, row := range snapshot.Rows {
		rows[index] = cloneSemanticTapCells(row.Cells)
	}
	return live.SurfaceSnapshot{
		Size:   live.SurfaceSize{Cols: snapshot.Size.Cols, Rows: snapshot.Size.Rows},
		Screen: vterm.ScreenData{Cells: rows, IsAlternateScreen: snapshot.AltScreen},
		Cursor: snapshot.Cursor,
		Modes:  snapshot.Modes,
	}
}

func terminalVisitNativeScreenSnapshot(snapshot NativeScreenSnapshot, visit func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell)) vterm.TrimmedScreenRowsInfo {
	if visit != nil {
		for _, row := range snapshot.Rows {
			rowIndex := row.Index
			cells := cloneSemanticTapCells(row.Cells)
			visit(rowIndex, len(cells), func(index int) vterm.Cell {
				if index < 0 || index >= len(cells) {
					return vterm.Cell{}
				}
				return cells[index]
			})
		}
	}
	return vterm.TrimmedScreenRowsInfo{
		Cols:              snapshot.Size.Cols,
		Rows:              snapshot.Size.Rows,
		IsAlternateScreen: snapshot.AltScreen,
		Cursor:            snapshot.Cursor,
		Modes:             snapshot.Modes,
	}
}

func terminalVTermRowText(row []vterm.Cell) string {
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}

func terminalTrimTrailingEmptyRows(rows []string) []string {
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func (terminal *Terminal) HistoryWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled {
		return history.HistoryWindow{}, ErrHistoryDisabled
	}
	if terminal.historyStore == nil {
		return history.HistoryWindow{}, ErrHistoryNotRebuilt
	}
	switch req.Mode {
	case "", history.HistoryWindowModeLatest:
		return terminal.historyStore.LatestWindow(req)
	case history.HistoryWindowModeOlder:
		return terminal.historyStore.OlderWindow(req)
	case history.HistoryWindowModeOldest:
		return terminal.historyStore.OldestWindow(req)
	case history.HistoryWindowModeNewer:
		return terminal.historyStore.NewerWindow(req)
	default:
		return history.HistoryWindow{}, history.ErrHistoryUnsupportedWindowMode
	}
}

func (terminal *Terminal) HistoryCopy(req history.HistoryCopyRequest) (string, error) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled {
		return "", ErrHistoryDisabled
	}
	if terminal.historyStore == nil {
		return "", ErrHistoryNotRebuilt
	}
	return terminal.historyStore.Copy(req)
}

func (terminal *Terminal) HistoryFreeze(req history.FreezeHistoryRequest) (history.FrozenHistorySnapshot, error) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled {
		return history.FrozenHistorySnapshot{}, ErrHistoryDisabled
	}
	if terminal.historyStore == nil {
		return history.FrozenHistorySnapshot{}, ErrHistoryNotRebuilt
	}
	return terminal.historyStore.Freeze(req)
}

// HistoryRelease 释放 terminal history store 中的 core-owned token。
func (terminal *Terminal) HistoryRelease(token history.HistoryToken) error {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled {
		return ErrHistoryDisabled
	}
	if terminal.historyStore == nil {
		return ErrHistoryNotRebuilt
	}
	return terminal.historyStore.Release(token)
}

func (terminal *Terminal) ingestProcessHistoryTransactions(process TerminalProcess, txs []history.TerminalSemanticTransaction) error {
	terminal.mu.Lock()
	if terminal.process != process {
		terminal.mu.Unlock()
		return nil
	}
	if terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	terminal.mu.Unlock()
	terminal.ingestHistoryTransactions(txs)
	return nil
}

func (terminal *Terminal) ingestHistoryTransactions(txs []history.TerminalSemanticTransaction) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled || terminal.historyRenderer == nil || terminal.historyStore == nil {
		return
	}
	for _, tx := range txs {
		terminal.applyHistoryTransactionLocked(tx)
	}
}

func (terminal *Terminal) applyHistoryResizeTransaction(tx history.TerminalSemanticTransaction) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled || terminal.historyRenderer == nil || terminal.historyStore == nil {
		return
	}
	// 中文说明：resize 只作为 semantic non-history boundary 进入 renderer，不能从
	// resized live snapshot 生成 sealed history，也不能重写 sealed records。
	batch, err := terminal.historyRenderer.Apply(tx, history.HistoryDecision{
		Mode:               history.HistoryOutputModeBoundaryOnly,
		NonHistoryBoundary: true,
	})
	if err != nil {
		return
	}
	_ = terminal.historyStore.Apply(batch)
}

func (terminal *Terminal) applyHistoryTransactionLocked(tx history.TerminalSemanticTransaction) {
	decision := terminal.historyDecisionForTransaction(tx, terminal.historyStore.ReadState())
	batch, err := terminal.historyRenderer.Apply(tx, decision)
	if err != nil {
		return
	}
	_ = terminal.historyStore.Apply(batch)
}

func (terminal *Terminal) forceCloseHistory(reason history.CloseReason) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled || terminal.historyRenderer == nil || terminal.historyStore == nil {
		return
	}
	// 中文说明：process exit 是 lifecycle boundary，只能走 renderer.Close；不能伪造成
	// PTY bytes，也不能 fallback 到 live snapshot。
	batch, err := terminal.historyRenderer.Close(reason)
	if err != nil {
		return
	}
	_ = terminal.historyStore.Apply(batch)
}

func (terminal *Terminal) historyDecisionForTransaction(tx history.TerminalSemanticTransaction, state history.HistoryReadState) history.HistoryDecision {
	decision := history.HistoryDecision{Mode: history.HistoryOutputModeOrdinaryStream}
	hasEraseDisplay := historyTransactionHasEraseDisplay(tx)
	hasSynchronizedContent := historyTransactionHasSynchronizedContent(tx)
	hasContentAfterSyncEnd := historyTransactionHasContentAfterSyncEnd(tx)
	// 中文说明：single SemanticTap 后 begin/payload/end 可能同属一个 transaction；
	// 只要本 transaction 进入或处在 synchronized output mode，就仍是 primary
	// screen app repaint。只有已有 current frame 后，sync end 后紧跟普通 prompt 的
	// 同包输出才关闭 session，不能把 prompt 后的整屏再次发布为 current frame。
	syncFrameSession := hasSynchronizedContent
	syncEndThenOrdinaryStream := tx.SynchronizedEnd && state.HasPrimaryCurrent && hasContentAfterSyncEnd
	// 中文说明：RequiresFullReplace 只是 vterm/live stale 边界；只有没有 current
	// primary owner 且缺少 ordered content ops 时，PrimaryFrame side proof 才能作为
	// 初始 screen redraw 进入 frame reducer。若已有 sealed timeline，full-replace
	// snapshot 必须带本 transaction 的 touch proof，否则 shell prompt 这类普通输出
	// 会被最终整屏 frame 又发布一次，和已 sealed 的尾屏形成重复历史。
	fullReplaceFrameOnly := tx.RequiresFullReplace && !state.HasPrimaryCurrent && !historyTransactionHasOrderedContentOps(tx)
	fullReplaceTouchedRowsOnly := fullReplaceFrameOnly && state.HasTimeline && len(tx.PrimaryFrameTouchedRows) > 0
	fullReplaceCanPublish := fullReplaceFrameOnly && (!state.HasTimeline || fullReplaceTouchedRowsOnly)
	isPrimaryFrameSession := (syncFrameSession && !syncEndThenOrdinaryStream) || fullReplaceCanPublish || hasEraseDisplay
	if tx.RequiresFullReplace && tx.FullReplaceReason == "resize" {
		return history.HistoryDecision{Mode: history.HistoryOutputModeBoundaryOnly, NonHistoryBoundary: true}
	}
	if tx.AltEntered && !isPrimaryFrameSession {
		decision.ArchivePrimaryBeforeAlt = true
		decision.ClearPrimaryCurrent = true
	}
	if tx.AltExited && !isPrimaryFrameSession {
		decision.ClearAltFrame = true
	}
	if (tx.AltEntered || tx.AltExited || tx.AltFrame != nil) && !isPrimaryFrameSession {
		decision.Mode = history.HistoryOutputModeAltTransient
		decision.PublishAltFrame = tx.AltFrame != nil
	}
	if tx.PrimaryFrame != nil && isPrimaryFrameSession {
		decision.Mode = history.HistoryOutputModePrimaryFrameSession
		decision.PublishPrimaryFrame = true
		decision.ClosePrimaryFrameBeforePrimaryReplace = state.HasPrimaryCurrent && !hasEraseDisplay && (tx.SynchronizedBegin || tx.AltEntered) && !syncEndThenOrdinaryStream
		decision.ArchivePrimaryAfterPrimaryFrame = tx.AltEntered
		// 中文说明：同步输出或 full-replace direct damage 刚启动时，vterm
		// PrimaryFrame side proof 会包含屏幕上已经 sealed 的普通 shell tail。
		// 没有 clear 边界时，只允许本 transaction 触达的 rows 进入 current
		// frame，不能把旧 shell 屏幕复刻成第二份 screen app history truth。
		decision.PublishPrimaryFrameTouchedRowsOnly = !hasEraseDisplay && len(tx.PrimaryScrollOut) == 0 && (syncFrameSession || fullReplaceTouchedRowsOnly)
		// 中文说明：vterm 已经证明真正滚出 primary viewport 的 payload 必须进入
		// authoritative history；ED2 clear-time proof 只有在已有 primary current
		// ownership 时才消费，renderer 不能靠更早的 scroll-out 状态补 seal。
		decision.ConsumeScrollOutProof = historyTransactionShouldConsumeScrollOut(tx, state)
		// 中文说明：ED2 不等于 ED3。若清屏前有 primary current frame，
		// vterm 的 clear-time scroll-out proof 表示该 frame 真实离开 viewport，
		// 应进入 scrollable history；普通 shell 已 sealed 可见行仍不能消费该
		// proof，否则会复制已经在 timeline 中的 shell tail。
		decision.ConsumeClearTimeScrollOutProof = hasEraseDisplay && state.HasPrimaryCurrent
		decision.ConsumeClearBoundary = hasEraseDisplay
	}
	if decision.Mode == history.HistoryOutputModeOrdinaryStream && state.HasPrimaryCurrent && (historyTransactionHasContentMutation(tx) || tx.RequiresFullReplace) {
		// 中文说明：screen app session 后恢复真正的普通输出，才是 terminal
		// 语义上的 session 边界；纯 synchronized begin/end 这类 mode 边界没有
		// payload，不能把 current frame close 进 committed history。full replace
		// stale 边界不能发布最终整屏，但必须关闭旧 current ownership，避免旧尾屏
		// 在 latest window 中继续作为 mutable frame 重复出现。
		decision.ClosePrimaryFrameBeforeStream = true
	}
	return decision
}

func historyTransactionHasEraseDisplay(tx history.TerminalSemanticTransaction) bool {
	for _, op := range tx.Ops {
		if op.Code == vterm.ScreenOpControl && op.Control == "ed" && op.Mode == 2 {
			return true
		}
	}
	return false
}

func historyTransactionHasContentMutation(tx history.TerminalSemanticTransaction) bool {
	for _, op := range tx.Ops {
		if historyOpMutatesContent(op) {
			return true
		}
	}
	return len(tx.PrimaryScrollOut) > 0 || historyTransactionHasOpScrollOut(tx)
}

func historyTransactionHasOrderedContentOps(tx history.TerminalSemanticTransaction) bool {
	for _, op := range tx.Ops {
		if historyOpMutatesContent(op) {
			return true
		}
	}
	return false
}

func historyTransactionHasContentAfterSyncEnd(tx history.TerminalSemanticTransaction) bool {
	afterSyncEnd := false
	inAltAfterSyncEnd := false
	for _, op := range tx.Ops {
		if op.Code == vterm.ScreenOpModes && op.Private && op.Mode == 2026 && !op.Enabled {
			afterSyncEnd = true
			continue
		}
		if afterSyncEnd && op.Code == vterm.ScreenOpModes && op.Private && (op.Mode == 47 || op.Mode == 1047 || op.Mode == 1049) {
			inAltAfterSyncEnd = op.Enabled
			continue
		}
		if afterSyncEnd && !inAltAfterSyncEnd && historyOpMutatesStreamContent(op) {
			return true
		}
	}
	return false
}

func historyTransactionHasSynchronizedContent(tx history.TerminalSemanticTransaction) bool {
	// 中文说明：2026 begin 前的 shell 输出仍属于 ordinary stream；不能因为同一
	// tap transaction 后半段进入 synchronized mode，就把 begin 前内容接管成
	// primary frame。split end transaction 在 mode disable 前默认处于 sync scope。
	if tx.SynchronizedEnd && !historyTransactionHasContentAfterSyncEnd(tx) && (len(tx.PrimaryScrollOut) > 0 || len(tx.PrimaryFrameTouchedRows) > 0 || tx.PrimaryFrame != nil) {
		return true
	}
	inSyncScope := tx.SynchronizedEnd && !tx.SynchronizedBegin
	if tx.SynchronizedActive && !tx.SynchronizedBegin {
		inSyncScope = true
	}
	for _, op := range tx.Ops {
		if op.Code == vterm.ScreenOpModes && op.Private && op.Mode == 2026 {
			inSyncScope = op.Enabled
			continue
		}
		if inSyncScope && historyOpMutatesContent(op) {
			return true
		}
	}
	if inSyncScope && len(tx.PrimaryScrollOut) > 0 {
		return true
	}
	return false
}

func historyOpMutatesStreamContent(op history.TerminalSemanticOp) bool {
	switch op.Code {
	case vterm.ScreenOpWriteSpan, vterm.ScreenOpClearRect, vterm.ScreenOpClearToEOL:
		return true
	case vterm.ScreenOpControl:
		switch op.Control {
		case "ed", "el", "ech", "dch", "ich", "il", "dl", "su", "sd", "ri", "ris":
			return true
		}
	}
	return len(op.ScrollOut) > 0
}

func historyOpMutatesContent(op history.TerminalSemanticOp) bool {
	switch op.Code {
	case vterm.ScreenOpWriteSpan, vterm.ScreenOpScrollRect, vterm.ScreenOpCopyRect, vterm.ScreenOpClearRect, vterm.ScreenOpClearToEOL, vterm.ScreenOpResize:
		return true
	case vterm.ScreenOpControl:
		switch op.Control {
		case "ed", "el", "ech", "dch", "ich", "il", "dl", "su", "sd", "ri", "ris":
			return true
		}
	}
	return len(op.ScrollOut) > 0
}

func historyTransactionShouldConsumeScrollOut(tx history.TerminalSemanticTransaction, state history.HistoryReadState) bool {
	if len(tx.PrimaryScrollOut) == 0 && !historyTransactionHasOpScrollOut(tx) {
		return false
	}
	if state.HasPrimaryCurrent || tx.SynchronizedBegin || tx.SynchronizedActive || tx.SynchronizedEnd || tx.RequiresFullReplace {
		return true
	}
	return !historyTransactionHasEraseDisplay(tx)
}

func historyTransactionHasOpScrollOut(tx history.TerminalSemanticTransaction) bool {
	for _, op := range tx.Ops {
		if len(op.ScrollOut) > 0 {
			return true
		}
	}
	return false
}

func (terminal *Terminal) syncInfo(info TerminalInfo) {
	if terminal.update != nil {
		terminal.update(info)
	}
}
