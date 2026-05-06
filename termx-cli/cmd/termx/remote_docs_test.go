package main

import (
	"os"
	"strings"
	"testing"
)

func TestTermxCLIBothModeDocsAndSmokeScript(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	text := string(readme)
	for _, want := range []string{
		"termx remote login --server <web-control-url>",
		"termx daemon",
		"termx remote enable --cloud --local",
		"termx remote status",
		"local_web_url",
		"hub_url",
		"agent online",
		"VITE_CONTROL_URL=http://localhost:12306 npm run dev",
		"http://localhost:5173/localweb.html",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("README missing %q", want)
		}
	}

	script, err := os.ReadFile("../../scripts/smoke-both.sh")
	if err != nil {
		t.Fatalf("read smoke script: %v", err)
	}
	smoke := string(script)
	for _, want := range []string{
		"curl",
		"/api/health",
		"termx remote status --json",
		"local.http_url",
		"remote.state",
		"remote.control_url",
		"remote.hub_url",
		"CONTROL_URL",
		"TERMX_TOKEN",
		"/api/v1/machines",
	} {
		if !strings.Contains(smoke, want) {
			t.Fatalf("smoke script missing %q", want)
		}
	}
	if strings.Contains(smoke, "cloud-secret") || strings.Contains(smoke, "termx-development-hub-secret-change-me") {
		t.Fatal("smoke script must not embed development secrets")
	}
	if strings.Contains(smoke, "skipped Web Control") || strings.Contains(smoke, `!= "online" &&`) {
		t.Fatal("smoke script must require web-control token and remote online state")
	}
}
