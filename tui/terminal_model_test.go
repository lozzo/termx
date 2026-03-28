package tui

import "testing"

func TestTerminalProxyStoresIdentityAndMetadata(t *testing.T) {
	terminal := &Terminal{ID: "term-1"}
	terminal.SetMetadata("worker", []string{"tail", "-f", "worker.log"}, map[string]string{"role": "worker"})

	if terminal.Name != "worker" {
		t.Fatalf("expected terminal name worker, got %q", terminal.Name)
	}
	if len(terminal.Command) != 3 || terminal.Command[0] != "tail" {
		t.Fatalf("expected command to be stored, got %v", terminal.Command)
	}
	if terminal.Tags["role"] != "worker" {
		t.Fatalf("expected role tag worker, got %q", terminal.Tags["role"])
	}
}

func TestTerminalProxyClonesMetadataInput(t *testing.T) {
	command := []string{"bash"}
	tags := map[string]string{"role": "dev"}
	terminal := &Terminal{ID: "term-1"}
	terminal.SetMetadata("shell", command, tags)

	command[0] = "zsh"
	tags["role"] = "ops"

	if terminal.Command[0] != "bash" {
		t.Fatalf("expected terminal command clone, got %v", terminal.Command)
	}
	if terminal.Tags["role"] != "dev" {
		t.Fatalf("expected terminal tag clone, got %v", terminal.Tags)
	}
}
