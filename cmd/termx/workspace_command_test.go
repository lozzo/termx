package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	endpointdomain "github.com/lozzow/termx/client/endpoint"
)

func TestWorkspaceCommandUsesDaemonSnapshotAndVersionedMutations(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	socketPath, _, closeServer := startCLIEndpointServer(t)
	defer closeServer()
	if err := endpointdomain.Save("", endpointdomain.Registry{
		Version: endpointdomain.RegistryVersion, Default: endpointdomain.DefaultEndpointID,
		Endpoints: map[endpointdomain.EndpointID]endpointdomain.Endpoint{
			endpointdomain.DefaultEndpointID: testLocalEndpoint(endpointdomain.DefaultEndpointID, "Local", socketPath, endpointdomain.ConnectAuto, true),
		},
	}); err != nil {
		t.Fatal(err)
	}

	listing := executeWorkspaceCLI(t, "workspace", "list", "--json")
	if !strings.Contains(listing, `"id":"workspace-main"`) || !strings.Contains(listing, `"version":1`) {
		t.Fatalf("unexpected workspace list: %s", listing)
	}
	created := executeWorkspaceCLI(t, "workspace", "create", "--id", "workspace-two", "--name", "Build", "--json")
	if !strings.Contains(created, `"action":"workspace.create"`) || !strings.Contains(created, `"version":2`) {
		t.Fatalf("unexpected workspace create: %s", created)
	}
	mainShown := executeWorkspaceCLI(t, "workspace", "show", "workspace-main", "--json")
	if !strings.Contains(mainShown, `"active":false`) {
		t.Fatalf("show rewrote inactive workspace as active: %s", mainShown)
	}
	shown := executeWorkspaceCLI(t, "workspace", "show", "workspace-two", "--json")
	if !strings.Contains(shown, `"name":"Build"`) || !strings.Contains(shown, `"active":true`) {
		t.Fatalf("unexpected workspace show: %s", shown)
	}
	renamed := executeWorkspaceCLI(t, "workspace", "rename", "workspace-two", "Release", "--json")
	if !strings.Contains(renamed, `"action":"workspace.rename"`) || !strings.Contains(renamed, `"version":3`) {
		t.Fatalf("unexpected workspace rename: %s", renamed)
	}
	exportPath := filepath.Join(t.TempDir(), "workspace.json")
	executeWorkspaceCLI(t, "workspace", "export", "workspace-two", "--out", exportPath)
	payload, err := os.ReadFile(exportPath)
	if err != nil || !strings.Contains(string(payload), `"kind": "workspace_export"`) || !strings.Contains(string(payload), `"name": "Release"`) {
		t.Fatalf("workspace export = %s, %v", payload, err)
	}
	if info, err := os.Stat(exportPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("workspace export mode = %v, %v", info, err)
	}
	removed := executeWorkspaceCLI(t, "workspace", "remove", "workspace-two", "--json")
	if !strings.Contains(removed, `"action":"workspace.delete"`) || !strings.Contains(removed, `"version":4`) {
		t.Fatalf("unexpected workspace remove: %s", removed)
	}
	listing = executeWorkspaceCLI(t, "workspace", "list", "--json")
	if strings.Contains(listing, `"id":"workspace-two"`) {
		t.Fatalf("removed workspace remained in daemon snapshot: %s", listing)
	}
}

func executeWorkspaceCLI(t *testing.T, args ...string) string {
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
