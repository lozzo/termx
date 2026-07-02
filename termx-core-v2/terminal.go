package termxcorev2

import (
	"context"
	"fmt"
	"log/slog"
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
	live    *live.SurfaceTrack
	// 中文说明：liveOpMu 串行化 live SurfaceTrack 的 PTY 输出与 resize/restart 操作。
	// live 是唯一 response owner；resize 期间产生的新输出必须等 live 调整到新尺寸后再写入。
	liveOpMu     sync.Mutex
	liveRevision LiveRevision
	tap          *SemanticTap
	// 中文说明：tapOpMu 串行化 history semantic consumer 的 PTY 输出与 resize/restart 操作。
	// 它只服务 history journal 语义链路；live latest screen 不再被 history semantic write 定速。
	tapOpMu         sync.Mutex
	historyMu       sync.Mutex
	historyRenderer history.HistoryLogicalRenderer
	journalRenderer history.HistoryJournalRenderer
	historyStore    history.HistoryStore
	historyEnabled  bool
	logger          *slog.Logger
	historyStatus   HistoryBacklogStatus
	queueMu         sync.Mutex
	liveQ           *terminalLiveIngestQueue
	historyTapQ     *terminalLiveIngestQueue
	historyQ        *terminalHistoryIngestQueue
	events          *eventBroker
	update          func(TerminalInfo)
}

func newTerminal(info TerminalInfo, options TerminalCreateOptions, process TerminalProcess, events *eventBroker, update func(TerminalInfo), historyStore history.HistoryStore, historyEnabled bool, logger *slog.Logger) *Terminal {
	terminal := &Terminal{
		info:           info.Clone(),
		options:        cloneTerminalCreateOptions(options),
		process:        process,
		events:         events,
		update:         update,
		historyEnabled: historyEnabled,
		logger:         logger,
	}
	terminal.live = live.NewSurfaceTrackWithOptions(live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)}, live.SurfaceTrackOptions{
		OnResponse: terminal.handleLiveSurfaceResponse,
	})
	if historyEnabled {
		terminal.historyStatus = HistoryBacklogStatus{
			TerminalID:     info.ID,
			HistoryEnabled: true,
		}
		// 中文说明：history semantic tap 不持有 response owner；OSC/DA/DSR 只能由
		// live SurfaceTrack 回写一次，避免 live/history 双 vterm 双回写。
		terminal.tap = NewSemanticTap(info.ID, info.Size, nil)
		terminal.historyRenderer, terminal.journalRenderer = history.NewHistoryRenderers(nil, nil)
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
// 它先走 live SurfaceTrack latest 快路径，再把同一 PTY bytes 交给 history semantic
// consumer；history truth 仍只能来自 HistoryStore，不能从 live rows 反推。
func (terminal *Terminal) IngestOutput(output string) error {
	terminal.mu.Lock()
	if terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	info := terminal.info.Clone()
	terminal.mu.Unlock()

	revision, err := terminal.applyLiveOutput(output)
	if err != nil {
		return err
	}

	terminal.publishLiveInvalidated(info.ID, uint64(revision))
	if terminal.historyEnabled {
		if err := terminal.ingestHistorySemanticOutput(output, nil, info.ID); err != nil {
			return err
		}
	}
	return nil
}

// Resize 调整 terminal PTY 和 core native live screen 的尺寸。
// resize 和 PTY bytes 按同一顺序进入 live SurfaceTrack 与 history semantic consumer；
// history 只消费 resize transaction 作为 non-history boundary，不能由 resized live snapshot 生成 sealed history。
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
	if err := terminal.flushLiveQueue(context.Background()); err != nil {
		return err
	}
	if err := terminal.flushHistoryTapQueue(context.Background()); err != nil {
		return err
	}

	terminal.liveOpMu.Lock()
	terminal.tapOpMu.Lock()
	defer terminal.tapOpMu.Unlock()
	defer terminal.liveOpMu.Unlock()

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

	terminal.live.Resize(live.SurfaceSize{Cols: int(size.Cols), Rows: int(size.Rows)})
	terminal.liveRevision++
	revision := terminal.liveRevision
	if terminal.historyEnabled && terminal.tap != nil {
		result, err := terminal.tap.Resize(size)
		if err != nil {
			return err
		}
		terminal.enqueueOrApplyHistoryResizeTransaction(result.Transaction())
	}

	terminal.syncInfo(info)
	terminal.publishResize(info, oldSize, size)
	terminal.publishLiveInvalidated(info.ID, uint64(revision))
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
	if err := terminal.flushLiveQueue(ctx); err != nil {
		return err
	}
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
	terminal.liveQ = nil
	terminal.historyTapQ = nil
	terminal.historyQ = nil
	terminal.queueMu.Unlock()
	terminal.liveOpMu.Lock()
	terminal.live.ResetForRestartPreservingScreen()
	terminal.liveRevision++
	revision := terminal.liveRevision
	terminal.liveOpMu.Unlock()
	if terminal.historyEnabled {
		terminal.tapOpMu.Lock()
		terminal.tap = NewSemanticTap(info.ID, info.Size, nil)
		terminal.tapOpMu.Unlock()
	}
	terminal.syncInfo(info)
	terminal.watchProcess(process)
	_ = old.Close()
	terminal.publishLifecycle(EventTerminalChanged, info)
	terminal.publishLiveInvalidated(info.ID, uint64(revision))
	return nil
}

