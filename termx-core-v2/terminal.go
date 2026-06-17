package termxcorev2

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
)

type Terminal struct {
	mu        sync.Mutex
	info      TerminalInfo
	process   TerminalProcess
	liveMu    sync.Mutex
	live      *live.SurfaceTrack
	historyMu sync.Mutex
	history   *terminalHistoryPipeline
	queueMu   sync.Mutex
	historyQ  *terminalHistoryIngestQueue
	events    *eventBroker
	update    func(TerminalInfo)
}

func newTerminal(info TerminalInfo, process TerminalProcess, events *eventBroker, update func(TerminalInfo)) *Terminal {
	size := live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)}
	terminal := &Terminal{
		info:    info.Clone(),
		process: process,
		live:    live.NewSurfaceTrack(size),
		history: newTerminalHistoryPipeline(int(info.Size.Cols), int(info.Size.Rows)),
		events:  events,
		update:  update,
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
	terminal.mu.Unlock()
	terminal.syncInfo(info)
	return process.Close()
}

func (terminal *Terminal) Restart(ctx context.Context, factory ProcessFactory) error {
	terminal.mu.Lock()
	info := terminal.info.Clone()
	terminal.mu.Unlock()
	process, err := factory.Spawn(ctx, ProcessSpec{TerminalID: info.ID, Command: info.Command, Size: info.Size})
	if err != nil {
		return err
	}
	terminal.mu.Lock()
	old := terminal.process
	terminal.process = process
	terminal.info.State = TerminalStateRunning
	terminal.info.ExitCode = nil
	terminal.info.ExitedAt = time.Time{}
	info = terminal.info.Clone()
	terminal.mu.Unlock()
	terminal.liveMu.Lock()
	terminal.live = live.NewSurfaceTrack(live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)})
	terminal.liveMu.Unlock()
	terminal.resetHistoryPipeline(int(info.Size.Cols), int(info.Size.Rows))
	_ = old.Close()
	terminal.syncInfo(info)
	terminal.watchProcess(process)
	terminal.publish(EventTerminalChanged, info)
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

func (terminal *Terminal) resetHistoryPipeline(cols int, rows int) {
	terminal.historyMu.Lock()
	terminal.history = newTerminalHistoryPipeline(cols, rows)
	terminal.historyMu.Unlock()
	terminal.queueMu.Lock()
	terminal.historyQ = nil
	terminal.queueMu.Unlock()
}

func (terminal *Terminal) publish(typ EventType, info TerminalInfo) {
	if terminal.events == nil {
		return
	}
	terminalCopy := info.Clone()
	terminal.events.publish(Event{
		Type:       typ,
		TerminalID: info.ID,
		Terminal:   &terminalCopy,
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
	go historyQueue.Run(func(output string) error {
		return terminal.ingestProcessHistoryOutput(process, output)
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
	return pipeline.Ingest(output)
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
	terminal.syncInfo(info)
	terminal.publish(EventTerminalExited, info)
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
