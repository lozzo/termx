package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	endpointdomain "github.com/muxvia/muxvia/client/endpoint"
	corev2 "github.com/muxvia/muxvia/core"
	"github.com/muxvia/muxvia/internal/protocol"
	"github.com/muxvia/muxvia/proto/apipb"
	"github.com/muxvia/muxvia/shared/filelock"
)

func TestEndpointMutationHonorsRootTimeoutWhileRegistryLocked(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "endpoints.yaml")
	owner, err := filelock.Acquire(registryPath+".lock", true)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{
		"--timeout", "100ms", "endpoint", "--registry", registryPath,
		"add", "ssh", "blocked", "--host", "blocked.example",
	})
	started := time.Now()
	err = command.Execute()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("locked registry timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("locked registry ignored root timeout: %s", elapsed)
	}
}

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

	run("endpoint", "add", "ssh", "west", "--host", "root@west.example", "--credential-ref", "ssh:west")
	run("endpoint", "add", "cloud", "studio", "--device-id", "device-studio", "--device-fingerprint", "SHA256:studio", "--target-device-id", "device-studio", "--credential-ref", "grant:studio", "--relay", "relay_only")
	run("endpoint", "update", "west", "--label", "West build host")
	identityUpdate := newRootCmd()
	identityUpdate.SetOut(io.Discard)
	identityUpdate.SetErr(io.Discard)
	identityUpdate.SetArgs([]string{"endpoint", "update", "west", "--device-id", "device-west", "--device-fingerprint", "SHA256:west"})
	if err := identityUpdate.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("generic endpoint update must not mutate daemon identity: %v", err)
	}
	run("endpoint", "route", "update", "west", "ssh", "--remote-signaling-address", "127.0.0.1:42120", "--remote-ice-tcp-address", "127.0.0.1:42121")
	run("endpoint", "set-default", "west")

	listing := run("endpoint", "list", "--json")
	for _, expected := range []string{`"id":"local"`, `"id":"west"`, `"id":"studio"`, `"default":true`, `"kind":"managed-webrtc"`} {
		if !strings.Contains(listing, expected) {
			t.Fatalf("endpoint list missing %s: %s", expected, listing)
		}
	}
	shown := run("endpoint", "show", "west", "--json")
	if !strings.Contains(shown, `"label":"West build host"`) || !strings.Contains(shown, `"credential_ref":"ssh:west"`) {
		t.Fatalf("unexpected endpoint show: %s", shown)
	}

	run("endpoint", "disable", "west")
	registry, err := endpointdomain.Load("")
	if err != nil {
		t.Fatal(err)
	}
	registry, err = registry.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if registry.Default != endpointdomain.DefaultEndpointID || registry.Endpoints["west"].Enabled {
		t.Fatalf("disable did not select enabled default: %#v", registry)
	}
	run("endpoint", "enable", "west")
	run("endpoint", "remove", "studio")
	if _, ok := mustLoadEndpointRegistry(t).Endpoints["studio"]; ok {
		t.Fatal("removed endpoint is still present")
	}

	info, err := os.Stat(filepath.Join(configHome, "termx", endpointdomain.DefaultFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode = %o, want 600", info.Mode().Perm())
	}
}

func TestEndpointAddCreatesExplicitRegistryWithoutInventingLocalEndpoint(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "custom", "endpoints.yaml")
	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"endpoint", "--registry", registryPath, "add", "ssh", "west", "--host", "west.example"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	registry, err := endpointdomain.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Endpoints) != 1 || registry.Default != "west" {
		t.Fatalf("explicit registry should contain only the imported endpoint: %#v", registry)
	}
	if _, exists := registry.Endpoints[endpointdomain.DefaultEndpointID]; exists {
		t.Fatal("explicit registry creation invented a local endpoint")
	}
}

func TestTerminalCommandsRouteDuplicateIDsToOwningLocalEndpoint(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	localSocket, localClient, closeLocal := startCLIEndpointServer(t)
	defer closeLocal()
	westSocket, westClient, closeWest := startCLIEndpointServer(t)
	defer closeWest()

	for _, client := range []*protocol.Client{localClient, westClient} {
		if _, err := createCLIProtoTerminal(context.Background(), client, &apipb.TerminalCreateSpec{
			TerminalId: "same", Name: "same", Command: []string{"/bin/sh", "-c", "sleep 30"}, Size: &apipb.TerminalSize{Cols: 80, Rows: 24},
		}); err != nil {
			t.Fatal(err)
		}
	}
	registry := endpointdomain.Registry{
		Version: endpointdomain.RegistryVersion, Default: "local",
		Endpoints: map[endpointdomain.EndpointID]endpointdomain.Endpoint{
			"local": testLocalEndpoint("local", "Local", localSocket, endpointdomain.ConnectAuto, true),
			"west":  testLocalEndpoint("west", "West", westSocket, endpointdomain.ConnectOnDemand, true),
		},
	}
	if err := endpointdomain.Save("", registry); err != nil {
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
	server := newCoreV2TestServer(corev2.WithSocketPath(socketPath))
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

func mustLoadEndpointRegistry(t *testing.T) endpointdomain.Registry {
	t.Helper()
	registry, err := endpointdomain.Load("")
	if err != nil {
		t.Fatal(err)
	}
	registry, err = registry.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