func (terminal *Terminal) Wait() <-chan ProcessExit {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.process.Wait()
}

func (terminal *Terminal) LiveRows() []string {
	terminal.liveOpMu.Lock()
	defer terminal.liveOpMu.Unlock()
	return terminal.live.Rows()
}

func (terminal *Terminal) LiveSnapshot() live.SurfaceSnapshot {
	terminal.liveOpMu.Lock()
	defer terminal.liveOpMu.Unlock()
	return terminal.live.Snapshot()
}

func (terminal *Terminal) VisitLiveTrimmedScreenRows(visit func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell)) vterm.TrimmedScreenRowsInfo {
	terminal.liveOpMu.Lock()
	defer terminal.liveOpMu.Unlock()
	return terminal.live.VisitTrimmedScreenRows(visit)
}

// VisitLiveTrimmedScreenRowsWithRevision 在同一 live 锁内读取当前 native screen
// 与 live projection revision。revision 只表示当前屏投影版本，不是 history truth
// generation；protocol/TUI 用它拒绝旧 snapshot，不能把它解释成 logical history 版本。
func (terminal *Terminal) VisitLiveTrimmedScreenRowsWithRevision(visit func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell)) (vterm.TrimmedScreenRowsInfo, uint64) {
	terminal.liveOpMu.Lock()
	defer terminal.liveOpMu.Unlock()
	info := terminal.live.VisitTrimmedScreenRows(visit)
	return info, uint64(terminal.liveRevision)
}

// NativeScreenSnapshot 返回 core 当前 latest native screen。
// 调用方只能把它用于实时显示 projection；history/window/copy truth 必须继续走 HistoryWindow/Copy。
func (terminal *Terminal) NativeScreenSnapshot(terminalID string) NativeScreenSnapshot {
	terminal.liveOpMu.Lock()
	snapshot := terminalNativeScreenSnapshotFromLiveSurface(terminalID, terminal.liveRevision, terminal.live.Snapshot())
	terminal.liveOpMu.Unlock()
	snapshot.TerminalID = terminalID
	return snapshot
}

// LiveRevision 返回当前 terminal native screen 的 latest-only revision。
// 它只服务 live invalidation one-shot arm 的“是否已经有新屏幕”判断；
// history/window/copy 不能把它当成 logical history generation。
func (terminal *Terminal) LiveRevision() LiveRevision {
	terminal.liveOpMu.Lock()
	defer terminal.liveOpMu.Unlock()
	return terminal.liveRevision
}

