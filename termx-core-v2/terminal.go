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
	// 中文说明：这是 R358 legacy history 专用 parser owner，R372 后只作为
	// R373 切换到 single SemanticTap 前的隔离对象保留。它仍代表当前错误链路：
	// history worker replay raw PTY 并维护第二个 vterm，不能继续扩展为新模型。
	historySemantic *vterm.SemanticSource
	// 中文说明：history semantic worker 只在 vterm parser transaction 边界上保留
	// 未完成的 private CSI；它不是 raw history parser，也不生成 history truth。
	historySemanticPending string
	historyEnabled         bool
	queueMu                sync.Mutex
	historyQ               *terminalHistoryIngestQueue
	events                 *eventBroker
	update                 func(TerminalInfo)
}

func newTerminal(info TerminalInfo, options TerminalCreateOptions, process TerminalProcess, events *eventBroker, update func(TerminalInfo), historyStore history.HistoryStore, historyEnabled bool) *Terminal {
	size := live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)}
	terminal := &Terminal{
		info:           info.Clone(),
		options:        cloneTerminalCreateOptions(options),
		process:        process,
		events:         events,
		update:         update,
		historyEnabled: historyEnabled,
	}
	if historyEnabled {
		terminal.historyRenderer = history.NewHistoryLogicalRenderer(nil, nil)
		terminal.historySemantic = vterm.NewSemanticSource(size.Cols, size.Rows, 0, nil)
		if historyStore == nil {
			historyStore = history.NewInMemoryHistoryStore(info.ID)
		}
		terminal.historyStore = historyStore
	}
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

// IngestOutput 是测试和诊断入口，用一段 PTY 输出同步推进 terminal。
// 它会立即更新 native live screen，并在 history enabled 时同步喂给 history semantic
// source；真实 PTY 热路径使用 watchOutput 中的 live/history worker 分离，不能用本方法
// 的同步行为推断生产背压模型。
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
	surface.Write(rawOutput)
	terminal.bumpLiveRevisionLocked()
	liveRevision := terminal.liveRevision
	terminal.liveMu.Unlock()
	if terminal.historyEnabled {
		terminal.ingestHistoryOutputBatch([]string{output})
	}

	terminal.publishLiveInvalidated(info.ID, liveRevision)
	return nil
}

func normalizeTerminalOutput(output string) string {
	if output == "" || !strings.Contains(output, "\r") {
		return output
	}
	output = strings.ReplaceAll(output, "\r\n", "\n")
	return output
}

// splitTerminalHistorySemanticWrites 是 legacy history raw PTY replay 的临时分片器。
// R372 后它只标识待删除错误路径：history 不能长期从 raw bytes 重新解释 terminal
// 语义；R373 应由 single SemanticTap 直接 fan-out immutable semantic transaction。
func splitTerminalHistorySemanticWrites(pending *string, text string) []string {
	if text == "" && (pending == nil || *pending == "") {
		return nil
	}
	if pending != nil && *pending != "" {
		text = *pending + text
		*pending = ""
	}
	var writes []string
	var raw strings.Builder
	for text != "" {
		idx := strings.Index(text, "\x1b[?")
		if idx < 0 {
			raw.WriteString(text)
			break
		}
		if idx > 0 {
			raw.WriteString(text[:idx])
			text = text[idx:]
			continue
		}
		consumed, complete := consumeTerminalHistoryPrivateCSI(text)
		if !complete {
			if raw.Len() > 0 {
				writes = append(writes, raw.String())
				raw.Reset()
			}
			if pending != nil {
				*pending = text
			}
			return writes
		}
		if consumed <= 0 {
			raw.WriteString(text[:1])
			text = text[1:]
			continue
		}
		if raw.Len() > 0 {
			writes = append(writes, raw.String())
			raw.Reset()
		}
		writes = append(writes, text[:consumed])
		text = text[consumed:]
	}
	if raw.Len() > 0 {
		writes = append(writes, raw.String())
	}
	return writes
}

func consumeTerminalHistoryPrivateCSI(input string) (int, bool) {
	if !strings.HasPrefix(input, "\x1b[?") {
		return 0, true
	}
	for i := 3; i < len(input); i++ {
		b := input[i]
		if b >= 0x40 && b <= 0x7e {
			return i + 1, true
		}
	}
	return 0, false
}

// Resize 调整 terminal PTY 和 core native live screen 的尺寸。
// live resize 只更新当前屏；history resize 通过独立 semantic source 发送 non-history
// boundary，不能由 resized snapshot 生成或重写 authoritative history。
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
	terminal.live.Resize(live.SurfaceSize{Cols: int(size.Cols), Rows: int(size.Rows)})
	terminal.bumpLiveRevisionLocked()
	liveRevision := terminal.liveRevision
	terminal.liveMu.Unlock()
	if terminal.historyEnabled {
		terminal.applyHistoryResize(size)
	}

	terminal.syncInfo(info)
	terminal.publishResize(info, oldSize, size)
	terminal.publishLiveInvalidated(info.ID, liveRevision)
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
	terminal.historyQ = nil
	terminal.queueMu.Unlock()
	terminal.liveMu.Lock()
	terminal.live.Resize(live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)})
	terminal.live.ResetForRestartPreservingScreen()
	terminal.bumpLiveRevisionLocked()
	liveRevision := terminal.liveRevision
	terminal.liveMu.Unlock()
	terminal.resetHistorySemantic(info.Size)
	terminal.syncInfo(info)
	terminal.watchProcess(process)
	_ = old.Close()
	terminal.publishLifecycle(EventTerminalChanged, info)
	terminal.publishLiveInvalidated(info.ID, liveRevision)
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

