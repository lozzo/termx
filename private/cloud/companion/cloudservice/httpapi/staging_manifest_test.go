package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadManifestAllowsPublicTURNOnlyForStagingSSH(t *testing.T) {
	manifest := Manifest{
		Version: ManifestVersion, Profile: ProfileStagingSSH,
		ControlPlaneURL: "http://127.0.0.1:42001", HubURL: "http://127.0.0.1:42002",
		RelayURL: "turn:114.66.58.243:41003?transport=udp", HubID: "hub", Region: "region",
		AccountLabel: "staging", EnrollmentCode: "one-time", StartedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
	}
	path := filepath.Join(t.TempDir(), "runtime.json")
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err != nil {
		t.Fatal(err)
	}
	manifest.Profile = ProfileDevLocal
	payload, _ = json.Marshal(manifest)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("dev-local manifest accepted a public TURN address")
	}
}

func TestLoadManifestAllowsPublicHTTPOnlyForExplicitStagingProfile(t *testing.T) {
	manifest := Manifest{
		Version: ManifestVersion, Profile: ProfileStagingPublicHTTP,
		ControlPlaneURL: "http://114.66.58.243:41101", HubURL: "http://114.66.58.243:41102",
		RelayURL: "turn:114.66.58.243:41003?transport=udp", HubID: "hub", Region: "region",
		AccountLabel: "public-http-test-only", EnrollmentCode: "already-claimed", StartedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
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
