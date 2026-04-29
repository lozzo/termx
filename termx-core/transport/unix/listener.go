package unix

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/lozzow/termx/termx-core/transport"
)

type Transport struct {
	conn net.Conn
	done chan struct{}
	once sync.Once

	sendMu sync.Mutex
	zstdW  *zstd.Encoder
	zstdR  *zstd.Decoder

	sendQ chan sendRequest
	wg    sync.WaitGroup
}

type sendRequest struct {
	frame []byte
	done  chan error
}

const (
	packetKindFrame byte = iota
	packetKindFragmentStart
	packetKindFragmentContinue
	packetKindFragmentEnd
)

const (
	maxPacketPayloadSize = 64 << 10
	sendBatchWindow      = time.Millisecond
	maxBatchedBytes      = 128 << 10
)

func Dial(path string) (*Transport, error) {
	actualPath, _ := resolveSocketPath(path)
	conn, err := net.Dial("unix", actualPath)
	if err != nil {
		return nil, err
	}
	return newTransport(conn)
}

func (t *Transport) Send(frame []byte) error {
	if t == nil || t.zstdW == nil || t.sendQ == nil {
		return io.EOF
	}
	req := sendRequest{
		frame: append([]byte(nil), frame...),
		done:  make(chan error, 1),
	}
	select {
	case <-t.done:
		return io.EOF
	case t.sendQ <- req:
	}
	select {
	case <-t.done:
		return io.EOF
	case err := <-req.done:
		return err
	}
}

func (t *Transport) Recv() ([]byte, error) {
	if t == nil || t.zstdR == nil {
		return nil, io.EOF
	}
	for {
		kind, payload, err := t.readPacket()
		if err != nil {
			return nil, err
		}
		switch kind {
		case packetKindFrame:
			return payload, nil
		case packetKindFragmentStart:
			buf := append([]byte(nil), payload...)
			for {
				nextKind, nextPayload, err := t.readPacket()
				if err != nil {
					return nil, err
				}
				switch nextKind {
				case packetKindFragmentContinue:
					buf = append(buf, nextPayload...)
				case packetKindFragmentEnd:
					buf = append(buf, nextPayload...)
					return buf, nil
				default:
					return nil, fmt.Errorf("transport/unix: unexpected packet kind %d during fragmented frame", nextKind)
				}
			}
		default:
			return nil, fmt.Errorf("transport/unix: unexpected packet kind %d", kind)
		}
	}
}

func (t *Transport) Close() error {
	t.once.Do(func() {
		close(t.done)
		t.wg.Wait()
		_ = t.conn.Close()
		t.sendMu.Lock()
		if t.zstdW != nil {
			_ = t.zstdW.Close()
		}
		t.sendMu.Unlock()
	})
	return nil
}

func (t *Transport) Done() <-chan struct{} {
	return t.done
}

type Listener struct {
	path       string
	actualPath string
	aliasPath  string
	ln         net.Listener
}

func NewListener(path string) (*Listener, error) {
	actualPath, aliasPath := resolveSocketPath(path)
	_ = os.Remove(path)
	if actualPath != path {
		_ = os.Remove(actualPath)
	}
	ln, err := net.Listen("unix", actualPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(actualPath, 0600); err != nil {
		_ = ln.Close()
		_ = os.Remove(actualPath)
		return nil, err
	}
	if aliasPath != "" {
		if err := os.MkdirAll(filepath.Dir(aliasPath), 0o755); err != nil {
			_ = ln.Close()
			_ = os.Remove(actualPath)
			return nil, err
		}
		if err := os.Symlink(actualPath, aliasPath); err != nil {
			_ = ln.Close()
			_ = os.Remove(actualPath)
			return nil, err
		}
	}
	return &Listener{path: path, actualPath: actualPath, aliasPath: aliasPath, ln: ln}, nil
}

func (l *Listener) Accept(ctx context.Context) (transport.Transport, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		conn, err := l.ln.Accept()
		resCh <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return nil, transport.ErrListenerClosed
		}
		return newTransport(res.conn)
	}
}

