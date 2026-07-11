package shortcut

import "testing"

func TestRegistrySpecsAreCompleteAndCanonical(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range Specs() {
		if spec.ID == "" || spec.DefaultLabel == "" || len(spec.AllowedScenes) == 0 || spec.Display.Footer == "" || spec.Display.Help == "" || spec.Display.Click == "" {
			t.Fatalf("incomplete shortcut spec: %#v", spec)
		}
		if seen[spec.ID] {
			t.Fatalf("duplicate canonical shortcut spec %q", spec.ID)
		}
		seen[spec.ID] = true
	}
	for _, source := range []string{"panel.close", "pane.close", "tab.jump.3", "floating.summon.9", "help.close"} {
		if _, _, err := ParseInvocation(source); err != nil {
			t.Fatalf("parse %q: %v", source, err)
		}
	}
}

func TestRegistryAllowsKnownCrossSceneAndPaneAliases(t *testing.T) {
	cases := []struct {
		source string
		scene  string
		id     string
	}{
		{source: "system.open_terminal_picker", scene: "floating", id: "system.open_terminal_picker"},
		{source: "system.open_workbench_tree", scene: "workspace", id: "system.open_workbench_tree"},
		{source: "menu.pane", scene: "global", id: "menu.panel"},
	}
	for _, tc := range cases {
		invocation, spec, err := ParseInvocation(tc.source)
		if err != nil || invocation.ID != tc.id || !spec.AllowsScene(tc.scene) {
			t.Fatalf("source=%q scene=%q invocation=%#v spec=%#v err=%v", tc.source, tc.scene, invocation, spec, err)
		}
	}
}

func TestParameterizedInvocationUsesBaseIDAndTypedParam(t *testing.T) {
	invocation, spec, err := ParseInvocation("tab.jump.3")
	if err != nil {
		t.Fatal(err)
	}
	index, ok := invocation.Param("index")
	if invocation.ID != "tab.jump" || invocation.SourceActionID != "tab.jump.3" || !ok || index != 3 || spec.ID != "tab.jump" {
		t.Fatalf("unexpected invocation=%#v spec=%#v", invocation, spec)
	}
	for _, source := range []string{"tab.jump.0", "tab.jump.10", "tab.jump.x", "floating.summon.-1"} {
		if _, _, err := ParseInvocation(source); err == nil {
			t.Fatalf("expected invalid parameter for %q", source)
		}
	}
}
