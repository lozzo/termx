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
	mu              sync.Mutex
	info            TerminalInfo
	options         TerminalCreateOptions
	process         TerminalProcess
	liveMu          sync.Mutex
	live            *live.SurfaceTrack
	liveRevision    uint64
	historyMu       sync.Mutex
	historyRenderer history.HistoryLogicalRenderer
	historyStore    history.HistoryStore
	queueMu         sync.Mutex
	liveQ           *terminalLiveIngestQueue
	events          *eventBroker
	update          func(TerminalInfo)
}

func newTerminal(info TerminalInfo, options TerminalCreateOptions, process TerminalProcess, events *eventBroker, update func(TerminalInfo), historyStore history.HistoryStore) *Terminal {
	size := live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)}
	terminal := &Terminal{
		info:    info.Clone(),
		options: cloneTerminalCreateOptions(options),
		process: process,
		events:  events,
		update:  update,
	}
	terminal.historyRenderer = history.NewHistoryLogicalRenderer(nil, nil)
	if historyStore == nil {
		historyStore = history.NewInMemoryHistoryStore(info.ID)
	}
	terminal.historyStore = historyStore
	liveOptions := live.DefaultSurfaceTrackOptions()
	liveOptions.OnResponse = terminal.handleLiveSurfaceResponse
	terminal.live = live.NewSurfaceTrackWithOptions(size, liveOptions)
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

func (terminal *Terminal) IngestOutput(output string) error {
	rawOutput := output
	terminal.mu.Lock()
	if terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	info := terminal.info.Clone()
	surface := terminal.live
	terminal.mu.Unlock()

	terminal.liveMu.Lock()
	result := surface.WriteWithResult(rawOutput)
	terminal.bumpLiveRevisionLocked()
	terminal.liveMu.Unlock()
	terminal.applyHistoryWriteResult(result)

	terminal.publish(EventTerminalChanged, info)
	return nil
}

func normalizeTerminalOutput(output string) string {
	if output == "" || !strings.Contains(output, "\r") {
		return output
	}
	output = strings.ReplaceAll(output, "\r\n", "\n")
	return output
}

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

	terminal.liveMu.Lock()
	tx := terminal.live.Resize(live.SurfaceSize{Cols: int(size.Cols), Rows: int(size.Rows)})
	terminal.bumpLiveRevisionLocked()
	terminal.liveMu.Unlock()
	terminal.applyHistoryResizeTransaction(tx)

	terminal.syncInfo(info)
	terminal.publishResize(info, oldSize, size)
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
	if shouldCloseHistory {
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
	terminal.queueMu.Unlock()
	terminal.liveMu.Lock()
	terminal.live.Resize(live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)})
	terminal.live.ResetForRestartPreservingScreen()
	terminal.bumpLiveRevisionLocked()
	terminal.liveMu.Unlock()
	terminal.syncInfo(info)
	terminal.watchProcess(process)
	_ = old.Close()
	terminal.publishLifecycle(EventTerminalChanged, info)
	return nil
}

func (terminal *Terminal) Wait() <-chan ProcessExit {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.process.Wait()
}

func (terminal *Terminal) LiveRows() []string {
	terminal.liveMu.Lock()
	defer terminal.liveMu.Unlock()
	return terminal.live.Rows()
}

func (terminal *Terminal) LiveSnapshot() live.SurfaceSnapshot {
	terminal.liveMu.Lock()
	defer terminal.liveMu.Unlock()
	return terminal.live.Snapshot()
}

func (terminal *Terminal) VisitLiveTrimmedScreenRows(visit func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell)) vterm.TrimmedScreenRowsInfo {
	terminal.liveMu.Lock()
	defer terminal.liveMu.Unlock()
	return terminal.live.VisitTrimmedScreenRows(visit)
}

