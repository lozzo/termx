// Package ssh implements a client-side SSH transport for termx daemon protocol frames.
package ssh

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/shared/transport"
)

const (
	defaultSSHBinary      = "ssh"
	defaultRemoteCommand  = "termx"
	defaultConnectTimeout = 10 * time.Second
	maxFrameSize          = 64 << 20
)

// DialOptions 描述 SSH transport 的 dial identity。
// Address/AuthRef/RemoteSocket 来自 connections.yaml；Host key 和认证由 OpenSSH 配置与 known_hosts 处理，不能由 endpoint label 替代。
type DialOptions struct {
	Address        string
	AuthRef        string
	RemoteSocket   string
	RemoteCommand  string
	SSHBinary      string
	ConnectTimeout time.Duration
	ExtraArgs      []string
}

// Transport 是通过 OpenSSH stdio proxy 承载的 termx frame transport。
// 它只传输 termx daemon wire frame；远端命令固定为 termx stdio-proxy，不能退化为交互 shell。
type Transport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *limitedBuffer

	sendMu       sync.Mutex
	recvMu       sync.Mutex
	once         sync.Once
	killOnce     sync.Once
	dialDoneOnce sync.Once
	dialDone     chan struct{}
	done         chan struct{}
	waitCh       chan error
	closeErr     error
}

// Dial 启动 OpenSSH 并连接远端 termx stdio-proxy。
// 返回的 transport 只在远端 proxy 完成 daemon socket 连接后才会通过 protocol Hello；SSH 认证或 host key 失败会作为 transport 错误回传。
func Dial(ctx context.Context, opts DialOptions) (*Transport, error) {
	binaryName, args, err := BuildCommand(opts)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(binaryName, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr := newLimitedBuffer(32 << 10)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start ssh transport: %w", err)
	}
	t := &Transport{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		done:     make(chan struct{}),
		waitCh:   make(chan error, 1),
		dialDone: make(chan struct{}),
	}
	go t.wait()
	go t.watchDialContext(ctx)
	return t, nil
}

// CommitReady 把已完成 SSH 认证、daemon identity proof 与 protocol Hello 的进程生命周期移交给 ReadyPeerSession owner。
// 调用后原始 dial context 取消不再终止 winner；后续只能由 Transport.Close 或进程自身退出结束。
func (t *Transport) CommitReady() {
	if t == nil {
		return
	}
	t.dialDoneOnce.Do(func() { close(t.dialDone) })
}

// BuildCommand 生成 OpenSSH 命令参数。
// 该函数用于 harness 校验：SSH transport 必须执行远端 termx stdio-proxy，而不是打开 shell。
func BuildCommand(opts DialOptions) (string, []string, error) {
	target, err := sshTarget(opts.Address, opts.AuthRef)
	if err != nil {
		return "", nil, err
	}
	binaryName := strings.TrimSpace(opts.SSHBinary)
	if binaryName == "" {
		binaryName = defaultSSHBinary
	}
	remoteCommand := strings.TrimSpace(opts.RemoteCommand)
	if remoteCommand == "" {
		remoteCommand = defaultRemoteCommand
	}
	remoteSocket := strings.TrimSpace(opts.RemoteSocket)
	if remoteSocket == "" {
		remoteSocket = "auto"
	}
	timeout := opts.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	args := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", int(timeout.Round(time.Second).Seconds())),
	}
	args = append(args, opts.ExtraArgs...)
	args = append(args, target, remoteCommand, "--socket", remoteSocket, "daemon", "stdio-proxy")
	return binaryName, args, nil
}

