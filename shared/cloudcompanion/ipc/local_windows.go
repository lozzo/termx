//go:build windows

package ipc

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"golang.org/x/sys/windows"
)

// DefaultEndpoint 返回包含当前 Windows user SID hash 的固定 Named Pipe 路径。
// Named Pipe ACL 仍是授权真值；hash 只避免不同用户的默认名称互相碰撞。
func DefaultEndpoint() string {
	sid, err := currentUserSID()
	if err != nil {
		return `\\.\pipe\termx-cloud-current-user`
	}
	digest := sha256.Sum256([]byte(sid))
	return fmt.Sprintf(`\\.\pipe\termx-cloud-%x`, digest[:8])
}

// Listen 创建只允许当前 Windows user SID 访问的 byte-mode Named Pipe。
// endpoint 为空时只能使用固定默认路径；调用方不得从 Hub 或 release manifest 注入 pipe 名称。
func Listen(endpoint string) (net.Listener, error) {
	endpoint = resolvedEndpoint(endpoint)
	if !trustedPipeNamespace(endpoint) {
		return nil, fmt.Errorf("Cloud Companion Named Pipe path is outside the fixed namespace")
	}
	sid, err := currentUserSID()
	if err != nil {
		return nil, fmt.Errorf("resolve Cloud Companion Windows user SID: %w", err)
	}
	listener, err := winio.ListenPipe(endpoint, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")",
		MessageMode:        false,
		InputBufferSize:    maxFrameBytes,
		OutputBufferSize:   maxFrameBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("listen on Cloud Companion Named Pipe: %w", err)
	}
	return listener, nil
}

func dialLocal(ctx context.Context, endpoint string) (net.Conn, error) {
	endpoint = resolvedEndpoint(endpoint)
	if !trustedPipeNamespace(endpoint) {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "Cloud Companion Named Pipe path is outside the fixed namespace")
	}
	conn, err := winio.DialPipeContext(ctx, endpoint)
	if err != nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "Cloud Companion is not accepting Named Pipe connections")
	}
	return conn, nil
}

func resolvedEndpoint(endpoint string) string {
	if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
		return endpoint
	}
	return DefaultEndpoint()
}

func verifyLocalClient(conn net.Conn) error {
	return verifyNamedPipeProcessOwner(conn, false)
}

func verifyLocalServer(conn net.Conn) error {
	return verifyNamedPipeProcessOwner(conn, true)
}

func verifyNamedPipeProcessOwner(conn net.Conn, server bool) error {
	handleConn, ok := conn.(interface{ Fd() uintptr })
	if !ok || handleConn.Fd() == 0 {
		return fmt.Errorf("Cloud Companion Named Pipe handle is unavailable")
	}
	var processID uint32
	var err error
	if server {
		err = windows.GetNamedPipeServerProcessId(windows.Handle(handleConn.Fd()), &processID)
	} else {
		err = windows.GetNamedPipeClientProcessId(windows.Handle(handleConn.Fd()), &processID)
	}
	if err != nil || processID == 0 {
		return fmt.Errorf("resolve Cloud Companion Named Pipe peer process")
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return fmt.Errorf("open Cloud Companion Named Pipe peer process")
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("open Cloud Companion Named Pipe peer token")
	}
	defer token.Close()
	peerUser, err := token.GetTokenUser()
	if err != nil || peerUser == nil || peerUser.User.Sid == nil {
		return fmt.Errorf("resolve Cloud Companion Named Pipe peer SID")
	}
	currentSID, err := currentUserSID()
	if err != nil || peerUser.User.Sid.String() != currentSID {
		return fmt.Errorf("Cloud Companion Named Pipe peer belongs to another user")
	}
	return nil
}

func trustedPipeNamespace(endpoint string) bool {
	return strings.HasPrefix(strings.ToLower(endpoint), `\\.\pipe\termx-cloud-`)
}

func currentUserSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}