// VisitLiveTrimmedScreenRowsWithRevision 在同一 live 锁内读取当前 native screen
// 与 live projection revision。revision 只表示当前屏投影版本，不是 history truth
// generation；protocol/TUI 用它拒绝旧 snapshot，不能把它解释成 logical history 版本。
func (terminal *Terminal) VisitLiveTrimmedScreenRowsWithRevision(visit func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell)) (vterm.TrimmedScreenRowsInfo, uint64) {
	terminal.liveMu.Lock()
	defer terminal.liveMu.Unlock()
	info := terminal.live.VisitTrimmedScreenRows(visit)
	return info, terminal.liveRevision
}

func (terminal *Terminal) FlushHistory(ctx context.Context) error {
	terminal.queueMu.Lock()
	liveQueue := terminal.liveQ
	terminal.queueMu.Unlock()
	if liveQueue != nil {
		if err := liveQueue.Flush(ctx); err != nil {
			return err
		}
	}
	return nil
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
	liveQueue := newTerminalLiveIngestQueue()
	terminal.setIngestQueues(process, liveQueue)
	go liveQueue.Run(func(output string) error {
		return terminal.ingestProcessLiveOutput(process, output)
	})
	go func() {
		defer close(done)
		defer func() {
			liveQueue.Close()
			liveQueue.Wait()
			terminal.clearIngestQueues(process, liveQueue)
		}()
		for chunk := range output {
			if len(chunk) == 0 {
				continue
			}
			text := string(chunk)
			liveQueue.Enqueue(text)
		}
	}()
	return done
}

func (terminal *Terminal) setIngestQueues(process TerminalProcess, liveQueue *terminalLiveIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	terminal.liveQ = liveQueue
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) clearIngestQueues(process TerminalProcess, liveQueue *terminalLiveIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	if terminal.liveQ == liveQueue {
		terminal.liveQ = nil
	}
	terminal.queueMu.Unlock()
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

func (terminal *Terminal) ingestProcessLiveOutput(process TerminalProcess, output string) error {
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
	surface := terminal.live
	terminal.mu.Unlock()

	terminal.liveMu.Lock()
	result := surface.WriteWithResult(output)
	terminal.bumpLiveRevisionLocked()
	terminal.liveMu.Unlock()
	terminal.applyHistoryWriteResult(result)

	terminal.mu.Lock()
	stillCurrent := terminal.process == process && terminal.info.State != TerminalStateExited && terminal.info.State != TerminalStateRemoved
	terminal.mu.Unlock()
	if !stillCurrent {
		return nil
	}
	perftrace.Count("core.terminal.changed", len(output))
	terminal.publish(EventTerminalChanged, info)
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
	terminal.forceCloseHistory(history.CloseReasonProcessExit)
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
	terminal.liveMu.Lock()
	terminal.live.Write(text)
	terminal.bumpLiveRevisionLocked()
	terminal.liveMu.Unlock()
}

func (terminal *Terminal) bumpLiveRevisionLocked() {
	terminal.liveRevision++
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

func (terminal *Terminal) HistoryWindow(req history.HistoryWindowRequest) (history.HistoryWindow, error) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if terminal.historyStore == nil {
		return history.HistoryWindow{}, ErrHistoryNotRebuilt
	}
	switch req.Mode {
	case "", history.HistoryWindowModeLatest:
		return terminal.historyStore.LatestWindow(req)
	case history.HistoryWindowModeOlder, history.HistoryWindowModeOldest:
		return terminal.historyStore.OlderWindow(req)
	case history.HistoryWindowModeNewer:
		return terminal.historyStore.NewerWindow(req)
	default:
		return history.HistoryWindow{}, history.ErrHistoryUnsupportedWindowMode
	}
}

func (terminal *Terminal) HistoryCopy(req history.HistoryCopyRequest) (string, error) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if terminal.historyStore == nil {
		return "", ErrHistoryNotRebuilt
	}
	return terminal.historyStore.Copy(req)
}

func (terminal *Terminal) HistoryFreeze(req history.FreezeHistoryRequest) (history.FrozenHistorySnapshot, error) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if terminal.historyStore == nil {
		return history.FrozenHistorySnapshot{}, ErrHistoryNotRebuilt
	}
	return terminal.historyStore.Freeze(req)
}

