package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/controller"
)

func TestSupervisorStartsControllerAndTwoIndependentEdges(t *testing.T) {
	root := findRepoRoot(t)
	manifestPath := filepath.Join(t.TempDir(), "runtime.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--manifest", manifestPath, "--repo-root", root}) }()
	var manifest supervisorManifest
	if err := waitManifest(ctx, manifestPath, &manifest, done); err != nil {
		cancel()
		t.Fatal(err)
	}
	if manifest.Controller.PID == 0 || len(manifest.Edges) != 2 || len(manifest.Processes) != 3 {
		cancel()
		t.Fatalf("supervisor manifest = %#v", manifest)
	}
	credentialsInfo, err := os.Stat(manifest.CredentialsPath)
	if err != nil {
		cancel()
		t.Fatalf("development credentials = %q err=%v", manifest.CredentialsPath, err)
	}
	if credentialsInfo.Mode().Perm() != 0o600 {
		cancel()
		t.Fatalf("development credentials = %q mode=%v", manifest.CredentialsPath, credentialsInfo.Mode().Perm())
	}
	for _, pageURL := range []string{manifest.Controller.PublicURL + "/login", manifest.Controller.PublicURL + "/account", manifest.Controller.OperatorURL + "/operator"} {
		response, requestErr := http.Get(pageURL)
		if requestErr != nil || response.StatusCode != http.StatusOK {
			cancel()
			t.Fatalf("Controller Web page %q status=%v err=%v", pageURL, response.StatusCode, requestErr)
		}
		response.Body.Close()
	}
	pids := map[int]bool{manifest.Controller.PID: true}
	hubs := map[string]bool{}
	for _, edge := range manifest.Edges {
		if edge.PID == 0 || edge.ControlGeneration != 1 || edge.ProjectionRevision != 1 || edge.HubID == "" || edge.RelayID == "" || edge.HealthURL == "" || edge.RelayURL == "" {
			cancel()
			t.Fatalf("Edge manifest = %#v", edge)
		}
		if pids[edge.PID] || hubs[edge.HubID] {
			cancel()
			t.Fatalf("duplicate Edge process or Hub identity: %#v", manifest.Edges)
		}
		pids[edge.PID], hubs[edge.HubID] = true, true
		if err := waitHealth(ctx, edge.HealthURL+"/healthz", true); err != nil {
			cancel()
			t.Fatal(err)
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("supervisor exit = %v", err)
	}
	for _, endpoint := range []string{manifest.Controller.OperatorURL + "/healthz", manifest.Edges[0].HealthURL + "/healthz", manifest.Edges[1].HealthURL + "/healthz"} {
		if !waitUnavailable(endpoint, 5*time.Second) {
			t.Fatalf("child listener survived supervisor shutdown: %s", endpoint)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.work")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("repository root not found")
		}
		directory = parent
	}
}

func controllerPostgresDSN(t *testing.T, manifest supervisorManifest) string {
	t.Helper()
	record := processByName(t, manifest.Processes, "controller")
	config, err := controller.LoadConfig(record.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.PostgresDSN == "" {
		t.Fatal("Controller test config does not contain PostgreSQL DSN")
	}
	return config.PostgresDSN
}

func waitUnavailable(endpoint string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 100 * time.Millisecond}
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err != nil {
			return true
		}
		_ = response.Body.Close()
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
