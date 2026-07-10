package app

import (
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/shortcut"
)

func TestShortcutDispatcherPreservesParameterizedAndDirectionalSemantics(t *testing.T) {
	cases := []struct {
		source  string
		kind    input.IntentKind
		command string
		action  input.ShellAction
		reason  string
	}{
		{source: "panel.focus_prev", kind: input.IntentPaneCommand, command: "pane focus-prev"},
		{source: "resize.pan_left", kind: input.IntentWorkbenchCommand, command: "terminal layout pan-left"},
		{source: "tab.jump.3", kind: input.IntentWorkbenchCommand, command: "tab jump 3"},
		{source: "floating.summon.3", kind: input.IntentShellAction, action: input.ShellActionFloatingSummon, reason: "3"},
	}
	for _, tc := range cases {
		invocation, _, err := shortcut.ParseInvocation(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		intent, ok := shortcutIntentForInvocation(invocation, input.InputEvent{})
		if !ok || intent.Kind != tc.kind || intent.Command != tc.command || intent.Action != tc.action || intent.Reason != tc.reason || intent.Invocation.ID != invocation.ID {
			t.Fatalf("dispatch %q: %#v ok=%v", tc.source, intent, ok)
		}
	}
}

func TestShortcutDispatcherKeepsKillActionsDistinct(t *testing.T) {
	for _, source := range []string{"panel.kill", "panel.kill_and_close"} {
		invocation, _, err := shortcut.ParseInvocation(source)
		if err != nil {
			t.Fatal(err)
		}
		intent, ok := shortcutIntentForInvocation(invocation, input.InputEvent{})
		if !ok || intent.Invocation.ID != source {
			t.Fatalf("dispatch %q lost action identity: %#v", source, intent)
		}
	}
}
