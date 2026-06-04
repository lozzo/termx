package termxcorev2

import (
	"context"
	"errors"
	"io"
	"sync"
)

type ProcessFactory interface {
	Spawn(context.Context, ProcessSpec) (TerminalProcess, error)
}

type ProcessFactoryFunc func(context.Context, ProcessSpec) (TerminalProcess, error)

func (fn ProcessFactoryFunc) Spawn(ctx context.Context, spec ProcessSpec) (TerminalProcess, error) {
	return fn(ctx, spec)
}

type ProcessSpec struct {
	TerminalID string
	Command    []string
	Size       Size
}

type TerminalProcess interface {
	Input([]byte) error
	Resize(Size) error
	Kill() error
	Wait() <-chan ProcessExit
	Close() error
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
		waitCh: make(chan ProcessExit, 1),
	}
	return process, nil
}

type scriptedProcess struct {
	mu      sync.Mutex
	closed  bool
	waitCh  chan ProcessExit
	waited  bool
	inputs  [][]byte
	resizes []Size
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
