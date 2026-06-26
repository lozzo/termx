package termxcorev2

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

type Terminal struct {
	mu      sync.Mutex
	info    TerminalInfo
	options TerminalCreateOptions
	process TerminalProcess
	liveMu  sync.Mutex
	live    *live.SurfaceTrack
	queueMu sync.Mutex
	liveQ   *terminalLiveIngestQueue
	events  *eventBroker
	update  func(TerminalInfo)
}

func newTerminal(info TerminalInfo, options TerminalCreateOptions, process TerminalProcess, events *eventBroker, update func(TerminalInfo)) *Terminal {
	size := live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)}
	terminal := &Terminal{
		info:    info.Clone(),
		options: cloneTerminalCreateOptions(options),
		process: process,
		events:  events,
		update:  update,
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
	result := surface.WriteWithResult(rawOutput)
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
	terminal.liveMu.Unlock()
	terminal.applyHistoryWriteResult(result)

	terminal.mu.Lock()
	stillCurrent := terminal.process == process && terminal.info.State != TerminalStateExited && terminal.info.State != TerminalStateRemoved
	terminal.mu.Unlock()
	if !stillCurrent {
		return nil
	}
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
	terminal.liveMu.Unlock()
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
	// 中文说明：R319 已删除旧补丁式 projector/store。新的 logical renderer
	// 接入前，这里必须显式失败，不能返回旧错误模型拼出来的 history window。
	_ = req
	return history.HistoryWindow{}, ErrHistoryNotRebuilt
}

func (terminal *Terminal) HistoryCopy(req history.HistoryCopyRequest) (string, error) {
	// 中文说明：copy/search 只能从新 HistoryStore 的 authoritative frozen
	// projection 读取；旧 store 清场后不允许 TUI 或 live surface fallback。
	_ = req
	return "", ErrHistoryNotRebuilt
}

func (terminal *Terminal) HistoryFreeze(req history.FreezeHistoryRequest) (history.FrozenHistorySnapshot, error) {
	// 中文说明：freeze 必须冻结新 renderer 的 mutable frame projection。旧
	// current frame store 已删除，因此这里明确返回未重建。
	_ = req
	return history.FrozenHistorySnapshot{}, ErrHistoryNotRebuilt
}

// HistoryRelease 释放 terminal history store 中的 core-owned token。R319 清场后
// history store 尚未重建，因此该入口明确返回 ErrHistoryNotRebuilt，防止旧 token
// 生命周期或 TUI 本地缓存被误当作 authoritative history。
func (terminal *Terminal) HistoryRelease(token history.HistoryToken) error {
	_ = token
	return ErrHistoryNotRebuilt
}

func (terminal *Terminal) applyHistoryWriteResult(result live.SurfaceWriteResult) {
	// 中文说明：live surface 仍产出 vterm semantic transaction，但 R319 已清掉
	// 旧 projector/store。后续切片必须把 result.Segments 接入新的
	// HistoryLogicalRenderer，不能在这里恢复 raw parser 或旧内存 store。
	_ = result
}

func (terminal *Terminal) applyHistoryResizeTransaction(tx history.TerminalSemanticTransaction) {
	// 中文说明：resize 仍来自 vterm semantic transaction。新 renderer 接入前不
	// 允许从 resized live snapshot 生成 history 或改写 sealed records。
	_ = tx
}

func (terminal *Terminal) forceCloseHistory(reason history.CloseReason) {
	// 中文说明：process exit 是 lifecycle boundary，后续必须走
	// HistoryLogicalRenderer.Close。旧 ForceClose/projector 已删除，不能伪造成
	// PTY bytes 或 fallback 到旧 store。
	_ = reason
}

func (terminal *Terminal) syncInfo(info TerminalInfo) {
	if terminal.update != nil {
		terminal.update(info)
	}
}
