package core

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	creackpty "github.com/creack/pty"
	"github.com/lozzow/termx/shared/perftrace"
)

type ProcessFactory interface {
	Spawn(context.Context, ProcessSpec) (TerminalProcess, error)
}

type ProcessFactoryFunc func(context.Context, ProcessSpec) (TerminalProcess, error)

func (fn ProcessFactoryFunc) Spawn(ctx context.Context, spec ProcessSpec) (TerminalProcess, error) {
	return fn(ctx, spec)
}

type ProcessSpec struct {
	TerminalID         string
	Command            []string
	Size               Size
	Dir                string
	Env                []string
	ScrollbackSize     int
	ScrollbackMaxBytes int64
	ScrollbackMaxAge   time.Duration
}

func cloneProcessSpec(spec ProcessSpec) ProcessSpec {
	spec.Command = append([]string(nil), spec.Command...)
	spec.Env = append([]string(nil), spec.Env...)
	return spec
}

type TerminalProcess interface {
	Input([]byte) error
	Resize(Size) error
	Output() <-chan []byte
	Kill() error
	Wait() <-chan ProcessExit
	Close() error
}

type terminalProcessResourceSampler interface {
	ResourceUsage() (TerminalResourceUsage, bool)
}

// ptyProcessFactory 是 core-v2 真实 terminal process 边界；外部客户端仍只通过 protocol/socket 访问。
type ptyProcessFactory struct{}

func newPTYProcessFactory() ProcessFactory {
	return ptyProcessFactory{}
}

func (ptyProcessFactory) Spawn(ctx context.Context, spec ProcessSpec) (TerminalProcess, error) {
	finishTotal := perftrace.Measure("core.process.spawn.total")
	defer finishTotal(0)
	if len(spec.Command) == 0 {
		return nil, ErrInvalidCommand
	}
	size := spec.Size
	if !size.Valid() {
		size = Size{Cols: 80, Rows: 24}
	}
	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = ptyProcessEnv(spec.TerminalID, spec.Env)
	finishStart := perftrace.Measure("core.process.pty_start")
	file, err := creackpty.StartWithSize(cmd, &creackpty.Winsize{Cols: size.Cols, Rows: size.Rows})
	finishStart(0)
	if err != nil {
		return nil, err
	}
	process := &ptyProcess{
		file:     file,
		cmd:      cmd,
		outputCh: make(chan []byte, 64),
		waitCh:   make(chan ProcessExit, 1),
		readDone: make(chan struct{}),
	}
	go process.readLoop()
	go process.waitLoop()
	return process, nil
}

type ptyProcess struct {
	mu            sync.Mutex
	file          *os.File
	cmd           *exec.Cmd
	outputCh      chan []byte
	waitCh        chan ProcessExit
	readDone      chan struct{}
	closeOnce     sync.Once
	waitOnce      sync.Once
	killRequested atomic.Bool
}

const ptyReadBufferBytes = 64 * 1024

func (process *ptyProcess) Input(data []byte) error {
	process.mu.Lock()
	file := process.file
	process.mu.Unlock()
	if file == nil {
		return io.ErrClosedPipe
	}
	_, err := file.Write(data)
	return err
}

func (process *ptyProcess) Resize(size Size) error {
	if !size.Valid() {
		return ErrInvalidServerSize
	}
	process.mu.Lock()
	file := process.file
	process.mu.Unlock()
	if file == nil {
		return io.ErrClosedPipe
	}
	return creackpty.Setsize(file, &creackpty.Winsize{Cols: size.Cols, Rows: size.Rows})
}

func (process *ptyProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *ptyProcess) ResourceUsage() (TerminalResourceUsage, bool) {
	process.mu.Lock()
	cmd := process.cmd
	process.mu.Unlock()
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return TerminalResourceUsage{}, false
	}
	pid := cmd.Process.Pid
	// 中文说明：资源展示只做 Terminal Manager 诊断采样，真值仍归 OS 进程；
	// 采样失败时返回空值，不修改 terminal lifecycle，也不做状态 fallback。
	out, err := exec.Command("ps", "-o", "%cpu=,rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return TerminalResourceUsage{}, false
	}
	return parseProcessResourceUsage(pid, out, time.Now().UTC())
}

func (process *ptyProcess) Kill() error {
	process.mu.Lock()
	cmd := process.cmd
	process.killRequested.Store(true)
	process.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// creack/pty 会让子进程成为独立 session；优先给进程组发 HUP，失败时回退到主进程。
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP); err != nil {
		if signalErr := cmd.Process.Signal(syscall.SIGHUP); signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
			return errors.Join(err, signalErr)
		}
	}
	return nil
}