// ServeProxy 在远端 termx stdio-proxy 进程中桥接 stdin/stdout 与 daemon transport。
// stdin/stdout 使用长度前缀 frame；target 仍是本机 unix socket transport，不解析 protocol payload。
func ServeProxy(ctx context.Context, target transport.Transport, stdin io.Reader, stdout io.Writer) error {
	if target == nil {
		return errors.New("ssh transport proxy target is nil")
	}
	errCh := make(chan error, 2)
	go func() {
		errCh <- copyFramesToTarget(ctx, target, stdin)
	}()
	go func() {
		errCh <- copyFramesFromTarget(ctx, target, stdout)
	}()
	err := <-errCh
	_ = target.Close()
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Send 发送一个完整 termx protocol frame 到远端 daemon。
// frame 边界由 SSH transport 自己的长度前缀维护，不依赖 shell 或 PTY 行缓冲。
func (t *Transport) Send(frame []byte) error {
	if t == nil || t.stdin == nil {
		return io.EOF
	}
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	select {
	case <-t.done:
		return t.waitError()
	default:
	}
	if err := writeFrame(t.stdin, frame); err != nil {
		return fmt.Errorf("ssh transport send: %w", err)
	}
	return nil
}

// Recv 接收一个完整 termx protocol frame。
// 如果 SSH 进程退出，错误会带上 OpenSSH/远端 proxy stderr 摘要，便于展示认证、host key 或远端 socket 失败。
func (t *Transport) Recv() ([]byte, error) {
	if t == nil || t.stdout == nil {
		return nil, io.EOF
	}
	t.recvMu.Lock()
	defer t.recvMu.Unlock()
	frame, err := readFrame(t.stdout)
	if err != nil {
		select {
		case <-t.done:
			return nil, t.waitError()
		default:
		}
		return nil, err
	}
	return frame, nil
}

// Close 关闭 SSH transport 并结束远端 stdio-proxy。
// 它只管理 transport 进程，不修改 endpoint registry 或 TUI reducer state。
func (t *Transport) Close() error {
	if t == nil {
		return nil
	}
	t.once.Do(func() {
		select {
		case <-t.done:
			t.closeErr = t.waitError()
			return
		default:
		}
		t.CommitReady()
		if t.stdin != nil {
			_ = t.stdin.Close()
		}
		t.killProcess()
		<-t.done
	})
	return t.closeErr
}

func (t *Transport) watchDialContext(ctx context.Context) {
	select {
	case <-ctx.Done():
		if t.stdin != nil {
			_ = t.stdin.Close()
		}
		t.killProcess()
	case <-t.dialDone:
	case <-t.done:
	}
}

func (t *Transport) killProcess() {
	t.killOnce.Do(func() {
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
	})
}

// Done 返回 SSH 进程生命周期结束信号。
// protocol client 用它区分正常 frame 阻塞和 transport 已关闭。
func (t *Transport) Done() <-chan struct{} {
	if t == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return t.done
}

func (t *Transport) wait() {
	err := t.cmd.Wait()
	t.waitCh <- err
	close(t.done)
}

func (t *Transport) waitError() error {
	var err error
	select {
	case err = <-t.waitCh:
		t.waitCh <- err
	default:
		return io.EOF
	}
	if err == nil {
		return io.EOF
	}
	detail := strings.TrimSpace(t.stderr.String())
	if detail == "" {
		return fmt.Errorf("ssh transport closed: %w", err)
	}
	return fmt.Errorf("ssh transport closed: %w: %s", err, detail)
}

func sshTarget(address string, authRef string) (string, error) {
	authRef = strings.TrimSpace(authRef)
	if authRef != "" {
		if alias, ok := strings.CutPrefix(authRef, "ssh:"); ok {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				return "", fmt.Errorf("ssh auth_ref %q has empty alias", authRef)
			}
			return alias, nil
		}
		return "", fmt.Errorf("unsupported ssh auth_ref %q; expected empty or ssh:<host-alias>", authRef)
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return "", errors.New("ssh address is required")
	}
	return address, nil
}

func copyFramesToTarget(ctx context.Context, target transport.Transport, stdin io.Reader) error {
	reader := bufio.NewReader(stdin)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		frame, err := readFrame(reader)
		if err != nil {
			return err
		}
		if err := target.Send(frame); err != nil {
			return err
		}
	}
}

func copyFramesFromTarget(ctx context.Context, target transport.Transport, stdout io.Writer) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		frame, err := target.Recv()
		if err != nil {
			return err
		}
		if err := writeFrame(stdout, frame); err != nil {
			return err
		}
	}
}

func writeFrame(w io.Writer, frame []byte) error {
	if len(frame) > maxFrameSize {
		return fmt.Errorf("ssh transport frame too large: %d", len(frame))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(frame)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(frame) == 0 {
		return nil
	}
	_, err := w.Write(frame)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size > maxFrameSize {
		return nil, fmt.Errorf("ssh transport frame too large: %d", size)
	}
	frame := make([]byte, int(size))
	if _, err := io.ReadFull(r, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

type limitedBuffer struct {
	mu    sync.Mutex
	limit int
	buf   bytes.Buffer
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 || b.buf.Len() < b.limit {
		remaining := b.limit - b.buf.Len()
		if b.limit <= 0 || remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.buf.Write(p[:remaining])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
