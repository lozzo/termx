package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/internal/protocol"
	remotev2client "github.com/lozzow/termx/remote/client"
	"github.com/lozzow/termx/shared/connection"
)

func TestEndpointRegistryCommandLifecycle(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	run := func(args ...string) string {
		t.Helper()
		command := newRootCmd()
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(io.Discard)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("termx %s: %v", strings.Join(args, " "), err)
		}
		return output.String()
	}

	run("endpoint", "add", "ssh", "west", "--address", "root@west.example", "--auth-ref", "ssh:west")
	run("endpoint", "add", "cloud", "studio", "--hub-device-id", "device-studio", "--device-fingerprint", "SHA256:studio", "--grant-ref", "grant:studio", "--relay", "relay_only")
	run("endpoint", "update", "west", "--label", "West build host", "--remote-socket", "/run/user/1000/termx.sock")
	run("endpoint", "set-default", "west")

	listing := run("endpoint", "list", "--json")
	for _, expected := range []string{`"id":"local"`, `"id":"west"`, `"id":"studio"`, `"default":true`, `"transport":"hub-p2p"`} {
		if !strings.Contains(listing, expected) {
			t.Fatalf("endpoint list missing %s: %s", expected, listing)
		}
	}
	shown := run("endpoint", "show", "west", "--json")
	if !strings.Contains(shown, `"label":"West build host"`) || !strings.Contains(shown, `"auth_ref":"ssh:west"`) {
		t.Fatalf("unexpected endpoint show: %s", shown)
	}

	run("endpoint", "disable", "west")
	registry, err := connection.Load("")
	if err != nil {
		t.Fatal(err)
	}
	registry, err = registry.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if registry.Default != connection.DefaultEndpointID || registry.Connections["west"].Enabled {
		t.Fatalf("disable did not select enabled default: %#v", registry)
	}
	run("endpoint", "enable", "west")
	run("endpoint", "remove", "studio")
	if _, ok := mustLoadEndpointRegistry(t).Connections["studio"]; ok {
		t.Fatal("removed endpoint is still present")
	}

	info, err := os.Stat(filepath.Join(configHome, "termx", connection.DefaultFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode = %o, want 600", info.Mode().Perm())
	}
}

func TestEndpointTransportFailureDoesNotFallbackToLocal(t *testing.T) {
	oldLocal := dialCLIEndpointLocal
	oldSSH := dialCLIEndpointSSH
	oldCloud := dialCLIEndpointCloud
	t.Cleanup(func() {
		dialCLIEndpointLocal = oldLocal
		dialCLIEndpointSSH = oldSSH
		dialCLIEndpointCloud = oldCloud
	})

	localCalls := 0
	dialCLIEndpointLocal = func(string, string, *slog.Logger) (*protocol.Client, error) {
		localCalls++
		return nil, errors.New("local dial must not run")
	}
	sshFailure := errors.New("ssh transport failed")
	dialCLIEndpointSSH = func(context.Context, context.Context, connection.Config) (*protocol.Client, error) {
		return nil, sshFailure
	}
	cloudFailure := errors.New("managed transport failed")
	dialCLIEndpointCloud = func(context.Context, connection.Config) (*protocol.Client, remotev2client.Session, error) {
		return nil, remotev2client.Session{}, cloudFailure
	}

	sshConfig := connection.Config{ID: "west", Transport: connection.TransportSSH, Address: "west.example", Enabled: true}
	if _, _, err := openEndpointProtocolClient(context.Background(), sshConfig, "", ""); !errors.Is(err, sshFailure) {
		t.Fatalf("SSH error = %v", err)
	}
	cloudConfig := connection.Config{ID: "studio", Transport: connection.TransportHubP2P, Enabled: true}
	if _, _, err := openEndpointProtocolClient(context.Background(), cloudConfig, "", ""); !errors.Is(err, cloudFailure) {
		t.Fatalf("Cloud error = %v", err)
	}
	if localCalls != 0 {
		t.Fatalf("remote failure attempted %d local fallback dials", localCalls)
	}
}

func TestTerminalCommandsRouteDuplicateIDsToOwningLocalEndpoint(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	localSocket, localClient, closeLocal := startCLIEndpointServer(t)
	defer closeLocal()
	westSocket, westClient, closeWest := startCLIEndpointServer(t)
	defer closeWest()

	for _, client := range []*protocol.Client{localClient, westClient} {
		if _, err := client.Create(context.Background(), protocol.CreateParams{
			ID: "same", Name: "same", Command: []string{"/bin/sh", "-c", "sleep 30"}, Size: protocol.Size{Cols: 80, Rows: 24},
		}); err != nil {
			t.Fatal(err)
		}
	}
	registry := connection.Registry{
		Version: 1, Default: "local",
		Connections: map[connection.EndpointID]connection.Config{
			"local": {ID: "local", Label: "Local", Transport: connection.TransportLocal, ConnectMode: connection.ConnectAuto, Enabled: true, Socket: localSocket},
			"west":  {ID: "west", Label: "West", Transport: connection.TransportLocal, ConnectMode: connection.ConnectOnDemand, Enabled: true, Socket: westSocket},
		},
	}
	if err := connection.Save("", registry); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		command := newRootCmd()
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(io.Discard)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("termx %s: %v", strings.Join(args, " "), err)
		}
		return output.String()
	}
	listing := run("terminal", "list", "--all-endpoints", "--json")
	if strings.Count(listing, `"terminal_id":"same"`) != 2 || !strings.Contains(listing, `"target":"local:same"`) || !strings.Contains(listing, `"target":"west:same"`) {
		t.Fatalf("duplicate terminal IDs lost endpoint ownership: %s", listing)
	}
	shown := run("terminal", "show", "west:same", "--json")
	if !strings.Contains(shown, `"target":"west:same"`) || strings.Contains(shown, `"target":"local:same"`) {
		t.Fatalf("show routed to wrong endpoint: %s", shown)
	}
}

func startCLIEndpointServer(t *testing.T) (string, *protocol.Client, func()) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	server := corev2.NewServer(corev2.WithSocketPath(socketPath))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(ctx) }()
	if err := waitForSocket(socketPath, 2*time.Second, func() error {
		client, err := dialV3Client(socketPath)
		if err != nil {
			return err
		}
		return client.Close()
	}); err != nil {
		cancel()
		t.Fatalf("endpoint server did not become ready: %v", err)
	}
	client, err := dialV3Client(socketPath)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return socketPath, client, func() {
		_ = client.Close()
		cancel()
		_ = server.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("endpoint server did not stop")
		}
	}
}

func mustLoadEndpointRegistry(t *testing.T) connection.Registry {
	t.Helper()
	registry, err := connection.Load("")
	if err != nil {
		t.Fatal(err)
	}
	registry, err = registry.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
