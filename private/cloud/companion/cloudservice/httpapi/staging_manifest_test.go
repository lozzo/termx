package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRepositoryStagingPublicManifestMatchesStrictContract(t *testing.T) {
	if _, err := LoadManifest(filepath.Join("..", "..", "config", "staging-public-https.json")); err != nil {
		t.Fatalf("repository staging manifest: %v", err)
	}
}

func TestManifestRejectsStaticHubDirectoryFields(t *testing.T) {
	payload, err := json.Marshal(map[string]any{"version": ManifestVersion, "profile": ProfileDevLocal, "control_plane_url": "http://127.0.0.1:42001", "hub_url": "http://127.0.0.1:42002", "account_label": "staging", "started_at": time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseManifest(payload); err == nil {
		t.Fatal("manifest accepted a static Hub directory")
	}
}

func TestLoadManifestAllowsPublicHTTPOnlyForExplicitStagingProfile(t *testing.T) {
	manifest := Manifest{
		Version: ManifestVersion, Profile: ProfileStagingPublicHTTP,
		ControlPlaneURL: "http://114.66.58.243:41101",
		AccountLabel:    "public-http-test-only", StartedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
	}
	path := filepath.Join(t.TempDir(), "runtime.json")
	payload, _ := json.Marshal(manifest)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err != nil {
		t.Fatal(err)
	}
	manifest.Profile = ProfileStagingSSH
	payload, _ = json.Marshal(manifest)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("staging-ssh manifest accepted public HTTP origins")
	}
}

func TestLoadManifestAllowsPublicHTTPSOnlyForExplicitStagingProfile(t *testing.T) {
	manifest := Manifest{
		Version: ManifestVersion, Profile: ProfileStagingPublicHTTPS,
		ControlPlaneURL: "https://muxvia.com",
		AccountLabel:    "public-https-test-only", StartedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
	}
	payload, _ := json.Marshal(manifest)
	if _, err := ParseManifest(payload); err != nil {
		t.Fatal(err)
	}
	manifest.Profile = ProfileStagingSSH
	payload, _ = json.Marshal(manifest)
	if _, err := ParseManifest(payload); err == nil {
		t.Fatal("staging-ssh manifest accepted public HTTPS origins")
	}
}
