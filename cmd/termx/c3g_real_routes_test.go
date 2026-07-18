package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	localadapter "github.com/lozzow/termx/client/adapter/local"
	sshadapter "github.com/lozzow/termx/client/adapter/ssh"
	systemadapter "github.com/lozzow/termx/client/adapter/system"
	clientendpoint "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/apipb"
)

func TestC3GRealLocalAndOpenSSHRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("real OpenSSH route harness is disabled in short mode")
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skipf("OpenSSH client unavailable: %v", err)
	}
	sshdPath := "/usr/sbin/sshd"
	if _, err := os.Stat(sshdPath); err != nil {
		t.Skipf("OpenSSH server unavailable: %v", err)
	}

	socketPath, stopServer := startC3GCoreServer(t)
	defer stopServer()
	sshHost := startC3GSSHD(t, sshdPath, sshPath)
	defer sshHost.Close(t)

	target := c3gEndpoint(socketPath, sshHost)
	environment := clientruntime.RoutePlanEnvironment{SupportedRouteKinds: []clientendpoint.RouteKind{clientendpoint.RouteLocalUnix, clientendpoint.RouteSSHWebRTCTCP}}
	sshDialer := &c3gTrackingDialer{inner: sshadapter.NewDialer(sshadapter.Options{
		ClientName: "c3g-real-ssh", SSHBinary: sshHost.clientWrapper,
		ExtraArgs: sshHost.clientArgs(), ConnectTimeout: 3 * time.Second,
	}), started: make(chan struct{})}
	localDialer := &c3gWaitForSSHProcessDialer{
		inner:   localadapter.NewDialer(localadapter.Options{SocketOverride: socketPath, ClientName: "c3g-real-local"}),
		pidFile: sshHost.clientPIDFile,
	}
	dialers, err := clientruntime.NewRouteDialerMap(map[clientendpoint.RouteKind]clientruntime.RouteAttemptDialer{
		clientendpoint.RouteLocalUnix: localDialer, clientendpoint.RouteSSHWebRTCTCP: sshDialer,
	})
	if err != nil {
		t.Fatal(err)
	}
	owner := clientruntime.NewSessionOwner()
	runtime, err := clientruntime.NewClientRuntime(owner, c3gPlanSource{target: target, environment: environment}, systemadapter.Clock{}, dialers)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	localLease, err := runtime.EnsureSession(ctx, clientruntime.ConnectRequest{EndpointID: target.ID, Intent: clientruntime.ConnectIntentInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if localLease.Stamp.RouteID != "local" || sshDialer.calls.Load() != 1 {
		t.Fatalf("full race winner=%#v ssh calls=%d", localLease, sshDialer.calls.Load())
	}
	sshHost.waitForRecordedClientsExited(t)

	oldReady, err := owner.ApplicationSession(localLease)
	if err != nil {
		t.Fatal(err)
	}
	oldApplication, err := clientruntime.NewApplicationSession(oldReady.Stamp(), oldReady)
	if err != nil {
		t.Fatal(err)
	}
	created, err := oldApplication.TerminalCreate(ctx, &apipb.TerminalCreateCommand{Terminal: &apipb.TerminalCreateSpec{
		TerminalId: "c3g-term", Name: "c3g-term", Command: []string{"/bin/sh", "-c", "sleep 30"}, Size: &apipb.TerminalSize{Cols: 80, Rows: 24},
	}})
	if err != nil {
		t.Fatal(err)
	}
	wantRef := created.GetTerminal().GetRef()
	if wantRef.GetEndpointId() != string(target.ID) || wantRef.GetTerminalId() != "c3g-term" {
		t.Fatalf("created terminal ref = %#v", wantRef)
	}

	sshLease, err := runtime.EnsureSession(ctx, clientruntime.ConnectRequest{EndpointID: target.ID, RouteOverride: "ssh", Intent: clientruntime.ConnectIntentInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if sshLease.Stamp.RouteID != "ssh" || sshLease.Stamp.Generation <= localLease.Stamp.Generation {
		t.Fatalf("explicit SSH override lease = %#v after %#v", sshLease, localLease)
	}
	sshReady, err := owner.ApplicationSession(sshLease)
	if err != nil {
		t.Fatal(err)
	}
	sshApplication, err := clientruntime.NewApplicationSession(sshReady.Stamp(), sshReady)
	if err != nil {
		t.Fatal(err)
	}
	got, err := sshApplication.TerminalGet(ctx, &apipb.TerminalGetCommand{Terminal: &apipb.TerminalRef{EndpointId: string(target.ID), TerminalId: "c3g-term"}})
	if err != nil {
		remoteLog, _ := os.ReadFile(sshHost.remoteLog)
		t.Fatalf("SSH TerminalGet: %v\nremote log:\n%s", err, remoteLog)
	}
	if ref := got.GetTerminal().GetRef(); ref.GetEndpointId() != wantRef.GetEndpointId() || ref.GetTerminalId() != wantRef.GetTerminalId() {
		t.Fatalf("route switch changed TerminalRef: local=%#v ssh=%#v", wantRef, ref)
	}
	if _, err := oldApplication.TerminalList(ctx, &apipb.TerminalListCommand{}); clientruntime.CodeOf(err) != clientruntime.ErrorStaleSession || clientruntime.WasAttempted(err) {
		t.Fatalf("old generation operation error = %#v", err)
	}

	if err := runtime.Disconnect(ctx, clientruntime.DisconnectRequest{Stamp: sshLease.Stamp}); err != nil {
		t.Fatal(err)
	}
	stickyLease, err := runtime.EnsureSession(ctx, clientruntime.ConnectRequest{EndpointID: target.ID, Intent: clientruntime.ConnectIntentInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if stickyLease.Stamp.RouteID != "ssh" || stickyLease.Stamp.Generation <= sshLease.Stamp.Generation {
		t.Fatalf("sticky SSH reconnect lease = %#v after %#v", stickyLease, sshLease)
	}
	if err := runtime.Disconnect(ctx, clientruntime.DisconnectRequest{Stamp: stickyLease.Stamp}); err != nil {
		t.Fatal(err)
	}
	sshHost.waitForRecordedClientsExited(t)

	priorityTarget := c3gEndpoint(socketPath, sshHost)
	localPriority, sshPriority := 0, 1
	localRoute := priorityTarget.Routes["local"]
	localRoute.Priority = &localPriority
	priorityTarget.Routes["local"] = localRoute
	sshRoute := priorityTarget.Routes["ssh"]
	sshRoute.Priority = &sshPriority
	priorityTarget.Routes["ssh"] = sshRoute
	priorityTarget.SelectionPolicy = clientendpoint.SelectionPolicy{HedgeDelay: 2 * time.Second, HedgeDelayConfigured: true}
	prioritySSH := &c3gTrackingDialer{inner: sshadapter.NewDialer(sshadapter.Options{
		ClientName: "c3g-priority-ssh", SSHBinary: sshHost.clientWrapper,
		ExtraArgs: sshHost.clientArgs(), ConnectTimeout: 3 * time.Second,
	}), started: make(chan struct{})}
	priorityDialers, err := clientruntime.NewRouteDialerMap(map[clientendpoint.RouteKind]clientruntime.RouteAttemptDialer{
		clientendpoint.RouteLocalUnix:    localadapter.NewDialer(localadapter.Options{SocketOverride: socketPath, ClientName: "c3g-priority-local"}),
		clientendpoint.RouteSSHWebRTCTCP: prioritySSH,
	})
	if err != nil {
		t.Fatal(err)
	}
	priorityOwner := clientruntime.NewSessionOwner()
	priorityRuntime, err := clientruntime.NewClientRuntime(priorityOwner, c3gPlanSource{target: priorityTarget, environment: environment}, systemadapter.Clock{}, priorityDialers)
	if err != nil {
		t.Fatal(err)
	}
	defer priorityRuntime.Close()
	priorityLease, err := priorityRuntime.EnsureSession(ctx, clientruntime.ConnectRequest{EndpointID: priorityTarget.ID, Intent: clientruntime.ConnectIntentInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if priorityLease.Stamp.RouteID != "local" || prioritySSH.calls.Load() != 0 {
		t.Fatalf("priority hedge started lower group before local ready: lease=%#v ssh calls=%d", priorityLease, prioritySSH.calls.Load())
	}
}

type c3gPlanSource struct {
	target      clientendpoint.Endpoint
	environment clientruntime.RoutePlanEnvironment
}

func (source c3gPlanSource) Snapshot(_ context.Context, endpointID clientendpoint.EndpointID) (clientruntime.EndpointPlanSnapshot, error) {
	if endpointID != source.target.ID {
		return clientruntime.EndpointPlanSnapshot{}, fmt.Errorf("unexpected endpoint %q", endpointID)
	}
	return clientruntime.EndpointPlanSnapshot{Endpoint: source.target, Environment: source.environment, ConfigKey: "c3g-real-routes"}, nil
}

type c3gTrackingDialer struct {
	inner   clientruntime.RouteAttemptDialer
	started chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (dialer *c3gTrackingDialer) Dial(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadySession, error) {
	dialer.calls.Add(1)
	dialer.once.Do(func() { close(dialer.started) })
	return dialer.inner.Dial(ctx, request)
}

type c3gWaitForSSHProcessDialer struct {
	inner   clientruntime.RouteAttemptDialer
	pidFile string
}

func (dialer *c3gWaitForSSHProcessDialer) Dial(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadySession, error) {
	for {
		content, _ := os.ReadFile(dialer.pidFile)
		if len(strings.Fields(string(content))) > 0 {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return dialer.inner.Dial(ctx, request)
}

func c3gEndpoint(socketPath string, sshHost *c3gSSHHost) clientendpoint.Endpoint {
	return clientendpoint.Endpoint{
		ID: "c3g", Label: "C3G real routes", LabelSource: clientendpoint.SourceUser,
		ConnectMode: clientendpoint.ConnectAuto, Enabled: true,
		Routes: map[clientendpoint.RouteID]clientendpoint.AccessRoute{
			"local": {ID: "local", Kind: clientendpoint.RouteLocalUnix, Enabled: true, Source: clientendpoint.SourceManual, PolicySource: clientendpoint.SourceUser, Socket: socketPath},
			"ssh": {ID: "ssh", Kind: clientendpoint.RouteSSHWebRTCTCP, Enabled: true, Source: clientendpoint.SourceManual, PolicySource: clientendpoint.SourceUser,
				Host: "127.0.0.1", Port: uint16(sshHost.port), User: sshHost.user, RemoteSignalingAddress: "127.0.0.1:41120", RemoteICETCPAddress: "127.0.0.1:41121"},
		},
	}
}

func startC3GCoreServer(t *testing.T) (string, func()) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	server := newCoreV2TestServer(corev2.WithSocketPath(socketPath))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(ctx) }()
	if err := waitForSocket(socketPath, 3*time.Second, func() error {
		client, err := dialV3Client(socketPath)
		if err != nil {
			return err
		}
		return client.Close()
	}); err != nil {
		cancel()
		t.Fatalf("start C3G core server: %v", err)
	}
	return socketPath, func() {
		cancel()
		_ = server.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("C3G core server did not stop")
		}
	}
}

type c3gSSHHost struct {
	cmd           *exec.Cmd
	port          int
	user          string
	clientKey     string
	knownHosts    string
	clientWrapper string
	clientPIDFile string
	remoteLog     string
}

func startC3GSSHD(t *testing.T, sshdPath, sshPath string) *c3gSSHHost {
	t.Helper()
	dir := t.TempDir()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	port := c3gFreePort(t)
	hostKey := filepath.Join(dir, "host_key")
	clientKey := filepath.Join(dir, "client_key")
	c3gRun(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", hostKey)
	c3gRun(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", clientKey)
	publicKey, err := os.ReadFile(clientKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	termxBinary := filepath.Join(dir, "termx")
	build := exec.Command("go", "build", "-o", termxBinary, "./cmd/termx")
	build.Dir = filepath.Clean("../..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build remote termx: %v\n%s", err, output)
	}
	remoteWrapper := filepath.Join(dir, "remote-termx")
	remoteLog := filepath.Join(dir, "remote.log")
	remoteScript := "#!/bin/sh\nsleep 1\nset -- $SSH_ORIGINAL_COMMAND\nif [ \"$1\" = termx ]; then shift; fi\nexec " + strconv.Quote(termxBinary) + " \"$@\" 2>>" + strconv.Quote(remoteLog) + "\n"
	if err := os.WriteFile(remoteWrapper, []byte(remoteScript), 0o700); err != nil {
		t.Fatal(err)
	}
	authorizedKeys := filepath.Join(dir, "authorized_keys")
	entry := "command=\"" + remoteWrapper + "\",no-agent-forwarding,no-port-forwarding,no-pty,no-user-rc,no-X11-forwarding " + strings.TrimSpace(string(publicKey)) + "\n"
	if err := os.WriteFile(authorizedKeys, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	hostPublic, err := os.ReadFile(hostKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	hostFields := strings.Fields(string(hostPublic))
	if len(hostFields) < 2 {
		t.Fatalf("invalid host public key %q", hostPublic)
	}
	knownHosts := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(knownHosts, []byte(fmt.Sprintf("[127.0.0.1]:%d %s %s\n", port, hostFields[0], hostFields[1])), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "sshd_config")
	configText := fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nChallengeResponseAuthentication no\nUsePAM no\nPermitRootLogin no\nAllowUsers %s\nLogLevel ERROR\n", port, hostKey, filepath.Join(dir, "sshd.pid"), authorizedKeys, current.Username)
	if err := os.WriteFile(config, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sshdPath, "-D", "-e", "-f", config)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			break
		}
		if cmd.ProcessState != nil || time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("sshd did not start: %v %s", err, stderr.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	pidFile := filepath.Join(dir, "ssh-client-pids")
	clientWrapper := filepath.Join(dir, "ssh-client")
	clientScript := "#!/bin/sh\necho $$ >> " + strconv.Quote(pidFile) + "\nexec " + strconv.Quote(sshPath) + " \"$@\"\n"
	if err := os.WriteFile(clientWrapper, []byte(clientScript), 0o700); err != nil {
		t.Fatal(err)
	}
	return &c3gSSHHost{cmd: cmd, port: port, user: current.Username, clientKey: clientKey, knownHosts: knownHosts, clientWrapper: clientWrapper, clientPIDFile: pidFile, remoteLog: remoteLog}
}

func (host *c3gSSHHost) clientArgs() []string {
	return []string{"-i", host.clientKey, "-o", "IdentitiesOnly=yes", "-o", "UserKnownHostsFile=" + host.knownHosts, "-o", "GlobalKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR"}
}

func (host *c3gSSHHost) waitForRecordedClientsExited(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		content, _ := os.ReadFile(host.clientPIDFile)
		allExited := len(strings.Fields(string(content))) > 0
		for _, value := range strings.Fields(string(content)) {
			pid, err := strconv.Atoi(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(pid, 0); err == nil || !errors.Is(err, syscall.ESRCH) {
				allExited = false
				break
			}
		}
		if allExited {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("OpenSSH client processes did not exit; pids=%q", content)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (host *c3gSSHHost) Close(t *testing.T) {
	t.Helper()
	if host == nil || host.cmd == nil || host.cmd.Process == nil {
		return
	}
	_ = host.cmd.Process.Kill()
	_ = host.cmd.Wait()
}

func c3gFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func c3gRun(t *testing.T, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
}
