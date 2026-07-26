//go:build !windows

package core

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	crosspty "github.com/aymanbagabas/go-pty"
)

type ptyProcessPlatform interface {
	Kill() error
	ResourceUsage() (TerminalResourceUsage, bool)
	ProcessExited() error
	OutputDrained() error
	Close() error
}

type unixPTYProcessPlatform struct {
	process   *os.Process
	terminal  crosspty.Pty
	closeOnce sync.Once
	closeErr  error
}

func newPTYProcessPlatform(process *os.Process, terminal crosspty.Pty) (ptyProcessPlatform, error) {
	return &unixPTYProcessPlatform{process: process, terminal: terminal}, nil
}

func (platform *unixPTYProcessPlatform) Kill() error {
	if platform == nil || platform.process == nil {
		return nil
	}
	// 中文说明：Unix PTY command 由跨平台 PTY owner 建立独立 session；终止必须覆盖整个进程组。
	if err := syscall.Kill(-platform.process.Pid, syscall.SIGHUP); err != nil {
		if signalErr := platform.process.Signal(syscall.SIGHUP); signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
			return errors.Join(err, signalErr)
		}
	}
	return nil
}

func (platform *unixPTYProcessPlatform) ResourceUsage() (TerminalResourceUsage, bool) {
	if platform == nil || platform.process == nil || platform.process.Pid <= 0 {
		return TerminalResourceUsage{}, false
	}
	pid := platform.process.Pid
	// 中文说明：资源展示只做 Terminal Manager 诊断采样，真值仍归 OS 进程；采样失败不修改 lifecycle。
	out, err := exec.Command("ps", "-o", "%cpu=,rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return TerminalResourceUsage{}, false
	}
	return parseProcessResourceUsage(pid, out, time.Now().UTC())
}

func (*unixPTYProcessPlatform) ProcessExited() error { return nil }

func (platform *unixPTYProcessPlatform) OutputDrained() error { return platform.closeTerminal() }

func (platform *unixPTYProcessPlatform) Close() error { return platform.closeTerminal() }

func (platform *unixPTYProcessPlatform) closeTerminal() error {
	if platform == nil {
		return nil
	}
	platform.closeOnce.Do(func() {
		if platform.terminal != nil {
			platform.closeErr = platform.terminal.Close()
		}
	})
	return platform.closeErr
}
