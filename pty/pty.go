package pty

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	creackpty "github.com/creack/pty"
)

type Size struct {
	Cols uint16
	Rows uint16
}

type SpawnOptions struct {
	Command    []string
	Dir        string
	Env        []string
	Size       Size
	TerminalID string
}

type PTY struct {
	file *os.File
	cmd  *exec.Cmd

	done          chan struct{}
	waitOnce      sync.Once
	closeOnce     sync.Once
	killOnce      sync.Once
	waitErr       error
	exitCode      int
	killRequested atomic.Bool
}

func Spawn(opts SpawnOptions) (*PTY, error) {
	cmd := exec.Command(opts.Command[0], opts.Command[1:]...)
	cmd.Dir = opts.Dir
	cmd.Env = mergedEnv(opts.TerminalID, opts.Env)

	size := &creackpty.Winsize{Cols: opts.Size.Cols, Rows: opts.Size.Rows}
	file, err := creackpty.StartWithSize(cmd, size)
	if err != nil {
		return nil, err
	}

	p := &PTY{
		file: file,
		cmd:  cmd,
		done: make(chan struct{}),
	}
	go p.wait()
	return p, nil
}

func (p *PTY) Read(buf []byte) (int, error) {
	return p.file.Read(buf)
}

func (p *PTY) Write(data []byte) (int, error) {
	return p.file.Write(data)
}

func (p *PTY) Resize(cols, rows uint16) error {
	return creackpty.Setsize(p.file, &creackpty.Winsize{Cols: cols, Rows: rows})
}

func (p *PTY) Kill() error {
	var err error
	p.killOnce.Do(func() {
		p.killRequested.Store(true)
		pid := p.cmd.Process.Pid
		err = syscall.Kill(-pid, syscall.SIGHUP)
		if p.waitFor(500 * time.Millisecond) {
			return
		}
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		if p.waitFor(2 * time.Second) {
			return
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-p.done
	})
	return err
}

func (p *PTY) Wait() <-chan struct{} {
	return p.done
}

func (p *PTY) ExitCode() int {
	<-p.done
	return p.exitCode
}

func (p *PTY) Close() error {
	var err error
	p.closeOnce.Do(func() {
		select {
		case <-p.done:
		default:
			_ = p.Kill()
		}
		err = p.file.Close()
	})
	return err
}

func (p *PTY) wait() {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		if p.waitErr != nil {
			var exitErr *exec.ExitError
			if errors.As(p.waitErr, &exitErr) {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					if p.killRequested.Load() && status.Signaled() {
						p.exitCode = -1
					} else if status.Exited() {
						p.exitCode = status.ExitStatus()
					} else if status.Signaled() {
						p.exitCode = -1
					}
				}
			}
		} else if p.cmd.ProcessState != nil {
			p.exitCode = p.cmd.ProcessState.ExitCode()
		}
		close(p.done)
	})
}

func (p *PTY) waitFor(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return true
	case <-timer.C:
		return false
	}
}

func mergedEnv(id string, extra []string) []string {
	env := os.Environ()
	env = append(env,
		"TERM=xterm-256color",
		"TERMX=1",
		"TERMX_TERMINAL_ID="+id,
	)
	env = append(env, extra...)
	return env
}