func (l *Listener) Close() error {
	err := l.ln.Close()
	if l.aliasPath != "" {
		_ = os.Remove(l.aliasPath)
	}
	_ = os.Remove(l.actualPath)
	if l.actualPath != l.path {
		_ = os.Remove(l.path)
	}
	return err
}

func (l *Listener) Addr() string {
	return l.path
}

func resolveSocketPath(path string) (string, string) {
	if len(path) <= maxSocketPathBytes() {
		return path, ""
	}
	sum := sha256.Sum256([]byte(path))
	actual := filepath.Join(shortSocketBaseDir(), fmt.Sprintf("termx-%x.sock", sum[:8]))
	return actual, path
}

func shortSocketBaseDir() string {
	if runtime.GOOS == "darwin" {
		return "/tmp"
	}
	return os.TempDir()
}

func maxSocketPathBytes() int {
	return len(syscall.RawSockaddrUnix{}.Path) - 1
}

func newTransport(conn net.Conn) (*Transport, error) {
	if conn == nil {
		return nil, io.EOF
	}
	writer, err := zstd.NewWriter(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader, err := zstd.NewReader(conn)
	if err != nil {
		_ = writer.Close()
		_ = conn.Close()
		return nil, err
	}
	t := &Transport{
		conn:  conn,
		done:  make(chan struct{}),
		zstdW: writer,
		zstdR: reader,
		sendQ: make(chan sendRequest, 256),
	}
	t.wg.Add(1)
	go t.runSender()
	return t, nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func (t *Transport) readPacket() (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(t.zstdR, header[:]); err != nil {
		return 0, nil, err
	}
	kind := header[0]
	n := binary.BigEndian.Uint32(header[1:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(t.zstdR, buf); err != nil {
		return 0, nil, err
	}
	return kind, buf, nil
}

func (t *Transport) runSender() {
	defer t.wg.Done()
	var (
		batch            []sendRequest
		batchedByteCount int
		timer            *time.Timer
		timerCh          <-chan time.Time
	)
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		err := t.writeBatch(batch)
		for _, req := range batch {
			req.done <- err
		}
		batch = batch[:0]
		batchedByteCount = 0
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timerCh = nil
		return err == nil
	}
	for {
		select {
		case req, ok := <-t.sendQ:
			if !ok {
				_ = flush()
				return
			}
			batch = append(batch, req)
			batchedByteCount += len(req.frame)
			if timer == nil {
				timer = time.NewTimer(sendBatchWindow)
			} else {
				timer.Reset(sendBatchWindow)
			}
			timerCh = timer.C
			if batchedByteCount >= maxBatchedBytes {
				if !flush() {
					return
				}
			}
		case <-timerCh:
			if !flush() {
				return
			}
		case <-t.done:
			_ = flush()
			return
		}
	}
}

func (t *Transport) writeBatch(batch []sendRequest) error {
	if t == nil || t.zstdW == nil {
		return io.EOF
	}
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	for _, req := range batch {
		if err := t.writeLogicalFrame(req.frame); err != nil {
			return err
		}
	}
	return t.zstdW.Flush()
}

func (t *Transport) writeLogicalFrame(frame []byte) error {
	if len(frame) <= maxPacketPayloadSize {
		return t.writePacket(packetKindFrame, frame)
	}
	offset := 0
	for offset < len(frame) {
		end := offset + maxPacketPayloadSize
		if end > len(frame) {
			end = len(frame)
		}
		kind := packetKindFragmentContinue
		switch {
		case offset == 0:
			kind = packetKindFragmentStart
		case end == len(frame):
			kind = packetKindFragmentEnd
		}
		if err := t.writePacket(kind, frame[offset:end]); err != nil {
			return err
		}
		offset = end
	}
	return nil
}

func (t *Transport) writePacket(kind byte, payload []byte) error {
	if kind != packetKindFrame && kind != packetKindFragmentStart && kind != packetKindFragmentContinue && kind != packetKindFragmentEnd {
		return errors.New("transport/unix: invalid packet kind")
	}
	var header [5]byte
	header[0] = kind
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if err := writeAll(t.zstdW, header[:]); err != nil {
		return err
	}
	return writeAll(t.zstdW, payload)
}
