package webshell

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWebCreateArgsUsesExistingID(t *testing.T) {
	args, err := resolveWebCreateArgs("term-1", nil)
	if err != nil {
		t.Fatalf("resolveWebCreateArgs returned error: %v", err)
	}
	if args != nil {
		t.Fatalf("expected nil create args for existing terminal, got %#v", args)
	}
}

func TestResolveWebCreateArgsRejectsMixedModes(t *testing.T) {
	_, err := resolveWebCreateArgs("term-1", []string{"bash"})
	if err == nil {
		t.Fatal("expected mixed id and command to fail")
	}
}

func TestResolveWebCreateArgsDefaultsToShell(t *testing.T) {
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fake-shell"))
	args, err := resolveWebCreateArgs("", nil)
	if err != nil {
		t.Fatalf("resolveWebCreateArgs returned error: %v", err)
	}
	if len(args) != 1 || args[0] != os.Getenv("SHELL") {
		t.Fatalf("expected shell fallback, got %#v", args)
	}
}

func TestParseWebAttachMode(t *testing.T) {
	mode, err := parseWebAttachMode("observer")
	if err != nil {
		t.Fatalf("parseWebAttachMode returned error: %v", err)
	}
	if mode != "observer" {
		t.Fatalf("expected observer mode, got %q", mode)
	}
	if _, err := parseWebAttachMode("invalid"); err == nil {
		t.Fatal("expected invalid mode to fail")
	}
}