// NativeScreenSnapshot 返回 core 当前 latest native screen。
// 调用方只能把它用于实时显示 projection；history/window/copy truth 必须继续走 HistoryWindow/Copy。
func (terminal *Terminal) NativeScreenSnapshot(terminalID string) NativeScreenSnapshot {
	terminal.liveMu.Lock()
	defer terminal.liveMu.Unlock()
	var rows []NativeScreenRow
	screenInfo := terminal.live.VisitTrimmedScreenRows(func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell) {
		for len(rows) < rowIndex {
			rows = append(rows, NativeScreenRow{Index: len(rows)})
		}
		row := NativeScreenRow{Index: rowIndex}
		if cellCount > 0 {
			row.Cells = make([]vterm.Cell, cellCount)
			for i := 0; i < cellCount; i++ {
				row.Cells[i] = cellAt(i)
			}
		}
		rows = append(rows, row)
	})
	for len(rows) < screenInfo.Rows {
		rows = append(rows, NativeScreenRow{Index: len(rows)})
	}
	return NativeScreenSnapshot{
		TerminalID: terminalID,
		Revision:   LiveRevision(terminal.liveRevision),
		Size:       NativeScreenSize{Cols: screenInfo.Cols, Rows: screenInfo.Rows},
		Rows:       rows,
		Cursor:     screenInfo.Cursor,
		Modes:      screenInfo.Modes,
		AltScreen:  screenInfo.IsAlternateScreen,
		Timestamp:  time.Now().UTC(),
	}
}

// LiveRevision 返回当前 terminal native screen 的 latest-only revision。
// 它只服务 live invalidation one-shot arm 的“是否已经有新屏幕”判断；
// history/window/copy 不能把它当成 logical history generation。
func (terminal *Terminal) LiveRevision() LiveRevision {
	terminal.liveMu.Lock()
	defer terminal.liveMu.Unlock()
	return LiveRevision(terminal.liveRevision)
}

// FlushHistory 等待当前 terminal 的 history semantic worker 追平已入队输出。
// 它不等待 live queue，也不关心客户端渲染进度；调用边界是 history.window/freeze/copy
// 和 lifecycle close，避免读取到尚未应用到 authoritative store 的历史。
func (terminal *Terminal) FlushHistory(ctx context.Context) error {
	terminal.queueMu.Lock()
	queue := terminal.historyQ
	terminal.queueMu.Unlock()
	if queue == nil {
		return nil
	}
	// 中文说明：history/copy/freeze 只等待 authoritative history worker 追平；
	// live native screen 没有 flush fence，不能因客户端查询反压最新屏链路。
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
	liveQueue := newTerminalLiveIngestQueue()
	var historyWorker *terminalHistoryIngestQueue
	if terminal.historyEnabled {
		historyWorker = newTerminalHistoryIngestQueue()
		terminal.setHistoryQueue(process, historyWorker)
		go historyWorker.Run(func(outputs []string) error {
			return terminal.ingestProcessHistoryOutputBatch(process, outputs)
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
			if historyWorker != nil {
				historyWorker.Enqueue(text)
			}
		}
	}()
	return done
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
	finishLiveWrite := perftrace.Measure("core.live.write_screen")
	surface.Write(output)
	finishLiveWrite(len(output))
	terminal.bumpLiveRevisionLocked()
	liveRevision := terminal.liveRevision
	terminal.liveMu.Unlock()

	terminal.mu.Lock()
	stillCurrent := terminal.process == process && terminal.info.State != TerminalStateExited && terminal.info.State != TerminalStateRemoved
	terminal.mu.Unlock()
	if !stillCurrent {
		return nil
	}
	perftrace.Count("core.terminal.changed", len(output))
	perftrace.Count("core.live.invalidation_publish", len(output))
	terminal.publishLiveInvalidated(info.ID, liveRevision)
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
	terminal.liveMu.Lock()
	terminal.live.Write(text)
	terminal.bumpLiveRevisionLocked()
	liveRevision := terminal.liveRevision
	terminal.liveMu.Unlock()
	terminal.publishLiveInvalidated(info.ID, liveRevision)
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

func (terminal *Terminal) ingestProcessHistoryOutputBatch(process TerminalProcess, outputs []string) error {
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
	terminal.ingestHistoryOutputBatch(outputs)
	return nil
}

func (terminal *Terminal) ingestHistoryOutputBatch(outputs []string) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled || terminal.historyRenderer == nil || terminal.historyStore == nil || terminal.historySemantic == nil {
		return
	}
	for _, output := range outputs {
		for _, semanticWrite := range splitTerminalHistorySemanticWrites(&terminal.historySemanticPending, output) {
			tx, err := terminal.historySemantic.ApplyPTYWrite([]byte(semanticWrite))
			if err != nil {
				continue
			}
			terminal.applyHistoryTransactionLocked(tx)
		}
	}
}

func (terminal *Terminal) applyHistoryResize(size Size) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled || terminal.historyRenderer == nil || terminal.historyStore == nil || terminal.historySemantic == nil {
		return
	}
	tx, err := terminal.historySemantic.Resize(vterm.TerminalSemanticSize{Cols: int(size.Cols), Rows: int(size.Rows)})
	if err != nil {
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

func (terminal *Terminal) resetHistorySemantic(size Size) {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	if !terminal.historyEnabled {
		return
	}
	// 中文说明：restart 是新 PTY process，但不是新 terminal identity。history store
	// 保留旧逻辑行；terminal semantic decoder 必须重建，避免旧程序 mode/pending CSI
	// 污染新进程后续输出。
	terminal.historySemantic = vterm.NewSemanticSource(int(size.Cols), int(size.Rows), 0, nil)
	terminal.historySemanticPending = ""
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