func (process *ptyProcess) Wait() <-chan ProcessExit {
	return process.waitCh
}

func (process *ptyProcess) Close() error {
	var err error
	process.closeOnce.Do(func() {
		_ = process.Kill()
		process.mu.Lock()
		file := process.file
		process.file = nil
		process.mu.Unlock()
		if file != nil {
			err = file.Close()
		}
	})
	return err
}

func (process *ptyProcess) readLoop() {
	defer close(process.outputCh)
	defer close(process.readDone)
	buf := make([]byte, ptyReadBufferBytes)
	for {
		process.mu.Lock()
		file := process.file
		process.mu.Unlock()
		if file == nil {
			return
		}
		n, err := file.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			process.outputCh <- chunk
		}
		if err != nil {
			return
		}
	}
}

func (process *ptyProcess) waitLoop() {
	process.waitOnce.Do(func() {
		err := process.cmd.Wait()
		<-process.readDone
		process.waitCh <- ProcessExit{
			Code: processExitCode(err, process.killRequested.Load()),
			Err:  err,
		}
		close(process.waitCh)
	})
}

func processExitCode(err error, killed bool) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if killed && status.Signaled() {
				return -1
			}
			if status.Exited() {
				return status.ExitStatus()
			}
			if status.Signaled() {
				return -1
			}
		}
	}
	return -1
}

func parseProcessResourceUsage(pid int, output []byte, sampledAt time.Time) (TerminalResourceUsage, bool) {
	if pid <= 0 {
		return TerminalResourceUsage{}, false
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 {
		return TerminalResourceUsage{}, false
	}
	cpu, err := strconv.ParseFloat(strings.TrimSuffix(fields[0], "%"), 64)
	if err != nil {
		return TerminalResourceUsage{}, false
	}
	rssKB, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return TerminalResourceUsage{}, false
	}
	if cpu < 0 {
		cpu = 0
	}
	return TerminalResourceUsage{
		PID:            pid,
		CPUPercentX100: int(cpu*100 + 0.5),
		MemoryBytes:    rssKB * 1024,
		SampledAt:      sampledAt,
	}, true
}

func ptyProcessEnv(id string, extra []string) []string {
	env := os.Environ()
	env = append(env,
		"TERM=xterm-256color",
		"TERMX=1",
		"TERMX_TERMINAL_ID="+id,
	)
	return append(env, extra...)
}

type ProcessExit struct {
	Code int
	Err  error
}

type scriptedProcessFactory struct{}

func newScriptedProcessFactory() ProcessFactory {
	return scriptedProcessFactory{}
}

func (scriptedProcessFactory) Spawn(_ context.Context, spec ProcessSpec) (TerminalProcess, error) {
	if len(spec.Command) == 0 {
		return nil, ErrInvalidCommand
	}
	process := &scriptedProcess{
		outputCh: make(chan []byte),
		waitCh:   make(chan ProcessExit, 1),
	}
	close(process.outputCh)
	return process, nil
}

type scriptedProcess struct {
	mu       sync.Mutex
	closed   bool
	waitCh   chan ProcessExit
	waited   bool
	inputs   [][]byte
	resizes  []Size
	outputCh chan []byte
}

func (process *scriptedProcess) Input(data []byte) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return io.ErrClosedPipe
	}
	process.inputs = append(process.inputs, append([]byte(nil), data...))
	return nil
}

func (process *scriptedProcess) Resize(size Size) error {
	if !size.Valid() {
		return ErrInvalidServerSize
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return io.ErrClosedPipe
	}
	process.resizes = append(process.resizes, size)
	return nil
}

func (process *scriptedProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *scriptedProcess) Kill() error {
	process.mu.Lock()
	if process.closed {
		process.mu.Unlock()
		return nil
	}
	process.closed = true
	process.mu.Unlock()
	process.finish(ProcessExit{Code: -1, Err: errors.New("process killed")})
	return nil
}

func (process *scriptedProcess) Wait() <-chan ProcessExit {
	return process.waitCh
}

func (process *scriptedProcess) Close() error {
	return process.Kill()
}

func (process *scriptedProcess) finish(exit ProcessExit) {
	process.mu.Lock()
	if process.waited {
		process.mu.Unlock()
		return
	}
	process.waited = true
	process.mu.Unlock()
	process.waitCh <- exit
	close(process.waitCh)
}
