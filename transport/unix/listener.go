package unix

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"

	"github.com/lozzow/termx/transport"
)

type Transport struct {
	conn net.Conn
	done chan struct{}
	once sync.Once
}

func Dial(path string) (*Transport, error) {
	actualPath, _ := resolveSocketPath(path)
	conn, err := net.Dial("unix", actualPath)
	if err != nil {
		return nil, err
	}
	return &Transport{conn: conn, done: make(chan struct{})}, nil
}

func (t *Transport) Send(frame []byte) error {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(frame)))
	if _, err := t.conn.Write(header); err != nil {
		return err
	}
	_, err := t.conn.Write(frame)
	return err
}

func (t *Transport) Recv() ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(t.conn, header); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header)
	buf := make([]byte, n)
	if _, err := io.ReadFull(t.conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (t *Transport) Close() error {
	t.once.Do(func() {
		close(t.done)
		_ = t.conn.Close()
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
		return &Transport{conn: res.conn, done: make(chan struct{})}, nil
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
