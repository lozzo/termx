package termxcorev2

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

type Terminal struct {
	mu           sync.Mutex
	info         TerminalInfo
	options      TerminalCreateOptions
	process      TerminalProcess
	liveMu       sync.Mutex
	live         *live.SurfaceTrack
	historyMu    sync.Mutex
	history      *terminalHistoryPipeline
	historyClose func() error
	queueMu      sync.Mutex
	historyQ     *terminalHistoryIngestQueue
	events       *eventBroker
	update       func(TerminalInfo)
}

func newTerminal(info TerminalInfo, options TerminalCreateOptions, process TerminalProcess, events *eventBroker, update func(TerminalInfo), historyBackend history.StorageBackend) *Terminal {
	size := live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)}
	var historyClose func() error
	if closer, ok := historyBackend.(interface{ Close() error }); ok {
		historyClose = closer.Close
	}
	terminal := &Terminal{
		info:         info.Clone(),
		options:      cloneTerminalCreateOptions(options),
		process:      process,
		history:      newTerminalHistoryPipelineWithStorage(int(info.Size.Cols), int(info.Size.Rows), historyBackend),
		historyClose: historyClose,
		events:       events,
		update:       update,
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
	terminal.liveMu.Unlock()

	if err := terminal.historyPipeline().Ingest(output); err != nil {
		return err
	}
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
	terminal.live.Resize(live.SurfaceSize{Cols: int(size.Cols), Rows: int(size.Rows)})
	terminal.liveMu.Unlock()

	if err := terminal.historyPipeline().Resize(int(size.Cols), int(size.Rows), resizeHistoryEvent(oldSize, size)); err != nil {
		return err
	}
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
	terminal.mu.Lock()
	process := terminal.process
	terminal.info.State = TerminalStateRemoved
	info := terminal.info.Clone()
	closeHistory := terminal.historyClose
	terminal.historyClose = nil
	terminal.mu.Unlock()
	terminal.syncInfo(info)
	processErr := process.Close()
	if closeHistory != nil {
		if err := closeHistory(); err != nil && processErr == nil {
			return err
		}
	}
	return processErr
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
	if err := terminal.historyPipeline().ResetForRestart(); err != nil {
		_ = process.Close()
		terminal.mu.Lock()
		if terminal.process == process {
			terminal.process = old
			terminal.info = oldInfo
		}
		terminal.mu.Unlock()
		return err
	}
	terminal.queueMu.Lock()
	terminal.historyQ = nil
	terminal.queueMu.Unlock()
	terminal.liveMu.Lock()
	terminal.live.Resize(live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)})
	terminal.live.ResetForRestartPreservingScreen()
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

func (terminal *Terminal) LatestWindow(cols, rows int) (history.HistoryWindow, error) {
	return terminal.historyPipeline().LatestWindow(cols, rows)
}

func (terminal *Terminal) OlderWindow(cols, rows int, cursor history.HistoryCursor) (history.HistoryWindow, error) {
	return terminal.historyPipeline().OlderWindow(cols, rows, cursor)
}

func (terminal *Terminal) CommittedCursorValid(cols int, cursor history.HistoryCursor) bool {
	return terminal.historyPipeline().CommittedCursorValid(cols, cursor)
}

func (terminal *Terminal) FreezeSnapshot() history.FrozenSnapshot {
	return terminal.historyPipeline().FreezeSnapshot()
}

func (terminal *Terminal) FreezePinnedSnapshot() history.FrozenSnapshot {
	return terminal.historyPipeline().FreezePinnedSnapshot()
}

func (terminal *Terminal) FreezePinnedSnapshotAtGeneration(generation history.Generation) history.FrozenSnapshot {
	return terminal.historyPipeline().FreezePinnedSnapshotAtGeneration(generation)
}

func (terminal *Terminal) HistoryGeneration() history.Generation {
	return terminal.historyPipeline().Generation()
}

func (terminal *Terminal) RetainedHistoryLineCount() int {
	return terminal.historyPipeline().RetainedHistoryLineCount()
}

func (terminal *Terminal) FlushHistory(ctx context.Context) error {
	terminal.queueMu.Lock()
	queue := terminal.historyQ
	terminal.queueMu.Unlock()
	if queue == nil {
		return nil
	}
	// 中文说明：copy/history 冻结前只等待已经读入队列的历史输出追平；
	// 不持 terminal 主锁，避免 history worker 处理同批输出时反向等待自己。
	return queue.Flush(ctx)
}

func (terminal *Terminal) historyPipeline() *terminalHistoryPipeline {
	terminal.historyMu.Lock()
	defer terminal.historyMu.Unlock()
	return terminal.history
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
	historyQueue := newTerminalHistoryIngestQueue()
	terminal.setHistoryQueue(process, historyQueue)
	go liveQueue.Run(func(output string) error {
		return terminal.ingestProcessLiveOutput(process, output)
	})
	go historyQueue.Run(func(outputs []string) error {
		return terminal.ingestProcessHistoryOutputBatch(process, outputs)
	})
	go func() {
		defer close(done)
		defer func() {
			liveQueue.Close()
			liveQueue.Wait()
			historyQueue.Close()
			historyQueue.Wait()
			terminal.clearHistoryQueue(process, historyQueue)
		}()
		for chunk := range output {
			if len(chunk) == 0 {
				continue
			}
			text := string(chunk)
			liveQueue.Enqueue(text)
			historyQueue.Enqueue(text)
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
	surface.Write(output)
	terminal.liveMu.Unlock()

	terminal.mu.Lock()
	stillCurrent := terminal.process == process && terminal.info.State != TerminalStateExited && terminal.info.State != TerminalStateRemoved
	terminal.mu.Unlock()
	if !stillCurrent {
		return nil
	}
	terminal.publish(EventTerminalChanged, info)
	return nil
}

func (terminal *Terminal) ingestProcessHistoryOutput(process TerminalProcess, output string) error {
	return terminal.ingestProcessHistoryOutputBatch(process, []string{output})
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
	terminal.historyMu.Lock()
	pipeline := terminal.history
	terminal.historyMu.Unlock()
	terminal.mu.Unlock()
	return pipeline.IngestBatch(outputs)
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
	_ = terminal.historyPipeline().ForceCommitFrontier()
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
	terminal.liveMu.Unlock()
	_ = terminal.historyPipeline().AppendSystemLines(lines)
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

func historyCellTextWidthForTerminal(text string) int {
	width := xansi.StringWidth(text)
	if width < 0 {
		return 0
	}
	return width
}

func (terminal *Terminal) syncInfo(info TerminalInfo) {
	if terminal.update != nil {
		terminal.update(info)
	}
}

func resizeHistoryEvent(oldSize Size, newSize Size) history.HistoryEvent {
	event := history.HistoryEvent{Kind: history.EventResize, ResizeDirection: history.ResizeSame}
	switch {
	case newSize.Rows > oldSize.Rows:
		event.ResizeDirection = history.ResizeGrow
		event.Count = int(newSize.Rows - oldSize.Rows)
	case newSize.Rows < oldSize.Rows:
		event.ResizeDirection = history.ResizeShrink
		event.Count = int(oldSize.Rows - newSize.Rows)
	}
	return event
}
