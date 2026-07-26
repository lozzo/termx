//go:build windows

package terminalhost

import (
	"os"
	"sync"
	"time"

	xterm "github.com/charmbracelet/x/term"
)

const windowsResizePollInterval = 200 * time.Millisecond

type windowsResizeSignal struct{}

func (windowsResizeSignal) String() string { return "windows-console-resize" }
func (windowsResizeSignal) Signal()        {}

func defaultResizeSignalFactory() (<-chan os.Signal, func()) {
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		ticker := time.NewTicker(windowsResizePollInterval)
		defer ticker.Stop()
		lastCols, lastRows, _ := xterm.GetSize(os.Stdin.Fd())
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				cols, rows, err := xterm.GetSize(os.Stdin.Fd())
				if err != nil || cols <= 0 || rows <= 0 || (cols == lastCols && rows == lastRows) {
					continue
				}
				lastCols, lastRows = cols, rows
				select {
				case signals <- windowsResizeSignal{}:
				default:
				}
			}
		}
	}()
	// 中文说明：Windows 控制台没有 SIGWINCH；尺寸真值由控制台 API 轮询，且只在变化时通知 Host。
	return signals, func() {
		stopOnce.Do(func() { close(done) })
	}
}
