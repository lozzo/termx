package termxcorev2

import (
	"context"
	"strings"
	"sync"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
)

type Terminal struct {
	mu      sync.Mutex
	info    TerminalInfo
	process TerminalProcess
	history *history.HistoryTrack
	live    *live.SurfaceTrack
	events  *eventBroker
	update  func(TerminalInfo)
}

func newTerminal(info TerminalInfo, process TerminalProcess, events *eventBroker, update func(TerminalInfo)) *Terminal {
	size := live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)}
	terminal := &Terminal{
		info:    info.Clone(),
		process: process,
		history: history.NewHistoryTrack(),
		live:    live.NewSurfaceTrack(size),
		events:  events,
		update:  update,
	}
	terminal.watchExit(process)
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
	terminal.mu.Lock()
	if terminal.info.State == TerminalStateExited || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return ErrTerminalExited
	}
	terminal.live.Write(output)
	for _, part := range strings.SplitAfter(output, "\n") {
		if part == "" {
			continue
		}
		if strings.HasSuffix(part, "\n") {
			text := strings.TrimSuffix(part, "\n")
			if text != "" {
				if err := terminal.history.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{{Text: text}}}); err != nil {
					terminal.mu.Unlock()
					return err
				}
			}
			if err := terminal.history.Apply(history.HistoryEvent{Kind: history.EventSealLogicalLine}); err != nil {
				terminal.mu.Unlock()
				return err
			}
			if err := terminal.history.Apply(history.HistoryEvent{Kind: history.EventCommitFrontier}); err != nil {
				terminal.mu.Unlock()
				return err
			}
			continue
		}
		if err := terminal.history.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{{Text: part}}}); err != nil {
			terminal.mu.Unlock()
			return err
		}
	}
	info := terminal.info.Clone()
	terminal.mu.Unlock()
	terminal.publish(EventTerminalChanged, info)
	return nil
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
	terminal.live.Resize(live.SurfaceSize{Cols: int(size.Cols), Rows: int(size.Rows)})
	err := terminal.history.Apply(resizeHistoryEvent(oldSize, size))
	info := terminal.info.Clone()
	terminal.mu.Unlock()
	if err != nil {
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
	info = terminal.info.Clone()
	terminal.history = history.NewHistoryTrack()
	terminal.live = live.NewSurfaceTrack(live.SurfaceSize{Cols: int(info.Size.Cols), Rows: int(info.Size.Rows)})
	terminal.mu.Unlock()
	_ = old.Close()
	terminal.syncInfo(info)
	terminal.watchExit(process)
	terminal.publish(EventTerminalChanged, info)
	return nil
}

func (terminal *Terminal) Wait() <-chan ProcessExit {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.process.Wait()
}

func (terminal *Terminal) LiveRows() []string {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.live.Rows()
}

func (terminal *Terminal) LatestWindow(cols, rows int) (history.HistoryWindow, error) {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.history.LatestWindow(history.HistoryWindowRequest{Cols: cols, Rows: rows})
}

func (terminal *Terminal) OlderWindow(cols, rows int, cursor history.HistoryCursor) (history.HistoryWindow, error) {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.history.OlderWindow(history.HistoryWindowRequest{Cols: cols, Rows: rows, Cursor: cursor})
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

func (terminal *Terminal) watchExit(process TerminalProcess) {
	go func() {
		exit, ok := <-process.Wait()
		if !ok {
			return
		}
		terminal.markExited(process, exit)
	}()
}

func (terminal *Terminal) markExited(process TerminalProcess, exit ProcessExit) {
	terminal.mu.Lock()
	if terminal.process != process || terminal.info.State == TerminalStateRemoved {
		terminal.mu.Unlock()
		return
	}
	_ = terminal.history.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier})
	terminal.info.State = TerminalStateExited
	code := exit.Code
	terminal.info.ExitCode = &code
	info := terminal.info.Clone()
	terminal.mu.Unlock()
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