// HistoryBacklogStatus 返回 terminal history semantic transaction backlog 的追平诊断。
// 它只读取 tap 后 history worker 的 applied/target seq，不 flush、不读取 live screen、
// 不构造 HistoryWindow；history disabled 时返回 disabled 状态，调用方不能把该诊断
// 当作 authoritative history payload 或 frozen token。
func (terminal *Terminal) HistoryBacklogStatus() HistoryBacklogStatus {
	terminal.mu.Lock()
	terminalID := terminal.info.ID
	terminal.mu.Unlock()
	terminal.queueMu.Lock()
	status := terminal.historyStatus
	status.TerminalID = terminalID
	status.HistoryEnabled = terminal.historyEnabled
	queue := terminal.historyQ
	terminal.queueMu.Unlock()
	if !terminal.historyEnabled {
		return status
	}
	if queue == nil {
		terminal.historyMu.Lock()
		enabled := terminal.historyEnabled && terminal.historyStore != nil
		terminal.historyMu.Unlock()
		status.HistoryEnabled = enabled
		return status
	}
	status = queue.Status()
	status.TerminalID = terminalID
	status.HistoryEnabled = true
	return status
}

// FlushHistory 等待当前 terminal 的 history semantic queue 与 compact journal worker 追平。
// 第一段 fence 保证已进入 history semantic 队列的 bytes 先推进 history tap；第二段
// fence 再等待 tap 产出的 compact journal 落到 authoritative history store。
// 它不等待客户端 render；调用边界是 history.window/freeze/copy 和 lifecycle close。
func (terminal *Terminal) FlushHistory(ctx context.Context) error {
	if err := terminal.flushHistoryTapQueue(ctx); err != nil {
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
	if err := queue.Flush(ctx); err != nil {
		return err
	}
	terminal.cacheHistoryBacklogStatus(queue)
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
	liveQueue := newTerminalLiveIngestQueue()
	terminal.setLiveQueue(process, liveQueue)
	var historyTapQueue *terminalLiveIngestQueue
	var historyWorker *terminalHistoryIngestQueue
	if terminal.historyEnabled {
		historyTapQueue = newTerminalHistoryTapIngestQueue()
		terminal.setHistoryTapQueue(process, historyTapQueue)
		startSeq := terminal.historyBacklogStartSeq()
		historyWorker = newTerminalHistoryIngestQueue(startSeq)
		terminal.setHistoryQueue(process, historyWorker)
		go historyWorker.Run(func(journals []history.HistoryJournal) error {
			return terminal.ingestProcessHistoryJournals(process, journals)
		})
		go historyTapQueue.Run(func(output string) error {
			return terminal.ingestProcessHistoryTapOutput(process, output, historyWorker)
		})
	}
	go liveQueue.Run(func(output string) error {
		return terminal.ingestProcessLiveOutput(process, output)
	})
	go func() {
		defer close(done)
		defer func() {
			liveQueue.Close()
			liveQueue.Wait()
			terminal.clearLiveQueue(process, liveQueue)
			if historyTapQueue != nil {
				historyTapQueue.Close()
				historyTapQueue.Wait()
				terminal.clearHistoryTapQueue(process, historyTapQueue)
			}
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
			liveQueue.Enqueue(text)
			if historyTapQueue != nil {
				historyTapQueue.Enqueue(text)
			}
		}
	}()
	return done
}

func (terminal *Terminal) setLiveQueue(process TerminalProcess, queue *terminalLiveIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	terminal.liveQ = queue
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) clearLiveQueue(process TerminalProcess, queue *terminalLiveIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	if terminal.liveQ == queue {
		terminal.liveQ = nil
	}
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) flushLiveQueue(ctx context.Context) error {
	terminal.queueMu.Lock()
	queue := terminal.liveQ
	terminal.queueMu.Unlock()
	if queue == nil {
		return nil
	}
	return queue.Flush(ctx)
}

func (terminal *Terminal) setHistoryTapQueue(process TerminalProcess, queue *terminalLiveIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	terminal.historyTapQ = queue
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) clearHistoryTapQueue(process TerminalProcess, queue *terminalLiveIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	if terminal.historyTapQ == queue {
		terminal.historyTapQ = nil
	}
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) flushHistoryTapQueue(ctx context.Context) error {
	terminal.queueMu.Lock()
	queue := terminal.historyTapQ
	terminal.queueMu.Unlock()
	if queue == nil {
		return nil
	}
	return queue.Flush(ctx)
}

func (terminal *Terminal) setHistoryQueue(process TerminalProcess, queue *terminalHistoryIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminalID := terminal.info.ID
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	terminal.historyQ = queue
	terminal.cacheHistoryBacklogStatusLocked(queue, terminalID)
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) clearHistoryQueue(process TerminalProcess, queue *terminalHistoryIngestQueue) {
	terminal.mu.Lock()
	current := terminal.process == process
	terminalID := terminal.info.ID
	terminal.mu.Unlock()
	if !current {
		return
	}
	terminal.queueMu.Lock()
	if terminal.historyQ == queue {
		terminal.cacheHistoryBacklogStatusLocked(queue, terminalID)
		terminal.historyQ = nil
	}
	terminal.queueMu.Unlock()
}

// cacheHistoryBacklogStatus 把当前 worker 的追平序号保存到 terminal 级诊断缓存。
// queue 对象会随 process lifecycle 重建；缓存保证诊断 owner 仍是 terminal，而不是
// 短生命周期 worker。
func (terminal *Terminal) cacheHistoryBacklogStatus(queue *terminalHistoryIngestQueue) {
	if queue == nil {
		return
	}
	terminal.mu.Lock()
	terminalID := terminal.info.ID
	terminal.mu.Unlock()
	terminal.queueMu.Lock()
	terminal.cacheHistoryBacklogStatusLocked(queue, terminalID)
	terminal.queueMu.Unlock()
}

// cacheHistoryBacklogStatusLocked 在 queueMu 内更新 terminal 级 backlog 诊断。
// 调用方必须已确认 queue 属于当前 terminal/process；该方法只复制 seq 元数据，
// 不读取或修改 HistoryStore payload。
func (terminal *Terminal) cacheHistoryBacklogStatusLocked(queue *terminalHistoryIngestQueue, terminalID string) {
	terminal.historyStatus = queue.Status()
	terminal.historyStatus.TerminalID = terminalID
	terminal.historyStatus.HistoryEnabled = terminal.historyEnabled
}

// historyBacklogStartSeq 返回新 history worker 的起始序号。
// worker 可以随 process lifecycle 重建，但 diagnostic seq 代表 terminal history
// backlog 边界，不能因为 queue 对象重建而倒退。
func (terminal *Terminal) historyBacklogStartSeq() uint64 {
	terminal.queueMu.Lock()
	defer terminal.queueMu.Unlock()
	if terminal.historyStatus.TargetSeq > terminal.historyStatus.AppliedSeq {
		return terminal.historyStatus.TargetSeq
	}
	return terminal.historyStatus.AppliedSeq
}

func (terminal *Terminal) enqueueOrApplyHistoryResizeTransaction(tx history.TerminalSemanticTransaction) {
	terminal.mu.Lock()
	terminalID := terminal.info.ID
	terminal.mu.Unlock()
	journal := history.HistoryJournalFromDecision(terminalID, tx, history.HistoryDecision{
		Mode:               history.HistoryOutputModeBoundaryOnly,
		NonHistoryBoundary: true,
	})
	terminal.queueMu.Lock()
	queue := terminal.historyQ
	terminal.queueMu.Unlock()
	if queue != nil && queue.Enqueue(journal) {
		return
	}
	// 中文说明：没有异步 history worker 时，resize 仍只按 boundary-only 进入 renderer；
	// 不能从 resized live snapshot 回填 committed history，也不能等待未来输出兜底。
	terminal.applyHistoryJournal(journal)
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
	terminal.mu.Unlock()

	finishLiveWrite := perftrace.Measure("core.live.write_screen")
	revision, err := terminal.applyLiveOutput(output)
	finishLiveWrite(len(output))
	if err != nil {
		return err
	}

	terminal.mu.Lock()
	stillCurrent := terminal.process == process && terminal.info.State != TerminalStateExited && terminal.info.State != TerminalStateRemoved
	terminal.mu.Unlock()
	if !stillCurrent {
		return nil
	}
	// 中文说明：live latest screen 已经由 SurfaceTrack 快路径更新；wake 必须先发出，
	// history semantic write / journal queue / store 写入不能反压用户可见的实时屏幕。
	perftrace.Count("core.terminal.changed", len(output))
	perftrace.Count("core.live.invalidation_publish", len(output))
	terminal.publishLiveInvalidated(info.ID, uint64(revision))
	return nil
}

func (terminal *Terminal) ingestProcessHistoryTapOutput(process TerminalProcess, output string, historyWorker *terminalHistoryIngestQueue) error {
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
	return terminal.ingestHistorySemanticOutput(output, historyWorker, info.ID)
}

func (terminal *Terminal) applyLiveOutput(output string) (LiveRevision, error) {
	terminal.liveOpMu.Lock()
	defer terminal.liveOpMu.Unlock()
	if terminal.live == nil {
		return terminal.liveRevision, nil
	}
	terminal.live.Write(output)
	terminal.liveRevision++
	return terminal.liveRevision, nil
}

func (terminal *Terminal) ingestHistorySemanticOutput(output string, historyWorker *terminalHistoryIngestQueue, terminalID string) error {
	if !terminal.historyEnabled || terminal.tap == nil {
		return nil
	}
	terminal.tapOpMu.Lock()
	finishHistoryWrite := perftrace.Measure("core.history.semantic_write")
	result, err := terminal.tap.ApplyPTYWrite([]byte(output))
	finishHistoryWrite(len(output))
	terminal.tapOpMu.Unlock()
	if err != nil {
		return err
	}
	finishHistoryFanout := perftrace.Measure("core.history.journal_fanout")
	terminal.enqueueOrApplyProcessHistoryJournal(result, historyWorker, terminalID)
	finishHistoryFanout(len(output))
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
	revision, err := terminal.applyLiveOutput(text)
	if err != nil {
		return
	}
	terminal.publishLiveInvalidated(info.ID, uint64(revision))
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

func terminalNativeScreenSnapshotFromLiveSurface(terminalID string, revision LiveRevision, snapshot live.SurfaceSnapshot) NativeScreenSnapshot {
	rows := make([]NativeScreenRow, len(snapshot.Screen.Cells))
	for index, row := range snapshot.Screen.Cells {
		rows[index] = NativeScreenRow{
			Index: index,
			Cells: cloneSemanticTapCells(row),
		}
	}
	return NativeScreenSnapshot{
		TerminalID: terminalID,
		Revision:   revision,
		Size:       NativeScreenSize{Cols: snapshot.Size.Cols, Rows: snapshot.Size.Rows},
		Rows:       rows,
		Cursor:     snapshot.Cursor,
		Modes:      snapshot.Modes,
		AltScreen:  snapshot.Screen.IsAlternateScreen,
		Timestamp:  time.Now().UTC(),
	}
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

// HistoryCopyEntryProjection 返回 copy/history 入口的 materialized projection。
// 调用边界：它只读取已应用到 HistoryStore 的 frontier，不等待 history backlog 追平，
// 也不创建 frozen token；调用方必须把 catchup/capability 元数据继续暴露给 UI。
func (terminal *Terminal) HistoryCopyEntryProjection(req history.CopyEntryProjectionRequest) (history.CopyEntryProjection, error) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled {
		return history.CopyEntryProjection{}, ErrHistoryDisabled
	}
	if terminal.historyStore == nil {
		return history.CopyEntryProjection{}, ErrHistoryNotRebuilt
	}
	return terminal.historyStore.CopyEntryProjection(req)
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

func (terminal *Terminal) ingestProcessHistoryJournals(process TerminalProcess, journals []history.HistoryJournal) error {
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
	terminal.ingestHistoryJournals(journals)
	return nil
}

func (terminal *Terminal) ingestHistoryJournals(journals []history.HistoryJournal) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled || terminal.journalRenderer == nil || terminal.historyStore == nil {
		return
	}
	terminal.applyHistoryJournalsLocked(journals)
}

func (terminal *Terminal) applyHistoryJournalsLocked(journals []history.HistoryJournal) {
	merged := history.HistoryMutationBatch{
		Mutations: make([]history.HistoryMutation, 0, estimatedHistoryMutationCount(journals)),
	}
	for _, journal := range journals {
		batch, err := terminal.journalRenderer.ApplyJournal(journal)
		if err != nil {
			continue
		}
		if batch.Seq > merged.Seq {
			merged.Seq = batch.Seq
		}
		if batch.Generation > merged.Generation {
			merged.Generation = batch.Generation
		}
		merged.Mutations = append(merged.Mutations, batch.Mutations...)
	}
	if len(merged.Mutations) == 0 {
		return
	}
	// 中文说明：history worker 已经按 history semantic seq 顺序拿到 compact journal；
	// 同一 worker batch 内可以合并 renderer mutations 后一次写入 HistoryStore，
	// 减少文件 backend 小事务和 storage flush 次数。这里不改变 history truth：
	// authoritative logical-line/window/cursor 仍只由 HistoryStore.Apply 产生。
	_ = terminal.historyStore.Apply(merged)
}

func estimatedHistoryMutationCount(journals []history.HistoryJournal) int {
	count := 0
	for _, journal := range journals {
		for _, item := range journal.Items {
			switch item.Kind {
			case history.HistoryJournalItemOrdinaryLineBatch:
				if item.Ordinary != nil {
					count += len(item.Ordinary.Lines) * 2
					if item.Ordinary.OpenUpdate != nil {
						count++
					}
					count += len(item.Ordinary.Commands)
				}
			case history.HistoryJournalItemBoundary, history.HistoryJournalItemFrameEvent, history.HistoryJournalItemScrollOutProof:
				count += 4
			}
		}
	}
	if count < len(journals) {
		return len(journals)
	}
	return count
}

func (terminal *Terminal) enqueueOrApplyProcessHistoryJournal(result SemanticTapResult, queue *terminalHistoryIngestQueue, terminalID string) {
	journal := result.HistoryJournal()
	if journal.TerminalID == "" {
		journal.TerminalID = terminalID
	}
	if queue != nil && terminal.historyJournalCanQueue(journal) && !queue.NeedsClassifierFlush(journal) && queue.Enqueue(journal) {
		return
	}
	if queue != nil && queue.NeedsClassifierFlush(journal) {
		// 中文说明：普通 sealed-line journal 可直接排队；一旦 backlog 或当前
		// journal 会改变 classifier state，必须先追平 authoritative store，再
		// 用同一 semantic transaction 构造 decision-aware journal。这里仍只消费
		// compact journal 队列，不回放 raw PTY，也不走 full transaction renderer。
		_ = queue.Flush(context.Background())
	}
	tx := result.Transaction()
	terminal.historyMu.Lock()
	state := history.HistoryReadState{}
	if terminal.historyEnabled && terminal.historyStore != nil {
		state = terminal.historyStore.ReadState()
	}
	decision := terminal.historyDecisionForTransaction(tx, state)
	journal = history.HistoryJournalFromDecision(terminalID, tx, decision)
	terminal.historyMu.Unlock()
	if journal.TerminalID == "" {
		journal.TerminalID = terminalID
	}
	if queue != nil && terminal.historyJournalCanQueue(journal) && queue.Enqueue(journal) {
		return
	}
	if queue != nil {
		// 中文说明：history consumer 的生产入口只排 compact journal。若当前
		// journal 不能进入异步队列，先追平旧队列，再同步应用同一 journal；
		// 不回拉 full transaction，也不从 live snapshot/raw PTY 建第二条 truth。
		_ = queue.Flush(context.Background())
	}
	terminal.applyHistoryJournal(journal)
}

func (terminal *Terminal) historyJournalCanQueue(journal history.HistoryJournal) bool {
	if !terminal.historyEnabled || terminal.journalRenderer == nil || terminal.historyStore == nil {
		return false
	}
	return historyJournalAllowsTerminalQueue(journal)
}

func (terminal *Terminal) applyHistoryJournal(journal history.HistoryJournal) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	terminal.applyHistoryJournalsLocked([]history.HistoryJournal{journal})
}

func historyJournalAllowsTerminalQueue(journal history.HistoryJournal) bool {
	// 中文说明：R386 process hot path 先看 compact journal 自身是否能安全排队，
	// 避免普通 sealed line 为了 classifier 再同步读取 HistoryStore。复杂语义必须
	// 先 flush backlog 后用同一 transaction 的 decision-aware journal 同步应用，
	// 不能回到 full transaction renderer 或从 journal 猜测 current-screen close proof。
	if len(journal.Items) == 0 {
		return false
	}
	for _, item := range journal.Items {
		if item.Kind != history.HistoryJournalItemOrdinaryLineBatch {
			return false
		}
		if item.Ordinary == nil {
			return false
		}
	}
	return true
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
	// 中文说明：history semantic consumer 可能把 begin/payload/end 放在同一个 transaction；
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
	// 中文说明：有些 primary screen app 不开启 DEC 2026，而是用 CUP/VPA/HPA/CHA
	// 直接定位光标并重绘当前 viewport。vterm 已经提供 PrimaryFrame side proof；
	// classifier 必须把它归到 mutable screen-frame，而不是把每次 repaint 的
	// 中间行写成 ordinary history。
	cursorAddressedFrameSession := historyTransactionHasCursorAddressedPrimaryFrame(tx) && !syncEndThenOrdinaryStream
	isPrimaryFrameSession := (syncFrameSession && !syncEndThenOrdinaryStream) || cursorAddressedFrameSession || fullReplaceCanPublish || hasEraseDisplay
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
		// 中文说明：synchronized output begin/end 只是 screen app 的 repaint
		// flush 边界，不是 lifecycle boundary。连续 repaint 必须替换 mutable
		// current primary frame；只有 alt-enter、ordinary stream 恢复或 process
		// close 这类明确边界才能把旧 frame seal 进 timeline。
		decision.ArchivePrimaryAfterPrimaryFrame = tx.AltEntered
		// 中文说明：同步输出或 full-replace direct damage 刚启动时，vterm
		// PrimaryFrame side proof 会包含屏幕上已经 sealed 的普通 shell tail。
		// 没有 clear 边界时，只允许本 transaction 触达的 rows 进入 current
		// frame，不能把旧 shell 屏幕复刻成第二份 screen app history truth。
		// 中文说明：scroll-out proof 只证明部分内容离开 viewport，不能把
		// PrimaryFrame side proof 升级成整屏 ownership。没有 ED2 clear 这类明确
		// 清场边界时，primary repaint 仍只能接管本 transaction 的 touched rows，
		// 否则已经 seal 的 shell logical line 会被 final/current screen-frame 复制。
		decision.PublishPrimaryFrameTouchedRowsOnly = !hasEraseDisplay && len(tx.PrimaryFrameTouchedRows) > 0 && (syncFrameSession || cursorAddressedFrameSession || fullReplaceTouchedRowsOnly)
		decision.SkipPreExistingPrimaryScrollOut = decision.PublishPrimaryFrameTouchedRowsOnly && state.HasTimeline && !state.HasPrimaryCurrent
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

func historyTransactionHasCursorAddressedPrimaryFrame(tx history.TerminalSemanticTransaction) bool {
	if tx.PrimaryFrame == nil || !historyTransactionHasContentMutation(tx) {
		return false
	}
	for _, op := range tx.Ops {
		if op.Code != vterm.ScreenOpControl {
			continue
		}
		switch op.Control {
		case "cup", "vpa", "hpa", "cha":
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