// HistoryRelease 释放 terminal history store 中的 core-owned token。
func (terminal *Terminal) HistoryRelease(token history.HistoryToken) error {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if terminal.historyStore == nil {
		return ErrHistoryNotRebuilt
	}
	return terminal.historyStore.Release(token)
}

func (terminal *Terminal) applyHistoryWriteResult(result live.SurfaceWriteResult) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if terminal.historyRenderer == nil || terminal.historyStore == nil {
		return
	}
	for _, segment := range result.Segments {
		for _, tx := range segment.Transactions {
			decision := terminal.historyDecisionForTransaction(tx, terminal.historyStore.ReadState())
			batch, err := terminal.historyRenderer.Apply(tx, decision)
			if err != nil {
				continue
			}
			_ = terminal.historyStore.Apply(batch)
		}
	}
}

func (terminal *Terminal) applyHistoryResizeTransaction(tx history.TerminalSemanticTransaction) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if terminal.historyRenderer == nil || terminal.historyStore == nil {
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

func (terminal *Terminal) forceCloseHistory(reason history.CloseReason) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if terminal.historyRenderer == nil || terminal.historyStore == nil {
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
	hasContentMutation := historyTransactionHasContentMutation(tx)
	hasContentAfterSyncEnd := historyTransactionHasContentAfterSyncEnd(tx)
	syncFrameSession := ((tx.SynchronizedActive || tx.SynchronizedEnd) && hasContentMutation && !hasContentAfterSyncEnd)
	// 中文说明：RequiresFullReplace 只是 vterm/live stale 边界；只有没有 current
	// primary owner 且缺少 ordered content ops 时，PrimaryFrame side proof 才能作为
	// 初始 screen redraw 进入 frame reducer。若已有 sealed timeline，full-replace
	// snapshot 必须带本 transaction 的 touch proof，否则 shell prompt 这类普通输出
	// 会被最终整屏 frame 又发布一次，和已 sealed 的尾屏形成重复历史。
	fullReplaceFrameOnly := tx.RequiresFullReplace && !state.HasPrimaryCurrent && !historyTransactionHasOrderedContentOps(tx)
	fullReplaceTouchedRowsOnly := fullReplaceFrameOnly && state.HasTimeline && len(tx.PrimaryFrameTouchedRows) > 0
	fullReplaceCanPublish := fullReplaceFrameOnly && (!state.HasTimeline || fullReplaceTouchedRowsOnly)
	isPrimaryFrameSession := syncFrameSession || fullReplaceCanPublish || hasEraseDisplay
	if tx.RequiresFullReplace && tx.FullReplaceReason == "resize" {
		return history.HistoryDecision{Mode: history.HistoryOutputModeBoundaryOnly, NonHistoryBoundary: true}
	}
	if tx.AltEntered {
		decision.ArchivePrimaryBeforeAlt = true
		decision.ClearPrimaryCurrent = true
	}
	if tx.AltExited {
		decision.ClearAltFrame = true
	}
	if tx.AltFrame != nil {
		decision.Mode = history.HistoryOutputModeAltTransient
		decision.PublishAltFrame = true
	}
	if tx.PrimaryFrame != nil && isPrimaryFrameSession {
		decision.Mode = history.HistoryOutputModePrimaryFrameSession
		decision.PublishPrimaryFrame = true
		// 中文说明：同步输出或 full-replace direct damage 刚启动时，vterm
		// PrimaryFrame side proof 会包含屏幕上已经 sealed 的普通 shell tail。
		// 没有 clear 边界时，只允许本 transaction 触达的 rows 进入 current
		// frame，不能把旧 shell 屏幕复刻成第二份 screen app history truth。
		decision.PublishPrimaryFrameTouchedRowsOnly = !hasEraseDisplay && (syncFrameSession || fullReplaceTouchedRowsOnly)
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
	for _, op := range tx.Ops {
		if op.Code == vterm.ScreenOpModes && op.Private && op.Mode == 2026 && !op.Enabled {
			afterSyncEnd = true
			continue
		}
		if afterSyncEnd && historyOpMutatesContent(op) {
			return true
		}
	}
	return false
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
