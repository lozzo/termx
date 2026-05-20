package bridge

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestSendBlocksWhenBufferFull(t *testing.T) {
	dc := newMockDataChannel()
	dc.setBufferedAmount(sendBufferHigh + 1024)
	transport := newDataChannelTransport(dc)
	defer transport.Close()

	done := make(chan error, 1)
	go func() {
		done <- transport.Send([]byte("frame"))
	}()

	select {
	case err := <-done:
		t.Fatalf("Send returned while buffered amount was above high watermark: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if dc.sentCount() != 0 {
		t.Fatalf("Send should not call data channel while backpressured, got %d sends", dc.sentCount())
	}
	if dc.bufferedAmountLowThreshold() != sendBufferLow {
		t.Fatalf("expected low threshold %d, got %d", sendBufferLow, dc.bufferedAmountLowThreshold())
	}
	if !dc.hasBufferedAmountLowHandler() {
		t.Fatal("expected buffered amount low handler to be registered")
	}
}

func TestSendResumesAfterLowWatermark(t *testing.T) {
	dc := newMockDataChannel()
	dc.setBufferedAmount(sendBufferHigh + 1024)
	transport := newDataChannelTransport(dc)
	defer transport.Close()

	done := make(chan error, 1)
	go func() {
		done <- transport.Send([]byte("frame"))
	}()

	select {
	case err := <-done:
		t.Fatalf("Send returned before low-watermark notification: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	dc.setBufferedAmount(sendBufferLow)
	dc.emitBufferedAmountLow()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Send returned error after low-watermark notification: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not resume after low-watermark notification")
	}
	if dc.sentCount() != 1 {
		t.Fatalf("expected one frame send after drain, got %d", dc.sentCount())
	}
	if got := string(dc.sentFrame(0)); got != "frame" {
		t.Fatalf("unexpected frame payload %q", got)
	}
}

func TestSendIgnoresStaleLowWatermarkToken(t *testing.T) {
	dc := newMockDataChannel()
	transport := newDataChannelTransport(dc)
	defer transport.Close()
	dc.emitBufferedAmountLow()
	dc.setBufferedAmount(sendBufferHigh + 1024)

	done := make(chan error, 1)
	go func() {
		done <- transport.Send([]byte("frame"))
	}()

	select {
	case err := <-done:
		t.Fatalf("Send returned using stale low-watermark token while buffer remained high: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if dc.sentCount() != 0 {
		t.Fatalf("Send should not call data channel while buffer remains high, got %d sends", dc.sentCount())
	}

	dc.setBufferedAmount(sendBufferLow)
	dc.emitBufferedAmountLow()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Send returned error after real drain: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not resume after real low-watermark notification")
	}
}

func TestSendReturnsEOFWhenClosedWhileBlocked(t *testing.T) {
	dc := newMockDataChannel()
	dc.setBufferedAmount(sendBufferHigh + 1024)
	transport := newDataChannelTransport(dc)

	done := make(chan error, 1)
	go func() {
		done <- transport.Send([]byte("frame"))
	}()

	select {
	case err := <-done:
		t.Fatalf("Send returned before close while buffer was high: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("expected io.EOF after close while blocked, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not unblock after Close")
	}
	if dc.sentCount() != 0 {
		t.Fatalf("Send should not call data channel after blocked close, got %d sends", dc.sentCount())
	}
}

func TestRecvReturnsEOFWithoutClosingReceiveChannel(t *testing.T) {
	dc := newMockDataChannel()
	transport := newDataChannelTransport(dc)
	defer transport.Close()

	transport.recvCh <- []byte("queued")
	if err := transport.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if _, err := transport.Recv(); err != io.EOF {
		t.Fatalf("expected Recv to prefer done over queued frames after Close, got %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("recvCh should not be closed by Close, send panicked: %v", r)
		}
	}()
	transport.recvCh <- []byte("late")
}

func TestSendDoesNotRacePastClose(t *testing.T) {
	dc := newMockDataChannel()
	dc.blockSend = make(chan struct{})
	transport := newDataChannelTransport(dc)

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- transport.Send([]byte("frame"))
	}()
	<-started
	dc.waitForSendBlocked(t)

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- transport.Close()
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before in-flight Send released: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(dc.blockSend)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("in-flight Send returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight Send did not finish")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after in-flight Send released")
	}
	if err := transport.Send([]byte("after-close")); err != io.EOF {
		t.Fatalf("Send after Close should return io.EOF, got %v", err)
	}
}

type mockDataChannel struct {
	mu                  sync.Mutex
	bufferedAmount      uint64
	lowThreshold        uint64
	onBufferedAmountLow func()
	onMessage           func(webrtc.DataChannelMessage)
	onClose             func()
	blockSend           chan struct{}
	sendBlocked         chan struct{}
	sendCount           int
	sent                [][]byte
}

func newMockDataChannel() *mockDataChannel {
	return &mockDataChannel{sendBlocked: make(chan struct{})}
}

func (m *mockDataChannel) OnMessage(fn func(webrtc.DataChannelMessage)) {
	m.onMessage = fn
}

func (m *mockDataChannel) OnClose(fn func()) {
	m.onClose = fn
}

func (m *mockDataChannel) BufferedAmount() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bufferedAmount
}

func (m *mockDataChannel) SetBufferedAmountLowThreshold(th uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lowThreshold = th
}

func (m *mockDataChannel) OnBufferedAmountLow(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onBufferedAmountLow = fn
}

func (m *mockDataChannel) Send(frame []byte) error {
	m.mu.Lock()
	block := m.blockSend
	if block != nil {
		select {
		case <-m.sendBlocked:
		default:
			close(m.sendBlocked)
		}
	}
	m.mu.Unlock()
	if block != nil {
		<-block
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCount++
	m.sent = append(m.sent, append([]byte(nil), frame...))
	return nil
}

func (m *mockDataChannel) Close() error {
	return nil
}

func (m *mockDataChannel) emitBufferedAmountLow() {
	m.mu.Lock()
	fn := m.onBufferedAmountLow
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (m *mockDataChannel) setBufferedAmount(amount uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bufferedAmount = amount
}

func (m *mockDataChannel) sentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendCount
}

func (m *mockDataChannel) sentFrame(index int) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.sent[index]...)
}

func (m *mockDataChannel) bufferedAmountLowThreshold() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lowThreshold
}

func (m *mockDataChannel) hasBufferedAmountLowHandler() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.onBufferedAmountLow != nil
}

func (m *mockDataChannel) waitForSendBlocked(t *testing.T) {
	t.Helper()
	select {
	case <-m.sendBlocked:
	case <-time.After(time.Second):
		t.Fatal("mock data channel Send did not block")
	}
}
