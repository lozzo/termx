package termxcorev2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport/memory"
)

func newProtocolClient(t *testing.T) (*Server, *protocol.Client, func()) {
	t.Helper()
	return newProtocolClientWithServer(t, NewServer(WithProcessFactory(newRecordingProcessFactory())))
}

func newProtocolClientWithServer(t *testing.T, server *Server) (*Server, *protocol.Client, func()) {
	t.Helper()
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeTransport(context.Background(), serverTransport)
	}()
	client := protocol.NewClient(clientTransport)
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "core-v2-test"}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	closeClient := func() {
		_ = client.Close()
		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("server transport returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("server transport did not stop")
		}
	}
	return server, client, closeClient
}

type recordingProcessFactory struct {
	processes []*recordingProcess
}

func newRecordingProcessFactory() *recordingProcessFactory {
	return &recordingProcessFactory{}
}

func (factory *recordingProcessFactory) Spawn(_ context.Context, spec ProcessSpec) (TerminalProcess, error) {
	process := newRecordingProcess(spec)
	factory.processes = append(factory.processes, process)
	return process, nil
}

type recordingProcess struct {
	spec   ProcessSpec
	inputs [][]byte
	size   Size
	output chan []byte
	wait   chan ProcessExit
	closed bool
}

func newRecordingProcess(spec ProcessSpec) *recordingProcess {
	return &recordingProcess{
		spec:   cloneProcessSpec(spec),
		size:   spec.Size,
		output: make(chan []byte, 16),
		wait:   make(chan ProcessExit, 1),
	}
}

func (process *recordingProcess) Input(data []byte) error {
	if process.closed {
		return errors.New("process closed")
	}
	process.inputs = append(process.inputs, append([]byte(nil), data...))
	return nil
}

func (process *recordingProcess) Resize(size Size) error {
	if !size.Valid() {
		return ErrInvalidServerSize
	}
	process.size = size
	return nil
}

func (process *recordingProcess) Output() <-chan []byte {
	return process.output
}

func (process *recordingProcess) Kill() error {
	process.wait <- ProcessExit{Code: 0}
	return nil
}

func (process *recordingProcess) Wait() <-chan ProcessExit {
	return process.wait
}

func (process *recordingProcess) Close() error {
	if !process.closed {
		process.closed = true
		close(process.output)
	}
	return nil
}

func requireProtocolEvent(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protocol event")
		return protocol.Event{}
	}
}
