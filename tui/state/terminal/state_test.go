package terminal

import (
	"reflect"
	"testing"

	"github.com/lozzow/termx/tui/state/types"
)

func TestSnapshotFromMetadataClonesCommandAndTags(t *testing.T) {
	meta := Metadata{
		ID:      types.TerminalID("term-1"),
		Name:    "main shell",
		Command: []string{"bash", "-lc", "pwd"},
		Tags: map[string]string{
			"role": "primary",
		},
	}

	snapshot := SnapshotFromMetadata(meta)
	if snapshot.TerminalID != meta.ID {
		t.Fatalf("expected terminal id %q, got %q", meta.ID, snapshot.TerminalID)
	}
	if snapshot.TerminalName != meta.Name {
		t.Fatalf("expected terminal name %q, got %q", meta.Name, snapshot.TerminalName)
	}
	if !reflect.DeepEqual(snapshot.Command, meta.Command) {
		t.Fatalf("expected command %v, got %v", meta.Command, snapshot.Command)
	}
	if !reflect.DeepEqual(snapshot.Tags, meta.Tags) {
		t.Fatalf("expected tags %v, got %v", meta.Tags, snapshot.Tags)
	}

	meta.Command[0] = "zsh"
	meta.Tags["role"] = "secondary"
	if snapshot.Command[0] != "bash" {
		t.Fatalf("expected snapshot command to be cloned, got %v", snapshot.Command)
	}
	if snapshot.Tags["role"] != "primary" {
		t.Fatalf("expected snapshot tags to be cloned, got %v", snapshot.Tags)
	}
}
